package claude

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// verifiedReleases is this environment's rows of the versioned
// provider-capability table (curator-spec Decision 0018 choice 6): the
// tool releases whose yolo mapping this plugin verified, each under the
// grammar version it was verified against.
//
// The 2.1.312 row was qualified against the installed binary: its help, the
// Commander parser and the eager settings scan (the pinned comparison tests
// run the native functions), the six `--permission-mode` values and the
// bypass flag. Its V3 grammar retains V2 and refuses the permission selectors
// and explicit settings whose interaction with the mapped bypass that
// qualification did not prove. The 2.1.261 row is retired under the owner's
// support window (harness releases from about the last two weeks,
// 2026-10-29): this module's option tables describe in-window releases only,
// so a retired release fails closed under yolo like any other release without
// a row, while native forwards verbatim. Stay on an older module release to
// run an older harness.
var verifiedReleases = []agentic.ReleaseCapability{
	{Release: "2.1.312", Grammar: agentic.PermissionGrammarV3, YoloSupported: true},
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
	inheritPermissionModeFlag           = "--inherit-permission-mode"
	planModeRequiredFlag                = "--plan-mode-required"
	permissionPromptsFlag               = "--permission-prompts"
	enableAutoModeFlag                  = "--enable-auto-mode"
	managedSettingsFlag                 = "--managed-settings"
	projectConfigRootFlag               = "--project-config-root"
)

// v3UnqualifiedSelectors are 2.1.312 permission selectors that parse, but
// whose interaction with the mapped bypass was not qualified: a mode carried
// from a parent session, required plan mode, who answers prompts, the
// deprecated auto-mode opt-in, and the two explicit configuration sources
// this module does not inspect, policy-tier managed settings and an alternate
// project configuration root. Either can carry permission settings that turn
// the bypass off. V3 refuses them under yolo; native forwards them verbatim.
var v3UnqualifiedSelectors = map[string]bool{
	inheritPermissionModeFlag: true,
	planModeRequiredFlag:      true,
	permissionPromptsFlag:     true,
	enableAutoModeFlag:        true,
	managedSettingsFlag:       true,
	projectConfigRootFlag:     true,
}

// v3QualifiedSettingsKeys are the top-level members of an explicit settings
// source V3 admits beside the mapped bypass, each with a qualified shape: the
// two string annotations `$schema` and `model`, `permissions` (members below)
// and an empty `env`. Everything else is unqualified until shown not to touch
// the bypass. 2.1.312 runs flag-settings `hooks` before mode evaluation, so a
// PreToolUse hook answering "ask" or "deny" overrides the bypass per call; a
// plugin, helper or other executable customization is unqualified for the
// same reason, and so is any key this release adds later.
var v3QualifiedSettingsKeys = map[string]bool{
	"$schema":     true,
	"model":       true,
	"permissions": true,
	"env":         true,
}

// v3QualifiedPermissionKeys are the members of an explicit settings
// `permissions` object V3 admits beside the mapped bypass: the rule lists and
// extra directories. Any other member is unqualified. 2.1.312's permission
// resolver skips the bypass mode when `disableBypassPermissionsMode` is
// "disable", and `defaultMode` selects another mode.
var v3QualifiedPermissionKeys = map[string]bool{
	"allow":                 true,
	"deny":                  true,
	"ask":                   true,
	"additionalDirectories": true,
}

