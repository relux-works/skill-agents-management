//go:build !unix

package providerlimits

import (
	"fmt"
	"os"
	"time"
)

// processAlive reports whether a pid names a live process. On platforms without
// the unix signal-0 probe, os.FindProcess is the available signal.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if process == nil {
		return false
	}
	return true
}

// ProcessStartTime is unavailable on this platform. Callers treat an error as
// "reuse cannot be ruled out", which costs at most one extra probe.
func ProcessStartTime(pid int) (time.Time, error) {
	return time.Time{}, fmt.Errorf("providerlimits: process start time is not readable on this platform (pid %d)", pid)
}
