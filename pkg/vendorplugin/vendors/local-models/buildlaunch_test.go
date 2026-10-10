package localmodels

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine/engines/mlx"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file proves the SEAM between this vendor and the REAL pi system
// through the real vendorplugin.BuildLaunch entry point — not each package
// in isolation (pi's own suite already proves its Preflight table; this
// package's own suite already proves Spawn/Availability). It imports
// pkg/agentic/systems/pi directly (a normal, non-blank import, test-only):
// that package's init() registers "pi" into agentic.Default, which is
// harmless here because THIS package's test binary carries no
// "every compiled-in system needs its own smoke golden" guard — that guard
// lives only in internal/regress, and pi's own pinned-parity golden is
// exactly the open item (adversarial plan case 17) this vendor's Story does
// not resolve.

type countingStatusReader struct {
	status localruntime.Status
	err    error
	hang   bool
	calls  int
}

type countingObservationAdapter struct {
	engine plugin.Ref
	calls  int
}

func (a *countingObservationAdapter) EngineObservationAdapterDeclaration() vendorplugin.EngineObservationAdapterDeclaration {
	return vendorplugin.EngineObservationAdapterDeclaration{
		Contract:       vendorplugin.EngineObservationAdapterContract,
		SchemaVersion:  vendorplugin.EngineObservationAdapterSchemaVersion,
		Engine:         a.engine,
		EngineKind:     inferenceengine.EngineKindNativeTransformer,
		EngineContract: inferenceengine.ContractVersion,
	}
}

func (a *countingObservationAdapter) ObserveEngine(context.Context, vendorplugin.EngineObservationQuery) (vendorplugin.EngineObservation, error) {
	a.calls++
	return vendorplugin.EngineObservation{}, errors.New("observation must not run for a request rejected by the system's pure launch gate")
}

func (r *countingStatusReader) Status(ctx context.Context, _ localruntime.StatusQuery) (localruntime.Status, error) {
	r.calls++
	if r.hang {
		<-ctx.Done()
		return localruntime.Status{}, ctx.Err()
	}
	if r.err != nil {
		return localruntime.Status{}, r.err
	}
	return r.status, nil
}

// isolatedLocalQwenRegistry builds a registry carrying ONLY the real pi
// system (with the given fake StatusReader) and this vendor (with the given
// config), declaring local-qwen.
func isolatedLocalQwenRegistry(t *testing.T, reader localruntime.StatusReader, cfg Config, configuredPlugins ...plugin.Plugin) *vendorplugin.Registry {
	return isolatedLocalQwenRegistryWithAdapters(t, reader, cfg, nil, configuredPlugins...)
}

func isolatedLocalQwenRegistryWithAdapters(t *testing.T, reader localruntime.StatusReader, cfg Config, adapters []vendorplugin.EngineObservationAdapter, configuredPlugins ...plugin.Plugin) *vendorplugin.Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(pi.New(reader)); err != nil {
		t.Fatalf("registering pi: %v", err)
	}
	registry, err := vendorplugin.NewRegistryWithEngineObservationAdapters(systems, adapters...)
	if err != nil {
		t.Fatalf("constructing vendor registry: %v", err)
	}
	if len(configuredPlugins) == 0 {
		configuredPlugins = cfg.EnginePlugins()
	}
	for _, engine := range configuredPlugins {
		if err := registry.RegisterPlugin(engine); err != nil {
			t.Fatalf("registering configured engine: %v", err)
		}
	}
	if err := registry.Register(New(cfg)); err != nil {
		t.Fatalf("registering local-models: %v", err)
	}
	decl := vendorplugin.RuntimeDeclaration{
		ID:     "local-qwen",
		System: "pi",
		Vendor: VendorID,
		Broker: vendorplugin.BrokerProvenance{Checked: []string{"test"}, Found: "test"},
		Engine: cfg.Runtimes[0].Engine,
	}
	if err := registry.DeclareRuntime(decl); err != nil {
		t.Fatalf("declaring local-qwen: %v", err)
	}
	return registry
}

func fakePiOnPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := execfixture.WriteFile(filepath.Join(dir, "pi"), []byte("#!/bin/sh\n: > \"$0.started\"\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing fake pi: %v", err)
	}
	return dir
}

