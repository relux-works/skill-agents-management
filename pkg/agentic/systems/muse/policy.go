package muse

import (
	"errors"
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ErrMuseHelpEvidenceRequired refuses a yolo mapping requested without
// the bounded help evidence it resolves from. The public no-evidence
// PermissionMapping query reports it for yolo; BuildPlan obtains the
// evidence by probing the selected binary instead of refusing.
var ErrMuseHelpEvidenceRequired = errors.New("muse: yolo mapping requires bounded help evidence")

// PermissionMapping exposes the Muse posture mapping WITHOUT evidence:
// native contributes no flag, and yolo reports evidence-required. It
// answers what the posture means, not whether any binary supports it;
// the evidence-backed mapping below is what plans drive.
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
	// The release claim travels with the request for the sealer's
	// attestation cross-check; the mapping itself no longer consults it.
	if effective == agentic.PermissionModeNative {
		return agentic.PermissionMapping{Grammar: agentic.PermissionGrammarV1}, nil
	}
	return agentic.PermissionMapping{}, fmt.Errorf("muse: refusing yolo: %w: probe the selected binary help for the %q declaration",
		ErrMuseHelpEvidenceRequired, museYoloFlag)
}

// permissionMappingWithEvidence maps the interactive posture from parsed
// help evidence: the internal mapper BuildPlan drives. Native forwards
// with no evidence consulted; yolo requires the declared bypass flag.
// No version table gates either posture: a novel well-formed build maps
// exactly like a long-shipped one when its help declares the flag, and
// a help text without the declaration refuses typed as unsupported on
// that binary, never as a release-list miss.
func permissionMappingWithEvidence(declaration helpDeclaration, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	var effective agentic.PermissionMode
	if value, err := mode.Resolve(); err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("muse: %w", err)
	} else {
		effective = value
	}
	mapping := agentic.PermissionMapping{Grammar: agentic.PermissionGrammarV1}
	if effective == agentic.PermissionModeNative {
		return mapping, nil
	}
	if !declaration.declared {
		return agentic.PermissionMapping{}, fmt.Errorf("muse: refusing yolo: %w: selected binary help declares no %q option",
			agentic.ErrPermissionModeUnsupported, museYoloFlag)
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
