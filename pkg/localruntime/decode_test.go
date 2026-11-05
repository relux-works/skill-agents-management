package localruntime

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// validFixture is the pinned, minimal shape every decode test starts from,
// mirroring the fields §2.3.2 of the architecture decision requires.
func validFixture() map[string]any {
	return map[string]any{
		"runtime_key":    "local-qwen@/home/op/project",
		"profile_digest": "deadbeef",
		"broker":         map[string]any{"state": "serving", "source": "attested"},
		"sharing":        map[string]any{"configured": map[string]any{"max_leases": 3}},
		"runtime":        map[string]any{"pid": 4242, "start_time": "2026-08-29T10:00:00Z"},
		"leases":         []any{map[string]any{"id": "lease-1"}},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling fixture: %v", err)
	}
	return data
}

// TestDecodeStatusHappyPath is case 19(f): the pinned fixture shape with no
// extra fields decodes cleanly.
func TestDecodeStatusHappyPath(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	status, err := decodeStatus(mustJSON(t, validFixture()), "local-qwen", "qwen-3.8-27b-mlx-8bit", now)
	if err != nil {
		t.Fatalf("decodeStatus: %v", err)
	}
	if status.BrokerState != "serving" || status.BrokerSource != SourceAttested {
		t.Fatalf("broker state/source = %q/%q, want serving/attested", status.BrokerState, status.BrokerSource)
	}
	if status.PID != 4242 {
		t.Fatalf("PID = %d, want 4242", status.PID)
	}
	if status.MaxLeases != 3 || status.ActiveLeases != 1 {
		t.Fatalf("leases = %d/%d, want 1/3", status.ActiveLeases, status.MaxLeases)
	}
	if status.Contract != StatusContract || status.SchemaVersion != StatusSchemaVersion {
		t.Fatalf("contract/version = %q/%d, want %q/%d", status.Contract, status.SchemaVersion, StatusContract, StatusSchemaVersion)
	}
	if !status.AsOf.Equal(now) {
		t.Fatalf("AsOf = %v, want %v", status.AsOf, now)
	}
}

// TestDecodeStatusExtraTopLevelFieldIsIgnored is case 19(e): additive-safe
// decode. A previously-unknown top-level field must not break decoding, and
// every currently-read field must still populate identically.
func TestDecodeStatusExtraTopLevelFieldIsIgnored(t *testing.T) {
	fixture := validFixture()
	fixture["future_extension_field"] = map[string]any{"value": 7}
	status, err := decodeStatus(mustJSON(t, fixture), "local-qwen", "m", time.Now())
	if err != nil {
		t.Fatalf("decodeStatus with an unknown extra field: %v", err)
	}
	if status.BrokerState != "serving" {
		t.Fatalf("BrokerState = %q, want serving (an unknown field must not disturb known ones)", status.BrokerState)
	}
}

// TestDecodeStatusPreRestartDeadlineFixture is adversarial case 27a. The
// first restart-status extension remains valid compatibility input: its facts
// are consumed, while the later restart_not_before/half_open facts remain
// explicitly absent rather than being fabricated from restart_count.
func TestDecodeStatusPreRestartDeadlineFixture(t *testing.T) {
	fixture := validFixture()
	fixture["restart_count"] = 2
	fixture["quarantined_until"] = nil
	fixture["last_readiness_match"] = "2026-08-29T12:00:00Z"
	fixture["manual_quarantine"] = false
	status, err := decodeStatus(mustJSON(t, fixture), "local-qwen", "m", time.Now())
	if err != nil {
		t.Fatalf("decodeStatus(pre-extension fixture): %v", err)
	}
	if !status.RestartCountPresent || status.RestartCount != 2 {
		t.Fatalf("restart_count = %d present=%t, want 2/true", status.RestartCount, status.RestartCountPresent)
	}
	if !status.QuarantinedUntilPresent || status.QuarantinedUntil != nil {
		t.Fatalf("quarantined_until = %v present=%t, want nil/true", status.QuarantinedUntil, status.QuarantinedUntilPresent)
	}
	wantReady := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if !status.LastReadinessMatchPresent || status.LastReadinessMatch == nil || !status.LastReadinessMatch.Equal(wantReady) {
		t.Fatalf("last_readiness_match = %v present=%t, want %v/true", status.LastReadinessMatch, status.LastReadinessMatchPresent, wantReady)
	}
	if !status.ManualQuarantinePresent || status.ManualQuarantine {
		t.Fatalf("manual_quarantine = %t present=%t, want false/true", status.ManualQuarantine, status.ManualQuarantinePresent)
	}
	if status.RestartNotBeforePresent || status.RestartNotBefore != nil || status.HalfOpenPresent || status.HalfOpen {
		t.Fatalf("pre-extension fixture fabricated later facts: %+v", status)
	}
}

