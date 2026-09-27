package runtimecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const testEngineCatalog = `
[engines.local-qwen]
engine = "mlx"
`

const testRuntimeCatalog = `
schema_version = 1

[runtimes.local-qwen]
system = "pi"
engine = { plugin = "mlx", name = "local-qwen" }
`

const testBoundModels = `
schema_version = 1

[bindings.developer]
run = "local-qwen"
profile = "developer-local"
`

func TestParseRuntimeCatalogPreservesMetadataAndEngineReference(t *testing.T) {
	body := []byte(`
schema_version = 1

[runtimes.local-qwen]
system = "pi"

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit"]
description = "Qwen local model"
context_window_tokens = 131072
cache_budget_bytes = 6442450944
effort_support = "none"
billing_class = "operator-managed"
engine = { plugin = "mlx", name = "local-qwen" }
`)

	catalog, err := ParseRuntimeCatalog(body)
	if err != nil {
		t.Fatalf("ParseRuntimeCatalog: %v", err)
	}
	runtime, ok := catalog.Runtime("LOCAL-QWEN")
	if !ok || runtime.System != "pi" {
		t.Fatalf("runtime = %+v, found %t", runtime, ok)
	}
	if len(runtime.Models) != 1 {
		t.Fatalf("model count = %d, want 1", len(runtime.Models))
	}
	model := runtime.Models[0]
	if model.ID != "qwen-3.8-27b-mlx-8bit" || model.Engine == nil || *model.Engine != (EngineReference{Plugin: "mlx", Name: "local-qwen"}) {
		t.Fatalf("model identity/engine = %+v", model)
	}
	if model.Fields["context_window_tokens"] != int64(131072) || model.Fields["cache_budget_bytes"] != int64(6442450944) || model.Fields["effort_support"] != "none" || model.Fields["billing_class"] != "operator-managed" {
		t.Fatalf("model policy metadata changed: %#v", model.Fields)
	}
}

func TestParseRuntimeCatalogRefusesMalformedConflictingAndUnsupportedInput(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "malformed_schema_version_type",
			body: "schema_version = \"1\"\n[runtimes.local-qwen]\nsystem = \"pi\"\n",
			want: ErrMalformed,
		},
		{
			name: "unsupported_schema_version",
			body: "schema_version = 2\n[runtimes.local-qwen]\nsystem = \"pi\"\n",
			want: ErrUnsupported,
		},
		{
			name: "normalized_duplicate_runtime_ids",
			body: "schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n[runtimes.LOCAL-QWEN]\nsystem = \"pi\"\n",
			want: ErrConflicting,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseRuntimeCatalog([]byte(test.body))
			requireRefusal(t, err, test.want)
		})
	}
}

func TestParseRuntimeCatalogRefusesInvalidModelWindowAndEffort(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "negative_context_window",
			body: "schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n[runtimes.local-qwen.models.m]\ncontext_window_tokens = -1\n",
		},
		{
			name: "unsupported_effort_word",
			body: "schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n[runtimes.local-qwen.models.m]\neffort_support = \"yolo\"\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseRuntimeCatalog([]byte(test.body))
			requireRefusal(t, err, ErrMalformed)
		})
	}
}

func TestParseRuntimeCatalogRefusesNonPositiveCacheBudget(t *testing.T) {
	_, err := ParseRuntimeCatalog([]byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n[runtimes.local-qwen.models.m]\ncache_budget_bytes = 0\n"))
	requireRefusal(t, err, ErrMalformed)
}

func TestParseRuntimeCatalogRefusesPaddedEngineReference(t *testing.T) {
	_, err := ParseRuntimeCatalog([]byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\nengine = { plugin = \"mlx\", name = \" local-qwen \" }\n"))
	requireRefusal(t, err, ErrMalformed)
}

func TestParseEngineCatalogRefusesMissingKind(t *testing.T) {
	_, err := ParseEngineCatalog([]byte("[engines.local-qwen]\nname = \"local-qwen\"\n"))
	requireRefusal(t, err, ErrMalformed)
}

func TestParseRuntimeCatalogRejectsCredentialFieldWithoutEchoingInput(t *testing.T) {
	for _, field := range []string{"token", "tokenValue", "secretValue", "apiKey", "accessToken", "credentialValue"} {
		t.Run(field, func(t *testing.T) {
			body := []byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n" + field + " = \"\"\n")
			_, err := ParseRuntimeCatalog(body)
			requireRefusal(t, err, ErrUnsupported)
			if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(field)) || strings.Contains(err.Error(), "system =") {
				t.Fatalf("refusal echoed TOML fields: %v", err)
			}
		})
	}
}

