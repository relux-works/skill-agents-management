package inferenceengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// ContractVersion is the first observed-process inference-engine contract.
const ContractVersion = "observed-process/v1"

// ExecutionOwner names the repository that owns OS process, SSH, and
// supervision execution. Engine plugins declare observations and policy; they
// do not execute those mechanisms.
const ExecutionOwner = "agents-infra"

// Fact identifies one consumer-visible engine fact. The set is closed for a
// contract version so a newly required fact cannot be silently omitted by an
// older engine plugin.
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

// FactDefinition records why a fact exists. Evidence contains the task IDs
// whose measurements established the difference; it is specification
// provenance, not runtime evidence.
type FactDefinition struct {
	Fact     Fact
	Meaning  string
	Evidence []string
}

var measuredFacts = []FactDefinition{
	{FactContextArgv, "effective context/KV capacity and the exact argv spelling that established it (--max-kv-size for the measured MLX Swift runtime; --ctx-size for llama.cpp)", []string{"TASK-260828-2jbufw", "TASK-260828-3fgca3"}},
	{FactPrefillArgv, "prefill chunk size and the exact argv spelling that established it (--prefill-step-size for MLX; -ub/--ubatch-size for llama.cpp)", []string{"TASK-260828-2jbufw", "TASK-260828-3fgca3"}},
	{FactReasoningStreamField, "the stream field whose first non-empty delta defines reasoning TTFT (delta.reasoning or delta.reasoning_content)", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactHealth, "the engine-specific liveness endpoint and response semantics", []string{"TASK-260827-qyebv8", "TASK-260828-2jbufw"}},
	{FactReadiness, "the observation proving weights are resident, distinct from an endpoint merely answering", []string{"TASK-260827-qyebv8", "TASK-260828-2jbufw"}},
	{FactWeightArtifact, "the complete weight shape: safetensors plus config.json, or one GGUF plus an optional separate mmproj", []string{"TASK-260828-2jbufw", "TASK-260828-2wcrph"}},
	{FactMemoryAccounting, "the accounting method valid for the artifact mapping; Mach physical footprint alone is invalid for mmap-loaded GGUF weights", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactSpeculativeDecoding, "the capability and active state observed from the runtime; a GGUF MTP head does not prove an MLX build retained or enabled it", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactLoadState, "the observed transition that establishes weights became resident", []string{"TASK-260827-qyebv8", "TASK-260829-1qh0ud"}},
	{FactUnloadState, "the observed transition that establishes weights are no longer resident", []string{"TASK-260829-1qh0ud"}},
	{FactInferenceBusy, "whether inference is actively using the resident engine", []string{"TASK-260829-1qh0ud"}},
	{FactMemoryPressureSequence, "the observed pressure state and sequencing rule that consults load, unload, and inference-busy before relief", []string{"TASK-260829-1qh0ud"}},
	{FactLocalExecutable, "the locally observed model-harness executable selected by profile expansion", []string{"TASK-260830-12n20p"}},
	{FactLocalArgv, "the exact local argv token vector selected by profile expansion", []string{"TASK-260828-2jbufw", "TASK-260830-12n20p"}},
	{FactSSHForwarding, "the remote profile and forwarding declaration selected instead of a local executable", []string{"TASK-260828-2jbufw", "TASK-260830-12n20p"}},
	{FactStressPolicy, "the profile's declared stress policy; execution remains outside this module", []string{"TASK-260830-12n20p"}},
	{FactRestartSupervisionPolicy, "the profile's declared restart/backoff policy; supervision execution remains outside this module", []string{"TASK-260829-2t5xmi", "TASK-260830-12n20p"}},
}

// MeasuredFacts returns the closed fact inventory with defensive evidence
// copies. A consumer can render this as provenance without trusting a concrete
// engine plugin to cite itself.
func MeasuredFacts() []FactDefinition {
	result := make([]FactDefinition, len(measuredFacts))
	for i, definition := range measuredFacts {
		result[i] = definition
		result[i].Evidence = append([]string(nil), definition.Evidence...)
	}
	return result
}

