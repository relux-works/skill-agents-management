package vendorplugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// The native-Pi runtimes through the ONE launch entry point, BuildLaunch, on
// the real vendor rows (TASK-260908-ggxfte rev2 §4). Every refusal below is a
// production gate reached through that call, never a helper unit test.

var piRuntimes = []vendorplugin.RuntimeID{"pi-anthropic", "pi-openai", "pi-google"}

// piLaunchEnv is a launch PATH holding ONLY a stub `pi`: no agents-infra
// wrapper, so a plan that needed one could not build.
func piLaunchEnv(t *testing.T) ([]string, string) {
	t.Helper()
	binDir := t.TempDir()
	writeStubBinary(t, binDir, "pi")
	return []string{"PATH=" + binDir, "PI_CODING_AGENT_DIR=/Users/op/.managed/pi"}, binDir
}

func piRequest(t *testing.T, runtime vendorplugin.RuntimeID, model, effort string) vendorplugin.SpawnRequest {
	t.Helper()
	env, _ := piLaunchEnv(t)
	return vendorplugin.SpawnRequest{
		Runtime: runtime,
		Model:   vendorplugin.ModelID(model),
		Effort:  effort,
		WorkDir: "/Users/op/project",
		Home:    "/Users/op/.managed/pi",
		Env:     env,
		Run:     agentic.RunContext{RunID: "RUN-pi-native", TaskID: "TASK-pi-native"},
	}
}

func TestPiAnthropicGoldenPlan(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	req := piRequest(t, "pi-anthropic", "claude-opus-5", "high")
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildLaunch(pi-anthropic, claude-opus-5, high): %v", err)
	}
	if want := []string{"--model", "anthropic/claude-opus-5", "--thinking", "high"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
	if filepath.Base(plan.Binary) != "pi" {
		t.Errorf("Binary = %q, want the pi binary on the launch PATH", plan.Binary)
	}
	if plan.Stdin.Attached {
		t.Errorf("stdin attached to an interactive native launch")
	}
	if plan.Home != "/Users/op/.managed/pi" {
		t.Errorf("Home = %q, want the managed home the request named", plan.Home)
	}
	if plan.System != "pi-native" || plan.Mode != agentic.LaunchModeInteractive {
		t.Errorf("plan = (%s, %s)", plan.System, plan.Mode)
	}
	found := false
	for _, entry := range plan.Env {
		if entry == "PI_CODING_AGENT_DIR=/Users/op/.managed/pi" {
			found = true
		}
	}
	if !found {
		t.Errorf("the managed-home variable did not pass through: %v", plan.Env)
	}
	dry, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch(dry-run): %v", err)
	}
	if !reflect.DeepEqual(dry.Argv, plan.Argv) || dry.Binary != plan.Binary {
		t.Errorf("dry run argv %v / binary %q differ from the interactive %v / %q", dry.Argv, dry.Binary, plan.Argv, plan.Binary)
	}
}

func TestPiGoogleEffortNoneRowCarriesNoThinkingFlag(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, piRequest(t, "pi-google", "gemini-3.1-pro-preview", ""), agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildLaunch(pi-google): %v", err)
	}
	if want := []string{"--model", "google/gemini-3.1-pro-preview"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
}

// TestEveryPiRuntimeModelLaunchesProviderQualified is the argv-never-bare
// sweep over every (pi-* runtime × model) pair the registry admits, and it
// reports the count it drove so a shrunken membership is visible.
func TestEveryPiRuntimeModelLaunchesProviderQualified(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	driven := 0
	for _, id := range piRuntimes {
		runtime, err := registry.ResolveRuntime(id)
		if err != nil {
			t.Fatalf("ResolveRuntime(%s): %v", id, err)
		}
		models := vendorplugin.RuntimeModels(runtime)
		if len(models) == 0 {
			t.Fatalf("runtime %s admits no model; it is declared and unlaunchable", id)
		}
		for _, model := range models {
			effort := model.Effort.Recommended
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, piRequest(t, id, string(model.ID), effort), agentic.LaunchModeInteractive)
			if err != nil {
				t.Errorf("BuildLaunch(%s, %s, %q): %v", id, model.ID, effort, err)
				continue
			}
			driven++
			prefix := string(runtime.VendorID) + "/"
			for i, arg := range plan.Argv {
				if arg == string(model.ID) || arg == string(model.AliasOf) {
					t.Errorf("%s/%s: argv[%d] = %q is a bare id: %v", id, model.ID, i, arg, plan.Argv)
				}
				if i > 0 && plan.Argv[i-1] == "--model" && !strings.HasPrefix(arg, prefix) {
					t.Errorf("%s/%s: --model value %q lacks the %q prefix", id, model.ID, arg, prefix)
				}
			}
			if strings.TrimSpace(effort) == "" && len(plan.Argv) != 2 {
				t.Errorf("%s/%s: effort-none row produced %v", id, model.ID, plan.Argv)
			}
			if strings.TrimSpace(effort) != "" && !reflect.DeepEqual(plan.Argv[2:], []string{"--thinking", effort}) {
				t.Errorf("%s/%s: effort %q not transported: %v", id, model.ID, effort, plan.Argv)
			}
		}
	}
	t.Logf("drove %d (runtime × model) pairs through BuildLaunch", driven)
	if driven < 24 {
		t.Fatalf("drove only %d pairs; the catalog-verified membership is 8 anthropic + 9 openai + 7 google = 24 rows", driven)
	}
}

