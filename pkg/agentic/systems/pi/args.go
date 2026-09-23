package pi

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

var (
	ErrProfileMissing    = errors.New("pi: launch profile is missing")
	ErrTurnPromptInvalid = errors.New("pi: turn prompt is invalid")
)

// Args builds the exact Process-A exec argv. It remains exported for callers
// that used the v0.5.0 helper; System.Argv additionally supplies the launch
// mode so dry-run can avoid reading PromptPath.
func Args(req agentic.LaunchRequest) ([]string, error) {
	return args(req, agentic.LaunchModeExec)
}

// interactiveArgs is the interactive primary-session argv (curator-spec
// Decision 0013 §5): `pi --model <id>` on the same agents-infra wrapper the
// exec mode resolves. The wrapper hands every argument that is not a `spawn`,
// `turn` or `lifecycle` subcommand to its interactive session launcher
// (relux-agents-infra, tools/agents-infra/main.go runPi), and raw pi accepts
// `--model <pattern>` (checked against `pi --help` at 0.84.2). This system
// declares EffortTransportNone, so there is no effort flag to add and BuildPlan
// has already refused a request carrying one.
//
// The profile is deliberately NOT spelled. It is the Process-A lease assertion
// of the `spawn` subcommand; the interactive wrapper resolves its own profile
// from the project configuration under AGENTS_INFRA_CALLER_CWD, which env.go
// passes through verbatim. A Profile arriving on the request — local-models'
// Spawn always contributes one — therefore reaches no flag here, which is the
// wrapper's contract rather than a drop.
//
// Yolo is REFUSED here, not mapped: `pi --help` at the pinned 0.84.2 documents
// no permission-bypass flag — only `--approve`/`-a` ("Trust project-local
// files for this run") and `--no-approve`/`-na`, which curator-spec Decision
// 0018 records as not equivalent — and `agents-infra pi --help` passes that
// same help through, so the wrapper adds no approval flag either. Refusing
// with ErrPermissionModeUnsupported is Decision 0018's pi row.
func interactiveArgs(req agentic.LaunchRequest, effective agentic.PermissionMode) ([]string, error) {
	model := strings.TrimSpace(req.Model.ID)
	if model == "" {
		return nil, fmt.Errorf("pi: an interactive launch requires a model id")
	}
	if effective == agentic.PermissionModeYolo {
		// Drift fails closed before support is even asked: an unpinned
		// or newer release, and an empty one, refuse as unverified,
		// and only the verified release reaches the unsupported
		// refusal below.
		if _, err := agentic.LookupReleaseCapability(verifiedReleases, req.ToolRelease); err != nil {
			return nil, fmt.Errorf("pi: %w", err)
		}
		return nil, fmt.Errorf("pi: refusing yolo: %w: pi 0.84.2 documents no interactive permission-bypass flag; --approve trusts project-local files for this run and is not equivalent",
			agentic.ErrPermissionModeUnsupported)
	}
	return append([]string{"pi", "--model", model}, nativeArgsSuffix(req)...), nil
}

func args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	effective, err := req.PermissionMode.Resolve()
	if err != nil {
		return nil, fmt.Errorf("pi: %w", err)
	}
	if mode != agentic.LaunchModeInteractive && !req.PermissionMode.IsZero() {
		return nil, fmt.Errorf("pi: %w: permission mode %q is valid only for interactive launches",
			agentic.ErrPermissionModeNotInteractive, strings.TrimSpace(string(req.PermissionMode)))
	}
	if mode != agentic.LaunchModeInteractive && len(req.NativeArgs) != 0 {
		return nil, fmt.Errorf("pi: %w: %d native argument(s) reach no verbatim suffix outside an interactive launch",
			agentic.ErrNativeArgsNotInteractive, len(req.NativeArgs))
	}
	if mode == agentic.LaunchModeInteractive {
		return interactiveArgs(req, effective)
	}
	prepared, err := prepareLaunchRequest(req, mode)
	if err != nil {
		return nil, err
	}
	profile := prepared.Profile
	prompt := "<prompt>"
	if mode != agentic.LaunchModeDryRun {
		prompt = string(prepared.Prompt)
	}
	return []string{
		"pi", "spawn",
		"--profile", profile,
		"--prompt", prompt,
		"--deadline", deadlineArg(req.Deadline),
		"--result-schema", "1",
	}, nil
}

// nativeArgsSuffix returns the caller's native arguments for the verbatim
// interactive suffix, copied so the plan's argv never aliases the request's
// backing array.
func nativeArgsSuffix(req agentic.LaunchRequest) []string {
	return append([]string{}, req.NativeArgs...)
}

// defaultDeadline is the Process-A deadline spelled when the caller declared
// none. It is the historical constant, kept only as the zero-value fallback:
// a caller with a fence of its own (task-board's hard timeout) passes it on
// LaunchRequest.Deadline and the child gets that fence, not this one.
const defaultDeadline = "30m"

// deadlineArg spells the caller's deadline the way `agents-infra pi spawn
// --deadline` parses it (a Go duration). A zero or negative deadline is "none
// declared" and yields the documented default, spelled exactly as before so a
// plan without a declared deadline stays byte-identical.
func deadlineArg(deadline time.Duration) string {
	if deadline <= 0 {
		return defaultDeadline
	}
	return deadline.String()
}

func prepareLaunchRequest(req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.LaunchRequest, error) {
	if mode == agentic.LaunchModeInteractive {
		return req, nil
	}
	if strings.TrimSpace(req.Profile) == "" {
		return agentic.LaunchRequest{}, fmt.Errorf("%w: the vendor must resolve an exact profile before planning", ErrProfileMissing)
	}
	if mode == agentic.LaunchModeDryRun {
		return req, nil
	}
	data, err := turnPrompt(req)
	if err != nil {
		return agentic.LaunchRequest{}, err
	}
	prepared := req
	prepared.PromptPath = ""
	prepared.Prompt = data
	return prepared, nil
}

func turnPrompt(req agentic.LaunchRequest) ([]byte, error) {
	data := req.Prompt
	if path := strings.TrimSpace(req.PromptPath); path != "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%w: reading PromptPath: %w", ErrTurnPromptInvalid, err)
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: prompt is empty", ErrTurnPromptInvalid)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: prompt is not UTF-8", ErrTurnPromptInvalid)
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return nil, fmt.Errorf("%w: prompt contains NUL", ErrTurnPromptInvalid)
	}
	return append([]byte(nil), data...), nil
}