// TestDecodeStatusPostRestartDeadlineFixture is adversarial case 27b's wire
// half. It pins the exact landed Handoff-A follow-up shape, including a
// present deadline and half-open lifecycle evidence.
func TestDecodeStatusPostRestartDeadlineFixture(t *testing.T) {
	fixture := validFixture()
	fixture["restart_count"] = 2
	fixture["restart_not_before"] = "2026-08-29T12:00:04Z"
	fixture["quarantined_until"] = nil
	fixture["last_readiness_match"] = "2026-08-29T12:00:00Z"
	fixture["manual_quarantine"] = false
	fixture["half_open"] = true
	status, err := decodeStatus(mustJSON(t, fixture), "local-qwen", "m", time.Now())
	if err != nil {
		t.Fatalf("decodeStatus(post-extension fixture): %v", err)
	}
	wantDeadline := time.Date(2026, 8, 29, 12, 0, 4, 0, time.UTC)
	if !status.RestartNotBeforePresent || status.RestartNotBefore == nil || !status.RestartNotBefore.Equal(wantDeadline) {
		t.Fatalf("restart_not_before = %v present=%t, want %v/true", status.RestartNotBefore, status.RestartNotBeforePresent, wantDeadline)
	}
	if !status.HalfOpenPresent || !status.HalfOpen {
		t.Fatalf("half_open = %t present=%t, want true/true", status.HalfOpen, status.HalfOpenPresent)
	}
	if !status.ManualQuarantinePresent || status.ManualQuarantine {
		t.Fatalf("manual_quarantine = %t present=%t, want false/true", status.ManualQuarantine, status.ManualQuarantinePresent)
	}
}

// TestDecodeStatusRestartExtensionWrongTypesAreRefused pins the extension's
// presence/type matrix. Present malformed facts are read failures, not legacy
// absence and not zero values.
func TestDecodeStatusRestartExtensionWrongTypesAreRefused(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{
		{"restart_count", "two"},
		{"restart_count", nil},
		{"restart_count", -1},
		{"restart_not_before", "not-a-timestamp"},
		{"restart_not_before", 42},
		{"quarantined_until", "not-a-timestamp"},
		{"last_readiness_match", "not-a-timestamp"},
		{"manual_quarantine", "false"},
		{"manual_quarantine", nil},
		{"half_open", "true"},
		{"half_open", nil},
	} {
		t.Run(tc.field, func(t *testing.T) {
			fixture := validFixture()
			for key, value := range map[string]any{
				"restart_count":        2,
				"restart_not_before":   "2026-08-29T12:00:04Z",
				"quarantined_until":    nil,
				"last_readiness_match": "2026-08-29T12:00:00Z",
				"manual_quarantine":    false,
				"half_open":            false,
			} {
				fixture[key] = value
			}
			fixture[tc.field] = tc.value
			_, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now())
			if !errors.Is(err, ErrDecodeFailure) {
				t.Fatalf("%s=%v: err = %v, want ErrDecodeFailure", tc.field, tc.value, err)
			}
		})
	}
}

// TestDecodeStatusRestartExtensionPartialCohortsAreRefused proves the cohort
// boundary by narrowing it one member at a time. The producer serializes all
// members of each cohort without omitempty, so a mixed response is a failed
// read rather than an older producer shape.
func TestDecodeStatusRestartExtensionPartialCohortsAreRefused(t *testing.T) {
	current := map[string]any{
		"restart_count":        2,
		"restart_not_before":   "2026-08-29T12:00:04Z",
		"quarantined_until":    nil,
		"last_readiness_match": "2026-08-29T12:00:00Z",
		"manual_quarantine":    false,
		"half_open":            false,
	}
	for field := range current {
		t.Run("current missing "+field, func(t *testing.T) {
			fixture := validFixture()
			for key, value := range current {
				if key != field {
					fixture[key] = value
				}
			}
			_, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now())
			if !errors.Is(err, ErrDecodeFailure) {
				t.Fatalf("current cohort missing %s: err = %v, want ErrDecodeFailure", field, err)
			}
		})
	}

	fixture := validFixture()
	fixture["restart_count"] = 2
	_, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now())
	if !errors.Is(err, ErrDecodeFailure) {
		t.Fatalf("partial pre-extension cohort: err = %v, want ErrDecodeFailure", err)
	}
}

// TestDecodeStatusMissingRequiredFieldIsRefused is case 19(a): a
// structurally-necessary field's absence is a decode failure naming the
// missing field.
func TestDecodeStatusMissingRequiredFieldIsRefused(t *testing.T) {
	for _, field := range []string{"runtime_key", "profile_digest", "broker", "sharing", "leases"} {
		t.Run(field, func(t *testing.T) {
			fixture := validFixture()
			delete(fixture, field)
			_, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now())
			if !errors.Is(err, ErrDecodeFailure) {
				t.Fatalf("missing %q: err = %v, want ErrDecodeFailure", field, err)
			}
		})
	}
}

