package vendorplugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// narwhalID names a vendor that exists nowhere else in this module: not in the
// registry, not in the frozen runtime table, not in the CLI, not in any
// non-test source. TestVendorDoubleExistsOnlyInTests proves that rather than
// asserting it.
//
// It is the Layer-2 descendant of pkg/agentic's pangolin, which proves the
// same property one layer down: registering a plugin the core has never heard
// of must drive every dispatch surface with no edit anywhere except the
// registration call.
const narwhalID VendorID = "narwhal"

// tuskID names the runtime declaration that pairs the vendor double with the
// system double. A runtime is a declared pair, so the seam proof needs one of
// those too — and it is a DECLARATION, which is the whole point: adding a
// runtime is not a code change.
const tuskID RuntimeID = "tusk"

// pangolinID is the agentic system the vendor double declares support for.
//
// The spelling is deliberately the same as pkg/agentic's own test double, and
// the implementation below is deliberately a separate, minimal one. It has to
// be: that double is unexported test-only code, and pkg/agentic's
// TestTestDoubleExistsOnlyInTests requires it to STAY test-only — exporting it
// through a helper package so this file could import it would fail that test
// and put a fake harness in a production package. What is shared is the id and
// the contract; what is not shared is a fake nobody would notice drifting,
// because this one implements the minimum BuildPlan needs and nothing else.
const pangolinID agentic.SystemID = "pangolin"

// pangolinSystem is the Layer-1 double: enough of an agentic system for
// agentic.BuildPlan to produce a plan, so a Layer-2 test observes a real
// two-layer launch rather than a mock of one.
type pangolinSystem struct {
	id   agentic.SystemID
	caps agentic.Capabilities
}

func newPangolinSystem() *pangolinSystem {
	return &pangolinSystem{
		id: pangolinID,
		caps: agentic.Capabilities{
			LaunchModes:         []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeManagedSession},
			EffortTransport:     agentic.EffortTransportArgv,
			SupportsGoal:        true,
			SupportsBudget:      true,
			SupportsServiceTier: true,
			CompositionGrammar:  agentic.GrammarID("pangolin-json"),
			HomeEnvVar:          "PANGOLIN_HOME",
			DefaultHome:         "~/.pangolin",
			AuthHint:            "run `pangolin login` and retry",
		},
	}
}

func (p *pangolinSystem) ID() agentic.SystemID                          { return p.id }
func (p *pangolinSystem) Capabilities() agentic.Capabilities            { return p.caps }
func (p *pangolinSystem) ValidateComposition(agentic.Composition) error { return nil }

func (p *pangolinSystem) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return "/opt/pangolin/bin/pangolin", nil
}

func (p *pangolinSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	argv := []string{"--mode", mode.String(), "--model", req.Model.ID}
	if effort := strings.TrimSpace(req.Effort); effort != "" {
		argv = append(argv, "--effort", effort)
	}
	return append(argv, req.PromptPath), nil
}

func (p *pangolinSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	home := req.Home
	if home == "" {
		home = p.caps.DefaultHome
	}
	return append(append([]string(nil), parent...), p.caps.HomeEnvVar+"="+home), nil
}

func (p *pangolinSystem) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{Attached: true, Bytes: req.Prompt}, nil
}

// narwhalVendor is the ONE vendor double in this package. Its fields configure
// what it declares and what each surface answers, so a refusal test builds a
// variant of the same double rather than introducing a second fake whose drift
// from this one nobody would notice.
type narwhalVendor struct {
	id     VendorID
	models []Model
	calls  map[string]int

	// unstableID makes ID() answer id once and this value afterwards: a plugin
	// that breaks Vendor.ID's stability rule the way an accident would.
	unstableID VendorID

	availability    Availability
	availabilityErr error

	// The spawn knobs make the double REDIRECT a launch — the one thing a
	// vendor may not do — so BuildLaunch's fidelity checks have something real
	// to refuse.
	spawnErr           error
	spawnSystem        agentic.SystemID
	spawnModelID       string
	spawnEffortSupport *agentic.EffortSupport
	spawnEffort        *string
	spawnDropGoal      bool
	spawnDropBudget    bool
	spawnTier          *string
	spawnDropComposit  bool
	spawnSkipAuthEnv   bool
}

