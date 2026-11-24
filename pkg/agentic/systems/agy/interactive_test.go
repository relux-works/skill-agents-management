package agy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const (
	interactiveSourceURL     = "https://codelabs.developers.google.com/antigravity-cli-hands-on"
	interactiveSourceVersion = "1.1.27"
	interactiveSourceChecked = "2026-11-24"
	interactiveGoldenFile    = "testdata/interactive.json"
)

type interactiveGolden struct {
	Source struct {
		URL        string `json:"url"`
		Title      string `json:"title"`
		CLIVersion string `json:"cli_version"`
		Checked    string `json:"checked"`
	} `json:"source"`
	Binary     string   `json:"binary"`
	ModelID    string   `json:"model_id"`
	NativeArgs []string `json:"native_args"`
	Argv       []string `json:"argv"`
}

func loadInteractiveGolden(t *testing.T) interactiveGolden {
	t.Helper()
	data, err := os.ReadFile(interactiveGoldenFile)
	if err != nil {
		t.Fatalf("reading interactive golden: %v", err)
	}
	var golden interactiveGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("decoding interactive golden: %v", err)
	}
	if golden.Source.URL != interactiveSourceURL || golden.Source.Title != "Hands-on with Antigravity CLI" || golden.Source.CLIVersion != interactiveSourceVersion || golden.Source.Checked != interactiveSourceChecked {
		t.Fatalf("interactive golden source = %+v, want the recorded Google Codelab source", golden.Source)
	}
	return golden
}

func TestInteractiveArgvMatchesSourcedGolden(t *testing.T) {
	t.Parallel()
	golden := loadInteractiveGolden(t)
	req := launchRequest(t, "")
	req.Model.ID = golden.ModelID
	req.NativeArgs = append([]string(nil), golden.NativeArgs...)
	system := NewWithRuntime(Runtime{Executable: golden.Binary})

	plan := paritycase.BuildPlan(t, system, req, agentic.LaunchModeInteractive)
	if plan.Binary != golden.Binary {
		t.Errorf("Binary = %q, want resolved executable %q", plan.Binary, golden.Binary)
	}
	if !reflect.DeepEqual(plan.Argv, golden.Argv) {
		t.Fatalf("interactive argv = %#v, want sourced golden %#v", plan.Argv, golden.Argv)
	}
	if plan.ModelIdentity.Requested != golden.ModelID || plan.ModelIdentity.Launched != golden.ModelID {
		t.Errorf("model identity = %+v, want the same model-row ID requested and launched", plan.ModelIdentity)
	}
	for _, forbidden := range []string{"-p", "--print", "--output-format", "--print-timeout", "--mode", "--disable-slash-commands", "--add-dir"} {
		for _, arg := range plan.Argv {
			if arg == forbidden {
				t.Errorf("interactive argv includes headless flag %q: %v", forbidden, plan.Argv)
			}
		}
	}
}

func TestInteractiveNativeTailRefusesModelAndHeadlessOverrides(t *testing.T) {
	t.Parallel()
	system := NewWithRuntime(Runtime{Executable: "/preflighted/agy"})
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "model override", args: []string{"--model", "other"}},
		{name: "model equals override", args: []string{"--model=other"}},
		{name: "short model override", args: []string{"-m", "other"}},
		{name: "headless short prompt", args: []string{"-p", "task"}},
		{name: "attached headless short prompt", args: []string{"-ptask"}},
		{name: "positional tail after separator", args: []string{"--", "task"}},
		{name: "headless switch after separator", args: []string{"--", "-p", "task"}},
		{name: "prompt flag", args: []string{"--prompt", "task"}},
		{name: "print flag", args: []string{"--print", "task"}},
		{name: "output format flag", args: []string{"--output-format", "json"}},
		{name: "print timeout flag", args: []string{"--print-timeout", "30m"}},
		{name: "exec mode flag", args: []string{"--mode", "accept-edits"}},
		{name: "exec slash-command flag", args: []string{"--disable-slash-commands"}},
		{name: "exec add-dir flag", args: []string{"--add-dir", "/tmp/work"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := launchRequest(t, "")
			req.NativeArgs = []string{"--dangerously-skip-permissions"}
			control, err := paritycase.TryBuildPlan(system, req, agentic.LaunchModeInteractive)
			if err != nil || !reflect.DeepEqual(control.Argv[len(control.Argv)-1:], req.NativeArgs) {
				t.Fatalf("documented native-tail positive control: plan=%+v err=%v", control, err)
			}

			req.NativeArgs = append([]string(nil), tc.args...)
			plan, err := paritycase.TryBuildPlan(system, req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrNativePolicyConflict) || !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("interactive native args %v: plan=%+v err=%v; want empty plan and ErrNativePolicyConflict", tc.args, plan, err)
			}
		})
	}
}

func TestInteractiveOmitsAbsentOptionalModel(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "")
	req.Model.ID = ""
	req.NativeArgs = []string{"--dangerously-skip-permissions"}
	argv, err := NewWithRuntime(Runtime{Executable: "/preflighted/agy"}).Argv(req, agentic.LaunchModeInteractive)
	want := []string{"--dangerously-skip-permissions"}
	if err != nil || !reflect.DeepEqual(argv, want) {
		t.Fatalf("interactive argv without model = %v, err=%v; want optional model omitted before native tail %v", argv, err, want)
	}
}

