package muse

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// verifiedReleases is Muse's per-release permission table. The rows are
// Muse Code 1.4.1 and 1.4.2, whose pinned TUI help documents --yolo. Later
// releases must be verified before this table can map yolo for them.
var verifiedReleases = []agentic.ReleaseCapability{
	{Release: "1.4.1", Grammar: agentic.PermissionGrammarV1, YoloSupported: true},
	{Release: "1.4.2", Grammar: agentic.PermissionGrammarV1, YoloSupported: true},
}

// verifiedBuilds is the closed list of module-verified Muse builds the
// interactive exec-plan sealer binds: full build identities (release plus
// revision), not the short triples the permission table keys. A binary
// attesting any other build — including an unpinned revision of a
// verified triple — seals nothing and refuses at plan time.
var verifiedBuilds = []string{"1.4.1-R4503.1", "1.4.2-R4684.1"}

// PermissionMapping exposes the release-pinned Muse posture mapping without
// building a launch plan. Native contributes no flag; yolo maps to Muse's
// documented bypass flag for a verified release.
func (*System) PermissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	return permissionMapping(toolRelease, mode)
}

func permissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	var effective agentic.PermissionMode
	if value, err := mode.Resolve(); err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("muse: %w", err)
	} else {
		effective = value
	}
	var capability agentic.ReleaseCapability
	if value, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease); err != nil {
		// Keep the specific release-drift classification used by the other
		// harnesses while also classifying the absent mapping as unsupported.
		return agentic.PermissionMapping{}, fmt.Errorf("muse: refusing yolo: %w: %w",
			agentic.ErrPermissionModeUnsupported, err)
	} else {
		capability = value
	}
	mapping := agentic.PermissionMapping{Grammar: capability.Grammar}
	if effective == agentic.PermissionModeNative {
		return mapping, nil
	}
	if !capability.YoloSupported {
		return agentic.PermissionMapping{}, fmt.Errorf("muse: refusing yolo: %w: tool release %q documents no bypass flag",
			agentic.ErrPermissionModeUnsupported, capability.Release)
	}
	mapping.Flag = museYoloFlag
	return mapping, nil
}

// scanMuseNativePolicy refuses a caller suffix that already selects the same
// complete bypass posture that yolo would add. The explicit Muse yolo flag is
// duplicate in any active flag position; the two bare switches together are
// its documented equivalent. A lone switch is not a complete posture, and
// text after -- is prompt data under nativeargs' shared parsing rule.
func scanMuseNativePolicy(args []string) error {
	seenDisableApproval := false
	seenDisableSandbox := false
	for _, index := range nativeargs.FlagIndexes(args) {
		name, _, hasValue := nativeargs.SplitFlagValue(args[index])
		switch name {
		case museYoloFlag:
			return fmt.Errorf("%w: native arguments already carry %q; refusing rather than emitting it twice",
				agentic.ErrPermissionModeDuplicate, museYoloFlag)
		case museDisableApprovalFlag:
			seenDisableApproval = seenDisableApproval || !hasValue
		case museDisableSandboxFlag:
			seenDisableSandbox = seenDisableSandbox || !hasValue
		}
	}
	if seenDisableApproval && seenDisableSandbox {
		return fmt.Errorf("%w: native arguments already carry both %q and %q; refusing rather than emitting an equivalent bypass posture twice",
			agentic.ErrPermissionModeDuplicate, museDisableApprovalFlag, museDisableSandboxFlag)
	}
	return nil
}