// spoofingVendor wraps a real vendor and has its Spawn set Vendor (and
// Runtime) to a lie. BuildLaunch must overwrite both after Spawn returns.
type spoofingVendor struct{ inner vendorplugin.Vendor }

func (s spoofingVendor) ID() vendorplugin.VendorID    { return s.inner.ID() }
func (s spoofingVendor) Models() []vendorplugin.Model { return s.inner.Models() }
func (s spoofingVendor) Availability(q vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return s.inner.Availability(q)
}
func (s spoofingVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	launch, err := s.inner.Spawn(sc)
	launch.Vendor = "openai"
	launch.Runtime = "codex"
	return launch, err
}

func spoofedRegistry(t *testing.T) *vendorplugin.Registry {
	t.Helper()
	registry := vendorplugin.NewRegistry(agentic.Default)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	anthropic, ok := vendorplugin.Default.Lookup("anthropic")
	if !ok {
		t.Fatal("anthropic is not registered in the default registry")
	}
	if err := registry.Register(spoofingVendor{inner: anthropic}); err != nil {
		t.Fatalf("registering the spoofing vendor: %v", err)
	}
	return registry
}

// TestBuildLaunchOverwritesAVendorSetBySpawn is the authoritative-Vendor
// clause: the qualified identity comes from the binding, not from whatever
// the vendor's Spawn wrote.
func TestBuildLaunchOverwritesAVendorSetBySpawn(t *testing.T) {
	plan, err := vendorplugin.BuildLaunch(context.Background(), spoofedRegistry(t), piRequest(t, "pi-anthropic", "claude-opus-5", "high"), agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if want := []string{"--model", "anthropic/claude-opus-5", "--thinking", "high"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("Argv = %#v, want %#v; the vendor's spoofed Vendor reached argv", plan.Argv, want)
	}
}

// TestTheSpoofWouldHaveReachedArgvWithoutTheOverwrite is the narrowing for the
// test above: the same spoofing Spawn handed straight to the plugin (the path
// BuildLaunch's overwrite guards) DOES emit the lie, so the overwrite is
// load-bearing rather than the spoof being inert.
func TestTheSpoofWouldHaveReachedArgvWithoutTheOverwrite(t *testing.T) {
	anthropic, _ := vendorplugin.Default.Lookup("anthropic")
	registry := spoofedRegistry(t)
	runtime, err := registry.ResolveRuntime("pi-anthropic")
	if err != nil {
		t.Fatalf("ResolveRuntime: %v", err)
	}
	var model vendorplugin.Model
	for _, row := range anthropic.Models() {
		if row.ID == "claude-opus-5" {
			model = row
		}
	}
	launch, err := spoofingVendor{inner: anthropic}.Spawn(vendorplugin.SpawnContext{Runtime: runtime, Model: model, Effort: "high", Request: piRequest(t, "pi-anthropic", "claude-opus-5", "high")})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	argv, err := pinative.Args(launch, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	if argv[1] != "openai/claude-opus-5" {
		t.Fatalf("the spoof did not reach argv on the unguarded path (%v); the overwrite test above measures nothing", argv)
	}
}

func TestBuildLaunchRefusalsOnPiRuntimes(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tt := range []struct {
		name    string
		runtime vendorplugin.RuntimeID
		model   string
		effort  string
		mode    agentic.LaunchMode
		mutate  func(*vendorplugin.SpawnRequest)
		want    error
	}{
		{"effort outside the vocabulary", "pi-anthropic", "claude-opus-5", "ultra", agentic.LaunchModeInteractive, nil, vendorplugin.ErrEffortNotInVocabulary},
		{"empty effort on a required row", "pi-anthropic", "claude-opus-5", "", agentic.LaunchModeInteractive, nil, vendorplugin.ErrEffortMissing},
		{"effort on an effort-none row", "pi-google", "gemini-3.1-pro-preview", "high", agentic.LaunchModeInteractive, nil, vendorplugin.ErrEffortNotInVocabulary},
		{"empty model", "pi-anthropic", "", "high", agentic.LaunchModeInteractive, nil, vendorplugin.ErrUnknownModel},
		{"exec mode", "pi-anthropic", "claude-opus-5", "high", agentic.LaunchModeExec, func(r *vendorplugin.SpawnRequest) { r.Prompt = []byte("body") }, agentic.ErrUnsupportedLaunchMode},
		{"composition", "pi-anthropic", "claude-opus-5", "high", agentic.LaunchModeInteractive, func(r *vendorplugin.SpawnRequest) {
			r.Composition = agentic.Composition{Prefix: []string{"--mcp", "x"}}
		}, agentic.ErrCompositionNotInteractive},
		{"another vendor's model", "pi-anthropic", "gpt-5.5", "high", agentic.LaunchModeInteractive, nil, vendorplugin.ErrUnknownModel},
		{"a row absent from the Pi catalog", "pi-anthropic", "claude-fable-5-1", "high", agentic.LaunchModeInteractive, nil, vendorplugin.ErrModelNotDrivenBySystem},
		{"an openai head absent from the catalog", "pi-openai", "gpt-6-astra", "high", agentic.LaunchModeInteractive, nil, vendorplugin.ErrModelNotDrivenBySystem},
		{"the alias mirrors its absent target", "pi-openai", "astra", "high", agentic.LaunchModeInteractive, nil, vendorplugin.ErrModelNotDrivenBySystem},
		{"an antigravity-only google row", "pi-google", "gemini-3.1-pro-high", "", agentic.LaunchModeInteractive, nil, vendorplugin.ErrModelNotDrivenBySystem},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := piRequest(t, tt.runtime, tt.model, tt.effort)
			if tt.mutate != nil {
				tt.mutate(&req)
			}
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, tt.mode)
			if !errors.Is(err, tt.want) {
				t.Fatalf("BuildLaunch(%s, %q, %q, %s) = argv %v, err %v; want %v", tt.runtime, tt.model, tt.effort, tt.mode, plan.Argv, err, tt.want)
			}
		})
	}
}

