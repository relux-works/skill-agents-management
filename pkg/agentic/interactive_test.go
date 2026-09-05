package agentic

import (
	"errors"
	"strings"
	"testing"
)

// This file is the core half of LaunchModeInteractive (curator-spec Decision
// 0013 §5). Every test but the first two is a negative one: the mode's whole
// value at this layer is what BuildPlan refuses before any plugin surface runs,
// and a gate is worth exactly what it rejects. The per-system argv is each
// plugin's to spell and each plugin's interactive_test.go to prove.

// interactivePangolin is the test double declared for the mode, with the
// request stripped down to what the mode admits. Everything the default
// request carries — goal, budget, tier, prompt, composition — is a thing the
// interactive grammar has no channel for, so the positive baseline is the
// request with all of them removed.
func interactivePangolin() *pangolinSystem {
	sys := newPangolin()
	sys.caps.LaunchModes = append(sys.caps.LaunchModes, LaunchModeInteractive)
	sys.detachedStdin = true
	return sys
}

func interactiveRequest() LaunchRequest {
	req := pangolinRequest()
	req.Goal = nil
	req.Budget = nil
	req.ServiceTier = ""
	req.PromptPath = ""
	req.Prompt = nil
	req.Composition = Composition{}
	return req
}

func TestLaunchModeInteractiveIsDeclaredAppendedAndNamed(t *testing.T) {
	if !LaunchModeInteractive.Valid() {
		t.Error("LaunchModeInteractive is not a valid mode")
	}
	if got := LaunchModeInteractive.String(); got != "interactive" {
		t.Errorf("String() = %q, want %q", got, "interactive")
	}
	// Appended, never inserted: the three earlier values are what existing
	// declarations and goldens were built against.
	if LaunchModeInteractive != LaunchModeManagedSession+1 || LaunchModeManagedSession != 2 {
		t.Errorf("LaunchModeInteractive = %d; it must be appended after LaunchModeManagedSession (%d)", LaunchModeInteractive, LaunchModeManagedSession)
	}
}

// The positive path proves the gate below is reachable — a request with
// nothing the mode forbids builds a plan — and that the stdin rule admits an
// unattached payload.
func TestBuildPlanAdmitsAnInteractiveLaunchCarryingOnlyModelAndEffort(t *testing.T) {
	sys := interactivePangolin()
	registry := registerPangolin(t, sys)

	plan, err := BuildPlan(registry, interactiveRequest(), LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan(interactive): %v", err)
	}
	if plan.Mode != LaunchModeInteractive {
		t.Errorf("plan.Mode = %s, want interactive", plan.Mode)
	}
	if plan.Stdin.Attached {
		t.Errorf("the plan attached a stdin to an interactive launch under an argv transport: %+v", plan.Stdin)
	}
	if plan.Home != "~/.pangolin" || plan.WorkDir != "/work/story" {
		t.Errorf("Home/WorkDir did not carry as in every other mode: %q / %q", plan.Home, plan.WorkDir)
	}
}

// An undeclared mode is refused exactly as any other undeclared mode is —
// there is no implicit interactive support for a system that never said so.
func TestBuildPlanRefusesInteractiveForASystemThatDidNotDeclareIt(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)

	_, err := BuildPlan(registry, interactiveRequest(), LaunchModeInteractive)
	if !errors.Is(err, ErrUnsupportedLaunchMode) {
		t.Fatalf("err = %v, want ErrUnsupportedLaunchMode", err)
	}
	if sys.calls["Argv"] != 0 {
		t.Error("Argv was dispatched for a mode the system never declared")
	}
}

// The decision's own sentinel. A prefix alone, servers alone, and both are each
// refused — IsZero is the boundary, and a half-filled composition is still a
// composition the composer owns.
func TestBuildPlanRefusesAnInteractiveLaunchCarryingAComposition(t *testing.T) {
	cases := map[string]Composition{
		"prefix and servers": pangolinRequest().Composition,
		"prefix only":        {Prefix: []string{"--pangolin-mcp", "{}"}},
		"servers only":       {Servers: []CompositionServer{{Name: "jira", Transport: "http"}}},
	}
	for name, composition := range cases {
		t.Run(name, func(t *testing.T) {
			sys := interactivePangolin()
			registry := registerPangolin(t, sys)
			req := interactiveRequest()
			req.Composition = composition

			_, err := BuildPlan(registry, req, LaunchModeInteractive)
			if !errors.Is(err, ErrCompositionNotInteractive) {
				t.Fatalf("err = %v, want ErrCompositionNotInteractive", err)
			}
			if errors.Is(err, ErrCompositionUnsupported) {
				t.Error("the refusal was ErrCompositionUnsupported; the system declares a grammar, and the operator needs to hear that the prefix does not belong in this mode at all")
			}
			if sys.calls["ValidateComposition"] != 0 {
				t.Error("the plugin was asked to validate a composition the mode forbids; the refusal must come before grammar validation")
			}
			if sys.calls["Argv"] != 0 {
				t.Error("Argv was dispatched for a request the mode refuses")
			}
		})
	}

	t.Run("and the same composition is admitted in exec mode", func(t *testing.T) {
		// The narrowing: the refusal is the MODE's, not a new rule against
		// compositions in general.
		registry := registerPangolin(t, interactivePangolin())
		if _, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec); err != nil {
			t.Fatalf("the exec launch carrying the same composition was refused: %v", err)
		}
	})
}

