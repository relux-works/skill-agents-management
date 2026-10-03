package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ProviderSnapshot is a validated local-provider value object: the resolved
// provider entry exactly as the plugin splices it into argv, plus the digest
// of the config.toml bytes it was parsed from, plus the validated native
// model metadata for the snapshotted slug (catalog path, catalog digest and
// effort vocabulary).
//
// The zero value is invalid. The only way to obtain a valid snapshot is
// ReadProviderSnapshot, which runs the same typed validation as the ID-only
// path. All fields are unexported, so a caller outside this package cannot
// mint a valid snapshot by literal construction: a forged or zero value is
// refused as malformed where the binding is consumed. A snapshot is immutable
// after construction; sharing one across bindings and goroutines is safe.
type ProviderSnapshot struct {
	providerID    string
	name          string
	baseURL       string
	digest        string
	modelSlug     string
	catalogPath   string
	catalogDigest string
	catalogData   string
	effortVocab   []string
	sealed        bool
}

// ProviderID returns the bound provider id, empty on the zero value.
func (s *ProviderSnapshot) ProviderID() string {
	if s == nil {
		return ""
	}
	return s.providerID
}

// Digest returns the lowercase hex SHA-256 of the config.toml bytes the
// snapshot was parsed from, empty on the zero value. A consumer attests the
// planned provider by this digest and launches with the snapshot that
// carries it, so a config.toml mutated between plan and exec cannot change
// the launch while the manifest still records the old digest.
func (s *ProviderSnapshot) Digest() string {
	if s == nil {
		return ""
	}
	return s.digest
}

// ModelSlug returns the launch-model slug the snapshot was validated for,
// empty on the zero value. A snapshot bound to one slug refuses a plan for
// another as conflicting, so a catalog entry validated for Qwen cannot be
// replayed for a different hosted slug.
func (s *ProviderSnapshot) ModelSlug() string {
	if s == nil {
		return ""
	}
	return s.modelSlug
}

// CatalogPath returns the operator catalog path as provenance. It is never
// spliced into launch argv; plans materialize the retained bytes separately.
func (s *ProviderSnapshot) CatalogPath() string {
	if s == nil {
		return ""
	}
	return s.catalogPath
}

// CatalogDigest returns the SHA-256 of the retained catalog bytes. Plans
// materialize these bytes into a private content-addressed file; the operator
// path remains provenance only and is never forwarded to the launched child.
func (s *ProviderSnapshot) CatalogDigest() string {
	if s == nil {
		return ""
	}
	return s.catalogDigest
}

// EffortVocabulary returns a copy of the effort words the snapshotted model
// row declares, nil on the zero value. The snapshot plan checks the launch
// effort against these words without reading any file.
func (s *ProviderSnapshot) EffortVocabulary() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.effortVocab...)
}

// valid reports whether the snapshot came from the constructor.
func (s *ProviderSnapshot) valid() bool {
	return s != nil && s.sealed && s.providerID != "" && s.digest != "" && s.modelSlug != "" && s.catalogPath != "" && s.catalogDigest != "" && s.catalogData != "" && len(s.effortVocab) > 0
}

// entry returns the resolved provider entry the snapshot carries.
func (s *ProviderSnapshot) entry() privateProvider {
	return privateProvider{Name: s.name, BaseURL: s.baseURL}
}

