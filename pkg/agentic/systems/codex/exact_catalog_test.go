package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// rewriteWrittenCatalogOnce applies byte surgery to the compact catalog the
// provider helper wrote, replacing exactly one occurrence. Byte surgery
// preserves duplicate keys and case variants that a map round-trip would
// erase, which is the whole point of these attacks.
func rewriteWrittenCatalogOnce(t *testing.T, home, old, new string) {
	t.Helper()
	path := filepath.Join(home, "catalog.local.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := bytes.Count(raw, []byte(old)); count != 1 {
		t.Fatalf("pattern %q occurs %d times, want exactly 1", old, count)
	}
	raw = bytes.Replace(raw, []byte(old), []byte(new), 1)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// rewriteWrittenCatalogLast replaces the last of exactly two occurrences of
// a pattern, addressing the unselected row of a two-row catalog.
func rewriteWrittenCatalogLast(t *testing.T, home, old, new string) {
	t.Helper()
	path := filepath.Join(home, "catalog.local.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := bytes.Count(raw, []byte(old)); count != 2 {
		t.Fatalf("pattern %q occurs %d times, want exactly 2", old, count)
	}
	at := bytes.LastIndex(raw, []byte(old))
	raw = append(append(append([]byte(nil), raw[:at]...), []byte(new)...), raw[at+len(old):]...)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// appendUnselectedRow adds a second native-valid row for slug to the written
// catalog. The map round-trip runs before any duplicate or alias surgery, so
// it erases no attack.
func appendUnselectedRow(t *testing.T, home, slug string) {
	t.Helper()
	path := filepath.Join(home, "catalog.local.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	rows, ok := catalog["models"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("written catalog has %v rows, want 1", len(rows))
	}
	cloned, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(cloned, &row); err != nil {
		t.Fatal(err)
	}
	row["slug"] = slug
	catalog["models"] = append(rows, row)
	raw, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// TestLocalCatalogDuplicateRecognizedFieldRefuses is the Q1 class: a repeated
// recognized key is native-invalid (Codex 0.159 reports "duplicate field"),
// so every production entry refuses it typed malformed before any child
// starts — in the selected row, in an unselected row, at the catalog root,
// and in nested messages. Identical valid values still refuse: the gate is
// occurrence-based, not value-based.
func TestLocalCatalogDuplicateRecognizedFieldRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rewrite func(t *testing.T, home string)
	}{
		{
			name: "selected_row_shell_type",
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"shell_type":`, `"shell_type":"unified_exec","shell_type":`)
			},
		},
		{
			name: "selected_row_display_name",
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"display_name":`, `"display_name":"duplicate-first","display_name":`)
			},
		},
		{
			name: "top_level_models",
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `{"models":[`, `{"models":[],"models":[`)
			},
		},
		{
			name: "unselected_row_shell_type",
			rewrite: func(t *testing.T, home string) {
				appendUnselectedRow(t, home, "other-row")
				rewriteWrittenCatalogLast(t, home, `"shell_type":`, `"shell_type":"unified_exec","shell_type":`)
			},
		},
		{
			name: "nested_token_budget_threshold",
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"reminder_threshold_tokens":6144`, `"reminder_threshold_tokens":6144,"reminder_threshold_tokens":6144`)
			},
		},
		{
			name: "nested_approval_message",
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"on_request":null`, `"on_request":null,"on_request":null`)
			},
		},
		{
			name: "identical_values_still_refuse",
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"priority":3`, `"priority":3,"priority":3`)
			},
		},
		{
			name: "duplicate_effort_key",
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"effort":"low"`, `"effort":"low","effort":"low"`)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			test.rewrite(t, home)
			req := catalogRequest(t, home, workDir, "low")
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
				_, err := buildCodexPlanForMode(t, req, mode)
				if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
					t.Fatalf("BuildPlan(%s) error = %v, want %v", mode, err, agentic.ErrLocalProviderMalformed)
				}
				var refusal *agentic.LocalProviderRefusal
				if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderMalformed {
					t.Fatalf("BuildPlan(%s) error = %v, want typed malformed", mode, err)
				}
			}
			if _, err := ReadProviderSnapshot(req); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
				t.Fatalf("ReadProviderSnapshot error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
			}
		})
	}
}