func localQwenSpawnRequest(t *testing.T) vendorplugin.SpawnRequest {
	return vendorplugin.SpawnRequest{
		Runtime: "local-qwen",
		Model:   "qwen-3.8-27b-mlx-8bit",
		Prompt:  []byte("inspect the repository"),
		Env:     []string{"PATH=" + fakePiOnPath(t)},
	}
}

func configWithoutEngine() Config {
	cfg := validConfig()
	cfg.InferenceEngines = nil
	cfg.Runtimes[0].Engine = plugin.Ref{}
	for id, model := range cfg.Runtimes[0].Models {
		model.Engine = plugin.Ref{}
		cfg.Runtimes[0].Models[id] = model
	}
	return cfg
}

// TestBuildLaunchAdmitsLocalQwenThroughTheRealPiPreflight is the end-to-end
// seam proof: a live, attested broker admits, and the resulting Plan's
// argv/env carry what this vendor's real Spawn and pi's real Argv/ChildEnv
// build together.
// TestBuildLaunchCarriesTheCallerDeadlineIntoThePiPlan proves the native Pi
// plan does not claim a timeout that Pi itself cannot enforce. The caller
// retains the external hard fence.
func TestBuildLaunchCarriesTheCallerDeadlineIntoThePiPlan(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())
	req := localQwenSpawnRequest(t)
	req.Deadline = 6 * time.Hour

	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	for _, argument := range plan.Argv {
		if argument == "--deadline" || argument == "6h0m0s" {
			t.Fatalf("native Pi plan carries a timeout option/value it cannot enforce: %v", plan.Argv)
		}
	}
}

func TestBuildLaunchAdmitsLocalQwenThroughTheRealPiPreflight(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())
	req := localQwenSpawnRequest(t)

	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("StatusReader called %d times, want exactly 1", reader.calls)
	}
	if filepath.Base(plan.Binary) != "pi" {
		t.Fatalf("plan.Binary = %q, want native pi", plan.Binary)
	}
	if _, err := os.Stat(plan.Binary + ".started"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("BuildLaunch started native Pi while constructing a plan: marker stat error = %v", err)
	}
	if got := launchArgValue(plan.Argv, "--model"); got != "qwen-local/qwen-local" {
		t.Fatalf("plan.Argv --model = %q, want the configured Pi provider/model identity", got)
	}
	if got := launchArgValue(plan.Argv, "--tools"); got != "read,bash,edit,write" {
		t.Fatalf("plan.Argv --tools = %q, want the closed unattended allowlist", got)
	}
	for _, flag := range []string{"--no-approve", "--no-extensions", "--no-session", "--print"} {
		if !launchHasArg(plan.Argv, flag) {
			t.Errorf("plan.Argv %v omits required Pi flag %s", plan.Argv, flag)
		}
	}
	if launchHasArg(plan.Argv, "--") || plan.Argv[len(plan.Argv)-1] != "inspect the repository" {
		t.Fatalf("plan.Argv = %v, want the complete prompt as the final Pi message operand without a -- delimiter", plan.Argv)
	}
	foundEnv := false
	for _, entry := range plan.Env {
		if entry == "CURATOR_ENGINES_PROJECT_DIR=/Users/op/skill-agents-management" {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("plan.Env = %v; missing CURATOR_ENGINES_PROJECT_DIR contributed by this vendor's Spawn", plan.Env)
	}
	if plan.Provenance != (agentic.LaunchProvenance{}) {
		t.Fatalf("preflight-only fixture invented engine provenance: %#v", plan.Provenance)
	}
}

func TestBuildLaunchKeepsTheNativePiUnattendedPolicy(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	tests := []struct {
		name  string
		check func() bool
	}{
		{name: "no_approve", check: func() bool { return launchHasArg(plan.Argv, "--no-approve") }},
		{name: "no_extensions", check: func() bool { return launchHasArg(plan.Argv, "--no-extensions") }},
		{name: "tools_allowlist", check: func() bool { return launchArgValue(plan.Argv, "--tools") == "read,bash,edit,write" }},
		{name: "no_session", check: func() bool { return launchHasArg(plan.Argv, "--no-session") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.check() {
				t.Fatalf("BuildLaunch Argv %v dropped unattended policy %s", plan.Argv, test.name)
			}
		})
	}
}

