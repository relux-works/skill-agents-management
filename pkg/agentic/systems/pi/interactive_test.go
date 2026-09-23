package pi

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// LaunchModeInteractive (curator-spec Decision 0013 §5) for pi: the interactive
// primary session the agents-infra wrapper starts for any first argument that
// is not `spawn`, `turn` or `lifecycle`. There is no golden — pi has none for
// any mode; its exec surface is pinned in pi_test.go the same way — so the
// evidence is the exact argv, environment and stdin through the real registry
// and BuildPlan, plus the decision's negative: no Process-A `spawn` grammar and
// no exec-mode marker of any system on the interactive argv.

var execModeMarkers = []string{
	"spawn", "--profile", "--prompt", "--deadline", "--result-schema",
	"-p", "--print", "--mode",
	"exec", "--output-format",
	"--dangerously-skip-permissions",
	"--dangerously-bypass-approvals-and-sandbox",
	"--max-budget-usd",
}

func markersOn(argv []string) int {
	fired := 0
	for _, arg := range argv {
		for _, marker := range execModeMarkers {
			if arg == marker {
				fired++
			}
		}
	}
	return fired
}

func interactiveRequest(t *testing.T) (agentic.LaunchRequest, string) {
	t.Helper()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "agents-infra"))
	writeExecutable(t, filepath.Join(binDir, "pi"))
	return agentic.LaunchRequest{
		System:  New(&fakeStatusReader{}).ID(),
		Model:   agentic.Model{ID: "qwen-3.8-27b-mlx-8bit", Effort: agentic.EffortSupportNone},
		WorkDir: "/Users/op/project",
		Env:     []string{"PATH=" + binDir, "AGENTS_INFRA_CALLER_CWD=/Users/op/project"},
		Run:     agentic.RunContext{RunID: "RUN-pi-interactive", TaskID: "TASK-pi-interactive"},
		// local-models' Spawn always contributes the lease profile; the
		// interactive wrapper resolves its own from the project configuration,
		// so the request carries it and the argv must not.
		Profile: "local-qwen",
	}, binDir
}

func buildPlan(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.Plan, error) {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(New(&fakeStatusReader{})); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return agentic.BuildPlan(registry, req, mode)
}

func TestTheInteractiveArgvIsTheModelFlagOnTheWrapper(t *testing.T) {
	req, binDir := interactiveRequest(t)
	plan, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan(interactive): %v", err)
	}
	if want := []string{"pi", "--model", "qwen-3.8-27b-mlx-8bit"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
	if filepath.Base(plan.Binary) != "agents-infra" || filepath.Dir(plan.Binary) != binDir {
		t.Errorf("Binary = %q, want the agents-infra wrapper on the launch PATH, never raw pi", plan.Binary)
	}
	if plan.Stdin.Attached || len(plan.Stdin.Bytes) != 0 {
		t.Errorf("Stdin = %+v, want detached: EffortTransportNone puts nothing on stdin", plan.Stdin)
	}
	wantEnv := []string{
		"PATH=" + binDir, "AGENTS_INFRA_CALLER_CWD=/Users/op/project",
		agentic.EnvRunID + "=RUN-pi-interactive",
		agentic.EnvTaskID + "=TASK-pi-interactive",
	}
	if !reflect.DeepEqual(plan.Env, wantEnv) {
		t.Errorf("Env = %#v, want %#v; the caller CWD must pass through so the wrapper finds the project configuration", plan.Env, wantEnv)
	}
	if n := markersOn(plan.Argv); n != 0 {
		t.Errorf("the interactive argv carries %d exec-mode marker(s): %v", n, plan.Argv)
	}
	if plan.WorkDir != "/Users/op/project" {
		t.Errorf("WorkDir = %q, want the request's", plan.WorkDir)
	}
}

// The narrowing for the marker sweep: it fires on the exec grammar of the same
// system, so its silence above is a measurement.
func TestTheMarkerSweepFiresOnTheProcessAGrammar(t *testing.T) {
	req, _ := interactiveRequest(t)
	req.Prompt = []byte("turn")
	plan, err := buildPlan(t, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan(exec): %v", err)
	}
	if n := markersOn(plan.Argv); n < 4 {
		t.Fatalf("the sweep saw %d marker(s) on the exec argv %v; it has to see spawn, --profile, --prompt and --deadline there", n, plan.Argv)
	}
}

