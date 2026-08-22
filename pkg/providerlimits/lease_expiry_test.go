package providerlimits

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A live PRE-LAUNCH process is evidence about the CLI, not about the provider.
//
// This test holds a real child process open past the lease expiry and asserts the
// group is still probing and still refused to everybody else — renewed under the
// same owner, never converted to available. It then advances past the claim
// deadline and asserts the wedged claimant is fenced out and the group returns
// CLAIM-GATED: exactly one of twenty concurrent callers gets in.
//
// It fails on the rule where any live owner converted the group to available: the
// first assertion would find the group available, and all twenty callers would
// have kept it.
func TestLivePreLaunchProcessNeverProducesAvailability(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	child := startLiveChild(t)
	claimant := Claimant{
		RunID:        "RUN-260802-000030",
		OwnerKind:    OwnerProcess,
		PID:          child.pid,
		PIDStartTime: child.startTime,
	}
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, claimant)
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	stepAtClaim := f.record(t, codexPlan).BackoffStep

	// Past expires_at, still well inside claim_deadline: the claimant is alive and
	// still working through the pre-launch steps.
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes())*time.Minute + time.Second)

	if _, granted := f.claim(t, codexPlan, "RUN-260802-000031"); granted {
		t.Fatal("a concurrent caller was admitted; a live pre-launch claimant keeps the gate")
	}
	record := f.record(t, codexPlan)
	if record.State != StateProbing {
		t.Fatalf("state = %q, want probing (renewed). A live pre-launch process has not talked to the provider at all", record.State)
	}
	if record.State == StateAvailable {
		t.Fatal("the group became available on the strength of a surviving CLI process")
	}
	if record.ProbeLease == nil {
		t.Fatal("the lease must survive renewal")
	}
	if record.ProbeLease.Renewals != 1 {
		t.Fatalf("renewals = %d, want 1", record.ProbeLease.Renewals)
	}
	if !record.ProbeLease.ExpiresAt.After(lease.ExpiresAt) {
		t.Fatalf("expires_at did not move: %s", record.ProbeLease.ExpiresAt)
	}
	if !record.ProbeLease.ClaimedAt.Equal(lease.ClaimedAt) {
		t.Fatal("renewal must not rewrite the fence")
	}
	if !record.ProbeLease.ClaimDeadline.Equal(lease.ClaimDeadline) {
		t.Fatal("the claim deadline is not renewable")
	}
	if record.BackoffStep != stepAtClaim {
		t.Fatalf("backoff step = %d, want %d: renewal learns nothing", record.BackoffStep, stepAtClaim)
	}

	// Past claim_deadline, still alive: thirty minutes without reaching a launch is
	// a wedged claimant, not a slow one. The lease is broken and FENCED, and the
	// group returns claim-gated rather than available.
	f.clock.Set(lease.ClaimDeadline.Add(time.Second))
	leased, unleased, leases := f.claimConcurrently(t, codexPlan, 20)
	if unleased != 0 {
		t.Fatalf("%d callers were admitted without a lease; the wedged claimant's group became available", unleased)
	}
	if leased != 1 {
		t.Fatalf("%d callers were granted, want exactly 1", leased)
	}
	if leases[0].RunID == lease.RunID {
		t.Fatal("the wedged claimant must not be handed its own lease back")
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepAtClaim {
		t.Fatalf("backoff step = %d, want %d: nothing was learned", got, stepAtClaim)
	}
	if !f.warnings.Contains("never launched within") {
		t.Errorf("a wedged claimant must produce a loud warning; warnings = %v", f.warnings.All())
	}
}

