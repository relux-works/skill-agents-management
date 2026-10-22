package engineobservation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

func catalogTestScope(t *testing.T) CatalogScope {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "A"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "cache"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "A", "engines.toml"), []byte("[engines.same-name]\nengine = \"mlx\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	q := testQuery()
	return CatalogScope{Engine: q.Engine, Runtime: q.Runtime, Model: q.Model, Profile: q.Profile,
		Project: dir, EnginesConfig: filepath.Join(dir, "A", "engines.toml"), CacheRoot: filepath.Join(dir, "cache")}
}

// Invalid scopes refuse at construction without executing the supplied runner;
// a valid scope constructs without executing the runner.
func TestCatalogAdapterConstructionRefusesWithoutProcess(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*CatalogScope)
		cause error
	}{
		{"missing-catalog", func(s *CatalogScope) { s.EnginesConfig = "" }, ErrCatalogRequired},
		{"relative-catalog", func(s *CatalogScope) { s.EnginesConfig = "engines.toml" }, ErrReadingsQueryInvalid},
		{"nul-catalog", func(s *CatalogScope) { s.EnginesConfig += "\x00" }, ErrReadingsQueryInvalid},
		{"missing-cache", func(s *CatalogScope) { s.CacheRoot = "" }, ErrReadingsQueryInvalid},
		{"relative-cache", func(s *CatalogScope) { s.CacheRoot = "cache" }, ErrReadingsQueryInvalid},
		{"nul-cache", func(s *CatalogScope) { s.CacheRoot += "\x00" }, ErrReadingsQueryInvalid},
		{"relative-project", func(s *CatalogScope) { s.Project = "project" }, ErrReadingsQueryInvalid},
		{"nul-project", func(s *CatalogScope) { s.Project += "\x00" }, ErrReadingsQueryInvalid},
		{"missing-runtime", func(s *CatalogScope) { s.Runtime = "" }, ErrReadingsQueryInvalid},
		{"missing-model", func(s *CatalogScope) { s.Model = "" }, ErrReadingsQueryInvalid},
		{"missing-profile", func(s *CatalogScope) { s.Profile = "" }, ErrReadingsQueryInvalid},
		{"empty-engine-id", func(s *CatalogScope) { s.Engine.ID = "" }, ErrReadingsQueryInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := catalogTestScope(t)
			tc.edit(&scope)
			calls := 0
			adapter, err := NewCatalogAdapter(inferenceengine.EngineKindNativeTransformer, scope,
				WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { calls++; return nil, nil }))
			if adapter != nil || !errors.Is(err, tc.cause) || !errors.Is(err, ErrReadingsQueryInvalid) || calls != 0 {
				t.Fatalf("construction adapter=%v err=%v calls=%d", adapter, err, calls)
			}
		})
	}
	scope := catalogTestScope(t)
	if _, err := NewCatalogAdapter(inferenceengine.EngineKindNativeTransformer, scope); err != nil {
		t.Fatal(err)
	}
}

