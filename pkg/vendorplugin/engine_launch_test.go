package vendorplugin

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

var testEngineRef = plugin.Ref{ID: "test-engine", Kind: inferenceengine.Kind}

func engineRegistry(t *testing.T, runtimeEngine, modelEngine plugin.Ref) *Registry {
	t.Helper()
	systems := systemsWithPangolin(t)
	registry := NewRegistry(systems)
	if err := registry.RegisterPlugin(inferenceengine.NewConfigured("test-engine")); err != nil {
		t.Fatalf("RegisterPlugin(engine): %v", err)
	}
	if modelEngine != (plugin.Ref{}) && modelEngine.ID != testEngineRef.ID {
		if err := registry.RegisterPlugin(inferenceengine.NewConfigured(modelEngine.ID)); err != nil {
			t.Fatalf("RegisterPlugin(model engine): %v", err)
		}
	}
	vendor := newNarwhal()
	vendor.models[0].Publisher = "narwhal-labs"
	vendor.models[0].Family = "narwhal"
	vendor.models[0].Engine = modelEngine
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register(vendor): %v", err)
	}
	declaration := tuskDeclaration()
	declaration.Engine = runtimeEngine
	if err := registry.DeclareRuntime(declaration); err != nil {
		t.Fatalf("DeclareRuntime: %v", err)
	}
	return registry
}

// BuildLaunch is the production call site: it must resolve the declaration's
// requested engine through the same graph that registered systems and vendors.
func TestBuildLaunchCarriesRequestedAndResolvedEngineProvenance(t *testing.T) {
	registry := engineRegistry(t, testEngineRef, testEngineRef)
	req := narwhalRequest()
	req.Engine = testEngineRef

	plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if plan.Provenance.Runtime != string(req.Runtime) || plan.Provenance.Model != string(req.Model) {
		t.Fatalf("provenance runtime/model = %q/%q, want %q/%q", plan.Provenance.Runtime, plan.Provenance.Model, req.Runtime, req.Model)
	}
	if plan.Provenance.Publisher != "narwhal-labs" || plan.Provenance.Family != "narwhal" {
		t.Fatalf("publisher/family = %q/%q", plan.Provenance.Publisher, plan.Provenance.Family)
	}
	if plan.Provenance.RequestedEngine != testEngineRef || plan.Provenance.ResolvedEngine != testEngineRef {
		t.Fatalf("engine provenance = %#v/%#v, want %#v", plan.Provenance.RequestedEngine, plan.Provenance.ResolvedEngine, testEngineRef)
	}
}

func TestBuildLaunchRefusesEngineDeclarationAndExpectationMismatches(t *testing.T) {
	other := plugin.Ref{ID: "other-engine", Kind: inferenceengine.Kind}

	t.Run("runtime and model disagree", func(t *testing.T) {
		registry := engineRegistry(t, testEngineRef, other)
		_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
		if !errors.Is(err, ErrInferenceEngineMismatch) {
			t.Fatalf("BuildLaunch mismatch err = %v, want ErrInferenceEngineMismatch", err)
		}
	})

	t.Run("caller requests substitution", func(t *testing.T) {
		registry := engineRegistry(t, testEngineRef, testEngineRef)
		req := narwhalRequest()
		req.Engine = other
		_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
		if !errors.Is(err, ErrInferenceEngineMismatch) {
			t.Fatalf("BuildLaunch substitution err = %v, want ErrInferenceEngineMismatch", err)
		}
	})

	t.Run("one declaration silently drops the requirement", func(t *testing.T) {
		registry := engineRegistry(t, testEngineRef, plugin.Ref{})
		_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
		if !errors.Is(err, ErrInferenceEngineMismatch) {
			t.Fatalf("BuildLaunch dropped model engine err = %v, want ErrInferenceEngineMismatch", err)
		}
	})
}

func TestEngineGraphRefusalsPrecedeAPlan(t *testing.T) {
	t.Run("missing engine", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		vendor := newNarwhal()
		vendor.models[0].Engine = testEngineRef
		err := registry.Register(vendor)
		if !errors.Is(err, plugin.ErrMissingDependency) {
			t.Fatalf("Register(vendor requiring absent engine) = %v, want ErrMissingDependency", err)
		}
	})

	t.Run("wrong kind", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := registry.RegisterPlugin(testGraphPlugin{declaration: plugin.Declaration{ID: testEngineRef.ID, Kind: "sidecar"}}); err != nil {
			t.Fatalf("RegisterPlugin(wrong kind): %v", err)
		}
		vendor := newNarwhal()
		vendor.models[0].Engine = testEngineRef
		err := registry.Register(vendor)
		if !errors.Is(err, plugin.ErrUnsatisfiableDeclaration) {
			t.Fatalf("Register(vendor requiring wrong kind) = %v, want ErrUnsatisfiableDeclaration", err)
		}
	})

	t.Run("duplicate identity", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := registry.RegisterPlugin(inferenceengine.NewConfigured("test-engine")); err != nil {
			t.Fatalf("first RegisterPlugin: %v", err)
		}
		err := registry.RegisterPlugin(inferenceengine.NewConfigured("test-engine"))
		if !errors.Is(err, plugin.ErrDuplicatePlugin) {
			t.Fatalf("duplicate RegisterPlugin = %v, want ErrDuplicatePlugin", err)
		}
	})
}