func TestBuildLaunchUsesTheConfiguredPiProviderModelIdentity(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if got := launchArgValue(plan.Argv, "--model"); got != "qwen-local/qwen-local" {
		t.Fatalf("BuildLaunch --model = %q, want Pi catalog identity qwen-local/qwen-local", got)
	}
	if reader.calls != 1 {
		t.Fatalf("StatusReader calls = %d, want one preflight before returning the plan", reader.calls)
	}
}

func TestBuildLaunchRefusesAbsentEngineBeforePiStarts(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())
	binDir := fakePiOnPath(t)
	req := localQwenSpawnRequest(t)
	req.Env = []string{"PATH=" + binDir}

	_, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	if !errors.Is(err, pi.ErrPreflightRefused) {
		t.Fatalf("BuildLaunch absent/determined = %v, want ErrPreflightRefused", err)
	}
	if reader.calls != 1 {
		t.Fatalf("StatusReader calls = %d, want exactly one", reader.calls)
	}
	if _, err := os.Stat(filepath.Join(binDir, "pi.started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent-engine refusal started Pi: marker stat error = %v", err)
	}
}

func TestBuildLaunchIsInvariantAcrossCacheBudgets(t *testing.T) {
	request := localQwenSpawnRequest(t)
	var baseline agentic.Plan
	for i, cacheBudget := range []*int64{nil, cacheBudgetPtr(1), cacheBudgetPtr(12_884_901_888)} {
		cfg := configWithoutEngine()
		entry := cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"]
		entry.CacheBudgetBytes = cacheBudget
		cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"] = entry

		reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
		registry := isolatedLocalQwenRegistry(t, reader, cfg)
		plan, err := vendorplugin.BuildLaunch(context.Background(), registry, request, agentic.LaunchModeExec)
		if err != nil {
			t.Fatalf("BuildLaunch(cache budget %v): %v", cacheBudget, err)
		}
		if reader.calls != 1 {
			t.Fatalf("StatusReader calls = %d, want exactly 1 regardless of cache metadata", reader.calls)
		}
		if i == 0 {
			baseline = plan
			continue
		}
		if !reflect.DeepEqual(plan, baseline) {
			t.Fatalf("cache metadata changed runtime plan\nbaseline: %#v\nmutated:  %#v", baseline, plan)
		}
	}
}

func TestBuildLaunchRefusesInvalidPiProviderIdentityBeforeObservation(t *testing.T) {
	cfg := validConfig()
	entry := cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"]
	entry.Pointer.PiModelIdentity = "local-models//qwen-3.8-27b-mlx-8bit"
	cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"] = entry
	adapter := &countingObservationAdapter{engine: cfg.Runtimes[0].Engine}
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistryWithAdapters(t, reader, cfg, []vendorplugin.EngineObservationAdapter{adapter})

	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	if !errors.Is(err, pi.ErrSystemModelIdentityInvalid) {
		t.Fatalf("BuildLaunch invalid Pi identity = %v, want ErrSystemModelIdentityInvalid", err)
	}
	if plan.Binary != "" || len(plan.Argv) != 0 || adapter.calls != 0 || reader.calls != 0 {
		t.Fatalf("plan=%+v adapter calls=%d status calls=%d; invalid identity must refuse before observation and preflight", plan, adapter.calls, reader.calls)
	}
}

