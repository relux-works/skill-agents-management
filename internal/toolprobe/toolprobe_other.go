//go:build !unix

package toolprobe

import (
	"os/exec"
)

// setProbeProcessGroup has no process-group isolation off unix; the
// teardown kills the direct child only. A descendant holding the probe
// pipes past the drain deadline reports teardown-incomplete instead of
// completing: callers must treat that as no-evidence, never as a result.
func setProbeProcessGroup(command *exec.Cmd) {}

// killProbeChild kills the direct probe child off unix: without a group
// to signal, the runner cannot reach pipe-holding descendants. A
// descendant holding the probe pipes past the drain deadline reports
// teardown-incomplete instead of completing: callers must treat that as
// no-evidence, never as a result.
func killProbeChild(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	_ = command.Process.Kill()
}
