package engineobservation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// ErrCatalogRequired distinguishes a missing selected catalog from other
// invalid query inputs. It never authorizes project/HOME-based fallback.
var ErrCatalogRequired = errors.New("engineobservation: selected catalog is required")

// ErrCatalogProvenanceUnavailable means status omitted catalog provenance,
// as legacy/compat reads and older curator-engines binaries do.
var ErrCatalogProvenanceUnavailable = errors.New("engineobservation: catalog provenance is unavailable")

// ErrCatalogIdentityMismatch means status attests a different selected catalog
// or cache root. No readings from that response may reach admission.
var ErrCatalogIdentityMismatch = errors.New("engineobservation: catalog identity mismatch")

// CatalogScope is the immutable selected binding copied by NewCatalogAdapter.
// EnginesConfig is Config.EnginesConfig from the caller's LoadAt result;
// CacheRoot is explicit consumer policy, never inferred from that file's path.
type CatalogScope struct {
	Engine                                     plugin.Ref
	Runtime                                    vendorplugin.RuntimeID
	Model                                      vendorplugin.ModelID
	Profile, Project, EnginesConfig, CacheRoot string
}

// NewCatalogAdapter freezes canonical catalog/cache paths without reading
// catalog contents, executing status, or changing state. The catalog leaf must
// be a regular non-symlink file; parent and cache aliases resolve once.
func NewCatalogAdapter(kind inferenceengine.EngineKind, scope CatalogScope, opts ...Option) (*Adapter, error) {
	if scope.EnginesConfig == "" {
		return nil, fmt.Errorf("%w: %w", ErrReadingsQueryInvalid, ErrCatalogRequired)
	}
	for _, path := range []string{scope.Project, scope.EnginesConfig, scope.CacheRoot} {
		if !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
			return nil, fmt.Errorf("%w: catalog, cache root and project must be absolute NUL-free paths", ErrReadingsQueryInvalid)
		}
	}
	if scope.Engine.ID == "" || strings.TrimSpace(string(scope.Runtime)) == "" || strings.TrimSpace(string(scope.Model)) == "" || strings.TrimSpace(scope.Profile) == "" || strings.ContainsAny(string(scope.Engine.ID)+string(scope.Runtime)+string(scope.Model)+scope.Profile, "\x00") {
		return nil, fmt.Errorf("%w: selected binding identity is incomplete", ErrReadingsQueryInvalid)
	}
	catalog, err := canonicalCatalog(scope.EnginesConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid selected catalog", ErrReadingsQueryInvalid)
	}
	cache, err := filepath.EvalSymlinks(scope.CacheRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cache root", ErrReadingsQueryInvalid)
	}
	info, err := os.Stat(cache)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: invalid cache root", ErrReadingsQueryInvalid)
	}
	scope.EnginesConfig, scope.CacheRoot = catalog, cache
	a, err := NewAdapter(scope.Engine, kind, func(q vendorplugin.EngineObservationQuery) (string, bool) {
		return scope.Project, q.Engine == scope.Engine && q.Runtime == scope.Runtime && q.Model == scope.Model && q.Profile == scope.Profile
	}, opts...)
	if err != nil {
		return nil, err
	}
	a.catalog = &scope
	return a, nil
}

func canonicalCatalog(path string) (string, error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	canonical := filepath.Join(parent, filepath.Base(path))
	info, err := os.Lstat(canonical)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("catalog leaf is not a regular non-symlink file")
	}
	return canonical, nil
}

// decodeCatalogProvenance validates only the producer-owned v1 identity. It
// never reopens catalog contents. The digest is shape-checked, not compared
// with LoadAt bytes: Config does not yet carry that byte-snapshot digest.
func decodeCatalogProvenance(output []byte, scope CatalogScope) error {
	malformed := func() error { return fmt.Errorf("%w: invalid status provenance", ErrReadingsMalformed) }
	outer, err := decodeStrictObject(output, nil, nil, map[string]bool{"provenance": true, "readings": true})
	if err != nil {
		return malformed()
	}
	raw, ok := outer["provenance"]
	if !ok {
		return ErrCatalogProvenanceUnavailable
	}
	fields := map[string]bool{"contract_version": true, "engines_config": true, "engines_config_sha256": true, "cache_root": true}
	members, err := decodeStrictObject(raw, fields, []string{"contract_version", "engines_config", "engines_config_sha256", "cache_root"}, fields)
	if err != nil {
		return malformed()
	}
	values := make(map[string]string, len(fields))
	for name := range fields {
		value, err := requiredStrictString(members, name)
		if err != nil {
			return malformed()
		}
		values[name] = value
	}
	if values["contract_version"] != "status-provenance/v1" {
		return malformed()
	}
	for _, key := range []string{"engines_config", "cache_root"} {
		path := values[key]
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
			return malformed()
		}
	}
	digest := values["engines_config_sha256"]
	if len(digest) != 64 {
		return malformed()
	}
	for _, ch := range digest {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return malformed()
		}
	}
	if values["engines_config"] != scope.EnginesConfig || values["cache_root"] != scope.CacheRoot {
		return ErrCatalogIdentityMismatch
	}
	return nil
}
