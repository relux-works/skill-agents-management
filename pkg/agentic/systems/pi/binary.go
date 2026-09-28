package pi

import (
	"github.com/relux-works/skill-agents-management/internal/launchenv"
)

// executableName is the retained native Pi worker binary.
const executableName = "pi"

// resolveBinary returns the exact executable a launch will run, read from
// the launch environment handed to this plugin rather than the process's
// own ambient PATH.
func resolveBinary(env []string) (string, error) {
	return launchenv.LookPath(env, executableName)
}
