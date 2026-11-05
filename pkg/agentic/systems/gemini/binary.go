package gemini

import (
	"github.com/relux-works/skill-agents-management/internal/launchenv"
)

// Gemini has ONE binary-resolution path: whatever `gemini` resolves to on PATH.
//
// The source is one line — `func(*Config) (string, error) { return
// exec.LookPath("gemini") }` in the adapter table, and the identical lookup
// inside buildGeminiCommand. There is no managed npm package root, no shim to
// unwrap and no preflighted runtime, so none of codex's or agy's resolution
// machinery is imported here. A port that reached for it would be carrying
// ordered candidate paths for a harness that has one, and every one of them
// would be dead code nothing could ever cover.
//
// The one reshape is the same one every other ported plugin makes and for the
// same reason: the lookup reads the environment it was HANDED rather than the
// process's own. The System contract states that a plugin reaches for no
// ambient state and that a dry run must report the same target a real launch
// would use, and both are unprovable while resolution reads a process global.

// executableName is the gemini program name, spelled once.
const executableName = "gemini"

// ErrNoPathInLaunchEnvironment is returned when the launch environment carries
// no PATH at all.
//
// It is a REFUSAL rather than a fallback onto the ambient process PATH, and it
// is deliberately distinct from "gemini is not installed": a caller that
// curated an environment without PATH did not ask this plugin to substitute its
// own, and telling it the binary is missing sends it to fix the wrong thing.
//
// The sentinel is internal/launchenv's, aliased under this plugin's own name so
// a caller holding only this package can match on it. An alias rather than a
// wrapper: two sentinels for one condition is two things a caller has to know
// about, and errors.Is would answer differently depending on which one a future
// edit returned.
var ErrNoPathInLaunchEnvironment = launchenv.ErrNoPath

// resolveBinary returns the exact executable a launch will run.
func resolveBinary(env []string) (string, error) {
	return launchenv.LookPath(env, executableName)
}
