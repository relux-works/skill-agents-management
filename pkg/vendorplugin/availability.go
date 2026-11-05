package vendorplugin

import (
	"fmt"
	"strings"
	"time"
)

// Availability is a STRUCTURED verdict about whether a vendor can serve
// requests, not a boolean.
//
// A boolean cannot carry the three things every caller of it ended up needing
// in the extraction source: WHEN a limit clears, WHAT the answer was read from,
// and whether the answer is an observation at all. "false" spread over a rate
// limit that clears in four minutes, a network that is down, and a check
// nobody could run — and the code downstream guessed which one it was.
//
// The four states below are the whole vocabulary, and each is defined by what
// its evidence must contain rather than by what it means in prose:
//
//   - Healthy: at least one source was read, none of them failed.
//   - Limited: a clear time AND an observation that established it.
//   - Unreachable: an observation or a read failure that established it.
//   - Unknown: everything else, including the two cases that must never be
//     confused with each other — checked and found nothing, and could not
//     check. Checked distinguishes them.
//
// # The seam for the local-model resource plane
//
// docs/architecture.md describes a local-models plugin owning a resource plane:
// is the model loaded, is inference running on it, is there memory for another,
// must one be evicted first. That plane is NOT built here. What is built is the
// verdict it will report through, and it fits with no interface change:
//
//   - "loaded and idle" is Healthy with an Observed entry from the plane.
//   - "busy until the running turn finishes" and "waiting on an eviction" are
//     Limited with Until set to the expected clear time.
//   - "the daemon is not running" is Unreachable.
//   - "the plane was asked and could not tell" is Unknown with the plane in
//     Failures — NOT Healthy, and not Unreachable either.
//
// Composition works because Availability is a value: a local vendor's verdict
// is its own answer merged with the plane's, and merging two of these is
// ordinary code rather than a contract change. Nothing in this file knows what
// a resource plane is, and nothing needs to.
type Availability struct {
	State AvailabilityState

	// Until is when a Limited verdict expects to clear. It is required for
	// Limited and forbidden for everything else: an Unknown carrying a clear
	// time is a Limited verdict wearing the wrong label, and a caller that
	// scheduled a retry off it would be acting on a guess.
	Until time.Time

	// Checked names the sources that were actually READ to produce this
	// verdict. An empty Checked on an Unknown means nobody looked; a non-empty
	// Checked on an Unknown means the sources were read and said nothing. An
	// absence and a failure to read are different facts, and this field is
	// where the difference lives.
	Checked []string

	// Observed are the observations the verdict rests on.
	Observed []Observation

	// Failures are the sources that could not be read. A verdict carrying one
	// can never be Healthy: a health claim resting on a source that failed is
	// a guess with an evidence list attached.
	Failures []ReadFailure
}

// Observation is one thing a source actually showed.
type Observation struct {
	// Source names where the observation came from: a response header, a
	// state file, a probe.
	Source string
	// Detail is what it said.
	Detail string
	// At is when it was seen. The zero value is legal and means the source
	// carried no timestamp — which is different from claiming it was seen now.
	At time.Time
}

// ReadFailure is a source that could not be read at all.
type ReadFailure struct {
	// Source names what could not be read.
	Source string
	// Reason is why, in text a caller can surface. It is a string rather than
	// an error so a verdict stays a comparable value that can be logged,
	// snapshotted and compared across runs.
	Reason string
}

// AvailabilityState is the verdict's kind.
type AvailabilityState int

const (
	// AvailabilityUnknown is the ZERO VALUE deliberately. A verdict nobody
	// filled in must not read as healthy: the standing rule is prove or report
	// nothing, and a zero-value struct has proved nothing.
	AvailabilityUnknown AvailabilityState = iota
	// AvailabilityHealthy means requests can be made right now.
	AvailabilityHealthy
	// AvailabilityLimited means requests are refused until a stated time.
	AvailabilityLimited
	// AvailabilityUnreachable means the vendor could not be reached at all.
	AvailabilityUnreachable
)

// Valid reports whether s is one of the declared states.
func (s AvailabilityState) Valid() bool {
	switch s {
	case AvailabilityUnknown, AvailabilityHealthy, AvailabilityLimited, AvailabilityUnreachable:
		return true
	default:
		return false
	}
}

func (s AvailabilityState) String() string {
	switch s {
	case AvailabilityHealthy:
		return "healthy"
	case AvailabilityLimited:
		return "limited"
	case AvailabilityUnreachable:
		return "unreachable"
	case AvailabilityUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("availability-state(%d)", int(s))
	}
}

// Serviceable reports whether requests can be made right now.
//
// Only Healthy answers true. Unknown is NOT optimistic here — a caller that
// treated "we could not tell" as "go ahead" would be acting on a proxy signal,
// and the fail-open behaviour invariant 2 of docs/architecture.md describes for
// an ABSENT limit-state file is a decision that belongs to the limit plane
// reading that file, not to a verdict that already exists and says unknown.
func (a Availability) Serviceable() bool { return a.State == AvailabilityHealthy }

