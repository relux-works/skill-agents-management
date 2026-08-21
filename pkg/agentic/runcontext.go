package agentic

import (
	"path/filepath"
	"strings"
)

// This file is the port of the extraction source's withSpawnEnv and
// appendOrReplaceEnv (skill-project-management,
// tools/board-cli/internal/spawn/spawn.go).
//
// It lives in the core rather than in a plugin because the source had exactly
// one of it: every adapter's BuildCommand ended in
// `withSpawnEnv(<its own filter>(os.Environ()), cfg)`, and the run-context half
// of that expression was identical for all six. Copying it into six plugins
// would be six sources for one fact — invariant 5 of docs/architecture.md — and
// the divergence would be invisible, because a system that injects one variable
// differently still launches.
//
// What is NOT here is the per-system half: which parent keys a harness must not
// inherit, and any variable only one harness reads. Those are the plugin's, and
// a plugin composes them around these calls.

// The run-context variable names. They are the CALLER's convention — this
// module never reads a board — and they are spelled once, here, so that six
// plugins injecting the same context cannot disagree about a name.
//
// They are frozen for the same reason the runtime ids are: a child reads them
// to find the work it was launched for, and a rename is a spawned agent
// reporting against nothing with no error anywhere.
const (
	// EnvRunID and EnvTaskID identify the run and its unit of work.
	EnvRunID  = "TASK_BOARD_RUN_ID"
	EnvTaskID = "TASK_BOARD_TASK_ID"
	// EnvBoardDir is the selector the caller's CLI actually reads to find the
	// authoritative board. EnvLegacyBoardDir is separate run context that
	// predates it; the source carried both and so does this.
	EnvBoardDir       = "TASK_BOARD_DIR"
	EnvLegacyBoardDir = "TASK_BOARD_BOARD_DIR"
	// EnvDeliveryGoalID is the goal a goal-bound launch is bound to.
	EnvDeliveryGoalID = "TASK_BOARD_DELIVERY_GOAL_ID"
	// EnvContextID is the writable context the child may act on.
	EnvContextID = "TASK_BOARD_CONTEXT_ID"
)

// SetEnvValue returns environ with every entry for key removed and, when value
// is non-empty, one `key=value` entry appended.
//
// It is the source's appendOrReplaceEnv, ported with its two load-bearing
// properties intact:
//
//   - Matching is on the WHOLE key, not on a prefix. A prefix match would
//     rewrite TASK_BOARD_RUN_ID_SUFFIX while replacing TASK_BOARD_RUN_ID, and
//     the parity goldens seed TASK_BOARD_LIKE_BUT_NOT specifically because that
//     is a defect a port can introduce by writing one plausible line.
//   - An EMPTY value removes the key rather than exporting a blank one. A child
//     that inherits `TASK_BOARD_DELIVERY_GOAL_ID=` reads a goal id that is
//     present and meaningless; a child that inherits nothing reads an absence.
//     The parity goldens record the removal, not a blank, so the distinction is
//     pinned rather than described.
func SetEnvValue(environ []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		if entry == key || strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	if value != "" {
		out = append(out, prefix+value)
	}
	return out
}

// WithRunContext applies the caller's run context to a child environment.
//
// parent is the environment AFTER the calling plugin's own filtering: a plugin
// strips what its harness must not inherit, then hands the result here so the
// run context is written last and cannot be filtered back out by mistake.
//
// Every value is trimmed and an empty one removes its key, which is what makes
// "this launch has no goal" and "this launch has a goal" produce different
// child environments rather than a blank variable that reads as both.
func WithRunContext(parent []string, req LaunchRequest) []string {
	env := append([]string(nil), parent...)
	env = SetEnvValue(env, EnvRunID, strings.TrimSpace(req.Run.RunID))
	env = SetEnvValue(env, EnvTaskID, strings.TrimSpace(req.Run.TaskID))
	env = SetEnvValue(env, EnvLegacyBoardDir, strings.TrimSpace(req.Run.BoardDir))
	env = SetEnvValue(env, EnvBoardDir, AbsoluteBoardDir(req.Run.BoardDir))
	goalID := ""
	if req.Goal != nil {
		goalID = strings.TrimSpace(req.Goal.ID)
	}
	env = SetEnvValue(env, EnvDeliveryGoalID, goalID)
	env = SetEnvValue(env, EnvContextID, strings.TrimSpace(req.Run.ContextID))
	return env
}

// AbsoluteBoardDir resolves the board selector exported to a child.
//
// A relative path would be resolved by the child against the CHILD's working
// directory — which, for a launch into a story worktree, is a checkout artifact
// rather than the authoritative board. Making it absolute against this
// process's working directory is the source's fix for exactly that split.
//
// A path that cannot be made absolute is returned unchanged rather than
// dropped: exporting a worse selector is recoverable, exporting none silently
// re-enables the resolution that broke.
func AbsoluteBoardDir(boardDir string) string {
	trimmed := strings.TrimSpace(boardDir)
	if trimmed == "" {
		return ""
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return trimmed
	}
	return abs
}