// narwhalAuthEnv is the vendor's legitimate ADDITION to a launch: the kind of
// thing a vendor owns and a system does not. BuildLaunch must let it through
// while refusing every redirection.
const narwhalAuthEnv = "NARWHAL_TOKEN_VAR=NARWHAL_TOKEN"

func newNarwhal() *narwhalVendor {
	return &narwhalVendor{
		id:    narwhalID,
		calls: map[string]int{},
		models: []Model{
			{
				ID:          "narwhal-deep",
				Description: "long-horizon reasoning over unfamiliar code; the expensive one",
				Lifecycle:   LifecycleCurrent,
				Rank: CapabilityRank{
					Score: 20,
					Basis: []RankEvidence{{
						Source:      "narwhal internal eval 2026-07",
						Observation: "solved 41/50 multi-file refactors against 28/50 for narwhal-flat",
					}},
				},
				Effort: EffortDeclaration{
					Support:     agentic.EffortSupportRequired,
					Vocabulary:  []string{"shallow", "deep"},
					Recommended: "deep",
				},
				Systems: []agentic.SystemID{pangolinID},
			},
			{
				ID:          "narwhal-flat",
				Description: "cheap single-file edits and formatting passes",
				Lifecycle:   LifecycleCurrent,
				Rank: CapabilityRank{
					Score: 10,
					Basis: []RankEvidence{{
						Source:      "narwhal internal eval 2026-07",
						Observation: "28/50 multi-file refactors at a fifth of the cost per run",
					}},
				},
				Effort:  EffortDeclaration{Support: agentic.EffortSupportNone},
				Systems: []agentic.SystemID{pangolinID},
			},
		},
		availability: Healthy("narwhal /v1/quota response headers"),
	}
}

func (n *narwhalVendor) record(surface string) { n.calls[surface]++ }

func (n *narwhalVendor) ID() VendorID {
	n.record("ID")
	if n.unstableID != "" && n.calls["ID"] > 1 {
		return n.unstableID
	}
	return n.id
}

func (n *narwhalVendor) Models() []Model {
	n.record("Models")
	return n.models
}

func (n *narwhalVendor) Availability(AvailabilityQuery) (Availability, error) {
	n.record("Availability")
	if n.availabilityErr != nil {
		return Availability{}, n.availabilityErr
	}
	return n.availability, nil
}

func (n *narwhalVendor) Spawn(sc SpawnContext) (agentic.LaunchRequest, error) {
	n.record("Spawn")
	if n.spawnErr != nil {
		return agentic.LaunchRequest{}, n.spawnErr
	}
	req := sc.Request
	launch := agentic.LaunchRequest{
		System:      sc.Runtime.SystemID,
		Model:       sc.Model.Launchable(),
		Effort:      sc.Effort,
		PromptPath:  req.PromptPath,
		Prompt:      req.Prompt,
		WorkDir:     req.WorkDir,
		Home:        req.Home,
		Env:         append(append([]string(nil), req.Env...), narwhalAuthEnv),
		Goal:        req.Goal,
		Budget:      req.Budget,
		ServiceTier: req.ServiceTier,
		Composition: req.Composition,
	}
	if n.spawnSkipAuthEnv {
		launch.Env = append([]string(nil), req.Env...)
	}
	if n.spawnSystem != "" {
		launch.System = n.spawnSystem
	}
	if n.spawnModelID != "" {
		launch.Model.ID = n.spawnModelID
	}
	if n.spawnEffortSupport != nil {
		launch.Model.Effort = *n.spawnEffortSupport
	}
	if n.spawnEffort != nil {
		launch.Effort = *n.spawnEffort
	}
	if n.spawnDropGoal {
		launch.Goal = nil
	}
	if n.spawnDropBudget {
		launch.Budget = nil
	}
	if n.spawnTier != nil {
		launch.ServiceTier = *n.spawnTier
	}
	if n.spawnDropComposit {
		launch.Composition = agentic.Composition{}
	}
	return launch, nil
}

