package localruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// validFixture starts from the captured v0.1.0 producer payload. It removes
// only the optional restart-status cohort so legacy-generation tests can add
// exactly the fields their scenario exercises.
func validFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/status-v0.1.0-runtime-present.json")
	if err != nil {
		t.Fatalf("read curator-engines status golden: %v", err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode curator-engines status golden: %v", err)
	}
	for _, field := range append(append([]string{}, restartStatusPreExtensionFields...), restartStatusCurrentOnlyFields...) {
		delete(fixture, field)
	}
	return fixture
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling fixture: %v", err)
	}
	return data
}

// statusThroughCLIReader exercises the production Status entrypoint while
// substituting only the subprocess result.
func statusThroughCLIReader(raw []byte) (Status, error) {
	reader := NewCLIStatusReader(WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) {
		return raw, nil
	}))
	return reader.Status(context.Background(), StatusQuery{
		Runtime:               "test-runtime",
		Model:                 "test-model",
		CuratorEnginesProject: "/project",
		CuratorEnginesProfile: "qwen-local-engine",
	})
}

// TestDecodeStatusHappyPath is case 19(f): the captured released status shape
// decodes cleanly.
func TestDecodeStatusHappyPath(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fixture := validFixture(t)
	runtime := fixture["runtime"].(map[string]any)
	start := runtime["start_time"].(map[string]any)
	wantStart := time.Unix(int64(start["seconds"].(float64)), int64(start["microseconds"].(float64))*1000)
	status, err := decodeStatus(mustJSON(t, fixture), "qwen-local-engine", "qwen-3.8-27b-mlx-8bit", "qwen-local-engine", now)
	if err != nil {
		t.Fatalf("decodeStatus: %v", err)
	}
	if status.BrokerState != "serving" || status.BrokerSource != SourceAttested {
		t.Fatalf("broker state/source = %q/%q, want serving/attested", status.BrokerState, status.BrokerSource)
	}
	if status.PID != int(runtime["pid"].(float64)) {
		t.Fatalf("PID = %d, want %v", status.PID, runtime["pid"])
	}
	sharing := fixture["sharing"].(map[string]any)
	configured := sharing["configured"].(map[string]any)
	if status.MaxLeases != int(configured["max_leases"].(float64)) || status.ActiveLeases != len(fixture["leases"].([]any)) {
		t.Fatalf("leases = %d/%d, want %d/%v", status.ActiveLeases, status.MaxLeases, len(fixture["leases"].([]any)), configured["max_leases"])
	}
	if !status.StartedAt.Equal(wantStart) {
		t.Fatalf("StartedAt = %v, want %v", status.StartedAt, wantStart)
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
	fixture := validFixture(t)
	fixture["future_extension_field"] = map[string]any{"value": 7}
	status, err := decodeStatus(mustJSON(t, fixture), "qwen-local-engine", "m", "qwen-local-engine", time.Now())
	if err != nil {
		t.Fatalf("decodeStatus with an unknown extra field: %v", err)
	}
	if status.BrokerState != "serving" {
		t.Fatalf("BrokerState = %q, want serving (an unknown field must not disturb known ones)", status.BrokerState)
	}
}

func TestCLIStatusReaderDecodesCapturedCuratorEnginesV010Status(t *testing.T) {
	raw, err := os.ReadFile("testdata/status-v0.1.0-runtime-present.json")
	if err != nil {
		t.Fatalf("read curator-engines v0.1.0 status golden: %v", err)
	}
	if bytes.Contains(raw, []byte("/Users/")) || bytes.Contains(raw, []byte("/home/")) {
		t.Fatal("captured status golden contains a personal home path")
	}
	if bytes.Contains(raw, []byte(`"lease_token"`)) {
		t.Fatal("captured status golden unexpectedly contains a lease token field")
	}
	var payload struct {
		EngineIdentity struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"engine_identity"`
		Broker struct {
			State  string `json:"state"`
			Source string `json:"source"`
		} `json:"broker"`
		Sharing struct {
			Configured struct {
				MaxLeases int `json:"max_leases"`
			} `json:"configured"`
		} `json:"sharing"`
		Runtime *struct {
			PID       int `json:"pid"`
			StartTime struct {
				Seconds      int64 `json:"seconds"`
				Microseconds int32 `json:"microseconds"`
			} `json:"start_time"`
		} `json:"runtime"`
		Leases []struct {
			EngineIdentity  *wireEngineIdentity   `json:"engine_identity"`
			ClientStartTime *wireProcessStartTime `json:"client_start_time"`
		} `json:"leases"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode captured curator-engines status: %v", err)
	}
	if payload.Runtime == nil {
		t.Fatal("captured status has no runtime")
	}
	status, err := statusThroughCLIReader(raw)
	if err != nil {
		t.Fatalf("CLIStatusReader.Status(captured v0.1.0 status): %v", err)
	}
	wantStartedAt := time.Unix(payload.Runtime.StartTime.Seconds, int64(payload.Runtime.StartTime.Microseconds)*1000)
	if status.PID != payload.Runtime.PID || !status.StartedAt.Equal(wantStartedAt) {
		t.Fatalf("runtime = pid %d at %v, want pid %d at %v", status.PID, status.StartedAt, payload.Runtime.PID, wantStartedAt)
	}
	if status.BrokerState != payload.Broker.State || status.BrokerSource != BrokerObservationSource(payload.Broker.Source) {
		t.Fatalf("broker = (%q, %q), want (%q, %q)", status.BrokerState, status.BrokerSource, payload.Broker.State, payload.Broker.Source)
	}
	if status.MaxLeases != payload.Sharing.Configured.MaxLeases || status.ActiveLeases != len(payload.Leases) {
		t.Fatalf("leases = active %d/max %d, want active %d/max %d", status.ActiveLeases, status.MaxLeases, len(payload.Leases), payload.Sharing.Configured.MaxLeases)
	}
	if payload.EngineIdentity.Name != "qwen-local-engine" || payload.EngineIdentity.Key == "" {
		t.Fatalf("fixture engine identity = (%q, %q), want selected engine and non-empty key", payload.EngineIdentity.Name, payload.EngineIdentity.Key)
	}
	for i, lease := range payload.Leases {
		if lease.EngineIdentity == nil || lease.EngineIdentity.Name == "" || lease.EngineIdentity.Key == "" {
			t.Fatalf("fixture leases[%d].engine_identity = %+v, want released identity object", i, lease.EngineIdentity)
		}
		if lease.ClientStartTime == nil || lease.ClientStartTime.Seconds == nil || lease.ClientStartTime.Microseconds == nil {
			t.Fatalf("fixture leases[%d].client_start_time = %+v, want released ProcessStartTime object", i, lease.ClientStartTime)
		}
		if *lease.ClientStartTime.Seconds < 0 || *lease.ClientStartTime.Microseconds < 0 || *lease.ClientStartTime.Microseconds > 999999 {
			t.Fatalf("fixture leases[%d].client_start_time = %+v, want released ProcessStartTime range", i, lease.ClientStartTime)
		}
	}
}

func TestCLIStatusReaderDecodesStartTimeAsExactUnixInstant(t *testing.T) {
	fixture := validFixture(t)
	runtime := fixture["runtime"].(map[string]any)
	runtime["start_time"] = map[string]any{"seconds": int64(1_800_000_123), "microseconds": int32(456789)}
	status, err := statusThroughCLIReader(mustJSON(t, fixture))
	if err != nil {
		t.Fatalf("CLIStatusReader.Status: %v", err)
	}
	want := time.Unix(1_800_000_123, 456_789_000)
	if !status.StartedAt.Equal(want) {
		t.Fatalf("StartedAt = %v, want exact instant %v", status.StartedAt, want)
	}
}

func TestCLIStatusReaderRejectsInvalidStartTime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "string", mutate: func(f map[string]any) { f["runtime"].(map[string]any)["start_time"] = "2026-08-29T10:00:00Z" }},
		{name: "missing start_time", mutate: func(f map[string]any) { delete(f["runtime"].(map[string]any), "start_time") }},
		{name: "null start_time", mutate: func(f map[string]any) { f["runtime"].(map[string]any)["start_time"] = nil }},
		{name: "missing seconds", mutate: func(f map[string]any) {
			delete(f["runtime"].(map[string]any)["start_time"].(map[string]any), "seconds")
		}},
		{name: "missing microseconds", mutate: func(f map[string]any) {
			delete(f["runtime"].(map[string]any)["start_time"].(map[string]any), "microseconds")
		}},
		{name: "non-integer seconds", mutate: func(f map[string]any) { f["runtime"].(map[string]any)["start_time"].(map[string]any)["seconds"] = 1.5 }},
		{name: "non-integer microseconds", mutate: func(f map[string]any) {
			f["runtime"].(map[string]any)["start_time"].(map[string]any)["microseconds"] = 1.5
		}},
		{name: "negative seconds", mutate: func(f map[string]any) {
			f["runtime"].(map[string]any)["start_time"].(map[string]any)["seconds"] = int64(-1)
		}},
		{name: "microseconds below zero", mutate: func(f map[string]any) {
			f["runtime"].(map[string]any)["start_time"].(map[string]any)["microseconds"] = int32(-1)
		}},
		{name: "microseconds above range", mutate: func(f map[string]any) {
			f["runtime"].(map[string]any)["start_time"].(map[string]any)["microseconds"] = int32(1_000_000)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := validFixture(t)
			tc.mutate(fixture)
			_, err := statusThroughCLIReader(mustJSON(t, fixture))
			if !errors.Is(err, ErrDecodeFailure) {
				t.Fatalf("CLIStatusReader.Status error = %v, want ErrDecodeFailure", err)
			}
		})
	}
}

