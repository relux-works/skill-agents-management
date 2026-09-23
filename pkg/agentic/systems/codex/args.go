package codex

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE CODEX CLI FLAGS ARE SPELLED.
//
// The extraction source paid a whole task (skill-project-management,
// TASK-260817-1v9v95) to collapse three independently-drifted spellings —
// buildCodexCommand, BuildArgs' Codex case, and cmd.managedCodexSpawnArgs —
// into one CodexArgs. They had already drifted in production: the managed
// session spelled `--model` / `--sandbox danger-full-access` /
// `--ask-for-approval never` where the other two spelled `-m` /
// `--dangerously-bypass-approvals-and-sandbox` for the same intent, so two
// codex launches from one binary disagreed about what they were asking for.
//
// The unification survives the port as ONE function. argvguard_test.go fails
// the build if a second site in this module starts spelling codex flags, and
// it proves it can by narrowing itself onto this file's own Args.

// LaunchModeArgs is the pair of argv grammars codex has, expressed against
// this repository's launch modes.
//
// agentic.LaunchModeExec and agentic.LaunchModeDryRun map onto the SAME
// grammar, which is the source's shape restated rather than a simplification:
// the source's codexDryRunArgs body is `return CodexArgs(cfg,
// CodexLaunchModeExec)`. Codex is the system that proves why the core made
// dry-run a mode instead of a sibling method — there is nothing for a dry run
// to spell differently, so there was nothing for a second function to get
// wrong except by existing.
//
// agentic.LaunchModeManagedSession is the provider-args FRAGMENT handed to an
// external composer that owns the PTY. It is not a complete argv and this
// module never execs it.
//
// agentic.LaunchModeInteractive (curator-spec Decision 0013 §5) is the
// interactive `codex` session a human drives, as a COMPLETE argv the launcher
// hands to a terminal: `-m <model>` plus the `-c model_reasoning_effort=…`
// override when an effort was requested — and, when the request carries
// PermissionMode "yolo", the ONE bypass flag below (curator-spec Decision
// 0018's codex_cli row). Nothing else: no `exec`, no sandbox or approval
// policy, no profile, no service tier, no composition prefix, no `-` prompt
// marker. The composer that owns the terminal spells the MCP channel; the yolo
// flag is spelled HERE, once, because Decision 0013 D5 forbids the launcher
// from spelling a provider flag. Both spellings were checked against
// `codex --help` at 0.153.2: `-m, --model <MODEL>` and `-c, --config
// <key=value>` are top-level flags of the interactive invocation, not `exec`
// subcommand flags.

// bypassApprovalsAndSandboxFlag is the ONE spelling of codex's
// permission-bypass flag in this plugin. The exec grammar emits it
// unconditionally (the source's construction, golden-captured); the
// interactive grammar emits it exactly when the request carries PermissionMode
// "yolo". The spelling is the module's own exec spelling, corroborated by
// `codex --help` at installed 0.153.4 ("Skip all confirmation prompts and
// execute commands without sandboxing") and by Decision 0018's verification at
// 0.153.4; the (environment, tool release) capability table that re-verifies
// it per release is F-M1b's. Both branches reference this const rather than
// repeating the literal, so the yolo mapping is one site and the argvguard
// proof — whose signature carries this flag — holds it.
const bypassApprovalsAndSandboxFlag = "--dangerously-bypass-approvals-and-sandbox"