// tuskDeclaration is the runtime declaration pairing the two doubles.
func tuskDeclaration() RuntimeDeclaration {
	return RuntimeDeclaration{
		ID:     tuskID,
		System: pangolinID,
		Vendor: narwhalID,
		Broker: BrokerProvenance{
			Checked: []string{"the double's own registration"},
			Found:   "the vendor double is registered under this id in this test binary",
		},
	}
}

// registerNarwhal is THE one edit. Nothing else in this package, the CLI, or
// any production source knows the vendor, the runtime or the pair exists.
func registerNarwhal(t *testing.T, vendor *narwhalVendor) *Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(newPangolinSystem()); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	registry := NewRegistry(systems)
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register(narwhal): %v", err)
	}
	if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
		t.Fatalf("DeclareRuntime(tusk): %v", err)
	}
	return registry
}

// narwhalRequest is a fully populated spawn request, so a test that wants to
// exercise one refusal starts from a request that would otherwise succeed.
func narwhalRequest() SpawnRequest {
	return SpawnRequest{
		Runtime:     tuskID,
		Model:       "narwhal-deep",
		Effort:      "deep",
		PromptPath:  "/tmp/assignment.md",
		Prompt:      []byte("do the thing"),
		WorkDir:     "/work/story",
		Env:         []string{"PATH=/usr/bin", "HOME=/home/agent"},
		Goal:        &agentic.Goal{ID: "GOAL-1", Objective: "ship the contract", Revision: 3},
		Budget:      &agentic.Budget{USD: 12.5},
		ServiceTier: "priority",
		Composition: agentic.Composition{
			Prefix:  []string{"--pangolin-mcp", `{"servers":{"jira":{"url":"https://jira.example/mcp"}}}`},
			Servers: []agentic.CompositionServer{{Name: "jira", Transport: "http"}},
		},
	}
}

// TestRegisteringOneVendorDrivesEveryDispatchSurface is the seam proof.
//
// The set of surfaces is read off the Vendor interface by reflection rather
// than written out by hand: a method added to the contract and not driven from
// the package's entry points fails this test on the next run, which is the
// only version of "every dispatch surface" that survives the contract growing.
func TestRegisteringOneVendorDrivesEveryDispatchSurface(t *testing.T) {
	vendor := newNarwhal()
	registry := registerNarwhal(t, vendor)

	if _, err := registry.ResolveRuntime(tuskID); err != nil {
		t.Fatalf("ResolveRuntime(tusk): %v", err)
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeManagedSession} {
		if _, err := BuildLaunch(registry, narwhalRequest(), mode); err != nil {
			t.Fatalf("BuildLaunch(tusk, %s): %v", mode, err)
		}
	}
	if _, err := CheckAvailability(registry, narwhalID, AvailabilityQuery{Model: "narwhal-deep"}); err != nil {
		t.Fatalf("CheckAvailability(narwhal): %v", err)
	}

	contract := reflect.TypeOf((*Vendor)(nil)).Elem()
	var undriven []string
	for i := 0; i < contract.NumMethod(); i++ {
		name := contract.Method(i).Name
		if vendor.calls[name] == 0 {
			undriven = append(undriven, name)
		}
	}
	sort.Strings(undriven)
	if len(undriven) > 0 {
		t.Fatalf("registering a vendor did not drive %v; those surfaces dispatch through something other than the registry, or through nothing at all", undriven)
	}
}

// TestVendorDoubleExistsOnlyInTests is the seam's other half: the double
// drives every surface AND the module contains no trace of it. If supporting
// it had required a second edit — a constant, a case, a table row — the id
// would appear in a non-test source and this fails.
func TestVendorDoubleExistsOnlyInTests(t *testing.T) {
	for name, src := range moduleSources(t) {
		for _, spelling := range []string{string(narwhalID), string(tuskID)} {
			if strings.Contains(src, spelling) {
				t.Errorf("%s mentions the test double %q; registering a vendor and declaring a runtime must touch nothing but those two calls", name, spelling)
			}
		}
	}
}

