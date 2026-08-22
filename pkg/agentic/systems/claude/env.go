package claude

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The claude child-environment contract, ported from the source's
// buildClaudeCommand: `withSpawnEnv(filterEnv(os.Environ(), "CLAUDECODE"), cfg)`
// (skill-project-management, tools/board-cli/internal/spawn/spawn.go:934).
//
// # What it is for
//
// Claude Code sets CLAUDECODE in every session it owns and reads it to detect
// nesting, refusing to start inside another session. A spawn is a DELIBERATE
// launch, so the marker is cleared and the child starts its own session. The
// source's comment says exactly that and this is its port.
//
// # ONE key, and the ones it deliberately does NOT strip
//
// This is where a port that reasons from the codex plugin goes wrong, so it is
// stated against the source line rather than from memory. A claude child in the
// source inherits:
//
//   - the whole CODEX_* family, including CODEX_MANAGED_PACKAGE_ROOT;
//   - TASK_BOARD_CODEX_APP_SERVER_URL / _AUTH_TOKEN_ENV and the credential each
//     names;
//   - TASK_BOARD_SESSION_MANAGER_URL / _AUTH_TOKEN_ENV and its credential;
//   - TASK_BOARD_SESSION_ID.
//
// filterCodexRuntimeEnv strips those from a CODEX child. filterQwenRuntimeEnv
// is filterCodexRuntimeEnv PLUS CLAUDECODE, for a qwen child. Claude's filter
// is the other direction and stops at one key, because this list was written
// when the only nesting problem anyone had was claude inside claude.
//
// The claude goldens are the authority and they agree: env_removed for both
// claude cases is CLAUDECODE plus the four run-context keys withSpawnEnv
// replaces, and every seeded CODEX_* and credential key survives into the
// child. TestTheClaudeChildKeepsTheCodexFamily pins the survival, so the
// asymmetry is visible in the suite rather than only here.
//
// That is the SAME open bug the codex plugin records — the source's
// BUG-260819-3qn52o, in backlog and unfixed at the commit these goldens were
// captured from — seen from the other side: every filter names the OTHER
// runtimes it knew about when it was written, so a seventh runtime opens a leak
// in both directions by existing. Closing it HERE would be a behaviour change
// no golden covers, in a port whose acceptance is byte-identical launch
// surfaces, and it would diverge this plugin from the source while the source's
// own bug stayed open.
//
// # PATH is NOT sanitized
//
// codex rewrites PATH to drop the parent runtime's arg0-shim directories.
// Claude does not: filterEnv touches only the key it is given. The parity
// goldens cannot see this either way — the harness excludes PATH from its
// environment diff — so TestTheClaudeChildPathIsNotSanitized is the only
// evidence this claim has, and it exists for the same reason codex's
// path-sanitizing test does.

// sessionMarkerEnv is the nesting marker Claude Code sets in a session it owns.
// A child that inherits it refuses to start.
const sessionMarkerEnv = "CLAUDECODE"

// runtimeEnvKeys is the whole blocked set, in the source's order.
//
// It is a package-level var holding ONE entry rather than an inline constant so
// that TestEveryStrippedKeyIsCarriedByTheGolden can narrow it — remove an entry
// and the golden must fail — which is what tells a list's entries apart from
// entries that were never there. A list of one narrows to a list of none, and
// that is still the measurement worth having: it is what proves this plugin's
// single strip is the thing CLAUDECODE=1 in the golden's env_removed comes
// from, rather than something the run-context injection happens to also do.
var runtimeEnvKeys = []string{sessionMarkerEnv}

// filterRuntimeEnv strips the parent claude session marker from an environment.
//
// Matching is on the WHOLE key. That is not an implementation detail to tidy
// up: `strings.HasPrefix(key, "CLAUDECODE")` is the plausible
// one-character-different reading of this file and it silently steals
// CLAUDECODE_LIKE_BUT_NOT, which the capture seeds for exactly this plugin.
// TestAPrefixStripFailsAgainstTheClaudeGolden drives that defect through this
// plugin rather than through a probe.
//
// The exact-key rule is not restated here: agentic.SetEnvValue with an empty
// value IS "remove every entry for this whole key", it is the same function the
// run-context injection below replaces keys with, and one implementation of
// whole-key matching in this module is the point of invariant 5. A local loop
// here would be a second one, agreeing until the day one of them was widened.
func filterRuntimeEnv(environ []string) []string {
	env := environ
	for _, key := range runtimeEnvKeys {
		env = agentic.SetEnvValue(env, key, "")
	}
	return env
}

// childEnv is the whole environment contract in one expression: strip what the
// harness must not inherit, then write the caller's run context over the result.
//
// The order — filter, then inject — is the source's. No key in runtimeEnvKeys
// is also a run-context key, so the two orders currently produce identical
// environments; what makes this the right one anyway is the day that stops
// being true, when a blocked key that collides with an injected one would, under
// the other order, have the filter delete what this function just wrote.
// TestNoStrippedKeyCollidesWithAnInjectedOne pins the non-overlap, so the
// collision arrives as a failing test naming this comment rather than as a
// child that lost its run id.
//
// Nothing claude-specific is injected. Codex writes its resolved service tier;
// claude's adapter declares SupportsServiceTier false and the source writes no
// claude-only variable at all, so this is agentic.WithRunContext and nothing
// else — and the goldens' env_added carries exactly the run-context keys.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	return agentic.WithRunContext(filterRuntimeEnv(parent), req)
}
