package providerlimits

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SchemaVersion is the on-disk schema this binary writes and understands. A file
// carrying a newer version is never migrated downward.
const SchemaVersion = 3

// Bounds this package owns. Configuration parsing lives elsewhere; these are the
// defaults and limits the module itself enforces on already-resolved values.
const (
	// MaxLadderStep is the hard ceiling on any single backoff step. There is no
	// reset TTL and no multi-day clamp anywhere in this package: a provider's
	// own reset prose can never lengthen a suppression.
	MaxLadderStep = 6 * time.Hour

	DefaultProbeLeaseMinutes = 10
	MinProbeLeaseMinutes     = 1
	MaxProbeLeaseMinutes     = 120

	DefaultProbeClaimMaxMinutes = 30
	MinProbeClaimMaxMinutes     = 1
	MaxProbeClaimMaxMinutes     = 240

	// DefaultLockTimeout is the bounded wait for an identity lock. A caller that
	// cannot take the lock proceeds on a stale read and takes no claim.
	DefaultLockTimeout = 2 * time.Second

	// lockBreakAge is how old a lock file held by a dead pid must be before it
	// is broken.
	lockBreakAge = 60 * time.Second

	// StaleRecordTTL drops a group record with no observation for this long, so
	// a stale escalation cannot follow the machine forever.
	StaleRecordTTL = 24 * time.Hour

	// EmptyIdentityTTL deletes an identity file that holds no group records and
	// has not been seen for this long.
	EmptyIdentityTTL = 30 * 24 * time.Hour

	// LostStateHorizon is how long after an identity's records were lost this
	// binary keeps treating that identity's state as indeterminate.
	//
	// It is MaxLadderStep because MaxLadderStep is the hard ceiling on a single
	// backoff step, and a suppression is armed at last_observation + step. Every
	// record that could have been in the lost file therefore had a
	// next_probe_at no later than lost_at + MaxLadderStep, so once this much
	// time has passed nothing the file could have held would still be
	// suppressing anything. It is emphatically NOT a claim that the groups are
	// available — it is a claim that they would be at worst probe-eligible, and
	// probe-eligible is what the degraded window already admits them as.
	LostStateHorizon = MaxLadderStep
)

// Sentinel errors. None of them is a reason to fail a spawn: limit state is an
// optimisation and must never be able to prevent a launch.
var (
	// ErrLeaseLost is returned by a lease-mutating call whose fence does not
	// match the stored lease. The call wrote nothing.
	ErrLeaseLost = errors.New("providerlimits: probe lease lost")
	// ErrNotProviderQuota is returned by Observe for an observation that does
	// not carry the provider-quota gate. Nothing was written.
	ErrNotProviderQuota = errors.New("providerlimits: observation is not provider-quota evidence")
	// ErrUnsupportedProvider is returned by Observe when the observation or the
	// identity names a provider this module cannot classify. Nothing was written.
	ErrUnsupportedProvider = errors.New("providerlimits: provider has no limit classifier and can never be suppressed")
	// ErrLockTimeout is returned by write paths that could not take the identity
	// lock within the bounded wait.
	ErrLockTimeout = errors.New("providerlimits: identity lock timeout")
	// ErrReadOnlyState is returned when the identity file carries a newer schema
	// version, so writing it would be a downgrade.
	ErrReadOnlyState = errors.New("providerlimits: identity state file is newer than this binary understands")
	// ErrStateWriteFailed is returned when the state could not be persisted. The
	// observation is lost, not fatal — but a claim that depended on the write is
	// refused, because an unpersisted lease would gate nobody.
	ErrStateWriteFailed = errors.New("providerlimits: identity state could not be written")
)

// GroupState is one group's position in the four-state machine.
//
// probe_eligible is emphatically NOT available: it is admitted to a claim holder
// only, and stays subtracted for every other caller. Collapsing the two is what
// lets every concurrent preflight admit an exhausted group at once.
type GroupState string

const (
	StateAvailable     GroupState = "available"
	StateSuppressed    GroupState = "suppressed"
	StateProbeEligible GroupState = "probe_eligible"
	StateProbing       GroupState = "probing"
)

// OwnerKind is who owns a probe lease. The owner genuinely changes identity
// mid-flight: before the runner is started only the spawning CLI process can
// resolve the probe, and after it is started that process routinely exits while
// the run continues.
type OwnerKind string

const (
	// OwnerProcess is the pre-launch owner: the spawning CLI process.
	OwnerProcess OwnerKind = "process"
	// OwnerRun is the post-launch owner: the started run.
	OwnerRun OwnerKind = "run"
)

// Lease is a held probe claim.
//
// The fence is (RunID, ClaimedAt). It is assigned once, when the claim is
// granted, and no later operation rewrites it: TransferProbe changes OwnerKind,
// ExpiresAt and the pid fields, renewal changes ExpiresAt and Renewals, and
// neither touches the fence.
type Lease struct {
	Group        string    `json:"group,omitempty"`
	Identity     string    `json:"identity,omitempty"`
	RunID        string    `json:"run_id"`
	OwnerKind    OwnerKind `json:"owner_kind"`
	PID          int       `json:"pid,omitempty"`
	PIDStartTime time.Time `json:"pid_start_time,omitzero"`
	// ClaimedAt is half of the fence and is never rewritten.
	ClaimedAt time.Time `json:"claimed_at"`
	// ExpiresAt is now + probe_lease_minutes at grant. It is renewable while the
	// owner is a live pre-launch process.
	ExpiresAt time.Time `json:"expires_at"`
	// ClaimDeadline is ClaimedAt + probe_claim_max_minutes and is NOT renewable.
	// ExpiresAt alone cannot bound a pre-launch owner, because a live process
	// would renew forever.
	ClaimDeadline time.Time `json:"claim_deadline"`
	Renewals      int       `json:"renewals"`
}

// Held reports whether this value is an actual lease rather than the zero value
// returned when a group needed no claim.
func (l Lease) Held() bool { return l.RunID != "" && !l.ClaimedAt.IsZero() }

// ResolveAt is the instant at which this lease must be re-resolved. It is the
// single definition of lease expiry: the resolver, the LeaseLive predicate and
// the report projection all read it, so none of them can disagree about whether
// a stored lease is still holding the gate.
//
// For a PROCESS owner the immutable ClaimDeadline DOMINATES the renewable
// ExpiresAt. Renewal may postpone the next resolver opportunity, but never past
// the deadline: a renewal taken shortly before it would otherwise hide the
// deadline behind a future expiry, and the wedged claimant AC 10 requires to be
// broken and fenced at claim_deadline would keep the group subtracted for up to
// one further lease interval. ExpiresAt is capped at grant and at every renewal,
// so the two normally coincide; this reads min() anyway, because a state file
// written by another binary on the same machine is not this binary's to trust.
//
// A run owner has no deadline — its bound is the run's own liveness — so its
// resolution instant is ExpiresAt alone.
func (l Lease) ResolveAt() time.Time {
	if l.OwnerKind == OwnerProcess && !l.ClaimDeadline.IsZero() && l.ClaimDeadline.Before(l.ExpiresAt) {
		return l.ClaimDeadline
	}
	return l.ExpiresAt
}

// fenceMatches compares the fence a caller believes it holds against the stored
// lease. RunID alone would be insufficient: one spawn can legitimately abandon a
// group and re-claim it later, and must not be mistaken for its own stale self.
func fenceMatches(stored *Lease, held Lease) bool {
	return stored != nil && stored.RunID == held.RunID && stored.ClaimedAt.Equal(held.ClaimedAt)
}

// Claimant identifies who is asking for a probe claim.
type Claimant struct {
	RunID        string
	OwnerKind    OwnerKind
	PID          int
	PIDStartTime time.Time
}

// ProcessClaimant is the pre-launch claimant: the spawning CLI process. It is
// deliberately not a child pid and not the runner pid, because neither exists
// when the claim is taken.
//
// A non-nil error means this process's own start time could not be read. The
// claimant is still usable; its lease is simply resolvable as dead earlier,
// which costs at most one extra probe.
func ProcessClaimant(runID string) (Claimant, error) {
	pid := os.Getpid()
	claimant := Claimant{RunID: runID, OwnerKind: OwnerProcess, PID: pid}
	start, err := ProcessStartTime(pid)
	if err != nil {
		return claimant, err
	}
	claimant.PIDStartTime = start
	return claimant, nil
}