// ReadProviderSnapshot performs exactly one read+parse+resolve of the private
// config.toml selected by req, plus the catalog hop for req.Model.ID, and
// returns the validated snapshot for req.LocalProvider.ID, or the same typed
// refusals the ID-only plan path returns for Exec and DryRun modes.
//
// It validates the provider id, resolves the private configuration home
// (including the conflicting-CODEX_HOME check), reads config.toml once,
// parses it, resolves the private entry through the existing validation, and
// resolves the native model metadata for the launch model through
// resolveLocalCatalog. The mode check is deliberately absent: the snapshot is
// mode-agnostic, and Argv still refuses unsupported modes (interactive,
// managed-session) for a snapshot binding without reading config.toml. The
// launch effort is deliberately unchecked: the snapshot carries the
// vocabulary and the plan path checks the effort, so one snapshot serves
// every in-vocabulary effort. Any snapshot already carried by
// req.LocalProvider is ignored; this constructor always reads fresh bytes
// so a consumer can re-snapshot after a rotation and observe a new digest.
func ReadProviderSnapshot(req agentic.LaunchRequest) (*ProviderSnapshot, error) {
	if req.LocalProvider == nil || strings.TrimSpace(req.LocalProvider.ID) == "" {
		return nil, &agentic.LocalProviderRefusal{Kind: agentic.LocalProviderUnbound}
	}
	providerID := req.LocalProvider.ID
	if providerID != strings.TrimSpace(providerID) {
		return nil, localProviderRefusal(agentic.LocalProviderMalformed, "provider id")
	}
	if !strings.HasPrefix(providerID, "local-") {
		return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)
	}
	if !providerIDPattern.MatchString(providerID) {
		return nil, localProviderRefusal(agentic.LocalProviderMalformed, "provider id")
	}
	if strings.TrimSpace(req.Model.ID) == "" {
		return nil, localProviderRefusal(agentic.LocalProviderUnbound, providerID)
	}
	var root string
	if rootValue, err := localProviderHome(req); err != nil {
		return nil, err
	} else {
		root = rootValue
	}
	configPath := filepath.Join(root, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		kind := agentic.LocalProviderReadFailed
		if errors.Is(err, os.ErrNotExist) {
			kind = agentic.LocalProviderAbsent
		}
		return nil, localProviderRefusal(kind, providerID)
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil || config == nil {
		return nil, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	var provider privateProvider
	if providerValue, err := resolvePrivateProvider(config, providerID); err != nil {
		return nil, err
	} else {
		provider = providerValue
	}
	var catalog resolvedCatalog
	if catalogValue, err := resolveLocalCatalog(config, root, codexPolicyEnvValue(req.Env, "HOME"), req.Model.ID, providerID); err != nil {
		return nil, err
	} else {
		catalog = catalogValue
	}
	sum := sha256.Sum256(data)
	catalogSum := sha256.Sum256(catalog.data)
	return &ProviderSnapshot{
		providerID:    providerID,
		name:          provider.Name,
		baseURL:       provider.BaseURL,
		digest:        hex.EncodeToString(sum[:]),
		modelSlug:     catalog.slug,
		catalogPath:   catalog.path,
		catalogDigest: hex.EncodeToString(catalogSum[:]),
		catalogData:   string(catalog.data),
		effortVocab:   append([]string(nil), catalog.vocab...),
		sealed:        true,
	}, nil
}

// providerArgv spells the Codex-supported -c overrides for one resolved local
// provider entry. It is the single spelling shared by the ID-only path and
// the snapshot path, so the two cannot drift on the same entry.
func providerArgv(providerID string, provider privateProvider) []string {
	return []string{
		"-c", "model_provider=" + strconv.Quote(providerID),
		"-c", "model_providers." + providerID + ".name=" + strconv.Quote(provider.Name),
		"-c", "model_providers." + providerID + ".base_url=" + strconv.Quote(provider.BaseURL),
		"-c", "model_providers." + providerID + ".wire_api=\"responses\"",
		"-c", "model_providers." + providerID + ".requires_openai_auth=false",
	}
}

// catalogArgv spells the Codex-supported -c override pinning the validated
// catalog path, so Codex loads the operator metadata the plugin validated
// rather than the hosted built-in for the registered slug. It is the single
// spelling shared by the ID-only path and the snapshot path.
func catalogArgv(catalogPath string) []string {
	return []string{"-c", "model_catalog_json=" + strconv.Quote(catalogPath)}
}
