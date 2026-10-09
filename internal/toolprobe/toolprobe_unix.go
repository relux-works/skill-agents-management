//go:build unix

package toolprobe

import (
	"os/exec"
	"syscall"
)

// setProbeProcessGroup puts the probe subprocess in its own process group
// so a deadline kill reaches its descendants too. A probe child can fork
// holders that inherit its stdout pipe; killing only the direct child
// would leave the exec copy — and therefore Wait — blocked until every
// descendant exits.
func setProbeProcessGroup(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
}

// killProbeChild SIGKILLs the probe child's process group. The probe
// subprocess is the group leader (see setProbeProcessGroup), so this
// reaches the subprocess and every descendant holding its pipes.
// Best-effort by design: a group that already exited reports ESRCH.
// Only this probe's group is ever signalled.
func killProbeChild(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}
