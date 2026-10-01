//go:build unix

package changelog_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Groups remain local cleanup boundaries, but every nested group is registered
// with ALL ancestor supervisors. A shared lock serializes Start+registration
// against cleanup. A closing ancestor refuses launches, so a child cannot
// escape between the final registry read and the kill.
const changelogScopesEnv = "CHANGELOG_TEST_PROCESS_SCOPES"

type changelogProcessScope struct {
	paths     []string
	own, lock string
}

var changelogProcessScopes sync.Map        // *exec.Cmd -> *changelogProcessScope
var changelogProcessScopeFailures sync.Map // *exec.Cmd -> error (sticky)

func prepareChangelogProcessScope(cmd *exec.Cmd, attestation string) error {
	var ancestors []string
	if raw := os.Getenv(changelogScopesEnv); raw != "" {
		if err := json.Unmarshal([]byte(raw), &ancestors); err != nil || len(ancestors) == 0 {
			return fmt.Errorf("invalid managed process scopes: %q", raw)
		}
	}
	root := filepath.Dir(attestation)
	for _, path := range ancestors {
		if filepath.Dir(path) != root {
			return errors.New("process scope outside managed tree")
		}
	}
	f, err := os.CreateTemp(root, "groups-")
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	scope := &changelogProcessScope{paths: append(ancestors, f.Name()), own: f.Name(), lock: filepath.Join(root, "launch-lock")}
	data, _ := json.Marshal(scope.paths)
	cmd.Env = changelogSetEnv(cmd.Env, changelogScopesEnv, string(data))
	changelogProcessScopes.Store(cmd, scope)
	return nil
}

func (s *changelogProcessScope) locked(action func() error) error {
	f, err := os.OpenFile(s.lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return action()
}

func runChangelogRegistered(cmd *exec.Cmd) error {
	value, ok := changelogProcessScopes.Load(cmd)
	if !ok {
		return errors.New("child has no process supervisor")
	}
	scope := value.(*changelogProcessScope)
	err := scope.locked(func() error {
		for _, path := range scope.paths {
			if _, err := os.Stat(path + ".closed"); err == nil {
				return errors.New("ancestor process scope closed")
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		for _, path := range scope.paths {
			// Register while holding the tree lock; cleanup cannot miss an admitted
			// group, even if the launched child immediately launches another one.
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				killChangelogChildProcessGroup(cmd.Process.Pid)
				return err
			}
			_, err = fmt.Fprintln(f, cmd.Process.Pid)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				killChangelogChildProcessGroup(cmd.Process.Pid)
				return errors.Join(err, closeErr)
			}
		}
		return nil
	})
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Wait()
		}
		return err
	}
	return cmd.Wait()
}

func setChangelogChildProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killChangelogChildProcessGroup(pid int) { _ = syscall.Kill(-pid, syscall.SIGKILL) }

// Every return closes this subtree and kills every registered group in it.
// The caller's own group belongs to its ancestor scope, never to this one,
// so nested cleanup cannot kill the runner that must report the timeout.
func cleanupChangelogChildProcessGroup(cmd *exec.Cmd) {
	value, ok := changelogProcessScopes.Load(cmd)
	if ok {
		scope := value.(*changelogProcessScope)
		err := scope.locked(func() error {
			if err := os.WriteFile(scope.own+".closed", []byte("closed\n"), 0o600); err != nil {
				return err
			}
			data, err := os.ReadFile(scope.own)
			if err != nil {
				return err
			}
			killed := map[string]bool{}
			for _, line := range splitChangelogPIDs(string(data)) {
				pid, err := strconv.Atoi(line)
				if err != nil || pid <= 0 {
					return errors.New("invalid registered process group")
				}
				killChangelogChildProcessGroup(pid)
				killed[line] = true
			}
			// Retire reaped groups from ancestor registries before releasing
			// the lock, so later cleanup cannot target a recycled PID.
			for _, path := range scope.paths[:len(scope.paths)-1] {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var remaining strings.Builder
				for _, line := range splitChangelogPIDs(string(data)) {
					if !killed[line] {
						fmt.Fprintln(&remaining, line)
					}
				}
				if err := os.WriteFile(path, []byte(remaining.String()), 0o600); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			changelogProcessScopeFailures.Store(cmd, err)
		}
	}
	if cmd.Process != nil {
		killChangelogChildProcessGroup(cmd.Process.Pid)
	}
}

func forgetChangelogProcessScope(cmd *exec.Cmd) {
	changelogProcessScopes.Delete(cmd)
	changelogProcessScopeFailures.Delete(cmd)
}
func changelogProcessScopeFailure(cmd *exec.Cmd) error {
	if value, ok := changelogProcessScopeFailures.Load(cmd); ok {
		return fmt.Errorf("process subtree cleanup failed: %w", value.(error))
	}
	return nil
}
func changelogProcessGroupsSupported() bool { return true }
