// Curator-catalog resolution for local runtimes.
//
// The launch path reads ~/.curator/runtimes.toml plus ~/.curator/engines.toml
// as its source of local runtime/model rows and engine pointers. The legacy
// ~/.agents/.configs/local-models.toml is an explicit fallback with a typed
// deprecation warning. When both are present and disagree on an overlapping
// runtime, resolution refuses typed conflicting rather than applying
// last-writer-wins.
//
// Ownership: runtimes.toml is operator-populated under the schema owned by
// this module (docs/operator-file-schema-v1.md); engines.toml is owned by
// curator-engines and is only looked up here, never written; the legacy file
// is operator-owned and deprecated, migrated once with runtimecatalog.ImportAt.
//
// Accepted subset: only the fields the operator schema defines for the two
// Curator files, including the optional engines.<name>.weights.id and the
// runtime-model expected_weights_id pin. runtimes.toml is strict: unknown
// fields refuse as malformed (credential-shaped ones as unsupported).
// engines.toml entries read engine and weights; other engine-owned attributes
// are tolerated per docs/operator-file-schema-v1.md:153-163 (not interpreted,
// not carried) but still pass the credential walk at any depth.
// engines.<name>.weights is strict with only id: any other member refuses
// unsupported per docs/operator-file-schema-v1.md:189-192 (fail-closed
// certified-execution refusal for an extension this reader does not support).
// Credential values are never read or carried: the only admitted credential
// shape is the exact reference table, validated and dropped.
package localmodels

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/relux-works/skill-agents-management/internal/enginecatalogschema"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

const (
	runtimesFileName = "runtimes.toml"
	enginesFileName  = "engines.toml"
	legacyFileName   = "local-models.toml"

	curatorSchemaVersion = 1
)

// LoadAt resolves the local-models configuration from the Curator catalog
// directory plus the legacy file path. It is the production resolution entry
// point; Peek memoizes it over the home-derived paths.
//
//   - Both absent: typed absent refusal (Peek maps this to ConfigResult.Absent).
//   - Curator present: parsed strictly, engine references looked up in
//     engines.toml, and the result used. A present legacy file is parsed for
//     comparison: overlapping runtimes that differ refuse conflicting, while
//     equal content resolves from the Curator rows with a deprecation warning.
//   - Curator absent, legacy present: legacy fallback with a deprecation
//     warning naming the importer.
//   - Any existing-but-unreadable file refuses read_failed and never falls
//     back. Any malformed, unbound, or unsupported value refuses typed.
//
// Disagreement means one normalized runtime ID present in both sources whose
// rows differ: system, effective engine reference, or any model field
// (metadata, effort axis, engine reference, or pointer). Runtimes present in
// only one source are not a conflict; the inference-engines header and the
// Curator-only weights pin are not compared. All errors are sanitized
// *Refusal values: kind, basename, and identifier only.
func LoadAt(curatorDir, legacyPath string) (Config, *DeprecationWarning, error) {
	runtimesData, runtimesPresent, err := readOptionalFile(curatorDir, runtimesFileName)
	if err != nil {
		return Config{}, nil, err
	}
	var curatorCfg Config
	curatorValid := false
	if runtimesPresent {
		enginesData, enginesPresent, err := readOptionalFile(curatorDir, enginesFileName)
		if err != nil {
			return Config{}, nil, err
		}
		engines := map[string]curatorEngineEntry{}
		if enginesPresent {
			engines, err = parseCuratorEngines(enginesData)
			if err != nil {
				return Config{}, nil, err
			}
		}
		curatorCfg, err = parseCuratorRuntimes(runtimesData, engines, enginesPresent)
		if err != nil {
			return Config{}, nil, err
		}
		if enginesPresent {
			curatorCfg.EnginesConfig, err = filepath.Abs(filepath.Join(curatorDir, enginesFileName))
			if err != nil {
				return Config{}, nil, refuse(RefusalReadFailed, enginesFileName, "")
			}
		}
		curatorValid = true
	}

	legacyData, legacyPresent, err := readOptionalPath(legacyPath, legacyFileName)
	if err != nil {
		return Config{}, nil, err
	}
	if !curatorValid && !legacyPresent {
		return Config{}, nil, refuse(RefusalAbsent, runtimesFileName, "")
	}
	if !curatorValid {
		cfg, err := parseConfig(legacyData)
		if err != nil {
			return Config{}, nil, refuse(RefusalMalformed, legacyFileName, "")
		}
		return cfg, &DeprecationWarning{Source: legacyFileName, Importer: importerName}, nil
	}
	if !legacyPresent {
		return curatorCfg, nil, nil
	}
	legacyCfg, err := parseConfig(legacyData)
	if err != nil {
		return Config{}, nil, refuse(RefusalMalformed, legacyFileName, "")
	}
	if subject, differ := curatorLegacyDiffer(curatorCfg, legacyCfg); differ {
		return Config{}, nil, refuse(RefusalConflicting, runtimesFileName, subject)
	}
	return curatorCfg, &DeprecationWarning{Source: legacyFileName, Importer: importerName}, nil
}

