package inferenceengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// ContractVersion is the first declaration-only inference-engine contract.
const ContractVersion = "observed-process/v1"

// ExecutionOwner pins OS process, SSH, polling, and supervision execution to
// agents-infra. This module specifies facts and validates candidate shapes; it
// does not obtain evidence from a running process.
const ExecutionOwner = "agents-infra"

// Fact identifies one consumer-visible engine fact. The set is closed for a
// contract version so a newly required fact cannot be silently omitted.
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

// FactDefinition records why a fact exists. Evidence is specification
// provenance, not runtime evidence.
type FactDefinition struct {
	Fact     Fact
	Meaning  string
	Evidence []string
}

var measuredFacts = []FactDefinition{
	{FactContextArgv, "effective context/KV capacity and exact argv spelling (--max-kv-size for measured MLX Swift; --ctx-size for llama.cpp)", []string{"TASK-260828-2jbufw", "TASK-260828-3fgca3"}},
	{FactPrefillArgv, "prefill chunk and exact argv spelling (--prefill-step-size for MLX; -ub/--ubatch-size for llama.cpp)", []string{"TASK-260828-2jbufw", "TASK-260828-3fgca3"}},
	{FactReasoningStreamField, "the stream field whose first non-empty delta defines reasoning TTFT", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactHealth, "process-alive and endpoint-answering liveness", []string{"TASK-260827-qyebv8", "TASK-260828-2jbufw"}},
	{FactReadiness, "endpoint answering with weights resident; endpoint response alone is not readiness", []string{"TASK-260827-qyebv8", "TASK-260828-2jbufw"}},
	{FactWeightArtifact, "safetensors shards plus config.json, or one GGUF plus optional separate mmproj", []string{"TASK-260828-2jbufw", "TASK-260828-2wcrph"}},
	{FactMemoryAccounting, "mapping-aware accounting; Mach physical footprint alone is invalid for mmap-loaded GGUF", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactSpeculativeDecoding, "runtime-observed capability and active state", []string{"TASK-260828-2wcrph", "TASK-260829-3cwcb6"}},
	{FactLoadState, "the sequenced transition establishing weights became resident", []string{"TASK-260827-qyebv8", "TASK-260829-1qh0ud"}},
	{FactUnloadState, "the sequenced transition establishing weights are no longer resident", []string{"TASK-260829-1qh0ud"}},
	{FactInferenceBusy, "the sequenced busy or idle state of the resident engine", []string{"TASK-260829-1qh0ud"}},
	{FactMemoryPressureSequence, "pressure, load, unload, and busy observations ordered before the relief action", []string{"TASK-260829-1qh0ud"}},
	{FactLocalExecutable, "model-harness local executable selected by profile expansion", []string{"TASK-260830-12n20p"}},
	{FactLocalArgv, "exact local argv tokens selected by profile expansion", []string{"TASK-260828-2jbufw", "TASK-260830-12n20p"}},
	{FactSSHForwarding, "remote profile and forwarding declaration selected instead of local execution", []string{"TASK-260828-2jbufw", "TASK-260830-12n20p"}},
	{FactStressPolicy, "profile stress policy", []string{"TASK-260830-12n20p"}},
	{FactRestartSupervisionPolicy, "profile restart/backoff policy", []string{"TASK-260829-2t5xmi", "TASK-260830-12n20p"}},
}

// MeasuredFacts returns the closed inventory with defensive evidence copies.
func MeasuredFacts() []FactDefinition {
	result := make([]FactDefinition, len(measuredFacts))
	for i, definition := range measuredFacts {
		result[i] = definition
		result[i].Evidence = append([]string(nil), definition.Evidence...)
	}
	return result
}

// FailureAction is the only admissible behavior when derivation fails.
type FailureAction string

const FailureActionRefuse FailureAction = "refuse"

// FailurePolicy independently closes read, shape, and capability failures.
// An engine that cannot express a fact must declare Unsupported=refuse.
type FailurePolicy struct {
	ReadFailure FailureAction
	Malformed   FailureAction
	Unsupported FailureAction
}

// DerivationSource is the agents-infra-owned observation category required for
// a fact. It is a specification label, never proof that a read happened.
type DerivationSource string

