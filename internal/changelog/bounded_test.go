package changelog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file bounds every child process the changelog tests start. A previous
// revision ran mutant child test processes, script runs and syntax checks
// through exec.Command with no deadline, so one blocking child hung the whole
// package past the go test timeout with no diagnostic naming the culprit.
// A later revision added per-run deadlines but fanned test goroutines out
// without bound and captured output in unbounded buffers, so the aggregate
// run could still saturate a shared host. This file therefore bounds the
// aggregate too: one package-wide admission cap around every launch and a
// per-stream capture cap keeping the head and tail of an over-long stream.

const (
	// Scripts, git and already-built test binaries keep the short bound.
	defaultChangelogChildTimeout = 120 * time.Second
	// Go-tool children may compile before running, including overlay mutants.
	// Allow cold compilation on slow hosts without relaxing other children.
	defaultChangelogCompileChildTimeout = 10 * time.Minute
	// changelogChildWaitDelay bounds Wait after the child exits while a pipe
	// is still held open. The process-group kill closes pipes promptly in the
	// normal case; this only backstops a descendant that escaped it.
	changelogChildWaitDelay = 2 * time.Second
	// changelogChildTimeoutEnv overrides both command-class defaults.
	// It carries a Go duration ("90s", "3m"). Empty, unparsable or
	// non-positive values fall back to the command-class default. A positive
	// explicit per-call timeout takes precedence over this override.
	changelogChildTimeoutEnv = "CHANGELOG_TEST_CHILD_TIMEOUT"
)

// changelogChildTimeout resolves the per-run child deadline.
func changelogChildTimeout(name string) time.Duration {
	timeout := defaultChangelogChildTimeout
	// Classify the executable, including absolute Go-tool paths. Conservatively
	// allow compilation for every direct Go-tool invocation; do not guess from
	// shell text or classify an already-built test binary as a compiler.
	if base := filepath.Base(name); base == "go" || base == "go.exe" {
		timeout = defaultChangelogCompileChildTimeout
	}
	raw := strings.TrimSpace(os.Getenv(changelogChildTimeoutEnv))
	if raw == "" {
		return timeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return timeout
	}
	return d
}

const (
	// defaultChangelogChildConcurrency bounds how many test children run
	// at once for the whole package. Individual deadlines bound one run;
	// only a shared cap bounds the aggregate fan-out on a shared host.
	defaultChangelogChildConcurrency = 2
	// changelogChildConcurrencyEnv overrides the package-wide child cap.
	// Empty, unparsable or non-positive values fall back to the default.
	changelogChildConcurrencyEnv = "CHANGELOG_TEST_CHILD_CONCURRENCY"
)

// changelogChildMaxConcurrent resolves the package-wide child cap. It is
// read on every acquire so the override applies per test.
func changelogChildMaxConcurrent() int {
	raw := strings.TrimSpace(os.Getenv(changelogChildConcurrencyEnv))
	if raw == "" {
		return defaultChangelogChildConcurrency
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultChangelogChildConcurrency
	}
	return n
}

// changelogConcurrencyGate is the single package-wide admission limiter
// around every test child launch. t.Parallel fans test goroutines out
// without bound; this gate admits at most changelogChildMaxConcurrent
// children regardless of it.
type changelogConcurrencyGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	active int
}

var changelogChildGate = newChangelogConcurrencyGate()