// FailureAction is the result required when an observation cannot establish a
// fact. Version 1 intentionally recognizes only Refuse.
type FailureAction string

const FailureActionRefuse FailureAction = "refuse"

// FailurePolicy closes the three non-observed outcomes independently. There
// is no caller-value or configured-default field in this contract.
type FailurePolicy struct {
	ReadFailure FailureAction
	Malformed   FailureAction
	Unsupported FailureAction
}

// ValueContract is a closed grammar that the resolver can validate without
// trusting an engine's description of its own value.
type ValueContract string

const (
	ValueContractArgvTokens      ValueContract = "argv-tokens/v1"
	ValueContractJSONFieldPath   ValueContract = "json-field-path/v1"
	ValueContractAbsolutePath    ValueContract = "absolute-path/v1"
	ValueContractCanonicalObject ValueContract = "canonical-json-object/v1"
)

// ObservationRule tells an agents-infra-composed engine kind exactly what to
// derive and how to parse a value. FailurePolicy is mandatory even when an
// engine cannot express the fact: Unsupported=refuse is a supported
// declaration.
type ObservationRule struct {
	Fact          Fact
	Method        string
	ValueContract ValueContract
	OnFailure     FailurePolicy
}

// ModelHarnessExpansion binds model-harness profile expansion to the same
// observed fact rules. The strings are Fact keys rather than executable
// commands, so this module cannot accidentally become a second process owner.
type ModelHarnessExpansion struct {
	LocalExecutable    Fact
	LocalArgv          Fact
	SSHForwarding      Fact
	StressPolicy       Fact
	RestartSupervision Fact
	ExecutionOwner     string
}

// Contract is the declaration supplied by one inference-engine plugin.
type Contract struct {
	Version          string
	Rules            []ObservationRule
	ProfileExpansion ModelHarnessExpansion
}

// Engine is a graph plugin that declares and derives the complete
// observed-process contract for its engine kind. The concrete implementation
// is composed by agents-infra; ResolveObserved never accepts a caller-owned
// observer or caller value.
type Engine interface {
	plugin.Plugin
	EngineContract() Contract
	DeriveObservation(context.Context, ObservationRule) ObservationResult
}

// Outcome is the complete result vocabulary for one derivation. Absence is a
// successful measured fact; it is not a failed read and not a string value.
type Outcome string

const (
	OutcomeObservedValue  Outcome = "observed-value"
	OutcomeObservedAbsent Outcome = "observed-absent"
	OutcomeNotObserved    Outcome = "not-observed"
)

// NotObservedCause explains why an engine kind could not derive a fact. Every
// cause is a refusal at ResolveObserved; it can never become an absence.
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

// ObservationResult is sealed to this package so a caller cannot implement a
// look-alike result with self-stamped provenance fields. Engine kinds construct
// results through the functions below; the resolver independently revalidates
// metadata and canonical values before exposing them to consumers.
type ObservationResult interface {
	observationResult()
	metadata() observationMetadata
	Fact() Fact
	Outcome() Outcome
}

// ObservedValue is a process-derived value in the rule's canonical grammar.
// Its fields are private so a caller cannot overwrite the fact, method,
// contract, or value after derivation.
type ObservedValue struct {
	observationMetadata
	value string
}

func (ObservedValue) observationResult()                   {}
func (result ObservedValue) metadata() observationMetadata { return result.observationMetadata }
func (result ObservedValue) Fact() Fact                    { return result.fact }
func (ObservedValue) Outcome() Outcome                     { return OutcomeObservedValue }
func (result ObservedValue) Value() string                 { return result.value }
func (result ObservedValue) ValueContract() ValueContract  { return result.valueContract }

// ObservedAbsent means the engine kind positively established that the fact
// is absent. It is a successful result and carries no fallback value.
type ObservedAbsent struct{ observationMetadata }

