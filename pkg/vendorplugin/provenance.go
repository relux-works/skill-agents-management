package vendorplugin

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// ValidateLaunchProvenance is the persisted consumer gate. It reconstructs
// the configured engine from the registry's runtime/model declarations and
// resolves that identity through the registry's plugin graph before comparing
// either trusted value with the untrusted JSON projection.
//
// A consumer must not use the projection's configured_engine as its own
// expectation: configured_engine and resolved_engine have the same persistence
// trust boundary, so an attacker can replace both consistently.
func (r *Registry) ValidateLaunchProvenance(projection agentic.LaunchProvenanceV1) error {
	if r == nil {
		return fmt.Errorf("%w: consumer registry is absent", agentic.ErrLaunchProvenanceInvalid)
	}
	if projection.EngineBinding == agentic.EngineBindingNoneV1 {
		return projection.ValidateAgainst(plugin.Ref{}, plugin.Ref{})
	}

	runtimeID, err := NormalizeRuntimeID(projection.Runtime)
	if err != nil {
		return fmt.Errorf("%w: persisted runtime %q is not configured: %v", agentic.ErrLaunchProvenanceMismatch, projection.Runtime, err)
	}
	binding, err := resolveLaunchBinding(r, runtimeID)
	if err != nil {
		return fmt.Errorf("%w: persisted runtime %q has no trusted launch binding: %v", agentic.ErrLaunchProvenanceMismatch, projection.Runtime, err)
	}
	model, err := selectModel(binding, ModelID(projection.Model))
	if err != nil {
		return fmt.Errorf("%w: persisted model %q is not configured for runtime %q: %v", agentic.ErrLaunchProvenanceMismatch, projection.Model, projection.Runtime, err)
	}

	if projection.System != binding.SystemID || projection.Broker != string(binding.VendorID) ||
		projection.Publisher != model.Publisher || projection.Family != model.Family {
		return fmt.Errorf("%w: persisted launch axes system=%q broker=%q publisher=%q family=%q; configured axes system=%q broker=%q publisher=%q family=%q",
			agentic.ErrLaunchProvenanceMismatch,
			projection.System, projection.Broker, projection.Publisher, projection.Family,
			binding.SystemID, binding.VendorID, model.Publisher, model.Family)
	}

	configured, resolved, err := resolveInferenceEngine(r, binding.Engine, model.Engine, plugin.Ref{})
	if err != nil {
		return fmt.Errorf("%w: trusted engine authority for runtime %q model %q refused: %v",
			agentic.ErrLaunchProvenanceMismatch, projection.Runtime, projection.Model, err)
	}
	return projection.ValidateAgainst(configured, resolved)
}
