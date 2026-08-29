package inferenceengine_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type contractEngine struct {
	declaration plugin.Declaration
	contracts   []inferenceengine.Contract
	reads       int
	derive      func(inferenceengine.ObservationRule) inferenceengine.ObservationResult
}

func (e *contractEngine) PluginDeclaration() plugin.Declaration { return e.declaration }

func (e *contractEngine) EngineContract() inferenceengine.Contract {
	index := e.reads
	e.reads++
	if index >= len(e.contracts) {
		index = len(e.contracts) - 1
	}
	return e.contracts[index]
}

func (e *contractEngine) DeriveObservation(_ context.Context, rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
	return e.derive(rule)
}

func valueContract(fact inferenceengine.Fact) inferenceengine.ValueContract {
	switch fact {
	case inferenceengine.FactContextArgv, inferenceengine.FactPrefillArgv, inferenceengine.FactLocalArgv:
		return inferenceengine.ValueContractArgvTokens
	case inferenceengine.FactReasoningStreamField:
		return inferenceengine.ValueContractJSONFieldPath
	case inferenceengine.FactLocalExecutable:
		return inferenceengine.ValueContractAbsolutePath
	default:
		return inferenceengine.ValueContractCanonicalObject
	}
}

func completeContract() inferenceengine.Contract {
	rules := make([]inferenceengine.ObservationRule, 0, len(inferenceengine.MeasuredFacts()))
	for _, definition := range inferenceengine.MeasuredFacts() {
		rules = append(rules, inferenceengine.ObservationRule{
			Fact:          definition.Fact,
			Method:        "engine-derivation/" + string(definition.Fact),
			ValueContract: valueContract(definition.Fact),
			OnFailure: inferenceengine.FailurePolicy{
				ReadFailure: inferenceengine.FailureActionRefuse,
				Malformed:   inferenceengine.FailureActionRefuse,
				Unsupported: inferenceengine.FailureActionRefuse,
			},
		})
	}
	return inferenceengine.Contract{
		Version: inferenceengine.ContractVersion,
		Rules:   rules,
		ProfileExpansion: inferenceengine.ModelHarnessExpansion{
			LocalExecutable:    inferenceengine.FactLocalExecutable,
			LocalArgv:          inferenceengine.FactLocalArgv,
			SSHForwarding:      inferenceengine.FactSSHForwarding,
			StressPolicy:       inferenceengine.FactStressPolicy,
			RestartSupervision: inferenceengine.FactRestartSupervisionPolicy,
			ExecutionOwner:     inferenceengine.ExecutionOwner,
		},
	}
}

func goodRawValue(contract inferenceengine.ValueContract) string {
	switch contract {
	case inferenceengine.ValueContractArgvTokens:
		return `["--ctx-size", "4096"]`
	case inferenceengine.ValueContractJSONFieldPath:
		return "delta.reasoning_content"
	case inferenceengine.ValueContractAbsolutePath:
		path, err := filepath.Abs(filepath.Join("testdata", "engine"))
		if err != nil {
			panic(err)
		}
		return path
	case inferenceengine.ValueContractCanonicalObject:
		return `{"observed": true}`
	default:
		panic("unexpected value contract: " + contract)
	}
}

func goodResult(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
	return inferenceengine.ObserveValue(rule, goodRawValue(rule.ValueContract))
}

func newRegistryWithEngine(t *testing.T, contract inferenceengine.Contract, derive func(inferenceengine.ObservationRule) inferenceengine.ObservationResult) (*plugin.Registry, *contractEngine) {
	t.Helper()
	if derive == nil {
		derive = goodResult
	}
	engine := &contractEngine{
		declaration: plugin.Declaration{ID: "llama-cpp", Kind: inferenceengine.Kind},
		contracts:   []inferenceengine.Contract{contract},
		derive:      derive,
	}
	registry := plugin.NewRegistry()
	if err := registry.Register(engine); err != nil {
		t.Fatalf("Register(engine): %v", err)
	}
	return registry, engine
}

