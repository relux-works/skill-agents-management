package localmodels

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
				RestartNotBeforePresent: true,
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
	verdict := mapAvailability(localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested, ActiveLeases: 3, MaxLeases: 3, RestartNotBeforePresent: true})
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
	reader := &scriptedStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested, ActiveLeases: 0, MaxLeases: 1, RestartNotBeforePresent: true}}
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

// TestCheckAvailabilityConsumesRestartExtensionFixtures is adversarial case
// 27b through the production vendorplugin.CheckAvailability call site and the
// real CLI status decoder. No helper-minted Status can satisfy this proof.
func TestCheckAvailabilityConsumesRestartExtensionFixtures(t *testing.T) {
	checkedAt := time.Date(2026, 8, 29, 12, 0, 1, 0, time.UTC)
	backoffUntil := checkedAt.Add(3 * time.Second)
	quarantinedUntil := checkedAt.Add(30 * time.Second)
	readyAt := checkedAt.Add(-time.Second)

	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   vendorplugin.AvailabilityState
		until  time.Time
	}{
		{
			name: "active backoff",
			mutate: func(f map[string]any) {
				f["restart_count"] = 2
				f["restart_not_before"] = backoffUntil.Format(time.RFC3339)
				f["quarantined_until"] = nil
				f["last_readiness_match"] = readyAt.Format(time.RFC3339)
				f["manual_quarantine"] = false
				f["half_open"] = false
			},
			want: vendorplugin.AvailabilityLimited, until: backoffUntil,
		},
		{
			name: "active quarantine takes precedence",
			mutate: func(f map[string]any) {
				f["restart_count"] = 4
				f["restart_not_before"] = backoffUntil.Format(time.RFC3339)
				f["quarantined_until"] = quarantinedUntil.Format(time.RFC3339)
				f["last_readiness_match"] = readyAt.Format(time.RFC3339)
				f["manual_quarantine"] = false
				f["half_open"] = false
			},
			want: vendorplugin.AvailabilityLimited, until: quarantinedUntil,
		},
		{
			name: "historical restart count and half-open do not imply backoff",
			mutate: func(f map[string]any) {
				f["restart_count"] = 2
				f["restart_not_before"] = nil
				f["quarantined_until"] = nil
				f["last_readiness_match"] = readyAt.Format(time.RFC3339)
				f["manual_quarantine"] = false
				f["half_open"] = true
			},
			want: vendorplugin.AvailabilityHealthy,
		},
		{
			name: "elapsed deadlines do not imply a current limit",
			mutate: func(f map[string]any) {
				f["restart_count"] = 2
				f["restart_not_before"] = checkedAt.Add(-time.Second).Format(time.RFC3339)
				f["quarantined_until"] = checkedAt.Format(time.RFC3339)
				f["last_readiness_match"] = readyAt.Format(time.RFC3339)
				f["manual_quarantine"] = false
				f["half_open"] = false
			},
			want: vendorplugin.AvailabilityHealthy,
		},
		{
			name: "pre-deadline fixture cannot prove backoff inactive",
			mutate: func(f map[string]any) {
				f["restart_count"] = 2
				f["quarantined_until"] = nil
				f["last_readiness_match"] = readyAt.Format(time.RFC3339)
				f["manual_quarantine"] = false
			},
			want: vendorplugin.AvailabilityUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := restartExtensionAvailabilityFixture(t, tc.mutate)
			reader := localruntime.NewCLIStatusReader(
				localruntime.WithCommandRunner(func(context.Context, string, []string) ([]byte, error) { return fixture, nil }),
				localruntime.WithClock(func() time.Time { return checkedAt }),
			)
			registry := registryWithPi(t)
			if err := registry.Register(availabilityFixtureVendor(reader)); err != nil {
				t.Fatalf("Register: %v", err)
			}
			verdict, err := vendorplugin.CheckAvailability(registry, VendorID, vendorplugin.AvailabilityQuery{
				Model: "qwen-3.8-27b-mlx-8bit", Runtime: "local-qwen",
			})
			if err != nil {
				t.Fatalf("CheckAvailability: %v", err)
			}
			if verdict.State != tc.want || !verdict.Until.Equal(tc.until) {
				t.Fatalf("verdict state/until = %s/%v, want %s/%v; verdict=%+v", verdict.State, verdict.Until, tc.want, tc.until, verdict)
			}
		})
	}
}

