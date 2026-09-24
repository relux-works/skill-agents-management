package claude

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// verifiedReleases is this environment's rows of the versioned
// provider-capability table (curator-spec Decision 0018 choice 6): the
// tool releases whose yolo mapping this plugin verified, each under the
// grammar version it was verified against.
//
// There is exactly one row: claude 2.1.261, the release this plugin's
// grammar comments were checked against. The mapping it verifies is the
// bypass const in args.go; the grammar it names closes the
// `--permission-mode` values below. A newer release — installed 2.1.274
// included — has no row until it is re-verified, and yolo there fails
// closed while native forwards verbatim.
var verifiedReleases = []agentic.ReleaseCapability{
	{Release: "2.1.261", Grammar: agentic.PermissionGrammarV2, YoloSupported: true},
}

// permissionModeFlag is the session policy selector whose VALUES are
// closed under the pinned grammar. The flag itself is spelled here once,
// for the scan to recognize in either value form.
const permissionModeFlag = "--permission-mode"

const (
	allowDangerouslySkipPermissionsFlag = "--allow-dangerously-skip-permissions"
	restrictedFlag                      = "--restricted"
	printShortFlag                      = "-p"
	printLongFlag                       = "--print"
)

// ClassifyNonInteractiveArgs reports whether the caller suffix selects
// Claude's print form. It uses the same exact release row and permission
// grammar as the yolo conflict scanner; prompt text after `--` is excluded by
// the shared nativeargs flag-position parser.
func (*System) ClassifyNonInteractiveArgs(toolRelease string, suffix []string) (agentic.NativeArgsClassification, error) {
	capability, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease)
	if err != nil {
		return agentic.NativeArgsClassification{}, fmt.Errorf("claude: %w", err)
	}
	for _, index := range nativeargs.FlagIndexes(suffix) {
		name, _, _ := nativeargs.SplitFlagValue(suffix[index])
		if name == printShortFlag || name == printLongFlag {
			return agentic.NativeArgsClassification{Form: agentic.NonInteractiveFormPrint, Grammar: capability.Grammar}, nil
		}
	}
	return agentic.NativeArgsClassification{Grammar: capability.Grammar}, nil
}

// PermissionMapping exposes Claude's provider mapping before launch-plan
// admission. Native maps to no module-owned argument; yolo returns the same
// spelling used by Args, under the grammar verified for this exact release.
func (*System) PermissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	return permissionMapping(toolRelease, mode)
}

func permissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	effective, err := mode.Resolve()
	if err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("claude: %w", err)
	}
	capability, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease)
	if err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("claude: %w", err)
	}
	mapping := agentic.PermissionMapping{Grammar: capability.Grammar}
	if effective == agentic.PermissionModeNative {
		return mapping, nil
	}
	if !capability.YoloSupported {
		return agentic.PermissionMapping{}, fmt.Errorf("claude: refusing yolo: %w: tool release %q documents no bypass flag",
			agentic.ErrPermissionModeUnsupported, capability.Release)
	}
	mapping.Flag = bypassPermissionsFlag
	return mapping, nil
}

// knownPermissionModes is the closed `--permission-mode` value set at
// the pinned release: the six choices `claude --help` lists
// ("acceptEdits", "auto", "bypassPermissions", "manual", "dontAsk",
// "plan" — Decision 0018's verification at 2.1.273, unchanged at
// installed 2.1.274). A seventh value is an unknown native policy form:
// refused, never resolved into a claim about the session's posture.
var knownPermissionModes = map[string]bool{
	"acceptEdits":       true,
	"auto":              true,
	"bypassPermissions": true,
	"manual":            true,
	"dontAsk":           true,
	"plan":              true,
}

// scanNativePolicy classifies the flag positions of the caller's native
// arguments under yolo, against the pinned release's closed grammar.
// nativeargs.FlagIndexes is the only positions reader: prompt text —
// after `--`, or never dash-leading — is never classified, however
// flag-like it reads.
//
// Known `--permission-mode` values, `--allow-dangerously-skip-permissions`,
// and `--restricted` conflict with yolo and are refused with a typed error.
// A value outside the six known permission modes remains an unknown policy
// form: the module cannot vouch for a bypass flag beside a mode it cannot
// name. The mapped bypass flag remains the duplicate refusal, in exact or
// `=` form, because the plan would otherwise emit it twice.
//
// Everything else is forwarded verbatim with no claim, including unknown
// top-level flags. Flag positions come only from nativeargs.FlagIndexes, so
// prompt text after `--` is not inspected.
func scanNativePolicy(args []string) error {
	for _, i := range nativeargs.FlagIndexes(args) {
		el := args[i]
		name, value, hasValue := nativeargs.SplitFlagValue(el)
		if name == permissionModeFlag {
			mode := value
			placement := agentic.NativePolicyPlacementEquals
			if !hasValue {
				if i+1 >= len(args) || args[i+1] == "--" {
					return fmt.Errorf("%w: %s expects a mode value and the arguments end there", agentic.ErrNativePolicyUnknown, permissionModeFlag)
				}
				mode = args[i+1]
				placement = agentic.NativePolicyPlacementSeparateToken
			}
			if !knownPermissionModes[mode] {
				return fmt.Errorf("%w: %s mode %q is not one of the six verified under %s", agentic.ErrNativePolicyUnknown, permissionModeFlag, mode, agentic.PermissionGrammarV2)
			}
			return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}
		}
		if name == bypassPermissionsFlag {
			return fmt.Errorf("%w: the native arguments already carry %q; refusing rather than emitting it twice",
				agentic.ErrPermissionModeDuplicate, bypassPermissionsFlag)
		}
		if name == allowDangerouslySkipPermissionsFlag || name == restrictedFlag {
			placement := agentic.NativePolicyPlacementFlag
			if hasValue {
				placement = agentic.NativePolicyPlacementEquals
			}
			return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}
		}
	}
	return nil
}
