package localmodels

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// curatorPiRuntimes is the Curator-catalog equivalent of validTOML: the pi
// local-qwen runtime with an mlx engine reference and a full pointer.
func curatorPiRuntimes(project string) string {
	return `schema_version = 1

[runtimes.local-qwen]
system = "pi"
engine = { plugin = "mlx", name = "local-qwen" }

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit"]
description = "Local Qwen3.8-27B, MLX 8-bit"
publisher = "alibaba"
family = "qwen"
lifecycle = "current"
context_window_tokens = 131072
cache_budget_bytes = 6442450944
effort_support = "none"
engine = { plugin = "mlx", name = "local-qwen" }

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit".pointer]
curator_engines_project = ` + quoteForTOML(project) + `
curator_engines_profile = "local-qwen"
pi_model_identity = "qwen-local/qwen-local"
`
}

const curatorEnginesMLX = `[engines.local-qwen]
engine = "mlx"
`

func quoteForTOML(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func writeCuratorFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func legacyPiTOML(project string) string {
	return strings.Replace(validTOML, "/Users/op/skill-agents-management", project, 1)
}

func requireRefusal(t *testing.T, err, want error, kind RefusalKind) *Refusal {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %v", want)
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want errors.Is(err, %v)", err, want)
	}
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Kind != kind {
		t.Fatalf("error %T %v is not a typed %q refusal", err, err, kind)
	}
	return refusal
}

func requireSanitized(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	message := err.Error()
	for _, value := range forbidden {
		if value != "" && strings.Contains(message, value) {
			t.Fatalf("refusal %q embeds forbidden value %q", message, value)
		}
	}
}

// TestLoadAtCuratorOnlyResolvesPiRowsAndEnginePointer is AC1 (pi half): with
// only runtimes.toml plus engines.toml present, the pi runtime resolves its
// rows and engine pointer through the production LoadAt entry point.
func TestLoadAtCuratorOnlyResolvesPiRowsAndEnginePointer(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
	writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)

	cfg, warning, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadAt curator-only: %v", err)
	}
	if warning != nil {
		t.Fatalf("curator-only resolution carries a deprecation warning: %v", warning.Warning())
	}
	if cfg.Source != runtimesFileName {
		t.Fatalf("Source = %q, want %q", cfg.Source, runtimesFileName)
	}
	if len(cfg.InferenceEngines) != 1 || cfg.InferenceEngines[0] != "mlx" {
		t.Fatalf("InferenceEngines = %v, want [mlx] derived from the referenced plugin", cfg.InferenceEngines)
	}
	runtime, ok := runtimeEntry(cfg, "local-qwen")
	if !ok || runtime.System != "pi" {
		t.Fatalf("runtime = %+v, found %t", runtime, ok)
	}
	wantEngine := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	if runtime.Engine != wantEngine {
		t.Fatalf("runtime engine = %#v, want %#v", runtime.Engine, wantEngine)
	}
	model, ok := runtime.Models["qwen-3.8-27b-mlx-8bit"]
	if !ok {
		t.Fatal("model qwen-3.8-27b-mlx-8bit not found")
	}
	if model.Pointer.CuratorEnginesProject != project || model.Pointer.CuratorEnginesProfile != "local-qwen" || model.Pointer.PiModelIdentity != "qwen-local/qwen-local" {
		t.Fatalf("pointer = %+v, want the curator row's project/profile/pi identity", model.Pointer)
	}
	if model.Engine != wantEngine {
		t.Fatalf("model engine = %#v, want %#v", model.Engine, wantEngine)
	}
	if model.Description != "Local Qwen3.8-27B, MLX 8-bit" || model.Publisher != "alibaba" || model.Family != "qwen" {
		t.Fatalf("model provenance = %+v", model)
	}
	if model.CacheBudgetBytes == nil || *model.CacheBudgetBytes != 6_442_450_944 {
		t.Fatalf("cache budget = %v, want 6442450944", model.CacheBudgetBytes)
	}
	if model.ExpectedWeightsID != "" {
		t.Fatalf("ExpectedWeightsID = %q, want unpinned empty", model.ExpectedWeightsID)
	}
}

