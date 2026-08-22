package providerlimits

import (
	"time"
)

// ProcessLivenessChecker is the process arm of OwnerLiveness, shipped by this
// package because a pre-launch lease is owned by a local pid on the same machine
// that took the claim.
//
// It never returns known=false. Local pid liveness is always determinable, and
// the one failure mode — an unreadable start time — is reported as NOT ALIVE
// rather than unknown: reuse cannot be ruled out, and the conservative direction
// costs one extra probe while the lenient one costs a herd.
type ProcessLivenessChecker struct{}

// Alive reports whether the lease's pid is still the process that took the claim.
func (ProcessLivenessChecker) Alive(lease Lease) (bool, bool) {
	if lease.PID <= 0 {
		return false, true
	}
	if !processAlive(lease.PID) {
		return false, true
	}
	if lease.PIDStartTime.IsZero() {
		// The claim was recorded without a readable start time, so pid reuse
		// cannot be excluded from the stored side. That is exactly the case the
		// rule covers: an unreadable start time is NOT ALIVE, never "assume the
		// same process". Trusting the bare pid would let a recycled pid hold a
		// probe lease indefinitely and wedge the group behind a claimant that no
		// longer exists.
		//
		// On a platform with no start-time reader this makes every lease
		// resolvable at its expiry, which is the intended cost: one extra probe
		// per backoff window, against a herd in the lenient direction.
		return false, true
	}
	start, err := ProcessStartTime(lease.PID)
	if err != nil {
		// Unreadable start time: reuse cannot be ruled out, so this is dead.
		return false, true
	}
	if !start.Truncate(time.Second).Equal(lease.PIDStartTime.Truncate(time.Second)) {
		// Same pid, different process: the claimant is gone and its pid was
		// recycled.
		return false, true
	}
	return true, true
}

// lockHolderGone reports whether the process recorded in a lock file is gone.
//
// It deliberately does NOT reuse the lease rule above, because the two answers
// have opposite costs. Misjudging a lease owner as gone costs one extra probe;
// misjudging a LOCK holder as gone unlinks a live lock and lets two writers race
// one identity file, which is lost state. So an unreadable start time keeps the
// lock: without positive evidence that the holder died, the lock stands.
func lockHolderGone(held lockInfo) bool {
	if held.PID <= 0 {
		return true
	}
	if !processAlive(held.PID) {
		return true
	}
	if held.PIDStartTime.IsZero() {
		return false
	}
	start, err := ProcessStartTime(held.PID)
	if err != nil {
		return false
	}
	return !start.Truncate(time.Second).Equal(held.PIDStartTime.Truncate(time.Second))
}

// RunLivenessFunc adapts a function to OwnerLiveness. The run arm is injected by
// the caller that owns the run store, which is what keeps this package free of a
// dependency on it.
type RunLivenessFunc func(lease Lease) (alive bool, known bool)

// Alive implements OwnerLiveness.
func (f RunLivenessFunc) Alive(lease Lease) (bool, bool) { return f(lease) }
