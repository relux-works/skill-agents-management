package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticMutantProcessEvidenceRetainsOutputAndExit(t *testing.T) {
	for _, red := range []bool{false, true} {
		t.Run(fmt.Sprint(red), func(t *testing.T) {
			root := t.TempDir()
			fixture := `package fixture
import "testing"
func TestWitness(t *testing.T) { t.Log("first evidence line"); %s; t.Log("last evidence line") }
`
			statement := `t.Log("green witness")`
			if red {
				statement = `t.Error("expected red witness")`
			}
			for name, body := range map[string]string{"go.mod": "module evidence.fixture\n\ngo 1.25.5\n", "witness_test.go": fmt.Sprintf(fixture, statement)} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			candidate := mutant{name: "muse-evidence-witness", testPackage: ".", runPattern: "^TestWitness$"}
			output, code := runCandidateTests(root, root, "", "", candidate)
			want := 0
			if red {
				want = 1
			}
			if code != want || !strings.Contains(output, "first evidence line") || !strings.Contains(output, "last evidence line") {
				t.Fatalf("exit=%d want=%d output=%s", code, want, output)
			}
			logs, err := filepath.Glob(filepath.Join(root, ".temp", "launch-context-mutants", "process-evidence", "*.log"))
			if err != nil || len(logs) != 1 {
				t.Fatalf("logs=%v err=%v", logs, err)
			}
			data, err := os.ReadFile(logs[0])
			record := fmt.Sprintf("MUTANT_PROCESS | mutant=%s | exit=%d | package=. | pattern=^TestWitness$\n", candidate.name, want)
			if err != nil || string(data) != record+output {
				t.Fatalf("process evidence lost output/exit: err=%v\n%s", err, data)
			}
		})
	}
}

func TestMuseSealMutantCensus(t *testing.T) {
	required := map[string]bool{}
	for _, name := range []string{"binary-hash-skipped", "release-mismatch-skipped", "unverified-plan-admitted-unsealed", "short-release-bound", "finalized-import-refused", "finalized-release-check-skipped", "finalized-pin-check-skipped", "finalized-xdg-single-layer-admitted", "finalized-xdg-identity-check-skipped", "keyed-import-wrong-key-admitted", "env-value-in-selector", "duplicate-xdg-config-admitted", "env-cache-home-unchecked", "argv-extra-admitted"} {
		required["muse-seal-"+name] = false
	}
	required["muse-resume-1-4-2-row-removed"] = false
	for _, candidate := range narrowingMutants() {
		if _, ok := required[candidate.name]; !ok {
			continue
		}
		if required[candidate.name] {
			t.Fatalf("duplicate Muse mutant %s", candidate.name)
		}
		if candidate.testName == "" || candidate.runPattern == "" || candidate.failureText == "" || candidate.testPackage != "./pkg/agentic/systems/muse" {
			t.Fatalf("Muse mutant lacks named behavioral gate: %s", candidate.name)
		}
		required[candidate.name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing required Muse narrowing mutant %s", name)
		}
	}
}

// TestMuseSealMutantKillsExecute is the kill census: definitions alone prove
// nothing, because a shadowed mutant keeps its definition while its inner
// test passes. Each registered Muse seal mutant is applied to an isolated
// copy of the module and its named test must fail there with the mutant's
// targeted assertion. A mutant whose inner test passes fails this census.
func TestMuseSealMutantKillsExecute(t *testing.T) {
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
	for _, candidate := range museSealMutants() {
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

func TestMuseNetworkMutantCensus(t *testing.T) {
	required := map[string]bool{}
	for _, name := range []string{"release-mismatch-admitted", "interactive-replay-admitted", "managed-hook-patch-skipped", "owned-source-reread", "source-drift-admitted", "version-gate-widened", "mcp-injection-skipped", "hook-wrapping-skipped", "duplicate-command-admitted", "mcp-transport-alias-admitted", "prompt-hook-admitted", "project-source-admitted", "private-bytes-unverified", "home-patch-admitted", "effective-first-wins", "path-first-match", "duplicate-path-exempted", "duplicate-path-admission-narrowed", "duplicate-xdg-config-admitted", "unknown-sibling-admitted"} {
		required["muse-network-"+name] = false
	}
	for _, candidate := range narrowingMutants() {
		if _, ok := required[candidate.name]; !ok {
			continue
		}
		if required[candidate.name] {
			t.Fatalf("duplicate Muse mutant %s", candidate.name)
		}
		if candidate.testName == "" || candidate.runPattern == "" || candidate.failureText == "" || candidate.testPackage != "./pkg/agentic/systems/muse" {
			t.Fatalf("Muse mutant lacks named behavioral gate: %s", candidate.name)
		}
		required[candidate.name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing required Muse narrowing mutant %s", name)
		}
	}
}