// TestAnAliasRowLaunchesItsTargetQualified drives the alias substitution
// through BuildLaunch on a mutant that admits the astra pair to pi-native
// (the real pair is refused because gpt-6-astra is absent from the catalog;
// the mutant is the only way to reach the alias arm on a pi runtime).
func TestAnAliasRowLaunchesItsTargetQualified(t *testing.T) {
	registry := isolatedRegistry(t, map[vendorplugin.VendorID]func(vendorplugin.Model) vendorplugin.Model{
		"openai": func(m vendorplugin.Model) vendorplugin.Model {
			if m.ID == "astra" || m.ID == "gpt-6-astra" {
				m.Systems = append(append([]agentic.SystemID(nil), m.Systems...), "pi-native")
			}
			return m
		},
	})
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, piRequest(t, "pi-openai", "astra", "high"), agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildLaunch(pi-openai, astra): %v", err)
	}
	if want := []string{"--model", "openai/gpt-6-astra", "--thinking", "high"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %#v, want %#v", plan.Argv, want)
	}
	if plan.ModelIdentity.Requested != "astra" || plan.ModelIdentity.Launched != "gpt-6-astra" {
		t.Errorf("ModelIdentity = %#v", plan.ModelIdentity)
	}
}

// TestAliasMirrorRefusesAOneSidedPiNativeMembership is the sameSystems rule
// applied to pi-native: an alias that names it while its target does not (or
// the reverse) does not register.
func TestAliasMirrorRefusesAOneSidedPiNativeMembership(t *testing.T) {
	for _, side := range []string{"astra", "gpt-6-astra"} {
		registry := vendorplugin.NewRegistry(agentic.Default)
		if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		openai, _ := vendorplugin.Default.Lookup("openai")
		err := registry.Register(mutantVendor{inner: openai, mutate: func(m vendorplugin.Model) vendorplugin.Model {
			if m.ID == vendorplugin.ModelID(side) {
				m.Systems = append(append([]agentic.SystemID(nil), m.Systems...), "pi-native")
			}
			return m
		}})
		if !errors.Is(err, vendorplugin.ErrAliasInvalid) {
			t.Fatalf("%s alone naming pi-native registered with %v; want ErrAliasInvalid", side, err)
		}
	}
}

