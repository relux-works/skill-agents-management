package qwen

import (
	"github.com/relux-works/skill-agents-management/internal/runtimeenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The qwen child-environment contract, ported from the source's
// filterQwenRuntimeEnv (skill-project-management,
// tools/board-cli/internal/spawn/adapter.go:375):
//
//	func filterQwenRuntimeEnv(environ []string) []string {
//		return filterCodexRuntimeEnv(filterEnv(environ, "CLAUDECODE"))
//	}
//
// # The CLAUDECODE strip is a FIX, and it stays fixed
//
// The source's own comment on that function records what it was before: "Before
// this task buildQwenCommand passed raw os.Environ(): a qwen child inherited
// [the parent's runtime state]". The task was TASK-260817-2eo4ok. The concrete
// failure it closed is the CLAUDECODE marker — Claude Code sets it in every
// session it owns, a harness that sees it believes it is nested, and a qwen
// child launched from inside a Claude Code session refused to start.
//
// So this one key is not decoration on top of the codex list. It is the whole
// reason qwen has a filter of its own rather than reusing codex's, and a port
// that dropped it would reintroduce a fixed defect while every argv comparison
// stayed green. TestTheQwenChildLosesTheParentClaudeMarker pins it directly and
// TestEveryStrippedKeyIsCarriedByTheGolden narrows it against the fixture.
//
// # The rest of the list is the CODEX family, single-sourced
//
// filterCodexRuntimeEnv is internal/runtimeenv.Filter here, shared with the
// codex plugin. The source shares it too, and its comment says why in as many
// words: it "keeps this single-sourced instead of restating the list". A copy
// of the eleven keys in this file would be the drift that comment closed, and
// it would be invisible — a child that inherits one key too many still
// launches.
//
// # What this filter deliberately does NOT strip
//
// Three leaks are OPEN in the source and stay open here. All three are recorded
// in the source's own BUG-260819-3qn52o, which is in backlog, unfixed, at the
// commit these goldens were captured from:
//
//  1. TASK_BOARD_TOKEN and TASK_BOARD_BUILDER_GATEWAY_TOKEN — board
//     credentials, not vendor ones — reach every qwen child, because
//     filterCodexRuntimeEnv does not carry them and this filter wraps it
//     unchanged. The bug's own words: "these are board credentials, not vendor
//     ones, so the blast radius is the board itself rather than one vendor
//     account".
//  2. The REVERSE direction is uncovered. Qwen Code sets QWEN_CODE_SESSION_ID
//     into its own environment at session start; neither the claude filter nor
//     the codex list names any QWEN_ variable, so a claude or codex child
//     spawned from inside a qwen session inherits it.
//  3. And the same gap points at THIS plugin: nothing here strips
//     QWEN_CODE_SESSION_ID either, so a qwen child spawned from inside a qwen
//     session inherits its parent's session id. That is the identical defect,
//     one runtime closer to home, and it follows from the same cause — every
//     filter names the OTHER runtimes it knew about when it was written.
//
// Closing any of them HERE would be a behaviour change no golden covers, in a
// port whose acceptance is byte-identical launch surfaces, and it would diverge
// this plugin from the source while the source's own bug stayed open. They are
// named here and pinned by TestTheSourcesOpenEnvLeaksStayOpen, so the residual
// is visible in the suite rather than only in prose.
//
// # PATH sanitization comes along with the codex list
//
// runtimeenv.Filter rewrites PATH to drop a parent codex runtime's arg0-shim
// directories, so a qwen child gets that strip too — the source's own note on
// the parity capture says qwen's child PATH VALUE changed in the refactor those
// goldens proved, and that its harness could not see it. The goldens exclude
// PATH from the environment diff on both sides, so
// TestTheQwenChildPathIsSanitized is the only evidence this claim has.

// sessionMarkerEnv is the nesting marker Claude Code sets in a session it owns.
// A qwen child that inherits it believes it is nested and refuses to start.
const sessionMarkerEnv = "CLAUDECODE"

// runtimeEnvKeys is the WHOLE blocked set for a qwen child: the codex family
// plus the claude session marker.
//
// It is assembled here rather than hidden inside the shared helper because the
// extra key IS this plugin's contract, and a reader has to be able to see the
// difference between qwen's filter and codex's without following an import. It
// is a package-level var so a test can range over it — one key at a time is put
// back into the child and the golden must then fail, which is what tells the
// twelve entries apart from entries that were never there.
var runtimeEnvKeys = append(runtimeenv.Keys(), sessionMarkerEnv)

// filterRuntimeEnv strips the parent codex runtime state and the parent claude
// session marker, and sanitizes PATH.
//
// The composition order is the source's: CLAUDECODE first, then the codex
// filter. The two blocked sets are disjoint and the codex half resolves its
// credential pointers against whatever it is handed — neither of which
// CLAUDECODE is — so the orders produce identical environments today.
// TestTheFilterOrderDoesNotChangeTheResult pins that, so the day it stops being
// true arrives as a failing test naming this comment rather than as a child
// that kept a key somebody believed was stripped.
func filterRuntimeEnv(environ []string) []string {
	return runtimeenv.Filter(runtimeenv.FilterKeys(environ, sessionMarkerEnv))
}

// childEnv is the whole environment contract in one expression: strip what the
// harness must not inherit, then write the caller's run context over the result.
//
// The order — filter, then inject — is the source's. No key in runtimeEnvKeys
// is also a run-context key, so the two orders currently produce identical
// environments; what makes this the right one anyway is the day that stops
// being true, when a blocked key that collides with an injected one would,
// under the other order, have the filter delete what this function just wrote.
// TestNoStrippedKeyCollidesWithAnInjectedOne pins the non-overlap, so the
// collision arrives as a failing test naming this comment rather than as a
// child that lost its run id.
//
// Nothing qwen-specific is injected. Codex writes its resolved service tier;
// qwen's adapter declares SupportsServiceTier false and the source writes no
// qwen-only variable at all, so this is agentic.WithRunContext and nothing else
// — and the goldens' env_added carries exactly the run-context keys.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	return agentic.WithRunContext(filterRuntimeEnv(parent), req)
}
