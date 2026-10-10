package agentic

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
)

type auxFixtureSystem struct{ *pangolinSystem }

func (auxFixtureSystem) AcceptUnsealedExecGuard() {}
func (auxFixtureSystem) AuxiliaryArgv(role AuxRole) ([]string, error) {
	if role == ClaudeVersionProbe {
		return []string{"--version"}, nil
	}
	if role == ClaudeGoalProbe {
		return []string{"-p", "--output-format", "json", "/goal"}, nil
	}
	return nil, ErrAuxRoleUnknown
}
func auxFixture(t *testing.T) (Plan, []AuxPlan, []AuxResult) {
	t.Helper()
	sys := auxFixtureSystem{newPangolin()}
	sys.binary = filepath.Join(t.TempDir(), "provider")
	if err := execfixture.WriteFile(sys.binary, []byte("original"), 0700); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatal(err)
	}
	primary, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := BuildAuxiliaryPlans(primary)
	if err != nil {
		t.Fatal(err)
	}
	results := []AuxResult{{Role: ClaudeVersionProbe, Status: AuxSucceeded, NoSurvivors: true, Version: "2.1.288"}, {Role: ClaudeGoalProbe, Status: AuxSucceeded, NoSurvivors: true, GoalReady: true}}
	return primary, plans, results
}

