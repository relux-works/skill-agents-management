package qwen

import (
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// QWEN IS THE FIXTURE THE BYSTANDER KEYS WERE SEEDED FOR.
//
// The goldens' README says so directly: filterQwenRuntimeEnv is
// filterCodexRuntimeEnv plus CLAUDECODE, and between them they strip every
// other key in the pinned parent environment — so without a surviving key,
// qwen/exec's env_removed would cover 100% of its parent_env, and a port whose
// ChildEnv DISCARDS THE WHOLE PARENT ENVIRONMENT would produce a byte-identical
// diff. The shape of such a golden actively invites that defect: a port author
// reading "everything removed" writes "return only the injections" and passes.
//
// The parity package carries both attacks against a hand-written probe. Here
// they are driven through THIS PLUGIN, because a probe that fails proves the
// fixture can catch the defect and says nothing about whether the shipped qwen
// plugin has it.
//
// Both carry the narrowing subtest as well: take the surviving keys back out of
// the recorded parent environment and the identical defect must walk straight
// through. Without that, "the golden caught it" would be equally consistent
// with the golden catching it for some unrelated reason, and the four seeded
// bystanders would be decoration.

// qwenBystanders are the keys the capture seeds that NO qwen filter touches.
//
// Unlike codex, this plugin has no INCIDENTAL survivor to fall back on:
// CLAUDECODE is exactly what qwen's filter adds to codex's, so these four are
// the whole of qwen's preservation evidence. That is the situation the capture
// script's bystander convention exists for.
var qwenBystanders = []string{
	"PARITY_BYSTANDER",
	"CODEX_LIKE_BUT_NOT",
	"CLAUDECODE_LIKE_BUT_NOT",
	"TASK_BOARD_LIKE_BUT_NOT",
}

// TestAWholeEnvironmentWipeFailsAgainstTheQwenGolden is the preservation bound,
// against the shipped plugin.
//
// If this ever passes, the bystander keys have been dropped from a recapture
// and this plugin could return only its injections while every qwen golden
// stayed green.
func TestAWholeEnvironmentWipeFailsAgainstTheQwenGolden(t *testing.T) {
	const subject = "qwen/exec"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)

	clean := paritycase.BuildPlan(t, New(), req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
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

	plan := paritycase.BuildPlan(t, paritycase.WholeEnvWipeSystem{System: New()}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a qwen plugin that discards the WHOLE parent environment byte-matched %s; the golden would prove only the filter's lower bound", subject)
	}
	if !paritycase.NamesField(diffs, "EnvRemoved") {
		t.Errorf("the wipe was planted in the environment but the harness reported %v", diffs)
	}

	// The narrowing that shows WHICH part of the fixture is doing the catching.
	// Take the four seeded bystanders back out of the recorded parent
	// environment — the shape the fixture had before they were seeded — and the
	// identical wipe walks straight through.
	t.Run("and would not be caught without the seeded bystanders", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = paritycase.WithoutKeys(g.Capture.ParentEnv, qwenBystanders...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("no bystander was removed, so this narrowing changed nothing; qwenBystanders and the fixture's parent_env disagree")
		}
		narrowedReq := req
		narrowedReq.Env = paritycase.WithoutKeys(req.Env, qwenBystanders...)
		wiped := paritycase.BuildPlan(t, paritycase.WholeEnvWipeSystem{System: New()}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, wiped, dirs.Substitutions()); len(diffs) != 0 {
			t.Fatalf("removing the four seeded bystanders did NOT restore the bypass (%v); something other than them is catching the wipe, and the capture script's convention is not what this test protects", diffs)
		}
	})
}

