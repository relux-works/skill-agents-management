package localruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrDecodeFailure is returned for every shape this adapter cannot honestly
// read: a missing structurally-required field, a field of the wrong JSON
// type, or a syntactically well-formed (BrokerState, BrokerSource) pair that
// is not one of the eleven jointly-reachable pairs the shipped broker can
// actually produce.
var ErrDecodeFailure = errors.New("localruntime: status response failed to decode")

var (
	restartStatusPreExtensionFields = []string{
		"restart_count",
		"quarantined_until",
		"last_readiness_match",
		"manual_quarantine",
	}
	restartStatusCurrentOnlyFields = []string{
		"restart_not_before",
		"half_open",
	}
)

// brokerPair is one (state, source) combination.
type brokerPair struct {
	state  string
	source BrokerObservationSource
}

// frozenBrokerPairs is the closed, source-cited set of (BrokerState,
// BrokerSource) pairs the shipped broker can produce, read directly off the
// architecture decision's own frozen table. Eleven pairs total: one
// positively-absent, one candidate-only, five record-derived-unverified (one
// per BrokerState including "unverified-stale"), four attested (one per
// live BrokerState except "unverified-stale", which is a record-only
// concept).
//
// A pair outside this set — including a syntactically valid state paired
// with a source that could never jointly produce it, such as
// ("serving", "determined") — is a decode failure, never silently accepted
// because each string individually appears somewhere in the vocabulary.
var frozenBrokerPairs = map[brokerPair]bool{
	{"absent", SourceDetermined}: true,

	{"starting-unverified", SourceCandidateOnly}: true,

	{"starting", SourceRecordUnverified}:         true,
	{"serving", SourceRecordUnverified}:          true,
	{"lingering", SourceRecordUnverified}:        true,
	{"draining", SourceRecordUnverified}:         true,
	{"unverified-stale", SourceRecordUnverified}: true,

	{"starting", SourceAttested}:  true,
	{"serving", SourceAttested}:   true,
	{"lingering", SourceAttested}: true,
	{"draining", SourceAttested}:  true,
}

// ValidBrokerPair reports whether (state, source) is one of the eleven
// frozen, jointly-reachable pairs. It is exported so a fake StatusReader in
// another package's tests can validate what it scripts against the same
// closed vocabulary this adapter enforces.
func ValidBrokerPair(state string, source BrokerObservationSource) bool {
	return frozenBrokerPairs[brokerPair{state: state, source: source}]
}

// wireBroker is the `broker` object's shape.
type wireBroker struct {
	State  string `json:"state"`
	Source string `json:"source"`
}

// wireSharingConfigured is the `sharing.configured` object's shape.
type wireSharingConfigured struct {
	MaxLeases int `json:"max_leases"`
}

// wireSharing is the `sharing` object's shape.
type wireSharing struct {
	Configured wireSharingConfigured `json:"configured"`
}

// wireRuntime is the nullable `runtime` object's shape.
type wireRuntime struct {
	PID       int    `json:"pid"`
	StartTime string `json:"start_time"`
}

