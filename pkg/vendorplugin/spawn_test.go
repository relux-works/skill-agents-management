package vendorplugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestBuildLaunchCarriesTheFullSpawnParameterSurface is AC1's spawn half.
//
// The parameters the board has today — model, effort, env, stdin,
// goal/budget/service-tier, composition — reach the observable launch surface
// through the agentic contract's own types. Nothing here re-declares a Goal, a
// Budget or a Composition, and the assertions are on the agentic.Plan, which
// invariant 1 of docs/architecture.md makes the parity bar.
func TestBuildLaunchCarriesTheFullSpawnParameterSurface(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())

	plan, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if plan.System != pangolinID {
		t.Errorf("plan.System = %q, want the runtime's declared harness %q", plan.System, pangolinID)
	}
	if plan.Binary != "/opt/pangolin/bin/pangolin" {
		t.Errorf("plan.Binary = %q, want the system plugin's resolved executable", plan.Binary)
	}
	wantArgv := []string{"--mode", "exec", "--model", "narwhal-deep", "--effort", "deep", "/tmp/assignment.md"}
	if fmt.Sprint(plan.Argv) != fmt.Sprint(wantArgv) {
		t.Errorf("plan.Argv = %#v\nwant        = %#v", plan.Argv, wantArgv)
	}
	if !plan.Stdin.Attached || string(plan.Stdin.Bytes) != "do the thing" {
		t.Errorf("plan.Stdin = %#v, want the prompt bytes attached", plan.Stdin)
	}
	if plan.WorkDir != "/work/story" {
		t.Errorf("plan.WorkDir = %q, want the request's child working directory", plan.WorkDir)
	}
	if plan.Home != "~/.pangolin" {
		t.Errorf("plan.Home = %q, want the system's declared default home", plan.Home)
	}
	// The vendor's own addition reached the child. This is the half of the
	// fidelity contract that must NOT be refused: a vendor owns authentication.
	if !containsEnv(plan.Env, narwhalAuthEnv) {
		t.Errorf("plan.Env = %v, want the vendor's authentication entry", plan.Env)
	}
	// And the caller's parent environment survived the trip through both layers.
	if !containsEnv(plan.Env, "PATH=/usr/bin") {
		t.Errorf("plan.Env = %v, want the parent environment the caller supplied", plan.Env)
	}
	for _, want := range []string{
		"TASK_BOARD_RUN_ID=RUN-1",
		"TASK_BOARD_TASK_ID=TASK-1",
		"TASK_BOARD_DIR=/work/.task-board",
		"TASK_BOARD_CONTEXT_ID=CTX-1",
	} {
		if !containsEnv(plan.Env, want) {
			t.Errorf("plan.Env = %v, want tracked-run identity %q", plan.Env, want)
		}
	}
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func TestPassthroughLaunchPreservesTheTypedRunContext(t *testing.T) {
	req := narwhalRequest()
	launch := PassthroughLaunch(SpawnContext{
		Runtime: Runtime{SystemID: pangolinID},
		Model:   newNarwhal().models[0],
		Effort:  req.Effort,
		Request: req,
	})
	if launch.Run != req.Run {
		t.Fatalf("PassthroughLaunch.Run = %#v, want %#v", launch.Run, req.Run)
	}
}

// The goal, the budget and the service tier are gated by the SYSTEM's
// capabilities, and a launch composition by its grammar — all in Layer 1. This
// test proves the vendor layer hands them down rather than swallowing them:
// the same request against a system that supports none of them must be refused
// by agentic.BuildPlan, through BuildLaunch, unchanged.
func TestBuildLaunchDefersTheLayerOneRefusalsRatherThanSwallowingThem(t *testing.T) {
	system := newPangolinSystem()
	system.caps.SupportsBudget = false
	systems := agentic.NewRegistry()
	if err := systems.Register(system); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	registry := NewRegistry(systems)
	if err := registry.Register(newNarwhal()); err != nil {
		t.Fatalf("Register(narwhal): %v", err)
	}
	if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
		t.Fatalf("DeclareRuntime: %v", err)
	}

	_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	requireErrorIs(t, err, agentic.ErrBudgetUnsupported, "BuildLaunch(budget on a system that declares none)")
}

