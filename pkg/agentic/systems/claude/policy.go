package claude

import (
	"fmt"
	"strings"

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
	{Release: "2.1.261", Grammar: agentic.PermissionGrammarV1, YoloSupported: true},
}

// permissionModeFlag is the session policy selector whose VALUES are
// closed under the pinned grammar. The flag itself is spelled here once,
// for the scan to recognize in either value form.
const permissionModeFlag = "--permission-mode"

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
// Two forms refuse. A `--permission-mode` value outside the known six,
// in separate or `=` form, is an unknown policy form
// (ErrNativePolicyUnknown, which the caller maps to usage exit 2): the
// module cannot vouch for a bypass flag beside a mode it cannot name.
// The mapped bypass flag in a flag position is a duplicate
// (ErrPermissionModeDuplicate), in exact or `=` form: the plan would
// otherwise emit it twice, once mapped and once forwarded.
//
// Everything else is forwarded verbatim with no claim: known modes
// (whose conflicts with yolo are Decision 0018 item 4's table, a later
// leaf — this scan classifies, it does not refuse them), and unknown
// top-level flags, which the provider refuses or ignores itself and
// which would break benign forward compatibility if this layer refused
// them.
func scanNativePolicy(args []string) error {
	for _, i := range nativeargs.FlagIndexes(args) {
		el := args[i]
		name, value, hasValue := nativeargs.SplitFlagValue(el)
		if name == permissionModeFlag {
			mode := value
			if !hasValue {
				if i+1 >= len(args) {
					return fmt.Errorf("%w: %s expects a mode value and the arguments end there", agentic.ErrNativePolicyUnknown, permissionModeFlag)
				}
				mode = args[i+1]
			}
			if !knownPermissionModes[mode] {
				return fmt.Errorf("%w: %s mode %q is not one of the six verified under %s", agentic.ErrNativePolicyUnknown, permissionModeFlag, mode, agentic.PermissionGrammarV1)
			}
			continue
		}
		if el == bypassPermissionsFlag || strings.HasPrefix(el, bypassPermissionsFlag+"=") {
			return fmt.Errorf("%w: the native arguments already carry %q; refusing rather than emitting it twice",
				agentic.ErrPermissionModeDuplicate, bypassPermissionsFlag)
		}
	}
	return nil
}