// moduleSources reads every non-test Go source in the module, discovered by
// walking rather than from a list of directories.
//
// It is a second copy of the walk pkg/agentic's guard uses, and it has to be:
// both are test-only, and Go has no way to share an unexported test helper
// across packages. Sharing it through a production package would put test
// scaffolding in the shipped module to save nine lines.
//
// Two copies of one rule drift, which is the failure this module's guard exists
// to prevent, so the copy is held rather than trusted:
// TestModuleScanScopeMatchesTheGuard plants the same fixture tree pkg/agentic's
// TestSingleSourceGuardScanScope does and requires the same verdicts.
func moduleSources(t *testing.T) map[string]string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("no go.mod above %s; there is nothing to scan", dir)
		}
		root = parent
	}
	sources, err := walkModuleSources(root)
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(sources) < 6 {
		t.Fatalf("scanned %d sources, which is too few for the claim to mean anything", len(sources))
	}
	return sources
}

// moduleSkipDirs are the directory names excluded on top of the toolchain's own
// dot/underscore rule: dependency trees and the directory go build itself
// ignores. See the scan-scope section of pkg/agentic's guard threat model,
// which is where the decision is argued.
var moduleSkipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"testdata":     true,
}

// skipModuleDir answers whether the walk refuses to descend into dir, mirroring
// go/build's own exclusion rules rather than naming directories.
//
// The rule it replaced was a denylist of specific dot-directory names, and a
// bootstrapped checkout with agents-infra installed at .agents/ walked straight
// past it. root itself is never skipped: this checkout habitually lives under
// .temp/, and testing the root's own name would scan zero files while reporting
// clean. A stat failing for any reason other than "not there" is propagated
// rather than read as absence.
func skipModuleDir(root, path, name string) (bool, error) {
	if path == root {
		return false, nil
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true, nil
	}
	if moduleSkipDirs[name] {
		return true, nil
	}
	switch _, err := os.Stat(filepath.Join(path, "go.mod")); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("deciding whether %s is a nested module: %w", path, err)
	}
}

// walkModuleSources collects every non-test Go source under root that the Go
// build of the module rooted there would compile, keyed by slash-separated path
// relative to root. root is an argument so the exclusion rules can be driven
// over a fixture tree rather than only over the real checkout.
func walkModuleSources(root string) (map[string]string, error) {
	sources := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			skip, err := skipModuleDir(root, path, entry.Name())
			if err != nil {
				return err
			}
			if skip {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sources[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sources, nil
}

// helpers shared by the refusal tests below.

func mustRegister(t *testing.T, vendor Vendor, systems *agentic.Registry) (*Registry, error) {
	t.Helper()
	registry := NewRegistry(systems)
	return registry, registry.Register(vendor)
}

func systemsWithPangolin(t *testing.T) *agentic.Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(newPangolinSystem()); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	return systems
}

func requireErrorIs(t *testing.T, err error, want error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: no error at all; the gate admitted what it must reject", what)
	}
	if !errors.Is(err, want) {
		t.Fatalf("%s: err = %v, want %v", what, err, want)
	}
}

func requireMentions(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("no error to read")
	}
	text := err.Error()
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("error %q does not name %q; the refusal has to be fixable from its own text", text, fragment)
		}
	}
}

// renamedPangolin drives the same Layer-1 double under a different system id.
//
// It exists so a test can register the agentic system a FROZEN runtime names —
// muse names the system "muse" — without introducing a second fake whose drift
// from pangolinSystem nobody would notice. Everything except ID() is the
// double above.
type renamedPangolin struct {
	*pangolinSystem
	id agentic.SystemID
}

func (r *renamedPangolin) ID() agentic.SystemID { return r.id }

// systemsNamed registers the pangolin double once per id, so a test can build
// an agentic registry that carries whatever system ids its runtimes declare.
func systemsNamed(t *testing.T, ids ...agentic.SystemID) *agentic.Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	for _, id := range ids {
		if err := systems.Register(&renamedPangolin{pangolinSystem: newPangolinSystem(), id: id}); err != nil {
			t.Fatalf("Register(system %q): %v", id, err)
		}
	}
	return systems
}
