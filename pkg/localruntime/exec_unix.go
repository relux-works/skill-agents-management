//go:build unix

package localruntime

import (
	"os/exec"
	"syscall"
)

// setStatusProcessGroup puts the status subprocess in its own process group
// so a deadline kill reaches its descendants too. The status command can
// fork children that inherit its stdout pipe; killing only the direct child
// would leave Run blocked on the copy goroutine until every descendant
// exits.
func setStatusProcessGroup(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
}

// killStatusProcessGroup SIGKILLs the process group led by pid. The status
// subprocess is the group leader (see setStatusProcessGroup), so this
// reaches the subprocess and every descendant holding its pipes.
// Best-effort by design: a group that already exited reports ESRCH, and
// WaitDelay bounds Wait regardless. Only this status command group is
// killed; the runtime process being reported on lives in its own group and
// is never signalled here — this package still never starts, stops or
// signals the process it reports on.
func killStatusProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