// GroupRecord is one group's persisted state.
type GroupRecord struct {
	Provider                     string     `json:"provider"`
	State                        GroupState `json:"state"`
	BackoffStep                  int        `json:"backoff_step"`
	SuppressedSince              *time.Time `json:"suppressed_since,omitempty"`
	NextProbeAt                  *time.Time `json:"next_probe_at,omitempty"`
	ConsecutiveLimitObservations int        `json:"consecutive_limit_observations"`
	LastSuccessAt                *time.Time `json:"last_success_at,omitempty"`
	// LastObservationAt anchors the 24h stale-record garbage collection.
	LastObservationAt *time.Time `json:"last_observation_at,omitempty"`
	// ProbeProvisionalSince records that availability came from a launched run
	// confirmed alive rather than from a child exiting 0. Nothing about it is
	// sticky: a later quota observation re-suppresses at the next step.
	ProbeProvisionalSince *time.Time `json:"probe_provisional_since,omitempty"`
	ProbeAbandoned        int        `json:"probe_abandoned,omitempty"`
	ClearedAt             *time.Time `json:"cleared_at,omitempty"`
	ClearedBy             string     `json:"cleared_by,omitempty"`
	// AgedOutAt records that the 24h stale sweep reset this group's escalation.
	// It is diagnostic: the record's state and step already carry the effect.
	AgedOutAt         *time.Time `json:"aged_out_at,omitempty"`
	Evidence          *Evidence  `json:"evidence,omitempty"`
	ProviderResetHint *ResetHint `json:"provider_reset_hint,omitempty"`
	ProbeLease        *Lease     `json:"probe_lease"`
}

// EffectiveState is the state a reader should act on, without writing anything.
// A suppressed record whose backoff step has elapsed reads probe_eligible; a
// probing record reads probing regardless of lease expiry, because resolving a
// stale lease is a write and reads never write.
func (r GroupRecord) EffectiveState(now time.Time) GroupState {
	switch r.State {
	case StateSuppressed:
		if r.NextProbeAt != nil && !now.Before(*r.NextProbeAt) {
			return StateProbeEligible
		}
		return StateSuppressed
	case "":
		return StateAvailable
	default:
		return r.State
	}
}

// LeaseLive reports whether a stored lease has not yet reached its resolution
// instant, which for a pre-launch process owner is bounded by the claim deadline.
func (r GroupRecord) LeaseLive(now time.Time) bool {
	return r.ProbeLease != nil && now.Before(r.ProbeLease.ResolveAt())
}

type identityStateFile struct {
	Version     int                     `json:"version"`
	Identity    string                  `json:"identity"`
	Provider    string                  `json:"provider"`
	HomeDisplay string                  `json:"home_display"`
	Groups      map[string]*GroupRecord `json:"groups"`
	// DegradedSince is the instant this machine LOST the records that used to
	// be in this file, because the file could not be read or did not parse. It
	// is the tombstone that keeps a quarantine from laundering an unreadable
	// file into a proven absence: without it the very next read finds no file
	// at all and reports StateReadAbsent, which is a statement about the
	// provider that nobody ever established.
	//
	// It is dropped by the first write that happens after LostStateHorizon.
	DegradedSince *time.Time `json:"degraded_since,omitempty"`
}

// IdentityState is a read-only projection of one identity's state file.
type IdentityState struct {
	Identity Identity
	Version  int
	Groups   map[string]GroupRecord
	// ReadOnly is true when the file carries a newer schema version, so this
	// binary treats it as empty and refuses to write it.
	ReadOnly bool
	// Read is why Groups is what it is. An empty Groups map under
	// StateReadAbsent is a proven absence; under any indeterminate read it is
	// this binary's fail-open substitute for a state it never established.
	Read StateRead
}

// OwnerLiveness answers whether a lease's owner is still alive.
//
// The module ships the process arm (a local pid plus start-time check) and uses
// it for OwnerProcess. The run arm is injected, which is what keeps this package
// free of a dependency on the run store.
//
// known=false means "no answer available". It is a run-owner condition only: a
// local pid is always determinable, and an unreadable start time is reported as
// not alive rather than unknown, because reuse cannot be ruled out.
type OwnerLiveness interface {
	Alive(lease Lease) (alive bool, known bool)
}

// Options configures a Store. Every duration and bound here is an
// already-resolved value; this package parses no configuration.
type Options struct {
	Layout Layout
	// Ladder is the backoff ladder. Nil means DefaultLadder. Every step must be
	// positive, the ladder must be non-decreasing, and no step may exceed
	// MaxLadderStep. The last step repeats.
	Ladder []time.Duration
	// ProbeLeaseMinutes is how long a lease survives without being reconfirmed.
	// Zero means DefaultProbeLeaseMinutes; the value is clamped to
	// [MinProbeLeaseMinutes, MaxProbeLeaseMinutes].
	ProbeLeaseMinutes int
	// ProbeClaimMaxMinutes bounds the whole pre-launch phase. Zero means
	// DefaultProbeClaimMaxMinutes; the value is clamped to
	// [MinProbeClaimMaxMinutes, MaxProbeClaimMaxMinutes] and raised to
	// ProbeLeaseMinutes if it would otherwise be smaller.
	ProbeClaimMaxMinutes int
	// RunLiveness answers liveness for OwnerRun leases. Nil means every run
	// lease resolves as known=false, which is claim-gated rather than available.
	RunLiveness OwnerLiveness
	// ProcessLiveness overrides the shipped process arm. Tests use this; production
	// callers should leave it nil.
	ProcessLiveness OwnerLiveness
	// Now overrides the clock. Nil means time.Now.
	Now func() time.Time
	// Warn receives operator-facing warnings. Nil discards them.
	Warn func(string)
	// LockTimeout is the bounded wait for an identity lock. Zero means
	// DefaultLockTimeout.
	LockTimeout time.Duration
}

// Store is the per-machine limit-state store.
type Store struct {
	layout          Layout
	ladder          []time.Duration
	leaseMinutes    int
	claimMaxMinutes int
	runLiveness     OwnerLiveness
	procLiveness    OwnerLiveness
	nowFn           func() time.Time
	warnFn          func(string)
	lockTimeout     time.Duration

	// writeState persists one identity file. It is a field rather than a direct
	// call so a test can inject the write failure the fail-open contract has to
	// survive: a filesystem that refuses the rename while the lock still works is
	// not reproducible with directory permissions alone.
	writeState func(path string, payload any) error

	// beforeCollectLock runs between nominating an identity for collection and
	// taking that identity's lock. It exists for the same reason writeState does:
	// the window the unlocked scan opens is real but not otherwise reachable from
	// a deterministic test, and a gate no test can attack is an unguarded one.
	// Production leaves it nil.
	beforeCollectLock func(key string)

	identityGCOnce sync.Once
}

// DefaultLadder is the shipped backoff ladder. The last step repeats.
func DefaultLadder() []time.Duration {
	return []time.Duration{
		2 * time.Minute,
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		60 * time.Minute,
		120 * time.Minute,
	}
}

// ValidateLadder enforces the ladder contract: non-empty, every step positive,
// non-decreasing, and no step above MaxLadderStep. A step above the maximum is
// rejected rather than silently used.
func ValidateLadder(ladder []time.Duration) error {
	if len(ladder) == 0 {
		return fmt.Errorf("providerlimits: backoff ladder must have at least one step")
	}
	for i, step := range ladder {
		if step <= 0 {
			return fmt.Errorf("providerlimits: backoff ladder step %d is %s; every step must be a positive duration", i+1, step)
		}
		if step > MaxLadderStep {
			return fmt.Errorf("providerlimits: backoff ladder step %d is %s, above the %s maximum", i+1, step, MaxLadderStep)
		}
		if i > 0 && step < ladder[i-1] {
			return fmt.Errorf("providerlimits: backoff ladder step %d (%s) is shorter than step %d (%s); the ladder must be non-decreasing", i+1, step, i, ladder[i-1])
		}
	}
	return nil
}

