package qwen

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE QWEN CLI FLAGS ARE SPELLED.
//
// The extraction source reached that state for codex only after paying a whole
// task to collapse three independently-drifted spellings into one function
// (skill-project-management, TASK-260817-1v9v95). Qwen never drifted there
// because it never had a second constructor: buildQwenArgs was the single site
// and qwenDryRunArgs called it, returning its result unchanged. This file keeps
// that property rather than discovering it the expensive way, and
// argvguard_test.go fails the build if a second site in this module starts
// spelling qwen flags — proving it can by narrowing itself onto this file's own
// Args.
//
// # The two modes are the SAME argv, with nothing substituted
//
// qwenDryRunArgs is `return buildQwenArgs(cfg), nil`. There is no placeholder
// anywhere, because nothing in this grammar names a file: the assignment
// reaches the child on stdin, and a dry run simply does not build the stdin.
// That is why qwen/exec and qwen/dry-run record IDENTICAL args in the goldens,
// and it is the cleanest illustration of why this repository made dry-run a
// MODE rather than a sibling method — there is nothing for a second function to
// get right except by not existing.
//
// # What is NOT here: the effort
//
// buildQwenArgs never references cfg.ReasoningEffort. Qwen's effort transport is
// stdin (stdin.go), so an argv-only reading of this plugin would conclude the
// configured effort is dropped. It is not; it is one field of the initialize
// frame, and the goldens compare that frame byte for byte.

// Args builds the qwen argv for one launch mode, excluding the binary.
//
// It is the single construction site. Every surface of this plugin that needs
// qwen flags calls it: System.Argv for both modes, and nothing else.
//
// The ORDER is the source's, verbatim, and the composition prefix comes first
// because it is spliced ahead of every provider flag.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	switch mode {
	case agentic.LaunchModeExec, agentic.LaunchModeDryRun:
	default:
		return nil, fmt.Errorf("qwen: unsupported launch mode %s", mode)
	}

	args := compositionArgvPrefix(req)
	return append(args,
		"--model", strings.TrimSpace(req.Model.ID),
		"--approval-mode", "yolo",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	), nil
}

// compositionArgvPrefix returns the already-composed MCP prefix a launch
// carries, copied so a plugin cannot hand a caller's backing array to a process
// launcher that appends to it.
func compositionArgvPrefix(req agentic.LaunchRequest) []string {
	return append([]string{}, req.Composition.Prefix...)
}
