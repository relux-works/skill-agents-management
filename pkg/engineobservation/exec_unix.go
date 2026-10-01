//go:build unix

package engineobservation

import (
	"os/exec"
	"syscall"
)

// setObservationProcessGroup puts the status subprocess in its own process
// group so a deadline kill reaches its descendants too. The observation
// command can fork children that inherit its stdout pipe; killing only the
// direct child would leave Run blocked on the copy goroutine until every
// descendant exits.
func setObservationProcessGroup(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
}

// killObservationProcessGroup SIGKILLs the process group led by pid. The
// status subprocess is the group leader (see setObservationProcessGroup),
// so this reaches the subprocess and every descendant holding its pipes.
// Best-effort by design: a group that already exited reports ESRCH, and
// WaitDelay bounds Wait regardless. Only this observation command group is
// killed; the managed engine process lives in its own group and is never
// signalled here.
func killObservationProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
