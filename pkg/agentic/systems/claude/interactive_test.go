package claude

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// LaunchModeInteractive (curator-spec Decision 0013 §5) has NO golden, and this
// file is why it does not: pkg/agentic/parity holds only surfaces the SOURCE
// repository's harness captured, and the source never had a constructed
// interactive argv — its interactive path filtered a human's argv. A golden
// captured by code living next to the port would prove only that the plugin
// agrees with itself, which parity/doc.go names as precisely the assurance a
// golden must not give. So the mode's evidence is here instead, in the same
// shape the other uncaptured surfaces use (args_test.go): the EXACT argv,
// environment and stdin, driven through the real registry and BuildPlan, plus
// the negative that the decision requires — every exec-mode marker absent.

// execModeMarkers are the tokens the decision names as forbidden on the
// interactive argv, claude's own included. Any one of them present means the
// plugin has spelled a headless, unrestricted or goal-bound launch where a
// human's terminal session was asked for.
var execModeMarkers = []string{
	"-p", "--print",
	"--output-format",
	"--dangerously-skip-permissions",
	"--permission-mode",
	"--max-budget-usd",
	"--append-system-prompt-file",
	goalDirectivePrefix,
	mcpConfigFlag,
	"exec",
	"--dangerously-bypass-approvals-and-sandbox",
}

func interactiveRequest(workDir string) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
		Effort:  parityEffort,
		WorkDir: workDir,
		Home:    workDir + "/.claude-home",
		Env:     []string{"HOME=/home/agent", "CLAUDECODE=1", "TERM=xterm-256color"},
		Run:     agentic.RunContext{RunID: parityPromptRunID, TaskID: parityPromptTaskID},
	}
}

func assertNoExecMarker(t *testing.T, argv []string) {
	t.Helper()
	for _, arg := range argv {
		for _, marker := range execModeMarkers {
			if arg == marker || strings.HasPrefix(arg, goalDirectivePrefix) {
				t.Errorf("the interactive argv carries the exec-mode marker %q: %v", marker, argv)
			}
		}
	}
}

// TestTheInteractiveArgvIsModelAndEffortOnly is the positive golden, pinned as
// a whole argv rather than as substrings: an argv with a flag appended passes
// a Contains sweep and is a different session.
func TestTheInteractiveArgvIsModelAndEffortOnly(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req.Env = append(req.Env, "PATH="+binDir)

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeInteractive)

	if want := []string{"--model", parityModel, "--effort", parityEffort}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
	if plan.Binary != binDir+"/"+executableName {
		t.Errorf("Binary = %q, want the stub on the launch PATH; the interactive session runs the same executable exec mode resolves", plan.Binary)
	}
	if plan.Stdin.Attached || len(plan.Stdin.Bytes) != 0 {
		t.Errorf("Stdin = %+v, want nothing attached: claude carries effort on argv, so nothing rides stdin", plan.Stdin)
	}
	if plan.Home != req.Home || plan.WorkDir != workDir {
		t.Errorf("Home/WorkDir = %q/%q, want the request's %q/%q", plan.Home, plan.WorkDir, req.Home, workDir)
	}

	// The environment contract is mode-independent: the parent session marker
	// is stripped and the run context is written, exactly as in exec mode.
	wantEnv := []string{
		"HOME=/home/agent", "TERM=xterm-256color", "PATH=" + binDir,
		agentic.EnvRunID + "=" + parityPromptRunID,
		agentic.EnvTaskID + "=" + parityPromptTaskID,
	}
	if !reflect.DeepEqual(plan.Env, wantEnv) {
		t.Errorf("Env = %#v, want %#v", plan.Env, wantEnv)
	}
	assertNoExecMarker(t, plan.Argv)

	t.Run("and an effortless model carries the model flag alone", func(t *testing.T) {
		r := req
		r.Model.Effort = agentic.EffortSupportNone
		r.Effort = ""
		got := buildParityPlan(t, New(), r, agentic.LaunchModeInteractive).Argv
		if want := []string{"--model", parityModel}; !reflect.DeepEqual(got, want) {
			t.Errorf("Argv = %#v, want %#v", got, want)
		}
	})
}

// TestTheInteractiveArgvCarriesNoExecMarker is the decision's negative golden,
// and the narrowing that makes it mean something: the SAME marker sweep run on
// the exec plan of the same request must fire, or a green result here is
// equally consistent with a sweep that sees nothing.
func TestTheInteractiveArgvCarriesNoExecMarker(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)

	assertNoExecMarker(t, argvFor(t, req, agentic.LaunchModeInteractive))

	exec := argvFor(t, req, agentic.LaunchModeExec)
	fired := 0
	for _, arg := range exec {
		for _, marker := range execModeMarkers {
			if arg == marker {
				fired++
			}
		}
	}
	if fired < 3 {
		t.Fatalf("the marker sweep saw %d marker(s) on the exec argv %v; it has to see -p, --output-format and --dangerously-skip-permissions there, or its silence on the interactive argv proves nothing", fired, exec)
	}
}

// TestAnInteractiveLaunchRefusesWhatItsGrammarCannotCarry drives each forbidden
// parameter through BuildPlan and, for the two claude repeats, through the
// plugin held directly.
func TestAnInteractiveLaunchRefusesWhatItsGrammarCannotCarry(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)

	t.Run("a composition is refused with the decision's sentinel", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.Composition = agentic.Composition{
			Prefix:  []string{mcpConfigFlag, `{"mcpServers":{"docs":{"type":"stdio","command":"docs-server"}}}`},
			Servers: []agentic.CompositionServer{{Name: "docs", Transport: "stdio"}},
		}
		if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrCompositionNotInteractive) {
			t.Fatalf("err = %v, want ErrCompositionNotInteractive", err)
		}
		// The same composition leads the exec argv, so the refusal is the
		// mode's rather than the grammar's.
		if args := argvFor(t, req, agentic.LaunchModeExec); args[0] != mcpConfigFlag {
			t.Errorf("the exec argv does not lead with the composition: %v", args)
		}
	})

	for _, c := range []struct {
		name   string
		mutate func(*agentic.LaunchRequest)
	}{
		{"goal", func(r *agentic.LaunchRequest) { r.Goal = parityGoal() }},
		{"budget", func(r *agentic.LaunchRequest) { r.Budget = &agentic.Budget{USD: 12.5} }},
		{"prompt path", func(r *agentic.LaunchRequest) { r.PromptPath = writePromptFile(t, workDir, parityPromptBody) }},
		{"prompt bytes", func(r *agentic.LaunchRequest) { r.Prompt = []byte(parityPromptBody) }},
	} {
		t.Run(c.name+" is refused by BuildPlan", func(t *testing.T) {
			req := interactiveRequest(workDir)
			c.mutate(&req)
			if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrParameterNotInteractive) {
				t.Fatalf("err = %v, want ErrParameterNotInteractive", err)
			}
		})
	}

	t.Run("the plugin held directly refuses a goal and a budget itself", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.Goal = parityGoal()
		if _, err := New().Argv(req, agentic.LaunchModeInteractive); err == nil {
			t.Error("Argv built an interactive argv for a goal-bound request")
		}
		req = interactiveRequest(workDir)
		req.Budget = &agentic.Budget{USD: 12.5}
		if _, err := New().Argv(req, agentic.LaunchModeInteractive); err == nil {
			t.Error("Argv built an interactive argv for a budget-bearing request")
		}
	})

	t.Run("a required effort is still required", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.Effort = ""
		if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrEffortMissing) {
			t.Fatalf("err = %v, want ErrEffortMissing; no default is injected in this mode either", err)
		}
	})
}