// The effort transport is Layer 1's and the vocabulary is Layer 2's, and
// neither layer may answer for the other. A system that cannot carry an effort
// must refuse a required-effort model even though the vendor's vocabulary
// accepted the word.
func TestBuildLaunchRefusesAnEffortTheSystemCannotCarry(t *testing.T) {
	system := newPangolinSystem()
	system.caps.EffortTransport = agentic.EffortTransportNone
	systems := agentic.NewRegistry()
	if err := systems.Register(system); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	registry := NewRegistry(systems)
	if err := registry.Register(newNarwhal()); err != nil {
		t.Fatalf("Register(narwhal): %v", err)
	}
	if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
		t.Fatalf("DeclareRuntime: %v", err)
	}

	_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	requireErrorIs(t, err, agentic.ErrEffortNotTransportable, "BuildLaunch(required effort on a system with no transport)")
}

// No default is injected anywhere. A required-effort model with no effort is a
// refusal, and the refusal carries the vocabulary AND the vendor's
// recommendation so an agent hitting it mid-run can fix the call from the
// error text — which is what invariant 4 asks of a refusal.
func TestBuildLaunchRefusesAMissingEffortAndNamesTheVocabulary(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	req.Effort = "   "

	_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	requireErrorIs(t, err, ErrEffortMissing, "BuildLaunch(required-effort model with no effort)")
	requireMentions(t, err, "narwhal-deep", "shallow", "deep")
}

// The recommendation is a declaration, not a default. The test above proves
// the refusal; this one proves what the refusal PREVENTS — the vendor is never
// asked to build anything, so there is no path on which a recommended value
// could be filled in downstream of the check.
func TestAVendorNeverSeesAnUnadmittedEffort(t *testing.T) {
	vendor := newNarwhal()
	registry := registerNarwhal(t, vendor)
	req := narwhalRequest()
	req.Effort = ""

	if _, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec); err == nil {
		t.Fatal("a launch was built with no effort for a required-effort model")
	}
	if vendor.calls["Spawn"] != 0 {
		t.Fatal("the vendor was asked to build a launch whose effort was never admitted; a plugin holding an unvalidated effort can supply its own")
	}

	req.Effort = "medium"
	if _, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec); err == nil {
		t.Fatal("a launch was built with an effort outside the vocabulary")
	}
	if vendor.calls["Spawn"] != 0 {
		t.Fatal("the vendor was asked to build a launch carrying an effort its own model does not accept")
	}
}

func TestBuildLaunchRefusesAnEffortOutsideTheVocabulary(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	req.Effort = "medium"

	_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	requireErrorIs(t, err, ErrEffortNotInVocabulary, "BuildLaunch(effort outside the model's vocabulary)")
	requireMentions(t, err, "medium", "shallow", "deep")
}

// The vocabulary is matched EXACTLY on the trimmed word, and that bound is the
// rule rather than an implementation detail: an effort vocabulary is a short
// closed set the VENDOR publishes, so folding "Deep" into "deep" here would be
// a second normalization of a value this package does not own — invariant 5.
//
// Deleting the membership check is one mutant and it is already killed.
// WIDENING it — strings.EqualFold — is the case the rule is actually about,
// and it hands the vendor a word it never published. So this drives the
// production entry point with words that differ from the vocabulary only in
// case, and then drives the two that differ only in surrounding whitespace to
// pin that the fix is exactness, not the removal of the trim.
func TestBuildLaunchDoesNotFoldTheEffortVocabulary(t *testing.T) {
	for _, word := range []string{"Deep", "DEEP", "dEeP", "Shallow", "de ep"} {
		vendor := newNarwhal()
		registry := registerNarwhal(t, vendor)
		req := narwhalRequest()
		req.Effort = word

		_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
		requireErrorIs(t, err, ErrEffortNotInVocabulary, "BuildLaunch(effort "+word+", vocabulary [shallow deep])")
		requireMentions(t, err, word, "shallow", "deep")
		if vendor.calls["Spawn"] != 0 {
			t.Fatalf("the vendor was handed the effort %q, which it never published", word)
		}
	}

	for _, word := range []string{"deep ", " deep", "\tdeep\n"} {
		registry := registerNarwhal(t, newNarwhal())
		req := narwhalRequest()
		req.Effort = word

		if _, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec); err != nil {
			t.Errorf("a published effort surrounded by whitespace (%q) was refused: %v", word, err)
		}
	}
}

