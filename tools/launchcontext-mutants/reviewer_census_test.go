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

func TestSealReworkMutantCensus(t *testing.T) {
	required := map[string]bool{}
	for _, name := range []string{"seal-import-skips-catalog-digest", "seal-finalize-admits-extra-argv", "seal-muse-accepts-unsealed", "seal-finalize-skips-base-verification", "seal-import-drops-one-binding", "seal-strict-decoder-unknown-member", "seal-selector-carries-value", "seal-export-returns-alias", "seal-exec-boundary-allows-blank", "seal-hosted-admits-unknown-version", "seal-unsealed-admits-binding-member", "seal-export-collapses-empty-selectors", "seal-finalize-admits-invalid-utf8-tail", "seal-typed-import-admits-invalid-utf8", "seal-export-admits-invalid-utf8-binding"} {
		required[name] = false
	}
	for _, m := range sealMutants() {
		if _, ok := required[m.name]; !ok {
			continue
		}
		if required[m.name] || m.testName == "" || m.failureText == "" || len(m.replacements) == 0 {
			t.Fatalf("unbound/duplicate seal mutant %s", m.name)
		}
		required[m.name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing seal mutant %s", name)
		}
	}
}

// TestSealMutantKillsExecute is the kill census: definitions alone prove
// nothing, because a shadowed mutant keeps its definition while its inner
// test passes. Each registered seal mutant is applied to an isolated copy
// of the module and its named test must fail there with the mutant's
// targeted assertion. A mutant whose inner test passes fails this census.
func TestSealMutantKillsExecute(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get test package directory: %v", err)
	}
	moduleRoot := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	gitDir, err := commandOutput(moduleRoot, "git", "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatalf("locate worktree git directory: %v", err)
	}
	gitIndex, err := commandOutput(moduleRoot, "git", "rev-parse", "--git-path", "index")
	if err != nil {
		t.Fatalf("locate worktree git index: %v", err)
	}
	for _, candidate := range sealMutants() {
		t.Run(candidate.name, func(t *testing.T) {
			isolated := t.TempDir()
			if err := copyTree(moduleRoot, isolated); err != nil {
				t.Fatalf("copy module for mutant %q: %v", candidate.name, err)
			}
			for _, change := range candidate.replacements {
				sourceFile := change.file
				if sourceFile == "" {
					sourceFile = candidate.file
				}
				path := filepath.Join(isolated, sourceFile)
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read mutant %q source %s: %v", candidate.name, sourceFile, err)
				}
				if count := strings.Count(string(body), change.before); count != 1 {
					t.Fatalf("mutant %q expected exactly one source site in %s, found %d", candidate.name, sourceFile, count)
				}
				mutated := strings.Replace(string(body), change.before, change.after, 1)
				if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
					t.Fatalf("write mutant %q source %s: %v", candidate.name, sourceFile, err)
				}
			}
			output, exitCode := runCandidateTests(isolated, moduleRoot, gitDir, gitIndex, candidate)
			if err := validateMutantNamedTestExecution(candidate, output); err != nil {
				t.Fatalf("mutant %q selected no named test: %v", candidate.name, err)
			}
			if exitCode == 0 {
				t.Fatalf("mutant %q survived: inner test %s passed", candidate.name, candidate.testName)
			}
			if !strings.Contains(output, "--- FAIL: "+candidate.testName) {
				t.Fatalf("mutant %q failed without named test %q (exit %d):\n%s", candidate.name, candidate.testName, exitCode, output)
			}
			if candidate.failureText != "" && !strings.Contains(output, candidate.failureText) {
				t.Fatalf("mutant %q did not fail for its targeted assertion %q (exit %d):\n%s", candidate.name, candidate.failureText, exitCode, output)
			}
		})
	}
}
