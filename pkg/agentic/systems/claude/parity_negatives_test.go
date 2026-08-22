package claude

import (
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// The parity package carries two permanent negatives that exist to bite PORTS:
// a ChildEnv that discards the whole parent environment, and one that strips
// the right families by prefix where the source strips by exact key. There they
// are driven through a hand-written probe. Here they are driven through THIS
// PLUGIN, because a probe that fails proves the fixture can catch the defect
// and says nothing about whether the shipped claude plugin has it.
//
// Both attacks wrap the real System and override exactly one method, so
// everything else about the plan — argv, binary, stdin — stays correct and the
// comparison isolates the environment.
//
// Both carry a NARROWING subtest: take the relevant keys back out of the
// recorded parent environment and the identical defect must walk straight
// through. Without that, "the golden caught it" would be equally consistent
// with the golden catching it for some unrelated reason, and the seeded keys
// would be decoration.

// claudeBystanders are the keys the capture seeds that NO claude filter
// touches. They are the seeded half of this plugin's preservation evidence.
var claudeBystanders = []string{
	"PARITY_BYSTANDER",
	"CODEX_LIKE_BUT_NOT",
	"CLAUDECODE_LIKE_BUT_NOT",
	"TASK_BOARD_LIKE_BUT_NOT",
}

// claudeIncidentalSurvivors are the keys a claude child keeps for a reason
// nobody maintains: this filter simply has no entry for the codex runtime
// family, because stripping it is filterCodexRuntimeEnv's job and the qwen
// filter is the one that composes the two.
//
// They are named separately from the bystanders because the difference matters
// to what the evidence is worth. A bystander is seeded on purpose and a fixture
// test fails if a recapture drops it; these survive by omission and would
// vanish the day someone widened runtimeEnvKeys for a good reason. A
// preservation bound resting on them would break silently, so the narrowing
// below proves the four seeded bystanders carry the bound without them.
var claudeIncidentalSurvivors = []string{
	"CODEX_THREAD_ID",
	"CODEX_SESSION",
	"CODEX_CI",
	"CODEX_MANAGED_BY_NPM",
	"CODEX_MANAGED_BY_BUN",
	"CODEX_MANAGED_PACKAGE_ROOT",
	"TASK_BOARD_CODEX_APP_SERVER_URL",
	"TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV",
	"PARITY_CODEX_APP_SERVER_TOKEN",
	"TASK_BOARD_SESSION_MANAGER_URL",
	"TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV",
	"PARITY_SESSION_MANAGER_TOKEN",
	"TASK_BOARD_SESSION_ID",
}

// claudeSurvivors is every key this filter preserves out of the pinned parent
// environment.
var claudeSurvivors = append(append([]string(nil), claudeBystanders...), claudeIncidentalSurvivors...)

// wholeEnvWipeSystem is the port defect every golden's env_removed list
// invites: a ChildEnv that returns only its own injections.
//
// It is written as the real ChildEnv over an EMPTY parent rather than as a
// hand-listed set of injections, which is what makes it a plausible mistake
// rather than a contrived one.
type wholeEnvWipeSystem struct{ *System }

func (w wholeEnvWipeSystem) ChildEnv(_ []string, req agentic.LaunchRequest) ([]string, error) {
	return w.System.ChildEnv(nil, req)
}

// prefixStripSystem is the second defect: filtering a whole family by PREFIX
// before handing the rest to the real filter.
//
// "Strip the CLAUDECODE family" is a reading of env.go a port author can arrive
// at honestly, and env.go strips by whole key, so the two differ only on keys
// that LOOK like the family without being in it. That is what the capture seeds
// CLAUDECODE_LIKE_BUT_NOT for, and a plain bystander cannot see the difference
// at all.
type prefixStripSystem struct {
	*System
	prefixes []string
}

func (p prefixStripSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	kept := make([]string, 0, len(parent))
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		over := false
		for _, prefix := range p.prefixes {
			if strings.HasPrefix(key, prefix) {
				over = true
				break
			}
		}
		if !over {
			kept = append(kept, entry)
		}
	}
	return p.System.ChildEnv(kept, req)
}

