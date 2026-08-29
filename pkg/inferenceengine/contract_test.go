package inferenceengine_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type declarationEngine struct {
	declaration plugin.Declaration
	contracts   []inferenceengine.Contract
	reads       int
}

type wrongKindEngine struct{}

func (wrongKindEngine) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: "sidecar", Kind: "sidecar"}
}

func (wrongKindEngine) EngineContract() inferenceengine.Contract {
	return inferenceengine.RequiredContract()
}

func (e *declarationEngine) PluginDeclaration() plugin.Declaration { return e.declaration }

func (e *declarationEngine) EngineContract() inferenceengine.Contract {
	contract := e.contracts[0]
	if len(e.contracts) > 1 {
		e.contracts = e.contracts[1:]
	}
	return contract
}

// DeriveObservation intentionally looks like the rejected revision-2 API. It
// is extra caller behavior, not part of inferenceengine.Engine, and must never
// be called by contract resolution.
func (e *declarationEngine) DeriveObservation(inferenceengine.Fact) string {
	e.reads++
	return `["--ctx-size","999999"]`
}

func registryWithEngine(t *testing.T, contract inferenceengine.Contract) (*plugin.Registry, *declarationEngine) {
	t.Helper()
	engine := &declarationEngine{
		declaration: plugin.Declaration{ID: "llama-cpp", Kind: inferenceengine.Kind},
		contracts:   []inferenceengine.Contract{contract},
	}
	registry := plugin.NewRegistry()
	if err := registry.Register(engine); err != nil {
		t.Fatalf("Register(engine): %v", err)
	}
	return registry, engine
}

func TestEngineTrustBoundaryIsDeclarationOnly(t *testing.T) {
	engine := reflect.TypeOf((*inferenceengine.Engine)(nil)).Elem()
	want := []string{"EngineContract", "PluginDeclaration"}
	if engine.NumMethod() != len(want) {
		t.Fatalf("Engine has %d methods, want declaration-only %v", engine.NumMethod(), want)
	}
	for index, name := range want {
		if engine.Method(index).Name != name {
			t.Fatalf("Engine method %d = %q, want %q", index, engine.Method(index).Name, name)
		}
	}
	if _, found := engine.MethodByName("DeriveObservation"); found {
		t.Fatal("caller-registered Engine can still substitute agents-infra derivation")
	}
}

func TestResolveContractNeverCallsCallerMintedDerivation(t *testing.T) {
	registry, engine := registryWithEngine(t, inferenceengine.RequiredContract())
	resolved, err := inferenceengine.ResolveContract(registry, "llama-cpp")
	if err != nil {
		t.Fatalf("ResolveContract: %v", err)
	}
	if engine.reads != 0 {
		t.Fatalf("ResolveContract called caller derivation %d times", engine.reads)
	}
	resolvedType := reflect.TypeOf(resolved)
	for _, forbidden := range []string{"Results", "Values", "Observations"} {
		if _, found := resolvedType.FieldByName(forbidden); found {
			t.Fatalf("ResolvedContract exposes caller-minted %s", forbidden)
		}
	}
	if resolved.Declaration.ID != "llama-cpp" || resolved.Contract.Version != inferenceengine.ContractVersion {
		t.Fatalf("resolved declaration = %#v", resolved)
	}
}

