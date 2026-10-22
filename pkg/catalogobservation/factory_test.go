package catalogobservation_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	"github.com/relux-works/skill-agents-management/pkg/catalogobservation"
	"github.com/relux-works/skill-agents-management/pkg/engineobservation"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine/engines/llamacpp"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine/engines/mlx"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

func loadedConfig(t *testing.T, kind string) localmodels.Config {
	t.Helper()
	dir, project := t.TempDir(), t.TempDir()
	body := fmt.Sprintf(`schema_version = 1
[runtimes.selected]
system = "pi"
engine = { plugin = %q, name = "same-name" }
[runtimes.selected.models.model]
description = "fixture"
lifecycle = "current"
effort_support = "none"
pointer = { curator_engines_project = %q, curator_engines_profile = "same-name", pi_model_identity = "test/model" }
`, kind, project)
	for name, text := range map[string]string{"runtimes.toml": body, "engines.toml": fmt.Sprintf("[engines.same-name]\nengine = %q\n", kind)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _, err := localmodels.LoadAt(dir, filepath.Join(dir, "absent-legacy.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func factory(t *testing.T) *catalogobservation.Factory {
	t.Helper()
	f, err := catalogobservation.NewFactory(mlx.New(), llamacpp.New())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Real LoadAt + public composition binds the selected catalog, kind, row and
// pointer. Mutating caller config afterward cannot redirect an existing adapter.
// Exact provenance admits; malformed or absent provenance is tested separately.
func TestFactoryLoadedBindingsAndImmutableScope(t *testing.T) {
	for _, tc := range []struct {
		id   string
		kind inferenceengine.EngineKind
	}{{"mlx", inferenceengine.EngineKindNativeTransformer}, {"llama-cpp", inferenceengine.EngineKindGGUFServer}} {
		t.Run(tc.id, func(t *testing.T) {
			cfg := loadedConfig(t, tc.id)
			wantConfig, wantProject := cfg.EnginesConfig, cfg.Runtimes[0].Models["model"].Pointer.CuratorEnginesProject
			cache, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			b, err := factory(t).ForBinding(cfg, "selected", "model", cache, engineobservation.WithCommandRunner(func(_ context.Context, name string, args []string, dir string) ([]byte, error) {
				calls++
				want := []string{"status", "--engine", "same-name", "--json", "--config", wantConfig, "--cache-root", cache}
				if name != "curator-engines" || !reflect.DeepEqual(args, want) || dir != wantProject {
					t.Fatalf("invocation %s %q cwd=%q", name, args, dir)
				}
				return factoryStatus(t, wantConfig, cache), nil
			}))
			if err != nil || b.Engine == nil || len(b.Adapters) != 1 {
				t.Fatalf("binding %#v %v", b, err)
			}
			if b.Engine.PluginDeclaration().Ref() != (plugin.Ref{ID: plugin.ID(tc.id), Kind: inferenceengine.Kind}) || b.Adapters[0].EngineObservationAdapterDeclaration().EngineKind != tc.kind {
				t.Fatal("concrete kind/ref lost")
			}
			cfg.EnginesConfig = "/different/engines.toml"
			row := cfg.Runtimes[0].Models["model"]
			row.Pointer.CuratorEnginesProject = "/different/project"
			cfg.Runtimes[0].Models["model"] = row
			q := vendorplugin.EngineObservationQuery{Engine: plugin.Ref{ID: plugin.ID(tc.id), Kind: inferenceengine.Kind}, Runtime: "selected", Model: "model", Profile: "same-name"}
			got, err := b.Adapters[0].ObserveEngine(context.Background(), q)
			if err != nil || len(got.Readings) == 0 || calls != 1 {
				t.Fatalf("observation %#v %v calls=%d", got, err, calls)
			}
		})
	}
}

// Invalid selected bindings never invoke status; unselected unsupported rows
// and a pointer-only selected sibling do not create an observation requirement.
func TestFactorySelectedRefusalsAndPointerOnlySibling(t *testing.T) {
	f := factory(t)
	for _, tc := range []struct {
		name           string
		edit           func(*localmodels.Config)
		runtime, model vendorplugin.RuntimeID
		cause          error
	}{
		{"absent-runtime", func(*localmodels.Config) {}, "missing", "model", catalogobservation.ErrBindingInvalid},
		{"absent-model", func(*localmodels.Config) {}, "selected", "missing", catalogobservation.ErrBindingInvalid},
		{"duplicate-runtime", func(c *localmodels.Config) { c.Runtimes = append(c.Runtimes, c.Runtimes[0]) }, "selected", "model", catalogobservation.ErrBindingInvalid},
		{"missing-catalog", func(c *localmodels.Config) { c.EnginesConfig = "" }, "selected", "model", engineobservation.ErrCatalogRequired},
		{"unsupported-engine", func(c *localmodels.Config) {
			r := c.Runtimes[0].Models["model"]
			r.Engine.ID = "unknown"
			c.Runtimes[0].Models["model"] = r
		}, "selected", "model", catalogobservation.ErrEngineUnsupported},
		{"wrong-kind", func(c *localmodels.Config) {
			r := c.Runtimes[0].Models["model"]
			r.Engine.Kind = "vendor"
			c.Runtimes[0].Models["model"] = r
		}, "selected", "model", catalogobservation.ErrEngineUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadedConfig(t, "mlx")
			tc.edit(&cfg)
			calls := 0
			b, err := f.ForBinding(cfg, tc.runtime, vendorplugin.ModelID(tc.model), t.TempDir(), engineobservation.WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { calls++; return nil, nil }))
			if !errors.Is(err, tc.cause) || b.Engine != nil || len(b.Adapters) != 0 || calls != 0 {
				t.Fatalf("invalid binding %#v %v calls=%d", b, err, calls)
			}
		})
	}
	cfg := loadedConfig(t, "mlx")
	row := cfg.Runtimes[0].Models["model"]
	row.Engine = plugin.Ref{}
	cfg.Runtimes = append(cfg.Runtimes, localmodels.RuntimeEntry{ID: "pure", System: "pi", Models: map[vendorplugin.ModelID]localmodels.ModelEntry{"model": row}}, localmodels.RuntimeEntry{ID: "unrelated", Engine: plugin.Ref{ID: "unknown", Kind: inferenceengine.Kind}})
	cfg.EnginesConfig = ""
	b, err := f.ForBinding(cfg, "pure", "model", "")
	if err != nil || b.Engine != nil || len(b.Adapters) != 0 {
		t.Fatalf("pointer-only sibling %#v %v", b, err)
	}
}

type declaredEngine struct {
	ref  plugin.Ref
	kind inferenceengine.EngineKind
}

func (e declaredEngine) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: e.ref.ID, Kind: e.ref.Kind}
}
func (e declaredEngine) ObservationKind() inferenceengine.EngineKind { return e.kind }