// TestBuildLaunchRefusesInvalidPiPromptBeforeObservationOrPreflight drives the
// real vendorplugin.BuildLaunch -> pi.System production path. A prompt gate
// left only in agentic.BuildPlan is too late: the adapter and StatusReader are
// observable effects even though neither starts a process.
func TestBuildLaunchRefusesInvalidPiPromptBeforeObservationOrPreflight(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*vendorplugin.SpawnRequest)
	}{
		{"missing", func(req *vendorplugin.SpawnRequest) { req.Prompt = nil }},
		{"unreadable", func(req *vendorplugin.SpawnRequest) {
			req.Prompt = nil
			req.PromptPath = filepath.Join(t.TempDir(), "missing")
		}},
		{"invalid UTF-8", func(req *vendorplugin.SpawnRequest) { req.Prompt = []byte{0xff} }},
		{"NUL", func(req *vendorplugin.SpawnRequest) { req.Prompt = []byte("a\x00b") }},
		{"leading long option", func(req *vendorplugin.SpawnRequest) { req.Prompt = []byte("--list-models") }},
		{"leading short option", func(req *vendorplugin.SpawnRequest) { req.Prompt = []byte("-inspect") }},
		{"leading at-file reference", func(req *vendorplugin.SpawnRequest) { req.Prompt = []byte("@repo") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, withEngine := range []bool{false, true} {
				name := "without-engine"
				cfg := configWithoutEngine()
				var adapters []vendorplugin.EngineObservationAdapter
				var adapter *countingObservationAdapter
				if withEngine {
					name = "with-engine"
					cfg = validConfig()
					adapter = &countingObservationAdapter{engine: cfg.Runtimes[0].Engine}
					adapters = []vendorplugin.EngineObservationAdapter{adapter}
				}
				t.Run(name, func(t *testing.T) {
					reader := &countingStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
					registry := isolatedLocalQwenRegistryWithAdapters(t, reader, cfg, adapters)
					req := localQwenSpawnRequest(t)
					test.mutate(&req)
					plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
					if !errors.Is(err, pi.ErrTurnPromptInvalid) {
						t.Fatalf("BuildLaunch error=%v, want ErrTurnPromptInvalid", err)
					}
					if plan.Binary != "" || len(plan.Argv) != 0 || reader.calls != 0 {
						t.Fatalf("plan=%+v preflight calls=%d, want no plan or preflight", plan, reader.calls)
					}
					if adapter != nil && adapter.calls != 0 {
						t.Fatalf("adapter calls=%d, want zero", adapter.calls)
					}
				})
			}
		})
	}
}

// ConsumerProvenance and Registry.ValidateLaunchProvenance are the production
// publication/read gates. This probe starts with the real BuildLaunch
// projection, crosses JSON persistence, removes both refs, and requires the
// independent engine_binding fact to prevent absent evidence from passing.
func TestBuildLaunchLocalQwenPersistedProvenanceRefusesBothEngineRefsRemoved(t *testing.T) {
	registry, projection := buildLocalQwenConsumerProvenance(t)
	persisted := mutatePersistedProvenance(t, projection, func(record map[string]any) {
		delete(record, "configured_engine")
		delete(record, "resolved_engine")
	})
	if err := registry.ValidateLaunchProvenance(persisted); !errors.Is(err, agentic.ErrLaunchProvenanceMismatch) {
		t.Fatalf("ValidateLaunchProvenance(both engine refs removed) = %v, want ErrLaunchProvenanceMismatch", err)
	}
}

// The persistence caller controls engine_binding and both refs, so the
// discriminator alone is not evidence. Preserve the real local-qwen identity
// axes while downgrading the binding and deleting the pair; Validate must
// recognize that this cannot be a genuine legacy/no-engine record.
func TestBuildLaunchLocalQwenPersistedProvenanceRefusesBindingDowngradeWithBothRefsRemoved(t *testing.T) {
	registry, projection := buildLocalQwenConsumerProvenance(t)
	persisted := mutatePersistedProvenance(t, projection, func(record map[string]any) {
		record["engine_binding"] = string(agentic.EngineBindingNoneV1)
		delete(record, "configured_engine")
		delete(record, "resolved_engine")
	})
	if err := registry.ValidateLaunchProvenance(persisted); !errors.Is(err, agentic.ErrLaunchProvenanceMismatch) {
		t.Fatalf("ValidateLaunchProvenance(binding downgrade plus refs removed) = %v, want ErrLaunchProvenanceMismatch", err)
	}
}

// Equal refs are not corroborating evidence when both were forged. Drive the
// same persisted consumer surface and narrow each graph identity rule in turn.
func TestBuildLaunchLocalQwenPersistedProvenanceRefusesEqualForgedEngineRefs(t *testing.T) {
	for _, test := range []struct {
		name string
		id   string
		kind string
		want error
	}{
		{name: "normalized correct-kind unconfigured identity", id: "attacker-engine", kind: "inference-engine", want: agentic.ErrLaunchProvenanceMismatch},
		{name: "wrong kind", id: "mlx", kind: "vendor", want: agentic.ErrLaunchProvenanceInvalid},
		{name: "unnormalized id", id: "MLX", kind: "inference-engine", want: agentic.ErrLaunchProvenanceInvalid},
		{name: "unnormalized kind", id: "mlx", kind: "Inference-Engine", want: agentic.ErrLaunchProvenanceInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, projection := buildLocalQwenConsumerProvenance(t)
			persisted := mutatePersistedProvenance(t, projection, func(record map[string]any) {
				for _, field := range []string{"configured_engine", "resolved_engine"} {
					ref := record[field].(map[string]any)
					ref["id"] = test.id
					ref["kind"] = test.kind
				}
			})
			if err := registry.ValidateLaunchProvenance(persisted); !errors.Is(err, test.want) {
				t.Fatalf("ValidateLaunchProvenance(equal forged refs) = %v, want %v", err, test.want)
			}
		})
	}
}