// Exact scope reaches one bounded status read with explicit catalog/cache/cwd.
// Query drift never reaches it; later caller mutation cannot retarget the adapter.
func TestCatalogAdapterScopeAndCarriers(t *testing.T) {
	scope := catalogTestScope(t)
	want := scope
	calls := 0
	a, err := NewCatalogAdapter(inferenceengine.EngineKindNativeTransformer, scope,
		WithCommandRunner(func(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
			calls++
			wantArgs := []string{"status", "--engine", want.Profile, "--json", "--config", want.EnginesConfig, "--cache-root", want.CacheRoot}
			if name != "curator-engines" || !reflect.DeepEqual(args, wantArgs) || dir != want.Project {
				t.Fatalf("catalog invocation: %q %q cwd=%q", name, args, dir)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("status read unbounded")
			}
			return catalogStatus(t, want), nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	scope.EnginesConfig, scope.Project, scope.CacheRoot = "/other/catalog", "/other/project", "/other/cache"
	q := vendorplugin.EngineObservationQuery{Engine: want.Engine, Runtime: want.Runtime, Model: want.Model, Profile: want.Profile}
	for _, edit := range []func(*vendorplugin.EngineObservationQuery){
		func(q *vendorplugin.EngineObservationQuery) { q.Engine.ID = "llama-cpp" },
		func(q *vendorplugin.EngineObservationQuery) { q.Runtime = "other" },
		func(q *vendorplugin.EngineObservationQuery) { q.Model = "other" },
		func(q *vendorplugin.EngineObservationQuery) { q.Profile = "other" },
	} {
		bad := q
		edit(&bad)
		got, err := a.ObserveEngine(context.Background(), bad)
		if !errors.Is(err, ErrReadingsQueryInvalid) || !reflect.DeepEqual(got, vendorplugin.EngineObservation{}) || calls != 0 {
			t.Fatalf("scope drift got=%#v err=%v calls=%d", got, err, calls)
		}
	}
	got, err := a.ObserveEngine(context.Background(), q)
	if err != nil || len(got.Readings) == 0 || calls != 1 {
		t.Fatalf("exact scope got=%#v err=%v calls=%d", got, err, calls)
	}
}

// Same-name catalogs contain identical readings and engine names. Provenance
// admits selected A but refuses B, with no partial observation. Legacy callers
// still transcribe responses without requiring catalog provenance.
func TestCatalogAdapterSameNameCatalogs(t *testing.T) {
	scope := catalogTestScope(t)
	b := scope
	b.EnginesConfig = filepath.Join(scope.Project, "B", "engines.toml")
	if err := os.MkdirAll(filepath.Dir(b.EnginesConfig), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.EnginesConfig, []byte("[engines.same-name]\nengine = \"mlx\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		emitted CatalogScope
		cause   error
	}{{"A", scope, nil}, {"B", b, ErrCatalogIdentityMismatch}} {
		t.Run(tc.name, func(t *testing.T) {
			run := WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) {
				return catalogStatus(t, tc.emitted), nil
			})
			a, err := NewCatalogAdapter(inferenceengine.EngineKindNativeTransformer, scope, run)
			if err != nil {
				t.Fatal(err)
			}
			got, err := a.ObserveEngine(context.Background(), testQuery())
			if tc.cause == nil {
				if err != nil || len(got.Readings) == 0 {
					t.Fatalf("selected A refused: %#v %v", got, err)
				}
			} else if !errors.Is(err, tc.cause) || !reflect.DeepEqual(got, vendorplugin.EngineObservation{}) {
				t.Fatalf("foreign B accepted: %#v %v", got, err)
			}
		})
	}
	legacy, err := NewAdapter(scope.Engine, inferenceengine.EngineKindNativeTransformer, testResolver(scope.Project, true), WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) {
		return renderStatus(t, inferenceengine.ContractVersionV3, healthyRows()), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := legacy.ObserveEngine(context.Background(), testQuery())
	if err != nil || len(got.Readings) == 0 {
		t.Fatalf("legacy control: %#v %v", got, err)
	}
}

func catalogStatus(t *testing.T, scope CatalogScope) []byte {
	t.Helper()
	body := renderStatus(t, inferenceengine.ContractVersionV3, healthyRows())
	provenance, err := json.Marshal(map[string]string{"contract_version": "status-provenance/v1", "engines_config": scope.EnginesConfig, "engines_config_sha256": strings.Repeat("a", 64), "cache_root": scope.CacheRoot})
	if err != nil {
		t.Fatal(err)
	}
	return append(append(append([]byte(`{"provenance":`), provenance...), ','), body[1:]...)
}

// Malformed or unsupported provenance never yields readings or identity. A
// healthy response with the exact same readings provides the positive control.
func TestCatalogProvenanceRefusals(t *testing.T) {
	scope := catalogTestScope(t)
	valid := string(catalogStatus(t, scope))
	for _, tc := range []struct {
		name  string
		body  string
		cause error
	}{
		{"absent", string(renderStatus(t, inferenceengine.ContractVersionV3, healthyRows())), ErrCatalogProvenanceUnavailable},
		{"null", `{"provenance":null}`, ErrReadingsMalformed},
		{"version", strings.Replace(valid, "status-provenance/v1", "status-provenance/v2", 1), ErrReadingsMalformed},
		{"duplicate", strings.Replace(valid, `"provenance":`, `"provenance":{},"provenance":`, 1), ErrReadingsMalformed},
		{"alias", strings.Replace(valid, `"provenance":`, `"Provenance":`, 1), ErrReadingsMalformed},
		{"member-alias", strings.Replace(valid, `"cache_root":`, `"Cache_Root":`, 1), ErrReadingsMalformed},
		{"member-duplicate", strings.Replace(valid, `"cache_root":`, `"cache_root":"/bad","cache_root":`, 1), ErrReadingsMalformed},
		{"missing-member", strings.Replace(valid, `"contract_version":"status-provenance/v1",`, "", 1), ErrReadingsMalformed},
		{"null-member", strings.Replace(valid, `"status-provenance/v1"`, `null`, 1), ErrReadingsMalformed},
		{"digest-upper", strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), ErrReadingsMalformed},
		{"digest-short", strings.Replace(valid, strings.Repeat("a", 64), "abc", 1), ErrReadingsMalformed},
		{"digest-not-hex", strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("g", 64), 1), ErrReadingsMalformed},
		{"relative-catalog", strings.Replace(valid, scope.EnginesConfig, "engines.toml", 1), ErrReadingsMalformed},
		{"unclean-catalog", strings.Replace(valid, scope.EnginesConfig, scope.Project+"/A/../A/engines.toml", 1), ErrReadingsMalformed},
		{"nul-catalog", strings.Replace(valid, scope.EnginesConfig, `/bad\u0000path`, 1), ErrReadingsMalformed},
		{"relative-cache", strings.Replace(valid, scope.CacheRoot, "cache", 1), ErrReadingsMalformed},
		{"foreign-cache", strings.Replace(valid, scope.CacheRoot, scope.Project+"/other-cache", 1), ErrCatalogIdentityMismatch},
		{"foreign-catalog", strings.Replace(valid, scope.EnginesConfig, scope.Project+"/B/engines.toml", 1), ErrCatalogIdentityMismatch},
		{"trailing-json", valid + `{}`, ErrReadingsMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewCatalogAdapter(inferenceengine.EngineKindNativeTransformer, scope, WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return []byte(tc.body), nil }))
			if err != nil {
				t.Fatal(err)
			}
			got, err := a.ObserveEngine(context.Background(), testQuery())
			if !errors.Is(err, tc.cause) || !reflect.DeepEqual(got, vendorplugin.EngineObservation{}) {
				t.Fatalf("refusal got=%#v err=%v want=%v", got, err, tc.cause)
			}
		})
	}
}

