package localmodels

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests for rework-01 (review rev1 changes_requested, 6 findings).
// Each test fails on the rev1 candidate and passes on the rework, driven
// through the production LoadAt/Peek entry points.

// TestReworkRefusalSubjectOmitsUnsafeEngineName is finding
// refusal-subject-value-leak: an engine.name carrying a path, a
// secret-looking string, or control characters must not appear in any error
// text; the subject is omitted instead of echoed.
func TestReworkRefusalSubjectOmitsUnsafeEngineName(t *testing.T) {
	unsafe := []struct {
		name  string
		value string
		toml  string
	}{
		{name: "path", value: "/review-synthetic-private/path", toml: `/review-synthetic-private/path`},
		{name: "secret-looking", value: "Bearer REVIEW-SYNTHETIC abc/def==", toml: `Bearer REVIEW-SYNTHETIC abc/def==`},
		{name: "control characters", value: "bad\x07name", toml: `bad\u0007name`},
	}
	for _, test := range unsafe {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			body := strings.ReplaceAll(curatorPiRuntimes(t.TempDir()),
				`name = "local-qwen"`, `name = "`+test.toml+`"`)
			writeCuratorFile(t, dir, runtimesFileName, body)
			writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX)
			_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
			refusal := requireRefusal(t, err, ErrUnbound, RefusalUnbound)
			if refusal.Subject != "" {
				t.Fatalf("subject = %q, want omitted for unsafe engine name", refusal.Subject)
			}
			requireSanitized(t, err, test.value, test.toml, dir)
			for _, fragment := range []string{"review-synthetic-private", "REVIEW-SYNTHETIC", "abc/def"} {
				if strings.Contains(err.Error(), fragment) {
					t.Fatalf("refusal %q leaks unsafe fragment %q", err.Error(), fragment)
				}
			}
		})
	}
	t.Run("identifier-shaped engine name is kept", func(t *testing.T) {
		dir := t.TempDir()
		body := strings.ReplaceAll(curatorPiRuntimes(t.TempDir()),
			`name = "local-qwen"`, `name = "missing-engine"`)
		writeCuratorFile(t, dir, runtimesFileName, body)
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX)
		_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
		refusal := requireRefusal(t, err, ErrUnbound, RefusalUnbound)
		if refusal.Subject != "missing-engine" {
			t.Fatalf("subject = %q, want the safe identifier", refusal.Subject)
		}
	})
}

// TestReworkSanitizeRefusalSubjectGrammar pins the subject grammar directly:
// identifier shapes pass, value shapes are omitted.
func TestReworkSanitizeRefusalSubjectGrammar(t *testing.T) {
	for _, subject := range []string{"local-qwen", "missing-engine", "qwen-3.8-27b-mlx-8bit", "schema_version", "engine"} {
		if got := sanitizeRefusalSubject(subject); got != subject {
			t.Fatalf("sanitizeRefusalSubject(%q) = %q, want kept", subject, got)
		}
	}
	for _, subject := range []string{
		"/review-synthetic-private/path",
		"Bearer REVIEW-SYNTHETIC abc/def==",
		"bad\x07name",
		"has space",
		`quote"`,
		"trailing\n",
		"",
		strings.Repeat("a", 129),
	} {
		if got := sanitizeRefusalSubject(subject); got != "" {
			t.Fatalf("sanitizeRefusalSubject(%q) = %q, want omitted", subject, got)
		}
	}
}

// TestReworkEngineCredentialReferenceGate is finding
// engine-credential-reference-bypass: an engines.toml entry goes through the
// same validCredentialReference gate as runtime and model paths. A credential
// string refuses unsupported; a valid reference table is admitted and
// dropped; an invalid reference refuses.
func TestReworkEngineCredentialReferenceGate(t *testing.T) {
	t.Run("credential string refuses", func(t *testing.T) {
		dir := t.TempDir()
		writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+"credential = \"SYNTHETIC-REVIEW-SECRET\"\n")
		_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
		requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
		requireSanitized(t, err, "SYNTHETIC-REVIEW-SECRET")
	})
	t.Run("invalid credential reference refuses", func(t *testing.T) {
		dir := t.TempDir()
		writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+"credential = { source = \"env-name\", name = \"invalid name\" }\n")
		_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
		requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
	})
	t.Run("valid credential reference is admitted and dropped", func(t *testing.T) {
		dir := t.TempDir()
		writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+"credential = { source = \"env-name\", name = \"MODEL_TOKEN\" }\n")
		cfg, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
		if err != nil {
			t.Fatalf("valid engine credential reference should validate: %v", err)
		}
		if len(cfg.Runtimes) != 1 {
			t.Fatalf("runtimes = %d, want 1", len(cfg.Runtimes))
		}
	})
}