func TestResolveObservedDrivesEngineOwnedDerivationThroughTheProductionEntryPoint(t *testing.T) {
	contract := completeContract()
	var calls []inferenceengine.Fact
	registry, _ := newRegistryWithEngine(t, contract, func(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
		calls = append(calls, rule.Fact)
		return goodResult(rule)
	})

	resolved, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	definitions := inferenceengine.MeasuredFacts()
	if len(resolved.Results) != len(definitions) || len(calls) != len(definitions) {
		t.Fatalf("results/calls = %d/%d, want %d/%d", len(resolved.Results), len(calls), len(definitions), len(definitions))
	}
	for index, definition := range definitions {
		if calls[index] != definition.Fact || resolved.Results[index].Fact() != definition.Fact {
			t.Fatalf("result %d = %q/%q, want %q", index, calls[index], resolved.Results[index].Fact(), definition.Fact)
		}
		if len(definition.Evidence) == 0 {
			t.Fatalf("MeasuredFacts()[%d] has no task evidence", index)
		}
	}
	value, ok := resolved.Results[0].(inferenceengine.ObservedValue)
	if !ok || value.Value() != `["--ctx-size","4096"]` {
		t.Fatalf("first result = %#v, want canonical argv", resolved.Results[0])
	}
}

func TestResolveObservedRepresentsExactlyThreeTypedOutcomes(t *testing.T) {
	want := map[inferenceengine.Outcome]bool{
		inferenceengine.OutcomeObservedValue:  true,
		inferenceengine.OutcomeObservedAbsent: true,
		inferenceengine.OutcomeNotObserved:    true,
	}
	rule := completeContract().Rules[0]
	results := []inferenceengine.ObservationResult{
		inferenceengine.ObserveValue(rule, `["--ctx-size","4096"]`),
		inferenceengine.ObserveAbsent(rule),
		inferenceengine.ObserveFailure(rule, inferenceengine.NotObservedUnsupported, "engine cannot express context argv"),
	}
	for _, result := range results {
		if !want[result.Outcome()] {
			t.Fatalf("unexpected outcome %q", result.Outcome())
		}
		delete(want, result.Outcome())
	}
	if len(want) != 0 {
		t.Fatalf("missing typed outcomes: %v", want)
	}
}

func TestResolveObservedCarriesPositiveAbsenceToResolution(t *testing.T) {
	tests := []struct {
		name string
		fact inferenceengine.Fact
	}{
		{"capability absent", inferenceengine.FactSpeculativeDecoding},
		{"local profile has no ssh forwarding", inferenceengine.FactSSHForwarding},
		{"remote profile has no local executable", inferenceengine.FactLocalExecutable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, _ := newRegistryWithEngine(t, completeContract(), func(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
				if rule.Fact == test.fact {
					return inferenceengine.ObserveAbsent(rule)
				}
				return goodResult(rule)
			})

			resolved, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
			if err != nil {
				t.Fatalf("ResolveObserved observed absence: %v", err)
			}
			for _, result := range resolved.Results {
				if result.Fact() != test.fact {
					continue
				}
				if _, ok := result.(inferenceengine.ObservedAbsent); !ok {
					t.Fatalf("%s result = %T, want ObservedAbsent", test.fact, result)
				}
				return
			}
			t.Fatalf("%s result missing", test.fact)
		})
	}
}

