// Package inferenceengine specifies the observed inference-engine boundary.
// agents-infra owns process, SSH, polling, signal, and supervision execution.
package inferenceengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

const ContractVersion = "observed-process/v2"
const ExecutionOwner = "agents-infra"

type EngineKind string

const (
	EngineKindNativeTransformer EngineKind = "native-transformer"
	EngineKindGGUFServer        EngineKind = "gguf-server"
)

type Fact string

const (
	FactContextArgv              Fact = "context-argv"
	FactPrefillArgv              Fact = "prefill-argv"
	FactReasoningStreamField     Fact = "reasoning-stream-field"
	FactHealth                   Fact = "health"
	FactReadiness                Fact = "readiness"
	FactWeightArtifact           Fact = "weight-artifact"
	FactMemoryAccounting         Fact = "memory-accounting"
	FactSpeculativeDecoding      Fact = "speculative-decoding"
	FactLoadState                Fact = "load-state"
	FactUnloadState              Fact = "unload-state"
	FactInferenceBusy            Fact = "inference-busy"
	FactMemoryPressureSequence   Fact = "memory-pressure-sequence"
	FactLocalExecutable          Fact = "profile-local-executable"
	FactLocalArgv                Fact = "profile-local-argv"
	FactSSHForwarding            Fact = "profile-ssh-forwarding"
	FactStressPolicy             Fact = "profile-stress-policy"
	FactRestartSupervisionPolicy Fact = "profile-restart-supervision-policy"
)

type FactDefinition struct {
	Fact     Fact
	Meaning  string
	Evidence []string
}

var measuredFacts = []FactDefinition{
	{FactContextArgv, "effective context/KV capacity and exact argv spelling", []string{"TASK-260828-2jbufw", "TASK-260828-3fgca3"}},
	{FactPrefillArgv, "prefill chunk size and exact argv spelling", []string{"TASK-260828-2jbufw", "TASK-260828-3fgca3"}},
	{FactReasoningStreamField, "reasoning TTFT stream field", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactHealth, "engine liveness endpoint and response", []string{"TASK-260827-qyebv8", "TASK-260828-2jbufw"}},
	{FactReadiness, "positive proof that weights are resident", []string{"TASK-260827-qyebv8", "TASK-260828-2jbufw"}},
	{FactWeightArtifact, "complete safetensors/config or GGUF/mmproj shape", []string{"TASK-260828-2jbufw", "TASK-260828-2wcrph"}},
	{FactMemoryAccounting, "artifact-aware memory accounting", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactSpeculativeDecoding, "runtime-observed capability and active state", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactLoadState, "transition establishing resident weights", []string{"TASK-260827-qyebv8", "TASK-260829-1qh0ud"}},
	{FactUnloadState, "transition establishing non-resident weights", []string{"TASK-260829-1qh0ud"}},
	{FactInferenceBusy, "active inference state", []string{"TASK-260829-1qh0ud"}},
	{FactMemoryPressureSequence, "pressure decision after load/unload/busy consultation", []string{"TASK-260829-1qh0ud"}},
	{FactLocalExecutable, "profile-expanded local executable", []string{"TASK-260830-12n20p"}},
	{FactLocalArgv, "profile-expanded local argv", []string{"TASK-260828-2jbufw", "TASK-260830-12n20p"}},
	{FactSSHForwarding, "remote forwarding selected instead of local execution", []string{"TASK-260828-2jbufw", "TASK-260830-12n20p"}},
	{FactStressPolicy, "profile stress policy", []string{"TASK-260830-12n20p"}},
	{FactRestartSupervisionPolicy, "profile restart/backoff policy", []string{"TASK-260829-2t5xmi", "TASK-260830-12n20p"}},
}

func MeasuredFacts() []FactDefinition {
	result := make([]FactDefinition, len(measuredFacts))
	for i, definition := range measuredFacts {
		result[i] = definition
		result[i].Evidence = append([]string(nil), definition.Evidence...)
	}
	return result
}

type FailureAction string

const FailureActionRefuse FailureAction = "refuse"

type FailurePolicy struct{ ReadFailure, Malformed, Unsupported FailureAction }

// ValueContract is closed and fact-specific. There is no arbitrary-object
// grammar that can turn syntactically valid JSON into a semantic observation.
type ValueContract string

