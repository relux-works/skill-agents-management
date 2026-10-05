package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchBoundaryMutantCensus(t *testing.T) {
	required := map[string]bool{}
	for _, name := range []string{"launch-boundary-binary-identity-skipped", "launch-boundary-mcp-check-skipped", "launch-boundary-prompt-check-skipped", "launch-boundary-path-case-admitted", "launch-boundary-refusal-message-altered", "launch-boundary-mcp-inline-admission-skipped", "launch-boundary-mcp-path-admission-skipped", "launch-boundary-mcp-permission-message-altered", "launch-boundary-provenance-wiring-restored"} {
		required[name] = false
	}
	for _, candidate := range launchBoundaryMutants() {
		if _, ok := required[candidate.name]; !ok {
			continue
		}
		if required[candidate.name] {
			t.Fatalf("duplicate launch-boundary mutant %s", candidate.name)
		}
		if candidate.testName == "" || candidate.runPattern == "" || candidate.failureText == "" || candidate.testPackage != "./pkg/agentic" || candidate.narrows == "" || len(candidate.replacements) == 0 {
			t.Fatalf("launch-boundary mutant lacks named behavioral gate: %s", candidate.name)
		}
		required[candidate.name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing required launch-boundary narrowing mutant %s", name)
		}
	}
}

// TestLaunchBoundaryMutantKillsExecute is the kill census: definitions alone
// prove nothing, because a shadowed mutant keeps its definition while its
// inner test passes. Each registered launch-boundary mutant is applied to an
// isolated copy of the module and its named test must fail there with the
// mutant's targeted assertion. A mutant whose inner test passes fails this
// census.
func TestLaunchBoundaryMutantKillsExecute(t *testing.T) {
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
	for _, candidate := range launchBoundaryMutants() {
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
