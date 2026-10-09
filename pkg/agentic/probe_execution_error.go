package agentic

import (
	"fmt"
	"strings"
)

// Probe stage identities carried by ProbeExecutionError. The stage names the
// probe argv that ran: --version or --help. Callers match on these constants,
// never on prose inside the error text.
const (
	// ProbeStageVersion is a `<binary> --version` probe.
	ProbeStageVersion = "version"
	// ProbeStageHelp is a `<binary> --help` probe.
	ProbeStageHelp = "help"
)

// ProbeEvidenceSampleBytes bounds the stdout/stderr samples a
// ProbeExecutionError carries: the first 4 KiB of each stream. Size past
// the sample is reported in Detail, never by growing the error.
const ProbeEvidenceSampleBytes = 4 << 10

// ProbeExecutionError is the typed record of one failed tool-probe
// execution: a version or help probe the module ran and could not complete
// normally. It is an ATTEMPT record, not a static verdict: ExecAttempted
// reports whether the probe reached the process start, ChildStarted whether
// a child actually ran, and Timeout/OutputLimited which bound fired. Detail
// carries the exit, signal, drain or teardown fact in one line.
//
// Only an error returned by the invoked module probe may become a host
// attempt event. Static failures — an unparsable answer, a drifted seal, a
// commitment mismatch — carry their own typed errors and never share this
// wrapper: a static error must not acquire exec_attempted by wrapping.
//
// Consumers match with errors.As for *ProbeExecutionError; the zero value
// is meaningless and is never returned.
type ProbeExecutionError struct {
	// Stage names the probe argv: ProbeStageVersion or ProbeStageHelp.
	Stage string
	// ExecAttempted reports that the probe attempted the process start.
	// A fork/exec failure is attempted but not started.
	ExecAttempted bool
	// ChildStarted reports that a child process actually ran.
	ChildStarted bool
	// Timeout reports that the probe's execution deadline fired first.
	Timeout bool
	// OutputLimited reports that the probe's stdout cap fired first: the
	// first byte past the cap terminated the probe.
	OutputLimited bool
	// TeardownIncomplete reports that the child or its pipes could not be
	// confirmed reaped within the bounded drain. The probe is over; a
	// second child must not start on this evidence.
	TeardownIncomplete bool
	// Detail is the one-line exit, signal, size or teardown fact.
	Detail string
	// Stdout is the first ProbeEvidenceSampleBytes of captured stdout.
	Stdout []byte
	// Stderr is the first ProbeEvidenceSampleBytes of captured stderr.
	Stderr []byte
}

// Error renders the attempt classification without the evidence samples:
// samples are bounded but still too large for a message line.
func (e *ProbeExecutionError) Error() string {
	if e == nil {
		return "agentic: no probe attempt"
	}
	var causes []string
	if e.Timeout {
		causes = append(causes, "timeout")
	}
	if e.OutputLimited {
		causes = append(causes, "output past the stdout cap")
	}
	if e.TeardownIncomplete {
		causes = append(causes, "teardown incomplete")
	}
	cause := strings.Join(causes, ", ")
	if cause == "" {
		cause = "probe failed"
	}
	stage := e.Stage
	if stage == "" {
		stage = "unknown"
	}
	if e.Detail == "" {
		return fmt.Sprintf("agentic: %s probe %s (exec attempted=%v, child started=%v)",
			stage, cause, e.ExecAttempted, e.ChildStarted)
	}
	return fmt.Sprintf("agentic: %s probe %s (exec attempted=%v, child started=%v): %s",
		stage, cause, e.ExecAttempted, e.ChildStarted, e.Detail)
}
