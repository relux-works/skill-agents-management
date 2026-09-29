package muse

import (
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/internal/runtimeenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

func leakedParentEnvNames(child []string, names []string) []string {
	childEnv := paritycase.EnvMap(child)
	var leaked []string
	for _, name := range names {
		if _, ok := childEnv[name]; ok {
			leaked = append(leaked, name)
		}
	}
	return leaked
}

// These tests attack the Muse environment boundary through BuildPlan. The
// golden catches accidental loss of allowed process state, while the explicit
// credential-negative test catches an allowlist weakened by one admitted name.

// wipeSurvivorKeys returns the keys of parent entries that the golden says the
// system preserves. The allowlist golden uses this set to prove a whole-env
// wipe is not an acceptable substitute for filtering.
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

// TestAWholeEnvironmentWipeFailsAgainstTheMuseGolden proves the Muse child
// still receives the allowed parent environment rather than an empty one.
func TestAWholeEnvironmentWipeFailsAgainstTheMuseGolden(t *testing.T) {
	const subject = "muse/exec"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)
	// The source capture omits HOME, so seed one deterministic allowed parent
	// entry here. It makes the whole-environment wipe observable even though
	// every key in the frozen capture itself is outside Muse's allowlist.
	const allowedParentSentinel = "HOME=/parity/pinned/allowlisted-home"
	g.Capture.ParentEnv = append(g.Capture.ParentEnv, allowedParentSentinel)
	req.Env = append(req.Env, allowedParentSentinel)
	g = museGoldenWithDeclaredEnvDivergence(g)

	clean := paritycase.BuildPlan(t, New(), req, c.mode)
	if diffs := parity.ComparePlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
		t.Fatalf("the correct plan already differs, so the wipe below proves nothing: %v", diffs)
	}

	survivors := wipeSurvivorKeys(g)
	if len(survivors) != 1 || survivors[0] != "HOME" {
		t.Fatalf("%s records %d parent entries and removes all %d of them, so no key proves this plugin preserves anything",
			subject, len(g.Capture.ParentEnv), len(g.Surface.EnvRemoved))
	}

	plan := paritycase.BuildPlan(t, paritycase.WholeEnvWipeSystem{System: New()}, req, c.mode)
	diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a muse plugin that discards the whole parent environment byte-matched %s", subject)
	}
	if !paritycase.NamesField(diffs, "EnvRemoved") {
		t.Errorf("the defect was planted in the environment but the harness reported %v", diffs)
	}

	t.Run("the golden must contain inherited parent keys", func(t *testing.T) {
		narrowed := g
		narrowed.Capture.ParentEnv = paritycase.WithoutKeys(g.Capture.ParentEnv, survivors...)
		if len(narrowed.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatal("no inherited entry was removed, so this narrowing changed nothing")
		}
		narrowedReq := req
		narrowedReq.Env = paritycase.WithoutKeys(req.Env, survivors...)
		wiped := paritycase.BuildPlan(t, paritycase.WholeEnvWipeSystem{System: New()}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, wiped, dirs.Substitutions()); len(diffs) != 0 {
			t.Fatalf("removing all allowed inherited keys did not restore the bypass (%v)", diffs)
		}
	})
}

// borrowedFilterSystem is the plausible port defect that uses the Codex-family
// filter INSTEAD OF Muse's allowlist. It preserves Muse's run-context overlay
// and update pin so the golden negative isolates the filtering boundary.
type borrowedFilterSystem struct{ agentic.System }

func (b borrowedFilterSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env := agentic.WithRunContext(runtimeenv.Filter(parent), req)
	return agentic.SetEnvValue(env, museNoAutoUpdateEnv, "1"), nil
}

func unallowedParentKeysKeptByCodexFilter(parent []string) []string {
	filtered := paritycase.EnvMap(runtimeenv.Filter(parent))
	var keys []string
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if name == agentic.EnvRunID || name == agentic.EnvTaskID || allowMuseParentEnvName(name) {
			continue
		}
		if _, kept := filtered[name]; kept {
			keys = append(keys, name)
		}
	}
	return keys
}

