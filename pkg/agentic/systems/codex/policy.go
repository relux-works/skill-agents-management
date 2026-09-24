package codex

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
// There is exactly one row: codex 0.153.2, the release this plugin's
// grammar comments were checked against. The mapping it verifies is the
// bypass const in args.go; the grammar it names closes the `-c` keys
// below. A newer release — installed 0.153.4 included — has no row
// until it is re-verified, and yolo there fails closed while native
// forwards verbatim.
var verifiedReleases = []agentic.ReleaseCapability{
	{Release: "0.153.2", Grammar: agentic.PermissionGrammarV2, YoloSupported: true},
}

// configFlag and configFlagLong are the two spellings of the config
// override flag, `-c, --config <key=value>`. Both name the same key
// grammar, so the scan recognizes both — and only both.
const (
	configFlag     = "-c"
	configFlagLong = "--config"
)

// knownConfigKeys is the closed `-c`/`--config` key set at the pinned
// release. Two keys are this module's own transports (args.go spells
// both); three are Decision 0018 item 4's conflicting policy keys; the
// `mcp_servers.` shape below is the composition grammar's
// (composition.go), which a caller may also override directly. Any
// other key is an unknown native policy form: a `-c` override reaches
// the provider's config surface directly, so an unlisted key is refused
// rather than resolved into a claim about the session's posture.
var knownConfigKeys = map[string]bool{
	"model_reasoning_effort": true,
	"service_tier":           true,
	"approval_policy":        true,
	"sandbox_mode":           true,
	"sandbox_permissions":    true,
}

var knownApprovalValues = map[string]bool{"on-request": true, "never": true}
var knownSandboxValues = map[string]bool{"read-only": true, "workspace-write": true, "danger-full-access": true}

// classifyStoredSandboxMode separates the recognized stored values from the
// ones that relax the sandbox. Keeping the provider's closed value set here
// avoids a second spelling in the settings inspector.
func classifyStoredSandboxMode(mode string) (known, relaxing bool) {
	known = knownSandboxValues[mode]
	return known, known && mode != "read-only"
}

const approveForMeFlag = "--approve-for-me"

// mcpServersKeyPrefix admits the composition-evidenced config shape:
// `mcp_servers.<server>.<field>` overrides name MCP servers the same
// way the composition prefix does, so they are known — and forwarded
// verbatim with no claim, like every other known key.
const mcpServersKeyPrefix = "mcp_servers."

// ClassifyNonInteractiveArgs reports the Codex exec subcommand (or its `e`
// alias) when it is the first positional command in the caller suffix. Known
// root options are parsed using this release's command grammar; an unknown
// or unmodeled root flag returns ErrNativeArgsClassificationIndeterminate
// because its value arity is not established by this classifier. The scan
// also stops at `--`, so prompt text can never become a subcommand.
func (*System) ClassifyNonInteractiveArgs(toolRelease string, suffix []string) (agentic.NativeArgsClassification, error) {
	capability, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease)
	if err != nil {
		return agentic.NativeArgsClassification{}, fmt.Errorf("codex: %w", err)
	}
	form, err := classifyCodexNonInteractiveForm(suffix)
	if err != nil {
		return agentic.NativeArgsClassification{}, fmt.Errorf("codex: %w", err)
	}
	return agentic.NativeArgsClassification{Form: form, Grammar: capability.Grammar}, nil
}

// PermissionMapping exposes Codex's provider mapping before launch-plan
// admission. Native maps to no module-owned argument; yolo returns the same
// spelling used by Args, under the grammar verified for this exact release.
func (*System) PermissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	return permissionMapping(toolRelease, mode)
}

func permissionMapping(toolRelease string, mode agentic.PermissionMode) (agentic.PermissionMapping, error) {
	effective, err := mode.Resolve()
	if err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("codex: %w", err)
	}
	capability, err := agentic.LookupReleaseCapability(verifiedReleases, toolRelease)
	if err != nil {
		return agentic.PermissionMapping{}, fmt.Errorf("codex: %w", err)
	}
	mapping := agentic.PermissionMapping{Grammar: capability.Grammar}
	if effective == agentic.PermissionModeNative {
		return mapping, nil
	}
	if !capability.YoloSupported {
		return agentic.PermissionMapping{}, fmt.Errorf("codex: refusing yolo: %w: tool release %q documents no bypass flag",
			agentic.ErrPermissionModeUnsupported, capability.Release)
	}
	mapping.Flag = bypassApprovalsAndSandboxFlag
	return mapping, nil
}