// A renewal must never move a process lease past the immutable claim deadline.
//
// The default 10m/30m geometry divides evenly, so a renewal happens to land on the
// deadline and the bound holds by accident. This test uses 17m/30m, where an
// uncapped renewal at the first expiry would run to claimed_at+34m01s — four
// minutes past the hard bound — and the group would still refuse every caller at
// claim_deadline+1s, because the resolver would not look again until that later
// expiry.
//
// It fails on the reviewed implementation in two independent places: the persisted
// expiry overruns the deadline, and the twenty concurrent callers at the deadline
// are all refused.
func TestProcessRenewalNeverOutlivesTheClaimDeadline(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) {
		o.ProbeLeaseMinutes = 17
		o.ProbeClaimMaxMinutes = 30
	})
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	child := startLiveChild(t)
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, Claimant{
		RunID:        "RUN-260803-000100",
		OwnerKind:    OwnerProcess,
		PID:          child.pid,
		PIDStartTime: child.startTime,
	})
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	stepAtClaim := f.record(t, codexPlan).BackoffStep

	// One second past the first expiry, still inside the deadline: the live
	// pre-launch owner renews. An uncapped renewal would end at claimed_at+34m01s.
	f.clock.Advance(17*time.Minute + time.Second)
	uncapped := f.clock.Now().Add(17 * time.Minute)
	if !uncapped.After(lease.ClaimDeadline) {
		t.Fatal("the fixture is broken: an uncapped renewal must overrun the deadline for this test to prove anything")
	}
	if _, granted := f.claim(t, codexPlan, "RUN-260803-000101"); granted {
		t.Fatal("a concurrent caller was admitted while a live pre-launch claimant held the gate")
	}

	renewed := f.storedLease(t, codexPlan)
	if renewed.Renewals != 1 {
		t.Fatalf("renewals = %d, want 1", renewed.Renewals)
	}
	if !renewed.ExpiresAt.After(lease.ExpiresAt) {
		t.Fatalf("expires_at did not move: %s", renewed.ExpiresAt)
	}
	if renewed.ExpiresAt.After(renewed.ClaimDeadline) {
		t.Fatalf("renewed expires_at %s overruns the immutable claim deadline %s by %s",
			renewed.ExpiresAt, renewed.ClaimDeadline, renewed.ExpiresAt.Sub(renewed.ClaimDeadline))
	}
	if !renewed.ClaimDeadline.Equal(lease.ClaimDeadline) {
		t.Fatal("the claim deadline is not renewable")
	}
	if !renewed.ClaimedAt.Equal(lease.ClaimedAt) {
		t.Fatal("renewal must not rewrite the fence")
	}

	// One second past the deadline, with the claimant still genuinely alive: it is
	// wedged, and the group must come back CLAIM-GATED at the same step.
	f.clock.Set(lease.ClaimDeadline.Add(time.Second))
	if !processAlive(child.pid) {
		t.Fatal("the child died early; the test would prove nothing")
	}
	f.warnings.Reset()
	leased, unleased, leases := f.claimConcurrently(t, codexPlan, 20)
	if unleased != 0 {
		t.Fatalf("%d callers were admitted without a lease; the wedged claimant's group became available", unleased)
	}
	if leased != 1 {
		t.Fatalf("%d callers were granted, want exactly 1: the deadline must dominate a renewed expiry", leased)
	}
	if leases[0].RunID == lease.RunID {
		t.Fatal("the wedged claimant must not be handed its own lease back")
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepAtClaim {
		t.Fatalf("backoff step = %d, want %d: breaking a wedged claim learns nothing", got, stepAtClaim)
	}
	if !f.warnings.Contains("never launched within") {
		t.Errorf("a wedged claimant must produce a loud warning; warnings = %v", f.warnings.All())
	}
}