func TestParseRuntimeCatalogAcceptsCredentialReferenceWithoutValue(t *testing.T) {
	body := []byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\ncredential = { source = \"env-name\", name = \"MODEL_TOKEN\" }\n")
	catalog, err := ParseRuntimeCatalog(body)
	if err != nil {
		t.Fatalf("credential reference with no credential value should parse: %v", err)
	}
	runtime, ok := catalog.Runtime("local-qwen")
	credential, credentialOK := runtime.Fields["credential"].(map[string]any)
	if !ok || !credentialOK || credential["name"] != "MODEL_TOKEN" {
		t.Fatalf("runtime credential reference = %#v", runtime.Fields["credential"])
	}
}

func TestReviewAttackCredentialKeySpellings(t *testing.T) {
	fields := []string{
		"apikey",
		"APIKEY",
		`"api key"`,
		"api-key",
		`"API.KEY"`,
		"openaiapikey",
		"passwd",
		"password",
		"secretValue",
		"bearer",
		"private-key",
		"clientSecret",
		"authorization",
		"accessKey",
		"tokenValue",
		"credentialValue",
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			body := []byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n" + field + " = \"\"\n")
			_, err := ParseRuntimeCatalog(body)
			requireRefusal(t, err, ErrUnsupported)
		})
	}
}

func TestParseRuntimeCatalogRejectsRevision3CredentialKeyFamilies(t *testing.T) {
	fields := []string{
		"passphrase",
		"pwd",
		"pass",
		"auth",
		"authToken",
		"authtoken",
		"basicAuth",
		"basicauth",
		"cookie",
		"session_key",
		"session_token",
		"session_secret",
		"session_id",
		"jwt",
		"privkey",
		"signing_key",
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			body := []byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n" + field + " = \"\"\n")
			_, err := ParseRuntimeCatalog(body)
			requireRefusal(t, err, ErrUnsupported)
		})
	}
}

func TestParseRuntimeCatalogAcceptsCredentialKeyBoundaries(t *testing.T) {
	body := []byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\npassthrough = \"\"\nauthor = \"\"\nkey = \"\"\npat = \"\"\nsk = \"\"\n")
	if _, err := ParseRuntimeCatalog(body); err != nil {
		t.Fatalf("non-credential and documented bound keys should remain accepted: %v", err)
	}
}

func TestCredentialKeyStemListMatchesSchema(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "docs", "operator-file-schema-v1.md"))
	if err != nil {
		t.Fatalf("read versioned operator-file schema: %v", err)
	}
	const linePrefix = "- **Exact compacted credential-key stems:** `"
	var documented string
	count := 0
	for _, line := range strings.Split(string(schema), "\n") {
		if !strings.HasPrefix(line, linePrefix) {
			continue
		}
		count++
		documented = strings.TrimSuffix(strings.TrimPrefix(line, linePrefix), "`.")
	}
	if count != 1 {
		t.Fatalf("schema has %d exact credential-stem lists, want one", count)
	}
	if got := strings.Join(credentialKeyStems[:], ", "); documented != got {
		t.Fatalf("schema stems = %q, code stems = %q", documented, got)
	}
}

func TestReviewAttackCredentialThroughImport(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, RuntimesFile), []byte(testRuntimeCatalog), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(t.TempDir(), "legacy.toml")
	legacyBody := []byte("schema_version = 1\n[billing]\napikey = \"\"\n[bindings.developer]\nrun = \"local-qwen\"\nprofile = \"developer-local\"\n")
	if err := os.WriteFile(legacy, legacyBody, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ImportAt(directory, "", legacy)
	requireRefusal(t, err, ErrUnsupported)
	if _, statErr := os.Stat(filepath.Join(directory, ModelsFile)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("credential-shaped source wrote models.toml; stat error = %v", statErr)
	}
}

