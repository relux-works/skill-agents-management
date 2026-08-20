package vendorplugin

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

var (
	// ErrUnknownModel is returned when a spawn names a model the resolved
	// vendor does not declare.
	ErrUnknownModel = errors.New("vendorplugin: vendor declares no such model")
	// ErrModelNotDrivenBySystem is returned when a model is real, the runtime
	// is real, and the model never declared that harness. It is a refusal
	// rather than a best effort: a launch on an undeclared harness is a run
	// nobody has evidence works.
	ErrModelNotDrivenBySystem = errors.New("vendorplugin: model does not declare the runtime's agentic system")
	// ErrEffortMissing is returned when a required-effort model was given no
	// effort. The refusal names the vocabulary and the vendor's recommendation
	// so an agent hitting it mid-run can fix the call from the error text —
	// which is what invariant 4 of docs/architecture.md asks of a refusal.
	// Nothing substitutes the recommendation: a launch that silently ran at a
	// default is the wrong-cost launch that invariant exists to close.
	ErrEffortMissing = errors.New("vendorplugin: model requires an explicit reasoning effort")
	// ErrEffortNotInVocabulary is returned when an effort word is not one the
	// model accepts, or when a model with no effort axis was given one.
	ErrEffortNotInVocabulary = errors.New("vendorplugin: effort is not in the model's vocabulary")
	// ErrAvailabilityInvalid is returned when a verdict's state and its
	// evidence contradict each other.
	ErrAvailabilityInvalid = errors.New("vendorplugin: availability verdict contradicts its own evidence")
	// ErrVendorContract is returned when a vendor plugin answers a dispatch
	// surface with something the contract forbids — most importantly a launch
	// request that redirects the launch away from what was admitted. A plugin
	// bug surfaces as a refusal here rather than as a launch on the wrong
	// harness, the wrong model or the wrong effort.
	ErrVendorContract = errors.New("vendorplugin: vendor violated the plugin contract")
)

// SpawnRequest is everything a caller supplies for one launch through a
// runtime.
//
// The launch parameters are agentic's own types — Goal, Budget, Composition —
// rather than vendor-layer copies of them. A second Goal type here would be
// the shadow-declaration disease in the type system: two structs that agree
// until one of them gains a field.
type SpawnRequest struct {
	// Runtime selects the declared (agentic system × vendor) pair.
	Runtime RuntimeID
	// Model selects a row from that runtime's vendor. It must be one the
	// vendor declares AND one that declares the runtime's system.
	Model ModelID
	// Effort is the reasoning effort in the MODEL's vocabulary. It is required
	// for a model whose effort support is required, refused for a model with
	// no effort axis, and never defaulted.
	Effort string

	// PromptPath is the assignment prompt file on disk; Prompt is its bytes
	// for a system whose transport is stdin. Which one a harness reads is the
	// agentic system's declaration.
	PromptPath string
	Prompt     []byte

	// WorkDir is the child's working directory; Home overrides the harness
	// configuration home. Home is load-bearing beyond the launch: on-disk
	// limit state is keyed by (provider, home).
	WorkDir string
	Home    string

	// Env is the parent environment the child inherits from, before the
	// system's filtering and the vendor's additions.
	Env []string

	Goal        *agentic.Goal
	Budget      *agentic.Budget
	ServiceTier string
	Composition agentic.Composition
}