// TestABorrowedRuntimeFilterFailsAgainstTheMuseGolden keeps the original
// golden-negative name while re-pointing its mutant at the chosen Muse
// divergence. A Codex-only strip must not stand in for Muse's complete
// allowlist, even when the run-context and update-pin writes are correct.
func TestABorrowedRuntimeFilterFailsAgainstTheMuseGolden(t *testing.T) {
	const subject = "muse/exec"
	c := parityCaseFor(t, subject)
	g, dirs, req := prepareParityCase(t, c)

	clean := paritycase.BuildPlan(t, New(), req, c.mode)
	if diffs := compareMuseGoldenPlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
		t.Fatalf("the correct plan already differs, so the borrowed-filter mutant proves nothing: %v", diffs)
	}

	plan := paritycase.BuildPlan(t, borrowedFilterSystem{System: New()}, req, c.mode)
	diffs := parity.ComparePlan(museGoldenWithDeclaredEnvDivergence(g), plan, dirs.Substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a Muse plugin using only the Codex-family filter byte-matched %s", subject)
	}
	if !paritycase.NamesField(diffs, "EnvRemoved") {
		t.Errorf("the defect was planted in parent filtering but the harness reported %v", diffs)
	}

	t.Run("the golden must contain a disallowed key the borrowed filter keeps", func(t *testing.T) {
		survivors := unallowedParentKeysKeptByCodexFilter(req.Env)
		if len(survivors) == 0 {
			t.Fatal("the capture has no unallowed parent key that survives the Codex-family filter")
		}
		narrowed := g
		narrowed.Capture.ParentEnv = paritycase.WithoutKeys(g.Capture.ParentEnv, survivors...)
		narrowed.Surface.EnvRemoved = paritycase.WithoutKeys(g.Surface.EnvRemoved, survivors...)
		narrowedReq := req
		narrowedReq.Env = paritycase.WithoutKeys(req.Env, survivors...)
		narrowed = museGoldenWithDeclaredEnvDivergence(narrowed)
		narrowed.Surface.EnvRemoved = paritycase.WithoutKeys(narrowed.Surface.EnvRemoved, survivors...)
		borrowed := paritycase.BuildPlan(t, borrowedFilterSystem{System: New()}, narrowedReq, c.mode)
		if diffs := parity.ComparePlan(narrowed, borrowed, dirs.Substitutions()); len(diffs) != 0 {
			t.Fatalf("removing the observed survivor keys did not restore parity (%v)", diffs)
		}
	})
}

// oneNameWideningSystem admits a single unrelated captured parent variable.
// The frozen literal divergence must still reject it.
type oneNameWideningSystem struct {
	agentic.System
	name string
}

func (m oneNameWideningSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env := filterMuseParentEnv(parent)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if name == m.name {
			env = append(env, entry)
			break
		}
	}
	env = agentic.WithRunContext(env, req)
	return agentic.SetEnvValue(env, museNoAutoUpdateEnv, "1"), nil
}

func TestAnAllowlistWideningMutantFailsAgainstTheMuseGolden(t *testing.T) {
	const name = "PARITY_BYSTANDER"
	c := parityCaseFor(t, "muse/exec")
	g, dirs, req := prepareParityCase(t, c)

	clean := paritycase.BuildPlan(t, New(), req, c.mode)
	if diffs := compareMuseGoldenPlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
		t.Fatalf("the correct plan already differs, so the widening mutant proves nothing: %v", diffs)
	}
	if got, ok := paritycase.Lookup(req.Env, name); !ok || got != "keep-me" {
		t.Fatalf("the golden does not seed the widening control: %s=%q present=%v", name, got, ok)
	}

	mutant := paritycase.BuildPlan(t, oneNameWideningSystem{System: New(), name: name}, req, c.mode)
	diffs := parity.ComparePlan(museGoldenWithDeclaredEnvDivergence(g), mutant, dirs.Substitutions())
	if len(diffs) == 0 {
		t.Fatalf("admitting exactly %s still matched the declared Muse golden divergence", name)
	}
	if !paritycase.NamesField(diffs, "EnvRemoved") {
		t.Fatalf("the one-name widening mutant changed an unexpected surface: %v", diffs)
	}
}

// oneCredentialAdmissionSystem is the allowlist with exactly one forbidden
// credential name admitted. The rest of the filtering and run-context overlay
// stay active so the test attacks the credential predicate itself.
type oneCredentialAdmissionSystem struct {
	agentic.System
	name string
}

func (m oneCredentialAdmissionSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	filtered := filterMuseParentEnv(parent)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if name == m.name {
			filtered = append(filtered, entry)
			break
		}
	}
	env := agentic.WithRunContext(filtered, req)
	return agentic.SetEnvValue(env, museNoAutoUpdateEnv, "1"), nil
}

// TestAOneCredentialAdmissionMutantIsDetected proves the negative test's
// assertion reports exactly one leak when the filter is weakened to admit one
// credential-shaped parent variable.
func TestAOneCredentialAdmissionMutantIsDetected(t *testing.T) {
	c := parityCaseFor(t, "muse/exec")
	_, _, req := prepareParityCase(t, c)
	const entry = "RANDOM_PARENT_TOKEN=synthetic-test-value"
	const name = "RANDOM_PARENT_TOKEN"
	req.Env = append(req.Env, entry)

	clean := paritycase.BuildPlan(t, New(), req, c.mode)
	if leaks := leakedParentEnvNames(clean.Env, []string{name}); len(leaks) != 0 {
		t.Fatalf("the shipped allowlist admitted the credential-shaped control %q", name)
	}

	mutant := paritycase.BuildPlan(t, oneCredentialAdmissionSystem{System: New(), name: name}, req, c.mode)
	if leaks := leakedParentEnvNames(mutant.Env, []string{name}); len(leaks) != 1 || leaks[0] != name {
		t.Fatalf("the narrowing mutant was not detected for exactly %s: %v", name, leaks)
	}
}
