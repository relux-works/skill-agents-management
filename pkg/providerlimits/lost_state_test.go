package providerlimits

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The whole point of these tests is the difference between two facts that used
// to produce one value: "this machine never recorded anything" and "this machine
// recorded something and could not read it back".
//
// Every one of them drives the REAL store over REAL files. Injecting a read
// error into a fake would prove the branch exists; it would not prove that
// ClaimProbe, LoadIdentityState and the quarantine actually reach it.

// corrupt replaces the identity's state file with bytes that are not JSON, which
// is the shape a truncated or interrupted foreign write leaves behind.
func corruptStateFile(t *testing.T, f *storeFixture) string {
	t.Helper()
	path := f.store.Layout().StateFile(f.identity.Key)
	if err := os.WriteFile(path, []byte(`{"version":3,"groups":{`), 0o600); err != nil {
		t.Fatalf("corrupting %s: %v", path, err)
	}
	return path
}

// AC1. A corrupt file and an absent file must not resolve to the same value, and
// the difference must reach a DECISION rather than only a label.
//
// The two halves run against two separate stores so nothing but the file on disk
// differs between them.
func TestCorruptStateDoesNotResolveToTheSameValueAsAnAbsentOne(t *testing.T) {
	absent := newStoreFixture(t)
	if read := absent.store.LoadIdentityState(absent.identity).Read; read != StateReadAbsent {
		t.Fatalf("read on a machine with no file = %q, want %q", read, StateReadAbsent)
	}
	absentLease, absentGranted := absent.claim(t, codexPlan, "RUN-260806-00a000")
	if !absentGranted || absentLease.Held() {
		t.Fatalf("a proven absence must admit the group with no lease, got granted=%v held=%v", absentGranted, absentLease.Held())
	}

	corrupt := newStoreFixture(t)
	corrupt.suppress(t, codexPlan)
	corruptStateFile(t, corrupt)
	if read := corrupt.store.LoadIdentityState(corrupt.identity).Read; read != StateReadCorrupt {
		t.Fatalf("read on a corrupt file = %q, want %q", read, StateReadCorrupt)
	}
	corruptLease, corruptGranted := corrupt.claim(t, codexPlan, "RUN-260806-00a001")
	if corruptGranted || corruptLease.Held() {
		t.Fatalf("a lost record must not be admitted as an absent one, got granted=%v held=%v", corruptGranted, corruptLease.Held())
	}

	// The assertion that fails when the distinction is narrowed away: the two
	// reads differ AND the two claims differ. Collapsing corrupt back into
	// absent flips the second one.
	if absentGranted == corruptGranted {
		t.Fatalf("an absent file and a corrupt one produced the same claim decision (%v); the read failure is being reported as an absence again", absentGranted)
	}
}

// AC3. The claim a corrupt file must never grant: one that another live run is
// already holding.
//
// The lease is taken through the real claim path and is genuinely live — its
// resolution instant is in the future — when the file is corrupted underneath
// it.
func TestCorruptStateCannotGrantAProbeLeaseAnotherLiveRunHolds(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Set(f.nextProbeAt(t, codexPlan))

	const holder = "RUN-260806-00a010"
	held, granted := f.claim(t, codexPlan, holder)
	if !granted || !held.Held() {
		t.Fatalf("the fixture must start from a genuinely held lease, got granted=%v held=%v", granted, held.Held())
	}
	if !f.clock.Now().Before(held.ResolveAt()) {
		t.Fatalf("the held lease must still be live at %s (resolves at %s)", f.clock.Now(), held.ResolveAt())
	}

	corruptStateFile(t, f)

	lease, granted := f.claim(t, codexPlan, "RUN-260806-00a011")
	if granted || lease.Held() {
		t.Fatalf("a corrupt file granted a probe on a group run %s is still probing: granted=%v held=%v", holder, granted, lease.Held())
	}
	if !f.warnings.Contains("could still be live") {
		t.Errorf("the refusal must tell the operator why; warnings = %v", f.warnings.All())
	}

	// The refusal survives repetition. A caller that simply retries must not be
	// able to walk past the blackout, and the quarantine the first call
	// performed must not have turned the loss into an absence.
	if read := f.store.LoadIdentityState(f.identity).Read; read.Indeterminate() != true || read == StateReadAbsent {
		t.Fatalf("read after the quarantine = %q, want an indeterminate read", read)
	}
	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a012"); granted || lease.Held() {
		t.Fatalf("retrying inside the blackout was granted: granted=%v held=%v", granted, lease.Held())
	}
}

