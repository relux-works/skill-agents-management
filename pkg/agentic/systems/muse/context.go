package muse

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ValidateCuratorContext is the Muse channel-layer gate for a typed Curator
// fragment. BuildPlan invokes it before any launch surface is built. ChildEnv
// invokes the same pure builder for the fragment boundary, and Args repeats
// the per-form exclusion check, so direct plugin callers receive the same
// refusals.
//
// The helper is deliberately NOT named applyMuseCuratorContext: the mutant
// runner derives unselected system-prompt channel members from every
// apply*CuratorContext that skips nonmatching descriptors, and muse refuses
// every system-prompt channel, so no selection loop can exist here.
func (*System) ValidateCuratorContext(req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	if err := validateMuseCuratorContext(req); err != nil {
		return err
	}
	return validateForeignContextExclusion(req, mode)
}

// validateMuseCuratorContext validates the request-side fragment boundary
// and the Muse combination gates: the fragment environment must be muse; a
// Curator-managed launch must not combine with a managed network scope (the
// network adapter materializes a private XDG config root, which would shadow
// the fragment's config value while data, state and cache still point at the
// managed layout, and would erase an established root-context fact). The
// exclusion qualification is per form, so it is not checked here: each caller
// runs it with the launch mode it knows.
func validateMuseCuratorContext(req agentic.LaunchRequest) error {
	if err := agentic.ValidateCuratorContext(req.Context, req.Home); err != nil {
		return err
	}
	context := req.Context
	if !agentic.CuratorFragmentUsesXDGHomes(context.Environment) {
		return fmt.Errorf("%w: Muse requires the muse fragment environment", agentic.ErrCuratorContextUnsupported)
	}
	if !req.Network.IsZero() {
		return fmt.Errorf("%w: Muse Curator context cannot combine with a managed network scope", agentic.ErrCuratorContextUnsupported)
	}
	return nil
}
