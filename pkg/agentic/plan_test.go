package agentic

import (
	"errors"
	"strings"
	"testing"
)

// Every test in this file is a negative one. BuildPlan's whole job beyond
// dispatch is refusing launches the system it dispatched to could not honour,
// and a gate is worth exactly what it rejects. The positive path lives in
// double_test.go, where it proves the gate is reachable — which is a different
// claim.

func TestBuildPlanRefusesUnknownSystem(t *testing.T) {
	registry := registerPangolin(t, newPangolin())
	req := pangolinRequest()
	req.System = "opencode"

	_, err := BuildPlan(registry, req, LaunchModeExec)
	if !errors.Is(err, ErrUnknownSystem) {
		t.Fatalf("err = %v, want ErrUnknownSystem", err)
	}
	if !strings.Contains(err.Error(), "opencode") {
		t.Errorf("refusal %q does not name the system that was asked for", err)
	}
}

func TestBuildPlanRefusesUnnormalizableSystemID(t *testing.T) {
	registry := registerPangolin(t, newPangolin())
	req := pangolinRequest()
	req.System = "pango lin"

	_, err := BuildPlan(registry, req, LaunchModeExec)
	var invalid *InvalidSystemIDError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an InvalidSystemIDError", err)
	}
}

func TestBuildPlanRefusesUndeclaredLaunchMode(t *testing.T) {
	sys := newPangolin()
	sys.caps.LaunchModes = []LaunchMode{LaunchModeExec}
	registry := registerPangolin(t, sys)

	for _, mode := range []LaunchMode{LaunchModeDryRun, LaunchModeManagedSession, LaunchMode(97)} {
		_, err := BuildPlan(registry, pangolinRequest(), mode)
		if !errors.Is(err, ErrUnsupportedLaunchMode) {
			t.Errorf("BuildPlan(%s) err = %v, want ErrUnsupportedLaunchMode", mode, err)
		}
	}
	if sys.calls["Argv"] != 0 {
		t.Errorf("Argv was called %d times for undeclared modes; the refusal must come before dispatch, or a plugin has to defend itself against modes it never declared", sys.calls["Argv"])
	}
}

// AC4, at the launch site. A system whose transport is none, combined with a
// model that requires an effort, must be refused rather than launched with the
// effort dropped — the silent wrong-cost launch the source repository's
// EffortTransport was introduced to close.
func TestBuildPlanRefusesRequiredEffortUnderTransportNone(t *testing.T) {
	sys := newPangolin()
	sys.caps.EffortTransport = EffortTransportNone
	registry := registerPangolin(t, sys)

	req := pangolinRequest()
	req.Model = Model{ID: "pangolin-thinker", Effort: EffortSupportRequired}
	req.Effort = "high"

	_, err := BuildPlan(registry, req, LaunchModeExec)
	if !errors.Is(err, ErrEffortNotTransportable) {
		t.Fatalf("err = %v, want ErrEffortNotTransportable", err)
	}
	for _, fragment := range []string{"pangolin", "none", "pangolin-thinker"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("refusal %q does not name %q; the operator cannot tell which pair to fix", err, fragment)
		}
	}
	if sys.calls["Argv"] != 0 {
		t.Error("argv was built for a launch that cannot carry its own effort")
	}
}

// The mirror of the case above: even a model with no effort axis must not have
// an operator-supplied effort quietly discarded.
func TestBuildPlanRefusesAnEffortValueUnderTransportNone(t *testing.T) {
	sys := newPangolin()
	sys.caps.EffortTransport = EffortTransportNone
	registry := registerPangolin(t, sys)

	req := pangolinRequest()
	req.Model = Model{ID: "pangolin-flat", Effort: EffortSupportNone}
	req.Effort = "xhigh"

	_, err := BuildPlan(registry, req, LaunchModeExec)
	if !errors.Is(err, ErrEffortNotTransportable) {
		t.Fatalf("err = %v, want ErrEffortNotTransportable", err)
	}
	if !strings.Contains(err.Error(), "xhigh") {
		t.Errorf("refusal %q does not quote the effort value that would have been dropped", err)
	}
}

