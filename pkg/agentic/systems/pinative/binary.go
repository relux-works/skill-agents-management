package pinative

import (
	"github.com/relux-works/skill-agents-management/internal/launchenv"
)

// Native Pi has ONE binary-resolution path: whatever `pi` resolves to on the
// launch environment's PATH. The lookup reads the environment it was HANDED,
// never the process's own, so a dry run provably reports the same target a
// real launch would use.

// executableName is the Pi program name, spelled once.
const executableName = "pi"

// ErrNoPathInLaunchEnvironment is returned when the launch environment carries
// no PATH at all. It is a refusal rather than a fallback onto the ambient
// process PATH, aliased from internal/launchenv under this plugin's own name.
var ErrNoPathInLaunchEnvironment = launchenv.ErrNoPath

// resolveBinary returns the exact executable a launch will run.
func resolveBinary(env []string) (string, error) {
	return launchenv.LookPath(env, executableName)
}
