// Package toolprobe runs one tool's `--version` or `--help` probe: the
// `<binary> <flag>` subprocess a plugin resolves and parses.
//
// It performs NO caching and NO parsing: every call is a fresh subprocess
// read, and the release and help grammars are each tool's own (a plugin's
// probe.go). What is shared here is the part every probe must get
// identically right: the argv (exactly one probe flag, never a flag that
// could start a session), the bounded wait (the caller's context, plus
// this package's own timeout — whichever fires first), the byte caps
// enforced DURING the read (the first byte past the stdout cap terminates
// the probe; nothing buffers first and checks after), the process-group
// teardown (a deadline kill reaches descendants holding the pipes, with a
// bounded drain), and the environment discipline (the child sees the
// handed launch environment, or an empty one — never the ambient process
// environment, which the System contract forbids a plugin from reading).
//
// Attempt failures — fork/exec errors, timeouts, output past the cap,
// non-zero exits, unconfirmed teardowns — return a typed
// *agentic.ProbeExecutionError carrying the stage, the exec/child facts
// and bounded evidence samples. Pre-start refusals (no binary) stay plain
// errors: nothing was attempted.
package toolprobe

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/relux-works/skill-agents-management/internal/releasegrammar"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Probe argv flags. Each is a const rather than a parameter so no caller
// can probe with anything else: a probe that could spell an arbitrary argv
// is a launcher wearing a probe's name.
const (
	// versionArg is the version probe's whole argv after the binary.
	versionArg = "--version"
	// helpArg is the help probe's whole argv after the binary.
	helpArg = "--help"
)

// Probe execution bounds. Each composes with whatever deadline the
// caller's ctx already carries: context.WithTimeout always fires at the
// EARLIER of the two, so a caller with a tighter budget keeps it. The
// exact values are not a behavior contract — any bound in seconds
// serves — which is why no test pins them; the honoring of a fired bound
// is what the tests prove, with adversarial stubs.
const (
	// versionTimeout bounds one `--version` subprocess call.
	versionTimeout = 10 * time.Second
	// helpTimeout bounds one `--help` subprocess call, exactly like the
	// version probe: a help text is documentation, not a session, and an
	// unbounded help read is an unbounded wait wearing a probe's name.
	helpTimeout = 10 * time.Second
	// teardownTimeout bounds the drain after a deadline kill: reaping the
	// child and collecting the bounded pipe copies. Past it the probe
	// reports teardown-incomplete rather than waiting further; the
	// teardown never grows the execution bound, it shares this one.
	teardownTimeout = 5 * time.Second
)

// Probe output caps, enforced DURING the read by the capturing writer.
// Exactly-at-cap output is allowed; the first byte past the cap fires.
const (
	// maxVersionBytes caps a `--version` answer. A version is one line;
	// 64 KiB is orders of magnitude past that and still cheap to hold
	// while the answer is checked. Past the cap the probe refuses rather
	// than truncating: a truncated answer could parse as a release it
	// is not.
	maxVersionBytes = 64 << 10
	// maxHelpBytes caps a `--help` answer at 1 MiB. Help is prose, not
	// one line, but a help text past a megabyte is not documentation the
	// mapper can treat as an option declaration.
	maxHelpBytes = 1 << 20
	// maxStderrBytes caps stderr capture at 64 KiB. Stderr is never the
	// parsed answer, so past the cap the probe keeps only what it holds
	// and discards the rest without failing: a chatty diagnostic must
	// not become a probe refusal.
	maxStderrBytes = 64 << 10
)

// VersionOutput runs binary with argv `--version` in env and returns its
// stdout. Stderr is discarded from the answer: all probed tools print
// their release on stdout, and a diagnostic there must not become part of
// the parsed answer. (A bounded stderr sample still travels on the typed
// attempt error when the probe fails.)
//
// A nil env runs the child with an EMPTY environment, not the process's
// own: exec would otherwise inherit ambient state the System contract
// forbids.
func VersionOutput(ctx context.Context, binary string, env []string) ([]byte, error) {
	return runProbe(ctx, agentic.ProbeStageVersion, binary, env, versionArg, maxVersionBytes, versionTimeout)
}

