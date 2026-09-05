package agentic

import (
	"errors"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// LaunchProvenance is the resolved identity snapshot attached by the generic
// runtime boundary. Publisher and family remain observation-only; engine refs
// are graph identities and therefore carry both id and kind.
type LaunchProvenance struct {
	Broker          string
	Runtime         string
	Profile         string
	Model           string
	Publisher       string
	Family          string
	RequestedEngine plugin.Ref
	ResolvedEngine  plugin.Ref
}

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

	// ModelIdentity is what the caller asked for and what the harness was
	// handed. It is populated for EVERY plan, alias or not, because a consumer
	// that had to infer "no alias" from an absent field would be inferring it
	// from a read it cannot distinguish from a field nobody set.
	ModelIdentity ModelIdentity

	Provenance LaunchProvenance

	// Nodes is empty for the source-compatible single-process plan. A
	// consumer that needs an inference engine or sidecar calls
	// BuildMultiNodePlan, which preserves every field above as the primary
	// process and adds a validated, dependency-ordered node graph here.
	Nodes []PlanNode `json:"nodes,omitempty"`
}

// ModelIdentity is the pair one plan must keep: the model spelling the caller
// REQUESTED and the identity the harness was actually LAUNCHED under.
//
// They differ exactly when the request carried Model.AliasOf. Both are kept
// because they answer different questions and a run needs both answered: argv,
// cost and the provider's own logs are about Launched, while the operator's
// choice, the board's admission record and every audit line that has to
// reproduce what a human asked for are about Requested. Collapsing them onto
// one field would make an alias launch indistinguishable from a launch of the
// identity, which is the fact an audit is for.
type ModelIdentity struct {
	Requested string
	Launched  string
}

// IsAlias reports whether the plan substituted an identity for the requested
// spelling.
func (m ModelIdentity) IsAlias() bool { return m.Requested != m.Launched }

var (
	// ErrUnknownSystem is returned when no plugin is registered for the
	// requested identifier. It is a refusal, never a fallback: guessing a
	// system is how a launch ends up on the wrong harness.
	ErrUnknownSystem = errors.New("agentic: no system registered")
	// ErrUnsupportedLaunchMode is returned when a system was not declared for
	// the requested mode.
	ErrUnsupportedLaunchMode = errors.New("agentic: system does not support launch mode")
	// ErrModelMissing is returned when the request names no model. It is
	// checked once here, for every mode, before any plugin surface is
	// dispatched: a plan built around an empty Model.ID would hand the harness
	// its own default, and a run under a model nobody chose is the same
	// silent-substitution failure the effort and alias rules already refuse.
	// The exec grammar admitted `--model ""` before this sentinel existed;
	// pi's interactive argv kept its own refusal as a second line of defence.
	ErrModelMissing = errors.New("agentic: launch request names no model")
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
	// ErrCompositionNotInteractive is returned when an interactive launch
	// carries a composition — a prefix, a server list, or both. It is a
	// different refusal from ErrCompositionUnsupported on purpose: the system
	// may well declare a grammar, but in LaunchModeInteractive the MCP channel
	// belongs to the composer that owns the terminal session (Decision 0013
	// §6.5), and a second component spelling MCP flags is the defect class the
	// decision names M2.
	ErrCompositionNotInteractive = errors.New("agentic: interactive launch carries no composition; the MCP prefix is the composer's")
	// ErrParameterNotInteractive is returned when an interactive launch
	// carries a parameter its grammar has no channel for: a goal, a budget, a
	// service tier, or an assignment prompt. Decision 0013 §5 forbids each of
	// them on the interactive argv and on stdin, and a parameter a plugin
	// cannot place is refused rather than dropped — a dropped one produces a
	// session that looks like the one that was asked for and is not.
	ErrParameterNotInteractive = errors.New("agentic: interactive launch carries a parameter its grammar has no channel for")
	// ErrPluginContract is returned when a plugin answers a dispatch surface
	// with something the contract forbids — an empty binary reported as a
	// success, or a stdin payload carrying bytes while claiming nothing is
	// attached. A plugin bug surfaces as a refusal here rather than as a
	// half-formed launch downstream.
	ErrPluginContract = errors.New("agentic: system violated the plugin contract")
)

