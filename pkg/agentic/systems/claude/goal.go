package claude

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// GOAL MODE, AND THE HALF OF IT THAT IS NOT IN THE PLAN SURFACE.
//
// A goal-bound claude launch has two parts in the extraction source. Only one
// of them is expressible here, and saying which is which is the whole point of
// this file: an absence a reader has to infer is how a ported system quietly
// loses a gate.
//
// # In the plan surface: the directive and the system-prompt file
//
// buildClaudeArgs appends `--append-system-prompt-file <assignment>` and one
// positional argument, `/goal <provider condition>` (ClaudeGoalDirective,
// tools/board-cli/internal/spawn/claude_goal.go). That is what
// claude/goal-mode records, it is what Args builds, and it is byte-compared
// against the golden.
//
// The assignment moves from stdin to SYSTEM context in this mode, which is why
// System.Stdin attaches nothing: the child's single user turn is the directive.
//
// # NOT in the plan surface: launch PREPARATION
//
// Before it constructs anything, the source's command layer calls
// PreflightClaudeGoalLaunch → PrepareClaudeGoalLaunch
// (tools/board-cli/internal/spawn/claude_goal.go), which:
//
//  1. validates the board goal contract (pkgboard.ValidateGoalContract) and
//     refuses an empty provider condition;
//  2. runs `claude --version` and parses it, refusing below
//     ClaudeGoalMinimumVersion = 2.1.139 — the first release with the native
//     session-scoped /goal command;
//  3. runs `claude -p --output-format json /goal` as a CAPABILITY PROBE, and
//     classifies the failure into unavailable / workspace-untrusted /
//     hooks-disabled / preflight-failed;
//  4. decides, from the session state, which provider action the launch
//     requires: native_goal_bound for a fresh session, binding_retained for an
//     already-bound one, successor_required for a running unbound session that
//     a nested shell cannot rewrite.
//
// Steps 2 and 3 START PROCESSES. The System contract in pkg/agentic states that
// every method must be free of side effects other than reading the environment
// it was handed, and BuildPlan calls ALL of them to produce a dry-run plan — so
// putting preparation behind ResolveBinary, Argv or Stdin would make
// LaunchModeDryRun execute the harness twice. That is the exact drift this
// repository's dry-run-as-a-mode design exists to prevent, and it is why this
// is a stated boundary rather than an oversight.
//
// Step 4 is not a plan fact at all: it is a decision about a session that
// already exists, which nothing in a Plan can express. The source itself
// records this — ClaudeGoalBinding.Rollback is a documented no-op because
// Activate "does not mutate a live Claude session".
//
// # Where it has to land instead
//
// With the launch/session plane, when that is ported: preparation is a
// preflight the LAUNCHER runs before it asks for a plan, in the same position
// PreflightClaudeGoalLaunch holds in the source's command layer. This task does
// not invent a home for it, and nothing here silently absorbs it.
//
// What IS carried across is step 1's refusal, narrowed to the part a plugin
// holding no board can check. Everything else in preparation is a probe; the
// empty-provider-condition refusal is a property of the argv this file builds,
// and dropping it would mean a goal-bound launch shipping the literal argument
// `/goal ` — a child told it is goal-bound, bound to nothing, with no error
// anywhere. A validating gate getting weaker across a move is the failure this
// repository has already recorded once, on CompositionServer.BearerTokenEnvVar.

// goalDirectivePrefix is the native user directive a goal-bound claude
// invocation spends its one user turn on. It is the source's literal
// (ClaudeGoalDirective, tools/board-cli/internal/spawn/claude_goal.go).
const goalDirectivePrefix = "/goal "

// goalDirective renders the one positional argument a goal-bound launch
// carries, or refuses the launch.
//
// The condition text is passed through VERBATIM and is never rendered here.
// The board layer owns that renderer (pkgboard.RenderGoalProviderCondition) and
// validates a stored contract against a re-render, so a second renderer in this
// plugin would produce a predicate the board rejects as tampered — two
// spellings of one fact, which is invariant 5 of docs/architecture.md.
func goalDirective(goal *agentic.Goal) (string, error) {
	if goal == nil {
		return "", fmt.Errorf("claude: goal-bound launch requires a goal")
	}
	condition := strings.TrimSpace(goal.ProviderCondition)
	if condition == "" {
		return "", fmt.Errorf("claude: goal %q carries no provider condition; a `%s` directive with nothing after it binds the child to nothing", goal.ID, strings.TrimSpace(goalDirectivePrefix))
	}
	return goalDirectivePrefix + condition, nil
}