// The cap is a ceiling, not a target. A renewal that fits inside the deadline must
// still end one lease interval from now, or a claimant that dies right after
// renewing would keep the group subtracted until claim_deadline instead of until
// its own expiry — the recovery latency the lease interval exists to bound.
//
// It fails on a cap written as an unconditional assignment to ClaimDeadline.
func TestARenewalInsideTheDeadlineIsNotStretchedToIt(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) {
		o.ProbeLeaseMinutes = 17
		o.ProbeClaimMaxMinutes = 60
	})
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	child := startLiveChild(t)
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, Claimant{
		RunID:        "RUN-260803-000120",
		OwnerKind:    OwnerProcess,
		PID:          child.pid,
		PIDStartTime: child.startTime,
	})
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	stepAtClaim := f.record(t, codexPlan).BackoffStep

	f.clock.Advance(17*time.Minute + time.Second)
	renewalAt := f.clock.Now()
	if _, granted := f.claim(t, codexPlan, "RUN-260803-000121"); granted {
		t.Fatal("a concurrent caller was admitted while a live pre-launch claimant held the gate")
	}
	renewed := f.storedLease(t, codexPlan)
	if want := renewalAt.Add(17 * time.Minute); !renewed.ExpiresAt.Equal(want) {
		t.Fatalf("renewed expires_at = %s, want %s: a renewal that fits inside the deadline must not be stretched to it", renewed.ExpiresAt, want)
	}
	if !renewed.ExpiresAt.Before(renewed.ClaimDeadline) {
		t.Fatalf("the fixture needs a renewal strictly inside the deadline: expires_at=%s deadline=%s", renewed.ExpiresAt, renewed.ClaimDeadline)
	}

	// The consequence: a claimant that dies just after renewing is recovered at its
	// own expiry, well before the claim deadline.
	child.kill(t)
	f.clock.Set(renewed.ExpiresAt.Add(time.Second))
	if !f.clock.Now().Before(lease.ClaimDeadline) {
		t.Fatal("the fixture needs to stay inside the claim deadline")
	}
	f.warnings.Reset()
	leased, unleased, _ := f.claimConcurrently(t, codexPlan, 20)
	if unleased != 0 {
		t.Fatalf("%d callers were admitted without a lease", unleased)
	}
	if leased != 1 {
		t.Fatalf("%d callers were granted, want exactly 1: a dead owner must be recovered at its own expiry", leased)
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepAtClaim {
		t.Fatalf("backoff step = %d, want %d", got, stepAtClaim)
	}
	if !f.warnings.Contains("no longer alive") {
		t.Errorf("warnings = %v", f.warnings.All())
	}
}

// The deadline must not be reachable only through the renewal cap. A state file
// written by another binary on this machine — an older task-board whose renewal
// was uncapped — carries a process lease whose expiry is already past its own
// deadline, and this binary must still honour the hard bound rather than wait for
// the foreign expiry.
//
// The same instant is asserted on all three surfaces, because AC 16 requires the
// report and the store to agree: the report must show the lease as expired, the
// LeaseLive predicate must read false, and the resolver must return the group
// claim-gated.
//
// It fails on any implementation that tests ExpiresAt alone: nothing resolves, the
// report advertises a live lease, and all twenty callers are refused.
func TestAClaimDeadlineIsNotHiddenBehindAForeignUncappedExpiry(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) {
		o.ProbeLeaseMinutes = 17
		o.ProbeClaimMaxMinutes = 30
	})
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	child := startLiveChild(t)
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, Claimant{
		RunID:        "RUN-260803-000110",
		OwnerKind:    OwnerProcess,
		PID:          child.pid,
		PIDStartTime: child.startTime,
	})
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	stepAtClaim := f.record(t, codexPlan).BackoffStep

	// Rewrite only what an uncapped renewal would have persisted: the expiry runs
	// four minutes past the deadline. Everything else in the file is this module's
	// own production output.
	foreignExpiry := lease.ClaimDeadline.Add(4 * time.Minute)
	rewriteStoredLeaseExpiry(t, f, codexPlan, foreignExpiry)

	f.clock.Set(lease.ClaimDeadline.Add(time.Second))
	if !f.clock.Now().Before(foreignExpiry) {
		t.Fatal("the fixture needs the foreign expiry to still be in the future")
	}
	if !processAlive(child.pid) {
		t.Fatal("the child died early; the test would prove nothing")
	}

	record := f.record(t, codexPlan)
	if record.LeaseLive(f.clock.Now()) {
		t.Fatal("LeaseLive reports a lease past its claim deadline as live")
	}
	group := reportedGroup(t, f, codexPlan)
	if !group.LeaseExpired {
		t.Fatalf("the report advertises a live lease past claim_deadline %s: %+v", lease.ClaimDeadline, group.Lease)
	}

	f.warnings.Reset()
	leased, unleased, leases := f.claimConcurrently(t, codexPlan, 20)
	if unleased != 0 {
		t.Fatalf("%d callers were admitted without a lease", unleased)
	}
	if leased != 1 {
		t.Fatalf("%d callers were granted, want exactly 1: a foreign expiry must not outrank the deadline", leased)
	}
	if leases[0].RunID == lease.RunID {
		t.Fatal("the wedged claimant must not be handed its own lease back")
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepAtClaim {
		t.Fatalf("backoff step = %d, want %d", got, stepAtClaim)
	}
	if !f.warnings.Contains("never launched within") {
		t.Errorf("warnings = %v", f.warnings.All())
	}
}