func TestAuxUnknownRoleRefuses(t *testing.T) {
	_, plans, _ := auxFixture(t)
	plans[0].Role = "rogue"
	// Even a internally minted seal cannot widen the closed role enum.
	plans[0].seal.role = "rogue"
	if err := plans[0].VerifyBeforeExec(); !errors.Is(err, ErrAuxRoleUnknown) {
		t.Fatalf("unknown auxiliary admitted: %v", err)
	}
}
func TestAuxBinarySwappedBeforePrimaryRefuses(t *testing.T) {
	primary, plans, results := auxFixture(t)
	for _, p := range plans {
		if err := p.VerifyBeforeExec(); err != nil {
			t.Fatal(err)
		}
	}
	if err := execfixture.WriteFile(primary.Binary, []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPrimaryAfterAux(primary, plans, results); !errors.Is(err, ErrAuxRefused) {
		t.Fatalf("swapped binary admitted: %v", err)
	}
}
func TestAuxBinaryReplacementWithIdenticalBytesRefuses(t *testing.T) {
	primary, plans, results := auxFixture(t)
	replacement := primary.Binary + ".replacement"
	if err := execfixture.WriteFile(replacement, []byte("original"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, primary.Binary); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPrimaryAfterAux(primary, plans, results); !errors.Is(err, ErrAuxRefused) {
		t.Fatalf("replacement binary admitted: %v", err)
	}
}

func TestAuxTimeoutYieldsNoPrimaryStart(t *testing.T) {
	primary, plans, results := auxFixture(t)
	results[1].Status = AuxTimedOut
	starts := 0
	if VerifyPrimaryAfterAux(primary, plans, results) == nil {
		starts++
	}
	if starts != 0 {
		t.Fatal("timeout started primary")
	}
}
func TestAuxTimeoutHasDistinctTypedRefusalAndNoPrimaryStart(t *testing.T) {
	for _, index := range []int{0, 1} {
		t.Run(string([]AuxRole{ClaudeVersionProbe, ClaudeGoalProbe}[index]), func(t *testing.T) {
			primary, plans, results := auxFixture(t)
			results[index].Status = AuxTimedOut
			for _, noSurvivors := range []bool{true, false} {
				results[index].NoSurvivors = noSurvivors
				err := VerifyPrimaryAfterAux(primary, plans, results)
				starts := 0
				if err == nil {
					starts++
				}
				var timeout *AuxTimeoutError
				if starts != 0 || !errors.As(err, &timeout) || timeout.Role != results[index].Role || !errors.Is(err, ErrAuxRefused) {
					t.Fatalf("timeout classification or primary refusal lost: starts=%d error=%v", starts, err)
				}
			}
			results[index].Status = AuxFailed
			failed := VerifyPrimaryAfterAux(primary, plans, results)
			var timeout *AuxTimeoutError
			if !errors.Is(failed, ErrAuxRefused) || errors.As(failed, &timeout) {
				t.Fatalf("failed probe classified as timeout or admitted: %v", failed)
			}
		})
	}
}

func TestAuxAdmissionNegatives(t *testing.T) {
	for _, name := range []string{"missing-result", "duplicate-result", "failed", "survivor", "empty-version", "raw-version", "goal-not-ready", "cross-result", "primary-env", "aux-argv", "aux-env", "aux-cwd", "aux-home", "aux-stdin", "timeout-policy", "survivor-policy", "missing-seal", "missing-plans"} {
		t.Run(name, func(t *testing.T) {
			primary, plans, results := auxFixture(t)
			switch name {
			case "missing-result":
				results = results[:1]
			case "duplicate-result":
				results[1] = results[0]
			case "failed":
				results[1].Status = AuxFailed
			case "survivor":
				results[1].NoSurvivors = false
			case "empty-version":
				results[0].Version = ""
			case "raw-version":
				results[0].Version = "2.1.288\nEVIL=1"
			case "goal-not-ready":
				results[1].GoalReady = false
			case "cross-result":
				results[1].Version = "2.1.288"
			case "primary-env":
				primary.Env = append(primary.Env, "EVIL=1")
			case "aux-argv":
				plans[0].Process.Argv = append(plans[0].Process.Argv, "evil")
			case "aux-env":
				plans[0].Process.Env = append(plans[0].Process.Env, "EVIL=1")
			case "aux-cwd":
				plans[0].Process.WorkDir = "/other"
			case "aux-home":
				plans[0].Process.Home = "/other"
			case "aux-stdin":
				plans[0].Process.Stdin = StdinPayload{Attached: true, Bytes: []byte("evil")}
			case "timeout-policy":
				plans[0].HardTimeout++
			case "survivor-policy":
				plans[0].RequireNoSurvivors = false
			case "missing-seal":
				plans[0].seal = nil
			case "missing-plans":
				plans = nil
			}
			if err := VerifyPrimaryAfterAux(primary, plans, results); err == nil {
				t.Fatalf("%s admitted", name)
			}
		})
	}
}
func TestAuxBuildRefusesChangedAdmission(t *testing.T) {
	for _, field := range []string{"argv", "env", "binary", "cwd", "home"} {
		t.Run(field, func(t *testing.T) {
			primary, _, _ := auxFixture(t)
			switch field {
			case "argv":
				primary.Argv = append(primary.Argv, "evil")
			case "env":
				primary.Env = append(primary.Env, "EVIL=1")
			case "binary":
				primary.Binary = "other"
			case "cwd":
				primary.WorkDir = "/other"
			case "home":
				primary.Home = "/other"
			}
			if _, err := BuildAuxiliaryPlans(primary); err == nil {
				t.Fatalf("changed %s admission accepted", field)
			}
		})
	}
}

func TestAuxPrimaryAdmissionProperty(t *testing.T) {
	primary, plans, results := auxFixture(t)
	for _, p := range plans {
		if err := p.VerifyBeforeExec(); err != nil {
			t.Fatal(err)
		}
	}
	if err := VerifyPrimaryAfterAux(primary, plans, results); err != nil {
		t.Fatal(err)
	}
	if primary.Argv[0] == "--version" {
		t.Fatal("aux argv entered primary")
	}
}

// Test-only activation changes private predicate seams. Production never reads
// this environment variable. Each child executes the named real API test.
func init() {
	switch os.Getenv("AGENTS_AUX_TEST_MUTANT") {
	case "timeout-generic-refusal":
		auxTimeoutTyped = func(AuxStatus) bool { return false }
	case "process-argv":
		auxProcessMatches = func(a, b Plan) bool {
			if len(a.Argv) == len(b.Argv)+1 && a.Argv[len(a.Argv)-1] == "evil" {
				a.Argv = a.Argv[:len(a.Argv)-1]
			}
			return sameAuxProcess(a, b)
		}
	case "timeout-policy":
		auxPolicyMatches = func(p AuxPlan) bool {
			return p.RequireNoSurvivors && (p.HardTimeout == p.seal.timeout || p.HardTimeout == p.seal.timeout+1)
		}
	case "raw-version":
		auxResultTyped = func(role AuxRole, result AuxResult) bool {
			return validAuxResult(role, result) || (role == ClaudeVersionProbe && result.Version == "2.1.288\nEVIL=1" && !result.GoalReady)
		}
	case "admitted-home":
		auxAdmissionMatches = func(p Plan) bool {
			return p.WorkDir == p.auxiliaryBasis.workDir && (p.Home == p.auxiliaryBasis.home || p.Home == "/other")
		}
	case "unknown-role":
		auxRoleAllowed = func(role AuxRole) bool { return knownAuxRole(role) || role == "rogue" }
	case "changed-binary":
		auxBinaryMatches = func(a, b auxBinaryIdentity) bool {
			return sameAuxBinary(a, b) || (a.path == b.path && os.SameFile(a.info, b.info) && a.digest == sha256.Sum256([]byte("changed")))
		}
	case "timeout":
		auxStatusSucceeded = func(s AuxStatus) bool { return s == AuxSucceeded || s == AuxTimedOut }
	}
}
func TestAuxNarrowingMutants(t *testing.T) {
	for _, row := range []struct{ name, test, diagnostic string }{
		{"timeout-generic-refusal", "TestAuxTimeoutHasDistinctTypedRefusalAndNoPrimaryStart", "timeout classification or primary refusal lost"},
		{"process-argv", "TestAuxAdmissionNegatives/aux-argv", "aux-argv admitted"},
		{"timeout-policy", "TestAuxAdmissionNegatives/timeout-policy", "timeout-policy admitted"},
		{"raw-version", "TestAuxAdmissionNegatives/raw-version", "raw-version admitted"},
		{"admitted-home", "TestAuxBuildRefusesChangedAdmission/home", "changed home admission accepted"},
		{"unknown-role", "TestAuxUnknownRoleRefuses", "unknown auxiliary admitted"},
		{"changed-binary", "TestAuxBinarySwappedBeforePrimaryRefuses", "swapped binary admitted"},
		{"timeout", "TestAuxTimeoutYieldsNoPrimaryStart", "timeout started primary"},
	} {
		t.Run(row.name, func(t *testing.T) {
			// One bounded synchronous wait for the real Go test child; no polling.
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "test", "-mod=mod", ".", "-count=1", "-run", "^"+row.test+"$")
			cmd.Env = append(os.Environ(), "AGENTS_AUX_TEST_MUTANT="+row.name)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), row.diagnostic) {
				t.Fatalf("survivor or invalid run: %v\n%s", err, out)
			}
			t.Logf("mutant=%s test=%s expected-red exit=%d\n%s", row.name, row.test, exit.ExitCode(), out)
		})
	}
}