func classifyCodexNonInteractiveForm(suffix []string) (agentic.NonInteractiveForm, error) {
	for i := 0; i < len(suffix); i++ {
		el := suffix[i]
		if nativeargs.IsSeparator(el) {
			return agentic.NonInteractiveFormNone, nil
		}
		if !nativeargs.IsFlagElement(el) {
			if el == "exec" || el == "e" {
				return agentic.NonInteractiveFormExec, nil
			}
			return agentic.NonInteractiveFormNone, nil
		}
		name, _, hasValue := nativeargs.SplitFlagValue(el)
		if codexRootOptionTakesValue(name) {
			if !hasValue {
				if i+1 >= len(suffix) || nativeargs.IsSeparator(suffix[i+1]) {
					return agentic.NonInteractiveFormNone, nil
				}
				i++
			}
			continue
		}
		if !codexRootOptionIsBoolean(name) {
			// An unknown or unmodeled root flag may take a value. Without its
			// release-pinned arity, a following "exec" could be an option value
			// rather than a subcommand, so this suffix does not establish a known
			// form.
			return agentic.NonInteractiveFormNone, fmt.Errorf("%w: codex root flag %q", agentic.ErrNativeArgsClassificationIndeterminate, name)
		}
	}
	return agentic.NonInteractiveFormNone, nil
}

func codexRootOptionTakesValue(name string) bool {
	switch name {
	case "-C", "--cd", "--add-dir", "-m", "--model", "--local-provider", "-p", "--profile", "-c", "--config", "-a", "--ask-for-approval", "-s", "--sandbox", "--enable", "--disable", "--remote", "--remote-auth-token":
		return true
	default:
		return false
	}
}

func codexRootOptionIsBoolean(name string) bool {
	switch name {
	case "--oss", "--approve-for-me", "--not-so-yolo", bypassApprovalsAndSandboxFlag, yoloAliasFlag, "--dangerously-bypass-hook-trust", "--strict-config", "--search", "--no-alt-screen":
		return true
	default:
		return false
	}
}

// scanNativePolicy classifies the flag positions of the caller's native
// arguments under yolo, against the pinned release's closed grammar.
// nativeargs.FlagIndexes is the only positions reader: prompt text —
// after `--`, or never dash-leading — is never classified, however
// flag-like it reads.
//
// Decision 0018 conflicts are refused with NativePolicyConflictError:
// `-a`/`--ask-for-approval`, `-s`/`--sandbox`, `--approve-for-me`, every
// `--dangerously-bypass-*` selector, and the three policy config keys.
// Option values are checked against the pinned closed values before a
// conflict is returned. Unknown values, malformed config overrides and
// unknown config keys fail closed with ErrNativePolicyUnknown. The
// module-mapped bypass flag retains ErrPermissionModeDuplicate.
//
// Everything else is forwarded verbatim with no claim, including known
// non-conflicting config keys and unknown top-level flags. The scan only
// reads flag positions supplied by nativeargs.FlagIndexes, so `exec`
// placement is visible while text after `--` remains prompt.
func scanNativePolicy(args []string) error {
	for _, i := range nativeargs.FlagIndexes(args) {
		el := args[i]
		name, value, hasValue := nativeargs.SplitFlagValue(el)
		if name == configFlagLong || name == configFlag {
			override := value
			placement := agentic.NativePolicyPlacementEquals
			if !hasValue {
				if i+1 >= len(args) || args[i+1] == "--" {
					return fmt.Errorf("%w: %s expects a key=value override and the arguments end there", agentic.ErrNativePolicyUnknown, name)
				}
				override = args[i+1]
				placement = agentic.NativePolicyPlacementSeparateToken
			}
			if err := checkConfigOverride(override); err != nil {
				return err
			}
			key, _, _ := strings.Cut(override, "=")
			if isConflictingConfigKey(key) {
				return &agentic.NativePolicyConflictError{Selector: key, Placement: placement}
			}
			continue
		}
		if isAttachedConfigValue(el) {
			override := strings.TrimPrefix(el, configFlag)
			if err := checkConfigOverride(override); err != nil {
				return err
			}
			key, _, _ := strings.Cut(override, "=")
			if isConflictingConfigKey(key) {
				return &agentic.NativePolicyConflictError{Selector: key, Placement: agentic.NativePolicyPlacementAttachedShort}
			}
			continue
		}
		if name == "-a" || name == "--ask-for-approval" {
			optionValue, placement, err := nativePolicyOptionValue(args, i, value, hasValue, name)
			if err != nil {
				return err
			}
			if !knownApprovalValues[optionValue] {
				return fmt.Errorf("%w: %s value %q is not one of the values verified under %s", agentic.ErrNativePolicyUnknown, name, optionValue, agentic.PermissionGrammarV2)
			}
			return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}
		}
		if name == "-s" || name == "--sandbox" {
			optionValue, placement, err := nativePolicyOptionValue(args, i, value, hasValue, name)
			if err != nil {
				return err
			}
			if !knownSandboxValues[optionValue] {
				return fmt.Errorf("%w: %s value %q is not one of the values verified under %s", agentic.ErrNativePolicyUnknown, name, optionValue, agentic.PermissionGrammarV2)
			}
			return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}
		}
		if name == approveForMeFlag {
			return &agentic.NativePolicyConflictError{Selector: name, Placement: nativePolicyFlagPlacement(hasValue)}
		}
		if name == bypassApprovalsAndSandboxFlag {
			return fmt.Errorf("%w: the native arguments already carry %q; refusing rather than emitting it twice",
				agentic.ErrPermissionModeDuplicate, bypassApprovalsAndSandboxFlag)
		}
		if strings.HasPrefix(name, "--dangerously-bypass-") {
			return &agentic.NativePolicyConflictError{Selector: name, Placement: nativePolicyFlagPlacement(hasValue)}
		}
	}
	return nil
}