func TestThePiRuntimesAreFrozenWithTheirCatalogProvenance(t *testing.T) {
	want := map[vendorplugin.RuntimeID]vendorplugin.VendorID{"pi-anthropic": "anthropic", "pi-openai": "openai", "pi-google": "google"}
	seen := 0
	for _, declaration := range vendorplugin.FrozenRuntimes() {
		vendor, ok := want[declaration.ID]
		if !ok {
			continue
		}
		seen++
		if declaration.System != "pi-native" || declaration.Vendor != vendor {
			t.Errorf("%s = (%s × %s), want (pi-native × %s)", declaration.ID, declaration.System, declaration.Vendor, vendor)
		}
		if len(declaration.Broker.Checked) != 1 || !strings.Contains(declaration.Broker.Checked[0], "pi 0.84.2") || !strings.Contains(declaration.Broker.Checked[0], string(vendor)+".json") {
			t.Errorf("%s checked sources = %v, want the installed Pi provider data file", declaration.ID, declaration.Broker.Checked)
		}
	}
	if seen != 3 {
		t.Fatalf("saw %d native-Pi frozen rows, want 3", seen)
	}
}

// TestPiNativeMembershipMatchesTheInstalledCatalog is the no-secret installed-Pi
// probe: with Pi's provider data on disk, every row naming pi-native must be
// in the catalog and every registry row present in the catalog must name it.
// It reads bytes only; no model call, no credential. It skips, loudly, when
// Pi is not installed where the design recorded it.
func TestPiNativeMembershipMatchesTheInstalledCatalog(t *testing.T) {
	dataDir := os.Getenv("PI_AI_PROVIDER_DATA_DIR")
	if dataDir == "" {
		dataDir = "/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/providers/data"
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Skipf("installed Pi provider data not found at %s (%v); set PI_AI_PROVIDER_DATA_DIR", dataDir, err)
	}
	for _, vendor := range []vendorplugin.VendorID{"anthropic", "openai", "google"} {
		raw, err := os.ReadFile(filepath.Join(dataDir, string(vendor)+".json"))
		if err != nil {
			t.Fatalf("reading %s catalog: %v", vendor, err)
		}
		var byAPI map[string]map[string]json.RawMessage
		if err := json.Unmarshal(raw, &byAPI); err != nil {
			t.Fatalf("decoding %s catalog: %v", vendor, err)
		}
		catalog := map[string]bool{}
		for _, models := range byAPI {
			for id := range models {
				catalog[id] = true
			}
		}
		plugin, _ := vendorplugin.Default.Lookup(vendor)
		var mismatches []string
		for _, row := range plugin.Models() {
			native := row.DrivenBy("pi-native")
			inCatalog := catalog[row.Launchable().LaunchIdentity()]
			if native && !inCatalog {
				mismatches = append(mismatches, string(row.ID)+" names pi-native but is absent from the catalog")
			}
			if !native && inCatalog && row.AliasOf == "" {
				mismatches = append(mismatches, string(row.ID)+" is in the catalog but does not name pi-native")
			}
		}
		sort.Strings(mismatches)
		if len(mismatches) != 0 {
			t.Errorf("%s: %s", vendor, strings.Join(mismatches, "; "))
		}
	}
}

// The installed-Pi thinking contract (review rev1, finding R1): a word the
// row's vocabulary admits but Pi 0.84.2 would warn-and-drop (`ultra`, not a
// parser level) or silently clamp (`minimal` on rows whose catalog
// thinkingLevelMap maps it to null) is REFUSED through BuildLaunch, never
// transported into a plan that misrepresents the session. Production call
// site: spawn.go, the agentic.EffortAdmitter dispatch after resolveEffort.