const (
	ValueContractArgvTokens             ValueContract = "argv-tokens/v1"
	ValueContractJSONFieldPath          ValueContract = "json-field-path/v1"
	ValueContractAbsolutePath           ValueContract = "absolute-path/v1"
	ValueContractHealth                 ValueContract = "health/v1"
	ValueContractReadiness              ValueContract = "readiness/v1"
	ValueContractWeightArtifact         ValueContract = "weight-artifact/v1"
	ValueContractMemoryAccounting       ValueContract = "memory-accounting/v1"
	ValueContractSpeculativeDecoding    ValueContract = "speculative-decoding/v1"
	ValueContractLoadState              ValueContract = "load-state/v1"
	ValueContractUnloadState            ValueContract = "unload-state/v1"
	ValueContractInferenceBusy          ValueContract = "inference-busy/v1"
	ValueContractMemoryPressureSequence ValueContract = "memory-pressure-sequence/v1"
	ValueContractSSHForwarding          ValueContract = "ssh-forwarding/v1"
	ValueContractStressPolicy           ValueContract = "stress-policy/v1"
	ValueContractRestartPolicy          ValueContract = "restart-policy/v1"
)

type ObservationRule struct {
	Fact          Fact
	Method        string
	ValueContract ValueContract
	OnFailure     FailurePolicy
}
type ModelHarnessExpansion struct {
	LocalExecutable, LocalArgv, SSHForwarding, StressPolicy, RestartSupervision Fact
	ExecutionOwner                                                              string
}
type Contract struct {
	Version          string
	Rules            []ObservationRule
	ProfileExpansion ModelHarnessExpansion
}

type Outcome string

const (
	OutcomeObservedValue  Outcome = "observed-value"
	OutcomeObservedAbsent Outcome = "observed-absent"
	OutcomeNotObserved    Outcome = "not-observed"
)

type NotObservedCause string

const (
	NotObservedReadFailure NotObservedCause = "read-failure"
	NotObservedMalformed   NotObservedCause = "malformed"
	NotObservedUnsupported NotObservedCause = "unsupported"
)

type observationMetadata struct {
	fact          Fact
	method        string
	valueContract ValueContract
}
type ObservationResult interface {
	observationResult()
	metadata() observationMetadata
	Fact() Fact
	Outcome() Outcome
}
type ObservedValue struct {
	observationMetadata
	value string
}

func (ObservedValue) observationResult()              {}
func (r ObservedValue) metadata() observationMetadata { return r.observationMetadata }
func (r ObservedValue) Fact() Fact                    { return r.fact }
func (ObservedValue) Outcome() Outcome                { return OutcomeObservedValue }
func (r ObservedValue) Value() string                 { return r.value }
func (r ObservedValue) ValueContract() ValueContract  { return r.valueContract }

type ObservedAbsent struct{ observationMetadata }

func (ObservedAbsent) observationResult()              {}
func (r ObservedAbsent) metadata() observationMetadata { return r.observationMetadata }
func (r ObservedAbsent) Fact() Fact                    { return r.fact }
func (ObservedAbsent) Outcome() Outcome                { return OutcomeObservedAbsent }

type NotObserved struct {
	observationMetadata
	cause  NotObservedCause
	detail string
}

func (NotObserved) observationResult()              {}
func (r NotObserved) metadata() observationMetadata { return r.observationMetadata }
func (r NotObserved) Fact() Fact                    { return r.fact }
func (NotObserved) Outcome() Outcome                { return OutcomeNotObserved }
func (r NotObserved) Cause() NotObservedCause       { return r.cause }
func (r NotObserved) Detail() string                { return r.detail }

// ReadOutcome is the only data returned by the agents-infra read boundary.
// It cannot stamp a fact, method, engine label, or value contract.
type ReadOutcome struct {
	outcome Outcome
	value   string
	cause   NotObservedCause
	detail  string
}

func ReadValue(raw string) ReadOutcome { return ReadOutcome{outcome: OutcomeObservedValue, value: raw} }
func ReadAbsent() ReadOutcome          { return ReadOutcome{outcome: OutcomeObservedAbsent} }
func ReadFailure(cause NotObservedCause, detail string) ReadOutcome {
	return ReadOutcome{outcome: OutcomeNotObserved, cause: cause, detail: detail}
}

// Reading is an untrusted value at the schema boundary. Constructing one does
// not authorize a launch: vendorplugin.BuildLaunch obtains readings only from
// its package-owned engineFactSource and calls ValidateReadings before Spawn,
// Preflight, plan materialization, or any execution effect.
type Reading struct {
	fact    Fact
	outcome ReadOutcome
}