// withoutKeys drops the named entries from a recorded parent environment,
// reproducing the fixture shape that existed before they were seeded.
//
// It takes the keys explicitly rather than defaulting to the bystander list:
// every narrowing below removes a DIFFERENT subset, and which subset is the
// whole claim each one makes.
func withoutKeys(parent []string, drop ...string) []string {
	out := make([]string, 0, len(parent))
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		dropped := false
		for _, name := range drop {
			if key == name {
				dropped = true
				break
			}
		}
		if !dropped {
			out = append(out, entry)
		}
	}
	return out
}

// TestAWholeEnvironmentWipeFailsAgainstTheClaudeGolden is the preservation
// bound, against the shipped plugin.
//
// If this ever passes, the surviving keys have been dropped from a recapture
// and this plugin could return only its injections while every claude golden
// stayed green.
func TestAWholeEnvironmentWipeFailsAgainstTheClaudeGolden(t *testing.T) {
	const subject = "claude/prompt-mode"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)

	clean := buildParityPlan(t, New(), req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.substitutions()); len(diffs) != 0 {
		t.Fatalf("the correctly-filtered plan already differs, so the wipe below proves nothing: %v", diffs)
	}

	// The golden has to leave something to preserve. Without this the assertion
	// below could only ever pass by accident, and its failure would send the
	// reader hunting a comparator bug rather than a fixture that lost its
	// bystanders.
	removed := map[string]bool{}
	for _, entry := range g.Surface.EnvRemoved {
		removed[entry] = true
	}
	survivors := 0
	for _, entry := range g.Capture.ParentEnv {
		if !removed[entry] {
			survivors++
		}
	}
	if survivors == 0 {
		t.Fatalf("%s records %d parent entries and removes all %d of them, so no key proves this filter is SELECTIVE; reseed the bystander keys in .scripts/capture-parity-goldens.sh and recapture",
			subject, len(g.Capture.ParentEnv), len(g.Surface.EnvRemoved))
	}

	plan := buildParityPlan(t, wholeEnvWipeSystem{System: New()}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a claude plugin that discards the WHOLE parent environment byte-matched %s; the golden would prove only the filter's lower bound", subject)
	}
	if !namesField(diffs, "EnvRemoved") {
		t.Errorf("the wipe was planted in the environment but the harness reported %v", diffs)
	}

	// The narrowing that shows WHICH part of the fixture is doing the catching.
	// Take every key this filter preserves back out of the recorded parent
	// environment — the shape the fixture had before the bystanders were seeded
	// — and the identical wipe walks straight through.
	t.Run("and would not be caught without the surviving keys", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = withoutKeys(g.Capture.ParentEnv, claudeSurvivors...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("no surviving entry was removed, so this narrowing changed nothing; claudeSurvivors and the fixture's parent_env disagree")
		}
		narrowedReq := req
		narrowedReq.Env = withoutKeys(req.Env, claudeSurvivors...)
		wiped := buildParityPlan(t, wholeEnvWipeSystem{System: New()}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, wiped, dirs.substitutions()); len(diffs) != 0 {
			t.Fatalf("removing every surviving key did NOT restore the bypass (%v); something other than the survivors is catching the wipe, and the capture script's convention is not what this test protects", diffs)
		}
	})

	// And the half of that which the capture script is actually responsible
	// for. The codex family survives a claude child INCIDENTALLY — this filter
	// has no reason to strip it and only the qwen filter composes both — so it
	// is not evidence anyone maintains. Removing it must leave the wipe caught,
	// or this plugin's preservation bound would rest on keys a future filter
	// change could take away without anyone noticing.
	t.Run("and the seeded bystanders alone are enough", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = withoutKeys(g.Capture.ParentEnv, claudeIncidentalSurvivors...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("none of %v is in the fixture's parent_env, so this narrowing changed nothing", claudeIncidentalSurvivors)
		}
		narrowedReq := req
		narrowedReq.Env = withoutKeys(req.Env, claudeIncidentalSurvivors...)
		wiped := buildParityPlan(t, wholeEnvWipeSystem{System: New()}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, wiped, dirs.substitutions()); len(diffs) == 0 {
			t.Fatalf("with the incidental survivors removed the wipe byte-matched the fixture, so the four seeded bystanders are NOT carrying this bound on their own")
		}
	})
}

