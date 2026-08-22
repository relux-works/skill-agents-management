package providerlimits

import (
	"errors"
	"testing"
	"time"
)

const codexPlan = "codex-plan"

// --- the four-state machine and the ladder ---------------------------------

func TestMissingRecordReadsAvailable(t *testing.T) {
	f := newStoreFixture(t)
	if _, ok := f.store.GroupRecordFor(f.identity, codexPlan); ok {
		t.Fatal("a fresh store must hold no record")
	}
	lease, granted := f.claim(t, codexPlan, "RUN-260802-000001")
	if !granted {
		t.Fatal("a group with no record is available and must not be subtracted")
	}
	if lease.Held() {
		t.Fatal("an available group needs no lease")
	}
}

func TestQuotaObservationSuppressesAtStepZero(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	record := f.record(t, codexPlan)
	if record.State != StateSuppressed {
		t.Fatalf("state = %q, want suppressed", record.State)
	}
	if record.BackoffStep != 0 {
		t.Fatalf("backoff step = %d, want 0", record.BackoffStep)
	}
	want := f.clock.Now().Add(2 * time.Minute)
	if record.NextProbeAt == nil || !record.NextProbeAt.Equal(want) {
		t.Fatalf("next_probe_at = %v, want %s (ladder step 1)", record.NextProbeAt, want)
	}
	if record.Evidence == nil || record.Evidence.RunID != "RUN-260801-c95402" {
		t.Fatalf("evidence = %+v, want the observing run recorded", record.Evidence)
	}
}

func TestSuppressedGroupIsSubtractedUntilTheStepElapses(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)

	if _, granted := f.claim(t, codexPlan, "RUN-260802-000002"); granted {
		t.Fatal("a suppressed group must be refused before next_probe_at")
	}
	f.clock.Advance(2 * time.Minute)
	lease, granted := f.claim(t, codexPlan, "RUN-260802-000002")
	if !granted || !lease.Held() {
		t.Fatal("the step elapsed: exactly one caller must be admitted, with a lease")
	}
	if f.record(t, codexPlan).State != StateProbing {
		t.Fatalf("state = %q, want probing while a lease is held", f.record(t, codexPlan).State)
	}
}

// probe_eligible is not available: it is admitted to the claim holder only.
func TestProbeEligibleIsNotAvailable(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	record := f.record(t, codexPlan)
	if got := record.EffectiveState(f.clock.Now()); got != StateProbeEligible {
		t.Fatalf("effective state = %q, want probe_eligible", got)
	}
	if got := record.EffectiveState(f.clock.Now()); got == StateAvailable {
		t.Fatal("an elapsed backoff step must never read available")
	}

	if _, granted := f.claim(t, codexPlan, "RUN-A00000-000001"); !granted {
		t.Fatal("the first caller must win the claim")
	}
	if _, granted := f.claim(t, codexPlan, "RUN-B00000-000001"); granted {
		t.Fatal("a second caller must be refused while the lease is live")
	}
}

func TestLadderEscalationAndRepeatAtMax(t *testing.T) {
	f := newStoreFixture(t)
	ladder := f.store.Ladder()

	f.suppress(t, codexPlan)
	for step := 1; step < len(ladder)+3; step++ {
		record := f.record(t, codexPlan)
		// Move to the end of the current window, claim the probe, and fail it.
		f.clock.Set(f.nextProbeAt(t, codexPlan))
		lease, granted := f.claim(t, codexPlan, "RUN-260802-0000ff")
		if !granted || !lease.Held() {
			t.Fatalf("step %d: the probe must be claimable at next_probe_at", step)
		}
		if err := f.observeQuota(t, codexPlan, &lease, "RUN-260802-0000ff"); err != nil {
			t.Fatalf("step %d: Observe: %v", step, err)
		}
		record = f.record(t, codexPlan)
		wantStep := step
		if wantStep > len(ladder)-1 {
			wantStep = len(ladder) - 1
		}
		if record.BackoffStep != wantStep {
			t.Fatalf("after failure %d backoff step = %d, want %d", step, record.BackoffStep, wantStep)
		}
		wantNext := f.clock.Now().Add(ladder[wantStep])
		if !record.NextProbeAt.Equal(wantNext) {
			t.Fatalf("after failure %d next_probe_at = %s, want %s", step, record.NextProbeAt, wantNext)
		}
		if record.ProbeLease != nil {
			t.Fatalf("after failure %d the lease was not released", step)
		}
		if record.State != StateSuppressed {
			t.Fatalf("after failure %d state = %q, want suppressed", step, record.State)
		}
	}
	if got := f.record(t, codexPlan).BackoffStep; got != len(ladder)-1 {
		t.Fatalf("final step = %d, want the last step (%d) to repeat", got, len(ladder)-1)
	}
}

