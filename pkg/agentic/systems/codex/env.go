package codex

import (
	"github.com/relux-works/skill-agents-management/internal/runtimeenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The codex child-environment contract, ported from the source's
// filterCodexRuntimeEnv / filterEnvKeys / sanitizeCodexPath family
// (skill-project-management, tools/board-cli/internal/spawn/spawn.go).
//
// What it is for: a spawned `codex exec` is a DELIBERATELY INDEPENDENT CLI
// process. A child that inherits the parent's thread, session and app-server
// state tries to attach to the parent's in-process client and dies before it
// ever reaches its assignment — a failure that looks like the model refusing
// the task rather than like an environment bug.
//
// # The strips are EXACT KEYS, not prefixes
//
// filterEnvKeys below builds a blocked-key MAP and compares whole keys. That
// is not an implementation detail to tidy up: `strings.HasPrefix(key,
// "CODEX_")` is the plausible one-character-different reading of this file,
// and it silently steals every CODEX_-shaped variable an operator set for
// their own reasons. The parity goldens seed CODEX_LIKE_BUT_NOT,
// CLAUDECODE_LIKE_BUT_NOT and TASK_BOARD_LIKE_BUT_NOT precisely so a prefix
// port fails, and TestAPrefixStripFailsAgainstTheCodexGolden drives that
// defect through this plugin rather than through a probe.
//
// # What this filter deliberately does NOT strip
//
// Two leaks are OPEN in the source and stay open here. They are the source's
// own BUG-260819-3qn52o, which is in backlog, unfixed, at the commit these
// goldens were captured from:
//
//  1. TASK_BOARD_TOKEN and TASK_BOARD_BUILDER_GATEWAY_TOKEN — board
//     credentials, not vendor ones — reach every codex child.
//  2. The reverse direction is uncovered: another runtime's session state
//     (QWEN_CODE_SESSION_ID is the recorded example) is not stripped from a
//     codex child, because this list was written before that runtime existed.
//
// Closing either one HERE would be a behaviour change no golden covers, in a
// port whose acceptance is byte-identical launch surfaces — and it would
// diverge this plugin from the source while the source's own bug stayed open,
// which is how two implementations of one contract start drifting. They are
// named here and pinned by TestTheSourcesOpenEnvLeaksStayOpen, so the residual
// is visible in the suite rather than only in prose. The general shape the bug
// records is the thing worth fixing, and it is not fixable one plugin at a
// time: every filter names the OTHER runtimes it knew about when it was
// written, so a seventh runtime opens a leak in both directions by existing.

// # Where the list itself lives
//
// The eleven keys, the whole-key filter, the pointer resolution and the PATH
// sanitizer are internal/runtimeenv, shared with the qwen plugin. The source
// shares them too — filterQwenRuntimeEnv is filterCodexRuntimeEnv plus
// CLAUDECODE, and the comment on it says the sharing is deliberate — so a copy
// of the list in each plugin would be the drift the source already closed. The
// names below are this package's own spellings of that shared vocabulary, kept
// so a reader of this file sees the contract without following an import.

// The parent-runtime keys a codex child must not inherit, in the source's
// order. Each is spelled as its own constant so the blocked set reads as a list
// of facts rather than a wall of strings.
const (
	// threadIDEnv, sessionEnv and ciEnv are the parent codex process's own
	// session identity. A child inheriting them attaches to the parent's
	// thread instead of starting its own.
	threadIDEnv = runtimeenv.ThreadIDEnv
	sessionEnv  = runtimeenv.SessionEnv
	ciEnv       = runtimeenv.CIEnv
	// managedByNPMEnv and managedByBunEnv record how the PARENT was installed.
	// They are install provenance, and a child resolves its own.
	managedByNPMEnv = runtimeenv.ManagedByNPMEnv
	managedByBunEnv = runtimeenv.ManagedByBunEnv
	// appServerURLEnv and sessionManagerURLEnv point at the in-process
	// services the parent is attached to.
	appServerURLEnv      = runtimeenv.AppServerURLEnv
	sessionManagerURLEnv = runtimeenv.SessionManagerURLEnv
	// appServerTokenNameEnv and sessionManagerTokenNameEnv do not hold
	// credentials — they hold the NAME of the variable that does. Stripping
	// only the pointer would leave the token itself in the child, which is why
	// filterRuntimeEnv reads each pointer and blocks what it names.
	appServerTokenNameEnv      = runtimeenv.AppServerTokenNameEnv
	sessionManagerTokenNameEnv = runtimeenv.SessionManagerTokenNameEnv
	// sessionIDEnv is the parent's manager-side session.
	sessionIDEnv = runtimeenv.SessionIDEnv
)

// ServiceTierEnv is the variable a codex child reads to learn which service
// tier its launch resolved to. It is exported because a caller that displays
// the resolved tier reads the same name.
//
// It is NOT in internal/runtimeenv: that package holds what a codex-family
// child must not INHERIT, and this is the one variable this plugin INJECTS.
// A shared helper carrying it would make one system's contract read as the
// family's.
const ServiceTierEnv = "TASK_BOARD_CODEX_SERVICE_TIER"

// runtimeEnvKeys is the fixed part of the blocked set, in the source's order.
//
// It is a package-level var holding a COPY of the shared list rather than an
// inline literal so a test can range over it —
// TestEveryStrippedKeyIsCarriedByTheGolden removes one key at a time and shows
// which golden entry each is responsible for. A list nothing can narrow is a
// list whose entries nobody can tell apart.
var runtimeEnvKeys = runtimeenv.Keys()

// filterRuntimeEnv strips the parent codex runtime state from an environment
// and sanitizes PATH. It is internal/runtimeenv's Filter, which is the source's
// filterCodexRuntimeEnv.
func filterRuntimeEnv(environ []string) []string { return runtimeenv.Filter(environ) }

// filterEnvKeys returns a copy of environ with the named keys removed, matching
// on the WHOLE key.
func filterEnvKeys(environ []string, keys ...string) []string {
	return runtimeenv.FilterKeys(environ, keys...)
}

// sanitizePath rewrites the PATH entry, dropping the directories a parent codex
// runtime injected into it.
func sanitizePath(environ []string) []string { return runtimeenv.SanitizePath(environ) }

// isRuntimePathEntry reports whether a PATH element was injected by a parent
// codex runtime.
func isRuntimePathEntry(path string) bool { return runtimeenv.IsRuntimePathEntry(path) }

// childEnv is the whole environment contract in one expression: strip what the
// harness must not inherit, then write the caller's run context and this
// system's own service-tier variable over the result.
//
// The order — filter, then inject — is the source's, and it is worth saying
// exactly what it is worth TODAY rather than asserting more than the tests
// hold. No key in runtimeEnvKeys is also a run-context key, so the two orders
// currently produce identical environments; a mutation run that swapped them
// left the whole suite green, and claiming the order was load-bearing would
// have been a comment with nothing behind it.
//
// What makes the order the right one anyway is the day that stops being true.
// A blocked key that collides with an injected one would, under the other
// order, have the filter delete what this function just wrote — a variable
// missing from the child with nothing reporting why.
// TestNoStrippedKeyCollidesWithAnInjectedOne pins the non-overlap, so the
// collision arrives as a failing test naming this comment rather than as a
// child that lost its run id.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	env := agentic.WithRunContext(filterRuntimeEnv(parent), req)
	// Codex is the one system in the source that exports its resolved service
	// tier to the child, which is why this line is here and not in
	// agentic.WithRunContext with the shared context.
	//
	// The guard is the source's, kept rather than simplified: writing an empty
	// tier through SetEnvValue would REMOVE an inherited value, and the source
	// lets it pass through. That pass-through is itself a residual — a launch
	// that configured no tier hands the child whatever tier its parent was
	// running under — and TestAnUnconfiguredTierLeavesTheInheritedValueAlone
	// pins it rather than leaving the difference to be discovered as a parity
	// failure by whoever ports the next system.
	if tier := NormalizeServiceTier(req.ServiceTier); tier != "" {
		env = agentic.SetEnvValue(env, ServiceTierEnv, tier)
	}
	return env
}