// TestCheckAvailabilityRefusesMalformedRestartDeadlineAsAReadFailure is the
// negative production-call-site proof: malformed evidence must not be
// treated as an absent or elapsed deadline and admitted as Healthy.
func TestCheckAvailabilityRefusesMalformedRestartDeadlineAsAReadFailure(t *testing.T) {
	fixture := restartExtensionAvailabilityFixture(t, func(f map[string]any) {
		f["restart_count"] = 2
		f["restart_not_before"] = "not-a-timestamp"
		f["quarantined_until"] = nil
		f["last_readiness_match"] = nil
		f["manual_quarantine"] = false
		f["half_open"] = false
	})
	reader := localruntime.NewCLIStatusReader(
		localruntime.WithCommandRunner(func(context.Context, string, []string) ([]byte, error) { return fixture, nil }),
	)
	registry := registryWithPi(t)
	if err := registry.Register(availabilityFixtureVendor(reader)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	verdict, err := vendorplugin.CheckAvailability(registry, VendorID, vendorplugin.AvailabilityQuery{
		Model: "qwen-3.8-27b-mlx-8bit", Runtime: "local-qwen",
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityUnknown || len(verdict.Failures) == 0 {
		t.Fatalf("malformed restart_not_before was not surfaced as Unknown with failure evidence: %+v", verdict)
	}
}

// TestCheckAvailabilityRefusesPartialRestartStatusCohorts drives each
// narrowed current cohort through CLIStatusReader and the production
// vendorplugin.CheckAvailability entry point. No omitted lifecycle fact may
// be laundered into Healthy or a legacy fallback.
func TestCheckAvailabilityRefusesPartialRestartStatusCohorts(t *testing.T) {
	current := map[string]any{
		"restart_count":        2,
		"restart_not_before":   nil,
		"quarantined_until":    nil,
		"last_readiness_match": "2026-08-29T12:00:00Z",
		"manual_quarantine":    false,
		"half_open":            false,
	}
	for field := range current {
		t.Run("missing "+field, func(t *testing.T) {
			fixture := restartExtensionAvailabilityFixture(t, func(f map[string]any) {
				for key, value := range current {
					if key != field {
						f[key] = value
					}
				}
			})
			reader := localruntime.NewCLIStatusReader(
				localruntime.WithCommandRunner(func(context.Context, string, []string) ([]byte, error) { return fixture, nil }),
			)
			registry := registryWithPi(t)
			if err := registry.Register(availabilityFixtureVendor(reader)); err != nil {
				t.Fatalf("Register: %v", err)
			}
			verdict, err := vendorplugin.CheckAvailability(registry, VendorID, vendorplugin.AvailabilityQuery{
				Model: "qwen-3.8-27b-mlx-8bit", Runtime: "local-qwen",
			})
			if err != nil {
				t.Fatalf("CheckAvailability: %v", err)
			}
			if verdict.State != vendorplugin.AvailabilityUnknown || len(verdict.Failures) == 0 {
				t.Fatalf("partial current cohort missing %s was not Unknown with read-failure evidence: %+v", field, verdict)
			}
		})
	}
}

func restartExtensionAvailabilityFixture(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	fixture := map[string]any{
		"runtime_key":    "local-qwen@/home/op/project",
		"profile_digest": "deadbeef",
		"broker":         map[string]any{"state": "serving", "source": "attested"},
		"sharing":        map[string]any{"configured": map[string]any{"max_leases": 3}},
		"runtime":        map[string]any{"pid": 4242, "start_time": "2026-08-29T10:00:00Z"},
		"leases":         []any{},
	}
	mutate(fixture)
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
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
