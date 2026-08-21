package claude

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE CLAUDE CLI FLAGS ARE SPELLED.
//
// The extraction source reached that state for codex only after paying a whole
// task to collapse three independently-drifted spellings into one function
// (skill-project-management, TASK-260817-1v9v95). Claude never drifted there
// because it never had a second constructor: buildClaudeArgs was the single
// site, and claudeDryRunArgs called it. This file keeps that property rather
// than discovering it the expensive way, and argvguard_test.go fails the build
// if a second site in this module starts spelling claude flags — proving it can
// by narrowing itself onto this file's own Args.
//
// # The dry-run mode is the same grammar, with one substitution
//
// The source's claudeDryRunArgs is buildClaudeArgs with the assignment path
// replaced by the readable placeholder `<assignment-prompt-file>` when the
// config carries none. That substitution is the ONLY difference between the two
// modes, which is exactly why this repository made dry-run a mode instead of a
// sibling method: there is nothing for a second function to get right except by
// not existing.
//
// No golden covers claude's dry run. The source's capture harness recorded
// claude/prompt-mode and claude/goal-mode and nothing else, so the placeholder
// substitution is proved against the source's construction in args_test.go
// rather than against a fixture, and it is said here rather than left for a
// reader to infer from an absence.

// promptFilePlaceholder is what a dry run reports where a real launch would
// name an assignment file that does not exist yet. It is the source's literal
// (adapter.go, claudeDryRunArgs).
const promptFilePlaceholder = "<assignment-prompt-file>"

// Args builds the claude argv for one launch mode, excluding the binary.
//
// It is the single construction site. Every surface of this plugin that needs
// claude flags calls it: System.Argv for both modes, and nothing else.
//
// The ORDER is the source's, verbatim, and it is contract rather than taste:
// the composition prefix comes first because it is spliced ahead of every
// provider flag, and the goal pair comes last because the `/goal` directive is
// the child's user turn and everything after it would be read as part of it.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	switch mode {
	case agentic.LaunchModeExec, agentic.LaunchModeDryRun:
	default:
		return nil, fmt.Errorf("claude: unsupported launch mode %s", mode)
	}

	args := compositionArgvPrefix(req)
	args = append(args, "-p", "--output-format", "json", "--model", strings.TrimSpace(req.Model.ID))
	if effort := strings.TrimSpace(req.Effort); effort != "" {
		// Pure TRANSPORT. The word itself — "high", "xhigh", whatever a model's
		// vocabulary contains — arrives on the request from the vendor layer and
		// is passed through verbatim. This plugin does not know which words are
		// legal for which model and must not learn: that is invariant 4 of
		// docs/architecture.md, and a system enumerating a vocabulary is how a
		// model that gains an effort level becomes a launch that silently
		// refuses it.
		args = append(args, "--effort", effort)
	}
	if req.Budget != nil && req.Budget.USD > 0 {
		// The source's `%.2f` and its `> 0` guard, both kept rather than
		// improved. The guard means a Budget carrying zero or a negative figure
		// produces NO ceiling flag while BuildPlan has already admitted the
		// launch as budget-bearing — a configured budget silently dropped,
		// which is the same class of residual as codex's unknown service tier.
		// TestAZeroBudgetIsDroppedRatherThanRefused pins it so the behaviour is
		// visible in the suite instead of only in the arithmetic.
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", req.Budget.USD))
	}
	args = append(args, "--dangerously-skip-permissions")

	if req.Goal == nil {
		return args, nil
	}

	assignmentPath := strings.TrimSpace(req.PromptPath)
	if assignmentPath == "" {
		if mode != agentic.LaunchModeDryRun {
			return nil, fmt.Errorf("claude: goal-bound launch requires an assignment prompt file")
		}
		assignmentPath = promptFilePlaceholder
	}
	directive, err := goalDirective(req.Goal)
	if err != nil {
		return nil, err
	}
	args = append(args, "--append-system-prompt-file", assignmentPath, directive)
	return args, nil
}

// compositionArgvPrefix returns the already-composed MCP prefix a launch
// carries, copied so a plugin cannot hand a caller's backing array to a process
// launcher that appends to it.
func compositionArgvPrefix(req agentic.LaunchRequest) []string {
	return append([]string{}, req.Composition.Prefix...)
}
