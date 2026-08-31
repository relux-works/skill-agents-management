package muse

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file holds the plugin's declaration and the surfaces no golden pins: the
// refusals, the conditional flags, and the fact that muse never takes stdin.

// TestTheDeclarationIsWhatTheSourceRegistered compares the capability
// declaration against the source's adapter row, field for field.
func TestTheDeclarationIsWhatTheSourceRegistered(t *testing.T) {
	t.Parallel()
	caps := New().Capabilities()

	if got := New().ID(); got != systemID {
		t.Errorf("ID() = %q, want %q", got, systemID)
	}
	if normalized, err := agentic.NormalizeSystemID(string(systemID)); err != nil || normalized != systemID {
		t.Errorf("the plugin id does not normalize to itself: %q -> %q (%v)", systemID, normalized, err)
	}
	if !caps.SupportsMode(agentic.LaunchModeExec) || !caps.SupportsMode(agentic.LaunchModeDryRun) {
		t.Errorf("the source registers muse with a BuildCommand and a DryRunArgs; declared modes are %v", caps.LaunchModes)
	}
	if caps.SupportsMode(agentic.LaunchModeManagedSession) {
		t.Error("muse declares a managed-session surface the source has no builder for and the goldens do not cover")
	}
	if caps.EffortTransport != agentic.EffortTransportNone {
		t.Errorf("effort transport is %s, want none; buildMuseArgs never references the effort", caps.EffortTransport)
	}
	if caps.SupportsGoal || caps.SupportsBudget || caps.SupportsServiceTier {
		t.Errorf("muse's adapter row declares goal/budget/service-tier all false; got %v/%v/%v", caps.SupportsGoal, caps.SupportsBudget, caps.SupportsServiceTier)
	}
	if caps.CompositionGrammar != agentic.GrammarNone {
		t.Errorf("composition grammar is %q, want none; the source's table gives muse CompositionGrammarNone", caps.CompositionGrammar)
	}
	if caps.HomeEnvVar != "" || caps.DefaultHome != "" {
		t.Errorf("muse declares home %q/%q; the source registers no providerHomeRules entry for muse", caps.HomeEnvVar, caps.DefaultHome)
	}
	if caps.AuthHint != "" {
		t.Errorf("muse declares the auth hint %q; the source's providerAuthHint has no muse entry, and an invented remediation is one nobody has seen work", caps.AuthHint)
	}
}

// TestThePluginRegistersIntoTheDefaultRegistry drives the production
// registration path: package init, into agentic.Default, through the public
// agentic.Register — which is what the CLI's `plugins` command reads.
//
// Registering into an isolated registry — which every other test here does, for
// parallelism — proves the plugin is REGISTRABLE and says nothing about whether
// it is registerED. Those are different facts and only the second one makes the
// plugin reachable from a binary.
func TestThePluginRegistersIntoTheDefaultRegistry(t *testing.T) {
	sys, ok := agentic.Default.Lookup(systemID)
	if !ok {
		t.Fatalf("%s is not in the default registry; importing this package is supposed to be the whole of installing it", systemID)
	}
	if sys.ID() != systemID {
		t.Errorf("the default registry serves %q under the key %q", sys.ID(), systemID)
	}
}

// TestNothingAboutTheVendorReachesThisPlugin is the Layer boundary, held to the
// declaration rather than only asserted in the package comment.
//
// Muse is the system whose broker the extraction source records as UNKNOWN.
// That is a Layer-2 fact, it is settled there, and a system plugin that grew an
// opinion about it — a hint mentioning a provider, a home keyed to one, a
// capability gated on which broker answers — would put a vendor decision in the
// one layer that must not hold one. Every field checked here is empty or false
// for reasons that have nothing to do with muse's vendor, and this test exists
// so that a future edit adding one has to explain itself.
func TestNothingAboutTheVendorReachesThisPlugin(t *testing.T) {
	t.Parallel()
	caps := New().Capabilities()
	if caps.AuthHint != "" {
		t.Errorf("the auth hint %q names a remediation, which is an authentication fact and therefore the vendor layer's", caps.AuthHint)
	}
	if caps.HomeEnvVar != "" {
		t.Errorf("the home variable %q is where an account lives, and which account is a vendor question", caps.HomeEnvVar)
	}
	// The plan surface itself: a launch must be expressible with no vendor
	// input beyond the model id and its effort axis, which is all
	// agentic.Model carries.
	if _, err := paritycase.TryBuildPlan(New(), launchRequest(t, "assignment"), agentic.LaunchModeExec); err != nil {
		t.Errorf("a muse launch could not be built from the harness facts alone: %v", err)
	}
}