// Parent aliases are canonicalized once; a symlink catalog leaf and directory
// leaf refuse without executing. The cache root is independently canonicalized.
func TestCatalogCanonicalScope(t *testing.T) {
	scope := catalogTestScope(t)
	alias := filepath.Join(scope.Project, "alias")
	if err := os.Symlink(filepath.Dir(scope.EnginesConfig), alias); err != nil {
		t.Fatal(err)
	}
	cacheAlias := filepath.Join(scope.Project, "cache-alias")
	if err := os.Symlink(scope.CacheRoot, cacheAlias); err != nil {
		t.Fatal(err)
	}
	selected := scope
	selected.EnginesConfig = filepath.Join(alias, "engines.toml")
	selected.CacheRoot = cacheAlias
	a, err := NewCatalogAdapter(inferenceengine.EngineKindNativeTransformer, selected, WithCommandRunner(func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
		if args[5] != scope.EnginesConfig || args[7] != scope.CacheRoot {
			t.Fatalf("uncanonical argv %q", args)
		}
		return catalogStatus(t, scope), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.ObserveEngine(context.Background(), testQuery()); err != nil || len(got.Readings) == 0 {
		t.Fatalf("canonical control %#v %v", got, err)
	}
	leaf := filepath.Join(scope.Project, "leaf.toml")
	if err := os.Symlink(scope.EnginesConfig, leaf); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{leaf, scope.Project, filepath.Join(scope.Project, "missing.toml")} {
		selected.EnginesConfig = invalid
		calls := 0
		got, err := NewCatalogAdapter(inferenceengine.EngineKindNativeTransformer, selected, WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { calls++; return nil, nil }))
		if got != nil || !errors.Is(err, ErrReadingsQueryInvalid) || calls != 0 {
			t.Fatalf("invalid catalog adapter=%v err=%v calls=%d", got, err, calls)
		}
	}
}