// TestBuildLaunchDryRunPlansPiFromCuratorCatalog is AC1 (pi launch half): the
// curator-resolved rows plan a dry-run launch through the real
// vendorplugin.BuildLaunch entry point without starting Pi or reading status.
func TestBuildLaunchDryRunPlansPiFromCuratorCatalog(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
	writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
	cfg, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadAt: %v", err)
	}

	reader := &countingStatusReader{}
	registry := isolatedLocalQwenRegistry(t, reader, cfg)
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch dry-run: %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("StatusReader calls = %d, want zero for a dry-run plan", reader.calls)
	}
	if filepath.Base(plan.Binary) != "pi" {
		t.Fatalf("plan.Binary = %q, want native pi", plan.Binary)
	}
	if got := launchArgValue(plan.Argv, "--model"); got != "qwen-local/qwen-local" {
		t.Fatalf("plan.Argv --model = %q, want the curator row's Pi identity", got)
	}
	foundEnv := false
	for _, entry := range plan.Env {
		if entry == "CURATOR_ENGINES_PROJECT_DIR="+project {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("plan.Env = %v; missing curator pointer project %q", plan.Env, project)
	}
}

// TestLoadAtLegacyOnlyFallsBackWithTypedDeprecation is AC2 (legacy half):
// with only the legacy file present, resolution still works and emits a typed
// deprecation warning naming the importer.
func TestLoadAtLegacyOnlyFallsBackWithTypedDeprecation(t *testing.T) {
	project := t.TempDir()
	legacyPath := filepath.Join(t.TempDir(), legacyFileName)
	writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, legacyPiTOML(project))

	cfg, warning, err := LoadAt(t.TempDir(), legacyPath)
	if err != nil {
		t.Fatalf("LoadAt legacy-only: %v", err)
	}
	if cfg.Source != legacyFileName {
		t.Fatalf("Source = %q, want the legacy fallback", cfg.Source)
	}
	runtime, ok := runtimeEntry(cfg, "local-qwen")
	if !ok || runtime.System != "pi" {
		t.Fatalf("legacy fallback runtime = %+v, found %t", runtime, ok)
	}
	if warning == nil {
		t.Fatal("legacy fallback carries no deprecation warning")
	}
	if warning.Importer != "runtimecatalog.ImportAt" {
		t.Fatalf("warning importer = %q, want runtimecatalog.ImportAt", warning.Importer)
	}
	if warning.Source != legacyFileName {
		t.Fatalf("warning source = %q, want %q", warning.Source, legacyFileName)
	}
	message := warning.Warning()
	if !strings.Contains(message, "runtimecatalog.ImportAt") || !strings.Contains(message, legacyFileName) {
		t.Fatalf("warning %q does not name the source and the importer", message)
	}
	if strings.Contains(message, project) {
		t.Fatalf("warning %q embeds a value", message)
	}
}

// TestLoadAtBothPresentEqualContentIsNotConflict is AC2 (equal case): both
// present and agreeing resolves from the Curator rows with a warning, never a
// conflicting refusal.
func TestLoadAtBothPresentEqualContentIsNotConflict(t *testing.T) {
	t.Run("identical rows", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
		writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
		legacyPath := filepath.Join(t.TempDir(), legacyFileName)
		writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, legacyPiTOML(project))

		cfg, warning, err := LoadAt(curatorDir, legacyPath)
		if err != nil {
			t.Fatalf("LoadAt equal content: %v, want resolution (equal is not a conflict)", err)
		}
		if cfg.Source != runtimesFileName {
			t.Fatalf("Source = %q, want the Curator rows to win", cfg.Source)
		}
		if warning == nil || warning.Importer != "runtimecatalog.ImportAt" {
			t.Fatalf("equal content carries no typed importer warning: %+v", warning)
		}
	})
	t.Run("curator pin is additive, not a conflict", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		pinned := strings.Replace(curatorPiRuntimes(project),
			`effort_support = "none"`,
			`effort_support = "none"
expected_weights_id = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"`, 1)
		writeCuratorFile(t, curatorDir, runtimesFileName, pinned)
		writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
		legacyPath := filepath.Join(t.TempDir(), legacyFileName)
		writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, legacyPiTOML(project))

		cfg, _, err := LoadAt(curatorDir, legacyPath)
		if err != nil {
			t.Fatalf("LoadAt pinned curator plus legacy: %v, want resolution (the pin is curator-only)", err)
		}
		model := cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"]
		if !strings.HasPrefix(model.ExpectedWeightsID, "gguf-sha256:") {
			t.Fatalf("pin was not preserved: %q", model.ExpectedWeightsID)
		}
	})
	t.Run("disjoint runtime sets are not a conflict", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
		writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
		legacyPath := filepath.Join(t.TempDir(), legacyFileName)
		other := strings.Replace(legacyPiTOML(project), "local-qwen", "local-other", -1)
		writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, other)

		cfg, warning, err := LoadAt(curatorDir, legacyPath)
		if err != nil {
			t.Fatalf("LoadAt disjoint runtimes: %v, want resolution", err)
		}
		if cfg.Source != runtimesFileName || warning == nil {
			t.Fatalf("Source = %q warning = %+v, want curator rows plus a warning", cfg.Source, warning)
		}
	})
}

