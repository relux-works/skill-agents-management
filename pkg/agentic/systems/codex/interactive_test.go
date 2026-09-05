package codex

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// LaunchModeInteractive (curator-spec Decision 0013 §5) has NO golden, for the
// same reason the managed-session mode has none: pkg/agentic/parity holds only
// surfaces the SOURCE repository's harness captured, and a golden captured next
// to the port would prove only that the plugin agrees with itself
// (parity/doc.go). Its evidence is here instead, in the shape args_test.go
// already uses for the uncaptured surface: the EXACT argv, environment and
// stdin through the real registry and BuildPlan, and the negative the decision
// requires — every exec-mode marker absent, with the sweep shown to fire on the
// exec argv of the same request.

// execModeMarkers are the tokens the decision names as forbidden on the
// interactive argv, codex's own included.
var execModeMarkers = []string{
	"exec",
	"--dangerously-bypass-approvals-and-sandbox",
	"--skip-git-repo-check",
	"--sandbox", "danger-full-access",
	"--ask-for-approval", "-a",
	"-p", "--profile",
	"-C", "--add-dir",
	"-",
	"--output-format",
	"--dangerously-skip-permissions",
	"--max-budget-usd",
}

func interactiveRequest(workDir string) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
		Effort:  parityEffort,
		WorkDir: workDir,
		Home:    workDir + "/.codex-home",
		Env:     []string{"HOME=/home/agent", "TERM=xterm-256color"},
		Run:     agentic.RunContext{RunID: parityRunID, TaskID: parityTaskID},
	}
}

func interactivePlan(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.Plan, string) {
	t.Helper()
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, "codex")
	req.Env = append(append([]string(nil), req.Env...), "PATH="+binDir)
	return buildParityPlan(t, New(), req, mode), binDir
}

func interactivePlanError(t *testing.T, req agentic.LaunchRequest) error {
	t.Helper()
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, "codex")
	req.Env = append(append([]string(nil), req.Env...), "PATH="+binDir)
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeInteractive)
	return err
}

func markersOn(argv []string) int {
	fired := 0
	for _, arg := range argv {
		for _, marker := range execModeMarkers {
			if arg == marker || strings.HasPrefix(arg, "service_tier=") {
				fired++
			}
		}
	}
	return fired
}

// TestTheInteractiveArgvIsModelAndEffortOnly pins the whole argv. Codex's flag
// ORDER is load-bearing — everything here is a top-level flag of the
// interactive invocation, and there is no subcommand for anything to follow.
func TestTheInteractiveArgvIsModelAndEffortOnly(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	plan, binDir := interactivePlan(t, req, agentic.LaunchModeInteractive)

	if want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
	if plan.Binary != binDir+"/codex" {
		t.Errorf("Binary = %q, want the stub on the launch PATH", plan.Binary)
	}
	if plan.Stdin.Attached || len(plan.Stdin.Bytes) != 0 {
		t.Errorf("Stdin = %+v, want nothing attached: there is no `-` marker and no prompt", plan.Stdin)
	}
	if plan.Home != req.Home || plan.WorkDir != workDir {
		t.Errorf("Home/WorkDir = %q/%q, want %q/%q", plan.Home, plan.WorkDir, req.Home, workDir)
	}

	// The environment contract is mode-independent: the run context is
	// written, and — with no tier admitted in this mode — no tier variable is.
	wantEnv := []string{
		"HOME=/home/agent", "TERM=xterm-256color", "PATH=" + binDir,
		agentic.EnvRunID + "=" + parityRunID,
		agentic.EnvTaskID + "=" + parityTaskID,
	}
	if !reflect.DeepEqual(plan.Env, wantEnv) {
		t.Errorf("Env = %#v, want %#v", plan.Env, wantEnv)
	}
	for _, entry := range plan.Env {
		if strings.HasPrefix(entry, ServiceTierEnv+"=") {
			t.Errorf("an interactive child was handed a service tier: %s", entry)
		}
	}
	if n := markersOn(plan.Argv); n != 0 {
		t.Errorf("the interactive argv carries %d exec-mode marker(s): %v", n, plan.Argv)
	}

	t.Run("and an effortless model carries the model flag alone", func(t *testing.T) {
		r := req
		r.Model.Effort = agentic.EffortSupportNone
		r.Effort = ""
		got, _ := interactivePlan(t, r, agentic.LaunchModeInteractive)
		if want := []string{"-m", parityModel}; !reflect.DeepEqual(got.Argv, want) {
			t.Errorf("Argv = %#v, want %#v", got.Argv, want)
		}
	})
}

