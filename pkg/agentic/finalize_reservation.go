package agentic

import (
	"errors"
	"fmt"
	"strings"
)

// Typed reservation binding for FinalizePlan.
//
// A hosted launcher that starts a NEW managed session asks the session daemon
// to reserve one first; the daemon answers with a SES handle and the native
// session UUID the provider must start under. This file is the module's half
// of that handshake and nothing more: FinalizePlan carries those two
// server-supplied values into exactly two typed slots at module-fixed
// positions. The reserve endpoint, its authentication, single use and TTL are
// the daemon's; the module never generates, stores or checks the issuance of
// a value, it checks only that what it is asked to bind is well formed and
// agrees with itself.
//
// The two slots, and where they land in the finalized process:
//
//   - env ManagedSessionEnvSlot carries the SES handle, as the LAST entry of
//     the final environment, after every fragment and prompt overlay.
//   - argv ManagedSessionArgvSlot carries the native UUID as the FIRST two
//     tokens of the final argv, before the sealed base argv and the native
//     tail, so no `--` in either can turn the slot into prompt text.
//
// Everything else about a typed binding still refuses typed. The reservation
// is a NEW launch only: a resume or adoption intent under it refuses, whether
// it arrives as the reservation's Intent or as a selector the plugin's own
// resume grammar finds in the argv the slots would sit beside.
var (
	// ErrFinalizeReservationRefused refuses a reservation FinalizePlan
	// cannot bind: a slot set that is not exactly the two module slots, a
	// SES handle or UUID with no valid shape, a slot value that differs
	// from the reservation, a resume or adoption intent, or a process that
	// already carries a slot.
	ErrFinalizeReservationRefused = errors.New("agentic: finalize session reservation is refused")
)

const (
	// BindingKindManagedSession is the only typed binding kind FinalizePlan
	// admits, and only under a reservation.
	BindingKindManagedSession = "managed-session"
	// ManagedSessionEnvSlot names the environment slot that carries the
	// reserved SES handle.
	ManagedSessionEnvSlot = "TASK_BOARD_MANAGED_SESSION_ID"
	// ManagedSessionArgvSlot names the argv slot that carries the reserved
	// native session UUID.
	ManagedSessionArgvSlot = "--session-id"
)

// SessionReservation is the opaque, server-supplied reservation a typed
// managed-session binding must agree with. The module never constructs one.
// SessionID is the SES handle (resume grammar `SES-...`), NativeID the native
// session UUID, and Intent must stay the zero value or ResumeNew: a
// reservation starts a new session, it cannot resume or adopt one, and like
// every new intent it carries no Identity.
type SessionReservation struct {
	SessionID string
	NativeID  string
	Intent    ResumeIntent
}

// reservedSlots is a validated reservation ready to apply.
type reservedSlots struct {
	sessionID string
	nativeID  string
}

// boundReservation is what a finalized verifier binds about the slots,
// in-process only like Session: the keyed commitment of the env slot and the
// argv slot value. A binding rebuilt from the frozen guard wire (ImportSeal)
// carries none; across a process boundary the slots stay bound only through
// the full argv and environment commitments.
type boundReservation struct {
	envCommitment string
	nativeID      string
}