// NewReading labels one raw read for schema validation. It is deliberately a
// validator input, not evidence provenance or a production composition seam.
func NewReading(fact Fact, outcome ReadOutcome) Reading { return Reading{fact: fact, outcome: outcome} }

var (
	ErrEngineContractMissing   = errors.New("inferenceengine: concrete engine is not composed")
	ErrContractInvalid         = errors.New("inferenceengine: contract is invalid")
	ErrObservationRead         = errors.New("inferenceengine: observation read failed")
	ErrObservationMalformed    = errors.New("inferenceengine: observation is malformed")
	ErrObservationUnsupported  = errors.New("inferenceengine: observation is unsupported")
	ErrProfileExpansionInvalid = errors.New("inferenceengine: local and SSH profile expansion is invalid")
)

type ProfileMode string

const (
	ProfileModeLocal ProfileMode = "local"
	ProfileModeSSH   ProfileMode = "ssh"
)

type Resolution struct {
	Declaration plugin.Declaration
	EngineKind  EngineKind
	ProfileMode ProfileMode
	Contract    Contract
	Results     []ObservationResult
}

// ValidateReadings applies the closed engine contract to untrusted raw reads.
// It is intentionally not the production authorization entry: callers can
// construct Reading values, while only vendorplugin.BuildLaunch owns the
// source whose readings reach this validator before launch effects.
func ValidateReadings(id plugin.ID, kind EngineKind, readings []Reading) (Resolution, error) {
	if kind != EngineKindNativeTransformer && kind != EngineKindGGUFServer {
		return Resolution{}, fmt.Errorf("%w: %q has unsupported implementation kind %q", ErrEngineContractMissing, id, kind)
	}
	contract, err := validateContract(contractFor(kind))
	if err != nil {
		return Resolution{}, err
	}
	if len(readings) != len(contract.Rules) {
		return Resolution{}, fmt.Errorf("%w: %q returned %d facts, want %d", ErrObservationMalformed, id, len(readings), len(contract.Rules))
	}
	results := make([]ObservationResult, 0, len(contract.Rules))
	for index, rule := range contract.Rules {
		reading := readings[index]
		if reading.fact != rule.Fact {
			return Resolution{}, fmt.Errorf("%w: fact %d is %q, want %q", ErrObservationMalformed, index, reading.fact, rule.Fact)
		}
		result := deriveObservation(rule, reading.outcome)
		if err := validateResult(rule, result); err != nil {
			return Resolution{}, err
		}
		results = append(results, result)
	}
	if err := validateCrossFactInvariants(results); err != nil {
		return Resolution{}, err
	}
	mode, err := validateProfileExpansion(results)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{plugin.Declaration{ID: id, Kind: Kind}, kind, mode, cloneContract(contract), append([]ObservationResult(nil), results...)}, nil
}

func deriveObservation(rule ObservationRule, read ReadOutcome) ObservationResult {
	metadata := metadataFromRule(rule)
	switch read.outcome {
	case OutcomeObservedValue:
		value, err := canonicalizeValue(rule.ValueContract, read.value)
		if err != nil {
			return NotObserved{metadata, NotObservedMalformed, err.Error()}
		}
		return ObservedValue{metadata, value}
	case OutcomeObservedAbsent:
		return ObservedAbsent{metadata}
	case OutcomeNotObserved:
		return NotObserved{metadata, read.cause, read.detail}
	default:
		return NotObserved{metadata, NotObservedMalformed, "reader returned unknown outcome"}
	}
}
func metadataFromRule(rule ObservationRule) observationMetadata {
	return observationMetadata{rule.Fact, rule.Method, rule.ValueContract}
}
func validateResult(rule ObservationRule, result ObservationResult) error {
	switch typed := result.(type) {
	case ObservedValue:
		if typed.metadata() != metadataFromRule(rule) {
			return fmt.Errorf("%w: %s: metadata mismatch", ErrObservationMalformed, rule.Fact)
		}
		value, err := canonicalizeValue(rule.ValueContract, typed.value)
		if err != nil || value != typed.value {
			return fmt.Errorf("%w: %s: violates %s", ErrObservationMalformed, rule.Fact, rule.ValueContract)
		}
	case ObservedAbsent:
		if typed.metadata() != metadataFromRule(rule) {
			return fmt.Errorf("%w: %s: metadata mismatch", ErrObservationMalformed, rule.Fact)
		}
	case NotObserved:
		switch typed.cause {
		case NotObservedReadFailure:
			return fmt.Errorf("%w: %s: %s", ErrObservationRead, rule.Fact, typed.detail)
		case NotObservedMalformed:
			return fmt.Errorf("%w: %s: %s", ErrObservationMalformed, rule.Fact, typed.detail)
		case NotObservedUnsupported:
			return fmt.Errorf("%w: %s: %s", ErrObservationUnsupported, rule.Fact, typed.detail)
		default:
			return fmt.Errorf("%w: %s: unknown cause %q", ErrObservationMalformed, rule.Fact, typed.cause)
		}
	default:
		return fmt.Errorf("%w: %s: unrecognized result %T", ErrObservationMalformed, rule.Fact, result)
	}
	return nil
}

