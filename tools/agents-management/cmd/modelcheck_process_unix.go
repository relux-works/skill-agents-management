//go:build unix

package cmd

import (
	"os/exec"
	"syscall"
)

// setModelCheckProcessGroup puts the Pi child in its own process group so a
// deadline kill reaches Pi's descendants too. Pi routinely spawns tool
// children that inherit its stdout pipe; killing only the direct child would
// leave Wait blocked on the copy goroutine until every descendant exits.
func setModelCheckProcessGroup(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
}

// killModelCheckProcessGroup SIGKILLs the process group led by pid. The
// child is the group leader (see setModelCheckProcessGroup), so this reaches
// the child and every descendant holding its pipes. Best-effort by design:
// a group that already exited reports ESRCH, and WaitDelay bounds Wait
// regardless.
func killModelCheckProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
