package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelToolPolicyMutants(t *testing.T) {
	root, _ := os.Getwd()
	root = filepath.Clean(filepath.Join(root, "../.."))
	generated, err := generatedCuratorConflictMutants(root)
	if err != nil {
		t.Fatal(err)
	}
	members := append(narrowingMutants(), generated...)
	count := 0
	for _, m := range members {
		if strings.Contains(m.file, "toolpolicy") || strings.Contains(m.testName, "AskUserQuestion") || strings.Contains(m.testName, "Denial") {
			count++
			t.Log(m.name)
		}
	}
	if count == 0 {
		t.Fatalf("denied-tool narrowing mutants=0; inventory mapped two refusal sites but neither is generated")
	}
}

// The review census now requires all named policy classes, rather than merely
// some positive count. Execution and kills are recorded by the normal harness.
func TestAskUserQuestionPolicyMutantCensus(t *testing.T) {
	required := map[string]bool{}
	for _, name := range []string{"dryrun-deny", "caller-read-dropped", "allow-bare-skipped", "settings-bare-skipped", "settings-unreadable-admitted", "settings-invalid-admitted", "scalar-value-classified", "trimspace-restored", "settings-rule-trim-restored", "settings-inline-trim-restored", "eager-settings-dropped", "ambiguity-skipped", "conditional-deny"} {
		required["claude-toolpolicy-"+name] = false
	}
	for _, candidate := range narrowingMutants() {
		if _, known := required[candidate.name]; !known {
			continue
		}
		if required[candidate.name] {
			t.Fatalf("duplicate required mutant %s", candidate.name)
		}
		if candidate.testName == "" || candidate.runPattern == "" || candidate.failureText == "" || candidate.testPackage != "./pkg/agentic/systems/claude" {
			t.Fatalf("policy mutant %s has no production behavioral test", candidate.name)
		}
		required[candidate.name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing required policy class mutant: %s", name)
		}
	}
}
