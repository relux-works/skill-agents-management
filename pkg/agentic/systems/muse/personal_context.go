package muse

import (
	"errors"
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ErrForeignContextExclusionUnqualified refuses a launch whose Curator
// fragment requires the foreign-personal-context exclusion on a Muse build
// whose exclusion behavior has not been qualified (curator-spec environments
// §7.9 "Muse qualification order"). The exclusion is never dropped silently.
var ErrForeignContextExclusionUnqualified = errors.New("muse: required foreign personal context exclusion is unqualified on the selected build")

// foreignContextExclusionQualified maps each Muse release to the exact build
// on which environments §7.1 P1-P4 verified both exclusion spellings. 1.4.8
// stays absent until Curator's 1.4.8 P1-P4 replay passes.
var foreignContextExclusionQualified = map[string]string{
	"1.4.7": "1.4.7-R6101.1",
}

// requiresForeignContextExclusion reports whether the typed Curator fragment
// carries the v4 root-context fact. It is the only input: no file under the
// managed home is stat'ed, opened or recognized to decide exclusion
// (environments §10.2), and absence never permits filesystem inference.
func requiresForeignContextExclusion(req agentic.LaunchRequest) bool {
	return req.Context != nil && req.Context.RootContext != nil && req.Context.RootContext.Exclusive
}

// validateForeignContextExclusion refuses before any launch surface is built
// when the fact is present and the caller's release, or a frozen build it
// names, is not a qualified exclusion build. An unknown release refuses too.
func validateForeignContextExclusion(req agentic.LaunchRequest) error {
	if !requiresForeignContextExclusion(req) {
		return nil
	}
	build, qualified := foreignContextExclusionQualified[req.ToolRelease]
	if !qualified {
		return fmt.Errorf("%w: release %q", ErrForeignContextExclusionUnqualified, req.ToolRelease)
	}
	if req.FrozenToolBuild != "" && req.FrozenToolBuild != build {
		return fmt.Errorf("%w: build %q (qualified %q)", ErrForeignContextExclusionUnqualified, req.FrozenToolBuild, build)
	}
	return nil
}

// foreignContextExclusionBuildQualified is the seal-time check against the
// build the selected binary attests.
func foreignContextExclusionBuildQualified(release, build string) bool {
	qualified, ok := foreignContextExclusionQualified[release]
	return ok && qualified == build
}