// The same rule at the type that declares it. BuildLaunch trims the requested
// effort before asking, so driving only the launch path leaves Accepts' own
// half of the contract — exact on the trimmed word — resting on a caller that
// happens to trim first. Accepts is exported and EffortDeclaration.Validate
// calls it on the vendor's own recommendation, so the rule is pinned here as
// well as at the entry point.
func TestEffortVocabularyMembershipIsExactOnTheTrimmedWord(t *testing.T) {
	declaration := EffortDeclaration{
		Support:     agentic.EffortSupportRequired,
		Vocabulary:  []string{"shallow", "deep"},
		Recommended: "deep",
	}
	for _, word := range []string{"deep", "shallow", " deep", "deep\t", "\n shallow "} {
		if !declaration.Accepts(word) {
			t.Errorf("Accepts(%q) = false; the word is in the vocabulary once trimmed", word)
		}
	}
	for _, word := range []string{"Deep", "DEEP", "dEeP", "Shallow", "de ep", "deeper", "", "   "} {
		if declaration.Accepts(word) {
			t.Errorf("Accepts(%q) = true; the vocabulary is [shallow deep] and membership is exact, not folded", word)
		}
	}
}

// A model with no effort axis given an effort is refused rather than having the
// parameter dropped. A dropped parameter produces a run that looks like the one
// that was asked for and is not.
func TestBuildLaunchRefusesAnEffortForAModelWithNoEffortAxis(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	req.Model = "narwhal-flat"
	req.Effort = "deep"

	_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	requireErrorIs(t, err, ErrEffortNotInVocabulary, "BuildLaunch(effort on a model with no effort axis)")
	requireMentions(t, err, "narwhal-flat")
}

func TestBuildLaunchAcceptsAModelWithNoEffortAxisAndNoEffort(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	req.Model = "narwhal-flat"
	req.Effort = ""

	plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(effortless model): %v", err)
	}
	for _, arg := range plan.Argv {
		if arg == "--effort" {
			t.Fatalf("an effort flag reached a model with no effort axis: %v", plan.Argv)
		}
	}
}

func TestBuildLaunchRefusesAModelTheVendorDoesNotDeclare(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	req.Model = "narwhal-imaginary"

	_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	requireErrorIs(t, err, ErrUnknownModel, "BuildLaunch(model the vendor does not declare)")
	requireMentions(t, err, "narwhal-imaginary", "narwhal-deep")
}

// A model that exists, on a runtime that exists, is still refused when the
// model never declared that harness. Launching anyway would be a run nobody
// has evidence works — and the declaration is the only evidence there is.
func TestBuildLaunchRefusesAModelTheRuntimesSystemCannotDrive(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[0].Systems = []agentic.SystemID{"muse"}
	systems := systemsWithPangolin(t)
	if err := systems.Register(museStub{}); err != nil {
		t.Fatalf("Register(muse stub): %v", err)
	}
	registry := NewRegistry(systems)
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register(narwhal): %v", err)
	}
	if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
		t.Fatalf("DeclareRuntime: %v", err)
	}

	_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	requireErrorIs(t, err, ErrModelNotDrivenBySystem, "BuildLaunch(model that does not declare the runtime's system)")
	requireMentions(t, err, "narwhal-deep", string(pangolinID), string(tuskID))
}

// museStub exists only so the test above can register a SECOND system, making
// the model's declaration resolvable while still not being the runtime's. It
// declares the minimum a registration accepts.
type museStub struct{}

func (museStub) ID() agentic.SystemID { return "muse" }
func (museStub) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes:     []agentic.LaunchMode{agentic.LaunchModeExec},
		EffortTransport: agentic.EffortTransportArgv,
	}
}
func (museStub) ResolveBinary(agentic.LaunchRequest) (string, error) { return "/usr/bin/muse", nil }
func (museStub) Argv(agentic.LaunchRequest, agentic.LaunchMode) ([]string, error) {
	return []string{}, nil
}
func (museStub) ChildEnv(parent []string, _ agentic.LaunchRequest) ([]string, error) {
	return parent, nil
}
func (museStub) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}
func (museStub) ValidateComposition(agentic.Composition) error { return nil }