func TestNextProbeAtIsNeverEarlierThanSuppressedSince(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	for range 8 {
		record := f.record(t, codexPlan)
		if record.SuppressedSince == nil {
			t.Fatal("suppressed_since must be recorded")
		}
		if record.NextProbeAt == nil || record.NextProbeAt.Before(*record.SuppressedSince) {
			t.Fatalf("next_probe_at %s is before suppressed_since %s", record.NextProbeAt, record.SuppressedSince)
		}
		f.clock.Set(f.nextProbeAt(t, codexPlan))
		lease, _ := f.claim(t, codexPlan, "RUN-260802-0000ee")
		if err := f.observeQuota(t, codexPlan, &lease, "RUN-260802-0000ee"); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
}

func TestClearOnSuccess(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, granted := f.claim(t, codexPlan, "RUN-260802-000010")
	if !granted {
		t.Fatal("expected the probe claim")
	}
	_ = lease
	if err := f.store.ObserveSuccess(f.identity, codexPlan, Evidence{RunID: "RUN-260802-000010"}); err != nil {
		t.Fatalf("ObserveSuccess: %v", err)
	}
	record := f.record(t, codexPlan)
	if record.State != StateAvailable {
		t.Fatalf("state = %q, want available", record.State)
	}
	if record.BackoffStep != 0 || record.SuppressedSince != nil || record.NextProbeAt != nil {
		t.Fatalf("the ladder was not reset: %+v", record)
	}
	if record.ProbeLease != nil {
		t.Fatal("the lease must be released on success")
	}
	if record.LastSuccessAt == nil {
		t.Fatal("last_success_at must be written on every exit-0 run")
	}
	if record.Evidence != nil || record.ProviderResetHint != nil {
		t.Fatal("success clears the evidence and the hint")
	}
	// A later quota observation starts the ladder over rather than resuming it.
	f.suppress(t, codexPlan)
	if got := f.record(t, codexPlan).BackoffStep; got != 0 {
		t.Fatalf("backoff step after a success then a failure = %d, want 0", got)
	}
}

func TestSuccessOnAnyModelInTheGroupClears(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	// No lease is held: a plain exit-0 run in the group is still the evidence the
	// design accepts, and it is the only thing that clears a group.
	if err := f.store.ObserveSuccess(f.identity, codexPlan, Evidence{RunID: "RUN-260802-000011", Model: "gpt-5.4-mini"}); err != nil {
		t.Fatalf("ObserveSuccess: %v", err)
	}
	if got := f.record(t, codexPlan).State; got != StateAvailable {
		t.Fatalf("state = %q, want available", got)
	}
}

// --- ladder validation ------------------------------------------------------

// The maximum step is six hours, asserted against a literal rather than against
// the constant, so narrowing or widening the bound fails here rather than silently
// moving the test with it.
func TestMaxLadderStepIsSixHours(t *testing.T) {
	if MaxLadderStep != 6*time.Hour {
		t.Fatalf("MaxLadderStep = %s, want 6h", MaxLadderStep)
	}
	if err := ValidateLadder([]time.Duration{6 * time.Hour}); err != nil {
		t.Fatalf("a 6h step must be admitted: %v", err)
	}
	if err := ValidateLadder([]time.Duration{6*time.Hour - time.Second}); err != nil {
		t.Fatalf("a step just under 6h must be admitted: %v", err)
	}
	if err := ValidateLadder([]time.Duration{6*time.Hour + time.Second}); err == nil {
		t.Fatal("a step just over 6h must be rejected")
	}
	// And the same boundary through the production constructor.
	if _, err := NewStore(Options{Layout: LayoutAt(t.TempDir()), Ladder: []time.Duration{6 * time.Hour}}); err != nil {
		t.Fatalf("NewStore with a 6h ladder: %v", err)
	}
	if _, err := NewStore(Options{Layout: LayoutAt(t.TempDir()), Ladder: []time.Duration{6*time.Hour + time.Second}}); err == nil {
		t.Fatal("NewStore must reject a step just over 6h")
	}
}

func TestLadderValidation(t *testing.T) {
	cases := []struct {
		name   string
		ladder []time.Duration
		wantOK bool
	}{
		{"default", DefaultLadder(), true},
		{"single step", []time.Duration{time.Minute}, true},
		{"at the maximum", []time.Duration{time.Minute, MaxLadderStep}, true},
		{"above the maximum", []time.Duration{time.Minute, MaxLadderStep + time.Second}, false},
		{"far above the maximum", []time.Duration{7 * 24 * time.Hour}, false},
		{"decreasing", []time.Duration{5 * time.Minute, time.Minute}, false},
		{"zero step", []time.Duration{0}, false},
		{"negative step", []time.Duration{-time.Minute}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateLadder(tc.ladder)
			if tc.wantOK && err != nil {
				t.Fatalf("ValidateLadder(%v) = %v, want nil", tc.ladder, err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("ValidateLadder(%v) = nil, want a rejection", tc.ladder)
			}
		})
	}
}

// A ladder step above the 6h maximum is rejected at construction, not clamped
// silently. There is no reset TTL and no multi-day clamp anywhere in this module.
func TestNewStoreRejectsALadderStepAboveSixHours(t *testing.T) {
	_, err := NewStore(Options{
		Layout: LayoutAt(t.TempDir()),
		Ladder: []time.Duration{2 * time.Minute, 7 * time.Hour},
	})
	if err == nil {
		t.Fatal("a 7h ladder step must be rejected")
	}
	if _, err := NewStore(Options{Layout: LayoutAt(t.TempDir()), Ladder: []time.Duration{24 * time.Hour}}); err == nil {
		t.Fatal("a 24h ladder step must be rejected")
	}
	if _, err := NewStore(Options{Layout: LayoutAt(t.TempDir()), Ladder: []time.Duration{7 * 24 * time.Hour}}); err == nil {
		t.Fatal("a 7-day ladder step must be rejected; there is no 7-day clamp in this module")
	}
}

func TestClampLadderCapsAtTheMaximum(t *testing.T) {
	got := ClampLadder([]time.Duration{2 * time.Minute, 9 * time.Hour})
	if got[1] != MaxLadderStep {
		t.Fatalf("clamped step = %s, want %s", got[1], MaxLadderStep)
	}
	if err := ValidateLadder(got); err != nil {
		t.Fatalf("a clamped ladder must still validate: %v", err)
	}
}

func TestLeaseBoundsAreClamped(t *testing.T) {
	cases := []struct {
		name             string
		lease, claimMax  int
		wantLease        int
		wantClaimMinimum int
	}{
		{"defaults", 0, 0, DefaultProbeLeaseMinutes, DefaultProbeClaimMaxMinutes},
		{"below the floor", -5, -5, MinProbeLeaseMinutes, MinProbeClaimMaxMinutes},
		{"above the ceiling", 500, 5000, MaxProbeLeaseMinutes, MaxProbeClaimMaxMinutes},
		{"inverted", 90, 10, 90, 90},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(Options{
				Layout:               LayoutAt(t.TempDir()),
				ProbeLeaseMinutes:    tc.lease,
				ProbeClaimMaxMinutes: tc.claimMax,
			})
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			if got := store.ProbeLeaseMinutes(); got != tc.wantLease {
				t.Errorf("probe_lease_minutes = %d, want %d", got, tc.wantLease)
			}
			if got := store.ProbeClaimMaxMinutes(); got < tc.wantClaimMinimum {
				t.Errorf("probe_claim_max_minutes = %d, want at least %d", got, tc.wantClaimMinimum)
			}
			// The pre-launch bound can never be shorter than the lease it bounds.
			if store.ProbeClaimMaxMinutes() < store.ProbeLeaseMinutes() {
				t.Errorf("probe_claim_max_minutes (%d) < probe_lease_minutes (%d)", store.ProbeClaimMaxMinutes(), store.ProbeLeaseMinutes())
			}
		})
	}
}