// ClampLadder caps every step at MaxLadderStep. It is offered to the caller that
// resolves configuration; the resulting ladder still has to satisfy
// ValidateLadder, which a clamp cannot guarantee on its own.
func ClampLadder(ladder []time.Duration) []time.Duration {
	out := make([]time.Duration, len(ladder))
	for i, step := range ladder {
		if step > MaxLadderStep {
			step = MaxLadderStep
		}
		out[i] = step
	}
	return out
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// NewStore validates the resolved options and returns a store.
func NewStore(opts Options) (*Store, error) {
	if opts.Layout.Root == "" {
		return nil, fmt.Errorf("providerlimits: layout root is required")
	}
	ladder := opts.Ladder
	if len(ladder) == 0 {
		ladder = DefaultLadder()
	}
	if err := ValidateLadder(ladder); err != nil {
		return nil, err
	}
	leaseMinutes := opts.ProbeLeaseMinutes
	if leaseMinutes == 0 {
		leaseMinutes = DefaultProbeLeaseMinutes
	}
	leaseMinutes = clampInt(leaseMinutes, MinProbeLeaseMinutes, MaxProbeLeaseMinutes)

	claimMax := opts.ProbeClaimMaxMinutes
	if claimMax == 0 {
		claimMax = DefaultProbeClaimMaxMinutes
	}
	claimMax = clampInt(claimMax, MinProbeClaimMaxMinutes, MaxProbeClaimMaxMinutes)

	store := &Store{
		layout:          opts.Layout,
		ladder:          append([]time.Duration(nil), ladder...),
		leaseMinutes:    leaseMinutes,
		claimMaxMinutes: claimMax,
		runLiveness:     opts.RunLiveness,
		procLiveness:    opts.ProcessLiveness,
		nowFn:           opts.Now,
		warnFn:          opts.Warn,
		lockTimeout:     opts.LockTimeout,
		writeState:      writeJSONAtomic,
	}
	if store.procLiveness == nil {
		store.procLiveness = ProcessLivenessChecker{}
	}
	if store.lockTimeout <= 0 {
		store.lockTimeout = DefaultLockTimeout
	}
	if claimMax < leaseMinutes {
		store.warnf("provider-limits probe_claim_max_minutes (%d) is below probe_lease_minutes (%d); raising it to %d", claimMax, leaseMinutes, leaseMinutes)
		store.claimMaxMinutes = leaseMinutes
	}
	return store, nil
}

// Layout returns the store's state layout.
func (s *Store) Layout() Layout { return s.layout }

// Ladder returns the effective backoff ladder.
func (s *Store) Ladder() []time.Duration { return append([]time.Duration(nil), s.ladder...) }

// ProbeLeaseMinutes returns the effective lease duration in minutes.
func (s *Store) ProbeLeaseMinutes() int { return s.leaseMinutes }

// ProbeClaimMaxMinutes returns the effective pre-launch claim bound in minutes.
func (s *Store) ProbeClaimMaxMinutes() int { return s.claimMaxMinutes }

func (s *Store) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

func (s *Store) warnf(format string, args ...any) {
	if s.warnFn == nil {
		return
	}
	s.warnFn(fmt.Sprintf(format, args...))
}

// ladderStep returns the ladder duration for step k. The last step repeats.
func (s *Store) ladderStep(k int) time.Duration {
	if k < 0 {
		k = 0
	}
	if k >= len(s.ladder) {
		k = len(s.ladder) - 1
	}
	return s.ladder[k]
}

func (s *Store) escalatedStep(k int) int {
	if k+1 >= len(s.ladder) {
		return len(s.ladder) - 1
	}
	return k + 1
}

// --- locking ----------------------------------------------------------------

type lockInfo struct {
	PID          int       `json:"pid"`
	PIDStartTime time.Time `json:"pid_start_time,omitzero"`
	AcquiredAt   time.Time `json:"acquired_at"`
}

// withLock runs fn while holding an O_EXCL lock file. A lock whose pid is dead
// and whose mtime is older than lockBreakAge is broken and re-acquired with a
// warning. On timeout the lock is not taken and ErrLockTimeout is returned; the
// caller must treat that as "skip the write", never as a failure.
func (s *Store) withLock(path string, fn func() error) error {
	return withFileLock(path, s.lockTimeout, s.now, s.warnf, fn)
}

// withFileLock is the ONE lock implementation in this package.
//
// It is package-level rather than a Store method because the spread cursor is
// machine-scoped and cross-provider, so it has no Store to hang off; a second
// lock loop written for it would be a second place for the break-a-stale-lock
// rule and the bounded-wait rule to drift.
func withFileLock(
	path string,
	timeout time.Duration,
	now func() time.Time,
	warn func(format string, args ...any),
	fn func() error,
) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("providerlimits: creating state directory: %w", err)
	}
	if timeout <= 0 {
		timeout = DefaultLockTimeout
	}
	if now == nil {
		now = time.Now
	}
	if warn == nil {
		warn = func(string, ...any) {}
	}
	// The bounded wait is measured on the wall clock, not on the injectable
	// store clock: a frozen test clock must not turn a bounded wait into a hang.
	deadline := time.Now().Add(timeout)
	attempted := false
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			info := lockInfo{PID: os.Getpid(), AcquiredAt: now()}
			if start, startErr := ProcessStartTime(info.PID); startErr == nil {
				info.PIDStartTime = start
			}
			if payload, marshalErr := json.Marshal(info); marshalErr == nil {
				_, _ = file.Write(payload)
			}
			_ = file.Close()
			defer func() { _ = os.Remove(path) }()
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("providerlimits: acquiring lock %s: %w", path, err)
		}
		if !attempted {
			attempted = true
			if breakStaleLock(path, warn) {
				continue
			}
		}
		if !time.Now().Before(deadline) {
			return ErrLockTimeout
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (s *Store) breakStaleLock(path string) bool { return breakStaleLock(path, s.warnf) }

// breakStaleLock removes a lock whose holder is gone. Both conditions are
// required: the pid must be dead and the file must be older than lockBreakAge,
// so a live holder that has not yet written its metadata is never displaced.
func breakStaleLock(path string, warn func(format string, args ...any)) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) < lockBreakAge {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var held lockInfo
	if err := json.Unmarshal(data, &held); err != nil || held.PID <= 0 {
		warn("provider-limits lock %s is older than %s and carries no readable holder; breaking it", path, lockBreakAge)
		return os.Remove(path) == nil
	}
	if !lockHolderGone(held) {
		return false
	}
	warn("provider-limits lock %s was held by dead pid %d and is older than %s; breaking it", path, held.PID, lockBreakAge)
	return os.Remove(path) == nil
}

// --- state file IO ----------------------------------------------------------

// StateRead says how much of an identity's state file a read actually
// established.
//
// An absence and a failure to read are different facts. The two used to be
// collapsed here — every arm returned an empty state and the caller could not
// tell which arm it came from — and an empty group map renders as "nothing is
// suppressed and nothing is leased", which is a claim about the provider that a
// failed read never established.
type StateRead string

const (
	// StateReadOK marks a file that was read and parsed.
	StateReadOK StateRead = "ok"
	// StateReadAbsent marks a machine that has never recorded state for this
	// identity. This is a PROVEN absence: nothing was ever suppressed.
	StateReadAbsent StateRead = "absent"
	// StateReadUnreadable marks a file that exists and could not be read.
	StateReadUnreadable StateRead = "unreadable"
	// StateReadCorrupt marks a file that was read and did not parse.
	StateReadCorrupt StateRead = "corrupt"
	// StateReadSchemaAhead marks a file this binary refuses to interpret.
	StateReadSchemaAhead StateRead = "schema_ahead"
	// StateReadDegraded marks a file that parses but carries a degraded_since
	// tombstone inside LostStateHorizon: the records it used to hold were lost
	// to a quarantine and have not yet aged past the point where they could
	// still have been gating something. Whatever groups it DOES carry were
	// written after the loss and are proven; the absence of a group is not.
	StateReadDegraded StateRead = "degraded"
)

// Indeterminate reports whether the read failed to establish state. Only a
// successful read and a proven absence are determinate; everything else is a
// read this binary could not complete, and the difference must survive to the
// operator.
func (r StateRead) Indeterminate() bool {
	switch r {
	case StateReadOK, StateReadAbsent:
		return false
	default:
		return true
	}
}

// --- what an indeterminate read authorizes ----------------------------------
//
// THE ANSWER, in one line: an indeterminate read authorizes every write that can
// only ever subtract availability, and it authorizes no statement that a group
// is available, and no probe claim until a lease written before the loss could
// no longer be live.
//
// Spelled out, because the next reader will look for it here and nowhere else.
//
// It AUTHORIZES:
//
//   - Observe, ObserveSuccess, ClearGroup, AbandonProbe and TransferProbe. Every
//     one of them is either monotone toward suppression, fenced against a lease
//     it cannot see, or an explicit operator instruction. None of them can
//     invent availability out of a state nobody read, so none of them needs the
//     lost records to be correct. A machine that cannot read its own limit state
//     must still be able to RECORD what it just learned; refusing that would
//     throw away the only evidence anyone has.
//
//     ObserveSuccess needs its own reason, because it is the one operation in
//     that list that CLEARS a group, and clearing IS granting availability. The
//     reason is structural rather than one of the three above: it returns early
//     when the group has no record, and the tombstone a quarantine leaves behind
//     carries an EMPTY group map — so every record ObserveSuccess can still see
//     was written AFTER the loss, by this binary, and is proven. It cannot clear
//     a suppression it never read.
//   - Launching into a provider that has no classifier at all. Nothing can ever
//     suppress such a provider, so its availability never depended on this file.
//     That subset is fail-open and stays fail-open (see below).
//
// It REFUSES:
//
//   - Any probe claim, until LostStateHorizon's shorter sibling — the store's own
//     probe_claim_max_minutes — has elapsed since the loss. Inside that window a
//     lease written before the loss can still be live in another run, and
//     granting one would be exactly the herd the atomic claim exists to prevent.
//     See lostStateClaimAllowed.
//   - Reading a group with no record as AVAILABLE. Under a determinate read a
//     missing record IS availability; under an indeterminate one it is only the
//     shape of the hole the read left. ClaimProbe materialises such a group as
//     probe_eligible instead, so it is admitted to exactly one caller and stays
//     subtracted for everyone else — the same answer the 24h stale sweep gives a
//     record whose escalation it forgets. The read surfaces carry StateRead and
//     Indeterminate for the same reason, and the spawn partition turns them into
//     GATED rather than FREE.
//
// FAIL-OPEN DELIBERATELY RETAINED, and why it is safe there:
//
//   - StateReadSchemaAhead. The file is INTACT; a newer build of this same tool
//     on this same machine wrote it and is enforcing the gate correctly. This
//     binary refuses to write it, so it can never record a lease and can never
//     quarantine it — there is no time horizon that recovers, and treating it as
//     unclaimable would brick the identity permanently until an operator deleted
//     a perfectly good file. The recovery is to run the newer binary, which the
//     warning names. This is the one arm where an indeterminate read still
//     grants an unleased claim.
//   - Runtimes whose broker has no classifier (HasClassifierForRuntime ==
//     false). They can never be suppressed, so no state file ever gated them,
//     so a lost file lost nothing about them.
//   - Everything outside the limit plane. Limit state is an optimisation over
//     relaunching blindly; nothing here may turn a failed read into a refusal to
//     do work that never consulted it.
//
// WHY NOT SIMPLY FAIL CLOSED. A machine whose state file is corrupt and which
// then refuses every launch is bricked by a file it could have rewritten. So the
// loss is TIME-BOUNDED rather than permanent, and it is bounded by the two
// quantities the module already owns: probe_claim_max_minutes, after which no
// lost lease can still be live, and LostStateHorizon, after which no lost
// suppression can still be arming. Recovery is by the clock alone. It cannot be
// triggered by writing to the file, by racing the quarantine, or by corrupting
// it again — corrupting the file only restarts the blackout, which is strictly
// worse for whoever did it than leaving the file alone.
//
// THE ONE ARM THAT IS NOT CLOCK-RECOVERABLE, and it is deliberate: if the
// tombstone carrying the loss instant could not be WRITTEN, there is no recorded
// bound to wait out, and the refusal stands until a human makes the state
// directory writable or deletes the file. Waiting is the one thing that cannot
// help there, so neither the store warning nor the selection refusal may promise
// a self-heal on that arm — see lostStateClaimAllowed and quarantine.

