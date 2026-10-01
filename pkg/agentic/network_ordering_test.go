package agentic

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/refusal"
)

// networkSpySystem wraps the pangolin double with every optional pre-plan
// gate, recording each invocation. BuildPlan must refuse a malformed or
// undeclared network carrier before any of these runs (round-2 merged finding
// N1: the gate used to sit after the optional dispatches, and a refused
// carrier reached the pi preparer once).
type networkSpySystem struct {
	*pangolinSystem
	optional map[string]int
}

var (
	_ CuratorContextValidator    = (*networkSpySystem)(nil)
	_ ContextDescriptorValidator = (*networkSpySystem)(nil)
	_ LaunchRequestPreparer      = (*networkSpySystem)(nil)
)

func (s *networkSpySystem) ValidateCuratorContext(LaunchRequest, LaunchMode) error {
	s.optional["ValidateCuratorContext"]++
	return nil
}

func (s *networkSpySystem) ValidateContextDescriptors(LaunchRequest, LaunchMode) error {
	s.optional["ValidateContextDescriptors"]++
	return nil
}

func (s *networkSpySystem) PrepareLaunchRequest(req LaunchRequest, _ LaunchMode) (LaunchRequestPreparation, error) {
	s.optional["PrepareLaunchRequest"]++
	return LaunchRequestPreparation{PromptPath: req.PromptPath, Prompt: append([]byte(nil), req.Prompt...)}, nil
}

func newNetworkSpy() *networkSpySystem {
	return &networkSpySystem{pangolinSystem: newPangolin(), optional: map[string]int{}}
}

// spyRequest arms every optional gate: a Context that passes the request-side
// fragment boundary plus one semantic descriptor, so on the pass path the spy
// observes all three optional invocations.
func spyRequest() LaunchRequest {
	req := pangolinRequest()
	req.Home = "/home/agent/.codex"
	req.Context = &CuratorContext{
		Revision:    CuratorLaunchFragmentV1,
		Environment: "codex_cli",
		Profile: CuratorProfilePin{
			Name:       "testprofile",
			LockSHA256: "1e5912de9e3459a12b7365f338d385c1c4c7b4a6b622598e17ed4004d7658699",
		},
		Precedence: CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env:        map[string]string{"CODEX_HOME": "/home/agent/.codex"},
	}
	req.ContextDescriptors = []ContextDescriptor{
		{Kind: ContextSystemPrompt, SystemPrompt: &SystemPromptContext{Text: "extra instructions"}},
	}
	return req
}

func registerNetworkSpy(t *testing.T, sys *networkSpySystem) *Registry {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register(spy): %v", err)
	}
	// Registration reads ID twice and Capabilities once; the counts below
	// measure BuildPlan, not registration.
	sys.calls = map[string]int{}
	sys.optional = map[string]int{}
	return registry
}

// dispatchSurfaces is every System method BuildPlan can invoke. Capabilities
// is included: the declaration read is a plugin invocation, and the malformed
// carrier must be refused before even it runs.
var dispatchSurfaces = []string{"ID", "Capabilities", "ResolveBinary", "Argv", "ChildEnv", "Stdin", "ValidateComposition"}

var optionalSurfaces = []string{"ValidateCuratorContext", "ValidateContextDescriptors", "PrepareLaunchRequest"}

func assertSpySilent(t *testing.T, sys *networkSpySystem, wantCapabilities int) {
	t.Helper()
	for _, surface := range dispatchSurfaces {
		want := 0
		if surface == "Capabilities" {
			want = wantCapabilities
		}
		if sys.calls[surface] != want {
			t.Errorf("%s ran %d time(s), want %d; the network refusal must come first", surface, sys.calls[surface], want)
		}
	}
	for _, surface := range optionalSurfaces {
		if sys.optional[surface] != 0 {
			t.Errorf("%s ran %d time(s), want 0; the network refusal must come before every optional gate", surface, sys.optional[surface])
		}
	}
}

// TestBuildPlanRefusesNetworkBeforeAnyPluginCall is the N1 regression test
// (round-2 merged finding): for each refusal class the spy records no
// dispatch and no optional gate. The malformed carrier is refused before the
// first plugin invocation of any kind (Capabilities included: zero calls
// total); the undeclared carriers are refused right after the Capabilities
// declaration read that states the admission — the single allowed invocation.
func TestBuildPlanRefusesNetworkBeforeAnyPluginCall(t *testing.T) {
	malformed := testNetwork()
	malformed.Patch.Unset = []string{""}
	museShaped := testNetwork()
	museShaped.Record.AdapterIdentity.Harness = "muse"
	museShaped.Record.AdapterIdentity.Build = ""

	cases := map[string]struct {
		network          Network
		clearAdapters    bool
		wantErr          error
		wantCode         string
		wantCapabilities int
	}{
		"malformed": {
			network:          malformed,
			wantErr:          ErrNetworkProfileInvalid,
			wantCode:         refusal.CodeProfileInvalid,
			wantCapabilities: 0,
		},
		"undeclared": {
			network:          testNetwork(),
			clearAdapters:    true,
			wantErr:          ErrNetworkScopeUnsupported,
			wantCode:         refusal.CodeScopeUnsupported,
			wantCapabilities: 1,
		},
		"muse": {
			network:          museShaped,
			clearAdapters:    true,
			wantErr:          ErrNetworkScopeUnsupported,
			wantCode:         refusal.CodeScopeUnsupported,
			wantCapabilities: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sys := newNetworkSpy()
			if tc.clearAdapters {
				sys.caps.NetworkAdapters = nil
			}
			registry := registerNetworkSpy(t, sys)
			req := spyRequest()
			req.Network = tc.network.Clone()

			_, err := BuildPlan(registry, req, LaunchModeExec)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("BuildPlan err = %v, want %v", err, tc.wantErr)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != tc.wantCode {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, tc.wantCode)
			}
			assertSpySilent(t, sys, tc.wantCapabilities)
		})
	}
}

// TestNetworkSpyObservesEveryGateOnThePassPath is the control beside the
// refusal test above: with zero Network the same spy observes every optional
// gate exactly once and every dispatch surface at least once. Without it the
// zero counts above could mean the spy is never consulted.
func TestNetworkSpyObservesEveryGateOnThePassPath(t *testing.T) {
	sys := newNetworkSpy()
	registry := registerNetworkSpy(t, sys)

	plan, err := BuildPlan(registry, spyRequest(), LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan(zero Network): %v", err)
	}
	if _, ok := plan.NetworkProvenanceSnapshot(); ok {
		t.Fatal("zero Network produced network provenance on the spy path")
	}
	for _, surface := range optionalSurfaces {
		if sys.optional[surface] != 1 {
			t.Errorf("%s ran %d time(s), want exactly 1 on the pass path", surface, sys.optional[surface])
		}
	}
	for _, surface := range []string{"Capabilities", "ResolveBinary", "Argv", "ChildEnv", "Stdin", "ValidateComposition"} {
		if sys.calls[surface] == 0 {
			t.Errorf("%s never ran on the pass path; the spy cannot prove ordering for a surface it never sees", surface)
		}
	}
	if !strings.Contains(strings.Join(plan.Env, "\n"), "PANGOLIN_HOME=") {
		t.Errorf("spy plan.Env = %v, want the double's owned home entry", plan.Env)
	}
}
