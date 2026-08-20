package agentic

import (
	"errors"
	"fmt"
	"strings"
)

// Plan is the observable launch surface: the binary, the argv, the child
// environment and the stdin bytes, plus where the child runs and which
// configuration home it reads.
//
// Invariant 1 of docs/architecture.md makes this the parity bar. A refactor
// proves itself by producing an identical Plan, not by producing an argv
// string that reads the same — which is why the environment and the stdin
// bytes are fields here rather than side effects of a process start.
type Plan struct {
	System  SystemID
	Mode    LaunchMode
	Binary  string
	Argv    []string
	Env     []string
	Stdin   StdinPayload
	WorkDir string
	Home    string
}

var (
	// ErrUnknownSystem is returned when no plugin is registered for the
	// requested identifier. It is a refusal, never a fallback: guessing a
	// system is how a launch ends up on the wrong harness.
	ErrUnknownSystem = errors.New("agentic: no system registered")
	// ErrUnsupportedLaunchMode is returned when a system was not declared for
	// the requested mode.
	ErrUnsupportedLaunchMode = errors.New("agentic: system does not support launch mode")
	// ErrEffortNotTransportable is returned when a model requires an explicit
	// reasoning effort and the system's declared transport cannot carry one.
	// The source repo's failure this closes is the silent one: the launch
	// succeeded, the configured effort was dropped on the floor, and the cost
	// and quality of the run were whatever the harness defaulted to.
	ErrEffortNotTransportable = errors.New("agentic: system cannot carry the model's required reasoning effort")
	// ErrEffortMissing is returned when a required-effort model was given no
	// effort value. No default is injected anywhere: the vendor layer owns the
	// vocabulary and the recommended value, and inventing one here would make
	// a wrong-cost launch look like a configured one.
	ErrEffortMissing = errors.New("agentic: model requires an explicit reasoning effort")
	// ErrGoalUnsupported, ErrBudgetUnsupported and ErrServiceTierUnsupported
	// refuse a launch parameter the system cannot honour. Each is a refusal
	// rather than a drop for the same reason as effort: a dropped parameter
	// produces a run that looks like the one that was asked for and is not.
	ErrGoalUnsupported        = errors.New("agentic: system does not support goal-bound launches")
	ErrBudgetUnsupported      = errors.New("agentic: system does not support a launch budget")
	ErrServiceTierUnsupported = errors.New("agentic: system does not support a service tier")
	// ErrCompositionUnsupported is returned when a composition is attached to
	// a system declaring GrammarNone.
	ErrCompositionUnsupported = errors.New("agentic: system declares no launch composition grammar")
	// ErrPluginContract is returned when a plugin answers a dispatch surface
	// with something the contract forbids — an empty binary reported as a
	// success, or a stdin payload carrying bytes while claiming nothing is
	// attached. A plugin bug surfaces as a refusal here rather than as a
	// half-formed launch downstream.
	ErrPluginContract = errors.New("agentic: system violated the plugin contract")
)

// BuildPlan resolves one launch through the registry.
//
// It is the single dispatch site for the System contract: every surface the
// interface exposes is driven from here, by identifier lookup, with no switch
// over system identifiers anywhere. Adding a system is a registration and
// nothing else.
//
// The checks it performs are contract-level only — can this system, as it
// declared itself, carry what this request contains. Vendor admission (is the
// model available, is the provider rate-limited, is the pair admitted) is a
// later layer and deliberately absent: BuildPlan answers "is this launch
// expressible", not "is this launch allowed right now".
func BuildPlan(r *Registry, req LaunchRequest, mode LaunchMode) (Plan, error) {
	if r == nil {
		return Plan{}, errors.New("agentic: cannot build a plan without a registry")
	}
	id, err := NormalizeSystemID(string(req.System))
	if err != nil {
		return Plan{}, fmt.Errorf("agentic: building plan: %w", err)
	}
	sys, ok := r.Lookup(id)
	if !ok {
		return Plan{}, fmt.Errorf("%w: %s", ErrUnknownSystem, id)
	}
	caps := sys.Capabilities()

	if !mode.Valid() || !caps.SupportsMode(mode) {
		return Plan{}, fmt.Errorf("%w: %s does not declare %s", ErrUnsupportedLaunchMode, id, mode)
	}

	effort := strings.TrimSpace(req.Effort)
	if !caps.EffortTransport.CanCarry(req.Model.Effort) {
		return Plan{}, fmt.Errorf("%w: %s declares effort transport %s and model %q requires an effort; register the model under a system whose transport is argv or stdin, or register it as effort-support none",
			ErrEffortNotTransportable, id, caps.EffortTransport, req.Model.ID)
	}
	if req.Model.Effort == EffortSupportRequired && effort == "" {
		return Plan{}, fmt.Errorf("%w: model %q under %s; supply the effort value from the model's own vocabulary", ErrEffortMissing, req.Model.ID, id)
	}
	if effort != "" && caps.EffortTransport == EffortTransportNone {
		return Plan{}, fmt.Errorf("%w: %s declares effort transport none and cannot carry effort %q", ErrEffortNotTransportable, id, effort)
	}

	if req.Goal != nil && !caps.SupportsGoal {
		return Plan{}, fmt.Errorf("%w: %s", ErrGoalUnsupported, id)
	}
	if req.Budget != nil && !caps.SupportsBudget {
		return Plan{}, fmt.Errorf("%w: %s", ErrBudgetUnsupported, id)
	}
	if strings.TrimSpace(req.ServiceTier) != "" && !caps.SupportsServiceTier {
		return Plan{}, fmt.Errorf("%w: %s", ErrServiceTierUnsupported, id)
	}

	if !req.Composition.IsZero() {
		if caps.CompositionGrammar == GrammarNone {
			return Plan{}, fmt.Errorf("%w: %s", ErrCompositionUnsupported, id)
		}
		if err := sys.ValidateComposition(req.Composition); err != nil {
			return Plan{}, fmt.Errorf("agentic: %s rejected the launch composition for grammar %q: %w", id, caps.CompositionGrammar, err)
		}
	}

	binary, err := sys.ResolveBinary(req)
	if err != nil {
		return Plan{}, fmt.Errorf("agentic: %s could not resolve its binary: %w", id, err)
	}
	if strings.TrimSpace(binary) == "" {
		return Plan{}, fmt.Errorf("%w: %s resolved an empty binary with no error", ErrPluginContract, id)
	}

	argv, err := sys.Argv(req, mode)
	if err != nil {
		return Plan{}, fmt.Errorf("agentic: %s could not build %s argv: %w", id, mode, err)
	}

	env, err := sys.ChildEnv(req.Env, req)
	if err != nil {
		return Plan{}, fmt.Errorf("agentic: %s could not build the child environment: %w", id, err)
	}

	stdin, err := sys.Stdin(req)
	if err != nil {
		return Plan{}, fmt.Errorf("agentic: %s could not build its stdin payload: %w", id, err)
	}
	if !stdin.Attached && len(stdin.Bytes) > 0 {
		return Plan{}, fmt.Errorf("%w: %s returned %d stdin bytes while reporting nothing attached", ErrPluginContract, id, len(stdin.Bytes))
	}

	home := strings.TrimSpace(req.Home)
	if home == "" {
		home = caps.DefaultHome
	}

	return Plan{
		System:  id,
		Mode:    mode,
		Binary:  binary,
		Argv:    argv,
		Env:     env,
		Stdin:   stdin,
		WorkDir: req.WorkDir,
		Home:    home,
	}, nil
}
