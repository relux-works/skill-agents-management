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
// # Exec and dry-run differ in exactly ONE argument
//
// museDryRunArgs substitutes `<prompt-file>` for the assignment path when the
// config carries none. Muse is the only ported system whose assignment is a
// PATH in argv rather than bytes on stdin, which is why it is also the only one
// besides claude with a placeholder to substitute at all.
//
// # The optional flags are conditional, and the conditions are the source's
//
// `--workspace` is emitted only for a non-empty work directory and
// `--prompt-file` only for a non-empty path. A port that emitted either flag
// with an empty value would hand the child an argument naming nothing, which
// muse would read as a workspace at the process's own working directory or as
// an unreadable assignment.
//
// `--reasoning-effort` follows the same rule for the same reason, and it is the
// one flag here the extraction source never spelled: muse carried no effort at
// all when these goldens were captured, and the two muse goldens therefore
// record no effort pair. It is spelled here because muse-spark-1.3-contributor
// declares a required effort axis in the vendor layer, and a transport that did
// not exist would mean the operator's configured word reached nothing.
//
// The VALUE is passed through verbatim. This file does not know — and must not
// learn — which words the installed muse build accepts: that is invariant 4 of
// docs/architecture.md, and it was load-bearing in fact, not just in theory.
// The model's vocabulary is high/xhigh/max and muse 1.0.2 documented
// none|minimal|low|medium|high|xhigh|ultra, so `max` was a word this plugin
// transported and that CLI refused. muse 1.3.0 accepts it. Enumerating any
// CLI's set here to pre-empt a refusal would put a harness build number in the
// argv builder and would keep refusing words the next build already accepts.

// promptFilePlaceholder is what a dry run reports where a real launch would
// name an assignment file that does not exist yet. It is the source's literal
// (adapter.go, museDryRunArgs).
const (
	promptFilePlaceholder   = "<prompt-file>"
	museYoloFlag            = "--yolo"
	museDisableApprovalFlag = "--disable-approval"
	museDisableSandboxFlag  = "--disable-sandbox"
)

// Args dispatches each supported Muse launch mode to its argv builder,
// excluding the binary. System.Argv is the only production entry to this
// package's launch grammar: exec and dry-run are built here, and interactive
// is delegated to interactiveArgs.
//
// The ORDER for Meta is the source's, verbatim. The Muse-specific pseudo-model
// "echo" selects the offline provider through the existing Model.ID contract;
// Muse 1.4.1 documents model and reasoning-effort flags for non-echo and
// Meta providers respectively, so echo emits neither flag.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if mode == agentic.LaunchModeInteractive {
		return interactiveArgs(req)
	}
	if !req.PermissionMode.IsZero() {
		return nil, fmt.Errorf("muse: %w: permission mode %q is valid only for interactive launches",
			agentic.ErrPermissionModeNotInteractive, strings.TrimSpace(string(req.PermissionMode)))
	}
	if len(req.NativeArgs) != 0 {
		return nil, fmt.Errorf("muse: %w: %d native argument(s) reach no verbatim suffix outside an interactive launch",
			agentic.ErrNativeArgsNotInteractive, len(req.NativeArgs))
	}
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
	// Exec is Muse's headless-child form, so its bypass posture is fixed. The
	// dry-run uses this same grammar and therefore reports the same posture.
	args = append(args, "exec", "--json", museYoloFlag)
	if strings.TrimSpace(req.Model.ID) == "echo" {
		args = append(args, "--provider", "echo")
	} else {
		args = append(args, "--model", strings.TrimSpace(req.Model.ID))
	}
	if effort := strings.TrimSpace(req.Effort); effort != "" && strings.TrimSpace(req.Model.ID) != "echo" {
		// Pure TRANSPORT, in the position claude's `--effort` occupies: right
		// after the model it qualifies. BuildPlan has already refused an effort
		// this system could not carry and refused a required-effort model with
		// no word, so reaching this line means a value the operator chose.
		args = append(args, "--reasoning-effort", effort)
	}
	if workDir := req.WorkDir; workDir != "" {
		args = append(args, "--workspace", workDir)
	}
	if promptFile != "" {
		args = append(args, "--prompt-file", promptFile)
	}
	return args, nil
}

// interactiveArgs builds the no-subcommand TUI form. The TUI accepts the same
// model, effort, and workspace options as Muse exec, but it receives no
// assignment protocol: NativeArgs is the caller's verbatim optional prompt and
// option suffix.
func interactiveArgs(req agentic.LaunchRequest) ([]string, error) {
	effective, err := req.PermissionMode.Resolve()
	if err != nil {
		return nil, fmt.Errorf("muse: %w", err)
	}
	var args []string
	if strings.TrimSpace(req.Model.ID) == "echo" {
		args = append(args, "--provider", "echo")
	} else {
		args = append(args, "--model", strings.TrimSpace(req.Model.ID))
	}
	if effort := strings.TrimSpace(req.Effort); effort != "" && strings.TrimSpace(req.Model.ID) != "echo" {
		args = append(args, "--reasoning-effort", effort)
	}
	if req.WorkDir != "" {
		args = append(args, "--workspace", req.WorkDir)
	}
	if effective == agentic.PermissionModeYolo {
		mapping, err := permissionMapping(req.ToolRelease, effective)
		if err != nil {
			return nil, err
		}
		if err := scanMuseNativePolicy(req.NativeArgs); err != nil {
			return nil, fmt.Errorf("muse: %w", err)
		}
		args = append(args, mapping.Flag)
	}
	return append(args, append([]string(nil), req.NativeArgs...)...), nil
}
