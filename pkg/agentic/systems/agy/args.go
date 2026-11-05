package agy

import (
	"fmt"
	"os"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE ANTIGRAVITY CLI FLAGS ARE SPELLED.
//
// The extraction source reached that state for codex only after paying a whole
// task to collapse three independently-drifted spellings into one function
// (skill-project-management, TASK-260817-1v9v95). Agy never drifted there
// because it never had a second constructor: buildAgyArgs was the single site
// and agyDryRunArgs called it with a placeholder prompt. This file keeps that
// property rather than discovering it the expensive way, and argvguard_test.go
// fails the build if a second site in this module starts spelling agy flags —
// proving it can by narrowing itself onto this file's own Args.
//
// # The two modes differ in exactly ONE argument: the prompt
//
// agy takes the assignment as argv TEXT — the value of `--print` — not as a
// file path and not on stdin. A real launch reads the assignment and passes its
// contents; a dry run has no file, so it substitutes the source's `<prompt>`
// placeholder.
//
// # Which is why this is the only ported system with an ARG_MAX budget
//
// Putting the whole assignment in argv means a long prompt can exceed what the
// kernel will accept, and the failure mode without a check is ugly: exec fails
// with E2BIG from inside the launcher, after the run has been created, with an
// error naming neither the prompt nor its length. The source's budget is
// conservative for a stated reason — "Linux limits an individual argv string to
// 128 KiB even when ARG_MAX is larger" — and its refusal names the byte counts
// so an operator can act on it.
//
// The check is EXEC-only, because that is where it is real: a dry run's prompt
// is the ten-character placeholder, so measuring one would refuse nothing and
// would make the dry run's answer depend on an assignment it does not carry.

const (
	// trackedPrintTimeout is how long the child may run in print mode. It is
	// the source's agyTrackedPrintTimeout.
	trackedPrintTimeout = "30m"
	// argvBudgetBytes is the conservative ARG_MAX budget, verbatim from the
	// source's agyArgvBudgetBytes: 96 KiB, leaving headroom under Linux's
	// 128 KiB per-string limit for the executable path, the provider flags, the
	// model, the workspace and any launch-composition arguments.
	argvBudgetBytes = 96 * 1024
	// promptPlaceholder is what a dry run passes where a real launch passes the
	// assignment. It is the source's literal (adapter.go, agyDryRunArgs).
	promptPlaceholder = "<prompt>"
)

// Args builds the agy argv for one launch mode, excluding the binary.
//
// It is the single construction site. Every surface of this plugin that needs
// agy flags calls it: System.Argv for both modes, and nothing else.
//
// binary is the executable the launch will run, and it is a parameter rather
// than something resolved here because the budget below measures the WHOLE
// command line — the source's commandArgvBytes counts the binary path too, and
// a check that measured only the arguments would admit a command the kernel
// rejects whenever the executable path is long.
//
// The ORDER is the source's, verbatim.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode, binary string) ([]string, error) {
	var prompt string
	switch mode {
	case agentic.LaunchModeExec:
		assignment, err := assignmentText(req)
		if err != nil {
			return nil, err
		}
		prompt = assignment
	case agentic.LaunchModeDryRun:
		prompt = promptPlaceholder
	default:
		return nil, fmt.Errorf("agy: unsupported launch mode %s", mode)
	}

	// The composition prefix is spliced ahead of every provider flag, exactly
	// as launchCompositionArgvPrefix does in the source.
	args := append([]string{}, req.Composition.Prefix...)
	args = append(args,
		"--print", prompt,
		// The model id is passed through VERBATIM, including whatever effort
		// suffix the vendor layer's model row carries. This plugin does not
		// parse it and must not learn to: which suffixes exist and what they
		// mean is a per-model fact the vendor layer owns.
		"--model", strings.TrimSpace(req.Model.ID),
		"--output-format", "json",
		"--print-timeout", trackedPrintTimeout,
		"--mode", "accept-edits",
		"--dangerously-skip-permissions",
		"--disable-slash-commands",
	)
	if workDir := strings.TrimSpace(req.WorkDir); workDir != "" {
		args = append(args, "--add-dir", workDir)
	}

	if mode == agentic.LaunchModeExec {
		if bytes := commandArgvBytes(binary, args); bytes > argvBudgetBytes {
			return nil, fmt.Errorf(
				"agy: prompt is %d bytes and requires a %d-byte command argv, exceeding the conservative %d-byte ARG_MAX budget; shorten the assignment prompt",
				len(prompt), bytes, argvBudgetBytes,
			)
		}
	}
	return args, nil
}

// assignmentText reads the assignment a launch carries.
//
// PromptPath is read when the caller supplied one, because that is what the
// source does and because a file read is the transport actually exercised in
// production. A read FAILURE is an error, never an empty prompt: an absent
// assignment and an unreadable one are different facts, and passing `--print ""`
// for a permissions error would launch an agy child told to do nothing, whose
// run would be reported as the model producing no work.
//
// Prompt bytes are the alternative for a caller that has the text without a
// file.
//
// Supplying NEITHER is a REFUSAL, and this is the one place agy differs from
// the systems whose assignment travels on stdin. There, no assignment means
// nothing is attached, which is the legitimate state of a dry run. Here the
// prompt is an ARGUMENT: a launch with none would put an empty string in argv
// and the child would run with no instruction at all. The source refuses too —
// buildAgyCommand's os.ReadFile("") fails — and a dry run never reaches this
// function, because it substitutes the placeholder instead.
func assignmentText(req agentic.LaunchRequest) (string, error) {
	if path := strings.TrimSpace(req.PromptPath); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("agy: reading the assignment prompt: %w", err)
		}
		return string(data), nil
	}
	if len(req.Prompt) > 0 {
		return string(req.Prompt), nil
	}
	return "", fmt.Errorf("agy: an exec launch carries its assignment in argv and this one has none; supply the prompt file or its bytes")
}

// commandArgvBytes is the source's commandArgvBytes: the executable plus every
// argument, each counted with the NUL byte the kernel stores it with.
func commandArgvBytes(binary string, args []string) int {
	total := len(binary) + 1
	for _, arg := range args {
		total += len(arg) + 1
	}
	return total
}
