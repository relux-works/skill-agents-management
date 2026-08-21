package agentic

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// pangolinID names an agentic system that exists nowhere else in this module:
// not in the registry, not in the CLI, not in any non-test source.
// TestTestDoubleExistsOnlyInTests proves that rather than asserting it.
//
// It is the descendant of the source repository's axolotl adapter, which
// proved the same property about the table this contract replaces: registering
// a system the core has never heard of must drive every dispatch surface with
// no edit anywhere except the registration call.
const pangolinID SystemID = "pangolin"

// pangolinSystem is the one test double in this package. Its fields configure
// what it declares and what each surface answers, so a refusal test builds a
// variant of the same double rather than introducing a second fake whose drift
// from this one nobody would notice.
type pangolinSystem struct {
	id    SystemID
	caps  Capabilities
	calls map[string]int

	binary        string
	binaryErr     error
	argvErr       error
	envErr        error
	stdinErr      error
	compositionOK bool

	// emptyBinary, detachedStdinBytes and unstableID make the double break the
	// plugin contract on purpose, so the contract refusals — BuildPlan's, and
	// Register's — have something real to refuse.
	emptyBinary        bool
	detachedStdinBytes bool

	// unstableID, when set, makes ID() answer p.id once and this value on
	// every later call: a plugin that violates System.ID's stability rule
	// exactly the way an accident would, rather than one that never agrees
	// with itself. Registry.Register reads ID() twice and must refuse it.
	unstableID SystemID
}

func newPangolin() *pangolinSystem {
	return &pangolinSystem{
		id: pangolinID,
		caps: Capabilities{
			LaunchModes:         []LaunchMode{LaunchModeExec, LaunchModeDryRun, LaunchModeManagedSession},
			EffortTransport:     EffortTransportArgv,
			SupportsGoal:        true,
			SupportsBudget:      true,
			SupportsServiceTier: true,
			CompositionGrammar:  GrammarID("pangolin-json"),
			HomeEnvVar:          "PANGOLIN_HOME",
			DefaultHome:         "~/.pangolin",
			AuthHint:            "run `pangolin login` and retry",
		},
		calls:         map[string]int{},
		binary:        "/opt/pangolin/bin/pangolin",
		compositionOK: true,
	}
}

func (p *pangolinSystem) record(surface string) { p.calls[surface]++ }

func (p *pangolinSystem) ID() SystemID {
	p.record("ID")
	if p.unstableID != "" && p.calls["ID"] > 1 {
		return p.unstableID
	}
	return p.id
}

func (p *pangolinSystem) Capabilities() Capabilities {
	p.record("Capabilities")
	return p.caps
}

func (p *pangolinSystem) ResolveBinary(LaunchRequest) (string, error) {
	p.record("ResolveBinary")
	if p.binaryErr != nil {
		return "", p.binaryErr
	}
	if p.emptyBinary {
		return "   ", nil
	}
	return p.binary, nil
}

func (p *pangolinSystem) Argv(req LaunchRequest, mode LaunchMode) ([]string, error) {
	p.record("Argv")
	if p.argvErr != nil {
		return nil, p.argvErr
	}
	argv := []string{"--mode", mode.String(), "--model", req.Model.ID}
	if effort := strings.TrimSpace(req.Effort); effort != "" {
		argv = append(argv, "--effort", effort)
	}
	if req.ServiceTier != "" {
		argv = append(argv, "--tier", req.ServiceTier)
	}
	if req.Budget != nil {
		argv = append(argv, "--budget-usd", fmt.Sprintf("%.2f", req.Budget.USD))
	}
	if req.Goal != nil {
		argv = append(argv, "--goal", req.Goal.ID)
	}
	argv = append(argv, req.Composition.Prefix...)
	switch mode {
	case LaunchModeDryRun:
		argv = append(argv, "<assignment-prompt-file>")
	default:
		argv = append(argv, req.PromptPath)
	}
	return argv, nil
}

func (p *pangolinSystem) ChildEnv(parent []string, req LaunchRequest) ([]string, error) {
	p.record("ChildEnv")
	if p.envErr != nil {
		return nil, p.envErr
	}
	child := make([]string, 0, len(parent)+1)
	for _, entry := range parent {
		// The parent's own harness state is exactly what a child must not
		// inherit; this is the double's stand-in for filterCodexRuntimeEnv.
		if strings.HasPrefix(entry, "PANGOLIN_SESSION=") {
			continue
		}
		child = append(child, entry)
	}
	home := req.Home
	if home == "" {
		home = p.caps.DefaultHome
	}
	return append(child, p.caps.HomeEnvVar+"="+home), nil
}

func (p *pangolinSystem) Stdin(req LaunchRequest) (StdinPayload, error) {
	p.record("Stdin")
	if p.stdinErr != nil {
		return StdinPayload{}, p.stdinErr
	}
	if p.detachedStdinBytes {
		return StdinPayload{Attached: false, Bytes: []byte("orphaned")}, nil
	}
	return StdinPayload{Attached: true, Bytes: req.Prompt}, nil
}

func (p *pangolinSystem) ValidateComposition(c Composition) error {
	p.record("ValidateComposition")
	if !p.compositionOK {
		return errors.New("pangolin composition prefix is not pangolin-json")
	}
	if len(c.Prefix) == 0 {
		return errors.New("pangolin requires a composition prefix when servers are declared")
	}
	return nil
}