func TestBuildLaunchWithoutEngineRequirementPreservesLegacyPlan(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	plan, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if plan.Provenance != (agentic.LaunchProvenance{}) {
		t.Fatalf("legacy launch invented engine provenance: %#v", plan.Provenance)
	}
}

type launchIdentityTuple struct {
	system    string
	vendor    string
	runtime   string
	profile   string
	publisher string
	family    string
	model     string
	engine    string
}

type identitySystem struct {
	*pangolinSystem
	preflightCalls int
}

func (s *identitySystem) Preflight(context.Context, agentic.LaunchRequest) (agentic.PreflightEvidence, error) {
	s.preflightCalls++
	return agentic.PreflightEvidence{}, nil
}

type identityVendor struct{ *narwhalVendor }

func (v *identityVendor) Spawn(sc SpawnContext) (agentic.LaunchRequest, error) {
	launch, err := v.narwhalVendor.Spawn(sc)
	launch.Profile = sc.Request.Profile
	return launch, err
}

type identityLaunchFixture struct {
	registry *Registry
	system   *identitySystem
	vendor   *identityVendor
	request  SpawnRequest
}

func newIdentityLaunchFixture(t *testing.T, ids launchIdentityTuple, runtimeEngine, modelEngine plugin.Ref, extraEngines ...plugin.Ref) identityLaunchFixture {
	t.Helper()
	systemDouble := newPangolinSystem()
	systemDouble.id = agentic.SystemID(ids.system)
	system := &identitySystem{pangolinSystem: systemDouble}
	systems := agentic.NewRegistry()
	if err := systems.Register(system); err != nil {
		t.Fatalf("Register(%s system): %v", ids.system, err)
	}

	registry := NewRegistry(systems)
	registered := make(map[plugin.Ref]bool)
	for _, ref := range append([]plugin.Ref{{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}}, extraEngines...) {
		if ref == (plugin.Ref{}) || registered[ref] {
			continue
		}
		registered[ref] = true
		if err := registry.RegisterPlugin(inferenceengine.NewConfigured(ref.ID)); err != nil {
			t.Fatalf("RegisterPlugin(%s): %v", ref.ID, err)
		}
	}

	vendorDouble := newNarwhal()
	vendorDouble.id = VendorID(ids.vendor)
	model := vendorDouble.models[0]
	model.ID = ModelID(ids.model)
	model.Publisher = ids.publisher
	model.Family = ids.family
	model.Engine = modelEngine
	model.Systems = []agentic.SystemID{agentic.SystemID(ids.system)}
	vendorDouble.models = []Model{model}
	vendor := &identityVendor{narwhalVendor: vendorDouble}
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register(%s vendor): %v", ids.vendor, err)
	}
	if err := registry.DeclareRuntime(RuntimeDeclaration{
		ID:     RuntimeID(ids.runtime),
		System: agentic.SystemID(ids.system),
		Vendor: VendorID(ids.vendor),
		Engine: runtimeEngine,
		Broker: BrokerProvenance{Checked: []string{"metamorphic registration"}, Found: "registered test vendor"},
	}); err != nil {
		t.Fatalf("DeclareRuntime(%s): %v", ids.runtime, err)
	}

	request := narwhalRequest()
	request.Runtime = RuntimeID(ids.runtime)
	request.Profile = ids.profile
	request.Model = ModelID(ids.model)
	request.Engine = runtimeEngine
	return identityLaunchFixture{registry: registry, system: system, vendor: vendor, request: request}
}

