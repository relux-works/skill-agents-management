package muse

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THE MUSE CHILD-ENVIRONMENT CONTRACT IS AN EMPTY FILTER, exactly as gemini's
// is, and for the same reason it is written down rather than left out.
//
// The source is one line: `cmd.Env = withSpawnEnv(os.Environ(), cfg)`
// (skill-project-management, tools/board-cli/internal/spawn/spawn.go:1107,
// buildMuseCommand). No filterEnv, no filterCodexRuntimeEnv, no PATH
// sanitization. A muse child inherits the WHOLE parent environment, with the
// run context written over it and nothing else changed.
//
// A reader arriving from qwen's env.go — twelve keys, a fixed leak, three
// declared-open ones — has to be told here, in the same place, that muse strips
// nothing. An empty filter expressed by omitting the file would be
// indistinguishable from a port that forgot.
//
// # What a muse child therefore inherits
//
// Everything the goldens' pinned parent environment carries, and muse/exec
// agrees: its env_removed is FOUR entries, all of them the run-context keys
// withSpawnEnv replaces. So CLAUDECODE, the whole CODEX_* family, both
// app-server and session-manager URLs, the credentials their pointer variables
// name, TASK_BOARD_SESSION_ID, TASK_BOARD_TOKEN and
// TASK_BOARD_BUILDER_GATEWAY_TOKEN all reach a muse child.
//
// That is the source's BUG-260819-3qn52o, in backlog and unfixed at the commit
// these goldens were captured from. Its general shape is what matters: "each
// filter names the OTHER providers it knew about at the time it was written, so
// every new runtime silently opens a leak in both directions until someone
// edits N filters by hand". Muse's filter names nobody at all. Closing it here
// would diverge one plugin from the source while the source's own bug stayed
// open, in a task whose acceptance is byte-identical launch surfaces.
//
// TestTheEmptyFilterIsDeliberate pins the inheritance, so the residual is
// visible in the suite rather than only in this comment.

// childEnv is the whole environment contract: the parent, plus the caller's run
// context written over it.
//
// agentic.WithRunContext is the core's port of withSpawnEnv's run-context half,
// shared by all six systems because the source had exactly one of it.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	return agentic.WithRunContext(parent, req)
}