// TestARequiredEffortModelIsRefused is the gate EffortTransportNone exists for.
//
// Every muse model the source registers is effort-none today, so this is a
// bound on the FUTURE: a model row added with a required effort must fail to
// launch here rather than run at whatever the harness picks while the
// operator's configured value reaches nothing.
func TestARequiredEffortModelIsRefused(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	req.Model.Effort = agentic.EffortSupportRequired
	req.Effort = "high"
	_, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrEffortNotTransportable) {
		t.Errorf("a required-effort model under muse was admitted (err=%v)", err)
	}
}

// TestAnEffortValueAloneIsRefused is the narrower half: even a model that
// requires nothing must not carry an effort under a transport that cannot
// deliver it.
func TestAnEffortValueAloneIsRefused(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	req.Effort = "high"
	_, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrEffortNotTransportable) {
		t.Errorf("an effort value under muse was admitted (err=%v); it would have been dropped silently", err)
	}
}

// TestAnEffortlessModelLaunches is the reachability half of the two refusals
// above.
func TestAnEffortlessModelLaunches(t *testing.T) {
	t.Parallel()
	if _, err := paritycase.TryBuildPlan(New(), launchRequest(t, "assignment"), agentic.LaunchModeExec); err != nil {
		t.Errorf("an ordinary muse launch was refused: %v", err)
	}
}

// TestTheUnsupportedParametersAreRefusedRatherThanDropped covers the other
// three declarations.
func TestTheUnsupportedParametersAreRefusedRatherThanDropped(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate func(*agentic.LaunchRequest)
		want   error
	}{
		"a goal": {
			mutate: func(r *agentic.LaunchRequest) {
				r.Goal = &agentic.Goal{ID: "GOAL-1", ProviderCondition: "own it"}
			},
			want: agentic.ErrGoalUnsupported,
		},
		"a budget": {
			mutate: func(r *agentic.LaunchRequest) { r.Budget = &agentic.Budget{USD: 5} },
			want:   agentic.ErrBudgetUnsupported,
		},
		"a service tier": {
			mutate: func(r *agentic.LaunchRequest) { r.ServiceTier = "priority" },
			want:   agentic.ErrServiceTierUnsupported,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			req := launchRequest(t, "assignment")
			c.mutate(&req)
			if _, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec); !errors.Is(err, c.want) {
				t.Errorf("%s was admitted under muse (err=%v), want %v", name, err, c.want)
			}
		})
	}
}

// TestEveryCompositionIsRefused is the GrammarNone gate, checked at BOTH lines:
// BuildPlan refuses on the declaration, and the plugin refuses when called
// directly.
//
// The second line is not redundant. A future declaration that gained a grammar
// without gaining a validator would make BuildPlan dispatch here, and a plugin
// that answered nil would admit every shape.
func TestEveryCompositionIsRefused(t *testing.T) {
	t.Parallel()
	compositions := map[string]agentic.Composition{
		"an mcp-config prefix another system would accept": {
			Prefix:  []string{"--mcp-config", `{"mcpServers":{"local":{"type":"stdio","command":"docs-server"}}}`},
			Servers: []agentic.CompositionServer{{Name: "local", Transport: "stdio"}},
		},
		"a prefix with no servers": {Prefix: []string{"--mcp-config", "{}"}},
		"servers with no prefix":   {Servers: []agentic.CompositionServer{{Name: "local", Transport: "stdio"}}},
		"an arbitrary argv prefix": {Prefix: []string{"--model", "something-else"}},
	}
	for name, composition := range compositions {
		t.Run(name, func(t *testing.T) {
			if err := New().ValidateComposition(composition); err == nil {
				t.Error("the plugin admitted a composition for a system that declares no grammar")
			}
			req := launchRequest(t, "assignment")
			req.Composition = composition
			if _, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrCompositionUnsupported) {
				t.Errorf("BuildPlan admitted the composition (err=%v); the prefix would be spliced into the launch", err)
			}
		})
	}
}

// TestAZeroCompositionIsNotAComposition is the reachability half: absence and
// an empty composition are the same fact.
func TestAZeroCompositionIsNotAComposition(t *testing.T) {
	t.Parallel()
	if err := New().ValidateComposition(agentic.Composition{}); err != nil {
		t.Errorf("the zero composition was refused: %v", err)
	}
}

