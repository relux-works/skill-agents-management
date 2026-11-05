package agy

import (
	"strings"
)

// AGY HAS NO PATH RESOLUTION AT ALL, and that is the sentence to read before
// anything else in this file.
//
// Every other ported system resolves its binary by looking `claude`, `codex`,
// `qwen`, `gemini` or `muse` up on the PATH the launch environment carries.
// This one does not, and the source says why in as many words: "agy has no PATH
// fallback: the Antigravity preflight (agy_preflight.go) is the only source of
// truth for its executable" (resolveAgyBinary, adapter.go).
//
// So there is no internal/launchenv import here, no executableName constant to
// look up, and no ErrNoPathInLaunchEnvironment sentinel — a port that carried
// them by symmetry with its neighbours would be adding a resolution path the
// harness does not have, and it would resolve a DIFFERENT binary from the one
// the preflight validated the headless contract of.
//
// The preflight itself, and how its result reaches this plugin, is runtime.go.

// displayPlaceholder is what a launch with no preflight evidence reports as its
// binary. It is the source's literal (resolveAgyBinaryForDisplay), and
// agy/dry-run's golden records exactly this string rather than a resolved path.
//
// It is a DISPLAY value: it names the program a reader would type, and nothing
// may exec it. System.Argv refuses an exec launch while this is the answer,
// which is what keeps the placeholder inside dry-run output where it belongs.
const displayPlaceholder = "agy"

// trimmedExecutable is the evidence's executable, whitespace-trimmed.
//
// The trim is the source's (resolveAgyBinary and resolveAgyBinaryForDisplay
// both call strings.TrimSpace) and it is load-bearing rather than tidy: a
// Runtime carrying " " is evidence of nothing, and Runtime.IsZero treats it as
// absent for the same reason. Without the trim, an executable with a trailing
// newline — the shape a caller gets from reading a probe's output badly — would
// be exec'd as a path that does not exist.
func trimmedExecutable(r Runtime) string { return strings.TrimSpace(r.Executable) }