// A vendor may ADD to a launch and may not REDIRECT it. Each case below is a
// redirection that would produce a run looking exactly like the one that was
// asked for: the wrong harness, the wrong model, the wrong cost, an unbounded
// budget, a dropped goal, a composition the caller never approved.
func TestBuildLaunchRefusesAVendorThatRedirectsTheLaunch(t *testing.T) {
	tier := "batch"
	effort := "shallow"
	support := agentic.EffortSupportNone
	baseRun := narwhalRequest().Run
	wrongRunID := baseRun
	wrongRunID.RunID = "RUN-OTHER"
	wrongTaskID := baseRun
	wrongTaskID.TaskID = "TASK-OTHER"
	wrongBoardDir := baseRun
	wrongBoardDir.BoardDir = "/other/.task-board"
	droppedContextID := baseRun
	droppedContextID.ContextID = ""

	cases := map[string]func(v *narwhalVendor){
		"a different agentic system": func(v *narwhalVendor) { v.spawnSystem = "muse" },
		"no agentic system at all":   func(v *narwhalVendor) { v.spawnSystem = " " },
		"a different model":          func(v *narwhalVendor) { v.spawnModelID = "narwhal-flat" },
		"a different effort":         func(v *narwhalVendor) { v.spawnEffort = &effort },
		"a different effort support": func(v *narwhalVendor) { v.spawnEffortSupport = &support },
		"a different run id":         func(v *narwhalVendor) { v.spawnRun = &wrongRunID },
		"a different task id":        func(v *narwhalVendor) { v.spawnRun = &wrongTaskID },
		"a different board dir":      func(v *narwhalVendor) { v.spawnRun = &wrongBoardDir },
		"the writable context dropped": func(v *narwhalVendor) {
			v.spawnRun = &droppedContextID
		},
		"the goal dropped":         func(v *narwhalVendor) { v.spawnDropGoal = true },
		"the budget dropped":       func(v *narwhalVendor) { v.spawnDropBudget = true },
		"a different service tier": func(v *narwhalVendor) { v.spawnTier = &tier },
		"the composition dropped":  func(v *narwhalVendor) { v.spawnDropComposit = true },
	}
	for name, redirect := range cases {
		t.Run(name, func(t *testing.T) {
			vendor := newNarwhal()
			redirect(vendor)
			registry := registerNarwhal(t, vendor)

			_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
			requireErrorIs(t, err, ErrVendorContract, "BuildLaunch(vendor returning "+name+")")
		})
	}
}

// A vendor that fails to build a request fails the launch, with its own error
// preserved rather than replaced by a generic one.
func TestBuildLaunchSurfacesAVendorsOwnSpawnError(t *testing.T) {
	vendor := newNarwhal()
	vendor.spawnErr = errors.New("no narwhal credentials in this home")
	registry := registerNarwhal(t, vendor)

	_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if err == nil {
		t.Fatal("a vendor that could not build a launch produced no error")
	}
	requireMentions(t, err, "no narwhal credentials", string(narwhalID))
}

// A vendor's legitimate addition must survive: this is the control for the
// redirection tests above, which would all pass against an implementation that
// simply ignored the vendor's answer and rebuilt the request itself.
func TestBuildLaunchKeepsTheVendorsOwnAdditions(t *testing.T) {
	vendor := newNarwhal()
	vendor.spawnSkipAuthEnv = true
	registry := registerNarwhal(t, vendor)

	plan, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if containsEnv(plan.Env, narwhalAuthEnv) {
		t.Fatal("the plan carries an environment entry the vendor did not produce; the vendor's answer is being ignored and rebuilt")
	}
}

func TestBuildLaunchRefusesUnresolvableRuntimes(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	req.Runtime = "nosuch"

	_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	requireErrorIs(t, err, ErrUnknownRuntime, "BuildLaunch(undeclared runtime)")
}