// HelpOutput runs binary with argv `--help` in env and returns its stdout,
// under the same execution, teardown and environment bounds as the
// version probe and its own 1 MiB stdout cap. Stderr is captured bounded
// for the failure sample only, never parsed.
func HelpOutput(ctx context.Context, binary string, env []string) ([]byte, error) {
	return runProbe(ctx, agentic.ProbeStageHelp, binary, env, helpArg, maxHelpBytes, helpTimeout)
}

// IsReleaseTriple retains the probe API while sharing the pure grammar.
func IsReleaseTriple(s string) bool { return releasegrammar.IsReleaseTriple(s) }

// runProbe executes one probe argv under the execution bound, captures
// stdout to cap and stderr to its own bound, and classifies the outcome.
// Success returns the full stdout bytes, which never exceed cap.
func runProbe(ctx context.Context, stage, binary string, env []string, probeArg string, stdoutCap int, timeout time.Duration) ([]byte, error) {
	if strings.TrimSpace(binary) == "" {
		return nil, fmt.Errorf("toolprobe: no binary to probe")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return nil, &agentic.ProbeExecutionError{
			Stage:   stage,
			Timeout: err == context.DeadlineExceeded,
			Detail:  fmt.Sprintf("probe context already ended before start: %v", err),
		}
	}
	cmd := exec.Command(binary, probeArg)
	cmd.Env = env
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	// A nil Stdin reads from the null device: the probe child inherits
	// no ambient standard input either way.
	setProbeProcessGroup(cmd)
	stdout := newCappedBuffer(stdoutCap)
	stderr := newDiscardingBuffer(maxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	kill := sync.OnceFunc(func() { killProbeChild(cmd) })
	stdout.onOver = kill
	if err := cmd.Start(); err != nil {
		return nil, &agentic.ProbeExecutionError{
			Stage:         stage,
			ExecAttempted: true,
			Detail:        fmt.Sprintf("starting %s %s: %v", binary, probeArg, err),
		}
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	var waitErr error
	exited := false
	select {
	case waitErr = <-waitDone:
		exited = true
	case <-bounded.Done():
	}
	deadlineFired := false
	if !exited {
		kill()
		drain, drainCancel := context.WithTimeout(context.Background(), teardownTimeout)
		defer drainCancel()
		select {
		case waitErr = <-waitDone:
			exited = true
		case <-drain.Done():
		}
		if !exited {
			return nil, &agentic.ProbeExecutionError{
				Stage:              stage,
				ExecAttempted:      true,
				ChildStarted:       true,
				Timeout:            bounded.Err() == context.DeadlineExceeded,
				TeardownIncomplete: true,
				Detail:             probeDeadlineDetail(bounded, timeout, stdout.Len(), stderr.Len()),
				Stdout:             stdout.Sample(),
				Stderr:             stderr.Sample(),
			}
		}
		deadlineFired = true
	}
	if stdout.Over() {
		return nil, &agentic.ProbeExecutionError{
			Stage:         stage,
			ExecAttempted: true,
			ChildStarted:  true,
			OutputLimited: true,
			Detail:        fmt.Sprintf("%s %s answered past the %d byte cap (%d bytes captured)", binary, probeArg, stdoutCap, stdout.Len()),
			Stdout:        stdout.Sample(),
			Stderr:        stderr.Sample(),
		}
	}
	if waitErr == nil {
		// The deadline fired but the child had already exited cleanly:
		// only a pipe holder remained, now reaped by the group kill.
		// The captured bytes are the complete answer, not a timeout.
		if stage == agentic.ProbeStageHelp && stdout.Len() == 0 {
			// A help probe that prints nothing acquired no evidence:
			// zero bytes cannot declare an option and cannot digest
			// into one. This is an attempt failure, not a static
			// "declares no flag" verdict — absence of help is not
			// help without the flag.
			return nil, &agentic.ProbeExecutionError{
				Stage:         stage,
				ExecAttempted: true,
				ChildStarted:  true,
				Detail:        fmt.Sprintf("%s %s printed no output", binary, probeArg),
				Stdout:        stdout.Sample(),
				Stderr:        stderr.Sample(),
			}
		}
		return stdout.Bytes(), nil
	}
	if deadlineFired {
		return nil, &agentic.ProbeExecutionError{
			Stage:         stage,
			ExecAttempted: true,
			ChildStarted:  true,
			Timeout:       bounded.Err() == context.DeadlineExceeded,
			Detail:        probeDeadlineDetail(bounded, timeout, stdout.Len(), stderr.Len()),
			Stdout:        stdout.Sample(),
			Stderr:        stderr.Sample(),
		}
	}
	if waitErr != nil {
		return nil, &agentic.ProbeExecutionError{
			Stage:         stage,
			ExecAttempted: true,
			ChildStarted:  true,
			Detail:        fmt.Sprintf("%s %s: %v", binary, probeArg, waitErr),
			Stdout:        stdout.Sample(),
			Stderr:        stderr.Sample(),
		}
	}
	return stdout.Bytes(), nil
}

// probeDeadlineDetail renders the one-line deadline fact, distinguishing
// a fired execution deadline from a caller cancellation: both end the
// probe, but only the first is the probe's own bound firing.
func probeDeadlineDetail(bounded context.Context, timeout time.Duration, stdoutLen, stderrLen int) string {
	if bounded.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("probe exceeded its %s execution bound (%d stdout and %d stderr bytes captured)", timeout, stdoutLen, stderrLen)
	}
	return fmt.Sprintf("probe context ended before the child exited (%d stdout and %d stderr bytes captured)", stdoutLen, stderrLen)
}

// cappedBuffer is the during-read capturing writer: bytes past the limit
// fire onOver once and are refused, so a runaway answer can neither fill
// memory nor pass as a truncated truth. A discarding buffer instead drops
// past-limit bytes silently: stderr capture must not kill a probe for a
// chatty diagnostic. It is safe for the exec copy goroutine and the
// waiting caller concurrently.
type cappedBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	limit   int
	discard bool
	over    bool
	onOver  func()
}

