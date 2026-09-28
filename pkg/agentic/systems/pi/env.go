package pi

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Pi inherits the parent environment unfiltered. This preserves
// CURATOR_ENGINES_PROJECT_DIR for status preflight, which must query the same
// project directory declared by the local-models pointer.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	return agentic.WithRunContext(parent, req)
}