const (
	SourceProcessArgv         DerivationSource = "agents-infra/process-argv"
	SourceResponseStream      DerivationSource = "agents-infra/response-stream"
	SourceHealthPoll          DerivationSource = "agents-infra/health-poll"
	SourceResidencyProbe      DerivationSource = "agents-infra/residency-probe"
	SourceArtifactInspection  DerivationSource = "agents-infra/artifact-inspection"
	SourceProcessMappings     DerivationSource = "agents-infra/process-mappings"
	SourceRuntimeCapability   DerivationSource = "agents-infra/runtime-capability"
	SourceRuntimeLifecycle    DerivationSource = "agents-infra/runtime-lifecycle"
	SourceResourceObservation DerivationSource = "agents-infra/resource-observation"
	SourceHarnessExpansion    DerivationSource = "agents-infra/model-harness-expansion"
)

// ValueContract is closed and fact-specific. Generic non-empty JSON is not a
// contract and is intentionally absent.
type ValueContract string

const (
	ValueContractContextArgv          ValueContract = "context-argv/v1"
	ValueContractPrefillArgv          ValueContract = "prefill-argv/v1"
	ValueContractReasoningStreamField ValueContract = "reasoning-stream-field/v1"
	ValueContractHealth               ValueContract = "health/v1"
	ValueContractReadiness            ValueContract = "readiness/v1"
	ValueContractWeightArtifact       ValueContract = "weight-artifact/v1"
	ValueContractMemoryAccounting     ValueContract = "memory-accounting/v1"
	ValueContractSpeculativeDecoding  ValueContract = "speculative-decoding/v1"
	ValueContractLoadState            ValueContract = "load-state/v1"
	ValueContractUnloadState          ValueContract = "unload-state/v1"
	ValueContractInferenceBusy        ValueContract = "inference-busy/v1"
	ValueContractMemoryPressure       ValueContract = "memory-pressure-sequence/v1"
	ValueContractAbsolutePath         ValueContract = "absolute-path/v1"
	ValueContractLocalArgv            ValueContract = "local-argv/v1"
	ValueContractSSHForwarding        ValueContract = "ssh-forwarding/v1"
	ValueContractStressPolicy         ValueContract = "stress-policy/v1"
	ValueContractRestartSupervision   ValueContract = "restart-supervision/v1"
)

// ObservationRule is one immutable row of the versioned specification.
type ObservationRule struct {
	Fact          Fact
	Source        DerivationSource
	ValueContract ValueContract
	OnFailure     FailurePolicy
}

// ModelHarnessExpansion binds local and SSH profile shapes while retaining
// process and supervision ownership in agents-infra.
type ModelHarnessExpansion struct {
	LocalExecutable    Fact
	LocalArgv          Fact
	SSHForwarding      Fact
	StressPolicy       Fact
	RestartSupervision Fact
	ExecutionOwner     string
}

// Contract is declarative. It contains no observer, evidence, fallback, or
// caller-supplied value channel.
type Contract struct {
	Version          string
	Rules            []ObservationRule
	ProfileExpansion ModelHarnessExpansion
}

// Engine contributes only graph identity and the declarative contract. A
// caller-registered Engine is deliberately unable to mint observations.
type Engine interface {
	plugin.Plugin
	EngineContract() Contract
}

// ResolvedContract is declaration data, not observed process state.
type ResolvedContract struct {
	Declaration plugin.Declaration
	Contract    Contract
}

var (
	ErrEngineContractMissing = errors.New("inferenceengine: plugin does not implement Engine")
	ErrContractInvalid       = errors.New("inferenceengine: contract is invalid")
	ErrContractUnstable      = errors.New("inferenceengine: contract is not stable across reads")
	ErrObservationMalformed  = errors.New("inferenceengine: candidate value is malformed")
)

// RequiredContract returns the one admissible v1 contract. Engine plugins may
// return this declaration but may not alter derivation or failure policy.
func RequiredContract() Contract {
	rules := make([]ObservationRule, 0, len(measuredFacts))
	for _, definition := range measuredFacts {
		rules = append(rules, requiredRule(definition.Fact))
	}
	return Contract{
		Version: ContractVersion,
		Rules:   rules,
		ProfileExpansion: ModelHarnessExpansion{
			LocalExecutable:    FactLocalExecutable,
			LocalArgv:          FactLocalArgv,
			SSHForwarding:      FactSSHForwarding,
			StressPolicy:       FactStressPolicy,
			RestartSupervision: FactRestartSupervisionPolicy,
			ExecutionOwner:     ExecutionOwner,
		},
	}
}