// decodeStatus decodes one `agents-infra runtime status --json` response.
//
// It decodes into a map[string]json.RawMessage first specifically so
// "missing" and "present" are distinguishable — json.Unmarshal into a
// destination struct leaves the Go zero value for an absent field, which is
// indistinguishable from a legitimate zero, and this adapter must not
// confuse the two for `runtime`, whose absence is a real, common,
// non-error answer.
func decodeStatus(raw []byte, runtime RuntimeID, model ModelID, now time.Time) (Status, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Status{}, fmt.Errorf("%w: response is not a JSON object: %v", ErrDecodeFailure, err)
	}

	runtimeKey, err := requiredNonEmptyString(fields, "runtime_key")
	if err != nil {
		return Status{}, err
	}
	_ = runtimeKey // structurally required and validated; this adapter does not otherwise use it

	if _, err := requiredString(fields, "profile_digest"); err != nil {
		return Status{}, err
	}

	brokerRaw, ok := fields["broker"]
	if !ok {
		return Status{}, fmt.Errorf("%w: missing field %q", ErrDecodeFailure, "broker")
	}
	var broker wireBroker
	if err := json.Unmarshal(brokerRaw, &broker); err != nil {
		return Status{}, fmt.Errorf("%w: field %q is not an object with string state/source: %v", ErrDecodeFailure, "broker", err)
	}
	if strings.TrimSpace(broker.State) == "" || strings.TrimSpace(broker.Source) == "" {
		return Status{}, fmt.Errorf("%w: %q requires a non-empty state and source", ErrDecodeFailure, "broker")
	}
	if !ValidBrokerPair(broker.State, BrokerObservationSource(broker.Source)) {
		return Status{}, fmt.Errorf("%w: (%s, %s) is not one of the frozen, jointly-reachable broker (state, source) pairs",
			ErrDecodeFailure, broker.State, broker.Source)
	}

	sharingRaw, ok := fields["sharing"]
	if !ok {
		return Status{}, fmt.Errorf("%w: missing field %q", ErrDecodeFailure, "sharing")
	}
	var sharing wireSharing
	if err := json.Unmarshal(sharingRaw, &sharing); err != nil {
		return Status{}, fmt.Errorf("%w: field %q is not an object with sharing.configured.max_leases: %v", ErrDecodeFailure, "sharing", err)
	}
	if sharing.Configured.MaxLeases < 0 {
		return Status{}, fmt.Errorf("%w: sharing.configured.max_leases is negative", ErrDecodeFailure)
	}

	pid, startedAt, err := decodeOptionalRuntime(fields)
	if err != nil {
		return Status{}, err
	}

	leaseCount, err := decodeLeases(fields)
	if err != nil {
		return Status{}, err
	}
	if err := validateRestartStatusCohort(fields); err != nil {
		return Status{}, err
	}

	restartCount, restartCountPresent, err := optionalNonNegativeInt(fields, "restart_count")
	if err != nil {
		return Status{}, err
	}
	restartNotBefore, restartNotBeforePresent, err := optionalNullableTimestamp(fields, "restart_not_before")
	if err != nil {
		return Status{}, err
	}
	quarantinedUntil, quarantinedUntilPresent, err := optionalNullableTimestamp(fields, "quarantined_until")
	if err != nil {
		return Status{}, err
	}
	lastReadinessMatch, lastReadinessMatchPresent, err := optionalNullableTimestamp(fields, "last_readiness_match")
	if err != nil {
		return Status{}, err
	}
	manualQuarantine, manualQuarantinePresent, err := optionalBool(fields, "manual_quarantine")
	if err != nil {
		return Status{}, err
	}
	halfOpen, halfOpenPresent, err := optionalBool(fields, "half_open")
	if err != nil {
		return Status{}, err
	}

	return Status{
		Contract:                  StatusContract,
		SchemaVersion:             StatusSchemaVersion,
		Runtime:                   runtime,
		Model:                     model,
		BrokerState:               broker.State,
		BrokerSource:              BrokerObservationSource(broker.Source),
		PID:                       pid,
		StartedAt:                 startedAt,
		ActiveLeases:              leaseCount,
		MaxLeases:                 sharing.Configured.MaxLeases,
		RestartCount:              restartCount,
		RestartCountPresent:       restartCountPresent,
		RestartNotBefore:          restartNotBefore,
		RestartNotBeforePresent:   restartNotBeforePresent,
		QuarantinedUntil:          quarantinedUntil,
		QuarantinedUntilPresent:   quarantinedUntilPresent,
		LastReadinessMatch:        lastReadinessMatch,
		LastReadinessMatchPresent: lastReadinessMatchPresent,
		ManualQuarantine:          manualQuarantine,
		ManualQuarantinePresent:   manualQuarantinePresent,
		HalfOpen:                  halfOpen,
		HalfOpenPresent:           halfOpenPresent,
		AsOf:                      now,
	}, nil
}

// validateRestartStatusCohort accepts only the three producer generations
// frozen by the shared-runtime wire fixtures: no lifecycle fields (legacy),
// the complete pre-deadline cohort, or the complete current cohort. Every
// lifecycle field is serialized without omitempty by its producer, so any
// mixed presence is a partial failed read rather than forward compatibility.
func validateRestartStatusCohort(fields map[string]json.RawMessage) error {
	present := func(keys []string) int {
		count := 0
		for _, key := range keys {
			if _, ok := fields[key]; ok {
				count++
			}
		}
		return count
	}

	preCount := present(restartStatusPreExtensionFields)
	currentOnlyCount := present(restartStatusCurrentOnlyFields)
	switch {
	case preCount == 0 && currentOnlyCount == 0:
		return nil
	case preCount == len(restartStatusPreExtensionFields) && currentOnlyCount == 0:
		return nil
	case preCount == len(restartStatusPreExtensionFields) && currentOnlyCount == len(restartStatusCurrentOnlyFields):
		return nil
	default:
		return fmt.Errorf("%w: restart status fields form a partial cohort (pre-extension %d/%d, current-only %d/%d)",
			ErrDecodeFailure, preCount, len(restartStatusPreExtensionFields), currentOnlyCount, len(restartStatusCurrentOnlyFields))
	}
}

