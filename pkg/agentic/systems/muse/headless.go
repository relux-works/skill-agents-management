package muse

import (
	"errors"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ErrMuseHeadlessApproval refuses a headless plan without the fixed bypass
// posture. Approval requires an interactive owner and would stall the child.
var ErrMuseHeadlessApproval = errors.New("muse: approval would block a headless child")

// requireHeadlessBypass checks the actual argv at the plan-sealing boundary,
// including the dry-run mirror, before BuildPlan can return a launchable value.
func requireHeadlessBypass(plan agentic.Plan) error {
	if plan.Mode != agentic.LaunchModeExec && plan.Mode != agentic.LaunchModeDryRun {
		return nil
	}
	for _, i := range nativeargs.FlagIndexes(plan.Argv) {
		if plan.Argv[i] == museYoloFlag {
			return nil
		}
	}
	return ErrMuseHeadlessApproval
}
