package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

type catalogStringShape struct {
	name, spelling string // Includes quotes, never re-encoded with json.Marshal.
}

func invalidCatalogStringShapes() []catalogStringShape {
	shapes := []catalogStringShape{
		{"lone_high", `"\ud800"`},
		{"lone_high_upper_bound", `"\uDBFF"`},
		{"lone_low", `"\udc00"`},
		{"lone_low_upper_bound", `"\uDFFF"`},
		{"reversed_pair", `"\udc00\ud800"`},
		{"high_then_high", `"\ud800\udbff"`},
		{"high_then_scalar", `"\ud800\u0061"`},
		{"high_then_raw_scalar", `"\ud800a"`},
		{"high_then_escaped_backslash", `"\ud800\\udc00"`},
		{"high_then_truncated_low", `"\ud800\udc0"`},
		{"invalid_hex", `"\uZZZZ"`},
		{"short_hex", `"\u123"`},
		{"invalid_escape", `"\x41"`},
		{"unterminated", `"no closing quote`},
		{"trailing_backslash", `"trailing\`},
		{"invalid_utf8_byte", "\"\xff\""},
		{"invalid_utf8_continuation", "\"\x80\""},
		{"invalid_utf8_overlong", "\"\xc0\xaf\""},
		{"invalid_utf8_truncated", "\"\xe2\x82\""},
		{"invalid_utf8_encoded_surrogate", "\"\xed\xa0\x80\""},
		{"invalid_utf8_above_unicode", "\"\xf4\x90\x80\x80\""},
	}
	for c := byte(0); c < 0x20; c++ {
		shapes = append(shapes, catalogStringShape{fmt.Sprintf("raw_control_%02x", c), "\"" + string([]byte{c}) + "\""})
	}
	return shapes
}

// injectCatalogString exercises keys and values at every depth, including
// unselected rows and ignored unknown fields. None of the protocol/effort
// inputs change, so malformed lexing is the only reason to refuse.
func injectCatalogString(t *testing.T, home, scope, spelling string) {
	t.Helper()
	switch scope {
	case "selected_value":
		rewriteWrittenCatalogOnce(t, home, `"display_name":"GPT-6-Luna"`, `"display_name":`+spelling)
	case "unselected_value":
		appendUnselectedRow(t, home, "other-row")
		rewriteWrittenCatalogLast(t, home, `"display_name":"GPT-6-Luna"`, `"display_name":`+spelling)
	case "root_key":
		rewriteWrittenCatalogOnce(t, home, `{"models":[`, `{`+spelling+`:0,"models":[`)
	case "row_key":
		rewriteWrittenCatalogOnce(t, home, `"slug":"Qwen3.8-27B-Q4_K_M",`, `"slug":"Qwen3.8-27B-Q4_K_M",`+spelling+`:0,`)
	case "nested_key":
		rewriteWrittenCatalogOnce(t, home, `"reminder_threshold_tokens":6144`, spelling+`:0,"reminder_threshold_tokens":6144`)
	case "unknown_nested_value":
		rewriteWrittenCatalogOnce(t, home, `{"models":[`, `{"ignored":{"list":[`+spelling+`]},"models":[`)
	default:
		t.Fatal("unknown scope", scope)
	}
}

// TestLocalCatalogNativeStringLexingRefuses drives the production resolver
// via both public entries. Invalid bytes cannot yield a snapshot; an older
// valid snapshot must still plan its retained bytes after the lexical attack,
// on exec and dry-run, without re-reading the malformed operator catalog.
func TestLocalCatalogNativeStringLexingRefuses(t *testing.T) {
	for _, shape := range invalidCatalogStringShapes() {
		for _, scope := range []string{"selected_value", "unselected_value", "root_key", "row_key", "nested_key", "unknown_nested_value"} {
			t.Run(shape.name+"/"+scope, func(t *testing.T) {
				home, workDir := t.TempDir(), t.TempDir()
				writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				req := catalogRequest(t, home, workDir, "low")
				snapshot, err := ReadProviderSnapshot(req)
				if err != nil {
					t.Fatal("valid control", err)
				}
				retained := []byte(snapshot.catalogData)
				injectCatalogString(t, home, scope, shape.spelling)
				invalid, err := ReadProviderSnapshot(req)
				assertCatalogStringMalformed(t, err)
				if invalid != nil {
					t.Fatal("malformed bytes produced a snapshot")
				}
				for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
					plan, err := buildCodexPlanForMode(t, req, mode)
					assertCatalogStringMalformed(t, err)
					if plan.Binary != "" || len(plan.Argv) != 0 {
						t.Fatal("malformed bytes produced a plan")
					}
					plan, err = buildCodexPlanForMode(t, withSnapshot(req, snapshot), mode)
					if err != nil {
						t.Fatalf("retained snapshot (%s): %v", mode, err)
					}
					sealed, err := os.ReadFile(effectiveCatalogPath(t, plan))
					if err != nil || !bytes.Equal(sealed, retained) {
						t.Fatalf("snapshot (%s) launched invalid replacement: %v", mode, err)
					}
					if err := plan.VerifyBeforeExec(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func assertCatalogStringMalformed(t *testing.T, err error) {
	t.Helper()
	var refusal *agentic.LocalProviderRefusal
	if !errors.Is(err, agentic.ErrLocalProviderMalformed) || !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderMalformed {
		t.Fatalf("error = %v, want typed malformed", err)
	}
}

func validCatalogStringShapes() []catalogStringShape {
	return []catalogStringShape{
		{"surrogate_pair_min", `"\ud800\udc00"`},
		{"surrogate_pair_max", `"\uDBFF\uDFFF"`},
		{"scalar_boundaries", `"\u0000\ud7ff\ue000\uffff"`},
		{"all_json_escapes", `"\"\\\/\b\f\n\r\t"`},
		{"escaped_literal_surrogate_text", `"\\ud800"`},
		{"replacement_character", `"�\ufffd"`},
		{"raw_unicode", `"Հայերեն 😀 \u2028\u2029"`},
		{"raw_del_and_c1", "\"\x7f\u0080\u009f\""},
	}
}

func TestLocalCatalogNativeStringLexingAcceptsValidSpellings(t *testing.T) {
	for _, shape := range validCatalogStringShapes() {
		t.Run(shape.name, func(t *testing.T) {
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			injectCatalogString(t, home, "selected_value", shape.spelling)
			injectCatalogString(t, home, "root_key", shape.spelling)
			req := catalogRequest(t, home, workDir, "low")
			snapshot, err := ReadProviderSnapshot(req)
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
				for _, request := range []agentic.LaunchRequest{req, withSnapshot(req, snapshot)} {
					plan, err := buildCodexPlanForMode(t, request, mode)
					if err != nil {
						t.Fatal(err)
					}
					if err := plan.VerifyBeforeExec(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

// Native serde's ignored-value fast path can skip a lone-surrogate escape.
// The brief requires strict strings even there: declare that intentional
// subset explicitly rather than claiming universal native equivalence.
func TestLocalCatalogNativeStringLexingCrossCheck(t *testing.T) {
	binary := nativeCodex159(t)
	tests := []struct {
		name, scope, spelling, diagnostic string
		wantExit                          int
	}{
		{"lone_high_unknown_key", "root_key", `"\ud800"`, "hex escape", 1},
		{"invalid_utf8_key", "root_key", "\"\xff\"", "UTF-8", 1},
		{"ignored_surrogate_value_subset", "unknown_nested_value", `"\ud800"`, "", 0},
	}
	for _, shape := range invalidCatalogStringShapes() {
		tests = append(tests, struct {
			name, scope, spelling, diagnostic string
			wantExit                          int
		}{shape.name, "selected_value", shape.spelling, "", 1})
	}
	for _, shape := range validCatalogStringShapes() {
		tests = append(tests, struct {
			name, scope, spelling, diagnostic string
			wantExit                          int
		}{shape.name, "selected_value", shape.spelling, "", 0})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			injectCatalogString(t, home, test.scope, test.spelling)
			req := catalogRequest(t, home, workDir, "low")
			_, err := ReadProviderSnapshot(req)
			if test.wantExit != 0 || test.name == "ignored_surrogate_value_subset" {
				assertCatalogStringMalformed(t, err)
			} else if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "debug", "models", "-c", "model_catalog_json="+strconv.Quote(filepath.Join(home, "catalog.local.json")))
			command.Env = append(filterEnvKeys(os.Environ(), "HOME", "CODEX_HOME"), "HOME="+home, "CODEX_HOME="+home)
			out, err := command.CombinedOutput()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			t.Logf("native exit=%d (want %d)", code, test.wantExit)
			if code != test.wantExit || (test.diagnostic != "" && !strings.Contains(string(out), test.diagnostic)) {
				t.Fatalf("native exit=%d: %.512s", code, out)
			}
		})
	}
}
