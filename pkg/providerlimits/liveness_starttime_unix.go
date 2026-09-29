//go:build unix

package providerlimits

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// processStartTimeLayout matches the second-resolution value printed by
// `ps -o lstart=`. It is retained for the last-resort compatibility reader and
// for tests that compare the kernel value with the former implementation.
const processStartTimeLayout = "Mon Jan _2 15:04:05 2006"

const (
	processStartTimeFallbackTimeout  = 3 * time.Second
	processStartTimeFallbackAttempts = 2
	linuxATClockTicks                = uint64(17)
)

var errKernelStartTimeUnavailable = errors.New("providerlimits: kernel process start-time reader unavailable")

type kernelStartTimeReader func(pid int) (time.Time, error)
type psStartTimeReader func(ctx context.Context, pid int) (time.Time, error)

// ProcessStartTime reads the kernel process start time on Darwin and Linux. The
// ps reader is only reached if the kernel reader itself fails for a reason other
// than a missing pid or a permission denial.
//
// Resolution is one second, matching the former ps-derived value used by both
// the claim and the later liveness check. Callers must treat an error as "reuse
// cannot be ruled out", never as "assume the same process".
func ProcessStartTime(pid int) (time.Time, error) {
	return processStartTimeWithReaders(pid, kernelProcessStartTime, readPSStartTime)
}

// processStartTimeWithReaders keeps the failure policy shared by the native
// entry point and deterministic tests. A missing process and a permission
// denial are terminal facts; other kernel-read failures may try the bounded ps
// compatibility reader.
func processStartTimeWithReaders(
	pid int,
	kernel kernelStartTimeReader,
	ps psStartTimeReader,
) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, newProcessStartTimeError(pid, ProcessStartTimeInvalidPID, "invalid_pid", 0, nil)
	}
	var started time.Time
	var kernelErr error
	if kernel == nil {
		kernelErr = errKernelStartTimeUnavailable
	} else {
		started, kernelErr = kernel(pid)
	}
	if kernelErr == nil && !started.IsZero() {
		return normalizeProcessStartTime(started), nil
	}
	if kernelErr == nil {
		kernelErr = errors.New("kernel returned a zero start time")
	}

	switch classifyProcessStartTimeReadError(kernelErr) {
	case ProcessStartTimeMissingPID:
		return time.Time{}, newProcessStartTimeError(pid, ProcessStartTimeMissingPID, "kernel_missing_pid", 0, kernelErr)
	case ProcessStartTimePermissionDenied:
		return time.Time{}, newProcessStartTimeError(pid, ProcessStartTimePermissionDenied, "kernel_permission_denied", 0, kernelErr)
	}

	psTime, attempts, psErr, transient := readPSStartTimeWithRetries(pid, ps)
	if psErr == nil && !psTime.IsZero() {
		return normalizeProcessStartTime(psTime), nil
	}
	if psErr == nil {
		psErr = errors.New("ps returned a zero start time")
	}
	joined := errors.Join(kernelErr, psErr)
	if transient {
		return time.Time{}, newProcessStartTimeError(pid, ProcessStartTimeFallbackTransient, "ps_fallback_transient", attempts, joined)
	}
	return time.Time{}, newProcessStartTimeError(pid, ProcessStartTimeKernelReadFailure, "kernel_read_failure", attempts, joined)
}

func normalizeProcessStartTime(started time.Time) time.Time {
	return started.In(time.Local).Truncate(time.Second)
}

func classifyProcessStartTimeReadError(err error) ProcessStartTimeErrorKind {
	return classifyProcessStartTimeReadErrorWithMissingPIDMatcher(err, func(readErr error) bool {
		return errors.Is(readErr, fs.ErrNotExist) || errors.Is(readErr, syscall.ESRCH)
	})
}

func classifyProcessStartTimeReadErrorWithMissingPIDMatcher(err error, matchesMissingPID func(error) bool) ProcessStartTimeErrorKind {
	switch {
	case matchesMissingPID != nil && matchesMissingPID(err):
		return ProcessStartTimeMissingPID
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return ProcessStartTimePermissionDenied
	default:
		return ProcessStartTimeKernelReadFailure
	}
}

// readPSStartTimeWithRetries retries only transient ps failures. A timed-out
// command and a ps process killed with SIGKILL can be transient under host load;
// ordinary command or parse failures remain kernel-read errors to their caller.
func readPSStartTimeWithRetries(pid int, reader psStartTimeReader) (time.Time, int, error, bool) {
	var failures []error
	sawTransient := false
	for attempt := 1; attempt <= processStartTimeFallbackAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), processStartTimeFallbackTimeout)
		var started time.Time
		var err error
		if reader == nil {
			err = errKernelStartTimeUnavailable
		} else {
			started, err = reader(ctx, pid)
		}
		transient := isTransientPSFailure(ctx, err)
		cancel()
		if err == nil && !started.IsZero() {
			return started, attempt, nil, false
		}
		if err == nil {
			err = errors.New("ps returned a zero start time")
		}
		failures = append(failures, err)
		sawTransient = sawTransient || transient
		if !transient || attempt == processStartTimeFallbackAttempts {
			return time.Time{}, attempt, errors.Join(failures...), sawTransient
		}
		time.Sleep(10 * time.Millisecond)
	}
	return time.Time{}, processStartTimeFallbackAttempts, errors.Join(failures...), sawTransient
}

