package claude

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file covers the argv surfaces the claude goldens do NOT reach.
//
// Two goldens exist — prompt-mode and goal-mode, both exec — so three things
// the source's buildClaudeArgs does have no fixture behind them: the budget
// flag, the dry-run mode, and every refusal. Each is proved here against the
// source's own construction, and each says which source line it is measuring.
// An absence of coverage that nobody names is how a ported surface quietly
// stops working.

// argvFor drives the production entry point rather than calling Args directly:
// a helper that is unit-tested but called from nowhere promises nothing about
// the launch a caller would actually get.
func argvFor(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) []string {
	t.Helper()
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req.Env = append(append([]string(nil), req.Env...), "PATH="+binDir)
	return buildParityPlan(t, New(), req, mode).Argv
}

// planErrorFor is argvFor for the cases that must REFUSE. It returns the error
// BuildPlan produced, so a refusal is measured at the same boundary a caller
// meets it.
func planErrorFor(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	t.Helper()
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req.Env = append(append([]string(nil), req.Env...), "PATH="+binDir)

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err := agentic.BuildPlan(registry, req, mode)
	return err
}

func indexOf(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}

// TestTheBudgetCeilingReachesArgv covers the one adapter capability claude has
// and nobody else in the source's table does.
//
// SupportsBudget is declared true in Capabilities, and a declared capability
// that does not reproduce is the shape this repository refuses. NEITHER claude
// capture configured a budget, so the golden is silent and this is the only
// evidence the flag exists — measured against spawn.go:958-960, including its
// `%.2f`.
func TestTheBudgetCeilingReachesArgv(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Budget = &agentic.Budget{USD: 12.5}

	args := argvFor(t, req, agentic.LaunchModeExec)
	at := indexOf(args, "--max-budget-usd")
	if at < 0 {
		t.Fatalf("a budget-bearing launch carries no ceiling flag: %v", args)
	}
	if at+1 >= len(args) || args[at+1] != "12.50" {
		t.Fatalf("the ceiling was rendered as %v, want %q; the source formats it with %%.2f and a harness that parses cents reads 12.5 differently", args[at+1:], "12.50")
	}
	// The flag's POSITION is contract too: the source appends it before
	// --dangerously-skip-permissions, and the goal pair after that.
	if skip := indexOf(args, "--dangerously-skip-permissions"); skip < at {
		t.Errorf("the ceiling landed after --dangerously-skip-permissions; the source's order is model, effort, budget, permissions: %v", args)
	}
}

// TestTheModelIDIsTrimmedBeforeItReachesArgv holds the source's
// `model := strings.TrimSpace(cfg.Model)`.
//
// Without it a model id that picked up an edge space anywhere upstream reaches
// the child as a DIFFERENT model name than the one the request names, and
// claude resolves it against its own table: an unknown id is a refused launch
// at best, and a silently different resolution at worst. Neither golden carries
// a padded id, so nothing else in the suite reads this line.
func TestTheModelIDIsTrimmedBeforeItReachesArgv(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Model.ID = "  " + parityModel + "\t\n"

	args := argvFor(t, req, agentic.LaunchModeExec)
	at := indexOf(args, "--model")
	if at < 0 || at+1 >= len(args) {
		t.Fatalf("the argv carries no model pair: %v", args)
	}
	if args[at+1] != parityModel {
		t.Errorf("the model reached argv as %q, want %q; the source trims it and an untrimmed id is a different model name to the child", args[at+1], parityModel)
	}
}

// TestAZeroBudgetIsDroppedRatherThanRefused pins a RESIDUAL rather than a
// virtue.
//
// BuildPlan admits a launch carrying a non-nil Budget because claude declares
// SupportsBudget, and Args then drops any figure at or below zero — the
// source's `if cfg.Budget > 0` guard. So a caller that configured a ceiling of
// zero gets an uncapped launch with no error anywhere. That is the source's
// behaviour, ported deliberately rather than improved; changing it to a refusal
// is a behaviour change no golden covers and belongs to whoever owns the budget
// vocabulary, not to this port.
func TestAZeroBudgetIsDroppedRatherThanRefused(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Budget = &agentic.Budget{USD: 0}

	args := argvFor(t, req, agentic.LaunchModeExec)
	if at := indexOf(args, "--max-budget-usd"); at >= 0 {
		t.Fatalf("a zero budget produced a ceiling flag: %v", args)
	}
}