// TestAPrefixStripFailsAgainstTheClaudeGolden is the exact-key bound, against
// the shipped plugin.
//
// The subject prefix is CLAUDECODE alone. That is the narrow form of the defect
// on purpose: it over-strips CLAUDECODE_LIKE_BUT_NOT and disturbs nothing else,
// so the near-miss bystander is the ONLY thing that can be catching it — which
// is what the narrowing subtest then demonstrates by taking that key away.
//
// This is the seeded key's home. The capture script names CLAUDECODE_LIKE_BUT_NOT
// as pinning "the CLAUDECODE strip is an exact key, not a prefix match", and
// claude is the only system in the source whose filter is that strip alone.
func TestAPrefixStripFailsAgainstTheClaudeGolden(t *testing.T) {
	const subject = "claude/prompt-mode"
	const nearMiss = "CLAUDECODE_LIKE_BUT_NOT"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)
	prefixes := []string{sessionMarkerEnv}

	clean := buildParityPlan(t, New(), req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.substitutions()); len(diffs) != 0 {
		t.Fatalf("the correctly-filtered plan already differs, so the prefix strip below proves nothing: %v", diffs)
	}

	plan := buildParityPlan(t, prefixStripSystem{System: New(), prefixes: prefixes}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a claude plugin that strips %v by PREFIX byte-matched %s, but this filter strips by exact key; the near-miss bystander is not seeded", prefixes, subject)
	}
	if !namesField(diffs, "EnvRemoved") {
		t.Errorf("the over-strip was planted in the environment but the harness reported %v", diffs)
	}

	t.Run("and would not be caught by a plain bystander alone", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = withoutKeys(g.Capture.ParentEnv, nearMiss)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("%q is not in the fixture's parent_env, so this narrowing changed nothing", nearMiss)
		}
		narrowedReq := req
		narrowedReq.Env = withoutKeys(req.Env, nearMiss)
		over := buildParityPlan(t, prefixStripSystem{System: New(), prefixes: prefixes}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, over, dirs.substitutions()); len(diffs) != 0 {
			t.Fatalf("removing %q did NOT restore the bypass (%v); something other than the near-miss is catching the prefix strip, and the seeded key is unaccounted for", nearMiss, diffs)
		}
	})
}

// claudeTaskBoardSurvivors are the TASK_BOARD_-prefixed keys a claude child
// keeps, other than the seeded near-miss.
//
// They survive for the same incidental reason the CODEX_ family does — claude's
// filter is one key and none of these is it — and they matter here because they
// mean a TASK_BOARD_ prefix strip is over-stripping SEVERAL keys at once, not
// only the near-miss. The narrowing below has to take them out before it can
// say anything about what the near-miss is worth on its own.
var claudeTaskBoardSurvivors = []string{
	"TASK_BOARD_CODEX_APP_SERVER_URL",
	"TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV",
	"TASK_BOARD_SESSION_MANAGER_URL",
	"TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV",
	"TASK_BOARD_SESSION_ID",
}

