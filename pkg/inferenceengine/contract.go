package inferenceengine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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
	Absent      FailureAction
	Malformed   FailureAction
	Unsupported FailureAction
}

// ObservationRule tells an agents-infra observer exactly what to read and how
// to parse a value. FailurePolicy is mandatory even when an engine cannot
// express the fact: Unsupported=refuse is a supported declaration.
type ObservationRule struct {
	Fact          Fact
	Method        string
	ValueContract string
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

// Engine is a graph plugin that declares the complete observed-process
// contract for its engine kind.
type Engine interface {
	plugin.Plugin
	EngineContract() Contract
}

// ObservationState is the observer's result. Unknown values are malformed,
// never future-compatible success.
type ObservationState string

const (
	ObservationObserved    ObservationState = "observed"
	ObservationAbsent      ObservationState = "absent"
	ObservationMalformed   ObservationState = "malformed"
	ObservationUnsupported ObservationState = "unsupported"
)

// ObservationOrigin distinguishes process evidence from caller-supplied or
// inferred values.
type ObservationOrigin string

const ObservationOriginProcess ObservationOrigin = "observed-process"

// Observation is one result produced by the consumer-owned process observer.
// Method must equal the engine's declared method. Value is engine-specific but
// must satisfy the declared ValueContract before the observer returns it as
// Observed.
type Observation struct {
	Fact   Fact
	State  ObservationState
	Origin ObservationOrigin
	Method string
	Value  string
	Detail string
}

// Observer is implemented by agents-infra. It may inspect processes,
// endpoints, argv and artifacts, but this package never starts, stops, signals,
// forwards to, or supervises an OS process.
type Observer interface {
	Observe(context.Context, plugin.Declaration, ObservationRule) (Observation, error)
}

// Resolution is the fully observed engine contract returned to a consumer.
type Resolution struct {
	Declaration  plugin.Declaration
	Contract     Contract
	Observations []Observation
}

var (
	ErrEngineContractMissing  = errors.New("inferenceengine: plugin does not implement Engine")
	ErrContractInvalid        = errors.New("inferenceengine: contract is invalid")
	ErrContractUnstable       = errors.New("inferenceengine: contract is not stable across reads")
	ErrObservationRead        = errors.New("inferenceengine: observation read failed")
	ErrObservationAbsent      = errors.New("inferenceengine: required observation is absent")
	ErrObservationMalformed   = errors.New("inferenceengine: observation is malformed")
	ErrObservationUnsupported = errors.New("inferenceengine: observation is unsupported")
)

// ResolveObserved is the production entry point for an engine consumer. It
// resolves through the real plugin graph, validates the full engine contract,
// and refuses unless every required fact is observed from the process. It has
// no parameter through which a caller could supply a fallback value.
func ResolveObserved(ctx context.Context, registry *plugin.Registry, id plugin.ID, observer Observer) (Resolution, error) {
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
	if nilObserver(observer) {
		return Resolution{}, fmt.Errorf("%w: observer is nil", ErrObservationRead)
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

	observations := make([]Observation, 0, len(contract.Rules))
	for _, rule := range contract.Rules {
		observation, readErr := observer.Observe(ctx, resolved.Declaration, rule)
		if readErr != nil {
			return Resolution{}, fmt.Errorf("%w: %s: %v", ErrObservationRead, rule.Fact, readErr)
		}
		if observation.Fact != rule.Fact || observation.Method != rule.Method || observation.Origin != ObservationOriginProcess {
			return Resolution{}, fmt.Errorf("%w: %s: fact, method, and process origin must match the declared rule", ErrObservationMalformed, rule.Fact)
		}
		switch observation.State {
		case ObservationObserved:
			if strings.TrimSpace(observation.Value) == "" {
				return Resolution{}, fmt.Errorf("%w: %s: observed value is empty", ErrObservationMalformed, rule.Fact)
			}
		case ObservationAbsent:
			return Resolution{}, fmt.Errorf("%w: %s", ErrObservationAbsent, rule.Fact)
		case ObservationMalformed:
			return Resolution{}, fmt.Errorf("%w: %s", ErrObservationMalformed, rule.Fact)
		case ObservationUnsupported:
			return Resolution{}, fmt.Errorf("%w: %s", ErrObservationUnsupported, rule.Fact)
		default:
			return Resolution{}, fmt.Errorf("%w: %s: unknown state %q", ErrObservationMalformed, rule.Fact, observation.State)
		}
		observations = append(observations, observation)
	}

	return Resolution{
		Declaration:  cloneDeclaration(resolved.Declaration),
		Contract:     cloneContract(contract),
		Observations: append([]Observation(nil), observations...),
	}, nil
}

func nilObserver(observer Observer) bool {
	if observer == nil {
		return true
	}
	value := reflect.ValueOf(observer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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
		if strings.TrimSpace(rule.Method) == "" || strings.TrimSpace(rule.ValueContract) == "" {
			return Contract{}, fmt.Errorf("%w: %s needs an observation method and value contract", ErrContractInvalid, rule.Fact)
		}
		if rule.OnFailure.Absent != FailureActionRefuse ||
			rule.OnFailure.Malformed != FailureActionRefuse ||
			rule.OnFailure.Unsupported != FailureActionRefuse {
			return Contract{}, fmt.Errorf("%w: %s must refuse absent, malformed, and unsupported observations", ErrContractInvalid, rule.Fact)
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