// ResolveContract validates declaration data through the real plugin graph.
// It is not a production observation gate: agents-infra must derive facts from
// its owned process path and may use ValidateCandidateValue only for shape.
func ResolveContract(registry *plugin.Registry, id plugin.ID) (ResolvedContract, error) {
	resolved, err := registry.Resolve(id)
	if err != nil {
		return ResolvedContract{}, err
	}
	if resolved.Declaration.Kind != Kind {
		return ResolvedContract{}, fmt.Errorf("%w: %q declares %q", ErrWrongKind, resolved.Declaration.ID, resolved.Declaration.Kind)
	}
	engine, ok := resolved.Plugin.(Engine)
	if !ok {
		return ResolvedContract{}, fmt.Errorf("%w: %q", ErrEngineContractMissing, resolved.Declaration.ID)
	}
	first := engine.EngineContract()
	second := engine.EngineContract()
	if !reflect.DeepEqual(first, second) {
		return ResolvedContract{}, fmt.Errorf("%w: %q answered two different contracts", ErrContractUnstable, resolved.Declaration.ID)
	}
	contract, err := validateContract(first)
	if err != nil {
		return ResolvedContract{}, err
	}
	return ResolvedContract{Declaration: cloneDeclaration(resolved.Declaration), Contract: contract}, nil
}

func validateContract(raw Contract) (Contract, error) {
	required := RequiredContract()
	if raw.Version != required.Version {
		return Contract{}, fmt.Errorf("%w: version %q, want %q", ErrContractInvalid, raw.Version, required.Version)
	}
	if len(raw.Rules) != len(required.Rules) {
		return Contract{}, fmt.Errorf("%w: got %d rules, want %d", ErrContractInvalid, len(raw.Rules), len(required.Rules))
	}
	byFact := make(map[Fact]ObservationRule, len(raw.Rules))
	for _, rule := range raw.Rules {
		if _, duplicate := byFact[rule.Fact]; duplicate {
			return Contract{}, fmt.Errorf("%w: duplicate fact %q", ErrContractInvalid, rule.Fact)
		}
		byFact[rule.Fact] = rule
	}
	normalized := cloneContract(raw)
	normalized.Rules = normalized.Rules[:0]
	for _, want := range required.Rules {
		got, found := byFact[want.Fact]
		if !found {
			return Contract{}, fmt.Errorf("%w: missing fact %q", ErrContractInvalid, want.Fact)
		}
		if got != want {
			return Contract{}, fmt.Errorf("%w: %s must use source %q, value contract %q, and refuse read-failed, malformed, and unsupported derivations", ErrContractInvalid, want.Fact, want.Source, want.ValueContract)
		}
		normalized.Rules = append(normalized.Rules, got)
	}
	if normalized.ProfileExpansion != required.ProfileExpansion {
		return Contract{}, fmt.Errorf("%w: model-harness expansion must bind local executable/argv, SSH forwarding, stress/restart policy, and execution owner %q", ErrContractInvalid, ExecutionOwner)
	}
	return normalized, nil
}

func requiredRule(fact Fact) ObservationRule {
	return ObservationRule{
		Fact:          fact,
		Source:        sourceForFact(fact),
		ValueContract: ValueContractForFact(fact),
		OnFailure: FailurePolicy{
			ReadFailure: FailureActionRefuse,
			Malformed:   FailureActionRefuse,
			Unsupported: FailureActionRefuse,
		},
	}
}

func sourceForFact(fact Fact) DerivationSource {
	switch fact {
	case FactContextArgv, FactPrefillArgv:
		return SourceProcessArgv
	case FactReasoningStreamField:
		return SourceResponseStream
	case FactHealth:
		return SourceHealthPoll
	case FactReadiness:
		return SourceResidencyProbe
	case FactWeightArtifact:
		return SourceArtifactInspection
	case FactMemoryAccounting:
		return SourceProcessMappings
	case FactSpeculativeDecoding:
		return SourceRuntimeCapability
	case FactLoadState, FactUnloadState, FactInferenceBusy:
		return SourceRuntimeLifecycle
	case FactMemoryPressureSequence:
		return SourceResourceObservation
	case FactLocalExecutable, FactLocalArgv, FactSSHForwarding, FactStressPolicy, FactRestartSupervisionPolicy:
		return SourceHarnessExpansion
	default:
		return ""
	}
}