func buildLocalQwenConsumerProvenance(t *testing.T) (*vendorplugin.Registry, agentic.LaunchProvenanceV1) {
	t.Helper()
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
	registry := isolatedLocalQwenRegistry(t, reader, validConfig(), mlx.New())
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch(local-qwen): %v", err)
	}
	projection, err := plan.ConsumerProvenance()
	if err != nil {
		t.Fatalf("ConsumerProvenance(BuildLaunch(local-qwen)): %v", err)
	}
	return registry, projection
}

func mutatePersistedProvenance(t *testing.T, projection agentic.LaunchProvenanceV1, mutate func(map[string]any)) agentic.LaunchProvenanceV1 {
	t.Helper()
	body, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("Marshal provenance: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatalf("Unmarshal persisted provenance: %v", err)
	}
	mutate(record)
	body, err = json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal mutated provenance: %v", err)
	}
	var persisted agentic.LaunchProvenanceV1
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatalf("Unmarshal consumer provenance: %v", err)
	}
	return persisted
}

// TestBuildLaunchLegacyConfigWithoutEngineRequirementPreservesParity drives
// the exact v0.4.0 TOML shape through the production parser and BuildLaunch.
// Absence is a legitimate no-engine declaration: all pre-extension runtime,
// model and observable launch fields must survive without invented provenance.
func TestBuildLaunchLegacyConfigWithoutPiProviderIdentityRefusesExec(t *testing.T) {
	result := newConfigLoader(func() ([]byte, bool, error) {
		return []byte(legacyTOML), false, nil
	}).load()
	if result.Err != nil || result.Absent {
		t.Fatalf("v0.4.0 config without an engine requirement became malformed: %+v", result)
	}
	if len(result.Config.InferenceEngines) != 0 || len(result.Config.Runtimes) != 1 {
		t.Fatalf("legacy declarations changed: %+v", result.Config)
	}
	runtime := result.Config.Runtimes[0]
	model := runtime.Models["qwen-3.8-27b-mlx-8bit"]
	if runtime.Engine != (plugin.Ref{}) || model.Engine != (plugin.Ref{}) {
		t.Fatalf("legacy config invented engine refs: runtime=%#v model=%#v", runtime.Engine, model.Engine)
	}

	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, result.Config)
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	if !errors.Is(err, pi.ErrSystemModelIdentityMissing) {
		t.Fatalf("BuildLaunch(v0.4.0 config) = %v, want ErrSystemModelIdentityMissing", err)
	}
	if plan.Binary != "" || len(plan.Argv) != 0 || reader.calls != 0 {
		t.Fatalf("legacy plan=%+v StatusReader calls=%d; missing provider mapping must refuse before preflight or Pi", plan, reader.calls)
	}
}

// TestBuildLaunchRefusesLocalQwenWhenPreflightRefuses: a refusing Preflight
// stops BuildLaunch before agentic.BuildPlan.
func TestBuildLaunchRefusesLocalQwenWhenPreflightRefuses(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "draining", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())

	_, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	if !errors.Is(err, pi.ErrPreflightRefused) {
		t.Fatalf("BuildLaunch draining preflight = %v, want pi.ErrPreflightRefused", err)
	}
	if reader.calls != 1 {
		t.Fatalf("StatusReader called %d times, want exactly 1", reader.calls)
	}
}

// TestBuildLaunchConfiguredMLXRefusesBeforePiPreflight proves the independent
// production MLX observation seam. Until a read-only engine observation
// contract is available at trusted registry construction, configured
// local-qwen must refuse before Pi reads broker status; this is not evidence
// about Pi Preflight.
func TestBuildLaunchConfiguredMLXRefusesBeforePiPreflight(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
	registry := isolatedLocalQwenRegistry(t, reader, validConfig())

	_, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	if !errors.Is(err, vendorplugin.ErrEngineObservationAdapterMissing) || !errors.Is(err, inferenceengine.ErrEngineContractMissing) {
		t.Fatalf("BuildLaunch configured MLX = %v, want missing adapter and engine contract", err)
	}
	if reader.calls != 0 {
		t.Fatalf("StatusReader called %d times before MLX observation refusal, want 0", reader.calls)
	}
}

