// Local-model metadata is read from the private CODEX_HOME catalog, validated
// against the pinned native schema over every row, and retained by value. The
// selected row must disable Responses Lite/search/reasoning summary, use direct
// tools and multi-agent v1, and declare the explicitly requested effort. Never
// infer native metadata from the hosted slug used to register a local engine.
package codex

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/catalogfile"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// catalogConfigKey is the top-level config.toml key carrying the catalog path.
const catalogConfigKey = "model_catalog_json"

// catalogToolMode is the only tool mode a local engine supports: ordinary
// Responses with direct tools, never the Lite code_mode_only branch that
// emits additional_tools input items.
const catalogToolMode = "direct"

// catalogMultiAgentVersion is the only multi-agent version a local engine
// supports.
const catalogMultiAgentVersion = "v1"

// codexCatalog carries the local protocol fields. Production never decodes
// catalog bytes into it with encoding/json: the single exact-key parse builds
// codexCatalogModel carriers field by field, so duplicate recognized keys
// and case aliases native Codex ignores can never reach row selection.
type codexCatalog struct {
	Models []codexCatalogModel `json:"models"`
}

// codexCatalogModel is one row of the catalog. The three booleans are
// pointers so a missing (or null) field is distinguishable from an explicit
// false: an omitted key is malformed, never silently Lite-off.
type codexCatalogModel struct {
	Slug                              string               `json:"slug"`
	UseResponsesLite                  *bool                `json:"use_responses_lite"`
	ToolMode                          string               `json:"tool_mode"`
	MultiAgentVersion                 string               `json:"multi_agent_version"`
	SupportsSearchTool                *bool                `json:"supports_search_tool"`
	SupportsReasoningSummaryParameter *bool                `json:"supports_reasoning_summary_parameter"`
	SupportedReasoningLevels          []catalogEffortLevel `json:"supported_reasoning_levels"`
}

// catalogEffortLevel is one declared effort word with its display text.
type catalogEffortLevel struct {
	Effort string `json:"effort"`
}

// resolvedCatalog is the validated outcome: the absolute catalog path, the
// raw bytes it was parsed from, and the effort vocabulary of the selected
// entry.
type resolvedCatalog struct {
	path  string
	data  []byte
	vocab []string
	slug  string
}

// resolveLocalCatalog performs the catalog hop of a local-provider launch:
// it reads the model_catalog_json path from the already-parsed private
// config, resolves it against the validated home root, reads and parses the
// catalog, selects the entry for the launch model, validates its native
// metadata, and returns the vocabulary. Every failure is a typed
// LocalProviderRefusal; absolute paths and catalog contents never enter the
// error.
func resolveLocalCatalog(config map[string]any, root, homeEnv, modelID, providerID string) (resolvedCatalog, error) {
	raw, found := config[catalogConfigKey]
	if !found {
		return resolvedCatalog{}, localProviderRefusal(agentic.LocalProviderAbsent, providerID)
	}
	pathValue, ok := raw.(string)
	if !ok || pathValue != strings.TrimSpace(pathValue) || strings.TrimSpace(pathValue) == "" {
		return resolvedCatalog{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	catalogPath, ok := resolveCatalogPath(strings.TrimSpace(pathValue), root, homeEnv)
	if !ok {
		return resolvedCatalog{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	data, err := readRegularCatalog(catalogPath)
	if err != nil {
		kind := agentic.LocalProviderReadFailed
		if errors.Is(err, os.ErrNotExist) {
			kind = agentic.LocalProviderAbsent
		}
		return resolvedCatalog{}, localCatalogRefusal(kind, catalogPath, providerID)
	}
	models, ok := exactValidatedCatalogModels(data)
	if !ok {
		return resolvedCatalog{}, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)
	}
	// The two propagation calls below use the refusal if-init shape (see
	// localProviderArgs): a typed refusal may only appear in a return
	// expression or a refusal if-init, never in a plain assignment.
	var entry codexCatalogModel
	if entryValue, err := selectCatalogEntry(models, modelID, catalogPath, providerID); err != nil {
		return resolvedCatalog{}, err
	} else {
		entry = entryValue
	}
	var vocab []string
	if vocabValue, err := validateCatalogEntry(entry, catalogPath, providerID); err != nil {
		return resolvedCatalog{}, err
	} else {
		vocab = vocabValue
	}
	return resolvedCatalog{path: catalogPath, data: data, vocab: vocab, slug: strings.TrimSpace(modelID)}, nil
}

// resolveCatalogPath resolves a model_catalog_json value: ~ expands against
// the HOME environment value, an absolute path is cleaned, and a relative
// path joins the validated CODEX_HOME root (Codex resolves relative catalog
// paths against CODEX_HOME, not the caller workdir; probed at 0.159.0).
func resolveCatalogPath(raw, root, homeEnv string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, "~\\") {
		if strings.TrimSpace(homeEnv) == "" {
			return "", false
		}
		trimmed = filepath.Join(homeEnv, strings.TrimLeft(trimmed[1:], "/\\"))
	}
	if !filepath.IsAbs(trimmed) {
		if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
			return "", false
		}
		trimmed = filepath.Join(root, trimmed)
	}
	return filepath.Clean(trimmed), true
}

// selectCatalogEntry finds the single catalog row for the launch model. Zero
// matches is absent (the operator catalog carries no metadata for this
// slug, so the launch would fall back to the hosted built-in); more than one
// is malformed (ambiguous).
func selectCatalogEntry(models []codexCatalogModel, modelID, catalogPath, providerID string) (codexCatalogModel, error) {
	want := strings.TrimSpace(modelID)
	var found *codexCatalogModel
	matches := 0
	for i := range models {
		if models[i].Slug != want {
			continue
		}
		matches++
		if found == nil {
			entry := models[i]
			found = &entry
		}
	}
	if matches == 0 {
		return codexCatalogModel{}, localCatalogRefusal(agentic.LocalProviderAbsent, catalogPath, providerID)
	}
	if matches > 1 {
		return codexCatalogModel{}, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)
	}
	return *found, nil
}

