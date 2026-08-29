package pi

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// PI'S CHILD-ENVIRONMENT CONTRACT PASSES THE PARENT THROUGH UNFILTERED, and
// unlike muse's identically-shaped empty filter this one is load-bearing
// rather than merely undocumented: the architecture decision requires any
// AGENTS_INFRA_CALLER_CWD entry local-models's Spawn added to reach
// `agents-infra pi`'s own child environment VERBATIM — that variable is how
// `agents-infra pi` learns which project's project-config.toml to load. A
// filter here that stripped or rewrote it would silently break the one
// transport this design relies on to route Process A at the right
// workstation checkout.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	return agentic.WithRunContext(parent, req)
}
