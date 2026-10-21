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

// exclusionForm names one exclusion spelling. Curator qualifies each form
// separately, so a build can carry one form's row and not the other's.
type exclusionForm int

const (
	// exclusionFormExec is `--no-foreign-personal-context` on exec and dry-run.
	exclusionFormExec exclusionForm = iota
	// exclusionFormInteractive is `-c context.foreign_personal_skills=false`
	// on the TUI.
	exclusionFormInteractive
)

func (f exclusionForm) String() string {
	if f == exclusionFormInteractive {
		return "interactive"
	}
	return "exec"
}

// foreignContextExclusionQualified maps each form to the releases, and the
// exact build per release, on which a Curator qualification record
// (relux-works/curator docs/qualification) verified that form's exclusion
// under environments §7.1 P1-P4. Rows come only from those records:
//
//   - 1.4.7-R6101.1: both forms.
//   - 1.4.10-R6427.1 (curator 49d69b1c) and 1.4.11-R6667.1 (curator
//     142bbda9): exec only. Interactive P4 fails on both: the boolean
//     override leaves one foreign skill loaded.
//   - 1.4.8: neither form.
var foreignContextExclusionQualified = map[exclusionForm]map[string]string{
	exclusionFormExec: {
		"1.4.7":  "1.4.7-R6101.1",
		"1.4.10": "1.4.10-R6427.1",
		"1.4.11": "1.4.11-R6667.1",
	},
	exclusionFormInteractive: {
		"1.4.7": "1.4.7-R6101.1",
	},
}

// exclusionFormsFor returns the forms a launch mode must have qualified. A
// mode Muse does not map to one form needs every form, so an undeclared mode
// can never pick the more permissive table.
func exclusionFormsFor(mode agentic.LaunchMode) []exclusionForm {
	switch mode {
	case agentic.LaunchModeExec, agentic.LaunchModeDryRun:
		return []exclusionForm{exclusionFormExec}
	case agentic.LaunchModeInteractive:
		return []exclusionForm{exclusionFormInteractive}
	default:
		return []exclusionForm{exclusionFormExec, exclusionFormInteractive}
	}
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
// names, is not qualified for every form the mode needs. An unknown release
// refuses too.
func validateForeignContextExclusion(req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	if !requiresForeignContextExclusion(req) {
		return nil
	}
	for _, form := range exclusionFormsFor(mode) {
		build, qualified := foreignContextExclusionQualified[form][req.ToolRelease]
		if !qualified {
			return fmt.Errorf("%w: release %q (%s form)", ErrForeignContextExclusionUnqualified, req.ToolRelease, form)
		}
		if req.FrozenToolBuild != "" && req.FrozenToolBuild != build {
			return fmt.Errorf("%w: build %q (qualified %q, %s form)", ErrForeignContextExclusionUnqualified, req.FrozenToolBuild, build, form)
		}
	}
	return nil
}

// validateForeignContextExclusionInSomeForm is the mode-less check ChildEnv
// runs. The child environment is the same for every form, and BuildPlan calls
// ChildEnv for every mode, so requiring every form here would refuse an exec
// plan the mode-aware gate admits. It still refuses a release or frozen build
// that no form qualifies. The per-form refusal stays with the callers that
// know the mode: BuildPlan's ValidateCuratorContext, Args (the only argv
// builder, so a direct plugin caller cannot produce the argv either) and the
// interactive seal.
func validateForeignContextExclusionInSomeForm(req agentic.LaunchRequest) error {
	if !requiresForeignContextExclusion(req) {
		return nil
	}
	for _, form := range []exclusionForm{exclusionFormExec, exclusionFormInteractive} {
		build, qualified := foreignContextExclusionQualified[form][req.ToolRelease]
		if qualified && (req.FrozenToolBuild == "" || req.FrozenToolBuild == build) {
			return nil
		}
	}
	return fmt.Errorf("%w: release %q build %q (no form)", ErrForeignContextExclusionUnqualified, req.ToolRelease, req.FrozenToolBuild)
}

// foreignContextExclusionBuildQualified is the seal-time check against the
// build the selected binary attests, for one form.
func foreignContextExclusionBuildQualified(form exclusionForm, release, build string) bool {
	qualified, ok := foreignContextExclusionQualified[form][release]
	return ok && qualified == build
}
