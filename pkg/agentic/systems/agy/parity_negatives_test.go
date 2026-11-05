package agy

import (
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/internal/runtimeenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// AGY'S ENVIRONMENT NEGATIVES POINT THE OTHER WAY FROM QWEN'S, as gemini's and
// muse's do — the three empty-filter systems have the same bound to hold.
//
// qwen strips twelve keys, so the defect worth attacking is a filter that
// strips too much or matches by prefix. Agy strips NOTHING, so the defect worth
// attacking is a filter that EXISTS — a port author who read qwen's env.go,
// concluded "every system must clear the parent runtime state" and copied the
// filter one directory over.
//
// The prefix-strip attack the parity package carries is deliberately not
// reproduced here: this plugin has no exact-key strip for a prefix to be a near
// miss OF, so the attack would collapse into the borrowed-filter one below
// while pretending to measure something else.

// wipeSurvivorKeys returns the keys of every parent entry the golden does NOT
// record as removed — that is, everything this system's child inherits.
//
// It is computed from the fixture rather than listed, because for agy the list
// is almost the whole pinned environment and a hand-written copy would rot the
// first time the capture script seeds one more key.
func wipeSurvivorKeys(g parity.Golden) []string {
	removed := map[string]bool{}
	for _, entry := range g.Surface.EnvRemoved {
		removed[entry] = true
	}
	var keys []string
	for _, entry := range g.Capture.ParentEnv {
		if removed[entry] {
			continue
		}
		key, _, _ := strings.Cut(entry, "=")
		keys = append(keys, key)
	}
	return keys
}

// TestAWholeEnvironmentWipeFailsAgainstTheAgyGolden is the preservation bound,
// against the shipped plugin.
func TestAWholeEnvironmentWipeFailsAgainstTheAgyGolden(t *testing.T) {
	const subject = "agy/exec"
	c := parityCaseFor(t, subject)
	g, dirs, system, req := prepareParityCase(t, c)

	clean := paritycase.BuildPlan(t, system, req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
		t.Fatalf("the correct plan already differs, so the wipe below proves nothing: %v", diffs)
	}

	survivors := wipeSurvivorKeys(g)
	if len(survivors) == 0 {
		t.Fatalf("%s records %d parent entries and removes all %d of them, so no key proves this plugin preserves anything",
			subject, len(g.Capture.ParentEnv), len(g.Surface.EnvRemoved))
	}

	plan := paritycase.BuildPlan(t, paritycase.WholeEnvWipeSystem{System: system}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
	if len(diffs) == 0 {
		t.Fatalf("an agy plugin that discards the WHOLE parent environment byte-matched %s", subject)
	}
	if !paritycase.NamesField(diffs, "EnvRemoved") {
		t.Errorf("the wipe was planted in the environment but the harness reported %v", diffs)
	}

	// The narrowing that shows WHICH part of the fixture is doing the catching:
	// take every inherited key back out of the recorded parent environment and
	// the identical wipe walks straight through.
	t.Run("and would not be caught without the inherited keys", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = paritycase.WithoutKeys(g.Capture.ParentEnv, survivors...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatal("no inherited entry was removed, so this narrowing changed nothing")
		}
		narrowedReq := req
		narrowedReq.Env = paritycase.WithoutKeys(req.Env, survivors...)
		wiped := paritycase.BuildPlan(t, paritycase.WholeEnvWipeSystem{System: system}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, wiped, dirs.Substitutions()); len(diffs) != 0 {
			t.Fatalf("removing every inherited key did NOT restore the bypass (%v); something other than the survivors is catching the wipe", diffs)
		}
	})
}

// borrowedFilterSystem is the agy plugin with QWEN'S filter bolted on.
//
// It is written as a real call into the shared codex-family strip rather than
// as a hand-listed set of keys, so it is exactly what a port author would
// produce by copying the import and the one line that uses it.
type borrowedFilterSystem struct{ agentic.System }

func (b borrowedFilterSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return b.System.ChildEnv(runtimeenv.Filter(parent), req)
}

// TestABorrowedRuntimeFilterFailsAgainstTheAgyGolden is the bound that makes
// agy's empty filter LOAD-BEARING rather than an absence nobody measured.
//
// Without it, env.go's whole comment — "agy strips nothing, here is what it
// therefore inherits" — would be prose a reader trusts with nothing holding it
// to the code, and a port that added a filter for good reasons would pass every
// other test in this package.
func TestABorrowedRuntimeFilterFailsAgainstTheAgyGolden(t *testing.T) {
	const subject = "agy/exec"
	c := parityCaseFor(t, subject)
	g, dirs, system, req := prepareParityCase(t, c)

	clean := paritycase.BuildPlan(t, system, req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
		t.Fatalf("the correct plan already differs, so the borrowed filter below proves nothing: %v", diffs)
	}

	plan := paritycase.BuildPlan(t, borrowedFilterSystem{System: system}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
	if len(diffs) == 0 {
		t.Fatalf("an agy plugin carrying the codex-family filter byte-matched %s; the goldens would then say nothing about which systems strip parent runtime state", subject)
	}
	if !paritycase.NamesField(diffs, "EnvRemoved") {
		t.Errorf("the borrowed filter was planted in the environment but the harness reported %v", diffs)
	}

	t.Run("and would not be caught without the codex-family keys in parent_env", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = paritycase.WithoutKeys(g.Capture.ParentEnv, runtimeenv.Keys()...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatal("no codex-family entry was removed, so this narrowing changed nothing")
		}
		narrowedReq := req
		narrowedReq.Env = paritycase.WithoutKeys(req.Env, runtimeenv.Keys()...)
		borrowed := paritycase.BuildPlan(t, borrowedFilterSystem{System: system}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, borrowed, dirs.Substitutions()); len(diffs) != 0 {
			t.Fatalf("removing the codex-family keys did NOT restore the bypass (%v); the fixture is catching the borrowed filter with something else", diffs)
		}
	})
}

// TestTheEmptyFilterIsDeliberate states the same fact positively and names the
// keys, so a reader does not have to derive agy's inheritance from two attack
// tests.
//
// Every key here is one the source's BUG-260819-3qn52o records as leaking. If
// one of these starts failing, a filter has been added — which may well be
// right, but it is a behaviour change no golden covers, and env.go's comment
// has to change with it or the code and the prose disagree.
func TestTheEmptyFilterIsDeliberate(t *testing.T) {
	c := parityCaseFor(t, "agy/exec")
	g, _, system, req := prepareParityCase(t, c)
	plan := paritycase.BuildPlan(t, system, req, c.mode)
	child := paritycase.EnvMap(plan.Env)

	inherited := []string{
		"CLAUDECODE",
		runtimeenv.ThreadIDEnv,
		runtimeenv.SessionEnv,
		runtimeenv.ManagedPackageRootEnv,
		runtimeenv.SessionIDEnv,
		"PARITY_CODEX_APP_SERVER_TOKEN",
		"PARITY_SESSION_MANAGER_TOKEN",
		"PARITY_BYSTANDER",
	}
	parent := paritycase.EnvMap(g.Capture.ParentEnv)
	for _, key := range inherited {
		want, seeded := parent[key]
		if !seeded {
			t.Fatalf("%q is not seeded in the golden's parent_env, so this test measures nothing for it", key)
		}
		got, present := child[key]
		if !present {
			t.Errorf("the agy child no longer inherits %q; env.go declares this filter EMPTY and it is not", key)
			continue
		}
		if got != want {
			t.Errorf("the agy child rewrote %q from %q to %q", key, want, got)
		}
	}
}
