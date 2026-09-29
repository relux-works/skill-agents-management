//go:build darwin

package providerlimits

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func kernelProcessStartTime(pid int) (time.Time, error) {
	return kernelProcessStartTimeWithReaders(pid, unix.SysctlRaw, unix.SysctlKinfoProc, unix.Kill)
}

type darwinSysctlRawReader func(name string, args ...int) ([]byte, error)
type darwinSysctlKinfoProcReader func(name string, args ...int) (*unix.KinfoProc, error)
type darwinPIDProbe func(pid int, signal syscall.Signal) error

func kernelProcessStartTimeWithReaders(
	pid int,
	rawReader darwinSysctlRawReader,
	kinfoReader darwinSysctlKinfoProcReader,
	probe darwinPIDProbe,
) (time.Time, error) {
	data, err := rawReader("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, darwinSysctlReadError(pid, err, probe)
	}
	if len(data) == 0 {
		return time.Time{}, darwinEmptySysctlResultError(pid, probe)
	}
	info, err := kinfoReader("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, darwinSysctlReadError(pid, err, probe)
	}
	if info == nil || int(info.Proc.P_pid) != pid {
		return time.Time{}, syscall.ESRCH
	}
	started := info.Proc.P_starttime
	if started.Sec < 0 || started.Usec < 0 || started.Usec >= 1_000_000 {
		return time.Time{}, fmt.Errorf("sysctl kern.proc.pid returned invalid start time for pid %d", pid)
	}
	return time.Unix(started.Sec, int64(started.Usec)*int64(time.Microsecond)).In(time.Local), nil
}

func darwinSysctlReadError(pid int, sysctlErr error, probe darwinPIDProbe) error {
	if errors.Is(sysctlErr, syscall.EIO) && probe != nil {
		probeErr := probe(pid, 0)
		if errors.Is(probeErr, syscall.ESRCH) {
			return syscall.ESRCH
		}
		if errors.Is(probeErr, syscall.EPERM) || errors.Is(probeErr, syscall.EACCES) {
			return probeErr
		}
	}
	return fmt.Errorf("sysctl kern.proc.pid for pid %d: %w", pid, sysctlErr)
}

func darwinEmptySysctlResultError(pid int, probe darwinPIDProbe) error {
	if probe == nil {
		return fmt.Errorf("sysctl kern.proc.pid returned no data for pid %d: %w", pid, syscall.EIO)
	}
	probeErr := probe(pid, 0)
	switch {
	case errors.Is(probeErr, syscall.ESRCH):
		return syscall.ESRCH
	case errors.Is(probeErr, syscall.EPERM), errors.Is(probeErr, syscall.EACCES):
		return probeErr
	case probeErr == nil:
		return fmt.Errorf("sysctl kern.proc.pid returned no data for live pid %d: %w", pid, syscall.EIO)
	default:
		return fmt.Errorf("sysctl kern.proc.pid returned no data for pid %d (%v): %w", pid, probeErr, syscall.EIO)
	}
}
