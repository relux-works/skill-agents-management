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
func interactiveArgs(req agentic.LaunchRequest) ([]string, error) {
	model := strings.TrimSpace(req.Model.ID)
	if model == "" {
		return nil, fmt.Errorf("pi: an interactive launch requires a model id")
	}
	return []string{"pi", "--model", model}, nil
}

func args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if mode == agentic.LaunchModeInteractive {
		return interactiveArgs(req)
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
		"--deadline", "30m",
		"--result-schema", "1",
	}, nil
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