// refuseNonInteractiveParameters is the interactive grammar's request-side
// half, applied once here so that no plugin has to carry its own copy of the
// rule and no plugin can forget one.
//
// The composition refusal is the decision's own sentinel. The parameter
// refusal is the same constraint read from the other side: a goal, a budget, a
// service tier or a prompt would each have to reach the child through a flag
// or a stdin the mode forbids, so admitting one and building an argv without
// it is a launch parameter silently dropped — the refusal-over-drop rule every
// other sentinel in this file already follows.
//
// It sits BEFORE the composition-grammar checks below, so an interactive
// request carrying a composition is refused for being interactive rather than
// for its grammar: the operator fixing it needs to know the prefix does not
// belong here at all, not that it is malformed.
func refuseNonInteractiveParameters(id SystemID, req LaunchRequest) error {
	if !req.Composition.IsZero() {
		return fmt.Errorf("%w: %s was handed %d prefix argument(s) and %d server(s)", ErrCompositionNotInteractive, id, len(req.Composition.Prefix), len(req.Composition.Servers))
	}
	var carried []string
	if req.Goal != nil {
		carried = append(carried, "goal")
	}
	if req.Budget != nil {
		carried = append(carried, "budget")
	}
	if strings.TrimSpace(req.ServiceTier) != "" {
		carried = append(carried, "service tier")
	}
	if strings.TrimSpace(req.PromptPath) != "" || len(req.Prompt) > 0 {
		carried = append(carried, "assignment prompt")
	}
	if len(carried) > 0 {
		return fmt.Errorf("%w: %s was handed %s", ErrParameterNotInteractive, id, strings.Join(carried, ", "))
	}
	return nil
}

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
	req, err = PrepareLaunchRequest(sys, req, mode)
	if err != nil {
		return Plan{}, fmt.Errorf("agentic: %s rejected the launch request before planning: %w", id, err)
	}
	if strings.TrimSpace(req.Model.ID) == "" {
		return Plan{}, fmt.Errorf("%w: %s in %s mode", ErrModelMissing, id, mode)
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

	if mode == LaunchModeInteractive {
		if err := refuseNonInteractiveParameters(id, req); err != nil {
			return Plan{}, err
		}
	}

	if !req.Composition.IsZero() {
		if caps.CompositionGrammar == GrammarNone {
			return Plan{}, fmt.Errorf("%w: %s", ErrCompositionUnsupported, id)
		}
		if err := sys.ValidateComposition(req.Composition); err != nil {
			return Plan{}, fmt.Errorf("agentic: %s rejected the launch composition for grammar %q: %w", id, caps.CompositionGrammar, err)
		}
	}

	// ALIAS SUBSTITUTION, and the ONLY place in this module it happens.
	//
	// It sits after every contract refusal above and before the first plugin
	// surface below, and both halves of that position are load-bearing. After,
	// so a refusal names the spelling the operator typed rather than an
	// identity they never wrote. Before, so ResolveBinary, Argv, ChildEnv and
	// Stdin are ALL dispatched under the identity — a substitution applied to
	// argv alone would leave a system whose environment or binary lookup reads
	// the model with the alias the backend refuses.
	//
	// AliasOf is cleared on the way through. The substitution is then
	// idempotent by construction and no plugin can resolve it a second time,
	// which is what keeps "one hop" a property of the code rather than of the
	// data it happened to be given.
	identity := ModelIdentity{Requested: strings.TrimSpace(req.Model.ID), Launched: req.Model.LaunchIdentity()}
	req.Model.ID = identity.Launched
	req.Model.AliasOf = ""

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
	if mode == LaunchModeInteractive && stdin.Attached && caps.EffortTransport != EffortTransportStdin {
		// Decision 0013 §5: an interactive session attaches nothing unless the
		// effort itself rides stdin. A plugin that attached bytes anyway has
		// found a prompt channel the mode does not have, and the plan must not
		// carry it out to a terminal.
		return Plan{}, fmt.Errorf("%w: %s attached %d stdin bytes to an interactive launch under effort transport %s", ErrPluginContract, id, len(stdin.Bytes), caps.EffortTransport)
	}

	home := strings.TrimSpace(req.Home)
	if home == "" {
		home = caps.DefaultHome
	}

	return Plan{
		System:        id,
		Mode:          mode,
		Binary:        binary,
		Argv:          argv,
		Env:           env,
		Stdin:         stdin,
		WorkDir:       req.WorkDir,
		Home:          home,
		ModelIdentity: identity,
	}, nil
}