// reportedGroup is the report's projection of one group, so a test can assert the
// operator-facing surface and the store agree about the same instant.
func reportedGroup(t *testing.T, f *storeFixture, group string) GroupReport {
	t.Helper()
	report := f.store.Report([]Identity{f.identity})
	for _, identity := range report.Identities {
		if identity.Identity != f.identity.Key {
			continue
		}
		for _, row := range identity.Groups {
			if row.Group == group {
				return row
			}
		}
	}
	t.Fatalf("group %s is missing from the report", group)
	return GroupReport{}
}

// rewriteStoredLeaseExpiry edits one field of the on-disk state file, standing in
// for a state file this binary did not write.
func rewriteStoredLeaseExpiry(t *testing.T, f *storeFixture, group string, expiry time.Time) {
	t.Helper()
	path := f.store.Layout().StateFile(f.identity.Key)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var file identityStateFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	record := file.Groups[group]
	if record == nil || record.ProbeLease == nil {
		t.Fatalf("group %s holds no lease to rewrite", group)
	}
	record.ProbeLease.ExpiresAt = expiry
	payload, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// probe_claim_max_minutes must be >= probe_lease_minutes, otherwise a lease could
// pass its own outer bound before it was ever renewable.
// The 24h sweep never touches a record that holds a lease, even when the record
// itself is stale.
//
// The window is real, not hypothetical: a claim taken shortly before the TTL
// elapses can be renewed under a live pre-launch owner for up to
// probe_claim_max_minutes, and the record's observation anchor keeps ageing while
// that happens. Sweeping it would delete a lease somebody is actively holding,
// and a lease-free step 0 is claimable — so the group would be handed to a second
// spawn while the first is still probing it. Resolving a lease belongs to
// resolveStaleLease alone, which knows the owner; the sweep does not.
func TestTheSweepNeverTouchesARecordHoldingALease(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)

	// Claim ten minutes before the record goes stale, with a genuinely live
	// pre-launch owner.
	f.clock.Advance(StaleRecordTTL - 10*time.Minute)
	child := startLiveChild(t)
	claimant := Claimant{
		RunID:        "RUN-260802-000060",
		OwnerKind:    OwnerProcess,
		PID:          child.pid,
		PIDStartTime: child.startTime,
	}
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, claimant)
	if err != nil || !granted || !lease.Held() {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	stepAtClaim := f.record(t, codexPlan).BackoffStep

	// Past the lease expiry and past the stale-record TTL, but still inside the
	// claim deadline: the owner is alive and its lease is renewable.
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes())*time.Minute + time.Minute)
	if !f.record(t, codexPlan).staleAt(f.clock.Now()) {
		t.Fatal("the fixture needs a record that is stale while its lease is renewable")
	}
	if !f.clock.Now().Before(lease.ClaimDeadline) {
		t.Fatal("the fixture needs to stay inside the claim deadline")
	}

	if _, granted := f.claim(t, codexPlan, "RUN-260802-000061"); granted {
		t.Fatal("a second caller was admitted; the sweep aged out a group somebody was probing")
	}
	record := f.record(t, codexPlan)
	if record.ProbeLease == nil || record.ProbeLease.RunID != lease.RunID {
		t.Fatalf("the live owner's lease was swept away: %+v", record.ProbeLease)
	}
	if record.ProbeLease.Renewals != 1 {
		t.Fatalf("renewals = %d, want 1: the lease must be renewed by the owner-aware path", record.ProbeLease.Renewals)
	}
	if record.State != StateProbing {
		t.Fatalf("state = %q, want probing", record.State)
	}
	if record.BackoffStep != stepAtClaim {
		t.Fatalf("backoff step = %d, want %d: nothing was learned while the probe was in flight", record.BackoffStep, stepAtClaim)
	}
	if record.AgedOutAt != nil {
		t.Error("a record holding a lease must not be aged out")
	}
}