func TestImportAtRejectsRevision3CredentialKeyWithoutWritingOutputs(t *testing.T) {
	directory := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "legacy.toml")
	writeFile(t, legacy, "schema_version = 1\n[billing]\npassphrase = \"\"\n")

	_, err := ImportAt(directory, "", legacy)
	requireRefusal(t, err, ErrUnsupported)
	for _, name := range []string{RuntimesFile, ModelsFile} {
		if _, statErr := os.Lstat(filepath.Join(directory, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("rejected credential-key import wrote %s; lstat error = %v", name, statErr)
		}
	}
}

func TestParseRuntimeCatalogAcceptsTokenCounterFields(t *testing.T) {
	body := []byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n[runtimes.local-qwen.models.m]\ncontext_window_tokens = 131072\nmax_tokens = 8192\n")
	catalog, err := ParseRuntimeCatalog(body)
	if err != nil {
		t.Fatalf("token counters should remain valid model metadata: %v", err)
	}
	runtime, ok := catalog.Runtime("local-qwen")
	if !ok || len(runtime.Models) != 1 {
		t.Fatalf("runtime/model lookup = %+v, found %t", runtime, ok)
	}
	if runtime.Models[0].Fields["context_window_tokens"] != int64(131072) || runtime.Models[0].Fields["max_tokens"] != int64(8192) {
		t.Fatalf("token counters changed: %#v", runtime.Models[0].Fields)
	}
}

func TestParseRuntimeCatalogRejectsCredentialReferenceWithInvalidName(t *testing.T) {
	body := []byte("schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\ncredential = { source = \"env-name\", name = \"invalid name\" }\n")
	_, err := ParseRuntimeCatalog(body)
	requireRefusal(t, err, ErrUnsupported)
}

func TestParseBindingCatalogDistinguishesMissingAndMalformedFields(t *testing.T) {
	catalog, err := ParseBindingCatalog([]byte("schema_version = 1\n[bindings.developer]\nrun = \"local-qwen\"\n"))
	if err != nil {
		t.Fatalf("incomplete but readable binding should be discoverable: %v", err)
	}
	binding, ok := catalog.Binding("developer")
	if !ok || binding.Run != "local-qwen" || binding.Profile != "" {
		t.Fatalf("binding = %+v, found %t", binding, ok)
	}
	_, err = ParseBindingCatalog([]byte("schema_version = 1\n[bindings.developer]\nrun = 42\n"))
	requireRefusal(t, err, ErrMalformed)
	_, err = ParseBindingCatalog([]byte("schema_version = 1\n[bindings.developer]\nrun = \"bad/runtime\"\n"))
	requireRefusal(t, err, ErrMalformed)
}

func TestParseBindingCatalogPreservesExactProfileName(t *testing.T) {
	catalog, err := ParseBindingCatalog([]byte("schema_version = 1\n[bindings.developer]\nrun = \"local-qwen\"\nprofile = \" developer local \"\n"))
	if err != nil {
		t.Fatalf("ParseBindingCatalog: %v", err)
	}
	binding, ok := catalog.Binding("developer")
	if !ok || binding.Profile != " developer local " {
		t.Fatalf("profile = %q, found %t; profile names must retain their exact spelling", binding.Profile, ok)
	}
}