// BuildLaunch is deliberately the acceptance surface. The fixture changes all
// eight identity axes while leaving its behavior fixed; no helper beneath the
// production entry point is used as a substitute for the launch observation.
func TestBuildLaunchIsEquivariantUnderIdentityRenaming(t *testing.T) {
	var baselinePlan agentic.Plan
	var baselineGraph []string
	for index, ids := range launchIdentityCorpus() {
		t.Run(fmt.Sprintf("case-%02d", index), func(t *testing.T) {
			if index > 0 {
				requireInjectiveIdentityTuple(t, ids)
			}
			engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
			fixture := newIdentityLaunchFixture(t, ids, engine, engine)
			plan, err := BuildLaunch(context.Background(), fixture.registry, fixture.request, agentic.LaunchModeExec)
			if err != nil {
				t.Fatalf("BuildLaunch identity case %d (%+v): %v", index, ids, err)
			}
			wantProvenance := agentic.LaunchProvenance{
				Runtime: ids.runtime, Profile: ids.profile, Model: ids.model,
				Publisher: ids.publisher, Family: ids.family,
				RequestedEngine: engine, ResolvedEngine: engine,
			}
			if plan.Provenance != wantProvenance {
				t.Fatalf("identity case %d provenance = %#v, want %#v", index, plan.Provenance, wantProvenance)
			}
			if fixture.vendor.calls["Spawn"] != 1 || fixture.system.preflightCalls != 1 {
				t.Fatalf("identity case %d side effects: vendor Spawn=%d, system Preflight=%d, want 1/1",
					index, fixture.vendor.calls["Spawn"], fixture.system.preflightCalls)
			}

			canonicalPlan := canonicalIdentityPlan(plan, ids)
			graph := identityGraphSignature(t, fixture.registry, ids)
			if index == 0 {
				baselinePlan = canonicalPlan
				baselineGraph = graph
				return
			}
			if !reflect.DeepEqual(canonicalPlan, baselinePlan) {
				t.Fatalf("identity case %d changed non-identity launch output\n got: %#v\nwant: %#v", index, canonicalPlan, baselinePlan)
			}
			if !reflect.DeepEqual(graph, baselineGraph) {
				t.Fatalf("identity case %d changed graph topology: got %v, want %v", index, graph, baselineGraph)
			}
		})
	}
}

type refusalObservation struct {
	err       error
	plan      agentic.Plan
	spawn     int
	preflight int
}

func TestBuildLaunchRefusalClassesAreEquivariantUnderIdentityRenaming(t *testing.T) {
	mutations := []struct {
		name string
		want error
		run  func(*testing.T, launchIdentityTuple) refusalObservation
	}{
		{name: "missing-engine-plugin", want: plugin.ErrMissingDependency, run: missingEngineRefusal},
		{name: "engine-wrong-kind", want: plugin.ErrUnsatisfiableDeclaration, run: wrongKindEngineRefusal},
		{name: "duplicate-engine-identity", want: plugin.ErrDuplicatePlugin, run: duplicateEngineRefusal},
		{name: "cyclic-batch-registration", want: plugin.ErrDependencyCycle, run: cyclicEngineRefusal},
		{name: "partial-engine-reference", want: ErrInferenceEngineInvalid, run: partialEngineRefusal},
		{name: "unnormalized-engine-reference", want: ErrInferenceEngineInvalid, run: unnormalizedEngineRefusal},
		{name: "runtime-model-disagreement", want: ErrInferenceEngineMismatch, run: engineDisagreementRefusal},
		{name: "runtime-only-engine", want: ErrInferenceEngineMismatch, run: runtimeOnlyEngineRefusal},
		{name: "model-only-engine", want: ErrInferenceEngineMismatch, run: modelOnlyEngineRefusal},
		{name: "caller-substitution-replacement", want: ErrInferenceEngineMismatch, run: callerSubstitutionRefusal},
		{name: "caller-engine-for-no-engine-declarations", want: ErrInferenceEngineMismatch, run: callerEngineForLegacyRefusal},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			for index, ids := range launchIdentityCorpus() {
				t.Run(fmt.Sprintf("case-%02d", index), func(t *testing.T) {
					observation := mutation.run(t, ids)
					if !errors.Is(observation.err, mutation.want) {
						t.Fatalf("identity case %d (%+v): err=%v, want class %v", index, ids, observation.err, mutation.want)
					}
					if !reflect.DeepEqual(observation.plan, agentic.Plan{}) {
						t.Fatalf("identity case %d returned a plan on refusal: %#v", index, observation.plan)
					}
					if observation.spawn != 0 || observation.preflight != 0 {
						t.Fatalf("identity case %d reached launch side effects: vendor Spawn=%d system Preflight=%d",
							index, observation.spawn, observation.preflight)
					}
				})
			}
		})
	}
}

