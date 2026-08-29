package inferenceengine_test

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
)

func goodValue(fact inferenceengine.Fact) string {
	switch fact {
	case inferenceengine.FactContextArgv:
		return `["--ctx-size","4096"]`
	case inferenceengine.FactPrefillArgv:
		return `["--ubatch-size","512"]`
	case inferenceengine.FactReasoningStreamField:
		return "delta.reasoning_content"
	case inferenceengine.FactHealth:
		return `{"endpoint":"/health","healthy":true}`
	case inferenceengine.FactReadiness:
		return `{"weights_resident":true}`
	case inferenceengine.FactWeightArtifact:
		return `{"format":"gguf","model_path":"/models/model.gguf"}`
	case inferenceengine.FactMemoryAccounting:
		return `{"method":"mmap-aware","bytes":4096,"includes_mapped_weights":true}`
	case inferenceengine.FactSpeculativeDecoding:
		return `{"capable":true,"active":false}`
	case inferenceengine.FactLoadState:
		return `{"state":"loaded","weights_resident":true}`
	case inferenceengine.FactUnloadState:
		return `{"state":"unloaded","weights_resident":false}`
	case inferenceengine.FactInferenceBusy:
		return `{"busy":false}`
	case inferenceengine.FactMemoryPressureSequence:
		return `{"pressure":"warning","consulted":["load-state","unload-state","inference-busy"],"action":"unload-idle"}`
	case inferenceengine.FactLocalExecutable:
		return "/usr/local/bin/llama-server"
	case inferenceengine.FactLocalArgv:
		return `["--port","8080"]`
	case inferenceengine.FactSSHForwarding:
		return `{"host":"model-host","local_port":8080,"remote_port":8080}`
	case inferenceengine.FactStressPolicy:
		return `{"enabled":true,"max_concurrency":2}`
	case inferenceengine.FactRestartSupervisionPolicy:
		return `{"max_attempts":3,"initial_backoff_ms":100,"max_backoff_ms":1000}`
	default:
		panic("unknown fact: " + fact)
	}
}

func readings(overrides map[inferenceengine.Fact]inferenceengine.ReadOutcome) []inferenceengine.Reading {
	all := map[inferenceengine.Fact]inferenceengine.ReadOutcome{inferenceengine.FactSSHForwarding: inferenceengine.ReadAbsent()}
	for fact, result := range overrides {
		all[fact] = result
	}
	result := make([]inferenceengine.Reading, 0, len(inferenceengine.MeasuredFacts()))
	for _, definition := range inferenceengine.MeasuredFacts() {
		outcome, ok := all[definition.Fact]
		if !ok {
			outcome = inferenceengine.ReadValue(goodValue(definition.Fact))
		}
		result = append(result, inferenceengine.NewReading(definition.Fact, outcome))
	}
	return result
}

func validate(overrides map[inferenceengine.Fact]inferenceengine.ReadOutcome) (inferenceengine.Resolution, error) {
	return inferenceengine.ValidateReadings("mlx", inferenceengine.EngineKindNativeTransformer, readings(overrides))
}

func TestValidatorUsesConcreteEngineKindAndClosedInventory(t *testing.T) {
	resolved, err := validate(nil)
	if err != nil {
		t.Fatalf("ValidateReadings: %v", err)
	}
	if resolved.EngineKind != inferenceengine.EngineKindNativeTransformer || resolved.ProfileMode != inferenceengine.ProfileModeLocal {
		t.Fatalf("resolved kind/mode = %q/%q", resolved.EngineKind, resolved.ProfileMode)
	}
	definitions := inferenceengine.MeasuredFacts()
	if len(resolved.Results) != len(definitions) {
		t.Fatalf("results = %d, want %d", len(resolved.Results), len(definitions))
	}
	for index, definition := range definitions {
		if resolved.Results[index].Fact() != definition.Fact {
			t.Fatalf("result %d escaped closed inventory", index)
		}
		if len(definition.Evidence) == 0 {
			t.Fatalf("fact %s has no measurement provenance", definition.Fact)
		}
	}
}