func isTransientPSFailure(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}

func readPSStartTime(ctx context.Context, pid int) (time.Time, error) {
	output, err := exec.CommandContext(ctx, "ps", "-p", fmt.Sprint(pid), "-o", "lstart=").Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return time.Time{}, context.DeadlineExceeded
		}
		return time.Time{}, err
	}
	return parsePSStartTime(output)
}

func parsePSStartTime(output []byte) (time.Time, error) {
	field := strings.Join(strings.Fields(strings.TrimSpace(string(output))), " ")
	if field == "" {
		return time.Time{}, errors.New("ps reported no start time")
	}
	// `ps` collapses the day-of-month with a leading space, which Fields has
	// already normalised; parse with the underscore-padded layout either way.
	started, err := time.ParseInLocation(processStartTimeLayout, field, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing ps start time %q: %w", field, err)
	}
	return started, nil
}

// parseLinuxProcStatStartTicks reads field 22 after the command name. The
// command is parenthesized and may itself contain spaces or ')' bytes, so the
// final ')' is the only safe delimiter before the fixed fields.
func parseLinuxProcStatStartTicks(data []byte) (int64, error) {
	closeParen := bytes.LastIndexByte(data, ')')
	if closeParen < 0 || closeParen+1 >= len(data) {
		return 0, errors.New("malformed /proc/<pid>/stat command field")
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	const startTimeFieldAfterState = 19
	if len(fields) <= startTimeFieldAfterState {
		return 0, fmt.Errorf("malformed /proc/<pid>/stat: got %d fields after command", len(fields))
	}
	ticks, err := parseNonNegativeInt64(fields[startTimeFieldAfterState])
	if err != nil {
		return 0, fmt.Errorf("parsing /proc/<pid>/stat starttime: %w", err)
	}
	return ticks, nil
}

func parseLinuxProcBootTime(data []byte) (int64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "btime" {
			continue
		}
		if len(fields) != 2 {
			return 0, fmt.Errorf("malformed /proc/stat btime row %q", line)
		}
		seconds, err := parseNonNegativeInt64(fields[1])
		if err != nil {
			return 0, fmt.Errorf("parsing /proc/stat btime: %w", err)
		}
		return seconds, nil
	}
	return 0, errors.New("/proc/stat has no btime row")
}

// parseLinuxAuxvClockTicks reads AT_CLKTCK from the kernel-provided auxiliary
// vector. Linux documents this as the clock-tick frequency used by proc stat;
// it is commonly 100, but the value is architecture-dependent.
func parseLinuxAuxvClockTicks(data []byte) (int64, error) {
	wordSize := strconv.IntSize / 8
	if wordSize != 4 && wordSize != 8 {
		return 0, fmt.Errorf("unsupported Linux auxiliary-vector word size %d", wordSize)
	}
	entrySize := 2 * wordSize
	if len(data)%entrySize != 0 {
		return 0, fmt.Errorf("malformed /proc/self/auxv: %d bytes is not a multiple of %d", len(data), entrySize)
	}
	for offset := 0; offset < len(data); offset += entrySize {
		kind := linuxAuxvWord(data[offset : offset+wordSize])
		value := linuxAuxvWord(data[offset+wordSize : offset+entrySize])
		if kind == 0 { // AT_NULL terminates the vector.
			break
		}
		if kind != linuxATClockTicks {
			continue
		}
		clockTicks := int64(value)
		if clockTicks <= 0 {
			return 0, fmt.Errorf("invalid AT_CLKTCK value %d", value)
		}
		return clockTicks, nil
	}
	return 0, errors.New("/proc/self/auxv has no AT_CLKTCK entry")
}

func linuxAuxvWord(data []byte) uint64 {
	if len(data) == 4 {
		return uint64(binary.NativeEndian.Uint32(data))
	}
	return binary.NativeEndian.Uint64(data)
}

func linuxProcessStartTimeFromProcData(processStat, procStat []byte, clockTicksPerSecond int64) (time.Time, error) {
	ticks, err := parseLinuxProcStatStartTicks(processStat)
	if err != nil {
		return time.Time{}, err
	}
	bootTime, err := parseLinuxProcBootTime(procStat)
	if err != nil {
		return time.Time{}, err
	}
	if clockTicksPerSecond <= 0 {
		return time.Time{}, fmt.Errorf("invalid Linux clock-tick frequency %d", clockTicksPerSecond)
	}
	return time.Unix(bootTime+ticks/clockTicksPerSecond, 0).In(time.Local), nil
}

func parseNonNegativeInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, fmt.Errorf("negative value %d", parsed)
	}
	return parsed, nil
}
