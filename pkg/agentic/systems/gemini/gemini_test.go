package gemini

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file holds the plugin's declaration and the surfaces no golden pins: the
// refusals, and the one-argument difference between the two modes.

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
		t.Errorf("the source registers gemini with a BuildCommand and a DryRunArgs; declared modes are %v", caps.LaunchModes)
	}
	if caps.SupportsMode(agentic.LaunchModeManagedSession) {
		t.Error("gemini declares a managed-session surface the source has no builder for and the goldens do not cover")
	}
	if caps.EffortTransport != agentic.EffortTransportNone {
		t.Errorf("effort transport is %s, want none; buildGeminiArgs never references the effort", caps.EffortTransport)
	}
	if caps.SupportsGoal || caps.SupportsBudget || caps.SupportsServiceTier {
		t.Errorf("gemini's adapter row declares goal/budget/service-tier all false; got %v/%v/%v", caps.SupportsGoal, caps.SupportsBudget, caps.SupportsServiceTier)
	}
	if caps.CompositionGrammar != agentic.GrammarNone {
		t.Errorf("composition grammar is %q, want none; the source's table gives gemini CompositionGrammarNone", caps.CompositionGrammar)
	}
	if caps.HomeEnvVar != "" || caps.DefaultHome != "" {
		t.Errorf("gemini declares home %q/%q; the source registers no providerHomeRules entry for gemini", caps.HomeEnvVar, caps.DefaultHome)
	}
	const wantHint = "Gemini authentication is unavailable; set `GEMINI_API_KEY` or run Gemini CLI interactively to sign in, then retry the goal-bound spawn"
	if caps.AuthHint != wantHint {
		t.Errorf("auth hint is %q, want the source's providerAuthHint entry rendered as Message + \"; \" + Remediation", caps.AuthHint)
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

// TestARequiredEffortModelIsRefused is the gate EffortTransportNone exists for,
// measured against THIS plugin's declaration through the real dispatch.
//
// The refusal is what stops a launch that looks like the one that was asked for
// and is not: gemini has no way to carry an effort, so admitting the model would
// run it at whatever the harness defaults to while the operator's configured
// value reached nothing.
func TestARequiredEffortModelIsRefused(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	req.Model.Effort = agentic.EffortSupportRequired
	req.Effort = "high"
	_, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrEffortNotTransportable) {
		t.Errorf("a required-effort model under gemini was admitted (err=%v)", err)
	}
}

// TestAnEffortValueAloneIsRefused is the narrower half: even a model that
// requires nothing must not carry an effort under a transport that cannot
// deliver it. Without this, a caller could pass an effort, watch it vanish and
// see a successful launch.
func TestAnEffortValueAloneIsRefused(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	req.Effort = "high"
	_, err := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrEffortNotTransportable) {
		t.Errorf("an effort value under gemini was admitted (err=%v); it would have been dropped silently", err)
	}
}

// TestAnEffortlessModelLaunches is the reachability half of the two refusals
// above: without it, they would be equally consistent with a plugin that
// refuses every launch.
func TestAnEffortlessModelLaunches(t *testing.T) {
	t.Parallel()
	if _, err := paritycase.TryBuildPlan(New(), launchRequest(t, "assignment"), agentic.LaunchModeExec); err != nil {
		t.Errorf("an ordinary gemini launch was refused: %v", err)
	}
}

// TestTheUnsupportedParametersAreRefusedRatherThanDropped covers the other
// three declarations. Each is a refusal rather than a drop for the same reason
// as effort: a dropped parameter produces a run that looks like the one that
// was asked for and is not.
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
				t.Errorf("%s was admitted under gemini (err=%v), want %v", name, err, c.want)
			}
		})
	}
}

// TestEveryCompositionIsRefused is the GrammarNone gate, and it is checked at
// BOTH lines: BuildPlan refuses on the declaration, and the plugin refuses when
// called directly.
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
// an empty composition are the same fact, and refusing the zero value would
// refuse every ordinary launch.
func TestAZeroCompositionIsNotAComposition(t *testing.T) {
	t.Parallel()
	if err := New().ValidateComposition(agentic.Composition{}); err != nil {
		t.Errorf("the zero composition was refused: %v", err)
	}
}