// BuildLaunch resolves one launch through both layers.
//
// It is the single dispatch site for the Vendor contract: every surface the
// interface exposes is reached from here or from CheckAvailability, by
// identifier lookup, with no switch over vendor or runtime identifiers
// anywhere. Adding a vendor is a registration, and adding a runtime is a
// declaration.
//
// The order is deliberate. Resolution first, so a refusal names the missing
// plugin rather than a symptom. Then the model row and the effort word, which
// are the vendor's own vocabulary and must be validated before a plugin is
// asked to build anything. Then the plugin's Spawn. Then a fidelity check on
// what it returned — a vendor may ADD to a launch (authentication env, a
// configuration home) and may not REDIRECT it. Then agentic.BuildPlan, which
// applies the Layer-1 contract checks: the effort transport, the launch mode,
// the composition grammar, the goal/budget/tier support.
//
// What it does NOT do is ask whether the launch is allowed RIGHT NOW.
// Availability, limit state and admission are a separate question with a
// separate entry point (CheckAvailability), and folding them in here would
// make an expressibility check depend on a network read.
func BuildLaunch(r *Registry, req SpawnRequest, mode agentic.LaunchMode) (agentic.Plan, error) {
	if r == nil {
		return agentic.Plan{}, errors.New("vendorplugin: cannot build a launch without a registry")
	}
	runtime, err := r.ResolveRuntime(req.Runtime)
	if err != nil {
		return agentic.Plan{}, err
	}

	model, err := selectModel(runtime, req.Model)
	if err != nil {
		return agentic.Plan{}, err
	}
	if !model.DrivenBy(runtime.SystemID) {
		return agentic.Plan{}, fmt.Errorf("%w: runtime %s runs on %q and model %q declares %v",
			ErrModelNotDrivenBySystem, runtime.ID, runtime.SystemID, model.ID, model.Systems)
	}

	effort, err := resolveEffort(runtime, model, req.Effort)
	if err != nil {
		return agentic.Plan{}, err
	}

	launch, err := runtime.Vendor.Spawn(SpawnContext{
		Runtime: runtime,
		Model:   model,
		Effort:  effort,
		Request: req,
	})
	if err != nil {
		return agentic.Plan{}, fmt.Errorf("vendorplugin: vendor %s could not build the launch request for model %q: %w", runtime.VendorID, model.ID, err)
	}
	if err := checkLaunchFidelity(runtime, model, effort, req, launch); err != nil {
		return agentic.Plan{}, err
	}

	return agentic.BuildPlan(r.systems, launch, mode)
}

// selectModel finds the requested row in the vendor's own list, naming what it
// does declare when it cannot. Models() is read ONCE here and the row is
// carried by value: a plugin whose list changed between two reads cannot make
// the validated row and the launched row differ.
func selectModel(runtime Runtime, id ModelID) (Model, error) {
	models := runtime.Vendor.Models()
	declared := make([]string, 0, len(models))
	for _, model := range models {
		if model.ID == id {
			return model, nil
		}
		declared = append(declared, string(model.ID))
	}
	return Model{}, fmt.Errorf("%w: vendor %s has no model %q; it declares %v",
		ErrUnknownModel, runtime.VendorID, id, declared)
}

// resolveEffort validates the caller's effort word against the MODEL's
// vocabulary. It never supplies one.
func resolveEffort(runtime Runtime, model Model, raw string) (string, error) {
	effort := strings.TrimSpace(raw)
	switch model.Effort.Support {
	case agentic.EffortSupportRequired:
		if effort == "" {
			return "", fmt.Errorf("%w: model %q under runtime %s accepts %v and the vendor recommends %q; supply one of them",
				ErrEffortMissing, model.ID, runtime.ID, model.Effort.Vocabulary, model.Effort.Recommended)
		}
		if !model.Effort.Accepts(effort) {
			return "", fmt.Errorf("%w: model %q was given %q, which is not one of %v",
				ErrEffortNotInVocabulary, model.ID, effort, model.Effort.Vocabulary)
		}
		return effort, nil
	default:
		if effort != "" {
			return "", fmt.Errorf("%w: model %q declares no reasoning-effort axis and was given %q; a parameter the model cannot honour is refused rather than dropped",
				ErrEffortNotInVocabulary, model.ID, effort)
		}
		return "", nil
	}
}