// TestARunContextPrefixStripFailsAgainstTheClaudeGolden is the same exact-key
// bound one layer down, on the half of the environment contract this plugin
// does NOT own.
//
// agentic.WithRunContext replaces four whole keys through SetEnvValue. A port
// that reached for a TASK_BOARD_ prefix there would over-strip
// TASK_BOARD_LIKE_BUT_NOT, and because that code is shared by every plugin the
// defect would land in all six at once. Driving it through this plugin is what
// makes the shared function's bound this plugin's evidence too.
func TestARunContextPrefixStripFailsAgainstTheClaudeGolden(t *testing.T) {
	const subject = "claude/prompt-mode"
	const nearMiss = "TASK_BOARD_LIKE_BUT_NOT"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)
	prefixes := []string{"TASK_BOARD_"}

	plan := buildParityPlan(t, prefixStripSystem{System: New(), prefixes: prefixes}, req, c.mode)
	if diffs := parity.ComparePlan(g, plan, dirs.substitutions()); len(diffs) == 0 {
		t.Fatalf("a claude plugin that strips %v by PREFIX byte-matched %s; the run-context replacement is whole-key and nothing in the fixture is holding it there", prefixes, subject)
	}

	// Two narrowings, because unlike the CLAUDECODE case this defect
	// over-strips more than one key. The first establishes that the fixture's
	// TASK_BOARD_-shaped survivors are the whole of what catches it; the second
	// establishes that the SEEDED near-miss carries the bound on its own, which
	// is the only half a recapture is responsible for.
	t.Run("and would not be caught without any surviving TASK_BOARD_ key", func(t *testing.T) {
		drop := append(append([]string(nil), claudeTaskBoardSurvivors...), nearMiss)
		narrowed := g
		narrowed.Capture.ParentEnv = withoutKeys(g.Capture.ParentEnv, drop...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("none of %v is in the fixture's parent_env, so this narrowing changed nothing", drop)
		}
		narrowedReq := req
		narrowedReq.Env = withoutKeys(req.Env, drop...)
		over := buildParityPlan(t, prefixStripSystem{System: New(), prefixes: prefixes}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, over, dirs.substitutions()); len(diffs) != 0 {
			t.Fatalf("removing every surviving TASK_BOARD_ key did NOT restore the bypass (%v); something outside the environment is catching this strip", diffs)
		}
	})

	t.Run("and the seeded near-miss alone is enough", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = withoutKeys(g.Capture.ParentEnv, claudeTaskBoardSurvivors...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("none of %v is in the fixture's parent_env, so this narrowing changed nothing", claudeTaskBoardSurvivors)
		}
		narrowedReq := req
		narrowedReq.Env = withoutKeys(req.Env, claudeTaskBoardSurvivors...)
		over := buildParityPlan(t, prefixStripSystem{System: New(), prefixes: prefixes}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, over, dirs.substitutions()); len(diffs) == 0 {
			t.Fatalf("with only %q left to catch it, the prefix strip byte-matched the fixture; the seeded near-miss is NOT carrying this bound on its own and a recapture that dropped it would go unnoticed", nearMiss)
		}
	})
}

// TestThePluginPreservesEveryBystander is the same preservation bound stated
// positively and directly, so a reader does not have to derive it from two
// attack tests.
func TestThePluginPreservesEveryBystander(t *testing.T) {
	c := parityCaseFor(t, "claude/prompt-mode")
	g, _, req := prepareParityCase(t, c)
	plan := buildParityPlan(t, New(), req, c.mode)

	child := map[string]string{}
	for _, entry := range plan.Env {
		key, value, _ := strings.Cut(entry, "=")
		child[key] = value
	}
	for _, bystander := range claudeBystanders {
		want, seeded := "", false
		for _, entry := range g.Capture.ParentEnv {
			if key, value, _ := strings.Cut(entry, "="); key == bystander {
				want, seeded = value, true
			}
		}
		if !seeded {
			t.Fatalf("%q is not seeded in the golden's parent_env, so this test measures nothing", bystander)
		}
		got, present := child[bystander]
		if !present {
			t.Errorf("the claude child lost the bystander %q, which no claude filter touches", bystander)
			continue
		}
		if got != want {
			t.Errorf("the claude child rewrote the bystander %q from %q to %q", bystander, want, got)
		}
	}
}

func namesField(diffs []parity.Difference, field string) bool {
	for _, d := range diffs {
		if d.Field == field {
			return true
		}
	}
	return false
}