func TestValidatorRefusesUnsupportedImplementationKindAndIncompleteInventory(t *testing.T) {
	if _, err := inferenceengine.ValidateReadings("mlx", "forged-kind", readings(nil)); !errors.Is(err, inferenceengine.ErrEngineContractMissing) {
		t.Fatalf("forged kind error = %v", err)
	}
	if _, err := inferenceengine.ValidateReadings("mlx", inferenceengine.EngineKindNativeTransformer, readings(nil)[:1]); !errors.Is(err, inferenceengine.ErrObservationMalformed) {
		t.Fatalf("incomplete inventory error = %v", err)
	}
}

func TestEveryMeasuredFactHasObservedAbsentMalformedAndUnsupportedSemantics(t *testing.T) {
	malformed := map[inferenceengine.Fact]string{
		inferenceengine.FactContextArgv: `{}`, inferenceengine.FactPrefillArgv: `[]`, inferenceengine.FactReasoningStreamField: "reasoning",
		inferenceengine.FactHealth: `{"endpoint":""}`, inferenceengine.FactReadiness: `{"weights_resident":false}`,
		inferenceengine.FactWeightArtifact: `{"format":"gguf"}`, inferenceengine.FactMemoryAccounting: `{"method":"physical","bytes":1,"includes_mapped_weights":false}`,
		inferenceengine.FactSpeculativeDecoding: `{"capable":false,"active":true}`, inferenceengine.FactLoadState: `{"state":"loaded","weights_resident":false}`,
		inferenceengine.FactUnloadState: `{"state":"unloaded","weights_resident":true}`, inferenceengine.FactInferenceBusy: `{"busy":"sometimes"}`,
		inferenceengine.FactMemoryPressureSequence: `{"pressure":"critical","action":"evict"}`, inferenceengine.FactLocalExecutable: "relative/bin",
		inferenceengine.FactLocalArgv: `[""]`, inferenceengine.FactSSHForwarding: `{"host":"x","local_port":0,"remote_port":22}`,
		inferenceengine.FactStressPolicy: `{"enabled":true,"max_concurrency":0}`, inferenceengine.FactRestartSupervisionPolicy: `{"max_attempts":1,"initial_backoff_ms":100,"max_backoff_ms":10}`,
	}
	for _, definition := range inferenceengine.MeasuredFacts() {
		fact := definition.Fact
		t.Run(string(fact)+"/observed", func(t *testing.T) {
			if _, err := validate(nil); err != nil {
				t.Fatalf("observed: %v", err)
			}
		})
		t.Run(string(fact)+"/absent", func(t *testing.T) {
			overrides := map[inferenceengine.Fact]inferenceengine.ReadOutcome{fact: inferenceengine.ReadAbsent()}
			if fact == inferenceengine.FactLocalExecutable || fact == inferenceengine.FactLocalArgv {
				overrides[inferenceengine.FactLocalExecutable] = inferenceengine.ReadAbsent()
				overrides[inferenceengine.FactLocalArgv] = inferenceengine.ReadAbsent()
				overrides[inferenceengine.FactSSHForwarding] = inferenceengine.ReadValue(goodValue(inferenceengine.FactSSHForwarding))
			}
			if _, err := validate(overrides); err != nil {
				t.Fatalf("positive absence: %v", err)
			}
		})
		t.Run(string(fact)+"/malformed", func(t *testing.T) {
			_, err := validate(map[inferenceengine.Fact]inferenceengine.ReadOutcome{fact: inferenceengine.ReadValue(malformed[fact])})
			if !errors.Is(err, inferenceengine.ErrObservationMalformed) {
				t.Fatalf("malformed error = %v", err)
			}
		})
		t.Run(string(fact)+"/unsupported", func(t *testing.T) {
			_, err := validate(map[inferenceengine.Fact]inferenceengine.ReadOutcome{fact: inferenceengine.ReadFailure(inferenceengine.NotObservedUnsupported, "unsupported")})
			if !errors.Is(err, inferenceengine.ErrObservationUnsupported) {
				t.Fatalf("unsupported error = %v", err)
			}
		})
	}
}

func TestProductionEntryDistinguishesAbsenceFromReadFailure(t *testing.T) {
	absent := map[inferenceengine.Fact]inferenceengine.ReadOutcome{inferenceengine.FactSpeculativeDecoding: inferenceengine.ReadAbsent()}
	if _, err := validate(absent); err != nil {
		t.Fatalf("positive absence refused: %v", err)
	}
	failed := map[inferenceengine.Fact]inferenceengine.ReadOutcome{inferenceengine.FactSpeculativeDecoding: inferenceengine.ReadFailure(inferenceengine.NotObservedReadFailure, "endpoint reset")}
	if _, err := validate(failed); !errors.Is(err, inferenceengine.ErrObservationRead) {
		t.Fatalf("read failure = %v", err)
	}
}