// Args builds the codex argv for one launch mode, excluding the binary.
//
// It is the single construction site. Every surface of this plugin that needs
// codex flags calls it: System.Argv for all four modes, and nothing else.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	effective, err := req.PermissionMode.Resolve()
	if err != nil {
		return nil, fmt.Errorf("codex: %w", err)
	}
	if mode != agentic.LaunchModeInteractive && !req.PermissionMode.IsZero() {
		return nil, fmt.Errorf("codex: %w: permission mode %q is valid only for interactive launches",
			agentic.ErrPermissionModeNotInteractive, strings.TrimSpace(string(req.PermissionMode)))
	}
	if mode != agentic.LaunchModeInteractive && len(req.NativeArgs) != 0 {
		return nil, fmt.Errorf("codex: %w: %d native argument(s) reach no verbatim suffix outside an interactive launch",
			agentic.ErrNativeArgsNotInteractive, len(req.NativeArgs))
	}
	model := strings.TrimSpace(req.Model.ID)
	switch mode {
	case agentic.LaunchModeExec, agentic.LaunchModeDryRun:
		args := compositionArgvPrefix(req)
		args = append(args, "--search", "-a", "never")
		if profile := strings.TrimSpace(req.Profile); profile != "" {
			args = append(args, "-p", profile)
		}
		args = append(args, "exec", "-m", model)
		args = appendReasoningAndTier(args, req)
		args = append(args, bypassApprovalsAndSandboxFlag)
		args = append(args, "--skip-git-repo-check")
		args = append(args, "-C", strings.TrimSpace(req.WorkDir))
		if boardDir := strings.TrimSpace(req.Run.BoardDir); boardDir != "" {
			args = append(args, "--add-dir", boardDir)
		}
		// The prompt is read from stdin; "-" is codex's marker for it. The
		// stdin bytes themselves are System.Stdin's business, and the two are
		// only consistent because BuildPlan drives both from one request.
		args = append(args, "-")
		return args, nil
	case agentic.LaunchModeManagedSession:
		args := []string{
			"--model", model,
			"--search",
			"--sandbox", "danger-full-access",
			"--ask-for-approval", "never",
		}
		if profile := strings.TrimSpace(req.Profile); profile != "" {
			args = append(args, "--profile", profile)
		}
		args = appendReasoningAndTier(args, req)
		return args, nil
	case agentic.LaunchModeInteractive:
		// The profile and tier refusals are BuildPlan's too (a tier, through
		// ErrParameterNotInteractive), and are repeated here because a caller
		// holding the plugin directly meets this function first. A profile is
		// codex's own fact and no core rule names it, so this is its ONLY
		// refusal: the interactive grammar has no `-p`, and an admitted
		// profile that reached no flag would be a configured profile dropped.
		if profile := strings.TrimSpace(req.Profile); profile != "" {
			return nil, fmt.Errorf("codex: an interactive launch carries no profile; %q would reach no flag", profile)
		}
		if tier := strings.TrimSpace(req.ServiceTier); tier != "" {
			return nil, fmt.Errorf("codex: an interactive launch carries no service tier; %q would reach no override", tier)
		}
		args := []string{"-m", model}
		// With the tier refused above, appendReasoningAndTier contributes the
		// effort override alone — the same spelling the other two grammars use,
		// from the same fragment, so the three cannot drift.
		args = appendReasoningAndTier(args, req)
		if effective != agentic.PermissionModeYolo {
			// Native forwards the caller's arguments with no inspection
			// at all: the raw contract is unchanged and no claim is made
			// over them (curator-spec Decision 0018 item 4).
			return append(args, nativeArgsSuffix(req)...), nil
		}
		// Yolo appends the bypass flag AFTER model and effort, mirroring the
		// exec grammar's relative order (model, effort, bypass) and landing
		// before the caller's verbatim suffix (curator-spec Decision 0018
		// item 1). A composition prefix that already holds the flag is
		// refused, not de-duplicated: through BuildPlan the composition is
		// refused first anyway, so this fires for a caller holding the
		// plugin directly, where it is the only line. It stays the first
		// yolo check — an exact, release-independent match — so the
		// capability lookup below only ever classifies requests this one
		// admitted.
		for _, arg := range req.Composition.Prefix {
			if arg == bypassApprovalsAndSandboxFlag {
				return nil, fmt.Errorf("codex: %w: the composition prefix already carries %q; refusing rather than emitting it twice",
					agentic.ErrPermissionModeDuplicate, bypassApprovalsAndSandboxFlag)
			}
		}
		// The capability lookup establishes the verified grammar before the
		// scan classifies against it: an unpinned or newer release, and an
		// empty one, fail closed here, and the scan below never reasons
		// under a grammar no release verified.
		row, err := agentic.LookupReleaseCapability(verifiedReleases, req.ToolRelease)
		if err != nil {
			return nil, fmt.Errorf("codex: %w", err)
		}
		if !row.YoloSupported {
			return nil, fmt.Errorf("codex: refusing yolo: %w: tool release %q documents no bypass flag",
				agentic.ErrPermissionModeUnsupported, row.Release)
		}
		if err := scanNativePolicy(req.NativeArgs); err != nil {
			return nil, fmt.Errorf("codex: %w", err)
		}
		return append(append(args, bypassApprovalsAndSandboxFlag), nativeArgsSuffix(req)...), nil
	default:
		return nil, fmt.Errorf("codex: unsupported launch mode %s", mode)
	}
}

// appendReasoningAndTier appends the `-c` config overrides both codex launch
// modes splice in identically.
//
// It is NOT a second construction site and the guard's allowlist says so with
// a reason: it carries no mode-specific flag, only the effort/service-tier
// fragment. Inlining it into both branches of Args is what would create a
// second spelling, one branch at a time.
//
// Effort here is pure TRANSPORT. The word itself — "high", "xhigh", whatever a
// model's vocabulary contains — arrives on the request from the vendor layer
// and is quoted verbatim into the override. This plugin does not know which
// words are legal for which model and must not learn: that is invariant 4 of
// docs/architecture.md, and a system enumerating a vocabulary is how a model
// that gains an effort level becomes a launch that silently refuses it.
func appendReasoningAndTier(args []string, req agentic.LaunchRequest) []string {
	if effort := strings.TrimSpace(req.Effort); effort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	if tier := NormalizeServiceTier(req.ServiceTier); tier != "" {
		args = append(args, "-c", fmt.Sprintf("service_tier=%q", tier))
	}
	return args
}

// NormalizeServiceTier maps the runtime's service-tier vocabulary onto the
// catalog tier id `codex exec -c service_tier=` accepts.
//
// This mapping is a HARNESS fact, not a vendor one — it is what the codex CLI
// takes — which is why it lives in this plugin and is exported for the
// caller that has to display the resolved value.
//
// An unrecognized tier normalizes to the empty string, which drops the
// override. That is the source's behaviour (NormalizeCodexServiceTier,
// spawn.go), ported deliberately rather than improved: dropping is a silent
// downgrade of an operator's configured tier, and TestAnUnknownServiceTierIsDropped
// pins it so the residual is visible instead of implied. Changing it to a
// refusal is a behaviour change no golden covers, and it belongs to whoever
// owns the tier vocabulary, not to this port.
func NormalizeServiceTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "fast", "priority":
		return "priority"
	case "default", "standard":
		return "default"
	default:
		return ""
	}
}

// compositionArgvPrefix returns the already-composed MCP prefix a launch
// carries, copied so a plugin cannot hand a caller's backing array to a
// process launcher that appends to it.
func compositionArgvPrefix(req agentic.LaunchRequest) []string {
	return append([]string{}, req.Composition.Prefix...)
}

// nativeArgsSuffix returns the caller's native arguments for the verbatim
// interactive suffix, copied for the same reason: the plan's argv must not
// alias the request's backing array.
func nativeArgsSuffix(req agentic.LaunchRequest) []string {
	return append([]string{}, req.NativeArgs...)
}