func TestDecodeStatusRefusesUnsupportedContractVersionAndWrongEngine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "unsupported status contract", mutate: func(f map[string]any) { f["contract_version"] = 2 }},
		{name: "wrong selected engine", mutate: func(f map[string]any) { f["engine_identity"] = map[string]any{"name": "other", "key": "engine-key"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := validFixture(t)
			tc.mutate(fixture)
			_, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
			if !errors.Is(err, ErrDecodeFailure) {
				t.Fatalf("decodeStatus error = %v, want ErrDecodeFailure", err)
			}
		})
	}
}

// TestDecodeStatusPreRestartDeadlineFixture is adversarial case 27a. The
// first restart-status extension remains valid compatibility input: its facts
// are consumed, while the later restart_not_before/half_open facts remain
// explicitly absent rather than being fabricated from restart_count.
func TestDecodeStatusPreRestartDeadlineFixture(t *testing.T) {
	fixture := validFixture(t)
	fixture["restart_count"] = 2
	fixture["quarantined_until"] = nil
	fixture["last_readiness_match"] = "2026-08-29T12:00:00Z"
	fixture["manual_quarantine"] = false
	status, err := decodeStatus(mustJSON(t, fixture), "qwen-local-engine", "m", "qwen-local-engine", time.Now())
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
	fixture := validFixture(t)
	fixture["restart_count"] = 2
	fixture["restart_not_before"] = "2026-08-29T12:00:04Z"
	fixture["quarantined_until"] = nil
	fixture["last_readiness_match"] = "2026-08-29T12:00:00Z"
	fixture["manual_quarantine"] = false
	fixture["half_open"] = true
	status, err := decodeStatus(mustJSON(t, fixture), "qwen-local-engine", "m", "qwen-local-engine", time.Now())
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
			fixture := validFixture(t)
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
			_, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
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
			fixture := validFixture(t)
			for key, value := range current {
				if key != field {
					fixture[key] = value
				}
			}
			_, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
			if !errors.Is(err, ErrDecodeFailure) {
				t.Fatalf("current cohort missing %s: err = %v, want ErrDecodeFailure", field, err)
			}
		})
	}

	fixture := validFixture(t)
	fixture["restart_count"] = 2
	_, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
	if !errors.Is(err, ErrDecodeFailure) {
		t.Fatalf("partial pre-extension cohort: err = %v, want ErrDecodeFailure", err)
	}
}

