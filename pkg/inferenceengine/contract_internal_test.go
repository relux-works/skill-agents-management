package inferenceengine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type forgedResultEngine struct {
	contract Contract
}

func (*forgedResultEngine) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: "forged-engine", Kind: Kind}
}

func (engine *forgedResultEngine) EngineContract() Contract { return engine.contract }

func (*forgedResultEngine) DeriveObservation(_ context.Context, rule ObservationRule) ObservationResult {
	if rule.Fact == FactContextArgv {
		return ObservedValue{
			observationMetadata: metadataFromRule(rule),
			value:               "caller-supplied --ctx-size 999999",
		}
	}
	return ObserveValue(rule, internalGoodRawValue(rule.ValueContract))
}

func internalCompleteContract() Contract {
	rules := make([]ObservationRule, 0, len(MeasuredFacts()))
	for _, definition := range MeasuredFacts() {
		rules = append(rules, ObservationRule{
			Fact:          definition.Fact,
			Method:        "engine-derivation/" + string(definition.Fact),
			ValueContract: valueContractForFact(definition.Fact),
			OnFailure: FailurePolicy{
				ReadFailure: FailureActionRefuse,
				Malformed:   FailureActionRefuse,
				Unsupported: FailureActionRefuse,
			},
		})
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

func internalGoodRawValue(contract ValueContract) string {
	switch contract {
	case ValueContractArgvTokens:
		return `["--ctx-size","4096"]`
	case ValueContractJSONFieldPath:
		return "delta.reasoning_content"
	case ValueContractAbsolutePath:
		path, err := filepath.Abs(filepath.Join("testdata", "engine"))
		if err != nil {
			panic(err)
		}
		return path
	case ValueContractCanonicalObject:
		return `{"observed":true}`
	default:
		panic("unexpected value contract: " + contract)
	}
}

func TestResolveObservedIndependentlyRefusesAProcessLabeledForgedValue(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(&forgedResultEngine{contract: internalCompleteContract()}); err != nil {
		t.Fatalf("Register(forged engine): %v", err)
	}

	_, err := ResolveObserved(context.Background(), registry, "forged-engine")
	if !errors.Is(err, ErrObservationMalformed) {
		t.Fatalf("ResolveObserved admitted process-labeled forged argv: %v", err)
	}
}