func (ObservedAbsent) observationResult()                   {}
func (result ObservedAbsent) metadata() observationMetadata { return result.observationMetadata }
func (result ObservedAbsent) Fact() Fact                    { return result.fact }
func (ObservedAbsent) Outcome() Outcome                     { return OutcomeObservedAbsent }

// NotObserved means derivation did not establish either a value or absence.
// ResolveObserved refuses every cause.
type NotObserved struct {
	observationMetadata
	cause  NotObservedCause
	detail string
}

func (NotObserved) observationResult()                   {}
func (result NotObserved) metadata() observationMetadata { return result.observationMetadata }
func (result NotObserved) Fact() Fact                    { return result.fact }
func (NotObserved) Outcome() Outcome                     { return OutcomeNotObserved }
func (result NotObserved) Cause() NotObservedCause       { return result.cause }
func (result NotObserved) Detail() string                { return result.detail }

// ObserveValue parses and canonicalizes one value derived by an Engine kind.
// Invalid raw bytes become a typed malformed NotObserved result; they never
// reach a consumer as a plausible string.
func ObserveValue(rule ObservationRule, raw string) ObservationResult {
	metadata := metadataFromRule(rule)
	canonical, err := canonicalizeValue(rule.ValueContract, raw)
	if err != nil {
		return NotObserved{observationMetadata: metadata, cause: NotObservedMalformed, detail: err.Error()}
	}
	return ObservedValue{observationMetadata: metadata, value: canonical}
}

// ObserveAbsent records a positive absence derived by an Engine kind.
func ObserveAbsent(rule ObservationRule) ObservationResult {
	return ObservedAbsent{observationMetadata: metadataFromRule(rule)}
}

// ObserveFailure records why an Engine kind could not derive a fact.
func ObserveFailure(rule ObservationRule, cause NotObservedCause, detail string) ObservationResult {
	return NotObserved{observationMetadata: metadataFromRule(rule), cause: cause, detail: detail}
}

func metadataFromRule(rule ObservationRule) observationMetadata {
	return observationMetadata{fact: rule.Fact, method: rule.Method, valueContract: rule.ValueContract}
}

// Resolution is the fully observed engine contract returned to a consumer.
type Resolution struct {
	Declaration plugin.Declaration
	Contract    Contract
	Results     []ObservationResult
}

var (
	ErrEngineContractMissing  = errors.New("inferenceengine: plugin does not implement Engine")
	ErrContractInvalid        = errors.New("inferenceengine: contract is invalid")
	ErrContractUnstable       = errors.New("inferenceengine: contract is not stable across reads")
	ErrObservationRead        = errors.New("inferenceengine: observation read failed")
	ErrObservationMalformed   = errors.New("inferenceengine: observation is malformed")
	ErrObservationUnsupported = errors.New("inferenceengine: observation is unsupported")
)

