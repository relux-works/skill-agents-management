package gemini

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THE GEMINI CHILD-ENVIRONMENT CONTRACT IS AN EMPTY FILTER, AND THAT IS THE
// PORT'S MOST IMPORTANT SENTENCE ABOUT THIS FILE.
//
// The source is one line: `cmd.Env = withSpawnEnv(os.Environ(), cfg)`
// (skill-project-management, tools/board-cli/internal/spawn/spawn.go:1139,
// buildGeminiCommand). There is no filterEnv, no filterCodexRuntimeEnv, no
// PATH sanitization. A gemini child inherits the WHOLE parent environment, with
// the run context written over it and nothing else changed.
//
// # Why this file exists at all rather than just calling WithRunContext
//
// Because an empty filter is a DECISION, and a port that expressed it by
// leaving the file out would be indistinguishable from a port that forgot.
// A reader who has just come from qwen's env.go — twelve keys, a fixed leak,
// three declared-open ones — arrives here and has to be told, in the same
// place, that gemini strips nothing.
//
// # What a gemini child therefore inherits
//
// Everything the two goldens' pinned parent environment carries, and the
// goldens agree: gemini/exec's env_removed is FOUR entries, all of them the
// run-context keys withSpawnEnv replaces, and every CODEX_*, CLAUDECODE and
// credential key in parent_env survives into the child. Specifically:
//
//   - CLAUDECODE, so a gemini child launched from inside a Claude Code session
//     is told it is nested. Whether the gemini CLI reads that marker is not
//     something this repository has evidence about either way, and inventing a
//     strip on the guess would change a launch surface no golden covers.
//   - The whole CODEX_* family, including the parent's managed package root.
//   - TASK_BOARD_CODEX_APP_SERVER_URL / _AUTH_TOKEN_ENV and the credential each
//     names, TASK_BOARD_SESSION_MANAGER_URL / _AUTH_TOKEN_ENV and its
//     credential, and TASK_BOARD_SESSION_ID.
//   - TASK_BOARD_TOKEN and TASK_BOARD_BUILDER_GATEWAY_TOKEN, the board
//     credentials the source's BUG-260819-3qn52o names.
//
// That bug is the general shape: "each filter names the OTHER providers it knew
// about at the time it was written, so every new runtime silently opens a leak
// in both directions until someone edits N filters by hand". Gemini is the
// extreme case — its filter names nobody, so it leaks from every direction at
// once. The bug is in the source's backlog, unfixed at the commit these goldens
// were captured from, and its AC3 asks for a construction where "a child
// inherits only what its own runtime declares it needs". That is the fix, it is
// a design change across every plugin, and it is not this port's to make: doing
// it here would diverge one plugin from the source while the source's own bug
// stayed open, in a task whose acceptance is byte-identical launch surfaces.
//
// TestTheEmptyFilterIsDeliberate pins the inheritance, so the residual is
// visible in the suite rather than only in this comment. If it ever fails, a
// filter has been added — which may well be right, and the comment has to
// change with it or the code and the prose disagree.

// childEnv is the whole environment contract: the parent, plus the caller's run
// context written over it.
//
// There is no filter step to order this against, which is the one thing that
// makes gemini's version of this function shorter than every other plugin's.
// agentic.WithRunContext is the core's port of withSpawnEnv's run-context half,
// shared by all six systems because the source had exactly one of it.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	return agentic.WithRunContext(parent, req)
}