// TestTheTwoModesDifferInExactlyOneArgument pins the whole of gemini's
// dry-run mirror: the `-p` value, and nothing else.
func TestTheTwoModesDifferInExactlyOneArgument(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	launch, err := Args(req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	dry, err := Args(req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("Args(dry-run): %v", err)
	}
	if len(launch) != len(dry) {
		t.Fatalf("the two modes built different argv lengths:\n  exec:    %v\n  dry-run: %v", launch, dry)
	}
	differing := 0
	for i := range launch {
		if launch[i] != dry[i] {
			differing++
			if launch[i-1] != "-p" {
				t.Errorf("the modes differ at argument %d (%q vs %q), which does not follow -p", i, launch[i], dry[i])
			}
		}
	}
	if differing != 1 {
		t.Errorf("the two modes differ in %d arguments, want exactly one", differing)
	}
	if dry[1] != promptPlaceholder {
		t.Errorf("the dry run passes -p %q, want the source's placeholder %q", dry[1], promptPlaceholder)
	}
	if launch[1] != "" {
		t.Errorf("the launch passes -p %q; it must be EMPTY, because gemini appends what it reads on stdin to this value and the child would read the assignment twice", launch[1])
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

// TestAnAbsentWorkDirOmitsTheFlagEntirely pins the source's conditional: no
// work directory, no --include-directories pair. A port that emitted the flag
// with an empty value would hand the child a directory argument naming nothing.
func TestAnAbsentWorkDirOmitsTheFlagEntirely(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	req.WorkDir = ""
	args, err := Args(req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	for _, arg := range args {
		if arg == "--include-directories" {
			t.Errorf("the flag was emitted for a launch with no work directory: %v", args)
		}
	}
}

// TestAnAbsentPromptAttachesNothing is what makes a dry run side-effect free:
// BuildPlan calls Stdin for every mode, and a dry run has no assignment file
// yet.
func TestAnAbsentPromptAttachesNothing(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "")
	payload, err := New().Stdin(req)
	if err != nil {
		t.Fatalf("Stdin refused a launch carrying no assignment: %v", err)
	}
	if payload.Attached || len(payload.Bytes) != 0 {
		t.Errorf("a launch with no assignment attached %d stdin bytes (attached=%v)", len(payload.Bytes), payload.Attached)
	}
}

// TestAnUnreadablePromptIsAnErrorRatherThanAnAbsence is the distinction the
// stdin transport rests on: a failed read is not a legitimate absence.
//
// For gemini the consequence is sharper than for any other system: `-p` is
// already empty by design, so a plugin that answered "nothing attached" here
// would launch a child with NO assignment in either place.
func TestAnUnreadablePromptIsAnErrorRatherThanAnAbsence(t *testing.T) {
	t.Parallel()
	req := launchRequest(t, "assignment")
	req.PromptPath = filepath.Join(t.TempDir(), "does-not-exist.md")

	payload, err := New().Stdin(req)
	if err == nil {
		t.Fatalf("an unreadable assignment produced a payload rather than an error (attached=%v, %d bytes)", payload.Attached, len(payload.Bytes))
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refusal does not wrap the read failure: %v", err)
	}
	if _, planErr := paritycase.TryBuildPlan(New(), req, agentic.LaunchModeExec); planErr == nil {
		t.Error("BuildPlan admitted a launch whose assignment could not be read; the gate is not reached from the dispatch surface")
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
	if !strings.HasSuffix(launch.Binary, string(filepath.Separator)+executableName) {
		t.Errorf("the launch resolved %q, which is not the stub this test put on PATH", launch.Binary)
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

// launchRequest is an ordinary gemini launch with a stub binary on its PATH and
// an assignment on disk. body empty means no prompt file is written.
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