// decodeOptionalRuntime decodes the nullable `runtime` object. Absent or
// JSON null means no process was observed — a real, common answer, never a
// decode failure. Present-but-malformed IS a decode failure: this field is
// checked against null/absent explicitly, so an object that fails to parse
// once it exists cannot pass for "nothing observed".
func decodeOptionalRuntime(fields map[string]json.RawMessage) (pid int, startedAt time.Time, err error) {
	raw, ok := fields["runtime"]
	if !ok || strings.TrimSpace(string(raw)) == "null" {
		return 0, time.Time{}, nil
	}
	var wr wireRuntime
	if err := json.Unmarshal(raw, &wr); err != nil {
		return 0, time.Time{}, fmt.Errorf("%w: field %q is present but not an object with pid/start_time: %v", ErrDecodeFailure, "runtime", err)
	}
	parsed, err := time.Parse(time.RFC3339, wr.StartTime)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("%w: field %q.start_time is not an RFC3339 timestamp: %v", ErrDecodeFailure, "runtime", err)
	}
	return wr.PID, parsed, nil
}

// decodeLeases decodes the required `leases` array, returning its length.
// Nothing here reads an individual lease's shape — ActiveLeases is defined
// as len(Leases), and an empty array is a valid, common value.
func decodeLeases(fields map[string]json.RawMessage) (int, error) {
	raw, ok := fields["leases"]
	if !ok {
		return 0, fmt.Errorf("%w: missing field %q", ErrDecodeFailure, "leases")
	}
	var leases []json.RawMessage
	if err := json.Unmarshal(raw, &leases); err != nil {
		return 0, fmt.Errorf("%w: field %q is not an array: %v", ErrDecodeFailure, "leases", err)
	}
	return len(leases), nil
}

// requiredString decodes a structurally-required string field, refusing a
// missing key or a wrong JSON type. The value may itself be empty
// (profile_digest legally is).
func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("%w: missing field %q", ErrDecodeFailure, key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%w: field %q is not a string: %v", ErrDecodeFailure, key, err)
	}
	return value, nil
}

// requiredNonEmptyString is requiredString plus a non-empty check, for the
// one field (runtime_key) this adapter treats as required to be non-blank.
func requiredNonEmptyString(fields map[string]json.RawMessage, key string) (string, error) {
	value, err := requiredString(fields, key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: field %q is empty", ErrDecodeFailure, key)
	}
	return value, nil
}

// optionalNonNegativeInt decodes an additive integer field. Absence is
// accepted for compatibility with an older status producer; a present field
// must have the extension's exact type and range.
func optionalNonNegativeInt(fields map[string]json.RawMessage, key string) (value int, present bool, err error) {
	raw, present := fields[key]
	if !present {
		return 0, false, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return 0, true, fmt.Errorf("%w: field %q is null, not an integer", ErrDecodeFailure, key)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, true, fmt.Errorf("%w: field %q is not an integer: %v", ErrDecodeFailure, key, err)
	}
	if value < 0 {
		return 0, true, fmt.Errorf("%w: field %q is negative", ErrDecodeFailure, key)
	}
	return value, true, nil
}

// optionalNullableTimestamp preserves all three wire states: absent (legacy
// producer), null (current producer with no deadline/observation), and a
// strict RFC3339 timestamp. Present malformed input is a failed read, never
// laundered into absence.
func optionalNullableTimestamp(fields map[string]json.RawMessage, key string) (value *time.Time, present bool, err error) {
	raw, present := fields[key]
	if !present {
		return nil, false, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, true, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, true, fmt.Errorf("%w: field %q is not null or an RFC3339 string: %v", ErrDecodeFailure, key, err)
	}
	parsed, err := time.Parse(time.RFC3339, encoded)
	if err != nil {
		return nil, true, fmt.Errorf("%w: field %q is not an RFC3339 timestamp: %v", ErrDecodeFailure, key, err)
	}
	return &parsed, true, nil
}

func optionalBool(fields map[string]json.RawMessage, key string) (value bool, present bool, err error) {
	raw, present := fields[key]
	if !present {
		return false, false, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return false, true, fmt.Errorf("%w: field %q is null, not a boolean", ErrDecodeFailure, key)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, true, fmt.Errorf("%w: field %q is not a boolean: %v", ErrDecodeFailure, key, err)
	}
	return value, true, nil
}