// Effort is required per model and no default is injected anywhere. A
// required-effort model launched with no value is a configuration error, and
// substituting a recommended value here would make a wrong-cost launch
// indistinguishable from a configured one.
func TestBuildPlanRefusesRequiredEffortWithNoValue(t *testing.T) {
	registry := registerPangolin(t, newPangolin())

	for _, effort := range []string{"", "   "} {
		req := pangolinRequest()
		req.Effort = effort
		_, err := BuildPlan(registry, req, LaunchModeExec)
		if !errors.Is(err, ErrEffortMissing) {
			t.Fatalf("BuildPlan with effort %q err = %v, want ErrEffortMissing", effort, err)
		}
	}
}

// A transport that CAN carry effort must still admit a model that needs none;
// otherwise the gate is refusing a class it was never meant to cover.
func TestBuildPlanAdmitsAnEffortlessModelUnderEveryTransport(t *testing.T) {
	for _, transport := range []EffortTransport{EffortTransportNone, EffortTransportArgv, EffortTransportStdin} {
		sys := newPangolin()
		sys.caps.EffortTransport = transport
		registry := registerPangolin(t, sys)

		req := pangolinRequest()
		req.Model = Model{ID: "pangolin-flat", Effort: EffortSupportNone}
		req.Effort = ""

		if _, err := BuildPlan(registry, req, LaunchModeExec); err != nil {
			t.Errorf("BuildPlan under transport %s refused a model with no effort axis: %v", transport, err)
		}
	}
}