func TestClaimDeadlineIsNeverShorterThanTheLease(t *testing.T) {
	store, err := NewStore(Options{
		Layout:               LayoutAt(t.TempDir()),
		ProbeLeaseMinutes:    45,
		ProbeClaimMaxMinutes: 5,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if store.ProbeClaimMaxMinutes() < store.ProbeLeaseMinutes() {
		t.Fatalf("probe_claim_max_minutes (%d) < probe_lease_minutes (%d)", store.ProbeClaimMaxMinutes(), store.ProbeLeaseMinutes())
	}
}

// The process arm of OwnerLiveness is always answerable. It must never return
// known=false, and an unreadable start time must be reported as NOT alive rather
// than as unknown, because reuse cannot be ruled out.
func TestProcessLivenessNeverReturnsUnknown(t *testing.T) {
	checker := ProcessLivenessChecker{}
	child := startLiveChild(t)

	cases := []struct {
		name      string
		lease     Lease
		wantAlive bool
	}{
		{"live pid with a matching start time", Lease{OwnerKind: OwnerProcess, PID: child.pid, PIDStartTime: child.startTime}, true},
		{"live pid with a mismatched start time (reuse)", Lease{OwnerKind: OwnerProcess, PID: child.pid, PIDStartTime: child.startTime.Add(-time.Hour)}, false},
		{"no pid", Lease{OwnerKind: OwnerProcess, PID: 0}, false},
		{"negative pid", Lease{OwnerKind: OwnerProcess, PID: -1}, false},
		{"pid that cannot exist", Lease{OwnerKind: OwnerProcess, PID: 0x7ffffffe, PIDStartTime: child.startTime}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			alive, known := checker.Alive(tc.lease)
			if !known {
				t.Fatal("the process arm returned known=false; local pid liveness is always determinable")
			}
			if alive != tc.wantAlive {
				t.Fatalf("alive = %v, want %v", alive, tc.wantAlive)
			}
		})
	}

	child.kill(t)
	alive, known := checker.Alive(Lease{OwnerKind: OwnerProcess, PID: child.pid, PIDStartTime: child.startTime})
	if !known {
		t.Fatal("the process arm returned known=false for a dead pid")
	}
	if alive {
		t.Fatal("a killed child must read as not alive")
	}
}

// An unreadable start time resolves as NOT alive: the conservative direction costs
// one extra probe, while the lenient one costs a herd.
func TestUnreadableStartTimeResolvesAsNotAlive(t *testing.T) {
	// A pid that exists (this process) whose recorded start time cannot be
	// reconciled is the reachable form of "unreadable": the reader either fails or
	// returns something that does not match, and both mean the same thing.
	checker := ProcessLivenessChecker{}
	alive, known := checker.Alive(Lease{
		OwnerKind:    OwnerProcess,
		PID:          selfPID(),
		PIDStartTime: time.Unix(1, 0),
	})
	if !known {
		t.Fatal("known must stay true")
	}
	if alive {
		t.Fatal("an irreconcilable start time must resolve as not alive")
	}

	// And the store path treats an unknown answer from a process arm as dead,
	// warning that it was a programming error rather than a supported answer.
	f := newStoreFixture(t, func(o *Options) {
		o.ProcessLiveness = RunLivenessFunc(func(Lease) (bool, bool) { return true, false })
	})
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	if _, granted := f.claim(t, codexPlan, "RUN-260802-000040"); !granted {
		t.Fatal("expected the probe claim")
	}
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes()+1) * time.Minute)
	if _, granted := f.claim(t, codexPlan, "RUN-260802-000041"); !granted {
		t.Fatal("an unanswerable process owner must resolve as dead, returning the group claim-gated")
	}
	if !f.warnings.Contains("returned no answer") {
		t.Errorf("warnings = %v", f.warnings.All())
	}
}

// A recorded start time of ZERO is the reachable form of "unreadable at claim
// time": ProcessClaimant returns a usable claimant with a zero start time when its
// own start-time read fails, and every non-unix platform takes that branch on every
// claim.
//
// The rule is the same as for a mismatched start time: reuse cannot be excluded, so
// the owner is NOT ALIVE, and known stays true. This test asserts it directly on the
// checker, against a pid that genuinely exists right now.
//
// It fails on the reviewed implementation, which returned alive=true whenever the
// stored start time was zero and the pid existed.
func TestZeroRecordedStartTimeResolvesAsNotAlive(t *testing.T) {
	checker := ProcessLivenessChecker{}
	child := startLiveChild(t)

	if alive, _ := checker.Alive(Lease{OwnerKind: OwnerProcess, PID: child.pid, PIDStartTime: child.startTime}); !alive {
		t.Fatal("the control case is broken: a live child with its real start time must read alive")
	}

	alive, known := checker.Alive(Lease{OwnerKind: OwnerProcess, PID: child.pid})
	if !known {
		t.Fatal("known must stay true; local pid liveness is always determinable")
	}
	if alive {
		t.Fatal("a live pid with no recorded start time must resolve as NOT alive: pid reuse cannot be excluded")
	}
	// Same for this process, which is unambiguously alive.
	if alive, known := checker.Alive(Lease{OwnerKind: OwnerProcess, PID: selfPID()}); alive || !known {
		t.Fatalf("self with no recorded start time: alive=%v known=%v, want false/true", alive, known)
	}
}