func nativePolicyOptionValue(args []string, index int, value string, hasValue bool, selector string) (string, agentic.NativePolicyPlacement, error) {
	if hasValue {
		if value == "" {
			return "", "", fmt.Errorf("%w: %s requires a value", agentic.ErrNativePolicyUnknown, selector)
		}
		return value, agentic.NativePolicyPlacementEquals, nil
	}
	if index+1 >= len(args) || args[index+1] == "--" || nativeargs.IsFlagElement(args[index+1]) {
		return "", "", fmt.Errorf("%w: %s expects a value and the arguments end there", agentic.ErrNativePolicyUnknown, selector)
	}
	return args[index+1], agentic.NativePolicyPlacementSeparateToken, nil
}

func nativePolicyFlagPlacement(hasValue bool) agentic.NativePolicyPlacement {
	if hasValue {
		return agentic.NativePolicyPlacementEquals
	}
	return agentic.NativePolicyPlacementFlag
}

func isConflictingConfigKey(key string) bool {
	switch key {
	case "approval_policy", "sandbox_mode", "sandbox_permissions":
		return true
	default:
		return false
	}
}

// isAttachedConfigValue reports whether el is `-c` with its override
// attached, `-ckey=value`: a short flag with a value-taking definition
// parses its remainder as the value (observed against installed 0.153.4:
// `-cmodel_reasoning_effort="high" --help` prints help, exit 0, where a
// bogus flag errors). Long elements never take this path however they
// start: `--configx` is an unknown long, not `--config` with an
// attached value.
func isAttachedConfigValue(el string) bool {
	return len(el) > len(configFlag) && strings.HasPrefix(el, configFlag) && !strings.HasPrefix(el, "--")
}

// checkConfigOverride classifies one `-c` override value: `key=value`
// with a known key passes, and everything else — no `=`, a key with
// whitespace, an unlisted key — is refused as usage.
func checkConfigOverride(override string) error {
	key, _, ok := strings.Cut(override, "=")
	if !ok {
		return fmt.Errorf("%w: malformed codex -c override %q: want key=value", agentic.ErrNativePolicyUnknown, override)
	}
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return fmt.Errorf("%w: malformed codex -c key %q", agentic.ErrNativePolicyUnknown, key)
	}
	if !knownConfigKeys[key] && !strings.HasPrefix(key, mcpServersKeyPrefix) {
		return fmt.Errorf("%w: codex -c key %q is not one of the keys verified under %s", agentic.ErrNativePolicyUnknown, key, agentic.PermissionGrammarV2)
	}
	return nil
}