func launchIdentityCorpus() []launchIdentityTuple {
	tuples := []launchIdentityTuple{
		{system: "pi", vendor: "local-models", runtime: "local-qwen", profile: "local-qwen", publisher: "alibaba", family: "qwen", model: "qwen-3.8-27b-mlx-8bit", engine: "mlx"},
		{system: "sable-harness", vendor: "cedar-broker", runtime: "ember-runtime", profile: "frost-profile", publisher: "garnet-publisher", family: "heron-family", model: "indigo-model-v1", engine: "juniper-engine"},
		{system: "kestrel-harness", vendor: "lilac-broker", runtime: "marble-runtime", profile: "nectar-profile", publisher: "onyx-publisher", family: "pebble-family", model: "quartz-model-v2", engine: "raven-engine"},
	}
	rng := rand.New(rand.NewSource(260830))
	for caseIndex := 0; caseIndex < 8; caseIndex++ {
		value := func(axis string) string { return fmt.Sprintf("%s-%02d-%08x", axis, caseIndex, rng.Uint32()) }
		tuples = append(tuples, launchIdentityTuple{
			system: value("sys"), vendor: value("ven"), runtime: value("run"), profile: value("pro"),
			publisher: value("pub"), family: value("fam"), model: value("mod"), engine: value("eng"),
		})
	}
	return tuples
}

func requireInjectiveIdentityTuple(t *testing.T, ids launchIdentityTuple) {
	t.Helper()
	values := []string{ids.system, ids.vendor, ids.runtime, ids.profile, ids.publisher, ids.family, ids.model, ids.engine}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			t.Fatalf("identity rename is not injective: tuple %+v repeats %q", ids, value)
		}
		seen[value] = true
	}
}

func canonicalIdentityPlan(plan agentic.Plan, ids launchIdentityTuple) agentic.Plan {
	plan.System = "{system}"
	plan.Provenance = agentic.LaunchProvenance{}
	replacements := map[string]string{
		ids.system: "{system}", ids.vendor: "{vendor}", ids.runtime: "{runtime}", ids.profile: "{profile}",
		ids.publisher: "{publisher}", ids.family: "{family}", ids.model: "{model}", ids.engine: "{engine}",
	}
	for index, value := range plan.Argv {
		if replacement, ok := replacements[value]; ok {
			plan.Argv[index] = replacement
		}
	}
	for index, value := range plan.Env {
		if replacement, ok := replacements[value]; ok {
			plan.Env[index] = replacement
		}
	}
	return plan
}

func identityGraphSignature(t *testing.T, registry *Registry, ids launchIdentityTuple) []string {
	t.Helper()
	axes := map[plugin.ID]string{
		plugin.ID(ids.system): "system", plugin.ID(ids.vendor): "vendor", plugin.ID(ids.engine): "engine",
	}
	result := make([]string, 0, len(axes))
	for id, axis := range axes {
		declaration, ok := registry.Graph().Declaration(id)
		if !ok {
			t.Fatalf("graph omitted %s identity %q", axis, id)
		}
		dependencies := make([]string, 0, len(declaration.Dependencies))
		for _, dependency := range declaration.Dependencies {
			dependencyAxis, ok := axes[dependency.ID]
			if !ok {
				t.Fatalf("%s identity %q has unexpected dependency %#v", axis, id, dependency)
			}
			dependencies = append(dependencies, dependencyAxis+":"+string(dependency.Kind))
		}
		sort.Strings(dependencies)
		result = append(result, axis+":"+string(declaration.Kind)+":"+fmt.Sprint(dependencies))
	}
	sort.Strings(result)
	return result
}

func bareIdentityRegistration(t *testing.T, ids launchIdentityTuple) (*Registry, *identitySystem, *identityVendor) {
	t.Helper()
	systemDouble := newPangolinSystem()
	systemDouble.id = agentic.SystemID(ids.system)
	system := &identitySystem{pangolinSystem: systemDouble}
	systems := agentic.NewRegistry()
	if err := systems.Register(system); err != nil {
		t.Fatalf("Register(%s system): %v", ids.system, err)
	}
	vendorDouble := newNarwhal()
	vendorDouble.id = VendorID(ids.vendor)
	model := vendorDouble.models[0]
	model.ID = ModelID(ids.model)
	model.Publisher = ids.publisher
	model.Family = ids.family
	model.Systems = []agentic.SystemID{agentic.SystemID(ids.system)}
	vendorDouble.models = []Model{model}
	return NewRegistry(systems), system, &identityVendor{narwhalVendor: vendorDouble}
}

func registrationRefusal(err error, system *identitySystem, vendor *identityVendor) refusalObservation {
	return refusalObservation{err: err, spawn: vendor.calls["Spawn"], preflight: system.preflightCalls}
}