// TestMuseNeverTakesStdin is the flat answer stated as a bound, over every
// request shape that might tempt a port to attach something.
//
// muse/exec's golden records stdin_kind "none" for one of these; the others are
// shapes no capture covers, and a conditional added later would reach them.
func TestMuseNeverTakesStdin(t *testing.T) {
	t.Parallel()
	requests := map[string]agentic.LaunchRequest{
		"with a prompt file": launchRequest(t, "assignment"),
		"with inline bytes":  withPrompt(launchRequest(t, ""), []byte("assignment")),
		"with neither":       launchRequest(t, ""),
		"with an unreadable prompt path": func() agentic.LaunchRequest {
			req := launchRequest(t, "")
			req.PromptPath = filepath.Join(t.TempDir(), "does-not-exist.md")
			return req
		}(),
	}
	for name, req := range requests {
		t.Run(name, func(t *testing.T) {
			payload, err := New().Stdin(req)
			if err != nil {
				t.Fatalf("Stdin returned an error for a system that reads no stdin: %v", err)
			}
			if payload.Attached || len(payload.Bytes) != 0 {
				t.Errorf("muse attached %d stdin bytes (attached=%v); the assignment reaches the child as the --prompt-file PATH, and a child receiving both reads it twice", len(payload.Bytes), payload.Attached)
			}
		})
	}
}

// TestAnUnreadablePromptPathIsNotThisPluginsProblem states the boundary the
// test above implies, so it is not read as an oversight.
//
// Every other ported system READS the assignment, so an unreadable file is a
// refusal there. Muse only names the path, so the harness is what discovers the
// file is missing — and a plugin that stat'ed it here would be performing work
// in a method BuildPlan calls for a DRY RUN, whose whole promise is that it
// touches nothing.
func TestAnUnreadablePromptPathIsNotThisPluginsProblem(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "")
	req.PromptPath = filepath.Join(t.TempDir(), "does-not-exist.md")
	plan, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("the plugin refused a launch naming a file it never reads: %v", err)
	}
	named := false
	for i := 1; i < len(plan.Argv); i++ {
		if plan.Argv[i-1] == "--prompt-file" && plan.Argv[i] == req.PromptPath {
			named = true
		}
	}
	if !named {
		t.Errorf("the assignment path did not reach argv: %v", plan.Argv)
	}
}

