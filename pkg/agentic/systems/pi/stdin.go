package pi

import (
	"fmt"
	"os"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// stdin is the assignment transport, until case 17's pinned fixture
// establishes pi's real wire protocol (see args.go): the PromptPath file's
// bytes when supplied, the Prompt bytes directly otherwise, or nothing
// attached at all — the same precedented fallback gemini's Stdin uses.
//
// A read FAILURE is an error, never an empty payload: an absent prompt and
// an unreadable one are different facts.
func stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	if path := strings.TrimSpace(req.PromptPath); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return agentic.StdinPayload{}, fmt.Errorf("pi: reading the assignment prompt: %w", err)
		}
		return agentic.StdinPayload{Attached: true, Bytes: data}, nil
	}
	if len(req.Prompt) > 0 {
		return agentic.StdinPayload{Attached: true, Bytes: append([]byte(nil), req.Prompt...)}, nil
	}
	return agentic.StdinPayload{}, nil
}
