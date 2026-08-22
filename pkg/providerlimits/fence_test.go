package providerlimits

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func leaseBytes(t *testing.T, lease *Lease) string {
	t.Helper()
	if lease == nil {
		return "<nil>"
	}
	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatalf("marshalling lease: %v", err)
	}
	return string(data)
}

// Every lease-mutating call is fenced by (run_id, claimed_at).
//
// Without the fence, a claimant whose lease was broken at minute 31 could, at
// minute 33, re-arm a group a DIFFERENT spawn is at that moment probing: the second
// spawn's probe would be silently converted into a suppression it never earned.
//
// This test breaks a lease, re-issues it to a different caller, then drives the
// ORIGINAL owner through AbandonProbe and TransferProbe and asserts both write
// nothing. It fails on any implementation keyed on run_id alone, because the two
// leases here differ only in claimed_at when the same run re-claims.
func TestFenceRejectsAStaleOwnerAfterClaimDeadlineExpiry(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	child := startLiveChild(t)
	original, granted, err := f.store.ClaimProbe(f.identity, codexPlan, Claimant{
		RunID:        "RUN-260802-000050",
		OwnerKind:    OwnerProcess,
		PID:          child.pid,
		PIDStartTime: child.startTime,
	})
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}

	// The claimant wedges past its outer bound; the lease is broken and fenced.
	f.clock.Set(original.ClaimDeadline.Add(time.Second))
	// The SAME run re-claims. Only claimed_at distinguishes the two leases, which
	// is exactly why run_id alone is an insufficient fence.
	reissued, granted := f.claim(t, codexPlan, "RUN-260802-000050")
	if !granted || !reissued.Held() {
		t.Fatal("the broken lease must be re-offered, claim-gated")
	}
	if reissued.ClaimedAt.Equal(original.ClaimedAt) {
		t.Fatal("the re-issued lease must carry a fresh claimed_at")
	}
	before := leaseBytes(t, f.record(t, codexPlan).ProbeLease)

	if err := f.store.AbandonProbe(f.identity, codexPlan, original); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("AbandonProbe from the fenced-out owner = %v, want ErrLeaseLost", err)
	}
	if got := leaseBytes(t, f.record(t, codexPlan).ProbeLease); got != before {
		t.Fatalf("AbandonProbe from a fenced-out owner rewrote the lease:\n before %s\n after  %s", before, got)
	}
	if err := f.store.TransferProbe(f.identity, codexPlan, original, "RUN-260802-000050"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("TransferProbe from the fenced-out owner = %v, want ErrLeaseLost", err)
	}
	if got := leaseBytes(t, f.record(t, codexPlan).ProbeLease); got != before {
		t.Fatalf("TransferProbe from a fenced-out owner rewrote the lease:\n before %s\n after  %s", before, got)
	}
	if !f.warnings.Contains("re-issued or broken") {
		t.Errorf("a fenced-out call must warn; warnings = %v", f.warnings.All())
	}
}

func TestFenceRejectsAStaleOwnerAfterClear(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	original, granted := f.claim(t, codexPlan, "RUN-260802-000051")
	if !granted {
		t.Fatal("expected the probe claim")
	}

	// An operator clears the group mid-probe: legitimate, and how a wedged
	// claimant is recovered from.
	if err := f.store.ClearGroup(f.identity, codexPlan, "operator"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	if !f.warnings.Contains("displaced the probe lease held by run RUN-260802-000051") {
		t.Errorf("clearing a mid-probe group must name the displaced run; warnings = %v", f.warnings.All())
	}
	reissued, granted := f.claim(t, codexPlan, "RUN-260802-000052")
	if !granted || !reissued.Held() {
		t.Fatal("a cleared group must be claimable by exactly one next spawn")
	}
	before := leaseBytes(t, f.record(t, codexPlan).ProbeLease)

	if err := f.store.AbandonProbe(f.identity, codexPlan, original); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("AbandonProbe = %v, want ErrLeaseLost", err)
	}
	if err := f.store.TransferProbe(f.identity, codexPlan, original, "RUN-260802-000051"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("TransferProbe = %v, want ErrLeaseLost", err)
	}
	if got := leaseBytes(t, f.record(t, codexPlan).ProbeLease); got != before {
		t.Fatalf("the new owner's lease changed:\n before %s\n after  %s", before, got)
	}
}