func validateProfileExpansion(results []ObservationResult) (ProfileMode, error) {
	states := map[Fact]Outcome{}
	for _, result := range results {
		states[result.Fact()] = result.Outcome()
	}
	if states[FactLocalExecutable] == OutcomeObservedValue && states[FactLocalArgv] == OutcomeObservedValue && states[FactSSHForwarding] == OutcomeObservedAbsent {
		return ProfileModeLocal, nil
	}
	if states[FactLocalExecutable] == OutcomeObservedAbsent && states[FactLocalArgv] == OutcomeObservedAbsent && states[FactSSHForwarding] == OutcomeObservedValue {
		return ProfileModeSSH, nil
	}
	return "", fmt.Errorf("%w: local requires executable+argv values and SSH absence; SSH requires local absence and forwarding", ErrProfileExpansionInvalid)
}

func validateCrossFactInvariants(results []ObservationResult) error {
	values := map[Fact]string{}
	for _, result := range results {
		if value, ok := result.(ObservedValue); ok {
			values[result.Fact()] = value.Value()
		}
	}
	artifact, artifactOK := values[FactWeightArtifact]
	accounting, accountingOK := values[FactMemoryAccounting]
	if !artifactOK || !accountingOK {
		return nil
	}
	var artifactShape struct {
		Format string `json:"format"`
	}
	var accountingShape struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal([]byte(artifact), &artifactShape); err != nil {
		return fmt.Errorf("%w: weight artifact invariant: %v", ErrObservationMalformed, err)
	}
	if err := json.Unmarshal([]byte(accounting), &accountingShape); err != nil {
		return fmt.Errorf("%w: memory accounting invariant: %v", ErrObservationMalformed, err)
	}
	if artifactShape.Format == "gguf" && accountingShape.Method != "mmap-aware" {
		return fmt.Errorf("%w: GGUF requires mmap-aware memory accounting", ErrObservationMalformed)
	}
	return nil
}

func contractFor(kind EngineKind) Contract {
	rules := make([]ObservationRule, 0, len(measuredFacts))
	refuse := FailurePolicy{FailureActionRefuse, FailureActionRefuse, FailureActionRefuse}
	for _, definition := range measuredFacts {
		rules = append(rules, ObservationRule{definition.Fact, "agents-infra/" + string(kind) + "/" + string(definition.Fact), valueContractForFact(definition.Fact), refuse})
	}
	return Contract{ContractVersion, rules, ModelHarnessExpansion{FactLocalExecutable, FactLocalArgv, FactSSHForwarding, FactStressPolicy, FactRestartSupervisionPolicy, ExecutionOwner}}
}
func validateContract(raw Contract) (Contract, error) {
	if raw.Version != ContractVersion || len(raw.Rules) != len(measuredFacts) {
		return Contract{}, fmt.Errorf("%w: version/inventory mismatch", ErrContractInvalid)
	}
	for i, definition := range measuredFacts {
		rule := raw.Rules[i]
		if rule.Fact != definition.Fact || strings.TrimSpace(rule.Method) == "" || rule.ValueContract != valueContractForFact(rule.Fact) {
			return Contract{}, fmt.Errorf("%w: rule %d does not match %q", ErrContractInvalid, i, definition.Fact)
		}
		if rule.OnFailure != (FailurePolicy{FailureActionRefuse, FailureActionRefuse, FailureActionRefuse}) {
			return Contract{}, fmt.Errorf("%w: %s failure policy", ErrContractInvalid, rule.Fact)
		}
	}
	if raw.ProfileExpansion != (ModelHarnessExpansion{FactLocalExecutable, FactLocalArgv, FactSSHForwarding, FactStressPolicy, FactRestartSupervisionPolicy, ExecutionOwner}) {
		return Contract{}, fmt.Errorf("%w: profile expansion", ErrContractInvalid)
	}
	return cloneContract(raw), nil
}