// TestTheInteractiveArgvCarriesNoExecMarker is the decision's negative golden
// with its narrowing: the same sweep must fire on the exec argv, or its
// silence proves nothing.
func TestTheInteractiveArgvCarriesNoExecMarker(t *testing.T) {
	t.Parallel()
	interactive, err := Args(interactiveRequest("/tmp/project"), agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("Args(interactive): %v", err)
	}
	if n := markersOn(interactive); n != 0 {
		t.Errorf("the interactive argv carries %d exec-mode marker(s): %v", n, interactive)
	}
	exec, err := Args(parityRequest("/tmp/project"), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	if n := markersOn(exec); n < 5 {
		t.Fatalf("the marker sweep saw %d marker(s) on the exec argv %v; it has to see exec, the bypass flag, the git check, -C and the prompt marker there", n, exec)
	}
	managed, err := Args(parityRequest("/tmp/project"), agentic.LaunchModeManagedSession)
	if err != nil {
		t.Fatalf("Args(managed-session): %v", err)
	}
	if n := markersOn(managed); n < 3 {
		t.Fatalf("the marker sweep saw %d marker(s) on the managed-session fragment %v; it has to see the sandbox and approval policy there", n, managed)
	}
}

// TestAnInteractiveLaunchRefusesWhatItsGrammarCannotCarry covers the core
// refusals through BuildPlan and the two codex-specific ones — profile and
// tier — through the plugin held directly as well.
func TestAnInteractiveLaunchRefusesWhatItsGrammarCannotCarry(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)

	t.Run("a composition is refused with the decision's sentinel", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.Composition = agentic.Composition{
			Prefix:  []string{"-c", `mcp_servers.board.url="http://127.0.0.1:9/mcp"`},
			Servers: []agentic.CompositionServer{{Name: "board", Transport: "http"}},
		}
		if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrCompositionNotInteractive) {
			t.Fatalf("err = %v, want ErrCompositionNotInteractive", err)
		}
	})

	for _, c := range []struct {
		name   string
		mutate func(*agentic.LaunchRequest)
	}{
		{"goal", func(r *agentic.LaunchRequest) { r.Goal = &agentic.Goal{ID: "GOAL-1"} }},
		{"service tier", func(r *agentic.LaunchRequest) { r.ServiceTier = parityTier }},
		{"prompt path", func(r *agentic.LaunchRequest) { r.PromptPath = writePromptFile(t, workDir, "body") }},
		{"prompt bytes", func(r *agentic.LaunchRequest) { r.Prompt = []byte("body") }},
	} {
		t.Run(c.name+" is refused by BuildPlan", func(t *testing.T) {
			req := interactiveRequest(workDir)
			c.mutate(&req)
			if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrParameterNotInteractive) {
				t.Fatalf("err = %v, want ErrParameterNotInteractive", err)
			}
		})
	}

	t.Run("a profile is refused, and only here", func(t *testing.T) {
		// No core rule names a profile — it is codex's own fact — so this
		// refusal is the plugin's alone, and it has to reach BuildPlan's caller.
		req := interactiveRequest(workDir)
		req.Profile = parityProfile
		err := interactivePlanError(t, req)
		if err == nil {
			t.Fatal("an interactive launch carrying a profile was admitted; the profile reached no flag")
		}
		if !strings.Contains(err.Error(), parityProfile) {
			t.Errorf("refusal %q does not name the profile the operator has to remove", err)
		}
		if _, err := New().Argv(req, agentic.LaunchModeInteractive); err == nil {
			t.Error("Argv built an interactive argv for a request carrying a profile")
		}
	})

	t.Run("the plugin held directly refuses a tier itself", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.ServiceTier = parityTier
		if _, err := New().Argv(req, agentic.LaunchModeInteractive); err == nil {
			t.Error("Argv built an interactive argv for a request carrying a service tier")
		}
	})

	t.Run("a required effort is still required", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.Effort = ""
		if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrEffortMissing) {
			t.Fatalf("err = %v, want ErrEffortMissing", err)
		}
	})
}
