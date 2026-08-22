package codex

import (
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// The parity package carries two permanent negatives that exist to bite PORTS:
// a ChildEnv that discards the whole parent environment, and one that strips
// the right families by prefix where the source strips by exact key. There,
// they are driven through a hand-written probe. Here they are driven through
// THIS PLUGIN, because a probe that fails proves the fixture can catch the
// defect and says nothing about whether the shipped codex plugin has it.
//
// Both attacks wrap the real System and override exactly one method, so
// everything else about the plan — argv, binary, stdin — stays correct and the
// comparison isolates the environment.
//
// Both carry the parity package's narrowing subtest as well: take the
// bystander keys back out of the recorded parent environment and the identical
// defect must walk straight through. Without that, "the golden caught it"
// would be equally consistent with the golden catching it for some unrelated
// reason, and the four seeded bystanders would be decoration.

// codexBystanders are the keys the capture seeds that NO codex filter touches.
// They are this plugin's only positive preservation evidence.
var codexBystanders = []string{
	"PARITY_BYSTANDER",
	"CODEX_LIKE_BUT_NOT",
	"CLAUDECODE_LIKE_BUT_NOT",
	"TASK_BOARD_LIKE_BUT_NOT",
}

// incidentalSurvivor is the key a codex child keeps for no reason anybody
// maintains: the codex filter simply has no CLAUDECODE entry, because stripping
// it is the QWEN filter's addition on top of this one.
//
// It is named separately from the bystanders because the difference matters to
// what the evidence is worth. A bystander is seeded on purpose and a test fails
// if a recapture drops it; this key survives by omission and would vanish the
// day someone adds it to runtimeEnvKeys for a good reason. A preservation bound
// resting on it would break silently, so the narrowings below prove the four
// seeded bystanders carry the bound without it.
const incidentalSurvivor = "CLAUDECODE"

// codexSurvivors is every key this filter preserves out of the pinned parent
// environment: the four seeded bystanders plus the incidental one.
var codexSurvivors = append(append([]string(nil), codexBystanders...), incidentalSurvivor)

// wholeEnvWipeSystem is the port defect the codex goldens' SHAPE invites: a
// ChildEnv that returns only its own injections.
//
// It is written as the real ChildEnv over an EMPTY parent rather than as a
// hand-listed set of injections, which is what makes it a plausible mistake
// rather than a contrived one: it is what a port author writes after reading
// env_removed and concluding "codex keeps nothing of its parent".
type wholeEnvWipeSystem struct{ *System }

func (w wholeEnvWipeSystem) ChildEnv(_ []string, req agentic.LaunchRequest) ([]string, error) {
	return w.System.ChildEnv(nil, req)
}

// prefixStripSystem is the second defect: filtering a whole family by PREFIX
// before handing the rest to the real filter.
//
// "Strip the CODEX_ family" is a reading of env.go a port author can arrive at
// honestly, and env.go's blocked set is a KEY map, so the two differ only on
// keys that LOOK like the family without being in it. That is what the capture
// seeds CODEX_LIKE_BUT_NOT and TASK_BOARD_LIKE_BUT_NOT for, and a plain
// bystander cannot see the difference at all.
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

// TestAWholeEnvironmentWipeFailsAgainstTheCodexGolden is the preservation
// bound, against the shipped plugin.
//
// If this ever passes, the bystander keys have been dropped from a recapture
// and this plugin could return only its injections while every codex golden
// stayed green.
func TestAWholeEnvironmentWipeFailsAgainstTheCodexGolden(t *testing.T) {
	const subject = "codex/exec-default-path"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)

	clean := buildParityPlan(t, New(), req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.substitutions()); len(diffs) != 0 {
		t.Fatalf("the correctly-filtered plan already differs, so the wipe below proves nothing: %v", diffs)
	}

	// The golden has to leave something to preserve. Without this the
	// assertion below could only ever pass by accident, and its failure would
	// send the reader hunting a comparator bug rather than a fixture that lost
	// its bystanders.
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
		t.Fatalf("a codex plugin that discards the WHOLE parent environment byte-matched %s; the golden would prove only the filter's lower bound", subject)
	}
	if !namesField(diffs, "EnvRemoved") {
		t.Errorf("the wipe was planted in the environment but the harness reported %v", diffs)
	}

	// The narrowing that shows WHICH part of the fixture is doing the catching.
	// Take every key this filter preserves back out of the recorded parent
	// environment — the shape the fixture had before the bystanders were
	// seeded — and the identical wipe walks straight through.
	t.Run("and would not be caught without the surviving keys", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = withoutKeys(g.Capture.ParentEnv, codexSurvivors...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("no surviving entry was removed, so this narrowing changed nothing; codexSurvivors and the fixture's parent_env disagree")
		}
		narrowedReq := req
		narrowedReq.Env = withoutKeys(req.Env, codexSurvivors...)
		wiped := buildParityPlan(t, wholeEnvWipeSystem{System: New()}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, wiped, dirs.substitutions()); len(diffs) != 0 {
			t.Fatalf("removing every surviving key did NOT restore the bypass (%v); something other than the survivors is catching the wipe, and the capture script's convention is not what this test protects", diffs)
		}
	})

	// And the half of that which the capture script is actually responsible
	// for. CLAUDECODE survives a codex child INCIDENTALLY — the codex filter
	// simply has no reason to strip it, and only the qwen filter adds it — so
	// it is not evidence anyone maintains. Removing it must leave the wipe
	// caught, or this plugin's preservation bound would rest on a key that a
	// future filter change could take away without anyone noticing.
	t.Run("and the seeded bystanders alone are enough", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = withoutKeys(g.Capture.ParentEnv, incidentalSurvivor)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("%q is not in the fixture's parent_env, so this narrowing changed nothing", incidentalSurvivor)
		}
		narrowedReq := req
		narrowedReq.Env = withoutKeys(req.Env, incidentalSurvivor)
		wiped := buildParityPlan(t, wholeEnvWipeSystem{System: New()}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, wiped, dirs.substitutions()); len(diffs) == 0 {
			t.Fatalf("with %q removed the wipe byte-matched the fixture, so the four seeded bystanders are NOT carrying this bound on their own", incidentalSurvivor)
		}
	})
}

