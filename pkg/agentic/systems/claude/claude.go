// Package claude is the agentic-system plugin for the Claude Code CLI.
//
// It is a port of the claude half of skill-project-management's spawn adapter
// table (tools/board-cli/internal/spawn), proven against the launch-surface
// goldens that repository's own harness captured at commit
// ed4878123061b39fdae67160f6b5632117b48a2f. Two goldens cover it —
// claude/prompt-mode and claude/goal-mode — and parity_test.go drives both
// through the real agentic.Registry and agentic.BuildPlan.
//
// # What is hard about claude, stated where a reader meets it
//
//   - TWO launch surfaces from one grammar. Prompt mode streams the assignment
//     on stdin; goal mode appends the assignment as SYSTEM context and spends
//     the user turn on a `/goal` directive instead — so a goal-bound launch
//     attaches NO stdin at all. See args.go and goal.go.
//   - Goal-mode LAUNCH PREPARATION is a side effect the source performs before
//     construction, and it is deliberately NOT in this plan surface. goal.go
//     says exactly what the source does, why it cannot live behind a method the
//     dry-run mode also calls, and where it has to land instead.
//   - The environment contract is ONE exact key. Claude strips CLAUDECODE and
//     nothing else — not the codex family, which the qwen filter adds on top of
//     codex's and claude never had. See env.go, and the leak it leaves open.
//   - Binary resolution is plain PATH. There is no managed-package path, no npm
//     shim to unwrap, and none of codex's machinery is imported here.
//
// # Reshapes from the source, all deliberate
//
//  1. Binary resolution reads LaunchRequest.Env rather than the process
//     environment, through internal/launchenv (binary.go).
//  2. The composition grammar id is plugin-local rather than a core enum
//     (composition.go), and the grammar itself is shared with qwen and agy in
//     the source — so the id names the GRAMMAR, not this system.
//  3. agentic.Goal gained ProviderCondition, because the predicate the source
//     splices into argv is rendered by the board layer and a plugin holding
//     nothing ambient cannot otherwise reach it. Named on the field.
//  4. The goal-contract refusal the source performs in its preparation half is
//     carried here, because the preparation half is out of this plan surface
//     and a validating gate must not get weaker across a move (goal.go).
package claude

import (
	"fmt"
	"os"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// systemID is this plugin's identity.
//
// It is `claude-code`, the Layer-1 plugin id docs/architecture.md's table
// declares, and NOT `claude`. The two are different names for different things
// and the difference is load-bearing: `claude` is a frozen RUNTIME id — the
// (agentic system × vendor) pair that keys admitted-pair digests and
// limit-state filenames — and it is the spelling the extraction source's
// AgentType used, which is why the goldens record their system as "claude".
// parity_test.go maps between them in one place and says so there.
const systemID agentic.SystemID = "claude-code"

// System is the claude agentic-system plugin.
//
// It holds NO state. Every answer is computed from the LaunchRequest it is
// given, which is what lets one registered value serve concurrent launches and
// what makes a dry run provably identical to the launch it mirrors.
type System struct{}

// New returns the plugin. It is exported so a caller building an isolated
// registry — every test in this package, for one — can register it without
// depending on package initialization order.
func New() *System { return &System{} }

// init registers the plugin into the default registry, which is the
// registration path docs/architecture.md describes and the one the CLI's
// `plugins` command reads.
//
// A failure PANICS rather than being dropped. Register refuses a duplicate id,
// an unnormalized id, an unstable id and a declaration that could never launch
// — every one of those is a build-time bug in this package, and a plugin that
// silently fails to register is a launch that reports "no system registered"
// with nothing anywhere naming why.
func init() {
	if err := agentic.Register(New()); err != nil {
		panic(fmt.Sprintf("claude: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration.
//
// THREE launch modes: the source's two, and the interactive terminal session
// curator-spec Decision 0013 §5 added. The source registers claude with a
// BuildCommand and a DryRunArgs and nothing else: there is no managed-session
// args builder for claude anywhere in the adapter table. The interactive path
// that did exist there — cmd/claude_manager.go — FILTERS argv a human typed
// rather than constructing it from a config, which is a structurally different
// surface the goldens' own README names as uncovered by anything here.
// Declaring LaunchModeManagedSession for it would be a capability claim with
// no construction behind it.
//
// LaunchModeInteractive is NOT that filter. It is a constructed argv of model
// selection and effort transport only — `--model <id> [--effort <e>]` — with
// no golden behind it because no source capture ever produced one;
// interactive_test.go is its evidence and says so.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeExec,
			agentic.LaunchModeDryRun,
			agentic.LaunchModeInteractive,
		},
		// Argv: claude carries effort as `--effort <value>`. TRANSPORT only —
		// the words themselves belong to the model row in the vendor layer.
		EffortTransport: agentic.EffortTransportArgv,
		SupportsGoal:    true,
		// Claude is the ONE system in the source's adapter table that accepts a
		// spend ceiling (`--max-budget-usd`), and the only one whose adapter
		// sets SupportsBudget. No golden covers it — neither claude capture
		// configures a budget — so args_test.go proves it against the source's
		// construction instead, and says so there.
		SupportsBudget:      true,
		SupportsServiceTier: false,
		CompositionGrammar:  GrammarMCPConfigJSON,
		// The configuration home is a DECLARATION for a caller, not something
		// this plugin writes: the source sets no home variable on a claude
		// child, its adapter's DefaultHome field held the documentation string
		// "Config.ChildWorkDir()", and the goldens confirm the child inherits
		// no such variable. The values below name what the Claude Code CLI
		// itself reads, so a caller keying limit state by the resolved home has
		// something to resolve; they are outside what the goldens prove, and
		// moving either one needs a demonstrated migration on real state files
		// rather than an edit here.
		HomeEnvVar:  "CLAUDE_CONFIG_DIR",
		DefaultHome: "~/.claude",
		// Verbatim from the source's single providerAuthHint table
		// (provider_capability.go), which its adapter renders as
		// Message + "; " + Remediation.
		AuthHint: "Claude authentication is unavailable; run `claude login` and retry the goal-bound spawn",
	}
}

// ResolveBinary returns the exact executable this launch will run.
//
// It is the same function for every mode, which is the contract's requirement
// that a dry run report the launch's real target. The source's own bug was
// precisely the opposite: its dry-run path reported a hardcoded display string
// while a real launch resolved something else, and nothing compared them until
// a parity capture did.
func (*System) ResolveBinary(req agentic.LaunchRequest) (string, error) {
	return resolveBinary(req.Env)
}

// Argv builds the argument vector for one launch mode, excluding the binary.
//
// The mode is checked against the declaration before Args is called, so a mode
// this system did not declare is refused here as well as in BuildPlan. Two
// refusals for one fact is deliberate at a plugin boundary: BuildPlan's check
// protects the dispatch, this one protects a caller holding the plugin
// directly.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("claude: launch mode %s is not declared by this system", mode)
	}
	return Args(req, mode)
}

