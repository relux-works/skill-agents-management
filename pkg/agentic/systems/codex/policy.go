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
	{Release: "0.153.2", Grammar: agentic.PermissionGrammarV1, YoloSupported: true},
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
// both); three are Decision 0018 item 4's policy keys; the
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

// mcpServersKeyPrefix admits the composition-evidenced config shape:
// `mcp_servers.<server>.<field>` overrides name MCP servers the same
// way the composition prefix does, so they are known — and forwarded
// verbatim with no claim, like every other known key.
const mcpServersKeyPrefix = "mcp_servers."

// scanNativePolicy classifies the flag positions of the caller's native
// arguments under yolo, against the pinned release's closed grammar.
// nativeargs.FlagIndexes is the only positions reader: prompt text —
// after `--`, or never dash-leading — is never classified, however
// flag-like it reads.
//
// A `-c`/`--config` key outside the known set, in separate, `=`, or
// attached-short (`-ckey=value`) form, is an unknown policy form
// (ErrNativePolicyUnknown, which the caller maps to usage exit 2): the
// module cannot vouch for a bypass flag beside a config override it
// cannot name. So is a malformed override — a missing value, or a value
// without `key=`: unclassifiable fails closed. The mapped bypass flag
// in a flag position is a duplicate (ErrPermissionModeDuplicate), in
// exact or `=` form: the plan would otherwise emit it twice.
//
// Everything else is forwarded verbatim with no claim: known keys
// (whose conflicts with yolo are Decision 0018 item 4's table, a later
// leaf — this scan classifies, it does not refuse them), and unknown
// top-level flags, which the provider refuses itself (`error:
// unexpected argument`) and which would break benign forward
// compatibility if this layer refused them.
func scanNativePolicy(args []string) error {
	for _, i := range nativeargs.FlagIndexes(args) {
		el := args[i]
		name, value, hasValue := nativeargs.SplitFlagValue(el)
		if name == configFlagLong || name == configFlag {
			override := value
			if !hasValue {
				if i+1 >= len(args) {
					return fmt.Errorf("%w: %s expects a key=value override and the arguments end there", agentic.ErrNativePolicyUnknown, name)
				}
				override = args[i+1]
			}
			if err := checkConfigOverride(override); err != nil {
				return err
			}
			continue
		}
		if isAttachedConfigValue(el) {
			if err := checkConfigOverride(strings.TrimPrefix(el, configFlag)); err != nil {
				return err
			}
			continue
		}
		if el == bypassApprovalsAndSandboxFlag || strings.HasPrefix(el, bypassApprovalsAndSandboxFlag+"=") {
			return fmt.Errorf("%w: the native arguments already carry %q; refusing rather than emitting it twice",
				agentic.ErrPermissionModeDuplicate, bypassApprovalsAndSandboxFlag)
		}
	}
	return nil
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
		return fmt.Errorf("%w: codex -c key %q is not one of the keys verified under %s", agentic.ErrNativePolicyUnknown, key, agentic.PermissionGrammarV1)
	}
	return nil
}