// TestAPrefixStripFailsAgainstTheCodexGolden is the exact-key bound, against
// the shipped plugin.
//
// The subject prefix is CODEX_ alone. That is the narrow form of the defect on
// purpose: it over-strips CODEX_LIKE_BUT_NOT and disturbs nothing else, so the
// near-miss bystander is the ONLY thing that can be catching it — which is what
// the narrowing subtest then demonstrates by taking that key away.
//
// CLAUDECODE is deliberately not in the set: the codex filter does not strip it
// at all — that is the qwen filter's addition — so a codex port stripping
// CLAUDECODE* would be a different defect caught by the ordinary comparison.
func TestAPrefixStripFailsAgainstTheCodexGolden(t *testing.T) {
	const subject = "codex/exec-default-path"
	const nearMiss = "CODEX_LIKE_BUT_NOT"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)
	prefixes := []string{"CODEX_"}

	clean := buildParityPlan(t, New(), req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.substitutions()); len(diffs) != 0 {
		t.Fatalf("the correctly-filtered plan already differs, so the prefix strip below proves nothing: %v", diffs)
	}

	plan := buildParityPlan(t, prefixStripSystem{System: New(), prefixes: prefixes}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a codex plugin that strips %v by PREFIX byte-matched %s, but this filter strips by exact key; the near-miss bystanders are not seeded", prefixes, subject)
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

// TestAPrefixStripAlsoLeaksThePointedAtCredentials records a SECOND, separate
// consequence of the same defect, found while porting rather than predicted.
//
// filterRuntimeEnv does not carry a fixed list of credential variables: it
// reads TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV and
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
	c := parityCaseFor(t, "codex/exec-default-path")
	_, _, req := prepareParityCase(t, c)
	leaked := []string{"PARITY_CODEX_APP_SERVER_TOKEN", "PARITY_SESSION_MANAGER_TOKEN"}

	correct := buildParityPlan(t, New(), req, c.mode)
	for _, key := range leaked {
		if _, present := lookupEnv(correct.Env, key); present {
			t.Fatalf("the correct plugin already leaks %q, so this test measures nothing", key)
		}
	}

	defective := buildParityPlan(t, prefixStripSystem{System: New(), prefixes: []string{"CODEX_", "TASK_BOARD_"}}, req, c.mode)
	for _, key := range leaked {
		if _, present := lookupEnv(defective.Env, key); !present {
			t.Errorf("the prefix strip did not leak %q; the pointer-resolution consequence this test records is not reproducing, so either the defect or the filter has changed shape", key)
		}
	}
}

// TestThePluginPreservesEveryBystander is the same preservation bound stated
// positively and directly, so a reader does not have to derive it from two
// attack tests.
func TestThePluginPreservesEveryBystander(t *testing.T) {
	c := parityCaseFor(t, "codex/exec-default-path")
	g, _, req := prepareParityCase(t, c)
	plan := buildParityPlan(t, New(), req, c.mode)

	child := map[string]string{}
	for _, entry := range plan.Env {
		key, value, _ := strings.Cut(entry, "=")
		child[key] = value
	}
	for _, bystander := range codexBystanders {
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
			t.Errorf("the codex child lost the bystander %q, which no codex filter touches", bystander)
			continue
		}
		if got != want {
			t.Errorf("the codex child rewrote the bystander %q from %q to %q", bystander, want, got)
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