// TestReworkNestedEngineCredentialWalk is finding nested-engine-credential-bypass:
// every nested table and array of engine-owned content is walked; a
// credential-stem key at any depth refuses before its value is stored.
func TestReworkNestedEngineCredentialWalk(t *testing.T) {
	bodies := []struct {
		name   string
		suffix string
	}{
		{name: "nested table api_key", suffix: "[engines.local-qwen.transport]\napi_key = \"SYNTHETIC-REVIEW-SECRET\"\n"},
		{name: "doubly nested secret", suffix: "[engines.local-qwen.outer]\n[engines.local-qwen.outer.inner]\nsecret_value = \"SYNTHETIC-REVIEW-SECRET\"\n"},
		{name: "array of tables with token", suffix: "servers = [{ endpoint = \"x\", token = \"SYNTHETIC-REVIEW-SECRET\" }]\n"},
		{name: "nested credential string", suffix: "[engines.local-qwen.transport]\ncredential = \"SYNTHETIC-REVIEW-SECRET\"\n"},
	}
	for _, test := range bodies {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
			writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+test.suffix)
			_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
			requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
			requireSanitized(t, err, "SYNTHETIC-REVIEW-SECRET", dir)
		})
	}
	t.Run("nested neutral content is tolerated", func(t *testing.T) {
		dir := t.TempDir()
		writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+"[engines.local-qwen.transport]\nendpoint = \"127.0.0.1:8080\"\nretries = 3\n")
		if _, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml")); err != nil {
			t.Fatalf("neutral nested engine content should be tolerated: %v", err)
		}
	})
}

// TestReworkCredentialCounterInspectsValueType is finding
// credential-counter-type-bypass: docs/operator-file-schema-v1.md:102-105
// exempts only integer-valued *tokens counters. A string session_tokens
// refuses unsupported; an integer counter does not.
func TestReworkCredentialCounterInspectsValueType(t *testing.T) {
	t.Run("engines string session_tokens refuses", func(t *testing.T) {
		dir := t.TempDir()
		writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+"session_tokens = \"SYNTHETIC-REVIEW-SECRET\"\n")
		_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
		requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
		requireSanitized(t, err, "SYNTHETIC-REVIEW-SECRET")
	})
	t.Run("runtimes string session_tokens refuses", func(t *testing.T) {
		dir := t.TempDir()
		body := "schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\nsession_tokens = \"SYNTHETIC-REVIEW-SECRET\"\n"
		writeCuratorFile(t, dir, runtimesFileName, body)
		_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
		requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
		requireSanitized(t, err, "SYNTHETIC-REVIEW-SECRET")
	})
	t.Run("engines integer session_tokens is tolerated", func(t *testing.T) {
		dir := t.TempDir()
		writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+"session_tokens = 42\n")
		if _, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml")); err != nil {
			t.Fatalf("integer session_tokens should be a tolerated counter: %v", err)
		}
	})
	t.Run("runtimes integer session_tokens is malformed unknown field", func(t *testing.T) {
		dir := t.TempDir()
		body := "schema_version = 1\n[runtimes.local-qwen]\nsystem = \"pi\"\nsession_tokens = 42\n"
		writeCuratorFile(t, dir, runtimesFileName, body)
		_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
		// An integer counter is not credential-shaped, so the strict
		// runtimes.toml unknown-field rule reports malformed, not unsupported.
		requireRefusal(t, err, ErrMalformed, RefusalMalformed)
	})
}

// TestReworkWeightsUnknownMemberRefusesUnsupported is finding
// engine-unknown-field-admission (orchestrator correction):
// docs/operator-file-schema-v1.md:189-192 says an unknown member of the
// weights table refuses certified execution fail-closed until readers are
// updated; that forward-compat refusal is unsupported. The engine-owned
// mode attribute stays tolerated per :153-163 (see the existing
// engine-owned-attributes subtest).
func TestReworkWeightsUnknownMemberRefusesUnsupported(t *testing.T) {
	const validID = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"
	dir := t.TempDir()
	writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
	writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+
		"\n[engines.local-qwen.weights]\nid = \""+validID+"\"\nprovenance = \"x\"\n")
	_, _, err := LoadAt(dir, filepath.Join(t.TempDir(), "missing.toml"))
	requireRefusal(t, err, ErrUnsupported, RefusalUnsupported)
}

