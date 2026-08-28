package localmodels

import (
	"context"
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// scriptedStatusReader is the fake StatusReader §1 of the adversarial plan
// describes: scriptable to return any of the eleven frozen pairs, a read
// error, and it records every call's exact StatusQuery.
type scriptedStatusReader struct {
	status localruntime.Status
	err    error
	calls  []localruntime.StatusQuery
}

func (s *scriptedStatusReader) Status(_ context.Context, query localruntime.StatusQuery) (localruntime.Status, error) {
	s.calls = append(s.calls, query)
	if s.err != nil {
		return localruntime.Status{}, s.err
	}
	return s.status, nil
}

func availabilityFixtureVendor(reader localruntime.StatusReader) *Vendor {
	return New(validConfig(), WithStatusReader(reader))
}

// TestAvailabilityMappingTable is case 18: every mapping in the source-aware
// table, driven through the real CheckAvailability entry point, each
// resulting Availability independently passing Availability.Validate().
func TestAvailabilityMappingTable(t *testing.T) {
	cases := []struct {
		name        string
		state       string
		src         localruntime.BrokerObservationSource
		active, max int
		want        vendorplugin.AvailabilityState
	}{
		{"serving/attested below cap", "serving", localruntime.SourceAttested, 1, 3, vendorplugin.AvailabilityHealthy},
		{"serving/attested at cap", "serving", localruntime.SourceAttested, 3, 3, vendorplugin.AvailabilityUnknown},
		{"starting/attested", "starting", localruntime.SourceAttested, 0, 3, vendorplugin.AvailabilityUnknown},
		{"lingering/attested", "lingering", localruntime.SourceAttested, 0, 3, vendorplugin.AvailabilityHealthy},
		{"draining/attested", "draining", localruntime.SourceAttested, 0, 3, vendorplugin.AvailabilityUnreachable},
		{"absent/determined", "absent", localruntime.SourceDetermined, 0, 0, vendorplugin.AvailabilityUnreachable},
		{"starting-unverified/candidate-only", "starting-unverified", localruntime.SourceCandidateOnly, 0, 0, vendorplugin.AvailabilityUnknown},
		{"starting/record-derived-unverified", "starting", localruntime.SourceRecordUnverified, 0, 3, vendorplugin.AvailabilityUnknown},
		{"serving/record-derived-unverified", "serving", localruntime.SourceRecordUnverified, 1, 3, vendorplugin.AvailabilityUnknown},
		{"lingering/record-derived-unverified", "lingering", localruntime.SourceRecordUnverified, 0, 3, vendorplugin.AvailabilityUnknown},
		{"draining/record-derived-unverified", "draining", localruntime.SourceRecordUnverified, 0, 3, vendorplugin.AvailabilityUnknown},
		{"unverified-stale/record-derived-unverified", "unverified-stale", localruntime.SourceRecordUnverified, 0, 3, vendorplugin.AvailabilityUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &scriptedStatusReader{status: localruntime.Status{
				BrokerState: tc.state, BrokerSource: tc.src, ActiveLeases: tc.active, MaxLeases: tc.max,
			}}
			vendor := availabilityFixtureVendor(reader)
			registry := registryWithPi(t)
			if err := registry.Register(vendor); err != nil {
				t.Fatalf("Register: %v", err)
			}
			verdict, err := vendorplugin.CheckAvailability(registry, VendorID, vendorplugin.AvailabilityQuery{
				Model: "qwen-3.8-27b-mlx-8bit", Runtime: "local-qwen",
			})
			if err != nil {
				t.Fatalf("CheckAvailability: %v", err)
			}
			if verdict.State != tc.want {
				t.Fatalf("state = %s, want %s", verdict.State, tc.want)
			}
			if err := verdict.Validate(); err != nil {
				t.Fatalf("verdict failed its own Validate(): %v", err)
			}
		})
	}
}

