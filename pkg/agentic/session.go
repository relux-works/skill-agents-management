// Plan.Session is the module-owned record of a composed plan's native session
// metadata: the session name the provider will display and the remote-control
// intent with the exact argv positions that carry it.
//
// A hosted consumer must carry both into its launch payload, but it may not
// parse provider flags to get them: a second parser is a second grammar, and
// the two drift until a launch carries metadata for options it never passed.
// The system plugin owns the grammar — it builds the argv — so the plugin
// fills the record, and it fills it WHILE BuildPlan builds the plan: the
// record is a field of the registry-built Plan, not the answer of a separate
// API. There is nothing to call with a plan the owning grammar never built
// and nothing to route around, because the only value a consumer ever holds
// is the one that travelled inside the plan BuildPlan returned.
//
// The fill is PURE. The plugin reads the argv the registry hands it and the
// working directory that argv resolves against, and nothing else; it starts
// no process. Its only input is a SessionFill that this package alone can
// issue (it has no exported constructor and no exported field), so no
// exported function in the module derives a Session from caller-supplied
// argv: a caller holding the plugin has no argv to give it, and a zero
// SessionFill is refused by the plugin as not issued.
//
// Trust boundary: registry registration. A registered plugin owns the plans
// it builds, including one that embeds another plugin's type — Go embedding
// inherits behaviour by design, and that case is accepted scope, not a
// bypass this package tries to defeat.
//
// The record rides the exec seal IN-PROCESS ONLY. BuildPlan snapshots it with
// the base process, FinalizePlan refuses a base whose Session changed after
// sealing (and re-derives it when a native tail extends the argv), and a
// finalized in-process verifier refuses a plan whose Session differs from the
// one it was finalized with in any respect: presence (a dropped record
// refuses), name, RCEnabled and RCIndices, nil versus empty included.
//
// ACROSS ExportSeal/ImportSeal, Session is UNVERIFIED. The exported guard
// payload contract 1.0.0 is frozen with the Claude exec guard unsealed, so the
// wire carries no Session, and the settings-origin remote control depends on
// inputs outside argv (settings bytes, the verification working directory)
// that no seal binds: deriving the record again at import would present a
// guarantee the seal cannot give. So ImportSeal never derives, binds or
// checks a Session, and an imported plan never carries one: the process an
// ImportedProcess rebuilds (ImportedProcess.Process) holds a nil Session by
// construction. Whatever Session a plan handed to an imported verifier carries
// is not an input of that verification — admission says nothing about it, and
// a consumer that needs a Session across a process boundary must carry it
// itself and treat it as unverified.
package agentic

import (
	"errors"
	"fmt"
	"slices"
)

// PlanSession is the native session metadata for one composed argv, attached
// to Plan.Session by the plugin that built the argv. It is nil for a system
// that defines no native session surface.
//
// Name is the native session name when the plan sets one and nil when it
// sets none: absence of a name and an empty name are different facts, and a
// single string cannot say which one happened. The pointed-to string is a
// copy; mutating it cannot change the plugin's argv.
//
// RCEnabled reports remote-control INTENT — an RC-family selector on the argv
// or a settings-origin enablement — not whether the provider will ultimately
// honor it. A managed-policy disable the plugin never saw is a stated bound,
// not a modeled input.
//
// RCIndices are the exact positions in Plan.Argv of the RC-family tokens, in
// argv order, every one of them. An enabled RC with empty indices is the
// settings-origin form: the intent comes from settings, so no argv token
// carries it. A disabled RC carries no indices, ever — disabled-with-indices
// is incoherent and BuildPlan never admits it. The slice is always non-nil.
type PlanSession struct {
	Name      *string
	RCEnabled bool
	RCIndices []int
}

// SessionFill is the only input a SessionPlanner receives: the argv of a plan
// the registry is building (or finalizing with a native tail), the working
// directory that argv resolves relative paths against, and the launch mode.
//
// Only this package can issue one: the fields are unexported and there is no
// exported constructor, so no caller outside the module can hand a plugin
// argv of its choosing. The zero value is not issued; a plugin refuses it.
type SessionFill struct {
	argv    []string
	workDir string
	mode    LaunchMode
	issued  bool
}

func newSessionFill(argv []string, workDir string, mode LaunchMode) SessionFill {
	return SessionFill{argv: argv, workDir: workDir, mode: mode, issued: true}
}

// Issued reports whether BuildPlan or FinalizePlan issued this fill. A plugin must refuse one that was not.
func (f SessionFill) Issued() bool { return f.issued }

// Argv is a fresh copy of the argv to read: writing to it moves neither the
// plan's argv nor the fill.
func (f SessionFill) Argv() []string { return slices.Clone(f.argv) }

// WorkDir is the directory relative settings sources resolve against.
func (f SessionFill) WorkDir() string { return f.workDir }

