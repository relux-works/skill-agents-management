//go:build !unix

package cmd

import (
	"os/exec"
)

// setModelCheckProcessGroup has no process-group isolation off unix; the
// deadline Cancel kills the direct child and WaitDelay bounds Wait. A
// descendant that outlives the child can hold the stdout pipe until
// WaitDelay: a documented platform bound, not a hang.
func setModelCheckProcessGroup(command *exec.Cmd) {}

// killModelCheckProcessGroup is a no-op off unix; the direct child kill in
// the deadline Cancel is the only signal available.
func killModelCheckProcessGroup(pid int) {}