// ValueContractForFact returns the closed schema selected by one fact.
func ValueContractForFact(fact Fact) ValueContract {
	switch fact {
	case FactContextArgv:
		return ValueContractContextArgv
	case FactPrefillArgv:
		return ValueContractPrefillArgv
	case FactReasoningStreamField:
		return ValueContractReasoningStreamField
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
		return ValueContractMemoryPressure
	case FactLocalExecutable:
		return ValueContractAbsolutePath
	case FactLocalArgv:
		return ValueContractLocalArgv
	case FactSSHForwarding:
		return ValueContractSSHForwarding
	case FactStressPolicy:
		return ValueContractStressPolicy
	case FactRestartSupervisionPolicy:
		return ValueContractRestartSupervision
	default:
		return ""
	}
}

// ValidateCandidateValue validates and canonicalizes shape only. It does not
// establish provenance, perform a process read, or turn caller input into an
// observed fact. Only agents-infra's owned composition may decide admission.
func ValidateCandidateValue(fact Fact, raw string) (string, error) {
	canonical, err := validateCandidateValue(fact, raw)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrObservationMalformed, fact, err)
	}
	return canonical, nil
}

func validateCandidateValue(fact Fact, raw string) (string, error) {
	switch fact {
	case FactContextArgv:
		return validateKnobArgv(raw, "--max-kv-size", "--ctx-size")
	case FactPrefillArgv:
		return validateKnobArgv(raw, "--prefill-step-size", "-ub", "--ubatch-size")
	case FactLocalArgv:
		return validateArgv(raw)
	case FactReasoningStreamField:
		if raw != "delta.reasoning" && raw != "delta.reasoning_content" {
			return "", errors.New("field must be delta.reasoning or delta.reasoning_content")
		}
		return raw, nil
	case FactLocalExecutable:
		if !filepath.IsAbs(raw) || filepath.Clean(raw) != raw {
			return "", errors.New("path must be absolute and clean")
		}
		return raw, nil
	case FactHealth:
		var value healthValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if !value.ProcessAlive || !value.EndpointAnswering {
			return "", errors.New("healthy requires a live process and answering endpoint")
		}
		return encode(value)
	case FactReadiness:
		var value readinessValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if !value.EndpointAnswering || !value.WeightsResident {
			return "", errors.New("readiness requires both endpoint answering and weights resident")
		}
		return encode(value)
	case FactWeightArtifact:
		var value weightArtifactValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if err := validateWeightArtifact(value); err != nil {
			return "", err
		}
		return encode(value)
	case FactMemoryAccounting:
		var value memoryAccountingValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if value.Bytes <= 0 {
			return "", errors.New("bytes must be positive")
		}
		switch value.ArtifactMapping {
		case "anonymous":
			if value.Method != "mach-physical-footprint" {
				return "", errors.New("anonymous mapping requires mach-physical-footprint")
			}
		case "memory-mapped":
			if value.Method != "process-footprint-plus-mapped-resident-pages" {
				return "", errors.New("memory-mapped weights require mapped-resident-page accounting")
			}
		default:
			return "", errors.New("artifact_mapping must be anonymous or memory-mapped")
		}
		return encode(value)
	case FactSpeculativeDecoding:
		var value speculativeDecodingValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if value.Capability != "supported" && value.Capability != "absent" {
			return "", errors.New("capability must be supported or absent")
		}
		if value.Active && value.Capability != "supported" {
			return "", errors.New("active speculative decoding requires supported capability")
		}
		return encode(value)
	case FactLoadState:
		return validateTransition(raw, "resident", "loaded", "load")
	case FactUnloadState:
		return validateTransition(raw, "not-resident", "unloaded", "unload")
	case FactInferenceBusy:
		var value busyValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if (value.State != "busy" && value.State != "idle") || value.Sequence <= 0 {
			return "", errors.New("inference state must be busy or idle with a positive sequence")
		}
		return encode(value)
	case FactMemoryPressureSequence:
		var value pressureSequenceValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if err := validatePressureSequence(value); err != nil {
			return "", err
		}
		return encode(value)
	case FactSSHForwarding:
		var value sshForwardingValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if value.Mode != "ssh" || strings.TrimSpace(value.Profile) == "" || value.LocalHost != "127.0.0.1" || value.LocalPort <= 0 || value.RemoteHost != "127.0.0.1" || value.RemotePort <= 0 {
			return "", errors.New("ssh forwarding requires a named profile and positive loopback ports")
		}
		return encode(value)
	case FactStressPolicy:
		var value stressPolicyValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if value.Mode != "synthetic-prefill" || value.PromptTokens <= 0 || value.OutputTokens <= 0 || value.MemorySample != "process-and-mappings" {
			return "", errors.New("stress policy requires bounded synthetic-prefill and process-and-mappings sampling")
		}
		return encode(value)
	case FactRestartSupervisionPolicy:
		var value restartPolicyValue
		if err := decodeStrict(raw, &value); err != nil {
			return "", err
		}
		if value.Mode != "bounded-backoff" || value.MaxRestarts < 0 || len(value.BackoffSeconds) == 0 {
			return "", errors.New("restart policy requires bounded-backoff, max_restarts, and backoff_seconds")
		}
		previous := 0
		for _, seconds := range value.BackoffSeconds {
			if seconds <= previous {
				return "", errors.New("backoff_seconds must be positive and strictly increasing")
			}
			previous = seconds
		}
		return encode(value)
	default:
		return "", errors.New("unknown fact")
	}
}