// TestReworkPeekKeysMemoOnHomePaths is finding peek-home-cache-bypass: the
// process-wide Peek memo keys on the resolved home paths. HOME A absent then
// HOME B present in one process yields B's rows, not A's cached absence.
func TestReworkPeekKeysMemoOnHomePaths(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	t.Setenv("HOME", first)
	if got := Peek(); !got.Absent || got.Err != nil {
		t.Fatalf("first home should be absent: %+v", got)
	}
	curatorDir := filepath.Join(second, ".curator")
	if err := os.MkdirAll(curatorDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(second, ".agents", ".configs")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
	writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)
	t.Setenv("HOME", second)
	got := Peek()
	if got.Absent || got.Err != nil || len(got.Config.Runtimes) != 1 {
		t.Fatalf("Peek reused first-home result: absent=%t error=%v runtimes=%d",
			got.Absent, got.Err, len(got.Config.Runtimes))
	}
	if got.Config.Source != runtimesFileName {
		t.Fatalf("Source = %q, want the second home's Curator rows", got.Config.Source)
	}
}

// TestReworkHomeCacheKeysOnBothPaths pins that the memo key covers the legacy
// path as well as the curator directory: the same curator directory with a
// different legacy file is a different source set. It kills the narrowing
// mutant that keys on the curator directory alone.
func TestReworkHomeCacheKeysOnBothPaths(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	writeCuratorFile(t, curatorDir, runtimesFileName, curatorPiRuntimes(project))
	writeCuratorFile(t, curatorDir, enginesFileName, curatorEnginesMLX)

	absentLegacy := filepath.Join(t.TempDir(), "missing.toml")
	legacyPath := filepath.Join(t.TempDir(), legacyFileName)
	writeCuratorFile(t, filepath.Dir(legacyPath), legacyFileName,
		strings.Replace(legacyPiTOML(project), "Local Qwen3.8-27B, MLX 8-bit", "Renamed", 1))

	cache := newHomeCache()
	first := cache.get(curatorDir, absentLegacy)
	if first.Absent || first.Err != nil || first.Warning != nil {
		t.Fatalf("curator-only get = %+v, want rows without a warning", first)
	}
	second := cache.get(curatorDir, legacyPath)
	if second.Err == nil {
		t.Fatalf("same curator dir with a disagreeing legacy file should refuse conflicting, got %+v", second)
	}
	requireRefusal(t, second.Err, ErrConflicting, RefusalConflicting)
	// The first key still memoizes its own result.
	third := cache.get(curatorDir, absentLegacy)
	if third.Err != nil || third.Warning != nil {
		t.Fatalf("first key lost its memoized result: %+v", third)
	}
}

// Regression tests for rework-02 (review rev2: inherited-engine-false-conflict).
// The Curator parser materializes an inherited runtime-level engine into every
// model row, while the legacy parser preserves an empty model engine. The
// conflict comparison must use the EFFECTIVE engine (explicit model engine,
// else the runtime engine) on both sides. All three run through LoadAt.

// TestReworkInheritedEngineEqualRuntimeOnlyNoConflict is case (a): both sources
// declare the engine only at runtime level, identical, so equal effective
// content must resolve from the Curator rows with a deprecation warning, never
// a conflicting refusal. It fails on rev2 (false conflict) and passes now. It
// also kills the narrowing mutant that compares raw ModelEntry.Engine values.
func TestReworkInheritedEngineEqualRuntimeOnlyNoConflict(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	legacy := filepath.Join(t.TempDir(), legacyFileName)
	curator := strings.Replace(curatorPiRuntimes(project),
		"engine = { plugin = \"mlx\", name = \"local-qwen\" }\n\n[runtimes.local-qwen.models.\"qwen-3.8-27b-mlx-8bit\".pointer]",
		"\n[runtimes.local-qwen.models.\"qwen-3.8-27b-mlx-8bit\".pointer]", 1)
	runtimeOnlyLegacy := strings.Replace(legacyPiTOML(project), "engine                = \"mlx\"\n", "", 1)
	writeCuratorFile(t, dir, runtimesFileName, curator)
	writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX)
	writeCuratorFile(t, filepath.Dir(legacy), legacyFileName, runtimeOnlyLegacy)

	curatorOnly, _, curatorErr := LoadAt(dir, "")
	legacyOnly, _, legacyErr := LoadAt(t.TempDir(), legacy)
	if curatorErr != nil || legacyErr != nil {
		t.Fatalf("individual sources must resolve: curator=%v legacy=%v", curatorErr, legacyErr)
	}
	if curatorOnly.Runtimes[0].Engine != legacyOnly.Runtimes[0].Engine {
		t.Fatal("fixture runtime engines differ")
	}
	if curatorOnly.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"].Pointer != legacyOnly.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"].Pointer {
		t.Fatal("fixture pointers differ")
	}
	cfg, warning, err := LoadAt(dir, legacy)
	if err != nil {
		t.Fatalf("equal runtime-level-only engine declarations must resolve, got %v", err)
	}
	if cfg.Source != runtimesFileName || warning == nil {
		t.Fatalf("want Curator rows with a legacy deprecation warning: source=%q warning=%+v", cfg.Source, warning)
	}
}

