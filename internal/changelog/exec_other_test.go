//go:build !unix

package changelog_test

import "os/exec"

const changelogScopesEnv = "CHANGELOG_TEST_PROCESS_SCOPES"

// Non-Unix is a stated bound: direct-child kill and WaitDelay, without Unix
// process-tree cleanup. The Unix regressions explicitly skip this platform.
func prepareChangelogProcessScope(cmd *exec.Cmd, attestation string) error { return nil }
func runChangelogRegistered(cmd *exec.Cmd) error                           { return cmd.Run() }
func setChangelogChildProcessGroup(cmd *exec.Cmd)                          {}
func killChangelogChildProcessGroup(pid int)                               {}
func cleanupChangelogChildProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
func forgetChangelogProcessScope(cmd *exec.Cmd) {}
func changelogProcessGroupsSupported() bool     { return false }

func changelogProcessScopeFailure(cmd *exec.Cmd) error { return nil }