// Mode is the launch mode of the plan the fill belongs to.
func (f SessionFill) Mode() LaunchMode { return f.mode }

// SessionPlanner is optional. A plugin whose harness has a native session
// surface fills Plan.Session from its own argv grammar while BuildPlan runs.
//
// The method is the only place a session grammar lives. Its production
// callers are BuildPlan (the plan's own argv) and FinalizePlan (the argv after
// a native tail), each handing over a SessionFill only this package can issue. A caller holding the plugin
// directly has no way to give it argv, so there is nothing to derive from and
// nothing to route around — consumers read Plan.Session, which only a
// registry-built plan carries. A nil answer means the system defines no
// session surface for this launch.
type SessionPlanner interface {
	FillPlanSession(fill SessionFill) (*PlanSession, error)
}

var (
	// ErrSessionInvalid is returned when the argv carries session selectors
	// the plugin cannot honestly record: a repeated selector, two names that
	// differ, a required selector with no value, or an unreadable settings
	// source it must consult. It is a refusal at BuildPlan, never a guess —
	// recording one of two contradicting selectors would report session
	// metadata the launch does not carry.
	ErrSessionInvalid = errors.New("agentic: session metadata is invalid")
)

// SessionInvalidError carries the fixed reason a session record was refused.
// Reasons name selector spellings — grammar, not caller data — and never the
// session names under dispute.
type SessionInvalidError struct {
	Reason string
}

func (e *SessionInvalidError) Error() string {
	if e == nil {
		return ErrSessionInvalid.Error()
	}
	return fmt.Sprintf("%s: %s", ErrSessionInvalid, e.Reason)
}

func (e *SessionInvalidError) Unwrap() error { return ErrSessionInvalid }

// cloneSession returns a detached copy: the name, the index slice and the
// record itself share no memory with the original. A nil record stays nil and
// a nil index slice stays nil: collection presence is part of the record, so a
// copy must not turn one shape into the other.
func cloneSession(session *PlanSession) *PlanSession {
	if session == nil {
		return nil
	}
	clone := PlanSession{RCEnabled: session.RCEnabled, RCIndices: slices.Clone(session.RCIndices)}
	if session.Name != nil {
		name := *session.Name
		clone.Name = &name
	}
	return &clone
}

// sessionsEqual compares two records by value; nil equals only nil, a name
// pointer equals another only by pointed-to string (nil is not ""), and a nil
// index slice equals only a nil one (slices.Equal alone would call nil and
// empty the same, which is exactly the settings-origin shape an attacker
// would flatten).
func sessionsEqual(left, right *PlanSession) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.RCEnabled != right.RCEnabled || (left.RCIndices == nil) != (right.RCIndices == nil) || !slices.Equal(left.RCIndices, right.RCIndices) {
		return false
	}
	if left.Name == nil || right.Name == nil {
		return left.Name == right.Name
	}
	return *left.Name == *right.Name
}

// checkSessionRecord is the contract every plugin answer must meet before it
// is attached: the index slice is non-nil, its entries are unique, strictly
// inside the argv the plan carries, and an RC that is not enabled carries
// none. A plugin that answers otherwise has a bug, which surfaces here as a
// plugin-contract refusal rather than as a record a consumer would trust.
func checkSessionRecord(id SystemID, session *PlanSession, argv []string) error {
	if session == nil {
		return nil
	}
	if session.RCIndices == nil {
		return fmt.Errorf("%w: %s answered a session record with a nil RC index slice", ErrPluginContract, id)
	}
	if !session.RCEnabled && len(session.RCIndices) > 0 {
		return fmt.Errorf("%w: %s answered a disabled remote control carrying %d RC index(es)", ErrPluginContract, id, len(session.RCIndices))
	}
	seen := make(map[int]bool, len(session.RCIndices))
	for _, index := range session.RCIndices {
		if index < 0 || index >= len(argv) {
			return fmt.Errorf("%w: %s answered RC index %d outside an argv of %d token(s)", ErrPluginContract, id, index, len(argv))
		}
		if seen[index] {
			return fmt.Errorf("%w: %s answered RC index %d twice", ErrPluginContract, id, index)
		}
		seen[index] = true
	}
	return nil
}

// deriveSession asks the plugin for the record of one argv and holds the
// answer to the same contract everywhere it is asked: BuildPlan and the native
// tail re-derivation of FinalizePlan both call this, so a plugin answer is
// checked identically at every site. The returned
// record is a detached copy; a nil answer is a nil record.
func deriveSession(planner SessionPlanner, id SystemID, argv []string, workDir string, mode LaunchMode) (*PlanSession, error) {
	if value, err := planner.FillPlanSession(newSessionFill(argv, workDir, mode)); err != nil {
		return nil, fmt.Errorf("agentic: %s could not record its session metadata: %w", id, err)
	} else if err := checkSessionRecord(id, value, argv); err != nil {
		return nil, err
	} else {
		return cloneSession(value), nil
	}
}
