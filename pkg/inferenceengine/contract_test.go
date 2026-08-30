package inferenceengine_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type contractEngine struct {
	declaration plugin.Declaration
	contracts   []inferenceengine.Contract
	reads       int
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

type observingFunc func(context.Context, plugin.Declaration, inferenceengine.ObservationRule) (inferenceengine.Observation, error)

func (f observingFunc) Observe(ctx context.Context, declaration plugin.Declaration, rule inferenceengine.ObservationRule) (inferenceengine.Observation, error) {
	return f(ctx, declaration, rule)
}

type nilObserverDouble struct{}

func (*nilObserverDouble) Observe(context.Context, plugin.Declaration, inferenceengine.ObservationRule) (inferenceengine.Observation, error) {
	panic("typed-nil observer must be refused before dispatch")
}

func completeContract() inferenceengine.Contract {
	rules := make([]inferenceengine.ObservationRule, 0, len(inferenceengine.MeasuredFacts()))
	for _, definition := range inferenceengine.MeasuredFacts() {
		rules = append(rules, inferenceengine.ObservationRule{
			Fact:          definition.Fact,
			Method:        "process-observer/" + string(definition.Fact),
			ValueContract: "non-empty canonical value for " + string(definition.Fact),
			OnFailure: inferenceengine.FailurePolicy{
				Absent:      inferenceengine.FailureActionRefuse,
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

func newRegistryWithEngine(t *testing.T, contract inferenceengine.Contract) (*plugin.Registry, *contractEngine) {
	t.Helper()
	engine := &contractEngine{
		declaration: plugin.Declaration{ID: "llama-cpp", Kind: inferenceengine.Kind},
		contracts:   []inferenceengine.Contract{contract},
	}
	registry := plugin.NewRegistry()
	if err := registry.Register(engine); err != nil {
		t.Fatalf("Register(engine): %v", err)
	}
	return registry, engine
}

func goodObserver(calls *[]inferenceengine.Fact) observingFunc {
	return func(_ context.Context, _ plugin.Declaration, rule inferenceengine.ObservationRule) (inferenceengine.Observation, error) {
		if calls != nil {
			*calls = append(*calls, rule.Fact)
		}
		return inferenceengine.Observation{
			Fact:   rule.Fact,
			State:  inferenceengine.ObservationObserved,
			Origin: inferenceengine.ObservationOriginProcess,
			Method: rule.Method,
			Value:  "observed:" + string(rule.Fact),
		}, nil
	}
}

func TestResolveObservedDrivesTheCompleteContractThroughTheProductionEntryPoint(t *testing.T) {
	registry, _ := newRegistryWithEngine(t, completeContract())
	var calls []inferenceengine.Fact

	resolved, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", goodObserver(&calls))
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	definitions := inferenceengine.MeasuredFacts()
	if len(resolved.Observations) != len(definitions) || len(calls) != len(definitions) {
		t.Fatalf("observations/calls = %d/%d, want %d/%d", len(resolved.Observations), len(calls), len(definitions), len(definitions))
	}
	for i, definition := range definitions {
		if calls[i] != definition.Fact || resolved.Observations[i].Fact != definition.Fact {
			t.Fatalf("observation %d = %q/%q, want %q", i, calls[i], resolved.Observations[i].Fact, definition.Fact)
		}
		if len(definition.Evidence) == 0 {
			t.Fatalf("MeasuredFacts()[%d] has no task evidence", i)
		}
	}
}

func TestResolveObservedRefusesEveryNonObservedOutcome(t *testing.T) {
	tests := []struct {
		name  string
		state inferenceengine.ObservationState
		want  error
	}{
		{"absent", inferenceengine.ObservationAbsent, inferenceengine.ErrObservationAbsent},
		{"malformed", inferenceengine.ObservationMalformed, inferenceengine.ErrObservationMalformed},
		{"unsupported", inferenceengine.ObservationUnsupported, inferenceengine.ErrObservationUnsupported},
		{"unknown", "caller-default", inferenceengine.ErrObservationMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, _ := newRegistryWithEngine(t, completeContract())
			observer := observingFunc(func(_ context.Context, _ plugin.Declaration, rule inferenceengine.ObservationRule) (inferenceengine.Observation, error) {
				observation, _ := goodObserver(nil)(context.Background(), plugin.Declaration{}, rule)
				if rule.Fact == inferenceengine.FactContextArgv {
					observation.State = test.state
					observation.Value = ""
				}
				return observation, nil
			})
			_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", observer)
			if !errors.Is(err, test.want) {
				t.Fatalf("ResolveObserved error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResolveObservedKeepsReadFailureDistinctFromAbsence(t *testing.T) {
	registry, _ := newRegistryWithEngine(t, completeContract())
	observer := observingFunc(func(_ context.Context, _ plugin.Declaration, rule inferenceengine.ObservationRule) (inferenceengine.Observation, error) {
		return inferenceengine.Observation{}, fmt.Errorf("endpoint reset while reading %s", rule.Fact)
	})
	_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", observer)
	if !errors.Is(err, inferenceengine.ErrObservationRead) {
		t.Fatalf("ResolveObserved error = %v, want ErrObservationRead", err)
	}
	if errors.Is(err, inferenceengine.ErrObservationAbsent) {
		t.Fatalf("read failure was laundered into absence: %v", err)
	}
}

func TestResolveObservedRefusesTypedNilObserver(t *testing.T) {
	registry, _ := newRegistryWithEngine(t, completeContract())
	var observer *nilObserverDouble
	_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", observer)
	if !errors.Is(err, inferenceengine.ErrObservationRead) {
		t.Fatalf("ResolveObserved error = %v, want ErrObservationRead", err)
	}
}

func TestResolveObservedRefusesAnIncompleteOrFallbackContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*inferenceengine.Contract)
	}{
		{"missing measured fact", func(contract *inferenceengine.Contract) { contract.Rules = contract.Rules[1:] }},
		{"absent uses caller value", func(contract *inferenceengine.Contract) { contract.Rules[0].OnFailure.Absent = "use-caller-value" }},
		{"malformed uses configured value", func(contract *inferenceengine.Contract) {
			contract.Rules[0].OnFailure.Malformed = "use-configured-value"
		}},
		{"unsupported is silently dropped", func(contract *inferenceengine.Contract) { contract.Rules[0].OnFailure.Unsupported = "" }},
		{"missing observation method", func(contract *inferenceengine.Contract) { contract.Rules[0].Method = "" }},
		{"missing value grammar", func(contract *inferenceengine.Contract) { contract.Rules[0].ValueContract = "" }},
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
			registry, _ := newRegistryWithEngine(t, contract)
			_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", goodObserver(nil))
			if !errors.Is(err, inferenceengine.ErrContractInvalid) {
				t.Fatalf("ResolveObserved error = %v, want ErrContractInvalid", err)
			}
		})
	}
}

func TestResolveObservedRefusesForgedObservationShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*inferenceengine.Observation)
	}{
		{"caller origin", func(observation *inferenceengine.Observation) { observation.Origin = "caller" }},
		{"wrong method", func(observation *inferenceengine.Observation) { observation.Method = "configured-default" }},
		{"wrong fact", func(observation *inferenceengine.Observation) { observation.Fact = inferenceengine.FactHealth }},
		{"empty value", func(observation *inferenceengine.Observation) { observation.Value = " " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, _ := newRegistryWithEngine(t, completeContract())
			observer := observingFunc(func(_ context.Context, _ plugin.Declaration, rule inferenceengine.ObservationRule) (inferenceengine.Observation, error) {
				observation, _ := goodObserver(nil)(context.Background(), plugin.Declaration{}, rule)
				if rule.Fact == inferenceengine.FactContextArgv {
					test.mutate(&observation)
				}
				return observation, nil
			})
			_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", observer)
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
	}
	registry := plugin.NewRegistry()
	if err := registry.Register(engine); err != nil {
		t.Fatalf("Register(engine): %v", err)
	}
	_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", goodObserver(nil))
	if !errors.Is(err, inferenceengine.ErrContractUnstable) {
		t.Fatalf("ResolveObserved error = %v, want ErrContractUnstable", err)
	}
}

func TestResolveObservedRequiresTheEngineKindAndContract(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(llamaEngine{}); err != nil {
		t.Fatalf("Register(untyped inference engine): %v", err)
	}
	_, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", goodObserver(nil))
	if !errors.Is(err, inferenceengine.ErrEngineContractMissing) {
		t.Fatalf("ResolveObserved error = %v, want ErrEngineContractMissing", err)
	}

	other := plugin.NewRegistry()
	wrong := &contractEngine{
		declaration: plugin.Declaration{ID: "not-an-engine", Kind: "sidecar"},
		contracts:   []inferenceengine.Contract{completeContract()},
	}
	if err := other.Register(wrong); err != nil {
		t.Fatalf("Register(wrong kind): %v", err)
	}
	_, err = inferenceengine.ResolveObserved(context.Background(), other, "not-an-engine", goodObserver(nil))
	if !errors.Is(err, inferenceengine.ErrWrongKind) {
		t.Fatalf("ResolveObserved error = %v, want ErrWrongKind", err)
	}
}

func TestResolveObservedReturnsDefensiveCopies(t *testing.T) {
	contract := completeContract()
	registry, _ := newRegistryWithEngine(t, contract)
	resolved, err := inferenceengine.ResolveObserved(context.Background(), registry, "llama-cpp", goodObserver(nil))
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	resolved.Contract.Rules[0].Method = "mutated"
	definitions := inferenceengine.MeasuredFacts()
	definitions[0].Evidence[0] = "mutated"

	registry2, _ := newRegistryWithEngine(t, contract)
	again, err := inferenceengine.ResolveObserved(context.Background(), registry2, "llama-cpp", goodObserver(nil))
	if err != nil {
		t.Fatalf("ResolveObserved again: %v", err)
	}
	if again.Contract.Rules[0].Method == "mutated" || reflect.DeepEqual(inferenceengine.MeasuredFacts()[0].Evidence, definitions[0].Evidence) {
		t.Fatal("a caller mutation escaped a defensive copy")
	}
}
