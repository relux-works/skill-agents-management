//go:build unix

package providerlimits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
)

func TestParseLinuxProcStatStartTicksUsesFieldAfterCommand(t *testing.T) {
	fields := make([]string, 20)
	fields[0] = "S"
	for i := 1; i < len(fields); i++ {
		fields[i] = "1"
	}
	fields[19] = "987654"
	stat := []byte("321 (worker (child) with spaces) " + strings.Join(fields, " "))

	got, err := parseLinuxProcStatStartTicks(stat)
	if err != nil {
		t.Fatalf("parseLinuxProcStatStartTicks: %v", err)
	}
	if got != 987654 {
		t.Fatalf("starttime ticks = %d, want 987654", got)
	}
}

func TestLinuxProcStatField22NarrowingMutantIsKilled(t *testing.T) {
	assertProcessStartTimeSourceNarrowingMutantKilled(
		t,
		"pkg/providerlimits/liveness_starttime_unix.go",
		"const startTimeFieldAfterState = 19",
		"const startTimeFieldAfterState = 18",
		"TestParseLinuxProcStatStartTicksUsesFieldAfterCommand",
	)
}

func TestLegacyLockIdentityFormatNarrowingMutantIsKilled(t *testing.T) {
	assertProcessStartTimeSourceNarrowingMutantKilled(
		t,
		"pkg/providerlimits/state.go",
		"PIDStartTime time.Time `json:\"pid_start_time,omitzero\"`\n\tAcquiredAt",
		"PIDStartTime time.Time `json:\"pid_start_mismatched,omitzero\"`\n\tAcquiredAt",
		"TestLegacyPSRecordedLockStartTimeRemainsByteCompatible",
	)
}

func TestParseLinuxProcBootTimeReadsTheBtimeRow(t *testing.T) {
	data := []byte("cpu  10 2 3 4\nbtime 1750000000\nprocesses 99\n")
	got, err := parseLinuxProcBootTime(data)
	if err != nil {
		t.Fatalf("parseLinuxProcBootTime: %v", err)
	}
	if got != 1_750_000_000 {
		t.Fatalf("boot time = %d, want 1750000000", got)
	}
}

func TestParseLinuxAuxvClockTicksReadsATCLKTCK(t *testing.T) {
	wordSize := strconv.IntSize / 8
	entrySize := 2 * wordSize
	data := make([]byte, 2*entrySize)
	writeWord := func(dst []byte, value uint64) {
		t.Helper()
		if wordSize == 4 {
			binary.NativeEndian.PutUint32(dst, uint32(value))
			return
		}
		binary.NativeEndian.PutUint64(dst, value)
	}
	writeWord(data[0:wordSize], 6) // AT_PAGESZ
	writeWord(data[wordSize:entrySize], 4096)
	writeWord(data[entrySize:entrySize+wordSize], linuxATClockTicks)
	writeWord(data[entrySize+wordSize:], 250)

	got, err := parseLinuxAuxvClockTicks(data)
	if err != nil {
		t.Fatalf("parseLinuxAuxvClockTicks: %v", err)
	}
	if got != 250 {
		t.Fatalf("AT_CLKTCK = %d, want 250", got)
	}
}

func TestLinuxKernelStartTimeUsesBootTimeAndUserHZ(t *testing.T) {
	fields := make([]string, 20)
	fields[0] = "S"
	for i := 1; i < len(fields); i++ {
		fields[i] = "1"
	}
	fields[19] = "987654"
	processStat := []byte("321 (test process) " + strings.Join(fields, " "))
	procStat := []byte("cpu 1 2 3 4\nbtime 1750000000\n")

	got, err := linuxProcessStartTimeFromProcData(processStat, procStat, 100)
	if err != nil {
		t.Fatalf("linuxProcessStartTimeFromProcData: %v", err)
	}
	want := time.Unix(1_750_000_000+9_876, 0).In(time.Local)
	if !got.Equal(want) {
		t.Fatalf("Linux start time = %s, want %s", got, want)
	}
}

func TestLinuxKernelStartTimeUsesReportedClockTickRate(t *testing.T) {
	fields := make([]string, 20)
	fields[0] = "S"
	for i := 1; i < len(fields); i++ {
		fields[i] = "1"
	}
	fields[19] = "987654"
	processStat := []byte("321 (test process) " + strings.Join(fields, " "))
	procStat := []byte("cpu 1 2 3 4\nbtime 1750000000\n")

	got, err := linuxProcessStartTimeFromProcData(processStat, procStat, 250)
	if err != nil {
		t.Fatalf("linuxProcessStartTimeFromProcData: %v", err)
	}
	want := time.Unix(1_750_000_000+3_950, 0).In(time.Local)
	if !got.Equal(want) {
		t.Fatalf("Linux start time at 250 ticks/sec = %s, want %s", got, want)
	}
}

func TestProcessStartTimeInvalidPIDError(t *testing.T) {
	_, err := ProcessStartTime(0)
	requireProcessStartTimeError(t, err, "invalid_pid", ProcessStartTimeInvalidPID)
}