// loadIdentityFile reads one identity's state.
//
// The returned file is empty on every arm except StateReadOK and
// StateReadDegraded; the second result is why, and callers that make an
// authorization decision MUST consult it rather than reading the empty map as an
// absence.
//
// quarantine is true only on the write path. A read never moves the file,
// because reads never write.
func (s *Store) loadIdentityFile(identity Identity, quarantine bool) (*identityStateFile, StateRead) {
	fresh := &identityStateFile{
		Version:     SchemaVersion,
		Identity:    identity.Key,
		Provider:    identity.Provider,
		HomeDisplay: identity.HomeDisplay,
		Groups:      map[string]*GroupRecord{},
	}
	path := s.layout.StateFile(identity.Key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fresh, StateReadAbsent
		}
		s.warnf("provider-limits state %s unreadable (%v); its records are LOST, not absent", path, err)
		if quarantine {
			s.quarantine(path, err, fresh)
		}
		return fresh, StateReadUnreadable
	}
	var file identityStateFile
	if err := json.Unmarshal(data, &file); err != nil {
		if quarantine {
			s.quarantine(path, err, fresh)
		} else {
			s.warnf("provider-limits state %s is corrupt (%v); its records are LOST, not absent", path, err)
		}
		return fresh, StateReadCorrupt
	}
	if file.Version > SchemaVersion {
		s.warnf("provider-limits state %s has schema version %d, newer than this binary understands (%d); ignoring it and refusing to write it", path, file.Version, SchemaVersion)
		fresh.Version = file.Version
		return fresh, StateReadSchemaAhead
	}
	if file.Groups == nil {
		file.Groups = map[string]*GroupRecord{}
	}
	file.Version = SchemaVersion
	file.Identity = identity.Key
	if identity.Provider != "" {
		file.Provider = identity.Provider
	}
	if identity.HomeDisplay != "" {
		file.HomeDisplay = identity.HomeDisplay
	}
	if read := s.degradedRead(&file); read != StateReadOK {
		return &file, read
	}
	return &file, StateReadOK
}

// degradedRead applies the tombstone left by a quarantine.
//
// A degraded_since older than LostStateHorizon is CLEARED in memory, so the next
// write drops it from disk: past that horizon nothing the lost file could have
// held would still be arming a suppression, and keeping the marker would gate
// the identity forever.
//
// A stamp in the FUTURE — clock skew, or a hand-edited file — is read as "the
// loss just happened", which keeps the identity degraded rather than releasing
// it early. That is the conservative direction and it grants nobody anything;
// mutate normalises such a stamp on the next write so the window becomes
// measurable again. Nothing defends against a stamp written far in the PAST,
// and nothing needs to: whoever can write that stamp can write an empty state
// file instead and get the same result with less effort.
func (s *Store) degradedRead(file *identityStateFile) StateRead {
	if file.DegradedSince == nil {
		return StateReadOK
	}
	now := s.now()
	since := *file.DegradedSince
	if since.After(now) {
		since = now
	}
	if now.Sub(since) >= LostStateHorizon {
		file.DegradedSince = nil
		return StateReadOK
	}
	return StateReadDegraded
}

// lostStateClaimAllowed reports whether enough time has passed since this
// identity's records were lost that no lease written BEFORE the loss can still
// be live.
//
// The bound is the store's own probe_claim_max_minutes, which is the ceiling
// this store puts on the whole pre-launch phase of any lease it grants: a lease
// claimed at or before the loss has a claim deadline no later than
// lost_at + probe_claim_max_minutes, and a run-owned lease past its expiry is
// resolvable by anyone. The assumption this makes explicit is that the process
// that wrote the lost file ran with the same configured bound; a machine that
// changed probe_claim_max_minutes downward between runs can, in the interval
// between the two bounds, admit one duplicate probe. That is the whole residual
// risk, and it is one extra launch rather than a herd.
//
// A loss that could not be RECORDED — no tombstone reached the disk — is
// refused permanently, and the route that refuses it is this same elapsed-time
// comparison, not the nil guard below.
//
// The mechanism is a RESTAMP, and it is the load-bearing invariant of that arm.
// When the disk holds no tombstone, DegradedSince is written by the LOAD rather
// than read off the file: quarantine stamps fresh as its FIRST statement,
// before the rename and before the tombstone write, so both of its failure arms
// hand back a stamped file; and mutate stamps any indeterminate read that still
// arrives unstamped, before fn runs. So an unrecordable loss carries this
// load's now on EVERY load and never ages — since == now, the last line
// measures zero elapsed, and zero is never >= claim_max. Wait a month and the
// next load stamps that month's now. That is precisely why waiting cannot help,
// and why neither operator-facing surface may name a window on that arm, while
// a DEGRADED read — whose stamp came off the disk and therefore does age — is
// told to wait, because for it waiting works.
//
// The file == nil || DegradedSince == nil guard below is DEFENSIVE and
// unreachable from the only caller. ClaimProbe reaches here only under
// read.Indeterminate(); the two stampers above cover every such read; and
// schema_ahead, the one indeterminate read nobody stamps, is refused earlier by
// mutate with ErrReadOnlyState and never arrives. Flipping it to return true
// leaves the whole suite green, so do not read it as the thing that refuses and
// do not treat its survival as licence to change the bound. What is worth
// attacking is the comparison on the last line: narrowing it to admit a
// zero-elapsed loss fails eight tests, one of which —
// TestAnUnrecordableLossIsRefusedByTheBlackoutAloneWhileWritesStillWork — pins
// the permanence on a fixture where the write path is demonstrably live, so
// grantNeedsWrite cannot be the refuser there.
func (s *Store) lostStateClaimAllowed(file *identityStateFile, now time.Time) bool {
	// Defensive and unreachable from ClaimProbe — see the restamp paragraph
	// above. It is not what refuses an unrecordable loss, and no test can fail
	// from it; the refusal is the elapsed-time comparison at the bottom.
	if file == nil || file.DegradedSince == nil {
		return false
	}
	since := *file.DegradedSince
	if since.After(now) {
		since = now
	}
	return now.Sub(since) >= time.Duration(s.claimMaxMinutes)*time.Minute
}