// --- the atomic claim -------------------------------------------------------

// Twenty concurrent callers, one probe_eligible group, exactly one grant. This is
// the test that fails on a design that re-admits the group to every preflight
// once next_probe_at has passed: there, all twenty would have been admitted and
// each of their failures could have advanced the ladder.
func TestProbeClaimAtMaxParallelAdmitsExactlyOne(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	const parallel = 20
	leased, unleased, leases := f.claimConcurrently(t, codexPlan, parallel)
	if unleased != 0 {
		t.Fatalf("%d of %d callers were admitted without a lease; the group read as available and the claim gate was gone", unleased, parallel)
	}
	if leased != 1 {
		t.Fatalf("%d of %d callers were granted the probe, want exactly 1", leased, parallel)
	}
	winner := leases[0]

	// Every refused caller now fails its launch. Only the lease owner may
	// escalate, so the ladder must advance at most one step no matter how many
	// concurrent failures arrive.
	stepBefore := f.record(t, codexPlan).BackoffStep
	for i := range parallel {
		runID := runIDForIndex(i)
		if runID == winner.RunID {
			continue
		}
		err := f.observeQuota(t, codexPlan, nil, runID)
		if err != nil && !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("Observe from refused caller %s: %v", runID, err)
		}
	}
	if err := f.observeQuota(t, codexPlan, &winner, winner.RunID); err != nil {
		t.Fatalf("Observe from the lease owner: %v", err)
	}
	stepAfter := f.record(t, codexPlan).BackoffStep
	if stepAfter-stepBefore > 1 {
		t.Fatalf("backoff step went from %d to %d; %d concurrent failures must advance the ladder at most one step", stepBefore, stepAfter, parallel)
	}
	if stepAfter != stepBefore+1 {
		t.Fatalf("backoff step went from %d to %d; the resolved probe must advance exactly one step", stepBefore, stepAfter)
	}
}