// The one deliberate exception: a quota observation from a fenced-out owner IS
// recorded, because it is real provider evidence and is monotone — it can only
// suppress further, never grant availability. The current lease stays untouched.
func TestQuotaObservationFromAFencedOutOwnerIsStillRecorded(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	original, _ := f.claim(t, codexPlan, "RUN-260802-000053")

	if err := f.store.ClearGroup(f.identity, codexPlan, "operator"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	reissued, granted := f.claim(t, codexPlan, "RUN-260802-000054")
	if !granted {
		t.Fatal("expected the re-issued claim")
	}
	before := f.record(t, codexPlan)
	beforeLease := leaseBytes(t, before.ProbeLease)

	// The old claimant DID reach the provider and DID get a rate-limit error.
	err := f.observeQuota(t, codexPlan, &original, "RUN-260802-000053")
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("Observe from a fenced-out owner = %v, want ErrLeaseLost so the caller knows it no longer owns the lease", err)
	}
	after := f.record(t, codexPlan)
	if after.Evidence == nil || after.Evidence.RunID != "RUN-260802-000053" {
		t.Fatalf("the observation was discarded; evidence = %+v", after.Evidence)
	}
	if after.BackoffStep != before.BackoffStep+1 {
		t.Fatalf("backoff step = %d, want %d: real provider evidence must still escalate", after.BackoffStep, before.BackoffStep+1)
	}
	if got := leaseBytes(t, after.ProbeLease); got != beforeLease {
		t.Fatalf("the current lease was touched:\n before %s\n after  %s", beforeLease, got)
	}
	if after.ProbeLease == nil || after.ProbeLease.RunID != reissued.RunID {
		t.Fatalf("the current owner lost its lease: %+v", after.ProbeLease)
	}
}

func TestLeaseMutatorsRequireALease(t *testing.T) {
	f := newStoreFixture(t)
	if err := f.store.AbandonProbe(f.identity, codexPlan, Lease{}); err == nil {
		t.Fatal("AbandonProbe must refuse a zero lease")
	}
	if err := f.store.TransferProbe(f.identity, codexPlan, Lease{}, "RUN-260802-000055"); err == nil {
		t.Fatal("TransferProbe must refuse a zero lease")
	}
	// A nil stored lease is a lost fence, not a write.
	f.suppress(t, codexPlan)
	fake := Lease{Group: codexPlan, RunID: "RUN-260802-000056", ClaimedAt: f.clock.Now()}
	if err := f.store.AbandonProbe(f.identity, codexPlan, fake); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("AbandonProbe against a record with no lease = %v, want ErrLeaseLost", err)
	}
	if f.record(t, codexPlan).ProbeAbandoned != 0 {
		t.Fatal("a lost fence must write nothing")
	}
}

// --- ClearGroup (the operator surface's transition) -------------------------

