package pinative_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
)

// The Layer-1 contract of the native-Pi plugin, driven through the real
// agentic.BuildPlan on an isolated registry. There is no golden — native Pi
// never existed in the extraction source, so nothing captured one — and the
// evidence is the exact argv, binary, environment, stdin and home, plus the
// negatives: no bare model id under any request shape, no exec grammar, no
// preflight, no composition.

// execModeMarkers is the sweep from the design (§4): every headless flag of
// every system in this module, none of which may appear on a native-Pi argv.
var execModeMarkers = []string{
	"spawn", "--profile", "--prompt", "--deadline", "--result-schema",
	"-p", "--print", "--mode",
	"exec", "--output-format",
	"--dangerously-skip-permissions",
	"--dangerously-bypass-approvals-and-sandbox",
	"--max-budget-usd",
}

func markersOn(argv []string) []string {
	var found []string
	for _, arg := range argv {
		for _, marker := range execModeMarkers {
			if arg == marker {
				found = append(found, arg)
			}
		}
	}
	return found
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing fake executable %s: %v", path, err)
	}
}

// request is the reference launch: pi-anthropic × claude-opus-5 × high, the
// design's golden row. Only `pi` is on PATH — no agents-infra wrapper.
func request(t *testing.T) (agentic.LaunchRequest, string) {
	t.Helper()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "pi"))
	return agentic.LaunchRequest{
		System:  pinative.New().ID(),
		Model:   agentic.Model{ID: "claude-opus-5", Effort: agentic.EffortSupportRequired},
		Effort:  "high",
		WorkDir: "/Users/op/project",
		Env:     []string{"PATH=" + binDir, "PI_CODING_AGENT_DIR=/Users/op/.managed/pi"},
		Run:     agentic.RunContext{RunID: "RUN-pinative", TaskID: "TASK-pinative"},
		Runtime: "pi-anthropic",
		Vendor:  "anthropic",
	}, binDir
}

func buildPlan(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.Plan, error) {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(pinative.New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return agentic.BuildPlan(registry, req, mode)
}

func TestTheInteractiveArgvIsTheQualifiedModelAndThinking(t *testing.T) {
	req, binDir := request(t)
	plan, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan(interactive): %v", err)
	}
	if want := []string{"--model", "anthropic/claude-opus-5", "--thinking", "high"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
	if filepath.Base(plan.Binary) != "pi" || filepath.Dir(plan.Binary) != binDir {
		t.Errorf("Binary = %q, want raw pi on the launch PATH", plan.Binary)
	}
	if plan.Stdin.Attached || len(plan.Stdin.Bytes) != 0 {
		t.Errorf("Stdin = %#v, want detached", plan.Stdin)
	}
	if plan.Home != "" && plan.Home != "~/.pi/agent" {
		t.Errorf("Home = %q, want the declared default when the request carries none", plan.Home)
	}
	if found := markersOn(plan.Argv); len(found) != 0 {
		t.Errorf("the interactive argv carries exec-mode marker(s) %v", found)
	}
	if plan.WorkDir != req.WorkDir {
		t.Errorf("WorkDir = %q, want %q", plan.WorkDir, req.WorkDir)
	}
}

func TestTheDryRunMirrorsTheInteractiveLaunchExactly(t *testing.T) {
	req, _ := request(t)
	interactive, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan(interactive): %v", err)
	}
	dry, err := buildPlan(t, req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildPlan(dry-run): %v", err)
	}
	if !reflect.DeepEqual(dry.Argv, interactive.Argv) || dry.Binary != interactive.Binary || !reflect.DeepEqual(dry.Env, interactive.Env) || dry.Home != interactive.Home {
		t.Errorf("dry run differs from the launch it mirrors:\n dry=%#v\n int=%#v", dry, interactive)
	}
}

// TestTheArgvTransportIsActuallyCarried holds the Capabilities declaration
// (EffortTransportArgv) and args.go together: a request with an effort reaches
// `--thinking`, and a request without one carries no thinking flag at all.
func TestTheArgvTransportIsActuallyCarried(t *testing.T) {
	if got := pinative.New().Capabilities().EffortTransport; got != agentic.EffortTransportArgv {
		t.Fatalf("EffortTransport = %s, want argv", got)
	}
	req, _ := request(t)
	req.Model = agentic.Model{ID: "gemini-3.1-pro-preview", Effort: agentic.EffortSupportNone}
	req.Effort = ""
	req.Vendor = "google"
	plan, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan(effort-none): %v", err)
	}
	if want := []string{"--model", "google/gemini-3.1-pro-preview"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("effort-none Argv = %#v, want %#v", plan.Argv, want)
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeInteractive, agentic.LaunchModeDryRun} {
		req, _ := request(t)
		req.Effort = "xhigh"
		plan, err := buildPlan(t, req, mode)
		if err != nil {
			t.Fatalf("BuildPlan(%s): %v", mode, err)
		}
		if i := index(plan.Argv, "--thinking"); i < 0 || i+1 >= len(plan.Argv) || plan.Argv[i+1] != "xhigh" {
			t.Errorf("%s: the operator's effort did not reach --thinking: %v", mode, plan.Argv)
		}
	}
}