// TestAPrefixStripFailsAgainstTheQwenGolden is the exact-key bound, against the
// shipped plugin.
//
// The subject prefixes are CODEX_ and CLAUDECODE — the two families qwen's
// filter names — in their narrow form: they over-strip CODEX_LIKE_BUT_NOT and
// CLAUDECODE_LIKE_BUT_NOT and disturb nothing else, so the near-miss bystanders
// are the ONLY thing that can be catching it, which the narrowing subtest then
// demonstrates by taking them away.
func TestAPrefixStripFailsAgainstTheQwenGolden(t *testing.T) {
	const subject = "qwen/exec"
	nearMisses := []string{"CODEX_LIKE_BUT_NOT", "CLAUDECODE_LIKE_BUT_NOT"}
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)
	prefixes := []string{"CODEX_", "CLAUDECODE"}

	clean := paritycase.BuildPlan(t, New(), req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
		t.Fatalf("the correctly-filtered plan already differs, so the prefix strip below proves nothing: %v", diffs)
	}

	plan := paritycase.BuildPlan(t, paritycase.PrefixStripSystem{System: New(), Prefixes: prefixes}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a qwen plugin that strips %v by PREFIX byte-matched %s, but this filter strips by exact key; the near-miss bystanders are not seeded", prefixes, subject)
	}
	if !paritycase.NamesField(diffs, "EnvRemoved") {
		t.Errorf("the over-strip was planted in the environment but the harness reported %v", diffs)
	}

	t.Run("and would not be caught by a plain bystander alone", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = paritycase.WithoutKeys(g.Capture.ParentEnv, nearMisses...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("%v are not in the fixture's parent_env, so this narrowing changed nothing", nearMisses)
		}
		narrowedReq := req
		narrowedReq.Env = paritycase.WithoutKeys(req.Env, nearMisses...)
		over := paritycase.BuildPlan(t, paritycase.PrefixStripSystem{System: New(), Prefixes: prefixes}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, over, dirs.Substitutions()); len(diffs) != 0 {
			t.Fatalf("removing %v did NOT restore the bypass (%v); something other than the near-misses is catching the prefix strip, and the seeded keys are unaccounted for", nearMisses, diffs)
		}
	})
}

// TestAPrefixStripAlsoLeaksThePointedAtCredentials records the SECOND, separate
// consequence of the same defect, which qwen inherits from the codex filter it
// wraps.
//
// The filter does not carry a fixed list of credential variables: it reads
// TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV and
// TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV, each of which holds the NAME of
// the variable that holds the token, and blocks what they name. A port that
// strips the TASK_BOARD_ family by prefix removes those two pointers BEFORE the
// filter can read them — so the tokens they point at are never blocked and
// reach the child.
//
// So the prefix reading does not merely over-strip: it under-strips exactly the
// credentials this filter exists to contain. That is worth its own test because
// the two consequences are independent — a fixture that lost its near-miss
// bystanders would still catch this one, and a filter that stopped resolving
// pointers would still pass the test above.
func TestAPrefixStripAlsoLeaksThePointedAtCredentials(t *testing.T) {
	c := parityCaseFor(t, "qwen/exec")
	_, _, req := prepareParityCase(t, c)
	leaked := []string{"PARITY_CODEX_APP_SERVER_TOKEN", "PARITY_SESSION_MANAGER_TOKEN"}

	correct := paritycase.BuildPlan(t, New(), req, c.mode)
	for _, key := range leaked {
		if _, present := paritycase.Lookup(correct.Env, key); present {
			t.Fatalf("the correct plugin already leaks %q, so this test measures nothing", key)
		}
	}

	defective := paritycase.BuildPlan(t, paritycase.PrefixStripSystem{System: New(), Prefixes: []string{"CODEX_", "TASK_BOARD_"}}, req, c.mode)
	for _, key := range leaked {
		if _, present := paritycase.Lookup(defective.Env, key); !present {
			t.Errorf("the prefix strip did not leak %q; the pointer-resolution consequence this test records is not reproducing, so either the defect or the filter has changed shape", key)
		}
	}
}

// TestThePluginPreservesEveryBystander is the same preservation bound stated
// positively and directly, so a reader does not have to derive it from two
// attack tests.
func TestThePluginPreservesEveryBystander(t *testing.T) {
	c := parityCaseFor(t, "qwen/exec")
	g, _, req := prepareParityCase(t, c)
	plan := paritycase.BuildPlan(t, New(), req, c.mode)
	child := paritycase.EnvMap(plan.Env)

	for _, bystander := range qwenBystanders {
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
			t.Errorf("the qwen child lost the bystander %q, which no qwen filter touches", bystander)
			continue
		}
		if got != want {
			t.Errorf("the qwen child rewrote the bystander %q from %q to %q", bystander, want, got)
		}
	}
}