type healthValue struct {
	ProcessAlive      bool `json:"process_alive"`
	EndpointAnswering bool `json:"endpoint_answering"`
}

type readinessValue struct {
	EndpointAnswering bool `json:"endpoint_answering"`
	WeightsResident   bool `json:"weights_resident"`
}

type weightArtifactValue struct {
	Shape       string   `json:"shape"`
	WeightFiles []string `json:"weight_files"`
	ConfigPath  string   `json:"config_path,omitempty"`
	MMProjPath  string   `json:"mmproj_path,omitempty"`
}

type memoryAccountingValue struct {
	ArtifactMapping string `json:"artifact_mapping"`
	Method          string `json:"method"`
	Bytes           int64  `json:"bytes"`
}

type speculativeDecodingValue struct {
	Capability string `json:"capability"`
	Active     bool   `json:"active"`
}

type transitionValue struct {
	State      string `json:"state"`
	Transition string `json:"transition"`
	Sequence   int64  `json:"sequence"`
}

type busyValue struct {
	State    string `json:"state"`
	Sequence int64  `json:"sequence"`
}

type pressureSequenceValue struct {
	PressureState string   `json:"pressure_state"`
	LoadState     string   `json:"load_state"`
	UnloadState   string   `json:"unload_state"`
	InferenceBusy string   `json:"inference_busy"`
	Order         []string `json:"order"`
	Action        string   `json:"action"`
}

type sshForwardingValue struct {
	Mode       string `json:"mode"`
	Profile    string `json:"profile"`
	LocalHost  string `json:"local_host"`
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
}