func TestAnInteractiveLaunchRefusesWhatItsGrammarCannotCarry(t *testing.T) {
	t.Run("a composition is refused with the decision's sentinel, before the grammar refusal", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.Composition = agentic.Composition{Prefix: []string{"--x"}}
		_, err := buildPlan(t, req, agentic.LaunchModeInteractive)
		if !errors.Is(err, agentic.ErrCompositionNotInteractive) {
			t.Fatalf("err = %v, want ErrCompositionNotInteractive; pi declares GrammarNone, and in this mode the mode's refusal comes first", err)
		}
	})
	t.Run("an effort word is refused under transport none", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.Effort = "high"
		if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrEffortNotTransportable) {
			t.Fatalf("err = %v, want ErrEffortNotTransportable", err)
		}
	})
	t.Run("a prompt is refused rather than spliced in", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.Prompt = []byte("turn")
		if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrParameterNotInteractive) {
			t.Fatalf("err = %v, want ErrParameterNotInteractive", err)
		}
	})
	t.Run("a missing model is refused by the core sentinel before the plugin sees it", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.Model.ID = ""
		if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrModelMissing) {
			t.Fatalf("err = %v, want ErrModelMissing", err)
		}
	})
	t.Run("a missing model is refused by the plugin itself", func(t *testing.T) {
		// The plugin's own refusal is kept as a second line of defence for
		// a caller holding the plugin directly, outside BuildPlan.
		if _, err := New(&fakeStatusReader{}).Argv(agentic.LaunchRequest{Profile: "local-qwen"}, agentic.LaunchModeInteractive); err == nil {
			t.Fatal("Argv built `pi --model` with an empty model id")
		}
	})
	t.Run("no profile is required in this mode", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.Profile = ""
		if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); err != nil {
			t.Fatalf("an interactive launch without a profile was refused: %v; the profile is the spawn subcommand's lease assertion, not this grammar's", err)
		}
	})
}

// Yolo is REFUSED for pi (curator-spec Decision 0018's pi row): pi 0.84.2
// documents no interactive permission-bypass flag, and --approve trusts
// project-local files rather than bypassing permissions. The refusal must be
// explicit — a yolo that silently launched native would be a session whose
// posture is the opposite of what was asked for.
func TestAnInteractiveYoloLaunchIsRefusedAsUnsupported(t *testing.T) {
	req, _ := interactiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "0.84.2"
	if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrPermissionModeUnsupported", err)
	}
	if _, err := New(&fakeStatusReader{}).Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("Argv err = %v, want ErrPermissionModeUnsupported", err)
	}
}

func TestPermissionModeNegativesAreNamed(t *testing.T) {
	t.Run("an unknown value is refused", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.PermissionMode = "bogus"
		if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnknown) {
			t.Fatalf("BuildPlan err = %v, want ErrPermissionModeUnknown", err)
		}
		if _, err := New(&fakeStatusReader{}).Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnknown) {
			t.Fatalf("Argv err = %v, want ErrPermissionModeUnknown", err)
		}
	})
	t.Run("yolo in exec mode is refused", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.PermissionMode = agentic.PermissionModeYolo
		req.Prompt = []byte("turn")
		if _, err := buildPlan(t, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrPermissionModeNotInteractive) {
			t.Fatalf("BuildPlan err = %v, want ErrPermissionModeNotInteractive", err)
		}
		if _, err := New(&fakeStatusReader{}).Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrPermissionModeNotInteractive) {
			t.Fatalf("Argv err = %v, want ErrPermissionModeNotInteractive", err)
		}
	})
	t.Run("an explicit native matches the zero value byte for byte", func(t *testing.T) {
		native, _ := interactiveRequest(t)
		native.PermissionMode = agentic.PermissionModeNative
		zero, _ := interactiveRequest(t)
		planNative, err := buildPlan(t, native, agentic.LaunchModeInteractive)
		if err != nil {
			t.Fatalf("BuildPlan(native): %v", err)
		}
		planZero, err := buildPlan(t, zero, agentic.LaunchModeInteractive)
		if err != nil {
			t.Fatalf("BuildPlan(zero): %v", err)
		}
		if want := []string{"pi", "--model", "qwen-3.8-27b-mlx-8bit"}; !reflect.DeepEqual(planNative.Argv, want) || !reflect.DeepEqual(planZero.Argv, want) {
			t.Errorf("native = %#v, zero = %#v, want both %#v", planNative.Argv, planZero.Argv, want)
		}
	})
}
