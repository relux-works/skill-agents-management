package muse

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE MUSE CLI FLAGS ARE SPELLED.
//
// The extraction source reached that state for codex only after paying a whole
// task to collapse three independently-drifted spellings into one function
// (skill-project-management, TASK-260817-1v9v95). Muse never drifted there
// because it never had a second constructor: buildMuseArgs was the single site
// and museDryRunArgs called it with a different prompt-file argument. This file
// keeps that property rather than discovering it the expensive way, and
// argvguard_test.go fails the build if a second site in this module starts
// spelling muse flags — proving it can by narrowing itself onto this file's own
// Args.
//
// # The two modes differ in exactly ONE argument
//
// museDryRunArgs substitutes `<prompt-file>` for the assignment path when the
// config carries none. Muse is the only ported system whose assignment is a
// PATH in argv rather than bytes on stdin, which is why it is also the only one
// besides claude with a placeholder to substitute at all.
//
// # Both optional flags are conditional, and the conditions are the source's
//
// `--workspace` is emitted only for a non-empty work directory and
// `--prompt-file` only for a non-empty path. A port that emitted either flag
// with an empty value would hand the child an argument naming nothing, which
// muse would read as a workspace at the process's own working directory or as
// an unreadable assignment.

// promptFilePlaceholder is what a dry run reports where a real launch would
// name an assignment file that does not exist yet. It is the source's literal
// (adapter.go, museDryRunArgs).
const promptFilePlaceholder = "<prompt-file>"

// Args builds the muse argv for one launch mode, excluding the binary.
//
// It is the single construction site. Every surface of this plugin that needs
// muse flags calls it: System.Argv for both modes, and nothing else.
//
// The ORDER is the source's, verbatim.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	promptFile := strings.TrimSpace(req.PromptPath)
	switch mode {
	case agentic.LaunchModeExec:
	case agentic.LaunchModeDryRun:
		if promptFile == "" {
			promptFile = promptFilePlaceholder
		}
	default:
		return nil, fmt.Errorf("muse: unsupported launch mode %s", mode)
	}

	// The composition prefix is spliced ahead of every provider flag, exactly
	// as launchCompositionArgvPrefix does in the source. Muse declares NO
	// composition grammar, so BuildPlan refuses a non-zero composition before
	// this line is ever reached with one — keeping the splice here is what
	// makes that refusal the only thing standing between a caller and this
	// argv, rather than a silent drop that would produce a launch missing the
	// servers somebody reviewed.
	args := append([]string{}, req.Composition.Prefix...)
	args = append(args, "exec", "--json", "--yolo", "--model", strings.TrimSpace(req.Model.ID))
	if workDir := req.WorkDir; workDir != "" {
		args = append(args, "--workspace", workDir)
	}
	if promptFile != "" {
		args = append(args, "--prompt-file", promptFile)
	}
	return args, nil
}