// AC3, the blackout's MAGNITUDE rather than its existence.
//
// Deleting the blackout is caught by the test above. NARROWING it is not, and the
// dangerous narrowing is probe_claim_max_minutes -> probe_lease_minutes: with the
// shipped defaults that opens a twenty-minute hole. A lease claimed at T expires
// at T+lease, but its claim deadline is T+claim_max, and a LIVE pre-launch owner
// inside that deadline gets its lease renewed rather than handed over — see
// TestProcessRenewalNeverOutlivesTheClaimDeadline. So at T+lease+1m the first run
// can still legitimately be holding the group, and admitting a second probe there
// is two live runs on one probe, which is exactly what AC 3 forbids.
//
// The two tests around this one sit on either side of that gap: one probes at the
// instant of the loss, the other past claim_max, which is beyond BOTH candidate
// bounds. This one sits strictly inside it.
//
// The instant is computed from the store's own two bounds and never from 10 and
// 30. A test that hardcodes the defaults stops testing the moment an operator
// configures different ones.
func TestLostStateRefusesAProbeWhileTheLostLeaseCouldStillBeRenewed(t *testing.T) {
	f := newStoreFixture(t)
	leaseMinutes := f.store.ProbeLeaseMinutes()
	claimMaxMinutes := f.store.ProbeClaimMaxMinutes()
	if leaseMinutes >= claimMaxMinutes {
		t.Fatalf("this store leaves no gap to probe (lease=%dm claim_max=%dm); the narrowing this test pins is only observable when the two differ", leaseMinutes, claimMaxMinutes)
	}

	f.suppress(t, codexPlan)
	f.clock.Set(f.nextProbeAt(t, codexPlan))
	// The lost owner is ALIVE, so "it could still be holding the group" is a fact
	// about this fixture rather than an assumption about the arithmetic.
	f.liveness.set(true, true)

	const holder = "RUN-260806-00a0a0"
	held, granted := f.claim(t, codexPlan, holder)
	if !granted || !held.Held() {
		t.Fatalf("the fixture must start from a genuinely held lease, got granted=%v held=%v", granted, held.Held())
	}

	corruptStateFile(t, f)
	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a0a1"); granted || lease.Held() {
		t.Fatalf("the blackout must refuse at the instant of the loss: granted=%v held=%v", granted, lease.Held())
	}

	f.clock.Advance(time.Duration(leaseMinutes)*time.Minute + time.Minute)
	now := f.clock.Now()
	if !now.After(held.ExpiresAt) {
		t.Fatalf("the probe instant %s is not past the lost lease's expiry %s, so it does not sit inside the gap between the two bounds", now, held.ExpiresAt)
	}
	if !now.Before(held.ClaimDeadline) {
		t.Fatalf("the probe instant %s is not before the lost lease's claim deadline %s, so run %s could no longer hold the group and a narrowed bound would be harmless here", now, held.ClaimDeadline, holder)
	}

	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a0a2"); granted || lease.Held() {
		t.Fatalf("a probe was admitted %dm after the loss while run %s's claim deadline (%s) had not passed: granted=%v held=%v", leaseMinutes+1, holder, held.ClaimDeadline, granted, lease.Held())
	}
}

// The other half of AC3: the blackout is bounded, so a corrupt file cannot brick
// the machine. Once no lease written before the loss can still be live, exactly
// ONE caller is admitted — with a lease, not as an available group.
func TestLostStateAdmitsExactlyOneProbeOnceNoLostLeaseCanBeLive(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Set(f.nextProbeAt(t, codexPlan))
	held, granted := f.claim(t, codexPlan, "RUN-260806-00a020")
	if !granted || !held.Held() {
		t.Fatal("the fixture must start from a genuinely held lease")
	}
	corruptStateFile(t, f)
	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a021"); granted || lease.Held() {
		t.Fatal("the blackout must refuse the first claim")
	}

	f.clock.Advance(time.Duration(f.store.ProbeClaimMaxMinutes())*time.Minute + time.Minute)
	if f.clock.Now().Before(held.ResolveAt()) {
		t.Fatalf("the blackout ended at %s while the lost lease still resolves at %s", f.clock.Now(), held.ResolveAt())
	}

	leased, unleased, leases := f.claimConcurrently(t, codexPlan, 8)
	if leased != 1 || unleased != 0 {
		t.Fatalf("claims past the blackout: leased=%d unleased=%d, want exactly one leased and no unleased grant", leased, unleased)
	}
	if len(leases) != 1 || leases[0].RunID == held.RunID {
		t.Fatalf("the admitted probe must be a fresh lease, got %+v", leases)
	}
}