func TestClaimProbeIsTotalOverTheStateSpace(t *testing.T) {
	f := newStoreFixture(t)

	// available (no record)
	lease, granted := f.claim(t, codexPlan, "RUN-260802-000001")
	if !granted || lease.Held() {
		t.Fatalf("available: granted=%v held=%v, want granted with no lease", granted, lease.Held())
	}
	// available (explicit record)
	if err := f.store.ObserveSuccess(f.identity, codexPlan, Evidence{RunID: "RUN-260802-000001"}); err != nil {
		t.Fatalf("ObserveSuccess: %v", err)
	}
	f.suppress(t, codexPlan)
	// suppressed
	if _, granted := f.claim(t, codexPlan, "RUN-260802-000002"); granted {
		t.Fatal("suppressed: want refused")
	}
	// probe_eligible -> probing
	f.clock.Advance(2 * time.Minute)
	held, granted := f.claim(t, codexPlan, "RUN-260802-000003")
	if !granted || !held.Held() {
		t.Fatal("probe_eligible: want granted with a lease")
	}
	// probing, current owner re-observes its own lease
	again, granted := f.claim(t, codexPlan, "RUN-260802-000003")
	if !granted {
		t.Fatal("probing: the current owner must be granted its own lease")
	}
	if !again.ClaimedAt.Equal(held.ClaimedAt) {
		t.Fatal("re-observing a held lease must not rewrite the fence")
	}
	// probing, foreign caller
	if _, granted := f.claim(t, codexPlan, "RUN-260802-000004"); granted {
		t.Fatal("probing under a foreign lease: want refused")
	}
}

// --- lease lifecycle: expiry by owner kind ---------------------------------

func TestExpiredProcessLeaseWithADeadOwnerReturnsToProbeEligibleAtTheSameStep(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-000005")
	stepBefore := f.record(t, codexPlan).BackoffStep

	// The claimant's pid is fabricated and long gone.
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes()+1) * time.Minute)
	newLease, granted := f.claim(t, codexPlan, "RUN-260802-000006")
	if !granted || !newLease.Held() {
		t.Fatal("a dead claimant's lease must be broken and the group re-offered, claim-gated")
	}
	if newLease.ClaimedAt.Equal(lease.ClaimedAt) {
		t.Fatal("a re-issued lease must carry a fresh claimed_at")
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepBefore {
		t.Fatalf("backoff step = %d, want %d: a lease that ended without a classifiable exit learns nothing", got, stepBefore)
	}
	if !f.warnings.Contains("no longer alive") {
		t.Errorf("an operator must be warned about a broken lease; warnings = %v", f.warnings.All())
	}
}

func TestPidReuseIsDetectedAndTreatedAsDead(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	// A live pid (this test process) recorded with the wrong start time is a
	// recycled pid, not the claimant.
	claimant := Claimant{
		RunID:        "RUN-260802-000007",
		OwnerKind:    OwnerProcess,
		PID:          selfPID(),
		PIDStartTime: f.clock.Now().Add(-72 * time.Hour),
	}
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, claimant)
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	stepBefore := f.record(t, codexPlan).BackoffStep

	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes()+1) * time.Minute)
	newLease, granted := f.claim(t, codexPlan, "RUN-260802-000008")
	if !granted || !newLease.Held() {
		t.Fatal("a reused pid must resolve as dead: same pid, different process")
	}
	if newLease.ClaimedAt.Equal(lease.ClaimedAt) {
		t.Fatal("the re-issued lease must be a new lease")
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepBefore {
		t.Fatalf("backoff step = %d, want %d", got, stepBefore)
	}
}

