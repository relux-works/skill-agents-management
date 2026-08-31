package pi

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}