// A parameter the grammar has no channel for is refused, not dropped. Each is
// planted alone on an otherwise-admissible request, so the refusal is
// attributable to that one parameter and the error text names it.
func TestBuildPlanRefusesInteractiveParametersTheGrammarCannotCarry(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LaunchRequest)
		names  string
	}{
		{"goal", func(r *LaunchRequest) { r.Goal = &Goal{ID: "GOAL-1"} }, "goal"},
		{"budget", func(r *LaunchRequest) { r.Budget = &Budget{USD: 1} }, "budget"},
		{"service tier", func(r *LaunchRequest) { r.ServiceTier = "priority" }, "service tier"},
		{"prompt path", func(r *LaunchRequest) { r.PromptPath = "/tmp/assignment.md" }, "assignment prompt"},
		{"prompt bytes", func(r *LaunchRequest) { r.Prompt = []byte("do the thing") }, "assignment prompt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sys := interactivePangolin()
			registry := registerPangolin(t, sys)
			req := interactiveRequest()
			c.mutate(&req)

			_, err := BuildPlan(registry, req, LaunchModeInteractive)
			if !errors.Is(err, ErrParameterNotInteractive) {
				t.Fatalf("err = %v, want ErrParameterNotInteractive", err)
			}
			if !strings.Contains(err.Error(), c.names) {
				t.Errorf("refusal %q does not name the parameter (%q) the operator has to remove", err, c.names)
			}
			if sys.calls["Argv"] != 0 {
				t.Error("Argv was dispatched for a request the mode refuses")
			}
		})
	}
}

// The stdin rule, held at the dispatch site. The double attaches a stdin for
// every request; under an argv transport an interactive plan must not carry
// it out, and under a stdin transport it may — that is the effort encoding's
// channel and the only thing the mode lets ride stdin.
func TestBuildPlanRefusesAnAttachedInteractiveStdinUnderANonStdinTransport(t *testing.T) {
	for _, transport := range []EffortTransport{EffortTransportArgv, EffortTransportNone} {
		t.Run(transport.String(), func(t *testing.T) {
			sys := interactivePangolin()
			sys.caps.EffortTransport = transport
			sys.detachedStdin = false
			sys.attachedStdin = []byte("effort: high")
			registry := registerPangolin(t, sys)
			req := interactiveRequest()
			if transport == EffortTransportNone {
				req.Model.Effort = EffortSupportNone
				req.Effort = ""
			}

			_, err := BuildPlan(registry, req, LaunchModeInteractive)
			if !errors.Is(err, ErrPluginContract) {
				t.Fatalf("err = %v, want ErrPluginContract: a plugin found a stdin channel the mode does not have", err)
			}
		})
	}

	t.Run("an attached empty stream is refused too", func(t *testing.T) {
		// Attached-and-empty and not-attached are different facts to the
		// child (system.go, StdinPayload), and the mode names the second.
		sys := interactivePangolin()
		sys.detachedStdin = false
		registry := registerPangolin(t, sys)
		if _, err := BuildPlan(registry, interactiveRequest(), LaunchModeInteractive); !errors.Is(err, ErrPluginContract) {
			t.Fatalf("err = %v, want ErrPluginContract for an attached empty stdin", err)
		}
	})

	t.Run("and a stdin transport may attach the effort encoding", func(t *testing.T) {
		sys := interactivePangolin()
		sys.caps.EffortTransport = EffortTransportStdin
		sys.detachedStdin = false
		sys.attachedStdin = []byte("effort: high")
		registry := registerPangolin(t, sys)

		plan, err := BuildPlan(registry, interactiveRequest(), LaunchModeInteractive)
		if err != nil {
			t.Fatalf("BuildPlan: %v", err)
		}
		if !plan.Stdin.Attached || string(plan.Stdin.Bytes) != "effort: high" {
			t.Errorf("the effort encoding did not reach the plan's stdin: %+v", plan.Stdin)
		}
	})

	t.Run("and the same attached stdin is admitted in exec mode", func(t *testing.T) {
		sys := interactivePangolin()
		sys.detachedStdin = false
		sys.attachedStdin = []byte("do the thing")
		registry := registerPangolin(t, sys)
		if _, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec); err != nil {
			t.Fatalf("the exec launch was refused for an attached stdin: %v", err)
		}
	})
}

// Effort follows the model's EffortSupport in this mode as in every other, with
// no default injected: a required-effort model with no effort word is refused
// with the unchanged sentinel.
func TestBuildPlanInteractiveEffortFollowsTheModelWithNoDefault(t *testing.T) {
	sys := interactivePangolin()
	registry := registerPangolin(t, sys)
	req := interactiveRequest()
	req.Effort = ""

	if _, err := BuildPlan(registry, req, LaunchModeInteractive); !errors.Is(err, ErrEffortMissing) {
		t.Fatalf("err = %v, want ErrEffortMissing", err)
	}
	if _, ok := sys.seenModel["Argv"]; ok {
		t.Error("Argv was dispatched for a required-effort model with no effort")
	}
}
