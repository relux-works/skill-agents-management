package vendorplugin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// networkOrderSpySystem is the W1 regression spy (round-3 merged finding,
// repeat of the N1 class through the wrapper entry point). It wraps the
// pangolin double with every plugin surface buildLaunch can reach before
// BuildPlan — effort admission, request preparation, preflight — recording
// each invocation. buildLaunch must refuse a malformed or undeclared network
// carrier before any of them runs; the only permitted call is the Capabilities
// declaration read the admission decision needs.
//
// It follows the identitySystem embedding pattern from engine_launch_test.go:
// everything except the counted surfaces is the double above, so the spy
// cannot drift from it.
type networkOrderSpySystem struct {
	*pangolinSystem
	calls map[string]int

	prepareErr   error
	preflightErr error
	admitErr     error
}

var (
	_ agentic.LaunchRequestPreparer = (*networkOrderSpySystem)(nil)
	_ agentic.Preflightable         = (*networkOrderSpySystem)(nil)
	_ agentic.EffortAdmitter        = (*networkOrderSpySystem)(nil)
)

func (s *networkOrderSpySystem) record(surface string) { s.calls[surface]++ }

func (s *networkOrderSpySystem) ID() agentic.SystemID {
	s.record("ID")
	return s.pangolinSystem.ID()
}

func (s *networkOrderSpySystem) Capabilities() agentic.Capabilities {
	s.record("Capabilities")
	return s.pangolinSystem.Capabilities()
}

func (s *networkOrderSpySystem) ResolveBinary(req agentic.LaunchRequest) (string, error) {
	s.record("ResolveBinary")
	return s.pangolinSystem.ResolveBinary(req)
}

func (s *networkOrderSpySystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	s.record("Argv")
	return s.pangolinSystem.Argv(req, mode)
}

func (s *networkOrderSpySystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	s.record("ChildEnv")
	return s.pangolinSystem.ChildEnv(parent, req)
}

func (s *networkOrderSpySystem) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	s.record("Stdin")
	return s.pangolinSystem.Stdin(req)
}

func (s *networkOrderSpySystem) ValidateComposition(c agentic.Composition) error {
	s.record("ValidateComposition")
	return s.pangolinSystem.ValidateComposition(c)
}

func (s *networkOrderSpySystem) PrepareLaunchRequest(req agentic.LaunchRequest, _ agentic.LaunchMode) (agentic.LaunchRequestPreparation, error) {
	s.record("PrepareLaunchRequest")
	if s.prepareErr != nil {
		return agentic.LaunchRequestPreparation{}, s.prepareErr
	}
	return agentic.LaunchRequestPreparation{PromptPath: req.PromptPath, Prompt: append([]byte(nil), req.Prompt...)}, nil
}

func (s *networkOrderSpySystem) Preflight(_ context.Context, _ agentic.LaunchRequest) (agentic.PreflightEvidence, error) {
	s.record("Preflight")
	if s.preflightErr != nil {
		return agentic.PreflightEvidence{}, s.preflightErr
	}
	return agentic.PreflightEvidence{}, nil
}

func (s *networkOrderSpySystem) AdmitEffort(_, _, _ string, vocabulary []string) ([]string, error) {
	s.record("AdmitEffort")
	if s.admitErr != nil {
		return nil, s.admitErr
	}
	return vocabulary, nil
}

// newFailingNetworkOrderSpy builds the spy pair with every pre-plan gate
// failing: a vendor whose Spawn refuses, a system whose preparation, preflight
// and effort admission refuse, and no declared network adapter. Any of those
// failures reaching the caller instead of the typed network error is the W1
// mask, reproduced.
func newFailingNetworkOrderSpy() (*networkOrderSpySystem, *narwhalVendor) {
	sys := &networkOrderSpySystem{pangolinSystem: newPangolinSystem(), calls: map[string]int{}}
	sys.caps.NetworkAdapters = nil
	sys.prepareErr = errors.New("spy preparation refused")
	sys.preflightErr = errors.New("spy preflight refused")
	sys.admitErr = errors.New("spy effort refused")
	vendor := newNarwhal()
	vendor.spawnErr = errors.New("spy spawn refused")
	return sys, vendor
}