type stressPolicyValue struct {
	Mode         string `json:"mode"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
	MemorySample string `json:"memory_sample"`
}

type restartPolicyValue struct {
	Mode           string `json:"mode"`
	MaxRestarts    int    `json:"max_restarts"`
	BackoffSeconds []int  `json:"backoff_seconds"`
}

func validateKnobArgv(raw string, flags ...string) (string, error) {
	canonical, tokens, err := decodeArgv(raw)
	if err != nil {
		return "", err
	}
	if len(tokens) != 2 {
		return "", errors.New("knob argv must contain exactly flag and value")
	}
	allowed := false
	for _, flag := range flags {
		allowed = allowed || tokens[0] == flag
	}
	if !allowed {
		return "", fmt.Errorf("unexpected knob spelling %q", tokens[0])
	}
	value, err := strconv.Atoi(tokens[1])
	if err != nil || value <= 0 {
		return "", errors.New("knob value must be a positive integer")
	}
	return canonical, nil
}

func validateArgv(raw string) (string, error) {
	canonical, _, err := decodeArgv(raw)
	return canonical, err
}

func decodeArgv(raw string) (string, []string, error) {
	var tokens []string
	if err := decodeStrict(raw, &tokens); err != nil {
		return "", nil, err
	}
	if len(tokens) == 0 {
		return "", nil, errors.New("argv tokens are empty")
	}
	for index, token := range tokens {
		if token == "" {
			return "", nil, fmt.Errorf("argv token %d is empty", index)
		}
	}
	canonical, _ := encode(tokens)
	return canonical, tokens, nil
}

func validateTransition(raw, state, transition, label string) (string, error) {
	var value transitionValue
	if err := decodeStrict(raw, &value); err != nil {
		return "", err
	}
	if value.State != state || value.Transition != transition || value.Sequence <= 0 {
		return "", fmt.Errorf("%s state requires %s/%s with a positive sequence", label, state, transition)
	}
	return encode(value)
}

func validateWeightArtifact(value weightArtifactValue) error {
	if len(value.WeightFiles) == 0 {
		return errors.New("weight_files must not be empty")
	}
	for _, path := range value.WeightFiles {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("weight paths must be absolute and clean")
		}
	}
	switch value.Shape {
	case "safetensors":
		if !cleanAbsolute(value.ConfigPath) || filepath.Ext(value.ConfigPath) != ".json" || value.MMProjPath != "" {
			return errors.New("safetensors requires config_path and forbids mmproj_path")
		}
		for _, path := range value.WeightFiles {
			if filepath.Ext(path) != ".safetensors" {
				return errors.New("safetensors shape requires .safetensors weight files")
			}
		}
	case "gguf":
		if len(value.WeightFiles) != 1 || filepath.Ext(value.WeightFiles[0]) != ".gguf" || value.ConfigPath != "" {
			return errors.New("gguf requires exactly one .gguf weight file and no config_path")
		}
		if value.MMProjPath != "" && (!cleanAbsolute(value.MMProjPath) || filepath.Ext(value.MMProjPath) != ".gguf") {
			return errors.New("mmproj_path must name a .gguf artifact")
		}
	default:
		return errors.New("shape must be safetensors or gguf")
	}
	return nil
}

func validatePressureSequence(value pressureSequenceValue) error {
	wantOrder := []string{"pressure", "load-state", "unload-state", "inference-busy", "relief-action"}
	if !reflect.DeepEqual(value.Order, wantOrder) {
		return errors.New("order must consult pressure, load, unload, and inference-busy before relief-action")
	}
	if value.LoadState == "resident" && value.UnloadState != "loaded" || value.LoadState == "not-resident" && value.UnloadState != "unloaded" {
		return errors.New("load_state and unload_state conflict")
	}
	if value.LoadState != "resident" && value.LoadState != "not-resident" && value.LoadState != "unknown" {
		return errors.New("invalid load_state")
	}
	if value.UnloadState != "loaded" && value.UnloadState != "unloaded" && value.UnloadState != "unknown" {
		return errors.New("invalid unload_state")
	}
	if value.InferenceBusy != "busy" && value.InferenceBusy != "idle" && value.InferenceBusy != "unknown" {
		return errors.New("invalid inference_busy")
	}
	switch value.PressureState {
	case "healthy":
		if value.Action != "observe" {
			return errors.New("healthy pressure state requires observe")
		}
	case "pressured":
		if value.InferenceBusy == "busy" && value.Action != "refuse-new" {
			return errors.New("pressured busy engine must refuse-new without unload")
		}
		if value.InferenceBusy == "idle" && value.LoadState == "resident" && value.Action != "drain-idle" {
			return errors.New("pressured idle resident engine must drain-idle")
		}
		if value.InferenceBusy == "idle" && value.LoadState == "not-resident" && value.Action != "observe" {
			return errors.New("pressured idle unloaded engine requires observe")
		}
		if (value.InferenceBusy == "unknown" || value.LoadState == "unknown" || value.UnloadState == "unknown") && value.Action != "refuse-new" {
			return errors.New("unknown sequencing input must refuse-new")
		}
	case "unknown":
		if value.Action != "refuse-new" {
			return errors.New("unknown pressure state must refuse-new")
		}
	default:
		return errors.New("invalid pressure_state")
	}
	return nil
}

func cleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func decodeStrict(raw string, destination any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
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

func encode(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
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
