package main

import "testing"

// TestManagedSessionMutantCensus holds the registered typed-reservation
// members to the evidence contract: every AC5 named mutant and every gate
// group is present exactly once, each carries a named behavioral test through
// a real package, the narrowing statement and the failure text it must
// produce, and each source edit is a replacement (never a delete-only empty
// one).
func TestManagedSessionMutantCensus(t *testing.T) {
	required := map[string]bool{}
	for _, name := range []string{
		"argv-slot-position-not-fixed", "env-slot-position-not-fixed", "third-slot-admitted", "third-slot-unknown-name-admitted",
		"verifier-skips-the-argv-slot", "verifier-admits-a-moved-argv-slot", "verifier-skips-the-env-slot", "verifier-skips-the-env-slot-value", "verifier-admits-a-moved-env-slot",
		"slot-admitted-without-a-reservation",
		"forged-ses-admitted", "forged-uuid-admitted", "env-slot-mismatch-admitted", "argv-slot-mismatch-admitted", "env-slot-carries-the-native-id",
		"reservation-intent-latest-admitted", "reservation-intent-identity-on-new-admitted", "reservation-intent-empty-kind-identity-admitted", "tail-continue-admitted", "tail-adoption-admitted", "env-slot-already-carried-admitted",
		"system-without-a-session-grammar-admitted", "slots-alone-do-not-rederive-the-session",
	} {
		required["managed-session-"+name] = false
	}
	for _, candidate := range narrowingMutants() {
		seen, wanted := required[candidate.name]
		if !wanted {
			continue
		}
		if seen {
			t.Fatalf("duplicate managed-session mutant %s", candidate.name)
		}
		if candidate.testName == "" || candidate.runPattern == "" || candidate.failureText == "" || candidate.narrows == "" {
			t.Fatalf("managed-session mutant lacks a named behavioral gate: %s", candidate.name)
		}
		if candidate.testPackage != "./pkg/agentic" && candidate.testPackage != "./pkg/agentic/systems/codex" {
			t.Fatalf("managed-session mutant %s runs in %q, want a real production package", candidate.name, candidate.testPackage)
		}
		if len(candidate.replacements) == 0 {
			t.Fatalf("managed-session mutant %s carries no replacement", candidate.name)
		}
		for _, change := range candidate.replacements {
			if change.before == "" || change.after == "" || change.before == change.after {
				t.Fatalf("managed-session mutant %s is not a narrowing replacement", candidate.name)
			}
		}
		required[candidate.name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing required managed-session narrowing mutant %s", name)
		}
	}
}