// TestTheDryRunMirrorsTheExecGrammar is the mode with no golden at all.
//
// The source's claudeDryRunArgs is buildClaudeArgs with one substitution, so
// the claim is exactly that: identical argv when a prompt file exists, and the
// readable placeholder standing in for the assignment path when one does not.
// A dry run is required to be side-effect free, which is why the second half
// supplies no file rather than creating one.
func TestTheDryRunMirrorsTheExecGrammar(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)

	exec := argvFor(t, req, agentic.LaunchModeExec)
	dry := argvFor(t, req, agentic.LaunchModeDryRun)
	if strings.Join(exec, "\x00") != strings.Join(dry, "\x00") {
		t.Errorf("the dry run spells a different grammar than the launch it mirrors:\n  exec: %v\n  dry:  %v", exec, dry)
	}

	t.Run("and substitutes the placeholder for an assignment file that does not exist yet", func(t *testing.T) {
		goalReq := parityRequest(workDir, parityGoalRunID, parityGoalTaskID)
		goalReq.Goal = parityGoal()
		args := argvFor(t, goalReq, agentic.LaunchModeDryRun)
		at := indexOf(args, "--append-system-prompt-file")
		if at < 0 || at+1 >= len(args) {
			t.Fatalf("a goal-bound dry run carries no system-prompt argument: %v", args)
		}
		if args[at+1] != promptFilePlaceholder {
			t.Errorf("the dry run reported %q as the assignment path, want the placeholder %q", args[at+1], promptFilePlaceholder)
		}
	})
}

// TestAGoalBoundLaunchWithoutAnAssignmentFileIsRefused is the source's own
// refusal (spawn.go:966-969), and it must NOT be softened into the dry run's
// placeholder.
//
// The distinction is what the placeholder is for: a dry run has no assignment
// file yet BY DESIGN, and a real launch that has none is a launch whose child
// would be handed a system prompt naming a file that does not exist.
func TestAGoalBoundLaunchWithoutAnAssignmentFileIsRefused(t *testing.T) {
	t.Parallel()
	req := parityRequest(tempSlot(t), parityGoalRunID, parityGoalTaskID)
	req.Goal = parityGoal()

	err := planErrorFor(t, req, agentic.LaunchModeExec)
	if err == nil {
		t.Fatal("a goal-bound exec launch with no assignment prompt file was admitted")
	}
	if !strings.Contains(err.Error(), "assignment prompt file") {
		t.Errorf("the refusal was %v; it has to name the missing thing, because the operator fixing it has only this text", err)
	}
	if args := argvFor(t, req, agentic.LaunchModeDryRun); indexOf(args, promptFilePlaceholder) < 0 {
		t.Errorf("the same request was refused in dry-run mode too; the placeholder exists so a dry run does not have to create a file: %v", args)
	}
}

// TestAGoalWithNoProviderConditionIsRefused is the gate carried across from the
// source's PREPARATION half.
//
// validateClaudeGoalContract refuses an empty provider condition
// (tools/board-cli/internal/spawn/claude_goal.go), and that preparation is out
// of this plan surface — goal.go says why. Dropping the refusal with it would ship the literal
// argument `/goal ` to a child told it is goal-bound: a binding to nothing,
// with no error anywhere. A validating gate must not get weaker across a move.
//
// The NARROWING is the second half: the same request with a condition present
// must be admitted, or "refused" would be equally consistent with a plugin that
// refuses every goal.
func TestAGoalWithNoProviderConditionIsRefused(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityGoalRunID, parityGoalTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityGoalBody)
	req.Goal = &agentic.Goal{ID: parityGoalID, Revision: parityGoalRevision}

	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		if err := planErrorFor(t, req, mode); err == nil {
			t.Errorf("a %s launch bound to a goal carrying no provider condition was admitted; the child would receive a bare `/goal` directive and be bound to nothing", mode)
		}
	}

	t.Run("and a condition present is admitted", func(t *testing.T) {
		ok := req
		ok.Goal = parityGoal()
		args := argvFor(t, ok, agentic.LaunchModeExec)
		if at := indexOf(args, goalDirectivePrefix+parityProviderCondition(parityGoalID, parityGoalRunID)); at < 0 {
			t.Fatalf("the directive did not reach argv, so the refusal above proves nothing about the condition: %v", args)
		}
	})
}