// TestLoadAtBothPresentDisagreeingRefusesConflicting is AC2 (conflict half):
// the same runtime ID with different rows or engine pointer refuses typed
// conflicting, never last-writer-wins. Each row narrows one compared axis.
func TestLoadAtBothPresentDisagreeingRefusesConflicting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{"different description", func(body string) string {
			return strings.Replace(body, "Local Qwen3.8-27B, MLX 8-bit", "Renamed local model", 1)
		}},
		{"different engine profile pointer", func(body string) string {
			return strings.Replace(body, `curator_engines_profile  = "local-qwen"`, `curator_engines_profile  = "local-other"`, 1)
		}},
		{"different runtime engine", func(body string) string {
			second := strings.Replace(body, `inference_engines = ["mlx"]`, `inference_engines = ["mlx", "other"]`, 1)
			return strings.Replace(second, "engine = \"mlx\"", "engine = \"other\"", 1)
		}},
		{"different system", func(body string) string {
			return strings.Replace(body, `system = "pi"`, `system = "codex"`, 1)
		}},
		{"missing model row", func(body string) string {
			return strings.Replace(body, "qwen-3.8-27b-mlx-8bit", "qwen-renamed", -1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			curatorDir := t.TempDir()
			project := t.TempDir()
			legacyPath := filepath.Join(t.TempDir(), legacyFileName)
			writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
			writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
			writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, test.mutate(legacyPiTOML(project)))

			_, warning, err := LoadAt(curatorDir, legacyPath)
			refusal := requireRefusal(t, err, ErrConflicting, RefusalConflicting)
			if warning != nil {
				t.Fatalf("conflicting refusal carries a warning: %+v", warning)
			}
			if refusal.Subject != "local-qwen" {
				t.Fatalf("conflicting subject = %q, want the overlapping runtime id", refusal.Subject)
			}
			requireSanitized(t, err, curatorDir, project)
		})
	}
	t.Run("different engine project pointer", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		legacyPath := filepath.Join(t.TempDir(), legacyFileName)
		writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
		writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
		writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, legacyPiTOML(filepath.Join(project, "elsewhere")))
		_, _, err := LoadAt(curatorDir, legacyPath)
		refusal := requireRefusal(t, err, ErrConflicting, RefusalConflicting)
		if refusal.Subject != "local-qwen" {
			t.Fatalf("conflicting subject = %q, want the overlapping runtime id", refusal.Subject)
		}
		requireSanitized(t, err, curatorDir, project)
	})
}

