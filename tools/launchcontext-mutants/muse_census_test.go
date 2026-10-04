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
