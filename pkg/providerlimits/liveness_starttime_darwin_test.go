//go:build darwin

package providerlimits

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
)

func TestDarwinStartTimeIdentityBytesNarrowingMutantIsKilled(t *testing.T) {
	assertProcessStartTimeSourceNarrowingMutantKilled(
		t,
		"pkg/providerlimits/liveness_starttime_darwin.go",
		"return time.Unix(started.Sec, int64(started.Usec)*int64(time.Microsecond)).In(time.Local), nil",
		"return time.Unix(started.Sec+1, int64(started.Usec)*int64(time.Microsecond)).In(time.Local), nil",
		"TestProcessStartTimeMatchesLegacyPSBytes",
	)
}

func TestDarwinMissingPIDMappingNarrowingMutantIsKilled(t *testing.T) {
	assertProcessStartTimeSourceNarrowingMutantKilled(
		t,
		"pkg/providerlimits/liveness_starttime_darwin.go",
		"case errors.Is(probeErr, syscall.ESRCH):",
		"case errors.Is(probeErr, syscall.ESRCH) && pid%2 == 0:",
		"TestDarwinMissingPIDMappingDoesNotDependOnPIDParity",
	)
}

func TestDarwinProcessStartTimeMissingPIDIsTypedAndDoesNotForkPS(t *testing.T) {
	child := exec.Command("/bin/sh", "-c", "exit 0")
	if err := child.Start(); err != nil {
		t.Fatalf("start short-lived child: %v", err)
	}
	pid := child.Process.Pid
	if err := child.Wait(); err != nil {
		t.Fatalf("wait for short-lived child: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("reaped child pid %d is still addressable: %v", pid, err)
	}

	psDir := t.TempDir()
	marker := filepath.Join(psDir, "ps-was-run")
	psPath := filepath.Join(psDir, "ps")
	script := []byte("#!/bin/sh\nprintf invoked > \"$PROCESS_START_TIME_PS_MARKER\"\nexit 91\n")
	if err := execfixture.WriteFile(psPath, script, 0o755); err != nil {
		t.Fatalf("write ps sentinel: %v", err)
	}
	t.Setenv("PATH", psDir)
	t.Setenv("PROCESS_START_TIME_PS_MARKER", marker)

	_, err := ProcessStartTime(pid)
	requireProcessStartTimeError(t, err, "kernel_missing_pid", ProcessStartTimeMissingPID)
	var typed *ProcessStartTimeError
	if !errors.As(err, &typed) || !errors.Is(typed, syscall.ESRCH) {
		t.Fatalf("dead pid error cause = %v, want ESRCH", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("ps fallback ran for dead pid %d; marker stat error = %v", pid, statErr)
	}

	// This is the narrowing mutant that drops ESRCH from the classifier's
	// missing-pid set. The real production lookup above supplies its ESRCH cause;
	// the named error oracle must reject the narrowed classification.
	mutantKind := classifyProcessStartTimeReadErrorWithMissingPIDMatcher(typed.Err, func(readErr error) bool {
		return errors.Is(readErr, fs.ErrNotExist)
	})
	mutantErr := newProcessStartTimeError(pid, mutantKind, "kernel_missing_pid", 0, typed.Err)
	if mismatch := processStartTimeErrorMismatch(mutantErr, "kernel_missing_pid", ProcessStartTimeMissingPID); mismatch == nil {
		t.Fatal("narrowing mutant that omits ESRCH survived TestDarwinProcessStartTimeMissingPIDIsTypedAndDoesNotForkPS")
	}
}

func TestDarwinMissingPIDMappingDoesNotDependOnPIDParity(t *testing.T) {
	psDir := t.TempDir()
	marker := filepath.Join(psDir, "ps-was-run")
	psPath := filepath.Join(psDir, "ps")
	script := []byte("#!/bin/sh\nprintf invoked > \"$PROCESS_START_TIME_PS_MARKER\"\nexit 91\n")
	if err := execfixture.WriteFile(psPath, script, 0o755); err != nil {
		t.Fatalf("write ps sentinel: %v", err)
	}
	t.Setenv("PATH", psDir)
	t.Setenv("PROCESS_START_TIME_PS_MARKER", marker)

	seenParity := [2]bool{}
	for attempts := 0; attempts < 64 && (!seenParity[0] || !seenParity[1]); attempts++ {
		child := exec.Command("/bin/sh", "-c", "exit 0")
		if err := child.Start(); err != nil {
			t.Fatalf("start short-lived child: %v", err)
		}
		pid := child.Process.Pid
		if err := child.Wait(); err != nil {
			t.Fatalf("wait for short-lived child %d: %v", pid, err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("reaped child pid %d is still addressable: %v", pid, err)
		}

		_, err := ProcessStartTime(pid)
		requireProcessStartTimeError(t, err, "kernel_missing_pid", ProcessStartTimeMissingPID)
		seenParity[pid%2] = true
	}
	if !seenParity[0] || !seenParity[1] {
		t.Fatalf("did not exercise dead pids of both parities: seen=%v", seenParity)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("ps fallback ran for a dead pid; marker stat error = %v", statErr)
	}
}
