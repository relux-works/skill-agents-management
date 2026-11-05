package gemini

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE GEMINI CLI FLAGS ARE SPELLED.
//
// The extraction source reached that state for codex only after paying a whole
// task to collapse three independently-drifted spellings into one function
// (skill-project-management, TASK-260817-1v9v95). Gemini never drifted there
// because it never had a second constructor: buildGeminiArgs was the single
// site and geminiDryRunArgs called it with a different prompt argument. This
// file keeps that property rather than discovering it the expensive way, and
// argvguard_test.go fails the build if a second site in this module starts
// spelling gemini flags — proving it can by narrowing itself onto this file's
// own Args.
//
// # The two modes differ in exactly ONE argument, and that is the whole story
//
// A real launch passes an EMPTY `-p` and streams the assignment on stdin.
// gemini appends what it reads on stdin to the `-p` value, so an exec launch
// that also put the text in argv would deliver the assignment TWICE — and would
// bound it by the platform's argv size limit, which agy's own budget check
// exists because of.
//
// A dry run has no file to stream, so it substitutes the readable placeholder
// `<prompt>` into `-p` instead. The source says this in as many words:
// "Real launches pass an empty -p value and stream the prompt file on stdin.
// Use a readable stand-in only in dry-run output."
//
// That single substitution is exactly why this repository made dry-run a MODE
// rather than a sibling method: there is one difference between the two
// grammars, it is visible on one line here, and no second function can drift
// from it.

// promptPlaceholder is what a dry run reports where a real launch would stream
// the assignment. It is the source's literal (adapter.go, geminiDryRunArgs).
const promptPlaceholder = "<prompt>"

// Args builds the gemini argv for one launch mode, excluding the binary.
//
// It is the single construction site. Every surface of this plugin that needs
// gemini flags calls it: System.Argv for both modes, and nothing else.
//
// The ORDER is the source's, verbatim.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	prompt := ""
	switch mode {
	case agentic.LaunchModeExec:
	case agentic.LaunchModeDryRun:
		prompt = promptPlaceholder
	default:
		return nil, fmt.Errorf("gemini: unsupported launch mode %s", mode)
	}

	// The composition prefix is spliced ahead of every provider flag, exactly
	// as launchCompositionArgvPrefix does in the source. Gemini declares NO
	// composition grammar, so BuildPlan refuses a non-zero composition before
	// this line is ever reached with one — keeping the splice here is what
	// makes that refusal the only thing standing between a caller and this
	// argv, rather than a silent drop that would produce a launch missing the
	// servers somebody reviewed.
	args := append([]string{}, req.Composition.Prefix...)
	args = append(args, "-p", prompt, "-m", strings.TrimSpace(req.Model.ID), "-y", "--skip-trust", "-o", "json")
	if workDir := req.WorkDir; workDir != "" {
		args = append(args, "--include-directories", workDir)
	}
	return args, nil
}