func TestTransferredLeaseWithALiveRunBecomesProvisionallyAvailable(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-000009")
	stepBefore := f.record(t, codexPlan).BackoffStep

	if err := f.store.TransferProbe(f.identity, codexPlan, lease, "RUN-260802-000009"); err != nil {
		t.Fatalf("TransferProbe: %v", err)
	}
	f.liveness.set(true, true)
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes()+1) * time.Minute)

	// The resolution happens under the lock of whichever caller next looks.
	_, granted := f.claim(t, codexPlan, "RUN-260802-00000a")
	if !granted {
		t.Fatal("an available group must be granted")
	}
	record := f.record(t, codexPlan)
	if record.State != StateAvailable {
		t.Fatalf("state = %q, want available: a launched run confirmed alive is the one non-exit-0 path to availability", record.State)
	}
	if record.ProbeProvisionalSince == nil {
		t.Fatal("provisional availability must be recorded as such")
	}
	if record.ProbeLease != nil {
		t.Fatal("the lease must be released")
	}
	// Nothing about provisional clearing is sticky: a later quota observation from
	// that run re-suppresses at the next step.
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-000009"); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepBefore+1 {
		t.Fatalf("backoff step = %d, want %d", got, stepBefore+1)
	}
}

func TestTransferredLeaseWithATerminalOrUnknownRunIsClaimGated(t *testing.T) {
	cases := []struct {
		name         string
		alive, known bool
	}{
		{"terminal or absent run", false, true},
		// known=false is the absence of evidence, not its presence. Provisional
		// availability requires positive evidence of a live launched run.
		{"liveness unknown", false, false},
		{"liveness unknown but reported alive", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStoreFixture(t)
			f.suppress(t, codexPlan)
			f.clock.Advance(2 * time.Minute)
			lease, _ := f.claim(t, codexPlan, "RUN-260802-00000b")
			stepBefore := f.record(t, codexPlan).BackoffStep
			if err := f.store.TransferProbe(f.identity, codexPlan, lease, "RUN-260802-00000b"); err != nil {
				t.Fatalf("TransferProbe: %v", err)
			}
			f.liveness.set(tc.alive, tc.known)
			f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes()+1) * time.Minute)

			leased, unleased, _ := f.claimConcurrently(t, codexPlan, 20)
			if unleased != 0 {
				t.Fatalf("%d callers were admitted without a lease; the group became available on the absence of evidence", unleased)
			}
			if leased != 1 {
				t.Fatalf("%d callers were granted, want exactly 1: the group must return claim-gated", leased)
			}
			if got := f.record(t, codexPlan).BackoffStep; got != stepBefore {
				t.Fatalf("backoff step = %d, want %d", got, stepBefore)
			}
		})
	}
}

func TestNoInjectedRunLivenessResolvesToProbeEligible(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) { o.RunLiveness = nil })
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-00000c")
	if err := f.store.TransferProbe(f.identity, codexPlan, lease, "RUN-260802-00000c"); err != nil {
		t.Fatalf("TransferProbe: %v", err)
	}
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes()+1) * time.Minute)
	leased, unleased, _ := f.claimConcurrently(t, codexPlan, 20)
	if unleased != 0 || leased != 1 {
		t.Fatalf("leased=%d unleased=%d; with no liveness implementation the group must return claim-gated, not available", leased, unleased)
	}
}

// --- non-quota failure and abandonment -------------------------------------

// A non-quota non-zero exit re-arms at the SAME step: the group was reached but
// produced no quota evidence, so the ladder must not move.
func TestUnrelatedFailureReArmsAtTheSameStep(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-00000d")
	stepBefore := f.record(t, codexPlan).BackoffStep

	// The child exited non-zero for an unrelated reason, so the classifier
	// produces no observation at all.
	class := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("panic: nil map write\n")})
	if class.Class != ClassNotLimit {
		t.Fatalf("class = %q, want not-a-limit", class.Class)
	}
	if err := f.store.AbandonProbe(f.identity, codexPlan, lease); err != nil {
		t.Fatalf("AbandonProbe: %v", err)
	}
	record := f.record(t, codexPlan)
	if record.BackoffStep != stepBefore {
		t.Fatalf("backoff step = %d, want %d", record.BackoffStep, stepBefore)
	}
	want := f.clock.Now().Add(f.store.Ladder()[stepBefore])
	if !record.NextProbeAt.Equal(want) {
		t.Fatalf("next_probe_at = %s, want %s", record.NextProbeAt, want)
	}
}