// The production consequence of the rule above: a lease whose claimant could not
// record its start time is resolvable at expiry even while that pid is still
// running, and it resolves CLAIM-GATED at the SAME step — not available, and not
// escalated.
//
// It fails on the reviewed implementation, where the live pid renewed the lease
// indefinitely: no caller would have been admitted, and the group would have stayed
// wedged behind a claimant whose identity could not be confirmed.
func TestLeaseWithNoRecordedStartTimeIsClaimGatedAtTheSameStep(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	child := startLiveChild(t)
	claimant := Claimant{
		RunID:     "RUN-260802-000090",
		OwnerKind: OwnerProcess,
		PID:       child.pid,
		// No PIDStartTime: this is exactly what ProcessClaimant persists when its
		// own start-time read fails.
	}
	lease, granted, err := f.store.ClaimProbe(f.identity, codexPlan, claimant)
	if err != nil || !granted {
		t.Fatalf("ClaimProbe: granted=%v err=%v", granted, err)
	}
	if !lease.PIDStartTime.IsZero() {
		t.Fatal("the fixture must persist a lease with no start time")
	}
	stepAtClaim := f.record(t, codexPlan).BackoffStep

	// Past expires_at but well inside claim_deadline. The pid is still running, so
	// the only thing that can resolve this lease is the unreadable-start-time rule.
	f.clock.Advance(time.Duration(f.store.ProbeLeaseMinutes())*time.Minute + time.Second)
	if !processAlive(child.pid) {
		t.Fatal("the child died early; the test would prove nothing")
	}

	leased, unleased, leases := f.claimConcurrently(t, codexPlan, 20)
	if unleased != 0 {
		t.Fatalf("%d callers were admitted without a lease; an unconfirmable owner must not make the group available", unleased)
	}
	if leased != 1 {
		t.Fatalf("%d callers were granted, want exactly 1", leased)
	}
	if leases[0].RunID == lease.RunID {
		t.Fatal("the displaced claimant must not be handed its own lease back")
	}
	if got := f.record(t, codexPlan).BackoffStep; got != stepAtClaim {
		t.Fatalf("backoff step = %d, want %d: breaking an unconfirmable lease learns nothing", got, stepAtClaim)
	}
	if !f.warnings.Contains("no longer alive") {
		t.Errorf("recovering an unconfirmable owner must warn; warnings = %v", f.warnings.All())
	}
}

// The lock holder's unreadable start time resolves the OPPOSITE way, and that is
// deliberate. Misjudging a lease owner as gone costs one extra probe; misjudging a
// lock holder as gone unlinks a live lock and lets two writers race one identity
// file. Without positive evidence that the holder died, the lock stands.
func TestLockHeldByALivePidWithNoRecordedStartTimeIsNotBroken(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) { o.LockTimeout = 40 * time.Millisecond })
	child := startLiveChild(t)
	lockPath := f.store.Layout().LockFile(f.identity.Key)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// A holder that recorded no start time, exactly as a platform with no
	// start-time reader would write it.
	payload, _ := json.Marshal(lockInfo{PID: child.pid, AcquiredAt: f.clock.Now()})
	if err := os.WriteFile(lockPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockBreakAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-000091"); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Observe = %v, want ErrLockTimeout: a live holder keeps its lock even with no recorded start time", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("the live holder's lock was unlinked: %v", err)
	}

	// And once that holder is genuinely dead, the same lock is broken.
	child.kill(t)
	f.warnings.Reset()
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-000092"); err != nil {
		t.Fatalf("Observe over a dead holder's lock: %v", err)
	}
	if !f.warnings.Contains("was held by dead pid") {
		t.Errorf("breaking a dead holder's lock must warn; warnings = %v", f.warnings.All())
	}
}