// curatorEngineEntry is the projection of one engines.toml entry this package
// consumes: the declared kind plus the optional validated weights identity.
// Every other entry member stays owned by curator-engines and is ignored.
type curatorEngineEntry struct {
	Kind      string
	WeightsID string
}

// parseCuratorRuntimes strictly parses one runtimes.toml body and resolves
// its engine references against the already-parsed engines catalog.
func parseCuratorRuntimes(data []byte, engines map[string]curatorEngineEntry, enginesPresent bool) (Config, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil || document == nil {
		return Config{}, refuse(RefusalMalformed, runtimesFileName, "")
	}
	for key, value := range document {
		if key != "schema_version" && key != "runtimes" {
			return Config{}, unknownFieldRefusal(runtimesFileName, "", key, value)
		}
	}
	version, ok := curatorInteger(document["schema_version"])
	if !ok {
		return Config{}, refuse(RefusalMalformed, runtimesFileName, "schema_version")
	}
	if version > curatorSchemaVersion {
		return Config{}, refuse(RefusalUnsupported, runtimesFileName, "schema_version")
	}
	if version != curatorSchemaVersion {
		return Config{}, refuse(RefusalMalformed, runtimesFileName, "schema_version")
	}
	rawRuntimes, ok := curatorTable(document["runtimes"])
	if !ok {
		return Config{}, refuse(RefusalMalformed, runtimesFileName, "runtimes")
	}
	var cfg Config
	// seenIDs is a slice, scanned linearly: a map keyed by RuntimeID would
	// be the second binding table the single-source guard forbids.
	var seenIDs []vendorplugin.RuntimeID
	for rawID := range rawRuntimes {
		id, err := vendorplugin.NormalizeRuntimeID(rawID)
		if err != nil {
			return Config{}, refuse(RefusalMalformed, runtimesFileName, "runtime")
		}
		for _, seen := range seenIDs {
			if seen == id {
				return Config{}, refuse(RefusalConflicting, runtimesFileName, string(id))
			}
		}
		seenIDs = append(seenIDs, id)
	}
	for rawID, rawValue := range rawRuntimes {
		id, _ := vendorplugin.NormalizeRuntimeID(rawID)
		entry, ok := curatorTable(rawValue)
		if !ok {
			return Config{}, refuse(RefusalMalformed, runtimesFileName, string(id))
		}
		runtime, err := parseCuratorRuntime(string(id), entry, engines, enginesPresent)
		if err != nil {
			return Config{}, err
		}
		cfg.Runtimes = append(cfg.Runtimes, runtime)
	}
	sort.Slice(cfg.Runtimes, func(i, j int) bool { return cfg.Runtimes[i].ID < cfg.Runtimes[j].ID })
	engineIDs := map[plugin.ID]bool{}
	for _, runtime := range cfg.Runtimes {
		if runtime.Engine.ID != "" {
			engineIDs[runtime.Engine.ID] = true
		}
		for _, model := range runtime.Models {
			if model.Engine.ID != "" {
				engineIDs[model.Engine.ID] = true
			}
		}
	}
	for id := range engineIDs {
		probe := plugin.NewRegistry()
		if err := probe.Register(inferenceengine.NewConfigured(id)); err != nil {
			return Config{}, refuse(RefusalMalformed, runtimesFileName, string(id))
		}
		cfg.InferenceEngines = append(cfg.InferenceEngines, id)
	}
	sort.Slice(cfg.InferenceEngines, func(i, j int) bool { return cfg.InferenceEngines[i] < cfg.InferenceEngines[j] })
	cfg.Source = runtimesFileName
	return cfg, nil
}