// AbandonProbe is not a probe result. A run that died in prompt composition must
// never push an unrelated group up the ladder.
func TestAbandonProbeDoesNotAdvanceTheLadder(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-00000e")
	before := f.record(t, codexPlan)

	if err := f.store.AbandonProbe(f.identity, codexPlan, lease); err != nil {
		t.Fatalf("AbandonProbe: %v", err)
	}
	after := f.record(t, codexPlan)
	if after.BackoffStep != before.BackoffStep {
		t.Fatalf("backoff step advanced from %d to %d; an abandoned claim must never look like a failed probe", before.BackoffStep, after.BackoffStep)
	}
	if after.State != StateSuppressed {
		t.Fatalf("state = %q, want suppressed", after.State)
	}
	if after.ProbeLease != nil {
		t.Fatal("the lease must be released")
	}
	if after.ProbeAbandoned != 1 {
		t.Fatalf("probe_abandoned = %d, want 1", after.ProbeAbandoned)
	}
	want := f.clock.Now().Add(f.store.Ladder()[before.BackoffStep])
	if !after.NextProbeAt.Equal(want) {
		t.Fatalf("next_probe_at = %s, want now + ladder[k] (%s)", after.NextProbeAt, want)
	}
	if after.Evidence == nil {
		t.Fatal("an abandon learned nothing, so it must erase no evidence")
	}
}

// --- transfer ---------------------------------------------------------------

func TestTransferProbeRewritesTheOwnerAndKeepsTheFence(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-00000f")

	f.clock.Advance(30 * time.Second)
	if err := f.store.TransferProbe(f.identity, codexPlan, lease, "RUN-260802-00000f"); err != nil {
		t.Fatalf("TransferProbe: %v", err)
	}
	stored := f.record(t, codexPlan).ProbeLease
	if stored == nil {
		t.Fatal("the lease must survive the transfer")
	}
	if stored.OwnerKind != OwnerRun {
		t.Fatalf("owner kind = %q, want run", stored.OwnerKind)
	}
	if stored.PID != 0 || !stored.PIDStartTime.IsZero() {
		t.Fatalf("the pid fields must be dropped: %+v", stored)
	}
	if !stored.ClaimedAt.Equal(lease.ClaimedAt) {
		t.Fatal("claimed_at is half of the fence and must never be rewritten")
	}
	want := f.clock.Now().Add(time.Duration(f.store.ProbeLeaseMinutes()) * time.Minute)
	if !stored.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at = %s, want it re-based to %s so the lease measures the child's life", stored.ExpiresAt, want)
	}
}

// The background-spawn case: the claiming CLI process exits shortly after the
// runner starts. A pid-owned lease would be broken by dead-owner recovery on the
// very next selection, re-admitting the group — the herd the claim exists to
// prevent, reintroduced by bookkeeping.
func TestGroupStaysProbingAfterTheClaimingProcessDies(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	child := startLiveChild(t)
	claimant := Claimant{
		RunID:        "RUN-260802-000020",
		OwnerKind:    OwnerProcess,
		PID:          child.pid,
		PIDStartTime: child.startTime,
	}
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, claimant)
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	if err := f.store.TransferProbe(f.identity, codexPlan, lease, "RUN-260802-000020"); err != nil {
		t.Fatalf("TransferProbe: %v", err)
	}

	// Kill the claiming process. The run is still live.
	child.kill(t)
	f.liveness.set(true, true)
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes()) * time.Minute / 2)

	if _, granted := f.claim(t, codexPlan, "RUN-260802-000021"); granted {
		t.Fatal("the group must stay probing: the run that owns the lease is alive")
	}
	record := f.record(t, codexPlan)
	if record.State != StateProbing {
		t.Fatalf("state = %q, want probing", record.State)
	}
	if record.ProbeLease == nil || record.ProbeLease.OwnerKind != OwnerRun {
		t.Fatalf("lease = %+v, want a run-owned lease", record.ProbeLease)
	}
}