// quarantine moves a state file this binary cannot interpret out of the way and
// replaces it with a TOMBSTONE recording the instant the knowledge was lost.
//
// The tombstone is the whole point. A bare rename would leave no file at all,
// and the very next read would report StateReadAbsent — a proven absence — for a
// machine that in fact just lost every suppression and every lease it had. That
// is the failed-read-reported-as-absence shape this package refuses everywhere
// else, performed by the recovery path itself.
//
// If the tombstone cannot be written, the quarantined file is moved BACK, so the
// identity keeps reading as unreadable/corrupt rather than as absent. Failing to
// recover must never be more permissive than not trying.
//
// fresh is the empty state the caller will proceed on; it is stamped so that any
// write derived from this load carries the tombstone forward.
//
// One window is knowingly left open. LoadIdentityState takes no lock, so a
// reader in another process that lands between the rename and the tombstone
// write finds no file and reports StateReadAbsent — a proven absence — for the
// microseconds during which the pair is not atomic. Closing it means taking the
// identity lock on a read path documented to never write, which is a larger
// change than the exposure justifies: the window is bounded by two adjacent
// syscalls, whereas the absence this quarantine exists to prevent was permanent.
func (s *Store) quarantine(path string, cause error, fresh *identityStateFile) {
	now := s.now()
	fresh.DegradedSince = timePtr(now)
	target := fmt.Sprintf("%s.corrupt-%s", path, now.UTC().Format("20060102T150405Z"))
	if err := os.Rename(path, target); err != nil {
		s.warnf("provider-limits state %s could not be read (%v) and could not be quarantined (%v); no probe may be claimed for this identity, and this does NOT clear by waiting because the instant of the loss could not be recorded: make the state directory writable, or delete %s", path, cause, err, path)
		return
	}
	tombstone := *fresh
	tombstone.Groups = map[string]*GroupRecord{}
	if err := s.writeState(path, &tombstone); err != nil {
		if restoreErr := os.Rename(target, path); restoreErr != nil {
			s.warnf("provider-limits state %s was quarantined as %s, its tombstone could not be written (%v) and it could not be restored (%v); this identity will read as ABSENT on the next load", path, target, err, restoreErr)
			return
		}
		s.warnf("provider-limits state %s is unusable (%v) and its tombstone could not be written (%v); the file was restored so the loss is not mistaken for an absence, and no probe may be claimed for this identity — this does NOT clear by waiting because the instant of the loss could not be recorded: make the state directory writable, or delete %s", path, cause, err, path)
		return
	}
	s.warnf("provider-limits state %s was unusable (%v); quarantined as %s and its records recorded as LOST at %s, so no probe may be claimed for this identity for %s", path, cause, target, now.UTC().Format(time.RFC3339), time.Duration(s.claimMaxMinutes)*time.Minute)
}

func writeJSONAtomic(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("providerlimits: encoding %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("providerlimits: creating state directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("providerlimits: creating temporary file for %s: %w", path, err)
	}
	tempName := temp.Name()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)
		return fmt.Errorf("providerlimits: writing %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("providerlimits: closing %s: %w", path, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("providerlimits: replacing %s: %w", path, err)
	}
	return nil
}

// mutate runs fn against one identity's state under that identity's lock, and
// persists the result when fn reports a change.
//
// A lock timeout is reported as ErrLockTimeout and nothing is written; a
// newer-schema file is reported as ErrReadOnlyState and nothing is written. Both
// are fail-open conditions the caller warns about rather than failing on.
//
// fn receives the StateRead alongside the file, because the file alone cannot
// tell it whether an empty group map is a proven absence or a hole. A mutation
// that makes an authorization decision must consult it; one that only records
// what the caller just learned does not have to.
func (s *Store) mutate(identity Identity, fn func(file *identityStateFile, read StateRead) (bool, error)) error {
	var (
		inner   error
		touched bool
	)
	lockErr := s.withLock(s.layout.LockFile(identity.Key), func() error {
		file, read := s.loadIdentityFile(identity, true)
		if file.Version > SchemaVersion {
			inner = ErrReadOnlyState
			return nil
		}
		// An indeterminate read TAINTS every write derived from it. Without this
		// a single Observe over an unreadable file would write a clean,
		// parseable state holding exactly one group, and the next reader would
		// take that file at face value: one suppression proven, everything else
		// proven available. The stamp survives so the hole stays visible.
		switch {
		case read.Indeterminate() && file.DegradedSince == nil:
			file.DegradedSince = timePtr(s.now())
		case file.DegradedSince != nil && file.DegradedSince.After(s.now()):
			// A tombstone this binary could not have written. It is anchored to
			// now so the blackout it implies is bounded and measurable instead
			// of receding ahead of every read.
			file.DegradedSince = timePtr(s.now())
		}
		// The sweep runs BEFORE the closure, so a record that is stale on entry
		// is aged out before a fresh lease can rescue it from being swept.
		swept := s.sweepStaleRecords(file, s.now())
		changed, err := fn(file, read)
		inner = err
		if !changed && !swept {
			return nil
		}
		if writeErr := s.writeState(s.layout.StateFile(identity.Key), file); writeErr != nil {
			s.warnf("provider-limits state for identity %s not written: %v", identity.Key, writeErr)
			inner = ErrStateWriteFailed
			return nil
		}
		touched = true
		return nil
	})
	if lockErr != nil {
		if errors.Is(lockErr, ErrLockTimeout) {
			s.warnf("provider-limits state for identity %s not written: lock busy for more than %s; proceeding without the write", identity.Key, s.lockTimeout)
			return ErrLockTimeout
		}
		s.warnf("provider-limits state for identity %s not written: %v", identity.Key, lockErr)
		return ErrStateWriteFailed
	}
	if touched {
		s.touchIndex(identity)
		s.collectIdentitiesOnce()
	}
	return inner
}

// LoadIdentityState reads one identity's state without taking a lock and without
// writing anything. Writes are atomic renames, so a reader observes either the
// old or the new file, never a torn one.
func (s *Store) LoadIdentityState(identity Identity) IdentityState {
	file, read := s.loadIdentityFile(identity, false)
	groups := make(map[string]GroupRecord, len(file.Groups))
	for name, record := range file.Groups {
		if record == nil {
			continue
		}
		groups[name] = *record
	}
	return IdentityState{
		Identity: identity,
		Version:  file.Version,
		Groups:   groups,
		ReadOnly: file.Version > SchemaVersion,
		Read:     read,
	}
}

// GroupRecordFor reads one group's record. The second result is false when no
// record exists, which this state machine reads as available.
func (s *Store) GroupRecordFor(identity Identity, group string) (GroupRecord, bool) {
	state := s.LoadIdentityState(identity)
	record, ok := state.Groups[group]
	return record, ok
}

// --- garbage collection -----------------------------------------------------

// staleAt reports whether a group has gone StaleRecordTTL without an
// observation. The anchor is the last observation, or the moment suppression
// began when nothing has been observed since.
func (r GroupRecord) staleAt(now time.Time) bool {
	anchor := r.LastObservationAt
	if anchor == nil {
		anchor = r.SuppressedSince
	}
	if anchor == nil {
		return false
	}
	return now.Sub(*anchor) > StaleRecordTTL
}

// neutralGate reports whether a record is already in the aged-out shape: a
// claim gate carrying no escalation and no evidence. Ageing such a record out
// again would be a write with no effect, so the sweep leaves it alone.
func (r GroupRecord) neutralGate() bool {
	return r.State == StateProbeEligible &&
		r.BackoffStep == 0 &&
		r.ConsecutiveLimitObservations == 0 &&
		r.Evidence == nil &&
		r.ProviderResetHint == nil &&
		r.ProbeLease == nil
}

// sweepStaleRecords ages out group records with no observation for
// StaleRecordTTL. It runs on ENTRY to every mutation, inside the identity lock
// and before the mutation closure runs, because a sweep that runs afterwards is
// no sweep at all for the one caller that matters: ClaimProbe converts the
// record to probing and attaches a fresh live lease, and a live lease is exactly
// what a sweep must not touch. The stale escalation then rides out on that lease
// and the first probe after 24 h re-uses the old max step forever.
//
// Two outcomes, split on whether the record still gates anything:
//
//   - A record that gates nothing — available, whether from a success or from a
//     provisional probe — is DELETED. Nothing is lost: a missing record already
//     reads as available.
//   - A record that gates the group — suppressed, or probe_eligible carrying an
//     escalation — is RESET to probe_eligible(step 0) rather than deleted, for
//     the D16 reason. Deleting it would read as available and readmit every
//     concurrent spawn at once, which is the herd the atomic claim exists to
//     prevent. Ageing out must forget the escalation, not assert availability:
//     exactly one next spawn probes, at step 0, and only its exit 0 clears the
//     group.
//
// A record holding a live lease is never swept: somebody is probing it, and
// resolving that lease belongs to resolveStaleLease alone.
func (s *Store) sweepStaleRecords(file *identityStateFile, now time.Time) bool {
	changed := false
	for name, record := range file.Groups {
		if record == nil {
			delete(file.Groups, name)
			changed = true
			continue
		}
		if record.ProbeLease != nil {
			continue
		}
		if !record.staleAt(now) {
			continue
		}
		if record.EffectiveState(now) == StateAvailable {
			delete(file.Groups, name)
			changed = true
			continue
		}
		if s.ageOutRecord(name, record, now) {
			changed = true
		}
	}
	return changed
}

// ageOutRecord forgets one stale group's escalation without asserting
// availability: probe_eligible(step 0), probeable now, evidence and reset hint
// cleared. It returns false when the record already carries nothing to forget.
func (s *Store) ageOutRecord(group string, record *GroupRecord, now time.Time) bool {
	if record.neutralGate() {
		return false
	}
	s.warnf("provider-limits group %s has had no observation for %s; its backoff is aged out from step %d to a claim-gated step 0", group, StaleRecordTTL, record.BackoffStep)
	record.State = StateProbeEligible
	record.BackoffStep = 0
	record.ConsecutiveLimitObservations = 0
	record.Evidence = nil
	record.ProviderResetHint = nil
	record.ProbeProvisionalSince = nil
	record.ProbeLease = nil
	// The anchor is re-based so an aged-out record is not re-swept on every
	// following write, and suppressed_since keeps next_probe_at >= it.
	record.SuppressedSince = timePtr(now)
	record.LastObservationAt = nil
	record.NextProbeAt = timePtr(now)
	record.AgedOutAt = timePtr(now)
	return true
}