// pangolinRequest is a fully populated launch request, so a test that wants to
// exercise one refusal starts from a request that would otherwise succeed.
func pangolinRequest() LaunchRequest {
	return LaunchRequest{
		System:      pangolinID,
		Model:       Model{ID: "pangolin-large", Effort: EffortSupportRequired},
		Effort:      "high",
		PromptPath:  "/tmp/assignment.md",
		Prompt:      []byte("do the thing"),
		WorkDir:     "/work/story",
		Env:         []string{"PATH=/usr/bin", "PANGOLIN_SESSION=parent-123", "HOME=/home/agent"},
		Goal:        &Goal{ID: "GOAL-1", Objective: "ship the contract", Revision: 3},
		Budget:      &Budget{USD: 12.5},
		ServiceTier: "priority",
		Composition: Composition{
			Prefix:  []string{"--pangolin-mcp", `{"servers":{"jira":{"url":"https://jira.example/mcp"}}}`},
			Servers: []CompositionServer{{Name: "jira", Transport: "http"}},
		},
	}
}

// registerPangolin is THE one edit. Nothing else in this package, the CLI, or
// any production source knows the system exists.
func registerPangolin(t *testing.T, sys *pangolinSystem) *Registry {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	return registry
}

// TestRegisteringOneSystemDrivesEveryDispatchSurface is the seam proof.
//
// The set of surfaces is read off the System interface by reflection rather
// than written out by hand: a method added to the contract and not driven from
// BuildPlan fails this test on the next run, which is the only version of
// "every dispatch surface" that survives the contract growing.
func TestRegisteringOneSystemDrivesEveryDispatchSurface(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)

	for _, mode := range sys.caps.LaunchModes {
		if _, err := BuildPlan(registry, pangolinRequest(), mode); err != nil {
			t.Fatalf("BuildPlan(pangolin, %s): %v", mode, err)
		}
	}

	contract := reflect.TypeOf((*System)(nil)).Elem()
	var undriven []string
	for i := 0; i < contract.NumMethod(); i++ {
		name := contract.Method(i).Name
		if sys.calls[name] == 0 {
			undriven = append(undriven, name)
		}
	}
	sort.Strings(undriven)
	if len(undriven) > 0 {
		t.Fatalf("registering a system did not drive %v; those surfaces dispatch through something other than the registry, or through nothing at all", undriven)
	}
}

// TestPlanCarriesTheDoublesObservableLaunchSurface pins the four things
// invariant 1 of docs/architecture.md calls the parity bar — binary, argv,
// environment, stdin — to what the plugin produced, so a refactor that changes
// any of them cannot pass by producing an argv that merely reads the same.
func TestPlanCarriesTheDoublesObservableLaunchSurface(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)

	plan, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Binary != "/opt/pangolin/bin/pangolin" {
		t.Errorf("Binary = %q, want the plugin's resolved executable", plan.Binary)
	}
	wantArgv := []string{
		"--mode", "exec", "--model", "pangolin-large", "--effort", "high",
		"--tier", "priority", "--budget-usd", "12.50", "--goal", "GOAL-1",
		"--pangolin-mcp", `{"servers":{"jira":{"url":"https://jira.example/mcp"}}}`,
		"/tmp/assignment.md",
	}
	if fmt.Sprint(plan.Argv) != fmt.Sprint(wantArgv) {
		t.Errorf("Argv  = %#v\nwant  = %#v", plan.Argv, wantArgv)
	}
	for _, entry := range plan.Env {
		if strings.HasPrefix(entry, "PANGOLIN_SESSION=") {
			t.Errorf("child env inherited the parent's harness session state: %q", entry)
		}
	}
	if !containsEnv(plan.Env, "PANGOLIN_HOME=~/.pangolin") {
		t.Errorf("child env = %v, want it to carry the declared default home", plan.Env)
	}
	if !plan.Stdin.Attached || string(plan.Stdin.Bytes) != "do the thing" {
		t.Errorf("Stdin = %#v, want the prompt bytes attached", plan.Stdin)
	}
	if plan.WorkDir != "/work/story" {
		t.Errorf("WorkDir = %q, want the request's child working directory", plan.WorkDir)
	}
	if plan.Home != "~/.pangolin" {
		t.Errorf("Home = %q, want the declared default home", plan.Home)
	}
	if plan.System != pangolinID || plan.Mode != LaunchModeExec {
		t.Errorf("plan identity = (%s, %s), want (%s, exec)", plan.System, plan.Mode, pangolinID)
	}
}

// A dry run must mirror the real launch's binary rather than a display
// placeholder. This is the exact bug the source repository's BuildArgs
// carried: it printed a hardcoded name while the real launch ran a
// managed-npm or preflighted executable, so the reported command was not the
// command.
func TestDryRunMirrorsTheRealLaunchBinary(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)

	real, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan(exec): %v", err)
	}
	dry, err := BuildPlan(registry, pangolinRequest(), LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildPlan(dry-run): %v", err)
	}
	if dry.Binary != real.Binary {
		t.Errorf("dry-run binary = %q, real binary = %q; a dry run that names a different target is not a mirror", dry.Binary, real.Binary)
	}
	if fmt.Sprint(dry.Argv) == fmt.Sprint(real.Argv) {
		t.Errorf("dry-run argv is byte-identical to the real argv; the double was supposed to substitute a placeholder for the prompt file")
	}
}

// TestTestDoubleExistsOnlyInTests is the seam's other half: the double drives
// every surface AND the module contains no trace of it. If supporting it had
// required a second edit — a constant, a case, a table row — the id would
// appear in a non-test source and this fails.
func TestTestDoubleExistsOnlyInTests(t *testing.T) {
	for name, src := range moduleSources(t) {
		if strings.Contains(src, string(pangolinID)) {
			t.Errorf("%s mentions the test double %q; registering a system must touch nothing but the registration call", name, pangolinID)
		}
	}
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