// TestLocalCatalogCaseAliasKeysCannotOverride is the Q2 class: native serde
// matches field names exactly and ignores case variants, while Go struct
// decoding folds them. Every production entry reads exact-case keys only, so
// an alias can neither override a canonical value nor stand in for a missing
// one — across protocol, identity and effort fields.
func TestLocalCatalogCaseAliasKeysCannotOverride(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kind    agentic.LocalProviderRefusalKind
		want    error
		// snapshotWant is the constructor outcome: nil when the constructor
		// must succeed (it never checks effort) with snapshotVocab carried.
		snapshotWant  error
		snapshotVocab []string
		rewrite       func(t *testing.T, home string)
	}{
		{
			name:         "lite_alias_hides_true",
			kind:         agentic.LocalProviderUnsupported,
			want:         agentic.ErrLocalProviderUnsupported,
			snapshotWant: agentic.ErrLocalProviderUnsupported,
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"use_responses_lite":false`, `"use_responses_lite":true,"USE_RESPONSES_LITE":false`)
			},
		},
		{
			name:         "lite_alias_only",
			kind:         agentic.LocalProviderMalformed,
			want:         agentic.ErrLocalProviderMalformed,
			snapshotWant: agentic.ErrLocalProviderMalformed,
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"use_responses_lite":false`, `"USE_RESPONSES_LITE":false`)
			},
		},
		{
			name:         "slug_alias_only",
			kind:         agentic.LocalProviderMalformed,
			want:         agentic.ErrLocalProviderMalformed,
			snapshotWant: agentic.ErrLocalProviderMalformed,
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"slug":"Qwen3.8-27B-Q4_K_M"`, `"SLUG":"Qwen3.8-27B-Q4_K_M"`)
			},
		},
		{
			name:         "slug_alias_decoy",
			kind:         agentic.LocalProviderAbsent,
			want:         agentic.ErrLocalProviderAbsent,
			snapshotWant: agentic.ErrLocalProviderAbsent,
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"slug":"Qwen3.8-27B-Q4_K_M"`, `"slug":"other-row","SLUG":"Qwen3.8-27B-Q4_K_M"`)
			},
		},
		{
			name:         "tool_mode_alias_hides_code_mode_only",
			kind:         agentic.LocalProviderUnsupported,
			want:         agentic.ErrLocalProviderUnsupported,
			snapshotWant: agentic.ErrLocalProviderUnsupported,
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"tool_mode":"direct"`, `"tool_mode":"code_mode_only","TOOL_MODE":"direct"`)
			},
		},
		{
			name:         "multi_agent_alias_hides_v2",
			kind:         agentic.LocalProviderUnsupported,
			want:         agentic.ErrLocalProviderUnsupported,
			snapshotWant: agentic.ErrLocalProviderUnsupported,
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"multi_agent_version":"v1"`, `"multi_agent_version":"v2","MULTI_AGENT_VERSION":"v1"`)
			},
		},
		{
			name:          "effort_alias_hides_max",
			kind:          agentic.LocalProviderUnsupported,
			want:          agentic.ErrLocalProviderUnsupported,
			snapshotWant:  nil,
			snapshotVocab: []string{"max"},
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"effort":"low"`, `"effort":"max","EFFORT":"low"`)
			},
		},
		{
			name:         "effort_alias_only",
			kind:         agentic.LocalProviderMalformed,
			want:         agentic.ErrLocalProviderMalformed,
			snapshotWant: agentic.ErrLocalProviderMalformed,
			rewrite: func(t *testing.T, home string) {
				rewriteWrittenCatalogOnce(t, home, `"effort":"low"`, `"EFFORT":"low"`)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			test.rewrite(t, home)
			req := catalogRequest(t, home, workDir, "low")
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
				_, err := buildCodexPlanForMode(t, req, mode)
				if !errors.Is(err, test.want) {
					t.Fatalf("BuildPlan(%s) error = %v, want %v", mode, err, test.want)
				}
				var refusal *agentic.LocalProviderRefusal
				if !errors.As(err, &refusal) || refusal.Kind != test.kind {
					t.Fatalf("BuildPlan(%s) error = %v, want typed kind %q", mode, err, test.kind)
				}
			}
			snapshot, err := ReadProviderSnapshot(req)
			if test.snapshotWant == nil {
				if err != nil {
					t.Fatalf("ReadProviderSnapshot error = %v, want success carrying %q", err, test.snapshotVocab)
				}
				if vocab := snapshot.EffortVocabulary(); !reflect.DeepEqual(vocab, test.snapshotVocab) {
					t.Fatalf("snapshot vocabulary = %q, want %q (the alias must not leak into it)", vocab, test.snapshotVocab)
				}
				for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
					if _, err := buildCodexPlanForMode(t, withSnapshot(req, snapshot), mode); !errors.Is(err, test.want) {
						t.Fatalf("BuildPlan(snapshot %s) error = %v, want %v", mode, err, test.want)
					}
				}
				return
			}
			if snapshot != nil {
				t.Fatalf("ReadProviderSnapshot returned a snapshot on a refused catalog")
			}
			if !errors.Is(err, test.snapshotWant) {
				t.Fatalf("ReadProviderSnapshot error = %v, want %v", err, test.snapshotWant)
			}
		})
	}
}

// TestLocalCatalogUnknownKeysBehaveAsNative locks the other half of the
// exact-key contract: unknown keys — including repeated unknown keys — are
// ignored like native serde ignores them, and a catalog carrying them still
// launches with its validated bytes sealed.
func TestLocalCatalogUnknownKeysBehaveAsNative(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	rewriteWrittenCatalogOnce(t, home, `{"models":[`, `{"zz_top":1,"models":[`)
	rewriteWrittenCatalogOnce(t, home, `"slug":"Qwen3.8-27B-Q4_K_M",`, `"slug":"Qwen3.8-27B-Q4_K_M","zz_row":1,"zz_row":2,`)
	rewriteWrittenCatalogOnce(t, home, `"truncation_policy":`, `"zz_deep":{"k":[true,null]},"truncation_policy":`)
	written, err := os.ReadFile(filepath.Join(home, "catalog.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	req := catalogRequest(t, home, workDir, "low")
	if _, err := ReadProviderSnapshot(req); err != nil {
		t.Fatalf("ReadProviderSnapshot(unknown keys) = %v, want success", err)
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		plan, err := buildCodexPlanForMode(t, req, mode)
		if err != nil {
			t.Fatalf("BuildPlan(%s, unknown keys) = %v, want success", mode, err)
		}
		sealed, err := os.ReadFile(effectiveCatalogPath(t, plan))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sealed, written) {
			t.Fatalf("BuildPlan(%s) sealed bytes differ from the validated catalog", mode)
		}
	}
}

// nativeCodex159 returns the installed Codex binary when it is exactly the
// pinned 0.159.0 the catalog contract targets, or skips the test.
func nativeCodex159(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	home := t.TempDir()
	env := append(filterEnvKeys(os.Environ(), "HOME", "CODEX_HOME"), "HOME="+home, "CODEX_HOME="+home)
	version := exec.CommandContext(ctx, binary, "--version")
	version.Env = env
	output, err := version.Output()
	if err != nil || !strings.Contains(string(output), "0.159.0") {
		t.Skip("native check pins Codex 0.159.0")
	}
	return binary
}

// TestLocalCatalogNativeParityForDuplicateAndAlias cross-checks the exact-key
// gate against the installed Codex 0.159.0 loader on scratch homes only: a
// duplicate recognized field fails natively, a case alias loads with the
// canonical value, and repeated unknown keys load. It skips when the pinned
// binary is unavailable.
func TestLocalCatalogNativeParityForDuplicateAndAlias(t *testing.T) {
	t.Parallel()
	binary := nativeCodex159(t)
	t.Run("duplicate_shell_type", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
		rewriteWrittenCatalogOnce(t, home, `"shell_type":`, `"shell_type":"unified_exec","shell_type":`)
		req := catalogRequest(t, home, workDir, "low")
		if _, err := buildCodexPlan(t, req); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		env := append(filterEnvKeys(os.Environ(), "HOME", "CODEX_HOME"), "HOME="+home, "CODEX_HOME="+home)
		command := exec.CommandContext(ctx, binary, "debug", "models", "-c", "model_catalog_json="+strconv.Quote(filepath.Join(home, "catalog.local.json")))
		command.Env = env
		out, err := command.CombinedOutput()
		if err == nil {
			t.Fatalf("native loader accepted a duplicate recognized field: %.512s", out)
		}
		if !strings.Contains(string(out), "duplicate field") {
			t.Fatalf("native failure does not name the duplicate field: %.512s", out)
		}
	})
	t.Run("case_alias_lite", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
		rewriteWrittenCatalogOnce(t, home, `"use_responses_lite":false`, `"use_responses_lite":true,"USE_RESPONSES_LITE":false`)
		req := catalogRequest(t, home, workDir, "low")
		if _, err := buildCodexPlan(t, req); !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderUnsupported)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		env := append(filterEnvKeys(os.Environ(), "HOME", "CODEX_HOME"), "HOME="+home, "CODEX_HOME="+home)
		command := exec.CommandContext(ctx, binary, "debug", "models", "-c", "model_catalog_json="+strconv.Quote(filepath.Join(home, "catalog.local.json")))
		command.Env = env
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("native loader refused the alias catalog: %v: %.512s", err, out)
		}
		var loaded struct {
			Models []struct {
				UseResponsesLite bool `json:"use_responses_lite"`
			} `json:"models"`
		}
		if err := json.Unmarshal(out, &loaded); err != nil || len(loaded.Models) == 0 {
			t.Fatalf("parse native models output: %v: %.512s", err, out)
		}
		if !loaded.Models[0].UseResponsesLite {
			t.Fatalf("native loader did not read Lite=true from the canonical key")
		}
	})
	t.Run("duplicate_unknown", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
		rewriteWrittenCatalogOnce(t, home, `"slug":"Qwen3.8-27B-Q4_K_M",`, `"slug":"Qwen3.8-27B-Q4_K_M","zz_row":1,"zz_row":2,`)
		req := catalogRequest(t, home, workDir, "low")
		plan, err := buildCodexPlan(t, req)
		if err != nil {
			t.Fatalf("BuildPlan error = %v, want success", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		env := append(filterEnvKeys(os.Environ(), "HOME", "CODEX_HOME"), "HOME="+home, "CODEX_HOME="+home)
		command := exec.CommandContext(ctx, binary, "debug", "models", "-c", "model_catalog_json="+strconv.Quote(effectiveCatalogPath(t, plan)))
		command.Env = env
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("native loader refused repeated unknown keys: %v: %.512s", err, out)
		}
	})
}