func TestBuildLaunchRefusesAnEffortInstalledPiWouldDropOrClamp(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tt := range []struct {
		runtime         vendorplugin.RuntimeID
		model, effort   string
		accepted, recom string
	}{
		{"pi-openai", "gpt-5.6-sol", "ultra", "[low medium high xhigh max]", `"max"`},
		{"pi-openai", "gpt-5.6-terra", "ultra", "[low medium high xhigh max]", `"max"`},
		{"pi-openai", "gpt-5.3-codex", "minimal", "[low medium high xhigh]", `"xhigh"`},
		{"pi-openai", "gpt-5.2", "minimal", "[low medium high xhigh]", `"xhigh"`},
	} {
		t.Run(tt.model+"/"+tt.effort, func(t *testing.T) {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, piRequest(t, tt.runtime, tt.model, tt.effort), agentic.LaunchModeInteractive)
			if !errors.Is(err, vendorplugin.ErrEffortNotNativelySupported) || !errors.Is(err, pinative.ErrEffortNotNativelySupported) {
				t.Fatalf("BuildLaunch(%s, %s, %s) = argv %v, err %v; want ErrEffortNotNativelySupported (both identities)", tt.runtime, tt.model, tt.effort, plan.Argv, err)
			}
			for _, want := range []string{`"` + tt.model + `"`, string(tt.runtime), `"` + tt.effort + `"`, tt.accepted, "recommends " + tt.recom} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %s", err, want)
				}
			}
			if len(plan.Argv) != 0 {
				t.Errorf("a refused launch still produced argv %v", plan.Argv)
			}
			// Same word, same row, on Codex: the vocabulary is global and the
			// refusal is native Pi's alone.
			codex, err := vendorplugin.BuildLaunch(context.Background(), registry, codexRequestFor(t, tt.model, tt.effort), agentic.LaunchModeDryRun)
			if err != nil {
				t.Fatalf("the same (model, effort) on codex must still build: %v", err)
			}
			if !strings.Contains(strings.Join(codex.Argv, " "), `model_reasoning_effort="`+tt.effort+`"`) {
				t.Errorf("codex argv %v does not carry %q", codex.Argv, tt.effort)
			}
		})
	}
}

func codexRequestFor(t *testing.T, model, effort string) vendorplugin.SpawnRequest {
	t.Helper()
	binDir := t.TempDir()
	writeStubBinary(t, binDir, "codex")
	return vendorplugin.SpawnRequest{
		Runtime: "codex", Model: vendorplugin.ModelID(model), Effort: effort,
		WorkDir: "/Users/op/project", Env: []string{"PATH=" + binDir},
		Run: agentic.RunContext{RunID: "RUN-codex", TaskID: "TASK-codex"},
	}
}

func TestTheMaxWordStillBuildsWhereInstalledPiRunsIt(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tt := range []struct {
		runtime vendorplugin.RuntimeID
		model   string
		want    []string
	}{
		{"pi-openai", "gpt-5.6-sol", []string{"--model", "openai/gpt-5.6-sol", "--thinking", "max"}},
		{"pi-anthropic", "claude-opus-5", []string{"--model", "anthropic/claude-opus-5", "--thinking", "max"}},
	} {
		plan, err := vendorplugin.BuildLaunch(context.Background(), registry, piRequest(t, tt.runtime, tt.model, "max"), agentic.LaunchModeInteractive)
		if err != nil {
			t.Fatalf("BuildLaunch(%s, %s, max): %v", tt.runtime, tt.model, err)
		}
		if !reflect.DeepEqual(plan.Argv, tt.want) {
			t.Errorf("Argv = %#v, want %#v", plan.Argv, tt.want)
		}
	}
}