func newChangelogConcurrencyGate() *changelogConcurrencyGate {
	g := &changelogConcurrencyGate{}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// changelogChildAcquire blocks until fewer than the cap are running, then
// records the caller as running.
func changelogChildAcquire() {
	g := changelogChildGate
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.active >= changelogChildMaxConcurrent() {
		g.cond.Wait()
	}
	g.active++
}

// changelogChildRelease records the caller as finished and wakes waiters.
func changelogChildRelease() {
	g := changelogChildGate
	g.mu.Lock()
	g.active--
	g.mu.Unlock()
	g.cond.Broadcast()
}

// changelogChildSpanHook, when non-nil, brackets one semaphore hold window:
// it runs after acquire and its result runs before release. Tests use it
// to observe child concurrency; every other path leaves it nil.
var changelogChildSpanHook func() func()

// changelogChildStreamCap bounds each captured child stream (stdout,
// stderr). A runaway child writing under its deadline must not grow test
// memory without bound. Over-long streams keep their head and tail with a
// truncation marker between them, so diagnostics keep both edges.
const changelogChildStreamCap = 1 << 20

// changelogCappedBuffer is an io.Writer retaining at most cap bytes: the
// first half and the last half of everything written. Small outputs pass
// through exactly; the middle of an over-long stream is dropped as it
// arrives, so the retained size never exceeds the cap no matter how much
// the child writes.
type changelogCappedBuffer struct {
	head    []byte
	headCap int
	ring    []byte // fixed tailCap ring of the most recent overflow bytes
	ringPos int    // overflow bytes ever written (monotonic)
	tailCap int
	total   int64
}

func newChangelogCappedBuffer(cap int) *changelogCappedBuffer {
	headCap := cap / 2
	return &changelogCappedBuffer{headCap: headCap, tailCap: cap - headCap}
}

func (b *changelogCappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.total += int64(n)
	if len(b.head) < b.headCap {
		k := min(n, b.headCap-len(b.head))
		b.head = append(b.head, p[:k]...)
		p = p[k:]
	}
	if len(p) == 0 {
		return n, nil
	}
	if b.ring == nil {
		b.ring = make([]byte, b.tailCap)
	}
	for len(p) > 0 {
		at := b.ringPos % b.tailCap
		k := min(len(p), b.tailCap-at)
		copy(b.ring[at:at+k], p[:k])
		p = p[k:]
		b.ringPos += k
	}
	return n, nil
}

func (b *changelogCappedBuffer) tailString() string {
	if b.ring == nil {
		return ""
	}
	if b.ringPos <= b.tailCap {
		return string(b.ring[:b.ringPos])
	}
	at := b.ringPos % b.tailCap
	return string(b.ring[at:]) + string(b.ring[:at])
}

func (b *changelogCappedBuffer) String() string {
	tail := b.tailString()
	if skipped := b.total - int64(len(b.head)) - int64(len(tail)); skipped > 0 {
		return string(b.head) +
			fmt.Sprintf("\n...[truncated %d bytes of %d total]...\n", skipped, b.total) +
			tail
	}
	return string(b.head) + tail
}

// Timeout text is diagnostic only. Decisions use the sticky tree attestation.
const changelogTimeoutSentinel = "changelog test child TIMEOUT"
const changelogAttestationEnv = "CHANGELOG_TEST_ATTESTATION"
const changelogAttestationAncestorsEnv = "CHANGELOG_TEST_ATTESTATION_ANCESTORS"

func changelogOutputMarksTimeout(out string) bool {
	return strings.Contains(out, changelogTimeoutSentinel)
}

// Each run has a private result file. Nested runners append timeout records
// to every ancestor result; nobody resets them. Missing, malformed or unreadable
// attestations are unknown, never evidence of absence. Diagnostics cannot erase it.
func markChangelogTimeout(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString("timeout\n")
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func readChangelogAttestation(path string) (clean, timedOut bool) {
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), "clean\n") {
		return false, false
	}
	rest := strings.TrimPrefix(string(data), "clean\n")
	for rest != "" {
		if !strings.HasPrefix(rest, "timeout\n") {
			return false, false
		}
		timedOut = true
		rest = strings.TrimPrefix(rest, "timeout\n")
	}
	return true, timedOut
}

// Replace all occurrences so duplicate environment keys cannot change ownership.
func changelogSetEnv(env []string, key, value string) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}

// changelogTimeoutError is the NAMED, typed failure for a child that outlived
// its deadline. It prints the command, the elapsed time and the captured
// output. A timeout is never an exit code and — in the mutant harness —
// never a kill: a hung witness never counts as a kill, and a hung control
// fails the subtest.
type changelogTimeoutError struct {
	argv           []string
	dir            string
	elapsed        time.Duration
	stdout, stderr string
}

func (e *changelogTimeoutError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "changelog test child TIMEOUT after %s: %s",
		e.elapsed.Truncate(time.Millisecond), strings.Join(e.argv, " "))
	if e.dir != "" {
		fmt.Fprintf(&b, " in %s", e.dir)
	}
	if e.stdout != "" {
		fmt.Fprintf(&b, "\nstdout:\n%s", e.stdout)
	}
	if e.stderr != "" {
		fmt.Fprintf(&b, "\nstderr:\n%s", e.stderr)
	}
	if e.stdout == "" && e.stderr == "" {
		b.WriteString("\n(no output captured)")
	}
	return b.String()
}

// Timeout reports this error as a deadline expiry.
func (e *changelogTimeoutError) Timeout() bool { return true }

// asChangelogTimeout unwraps err to the typed timeout failure.
func asChangelogTimeout(err error) (*changelogTimeoutError, bool) {
	var timeout *changelogTimeoutError
	if errors.As(err, &timeout) {
		return timeout, true
	}
	return nil, false
}

// runChangelogChild executes name with args in dir under timeout, in its own
// process group killed at the deadline and cleaned up after every return,
// with stdin from /dev/null and WaitDelay set. At most
// changelogChildMaxConcurrent children run at once package-wide, and each
// captured stream is capped (head and tail kept). Stdout and stderr are
// captured separately. A non-zero exit is an ordinary result (returned with
// a nil error); a deadline expiry returns a *changelogTimeoutError, never
// an exit code; a failure to launch returns the launch error. A
// non-positive timeout selects the environment override or command-class default.
func runChangelogChild(dir string, env []string, timeout time.Duration, name string, args ...string) (string, string, int, error) {
	result := runChangelogOutcome(dir, env, timeout, name, args...)
	return result.stdout, result.stderr, result.exit, result.err
}

