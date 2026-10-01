package pi

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

var (
	ErrProfileMissing             = errors.New("pi: launch profile is missing")
	ErrTurnPromptInvalid          = errors.New("pi: turn prompt is invalid")
	ErrSystemModelIdentityMissing = errors.New("pi: launch request has no provider-qualified model identity")
	ErrSystemModelIdentityInvalid = errors.New("pi: launch request has an invalid provider-qualified model identity")
)

// unattendedTools is the closed built-in set the standalone worker may use.
// Extension discovery and project-local trust are disabled separately below.
const unattendedTools = "read,bash,edit,write"

// Args builds native Pi argv. System.Argv additionally supplies the launch
// mode so dry-run can avoid reading PromptPath.
func Args(req agentic.LaunchRequest) ([]string, error) {
	return args(req, agentic.LaunchModeExec)
}

// interactiveArgs is the native interactive-session argv. The profile is a
// curator-engines status selector and is intentionally not a Pi flag. Pi's
// model selector is provider-qualified to avoid an ambiguous bare model id.
//
// Yolo is REFUSED here, not mapped: `pi --help` at the pinned 0.84.2 documents
// no permission-bypass flag equivalent to yolo; `--approve` trusts
// project-local files and is not equivalent. Refusing with
// ErrPermissionModeUnsupported is Decision 0018's Pi row.
func interactiveArgs(req agentic.LaunchRequest, effective agentic.PermissionMode) ([]string, error) {
	var model string

	if modelValue, err := nativeModelIdentity(req); err != nil {
		return nil, err
	} else {
		model = modelValue
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
	return append([]string{"--model", model}, nativeArgsSuffix(req)...), nil
}

func args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	var effective agentic.PermissionMode

	if effectiveValue, err := req.PermissionMode.Resolve(); err != nil {
		return nil, fmt.Errorf("pi: %w", err)
	} else {
		effective = effectiveValue
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
	var prepared agentic.LaunchRequest

	if preparedValue, err := prepareLaunchRequest(req, mode); err != nil {
		return nil, err
	} else {
		prepared = preparedValue
	}
	var model string

	if modelValue, err := nativeModelIdentity(prepared); err != nil {
		return nil, err
	} else {
		model = modelValue
	}
	prompt := "<prompt>"
	if mode != agentic.LaunchModeDryRun {
		prompt = string(prepared.Prompt)
	}
	return []string{
		"--no-approve",
		"--no-extensions",
		"--no-session",
		"--tools", unattendedTools,
		"--model", model,
		"--print",
		prompt,
	}, nil
}

func nativeModelIdentity(req agentic.LaunchRequest) (string, error) {
	identity := strings.TrimSpace(req.SystemModelIdentity)
	if identity == "" {
		return "", ErrSystemModelIdentityMissing
	}
	if strings.ContainsAny(identity, " \t\r\n") || strings.Count(identity, "/") != 1 {
		return "", fmt.Errorf("%w: %q must be one provider/model pair", ErrSystemModelIdentityInvalid, identity)
	}
	provider, model, _ := strings.Cut(identity, "/")
	if provider == "" || model == "" {
		return "", fmt.Errorf("%w: %q must name both provider and model", ErrSystemModelIdentityInvalid, identity)
	}
	return identity, nil
}

// nativeArgsSuffix returns the caller's native arguments for the verbatim
// interactive suffix, copied so the plan's argv never aliases the request's
// backing array.
func nativeArgsSuffix(req agentic.LaunchRequest) []string {
	return append([]string{}, req.NativeArgs...)
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
	var data []byte

	if dataValue, err := turnPrompt(req); err != nil {
		return agentic.LaunchRequest{}, err
	} else {
		data = dataValue
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
	if data[0] == '-' || data[0] == '@' {
		return nil, fmt.Errorf("%w: prompt starts with a Pi option or @file prefix", ErrTurnPromptInvalid)
	}
	return append([]byte(nil), data...), nil
}