// ResolveObserved is the production entry point for an engine consumer. It
// resolves through the real plugin graph, validates the full engine contract,
// and refuses unless the engine kind derives either a canonical value or a
// positive absence for every fact. It has no parameter through which a caller
// can supply evidence or a fallback value.
func ResolveObserved(ctx context.Context, registry *plugin.Registry, id plugin.ID) (Resolution, error) {
	resolved, err := registry.Resolve(id)
	if err != nil {
		return Resolution{}, err
	}
	if resolved.Declaration.Kind != Kind {
		return Resolution{}, fmt.Errorf("%w: %q declares %q", ErrWrongKind, resolved.Declaration.ID, resolved.Declaration.Kind)
	}
	engine, ok := resolved.Plugin.(Engine)
	if !ok {
		return Resolution{}, fmt.Errorf("%w: %q", ErrEngineContractMissing, resolved.Declaration.ID)
	}
	first := engine.EngineContract()
	second := engine.EngineContract()
	if !reflect.DeepEqual(first, second) {
		return Resolution{}, fmt.Errorf("%w: %q answered two different contracts", ErrContractUnstable, resolved.Declaration.ID)
	}
	contract, err := validateContract(first)
	if err != nil {
		return Resolution{}, err
	}

	results := make([]ObservationResult, 0, len(contract.Rules))
	for _, rule := range contract.Rules {
		result := engine.DeriveObservation(ctx, rule)
		switch typed := result.(type) {
		case ObservedValue:
			if typed.metadata() != metadataFromRule(rule) {
				return Resolution{}, fmt.Errorf("%w: %s: engine result does not match the declared rule", ErrObservationMalformed, rule.Fact)
			}
			canonical, canonicalErr := canonicalizeValue(rule.ValueContract, typed.value)
			if canonicalErr != nil || canonical != typed.value {
				return Resolution{}, fmt.Errorf("%w: %s: value violates %s", ErrObservationMalformed, rule.Fact, rule.ValueContract)
			}
		case ObservedAbsent:
			if typed.metadata() != metadataFromRule(rule) {
				return Resolution{}, fmt.Errorf("%w: %s: engine result does not match the declared rule", ErrObservationMalformed, rule.Fact)
			}
			// Positive absence is a measured fact and reaches Resolution.
		case NotObserved:
			if typed.metadata() != metadataFromRule(rule) {
				return Resolution{}, fmt.Errorf("%w: %s: engine result does not match the declared rule", ErrObservationMalformed, rule.Fact)
			}
			switch typed.cause {
			case NotObservedReadFailure:
				return Resolution{}, fmt.Errorf("%w: %s: %s", ErrObservationRead, rule.Fact, typed.detail)
			case NotObservedMalformed:
				return Resolution{}, fmt.Errorf("%w: %s: %s", ErrObservationMalformed, rule.Fact, typed.detail)
			case NotObservedUnsupported:
				return Resolution{}, fmt.Errorf("%w: %s: %s", ErrObservationUnsupported, rule.Fact, typed.detail)
			default:
				return Resolution{}, fmt.Errorf("%w: %s: unknown not-observed cause %q", ErrObservationMalformed, rule.Fact, typed.cause)
			}
		default:
			return Resolution{}, fmt.Errorf("%w: %s: unrecognized result type %T", ErrObservationMalformed, rule.Fact, result)
		}
		results = append(results, result)
	}

	return Resolution{
		Declaration: cloneDeclaration(resolved.Declaration),
		Contract:    cloneContract(contract),
		Results:     append([]ObservationResult(nil), results...),
	}, nil
}