func parseCuratorRuntime(id string, entry map[string]any, engines map[string]curatorEngineEntry, enginesPresent bool) (RuntimeEntry, error) {
	for key, value := range entry {
		switch key {
		case "system", "engine", "models", "credential":
		default:
			return RuntimeEntry{}, unknownFieldRefusal(runtimesFileName, id, key, value)
		}
	}
	rawSystem, ok := entry["system"].(string)
	if !ok || strings.TrimSpace(rawSystem) == "" {
		return RuntimeEntry{}, refuse(RefusalMalformed, runtimesFileName, id)
	}
	system, err := agentic.NormalizeSystemID(rawSystem)
	if err != nil {
		return RuntimeEntry{}, refuse(RefusalMalformed, runtimesFileName, id)
	}
	if rawCredential, exists := entry["credential"]; exists {
		if !validCredentialReference(rawCredential) {
			return RuntimeEntry{}, refuse(RefusalUnsupported, runtimesFileName, id)
		}
	}
	var runtimeEngine *curatorEngineRef
	if rawEngine, exists := entry["engine"]; exists {
		runtimeEngine, err = parseCuratorEngineRef(rawEngine, id)
		if err != nil {
			return RuntimeEntry{}, err
		}
	}
	runtime := RuntimeEntry{ID: vendorplugin.RuntimeID(id), System: system, Models: map[vendorplugin.ModelID]ModelEntry{}}
	if rawModels, exists := entry["models"]; exists {
		models, ok := curatorTable(rawModels)
		if !ok {
			return RuntimeEntry{}, refuse(RefusalMalformed, runtimesFileName, id)
		}
		for rawModelID, rawModel := range models {
			modelID := vendorplugin.ModelID(rawModelID)
			if err := vendorplugin.ValidateModelID(modelID); err != nil {
				return RuntimeEntry{}, refuse(RefusalMalformed, runtimesFileName, id)
			}
			modelEntry, ok := curatorTable(rawModel)
			if !ok {
				return RuntimeEntry{}, refuse(RefusalMalformed, runtimesFileName, id)
			}
			model, err := parseCuratorModel(id, string(modelID), modelEntry, runtimeEngine, engines, enginesPresent)
			if err != nil {
				return RuntimeEntry{}, err
			}
			runtime.Models[modelID] = model
		}
	}
	if runtimeEngine != nil {
		ref, err := resolveCuratorEngine(*runtimeEngine, engines, enginesPresent)
		if err != nil {
			return RuntimeEntry{}, err
		}
		runtime.Engine = ref
	}
	return runtime, nil
}