// AC4. A suppressed group must not read as available because the file that
// recorded the suppression could not be parsed.
//
// "Available" in this state machine has one operational meaning: admitted to
// every caller with no lease. That is what is asserted here — both on the
// read-only report and on the claim itself, at both ends of the blackout.
func TestCorruptStateDoesNotMakeASuppressedGroupLookAvailable(t *testing.T) {
	f := newStoreFixture(t)
	f.escalateToMaxStep(t, codexPlan)
	if state := f.record(t, codexPlan).EffectiveState(f.clock.Now()); state != StateSuppressed {
		t.Fatalf("the fixture must start from a suppressed group, got %q", state)
	}
	corruptStateFile(t, f)

	// The read-only surface: no groups, but never presented as availability.
	report := f.store.Report([]Identity{f.identity})
	if len(report.Identities) != 1 {
		t.Fatalf("report identities = %d, want 1", len(report.Identities))
	}
	entry := report.Identities[0]
	if len(entry.Groups) != 0 {
		t.Fatalf("a corrupt file cannot produce group records, got %+v", entry.Groups)
	}
	if !entry.Indeterminate || entry.StateRead != StateReadCorrupt {
		t.Fatalf("report entry = {state_read:%q indeterminate:%v}, want a corrupt indeterminate read", entry.StateRead, entry.Indeterminate)
	}

	// Inside the blackout: refused outright.
	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a030"); granted || lease.Held() {
		t.Fatalf("a suppressed group read as available inside the blackout: granted=%v held=%v", granted, lease.Held())
	}

	// Past it: claim-gated, which is emphatically NOT available. Seven of eight
	// concurrent callers stay subtracted.
	f.clock.Advance(time.Duration(f.store.ProbeClaimMaxMinutes())*time.Minute + time.Minute)
	leased, unleased, _ := f.claimConcurrently(t, codexPlan, 8)
	if unleased != 0 {
		t.Fatalf("%d callers were admitted with no lease; a lost suppression must never read as available", unleased)
	}
	if leased != 1 {
		t.Fatalf("leased grants = %d, want exactly one", leased)
	}
}

// The quarantine must not launder a failed read into a proven absence. Renaming
// the unusable file away and leaving nothing behind would do exactly that: the
// next reader finds no file and reports StateReadAbsent, which is a claim about
// the provider nobody established.
func TestQuarantineLeavesATombstoneRatherThanAnAbsence(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	path := corruptStateFile(t, f)

	// A mutation drives the quarantine through the production path.
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260806-00a040"); err != nil {
		t.Fatalf("Observe over a corrupt file: %v", err)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("quarantine copies = %v (%v), want exactly one", matches, err)
	}
	state := f.store.LoadIdentityState(f.identity)
	if state.Read == StateReadAbsent {
		t.Fatal("the quarantine reported the loss as an absence")
	}
	if state.Read != StateReadDegraded {
		t.Fatalf("read after the quarantine = %q, want %q", state.Read, StateReadDegraded)
	}

	// The tombstone expires by the clock alone, and only once nothing the lost
	// file could have held would still be arming a suppression.
	f.clock.Advance(LostStateHorizon - time.Minute)
	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadDegraded {
		t.Fatalf("read one minute before the horizon = %q, want %q", read, StateReadDegraded)
	}
	f.clock.Advance(2 * time.Minute)
	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadOK {
		t.Fatalf("read past the horizon = %q, want %q", read, StateReadOK)
	}
}

