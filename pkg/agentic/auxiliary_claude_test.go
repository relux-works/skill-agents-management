package agentic_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

func TestClaudeAuxiliaryFixedProjections(t *testing.T) {
	primary := sealTestClaudePlan(t)
	plans, err := agentic.BuildAuxiliaryPlans(primary)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range [][]string{{"--version"}, {"-p", "--output-format", "json", "/goal"}} {
		p := plans[i]
		if !slices.Equal(p.Process.Argv, want) || p.Process.Binary != primary.Binary || p.Process.WorkDir != primary.WorkDir || p.Process.Home != primary.Home || !slices.Equal(p.Process.Env, primary.Env) || p.Process.Stdin.Attached || p.HardTimeout != []time.Duration{15 * time.Second, 60 * time.Second}[i] || !p.RequireNoSurvivors {
			t.Fatalf("bad projection: %#v", p)
		}
		if err := p.VerifyBeforeExec(); err != nil {
			t.Fatal(err)
		}
	}
	plans[0].Process.Env = append(plans[0].Process.Env, "EVIL=1")
	if slices.Contains(primary.Env, "EVIL=1") {
		t.Fatal("shared env")
	}
	if _, err := claude.New().AuxiliaryArgv("custom"); !errors.Is(err, agentic.ErrAuxRoleUnknown) {
		t.Fatalf("unknown role: %v", err)
	}
	if _, err := agentic.BuildAuxiliaryPlans(agentic.Plan{}); !errors.Is(err, agentic.ErrAuxRefused) {
		t.Fatalf("unadmitted primary: %v", err)
	}
}