// Validate refuses a verdict whose state and evidence contradict each other.
//
// CheckAvailability calls it on every plugin answer, so a vendor cannot report
// health it did not observe, a limit with no clear time, or a bare state with
// nothing behind it. A verdict is acted on downstream; one that is internally
// inconsistent is worse than no verdict, because it looks like an answer.
func (a Availability) Validate() error {
	if !a.State.Valid() {
		return fmt.Errorf("%w: %s is not a declared availability state", ErrAvailabilityInvalid, a.State)
	}
	for i, source := range a.Checked {
		if strings.TrimSpace(source) == "" {
			return fmt.Errorf("%w: checked source %d is blank", ErrAvailabilityInvalid, i)
		}
	}
	for i, observation := range a.Observed {
		if strings.TrimSpace(observation.Source) == "" {
			return fmt.Errorf("%w: observation %d names no source", ErrAvailabilityInvalid, i)
		}
		if strings.TrimSpace(observation.Detail) == "" {
			return fmt.Errorf("%w: observation %d from %q records nothing", ErrAvailabilityInvalid, i, observation.Source)
		}
	}
	for i, failure := range a.Failures {
		if strings.TrimSpace(failure.Source) == "" {
			return fmt.Errorf("%w: read failure %d names no source", ErrAvailabilityInvalid, i)
		}
		if strings.TrimSpace(failure.Reason) == "" {
			return fmt.Errorf("%w: read failure %d on %q gives no reason", ErrAvailabilityInvalid, i, failure.Source)
		}
	}
	if a.State != AvailabilityLimited && !a.Until.IsZero() {
		return fmt.Errorf("%w: a %s verdict carries a clear time of %s; only a limited verdict has one, and a caller scheduling a retry off this would be acting on a guess",
			ErrAvailabilityInvalid, a.State, a.Until.UTC().Format(time.RFC3339))
	}
	switch a.State {
	case AvailabilityHealthy:
		if len(a.Checked) == 0 {
			return fmt.Errorf("%w: healthy with nothing checked; a health claim that read no source is a guess", ErrAvailabilityInvalid)
		}
		if len(a.Failures) > 0 {
			return fmt.Errorf("%w: healthy while %q could not be read; a verdict resting on a failed read is unknown, not healthy",
				ErrAvailabilityInvalid, a.Failures[0].Source)
		}
	case AvailabilityLimited:
		if a.Until.IsZero() {
			return fmt.Errorf("%w: limited with no clear time; the caller's next question is when, and a limit that cannot answer it is unknown", ErrAvailabilityInvalid)
		}
		if len(a.Observed) == 0 {
			return fmt.Errorf("%w: limited with no observation; the clear time has to come from something that was read", ErrAvailabilityInvalid)
		}
	case AvailabilityUnreachable:
		if len(a.Observed) == 0 && len(a.Failures) == 0 {
			return fmt.Errorf("%w: unreachable with neither an observation nor a read failure behind it", ErrAvailabilityInvalid)
		}
	}
	return nil
}

// Healthy builds the verdict for a vendor that answered, naming the sources
// that were read. It refuses nothing: Validate is what refuses, and a caller
// that passes no sources gets a verdict that fails it.
func Healthy(checked ...string) Availability {
	return Availability{State: AvailabilityHealthy, Checked: checked}
}

// LimitedUntil builds a limit verdict that clears at until.
func LimitedUntil(until time.Time, observed ...Observation) Availability {
	checked := make([]string, 0, len(observed))
	for _, observation := range observed {
		checked = append(checked, observation.Source)
	}
	return Availability{State: AvailabilityLimited, Until: until, Checked: checked, Observed: observed}
}

// Unreachable builds the verdict for a vendor that could not be reached.
func Unreachable(observed []Observation, failures []ReadFailure) Availability {
	checked := make([]string, 0, len(observed)+len(failures))
	for _, observation := range observed {
		checked = append(checked, observation.Source)
	}
	for _, failure := range failures {
		checked = append(checked, failure.Source)
	}
	return Availability{State: AvailabilityUnreachable, Checked: checked, Observed: observed, Failures: failures}
}

// UnknownAfterCheck is the checked-and-empty verdict: these sources were read
// and none of them established anything. It is the honest answer the muse
// runtime's broker is recorded with in the extraction source, and it is a
// different fact from Unchecked.
func UnknownAfterCheck(checked ...string) Availability {
	return Availability{State: AvailabilityUnknown, Checked: checked}
}

// Unchecked is the verdict for a question nobody asked: no source was read.
// A caller must be able to tell it from UnknownAfterCheck, because "we looked
// and found nothing" and "we never looked" lead to different next moves.
func Unchecked() Availability { return Availability{State: AvailabilityUnknown} }
