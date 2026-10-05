package main

import "testing"

// TestClaudePlanSessionMutantCensus holds the registered Plan.Session members
// to the evidence contract: every AC7 named mutant and every gate group is
// present exactly once, each carries a named behavioral test through a real
// package, the narrowing statement and the failure text it must produce, and
// each source edit is a replacement (never a delete-only empty one).
func TestClaudePlanSessionMutantCensus(t *testing.T) {
	required := map[string]bool{}
	for _, name := range []string{
		"index-off-by-one", "settings-origin-dropped", "left-nil-for-claude", "nil-outside-interactive", "argv-enables-nothing",
		"duplicate-name-admitted", "duplicate-rc-admitted", "duplicate-prefix-admitted", "dangling-name-admitted", "dangling-prefix-admitted",
		"conflicting-names-admitted", "settings-null-admitted", "settings-quoted-true-admitted", "settings-unreadable-admitted",
		"argv-handed-uncopied", "record-attached-uncopied", "index-bound-off-by-one", "negative-index-admitted", "duplicate-zero-index-admitted",
		"nil-index-slice-admitted", "disabled-with-one-index-admitted",
		"seal-snapshot-skips-session", "seal-finalized-skips-session", "seal-compares-index-count-only", "seal-ignores-name-text",
		"in-process-verify-ignores-a-dropped-session", "snapshot-ignores-a-dropped-session",
		"imported-verifier-binds-an-absent-session", "imported-plan-carries-the-process-session",
		"empty-collection-equals-nil", "clone-normalizes-nil-collection-to-empty",
		"tail-starting-with-rc-not-rederived",
		"foreign-argv-helper-reexported", "foreign-argv-method-keeps-the-fill-name", "fill-admits-an-unissued-fill",
		"fill-gains-an-argv-writer", "fill-exports-its-argv-field",
		"projector-function-returns", "projector-method-under-new-name",
	} {
		required["claude-plan-session-"+name] = false
	}
	for _, candidate := range narrowingMutants() {
		seen, wanted := required[candidate.name]
		if !wanted {
			continue
		}
		if seen {
			t.Fatalf("duplicate Plan.Session mutant %s", candidate.name)
		}
		if candidate.testName == "" || candidate.runPattern == "" || candidate.failureText == "" || candidate.narrows == "" {
			t.Fatalf("Plan.Session mutant lacks a named behavioral gate: %s", candidate.name)
		}
		if candidate.testPackage != "./pkg/agentic" && candidate.testPackage != "./pkg/agentic/systems/claude" {
			t.Fatalf("Plan.Session mutant %s runs in %q, want a real production package", candidate.name, candidate.testPackage)
		}
		if len(candidate.replacements) == 0 {
			t.Fatalf("Plan.Session mutant %s carries no replacement", candidate.name)
		}
		for _, change := range candidate.replacements {
			if change.before == "" || change.after == "" || change.before == change.after {
				t.Fatalf("Plan.Session mutant %s is not a narrowing replacement", candidate.name)
			}
		}
		required[candidate.name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing required Plan.Session narrowing mutant %s", name)
		}
	}
}