// A degraded_since stamped in the future must not buy an indefinite blackout: a
// skewed clock or a hand-edited file would otherwise be able to gate the machine
// forever, which is the fail-closed failure mode this design exists to bound.
func TestFutureDegradedStampCannotExtendTheBlackout(t *testing.T) {
	f := newStoreFixture(t)
	path := f.store.Layout().StateFile(f.identity.Key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	future := f.clock.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	payload := `{"version":3,"identity":"` + f.identity.Key + `","provider":"codex","groups":{},"degraded_since":"` + future + `"}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadDegraded {
		t.Fatalf("read = %q, want %q: a future stamp is still a recorded loss", read, StateReadDegraded)
	}
	// It buys nothing while it stands: the identity stays gated, which is the
	// conservative direction.
	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a090"); granted || lease.Held() {
		t.Fatalf("a future degraded stamp granted a claim: granted=%v held=%v", granted, lease.Held())
	}
	// And the first write anchors it, so the blackout becomes measurable rather
	// than receding ahead of every read.
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260806-00a091"); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	f.clock.Advance(LostStateHorizon + time.Minute)
	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadOK {
		t.Fatalf("read = %q, want %q: an anchored stamp must expire on the horizon", read, StateReadOK)
	}
}

// An unreadable file — one that exists and cannot be opened — is the same fact
// as a corrupt one and takes the same path.
func TestUnreadableStateFileIsTombstonedAndBlocksTheClaim(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file, so the unreadable arm is unreachable here")
	}
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	path := f.store.Layout().StateFile(f.identity.Key)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("making %s unreadable: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadUnreadable {
		t.Fatalf("read = %q, want %q", read, StateReadUnreadable)
	}
	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a050"); granted || lease.Held() {
		t.Fatalf("an unreadable file granted a claim: granted=%v held=%v", granted, lease.Held())
	}
	if read := f.store.LoadIdentityState(f.identity).Read; read == StateReadAbsent {
		t.Fatal("the unreadable file was quarantined into an absence")
	}
}

// If the tombstone cannot be written, the unusable file is moved BACK. Failing to
// recover must never end up more permissive than not trying: a rename with no
// tombstone behind it is precisely the corrupt-becomes-absent laundering.
func TestTombstoneWriteFailureRestoresTheUnusableFile(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	path := corruptStateFile(t, f)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	f.store.writeState = func(string, any) error { return errors.New("disk full") }
	if lease, granted := f.claim(t, codexPlan, "RUN-260806-00a060"); granted || lease.Held() {
		t.Fatalf("a claim over an unwritable corrupt file was granted: granted=%v held=%v", granted, lease.Held())
	}
	f.store.writeState = writeJSONAtomic

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the corrupt file was moved away with no tombstone behind it: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the restored file changed:\n before %s\n after  %s", before, after)
	}
	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadCorrupt {
		t.Fatalf("read = %q, want %q", read, StateReadCorrupt)
	}
}

// An indeterminate read TAINTS the write derived from it. Otherwise one Observe
// over an unusable file produces a clean, parseable state holding exactly one
// group, and the next reader takes it at face value: one suppression proven,
// every other group proven available.
func TestAWriteDerivedFromALostReadStaysIndeterminate(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.suppress(t, "codex-code")
	corruptStateFile(t, f)

	if err := f.observeQuota(t, codexPlan, nil, "RUN-260806-00a070"); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	state := f.store.LoadIdentityState(f.identity)
	if _, ok := state.Groups[codexPlan]; !ok {
		t.Fatal("the observation this caller just made must be recorded: a lost read is not a reason to forget new evidence")
	}
	if _, ok := state.Groups["codex-code"]; ok {
		t.Fatal("the fixture is wrong: the second group's record survived the corruption")
	}
	if state.Read != StateReadDegraded {
		t.Fatalf("read = %q, want %q: the surviving group map is not a complete statement about the provider", state.Read, StateReadDegraded)
	}
	if !state.Read.Indeterminate() {
		t.Fatal("a state written over a lost read must not report as determinate")
	}
}

// The named fail-open subset, asserted so that it is a decision rather than an
// oversight: a schema-ahead file is intact, is being gated correctly by the newer
// binary that wrote it, and cannot be quarantined by this one — so it still
// admits the group.
func TestSchemaAheadStaysFailOpenAndIsTheOnlyIndeterminateReadThatDoes(t *testing.T) {
	f := newStoreFixture(t)
	path := f.store.Layout().StateFile(f.identity.Key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	future := []byte(`{"version":99,"identity":"` + f.identity.Key + `","provider":"codex","groups":{"codex-plan":{"state":"suppressed","backoff_step":4}}}`)
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatal(err)
	}
	read := f.store.LoadIdentityState(f.identity).Read
	if read != StateReadSchemaAhead || !read.Indeterminate() {
		t.Fatalf("read = %q (indeterminate=%v), want an indeterminate schema-ahead read", read, read.Indeterminate())
	}
	lease, granted := f.claim(t, codexPlan, "RUN-260806-00a080")
	if !granted || lease.Held() {
		t.Fatalf("schema-ahead is the retained fail-open arm: granted=%v held=%v, want an unleased grant", granted, lease.Held())
	}
	if !f.warnings.Contains("newer than this binary understands") {
		t.Errorf("the retained fail-open arm must still name itself; warnings = %v", f.warnings.All())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(future) {
		t.Fatalf("the newer file was rewritten:\n before %s\n after  %s", future, after)
	}
}

// Finding 2 of the cycle-1 review, store half.
//
// WHAT THIS TEST DOES AND DOES NOT PIN, because it was once cited for the wrong
// half. Its fixture fails EVERY write, so the claim refusals below are
// over-determined: ClaimProbe's probe_eligible -> probing transition exists only
// because it is persisted, so grantNeedsWrite refuses here no matter what the
// blackout decides. Narrowing lostStateClaimAllowed to admit a zero-elapsed loss
// fails eight other tests and leaves this one green. The assertion that the read
// stays StateReadCorrupt and the operator-facing half — the path, "does NOT clear
// by waiting", the remedy — are the load-bearing parts, and they are genuinely
// pinned: dropping the remedy sentence or dropping quarantine's restore both
// fail here.
//
// The blackout's OWN answer on a loss nobody could record is pinned separately,
// on a fixture where the write path is live, by
// TestAnUnrecordableLossIsRefusedByTheBlackoutAloneWhileWritesStillWork.
//
// What is asserted here is the operator contract: a diagnostic that does not
// tell the operator to wait out a window that will never close, and that names
// the remedy. This is reachable on a full disk, a read-only state directory or a
// permissions change, and every one of those is fixable.
func TestAnUnwritableTombstoneRefusesForeverAndNamesTheRemedyRatherThanAWait(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	path := corruptStateFile(t, f)

	f.store.writeState = func(string, any) error { return errors.New("read-only file system") }
	t.Cleanup(func() { f.store.writeState = writeJSONAtomic })

	start := f.clock.Now()
	// Every instant that lifts a RECORDED loss, and then far past all of them.
	probes := []struct {
		elapsed time.Duration
		runID   string
	}{
		{0, "RUN-260806-00a0b0"},
		{time.Duration(f.store.ProbeClaimMaxMinutes())*time.Minute + time.Minute, "RUN-260806-00a0b1"},
		{LostStateHorizon + time.Hour, "RUN-260806-00a0b2"},
		{30 * 24 * time.Hour, "RUN-260806-00a0b3"},
	}
	for _, probe := range probes {
		elapsed := probe.elapsed
		f.clock.Set(start.Add(elapsed))
		f.warnings.Reset()

		if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadCorrupt {
			t.Fatalf("at loss+%s the read = %q, want %q: an unwritable tombstone must leave the unusable file in place", elapsed, read, StateReadCorrupt)
		}
		lease, granted := f.claim(t, codexPlan, probe.runID)
		if granted || lease.Held() {
			t.Fatalf("at loss+%s a probe was admitted over a loss nobody could record: granted=%v held=%v", elapsed, granted, lease.Held())
		}

		// The operator-facing half. Waiting is the one thing that cannot help
		// here, so the warning may not imply it, and it has to say what does.
		for _, want := range []string{
			path,
			"does NOT clear by waiting",
			"make the state directory writable",
		} {
			if !f.warnings.Contains(want) {
				t.Fatalf("at loss+%s the warning does not carry %q; warnings = %v", elapsed, want, f.warnings.All())
			}
		}
	}
}

// The permanence of an unrecordable loss, isolated from the write path — the arm
// the cycle-2 review found described wrongly at lostStateClaimAllowed.
//
// TestAnUnwritableTombstoneRefusesForeverAndNamesTheRemedyRatherThanAWait proves
// the operator contract, but it cannot prove this one: its fixture fails EVERY
// write, so grantNeedsWrite refuses the claim whatever the blackout decides.
// Here the loss is unrecordable for a different reason — quarantine's RENAME
// fails, so no tombstone is even attempted — while writeState is the real
// writeJSONAtomic. Every refusal below is therefore the blackout's own.
//
// The mechanism this pins is the RESTAMP. Because no tombstone ever reaches the
// disk, DegradedSince is written by each load rather than read off the file, so
// it carries that load's now, never ages, and the elapsed-time bound measures
// zero at every instant — a month later included. Narrowing that bound to admit
// a zero-elapsed loss fails here. Flipping the DegradedSince == nil guard to
// return true does NOT, and must not be read as the mechanism: the guard is
// unreachable, which is exactly what the corrected comment now says.
func TestAnUnrecordableLossIsRefusedByTheBlackoutAloneWhileWritesStillWork(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	path := corruptStateFile(t, f)
	corrupt, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	start := f.clock.Now()
	// Every instant that lifts a RECORDED loss, and then far past all of them.
	probes := []struct {
		elapsed time.Duration
		runID   string
	}{
		{0, "RUN-260806-00a0d0"},
		{time.Duration(f.store.ProbeClaimMaxMinutes())*time.Minute + time.Minute, "RUN-260806-00a0d1"},
		{LostStateHorizon + time.Hour, "RUN-260806-00a0d2"},
		{30 * 24 * time.Hour, "RUN-260806-00a0d3"},
	}

	// Park a DIRECTORY on the name quarantine will move the file to at each of
	// those instants. rename(2) refuses to replace a directory with a file, so
	// quarantine takes its first failure arm: it fails before any tombstone is
	// attempted and leaves the unusable file exactly where it was.
	//
	// This arm IS reachable in production, by a non-writable state directory or a
	// permissions change — exactly what the shipped warning tells the operator to
	// fix — but on that route writeState fails too, so the blackout's refusal
	// would not be separable from grantNeedsWrite's. The directory is therefore a
	// synthetic obstruction, chosen only because it blocks the rename while
	// leaving writeState live. Nothing in this package ever puts a directory at
	// that name: the only self-collision the store can hit is one quarantined
	// file landing on another, which rename silently replaces, so quarantine
	// SUCCEEDS there and the loss becomes recordable.
	for _, probe := range probes {
		blocked := fmt.Sprintf("%s.corrupt-%s", path, start.Add(probe.elapsed).UTC().Format("20060102T150405Z"))
		if err := os.MkdirAll(filepath.Join(blocked, "occupied"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for _, probe := range probes {
		elapsed := probe.elapsed
		f.clock.Set(start.Add(elapsed))
		f.warnings.Reset()

		if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadCorrupt {
			t.Fatalf("at loss+%s the read = %q, want %q", elapsed, read, StateReadCorrupt)
		}
		lease, granted := f.claim(t, codexPlan, probe.runID)
		if granted || lease.Held() {
			t.Fatalf("at loss+%s a probe was admitted over a loss that could not be recorded: granted=%v held=%v", elapsed, granted, lease.Held())
		}

		// Anti-vacuity, and the whole reason this fixture exists: the refusal
		// above may not be an unpersistable grant. A second identity in the same
		// store and the same directory records a suppression that exists ONLY if
		// it was written, and it reads back off disk — so writeState is live at
		// this instant and grantNeedsWrite cannot be what refused the claim on
		// the corrupt identity.
		witness := writableWitnessIdentity(t, f, elapsed)
		if err := f.observeQuotaFor(t, witness, codexPlan, nil, probe.runID+"w"); err != nil {
			t.Fatalf("at loss+%s the witness observation failed: %v", elapsed, err)
		}
		witnessState := f.store.LoadIdentityState(witness)
		if witnessState.Read != StateReadOK {
			t.Fatalf("at loss+%s the witness read = %q, want %q: writes are not live, so this fixture proves nothing about the blackout", elapsed, witnessState.Read, StateReadOK)
		}
		if rec, ok := witnessState.Groups[codexPlan]; !ok || rec.State != StateSuppressed {
			t.Fatalf("at loss+%s the witness suppression did not persist (%+v); writes are not live, so this fixture proves nothing about the blackout", elapsed, witnessState.Groups)
		}

		// And the loss really is unrecordable: the file never moved and no
		// tombstone replaced it, so nothing on disk carries the loss instant.
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("at loss+%s the unusable file is gone: %v", elapsed, err)
		}
		if string(after) != string(corrupt) {
			t.Fatalf("at loss+%s the unusable file was rewritten, so the loss became recordable:\n before %s\n after  %s", elapsed, corrupt, after)
		}
		if !f.warnings.Contains("could not be quarantined") {
			t.Fatalf("at loss+%s this is not the rename-failure arm; warnings = %v", elapsed, f.warnings.All())
		}
	}
}

// writableWitnessIdentity is a fresh identity in the same store root, used to
// show that writeState works at this instant. It has to be distinct per probe,
// because a claim that already landed would be granted from its own record
// rather than from a write.
func writableWitnessIdentity(t *testing.T, f *storeFixture, elapsed time.Duration) Identity {
	t.Helper()
	identity, err := IdentityFor(ProviderCodex, fmt.Sprintf("%s/witness-%d", f.root, int64(elapsed)))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	return identity
}