// ChildEnv is the environment contract: strip the parent session marker, then
// write the run context.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin is claude's prompt transport, and it is MODE-DEPENDENT in a way no
// other ported system's is.
//
// An INTERACTIVE launch attaches nothing, and that is enforced upstream of this
// method rather than inside it: the surface takes no mode, and BuildPlan
// refuses an interactive request carrying a prompt (ErrParameterNotInteractive)
// before any plugin surface runs, then refuses an attached stdin under an argv
// effort transport after this one does. With neither a prompt nor a goal on
// the request, the last return below is the answer.
//
// A prompt-mode launch streams the assignment file on standard input. A
// GOAL-BOUND launch attaches nothing: the source's buildClaudeCommand sets
// cmd.Stdin only when cfg.LaunchGoal is nil, because the assignment has already
// reached the child as `--append-system-prompt-file` and the one user turn is
// the `/goal` directive. Attaching the prompt in both would give a goal-bound
// child the assignment twice — once as system context and once as its user
// turn — which is a different run from the one the source performs, and
// claude/goal-mode records stdin_kind "none" precisely here.
//
// PromptPath is read when the caller supplied one, because that is what the
// source does and because a file read is the transport actually exercised in
// production. A read FAILURE is an error, never an empty payload: an absent
// prompt and an unreadable one are different facts, and returning "nothing
// attached" for a permissions error would launch a claude child that sits on an
// empty stdin and reports the model produced no work.
//
// Prompt bytes are the alternative for a caller that has the text without a
// file. Supplying NEITHER attaches nothing, which is the state a dry run is in
// — BuildPlan calls this method for every mode, and a dry run that failed here
// because no prompt file exists yet would be a dry run performing work.
func (*System) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	if req.Goal != nil {
		return agentic.StdinPayload{}, nil
	}
	if path := strings.TrimSpace(req.PromptPath); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return agentic.StdinPayload{}, fmt.Errorf("claude: reading the assignment prompt: %w", err)
		}
		return agentic.StdinPayload{Attached: true, Bytes: data}, nil
	}
	if len(req.Prompt) > 0 {
		return agentic.StdinPayload{Attached: true, Bytes: append([]byte(nil), req.Prompt...)}, nil
	}
	return agentic.StdinPayload{}, nil
}

// ValidateComposition refuses a composition whose prefix does not match the
// grammar this system declares.
func (*System) ValidateComposition(c agentic.Composition) error {
	if c.IsZero() {
		return nil
	}
	return validateComposition(c)
}