func validateContract(raw Contract) (Contract, error) {
	if raw.Version != ContractVersion {
		return Contract{}, fmt.Errorf("%w: version %q, want %q", ErrContractInvalid, raw.Version, ContractVersion)
	}
	if len(raw.Rules) != len(measuredFacts) {
		return Contract{}, fmt.Errorf("%w: got %d rules, want the complete %d-fact inventory", ErrContractInvalid, len(raw.Rules), len(measuredFacts))
	}

	byFact := make(map[Fact]ObservationRule, len(raw.Rules))
	known := make(map[Fact]struct{}, len(measuredFacts))
	for _, definition := range measuredFacts {
		known[definition.Fact] = struct{}{}
	}
	for _, rule := range raw.Rules {
		if _, ok := known[rule.Fact]; !ok {
			return Contract{}, fmt.Errorf("%w: unknown fact %q", ErrContractInvalid, rule.Fact)
		}
		if _, duplicate := byFact[rule.Fact]; duplicate {
			return Contract{}, fmt.Errorf("%w: duplicate fact %q", ErrContractInvalid, rule.Fact)
		}
		if strings.TrimSpace(rule.Method) == "" || rule.ValueContract == "" {
			return Contract{}, fmt.Errorf("%w: %s needs an observation method and value contract", ErrContractInvalid, rule.Fact)
		}
		wantValueContract := valueContractForFact(rule.Fact)
		if rule.ValueContract != wantValueContract {
			return Contract{}, fmt.Errorf("%w: %s value contract is %q, want %q", ErrContractInvalid, rule.Fact, rule.ValueContract, wantValueContract)
		}
		if rule.OnFailure.ReadFailure != FailureActionRefuse ||
			rule.OnFailure.Malformed != FailureActionRefuse ||
			rule.OnFailure.Unsupported != FailureActionRefuse {
			return Contract{}, fmt.Errorf("%w: %s must refuse read-failed, malformed, and unsupported derivations", ErrContractInvalid, rule.Fact)
		}
		byFact[rule.Fact] = rule
	}

	normalized := cloneContract(raw)
	normalized.Rules = normalized.Rules[:0]
	for _, definition := range measuredFacts {
		rule, found := byFact[definition.Fact]
		if !found {
			return Contract{}, fmt.Errorf("%w: missing fact %q", ErrContractInvalid, definition.Fact)
		}
		normalized.Rules = append(normalized.Rules, rule)
	}

	wantProfile := ModelHarnessExpansion{
		LocalExecutable:    FactLocalExecutable,
		LocalArgv:          FactLocalArgv,
		SSHForwarding:      FactSSHForwarding,
		StressPolicy:       FactStressPolicy,
		RestartSupervision: FactRestartSupervisionPolicy,
		ExecutionOwner:     ExecutionOwner,
	}
	if normalized.ProfileExpansion != wantProfile {
		return Contract{}, fmt.Errorf("%w: profile expansion must bind local executable/argv, SSH forwarding, stress/restart policy, and execution owner %q", ErrContractInvalid, ExecutionOwner)
	}
	return normalized, nil
}

func valueContractForFact(fact Fact) ValueContract {
	switch fact {
	case FactContextArgv, FactPrefillArgv, FactLocalArgv:
		return ValueContractArgvTokens
	case FactReasoningStreamField:
		return ValueContractJSONFieldPath
	case FactLocalExecutable:
		return ValueContractAbsolutePath
	default:
		return ValueContractCanonicalObject
	}
}

var jsonFieldPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`)

func canonicalizeValue(contract ValueContract, raw string) (string, error) {
	switch contract {
	case ValueContractArgvTokens:
		var tokens []string
		if err := decodeSingleJSON(raw, &tokens); err != nil {
			return "", fmt.Errorf("argv tokens: %w", err)
		}
		if len(tokens) == 0 {
			return "", errors.New("argv tokens are empty")
		}
		for index, token := range tokens {
			if token == "" {
				return "", fmt.Errorf("argv token %d is empty", index)
			}
		}
		encoded, _ := json.Marshal(tokens)
		return string(encoded), nil
	case ValueContractJSONFieldPath:
		if !jsonFieldPath.MatchString(raw) {
			return "", errors.New("field path must contain at least two identifier segments")
		}
		return raw, nil
	case ValueContractAbsolutePath:
		if !filepath.IsAbs(raw) || filepath.Clean(raw) != raw {
			return "", errors.New("path must be absolute and clean")
		}
		return raw, nil
	case ValueContractCanonicalObject:
		var value map[string]json.RawMessage
		if err := decodeSingleJSON(raw, &value); err != nil {
			return "", fmt.Errorf("canonical object: %w", err)
		}
		if len(value) == 0 {
			return "", errors.New("canonical object is empty")
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("canonical object: %w", err)
		}
		return string(encoded), nil
	default:
		return "", fmt.Errorf("unknown value contract %q", contract)
	}
}

func decodeSingleJSON(raw string, destination any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func cloneContract(contract Contract) Contract {
	cloned := contract
	cloned.Rules = append([]ObservationRule(nil), contract.Rules...)
	return cloned
}

func cloneDeclaration(declaration plugin.Declaration) plugin.Declaration {
	cloned := declaration
	cloned.Dependencies = append([]plugin.Ref(nil), declaration.Dependencies...)
	return cloned
}
