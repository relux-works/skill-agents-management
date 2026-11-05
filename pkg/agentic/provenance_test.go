package agentic_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

func TestConsumerProvenancePublishesVersionedConfiguredAndResolvedEngines(t *testing.T) {
	engine := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	plan := agentic.Plan{
		System: "pi",
		Provenance: agentic.LaunchProvenance{
			Broker: "local-models", Publisher: "alibaba", Family: "qwen",
			Runtime: "local-qwen", Profile: "local-qwen", Model: "qwen-data-row",
			RequestedEngine: engine, ResolvedEngine: engine,
		},
	}

	projection, err := plan.ConsumerProvenance()
	if err != nil {
		t.Fatalf("ConsumerProvenance: %v", err)
	}
	body, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"contract":"agents-management.launch-provenance","schema_version":1,"system":"pi","broker":"local-models","publisher":"alibaba","family":"qwen","engine_binding":"required","configured_engine":{"id":"mlx","kind":"inference-engine"},"resolved_engine":{"id":"mlx","kind":"inference-engine"},"runtime":"local-qwen","profile":"local-qwen","model":"qwen-data-row"}`
	if string(body) != want {
		t.Fatalf("projection JSON = %s\nwant = %s", body, want)
	}
}

func TestConsumerProvenanceRefusesConfiguredResolvedMismatch(t *testing.T) {
	configured := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	plan := agentic.Plan{
		System: "pi",
		Provenance: agentic.LaunchProvenance{
			Broker: "local-models", Publisher: "alibaba", Family: "qwen",
			Runtime: "local-qwen", Profile: "local-qwen", Model: "qwen-data-row",
			RequestedEngine: configured,
			ResolvedEngine:  plugin.Ref{ID: "other", Kind: inferenceengine.Kind},
		},
	}
	if _, err := plan.ConsumerProvenance(); !errors.Is(err, agentic.ErrLaunchProvenanceMismatch) {
		t.Fatalf("ConsumerProvenance mismatch err = %v, want ErrLaunchProvenanceMismatch", err)
	}
}

func TestLaunchProvenanceV1RefusesMismatchFixture(t *testing.T) {
	engine := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	var projection agentic.LaunchProvenanceV1
	body, err := os.ReadFile("testdata/local-qwen-engine-mismatch-v1.json")
	if err != nil {
		t.Fatalf("ReadFile mismatch fixture: %v", err)
	}
	if err := json.Unmarshal(body, &projection); err != nil {
		t.Fatalf("Unmarshal mismatch fixture: %v", err)
	}
	if err := projection.ValidateAgainst(engine, engine); !errors.Is(err, agentic.ErrLaunchProvenanceMismatch) {
		t.Fatalf("ValidateAgainst mismatch fixture = %v, want ErrLaunchProvenanceMismatch", err)
	}
}

func TestLaunchProvenanceV1RefusesPartialAndUnknownEvidence(t *testing.T) {
	engine := &agentic.PluginRefV1{ID: "mlx", Kind: inferenceengine.Kind}
	base := agentic.LaunchProvenanceV1{
		Contract: agentic.LaunchProvenanceContract, SchemaVersion: agentic.LaunchProvenanceSchemaVersion,
		System: "pi", Broker: "local-models", Publisher: "alibaba", Family: "qwen",
		EngineBinding:    agentic.EngineBindingRequiredV1,
		ConfiguredEngine: engine, ResolvedEngine: engine,
		Runtime: "local-qwen", Profile: "local-qwen", Model: "qwen-data-row",
	}
	authority := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	for _, test := range []struct {
		name   string
		mutate func(*agentic.LaunchProvenanceV1)
		want   error
	}{
		{name: "configured engine absent", mutate: func(p *agentic.LaunchProvenanceV1) { p.ConfiguredEngine = nil }, want: agentic.ErrLaunchProvenanceMismatch},
		{name: "resolved engine absent", mutate: func(p *agentic.LaunchProvenanceV1) { p.ResolvedEngine = nil }, want: agentic.ErrLaunchProvenanceMismatch},
		{name: "binding absent", mutate: func(p *agentic.LaunchProvenanceV1) { p.EngineBinding = "" }, want: agentic.ErrLaunchProvenanceInvalid},
		{name: "wrong kind", mutate: func(p *agentic.LaunchProvenanceV1) {
			p.ConfiguredEngine.Kind = "vendor"
			p.ResolvedEngine.Kind = "vendor"
		}, want: agentic.ErrLaunchProvenanceInvalid},
		{name: "unnormalized id", mutate: func(p *agentic.LaunchProvenanceV1) { p.ConfiguredEngine.ID = "MLX"; p.ResolvedEngine.ID = "MLX" }, want: agentic.ErrLaunchProvenanceInvalid},
		{name: "unnormalized kind", mutate: func(p *agentic.LaunchProvenanceV1) {
			p.ConfiguredEngine.Kind = "Inference-Engine"
			p.ResolvedEngine.Kind = "Inference-Engine"
		}, want: agentic.ErrLaunchProvenanceInvalid},
		{name: "unknown schema", mutate: func(p *agentic.LaunchProvenanceV1) { p.SchemaVersion++ }, want: agentic.ErrLaunchProvenanceInvalid},
		{name: "missing broker axis", mutate: func(p *agentic.LaunchProvenanceV1) { p.Broker = "" }, want: agentic.ErrLaunchProvenanceInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuredEngine := *base.ConfiguredEngine
			resolvedEngine := *base.ResolvedEngine
			projection := base
			projection.ConfiguredEngine = &configuredEngine
			projection.ResolvedEngine = &resolvedEngine
			test.mutate(&projection)
			if err := projection.ValidateAgainst(authority, authority); !errors.Is(err, test.want) {
				t.Fatalf("ValidateAgainst = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConsumerProvenancePreservesNoEngineAbsence(t *testing.T) {
	projection, err := (agentic.Plan{System: "codex"}).ConsumerProvenance()
	if err != nil {
		t.Fatalf("ConsumerProvenance(no engine): %v", err)
	}
	if projection.ConfiguredEngine != nil || projection.ResolvedEngine != nil {
		t.Fatalf("no-engine projection invented refs: %#v", projection)
	}
	if projection.EngineBinding != agentic.EngineBindingNoneV1 {
		t.Fatalf("no-engine projection binding = %q, want %q", projection.EngineBinding, agentic.EngineBindingNoneV1)
	}
}

func TestLaunchProvenanceV1RefusesNoEngineRecordWithoutSystemIdentity(t *testing.T) {
	projection := agentic.LaunchProvenanceV1{
		Contract:      agentic.LaunchProvenanceContract,
		SchemaVersion: agentic.LaunchProvenanceSchemaVersion,
		EngineBinding: agentic.EngineBindingNoneV1,
	}
	if err := projection.ValidateAgainst(plugin.Ref{}, plugin.Ref{}); !errors.Is(err, agentic.ErrLaunchProvenanceInvalid) {
		t.Fatalf("ValidateAgainst(no-engine record without system) = %v, want ErrLaunchProvenanceInvalid", err)
	}
}

func TestLaunchProvenanceV1RefusesPersistedPairThatContradictsIndependentAuthority(t *testing.T) {
	forged := &agentic.PluginRefV1{ID: "attacker-engine", Kind: inferenceengine.Kind}
	projection := agentic.LaunchProvenanceV1{
		Contract: agentic.LaunchProvenanceContract, SchemaVersion: agentic.LaunchProvenanceSchemaVersion,
		System: "pi", Broker: "local-models", Publisher: "alibaba", Family: "qwen",
		EngineBinding: agentic.EngineBindingRequiredV1, ConfiguredEngine: forged, ResolvedEngine: forged,
		Runtime: "local-qwen", Profile: "local-qwen", Model: "qwen-data-row",
	}
	trusted := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	if err := projection.ValidateAgainst(trusted, trusted); !errors.Is(err, agentic.ErrLaunchProvenanceMismatch) {
		t.Fatalf("ValidateAgainst(equal forged pair, trusted mlx) = %v, want ErrLaunchProvenanceMismatch", err)
	}
}