// validateCatalogEntry checks the native-metadata shape of one selected row
// and returns its effort vocabulary. Missing keys are malformed (the operator
// did not state the fact); hosted-shaped values are unsupported (the row
// states a protocol the local engine cannot run).
func validateCatalogEntry(entry codexCatalogModel, catalogPath, providerID string) ([]string, error) {
	if entry.UseResponsesLite == nil || entry.SupportsSearchTool == nil || entry.SupportsReasoningSummaryParameter == nil {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)
	}
	if strings.TrimSpace(entry.ToolMode) == "" || strings.TrimSpace(entry.MultiAgentVersion) == "" {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)
	}
	if *entry.UseResponsesLite || *entry.SupportsSearchTool || *entry.SupportsReasoningSummaryParameter {
		return nil, localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)
	}
	if entry.ToolMode != catalogToolMode || entry.MultiAgentVersion != catalogMultiAgentVersion {
		return nil, localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)
	}
	if len(entry.SupportedReasoningLevels) == 0 {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)
	}
	seen := map[string]bool{}
	vocab := make([]string, 0, len(entry.SupportedReasoningLevels))
	for _, level := range entry.SupportedReasoningLevels {
		if strings.TrimSpace(level.Effort) == "" {
			return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)
		}
		if seen[level.Effort] {
			return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)
		}
		seen[level.Effort] = true
		vocab = append(vocab, level.Effort)
	}
	return vocab, nil
}

// checkLocalEffortVocabulary checks the whole declared vendor axis, not only
// the selected effort. A row advertising a word absent from native metadata
// is incompatible even when this launch selects a supported word.
func checkLocalEffortVocabulary(declared, native []string, catalogPath, providerID string) error {
	for _, word := range declared {
		if !slices.Contains(native, word) {
			return localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)
		}
	}
	return nil
}

// checkLocalEffort refuses an out-of-vocabulary effort for a local launch.
// An empty effort refuses as well: native defaults need not belong to the
// local vocabulary, so a local launch must state its effort explicitly. Comparison is exact on
// the trimmed word, matching vendorplugin.EffortDeclaration.Accepts.
func checkLocalEffort(effort string, vocab []string, catalogPath, providerID string) error {
	trimmed := strings.TrimSpace(effort)
	if trimmed == "" {
		return localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)
	}
	for _, word := range vocab {
		if word == trimmed {
			return nil
		}
	}
	return localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)
}

// localCatalogRefusal builds a typed refusal naming the catalog file's
// basename (never its absolute path) and the selected provider id.
func localCatalogRefusal(kind agentic.LocalProviderRefusalKind, catalogPath, subject string) error {
	return &agentic.LocalProviderRefusal{Kind: kind, File: filepath.Base(catalogPath), Subject: subject}
}

const maxCatalogBytes = 4 << 20

func readRegularCatalog(path string) ([]byte, error) {
	return catalogfile.ReadRegular(path, maxCatalogBytes)
}
