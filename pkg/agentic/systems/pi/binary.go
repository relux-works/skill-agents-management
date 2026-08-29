package pi

import (
	"github.com/relux-works/skill-agents-management/internal/launchenv"
)

// executableName is Process A's OWN wrapper binary — never raw `pi`. The
// architecture decision is explicit that task-board's pi plugin resolves
// `agents-infra` on PATH as Process A's binary, and that `agents-infra pi`
// is what actually execs the real `pi` binary as ITS child.
const executableName = "agents-infra"

// resolveBinary returns the exact executable a launch will run, read from
// the launch environment handed to this plugin rather than the process's
// own ambient PATH.
func resolveBinary(env []string) (string, error) {
	return launchenv.LookPath(env, executableName)
}