// TestTheTwoModesDifferOnlyInThePromptFile pins muse's dry-run mirror.
func TestTheTwoModesDifferOnlyInThePromptFile(t *testing.T) {
	t.Parallel()
	// The dry-run substitution happens only when the request carries NO path,
	// which is the source's condition: museDryRunArgs substitutes when
	// cfg.PromptFile is empty and passes the real path through otherwise.
	withFile := launchRequest(t, "assignment")
	launch, err := Args(withFile, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	dryWithFile, err := Args(withFile, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("Args(dry-run): %v", err)
	}
	if len(launch) != len(dryWithFile) {
		t.Fatalf("the modes differ in length for a request carrying a path:\n  exec:    %v\n  dry-run: %v", launch, dryWithFile)
	}
	for i := range launch {
		if launch[i] != dryWithFile[i] {
			t.Errorf("the modes differ at argument %d (%q vs %q) for a request that carries a real path", i, launch[i], dryWithFile[i])
		}
	}

	withoutFile := launchRequest(t, "")
	dry, err := Args(withoutFile, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("Args(dry-run): %v", err)
	}
	if dry[len(dry)-2] != "--prompt-file" || dry[len(dry)-1] != promptFilePlaceholder {
		t.Errorf("a dry run with no assignment file did not substitute the placeholder: %v", dry)
	}
}

// TestAnExecLaunchWithNoPromptOmitsTheFlagEntirely pins the other half of that
// condition: in EXEC mode there is no placeholder, so a request with no path
// gets no --prompt-file pair at all rather than a flag naming nothing.
func TestAnExecLaunchWithNoPromptOmitsTheFlagEntirely(t *testing.T) {
	t.Parallel()
	args, err := Args(launchRequest(t, ""), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	for _, arg := range args {
		if arg == "--prompt-file" || arg == promptFilePlaceholder {
			t.Errorf("an exec launch with no assignment emitted %q: %v", arg, args)
		}
	}
}

// TestAnAbsentWorkDirOmitsTheWorkspaceFlag pins the source's other conditional.
func TestAnAbsentWorkDirOmitsTheWorkspaceFlag(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	req.WorkDir = ""
	args, err := Args(req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	for _, arg := range args {
		if arg == "--workspace" {
			t.Errorf("the flag was emitted for a launch with no work directory: %v", args)
		}
	}
}

// TestAnUndeclaredModeIsRefusedByThePluginItself proves the plugin's own mode
// check, not only BuildPlan's.
func TestAnUndeclaredModeIsRefusedByThePluginItself(t *testing.T) {
	t.Parallel()
	if _, err := New().Argv(launchRequest(t, "assignment"), agentic.LaunchModeManagedSession); err == nil {
		t.Error("Argv built a managed-session argv for a system that declares no such mode")
	}
	if _, err := Args(launchRequest(t, "assignment"), agentic.LaunchMode(42)); err == nil {
		t.Error("Args built an argv for a launch mode that does not exist")
	}
}

// TestResolveBinaryRefusesAnEnvironmentWithNoPath: an absent PATH is a refusal,
// never a silent fallback onto the ambient process environment.
func TestResolveBinaryRefusesAnEnvironmentWithNoPath(t *testing.T) {
	t.Parallel()
	_, err := New().ResolveBinary(agentic.LaunchRequest{Env: []string{"HOME=/home/op"}})
	if !errors.Is(err, ErrNoPathInLaunchEnvironment) {
		t.Errorf("resolution with no PATH returned %v, want the no-PATH refusal", err)
	}
}

// TestTheDryRunReportsTheSameBinaryAsTheLaunch is the contract requirement the
// source's own bug broke: a display placeholder that drifts from the launch
// target.
func TestTheDryRunReportsTheSameBinaryAsTheLaunch(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	launch := paritycase.BuildPlan(t, New(), req, agentic.LaunchModeExec)
	dry := paritycase.BuildPlan(t, New(), req, agentic.LaunchModeDryRun)
	if launch.Binary != dry.Binary {
		t.Errorf("the dry run reports %q and a launch would run %q", dry.Binary, launch.Binary)
	}
}

// TestArgsDoesNotAliasTheCallersPrefix: a plugin that returned the caller's
// backing array would let its own appends write straight through into memory
// the caller still owns.
//
// The test is written against the BACKING ARRAY rather than against the
// returned slice, and that difference is the whole test. Checking `args[0]` and
// the caller's `prefix[0]` for divergence only works while the appends happen
// to fit in the existing capacity — the moment they overflow it, Go reallocates
// and an aliasing plugin looks correct. Here the caller keeps a longer array,
// hands over a two-element window of it, and asserts the rest of its own memory
// is untouched: an aliasing plugin overwrites it, whatever the capacity
// arithmetic does.
func TestArgsDoesNotAliasTheCallersPrefix(t *testing.T) {
	t.Parallel()
	const sentinel = "caller-owned"
	backing := make([]string, 64)
	for i := range backing {
		backing[i] = sentinel
	}
	backing[0], backing[1] = "--mcp-config", `{"mcpServers":{}}`

	req := launchRequest(t, "assignment")
	req.Composition = agentic.Composition{Prefix: backing[:2]}
	if _, err := Args(req, agentic.LaunchModeExec); err != nil {
		t.Fatalf("Args: %v", err)
	}
	for i := 2; i < len(backing); i++ {
		if backing[i] != sentinel {
			t.Fatalf("Args wrote %q into the caller's backing array at index %d; the plugin is appending through into memory the caller still holds", backing[i], i)
		}
	}
}

// launchRequest is an ordinary muse launch with a stub binary on its PATH.
// body empty means no prompt file is written and no path is set.
func launchRequest(t *testing.T, body string) agentic.LaunchRequest {
	t.Helper()
	binDir, workDir := t.TempDir(), t.TempDir()
	paritycase.WriteStubExecutable(t, binDir, executableName)
	req := agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel},
		WorkDir: workDir,
		Env:     []string{"PATH=" + binDir},
		Run:     agentic.RunContext{RunID: parityRunID, TaskID: parityTaskID},
	}
	if body != "" {
		req.PromptPath = paritycase.WritePromptFile(t, workDir, body)
	}
	return req
}

func withPrompt(req agentic.LaunchRequest, prompt []byte) agentic.LaunchRequest {
	req.Prompt = prompt
	return req
}
