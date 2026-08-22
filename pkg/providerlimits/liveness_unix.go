//go:build unix

package providerlimits

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// processAlive reports whether a pid names a live process. EPERM means the
// process exists and belongs to another user, which is still alive.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return true
	case errors.Is(err, syscall.EPERM):
		return true
	default:
		return false
	}
}

// processStartTimeLayout matches `ps -o lstart=` on both Darwin and Linux.
const processStartTimeLayout = "Mon Jan _2 15:04:05 2006"

// ProcessStartTime reads a process's start time, which is what makes pid reuse
// detectable. Resolution is one second, and both the claim and the later check
// go through this same reader, so the comparison is consistent.
//
// An error means the start time is unreadable. Callers must treat that as "reuse
// cannot be ruled out", never as "assume the same process".
func ProcessStartTime(pid int) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, fmt.Errorf("providerlimits: pid %d is not a process", pid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-p", fmt.Sprint(pid), "-o", "lstart=").Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("providerlimits: reading start time of pid %d: %w", pid, err)
	}
	field := strings.Join(strings.Fields(strings.TrimSpace(string(output))), " ")
	if field == "" {
		return time.Time{}, fmt.Errorf("providerlimits: pid %d reported no start time", pid)
	}
	// `ps` collapses the day-of-month with a leading space, which Fields already
	// normalised; parse with the underscore-padded layout either way.
	started, err := time.ParseInLocation(processStartTimeLayout, field, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("providerlimits: parsing start time %q of pid %d: %w", field, pid, err)
	}
	return started, nil
}
