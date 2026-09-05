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
// override when an effort was requested, and nothing else. No `exec`, no
// `--dangerously-bypass-approvals-and-sandbox`, no sandbox or approval policy,
// no profile, no service tier, no composition prefix, no `-` prompt marker: the
// composer that owns the terminal spells the MCP channel and the permission
// posture. Both spellings were checked against `codex --help` at 0.153.2:
// `-m, --model <MODEL>` and `-c, --config <key=value>` are top-level flags of
// the interactive invocation, not `exec` subcommand flags.

// Args builds the codex argv for one launch mode, excluding the binary.
//
// It is the single construction site. Every surface of this plugin that needs
// codex flags calls it: System.Argv for all four modes, and nothing else.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
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
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
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
		return appendReasoningAndTier(args, req), nil
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