func registerNetworkOrderSpy(t *testing.T, vendor *narwhalVendor, sys *networkOrderSpySystem) *Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(sys); err != nil {
		t.Fatalf("Register(spy system): %v", err)
	}
	registry := NewRegistry(systems)
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register(spy vendor): %v", err)
	}
	if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
		t.Fatalf("DeclareRuntime(tusk): %v", err)
	}
	// Registration reads IDs, Models and Capabilities; the counts below
	// measure buildLaunch, not registration.
	sys.calls = map[string]int{}
	vendor.calls = map[string]int{}
	return registry
}

var wrapperDispatchSurfaces = []string{"ID", "Capabilities", "ResolveBinary", "Argv", "ChildEnv", "Stdin", "ValidateComposition"}

var wrapperOptionalSurfaces = []string{"PrepareLaunchRequest", "Preflight", "AdmitEffort"}

var wrapperVendorSurfaces = []string{"Spawn", "Models"}

func assertWrapperSpySilent(t *testing.T, sys *networkOrderSpySystem, vendor *narwhalVendor, wantCapabilities int) {
	t.Helper()
	for _, surface := range wrapperDispatchSurfaces {
		want := 0
		if surface == "Capabilities" {
			want = wantCapabilities
		}
		if sys.calls[surface] != want {
			t.Errorf("%s ran %d time(s), want %d; the network refusal must come before every wrapper dispatch", surface, sys.calls[surface], want)
		}
	}
	for _, surface := range wrapperOptionalSurfaces {
		if sys.calls[surface] != 0 {
			t.Errorf("%s ran %d time(s), want 0; the network refusal must come before every pre-plan gate", surface, sys.calls[surface])
		}
	}
	for _, surface := range wrapperVendorSurfaces {
		if vendor.calls[surface] != 0 {
			t.Errorf("vendor %s ran %d time(s), want 0; the network refusal must come before every vendor dispatch", surface, vendor.calls[surface])
		}
	}
	for surface, count := range vendor.calls {
		if count != 0 && surface != "Spawn" && surface != "Models" {
			t.Errorf("vendor %s ran %d time(s), want 0; no vendor surface may precede the gate", surface, count)
		}
	}
}

// TestBuildLaunchRefusesNetworkBeforeAnyWrapperCall is the W1 regression test
// (round-3 merged finding, repeat of the N1 class through the other production
// entry point): for each refusal class, at each wrapper entry point, the spy
// records no vendor dispatch, no effort admission, no request preparation, no
// preflight and no Layer-1 dispatch — and the result is the typed network
// error, never a preparer failure. The malformed carrier is refused before the
// first plugin invocation of any kind (zero calls total); the well-formed but
// undeclared carriers are refused right after the Capabilities declaration
// read, the single allowed invocation.
func TestBuildLaunchRefusesNetworkBeforeAnyWrapperCall(t *testing.T) {
	malformed := testNetwork()
	malformed.Patch.Unset = []string{""}
	museShaped := testNetwork()
	museShaped.Record.AdapterIdentity.Harness = "muse"
	museShaped.Record.AdapterIdentity.Build = ""

	cases := map[string]struct {
		network          agentic.Network
		engine           plugin.Ref
		wantErr          error
		wantCode         string
		wantCapabilities int
	}{
		"malformed": {
			network:          malformed,
			wantErr:          agentic.ErrNetworkProfileInvalid,
			wantCode:         refusal.CodeProfileInvalid,
			wantCapabilities: 0,
		},
		"undeclared": {
			network:          testNetwork(),
			wantErr:          agentic.ErrNetworkScopeUnsupported,
			wantCode:         refusal.CodeScopeUnsupported,
			wantCapabilities: 1,
		},
		"muse": {
			network:          museShaped,
			wantErr:          agentic.ErrNetworkScopeUnsupported,
			wantCode:         refusal.CodeScopeUnsupported,
			wantCapabilities: 1,
		},
		// The engine expectation never diverts the gate: engine resolution
		// and observation sit strictly downstream of the vendor dispatch and
		// preparation the spy pins at zero, so the typed network error proves
		// the gate precedes engine handling too. The zero-network contrast is
		// TestBuildLaunchEngineMismatchSurfacesWithoutANetworkScope.
		"undeclared with engine expectation": {
			network:          testNetwork(),
			engine:           plugin.Ref{ID: "unregistered-engine", Kind: inferenceengine.Kind},
			wantErr:          agentic.ErrNetworkScopeUnsupported,
			wantCode:         refusal.CodeScopeUnsupported,
			wantCapabilities: 1,
		},
	}
	builds := map[string]func(*testing.T, *Registry, SpawnRequest) (agentic.Plan, error){
		"BuildLaunch": func(t *testing.T, registry *Registry, req SpawnRequest) (agentic.Plan, error) {
			t.Helper()
			return BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
		},
		"BuildLaunchWithEnvironment": func(t *testing.T, registry *Registry, req SpawnRequest) (agentic.Plan, error) {
			t.Helper()
			result, err := BuildLaunchWithEnvironment(context.Background(), registry, req, agentic.LaunchModeExec)
			if err != nil && !reflect.DeepEqual(result, BuildLaunchResult{}) {
				t.Fatalf("BuildLaunchWithEnvironment error result = %#v, want zero; a refusal carries no plan", result)
			}
			return result.Plan, err
		},
	}
	for entryName, build := range builds {
		t.Run(entryName, func(t *testing.T) {
			for name, tc := range cases {
				t.Run(name, func(t *testing.T) {
					sys, vendor := newFailingNetworkOrderSpy()
					registry := registerNetworkOrderSpy(t, vendor, sys)
					req := narwhalRequest()
					req.Network = tc.network.Clone()
					req.Engine = tc.engine

					plan, err := build(t, registry, req)
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("%s err = %v, want %v", entryName, err, tc.wantErr)
					}
					if code, ok := refusal.CodeOf(err); !ok || code != tc.wantCode {
						t.Fatalf("%s err = %v, want typed %q", entryName, err, tc.wantCode)
					}
					if !reflect.DeepEqual(plan, agentic.Plan{}) {
						t.Fatalf("%s plan = %#v, want zero; a refusal carries no plan", entryName, plan)
					}
					assertWrapperSpySilent(t, sys, vendor, tc.wantCapabilities)
				})
			}
		})
	}
}