// TestPiNativeThinkingRestrictionsMatchTheInstalledCatalog is the thinking
// half of the no-secret installed-Pi probe. From the installed bytes it
// re-derives (a) the parser list in dist/cli/args.js and (b) per catalog model
// `getSupportedThinkingLevels` (pi-ai models.js:548-560: reasoning required;
// a level mapped to null is unsupported; xhigh/max need an explicit mapping),
// then checks EVERY (pi-native row × vocabulary word) in both directions
// against pinative.AdmitEffort: a word the module refuses that Pi runs, or a
// word Pi drops or clamps that the module admits, fails. Reads bytes only.
func TestPiNativeThinkingRestrictionsMatchTheInstalledCatalog(t *testing.T) {
	root := os.Getenv("PI_CODING_AGENT_ROOT")
	if root == "" {
		root = "/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent"
	}
	argsJS, err := os.ReadFile(filepath.Join(root, "dist", "cli", "args.js"))
	if os.IsNotExist(err) {
		t.Skipf("installed Pi not found at %s (%v); set PI_CODING_AGENT_ROOT", root, err)
	}
	if err != nil {
		t.Fatalf("reading installed Pi parser: %v", err)
	}
	re := regexp.MustCompile(`VALID_THINKING_LEVELS\s*=\s*\[([^\]]*)\]`)
	m := re.FindSubmatch(argsJS)
	if m == nil {
		t.Fatalf("args.js does not declare VALID_THINKING_LEVELS")
	}
	var parser []string
	for _, q := range strings.Split(string(m[1]), ",") {
		parser = append(parser, strings.Trim(strings.TrimSpace(q), `"`))
	}
	if !reflect.DeepEqual(parser, pinative.ParserThinkingLevels) {
		t.Fatalf("installed parser levels %v != pinative.ParserThinkingLevels %v", parser, pinative.ParserThinkingLevels)
	}
	parses := map[string]bool{}
	for _, p := range parser {
		parses[p] = true
	}

	dataDir := filepath.Join(root, "node_modules", "@earendil-works", "pi-ai", "dist", "providers", "data")
	admitter := pinative.New()
	registry := isolatedRegistry(t, nil)
	var mismatches []string
	checked := 0
	for _, vendor := range []vendorplugin.VendorID{"anthropic", "openai", "google"} {
		raw, err := os.ReadFile(filepath.Join(dataDir, string(vendor)+".json"))
		if err != nil {
			t.Fatalf("reading %s catalog: %v", vendor, err)
		}
		var byAPI map[string]map[string]struct {
			Reasoning        bool                        `json:"reasoning"`
			ThinkingLevelMap map[string]*json.RawMessage `json:"thinkingLevelMap"`
		}
		if err := json.Unmarshal(raw, &byAPI); err != nil {
			t.Fatalf("decoding %s catalog: %v", vendor, err)
		}
		plugin, _ := vendorplugin.Default.Lookup(vendor)
		for _, row := range plugin.Models() {
			if !row.DrivenBy("pi-native") {
				continue
			}
			identity := row.Launchable().LaunchIdentity()
			var entry *struct {
				Reasoning        bool                        `json:"reasoning"`
				ThinkingLevelMap map[string]*json.RawMessage `json:"thinkingLevelMap"`
			}
			for _, models := range byAPI {
				if e, ok := models[identity]; ok {
					e := e
					entry = &e
				}
			}
			if entry == nil {
				t.Fatalf("%s: pi-native row %s is not in the catalog (membership test owns this)", vendor, row.ID)
			}
			piSupports := func(word string) bool {
				if !parses[word] || !entry.Reasoning {
					return false
				}
				mapped, present := entry.ThinkingLevelMap[word]
				if present && mapped == nil {
					return false
				}
				if word == "xhigh" || word == "max" {
					return present
				}
				return true
			}
			for _, word := range row.Effort.Vocabulary {
				checked++
				_, admitErr := admitter.AdmitEffort(string(vendor), identity, word, row.Effort.Vocabulary)
				for _, mode := range []agentic.LaunchMode{agentic.LaunchModeInteractive, agentic.LaunchModeDryRun} {
					plan, err := vendorplugin.BuildLaunch(context.Background(), registry, piRequest(t, vendorplugin.RuntimeID("pi-"+string(vendor)), string(row.ID), word), mode)
					if piSupports(word) {
						if err != nil || !reflect.DeepEqual(plan.Argv, []string{"--model", string(vendor) + "/" + identity, "--thinking", word}) {
							t.Errorf("%s/%s/%s: supported plan = %v, %v", row.ID, word, mode, plan.Argv, err)
						}
					} else if !errors.Is(err, vendorplugin.ErrEffortNotNativelySupported) || len(plan.Argv) != 0 {
						t.Errorf("%s/%s/%s: unsupported plan = %v, %v", row.ID, word, mode, plan.Argv, err)
					}
				}
				switch {
				case piSupports(word) && admitErr != nil:
					mismatches = append(mismatches, string(row.ID)+": module refuses "+word+" but installed Pi runs it")
				case !piSupports(word) && admitErr == nil:
					mismatches = append(mismatches, string(row.ID)+": module admits "+word+" but installed Pi drops or clamps it")
				}
			}
		}
	}
	sort.Strings(mismatches)
	if len(mismatches) != 0 {
		t.Errorf("%s", strings.Join(mismatches, "; "))
	}
	if checked != 71 {
		t.Fatalf("checked %d (row × word) pairs, want the 71 catalog-verified pairs", checked)
	}
	t.Logf("checked %d (pi-native row × vocabulary word) pairs against the installed catalog", checked)
}