func index(argv []string, want string) int {
	for i, arg := range argv {
		if arg == want {
			return i
		}
	}
	return -1
}

// TestAnAliasLaunchesItsTargetWithTheVendorPrefix drives BuildPlan's one alias
// substitution: the requested spelling never reaches argv, the target does,
// qualified.
func TestAnAliasLaunchesItsTargetWithTheVendorPrefix(t *testing.T) {
	req, _ := request(t)
	req.Model = agentic.Model{ID: "astra", AliasOf: "gpt-6-astra", Effort: agentic.EffortSupportRequired}
	req.Vendor = "openai"
	plan, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan(alias): %v", err)
	}
	if want := []string{"--model", "openai/gpt-6-astra", "--thinking", "high"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
	if plan.ModelIdentity.Requested != "astra" || plan.ModelIdentity.Launched != "gpt-6-astra" {
		t.Errorf("ModelIdentity = %#v", plan.ModelIdentity)
	}
	for _, arg := range plan.Argv {
		if strings.HasSuffix(arg, "/astra") || arg == "astra" {
			t.Errorf("the alias spelling reached argv: %v", plan.Argv)
		}
	}
}

// TestAMissingVendorIsRefusedNeverDowngradedToABareID is the F2 narrowing: the
// gate that qualifies the identity, with its input removed, refuses rather
// than falls back. A plugin that emitted `--model claude-opus-5` here would
// hand Pi an id it resolves as ambiguous across providers.
func TestAMissingVendorIsRefusedNeverDowngradedToABareID(t *testing.T) {
	for _, vendor := range []string{"", "   "} {
		req, _ := request(t)
		req.Vendor = vendor
		for _, mode := range []agentic.LaunchMode{agentic.LaunchModeInteractive, agentic.LaunchModeDryRun} {
			plan, err := buildPlan(t, req, mode)
			if !errors.Is(err, pinative.ErrVendorMissing) {
				t.Fatalf("Vendor=%q %s: err = %v (argv %v), want ErrVendorMissing", vendor, mode, err, plan.Argv)
			}
		}
	}
	// The same refusal from the plugin held directly, so a caller bypassing
	// BuildPlan gets no bare id either.
	req, _ := request(t)
	req.Vendor = ""
	if argv, err := pinative.Args(req, agentic.LaunchModeInteractive); !errors.Is(err, pinative.ErrVendorMissing) {
		t.Fatalf("Args(no vendor) = %v, %v; want ErrVendorMissing", argv, err)
	}
}

// TestAModelAlreadyQualifiedIsRefused keeps qualification single: a model id
// carrying its own provider segment would otherwise become
// `anthropic/openai/x` or, worse, be passed through as somebody else's prefix.
func TestAModelAlreadyQualifiedOrABadVendorIsRefused(t *testing.T) {
	req, _ := request(t)
	req.Model.ID = "openai/claude-opus-5"
	if argv, err := pinative.Args(req, agentic.LaunchModeInteractive); err == nil {
		t.Fatalf("Args(pre-qualified model) = %v, want a refusal", argv)
	}
	req, _ = request(t)
	req.Vendor = "anthropic/extra"
	if argv, err := pinative.Args(req, agentic.LaunchModeInteractive); err == nil {
		t.Fatalf("Args(vendor with separator) = %v, want a refusal", argv)
	}
}

func TestNoArgvElementIsEverABareModelID(t *testing.T) {
	for _, shape := range []struct {
		name   string
		mutate func(*agentic.LaunchRequest)
	}{
		{"reference", func(*agentic.LaunchRequest) {}},
		{"effort none", func(r *agentic.LaunchRequest) { r.Model.Effort = agentic.EffortSupportNone; r.Effort = "" }},
		{"alias", func(r *agentic.LaunchRequest) { r.Model.AliasOf = "claude-opus-4-8" }},
		{"profile carried", func(r *agentic.LaunchRequest) { r.Profile = "some-profile" }},
	} {
		req, _ := request(t)
		shape.mutate(&req)
		for _, mode := range []agentic.LaunchMode{agentic.LaunchModeInteractive, agentic.LaunchModeDryRun} {
			plan, err := buildPlan(t, req, mode)
			if err != nil {
				t.Fatalf("%s/%s: %v", shape.name, mode, err)
			}
			for i, arg := range plan.Argv {
				if arg == req.Model.ID || arg == req.Model.LaunchIdentity() {
					t.Errorf("%s/%s: argv[%d] = %q is a bare model id: %v", shape.name, mode, i, arg, plan.Argv)
				}
				if i > 0 && plan.Argv[i-1] == "--model" && !strings.HasPrefix(arg, req.Vendor+"/") {
					t.Errorf("%s/%s: --model value %q lacks the %q/ prefix", shape.name, mode, arg, req.Vendor)
				}
			}
		}
	}
}