// TestBuildLaunchRefusesNetworkBeforeEngineObservation pins the gate ahead of
// the last pre-plan surface: an engine-bound launch whose observation adapter
// counts every call. The refused scope returns the typed network error with
// zero observations, zero vendor dispatches and zero preparation — observation
// never runs for a carrier the gate refuses.
//
// The early gates succeed here on purpose: observation sits between
// preparation and preflight, so with Spawn, admission and preparation passing,
// a mutant that moves the gate after observation runs exactly one extra
// surface (the adapter) and dies on its count. The fully-failing mask is
// covered by TestBuildLaunchRefusesNetworkBeforeAnyWrapperCall.
func TestBuildLaunchRefusesNetworkBeforeEngineObservation(t *testing.T) {
	sys, vendor := newFailingNetworkOrderSpy()
	vendor.spawnErr = nil
	sys.admitErr = nil
	sys.prepareErr = nil
	systems := agentic.NewRegistry()
	if err := systems.Register(sys); err != nil {
		t.Fatalf("Register(spy system): %v", err)
	}
	adapter := &scriptedEngineObservationAdapter{engine: testEngineRef}
	registry, err := NewRegistryWithEngineObservationAdapters(systems, adapter)
	if err != nil {
		t.Fatalf("NewRegistryWithEngineObservationAdapters: %v", err)
	}
	if err := registry.RegisterPlugin(inferenceengine.NewConfigured(testEngineRef.ID)); err != nil {
		t.Fatalf("RegisterPlugin(engine): %v", err)
	}
	vendor.models[0].Engine = testEngineRef
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register(spy vendor): %v", err)
	}
	declaration := tuskDeclaration()
	declaration.Engine = testEngineRef
	if err := registry.DeclareRuntime(declaration); err != nil {
		t.Fatalf("DeclareRuntime(tusk): %v", err)
	}
	sys.calls = map[string]int{}
	vendor.calls = map[string]int{}

	req := narwhalRequest()
	req.Engine = testEngineRef
	req.Network = testNetwork()

	plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("BuildLaunch err = %v, want ErrNetworkScopeUnsupported", err)
	}
	if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
		t.Fatalf("BuildLaunch err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
	}
	if !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("BuildLaunch plan = %#v, want zero; a refusal carries no plan", plan)
	}
	if adapter.calls != 0 {
		t.Errorf("ObserveEngine ran %d time(s), want 0; the network refusal must come before observation", adapter.calls)
	}
	assertWrapperSpySilent(t, sys, vendor, 1)
}