func newCappedBuffer(limit int) *cappedBuffer { return &cappedBuffer{limit: limit} }

func newDiscardingBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit, discard: true}
}

// Write appends up to the limit and fires onOver on the first byte past
// it. Past the limit a capturing buffer errors every further byte: the
// exec copy stops, the child sees a broken pipe on its next write, and
// the fired kill ends it. A discarding buffer reports past-limit bytes
// written and keeps nothing.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.over {
		if b.discard {
			return len(p), nil
		}
		return 0, fmt.Errorf("toolprobe: output past the %d byte cap", b.limit)
	}
	room := b.limit - b.buf.Len()
	if room < 0 {
		room = 0
	}
	if len(p) > room {
		b.buf.Write(p[:room])
		b.over = true
		if b.onOver != nil {
			b.onOver()
		}
		if b.discard {
			return len(p), nil
		}
		return room, fmt.Errorf("toolprobe: output past the %d byte cap", b.limit)
	}
	return b.buf.Write(p)
}

// Over reports whether the cap fired. Callers read it after Wait, when
// the copy goroutine has finished; the mutex keeps timeout-path reads
// exact too.
func (b *cappedBuffer) Over() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.over
}

// Len reports the captured byte count, at most the cap.
func (b *cappedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// Bytes returns the captured bytes. Callers use it on the success path,
// where the cap never fired and the bytes are the whole answer.
func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

// Sample returns the first ProbeEvidenceSampleBytes of captured output
// for the attempt error: bounded evidence, never the whole answer.
func (b *cappedBuffer) Sample() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := b.buf.Bytes()
	if len(data) > agentic.ProbeEvidenceSampleBytes {
		data = data[:agentic.ProbeEvidenceSampleBytes]
	}
	return append([]byte(nil), data...)
}