func runChangelogOutcome(dir string, env []string, timeout time.Duration, name string, args ...string) changelogChildOutcome {
	if timeout <= 0 {
		timeout = changelogChildTimeout(name)
	}
	// The package-wide admission cap: however many test goroutines
	// t.Parallel fans out, at most the cap launches at once. The acquire
	// precedes the deadline so queueing never burns the per-run bound.
	changelogChildAcquire()
	defer changelogChildRelease()
	var endSpan func()
	if changelogChildSpanHook != nil {
		endSpan = changelogChildSpanHook()
	}
	if endSpan != nil {
		defer endSpan()
	}
	// Each run has its own result file. A timeout marks its file and every
	// ancestor, but never a sibling: witness timeout cannot taint its control.
	var ancestors []string
	if raw := os.Getenv(changelogAttestationAncestorsEnv); raw != "" {
		if err := json.Unmarshal([]byte(raw), &ancestors); err != nil {
			return changelogChildOutcome{err: err}
		}
	}
	directory := filepath.Dir(os.Getenv(changelogAttestationEnv))
	if os.Getenv(changelogAttestationEnv) == "" {
		var err error
		directory, err = os.MkdirTemp("", "changelog-attestation-")
		if err != nil {
			return changelogChildOutcome{err: err}
		}
		defer os.RemoveAll(directory)
	}
	file, err := os.CreateTemp(directory, "attestation-")
	if err != nil {
		return changelogChildOutcome{err: err}
	}
	attestation := file.Name()
	_, writeErr := file.WriteString("clean\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return changelogChildOutcome{err: err}
	}
	paths := append(ancestors, attestation)
	attestTimeout := func() error {
		var result error
		for _, path := range paths {
			result = errors.Join(result, markChangelogTimeout(path))
		}
		// A nested writer that cannot attest must not exit with an ordinary named
		// test failure. The reserved status is propagated as indeterminate.
		if result != nil && os.Getenv(changelogAttestationEnv) != "" {
			os.Exit(124)
		}
		return result
	}
	if env == nil {
		env = os.Environ()
	}
	env = changelogSetEnv(env, changelogAttestationEnv, attestation)
	encoded, _ := json.Marshal(paths)
	env = changelogSetEnv(env, changelogAttestationAncestorsEnv, string(encoded))
	// Measure from before arming the deadline, including command setup. Starting
	// at Run would omit setup time and could report elapsed < timeout even when
	// the context expired. Queueing remains excluded by the acquire above.
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	if err := prepareChangelogProcessScope(cmd, attestation); err != nil {
		return changelogChildOutcome{err: err}
	}
	defer forgetChangelogProcessScope(cmd)
	// A child that prompts (credential, pager, editor) must meet EOF, never
	// the test process's stdin.
	null, err := os.Open(os.DevNull)
	if err != nil {
		cmd.Stdin = bytes.NewReader(nil)
	} else {
		defer null.Close()
		cmd.Stdin = null
	}
	stdout := newChangelogCappedBuffer(changelogChildStreamCap)
	stderr := newChangelogCappedBuffer(changelogChildStreamCap)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Cancellation terminates this registered subtree, including groups launched
	// by nested wrappers. WaitDelay also bounds inherited pipe drains.
	setChangelogChildProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Attest BEFORE signaling: an outer supervisor can terminate nested
			// runners before their Run returns. Each scope excludes its caller.
			_ = attestTimeout()
			cancelChangelogChildTree(cmd)
			_ = cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = changelogChildWaitDelay
	runErr := runChangelogRegistered(cmd)
	// The group cleanup runs after EVERY return, not only in Cancel: Run
	// can return before the context deadline (a WaitDelay expiry, an early
	// child exit) while a same-group descendant still holds an output pipe,
	// and the deferred context cancel never reaches cmd.Cancel after Run
	// has returned. Without this a descendant outlives the bound.
	cleanupChangelogChildProcessGroup(cmd)
	elapsed := time.Since(start)
	result := changelogChildOutcome{
		stdout: stdout.String(), stderr: stderr.String(), exit: -1,
		timeout:   timeout,
		truncated: changelogCaptureTruncated(stdout) || changelogCaptureTruncated(stderr),
	}
	result.out = result.stdout + result.stderr
	if ctx.Err() == context.DeadlineExceeded {
		if err := attestTimeout(); err != nil {
			result.stderr += fmt.Sprintf("\nTIMEOUT attestation failed: %v", err)
		}
		result.err = &changelogTimeoutError{argv: append([]string{name}, args...), dir: dir,
			elapsed: elapsed, stdout: result.stdout, stderr: result.stderr}
	} else if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.exit = exitErr.ExitCode()
		} else {
			result.err = runErr
		}
	} else {
		result.exit = 0
	}
	result.err = errors.Join(result.err, changelogProcessScopeFailure(cmd))
	result.attested, result.timedOut = readChangelogAttestation(attestation)
	if result.exit == 124 {
		result.attested = false
		result.err = errors.New("TIMEOUT attestation unavailable (reserved child exit 124)")
	}
	return result
}

func changelogCaptureTruncated(writer io.Writer) bool {
	buffer, ok := writer.(*changelogCappedBuffer)
	return !ok || buffer.total > int64(changelogChildStreamCap)
}

func splitChangelogPIDs(data string) []string { return strings.Fields(data) }

func cancelChangelogChildTree(cmd *exec.Cmd) { cleanupChangelogChildProcessGroup(cmd) }
