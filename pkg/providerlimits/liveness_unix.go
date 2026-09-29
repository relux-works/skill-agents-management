//go:build unix

package providerlimits

import (
	"errors"
	"os"
	"syscall"
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
