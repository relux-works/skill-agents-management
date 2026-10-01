//go:build !unix

package engineobservation

import (
	"os/exec"
)

// setObservationProcessGroup has no process-group isolation off unix; the
// deadline Cancel kills the direct child and WaitDelay bounds Wait. A
// descendant that outlives the child can hold the stdout pipe until
// WaitDelay: a documented platform bound, not a hang.
func setObservationProcessGroup(command *exec.Cmd) {}

// killObservationProcessGroup is a no-op off unix; the direct child kill in
// the deadline Cancel is the only signal available.
func killObservationProcessGroup(pid int) {}