func TestExecIsNotDeclaredAndIsRefused(t *testing.T) {
	req, _ := request(t)
	req.Prompt = []byte("body")
	_, err := buildPlan(t, req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrUnsupportedLaunchMode) {
		t.Fatalf("BuildPlan(exec): err = %v, want ErrUnsupportedLaunchMode", err)
	}
	if _, err := pinative.Args(req, agentic.LaunchModeExec); err == nil {
		t.Fatal("Args(exec) built an argv for a mode this plugin does not declare")
	}
	if _, err := pinative.Args(req, agentic.LaunchModeManagedSession); err == nil {
		t.Fatal("Args(managed-session) built an argv for a mode this plugin does not declare")
	}
}

func TestACompositionIsRefusedOnTheInteractiveLaunch(t *testing.T) {
	req, _ := request(t)
	req.Composition = agentic.Composition{Prefix: []string{"--mcp", "x"}}
	if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrCompositionNotInteractive) {
		t.Fatalf("err = %v, want ErrCompositionNotInteractive", err)
	}
	if err := pinative.New().ValidateComposition(req.Composition); err == nil {
		t.Fatal("ValidateComposition admitted a composition; this system declares no grammar")
	}
	if err := pinative.New().ValidateComposition(agentic.Composition{}); err != nil {
		t.Fatalf("ValidateComposition(zero) = %v", err)
	}
}

// TestNoPreflightIsWiredAndAPlanBuildsWithoutTheWrapper is the "no
// agents-infra Preflightable" clause. The narrowing is the sibling assertion:
// the wrapper plugin IS Preflightable, so the type assertion discriminates.
func TestNoPreflightIsWiredAndAPlanBuildsWithoutTheWrapper(t *testing.T) {
	var system agentic.System = pinative.New()
	if _, ok := system.(agentic.Preflightable); ok {
		t.Fatal("pi-native implements Preflightable; the native plan must never probe agents-infra")
	}
	var wrapper agentic.System = pi.New(nil)
	if _, ok := wrapper.(agentic.Preflightable); !ok {
		t.Fatal("fixture assumption broken: the wrapper pi plugin must be Preflightable for this assertion to discriminate")
	}
	req, binDir := request(t)
	if _, err := os.Stat(filepath.Join(binDir, "agents-infra")); !os.IsNotExist(err) {
		t.Fatalf("fixture assumption broken: agents-infra must be absent from the launch PATH (%v)", err)
	}
	if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); err != nil {
		t.Fatalf("a native plan needs no wrapper on PATH, got %v", err)
	}
}

func TestTheSweepFiresOnTheWrapperExecArgv(t *testing.T) {
	// The marker sweep's silence on pi-native means nothing unless it sees
	// the wrapper's headless grammar on the sibling plugin.
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "agents-infra"))
	req := agentic.LaunchRequest{
		System:  pi.New(nil).ID(),
		Model:   agentic.Model{ID: "qwen-3.8-27b-mlx-8bit", Effort: agentic.EffortSupportNone},
		Env:     []string{"PATH=" + binDir},
		Profile: "local-qwen",
	}
	argv, err := pi.New(nil).Argv(req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("wrapper dry-run argv: %v", err)
	}
	if found := markersOn(argv); len(found) < 2 {
		t.Fatalf("the sweep saw %v on the wrapper argv %v; it has to see the headless grammar there", found, argv)
	}
}

func TestTheManagedHomeVariableReachesTheChildVerbatimAndTheHomeIsReported(t *testing.T) {
	req, _ := request(t)
	req.Home = "/Users/op/.managed/pi"
	plan, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Home != "/Users/op/.managed/pi" {
		t.Errorf("Home = %q, want the request's managed home", plan.Home)
	}
	found := false
	for _, entry := range plan.Env {
		if entry == "PI_CODING_AGENT_DIR=/Users/op/.managed/pi" {
			found = true
		}
	}
	if !found {
		t.Errorf("PI_CODING_AGENT_DIR did not reach the child verbatim: %v", plan.Env)
	}
	for _, key := range []string{"TASK_BOARD_RUN_ID=RUN-pinative", "TASK_BOARD_TASK_ID=TASK-pinative"} {
		if index(plan.Env, key) < 0 {
			t.Errorf("run context %q missing from the child environment %v", key, plan.Env)
		}
	}
	caps := pinative.New().Capabilities()
	if caps.HomeEnvVar != "PI_CODING_AGENT_DIR" || caps.DefaultHome != "~/.pi/agent" {
		t.Errorf("home rule = (%q, %q), want (PI_CODING_AGENT_DIR, ~/.pi/agent)", caps.HomeEnvVar, caps.DefaultHome)
	}
}

func TestNoPATHIsARefusalNotAFallback(t *testing.T) {
	req, _ := request(t)
	req.Env = []string{"PI_CODING_AGENT_DIR=/x"}
	_, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if !errors.Is(err, pinative.ErrNoPathInLaunchEnvironment) {
		t.Fatalf("err = %v, want ErrNoPathInLaunchEnvironment", err)
	}
}

func TestAnEmptyModelIsRefusedBeforeArgv(t *testing.T) {
	req, _ := request(t)
	req.Model.ID = ""
	if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrModelMissing) {
		t.Fatalf("err = %v, want ErrModelMissing", err)
	}
}