func parseCuratorModel(runtimeID, modelID string, entry map[string]any, runtimeEngine *curatorEngineRef, engines map[string]curatorEngineEntry, enginesPresent bool) (ModelEntry, error) {
	for key, value := range entry {
		switch key {
		case "description", "publisher", "family", "lifecycle", "context_window_tokens",
			"cache_budget_bytes", "effort_support", "effort_vocabulary",
			"recommended_effort", "engine", "pointer", "expected_weights_id", "credential":
		default:
			return ModelEntry{}, unknownFieldRefusal(runtimesFileName, runtimeID, key, value)
		}
	}
	description, ok := entry["description"].(string)
	if !ok || strings.TrimSpace(description) == "" {
		return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	publisher, err := optionalCuratorString(entry, "publisher", runtimeID)
	if err != nil {
		return ModelEntry{}, err
	}
	family, err := optionalCuratorString(entry, "family", runtimeID)
	if err != nil {
		return ModelEntry{}, err
	}
	rawLifecycle, ok := entry["lifecycle"].(string)
	if !ok {
		return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	lifecycle := vendorplugin.Lifecycle(rawLifecycle)
	if err := lifecycle.Validate(); err != nil {
		return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	contextWindow := 0
	if rawWindow, exists := entry["context_window_tokens"]; exists {
		window, ok := curatorInteger(rawWindow)
		if !ok || window < 0 {
			return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
		}
		contextWindow = int(window)
	}
	var cacheBudget *int64
	if rawBudget, exists := entry["cache_budget_bytes"]; exists {
		budget, ok := curatorInteger(rawBudget)
		if !ok || budget <= 0 {
			return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
		}
		value := budget
		cacheBudget = &value
	}
	rawEffort, ok := entry["effort_support"].(string)
	if !ok {
		return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	effort, err := parseEffortSupport(rawEffort)
	if err != nil {
		return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	var vocabulary []string
	if rawVocabulary, exists := entry["effort_vocabulary"]; exists {
		words, ok := rawVocabulary.([]any)
		if !ok {
			return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
		}
		for _, word := range words {
			text, ok := word.(string)
			if !ok {
				return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
			}
			vocabulary = append(vocabulary, text)
		}
	}
	recommended, err := optionalCuratorString(entry, "recommended_effort", runtimeID)
	if err != nil {
		return ModelEntry{}, err
	}
	var modelEngine *curatorEngineRef
	if rawEngine, exists := entry["engine"]; exists {
		modelEngine, err = parseCuratorEngineRef(rawEngine, runtimeID)
		if err != nil {
			return ModelEntry{}, err
		}
	}
	effective := modelEngine
	if effective == nil {
		effective = runtimeEngine
	}
	if modelEngine != nil && runtimeEngine != nil && *modelEngine != *runtimeEngine {
		return ModelEntry{}, refuse(RefusalConflicting, runtimesFileName, runtimeID)
	}
	var resolved plugin.Ref
	weightsID := ""
	if effective != nil {
		resolved, err = resolveCuratorEngine(*effective, engines, enginesPresent)
		if err != nil {
			return ModelEntry{}, err
		}
		weightsID = engines[effective.Name].WeightsID
	}
	rawPointer, exists := entry["pointer"]
	if !exists {
		return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	pointerTable, ok := curatorTable(rawPointer)
	if !ok {
		return ModelEntry{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	pointer, err := parseCuratorPointer(pointerTable, runtimeID)
	if err != nil {
		return ModelEntry{}, err
	}
	expectedWeights := ""
	if rawPin, exists := entry["expected_weights_id"]; exists {
		pin, err := parseWeightsID(rawPin, runtimesFileName, runtimeID)
		if err != nil {
			return ModelEntry{}, err
		}
		expectedWeights = pin
	}
	if rawCredential, exists := entry["credential"]; exists {
		if !validCredentialReference(rawCredential) {
			return ModelEntry{}, refuse(RefusalUnsupported, runtimesFileName, runtimeID)
		}
	}
	return ModelEntry{
		Description:         description,
		Publisher:           publisher,
		Family:              family,
		Lifecycle:           lifecycle,
		ContextWindowTokens: contextWindow,
		CacheBudgetBytes:    cacheBudget,
		EffortSupport:       effort,
		EffortVocabulary:    vocabulary,
		RecommendedEffort:   recommended,
		Pointer:             pointer,
		Engine:              resolved,
		ExpectedWeightsID:   expectedWeights,
		WeightsID:           weightsID,
	}, nil
}

func optionalCuratorString(entry map[string]any, key, runtimeID string) (string, error) {
	raw, exists := entry[key]
	if !exists {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	return value, nil
}

func parseCuratorPointer(table map[string]any, runtimeID string) (Pointer, error) {
	for key, value := range table {
		switch key {
		case "curator_engines_project", "curator_engines_profile", "pi_model_identity":
		default:
			return Pointer{}, unknownFieldRefusal(runtimesFileName, runtimeID, key, value)
		}
	}
	project, ok := table["curator_engines_project"].(string)
	if !ok || strings.TrimSpace(project) == "" {
		return Pointer{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	if !filepath.IsAbs(project) {
		return Pointer{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	profile, ok := table["curator_engines_profile"].(string)
	if !ok || strings.TrimSpace(profile) == "" {
		return Pointer{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	identity := ""
	if rawIdentity, exists := table["pi_model_identity"]; exists {
		identity, ok = rawIdentity.(string)
		if !ok {
			return Pointer{}, refuse(RefusalMalformed, runtimesFileName, runtimeID)
		}
	}
	return Pointer{CuratorEnginesProject: project, CuratorEnginesProfile: profile, PiModelIdentity: identity}, nil
}

// curatorEngineRef is one { plugin, name } reference from runtimes.toml.
type curatorEngineRef struct {
	Plugin string
	Name   string
}

func parseCuratorEngineRef(raw any, runtimeID string) (*curatorEngineRef, error) {
	entry, ok := curatorTable(raw)
	if !ok {
		return nil, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	for key, value := range entry {
		if key != "plugin" && key != "name" {
			return nil, unknownFieldRefusal(runtimesFileName, runtimeID, key, value)
		}
	}
	pluginID, pluginOK := entry["plugin"].(string)
	name, nameOK := entry["name"].(string)
	if !pluginOK || !nameOK || strings.TrimSpace(pluginID) == "" || strings.TrimSpace(name) == "" ||
		pluginID != strings.TrimSpace(pluginID) || name != strings.TrimSpace(name) {
		return nil, refuse(RefusalMalformed, runtimesFileName, runtimeID)
	}
	return &curatorEngineRef{Plugin: pluginID, Name: name}, nil
}

func resolveCuratorEngine(ref curatorEngineRef, engines map[string]curatorEngineEntry, enginesPresent bool) (plugin.Ref, error) {
	entry, found := engines[ref.Name]
	if !enginesPresent || !found {
		return plugin.Ref{}, refuse(RefusalUnbound, enginesFileName, ref.Name)
	}
	if entry.Kind == "" || entry.Kind != ref.Plugin {
		return plugin.Ref{}, refuse(RefusalUnsupported, enginesFileName, ref.Name)
	}
	return plugin.Ref{ID: plugin.ID(ref.Plugin), Kind: inferenceengine.Kind}, nil
}

// parseCuratorEngines parses one engines.toml body. Each entry must declare a
// non-empty engine kind; the optional weights table carries only id with the
// exact grammar. Remaining entry members are engine-owned: tolerated (not
// interpreted, not carried) per docs/operator-file-schema-v1.md:153-163, but
// still walked for credential-shaped keys at any depth, which refuse as
// unsupported before any value is stored.
func parseCuratorEngines(data []byte) (map[string]curatorEngineEntry, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil || document == nil {
		return nil, refuse(RefusalMalformed, enginesFileName, "")
	}
	for key, value := range document {
		switch key {
		case "schema_version":
			if !enginecatalogschema.ValidVersion(document) {
				return nil, refuse(RefusalMalformed, enginesFileName, "schema_version")
			}
		case "engines":
		default:
			return nil, unknownFieldRefusal(enginesFileName, "", key, value)
		}
	}
	rawEngines, ok := curatorTable(document["engines"])
	if !ok {
		return nil, refuse(RefusalMalformed, enginesFileName, "engines")
	}
	entries := make(map[string]curatorEngineEntry, len(rawEngines))
	for name, rawValue := range rawEngines {
		entry, ok := curatorTable(rawValue)
		if !ok || strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
			return nil, refuse(RefusalMalformed, enginesFileName, "engine")
		}
		kind, kindOK := entry["engine"].(string)
		if !kindOK || strings.TrimSpace(kind) == "" || kind != strings.TrimSpace(kind) {
			return nil, refuse(RefusalMalformed, enginesFileName, name)
		}
		weightsID := ""
		if rawWeights, exists := entry["weights"]; exists {
			weights, ok := curatorTable(rawWeights)
			if !ok {
				return nil, refuse(RefusalMalformed, enginesFileName, name)
			}
			// docs/operator-file-schema-v1.md:189-192: an unknown member of
			// the weights table refuses certified execution, fail-closed,
			// until readers are updated. That forward-compat refusal is
			// RefusalUnsupported: the file is well-formed but this consumer
			// does not support the extension; the update arrives with the
			// member in the same release.
			for key := range weights {
				if key != "id" {
					return nil, refuse(RefusalUnsupported, enginesFileName, name)
				}
			}
			rawID, exists := weights["id"]
			if !exists {
				return nil, refuse(RefusalMalformed, enginesFileName, name)
			}
			weightsID, ok = rawID.(string)
			if !ok {
				return nil, refuse(RefusalMalformed, enginesFileName, name)
			}
			id, err := parseWeightsID(weightsID, enginesFileName, name)
			if err != nil {
				return nil, err
			}
			weightsID = id
		}
		if engineEntryRefusesCredential(entry) {
			return nil, refuse(RefusalUnsupported, enginesFileName, name)
		}
		entries[name] = curatorEngineEntry{Kind: strings.TrimSpace(kind), WeightsID: weightsID}
	}
	return entries, nil
}

// parseWeightsID validates one exact-weights identity. Both the engine
// declaration and the operator pin match exactly
// ^gguf-sha256:[0-9a-f]{64}$, case-sensitive, never trimmed. A non-string,
// empty, or otherwise malformed value is malformed; another prefix is
// unsupported.
func parseWeightsID(raw any, file, subject string) (string, error) {
	value, ok := raw.(string)
	if !ok || value == "" {
		return "", refuse(RefusalMalformed, file, subject)
	}
	if strings.ContainsAny(value, " \t\n\r\v\f") {
		return "", refuse(RefusalMalformed, file, subject)
	}
	const prefix = "gguf-sha256:"
	if !strings.HasPrefix(value, prefix) {
		if isOtherWeightsPrefix(value) {
			return "", refuse(RefusalUnsupported, file, subject)
		}
		return "", refuse(RefusalMalformed, file, subject)
	}
	digest := strings.TrimPrefix(value, prefix)
	if len(digest) != 64 || !isLowerHex(digest) {
		return "", refuse(RefusalMalformed, file, subject)
	}
	return value, nil
}

func isOtherWeightsPrefix(value string) bool {
	prefix, _, found := strings.Cut(value, ":")
	if !found || prefix == "" || prefix == "gguf-sha256" {
		return false
	}
	for _, r := range prefix {
		if r > 127 || !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func isLowerHex(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

// curatorLegacyDiffer reports whether an overlapping runtime ID carries
// different rows in the two sources. It compares system, effective engine
// reference, and every model field except the Curator-only weights identity
// and operator pin; the inference-engines header is not compared. Runtimes present in only one
// source are not a conflict. Model engines compare by effective value
// (explicit model engine, else the runtime engine): the Curator parser
// materializes the inherited engine into each model row while the legacy
// parser preserves the empty declaration, so comparing raw declarations
// would refuse identical runtime-level-only content as conflicting.
func curatorLegacyDiffer(curator, legacy Config) (string, bool) {
	for _, runtime := range curator.Runtimes {
		// Linear scan through runtimeEntry: a RuntimeID-keyed map would be
		// the second binding table the single-source guard forbids.
		other, found := runtimeEntry(legacy, runtime.ID)
		if !found {
			continue
		}
		if runtime.System != other.System || runtime.Engine != other.Engine {
			return string(runtime.ID), true
		}
		if len(runtime.Models) != len(other.Models) {
			return string(runtime.ID), true
		}
		for modelID, model := range runtime.Models {
			legacyModel, found := other.Models[modelID]
			if !found || !curatorModelEqual(model, runtime.Engine, legacyModel, other.Engine) {
				return string(runtime.ID), true
			}
		}
	}
	return "", false
}

// effectiveModelEngine returns the engine one model row effectively uses: its
// explicit engine when present, otherwise its runtime's engine. Both sources
// normalize through this helper before comparison; neither parsed Config is
// mutated, so the legacy empty declaration stays preserved in the returned
// rows.
func effectiveModelEngine(model ModelEntry, runtimeEngine plugin.Ref) plugin.Ref {
	if model.Engine.ID != "" {
		return model.Engine
	}
	return runtimeEngine
}

func curatorModelEqual(a ModelEntry, aRuntime plugin.Ref, b ModelEntry, bRuntime plugin.Ref) bool {
	if a.Description != b.Description || a.Publisher != b.Publisher || a.Family != b.Family ||
		a.Lifecycle != b.Lifecycle || a.ContextWindowTokens != b.ContextWindowTokens ||
		a.EffortSupport != b.EffortSupport || a.RecommendedEffort != b.RecommendedEffort ||
		a.Pointer != b.Pointer {
		return false
	}
	if effectiveModelEngine(a, aRuntime) != effectiveModelEngine(b, bRuntime) {
		return false
	}
	if (a.CacheBudgetBytes == nil) != (b.CacheBudgetBytes == nil) {
		return false
	}
	if a.CacheBudgetBytes != nil && *a.CacheBudgetBytes != *b.CacheBudgetBytes {
		return false
	}
	if len(a.EffortVocabulary) != len(b.EffortVocabulary) {
		return false
	}
	for i := range a.EffortVocabulary {
		if a.EffortVocabulary[i] != b.EffortVocabulary[i] {
			return false
		}
	}
	return true
}

func readOptionalFile(directory, file string) ([]byte, bool, error) {
	return readOptionalPath(filepath.Join(directory, file), file)
}

func readOptionalPath(path, file string) ([]byte, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, nil
	}
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, refuse(RefusalReadFailed, file, "")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, refuse(RefusalReadFailed, file, "")
	}
	return data, true, nil
}

func unknownFieldRefusal(file, subject, field string, value any) error {
	if field == "credential" {
		// The exact credential table is the only admitted credential shape;
		// any other value under that name refuses unsupported without
		// storing it. Valid references never reach here: known "credential"
		// members are validated at their own site.
		return refuse(RefusalUnsupported, file, subject)
	}
	if isCredentialValueField(field, value) {
		return refuse(RefusalUnsupported, file, subject)
	}
	return refuse(RefusalMalformed, file, subject)
}

// engineEntryRefusesCredential walks every engine-owned member of one
// engines.toml entry (everything except engine and weights) for
// credential-shaped keys at any depth. The exact "credential" table goes
// through the same validCredentialReference gate as runtime and model
// paths; any other credential-stem key refuses before its value is stored.
// Engine-owned content is otherwise tolerated, never interpreted or
// carried.
func engineEntryRefusesCredential(entry map[string]any) bool {
	for key, value := range entry {
		if key == "engine" || key == "weights" {
			continue
		}
		if key == "credential" {
			if !validCredentialReference(value) {
				return true
			}
			continue
		}
		if isCredentialValueField(key, value) {
			return true
		}
		if engineValueRefusesCredential(value) {
			return true
		}
	}
	return false
}

// engineValueRefusesCredential recurses through nested TOML tables and
// arrays below one engine-owned value, applying the same credential gate
// at every depth.
func engineValueRefusesCredential(value any) bool {
	if table, ok := curatorTable(value); ok {
		for key, child := range table {
			if key == "credential" {
				if !validCredentialReference(child) {
					return true
				}
				continue
			}
			if isCredentialValueField(key, child) {
				return true
			}
			if engineValueRefusesCredential(child) {
				return true
			}
		}
		return false
	}
	if array, ok := value.([]any); ok {
		for _, element := range array {
			if engineValueRefusesCredential(element) {
				return true
			}
		}
	}
	return false
}

func curatorTable(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	if ok {
		return result, true
	}
	converted, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}
	result = make(map[string]any, len(converted))
	for key, child := range converted {
		result[key] = child
	}
	return result, true
}

func curatorInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	default:
		return 0, false
	}
}