// clear rewrites the record to probe_eligible(step 0). It must NOT delete it: a
// missing record reads as available in this state machine, so deleting it would
// remove the claim gate and readmit every concurrent spawn at once — while telling
// the operator it had done something safe.
//
// This test fails on delete-the-record behaviour at the first assertion.
func TestClearGroupIsClaimGatedAndDoesNotDeleteTheRecord(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	// Escalate a couple of steps so a step reset is observable.
	for range 2 {
		f.clock.Set(f.nextProbeAt(t, codexPlan))
		lease, _ := f.claim(t, codexPlan, "RUN-260802-000060")
		if err := f.observeQuota(t, codexPlan, &lease, "RUN-260802-000060"); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	if got := f.record(t, codexPlan).BackoffStep; got == 0 {
		t.Fatal("the fixture must reach a non-zero backoff step for the reset to mean anything")
	}

	if err := f.store.ClearGroup(f.identity, codexPlan, "operator"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}

	record, exists := f.store.GroupRecordFor(f.identity, codexPlan)
	if !exists {
		t.Fatal("the record was DELETED; a missing record reads as available, which removes the claim gate")
	}
	if record.State != StateProbeEligible {
		t.Fatalf("state = %q, want probe_eligible", record.State)
	}
	if record.State == StateAvailable {
		t.Fatal("clear must never assert availability")
	}
	if got := record.EffectiveState(f.clock.Now()); got == StateAvailable {
		t.Fatal("the cleared record must not read as available")
	}
	if record.BackoffStep != 0 {
		t.Fatalf("backoff step = %d, want 0", record.BackoffStep)
	}
	if record.NextProbeAt == nil || !record.NextProbeAt.Equal(f.clock.Now()) {
		t.Fatalf("next_probe_at = %v, want now: the operator asserted a reset, so there is no wait", record.NextProbeAt)
	}
	if record.Evidence != nil || record.ProviderResetHint != nil {
		t.Fatal("clear forgets the evidence and the hint")
	}
	if record.ClearedAt == nil || record.ClearedBy != "operator" {
		t.Fatalf("clear must be recorded: cleared_at=%v cleared_by=%q", record.ClearedAt, record.ClearedBy)
	}

	// Twenty concurrent spawns race the cleared group. Exactly one probes.
	leased, unleased, leases := f.claimConcurrently(t, codexPlan, 20)
	if unleased != 0 {
		t.Fatalf("%d callers were admitted without a lease; clearing produced a thundering herd", unleased)
	}
	if leased != 1 {
		t.Fatalf("%d callers were granted, want exactly 1", leased)
	}
	winner := leases[0]

	// The winner's exit 0 clears the group to available.
	if err := f.store.ObserveSuccess(f.identity, codexPlan, Evidence{RunID: winner.RunID}); err != nil {
		t.Fatalf("ObserveSuccess: %v", err)
	}
	if got := f.record(t, codexPlan).State; got != StateAvailable {
		t.Fatalf("state after the winner's exit 0 = %q, want available", got)
	}
}

func TestClearedGroupSuppressesAtStepOneWhenTheProbeFails(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	if err := f.store.ClearGroup(f.identity, codexPlan, "operator"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	lease, granted := f.claim(t, codexPlan, "RUN-260802-000061")
	if !granted || !lease.Held() {
		t.Fatal("expected exactly one claim on the cleared group")
	}
	if err := f.observeQuota(t, codexPlan, &lease, "RUN-260802-000061"); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	record := f.record(t, codexPlan)
	if record.BackoffStep != 1 {
		t.Fatalf("backoff step = %d, want 1: the operator was wrong and one 7-second launch re-suppressed the group by the ordinary k+1 rule", record.BackoffStep)
	}
	if record.State != StateSuppressed {
		t.Fatalf("state = %q, want suppressed", record.State)
	}
}

func TestClearGroupOnAnUnknownGroupCreatesAClaimGatedRecord(t *testing.T) {
	f := newStoreFixture(t)
	if err := f.store.ClearGroup(f.identity, "codex-spark", "operator"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	record, ok := f.store.GroupRecordFor(f.identity, "codex-spark")
	if !ok {
		t.Fatal("clear must leave a record behind")
	}
	if record.State != StateProbeEligible {
		t.Fatalf("state = %q, want probe_eligible", record.State)
	}
}

// --- Observe's own gate -----------------------------------------------------

// The run_budget class cannot reach Observe through the public API: it produces no
// Observation, and a hand-built one from another package carries no gate.
func TestObserveRefusesANonQuotaObservation(t *testing.T) {
	f := newStoreFixture(t)
	budget := ClassifyManaged(ProviderCodex, ManagedSignalBudgetLimited)
	obs, ok := budget.QuotaObservation(Evidence{RunID: "RUN-260802-000070"})
	if ok {
		t.Fatal("run_budget must not yield an Observation")
	}
	if err := f.store.Observe(f.identity, codexPlan, nil, obs); !errors.Is(err, ErrNotProviderQuota) {
		t.Fatalf("Observe = %v, want ErrNotProviderQuota", err)
	}
	if _, exists := f.store.GroupRecordFor(f.identity, codexPlan); exists {
		t.Fatal("a managed budget limit changed group state; a cap this runtime imposed proves nothing about the subscription")
	}
	// And nothing at all was written to disk.
	if _, err := os.Stat(f.store.Layout().StateFile(f.identity.Key)); !os.IsNotExist(err) {
		t.Fatalf("a state file exists after a run-budget observation: %v", err)
	}
	if !f.warnings.Contains("only shared-subscription evidence") {
		t.Errorf("warnings = %v", f.warnings.All())
	}
}

// The whole state space must be unchanged by a run-budget classification, not
// merely the one group it would have touched.
func TestManagedBudgetLimitSuppressesNothingAnywhere(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	before, err := os.ReadFile(f.store.Layout().StateFile(f.identity.Key))
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	budget := ClassifyManaged(ProviderCodex, ManagedSignalBudgetLimited)
	obs, _ := budget.QuotaObservation(Evidence{RunID: "RUN-260802-000071"})
	for _, group := range []string{codexPlan, "codex-spark", "claude-plan"} {
		if err := f.store.Observe(f.identity, group, nil, obs); !errors.Is(err, ErrNotProviderQuota) {
			t.Fatalf("Observe(%s) = %v, want ErrNotProviderQuota", group, err)
		}
	}
	after, err := os.ReadFile(f.store.Layout().StateFile(f.identity.Key))
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("the state file changed:\n before %s\n after  %s", before, after)
	}
}