func TestResolveObservedRefusesEveryNotObservedCause(t *testing.T) {
	tests := []struct {
		name  string
		cause inferenceengine.NotObservedCause
		want  error
	}{
		{"read failure", inferenceengine.NotObservedReadFailure, inferenceengine.ErrObservationRead},
		{"malformed", inferenceengine.NotObservedMalformed, inferenceengine.ErrObservationMalformed},
		{"unsupported", inferenceengine.NotObservedUnsupported, inferenceengine.ErrObservationUnsupported},
		{"unknown", "caller-default", inferenceengine.ErrObservationMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, _ := newRegistryWithEngine(t, completeContract(), func(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
				if rule.Fact == inferenceengine.FactContextArgv {
					return inferenceengine.ObserveFailure(rule, test.cause, "derivation failed")
				}
				return goodResult(rule)
			})
			_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
			if !errors.Is(err, test.want) {
				t.Fatalf("ResolveObserved error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResolveObservedDoesNotLaunderReadFailureIntoAbsence(t *testing.T) {
	contract := completeContract()
	readFailure, _ := newRegistryWithEngine(t, contract, func(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
		if rule.Fact == inferenceengine.FactSpeculativeDecoding {
			return inferenceengine.ObserveFailure(rule, inferenceengine.NotObservedReadFailure, "endpoint reset")
		}
		return goodResult(rule)
	})
	_, err := inferenceengine.ResolveObserved(context.Background(), readFailure, "llama-cpp")
	if !errors.Is(err, inferenceengine.ErrObservationRead) {
		t.Fatalf("read failure = %v, want ErrObservationRead", err)
	}

	absent, _ := newRegistryWithEngine(t, contract, func(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
		if rule.Fact == inferenceengine.FactSpeculativeDecoding {
			return inferenceengine.ObserveAbsent(rule)
		}
		return goodResult(rule)
	})
	if _, err := inferenceengine.ResolveObserved(context.Background(), absent, "llama-cpp"); err != nil {
		t.Fatalf("positive absence was refused as a read failure: %v", err)
	}
}

func TestResolveObservedHasNoCallerEvidenceChannelEvenWithAcceptedOriginLabel(t *testing.T) {
	forged := struct {
		Fact   string
		State  string
		Origin string
		Method string
		Value  string
	}{
		Fact:   "context-argv",
		State:  "observed",
		Origin: "observed-process",
		Method: "engine-derivation/context-argv",
		Value:  "caller-supplied --ctx-size 999999",
	}

	entry := reflect.TypeOf(inferenceengine.ResolveObserved)
	if entry.NumIn() != 3 {
		t.Fatalf("ResolveObserved accepts %d inputs; caller evidence channel reopened", entry.NumIn())
	}
	for index := 0; index < entry.NumIn(); index++ {
		if reflect.TypeOf(forged).AssignableTo(entry.In(index)) {
			t.Fatalf("forged caller evidence is assignable to production input %d", index)
		}
	}
	var exactEntry func(context.Context, *plugin.Registry, plugin.ID) (inferenceengine.Resolution, error) = inferenceengine.ResolveObserved
	_ = exactEntry
}

func TestResolveObservedEnforcesValueContractAtTheProductionEntryPoint(t *testing.T) {
	registry, _ := newRegistryWithEngine(t, completeContract(), func(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
		if rule.Fact == inferenceengine.FactContextArgv {
			return inferenceengine.ObserveValue(rule, "caller-supplied --ctx-size 999999")
		}
		return goodResult(rule)
	})
	_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
	if !errors.Is(err, inferenceengine.ErrObservationMalformed) {
		t.Fatalf("ResolveObserved admitted non-argv caller text: %v", err)
	}
}

func TestResolveObservedRefusesAnIncompleteOrFallbackContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*inferenceengine.Contract)
	}{
		{"missing measured fact", func(contract *inferenceengine.Contract) { contract.Rules = contract.Rules[1:] }},
		{"read failure uses caller value", func(contract *inferenceengine.Contract) { contract.Rules[0].OnFailure.ReadFailure = "use-caller-value" }},
		{"malformed uses configured value", func(contract *inferenceengine.Contract) {
			contract.Rules[0].OnFailure.Malformed = "use-configured-value"
		}},
		{"unsupported is silently dropped", func(contract *inferenceengine.Contract) { contract.Rules[0].OnFailure.Unsupported = "" }},
		{"missing observation method", func(contract *inferenceengine.Contract) { contract.Rules[0].Method = "" }},
		{"missing value contract", func(contract *inferenceengine.Contract) { contract.Rules[0].ValueContract = "" }},
		{"wrong value contract", func(contract *inferenceengine.Contract) {
			contract.Rules[0].ValueContract = inferenceengine.ValueContractCanonicalObject
		}},
		{"local executable not bound", func(contract *inferenceengine.Contract) { contract.ProfileExpansion.LocalExecutable = "" }},
		{"local argv not bound", func(contract *inferenceengine.Contract) { contract.ProfileExpansion.LocalArgv = "" }},
		{"ssh forwarding not bound", func(contract *inferenceengine.Contract) { contract.ProfileExpansion.SSHForwarding = "" }},
		{"stress policy not bound", func(contract *inferenceengine.Contract) { contract.ProfileExpansion.StressPolicy = "" }},
		{"restart supervision not bound", func(contract *inferenceengine.Contract) { contract.ProfileExpansion.RestartSupervision = "" }},
		{"execution moved into plugin", func(contract *inferenceengine.Contract) {
			contract.ProfileExpansion.ExecutionOwner = "skill-agents-management"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := completeContract()
			test.mutate(&contract)
			registry, _ := newRegistryWithEngine(t, contract, nil)
			_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
			if !errors.Is(err, inferenceengine.ErrContractInvalid) {
				t.Fatalf("ResolveObserved error = %v, want ErrContractInvalid", err)
			}
		})
	}
}

func TestResolveObservedRefusesMismatchedOrZeroEngineResults(t *testing.T) {
	tests := []struct {
		name   string
		derive func(inferenceengine.ObservationRule) inferenceengine.ObservationResult
	}{
		{"wrong rule", func(rule inferenceengine.ObservationRule) inferenceengine.ObservationResult {
			wrong := rule
			wrong.Fact = inferenceengine.FactHealth
			wrong.ValueContract = inferenceengine.ValueContractCanonicalObject
			return inferenceengine.ObserveValue(wrong, `{"observed":true}`)
		}},
		{"zero observed value", func(inferenceengine.ObservationRule) inferenceengine.ObservationResult {
			return inferenceengine.ObservedValue{}
		}},
		{"typed nil observed value", func(inferenceengine.ObservationRule) inferenceengine.ObservationResult {
			var result *inferenceengine.ObservedValue
			return result
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, _ := newRegistryWithEngine(t, completeContract(), test.derive)
			_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
			if !errors.Is(err, inferenceengine.ErrObservationMalformed) {
				t.Fatalf("ResolveObserved error = %v, want ErrObservationMalformed", err)
			}
		})
	}
}

func TestResolveObservedRefusesUnstableContracts(t *testing.T) {
	first := completeContract()
	second := completeContract()
	second.Rules[0].Method = "a-different-process-read"
	engine := &contractEngine{
		declaration: plugin.Declaration{ID: "llama-cpp", Kind: inferenceengine.Kind},
		contracts:   []inferenceengine.Contract{first, second},
		derive:      goodResult,
	}
	registry := plugin.NewRegistry()
	if err := registry.Register(engine); err != nil {
		t.Fatalf("Register(engine): %v", err)
	}
	_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
	if !errors.Is(err, inferenceengine.ErrContractUnstable) {
		t.Fatalf("ResolveObserved error = %v, want ErrContractUnstable", err)
	}
}

func TestResolveObservedRequiresTheEngineKindAndContract(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(llamaEngine{}); err != nil {
		t.Fatalf("Register(untyped inference engine): %v", err)
	}
	_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
	if !errors.Is(err, inferenceengine.ErrEngineContractMissing) {
		t.Fatalf("ResolveObserved error = %v, want ErrEngineContractMissing", err)
	}

	other := plugin.NewRegistry()
	wrong := &contractEngine{
		declaration: plugin.Declaration{ID: "not-an-engine", Kind: "sidecar"},
		contracts:   []inferenceengine.Contract{completeContract()},
		derive:      goodResult,
	}
	if err := other.Register(wrong); err != nil {
		t.Fatalf("Register(wrong kind): %v", err)
	}
	_, err = inferenceengine.ResolveObserved(context.Background(), other, "not-an-engine")
	if !errors.Is(err, inferenceengine.ErrWrongKind) {
		t.Fatalf("ResolveObserved error = %v, want ErrWrongKind", err)
	}
}

func TestResolveObservedReturnsDefensiveCopiesAndImmutableResults(t *testing.T) {
	contract := completeContract()
	registry, _ := newRegistryWithEngine(t, contract, nil)
	resolved, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp")
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	resolved.Contract.Rules[0].Method = "mutated"
	resolved.Results[0] = inferenceengine.ObserveAbsent(contract.Rules[0])
	definitions := inferenceengine.MeasuredFacts()
	definitions[0].Evidence[0] = "mutated"

	registry2, _ := newRegistryWithEngine(t, contract, nil)
	again, err := inferenceengine.ResolveObserved(context.Background(), registry2, "llama-cpp")
	if err != nil {
		t.Fatalf("ResolveObserved again: %v", err)
	}
	if again.Contract.Rules[0].Method == "mutated" || again.Results[0].Outcome() != inferenceengine.OutcomeObservedValue || reflect.DeepEqual(inferenceengine.MeasuredFacts()[0].Evidence, definitions[0].Evidence) {
		t.Fatal("a caller mutation escaped a defensive copy")
	}
}