func TestClaudeAuxiliaryFinalizedPrimary(t *testing.T) {
	primary, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{FragmentEnv: []string{"AUX_ADMITTED=1"}, NativeTail: []string{"--verbose"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := agentic.BuildAuxiliaryPlans(primary)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if !slices.Contains(p.Process.Env, "AUX_ADMITTED=1") || slices.Contains(p.Process.Argv, "--verbose") {
			t.Fatal("aux did not derive only admitted env and fixed argv")
		}
		if err := p.VerifyBeforeExec(); err != nil {
			t.Fatal(err)
		}
	}
	results := []agentic.AuxResult{{Role: agentic.ClaudeVersionProbe, Status: agentic.AuxSucceeded, NoSurvivors: true, Version: "2.1.288"}, {Role: agentic.ClaudeGoalProbe, Status: agentic.AuxSucceeded, NoSurvivors: true, GoalReady: true}}
	if err := agentic.VerifyPrimaryAfterAux(primary, plans, results); err != nil {
		t.Fatal(err)
	}
}

// Exercise refusal propagation through the exported API, including the source
// census that enumerates every new auxiliary refusal site.
func TestClaudeAuxiliaryAdmissionRefusals(t *testing.T) {
	for _, field := range []string{"argv", "home", "finalized-env", "unresolved-binary", "non-executable"} {
		t.Run(field, func(t *testing.T) {
			primary := sealTestClaudePlan(t)
			switch field {
			case "argv":
				primary.Argv = append(primary.Argv, "evil")
			case "home":
				primary.Home = "/other"
			case "finalized-env":
				var err error
				primary, err = agentic.FinalizePlan(primary, agentic.FinalizeOverlays{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				primary.Env = append(primary.Env, "EVIL=1")
			case "unresolved-binary":
				for _, env := range primary.Env {
					if strings.HasPrefix(env, "PATH=") {
						if err := os.Remove(filepath.Join(strings.TrimPrefix(env, "PATH="), "claude")); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "non-executable":
				for _, env := range primary.Env {
					if strings.HasPrefix(env, "PATH=") {
						if err := os.Chmod(filepath.Join(strings.TrimPrefix(env, "PATH="), "claude"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if _, err := agentic.BuildAuxiliaryPlans(primary); err == nil {
				t.Fatalf("%s admitted", field)
			}
		})
	}
}

func TestClaudeAuxiliaryPrimaryRefusals(t *testing.T) {
	for _, negative := range []string{"missing", "duplicate-result", "duplicate-plan", "timeout", "raw-version", "primary-env", "aux-env", "missing-binary"} {
		t.Run(negative, func(t *testing.T) {
			primary := sealTestClaudePlan(t)
			plans, err := agentic.BuildAuxiliaryPlans(primary)
			if err != nil {
				t.Fatal(err)
			}
			results := []agentic.AuxResult{{Role: agentic.ClaudeVersionProbe, Status: agentic.AuxSucceeded, NoSurvivors: true, Version: "2.1.288"}, {Role: agentic.ClaudeGoalProbe, Status: agentic.AuxSucceeded, NoSurvivors: true, GoalReady: true}}
			switch negative {
			case "missing":
				results = results[:1]
			case "duplicate-result":
				results[1] = results[0]
			case "duplicate-plan":
				plans[1] = plans[0]
			case "timeout":
				results[1].Status = agentic.AuxTimedOut
			case "raw-version":
				results[0].Version += "\nEVIL=1"
			case "primary-env":
				primary.Env = append(primary.Env, "EVIL=1")
			case "aux-env":
				plans[0].Process.Env = append(plans[0].Process.Env, "EVIL=1")
			case "missing-binary":
				for _, env := range primary.Env {
					if strings.HasPrefix(env, "PATH=") {
						if err := os.Remove(filepath.Join(strings.TrimPrefix(env, "PATH="), "claude")); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if err := agentic.VerifyPrimaryAfterAux(primary, plans, results); err == nil {
				t.Fatalf("%s admitted", negative)
			}
		})
	}
}

func TestPanelTimeoutHasDistinctTypedRefusal(t *testing.T) {
	p := sealTestClaudePlan(t)
	a, err := agentic.BuildAuxiliaryPlans(p)
	if err != nil {
		t.Fatal(err)
	}
	r := []agentic.AuxResult{{Role: agentic.ClaudeVersionProbe, Status: agentic.AuxSucceeded, NoSurvivors: true, Version: "2.1.288"}, {Role: agentic.ClaudeGoalProbe, Status: agentic.AuxTimedOut, NoSurvivors: true, GoalReady: true}}
	timeout := agentic.VerifyPrimaryAfterAux(p, a, r)
	r[1].Status = agentic.AuxFailed
	failed := agentic.VerifyPrimaryAfterAux(p, a, r)
	if timeout == nil {
		t.Fatal("timeout admitted")
	}
	if timeout == failed {
		t.Fatalf("timeout indistinguishable from failed: type=%T err=%v", timeout, timeout)
	}
}

func TestClaudeAuxiliaryTimeoutHasDistinctTypedRefusal(t *testing.T) {
	for _, index := range []int{0, 1} {
		t.Run(string([]agentic.AuxRole{agentic.ClaudeVersionProbe, agentic.ClaudeGoalProbe}[index]), func(t *testing.T) {
			primary := sealTestClaudePlan(t)
			plans, err := agentic.BuildAuxiliaryPlans(primary)
			if err != nil {
				t.Fatal(err)
			}
			results := []agentic.AuxResult{{Role: agentic.ClaudeVersionProbe, Status: agentic.AuxSucceeded, NoSurvivors: true, Version: "2.1.288"}, {Role: agentic.ClaudeGoalProbe, Status: agentic.AuxSucceeded, NoSurvivors: true, GoalReady: true}}
			results[index].Status = agentic.AuxTimedOut
			err = agentic.VerifyPrimaryAfterAux(primary, plans, results)
			var timeout *agentic.AuxTimeoutError
			if err == nil || !errors.As(err, &timeout) || timeout.Role != results[index].Role || !errors.Is(err, agentic.ErrAuxRefused) {
				t.Fatalf("timeout classification or primary refusal lost: %v", err)
			}
			results[index].Status = agentic.AuxFailed
			err = agentic.VerifyPrimaryAfterAux(primary, plans, results)
			if !errors.Is(err, agentic.ErrAuxRefused) || errors.As(err, &timeout) {
				t.Fatalf("failure misclassified or admitted: %v", err)
			}
		})
	}
}