// A system-only runtime is not a degraded vendor runtime. Its declaration is
// the explicit owner of the model and effort facts because no vendor was ever
// established, and BuildLaunch must still reach the ordinary Layer-1 plan
// without inventing a vendor or naming the historical muse id in dispatch.
func TestBuildLaunchUsesADeclarationOwnedSystemOnlyBinding(t *testing.T) {
	registry := NewRegistry(systemsNamed(t, "muse"))
	if err := SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("SeedFrozenRuntimes: %v", err)
	}

	request := SpawnRequest{
		Runtime:    "muse",
		Model:      "muse-spark",
		PromptPath: "/tmp/assignment.md",
		WorkDir:    "/work/story",
		Env:        []string{"PATH=/usr/bin"},
		Run: agentic.RunContext{
			RunID:     "RUN-MUSE",
			TaskID:    "TASK-MUSE",
			BoardDir:  "/work/.task-board",
			ContextID: "CTX-MUSE",
		},
	}
	plan, err := BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch(system-only runtime): %v", err)
	}
	if plan.System != "muse" {
		t.Fatalf("plan.System = %q, want muse", plan.System)
	}
	if !strings.Contains(strings.Join(plan.Argv, " "), "muse-spark") {
		t.Fatalf("plan.Argv = %v, want the declaration-owned model", plan.Argv)
	}
	for _, want := range []string{
		"TASK_BOARD_RUN_ID=RUN-MUSE",
		"TASK_BOARD_TASK_ID=TASK-MUSE",
		"TASK_BOARD_DIR=/work/.task-board",
		"TASK_BOARD_CONTEXT_ID=CTX-MUSE",
	} {
		if !containsEnv(plan.Env, want) {
			t.Errorf("plan.Env = %v, want %q", plan.Env, want)
		}
	}
}

func TestBuildLaunchValidatesDeclarationOwnedModelAndEffortFacts(t *testing.T) {
	declaration := RuntimeDeclaration{
		ID:     "system-only",
		System: pangolinID,
		Vendor: VendorUnresolved,
		Broker: BrokerProvenance{Checked: []string{"test fixture"}},
		Models: []Model{{
			ID:          "declared-reasoner",
			Description: "a declaration-owned model used to prove effort validation",
			Lifecycle:   LifecycleCurrent,
			Rank: CapabilityRank{Score: 10, Basis: []RankEvidence{{
				Source: "test fixture", Observation: "the system-only binding carries this row",
			}}},
			Effort: EffortDeclaration{
				Support:     agentic.EffortSupportRequired,
				Vocabulary:  []string{"low", "high"},
				Recommended: "high",
			},
			Systems: []agentic.SystemID{pangolinID},
		}},
	}
	registry := NewRegistry(systemsWithPangolin(t))
	if err := registry.DeclareRuntime(declaration); err != nil {
		t.Fatalf("DeclareRuntime(system-only): %v", err)
	}
	base := narwhalRequest()
	base.Runtime = declaration.ID
	base.Model = declaration.Models[0].ID

	t.Run("unknown model", func(t *testing.T) {
		req := base
		req.Model = "self-minted"
		_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
		requireErrorIs(t, err, ErrUnknownModel, "BuildLaunch(system-only unknown model)")
	})
	t.Run("missing effort", func(t *testing.T) {
		req := base
		req.Effort = ""
		_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
		requireErrorIs(t, err, ErrEffortMissing, "BuildLaunch(system-only missing effort)")
	})
	t.Run("forged effort", func(t *testing.T) {
		req := base
		req.Effort = "ultra"
		_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
		requireErrorIs(t, err, ErrEffortNotInVocabulary, "BuildLaunch(system-only forged effort)")
	})
	t.Run("declared effort", func(t *testing.T) {
		req := base
		req.Effort = "high"
		plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
		if err != nil {
			t.Fatalf("BuildLaunch(system-only declared effort): %v", err)
		}
		if !strings.Contains(strings.Join(plan.Argv, " "), "high") {
			t.Fatalf("plan.Argv = %v, want declared effort", plan.Argv)
		}
	})
}

// The dry run must mirror the real launch's target. It is the bug the source
// repository's BuildArgs carried — a printed command that was not the command
// — and it has to hold through both layers, not only through Layer 1.
func TestDryRunThroughBothLayersMirrorsTheRealLaunch(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())

	real, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(exec): %v", err)
	}
	dry, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch(dry-run): %v", err)
	}
	if dry.Binary != real.Binary {
		t.Errorf("dry-run binary = %q, real binary = %q; a dry run that names a different target is not a mirror", dry.Binary, real.Binary)
	}
	if !strings.Contains(fmt.Sprint(dry.Argv), "narwhal-deep") {
		t.Errorf("dry-run argv = %v, want the model the real launch would use", dry.Argv)
	}
}