func TestProcessStartTimeMissingPIDError(t *testing.T) {
	err := driveProcessStartTimeErrorSite("kernel_missing_pid")
	requireProcessStartTimeError(t, err, "kernel_missing_pid", ProcessStartTimeMissingPID)
}

func TestProcessStartTimePermissionDeniedError(t *testing.T) {
	err := driveProcessStartTimeErrorSite("kernel_permission_denied")
	requireProcessStartTimeError(t, err, "kernel_permission_denied", ProcessStartTimePermissionDenied)
}

func TestProcessStartTimeKernelReadFailureError(t *testing.T) {
	err := driveProcessStartTimeErrorSite("kernel_read_failure")
	requireProcessStartTimeError(t, err, "kernel_read_failure", ProcessStartTimeKernelReadFailure)
}

func TestProcessStartTimeFallbackTimeoutIsRetried(t *testing.T) {
	want := time.Date(2026, time.September, 29, 14, 0, 0, 0, time.Local)
	psCalls := 0
	ps := func(context.Context, int) (time.Time, error) {
		psCalls++
		if psCalls == 1 {
			return time.Time{}, context.DeadlineExceeded
		}
		return want, nil
	}
	got, err := processStartTimeWithReaders(
		42,
		func(int) (time.Time, error) { return time.Time{}, errors.New("injected kernel read failure") },
		ps,
	)
	if err != nil {
		t.Fatalf("processStartTimeWithReaders after transient fallback timeout: %v", err)
	}
	if psCalls != 2 {
		t.Fatalf("ps attempts = %d, want 2 after the first timeout", psCalls)
	}
	if gotBytes, wantBytes := marshalStartTime(t, got), marshalStartTime(t, normalizeProcessStartTime(want)); !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("retried fallback value = %s, want %s", gotBytes, wantBytes)
	}
}

func TestProcessStartTimeFallbackTimeoutExhaustionIsTransient(t *testing.T) {
	err := driveProcessStartTimeErrorSite("ps_fallback_transient")
	requireProcessStartTimeError(t, err, "ps_fallback_transient", ProcessStartTimeFallbackTransient)
	if !errors.Is(err, ErrProcessStartTimeTransient) {
		t.Fatalf("fallback timeout is not discoverable as transient: %v", err)
	}
}

func TestProcessStartTimeKernelValueSkipsPSFallback(t *testing.T) {
	want := time.Date(2026, time.September, 29, 14, 1, 2, 345_000_000, time.UTC)
	got, err := processStartTimeWithReaders(
		42,
		func(int) (time.Time, error) { return want, nil },
		func(context.Context, int) (time.Time, error) {
			t.Fatal("ps fallback ran after a successful kernel read")
			return time.Time{}, nil
		},
	)
	if err != nil {
		t.Fatalf("processStartTimeWithReaders: %v", err)
	}
	if !got.Equal(normalizeProcessStartTime(want)) {
		t.Fatalf("kernel value = %s, want %s", got, normalizeProcessStartTime(want))
	}
}

func TestLegacyPSRecordedLockStartTimeRemainsByteCompatible(t *testing.T) {
	// This byte fixture was captured before the kernel reader change by the
	// former ProcessStartTime implementation, then marshaled with lockInfo's
	// existing field tags and order.
	path := filepath.Join("testdata", "legacy-ps-lock.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read legacy ps fixture %s: %v", path, err)
	}
	var recorded lockInfo
	if err := json.Unmarshal(original, &recorded); err != nil {
		t.Fatalf("decode legacy ps lock bytes: %v", err)
	}
	if recorded.PID <= 0 || recorded.PIDStartTime.IsZero() {
		t.Fatalf("legacy ps fixture has no recorded process identity: %+v", recorded)
	}
	reencoded, err := json.Marshal(recorded)
	if err != nil {
		t.Fatalf("re-encode legacy ps lock: %v", err)
	}
	if !bytes.Equal(reencoded, original) {
		t.Fatalf("legacy ps lock bytes changed\n got: %s\nwant: %s", reencoded, original)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(original, &raw); err != nil {
		t.Fatalf("decode legacy ps raw fields: %v", err)
	}
	if !bytes.Equal(raw["pid_start_time"], mustMarshalStartTime(t, recorded.PIDStartTime)) {
		t.Fatalf("legacy pid_start_time token changed: %s", raw["pid_start_time"])
	}
}

func TestProcessStartTimeMatchesLegacyPSBytes(t *testing.T) {
	pid := os.Getpid()
	output, err := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "lstart=").Output()
	if err != nil {
		t.Fatalf("legacy ps query for live pid %d: %v", pid, err)
	}
	legacy, err := parsePSStartTime(output)
	if err != nil {
		t.Fatalf("parse legacy ps value: %v", err)
	}
	got, err := ProcessStartTime(pid)
	if err != nil {
		t.Fatalf("kernel ProcessStartTime(%d): %v", pid, err)
	}
	if gotBytes, legacyBytes := marshalStartTime(t, got), marshalStartTime(t, legacy); !bytes.Equal(gotBytes, legacyBytes) {
		t.Fatalf("kernel identity bytes = %s, former ps identity bytes = %s", gotBytes, legacyBytes)
	}
}

