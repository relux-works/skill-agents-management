//go:build unix

package muse

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// shortPipeHolderProof is the R5-1 post-leader pipe lifetime proof. A
// background holder that inherited the version probe's stdout pipe blocks
// reading a FIFO OWNED BY THE TEST, never by the leader, and stays blocked
// until the test has OBSERVED the leader's termination; only then does the
// test release it explicitly. The previous fixture released its holder when
// the leader's FIFO fd closed at exit, so the holder could finish — and the
// pipe could drain — before the leader's termination was observed, and the
// test could pass without ever holding the pipe past the parent exit.
//
// The proof runs one release round per probe run, because sealing runs the
// version stub twice (once to seal, once to re-verify):
//
//   - ready: the holder announces it is alive and about to block, and the
//     leader waits for that byte before printing. At leader exit the holder
//     therefore demonstrably exists and holds the probe's stdout pipe,
//     which it inherited at fork and never touches.
//   - pid: the leader writes its own PID just before exit. The release
//     pump reads one PID per run and waits for kill(pid, 0) == ESRCH —
//     the definitive reaped-by-the-probe signal, since the probe is the
//     leader's parent and only waiter — before releasing.
//   - hold: the test holds this FIFO O_RDWR for the whole proof, so the
//     holder's read blocks until the test writes a release line. One line
//     per observed exit; FIFO order pairs each line with its run's holder,
//     and a line written before its holder opens the FIFO simply waits in
//     the buffer. Nothing the leader does releases the holder: leader exit
//     closes no end the holder waits on.
//
// Every wait is readiness-based: FIFO rendezvous, the ESRCH signal, and
// probe exit. The 30s watchdogs are failure bounds, never state delays —
// on a loaded machine the proof waits as long as the signal takes — and
// the pump's spin yields the scheduler instead of sleeping. The pump never
// touches testing.T, so it can outlive a failed test goroutine safely; it
// reports through stop, which the test must check. The stop check is
// load-bearing: a proof breakdown releases through cleanup EOF and can
// still drain, so without it a broken proof could pass.
//
// No production seam was needed: leader termination is observed from the
// test process with FIFOs and kill(2) only.
type shortPipeHolderProof struct {
	holdPath  string
	readyPath string
	pidPath   string
	hold      *os.File
	pid       *os.File
	done      chan error
}

// newShortPipeHolderProof creates the proof FIFOs, holds the test-owned
// ends, and starts the release pump. The caller must keep the proof
// reachable for the whole probe run and must check stop.
func newShortPipeHolderProof(t *testing.T) *shortPipeHolderProof {
	t.Helper()
	dir := t.TempDir()
	proof := &shortPipeHolderProof{
		holdPath:  filepath.Join(dir, "holder-hold"),
		readyPath: filepath.Join(dir, "holder-ready"),
		pidPath:   filepath.Join(dir, "leader-pid"),
		done:      make(chan error, 1),
	}
	for _, fifo := range []string{proof.holdPath, proof.readyPath, proof.pidPath} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("creating the pipe-proof FIFO: %v", err)
		}
	}
	open := func(path string) *os.File {
		// O_RDWR never blocks on open and keeps the FIFO usable while
		// the test holds it: the holder's read blocks for a release
		// line, and the pump's PID read blocks between probe runs.
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatalf("holding the pipe-proof FIFO: %v", err)
		}
		return f
	}
	proof.hold = open(proof.holdPath)
	proof.pid = open(proof.pidPath)
	t.Cleanup(func() {
		// Wake the pump explicitly (a blocked FIFO read is not
		// interrupted by Close on Darwin), then close the held ends,
		// which releases a still-blocked holder through EOF, so a
		// failed test leaks neither a process nor a goroutine.
		_, _ = proof.pid.WriteString(shortPipeProofStop + "\n")
		_ = proof.hold.Close()
		_ = proof.pid.Close()
	})
	go proof.pump()
	return proof
}

// versionBody returns --version shell whose background holder is governed
// by the proof: ready handshake, version answer, PID report, exit.
func (p *shortPipeHolderProof) versionBody() string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	return "(printf . > " + quote(p.readyPath) + "; IFS= read -r holder_hold < " + quote(p.holdPath) + ") &\n" +
		"IFS= read -r holder_ready < " + quote(p.readyPath) + "\n" +
		"printf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\n" +
		"printf '%s\\n' \"$$\" > " + quote(p.pidPath) + "\n" +
		"exit 0\n"
}

// pump releases one holder per observed leader termination. It reports
// its terminal state on done and never touches testing.T.
func (p *shortPipeHolderProof) pump() {
	var terminal error
	defer func() { p.done <- terminal }()
	reader := bufio.NewReader(p.pid)
	for {
		line, err := reader.ReadString('\n')
		if err == nil && strings.TrimSpace(line) == shortPipeProofStop {
			// stop or cleanup woke the read explicitly.
			return
		}
		if err != nil {
			// The held PID end closes only at stop or test cleanup,
			// both past the probe run, so a read failure is the
			// shutdown signal, never a mid-proof event.
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 0 {
			terminal = fmt.Errorf("pipe proof read leader pid %q, want a positive pid", strings.TrimSpace(line))
			return
		}
		// Bounded wait on the definitive signal: the leader is the
		// probe's direct child and the probe its only waiter, so
		// ESRCH means the probe reaped it. The spin yields instead
		// of sleeping; the watchdog is a failure bound.
		watchdog := time.NewTimer(30 * time.Second)
		reaped := false
		for !reaped {
			if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
				reaped = true
				break
			}
			select {
			case <-watchdog.C:
				watchdog.Stop()
				terminal = fmt.Errorf("pipe proof: leader %d still present after 30s", pid)
				return
			default:
			}
			runtime.Gosched()
		}
		watchdog.Stop()
		if _, err := p.hold.WriteString("release\n"); err != nil {
			terminal = fmt.Errorf("pipe proof releasing the holder: %v", err)
			return
		}
	}
}

// stop ends the proof and reports the pump's terminal state. A non-nil
// error fails the test even when the probe drained: without this check a
// proof breakdown that still drained through cleanup EOF would pass.
func (p *shortPipeHolderProof) stop() error {
	// The PID end is held O_RDWR, which Go's poller excludes on Darwin, so
	// Close does not interrupt the pump's blocked read: wake it with an
	// explicit stop line, join it, and only then close the held ends.
	if _, err := p.pid.WriteString(shortPipeProofStop + "\n"); err != nil {
		_ = p.hold.Close()
		_ = p.pid.Close()
		return fmt.Errorf("pipe proof: waking the release pump: %v", err)
	}
	watchdog := time.NewTimer(30 * time.Second)
	defer watchdog.Stop()
	var err error
	select {
	case err = <-p.done:
	case <-watchdog.C:
		err = fmt.Errorf("pipe proof: the release pump never exited")
	}
	_ = p.hold.Close()
	_ = p.pid.Close()
	return err
}

// shortPipeProofStop is the line stop and cleanup write to the PID FIFO to
// end the pump's read; a leader never writes it (it writes its PID).
const shortPipeProofStop = "stop"