// TestBuildLaunchRefusesNetworkScopeForSystemOnlyMuse drives the wrapper entry
// point for the runtime AC 3 names: muse is system-only — no vendor exists to
// call Spawn — and the gate refuses the scope from the declaration alone,
// before the passthrough projection is ever built.
func TestBuildLaunchRefusesNetworkScopeForSystemOnlyMuse(t *testing.T) {
	systemDouble := newPangolinSystem()
	systemDouble.caps.NetworkAdapters = nil
	systems := agentic.NewRegistry()
	if err := systems.Register(&renamedPangolin{pangolinSystem: systemDouble, id: "muse"}); err != nil {
		t.Fatalf("Register(muse): %v", err)
	}
	registry := NewRegistry(systems)
	if err := SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("SeedFrozenRuntimes: %v", err)
	}

	req := museSpawnRequest()
	network := testNetwork()
	network.Record.AdapterIdentity.Harness = "muse"
	network.Record.AdapterIdentity.Build = ""
	req.Network = network

	plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	requireErrorIs(t, err, agentic.ErrNetworkScopeUnsupported, "BuildLaunch(muse, managed)")
	if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
		t.Fatalf("BuildLaunch(muse, managed) err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
	}
	if !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("BuildLaunch(muse, managed) plan = %#v, want zero; a refusal carries no plan", plan)
	}
}

// TestBuildLaunchNetworkSpyObservesEveryGateOnThePassPath is the control beside
// the refusal tests above: with zero Network the same spy (with succeeding
// gates) observes the vendor dispatch, effort admission, preparation,
// preflight and every Layer-1 surface — so the zero counts above are refusals,
// not a blind spy.
func TestBuildLaunchNetworkSpyObservesEveryGateOnThePassPath(t *testing.T) {
	sys := &networkOrderSpySystem{pangolinSystem: newPangolinSystem(), calls: map[string]int{}}
	sys.caps.NetworkAdapters = nil
	vendor := newNarwhal()
	registry := registerNetworkOrderSpy(t, vendor, sys)

	plan, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(zero Network): %v", err)
	}
	if _, ok := plan.NetworkProvenanceSnapshot(); ok {
		t.Fatal("zero Network produced network provenance on the spy path")
	}
	if vendor.calls["Spawn"] != 1 {
		t.Errorf("vendor Spawn ran %d time(s), want exactly 1 on the pass path", vendor.calls["Spawn"])
	}
	if vendor.calls["Models"] != 1 {
		t.Errorf("vendor Models ran %d time(s), want exactly 1 on the pass path", vendor.calls["Models"])
	}
	if sys.calls["AdmitEffort"] != 1 {
		t.Errorf("AdmitEffort ran %d time(s), want exactly 1 on the pass path", sys.calls["AdmitEffort"])
	}
	// Preparation runs twice by design: buildLaunch runs the pure pre-plan
	// gate before observation and preflight, and BuildPlan runs the same gate
	// so direct Layer-1 consumers cannot bypass it.
	if sys.calls["PrepareLaunchRequest"] != 2 {
		t.Errorf("PrepareLaunchRequest ran %d time(s), want exactly 2 on the pass path", sys.calls["PrepareLaunchRequest"])
	}
	if sys.calls["Preflight"] != 1 {
		t.Errorf("Preflight ran %d time(s), want exactly 1 on the pass path", sys.calls["Preflight"])
	}
	for _, surface := range []string{"Capabilities", "ResolveBinary", "Argv", "ChildEnv", "Stdin", "ValidateComposition"} {
		if sys.calls[surface] == 0 {
			t.Errorf("%s never ran on the pass path; the spy cannot prove ordering for a surface it never sees", surface)
		}
	}
	if !strings.Contains(strings.Join(plan.Env, "\n"), narwhalAuthEnv) {
		t.Errorf("spy plan.Env = %v, want the vendor's legitimate addition", plan.Env)
	}
}