func TestInteractivePromptRefusalHasPositiveControl(t *testing.T) {
	t.Parallel()
	system := NewWithRuntime(Runtime{Executable: "/preflighted/agy"})
	control := launchRequest(t, "")
	controlPlan, err := paritycase.TryBuildPlan(system, control, agentic.LaunchModeInteractive)
	if err != nil || len(controlPlan.Argv) == 0 {
		t.Fatalf("prompt-free positive control: plan=%+v err=%v", controlPlan, err)
	}

	cases := []struct {
		name string
		edit func(*agentic.LaunchRequest)
	}{
		{
			name: "prompt bytes",
			edit: func(req *agentic.LaunchRequest) { req.Prompt = []byte("assignment") },
		},
		{
			name: "prompt path",
			edit: func(req *agentic.LaunchRequest) {
				req.PromptPath = paritycase.WritePromptFile(t, req.WorkDir, "assignment")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := control
			tc.edit(&req)
			argv, directErr := system.Argv(req, agentic.LaunchModeInteractive)
			if !errors.Is(directErr, agentic.ErrParameterNotInteractive) || len(argv) != 0 {
				t.Fatalf("direct interactive argv with prompt = %v, err=%v; want no argv and ErrParameterNotInteractive", argv, directErr)
			}
			plan, err := paritycase.TryBuildPlan(system, req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrParameterNotInteractive) || !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("prompt-bearing interactive request: plan=%+v err=%v; want empty plan and ErrParameterNotInteractive", plan, err)
			}
		})
	}
}

func TestInteractiveWithoutResolvedRuntimeRefusesWithPositiveControl(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "")
	control, err := paritycase.TryBuildPlan(NewWithRuntime(Runtime{Executable: "/preflighted/agy"}), req, agentic.LaunchModeInteractive)
	if err != nil || control.Binary != "/preflighted/agy" {
		t.Fatalf("resolved-runtime positive control: plan=%+v err=%v", control, err)
	}

	plan, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeInteractive)
	if !errors.Is(err, ErrRuntimeNotPreflighted) || !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("interactive request without preflight: plan=%+v err=%v; want empty plan and ErrRuntimeNotPreflighted", plan, err)
	}
}

func TestNativeArgsRemainRefusedOutsideInteractiveMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode   agentic.LaunchMode
		prompt string
	}{
		{mode: agentic.LaunchModeExec, prompt: "assignment"},
		{mode: agentic.LaunchModeDryRun},
	} {
		t.Run(tc.mode.String(), func(t *testing.T) {
			var system *System
			if tc.mode == agentic.LaunchModeExec {
				system = NewWithRuntime(Runtime{Executable: "/preflighted/agy"})
			} else {
				system = New()
			}
			req := launchRequest(t, tc.prompt)
			control, err := paritycase.TryBuildPlan(system, req, tc.mode)
			if err != nil || len(control.Argv) == 0 {
				t.Fatalf("native-argument-free positive control: plan=%+v err=%v", control, err)
			}

			req.NativeArgs = []string{"--dangerously-skip-permissions"}
			plan, err := paritycase.TryBuildPlan(system, req, tc.mode)
			if !errors.Is(err, agentic.ErrNativeArgsNotInteractive) || !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("native args in %s: plan=%+v err=%v; want empty plan and ErrNativeArgsNotInteractive", tc.mode, plan, err)
			}
		})
	}
}

func TestInteractiveUsesSameResolvedRuntimeAndEnvironmentAsExec(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	system := NewWithRuntime(Runtime{Executable: "/preflighted/agy"})
	execPlan := paritycase.BuildPlan(t, system, req, agentic.LaunchModeExec)

	interactiveReq := req
	interactiveReq.PromptPath = ""
	interactiveReq.Prompt = nil
	interactiveReq.NativeArgs = []string{"--dangerously-skip-permissions"}
	interactivePlan := paritycase.BuildPlan(t, system, interactiveReq, agentic.LaunchModeInteractive)
	if interactivePlan.Binary != execPlan.Binary {
		t.Errorf("interactive binary %q differs from exec binary %q", interactivePlan.Binary, execPlan.Binary)
	}
	if !reflect.DeepEqual(interactivePlan.Env, execPlan.Env) {
		t.Errorf("interactive environment differs from exec environment:\n  interactive: %v\n  exec:        %v", interactivePlan.Env, execPlan.Env)
	}
	if interactivePlan.WorkDir != execPlan.WorkDir || interactivePlan.Home != execPlan.Home {
		t.Errorf("interactive target = (%q, %q), exec target = (%q, %q)", interactivePlan.WorkDir, interactivePlan.Home, execPlan.WorkDir, execPlan.Home)
	}
}

func TestExecAndDryRunGoldensRemainByteIdentical(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name string
		want string
	}{
		{name: "agy_exec.json", want: "826f6313463b486bdff84969bdb953a48df0d26ec8d8ef0e1895d2ba9469897c"},
		{name: "agy_dry-run.json", want: "a7f6e59cf29f573d64b351967fd05060d17a56a4ac592cd0a576aec7f294ca1d"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "parity", "testdata", "goldens", fixture.name)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			digest := sha256.Sum256(contents)
			got := hex.EncodeToString(digest[:])
			if got != fixture.want {
				t.Fatalf("%s SHA-256 = %s, want unchanged %s", fixture.name, got, fixture.want)
			}
		})
	}
}