// checkLaunchFidelity refuses a launch request that does not carry what was
// admitted.
//
// A vendor's legitimate job here is to ADD: authentication environment, a
// configuration home, whatever its API needs in the child. Redirecting the
// launch is not on that list, and every field checked below is one whose
// silent change produces a run that looks like the one that was asked for and
// is not — the wrong harness, the wrong model, the wrong cost, an unbounded
// budget, a dropped goal, a composition the caller never approved.
func checkLaunchFidelity(runtime Runtime, model Model, effort string, req SpawnRequest, launch agentic.LaunchRequest) error {
	if launch.System != runtime.SystemID {
		return fmt.Errorf("%w: vendor %s returned a launch for agentic system %q, but runtime %s is declared on %q",
			ErrVendorContract, runtime.VendorID, launch.System, runtime.ID, runtime.SystemID)
	}
	if launch.Model.ID != string(model.ID) {
		return fmt.Errorf("%w: vendor %s returned a launch for model %q after %q was admitted",
			ErrVendorContract, runtime.VendorID, launch.Model.ID, model.ID)
	}
	if launch.Model.Effort != model.Effort.Support {
		return fmt.Errorf("%w: vendor %s returned effort support %s for model %q, which declares %s",
			ErrVendorContract, runtime.VendorID, launch.Model.Effort, model.ID, model.Effort.Support)
	}
	if strings.TrimSpace(launch.Effort) != effort {
		return fmt.Errorf("%w: vendor %s returned effort %q after %q was admitted for model %q",
			ErrVendorContract, runtime.VendorID, launch.Effort, effort, model.ID)
	}
	if !reflect.DeepEqual(launch.Goal, req.Goal) {
		return fmt.Errorf("%w: vendor %s changed the launch goal; the objective a run is bound to is the caller's, not the vendor's", ErrVendorContract, runtime.VendorID)
	}
	if !reflect.DeepEqual(launch.Budget, req.Budget) {
		return fmt.Errorf("%w: vendor %s changed the launch budget; a dropped ceiling is an unbounded run that looks bounded", ErrVendorContract, runtime.VendorID)
	}
	if launch.ServiceTier != req.ServiceTier {
		return fmt.Errorf("%w: vendor %s returned service tier %q after %q was requested", ErrVendorContract, runtime.VendorID, launch.ServiceTier, req.ServiceTier)
	}
	if !reflect.DeepEqual(launch.Composition, req.Composition) {
		return fmt.Errorf("%w: vendor %s changed the launch composition; the MCP servers a child can reach are the caller's decision", ErrVendorContract, runtime.VendorID)
	}
	return nil
}

// CheckAvailability asks one vendor whether requests can be made right now,
// and refuses a verdict that contradicts its own evidence.
//
// It is separate from BuildLaunch on purpose: "is this launch expressible" and
// "is this vendor serving right now" are different questions, the second is
// the only one that may read the network or the disk, and a caller deciding
// whether to queue needs it without building a plan.
//
// An error from the plugin is returned as an error, NOT converted into a
// verdict of unknown. A failure to produce a verdict and a verdict of unknown
// are different facts: the second says what was checked.
func CheckAvailability(r *Registry, id VendorID, query AvailabilityQuery) (Availability, error) {
	if r == nil {
		return Availability{}, errors.New("vendorplugin: cannot check availability without a registry")
	}
	vendor, ok := r.Lookup(id)
	if !ok {
		return Availability{}, fmt.Errorf("%w: %s", ErrVendorNotRegistered, id)
	}
	if query.Model != "" {
		found := false
		for _, model := range vendor.Models() {
			if model.ID == query.Model {
				found = true
				break
			}
		}
		if !found {
			return Availability{}, fmt.Errorf("%w: vendor %s has no model %q to answer about", ErrUnknownModel, id, query.Model)
		}
	}
	verdict, err := vendor.Availability(query)
	if err != nil {
		return Availability{}, fmt.Errorf("vendorplugin: vendor %s could not produce an availability verdict: %w", id, err)
	}
	if err := verdict.Validate(); err != nil {
		return Availability{}, fmt.Errorf("%w: vendor %s: %w", ErrVendorContract, id, err)
	}
	return verdict, nil
}
