package pinative

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// verifiedReleases is this environment's rows of the versioned
// provider-capability table (curator-spec Decision 0018 choice 6).
//
// There is exactly one row: pi 0.84.2, carrying YoloSupported false —
// that release documents no permission-bypass flag, so yolo at the
// verified release is refused as unsupported, and any other release (or
// an empty one) fails closed earlier, as drift. The release string
// matches pi's row because both environments run the same tool
// release; the rows are still per-environment facts (the table keys
// environment AND release), which is why each plugin holds its own.
var verifiedReleases = []agentic.ReleaseCapability{
	{Release: "0.84.2", Grammar: agentic.PermissionGrammarV1, YoloSupported: false},
}

const (
	printShortFlag = "-p"
	printLongFlag  = "--print"
)

// ClassifyNonInteractiveArgs reports whether the caller suffix selects Pi's
// one-shot print form. It verifies the same release row as permission policy
// handling and delegates flag positions to the shared native-argument grammar.
func (*System) ClassifyNonInteractiveArgs(toolRelease string, suffix []string) (agentic.NativeArgsClassification, error) {
	capability, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease)
	if err != nil {
		return agentic.NativeArgsClassification{}, fmt.Errorf("pinative: %w", err)
	}
	for _, index := range nativeargs.FlagIndexes(suffix) {
		name, _, _ := nativeargs.SplitFlagValue(suffix[index])
		if name == printShortFlag || name == printLongFlag {
			return agentic.NativeArgsClassification{Form: agentic.NonInteractiveFormPrint, Grammar: capability.Grammar}, nil
		}
	}
	return agentic.NativeArgsClassification{Grammar: capability.Grammar}, nil
}

// PermissionMapping exposes Pi's verified native mapping before launch-plan
// admission. Native maps to no module-owned argument. Pi 0.84.2 documents no
// yolo bypass flag, so that mode keeps the existing typed refusal.
func (*System) PermissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	return permissionMapping(toolRelease, mode)
}

func permissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	effective, err := mode.Resolve()
	if err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("pinative: %w", err)
	}
	capability, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease)
	if err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("pinative: %w", err)
	}
	if effective == agentic.PermissionModeYolo {
		return agentic.PermissionMapping{}, fmt.Errorf("pinative: refusing yolo: %w: pi 0.84.2 documents no interactive permission-bypass flag; --approve trusts project-local files for this run and is not equivalent",
			agentic.ErrPermissionModeUnsupported)
	}
	return agentic.PermissionMapping{Grammar: capability.Grammar}, nil
}