// TestReworkInheritedEngineModelOverrideConflicts is case (b), the true-conflict
// control: runtime-level engines are equal but the legacy model overrides its
// engine differently, so the effective model engines differ and resolution must
// refuse typed conflicting.
func TestReworkInheritedEngineModelOverrideConflicts(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	legacy := filepath.Join(t.TempDir(), legacyFileName)
	curator := strings.Replace(curatorPiRuntimes(project),
		"engine = { plugin = \"mlx\", name = \"local-qwen\" }\n\n[runtimes.local-qwen.models.\"qwen-3.8-27b-mlx-8bit\".pointer]",
		"\n[runtimes.local-qwen.models.\"qwen-3.8-27b-mlx-8bit\".pointer]", 1)
	overridden := strings.Replace(legacyPiTOML(project), `inference_engines = ["mlx"]`, `inference_engines = ["mlx", "other"]`, 1)
	overridden = strings.Replace(overridden, "engine                = \"mlx\"", "engine                = \"other\"", 1)
	writeCuratorFile(t, dir, runtimesFileName, curator)
	writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX)
	writeCuratorFile(t, filepath.Dir(legacy), legacyFileName, overridden)

	if _, _, err := LoadAt(t.TempDir(), legacy); err != nil {
		t.Fatalf("overridden legacy source must resolve alone: %v", err)
	}
	_, warning, err := LoadAt(dir, legacy)
	refusal := requireRefusal(t, err, ErrConflicting, RefusalConflicting)
	if warning != nil {
		t.Fatalf("conflicting refusal carries a warning: %+v", warning)
	}
	if refusal.Subject != "local-qwen" {
		t.Fatalf("conflicting subject = %q, want the overlapping runtime id", refusal.Subject)
	}
	requireSanitized(t, err, dir, project)
}

// TestReworkInheritedVsExplicitIdenticalNoConflict is case (c): an inherited
// engine and an explicit identical engine are the same effective content, in
// either direction, so neither combination conflicts.
func TestReworkInheritedVsExplicitIdenticalNoConflict(t *testing.T) {
	t.Run("curator inherited vs legacy explicit", func(t *testing.T) {
		dir := t.TempDir()
		project := t.TempDir()
		legacy := filepath.Join(t.TempDir(), legacyFileName)
		curator := strings.Replace(curatorPiRuntimes(project),
			"engine = { plugin = \"mlx\", name = \"local-qwen\" }\n\n[runtimes.local-qwen.models.\"qwen-3.8-27b-mlx-8bit\".pointer]",
			"\n[runtimes.local-qwen.models.\"qwen-3.8-27b-mlx-8bit\".pointer]", 1)
		writeCuratorFile(t, dir, runtimesFileName, curator)
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX)
		writeCuratorFile(t, filepath.Dir(legacy), legacyFileName, legacyPiTOML(project))

		cfg, warning, err := LoadAt(dir, legacy)
		if err != nil {
			t.Fatalf("inherited vs explicit-identical must resolve, got %v", err)
		}
		if cfg.Source != runtimesFileName || warning == nil {
			t.Fatalf("want Curator rows with a warning: source=%q warning=%+v", cfg.Source, warning)
		}
	})
	t.Run("curator explicit vs legacy inherited", func(t *testing.T) {
		dir := t.TempDir()
		project := t.TempDir()
		legacy := filepath.Join(t.TempDir(), legacyFileName)
		runtimeOnlyLegacy := strings.Replace(legacyPiTOML(project), "engine                = \"mlx\"\n", "", 1)
		writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(project))
		writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX)
		writeCuratorFile(t, filepath.Dir(legacy), legacyFileName, runtimeOnlyLegacy)

		cfg, warning, err := LoadAt(dir, legacy)
		if err != nil {
			t.Fatalf("explicit vs inherited-identical must resolve, got %v", err)
		}
		if cfg.Source != runtimesFileName || warning == nil {
			t.Fatalf("want Curator rows with a warning: source=%q warning=%+v", cfg.Source, warning)
		}
	})
}