// Generic graph validation and concrete kind declarations remain authority;
// a known ID with a bad kind is not upgraded by its name.
func TestFactoryConcreteRegistrationRefusals(t *testing.T) {
	for _, engines := range [][]inferenceengine.ObservationPlugin{
		{nil}, {mlx.New(), mlx.New()},
		{declaredEngine{plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}, "unknown"}},
		{declaredEngine{plugin.Ref{ID: "mlx", Kind: "vendor"}, inferenceengine.EngineKindNativeTransformer}},
	} {
		if _, err := catalogobservation.NewFactory(engines...); err == nil {
			t.Fatal("invalid concrete declaration accepted")
		}
	}
}

type statusReader struct{ calls int }

func (r *statusReader) Status(_ context.Context, q localruntime.StatusQuery) (localruntime.Status, error) {
	r.calls++
	return localruntime.Status{Runtime: q.Runtime, Model: q.Model, BrokerState: "serving", BrokerSource: localruntime.SourceAttested}, nil
}

// Public BuildLaunch propagates missing/mismatched catalog evidence before Pi
// preflight and returns no plan. Attested and pointer-only controls build plans.
// This proves composition boundaries, not actual model inference.
func TestFactoryBuildLaunchRefusalAndPointerOnlyControl(t *testing.T) {
	for _, kind := range []string{"mlx", "llama-cpp", "pointer-only", "attested", "foreign-catalog"} {
		t.Run(kind, func(t *testing.T) {
			cfg := loadedConfig(t, "mlx")
			if kind == "llama-cpp" {
				cfg = loadedConfig(t, kind)
			}
			if kind == "pointer-only" {
				cfg.Runtimes[0].Engine = plugin.Ref{}
				row := cfg.Runtimes[0].Models["model"]
				row.Engine = plugin.Ref{}
				cfg.Runtimes[0].Models["model"] = row
				cfg.InferenceEngines = nil
				cfg.EnginesConfig = ""
			}
			calls := 0
			cache, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			b, err := factory(t).ForBinding(cfg, "selected", "model", cache, engineobservation.WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) {
				calls++
				if kind == "attested" {
					return factoryStatus(t, cfg.EnginesConfig, cache), nil
				}
				if kind == "foreign-catalog" {
					return factoryStatus(t, filepath.Join(t.TempDir(), "engines.toml"), cache), nil
				}
				return []byte(`{"readings":{}}`), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			reader := &statusReader{}
			systems := agentic.NewRegistry()
			if err := systems.Register(pi.New(reader)); err != nil {
				t.Fatal(err)
			}
			registry, err := vendorplugin.NewRegistryWithEngineObservationAdapters(systems, b.Adapters...)
			if err != nil {
				t.Fatal(err)
			}
			if b.Engine != nil {
				if err := registry.RegisterPlugin(b.Engine); err != nil {
					t.Fatal(err)
				}
			}
			if err := registry.Register(localmodels.New(cfg, localmodels.WithStatusReader(reader))); err != nil {
				t.Fatal(err)
			}
			if err := registry.DeclareRuntime(vendorplugin.RuntimeDeclaration{ID: "selected", System: "pi", Vendor: localmodels.VendorID, Engine: cfg.Runtimes[0].Engine, Broker: vendorplugin.BrokerProvenance{Checked: []string{"runtimes.toml"}, Found: "fixture binding"}}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := execfixture.WriteFile(filepath.Join(dir, "pi"), []byte("#!/bin/sh\nexit 99\n"), 0755); err != nil {
				t.Fatal(err)
			}
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, vendorplugin.SpawnRequest{Runtime: "selected", Model: "model", Prompt: []byte("fixture"), WorkDir: dir, Env: []string{"PATH=" + dir}}, agentic.LaunchModeExec)
			if kind == "pointer-only" || kind == "attested" {
				wantCalls := 1
				if kind == "pointer-only" {
					wantCalls = 0
				}
				if err != nil || plan.Binary == "" || reader.calls != 1 || calls != wantCalls {
					t.Fatalf("pointer control plan=%#v err=%v preflight=%d observe=%d", plan, err, reader.calls, calls)
				}
			} else {
				cause := engineobservation.ErrCatalogProvenanceUnavailable
				if kind == "foreign-catalog" {
					cause = engineobservation.ErrCatalogIdentityMismatch
				}
				if !errors.Is(err, cause) || !errors.Is(err, inferenceengine.ErrObservationRead) || plan.Binary != "" || reader.calls != 0 || calls != 1 {
					t.Fatalf("catalog plan=%#v err=%v preflight=%d observe=%d", plan, err, reader.calls, calls)
				}
			}
		})
	}
}

func factoryValueRow(fact inferenceengine.Fact, value string) map[string]any {
	return map[string]any{"fact": string(fact), "outcome": string(inferenceengine.OutcomeObservedValue), "value": value, "source": "engines.toml/v1"}
}

func factoryAbsentRow(fact inferenceengine.Fact) map[string]any {
	return map[string]any{"fact": string(fact), "outcome": string(inferenceengine.OutcomeObservedAbsent), "source": "engines.toml/v1"}
}

func factoryNotObservedRow(fact inferenceengine.Fact, cause inferenceengine.NotObservedCause, reason string) map[string]any {
	return map[string]any{
		"fact": string(fact), "outcome": string(inferenceengine.OutcomeNotObserved),
		"cause": string(cause), "reason": reason, "source": "unavailable",
	}
}

func factoryHealthyRows() []map[string]any {
	values := map[inferenceengine.Fact]string{
		inferenceengine.FactContextArgv:              `["--max-kv-size","8192"]`,
		inferenceengine.FactPrefillArgv:              `["--prefill-step-size","512"]`,
		inferenceengine.FactReasoningStreamField:     `delta.reasoning_content`,
		inferenceengine.FactHealth:                   `{"endpoint":"/health","healthy":true}`,
		inferenceengine.FactReadiness:                `{"weights_resident":true}`,
		inferenceengine.FactWeightArtifact:           `{"format":"safetensors","model_path":"/models/weights.safetensors","config_path":"/models/config.json"}`,
		inferenceengine.FactMemoryAccounting:         `{"method":"resident-bytes","bytes":4096,"includes_mapped_weights":true}`,
		inferenceengine.FactSpeculativeDecoding:      `{"capable":true,"active":false}`,
		inferenceengine.FactLoadState:                `{"state":"loaded","weights_resident":true}`,
		inferenceengine.FactUnloadState:              `{"state":"unloaded","weights_resident":false}`,
		inferenceengine.FactInferenceBusy:            `{"busy":false}`,
		inferenceengine.FactMemoryPressureSequence:   `{"pressure":"normal","consulted":["load-state","unload-state","inference-busy"],"action":"none"}`,
		inferenceengine.FactLocalExecutable:          `/opt/engines/bin/mlx-server`,
		inferenceengine.FactLocalArgv:                `["--port","8080"]`,
		inferenceengine.FactStressPolicy:             `{"configured":false}`,
		inferenceengine.FactRestartSupervisionPolicy: `{"configured":false}`,
	}
	rows := make([]map[string]any, 0, len(inferenceengine.MeasuredFacts()))
	for _, definition := range inferenceengine.MeasuredFacts() {
		if definition.Fact == inferenceengine.FactSSHForwarding {
			rows = append(rows, factoryAbsentRow(definition.Fact))
			continue
		}
		value, ok := values[definition.Fact]
		if !ok {
			panic("factoryHealthyRows lacks " + definition.Fact)
		}
		rows = append(rows, factoryValueRow(definition.Fact, value))
	}
	return rows
}

func factoryStatus(t *testing.T, catalog, cache string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"provenance": map[string]string{"contract_version": "status-provenance/v1", "engines_config": catalog, "cache_root": cache, "engines_config_sha256": strings.Repeat("a", 64)},
		"readings":   map[string]any{"contract_version": inferenceengine.ContractVersionV3, "facts": factoryHealthyRows()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The effective model override selects the concrete kind; unrelated unsupported
// runtime rows do not poison a supported selected binding.
func TestFactoryModelOverrideAndUnrelatedEngine(t *testing.T) {
	cfg := loadedConfig(t, "mlx")
	cfg.Runtimes[0].Engine = plugin.Ref{ID: "unknown-runtime-engine", Kind: inferenceengine.Kind}
	cfg.Runtimes = append(cfg.Runtimes, localmodels.RuntimeEntry{ID: "other", Engine: plugin.Ref{ID: "unsupported", Kind: inferenceengine.Kind}})
	b, err := factory(t).ForBinding(cfg, "selected", "model", t.TempDir())
	if err != nil || b.Engine.PluginDeclaration().Ref() != (plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}) || b.Adapters[0].EngineObservationAdapterDeclaration().EngineKind != inferenceengine.EngineKindNativeTransformer {
		t.Fatalf("model override %#v %v", b, err)
	}
	// Runtime inheritance remains available for manually assembled configurations.
	row := cfg.Runtimes[0].Models["model"]
	row.Engine = plugin.Ref{}
	cfg.Runtimes[0].Models["model"] = row
	cfg.Runtimes[0].Engine = plugin.Ref{ID: "llama-cpp", Kind: inferenceengine.Kind}
	b, err = factory(t).ForBinding(cfg, "selected", "model", t.TempDir())
	if err != nil || b.Engine.PluginDeclaration().Ref() != cfg.Runtimes[0].Engine || b.Adapters[0].EngineObservationAdapterDeclaration().EngineKind != inferenceengine.EngineKindGGUFServer {
		t.Fatalf("runtime inheritance %#v %v", b, err)
	}
}

type mutableEngine struct{ kind inferenceengine.EngineKind }

func (e *mutableEngine) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: "custom-engine", Kind: inferenceengine.Kind, Dependencies: []plugin.Ref{}}
}
func (e *mutableEngine) ObservationKind() inferenceengine.EngineKind { return e.kind }

// Caller-owned concrete state cannot change a factory's registered kind after
// construction. A custom ID works without any private ID-to-kind dispatch.
func TestFactoryFreezesConcreteKind(t *testing.T) {
	engine := &mutableEngine{kind: inferenceengine.EngineKindGGUFServer}
	f, err := catalogobservation.NewFactory(engine)
	if err != nil {
		t.Fatal(err)
	}
	engine.kind = inferenceengine.EngineKindNativeTransformer
	cfg := loadedConfig(t, "mlx")
	row := cfg.Runtimes[0].Models["model"]
	row.Engine = engine.PluginDeclaration().Ref()
	cfg.Runtimes[0].Models["model"] = row
	b, err := f.ForBinding(cfg, "selected", "model", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b.Adapters[0].EngineObservationAdapterDeclaration().EngineKind != inferenceengine.EngineKindGGUFServer {
		t.Fatal("factory kind followed caller mutation")
	}
}
