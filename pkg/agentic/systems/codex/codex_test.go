package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestThePluginRegistersItselfIntoTheDefaultRegistry proves the init() binding
// is real.
//
// Importing this package IS the registration — that is the whole mechanism
// docs/architecture.md describes and the CLI's `plugins` command reads — and a
// plugin whose init silently failed would leave every launch reporting "no
// system registered" with nothing naming why. This test is the only thing that
// observes it, because the test binary's own import is what triggers it.
func TestThePluginRegistersItselfIntoTheDefaultRegistry(t *testing.T) {
	sys, ok := agentic.Default.Lookup(systemID)
	if !ok {
		t.Fatalf("%s is not in the default registry; importing this package is supposed to register it", systemID)
	}
	if _, isCodex := sys.(*System); !isCodex {
		t.Fatalf("the default registry holds a %T under %s", sys, systemID)
	}
}

// TestASecondRegistrationIsRefused holds the registry's own rule from this
// side: registering the plugin twice is a build-time bug, not an idempotent
// no-op, and init() panics on it rather than dropping the error.
func TestASecondRegistrationIsRefused(t *testing.T) {
	t.Parallel()
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := registry.Register(New()); !errors.Is(err, agentic.ErrDuplicateSystem) {
		t.Errorf("second Register returned %v, want %v", err, agentic.ErrDuplicateSystem)
	}
}

// TestTheIdentifierIsItsOwnNormalForm is what the registry refuses at the
// boundary, asserted here so a rename shows up in this package rather than as a
// registration failure in somebody else's.
func TestTheIdentifierIsItsOwnNormalForm(t *testing.T) {
	t.Parallel()
	normalized, err := agentic.NormalizeSystemID(string(New().ID()))
	if err != nil {
		t.Fatalf("NormalizeSystemID(%q): %v", New().ID(), err)
	}
	if normalized != New().ID() {
		t.Errorf("ID() = %q but normalizes to %q; a caller holding the plugin and a caller reading the registry would see two names for one system", New().ID(), normalized)
	}
	if New().ID() != New().ID() {
		t.Error("ID() is not stable across reads")
	}
}

// TestTheDeclaredCapabilitiesMatchTheSourceAdapter pins every field the source
// declared for codex.
//
// Each of these gates a refusal in BuildPlan, so a flipped bit is a launch that
// is silently refused or silently admitted. They are asserted individually
// rather than as one struct comparison so a failure names the capability.
func TestTheDeclaredCapabilitiesMatchTheSourceAdapter(t *testing.T) {
	t.Parallel()
	caps := New().Capabilities()

	for _, mode := range []agentic.LaunchMode{
		agentic.LaunchModeExec,
		agentic.LaunchModeDryRun,
		agentic.LaunchModeManagedSession,
	} {
		if !caps.SupportsMode(mode) {
			t.Errorf("codex does not declare %s, which the source's adapter had a surface for", mode)
		}
	}
	if caps.EffortTransport != agentic.EffortTransportArgv {
		t.Errorf("EffortTransport = %s, want argv: codex carries effort as a -c override", caps.EffortTransport)
	}
	if !caps.SupportsGoal {
		t.Error("SupportsGoal = false; the source's codex adapter declares goal support")
	}
	if caps.SupportsBudget {
		t.Error("SupportsBudget = true; codex has no budget flag and BuildPlan must refuse a budget rather than drop it")
	}
	if !caps.SupportsServiceTier {
		t.Error("SupportsServiceTier = false; codex is the one system in the source that carries a tier")
	}
	if caps.CompositionGrammar != GrammarTOMLConfigPairs {
		t.Errorf("CompositionGrammar = %q, want %q", caps.CompositionGrammar, GrammarTOMLConfigPairs)
	}
	if caps.HomeEnvVar != "CODEX_HOME" || caps.DefaultHome != "~/.codex" {
		t.Errorf("home declaration = (%q, %q), want (CODEX_HOME, ~/.codex); limit state is keyed by the home this resolves to and may not move without a demonstrated migration", caps.HomeEnvVar, caps.DefaultHome)
	}
	if caps.AuthHint == "" {
		t.Error("AuthHint is empty; the source captured a codex authentication failure and its remediation")
	}
}

// TestABudgetIsRefusedRatherThanDropped is the negative for the one capability
// codex declares FALSE.
//
// A launch that carried a budget codex cannot honour and ran anyway is a run
// that looks like the one that was asked for and is not, which is exactly the
// class of failure the capability flags exist to close.
func TestABudgetIsRefusedRatherThanDropped(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, "budget prompt")
	req.Env = []string{"PATH=" + binDir}
	req.Budget = &agentic.Budget{USD: 5}

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrBudgetUnsupported) {
		t.Errorf("BuildPlan with a budget returned %v, want %v", err, agentic.ErrBudgetUnsupported)
	}
}

// TestStdinCarriesThePromptFile is the transport, driven through BuildPlan.
func TestStdinCarriesThePromptFile(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	const body = "the assignment prompt\nwith a second line\n"
	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, body)
	req.Env = []string{"PATH=" + binDir}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if !plan.Stdin.Attached {
		t.Fatal("nothing was attached to stdin; codex reads its prompt from the stream the argv's trailing - names")
	}
	if string(plan.Stdin.Bytes) != body {
		t.Errorf("stdin = %q, want %q", plan.Stdin.Bytes, body)
	}
	if !containsString(plan.Argv, "-") {
		t.Error("the argv does not carry the trailing -, so the child would not read the stream that was attached")
	}
}