// TestDecodeStatusWrongTypeIsRefused is case 19(b): a field present with the
// wrong JSON type is a decode failure naming the mismatch.
func TestDecodeStatusWrongTypeIsRefused(t *testing.T) {
	fixture := validFixture()
	fixture["sharing"] = map[string]any{"configured": map[string]any{"max_leases": "three"}}
	if _, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now()); !errors.Is(err, ErrDecodeFailure) {
		t.Fatalf("max_leases as a string: err = %v, want ErrDecodeFailure", err)
	}
}

// TestDecodeStatusRuntimeAbsentIsNotAnError is case 19(c): `runtime`
// absent/null means no process observed, not a decode failure.
func TestDecodeStatusRuntimeAbsentIsNotAnError(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"absent": func(f map[string]any) { delete(f, "runtime") },
		"null":   func(f map[string]any) { f["runtime"] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := validFixture()
			mutate(fixture)
			status, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now())
			if err != nil {
				t.Fatalf("runtime %s: %v", name, err)
			}
			if status.PID != 0 || !status.StartedAt.IsZero() {
				t.Fatalf("runtime %s: PID/StartedAt = %d/%v, want zero values", name, status.PID, status.StartedAt)
			}
		})
	}
}

// TestDecodeStatusRuntimePresentButMalformedIsRefused is case 19(d):
// `runtime` present but malformed inside is a decode failure, never folded
// into the legitimate-absence shape above.
func TestDecodeStatusRuntimePresentButMalformedIsRefused(t *testing.T) {
	fixture := validFixture()
	fixture["runtime"] = map[string]any{"pid": "not-a-number", "start_time": "2026-08-29T10:00:00Z"}
	if _, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now()); !errors.Is(err, ErrDecodeFailure) {
		t.Fatalf("malformed runtime object: err = %v, want ErrDecodeFailure", err)
	}
}

// TestDecodeStatusEveryFrozenPairDecodes is case 19b(a): each of the eleven
// real (state, source) pairs decodes to the matching Status fields.
func TestDecodeStatusEveryFrozenPairDecodes(t *testing.T) {
	for pair := range frozenBrokerPairs {
		t.Run(pair.state+"/"+string(pair.source), func(t *testing.T) {
			fixture := validFixture()
			fixture["broker"] = map[string]any{"state": pair.state, "source": string(pair.source)}
			status, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now())
			if err != nil {
				t.Fatalf("decodeStatus(%s, %s): %v", pair.state, pair.source, err)
			}
			if status.BrokerState != pair.state || status.BrokerSource != pair.source {
				t.Fatalf("got (%s, %s), want (%s, %s)", status.BrokerState, status.BrokerSource, pair.state, pair.source)
			}
		})
	}
	if len(frozenBrokerPairs) != 11 {
		t.Fatalf("frozenBrokerPairs has %d entries, want 11", len(frozenBrokerPairs))
	}
}

// TestDecodeStatusNeverJointlyReachablePairIsRefused is case 19b(b): a
// syntactically well-formed pair whose two strings each appear somewhere in
// the vocabulary, but never TOGETHER, must be a decode failure.
func TestDecodeStatusNeverJointlyReachablePairIsRefused(t *testing.T) {
	fixture := validFixture()
	fixture["broker"] = map[string]any{"state": "serving", "source": "determined"}
	_, err := decodeStatus(mustJSON(t, fixture), "r", "m", time.Now())
	if !errors.Is(err, ErrDecodeFailure) {
		t.Fatalf("(serving, determined): err = %v, want ErrDecodeFailure", err)
	}
}

// TestValidBrokerPairChecksTheJointPairNotEachStringIndependently is case
// 19b(c)'s narrowing negative: a mutant that validated state and source
// against their own closed sets independently, without checking the PAIR,
// would accept (serving, determined) because both strings individually
// appear in the vocabulary. This test pins the real function's answer
// directly, so that mutant fails here even if a decode-level assertion were
// ever weakened.
func TestValidBrokerPairChecksTheJointPairNotEachStringIndependently(t *testing.T) {
	knownStates := map[string]bool{}
	knownSources := map[BrokerObservationSource]bool{}
	for pair := range frozenBrokerPairs {
		knownStates[pair.state] = true
		knownSources[pair.source] = true
	}
	if !knownStates["serving"] || !knownSources[SourceDetermined] {
		t.Fatal("test setup: expected 'serving' and 'determined' to each individually be known")
	}
	if ValidBrokerPair("serving", SourceDetermined) {
		t.Fatal("ValidBrokerPair(serving, determined) = true; each string is individually known but the PAIR is never jointly reachable")
	}
}