func TestRequiredContractClosesEveryMeasuredKnobAndFailure(t *testing.T) {
	contract := inferenceengine.RequiredContract()
	definitions := inferenceengine.MeasuredFacts()
	if len(contract.Rules) != len(definitions) {
		t.Fatalf("rules=%d definitions=%d", len(contract.Rules), len(definitions))
	}
	seenContracts := map[inferenceengine.ValueContract]bool{}
	for index, definition := range definitions {
		rule := contract.Rules[index]
		if rule.Fact != definition.Fact || rule.Source == "" || rule.ValueContract == "" {
			t.Fatalf("rule %d = %#v for definition %#v", index, rule, definition)
		}
		if rule.OnFailure.ReadFailure != inferenceengine.FailureActionRefuse ||
			rule.OnFailure.Malformed != inferenceengine.FailureActionRefuse ||
			rule.OnFailure.Unsupported != inferenceengine.FailureActionRefuse {
			t.Fatalf("%s silently drops a derivation failure: %#v", rule.Fact, rule.OnFailure)
		}
		if len(definition.Evidence) == 0 {
			t.Fatalf("%s has no measurement provenance", definition.Fact)
		}
		seenContracts[rule.ValueContract] = true
	}
	if len(seenContracts) != len(definitions) {
		t.Fatalf("fact-specific contracts collapsed: %d contracts for %d facts", len(seenContracts), len(definitions))
	}
	if contract.ProfileExpansion.ExecutionOwner != inferenceengine.ExecutionOwner || inferenceengine.ExecutionOwner != "agents-infra" {
		t.Fatalf("execution owner = %q", contract.ProfileExpansion.ExecutionOwner)
	}
}