// TestDecodeStatusMissingRequiredFieldIsRefused is case 19(a): a
// structurally-necessary field's absence is a decode failure naming the
// missing field.
func TestDecodeStatusMissingRequiredFieldIsRefused(t *testing.T) {
	for _, field := range []string{"contract_version", "engine_identity", "runtime_key", "profile_digest", "broker", "sharing", "leases"} {
		t.Run(field, func(t *testing.T) {
			fixture := validFixture(t)
			delete(fixture, field)
			_, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
			if !errors.Is(err, ErrDecodeFailure) {
				t.Fatalf("missing %q: err = %v, want ErrDecodeFailure", field, err)
			}
		})
	}
}

// TestDecodeStatusWrongTypeIsRefused is case 19(b): a field present with the
// wrong JSON type is a decode failure naming the mismatch.
func TestDecodeStatusWrongTypeIsRefused(t *testing.T) {
	fixture := validFixture(t)
	fixture["sharing"] = map[string]any{"configured": map[string]any{"max_leases": "three"}}
	if _, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now()); !errors.Is(err, ErrDecodeFailure) {
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
			fixture := validFixture(t)
			mutate(fixture)
			status, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
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
	fixture := validFixture(t)
	fixture["runtime"] = map[string]any{"pid": "not-a-number", "start_time": map[string]any{"seconds": int64(1), "microseconds": int32(0)}}
	if _, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now()); !errors.Is(err, ErrDecodeFailure) {
		t.Fatalf("malformed runtime object: err = %v, want ErrDecodeFailure", err)
	}
}

// TestDecodeStatusEveryFrozenPairDecodes is case 19b(a): each of the eleven
// real (state, source) pairs decodes to the matching Status fields.
func TestDecodeStatusEveryFrozenPairDecodes(t *testing.T) {
	for pair := range frozenBrokerPairs {
		t.Run(pair.state+"/"+string(pair.source), func(t *testing.T) {
			fixture := validFixture(t)
			fixture["broker"] = map[string]any{"state": pair.state, "source": string(pair.source)}
			status, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
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
	fixture := validFixture(t)
	fixture["broker"] = map[string]any{"state": "serving", "source": "determined"}
	_, err := decodeStatus(mustJSON(t, fixture), "r", "m", "qwen-local-engine", time.Now())
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
