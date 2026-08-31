package localruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// ErrStatusReadFailed is returned for every way a status read can fail
// before a decode is even attempted: the binary is not on PATH, the
// subprocess exited non-zero, the caller's ctx (or this reader's own bounded
// timeout) fired first, or the response exceeded the configured byte cap.
var ErrStatusReadFailed = errors.New("localruntime: status read failed")

// statusCommandTimeout is this adapter's own bound on the `agents-infra
// runtime status --json` subprocess call, named rather than inlined so a
// test can shrink it via WithTimeout. It composes with whatever deadline the
// caller's ctx already carries: context.WithTimeout always fires at the
// EARLIER of the two.
const statusCommandTimeout = 10 * time.Second

// defaultStatusResponseMaxBytes and defaultStatusObservedMaxEvents are this
// reader's own internal safety bounds — not the extension #1 operator
// policy numbers (backoff, quarantine, log rotation) that §7.3 requires to
// have no code-level default. Both are overridable at construction via
// WithResponseCap/WithObservedCap, satisfying "exact integers set at
// construction time, test-injectable" without requiring every caller to
// plumb a number it has no opinion about.
const (
	defaultStatusResponseMaxBytes  = 1 << 20 // 1 MiB
	defaultStatusObservedMaxEvents = 32
)

// commandRunner executes one subprocess call and returns its stdout bytes.
// It is the injection seam: production uses runExec, tests substitute a
// scriptable double that never shells out.
type commandRunner func(ctx context.Context, name string, args []string) ([]byte, error)

// runExec is the production commandRunner: exec.CommandContext, stdout
// captured, stderr discarded. exec.LookPath failure and a non-zero exit both
// surface as the returned error, uniformly.
func runExec(ctx context.Context, name string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	return stdout.Bytes(), err
}

// CLIStatusReader is the StatusReader implementation that shells out to the
// already-shipped `agents-infra runtime status --json` subprocess.
//
// It performs NO caching: every Status call is a fresh subprocess read.
// Memoization belongs to local-models.toml's own loader (a different fact,
// a different lifetime) and never to a live status read.
type CLIStatusReader struct {
	run                     commandRunner
	statusResponseMaxBytes  int
	statusObservedMaxEvents int
	timeout                 time.Duration
	clock                   func() time.Time

	mu       sync.Mutex
	observed []Observation
}

// Option configures a CLIStatusReader at construction.
type Option func(*CLIStatusReader)

// WithCommandRunner substitutes the subprocess runner. Tests use this to
// avoid ever invoking a real agents-infra binary.
func WithCommandRunner(run commandRunner) Option { return func(r *CLIStatusReader) { r.run = run } }

// WithTimeout overrides this reader's own bound on a status subprocess call.
func WithTimeout(d time.Duration) Option { return func(r *CLIStatusReader) { r.timeout = d } }

// WithResponseCap overrides the stdout byte cap.
func WithResponseCap(n int) Option {
	return func(r *CLIStatusReader) { r.statusResponseMaxBytes = n }
}

// WithObservedCap overrides the Observed ring-buffer capacity.
func WithObservedCap(n int) Option {
	return func(r *CLIStatusReader) { r.statusObservedMaxEvents = n }
}

// WithClock overrides the clock Status.AsOf is stamped from.
func WithClock(now func() time.Time) Option { return func(r *CLIStatusReader) { r.clock = now } }

// NewCLIStatusReader returns a reader that shells out to the real
// agents-infra binary on PATH, unless overridden by an Option.
func NewCLIStatusReader(opts ...Option) *CLIStatusReader {
	r := &CLIStatusReader{
		run:                     runExec,
		statusResponseMaxBytes:  defaultStatusResponseMaxBytes,
		statusObservedMaxEvents: defaultStatusObservedMaxEvents,
		timeout:                 statusCommandTimeout,
		clock:                   time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Status shells out to `agents-infra runtime status --project <p> --profile
// <profile> --json`, bounded by the earlier of ctx's own deadline and this
// reader's own timeout, and decodes the result.
//
// A subprocess error, a response over the byte cap, and a decode failure are
// all returned as errors — never as a memoized prior answer and never
// silently coerced into a Status the caller could mistake for a real read.
func (r *CLIStatusReader) Status(ctx context.Context, query StatusQuery) (Status, error) {
	boundedCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := []string{"runtime", "status", "--project", query.AgentsInfraProject, "--profile", query.AgentsInfraProfile, "--json"}
	output, err := r.run(boundedCtx, "agents-infra", args)
	if err != nil {
		return Status{}, fmt.Errorf("%w: agents-infra runtime status: %w", ErrStatusReadFailed, err)
	}
	if len(output) > r.statusResponseMaxBytes {
		return Status{}, fmt.Errorf("%w: response is %d bytes, exceeding the %d byte cap", ErrStatusReadFailed, len(output), r.statusResponseMaxBytes)
	}

	status, err := decodeStatus(output, query.Runtime, query.Model, r.clock())
	if err != nil {
		return Status{}, err
	}
	status.Observed = r.recordObservation(fmt.Sprintf("broker_state=%s broker_source=%s", status.BrokerState, status.BrokerSource))
	return status, nil
}

// recordObservation appends one entry to the bounded ring buffer, evicting
// the oldest entry at the cap, and returns a copy of the current buffer so a
// caller ranging over it cannot reach this reader's own backing array.
func (r *CLIStatusReader) recordObservation(detail string) []Observation {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observed = append(r.observed, Observation{Source: "agents-infra runtime status --json", Detail: detail})
	if over := len(r.observed) - r.statusObservedMaxEvents; over > 0 {
		r.observed = r.observed[over:]
	}
	return append([]Observation(nil), r.observed...)
}
