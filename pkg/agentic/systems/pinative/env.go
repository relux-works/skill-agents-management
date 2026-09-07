package pinative

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// HomeEnvVar is the environment variable Pi reads its agent directory from
// (Pi 0.84.2, `PI_CODING_AGENT_DIR`). It is exported so a launcher composing
// the child environment and a limit-state query can spell the managed home
// from one constant.
const HomeEnvVar = "PI_CODING_AGENT_DIR"

// DefaultHome is Pi's conventional agent directory when HomeEnvVar is unset.
const DefaultHome = "~/.pi/agent"

// THE NATIVE-PI CHILD-ENVIRONMENT CONTRACT IS AN EMPTY FILTER, and it is
// written down rather than left out. The child inherits the whole parent
// environment with only the run context written over it. A launcher that
// wants the session in a managed home sets PI_CODING_AGENT_DIR on the
// request's Env; this plugin neither invents nor strips it, and
// TestTheManagedHomeVariableReachesTheChildVerbatim pins that.
//
// The plan's Home (BuildPlan: req.Home, else DefaultHome) is the value a
// provider-limit query must be keyed by; it is reported, not exported into
// the child here, because a launcher composing its own environment must be
// able to see the two agree rather than have one silently overwrite the other.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	return agentic.WithRunContext(parent, req)
}