func TestPreflightShowsBoundAndUnboundRowsWithProvenance(t *testing.T) {
	directory := t.TempDir()
	writeOperatorFile(t, directory, RuntimesFile, testRuntimeCatalog)
	writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
	writeOperatorFile(t, directory, ModelsFile, `
schema_version = 1
[bindings.developer]
run = "local-qwen"
profile = "developer-local"
[bindings.incomplete]
run = "local-qwen"
`)

	result, err := PreflightAt(directory, []string{"reviewer", "developer", "incomplete"}, func(system agentic.SystemID) bool { return system == "pi" })
	if err != nil {
		t.Fatalf("PreflightAt: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("preflight rows = %+v, want 3", result.Rows)
	}
	rows := map[string]PreflightRow{}
	for _, row := range result.Rows {
		rows[row.Role] = row
	}
	developer := rows["developer"]
	if developer.State != RowBound || developer.Run != "local-qwen" || developer.Profile != "developer-local" {
		t.Fatalf("developer row = %+v", developer)
	}
	if developer.Provenance.BindingSource != ModelsFile || developer.Provenance.RuntimeSource != RuntimesFile || developer.Provenance.EngineSource != EnginesFile || len(developer.Provenance.EngineReferences) != 1 {
		t.Fatalf("developer provenance = %+v", developer.Provenance)
	}
	for _, role := range []string{"incomplete", "reviewer"} {
		row := rows[role]
		if row.State != RowUnbound || !errors.Is(row.Refusal, ErrUnbound) {
			t.Errorf("%s row = %+v, want visible typed unbound row", role, row)
		}
	}
}

func TestResolveAtReturnsTypedRefusalsForAbsentMalformedUnboundAndUnsupported(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, string)
		supported SystemSupport
		want      error
	}{
		{
			name: "absent_models_file",
			prepare: func(*testing.T, string) {
			},
			want: ErrAbsent,
		},
		{
			name: "malformed_models_file",
			prepare: func(t *testing.T, directory string) {
				writeOperatorFile(t, directory, RuntimesFile, testRuntimeCatalog)
				writeOperatorFile(t, directory, ModelsFile, "schema_version = [broken\n")
			},
			want: ErrMalformed,
		},
		{
			name: "incomplete_binding",
			prepare: func(t *testing.T, directory string) {
				writeOperatorFile(t, directory, RuntimesFile, testRuntimeCatalog)
				writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
				writeOperatorFile(t, directory, ModelsFile, "schema_version = 1\n[bindings.developer]\nrun = \"local-qwen\"\n")
			},
			supported: func(agentic.SystemID) bool { return true },
			want:      ErrUnbound,
		},
		{
			name: "unsupported_runtime_system",
			prepare: func(t *testing.T, directory string) {
				writeOperatorFile(t, directory, RuntimesFile, testRuntimeCatalog)
				writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
				writeOperatorFile(t, directory, ModelsFile, testBoundModels)
			},
			supported: func(agentic.SystemID) bool { return false },
			want:      ErrUnsupported,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			test.prepare(t, directory)
			_, _, err := ResolveAt(directory, "developer", test.supported)
			requireRefusal(t, err, test.want)
		})
	}
}

func TestPreflightRefusesReadFailureAndUnsupportedEngineKind(t *testing.T) {
	t.Run("read_failure", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.Mkdir(filepath.Join(directory, ModelsFile), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := PreflightAt(directory, []string{"developer"}, nil)
		requireRefusal(t, err, ErrReadFailed)
	})
	t.Run("broken_symlink", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.Symlink(filepath.Join(directory, "missing-target"), filepath.Join(directory, ModelsFile)); err != nil {
			t.Fatal(err)
		}
		_, err := PreflightAt(directory, []string{"developer"}, nil)
		requireRefusal(t, err, ErrReadFailed)
	})
	t.Run("missing_engine_entry", func(t *testing.T) {
		directory := t.TempDir()
		writeOperatorFile(t, directory, RuntimesFile, testRuntimeCatalog)
		writeOperatorFile(t, directory, ModelsFile, testBoundModels)
		_, _, err := ResolveAt(directory, "developer", func(system agentic.SystemID) bool { return system == "pi" })
		requireRefusal(t, err, ErrUnbound)
	})
	t.Run("unsupported_engine_kind", func(t *testing.T) {
		directory := t.TempDir()
		writeOperatorFile(t, directory, RuntimesFile, testRuntimeCatalog)
		writeOperatorFile(t, directory, EnginesFile, "[engines.local-qwen]\nengine = \"other\"\n")
		writeOperatorFile(t, directory, ModelsFile, testBoundModels)
		_, _, err := ResolveAt(directory, "developer", func(system agentic.SystemID) bool { return system == "pi" })
		requireRefusal(t, err, ErrUnsupported)
	})
}

