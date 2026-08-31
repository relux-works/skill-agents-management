package qwen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// QWEN'S STDIN IS A PROTOCOL, NOT A PROMPT.
//
// Every other ported system either streams the assignment file verbatim on
// stdin (codex, claude prompt-mode, gemini) or attaches nothing (muse, agy).
// Qwen reads a two-line stream-json control stream, and the assignment is the
// text of the second line. This file is the port of buildQwenStreamInput
// (skill-project-management, tools/board-cli/internal/spawn/spawn.go).
//
// # It carries the effort, which nothing in argv does
//
// The first frame is the initialize control request, and its `effort` field is
// where a qwen launch's reasoning effort goes. That is the whole of
// EffortTransportStdin: a reviewer checking argv for a `--effort` flag would
// find none and conclude the value was dropped. qwen/exec's stdin_data records
// it, so the transport is compared byte for byte rather than asserted.
//
// # The encoding is exact, and every part of it is load-bearing
//
//   - MAPS, not structs. encoding/json sorts map keys, which is why the
//     goldens read {"request":...,"request_id":...,"type":...} in alphabetical
//     order rather than in the order the source wrote them. A struct port would
//     emit declaration order and differ from the fixture on byte one, and a
//     port that then "fixed" the fixture would have changed what the child
//     reads.
//   - SetEscapeHTML(false). The default encoder rewrites <, > and & into <
//     and friends. An assignment prompt containing a shell redirect or an HTML
//     tag would reach the child escaped, and the model would read the escape
//     sequence. The goldens' prompts contain none of those characters, so this
//     line is NOT proved by parity — TestThePromptIsNotHTMLEscaped is its only
//     evidence.
//   - Encode appends a newline per frame, which is what makes this a stream of
//     two lines rather than two concatenated documents. The trailing newline
//     after the second frame is in the golden.

// defaultSessionID is what the source falls back to when a launch carries
// neither a run id nor a task id. It is a literal rather than a generated
// value: two launches with no identity at all get the same session id, which is
// the source's behaviour and is visible here rather than hidden behind a
// generator that would make the fallback look safer than it is.
const defaultSessionID = "task-board-qwen"

// initializeRequestSuffix completes the initialize frame's request id.
const initializeRequestSuffix = "-initialize"

// streamInput builds the child's stdin.
//
// PromptPath is read when the caller supplied one, because that is what the
// source does and because a file read is the transport actually exercised in
// production. A read FAILURE is an error, never an empty payload: an absent
// prompt and an unreadable one are different facts, and returning "nothing
// attached" for a permissions error would launch a qwen child whose user frame
// carries an empty assignment and which reports the model produced no work.
//
// Prompt bytes are the alternative for a caller that has the text without a
// file.
//
// Supplying NEITHER attaches nothing at all — no frames, not even the
// initialize one. That is the state a dry run is in: BuildPlan calls this
// method for every mode, and the source's dry-run path (BuildArgs) never calls
// buildQwenStreamInput at all, so a dry run that failed here because no prompt
// file exists yet would be a dry run performing work the source does not do.
// Emitting a lone initialize frame instead would be worse than either: it would
// be a stdin payload the source never produces, recorded as if it were the
// contract.
func streamInput(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	prompt, attached, err := promptBytes(req)
	if err != nil || !attached {
		return agentic.StdinPayload{}, err
	}

	sessionID := sessionID(req)
	frames := []any{
		map[string]any{
			"type":       "control_request",
			"request_id": sessionID + initializeRequestSuffix,
			"request": map[string]any{
				"subtype": "initialize",
				"effort":  strings.TrimSpace(req.Effort),
			},
		},
		map[string]any{
			"type":       "user",
			"session_id": sessionID,
			"message": map[string]any{
				"role": "user",
				"content": []map[string]string{{
					"type": "text",
					"text": string(prompt),
				}},
			},
			"parent_tool_use_id": nil,
		},
	}

	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	encoder.SetEscapeHTML(false)
	for _, frame := range frames {
		if err := encoder.Encode(frame); err != nil {
			return agentic.StdinPayload{}, fmt.Errorf("qwen: encoding the stream input: %w", err)
		}
	}
	return agentic.StdinPayload{Attached: true, Bytes: input.Bytes()}, nil
}

// promptBytes reads the assignment, reporting whether there was one at all.
//
// The three-value return is the point: an error, an absence and an empty
// assignment are three different facts, and a caller collapsing the first two
// would turn an unreadable file into a launch with no stdin.
func promptBytes(req agentic.LaunchRequest) ([]byte, bool, error) {
	if path := strings.TrimSpace(req.PromptPath); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("qwen: reading the assignment prompt: %w", err)
		}
		return data, true, nil
	}
	if len(req.Prompt) > 0 {
		return append([]byte(nil), req.Prompt...), true, nil
	}
	return nil, false, nil
}

// sessionID is the source's fallback chain: the run id, then the task id, then
// a fixed literal.
//
// It is the id the CHILD reports its frames under, so it is not cosmetic: two
// concurrent launches that both fell through to the literal would report under
// one session. The chain is the source's and is ported rather than improved,
// because changing it would change what a real qwen child sends back.
func sessionID(req agentic.LaunchRequest) string {
	if id := strings.TrimSpace(req.Run.RunID); id != "" {
		return id
	}
	if id := strings.TrimSpace(req.Run.TaskID); id != "" {
		return id
	}
	return defaultSessionID
}