func (s *Store) collectIdentitiesOnce() {
	s.identityGCOnce.Do(func() {
		if err := s.CollectIdentities(); err != nil {
			s.warnf("provider-limits identity garbage collection skipped: %v", err)
		}
	})
}

// CollectIdentities deletes identity files that hold no group records and have
// not been seen for EmptyIdentityTTL, together with their index entries. It is
// best-effort: a failure is a warning, never a spawn blocker.
//
// Deletion happens INSIDE that identity's own lock, and eligibility is re-read
// there rather than trusted from the unlocked scan. The scan only nominates
// candidates: between the scan and the delete, a concurrent writer can be
// halfway through populating exactly this file, and removing it then would
// silently discard state a lock was being held to protect.
//
// The lock this function takes is its own — acquired and released by withLock —
// and no foreign lock file is ever unlinked. Unlinking a live lock would let a
// third writer acquire a replacement and race the holder, which is the same lost
// write by a slower route.
func (s *Store) CollectIdentities() error {
	entries, err := os.ReadDir(s.layout.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	index := s.readIndex()
	removed := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !hasStateSuffix(entry.Name()) {
			continue
		}
		key := trimStateSuffix(entry.Name())
		if !s.collectibleIdentity(key, index) {
			continue
		}
		if s.beforeCollectLock != nil {
			s.beforeCollectLock(key)
		}
		lockErr := s.withLock(s.layout.LockFile(key), func() error {
			// Re-read under the lock. The unlocked scan above is a hint; this is
			// the decision.
			if !s.collectibleIdentity(key, s.readIndex()) {
				return nil
			}
			if err := os.Remove(s.layout.StateFile(key)); err != nil {
				if !os.IsNotExist(err) {
					s.warnf("provider-limits identity %s not collected: %v", key, err)
				}
				return nil
			}
			removed = append(removed, key)
			return nil
		})
		if lockErr != nil {
			// A busy or unusable lock means somebody is working on that identity.
			// Skipping it is always correct: garbage collection has no deadline.
			s.warnf("provider-limits identity %s not collected: %v", key, lockErr)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	return s.withLock(s.layout.IndexLockFile(), func() error {
		current := s.readIndex()
		for _, key := range removed {
			delete(current.Identities, key)
		}
		current.Version = SchemaVersion
		return writeJSONAtomic(s.layout.IndexFile(), current)
	})
}

// collectibleIdentity reports whether one identity file is empty and older than
// EmptyIdentityTTL. It reads and decides only; the caller owns the lock and the
// deletion.
func (s *Store) collectibleIdentity(key string, index *IdentityIndex) bool {
	path := s.layout.StateFile(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var file identityStateFile
	if err := json.Unmarshal(data, &file); err != nil {
		return false
	}
	if len(file.Groups) > 0 {
		return false
	}
	lastSeen := time.Time{}
	if seen, ok := index.Identities[key]; ok {
		lastSeen = seen.LastSeen
	}
	if lastSeen.IsZero() {
		if info, statErr := os.Stat(path); statErr == nil {
			lastSeen = info.ModTime()
		}
	}
	if lastSeen.IsZero() {
		return false
	}
	return s.now().Sub(lastSeen) > EmptyIdentityTTL
}

func hasStateSuffix(name string) bool {
	return len(name) > len(stateFileSuffix) && name[len(name)-len(stateFileSuffix):] == stateFileSuffix
}

func trimStateSuffix(name string) string { return name[:len(name)-len(stateFileSuffix)] }

// --- the claim --------------------------------------------------------------

// ClaimProbe is the atomic probe claim.
//
// It performs one read-modify-write inside the same per-identity lock that guards
// every other state write, so exactly one caller can observe probe_eligible and
// transition it to probing. Everyone else observes probing under a foreign lease
// and subtracts the group, exactly as if it were still suppressed. That is what
// makes "one resolved probe per backoff window" a bound rather than an estimate:
// only the lease owner can escalate, and escalating writes a fresh next_probe_at.
//
// The function is total over the state space, because it is the one place that
// resolves a stale lease and a caller must be able to hand it any group without
// first knowing the state:
//
//	available      -> granted, no lease needed
//	suppressed     -> refused
//	probe_eligible -> granted, lease written
//	probing        -> granted only to the current owner; a stale lease is
//	                  resolved first and the table is re-evaluated
//
// A caller that cannot take the lock is NOT granted a claim. Fail-open here means
// "do not gain a probe", never "probe anyway": a missed probe costs one backoff
// window, while a herd costs a launch per concurrent spawn.
//
// The state space has a fifth row that is not a group state at all: the read
// that never established one. An unreadable, corrupt or freshly quarantined file
// is a HOLE, not an empty state, and this function is the one place where the
// difference decides an authorization. Inside the loss blackout it refuses every
// claim; past it, a group with no record is admitted as probe_eligible rather
// than available. The full contract, including what stays fail-open and why, is
// written above loadIdentityFile.
func (s *Store) ClaimProbe(identity Identity, group string, claimant Claimant) (Lease, bool, error) {
	if claimant.RunID == "" {
		return Lease{}, false, fmt.Errorf("providerlimits: claimant run ID is required")
	}
	if claimant.OwnerKind == "" {
		claimant.OwnerKind = OwnerProcess
	}
	var (
		granted bool
		lease   Lease
		// grantNeedsWrite is true only when the grant is the probe_eligible ->
		// probing transition, i.e. when it exists solely because it was
		// persisted. A write failure then has to refuse the claim: an
		// unpersisted lease would gate nobody, and every concurrent spawn would
		// probe. A grant that did not depend on a write (an available group, or
		// the current owner re-observing its own lease) is unaffected.
		grantNeedsWrite bool
	)
	err := s.mutate(identity, func(file *identityStateFile, read StateRead) (bool, error) {
		now := s.now()
		// The indeterminate arm. read is StateReadUnreadable, StateReadCorrupt
		// or StateReadDegraded: the records this identity had were LOST, so the
		// group map below is a hole rather than an absence, and the two must not
		// produce the same answer.
		//
		// StateReadSchemaAhead is excluded on purpose and never reaches here —
		// mutate refuses it with ErrReadOnlyState — and its retained fail-open
		// grant is handled at the bottom of this function.
		if read.Indeterminate() {
			if !s.lostStateClaimAllowed(file, now) {
				// A lease written before the loss can still be live. Granting
				// one now would hand a second run the probe another run is
				// already holding, which is the herd the atomic claim exists to
				// prevent — so this refuses, exactly as a lock timeout does.
				s.warnf("provider-limits probe claim on %s was refused for run %s: this identity's records were lost and a lease held by another run could still be live", group, claimant.RunID)
				granted, lease = false, Lease{}
				return false, nil
			}
			if existing, ok := file.Groups[group]; !ok || existing == nil {
				// Past the blackout, a group with no record is admitted as
				// PROBE_ELIGIBLE rather than available: one caller may test it,
				// everyone else stays subtracted. It is materialised rather than
				// merely treated as such, because the record IS the gate — a
				// group that is only notionally probe-eligible gates nobody, and
				// every concurrent spawn would claim it at once.
				//
				// Step 0 with no evidence is the same answer ageOutRecord gives
				// a record whose escalation it forgets: forget what cannot be
				// established, assert nothing, keep the gate.
				file.Groups[group] = &GroupRecord{
					Provider:        identity.Provider,
					State:           StateProbeEligible,
					SuppressedSince: timePtr(now),
					NextProbeAt:     timePtr(now),
				}
			}
		}
		record, ok := file.Groups[group]
		if !ok || record == nil {
			// No record is available in this state machine: nothing to claim.
			// Reachable only under a DETERMINATE read, where a missing record is
			// a proven absence of suppression.
			granted, lease = true, Lease{}
			return false, nil
		}
		changed := s.normalizeRecord(record, now)
		if resolved := s.resolveStaleLease(identity, group, record, now); resolved {
			changed = true
		}
		// A record that entered holding a lease was skipped by the entry sweep,
		// because resolving a lease belongs to resolveStaleLease alone. Now that
		// the lease is resolved, a stale escalation left behind by the break is
		// aged out before the claim decision reads the step — otherwise the very
		// probe this call is about to grant would carry it.
		if record.ProbeLease == nil && record.staleAt(now) && record.EffectiveState(now) != StateAvailable {
			if s.ageOutRecord(group, record, now) {
				changed = true
			}
		}
		switch record.EffectiveState(now) {
		case StateAvailable:
			granted, lease = true, Lease{}
		case StateSuppressed:
			granted, lease = false, Lease{}
		case StateProbeEligible:
			record.State = StateProbing
			record.ProbeLease = &Lease{
				Group:         group,
				Identity:      identity.Key,
				RunID:         claimant.RunID,
				OwnerKind:     claimant.OwnerKind,
				PID:           claimant.PID,
				PIDStartTime:  claimant.PIDStartTime,
				ClaimedAt:     now,
				ExpiresAt:     now.Add(time.Duration(s.leaseMinutes) * time.Minute),
				ClaimDeadline: now.Add(time.Duration(s.claimMaxMinutes) * time.Minute),
			}
			granted, lease, changed, grantNeedsWrite = true, *record.ProbeLease, true, true
		case StateProbing:
			if record.ProbeLease != nil && record.ProbeLease.RunID == claimant.RunID {
				granted, lease = true, *record.ProbeLease
			} else {
				granted, lease = false, Lease{}
			}
		}
		return changed, nil
	})
	switch {
	case err == nil:
	case errors.Is(err, ErrReadOnlyState):
		// The one indeterminate read that still fails OPEN, and the only place
		// this binary grants a claim it cannot back with a record.
		//
		// A schema-ahead file is INTACT: a newer build of this same tool on this
		// same machine wrote it and is gating correctly. This binary refuses to
		// write it, so it can neither record a lease nor quarantine it into a
		// tombstone — there is no clock that recovers, and refusing here would
		// brick the identity permanently over a perfectly good file. The
		// recovery is to run the newer binary, which the load warning names.
		return Lease{}, true, nil
	case grantNeedsWrite:
		// Fail-open here means "do not gain a probe", never "probe anyway".
		s.warnf("provider-limits probe claim on %s was not persisted (%v); the group stays subtracted for run %s", group, err, claimant.RunID)
		return Lease{}, false, nil
	}
	return lease, granted, nil
}

// normalizeRecord materialises the suppressed -> probe_eligible transition so the
// recorded state matches what a reader derives.
func (s *Store) normalizeRecord(record *GroupRecord, now time.Time) bool {
	if record.State == StateSuppressed && record.NextProbeAt != nil && !now.Before(*record.NextProbeAt) {
		record.State = StateProbeEligible
		return true
	}
	return false
}

// resolveStaleLease applies the expiry table to a lease past its resolution
// instant — ExpiresAt, or the claim deadline when that comes first. It splits on
// owner kind first and liveness second, because the two owners are evidence about
// different things: a live pre-launch process is evidence about the CLI, and only
// a launched run is evidence about the provider.
func (s *Store) resolveStaleLease(identity Identity, group string, record *GroupRecord, now time.Time) bool {
	lease := record.ProbeLease
	if record.State != StateProbing || lease == nil || now.Before(lease.ResolveAt()) {
		return false
	}
	switch lease.OwnerKind {
	case OwnerProcess:
		alive, known := s.procLiveness.Alive(*lease)
		if !known {
			// The process arm is always answerable. An unknown answer here is a
			// programming error, and the conservative resolution is "not alive".
			s.warnf("provider-limits process liveness returned no answer for pid %d on group %s; treating the claimant as dead", lease.PID, group)
			alive = false
		}
		switch {
		case alive && now.Before(lease.ClaimDeadline):
			// The renewal is CAPPED at the immutable deadline. An expiry written
			// past it would be a lie about how long this claimant may hold the
			// group, and the report would publish that lie as a live lease.
			renewed := now.Add(time.Duration(s.leaseMinutes) * time.Minute)
			if renewed.After(lease.ClaimDeadline) {
				renewed = lease.ClaimDeadline
			}
			lease.ExpiresAt = renewed
			lease.Renewals++
			// State stays probing and the group stays subtracted. No availability
			// is asserted, because nothing has been launched.
			return true
		case alive:
			s.warnf("provider-limits probe claim on %s held by pid %d (run %s) never launched within %d minutes; breaking the lease back to probe_eligible at step %d",
				group, lease.PID, lease.RunID, s.claimMaxMinutes, record.BackoffStep)
			s.breakLease(record, now)
			return true
		default:
			s.warnf("provider-limits probe claim on %s was held by pid %d (run %s), which is no longer alive; returning the group to probe_eligible at step %d",
				group, lease.PID, lease.RunID, record.BackoffStep)
			s.breakLease(record, now)
			return true
		}
	case OwnerRun:
		alive, known := false, false
		if s.runLiveness != nil {
			alive, known = s.runLiveness.Alive(*lease)
		}
		switch {
		case known && alive:
			// The one non-exit-0 path to available, and it requires a launch: the
			// runner started and a child of that provider has been alive for the
			// whole lease without producing a quota failure.
			record.State = StateAvailable
			record.ProbeLease = nil
			record.ProbeProvisionalSince = timePtr(now)
			record.NextProbeAt = nil
			return true
		case known:
			s.warnf("provider-limits probe on %s ran as %s, which is terminal or absent; returning the group to probe_eligible at step %d", group, lease.RunID, record.BackoffStep)
			s.breakLease(record, now)
			return true
		default:
			// known=false is the absence of evidence, not its presence.
			// Provisional availability requires a confirmed non-terminal run.
			s.warnf("provider-limits probe on %s ran as %s, whose liveness is unknown; returning the group to probe_eligible at step %d rather than asserting availability", group, lease.RunID, record.BackoffStep)
			s.breakLease(record, now)
			return true
		}
	default:
		s.warnf("provider-limits probe lease on %s carries unknown owner kind %q; returning the group to probe_eligible at step %d", group, lease.OwnerKind, record.BackoffStep)
		s.breakLease(record, now)
		return true
	}
}

// breakLease returns a group to probe_eligible at the SAME step. Nothing was
// learned, so the ladder must not move.
//
// If a quota observation escalated the record while the probe was in flight, the
// new window is respected: the group goes back to suppressed until that window
// elapses, rather than being handed a free probe the escalation just withdrew.
func (s *Store) breakLease(record *GroupRecord, now time.Time) {
	record.ProbeLease = nil
	if record.SuppressedSince == nil {
		record.SuppressedSince = timePtr(now)
	}
	if record.NextProbeAt != nil && now.Before(*record.NextProbeAt) {
		record.State = StateSuppressed
		return
	}
	record.State = StateProbeEligible
	record.NextProbeAt = timePtr(now)
}

// AbandonProbe resolves a lease as "nothing was learned": the group is suppressed
// at the SAME step and re-armed at now + ladder[k], the lease is released, and the
// ladder does NOT advance. A claim abandoned before launch must never look like a
// failed probe, or a run that died in prompt composition would push an unrelated
// group up the ladder.
//
// The call is fenced by (run_id, claimed_at). On a mismatch, or on a nil stored
// lease, it writes nothing and returns ErrLeaseLost.
func (s *Store) AbandonProbe(identity Identity, group string, held Lease) error {
	if !held.Held() {
		return fmt.Errorf("providerlimits: AbandonProbe requires the lease the caller holds")
	}
	return s.mutate(identity, func(file *identityStateFile, _ StateRead) (bool, error) {
		record := file.Groups[group]
		if record == nil {
			s.warnf("provider-limits abandon of probe on %s found no record; the lease was already resolved elsewhere", group)
			return false, ErrLeaseLost
		}
		if !fenceMatches(record.ProbeLease, held) {
			s.warnf("provider-limits abandon of probe on %s by run %s was refused: the lease has since been re-issued or broken", group, held.RunID)
			return false, ErrLeaseLost
		}
		now := s.now()
		record.ProbeLease = nil
		record.State = StateSuppressed
		record.ProbeAbandoned++
		if record.SuppressedSince == nil {
			record.SuppressedSince = timePtr(now)
		}
		// Same step, re-armed at now + ladder[k]. The existing evidence and reset
		// hint are left alone: an abandon learned nothing, so it erases nothing.
		next, _ := s.armAt(now, record.BackoffStep, record.SuppressedSince, nil)
		record.NextProbeAt = timePtr(next)
		return true, nil
	})
}

// TransferProbe moves ownership of a held lease from the spawning process to the
// started run. It is called immediately after the runner has been started, and
// only then.
//
// The transfer cannot be a pid rewrite: a tracked background spawn's CLI process
// exits shortly after the runner starts, so a process-owned lease would be broken
// by dead-owner recovery on the very next selection — the herd the claim exists to
// prevent, reintroduced by bookkeeping.
//
// The fence is preserved: ClaimedAt is not rewritten.
func (s *Store) TransferProbe(identity Identity, group string, held Lease, runID string) error {
	if !held.Held() {
		return fmt.Errorf("providerlimits: TransferProbe requires the lease the caller holds")
	}
	return s.mutate(identity, func(file *identityStateFile, _ StateRead) (bool, error) {
		record := file.Groups[group]
		if record == nil {
			s.warnf("provider-limits transfer of probe on %s found no record; the lease was already resolved elsewhere", group)
			return false, ErrLeaseLost
		}
		if !fenceMatches(record.ProbeLease, held) {
			s.warnf("provider-limits transfer of probe on %s by run %s was refused: the lease has since been re-issued or broken", group, held.RunID)
			return false, ErrLeaseLost
		}
		now := s.now()
		lease := record.ProbeLease
		lease.OwnerKind = OwnerRun
		lease.PID = 0
		lease.PIDStartTime = time.Time{}
		if runID != "" {
			lease.RunID = runID
		}
		// ExpiresAt is re-based so the lease measures the child's life rather
		// than the CLI's. ClaimedAt — the fence — is untouched.
		lease.ExpiresAt = now.Add(time.Duration(s.leaseMinutes) * time.Minute)
		return true, nil
	})
}

// Observe records a provider-quota observation against a group.
//
// Only an Observation carrying the provider-quota gate is accepted; the
// run-budget class has no way to reach this function, because it cannot produce
// such an Observation.
//
// held is the lease the caller believes it holds, or nil when the caller launched
// into a group that needed no claim. The fence rule degrades deliberately here:
// a quota observation from a fenced-out owner IS recorded against the group,
// because it is real provider evidence and is monotone — it can only suppress
// further, never grant availability — but it does not touch the current lease.
//
// The ladder advances at most one step per backoff window: an observation that
// arrives while the current window still covers now records its evidence without
// escalating. That is what bounds the ladder when many concurrent spawns fail at
// once.
func (s *Store) Observe(identity Identity, group string, held *Lease, obs Observation) error {
	if !obs.IsProviderQuota() {
		s.warnf("provider-limits observation for %s was refused: only shared-subscription evidence may suppress a group", group)
		return ErrNotProviderQuota
	}
	// The second gate is the provider itself. Limit detection is scoped to the
	// providers whose real error shapes were captured; a provider with no
	// classifier must be unsuppressible at the store boundary too, so that no
	// future call site can route foreign evidence into a group record.
	for _, provider := range [2]string{obs.Provider, identity.Provider} {
		if provider == "" || HasClassifierForRuntime(provider) {
			continue
		}
		s.warnf("provider-limits observation for %s was refused: provider %q has no limit classifier, so nothing about it can be suppressed", group, provider)
		return ErrUnsupportedProvider
	}
	return s.mutate(identity, func(file *identityStateFile, _ StateRead) (bool, error) {
		now := obs.At
		if now.IsZero() {
			now = s.now()
		}
		record := file.Groups[group]
		if record == nil {
			record = &GroupRecord{Provider: identity.Provider, State: StateAvailable}
			file.Groups[group] = record
		}
		if record.Provider == "" {
			record.Provider = identity.Provider
		}

		ownsLease := held != nil && fenceMatches(record.ProbeLease, *held)
		fenceLost := held != nil && !ownsLease
		keepForeignLease := !ownsLease && record.ProbeLease != nil

		hadSuppression := record.SuppressedSince != nil
		inWindow := record.NextProbeAt != nil && now.Before(*record.NextProbeAt)

		step := 0
		rearm := true
		switch {
		case !hadSuppression:
			step = 0
			record.SuppressedSince = timePtr(now)
		case inWindow:
			// The group is already suppressed for this window. Record the
			// evidence, but neither escalate nor move next_probe_at.
			step = record.BackoffStep
			rearm = false
		default:
			step = s.escalatedStep(record.BackoffStep)
		}

		record.BackoffStep = step
		record.ConsecutiveLimitObservations++
		record.LastObservationAt = timePtr(now)
		record.ProbeProvisionalSince = nil
		record.ClearedAt = nil
		record.ClearedBy = ""
		if obs.Evidence != (Evidence{}) {
			evidence := obs.Evidence
			record.Evidence = &evidence
		}

		if rearm {
			next, hint := s.armAt(now, step, record.SuppressedSince, obs.ResetHint)
			record.NextProbeAt = timePtr(next)
			record.ProviderResetHint = hint
		} else if obs.ResetHint != nil {
			// The window already covers now, so the hint could not have bound it.
			hint := *obs.ResetHint
			hint.Used = false
			hint.Note = ResetHintNote
			record.ProviderResetHint = &hint
		} else {
			record.ProviderResetHint = nil
		}

		if keepForeignLease {
			// Do not manage someone else's lease. The record's step and window
			// move; the lease does not.
			if fenceLost {
				s.warnf("provider-limits quota observation on %s from run %s was recorded, but its lease had been re-issued or broken; the current lease was left untouched", group, held.RunID)
			}
			if fenceLost {
				return true, ErrLeaseLost
			}
			return true, nil
		}
		record.ProbeLease = nil
		record.State = StateSuppressed
		if fenceLost {
			s.warnf("provider-limits quota observation on %s from run %s was recorded; its lease had already been resolved", group, held.RunID)
			return true, ErrLeaseLost
		}
		return true, nil
	})
}

// armAt computes next_probe_at for a step, and the hint record to store beside
// it.
//
// The reset hint survives only as min(): it may shorten the current step and can
// never lengthen it. A hint later than the step is ignored for suppression and
// stored with used=false; an earlier one binds and is stored with used=true.
// next_probe_at is never earlier than suppressed_since and never earlier than now.
func (s *Store) armAt(now time.Time, step int, suppressedSince *time.Time, hint *ResetHint) (time.Time, *ResetHint) {
	next := now.Add(s.ladderStep(step))
	var stored *ResetHint
	if hint != nil {
		copied := *hint
		copied.Used = false
		copied.Note = ResetHintNote
		if copied.Parsed != nil && copied.Parsed.After(now) && copied.Parsed.Before(next) {
			next = *copied.Parsed
			copied.Used = true
		}
		stored = &copied
	}
	if suppressedSince != nil && next.Before(*suppressedSince) {
		next = *suppressedSince
	}
	if next.Before(now) {
		next = now
	}
	return next, stored
}

// ObserveSuccess records an exit-0 run in the group. A successful launch on any
// model in the group is the only thing that clears it, so this is not fenced: any
// run that got a clean exit out of the provider is the evidence the design
// accepts.
func (s *Store) ObserveSuccess(identity Identity, group string, ev Evidence) error {
	return s.mutate(identity, func(file *identityStateFile, _ StateRead) (bool, error) {
		record := file.Groups[group]
		if record == nil {
			// Nothing to clear: a missing record already reads available.
			return false, nil
		}
		now := s.now()
		if record.ProbeLease != nil && ev.RunID != "" && record.ProbeLease.RunID != ev.RunID {
			s.warnf("provider-limits group %s was cleared by a successful run %s while run %s held its probe lease; releasing the lease", group, ev.RunID, record.ProbeLease.RunID)
		}
		record.State = StateAvailable
		record.BackoffStep = 0
		record.SuppressedSince = nil
		record.NextProbeAt = nil
		record.ConsecutiveLimitObservations = 0
		record.Evidence = nil
		record.ProviderResetHint = nil
		record.ProbeLease = nil
		record.ProbeProvisionalSince = nil
		record.ClearedAt = nil
		record.ClearedBy = ""
		record.LastSuccessAt = timePtr(now)
		record.LastObservationAt = timePtr(now)
		return true, nil
	})
}

// ClearGroup forgets a suppression without asserting availability.
//
// The record is REWRITTEN to probe_eligible(step 0) with next_probe_at = now. It
// is emphatically not deleted: a missing record reads as available in this state
// machine, so deleting it would remove the claim gate and readmit every concurrent
// spawn at once — the exact herd the atomic claim exists to prevent, performed
// while telling the operator it had done something safe.
//
// The operator's intent ("quota reset, try again now") is honoured with no wait,
// but it is claim-gated: exactly one next spawn probes, and only that launch's
// exit 0 clears the group to available.
//
// Any held lease is broken and fenced, so the displaced claimant's later lease
// operations become warned no-ops rather than writes.
func (s *Store) ClearGroup(identity Identity, group, by string) error {
	return s.mutate(identity, func(file *identityStateFile, _ StateRead) (bool, error) {
		now := s.now()
		record := file.Groups[group]
		if record == nil {
			record = &GroupRecord{Provider: identity.Provider}
			file.Groups[group] = record
		}
		if record.ProbeLease != nil {
			s.warnf("provider-limits clear of %s displaced the probe lease held by run %s", group, record.ProbeLease.RunID)
		}
		if record.Provider == "" {
			record.Provider = identity.Provider
		}
		record.State = StateProbeEligible
		record.BackoffStep = 0
		record.SuppressedSince = timePtr(now)
		record.NextProbeAt = timePtr(now)
		record.ConsecutiveLimitObservations = 0
		record.Evidence = nil
		record.ProviderResetHint = nil
		record.ProbeLease = nil
		record.ProbeProvisionalSince = nil
		record.LastObservationAt = timePtr(now)
		record.ClearedAt = timePtr(now)
		if by == "" {
			by = "operator"
		}
		record.ClearedBy = by
		return true, nil
	})
}

func timePtr(t time.Time) *time.Time {
	copied := t
	return &copied
}
