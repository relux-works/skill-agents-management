package ident

import (
	"strings"
	"testing"
)

// The rule is the one every plugin identifier in the module folds through, so
// a change here moves the keys of admitted-pair digests and limit-state
// filenames for BOTH layers at once. That is exactly why it is one function,
// and why it is pinned here rather than only through its callers.
func TestNormalizeAcceptsTheDeclaredSpellings(t *testing.T) {
	cases := map[string]string{
		"claude-code":   "claude-code",
		"codex":         "codex",
		"anthropic":     "anthropic",
		"openai":        "openai",
		"  Codex  ":     "codex",
		"QWEN-CODE":     "qwen-code",
		"opencode2":     "opencode2",
		"a":             "a",
		"agy-2-preview": "agy-2-preview",
	}
	for raw, want := range cases {
		got, reason, ok := Normalize(raw)
		if !ok {
			t.Errorf("Normalize(%q) refused: %s", raw, reason)
			continue
		}
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", raw, got, want)
		}
		if reason != "" {
			t.Errorf("Normalize(%q) succeeded and still gave a reason %q", raw, reason)
		}
	}
}

func TestNormalizeRefusesWithTheRuleItBroke(t *testing.T) {
	cases := map[string]string{
		"":             "it is empty",
		"   ":          "it is empty",
		"claude code":  `it contains " "`,
		"claude--code": "it has an empty hyphen-separated segment",
		"-codex":       "it has an empty hyphen-separated segment",
		"codex-":       "it has an empty hyphen-separated segment",
		"claude_code":  `it contains "_"`,
		"clаude":       `it contains "а"`, // a Cyrillic а, the copy-paste failure
	}
	for raw, wantReason := range cases {
		got, reason, ok := Normalize(raw)
		if ok {
			t.Errorf("Normalize(%q) = %q, want a refusal", raw, got)
			continue
		}
		if !strings.HasPrefix(reason, wantReason) {
			t.Errorf("Normalize(%q) reason = %q, want it to start with %q", raw, reason, wantReason)
		}
		if got != "" {
			t.Errorf("Normalize(%q) refused and still returned %q", raw, got)
		}
	}
}

// Idempotence is what makes the fold safe to apply at every boundary: an id
// that crosses twice must key the same state as one that crossed once.
func TestNormalizeIsIdempotent(t *testing.T) {
	for _, raw := range []string{"  Claude-Code ", "CODEX", "muse", "anthropic"} {
		once, _, ok := Normalize(raw)
		if !ok {
			t.Fatalf("Normalize(%q) refused a legal id", raw)
		}
		twice, _, ok := Normalize(once)
		if !ok {
			t.Fatalf("Normalize(%q) refused its own output", once)
		}
		if once != twice {
			t.Errorf("normalizing %q twice gave %q then %q", raw, once, twice)
		}
	}
}
