package providerlimits

import (
	"errors"
	"fmt"
)

// ProcessStartTimeErrorKind classifies why a process start-time lookup failed.
type ProcessStartTimeErrorKind string

const (
	ProcessStartTimeInvalidPID        ProcessStartTimeErrorKind = "invalid_pid"
	ProcessStartTimeMissingPID        ProcessStartTimeErrorKind = "missing_pid"
	ProcessStartTimePermissionDenied  ProcessStartTimeErrorKind = "permission_denied"
	ProcessStartTimeKernelReadFailure ProcessStartTimeErrorKind = "kernel_read_failure"
	ProcessStartTimeFallbackTransient ProcessStartTimeErrorKind = "fallback_transient"
)

// ErrProcessStartTimeTransient matches a fallback failure that can be retried.
var ErrProcessStartTimeTransient = errors.New("providerlimits: transient process start-time lookup failure")

// ProcessStartTimeError preserves the failure class and operating-system cause
// without changing the time value or state format used by callers.
type ProcessStartTimeError struct {
	PID      int
	Kind     ProcessStartTimeErrorKind
	Err      error
	site     string
	attempts int
}

func (e *ProcessStartTimeError) Error() string {
	if e == nil {
		return "providerlimits: nil process start-time error"
	}
	switch e.Kind {
	case ProcessStartTimeInvalidPID:
		return fmt.Sprintf("providerlimits: pid %d is not a process", e.PID)
	case ProcessStartTimeMissingPID:
		if e.Err != nil {
			return fmt.Sprintf("providerlimits: pid %d does not exist: %v", e.PID, e.Err)
		}
		return fmt.Sprintf("providerlimits: pid %d does not exist", e.PID)
	case ProcessStartTimePermissionDenied:
		return fmt.Sprintf("providerlimits: permission denied reading start time of pid %d: %v", e.PID, e.Err)
	case ProcessStartTimeFallbackTransient:
		return fmt.Sprintf("providerlimits: transient ps fallback for pid %d failed after %d attempt(s): %v", e.PID, e.attempts, e.Err)
	default:
		return fmt.Sprintf("providerlimits: reading start time of pid %d: %v", e.PID, e.Err)
	}
}

func (e *ProcessStartTimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is lets callers identify fallback failures that are safe to retry.
func (e *ProcessStartTimeError) Is(target error) bool {
	return target == ErrProcessStartTimeTransient && e != nil && e.Kind == ProcessStartTimeFallbackTransient
}

// Temporary reports whether retrying the failed fallback may succeed.
func (e *ProcessStartTimeError) Temporary() bool {
	return e != nil && e.Kind == ProcessStartTimeFallbackTransient
}

func newProcessStartTimeError(pid int, kind ProcessStartTimeErrorKind, site string, attempts int, cause error) *ProcessStartTimeError {
	return &ProcessStartTimeError{PID: pid, Kind: kind, Err: cause, site: site, attempts: attempts}
}