func valueContractForFact(fact Fact) ValueContract {
	switch fact {
	case FactContextArgv, FactPrefillArgv, FactLocalArgv:
		return ValueContractArgvTokens
	case FactReasoningStreamField:
		return ValueContractJSONFieldPath
	case FactLocalExecutable:
		return ValueContractAbsolutePath
	case FactHealth:
		return ValueContractHealth
	case FactReadiness:
		return ValueContractReadiness
	case FactWeightArtifact:
		return ValueContractWeightArtifact
	case FactMemoryAccounting:
		return ValueContractMemoryAccounting
	case FactSpeculativeDecoding:
		return ValueContractSpeculativeDecoding
	case FactLoadState:
		return ValueContractLoadState
	case FactUnloadState:
		return ValueContractUnloadState
	case FactInferenceBusy:
		return ValueContractInferenceBusy
	case FactMemoryPressureSequence:
		return ValueContractMemoryPressureSequence
	case FactSSHForwarding:
		return ValueContractSSHForwarding
	case FactStressPolicy:
		return ValueContractStressPolicy
	case FactRestartSupervisionPolicy:
		return ValueContractRestartPolicy
	default:
		return ""
	}
}

var jsonFieldPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`)

func canonicalizeValue(contract ValueContract, raw string) (string, error) {
	switch contract {
	case ValueContractArgvTokens:
		var value []string
		if err := decodeClosed(raw, &value); err != nil || len(value) == 0 {
			return "", fmt.Errorf("argv requires non-empty string array: %v", err)
		}
		for i, token := range value {
			if token == "" {
				return "", fmt.Errorf("argv token %d empty", i)
			}
		}
		return encode(value)
	case ValueContractJSONFieldPath:
		if !jsonFieldPath.MatchString(raw) {
			return "", errors.New("invalid field path")
		}
		return raw, nil
	case ValueContractAbsolutePath:
		if !cleanAbsolute(raw) {
			return "", errors.New("path must be absolute and clean")
		}
		return raw, nil
	case ValueContractHealth:
		var value struct {
			Endpoint string `json:"endpoint"`
			Healthy  *bool  `json:"healthy"`
		}
		if err := decodeClosed(raw, &value); err != nil || !strings.HasPrefix(value.Endpoint, "/") || value.Healthy == nil {
			return "", fmt.Errorf("health requires endpoint and boolean healthy: %v", err)
		}
		return encode(value)
	case ValueContractReadiness:
		var value struct {
			WeightsResident *bool `json:"weights_resident"`
		}
		if err := decodeClosed(raw, &value); err != nil || value.WeightsResident == nil || !*value.WeightsResident {
			return "", fmt.Errorf("readiness requires weights_resident=true: %v", err)
		}
		return encode(value)
	case ValueContractWeightArtifact:
		var value struct {
			Format     string `json:"format"`
			ModelPath  string `json:"model_path,omitempty"`
			ConfigPath string `json:"config_path,omitempty"`
			MMProjPath string `json:"mmproj_path,omitempty"`
		}
		if err := decodeClosed(raw, &value); err != nil {
			return "", err
		}
		if value.Format == "safetensors" {
			if !cleanAbsolute(value.ModelPath) || !cleanAbsolute(value.ConfigPath) || filepath.Base(value.ConfigPath) != "config.json" || value.MMProjPath != "" {
				return "", errors.New("invalid safetensors shape")
			}
		} else if value.Format == "gguf" {
			if !cleanAbsolute(value.ModelPath) || !strings.EqualFold(filepath.Ext(value.ModelPath), ".gguf") || value.ConfigPath != "" || (value.MMProjPath != "" && (!cleanAbsolute(value.MMProjPath) || !strings.EqualFold(filepath.Ext(value.MMProjPath), ".gguf"))) {
				return "", errors.New("invalid gguf shape")
			}
		} else {
			return "", errors.New("unknown weight format")
		}
		return encode(value)
	case ValueContractMemoryAccounting:
		var value struct {
			Method                string `json:"method"`
			Bytes                 *int64 `json:"bytes"`
			IncludesMappedWeights *bool  `json:"includes_mapped_weights"`
		}
		if err := decodeClosed(raw, &value); err != nil || value.Bytes == nil || *value.Bytes < 0 || value.IncludesMappedWeights == nil {
			return "", fmt.Errorf("invalid memory accounting: %v", err)
		}
		if value.Method != "resident-bytes" && value.Method != "mmap-aware" {
			return "", errors.New("invalid accounting method")
		}
		if value.Method == "mmap-aware" && !*value.IncludesMappedWeights {
			return "", errors.New("mmap-aware must include mapped weights")
		}
		return encode(value)
	case ValueContractSpeculativeDecoding:
		var value struct {
			Capable *bool `json:"capable"`
			Active  *bool `json:"active"`
		}
		if err := decodeClosed(raw, &value); err != nil || value.Capable == nil || value.Active == nil || *value.Active && !*value.Capable {
			return "", fmt.Errorf("invalid speculative state: %v", err)
		}
		return encode(value)
	case ValueContractLoadState:
		return lifecycle(raw, "loaded", true)
	case ValueContractUnloadState:
		return lifecycle(raw, "unloaded", false)
	case ValueContractInferenceBusy:
		var value struct {
			Busy *bool `json:"busy"`
		}
		if err := decodeClosed(raw, &value); err != nil || value.Busy == nil {
			return "", err
		}
		return encode(value)
	case ValueContractMemoryPressureSequence:
		var value struct {
			Pressure  string `json:"pressure"`
			Consulted []Fact `json:"consulted"`
			Action    string `json:"action"`
		}
		if err := decodeClosed(raw, &value); err != nil {
			return "", err
		}
		if value.Pressure != "normal" && value.Pressure != "warning" && value.Pressure != "critical" {
			return "", errors.New("invalid pressure")
		}
		want := []Fact{FactLoadState, FactUnloadState, FactInferenceBusy}
		if len(value.Consulted) != len(want) {
			return "", errors.New("missing consultations")
		}
		for i := range want {
			if value.Consulted[i] != want[i] {
				return "", errors.New("invalid consultation order")
			}
		}
		if value.Action != "none" && value.Action != "unload-idle" && value.Action != "refuse" {
			return "", errors.New("invalid pressure action")
		}
		return encode(value)
	case ValueContractSSHForwarding:
		var value struct {
			Host       string `json:"host"`
			LocalPort  *int   `json:"local_port"`
			RemotePort *int   `json:"remote_port"`
		}
		if err := decodeClosed(raw, &value); err != nil || strings.TrimSpace(value.Host) == "" || value.LocalPort == nil || value.RemotePort == nil || !validPort(*value.LocalPort) || !validPort(*value.RemotePort) {
			return "", fmt.Errorf("invalid ssh forwarding: %v", err)
		}
		return encode(value)
	case ValueContractStressPolicy:
		var value struct {
			Enabled        *bool `json:"enabled"`
			MaxConcurrency *int  `json:"max_concurrency"`
		}
		if err := decodeClosed(raw, &value); err != nil || value.Enabled == nil || value.MaxConcurrency == nil || *value.MaxConcurrency < 1 {
			return "", fmt.Errorf("invalid stress policy: %v", err)
		}
		return encode(value)
	case ValueContractRestartPolicy:
		var value struct {
			MaxAttempts      *int `json:"max_attempts"`
			InitialBackoffMS *int `json:"initial_backoff_ms"`
			MaxBackoffMS     *int `json:"max_backoff_ms"`
		}
		if err := decodeClosed(raw, &value); err != nil || value.MaxAttempts == nil || value.InitialBackoffMS == nil || value.MaxBackoffMS == nil || *value.MaxAttempts < 0 || *value.InitialBackoffMS < 1 || *value.MaxBackoffMS < *value.InitialBackoffMS {
			return "", fmt.Errorf("invalid restart policy: %v", err)
		}
		return encode(value)
	default:
		return "", fmt.Errorf("unknown value contract %q", contract)
	}
}
func lifecycle(raw, state string, resident bool) (string, error) {
	var value struct {
		State           string `json:"state"`
		WeightsResident *bool  `json:"weights_resident"`
	}
	if err := decodeClosed(raw, &value); err != nil || value.State != state || value.WeightsResident == nil || *value.WeightsResident != resident {
		return "", fmt.Errorf("invalid %s lifecycle: %v", state, err)
	}
	return encode(value)
}
func cleanAbsolute(value string) bool { return filepath.IsAbs(value) && filepath.Clean(value) == value }
func validPort(value int) bool        { return value > 0 && value <= 65535 }
func decodeClosed(raw string, destination any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}
func encode(value any) (string, error) {
	encoded, err := json.Marshal(value)
	return string(encoded), err
}
func cloneContract(contract Contract) Contract {
	cloned := contract
	cloned.Rules = append([]ObservationRule(nil), contract.Rules...)
	return cloned
}