// TestBuildLaunchSkipsPreflightOnDryRun: dry-run mode must call the
// StatusReader ZERO times, driven with the real pi system.
func TestBuildLaunchSkipsPreflightOnDryRun(t *testing.T) {
	reader := &countingStatusReader{err: errors.New("must never be called on a dry run")}
	registry := isolatedLocalQwenRegistry(t, reader, validConfig())
	req := localQwenSpawnRequest(t)

	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch(dry-run): %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("StatusReader called %d times on a dry run, want 0", reader.calls)
	}
	if plan.System != "pi" {
		t.Fatalf("plan.System = %q, want pi", plan.System)
	}
	if filepath.Base(plan.Binary) != "pi" {
		t.Fatalf("dry-run binary = %q, want native pi", plan.Binary)
	}
	if _, err := os.Stat(plan.Binary + ".started"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("BuildLaunch(dry-run) started native Pi: marker stat error = %v", err)
	}
	if got := launchArgValue(plan.Argv, "--model"); got != "qwen-local/qwen-local" {
		t.Fatalf("dry-run --model = %q, want configured Pi provider/model identity", got)
	}
	if launchHasArg(plan.Argv, "--") || plan.Argv[len(plan.Argv)-1] != "<prompt>" {
		t.Fatalf("dry-run argv = %v, want native Pi flags followed by the prompt placeholder", plan.Argv)
	}
}

// TestBuildLaunchPreflightTimeoutRefusesLocalQwen proves the
// context.WithTimeout BuildLaunch applies is real for this seam: a
// StatusReader that never returns must not hang BuildLaunch forever.
func TestBuildLaunchPreflightTimeoutRefusesLocalQwen(t *testing.T) {
	reader := &countingStatusReader{hang: true}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := vendorplugin.BuildLaunch(ctx, registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("BuildLaunch hanging StatusReader = %v, want context.DeadlineExceeded", err)
	}
	if reader.calls != 1 {
		t.Fatalf("StatusReader called %d times, want exactly 1", reader.calls)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("BuildLaunch took %v to return", elapsed)
	}
}

// AGENTIC_PI_REAL_BINARY opts into a read-only real Pi CLI probe. It is
// intentionally explicit so ordinary package tests never consult a user's Pi
// catalog or run a model request.
func TestBuildLaunchRealPi0842AcceptsConfiguredProviderIdentity(t *testing.T) {
	binary := os.Getenv("AGENTIC_PI_REAL_BINARY")
	if binary == "" {
		t.Skip("set AGENTIC_PI_REAL_BINARY to opt into the pinned real-Pi catalog probe")
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		t.Fatalf("LookPath(%q): %v", binary, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, resolved, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("Pi --version: %v; output=%s", err, version)
	}
	if got := strings.TrimSpace(string(version)); got != "0.84.2" {
		t.Fatalf("Pi version = %q, want pinned 0.84.2", got)
	}

	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, configWithoutEngine())
	req := localQwenSpawnRequest(t)
	req.Env = []string{"PATH=" + filepath.Dir(resolved)}
	plan, err := vendorplugin.BuildLaunch(ctx, registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch with Pi 0.84.2: %v", err)
	}
	if got := launchArgValue(plan.Argv, "--model"); got != "qwen-local/qwen-local" {
		t.Fatalf("BuildLaunch --model = %q, want configured Pi identity", got)
	}

	// --list-models validates Pi's real provider/model catalog without sending
	// a prompt to, or starting, the local model engine.
	output, err := exec.CommandContext(ctx, resolved,
		"--offline", "--no-approve", "--no-extensions", "--no-session",
		"--tools", "read,bash,edit,write",
		"--model", "qwen-local/qwen-local",
		"--list-models", "qwen-local",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("Pi catalog query: %v; output=%s", err, output)
	}
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "qwen-local" && fields[1] == "qwen-local" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Pi 0.84.2 catalog does not list qwen-local/qwen-local: %s", output)
	}
}

func launchArgValue(argv []string, name string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == name {
			return argv[i+1]
		}
	}
	return ""
}

func launchHasArg(argv []string, name string) bool {
	for _, argument := range argv {
		if argument == name {
			return true
		}
	}
	return false
}