// scanYoloSettings inspects the effective explicit --settings source, inline
// JSON or a file, under V3 yolo, and refuses before launch any top-level
// member outside v3QualifiedSettingsKeys, a qualified member of the wrong
// shape, an unqualified permissions member, or any member of a non-empty
// top-level `env`. 2.1.312
// applies a flag-settings `env` to its own process before it resolves the
// permission mode, and CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1 there forces mode
// "default" over the bypass flag; no env member is qualified yet, and an env
// variable can also redirect configuration roots. It reads the same source
// checkSettingsPolicy selects, rewrites nothing, and leaves implicit user,
// project and managed configuration and the launch environment outside this
// argv policy.
func scanYoloSettings(req agentic.LaunchRequest, grammar agentic.PermissionGrammarVersion) error {
	if grammar != agentic.PermissionGrammarV3 {
		return nil
	}
	var source string
	if value, present, err := effectiveSettingsSource(req.NativeArgs); err != nil {
		return err
	} else if !present {
		return nil
	} else {
		source = value
	}
	settings, kind := readSettingsObject(source, req.WorkDir)
	if kind != "" {
		return settingsPolicyRefusal(kind)
	}
	for _, key := range sortedKeys(settings) {
		if !v3QualifiedSettingsKeys[key] {
			return fmt.Errorf("%w: claude settings member %q is not qualified under %s", agentic.ErrNativePolicyUnknown, key, grammar)
		}
	}
	for _, key := range []string{"$schema", "model"} {
		var text string
		if raw, present := settings[key]; present && (string(raw) == "null" || json.Unmarshal(raw, &text) != nil) {
			return settingsPolicyRefusal(agentic.SettingsPolicyInvalid)
		}
	}
	if raw, present := settings["env"]; present {
		var env map[string]json.RawMessage
		if json.Unmarshal(raw, &env) != nil || env == nil {
			return settingsPolicyRefusal(agentic.SettingsPolicyInvalid)
		}
		if keys := sortedKeys(env); len(keys) > 0 {
			return fmt.Errorf("%w: claude settings env member %q is not qualified under %s", agentic.ErrNativePolicyUnknown, keys[0], grammar)
		}
	}
	raw, present := settings["permissions"]
	if !present {
		return nil
	}
	var permissions map[string]json.RawMessage
	if json.Unmarshal(raw, &permissions) != nil || permissions == nil {
		return settingsPolicyRefusal(agentic.SettingsPolicyInvalid)
	}
	for _, key := range sortedKeys(permissions) {
		if !v3QualifiedPermissionKeys[key] {
			return fmt.Errorf("%w: claude settings permissions member %q is not qualified under %s", agentic.ErrNativePolicyUnknown, key, grammar)
		}
	}
	return nil
}

// sortedKeys lists an object's member names in byte order, so a refusal names
// the same member every run.
func sortedKeys(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ClassifyNonInteractiveArgs reports whether the caller suffix selects
// Claude's print form. It uses the same exact release row and permission
// grammar as the yolo conflict scanner; prompt text after `--` is excluded by
// the shared nativeargs flag-position parser.
func (*System) ClassifyNonInteractiveArgs(toolRelease string, suffix []string) (agentic.NativeArgsClassification, error) {
	var capability agentic.ReleaseCapability

	if capabilityValue, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease); err != nil {
		return agentic.NativeArgsClassification{}, fmt.Errorf("claude: %w", err)
	} else {
		capability = capabilityValue
	}
	for _, index := range claudeFlagIndexes(suffix) {
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
	var effective agentic.PermissionMode

	if effectiveValue, err := mode.Resolve(); err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("claude: %w", err)
	} else {
		effective = effectiveValue
	}
	var capability agentic.ReleaseCapability

	if capabilityValue, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease); err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("claude: %w", err)
	} else {
		capability = capabilityValue
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
// claudeFlagIndexes is the only positions reader: prompt text —
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
// Under V3 the v3UnqualifiedSelectors are refused as unknown policy forms.
// Everything else is forwarded verbatim with no claim, including unknown
// top-level flags. Flag positions come only from claudeFlagIndexes, so
// prompt text after `--` is not inspected.
func scanNativePolicy(args []string, grammar agentic.PermissionGrammarVersion) error {
	for _, i := range claudeFlagIndexes(args) {
		el := args[i]
		name, value, hasValue := nativeargs.SplitFlagValue(el)
		if grammar == agentic.PermissionGrammarV3 && v3UnqualifiedSelectors[name] {
			return fmt.Errorf("%w: claude policy selector %q is not qualified under %s", agentic.ErrNativePolicyUnknown, name, grammar)
		}
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
				return fmt.Errorf("%w: %s mode %q is not one of the six verified under %s", agentic.ErrNativePolicyUnknown, permissionModeFlag, mode, grammar)
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