// TestAvailabilityServingAtCapacityNeverFabricatesALimitedUntil is case
// 18(b)'s narrowing negative, pinned directly against the mapping function:
// a mutant that maps "at capacity" to Limited with a fabricated Until must
// fail this specific assertion, not just the generic state check above.
func TestAvailabilityServingAtCapacityNeverFabricatesALimitedUntil(t *testing.T) {
	verdict := mapAvailability(localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested, ActiveLeases: 3, MaxLeases: 3})
	if verdict.State == vendorplugin.AvailabilityLimited {
		t.Fatal("at-capacity serving mapped to Limited; no evidence-derived clear time exists in v1")
	}
	if !verdict.Until.IsZero() {
		t.Fatalf("Until = %v, want zero", verdict.Until)
	}
}

// TestAvailabilityReadFailureIsUnknownWithFailures is case 18(i).
func TestAvailabilityReadFailureIsUnknownWithFailures(t *testing.T) {
	reader := &scriptedStatusReader{err: errors.New("agents-infra: exit status 1")}
	vendor := availabilityFixtureVendor(reader)
	registry := registryWithPi(t)
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register: %v", err)
	}
	verdict, err := vendorplugin.CheckAvailability(registry, VendorID, vendorplugin.AvailabilityQuery{
		Model: "qwen-3.8-27b-mlx-8bit", Runtime: "local-qwen",
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityUnknown {
		t.Fatalf("state = %s, want Unknown", verdict.State)
	}
	if len(verdict.Failures) == 0 {
		t.Fatal("a read failure produced a verdict with no Failures evidence")
	}
	if err := verdict.Validate(); err != nil {
		t.Fatalf("verdict failed Validate(): %v", err)
	}
}

// TestAvailabilityDisambiguatesByRuntime is case 16b: two declared
// RuntimeIDs sharing one vendor never have their Availability answers
// conflated, and a runtime with no pointer for the model gets its own
// refusal rather than silently falling back to the other's answer.
func TestAvailabilityDisambiguatesByRuntime(t *testing.T) {
	reader := &scriptedStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested, ActiveLeases: 0, MaxLeases: 1}}
	vendor := New(twoRuntimeConfig(), WithStatusReader(reader))
	registry := registryWithPi(t)
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register: %v", err)
	}

	healthy, err := vendorplugin.CheckAvailability(registry, VendorID, vendorplugin.AvailabilityQuery{Model: "qwen-3.8-27b-mlx-8bit", Runtime: "local-qwen"})
	if err != nil {
		t.Fatalf("CheckAvailability(local-qwen): %v", err)
	}
	if healthy.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("local-qwen state = %s, want Healthy", healthy.State)
	}

	_, err = vendorplugin.CheckAvailability(registry, VendorID, vendorplugin.AvailabilityQuery{Model: "qwen-3.8-27b-mlx-8bit", Runtime: "local-test-profile"})
	if err == nil {
		t.Fatal("CheckAvailability(local-test-profile), which has no pointer for this model, returned no error")
	}

	if len(reader.calls) != 1 {
		t.Fatalf("StatusReader was called %d times, want exactly 1 (the second query never reached a live read because no pointer resolved)", len(reader.calls))
	}
	if reader.calls[0].Runtime != "local-qwen" {
		t.Fatalf("recorded query.Runtime = %q, want local-qwen", reader.calls[0].Runtime)
	}
}

// TestAvailabilityEmptyModelIsUnchecked matches Spawn's own "ask about the
// vendor as a whole" gap: this vendor answers per model, so an empty query
// asks nothing rather than guessing which model was meant.
func TestAvailabilityEmptyModelIsUnchecked(t *testing.T) {
	vendor := availabilityFixtureVendor(&scriptedStatusReader{})
	verdict, err := vendor.Availability(vendorplugin.AvailabilityQuery{Runtime: "local-qwen"})
	if err != nil {
		t.Fatalf("Availability(no model): %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityUnknown || len(verdict.Checked) != 0 {
		t.Fatalf("verdict = %+v, want Unchecked", verdict)
	}
}