// TestBuildLaunchFailingPreparerSurfacesWithoutANetworkScope proves the failing
// gates are real: with zero Network the preparer error — the exact failure
// that masked the network refusal before W1 — surfaces, so the typed network
// errors above prove ordering, not broken doubles.
func TestBuildLaunchFailingPreparerSurfacesWithoutANetworkScope(t *testing.T) {
	sys, vendor := newFailingNetworkOrderSpy()
	// The vendor and effort gates fire before preparation; clear them so the
	// run reaches the preparer whose mask W1 closed.
	vendor.spawnErr = nil
	sys.admitErr = nil
	registry := registerNetworkOrderSpy(t, vendor, sys)

	_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if err == nil {
		t.Fatal("BuildLaunch(failing preparer): no error at all; the gate admitted what it must reject")
	}
	if !strings.Contains(err.Error(), "before observation") {
		t.Fatalf("BuildLaunch(failing preparer) err = %v, want the preparer refusal", err)
	}
	if code, ok := refusal.CodeOf(err); ok {
		t.Fatalf("BuildLaunch(failing preparer) err carries network code %q on a zero-network run", code)
	}
	if sys.calls["PrepareLaunchRequest"] == 0 {
		t.Fatal("PrepareLaunchRequest never ran; the mask control proves nothing")
	}
}

// TestBuildLaunchEngineMismatchSurfacesWithoutANetworkScope is the contrast
// for the "undeclared with engine expectation" row: the same engine
// expectation with zero Network dies in engine resolution, so the typed
// network error there proves the gate precedes engine handling.
func TestBuildLaunchEngineMismatchSurfacesWithoutANetworkScope(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	req.Engine = plugin.Ref{ID: "unregistered-engine", Kind: inferenceengine.Kind}

	_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	requireErrorIs(t, err, ErrInferenceEngineMismatch, "BuildLaunch(engine expectation without a scope)")
}

// TestBuildLaunchNetworkGateDefersToResolutionForUnresolvableRuntimes pins the
// gate's fall-through: a malformed carrier is refused even for a runtime that
// does not resolve (shape needs no system), while a well-formed scope for one
// reports the resolution error — admission is undecidable without the system,
// and the normal path reports it exactly as it always has. The noted-malformed
// diagnostic distinguishes the fall-through from a gate that returned its own
// lookup error: the helper alone would say unknown runtime, while the normal
// path says malformed config.
func TestBuildLaunchNetworkGateDefersToResolutionForUnresolvableRuntimes(t *testing.T) {
	newNotedRegistry := func(t *testing.T) *Registry {
		t.Helper()
		registry := registerNarwhal(t, newNarwhal())
		if err := registry.NoteUnregistered("nosuch", RegistrationDiagnostic{
			Reason: "malformed",
			Err:    errors.New("spy local-models.toml is malformed"),
		}); err != nil {
			t.Fatalf("NoteUnregistered: %v", err)
		}
		return registry
	}

	t.Run("malformed scope refused despite unresolvable runtime", func(t *testing.T) {
		malformed := testNetwork()
		malformed.Patch.Unset = []string{""}
		req := narwhalRequest()
		req.Runtime = "nosuch"
		req.Network = malformed

		_, err := BuildLaunch(context.Background(), newNotedRegistry(t), req, agentic.LaunchModeExec)
		requireErrorIs(t, err, agentic.ErrNetworkProfileInvalid, "BuildLaunch(malformed, unresolvable runtime)")
		if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeProfileInvalid {
			t.Fatalf("BuildLaunch(malformed, unresolvable runtime) err = %v, want typed %q", err, refusal.CodeProfileInvalid)
		}
	})

	t.Run("well-formed scope reports the resolution error", func(t *testing.T) {
		req := narwhalRequest()
		req.Runtime = "nosuch"
		req.Network = testNetwork()

		_, err := BuildLaunch(context.Background(), newNotedRegistry(t), req, agentic.LaunchModeExec)
		requireErrorIs(t, err, ErrRuntimeConfigMalformed, "BuildLaunch(well-formed, unresolvable runtime)")
		if code, ok := refusal.CodeOf(err); ok {
			t.Fatalf("BuildLaunch(well-formed, unresolvable runtime) err carries network code %q, want the resolution error", code)
		}
	})
}