// TestTheGoalConditionIsPassedThroughVerbatim holds the boundary the core's
// ProviderCondition field was added for.
//
// The board layer renders the predicate and validates a stored contract against
// a RE-RENDER, so a plugin that normalized, wrapped or re-worded the text would
// produce a directive the board rejects as tampered. Only the outer whitespace
// is trimmed, which is ClaudeGoalDirective's own strings.TrimSpace.
func TestTheGoalConditionIsPassedThroughVerbatim(t *testing.T) {
	t.Parallel()
	const awkward = "Own **GOAL-X**; run `task-board spawn goal RUN-X`.\nA stale revision is not success.  "
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityGoalRunID, parityGoalTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityGoalBody)
	req.Goal = &agentic.Goal{ID: parityGoalID, ProviderCondition: awkward}

	args := argvFor(t, req, agentic.LaunchModeExec)
	want := goalDirectivePrefix + strings.TrimSpace(awkward)
	if indexOf(args, want) < 0 {
		t.Errorf("the directive was rewritten; argv carries %v, want the verbatim %q", args[len(args)-1:], want)
	}
}

// TestTheCompositionPrefixLeadsTheArgv pins the position the source gives it.
//
// launchCompositionArgvPrefix is the FIRST thing buildClaudeArgs appends, and
// the position is contract rather than taste: --mcp-config after `-p` would be
// read by the harness as part of the prompt-mode argument list it has already
// begun parsing.
func TestTheCompositionPrefixLeadsTheArgv(t *testing.T) {
	t.Parallel()
	const config = `{"mcpServers":{"docs":{"type":"stdio","command":"docs-server"}}}`
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Composition = agentic.Composition{
		Prefix:  []string{mcpConfigFlag, config},
		Servers: []agentic.CompositionServer{{Name: "docs", Transport: "stdio"}},
	}

	args := argvFor(t, req, agentic.LaunchModeExec)
	if len(args) < 2 || args[0] != mcpConfigFlag || args[1] != config {
		t.Fatalf("the composition prefix is not leading the argv: %v", args)
	}
}

// TestThePluginDoesNotHandOutItsCallersBackingArray is the aliasing bound on
// compositionArgvPrefix.
//
// The prefix arrives on the request and is spliced into a slice this plugin
// then appends to. Without the copy, a second append would write through into
// the caller's array — and the caller here is whatever composed the MCP
// configuration, which may hold that slice for its own evidence record.
func TestThePluginDoesNotHandOutItsCallersBackingArray(t *testing.T) {
	t.Parallel()
	const config = `{"mcpServers":{"docs":{"type":"stdio","command":"docs-server"}}}`
	prefix := make([]string, 2, 64)
	prefix[0], prefix[1] = mcpConfigFlag, config

	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Composition = agentic.Composition{
		Prefix:  prefix,
		Servers: []agentic.CompositionServer{{Name: "docs", Transport: "stdio"}},
	}

	argvFor(t, req, agentic.LaunchModeExec)
	if got := prefix[:cap(prefix)][2]; got != "" {
		t.Errorf("the caller's backing array was written through: prefix[2] = %q", got)
	}
}

// TestAnUndeclaredLaunchModeIsRefused holds the second of the two refusals the
// plugin boundary carries.
//
// LaunchModeManagedSession is a mode this system does not declare — the source
// registers no managed-session args builder for claude — and Argv refuses it
// even when a caller holds the plugin directly rather than going through
// BuildPlan.
func TestAnUndeclaredLaunchModeIsRefused(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)

	if _, err := New().Argv(req, agentic.LaunchModeManagedSession); err == nil {
		t.Error("the plugin built argv for a mode it does not declare")
	}
	if err := planErrorFor(t, req, agentic.LaunchModeManagedSession); !errors.Is(err, agentic.ErrUnsupportedLaunchMode) {
		t.Errorf("BuildPlan returned %v, want %v", err, agentic.ErrUnsupportedLaunchMode)
	}
}