func buildRefusal(fixture identityLaunchFixture, mutate func(*SpawnRequest)) refusalObservation {
	request := fixture.request
	mutate(&request)
	plan, err := BuildLaunch(context.Background(), fixture.registry, request, agentic.LaunchModeExec)
	return refusalObservation{err: err, plan: plan, spawn: fixture.vendor.calls["Spawn"], preflight: fixture.system.preflightCalls}
}

func missingEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	registry, system, vendor := bareIdentityRegistration(t, ids)
	ref := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	vendor.models[0].Engine = ref
	err := registry.Register(vendor)
	if _, present := registry.Lookup(VendorID(ids.vendor)); present {
		t.Fatal("missing-engine refusal stored the vendor")
	}
	return registrationRefusal(err, system, vendor)
}

func wrongKindEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	registry, system, vendor := bareIdentityRegistration(t, ids)
	if err := registry.RegisterPlugin(testGraphPlugin{declaration: plugin.Declaration{ID: plugin.ID(ids.engine), Kind: "sidecar"}}); err != nil {
		t.Fatalf("register wrong-kind plugin: %v", err)
	}
	vendor.models[0].Engine = plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	err := registry.Register(vendor)
	if _, present := registry.Lookup(VendorID(ids.vendor)); present {
		t.Fatal("wrong-kind refusal stored the vendor")
	}
	return registrationRefusal(err, system, vendor)
}

func duplicateEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	registry, system, vendor := bareIdentityRegistration(t, ids)
	engine := inferenceengine.NewConfigured(plugin.ID(ids.engine))
	if err := registry.RegisterPlugin(engine); err != nil {
		t.Fatalf("register first engine: %v", err)
	}
	before := registry.Graph().Len()
	err := registry.RegisterPlugin(engine)
	if registry.Graph().Len() != before {
		t.Fatal("duplicate-engine refusal mutated the graph")
	}
	return registrationRefusal(err, system, vendor)
}

func cyclicEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	registry, system, vendor := bareIdentityRegistration(t, ids)
	aID := plugin.ID(ids.engine + "-cycle-a")
	bID := plugin.ID(ids.engine + "-cycle-b")
	a := testGraphPlugin{declaration: plugin.Declaration{ID: aID, Kind: inferenceengine.Kind, Dependencies: []plugin.Ref{{ID: bID, Kind: inferenceengine.Kind}}}}
	b := testGraphPlugin{declaration: plugin.Declaration{ID: bID, Kind: inferenceengine.Kind, Dependencies: []plugin.Ref{{ID: aID, Kind: inferenceengine.Kind}}}}
	before := registry.Graph().Len()
	err := registry.Graph().RegisterAll(a, b)
	if registry.Graph().Len() != before {
		t.Fatal("cyclic batch refusal mutated the graph")
	}
	return registrationRefusal(err, system, vendor)
}

func partialEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	registry, system, vendor := bareIdentityRegistration(t, ids)
	vendor.models[0].Engine = plugin.Ref{ID: plugin.ID(ids.engine)}
	return registrationRefusal(registry.Register(vendor), system, vendor)
}

func unnormalizedEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	registry, system, vendor := bareIdentityRegistration(t, ids)
	vendor.models[0].Engine = plugin.Ref{ID: plugin.ID(ids.engine + " "), Kind: inferenceengine.Kind}
	return registrationRefusal(registry.Register(vendor), system, vendor)
}

func engineDisagreementRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	replacement := plugin.Ref{ID: plugin.ID(ids.engine + "-replacement"), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, engine, replacement, replacement)
	return buildRefusal(fixture, func(request *SpawnRequest) { request.Engine = plugin.Ref{} })
}

func runtimeOnlyEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, engine, plugin.Ref{})
	return buildRefusal(fixture, func(request *SpawnRequest) { request.Engine = plugin.Ref{} })
}

func modelOnlyEngineRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, plugin.Ref{}, engine)
	return buildRefusal(fixture, func(request *SpawnRequest) { request.Engine = plugin.Ref{} })
}

func callerSubstitutionRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	replacement := plugin.Ref{ID: plugin.ID(ids.engine + "-replacement"), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, engine, engine, replacement)
	return buildRefusal(fixture, func(request *SpawnRequest) { request.Engine = replacement })
}

func callerEngineForLegacyRefusal(t *testing.T, ids launchIdentityTuple) refusalObservation {
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, plugin.Ref{}, plugin.Ref{})
	return buildRefusal(fixture, func(request *SpawnRequest) { request.Engine = engine })
}

type testGraphPlugin struct{ declaration plugin.Declaration }

func (p testGraphPlugin) PluginDeclaration() plugin.Declaration { return p.declaration }