// TestLoadAtRefusalKinds is AC3: each of the six refusal kinds is distinct and
// sanitized, driven through the production LoadAt entry point.
func TestLoadAtRefusalKinds(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, curatorDir, legacyDir, project string) (string, string)
		want    error
		kind    RefusalKind
	}{
		{
			name: "absent",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				return curatorDir, filepath.Join(legacyDir, "missing.toml")
			},
			want: ErrAbsent, kind: RefusalAbsent,
		},
		{
			name: "malformed runtimes",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				writeCuratorFile(t, curatorDir, runtimesFileName, "schema_version = [broken\n")
				return curatorDir, filepath.Join(legacyDir, "missing.toml")
			},
			want: ErrMalformed, kind: RefusalMalformed,
		},
		{
			name: "malformed unknown field",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				writeCuratorFile(t, curatorDir, runtimesFileName, "schema_version = 1\nfuture_table = {}\n[runtimes.local-qwen]\nsystem = \"pi\"\n")
				return curatorDir, filepath.Join(legacyDir, "missing.toml")
			},
			want: ErrMalformed, kind: RefusalMalformed,
		},
		{
			name: "read_failed runtimes directory",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				if err := os.Mkdir(filepath.Join(curatorDir, runtimesFileName), 0o700); err != nil {
					t.Fatal(err)
				}
				return curatorDir, filepath.Join(legacyDir, "missing.toml")
			},
			want: ErrReadFailed, kind: RefusalReadFailed,
		},
		{
			name: "conflicting",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
				writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
				legacyPath := filepath.Join(legacyDir, legacyFileName)
				writeCuratorFile(t, legacyDir, legacyFileName, strings.Replace(legacyPiTOML(project), "Local Qwen3.8-27B, MLX 8-bit", "Renamed", 1))
				return curatorDir, legacyPath
			},
			want: ErrConflicting, kind: RefusalConflicting,
		},
		{
			name: "unbound missing engine entry",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
				writeCuratorFile(t, curatorDir, enginesFileName, "[engines.other]\nengine = \"mlx\"\n")
				return curatorDir, filepath.Join(legacyDir, "missing.toml")
			},
			want: ErrUnbound, kind: RefusalUnbound,
		},
		{
			name: "unbound absent engines file",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
				return curatorDir, filepath.Join(legacyDir, "missing.toml")
			},
			want: ErrUnbound, kind: RefusalUnbound,
		},
		{
			name: "unsupported engine kind mismatch",
			prepare: func(t *testing.T, curatorDir, legacyDir, project string) (string, string) {
				writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
				writeCuratorFile(t, curatorDir, enginesFileName, "[engines.local-qwen]\nengine = \"other\"\n")
				return curatorDir, filepath.Join(legacyDir, "missing.toml")
			},
			want: ErrUnsupported, kind: RefusalUnsupported,
		},
	}
	if len(tests) != 8 {
		t.Fatalf("refusal table has %d rows, want 8 (one per kind plus malformed/read_failed/unbound variants)", len(tests))
	}
	kinds := map[RefusalKind]bool{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			curatorDir, legacyDir, project := t.TempDir(), t.TempDir(), t.TempDir()
			dir, legacy := test.prepare(t, curatorDir, legacyDir, project)
			_, _, err := LoadAt(dir, legacy)
			refusal := requireRefusal(t, err, test.want, test.kind)
			kinds[refusal.Kind] = true
			requireSanitized(t, err, curatorDir, legacyDir, project)
			if !strings.Contains(err.Error(), string(test.kind)) {
				t.Fatalf("refusal %q does not name its kind %q", err.Error(), test.kind)
			}
		})
	}
	for _, kind := range []RefusalKind{RefusalAbsent, RefusalMalformed, RefusalReadFailed, RefusalConflicting, RefusalUnbound, RefusalUnsupported} {
		if !kinds[kind] {
			t.Fatalf("refusal kind %q has no table row", kind)
		}
	}
}

// TestLoadAtReadFailureNeverFallsBackToLegacy pins the fail-closed order: a
// curator read failure refuses even when a valid legacy file is present.
func TestLoadAtReadFailureNeverFallsBackToLegacy(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(curatorDir, runtimesFileName), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(t.TempDir(), legacyFileName)
	writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, legacyPiTOML(project))

	_, _, err := LoadAt(curatorDir, legacyPath)
	requireRefusal(t, err, ErrReadFailed, RefusalReadFailed)
}

// TestLoadAtMalformedCuratorNeverFallsBackToLegacy pins that a malformed
// curator file refuses rather than silently using the legacy rows.
func TestLoadAtMalformedCuratorNeverFallsBackToLegacy(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	writeCuratorFile(t, curatorDir, runtimesFileName, "schema_version = [broken\n")
	legacyPath := filepath.Join(t.TempDir(), legacyFileName)
	writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, legacyPiTOML(project))

	_, _, err := LoadAt(curatorDir, legacyPath)
	requireRefusal(t, err, ErrMalformed, RefusalMalformed)
}

// TestLoadAtNeverWritesEnginesToml is AC4 (read-only half): resolution leaves
// the engine-owned file's bytes and mtime unchanged.
func TestLoadAtNeverWritesEnginesToml(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
	writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
	enginesPath := filepath.Join(curatorDir, enginesFileName)
	beforeBytes, err := os.ReadFile(enginesPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeStat, err := os.Stat(enginesPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadAt: %v", err)
	}
	reader := &countingStatusReader{}
	registry := isolatedLocalQwenRegistry(t, reader, cfg)
	if _, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeDryRun); err != nil {
		t.Fatalf("BuildLaunch dry-run: %v", err)
	}

	afterBytes, err := os.ReadFile(enginesPath)
	if err != nil {
		t.Fatal(err)
	}
	afterStat, err := os.Stat(enginesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBytes) != string(beforeBytes) {
		t.Fatal("resolution changed engines.toml bytes")
	}
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Fatalf("engines.toml mtime moved from %v to %v", beforeStat.ModTime(), afterStat.ModTime())
	}
}