func TestBuildPlanRefusesUnsupportedLaunchParameters(t *testing.T) {
	cases := []struct {
		name    string
		disable func(*Capabilities)
		mutate  func(*LaunchRequest)
		want    error
	}{
		{
			name:    "goal",
			disable: func(c *Capabilities) { c.SupportsGoal = false },
			mutate:  func(r *LaunchRequest) { r.Goal = &Goal{ID: "GOAL-1"} },
			want:    ErrGoalUnsupported,
		},
		{
			name:    "budget",
			disable: func(c *Capabilities) { c.SupportsBudget = false },
			mutate:  func(r *LaunchRequest) { r.Budget = &Budget{USD: 5} },
			want:    ErrBudgetUnsupported,
		},
		{
			name:    "service tier",
			disable: func(c *Capabilities) { c.SupportsServiceTier = false },
			mutate:  func(r *LaunchRequest) { r.ServiceTier = "priority" },
			want:    ErrServiceTierUnsupported,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sys := newPangolin()
			tc.disable(&sys.caps)
			registry := registerPangolin(t, sys)

			req := pangolinRequest()
			req.Goal, req.Budget, req.ServiceTier = nil, nil, ""
			tc.mutate(&req)

			if _, err := BuildPlan(registry, req, LaunchModeExec); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// The same parameters must go through when the system declares support, or the
// refusals above would be indistinguishable from a gate that rejects
// everything.
func TestBuildPlanAdmitsSupportedLaunchParameters(t *testing.T) {
	registry := registerPangolin(t, newPangolin())
	if _, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec); err != nil {
		t.Fatalf("BuildPlan with goal, budget, tier and composition all declared supported: %v", err)
	}
}

// A system declaring GrammarNone is refused at the contract level, before its
// own validator runs. Two lines of defence on purpose: the plugin's validator
// is where grammar-specific shape is judged, and this is where "no grammar at
// all" is, so a plugin cannot accidentally admit a composition by writing a
// permissive validator.
func TestBuildPlanRefusesCompositionUnderGrammarNone(t *testing.T) {
	sys := newPangolin()
	sys.caps.CompositionGrammar = GrammarNone
	sys.compositionOK = true
	registry := registerPangolin(t, sys)

	_, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
	if !errors.Is(err, ErrCompositionUnsupported) {
		t.Fatalf("err = %v, want ErrCompositionUnsupported", err)
	}
	if sys.calls["ValidateComposition"] != 0 {
		t.Error("a system declaring no grammar was asked to validate a composition")
	}
}

func TestBuildPlanPropagatesThePluginsCompositionRefusal(t *testing.T) {
	sys := newPangolin()
	sys.compositionOK = false
	registry := registerPangolin(t, sys)

	_, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
	if err == nil {
		t.Fatal("a composition the plugin rejected produced a plan")
	}
	if !strings.Contains(err.Error(), "pangolin-json") {
		t.Errorf("refusal %q does not name the grammar the composition was measured against", err)
	}
}

// No composition means the validator is not called at all. A system with no
// composition attached must not have to answer for one.
func TestBuildPlanSkipsCompositionValidationWhenNoneIsAttached(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)

	req := pangolinRequest()
	req.Composition = Composition{}
	if _, err := BuildPlan(registry, req, LaunchModeExec); err != nil {
		t.Fatalf("BuildPlan without a composition: %v", err)
	}
	if sys.calls["ValidateComposition"] != 0 {
		t.Error("ValidateComposition was called for a launch with no composition")
	}
}

// A plugin that answers a surface with something the contract forbids must
// surface as a refusal here, not as a half-formed launch downstream. An empty
// binary reported as a success is the shape that would otherwise exec whatever
// the shell resolves.
func TestBuildPlanRefusesPluginContractViolations(t *testing.T) {
	t.Run("empty binary with no error", func(t *testing.T) {
		sys := newPangolin()
		sys.emptyBinary = true
		registry := registerPangolin(t, sys)

		_, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
		if !errors.Is(err, ErrPluginContract) {
			t.Fatalf("err = %v, want ErrPluginContract", err)
		}
	})

	t.Run("stdin bytes while reporting nothing attached", func(t *testing.T) {
		sys := newPangolin()
		sys.detachedStdinBytes = true
		registry := registerPangolin(t, sys)

		_, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
		if !errors.Is(err, ErrPluginContract) {
			t.Fatalf("err = %v, want ErrPluginContract", err)
		}
	})
}

// An attached but empty stdin stream is legitimate and different from no
// stdin: the child sees EOF rather than a closed descriptor. The contract
// refusal above must not swallow it.
func TestBuildPlanAdmitsAnAttachedEmptyStdin(t *testing.T) {
	registry := registerPangolin(t, newPangolin())
	req := pangolinRequest()
	req.Prompt = nil

	plan, err := BuildPlan(registry, req, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan with an empty prompt: %v", err)
	}
	if !plan.Stdin.Attached || len(plan.Stdin.Bytes) != 0 {
		t.Errorf("Stdin = %#v, want an attached empty stream", plan.Stdin)
	}
}

// Each surface's own failure must reach the caller naming the system and the
// surface, rather than collapsing into one opaque "could not plan".
func TestBuildPlanPropagatesEachSurfaceFailure(t *testing.T) {
	boom := errors.New("preflight has not run")
	cases := []struct {
		name    string
		mutate  func(*pangolinSystem)
		mention string
	}{
		{"ResolveBinary", func(s *pangolinSystem) { s.binaryErr = boom }, "binary"},
		{"Argv", func(s *pangolinSystem) { s.argvErr = boom }, "argv"},
		{"ChildEnv", func(s *pangolinSystem) { s.envErr = boom }, "environment"},
		{"Stdin", func(s *pangolinSystem) { s.stdinErr = boom }, "stdin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sys := newPangolin()
			tc.mutate(sys)
			registry := registerPangolin(t, sys)

			_, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v, want it to wrap the plugin's own failure", err)
			}
			if !strings.Contains(err.Error(), "pangolin") || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("err = %q, want it to name the system and the %s surface", err, tc.mention)
			}
		})
	}
}

func TestBuildPlanRefusesANilRegistry(t *testing.T) {
	if _, err := BuildPlan(nil, pangolinRequest(), LaunchModeExec); err == nil {
		t.Fatal("BuildPlan produced a plan without a registry to dispatch through")
	}
}

// An explicit home overrides the declared default. On-disk limit state is
// keyed by (provider, home), so the value that reaches the plan is the value
// that names the state file.
func TestBuildPlanPrefersAnExplicitHomeOverTheDeclaredDefault(t *testing.T) {
	registry := registerPangolin(t, newPangolin())
	req := pangolinRequest()
	req.Home = "/tmp/alt-pangolin-home"

	plan, err := BuildPlan(registry, req, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Home != "/tmp/alt-pangolin-home" {
		t.Errorf("Home = %q, want the request's explicit home", plan.Home)
	}
	if !containsEnv(plan.Env, "PANGOLIN_HOME=/tmp/alt-pangolin-home") {
		t.Errorf("child env = %v, want the explicit home exported to the child", plan.Env)
	}
}
