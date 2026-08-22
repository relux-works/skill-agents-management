package parity

import (
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestDiffEnvExcludesPathAndOnlyPath proves the exclusion's BOUND, not merely
// its existence.
//
// Deleting the exclusion would show that something drops PATH. This narrows the
// question one key at a time: every neighbouring spelling — a key with PATH as
// a prefix, a key with PATH as a suffix, the lowercase spelling, a key whose
// VALUE contains a path — must still be reported. An exclusion that widened by
// one key is one environment key a port could change with nothing noticing,
// which is the same class of failure the guard's scan-scope test exists for.
func TestDiffEnvExcludesPathAndOnlyPath(t *testing.T) {
	parent := []string{
		"PATH=/parent/bin",
		"PATHEXT=.COM;.EXE",
		"MANPATH=/parent/man",
		"path=/parent/lower",
		"GOPATH=/parent/go",
		"HOME=/parent/home",
		"TASK_BOARD_RUN_ID=RUN-parent",
	}
	child := []string{
		"PATH=/child/bin",
		"PATHEXT=.COM",
		"MANPATH=/child/man",
		"path=/child/lower",
		"GOPATH=/child/go",
		"HOME=/parent/home",
		"TASK_BOARD_RUN_ID=RUN-child",
	}
	added, removed := DiffEnv(parent, child)

	for _, entry := range append(append([]string(nil), added...), removed...) {
		if entry == "PATH=/parent/bin" || entry == "PATH=/child/bin" {
			t.Errorf("DiffEnv reported %q; PATH is seeded by the capture harness and differs run to run for reasons unrelated to any port", entry)
		}
	}
	// Every neighbour must be reported on both sides. This is the narrowing:
	// each one is a key an over-eager exclusion would swallow.
	mustReport := map[string][]string{
		"added": {"PATHEXT=.COM", "MANPATH=/child/man", "path=/child/lower", "GOPATH=/child/go", "TASK_BOARD_RUN_ID=RUN-child"},
		"removed": {"PATHEXT=.COM;.EXE", "MANPATH=/parent/man", "path=/parent/lower", "GOPATH=/parent/go",
			"TASK_BOARD_RUN_ID=RUN-parent"},
	}
	for _, want := range mustReport["added"] {
		if !contains(added, want) {
			t.Errorf("DiffEnv did not report %q as added; the PATH exclusion has widened past the one key it is allowed to cover. added=%v", want, added)
		}
	}
	for _, want := range mustReport["removed"] {
		if !contains(removed, want) {
			t.Errorf("DiffEnv did not report %q as removed; the PATH exclusion has widened past the one key it is allowed to cover. removed=%v", want, removed)
		}
	}
	// HOME is unchanged between the two, so it belongs in neither list. An
	// entry reported as both added and removed when nothing about it moved
	// would drown every real difference in noise.
	if contains(added, "HOME=/parent/home") || contains(removed, "HOME=/parent/home") {
		t.Errorf("DiffEnv reported an unchanged entry. added=%v removed=%v", added, removed)
	}
	if len(envDiffExcludedKeys) != 1 || envDiffExcludedKeys[0] != "PATH" {
		t.Errorf("the env-diff exclusion list is %v; it is PATH and only PATH, ported from the source harness's diffEnv, and every widening is one more key a port can change unobserved", envDiffExcludedKeys)
	}
}

// TestDiffEnvReportsAValueReplacementOnBothSides pins the shape that makes a
// replacement legible. TASK_BOARD_RUN_ID is not added and is not removed — its
// VALUE changes — and the only way whole-entry set semantics can say that is by
// reporting the old entry removed and the new one added.
func TestDiffEnvReportsAValueReplacementOnBothSides(t *testing.T) {
	added, removed := DiffEnv([]string{"K=old"}, []string{"K=new"})
	if !contains(added, "K=new") || !contains(removed, "K=old") {
		t.Errorf("a value replacement did not appear on both sides: added=%v removed=%v", added, removed)
	}
}

func TestFromPlanMapsTheWholeSurface(t *testing.T) {
	parent := []string{"KEEP=1", "DROP=1", "PATH=/parent/bin"}
	plan := agentic.Plan{
		Mode:   agentic.LaunchModeExec,
		Binary: "/opt/probe/bin/probe",
		Argv:   []string{"exec", "--model", "probe-1"},
		Env:    []string{"KEEP=1", "ADD=2", "PATH=/child/bin"},
		Stdin:  agentic.StdinPayload{Attached: true, Bytes: []byte("prompt")},
	}
	got := FromPlan(plan, parent)
	want := Snapshot{
		Binary:     "/opt/probe/bin/probe",
		Args:       []string{"exec", "--model", "probe-1"},
		EnvAdded:   []string{"ADD=2"},
		EnvRemoved: []string{"DROP=1"},
		StdinKind:  StdinBytes,
		StdinData:  "prompt",
	}
	if diffs := Compare(want, got); len(diffs) != 0 {
		t.Errorf("FromPlan mapped the surface wrong: %v", diffs)
	}
}

// TestFromPlanKeepsAttachedEmptyStdinDistinctFromNone holds the distinction
// StdinPayload.Attached exists for. A harness reading its prompt from an
// attached empty stream sees EOF; one launched with no stdin at all sees a
// closed descriptor, and the two produce different child behaviour. Collapsing
// them into one empty string would make a port that got this wrong pass.
func TestFromPlanKeepsAttachedEmptyStdinDistinctFromNone(t *testing.T) {
	attachedEmpty := FromPlan(agentic.Plan{Mode: agentic.LaunchModeExec, Stdin: agentic.StdinPayload{Attached: true}}, nil)
	detached := FromPlan(agentic.Plan{Mode: agentic.LaunchModeExec}, nil)
	if attachedEmpty.StdinKind == detached.StdinKind {
		t.Fatalf("an attached empty stdin and no stdin both mapped to %q", attachedEmpty.StdinKind)
	}
	if attachedEmpty.StdinKind != StdinBytes || detached.StdinKind != StdinNone {
		t.Errorf("stdin kinds are %q (attached empty) and %q (none), want %q and %q",
			attachedEmpty.StdinKind, detached.StdinKind, StdinBytes, StdinNone)
	}
}

// TestFromPlanReportsNoEnvOrStdinForADryRun holds the boundary the source's own
// harness has: its dry-run capture path calls BuildArgs, which builds no
// command, so there is no environment and no stdin to observe. Reporting an
// empty env diff here would be reporting an absence that was never measured,
// and a dry-run golden would then appear to prove an environment contract it
// cannot.
func TestFromPlanReportsNoEnvOrStdinForADryRun(t *testing.T) {
	plan := agentic.Plan{
		Mode:   agentic.LaunchModeDryRun,
		Binary: "/opt/probe/bin/probe",
		Argv:   []string{"exec", "<assignment-prompt-file>"},
		Env:    []string{"THIS=would-be-a-lie"},
		Stdin:  agentic.StdinPayload{Attached: true, Bytes: []byte("so would this")},
	}
	got := FromPlan(plan, []string{"BASE=1"})
	if got.StdinKind != StdinDryRun {
		t.Errorf("dry-run stdin kind is %q, want %q", got.StdinKind, StdinDryRun)
	}
	if got.StdinData != "" || len(got.EnvAdded) != 0 || len(got.EnvRemoved) != 0 {
		t.Errorf("a dry-run snapshot reported an environment or stdin the source's dry-run path never observes: %+v", got)
	}
	if got.Binary == "" || len(got.Args) == 0 {
		t.Errorf("a dry-run snapshot dropped the two things it does prove, the binary and the argv: %+v", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, entry := range haystack {
		if entry == needle {
			return true
		}
	}
	return false
}