// TestLoadAtRefusesCredentialFieldsWithoutCarryingValues is AC4 (credential
// half): credential-shaped fields refuse as unsupported without echoing the
// field or the value, while the exact reference table is validated and
// dropped rather than carried.
func TestLoadAtRefusesCredentialFieldsWithoutCarryingValues(t *testing.T) {
	for _, field := range []string{"token", "apiKey", "secretValue", "mytokenfield", "passphrase", "authorization"} {
		t.Run(field, func(t *testing.T) {
			curatorDir := t.TempDir()
			project := t.TempDir()
			body := "schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\n" + field + " = \"supersecret-value\"\n"
			writeCuratorFile(t, curatorDir, runtimesFileName, body)
			_, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
			requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
			requireSanitized(t, err, curatorDir, project, "supersecret-value", field, "SUPERSECRET-VALUE")
		})
	}
	t.Run("engines entry credential field", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
		writeCuratorFile(t, curatorDir, enginesFileName, "[engines.local-qwen]\nengine = \"mlx\"\ntoken = \"supersecret-value\"\n")
		_, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
		requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
		requireSanitized(t, err, "supersecret-value", "token")
	})
	t.Run("exact credential reference is validated and dropped", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		body := strings.Replace(curatorPiRuntimes(project), `system = "pi"`, "system = \"pi\"\ncredential = { source = \"env-name\", name = \"MODEL_TOKEN\" }", 1)
		writeCuratorFile(t, curatorDir, runtimesFileName, body)
		writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
		cfg, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
		if err != nil {
			t.Fatalf("credential reference should validate: %v", err)
		}
		if len(cfg.Runtimes) != 1 {
			t.Fatalf("runtimes = %d, want 1", len(cfg.Runtimes))
		}
	})
	t.Run("credential reference with invalid name refuses", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		body := strings.Replace(curatorPiRuntimes(project), `system = "pi"`, "system = \"pi\"\ncredential = { source = \"env-name\", name = \"invalid name\" }", 1)
		writeCuratorFile(t, curatorDir, runtimesFileName, body)
		writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
		_, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
		requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
	})
}

// TestCredentialKeyStemListMatchesSchema fails the build if the loader's stem
// list drifts from the documented contract.
func TestCredentialKeyStemListMatchesSchema(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "operator-file-schema-v1.md"))
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

// TestLoadAtValidatesWeightsIdentities covers the additive exact-weights
// fields: valid identities validate, absence stays readable and unpinned, and
// malformed versus unsupported shapes refuse distinctly.
func TestLoadAtValidatesWeightsIdentities(t *testing.T) {
	const validID = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"
	tests := []struct {
		name    string
		pin     string
		weights string
		want    error
		kind    RefusalKind
		wantPin string
	}{
		{name: "valid pin and engine declaration", pin: validID, weights: validID, wantPin: validID},
		{name: "unpinned stays readable", pin: "", weights: ""},
		{name: "engine without weights stays readable", pin: "", weights: "absent-table"},
		{name: "uppercase digest is malformed", pin: "gguf-sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", want: ErrMalformed, kind: RefusalMalformed},
		{name: "padded digest is malformed", pin: " " + validID, want: ErrMalformed, kind: RefusalMalformed},
		{name: "short digest is malformed", pin: "gguf-sha256:1111", want: ErrMalformed, kind: RefusalMalformed},
		{name: "other prefix is unsupported", pin: "mlx-sha256:1111111111111111111111111111111111111111111111111111111111111111", want: ErrUnsupported, kind: RefusalUnsupported},
		{name: "engine other prefix is unsupported", pin: "", weights: "mlx-sha256:1111111111111111111111111111111111111111111111111111111111111111", want: ErrUnsupported, kind: RefusalUnsupported},
		{name: "engine uppercase digest is malformed", pin: "", weights: "gguf-sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", want: ErrMalformed, kind: RefusalMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			curatorDir := t.TempDir()
			project := t.TempDir()
			runtimes := curatorPiRuntimes(project)
			if test.pin != "" {
				runtimes = strings.Replace(runtimes, `effort_support = "none"`, "effort_support = \"none\"\nexpected_weights_id = "+quoteForTOML(test.pin), 1)
			}
			engines := curatorEnginesMLX
			switch test.weights {
			case "":
			case "absent-table":
			default:
				engines += "\n[engines.local-qwen.weights]\nid = " + quoteForTOML(test.weights) + "\n"
			}
			writeCuratorFile(t, curatorDir, runtimesFileName, runtimes)
			writeCuratorFile(t, curatorDir, enginesFileName, engines)
			cfg, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
			if test.want != nil {
				refusal := requireRefusal(t, err, test.want, test.kind)
				_ = refusal
				requireSanitized(t, err, curatorDir, project, test.pin, test.weights)
				return
			}
			if err != nil {
				t.Fatalf("LoadAt: %v", err)
			}
			model := cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"]
			if model.ExpectedWeightsID != test.wantPin {
				t.Fatalf("ExpectedWeightsID = %q, want %q", model.ExpectedWeightsID, test.wantPin)
			}
		})
	}
	t.Run("unknown weights member refuses", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
		writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX+"\n[engines.local-qwen.weights]\nid = \""+validID+"\"\nfuture = \"x\"\n")
		_, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
		// docs/operator-file-schema-v1.md:189-192: an unknown weights member
		// refuses certified execution fail-closed until readers are updated;
		// that forward-compat refusal is unsupported, not malformed.
		requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
	})
	t.Run("engine-owned attributes are ignored", func(t *testing.T) {
		curatorDir := t.TempDir()
		project := t.TempDir()
		writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
		// docs/operator-file-schema-v1.md:153-163: engine attributes other
		// than engine and weights are engine-owned (example mode = "local")
		// and tolerated here: not interpreted, not carried.
		writeCuratorFile(t, curatorDir, enginesFileName, "[engines.local-qwen]\nengine = \"mlx\"\nmode = \"local\"\n")
		if _, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml")); err != nil {
			t.Fatalf("engine-owned mode attribute should be ignored: %v", err)
		}
	})
}

