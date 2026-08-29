package pi

import (
	"context"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
)

// admitCases is every reachable (BrokerState, BrokerSource) pair (the frozen
// eleven, architecture decision §2.3.1), each labeled with whether §5.2.1's
// table admits or refuses it. This is case 21b, driven directly against
// this package's own Preflight — the real implementation of the table, not
// a fake standing in for it.
var admitCases = []struct {
	name   string
	state  string
	source localruntime.BrokerObservationSource
	admit  bool
}{
	{"absent/determined", "absent", localruntime.SourceDetermined, true},
	{"starting-unverified/candidate-only", "starting-unverified", localruntime.SourceCandidateOnly, false},
	{"starting/record-derived-unverified", "starting", localruntime.SourceRecordUnverified, false},
	{"serving/record-derived-unverified", "serving", localruntime.SourceRecordUnverified, false},
	{"lingering/record-derived-unverified", "lingering", localruntime.SourceRecordUnverified, false},
	{"draining/record-derived-unverified", "draining", localruntime.SourceRecordUnverified, false},
	{"unverified-stale/record-derived-unverified", "unverified-stale", localruntime.SourceRecordUnverified, false},
	{"starting/attested", "starting", localruntime.SourceAttested, true},
	{"serving/attested", "serving", localruntime.SourceAttested, true},
	{"lingering/attested", "lingering", localruntime.SourceAttested, true},
	{"draining/attested", "draining", localruntime.SourceAttested, false},
}

// TestPreflightAdmitRefuseTable is case 21b(a)-(f): the tightened,
// single-ADMIT table, with ("absent","determined") the ONLY non-attested
// fallback admit and every other unattested/indeterminate row refusing.
func TestPreflightAdmitRefuseTable(t *testing.T) {
	if len(admitCases) != 11 {
		t.Fatalf("admitCases has %d entries, want all eleven frozen pairs", len(admitCases))
	}
	for _, tc := range admitCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeStatusReader{status: localruntime.Status{BrokerState: tc.state, BrokerSource: tc.source}}
			system := New(reader)
			_, err := system.Preflight(context.Background(), agenticRequest())
			admitted := err == nil
			if admitted != tc.admit {
				t.Fatalf("admitted=%v (err=%v), want admit=%v", admitted, err, tc.admit)
			}
		})
	}
}

// TestPreflightAdmitRefuseAcrossRepeatedCalls is case 21b's four-call
// sequence: initial, retry after a refusal, resume after simulated abort
// clearance, and recovery after the reader is reconfigured to an ADMIT row —
// so a mutant that only misbehaves on a later call cannot hide behind a
// passing first call.
func TestPreflightAdmitRefuseAcrossRepeatedCalls(t *testing.T) {
	reader := &fakeStatusReader{status: localruntime.Status{BrokerState: "draining", BrokerSource: localruntime.SourceAttested}}
	system := New(reader)
	req := agenticRequest()

	// (1) initial call: REFUSE.
	if _, err := system.Preflight(context.Background(), req); err == nil {
		t.Fatal("call 1 (draining/attested): admitted, want refuse")
	}
	// (2) retry, same refusing state: REFUSE again.
	if _, err := system.Preflight(context.Background(), req); err == nil {
		t.Fatal("call 2 (retry): admitted, want refuse")
	}
	// (3) resume after a simulated interrupted-turn abort/clear: still the
	// same broker fact, still REFUSE.
	if _, err := system.Preflight(context.Background(), req); err == nil {
		t.Fatal("call 3 (resume): admitted, want refuse")
	}
	// (4) recovery: the fake reader is reconfigured to the sole non-attested
	// ADMIT row.
	reader.status = localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}
	if _, err := system.Preflight(context.Background(), req); err != nil {
		t.Fatalf("call 4 (recovery, absent/determined): refused, want admit: %v", err)
	}
}

// TestPreflightReadFailureRefuses is §5.2.1's read-failure row: a failed
// StatusReader.Status call refuses, distinct from a successful read
// reporting an inconclusive state.
func TestPreflightReadFailureRefuses(t *testing.T) {
	reader := &fakeStatusReader{err: context.DeadlineExceeded}
	system := New(reader)
	if _, err := system.Preflight(context.Background(), agenticRequest()); err == nil {
		t.Fatal("a StatusReader read failure was admitted")
	}
}

// TestPreflightRealTimeoutFires proves the ctx bound Preflight itself is
// asked to honor is real: a fake StatusReader that hangs forever must cause
// Preflight to return once the ctx handed to it is done, not hang forever.
// (BuildLaunch is what actually wraps ctx in context.WithTimeout with
// preflightTimeout; this test drives Preflight directly with an
// already-bounded ctx, which is the contract Preflight itself must honor
// regardless of who applied the bound.)
func TestPreflightRealTimeoutFires(t *testing.T) {
	reader := &fakeStatusReader{hang: true}
	system := New(reader)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := system.Preflight(ctx, agenticRequest())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Preflight admitted after its ctx was cancelled")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Preflight took %v to return after ctx cancellation", elapsed)
	}
}

// agenticRequest is a minimal, fully-populated LaunchRequest for a
// local-qwen-shaped launch.
func agenticRequest() agentic.LaunchRequest {
	return agentic.LaunchRequest{
		Runtime: "local-qwen",
		Model:   agentic.Model{ID: "qwen-3.8-27b-mlx-8bit"},
		Profile: "local-qwen",
		Env:     []string{"AGENTS_INFRA_CALLER_CWD=/Users/op/project"},
	}
}

// TestPreflightConstructsStatusQueryFromRequestFieldsOnly is case 16c: every
// StatusQuery field is sourced from the SAME LaunchRequest, and none of them
// is ever recovered from an ambient fallback.
func TestPreflightConstructsStatusQueryFromRequestFieldsOnly(t *testing.T) {
	reader := &fakeStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
	system := New(reader)

	req := agentic.LaunchRequest{
		Runtime: "local-test-profile",
		Model:   agentic.Model{ID: "some-other-model"},
		Profile: "some-other-profile",
		Env:     []string{"AGENTS_INFRA_CALLER_CWD=/Users/op/other-project"},
	}
	if _, err := system.Preflight(context.Background(), req); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(reader.calls) != 1 {
		t.Fatalf("StatusReader called %d times, want 1", len(reader.calls))
	}
	got := reader.calls[0]
	if got.Runtime != "local-test-profile" || got.Model != "some-other-model" ||
		got.AgentsInfraProject != "/Users/op/other-project" || got.AgentsInfraProfile != "some-other-profile" {
		t.Fatalf("StatusQuery = %+v, want fields sourced verbatim from the request", got)
	}
}

// TestPreflightNeverFallsBackToAmbientState is case 16c(c): a LaunchRequest
// with Runtime set but Profile/Env's AGENTS_INFRA_CALLER_CWD entry EMPTY
// must produce a StatusQuery with empty AgentsInfraProject/
// AgentsInfraProfile, never a value recovered from os.Getwd() or a direct
// process environment read.
func TestPreflightNeverFallsBackToAmbientState(t *testing.T) {
	reader := &fakeStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
	system := New(reader)

	req := agentic.LaunchRequest{Runtime: "local-qwen", Model: agentic.Model{ID: "m"}}
	if _, err := system.Preflight(context.Background(), req); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	got := reader.calls[0]
	if got.AgentsInfraProject != "" || got.AgentsInfraProfile != "" {
		t.Fatalf("StatusQuery = %+v, want empty project/profile with no ambient fallback", got)
	}
}