func TestProductionEntryRefusesReviewerArbitraryJSONMutants(t *testing.T) {
	tests := []struct {
		name string
		fact inferenceengine.Fact
		raw  string
	}{
		{"readiness endpoint proxy", inferenceengine.FactReadiness, `{"endpoint_answering":true,"weights_resident":false}`},
		{"busy string", inferenceengine.FactInferenceBusy, `{"busy":"sometimes"}`},
		{"pressure without sequence", inferenceengine.FactMemoryPressureSequence, `{"pressure":"critical","action":"evict"}`},
		{"unknown readiness member", inferenceengine.FactReadiness, `{"weights_resident":true,"forged":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validate(map[inferenceengine.Fact]inferenceengine.ReadOutcome{test.fact: inferenceengine.ReadValue(test.raw)})
			if !errors.Is(err, inferenceengine.ErrObservationMalformed) {
				t.Fatalf("mutant admitted: %v", err)
			}
		})
	}
}

func TestProductionEntryEnforcesExclusiveLocalOrSSHExpansion(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		resolved, err := validate(nil)
		if err != nil || resolved.ProfileMode != inferenceengine.ProfileModeLocal {
			t.Fatalf("local = %#v, %v", resolved, err)
		}
	})
	t.Run("ssh", func(t *testing.T) {
		reader := map[inferenceengine.Fact]inferenceengine.ReadOutcome{inferenceengine.FactLocalExecutable: inferenceengine.ReadAbsent(), inferenceengine.FactLocalArgv: inferenceengine.ReadAbsent(), inferenceengine.FactSSHForwarding: inferenceengine.ReadValue(goodValue(inferenceengine.FactSSHForwarding))}
		resolved, err := validate(reader)
		if err != nil || resolved.ProfileMode != inferenceengine.ProfileModeSSH {
			t.Fatalf("ssh = %#v, %v", resolved, err)
		}
	})
	tests := []struct {
		name      string
		overrides map[inferenceengine.Fact]inferenceengine.ReadOutcome
	}{
		{"simultaneous local and ssh", map[inferenceengine.Fact]inferenceengine.ReadOutcome{inferenceengine.FactSSHForwarding: inferenceengine.ReadValue(goodValue(inferenceengine.FactSSHForwarding))}},
		{"half local", map[inferenceengine.Fact]inferenceengine.ReadOutcome{inferenceengine.FactLocalArgv: inferenceengine.ReadAbsent()}},
		{"nothing selected", map[inferenceengine.Fact]inferenceengine.ReadOutcome{inferenceengine.FactLocalExecutable: inferenceengine.ReadAbsent(), inferenceengine.FactLocalArgv: inferenceengine.ReadAbsent(), inferenceengine.FactSSHForwarding: inferenceengine.ReadAbsent()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validate(test.overrides)
			if !errors.Is(err, inferenceengine.ErrProfileExpansionInvalid) {
				t.Fatalf("invalid expansion admitted: %v", err)
			}
		})
	}
}

func TestValidatorRefusesMissingDuplicateAndReorderedFacts(t *testing.T) {
	complete := readings(nil)
	if _, err := inferenceengine.ValidateReadings("mlx", inferenceengine.EngineKindNativeTransformer, complete[:len(complete)-1]); !errors.Is(err, inferenceengine.ErrObservationMalformed) {
		t.Fatalf("missing fact = %v", err)
	}
	reordered := append([]inferenceengine.Reading(nil), complete...)
	reordered[0], reordered[1] = reordered[1], reordered[0]
	if _, err := inferenceengine.ValidateReadings("mlx", inferenceengine.EngineKindNativeTransformer, reordered); !errors.Is(err, inferenceengine.ErrObservationMalformed) {
		t.Fatalf("reordered fact = %v", err)
	}
}

func TestResolutionIsDefensivelyCopied(t *testing.T) {
	resolved, err := validate(nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved.Contract.Rules[0].Method = "forged"
	resolved.Results[0] = nil
	again, err := validate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Contract.Rules[0].Method == "forged" || again.Results[0] == nil {
		t.Fatal("caller mutation escaped resolution")
	}
}