// TestResolveWithPathsMapsAbsent pins Peek's testable core: neither source
// present reports Absent rather than an error.
func TestResolveWithPathsMapsAbsent(t *testing.T) {
	result := resolveWithPaths(t.TempDir(), filepath.Join(t.TempDir(), "missing.toml"))
	if !result.Absent || result.Err != nil {
		t.Fatalf("result = %+v, want Absent without an error", result)
	}
	curatorDir := t.TempDir()
	project := t.TempDir()
	writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
	writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
	result = resolveWithPaths(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
	if result.Absent || result.Err != nil || len(result.Config.Runtimes) != 1 {
		t.Fatalf("result = %+v, want the resolved curator config", result)
	}
}

// TestLoadAtLegacyMalformedIsSanitized pins that a malformed legacy file
// refuses typed without echoing its contents.
func TestLoadAtLegacyMalformedIsSanitized(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), legacyFileName)
	writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName, "not = [valid toml")
	_, _, err := LoadAt(t.TempDir(), legacyPath)
	requireRefusal(t, err, ErrMalformed, RefusalMalformed)
	requireSanitized(t, err, "not = [valid toml")
}

// TestLoadAtEnginesAbsentWithoutRefsResolves pins that engines.toml is only
// required when a runtime references it.
func TestLoadAtEnginesAbsentWithoutRefsResolves(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	body := "schema_version = 1\n\n[runtimes.local-qwen]\nsystem = \"pi\"\n\n[runtimes.local-qwen.models.m]\ndescription = \"d\"\nlifecycle = \"current\"\neffort_support = \"none\"\n\n[runtimes.local-qwen.models.m.pointer]\ncurator_engines_project = " + quoteForTOML(project) + "\ncurator_engines_profile = \"local-qwen\"\n"
	writeCuratorFile(t, curatorDir, runtimesFileName, body)
	cfg, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadAt without engine refs: %v", err)
	}
	if len(cfg.InferenceEngines) != 0 {
		t.Fatalf("InferenceEngines = %v, want none without references", cfg.InferenceEngines)
	}
}

// TestLoadAtRefusesRetiredPointerAliases pins that the retired agents-infra
// pointer fields have no meaning in the Curator catalog.
func TestLoadAtRefusesRetiredPointerAliases(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	body := strings.Replace(curatorPiRuntimes(project), "curator_engines_project", "agents_infra_project", 1)
	body = strings.Replace(body, "curator_engines_profile", "agents_infra_profile", 1)
	writeCuratorFile(t, curatorDir, runtimesFileName, body)
	writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
	_, _, err := LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
	requireRefusal(t, err, ErrMalformed, RefusalMalformed)
}