// TestAnUnreadablePromptIsAnErrorAndNotAnEmptyStream holds the distinction the
// whole Stdin method rests on.
//
// A read that FAILED is not an absence. Returning "nothing attached" for a
// permission error would launch codex on an empty stdin, and the run would look
// like a model that produced no work rather than like a file this process could
// not open.
func TestAnUnreadablePromptIsAnErrorAndNotAnEmptyStream(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.PromptPath = filepath.Join(workDir, "prompt-that-was-never-written.md")
	req.Env = []string{"PATH=" + binDir}

	if _, err := New().Stdin(req); err == nil {
		t.Fatal("a prompt file that does not exist produced no error")
	}

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	plan, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
	if err == nil {
		t.Fatalf("BuildPlan admitted a launch whose prompt could not be read, producing stdin %+v", plan.Stdin)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("BuildPlan reported %v, which does not name the missing file", err)
	}
}

// TestAnEmptyPromptFileIsAnAttachedEmptyStream is the other side of that line:
// a file that exists and is empty attaches an empty stream, which the child
// sees as EOF rather than as a closed descriptor.
func TestAnEmptyPromptFileIsAnAttachedEmptyStream(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, "")

	payload, err := New().Stdin(req)
	if err != nil {
		t.Fatalf("Stdin: %v", err)
	}
	if !payload.Attached {
		t.Error("an empty prompt FILE was reported as no stdin at all; the child sees a closed descriptor in one case and EOF in the other, and they are different runs")
	}
	if len(payload.Bytes) != 0 {
		t.Errorf("stdin = %q, want nothing", payload.Bytes)
	}
}

// TestADryRunAttachesNothingAndPerformsNoWork holds the mode's whole contract.
//
// BuildPlan calls Stdin for every mode, so a dry run whose prompt file does not
// exist yet must not fail — and it must not create one either. A dry run that
// performs work is the drift the source's own BuildArgs bug was made of.
func TestADryRunAttachesNothingAndPerformsNoWork(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.Env = []string{"PATH=" + binDir}

	before := readDirNames(t, workDir)
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeDryRun)
	if plan.Stdin.Attached {
		t.Errorf("the dry run attached %q to stdin", plan.Stdin.Bytes)
	}
	if after := readDirNames(t, workDir); len(after) != len(before) {
		t.Errorf("the dry run changed the working directory from %v to %v", before, after)
	}
}

// TestPromptBytesAreAnAlternativeTransport covers the caller that holds the
// text without a file, and pins the precedence between the two.
func TestPromptBytesAreAnAlternativeTransport(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)

	bytesOnly := parityRequest(workDir)
	bytesOnly.Prompt = []byte("prompt from memory")
	payload, err := New().Stdin(bytesOnly)
	if err != nil {
		t.Fatalf("Stdin: %v", err)
	}
	if !payload.Attached || string(payload.Bytes) != "prompt from memory" {
		t.Errorf("stdin = %+v, want the supplied bytes attached", payload)
	}

	both := parityRequest(workDir)
	both.Prompt = []byte("prompt from memory")
	both.PromptPath = writePromptFile(t, workDir, "prompt from disk")
	payload, err = New().Stdin(both)
	if err != nil {
		t.Fatalf("Stdin: %v", err)
	}
	if string(payload.Bytes) != "prompt from disk" {
		t.Errorf("stdin = %q, want the FILE to win; the source opens the prompt file and the precedence has to be one thing, stated", payload.Bytes)
	}
}

// TestStdinDoesNotAliasTheCallersPromptBytes keeps a launcher from writing
// through the plan into the caller's buffer.
func TestStdinDoesNotAliasTheCallersPromptBytes(t *testing.T) {
	t.Parallel()
	prompt := []byte("prompt from memory")
	req := parityRequest(tempSlot(t))
	req.Prompt = prompt

	payload, err := New().Stdin(req)
	if err != nil {
		t.Fatalf("Stdin: %v", err)
	}
	payload.Bytes[0] = 'X'
	if prompt[0] == 'X' {
		t.Error("the payload aliases the caller's slice; a launcher that rewrites the buffer rewrites the caller's prompt")
	}
}

// TestARequiredEffortWithNoValueIsRefused holds the core rule at this plugin's
// boundary: codex CAN carry an effort, so a required-effort model with none
// supplied must be refused rather than launched on the harness default.
func TestARequiredEffortWithNoValueIsRefused(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.Effort = ""
	req.PromptPath = writePromptFile(t, workDir, "effortless prompt")
	req.Env = []string{"PATH=" + binDir}

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrEffortMissing) {
		t.Errorf("BuildPlan returned %v, want %v; a required effort dropped on the floor is a wrong-cost launch that looks like a configured one", err, agentic.ErrEffortMissing)
	}
}

// TestTheDeclaredHomeReachesThePlan pins that a launch with no home override
// lands on the declared default rather than on nothing — on-disk limit state is
// keyed by it.
func TestTheDeclaredHomeReachesThePlan(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, "home prompt")
	req.Env = []string{"PATH=" + binDir}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if plan.Home != New().Capabilities().DefaultHome {
		t.Errorf("Plan.Home = %q, want the declared default %q", plan.Home, New().Capabilities().DefaultHome)
	}

	req.Home = "/tmp/other-codex-home"
	if plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec); plan.Home != "/tmp/other-codex-home" {
		t.Errorf("Plan.Home = %q, want the override", plan.Home)
	}
}

func readDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
