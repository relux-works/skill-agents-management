package changelog_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestChangelogCompositionChild is an explicitly selected helper inside the
// real test binary. All descendants use the same managed launch entry point.
func TestChangelogCompositionChild(t *testing.T) {
	mode := os.Getenv("CHANGELOG_COMPOSITION_MODE")
	if mode == "" {
		t.Skip("selected child only")
	}
	if mode == "closed-launch" {
		var paths []string
		if err := json.Unmarshal([]byte(os.Getenv(changelogScopesEnv)), &paths); err != nil || len(paths) == 0 {
			t.Fatal("missing scope")
		}
		if err := os.WriteFile(paths[len(paths)-1]+".closed", []byte("closed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		marker := os.Getenv("CHANGELOG_COMPOSITION_PID")
		_, _, _, err := runChangelogChild("", fixtureEnv(t), 10*time.Second, "sh", "-c", `echo escaped > "$1"`, "closed-launch", marker)
		if err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("closed scope admitted child: %v", err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("closed scope child executed")
		}
		return
	}
	if mode == "sleeper" {
		_, _, _, err := runChangelogChild("", fixtureEnv(t), 60*time.Second,
			"sh", "-c", `echo $$ > "$1"; sleep 30`, "sleeper", os.Getenv("CHANGELOG_COMPOSITION_PID"))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	depth, _ := strconv.Atoi(os.Getenv("CHANGELOG_COMPOSITION_DEPTH"))
	if depth > 0 {
		env := changelogSetEnv(fixtureEnv(t), "CHANGELOG_COMPOSITION_DEPTH", strconv.Itoa(depth-1))
		result := runChangelogOutcome("", env, 30*time.Second, testExecutable(t), "-test.run=^TestChangelogCompositionChild$")
		fmt.Print(result.stdout)
		fmt.Fprint(os.Stderr, result.stderr)
		t.Fail()
		return
	}
	stream := os.Stdout
	if os.Getenv("CHANGELOG_COMPOSITION_STREAM") == "stderr" {
		stream = os.Stderr
	}
	size, _ := strconv.Atoi(os.Getenv("CHANGELOG_COMPOSITION_SIZE"))
	fmt.Fprintln(stream, "--- FAIL: CompositionWitness")
	fmt.Fprint(stream, strings.Repeat("h", size))
	if mode == "timeout" {
		_, _, _, err := runChangelogChild("", fixtureEnv(t), time.Nanosecond, "sh", "-c", "sleep 30")
		if _, ok := asChangelogTimeout(err); !ok {
			t.Fatalf("expected nested timeout: %v", err)
		}
		fmt.Fprintln(stream, err)
	}
	fmt.Fprint(stream, strings.Repeat("t", size))
	t.Fail() // Deliberate named-failure shape, never a mutation of the script.
}

func testExecutable(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChangelogOuterDeadlineKillsNestedDescendant(t *testing.T) {
	if !changelogProcessGroupsSupported() {
		t.Skip("unix process groups required")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	env := append(fixtureEnv(t), "CHANGELOG_COMPOSITION_MODE=sleeper", "CHANGELOG_COMPOSITION_PID="+pidFile)
	result := runChangelogOutcome("", env, time.Second, testExecutable(t), "-test.run=^TestChangelogCompositionChild$")
	if _, ok := asChangelogTimeout(result.err); !ok {
		t.Fatalf("outer named timeout missing: %v", result.err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("nested sleeper did not start: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	defer killChangelogChildProcessGroup(pid) // Mutants must leave no sleeper behind.
	time.Sleep(300 * time.Millisecond)
	_, stderr, exit, err := runChangelogChild("", fixtureEnv(t), 10*time.Second, "sh", "-c", `kill -0 "$1"`, "probe", strconv.Itoa(pid))
	if err != nil || exit == 0 {
		t.Fatalf("nested descendant alive after outer deadline: exit=%d err=%v stderr=%s", exit, err, stderr)
	}
}

// A closing supervisor refuses new admission, including a launch from an
// otherwise valid managed child. This drives the registered launch entry point.
func TestChangelogClosedScopeRefusesLaunch(t *testing.T) {
	if !changelogProcessGroupsSupported() {
		t.Skip("unix process supervision required")
	}
	env := append(fixtureEnv(t), "CHANGELOG_COMPOSITION_MODE=closed-launch",
		"CHANGELOG_COMPOSITION_PID="+filepath.Join(t.TempDir(), "marker"))
	result := runChangelogOutcome("", env, 20*time.Second, testExecutable(t), "-test.run=^TestChangelogCompositionChild$")
	if result.err != nil || result.exit != 0 {
		t.Fatalf("closed scope refusal failed: exit=%d err=%v output=%s", result.exit, result.err, result.out)
	}
}

// Generated class coverage: depth, stream, retained/truncated payload and
// timeout/clean trees vary independently. The classifier sees actual managed
// outcomes, not constructed text. Timeout state must survive every composition;
// truncation without readable evidence must be refused even for a clean tree.
func TestChangelogTimeoutAttestationProperty(t *testing.T) {
	for depth := 0; depth <= 2; depth++ {
		for _, stream := range []string{"stdout", "stderr"} {
			for _, size := range []int{64, 2 << 20} {
				for _, mode := range []string{"timeout", "clean"} {
					name := fmt.Sprintf("depth=%d/%s/size=%d/%s", depth, stream, size, mode)
					t.Run(name, func(t *testing.T) {
						env := append(fixtureEnv(t), "CHANGELOG_COMPOSITION_MODE="+mode,
							"CHANGELOG_COMPOSITION_DEPTH="+strconv.Itoa(depth), "CHANGELOG_COMPOSITION_STREAM="+stream,
							"CHANGELOG_COMPOSITION_SIZE="+strconv.Itoa(size))
						result := runChangelogOutcome("", env, 30*time.Second, testExecutable(t), "-test.run=^TestChangelogCompositionChild$")
						if result.err != nil || result.exit == 0 || !strings.Contains(result.out, "--- FAIL: CompositionWitness") {
							t.Fatalf("invalid premise: exit=%d err=%v", result.exit, result.err)
						}
						if !result.attested || result.timedOut != (mode == "timeout") {
							t.Fatalf("sticky attestation lost: attested=%t timeout=%t", result.attested, result.timedOut)
						}
						control := changelogChildOutcome{exit: 0, attested: true}
						verdict := classifyChangelogKill("composition", "CompositionWitness", "control", result, control)
						if verdict.kill != (mode == "clean") {
							t.Fatalf("timeout invariant broken: mode=%s truncated=%t verdict=%+v", mode, result.truncated, verdict)
						}
						if size > changelogChildStreamCap {
							if !result.truncated {
								t.Fatal("capture not marked truncated")
							}
							if mode == "timeout" && changelogOutputMarksTimeout(result.out) {
								t.Fatal("probe did not hide diagnostic in discarded middle")
							}
							result.attested = false
							if classifyChangelogKill("unknown", "CompositionWitness", "control", result, control).kill {
								t.Fatal("truncated unknown outcome credited as kill")
							}
						}
					})
				}
			}
		}
	}
}

func TestChangelogAttestationReadFailuresRefused(t *testing.T) {
	for _, contents := range []string{"", "timeout\n", "clean\nmalformed\n", "clean\ntimeout\nclean\n"} {
		t.Run(strconv.Quote(contents), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attestation")
			writeFile(t, path, contents)
			attested, timedOut := readChangelogAttestation(path)
			outcome := changelogChildOutcome{out: "--- FAIL: witness", exit: 1, attested: attested, timedOut: timedOut}
			if classifyChangelogKill("bad-evidence", "witness", "control", outcome, changelogChildOutcome{attested: true}).kill {
				t.Fatal("malformed evidence admitted")
			}
		})
	}
	if clean, _ := readChangelogAttestation(filepath.Join(t.TempDir(), "absent")); clean {
		t.Fatal("missing evidence admitted")
	}
}

func TestChangelogCompositionMutantsKilled(t *testing.T) {
	if !changelogProcessGroupsSupported() {
		t.Skip("unix process groups required")
	}
	mutants := []struct{ id, file, from, to, witness string }{
		{"ignore-closed-ancestor", "exec_unix_test.go", `if _, err := os.Stat(path + ".closed"); err == nil {`, `if _, err := os.Stat(path + ".closed"); err == nil && path == scope.own {`, "TestChangelogClosedScopeRefusesLaunch"},
		{"nested-unregistered-group", "exec_unix_test.go", "for _, path := range scope.paths {\n\t\t\t// Register", "for _, path := range scope.paths[len(scope.paths)-1:] {\n\t\t\t// Register", "TestChangelogOuterDeadlineKillsNestedDescendant"},
		{"timeout-text-only", "mutants_test.go", "\tif witness.timedOut {\n", "\tif changelogOutputMarksTimeout(witness.out) {\n", "TestChangelogTimeoutAttestationProperty"},
		{"credit-truncated-unknown", "mutants_test.go", "return outcome.attested", "return outcome.attested || outcome.truncated", "TestChangelogTimeoutAttestationProperty"},
	}
	for _, m := range mutants {
		t.Run(m.id, func(t *testing.T) {
			splices := [][2]string{{m.from, m.to}}

			out, exit := runOverlayMutant(t, m.id, filepath.Join(repoRoot(t), "internal", "changelog", m.file), splices, "^"+m.witness+"$")
			if exit == 0 || !strings.Contains(out, "--- FAIL: "+m.witness) {
				t.Fatalf("%s survivor: exit %d\n%s", m.id, exit, out)
			}
			t.Logf("%s killed by %s (exit %d expected-red)", m.id, m.witness, exit)
		})
	}
}