// resolveFinalizeBindings validates the typed bindings against the
// reservation in overlays and the argv the slots would sit beside. Without a
// reservation every binding is unknown, as before. env is the final
// environment the env slot would be appended to.
func resolveFinalizeBindings(base Plan, overlays FinalizeOverlays, bindings []TypedBinding, env []string) (*reservedSlots, error) {
	reservation := overlays.Reservation
	if reservation == nil {
		if err := refuseFinalizeBindings(bindings); err != nil {
			return nil, err
		}
		return nil, nil
	}
	values := make(map[string]string, 2)
	for _, binding := range bindings {
		if binding.Kind != BindingKindManagedSession || (binding.Name != ManagedSessionEnvSlot && binding.Name != ManagedSessionArgvSlot) {
			return nil, fmt.Errorf("%w: %q slot %q", ErrFinalizeBindingUnknown, binding.Kind, binding.Name)
		}
		values[binding.Name] = binding.Value
	}
	if len(bindings) != 2 || len(values) != 2 {
		return nil, fmt.Errorf("%w: a reservation binds exactly the %s and %s slots, got %d binding(s)", ErrFinalizeReservationRefused, ManagedSessionEnvSlot, ManagedSessionArgvSlot, len(bindings))
	}
	if !resumeHandle.MatchString(reservation.SessionID) {
		return nil, fmt.Errorf("%w: the reserved session handle is not a SES handle", ErrFinalizeReservationRefused)
	}
	if !resumeUUID.MatchString(reservation.NativeID) {
		return nil, fmt.Errorf("%w: the reserved native session id is not a UUID", ErrFinalizeReservationRefused)
	}
	if values[ManagedSessionEnvSlot] != reservation.SessionID || values[ManagedSessionArgvSlot] != reservation.NativeID {
		return nil, fmt.Errorf("%w: a slot value differs from the reservation", ErrFinalizeReservationRefused)
	}
	// The Intent follows the module's own ResumeIntent contract: an empty
	// Kind is a new launch, a new launch carries no Identity (the native
	// UUID travels only as the slot value), and nothing but new is admitted.
	intent := reservation.Intent
	if intent.Kind == "" {
		intent.Kind = ResumeNew
	}
	if err := ValidateResumeIntent(intent, ""); err != nil || intent.Kind != ResumeNew {
		return nil, fmt.Errorf("%w: a reservation starts a new session, it carries no resume or adoption intent and no identity", ErrFinalizeReservationRefused)
	}
	// The plugin that owns a native session grammar is the only judge of
	// what the argv selects; a system without one has no `--session-id`
	// to bind and admits no slot.
	elevator, ok := base.sessionPlanner.(ResumeSelectorElevator)
	if !ok {
		return nil, fmt.Errorf("%w: system %q has no native session grammar to bind a reservation to", ErrFinalizeBindingUnknown, string(base.System))
	}
	for _, entry := range env {
		if name, _, _ := strings.Cut(entry, "="); name == ManagedSessionEnvSlot {
			return nil, fmt.Errorf("%w: the process already carries %s", ErrFinalizeReservationRefused, ManagedSessionEnvSlot)
		}
	}
	argv := append(append([]string(nil), base.Argv...), overlays.NativeTail...)
	if elevation, err := elevator.ElevateResumeIntent(nil, argv); err != nil || elevation.Intent.Kind != ResumeNew {
		return nil, fmt.Errorf("%w: the argv carries a resume, adoption or native session selector", ErrFinalizeReservationRefused)
	}
	return &reservedSlots{sessionID: reservation.SessionID, nativeID: reservation.NativeID}, nil
}

// finalEnv appends the env slot last. A nil receiver is no reservation.
func (r *reservedSlots) finalEnv(env []string) []string {
	if r == nil {
		return env
	}
	return append(env, ManagedSessionEnvSlot+"="+r.sessionID)
}

// finalArgv returns the final argv: the argv slot first, then the sealed base
// argv, then the native tail. A nil receiver is no reservation.
func (r *reservedSlots) finalArgv(base, tail []string) []string {
	var head []string
	if r != nil {
		head = []string{ManagedSessionArgvSlot, r.nativeID}
	}
	return append(append(head, base...), tail...)
}

// bind records the slots in the finalized bindings.
func (r *reservedSlots) bind(b *finalizedBindings) {
	if r == nil {
		return
	}
	b.reservation = &boundReservation{envCommitment: literalCommitment(b.key, ManagedSessionEnvSlot, r.sessionID), nativeID: r.nativeID}
}

// verify checks both slots at their fixed positions before the whole-process
// comparison, so a changed slot is named as one. A nil receiver binds nothing.
func (r *boundReservation) verify(plan Plan, key SealCommitmentKey) error {
	if r == nil {
		return nil
	}
	if len(plan.Argv) < 2 || plan.Argv[0] != ManagedSessionArgvSlot || plan.Argv[1] != r.nativeID {
		return fmt.Errorf("%w: reservation argv slot differs", ErrFinalizedProcessChanged)
	}
	var name, value string
	if len(plan.Env) > 0 {
		name, value, _ = strings.Cut(plan.Env[len(plan.Env)-1], "=")
	}
	if name != ManagedSessionEnvSlot || literalCommitment(key, name, value) != r.envCommitment {
		return fmt.Errorf("%w: reservation env slot differs", ErrFinalizedProcessChanged)
	}
	return nil
}