func TestProcessStartTimeDoesNotExecutePSOnKernelSuccess(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("the direct-kernel-reader contract applies to Darwin and Linux")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ps-was-run")
	psPath := filepath.Join(dir, "ps")
	script := []byte("#!/bin/sh\nprintf invoked > \"$PROCESS_START_TIME_PS_MARKER\"\nexit 91\n")
	if err := execfixture.WriteFile(psPath, script, 0o755); err != nil {
		t.Fatalf("write ps sentinel: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("PROCESS_START_TIME_PS_MARKER", marker)
	if _, err := ProcessStartTime(os.Getpid()); err != nil {
		t.Fatalf("ProcessStartTime for live pid: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ps sentinel ran on successful kernel read; stat error = %v", err)
	}
}

func TestConcurrentProcessStartTimeLookupsRemainReliableUnderCPULoad(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("the concurrent kernel-reader contract applies to Darwin and Linux")
	}
	const (
		lookupWorkers = 32
		lookupsEach   = 8
	)
	pid := os.Getpid()
	want, err := ProcessStartTime(pid)
	if err != nil {
		t.Fatalf("initial ProcessStartTime(%d): %v", pid, err)
	}

	stopLoad := make(chan struct{})
	loadWorkers := runtime.GOMAXPROCS(0)
	if loadWorkers > 4 {
		loadWorkers = 4
	}
	if loadWorkers < 1 {
		loadWorkers = 1
	}
	var load sync.WaitGroup
	for range loadWorkers {
		load.Add(1)
		go func() {
			defer load.Done()
			seed := []byte("providerlimits-process-start-time-cpu-load")
			for {
				select {
				case <-stopLoad:
					return
				default:
					digest := sha256.Sum256(seed)
					seed = digest[:]
				}
			}
		}()
	}

	type lookupResult struct {
		value time.Time
		err   error
	}
	results := make(chan lookupResult, lookupWorkers*lookupsEach)
	var lookups sync.WaitGroup
	for range lookupWorkers {
		lookups.Add(1)
		go func() {
			defer lookups.Done()
			for range lookupsEach {
				value, err := ProcessStartTime(pid)
				results <- lookupResult{value: value, err: err}
			}
		}()
	}
	lookups.Wait()
	close(stopLoad)
	load.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Fatalf("live pid %d returned a start-time error under CPU load: %v", pid, result.err)
		}
		if !result.value.Equal(want) {
			t.Fatalf("concurrent start time = %s, want stable value %s", result.value, want)
		}
	}
}

func driveProcessStartTimeErrorSite(site string) error {
	pid := 42
	kernel := func(int) (time.Time, error) {
		switch site {
		case "kernel_missing_pid":
			return time.Time{}, os.ErrNotExist
		case "kernel_permission_denied":
			return time.Time{}, syscall.EACCES
		default:
			return time.Time{}, errors.New("injected kernel read failure")
		}
	}
	ps := func(context.Context, int) (time.Time, error) {
		switch site {
		case "ps_fallback_transient":
			return time.Time{}, context.DeadlineExceeded
		case "kernel_read_failure":
			return time.Time{}, errors.New("injected ps read failure")
		default:
			return time.Time{}, errors.New("ps fallback must not run for this site")
		}
	}
	if site == "invalid_pid" {
		pid = 0
	}
	_, err := processStartTimeWithReaders(pid, kernel, ps)
	return err
}

func requireProcessStartTimeError(t *testing.T, err error, site string, want ProcessStartTimeErrorKind) {
	t.Helper()
	if mismatch := processStartTimeErrorMismatch(err, site, want); mismatch != nil {
		t.Fatal(mismatch)
	}
}

func processStartTimeErrorMismatch(err error, site string, want ProcessStartTimeErrorKind) error {
	var got *ProcessStartTimeError
	if !errors.As(err, &got) {
		return fmt.Errorf("error %v is not a *ProcessStartTimeError", err)
	}
	if got.site != site {
		return fmt.Errorf("typed error site = %q, want %q", got.site, site)
	}
	if got.Kind != want {
		return fmt.Errorf("typed error kind = %q, want %q", got.Kind, want)
	}
	if want == ProcessStartTimeFallbackTransient {
		if !got.Temporary() || !errors.Is(got, ErrProcessStartTimeTransient) {
			return fmt.Errorf("fallback timeout is not marked transient: %v", got)
		}
	} else if got.Temporary() || errors.Is(got, ErrProcessStartTimeTransient) {
		return fmt.Errorf("non-transient error kind %q was marked transient: %v", want, got)
	}
	return nil
}

func marshalStartTime(t *testing.T, value time.Time) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal start time: %v", err)
	}
	return data
}

func mustMarshalStartTime(t *testing.T, value time.Time) []byte {
	t.Helper()
	data := marshalStartTime(t, value)
	return data
}