func TestValidateCandidateValueAcceptsClosedFactSpecificShapes(t *testing.T) {
	abs := func(path string) string {
		value, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	values := map[inferenceengine.Fact]string{
		inferenceengine.FactContextArgv:              `["--ctx-size","4096"]`,
		inferenceengine.FactPrefillArgv:              `["--prefill-step-size","2048"]`,
		inferenceengine.FactReasoningStreamField:     `delta.reasoning_content`,
		inferenceengine.FactHealth:                   `{"process_alive":true,"endpoint_answering":true}`,
		inferenceengine.FactReadiness:                `{"endpoint_answering":true,"weights_resident":true}`,
		inferenceengine.FactWeightArtifact:           `{"shape":"gguf","weight_files":["` + abs("model.gguf") + `"],"mmproj_path":"` + abs("mmproj.gguf") + `"}`,
		inferenceengine.FactMemoryAccounting:         `{"artifact_mapping":"memory-mapped","method":"process-footprint-plus-mapped-resident-pages","bytes":4096}`,
		inferenceengine.FactSpeculativeDecoding:      `{"capability":"supported","active":true}`,
		inferenceengine.FactLoadState:                `{"state":"resident","transition":"loaded","sequence":1}`,
		inferenceengine.FactUnloadState:              `{"state":"not-resident","transition":"unloaded","sequence":2}`,
		inferenceengine.FactInferenceBusy:            `{"state":"idle","sequence":3}`,
		inferenceengine.FactMemoryPressureSequence:   `{"pressure_state":"pressured","load_state":"resident","unload_state":"loaded","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"drain-idle"}`,
		inferenceengine.FactLocalExecutable:          abs("model-harness"),
		inferenceengine.FactLocalArgv:                `["run","qwen","--port","18011"]`,
		inferenceengine.FactSSHForwarding:            `{"mode":"ssh","profile":"qwen-remote","local_host":"127.0.0.1","local_port":18011,"remote_host":"127.0.0.1","remote_port":18011}`,
		inferenceengine.FactStressPolicy:             `{"mode":"synthetic-prefill","prompt_tokens":4096,"output_tokens":32,"memory_sample":"process-and-mappings"}`,
		inferenceengine.FactRestartSupervisionPolicy: `{"mode":"bounded-backoff","max_restarts":3,"backoff_seconds":[1,2,4]}`,
	}
	for _, definition := range inferenceengine.MeasuredFacts() {
		raw, found := values[definition.Fact]
		if !found {
			t.Fatalf("missing fixture for %s", definition.Fact)
		}
		t.Run(string(definition.Fact), func(t *testing.T) {
			if canonical, err := inferenceengine.ValidateCandidateValue(definition.Fact, raw); err != nil || canonical == "" {
				t.Fatalf("ValidateCandidateValue(%s) = %q, %v", definition.Fact, canonical, err)
			}
		})
	}
}

func TestReadinessRefusesEndpointAnsweringWithoutResidentWeights(t *testing.T) {
	_, err := inferenceengine.ValidateCandidateValue(
		inferenceengine.FactReadiness,
		`{"endpoint_answering":true,"weights_resident":false}`,
	)
	if !errors.Is(err, inferenceengine.ErrObservationMalformed) {
		t.Fatalf("false readiness admitted: %v", err)
	}
}

func TestStructuredFactsRefuseNarrowedSemantics(t *testing.T) {
	tests := []struct {
		name string
		fact inferenceengine.Fact
		raw  string
	}{
		{"mmap uses naive footprint", inferenceengine.FactMemoryAccounting, `{"artifact_mapping":"memory-mapped","method":"mach-physical-footprint","bytes":4096}`},
		{"load presence without transition", inferenceengine.FactLoadState, `{"state":"resident","transition":"","sequence":1}`},
		{"unload presence without ordering", inferenceengine.FactUnloadState, `{"state":"not-resident","transition":"unloaded","sequence":0}`},
		{"busy presence without state", inferenceengine.FactInferenceBusy, `{"state":"unknown","sequence":1}`},
		{"pressure skips unload consultation", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"pressured","load_state":"resident","unload_state":"loaded","inference_busy":"idle","order":["pressure","load-state","inference-busy","relief-action"],"action":"drain-idle"}`},
		{"busy pressure unloads", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"pressured","load_state":"resident","unload_state":"loaded","inference_busy":"busy","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"drain-idle"}`},
		{"unknown input acts healthy", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"unknown","load_state":"unknown","unload_state":"unknown","inference_busy":"unknown","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`},
		{"generic object field", inferenceengine.FactReadiness, `{"endpoint_answering":true,"weights_resident":true,"observed":true}`},
		{"wrong context spelling", inferenceengine.FactContextArgv, `["--max-model-len","4096"]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := inferenceengine.ValidateCandidateValue(test.fact, test.raw)
			if !errors.Is(err, inferenceengine.ErrObservationMalformed) {
				t.Fatalf("candidate admitted: %v", err)
			}
		})
	}
}

func TestClosedSchemasRejectMalformedVariants(t *testing.T) {
	abs := func(path string) string {
		value, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	tests := []struct {
		name string
		fact inferenceengine.Fact
		raw  string
	}{
		{"empty argv", inferenceengine.FactLocalArgv, `[]`},
		{"empty argv token", inferenceengine.FactLocalArgv, `["run",""]`},
		{"knob has extra token", inferenceengine.FactPrefillArgv, `["-ub","2048","extra"]`},
		{"knob is not positive", inferenceengine.FactContextArgv, `["--ctx-size","0"]`},
		{"wrong stream field", inferenceengine.FactReasoningStreamField, `delta.content`},
		{"relative executable", inferenceengine.FactLocalExecutable, `model-harness`},
		{"unhealthy health", inferenceengine.FactHealth, `{"process_alive":true,"endpoint_answering":false}`},
		{"artifact has no files", inferenceengine.FactWeightArtifact, `{"shape":"gguf","weight_files":[]}`},
		{"artifact has relative file", inferenceengine.FactWeightArtifact, `{"shape":"gguf","weight_files":["model.gguf"]}`},
		{"safetensors lacks config", inferenceengine.FactWeightArtifact, `{"shape":"safetensors","weight_files":["` + abs("model.safetensors") + `"]}`},
		{"safetensors config is relative", inferenceengine.FactWeightArtifact, `{"shape":"safetensors","weight_files":["` + abs("model.safetensors") + `"],"config_path":"config.json"}`},
		{"safetensors has wrong extension", inferenceengine.FactWeightArtifact, `{"shape":"safetensors","weight_files":["` + abs("model.bin") + `"],"config_path":"` + abs("config.json") + `"}`},
		{"gguf has multiple weights", inferenceengine.FactWeightArtifact, `{"shape":"gguf","weight_files":["` + abs("a.gguf") + `","` + abs("b.gguf") + `"]}`},
		{"gguf has wrong mmproj", inferenceengine.FactWeightArtifact, `{"shape":"gguf","weight_files":["` + abs("model.gguf") + `"],"mmproj_path":"` + abs("mmproj.bin") + `"}`},
		{"unknown artifact shape", inferenceengine.FactWeightArtifact, `{"shape":"bin","weight_files":["` + abs("model.bin") + `"]}`},
		{"unknown mapping", inferenceengine.FactMemoryAccounting, `{"artifact_mapping":"other","method":"mach-physical-footprint","bytes":1}`},
		{"nonpositive memory", inferenceengine.FactMemoryAccounting, `{"artifact_mapping":"anonymous","method":"mach-physical-footprint","bytes":0}`},
		{"unknown speculative capability", inferenceengine.FactSpeculativeDecoding, `{"capability":"unknown","active":false}`},
		{"absent speculative capability active", inferenceengine.FactSpeculativeDecoding, `{"capability":"absent","active":true}`},
		{"pressure load unload conflict", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"healthy","load_state":"resident","unload_state":"unloaded","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`},
		{"pressure invalid load", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"healthy","load_state":"bad","unload_state":"unknown","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`},
		{"pressure invalid unload", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"healthy","load_state":"unknown","unload_state":"bad","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`},
		{"pressure invalid busy", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"healthy","load_state":"unknown","unload_state":"unknown","inference_busy":"bad","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`},
		{"healthy drains", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"healthy","load_state":"resident","unload_state":"loaded","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"drain-idle"}`},
		{"idle pressure merely observes", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"pressured","load_state":"resident","unload_state":"loaded","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`},
		{"unloaded pressure drains", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"pressured","load_state":"not-resident","unload_state":"unloaded","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"drain-idle"}`},
		{"unknown pressure state", inferenceengine.FactMemoryPressureSequence, `{"pressure_state":"bad","load_state":"unknown","unload_state":"unknown","inference_busy":"unknown","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"refuse-new"}`},
		{"bad ssh mode", inferenceengine.FactSSHForwarding, `{"mode":"local","profile":"p","local_host":"127.0.0.1","local_port":1,"remote_host":"127.0.0.1","remote_port":1}`},
		{"unbounded stress", inferenceengine.FactStressPolicy, `{"mode":"synthetic-prefill","prompt_tokens":0,"output_tokens":1,"memory_sample":"process-and-mappings"}`},
		{"restart lacks backoff", inferenceengine.FactRestartSupervisionPolicy, `{"mode":"bounded-backoff","max_restarts":1,"backoff_seconds":[]}`},
		{"restart backoff not increasing", inferenceengine.FactRestartSupervisionPolicy, `{"mode":"bounded-backoff","max_restarts":2,"backoff_seconds":[2,1]}`},
		{"trailing json", inferenceengine.FactHealth, `{"process_alive":true,"endpoint_answering":true} {}`},
		{"unknown fact", inferenceengine.Fact("future-fact"), `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := inferenceengine.ValidateCandidateValue(test.fact, test.raw); !errors.Is(err, inferenceengine.ErrObservationMalformed) {
				t.Fatalf("candidate admitted: %v", err)
			}
		})
	}
}

func TestPressureSchemasAcceptSafeNonEvictingStates(t *testing.T) {
	values := []string{
		`{"pressure_state":"healthy","load_state":"resident","unload_state":"loaded","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`,
		`{"pressure_state":"pressured","load_state":"resident","unload_state":"loaded","inference_busy":"busy","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"refuse-new"}`,
		`{"pressure_state":"unknown","load_state":"unknown","unload_state":"unknown","inference_busy":"unknown","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"refuse-new"}`,
		`{"pressure_state":"pressured","load_state":"not-resident","unload_state":"unloaded","inference_busy":"idle","order":["pressure","load-state","unload-state","inference-busy","relief-action"],"action":"observe"}`,
	}
	for index, raw := range values {
		if _, err := inferenceengine.ValidateCandidateValue(inferenceengine.FactMemoryPressureSequence, raw); err != nil {
			t.Fatalf("safe pressure state %d: %v", index, err)
		}
	}
}

func TestResolveContractRefusesFallbacksAndUntrustedSources(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*inferenceengine.Contract)
	}{
		{"missing fact", func(contract *inferenceengine.Contract) { contract.Rules = contract.Rules[1:] }},
		{"duplicate fact", func(contract *inferenceengine.Contract) { contract.Rules[1] = contract.Rules[0] }},
		{"unknown fact", func(contract *inferenceengine.Contract) { contract.Rules[0].Fact = "future-fact" }},
		{"read failure reported as absence", func(contract *inferenceengine.Contract) { contract.Rules[0].OnFailure.ReadFailure = "observed-absent" }},
		{"malformed reported as absence", func(contract *inferenceengine.Contract) { contract.Rules[0].OnFailure.Malformed = "observed-absent" }},
		{"unsupported silently dropped", func(contract *inferenceengine.Contract) { contract.Rules[0].OnFailure.Unsupported = "" }},
		{"caller engine becomes source", func(contract *inferenceengine.Contract) { contract.Rules[0].Source = "engine/self-report" }},
		{"generic json replaces readiness schema", func(contract *inferenceengine.Contract) { contract.Rules[4].ValueContract = "canonical-json-object/v1" }},
		{"execution moves into plugin", func(contract *inferenceengine.Contract) {
			contract.ProfileExpansion.ExecutionOwner = "skill-agents-management"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := inferenceengine.RequiredContract()
			test.mutate(&contract)
			registry, _ := registryWithEngine(t, contract)
			_, err := inferenceengine.ResolveContract(registry, "llama-cpp")
			if !errors.Is(err, inferenceengine.ErrContractInvalid) {
				t.Fatalf("ResolveContract error = %v, want ErrContractInvalid", err)
			}
		})
	}
}

func TestResolveContractRefusesUnstableOrUntypedEngines(t *testing.T) {
	first := inferenceengine.RequiredContract()
	second := inferenceengine.RequiredContract()
	second.Rules[0].Source = "engine/self-report"
	engine := &declarationEngine{
		declaration: plugin.Declaration{ID: "llama-cpp", Kind: inferenceengine.Kind},
		contracts:   []inferenceengine.Contract{first, second},
	}
	registry := plugin.NewRegistry()
	if err := registry.Register(engine); err != nil {
		t.Fatal(err)
	}
	if _, err := inferenceengine.ResolveContract(registry, "llama-cpp"); !errors.Is(err, inferenceengine.ErrContractUnstable) {
		t.Fatalf("unstable error = %v", err)
	}

	untyped := plugin.NewRegistry()
	if err := untyped.Register(llamaEngine{}); err != nil {
		t.Fatal(err)
	}
	if _, err := inferenceengine.ResolveContract(untyped, "llama-cpp"); !errors.Is(err, inferenceengine.ErrEngineContractMissing) {
		t.Fatalf("untyped error = %v", err)
	}

	wrongKind := plugin.NewRegistry()
	if err := wrongKind.Register(wrongKindEngine{}); err != nil {
		t.Fatal(err)
	}
	if _, err := inferenceengine.ResolveContract(wrongKind, "sidecar"); !errors.Is(err, inferenceengine.ErrWrongKind) {
		t.Fatalf("wrong-kind error = %v", err)
	}
}

func TestResolveContractReturnsDefensiveCopies(t *testing.T) {
	contract := inferenceengine.RequiredContract()
	registry, _ := registryWithEngine(t, contract)
	resolved, err := inferenceengine.ResolveContract(registry, "llama-cpp")
	if err != nil {
		t.Fatal(err)
	}
	resolved.Contract.Rules[0].Source = "mutated"
	definitions := inferenceengine.MeasuredFacts()
	definitions[0].Evidence[0] = "mutated"
	if inferenceengine.RequiredContract().Rules[0].Source == "mutated" || reflect.DeepEqual(inferenceengine.MeasuredFacts()[0].Evidence, definitions[0].Evidence) {
		t.Fatal("caller mutation escaped defensive copy")
	}
}