func TestImportAtPreservesPoliciesAndSecondImportIsNoop(t *testing.T) {
	directory := t.TempDir()
	writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
	localModelsPath := filepath.Join(t.TempDir(), "local-models.toml")
	legacyConfigPath := filepath.Join(t.TempDir(), "legacy-config.toml")
	writeFile(t, localModelsPath, `
inference_engines = ["mlx"]
billing = { machine = "local-machine" }
[runtimes.local-qwen]
system = "pi"
engine = "mlx"
[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit"]
description = "Local Qwen"
publisher = "alibaba"
family = "qwen"
lifecycle = "current"
context_window_tokens = 131072
cache_budget_bytes = 6442450944
effort_support = "none"
engine = "mlx"
[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit".pointer]
agents_infra_project = "/legacy/agents"
agents_infra_profile = "local-qwen"
`)
	writeFile(t, legacyConfigPath, `
schema_version = 1
admission = { developer = ["qwen:high", "qwen:max"] }
windows = { developer = ["weekday:09:00-17:00"] }
effort = { developer = "high" }
billing = { developer = "team-monthly" }
[bindings.developer]
run = "local-qwen"
profile = "developer-local"
`)

	first, err := ImportAt(directory, localModelsPath, legacyConfigPath)
	if err != nil {
		t.Fatalf("first ImportAt: %v", err)
	}
	if first.Noop || !first.RuntimesChanged || !first.ModelsChanged {
		t.Fatalf("first import result = %+v, want both catalogs changed", first)
	}
	if len(first.Unbound) != 0 {
		t.Fatalf("import reported unresolved local models: %v", first.Unbound)
	}

	runtimeBytes, err := os.ReadFile(filepath.Join(directory, RuntimesFile))
	if err != nil {
		t.Fatal(err)
	}
	runtimes, err := ParseRuntimeCatalog(runtimeBytes)
	if err != nil {
		t.Fatalf("parse imported runtimes: %v", err)
	}
	var runtimeDocument map[string]any
	if err := toml.Unmarshal(runtimeBytes, &runtimeDocument); err != nil {
		t.Fatal(err)
	}
	runtime, ok := runtimes.Runtime("local-qwen")
	if !ok || len(runtime.Models) != 1 {
		t.Fatalf("imported runtime = %+v, found %t", runtime, ok)
	}
	model := runtime.Models[0]
	if model.Engine == nil || *model.Engine != (EngineReference{Plugin: "mlx", Name: "local-qwen"}) {
		t.Fatalf("imported engine reference = %+v", model.Engine)
	}
	if _, found := runtimeDocument["billing"]; found {
		t.Fatal("model billing policy was copied into runtimes.toml")
	}
	if model.Fields["context_window_tokens"] != int64(131072) || model.Fields["cache_budget_bytes"] != int64(6442450944) || model.Fields["effort_support"] != "none" {
		t.Fatalf("runtime import changed window/cache/effort: %#v", model.Fields)
	}
	if _, kept := model.Fields["pointer"]; kept {
		t.Fatal("legacy local filesystem pointer was retained in runtimes.toml")
	}

	legacyBytes, err := os.ReadFile(legacyConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var sourcePolicy, importedModels map[string]any
	if err := toml.Unmarshal(legacyBytes, &sourcePolicy); err != nil {
		t.Fatal(err)
	}
	modelsBytes, err := os.ReadFile(filepath.Join(directory, ModelsFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := toml.Unmarshal(modelsBytes, &importedModels); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"admission", "windows", "effort"} {
		if !reflect.DeepEqual(importedModels[field], sourcePolicy[field]) {
			t.Errorf("import changed %s: got %#v, want %#v", field, importedModels[field], sourcePolicy[field])
		}
	}
	billing, ok := importedModels["billing"].(map[string]any)
	if !ok || billing["developer"] != "team-monthly" || billing["machine"] != "local-machine" {
		t.Errorf("billing policies were not preserved from both inputs: %#v", importedModels["billing"])
	}
	if _, exists := importedModels["bindings"].(map[string]any)["developer"]; !exists {
		t.Fatalf("role binding was not imported: %#v", importedModels["bindings"])
	}

	beforeRuntimes := append([]byte(nil), runtimeBytes...)
	beforeModels := append([]byte(nil), modelsBytes...)
	second, err := ImportAt(directory, localModelsPath, legacyConfigPath)
	if err != nil {
		t.Fatalf("second ImportAt: %v", err)
	}
	if !second.Noop || second.RuntimesChanged || second.ModelsChanged {
		t.Fatalf("second import result = %+v, want no-op", second)
	}
	afterRuntimes, err := os.ReadFile(filepath.Join(directory, RuntimesFile))
	if err != nil {
		t.Fatal(err)
	}
	afterModels, err := os.ReadFile(filepath.Join(directory, ModelsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterRuntimes, beforeRuntimes) || !reflect.DeepEqual(afterModels, beforeModels) {
		t.Fatal("second import changed catalog bytes")
	}
}

func TestImportAtMigratesCombinedLegacyConfig(t *testing.T) {
	directory := t.TempDir()
	writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
	legacyConfigPath := filepath.Join(t.TempDir(), "legacy-config.toml")
	writeFile(t, legacyConfigPath, `
inference_engines = ["mlx"]
admission = { developer = ["qwen:high"] }
windows = { developer = ["weekday:09:00-17:00"] }
effort = { developer = "high" }
billing = { developer = "team-monthly" }
[runtimes.local-qwen]
system = "pi"
engine = "mlx"
[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit"]
description = "Local Qwen"
publisher = "alibaba"
family = "qwen"
lifecycle = "current"
context_window_tokens = 131072
cache_budget_bytes = 6442450944
effort_support = "none"
engine = "mlx"
[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit".pointer]
agents_infra_project = "/legacy/agents"
agents_infra_profile = "local-qwen"
[bindings.developer]
run = "local-qwen"
profile = "developer-local"
`)

	result, err := ImportAt(directory, "", legacyConfigPath)
	if err != nil {
		t.Fatalf("ImportAt combined legacy config: %v", err)
	}
	if result.Noop || !result.RuntimesChanged || !result.ModelsChanged || len(result.Unbound) != 0 {
		t.Fatalf("legacy import result = %+v", result)
	}
	runtimeBytes, err := os.ReadFile(filepath.Join(directory, RuntimesFile))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseRuntimeCatalog(runtimeBytes)
	if err != nil {
		t.Fatalf("ParseRuntimeCatalog imported legacy runtime: %v", err)
	}
	runtime, ok := catalog.Runtime("local-qwen")
	if !ok || len(runtime.Models) != 1 || runtime.Models[0].Engine == nil || runtime.Models[0].Engine.Name != "local-qwen" {
		t.Fatalf("legacy runtime conversion = %+v, found %t", runtime, ok)
	}
	if _, kept := runtime.Models[0].Fields["pointer"]; kept {
		t.Fatal("legacy local path pointer was retained")
	}
	modelsBytes, err := os.ReadFile(filepath.Join(directory, ModelsFile))
	if err != nil {
		t.Fatal(err)
	}
	var models map[string]any
	if err := toml.Unmarshal(modelsBytes, &models); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"admission", "windows", "effort", "billing", "bindings"} {
		if _, ok := models[key]; !ok {
			t.Errorf("legacy field %q missing from models.toml", key)
		}
	}
}

func TestImportAtRefusesMalformedLocalModelsSource(t *testing.T) {
	directory := t.TempDir()
	writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
	localModelsPath := filepath.Join(t.TempDir(), "local-models.toml")
	writeFile(t, localModelsPath, `
inference_engines = ["mlx"]
[runtimes.local-qwen]
system = "pi"
engine = "mlx"
[runtimes.local-qwen.models.m]
description = "Local Qwen"
publisher = "alibaba"
family = "qwen"
lifecycle = "current"
context_window_tokens = 131072
effort_support = "none"
engine = "mlx"
[runtimes.local-qwen.models.m.pointer]
agents_infra_profile = "local-qwen"
`)

	_, err := ImportAt(directory, localModelsPath, "")
	requireRefusal(t, err, ErrMalformed)
	if _, err := os.Stat(filepath.Join(directory, RuntimesFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed local-models source created runtimes.toml: %v", err)
	}
}

func TestPlanImportKeepsVersionedLegacyPolicyOutOfRuntimeCatalog(t *testing.T) {
	legacy := []byte(`
schema_version = 1
admission = { developer = ["qwen:high"] }
windows = { developer = ["weekday:09:00-17:00"] }
effort = { developer = "high" }
billing = { developer = "team-monthly" }
[runtimes.local-qwen]
system = "pi"
[runtimes.local-qwen.models.m]
description = "Local Qwen"
context_window_tokens = 131072
effort_support = "none"
[bindings.developer]
run = "local-qwen"
profile = "developer-local"
`)
	plan, err := PlanImport(nil, nil, nil, nil, legacy)
	if err != nil {
		t.Fatalf("PlanImport versioned legacy config: %v", err)
	}
	if _, err := ParseRuntimeCatalog(plan.Runtimes); err != nil {
		t.Fatalf("ParseRuntimeCatalog imported runtimes: %v", err)
	}
	var runtimeDocument map[string]any
	if err := toml.Unmarshal(plan.Runtimes, &runtimeDocument); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"admission", "windows", "effort", "billing"} {
		if _, exists := runtimeDocument[field]; exists {
			t.Errorf("legacy policy %q was copied into runtimes.toml", field)
		}
	}
	bindings, err := ParseBindingCatalog(plan.Models)
	if err != nil {
		t.Fatalf("ParseBindingCatalog imported models: %v", err)
	}
	if _, found := bindings.Binding("developer"); !found {
		t.Fatal("versioned legacy binding was not imported")
	}
	var models map[string]any
	if err := toml.Unmarshal(plan.Models, &models); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"admission", "windows", "effort", "billing"} {
		if _, exists := models[field]; !exists {
			t.Errorf("legacy policy %q missing from models.toml", field)
		}
	}
}

func TestPlanImportRefusesConflictingPolicyInsteadOfOverwriting(t *testing.T) {
	currentModels := []byte(`
schema_version = 1
admission = { developer = ["qwen:low"] }
`)
	legacy := []byte(`
admission = { developer = ["qwen:high"] }
`)
	_, err := PlanImport([]byte(testRuntimeCatalog), currentModels, []byte(testEngineCatalog), nil, legacy)
	requireRefusal(t, err, ErrConflicting)
}

func TestPlanImportRefusesConflictingRoleBinding(t *testing.T) {
	currentModels := []byte(`
schema_version = 1
[bindings.developer]
run = "other-runtime"
profile = "developer-local"
`)
	legacy := []byte(`
[bindings.developer]
run = "local-qwen"
profile = "developer-local"
`)
	_, err := PlanImport([]byte(testRuntimeCatalog), currentModels, []byte(testEngineCatalog), nil, legacy)
	requireRefusal(t, err, ErrConflicting)
}

func TestPlanImportRefusesDuplicateNormalizedLegacyRuntimeIDs(t *testing.T) {
	localModels := []byte(`
[runtimes.local-qwen]
system = "pi"
[runtimes.LOCAL-QWEN]
system = "pi"
`)
	_, err := PlanImport(nil, nil, nil, localModels, nil)
	requireRefusal(t, err, ErrConflicting)
}

func TestPlanImportRefusesUnsupportedCurrentModelsSchemaWithoutSources(t *testing.T) {
	_, err := PlanImport([]byte(testRuntimeCatalog), []byte("schema_version = 2\n"), nil, nil, nil)
	requireRefusal(t, err, ErrUnsupported)
}

func TestImportAtReadFailureDoesNotFallBackToLegacySource(t *testing.T) {
	directory := t.TempDir()
	writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
	legacyConfigPath := filepath.Join(t.TempDir(), "legacy-config.toml")
	writeFile(t, legacyConfigPath, "admission = { developer = [\"qwen:high\"] }\n")
	localModelsPath := filepath.Join(t.TempDir(), "local-models.toml")
	if err := os.Symlink(filepath.Join(filepath.Dir(localModelsPath), "missing-target"), localModelsPath); err != nil {
		t.Fatal(err)
	}
	_, err := ImportAt(directory, localModelsPath, legacyConfigPath)
	requireRefusal(t, err, ErrReadFailed)
	if _, statErr := os.Stat(filepath.Join(directory, ModelsFile)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("legacy source was imported after a failed local-models read; stat error = %v", statErr)
	}
}

func TestImportAtRefusesToOverwriteItsLegacySource(t *testing.T) {
	directory := t.TempDir()
	writeOperatorFile(t, directory, EnginesFile, testEngineCatalog)
	legacyConfigPath := filepath.Join(directory, ModelsFile)
	legacyBody := "admission = { developer = [\"qwen:high\"] }\n"
	writeFile(t, legacyConfigPath, legacyBody)
	_, err := ImportAt(directory, "", legacyConfigPath)
	requireRefusal(t, err, ErrConflicting)
	actual, err := os.ReadFile(legacyConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != legacyBody {
		t.Fatalf("import changed its legacy source: %q", actual)
	}
}

func requireRefusal(t *testing.T, err, want error) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %v", want)
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want errors.Is(err, %v)", err, want)
	}
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Kind == "" {
		t.Fatalf("error %T %v is not a typed Refusal", err, err)
	}
}

func writeOperatorFile(t *testing.T, directory, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(directory, name), body)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}
