package runtimecatalog

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

// Both engine readers and the public preflight/import paths accept absent/1
// versions and refuse the listed invalid versions as malformed, without partial
// results. Each invalid header has positive siblings using the same valid body.
func TestEngineCatalogSchemaVersionContract(t *testing.T) {
	for _, invalid := range []struct {
		name   string
		header string
	}{
		{"zero", "schema_version = 0\n"},
		{"future", "schema_version = 2\n"},
		{"negative", "schema_version = -1\n"},
		{"float", "schema_version = 1.0\n"},
		{"string", "schema_version = \"1\"\n"},
		{"boolean", "schema_version = true\n"},
		{"array", "schema_version = [1]\n"},
		{"inline_table", "schema_version = {}\n"},
		{"table", "[schema_version]\nvalue = 1\n"},
		{"max_int64", "schema_version = 9223372036854775807\n"},
		{"overflow", "schema_version = 9223372036854775808\n"},
		{"large_overflow", "schema_version = 999999999999999999999999999999999999\n"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			for _, version := range []struct {
				name   string
				header string
				valid  bool
			}{
				{"absent", "", true},
				{"one", "schema_version = 1\n", true},
				{"invalid", invalid.header, false},
			} {
				t.Run(version.name, func(t *testing.T) {
					checkEngineSchemaReaders(t, version.header+testEngineCatalog, version.valid)
				})
			}
		})
	}
}

func checkEngineSchemaReaders(t *testing.T, engines string, valid bool) {
	t.Helper()
	const modelID = "qwen-local"
	runtimes := testRuntimeCatalog + `
[runtimes.local-qwen.models.qwen-local]
description = "Local Qwen"
publisher = "alibaba"
family = "qwen"
lifecycle = "current"
context_window_tokens = 131072
effort_support = "none"
[runtimes.local-qwen.models.qwen-local.pointer]
curator_engines_project = "/synthetic/engine-project"
curator_engines_profile = "local-qwen"
pi_model_identity = "qwen-local/qwen-local"
`
	directory := t.TempDir()
	writeOperatorFile(t, directory, RuntimesFile, runtimes)
	writeOperatorFile(t, directory, EnginesFile, engines)
	writeOperatorFile(t, directory, ModelsFile, testBoundModels)

	t.Run("parse", func(t *testing.T) {
		catalog, err := ParseEngineCatalog([]byte(engines))
		if !valid {
			requireEngineSchemaMalformed(t, err)
			if !reflect.DeepEqual(catalog, EngineCatalog{}) {
				t.Fatalf("partial engine catalog: %+v", catalog)
			}
			return
		}
		want := EngineCatalog{Entries: []EngineEntry{{Name: "local-qwen", Kind: "mlx"}}}
		if err != nil || !reflect.DeepEqual(catalog, want) {
			t.Fatalf("catalog = %+v, err = %v; want %+v", catalog, err, want)
		}
	})
	t.Run("load", func(t *testing.T) {
		config, warning, err := localmodels.LoadAt(directory, filepath.Join(directory, "missing-legacy.toml"))
		if !valid {
			var refusal *localmodels.Refusal
			if !errors.Is(err, localmodels.ErrMalformed) || !errors.As(err, &refusal) || refusal.Kind != localmodels.RefusalMalformed || refusal.File != EnginesFile {
				t.Fatalf("LoadAt error = %T %v; want typed malformed engines.toml refusal", err, err)
			}
			if !reflect.DeepEqual(config, localmodels.Config{}) || warning != nil {
				t.Fatalf("partial load: %+v, warning = %v", config, warning)
			}
			return
		}
		// A valid load exports the canonical catalog, including under an
		// aliased TMPDIR; malformed schemas above must still refuse.
		wantPath, resolveErr := filepath.EvalSymlinks(filepath.Join(directory, EnginesFile))
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if err != nil || warning != nil || len(config.Runtimes) != 1 || config.Runtimes[0].ID != "local-qwen" || len(config.Runtimes[0].Models) != 1 || config.EnginesConfig != wantPath {
			t.Fatalf("LoadAt = %+v, warning = %v, err = %v", config, warning, err)
		}
	})
	t.Run("preflight", func(t *testing.T) {
		result, err := PreflightAt(directory, []string{"developer"}, func(system agentic.SystemID) bool { return system == "pi" })
		if !valid {
			requireEngineSchemaMalformed(t, err)
			if !reflect.DeepEqual(result, PreflightResult{}) {
				t.Fatalf("partial preflight: %+v", result)
			}
			return
		}
		if err != nil || len(result.Rows) != 1 {
			t.Fatalf("PreflightAt = %+v, err = %v", result, err)
		}
		row := result.Rows[0]
		if row.State != RowBound || row.Run != "local-qwen" || row.Profile != "developer-local" || row.Refusal != nil || row.Provenance.EngineSource != EnginesFile || !reflect.DeepEqual(row.Provenance.EngineReferences, []EngineReference{{Plugin: "mlx", Name: "local-qwen"}}) {
			t.Fatalf("preflight did not bind the declared engine: %+v", row)
		}
	})
	t.Run("import", func(t *testing.T) {
		legacy := strings.ReplaceAll(runtimes, `engine = { plugin = "mlx", name = "local-qwen" }`, `engine = "mlx"`)
		legacy = strings.TrimPrefix(strings.TrimSpace(legacy), "schema_version = 1")
		legacy = "inference_engines = [\"mlx\"]\n" + legacy
		plan, err := PlanImport(nil, nil, []byte(engines), []byte(legacy), []byte(testBoundModels))
		if !valid {
			requireEngineSchemaMalformed(t, err)
			if !reflect.DeepEqual(plan, ImportPlan{}) {
				t.Fatalf("partial import: %+v", plan)
			}
			return
		}
		if err != nil || !plan.Result.RuntimesChanged || !plan.Result.ModelsChanged || plan.Result.Noop || len(plan.Result.Unbound) != 0 || !reflect.DeepEqual(plan.Result.RuntimeIDs, []string{"local-qwen"}) {
			t.Fatalf("PlanImport = %+v, err = %v", plan, err)
		}
		catalog, err := ParseRuntimeCatalog(plan.Runtimes)
		if err != nil || len(catalog.Runtimes) != 1 || len(catalog.Runtimes[0].Models) != 1 {
			t.Fatalf("imported runtimes = %+v, err = %v", catalog, err)
		}
		model := catalog.Runtimes[0].Models[0]
		if model.ID != modelID || model.Engine == nil || *model.Engine != (EngineReference{Plugin: "mlx", Name: "local-qwen"}) {
			t.Fatalf("imported model lost engine binding: %+v", model)
		}
		bindings, err := ParseBindingCatalog(plan.Models)
		if err != nil || !reflect.DeepEqual(bindings.Bindings, []Binding{{Role: "developer", Run: "local-qwen", Profile: "developer-local"}}) {
			t.Fatalf("imported bindings = %+v, err = %v", bindings, err)
		}
	})
}

func requireEngineSchemaMalformed(t *testing.T, err error) {
	t.Helper()
	requireRefusal(t, err, ErrMalformed)
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Kind != RefusalMalformed || refusal.File != EnginesFile {
		t.Fatalf("error = %T %v; want typed malformed engines.toml refusal", err, err)
	}
}
