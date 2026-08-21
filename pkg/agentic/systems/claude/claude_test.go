package claude

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestThePluginRegistersIntoTheDefaultRegistry drives the production
// registration path: package init, into agentic.Default, which is what the
// CLI's `plugins` command reads.
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

// TestTheIDNormalizesToItselfAndIsStable is what Registry.Register checks, held
// here as well so a failure names this plugin rather than a generic refusal.
func TestTheIDNormalizesToItselfAndIsStable(t *testing.T) {
	t.Parallel()
	normalized, err := agentic.NormalizeSystemID(string(systemID))
	if err != nil {
		t.Fatalf("NormalizeSystemID(%q): %v", systemID, err)
	}
	if normalized != systemID {
		t.Errorf("the plugin id %q normalizes to %q; the registry key and the value a caller holds would then be two spellings of one system", systemID, normalized)
	}
	if New().ID() != New().ID() {
		t.Error("ID() is not stable across calls")
	}
}

// TestTheDeclaredCapabilities holds the adapter row this port carries.
//
// Each field is one line of the source's adapterTable entry for AgentClaude
// (adapter.go), and each is load-bearing somewhere a golden cannot see:
// BuildPlan refuses a launch parameter a system does not support, so a wrong
// flag here is a launch admitted or rejected for the wrong reason rather than a
// byte that differs.
func TestTheDeclaredCapabilities(t *testing.T) {
	t.Parallel()
	caps := New().Capabilities()

	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		if !caps.SupportsMode(mode) {
			t.Errorf("the source registers claude with both a BuildCommand and a DryRunArgs, but %s is not declared", mode)
		}
	}
	if caps.SupportsMode(agentic.LaunchModeManagedSession) {
		t.Error("managed-session is declared with no construction behind it; the source's adapter table has no managed-session args builder for claude, and cmd/claude_manager.go filters argv a human typed rather than constructing it")
	}
	if caps.EffortTransport != agentic.EffortTransportArgv {
		t.Errorf("effort transport is %s; the source's adapter declares EffortTransportArgv and buildClaudeArgs spells `--effort`", caps.EffortTransport)
	}
	if !caps.SupportsGoal {
		t.Error("goal support is off; claude is the system the whole /goal directive path exists for")
	}
	if !caps.SupportsBudget {
		t.Error("budget support is off; claude is the ONE agent in the source's table whose adapter sets SupportsBudget")
	}
	if caps.SupportsServiceTier {
		t.Error("service-tier support is on; the source's claude adapter declares SupportsServiceTier false and buildClaudeArgs spells no tier flag")
	}
	if caps.CompositionGrammar != GrammarMCPConfigJSON {
		t.Errorf("composition grammar is %q, want %q", caps.CompositionGrammar, GrammarMCPConfigJSON)
	}
	// The one pair here that no golden can see and that the code itself calls
	// load-bearing: system.go documents that on-disk limit state is keyed by
	// (provider, home), and AGENTS.md's ground rules say that identity must
	// never move silently. Capabilities' own comment says moving either value
	// needs a demonstrated migration on real state files — which is worth
	// exactly nothing unless something fails when they move. Codex pins the
	// identical pair (codex_test.go's TestTheDeclaredCapabilitiesMatchTheSource
	// Adapter); the same mutant applied here used to survive the whole suite.
	if caps.HomeEnvVar != "CLAUDE_CONFIG_DIR" || caps.DefaultHome != "~/.claude" {
		t.Errorf("home declaration = (%q, %q), want (CLAUDE_CONFIG_DIR, ~/.claude); these are what the Claude Code CLI reads, limit state is keyed by the home they resolve to, and moving either needs a demonstrated migration on real state files rather than an edit", caps.HomeEnvVar, caps.DefaultHome)
	}
	if caps.AuthHint == "" {
		t.Error("the auth hint is empty; the source's providerAuthHint table carries one for claude, and an empty hint asserts no remediation has been observed")
	}
}

// TestServiceTierIsRefusedRatherThanDropped is the consequence of the
// declaration above, measured at the boundary a caller meets.
//
// A system that dropped an unsupported parameter would launch a run that looks
// like the one that was asked for and is not.
func TestServiceTierIsRefusedRatherThanDropped(t *testing.T) {
	t.Parallel()
	req := parityRequest(tempSlot(t), parityPromptRunID, parityPromptTaskID)
	req.ServiceTier = "priority"
	if err := planErrorFor(t, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrServiceTierUnsupported) {
		t.Errorf("BuildPlan returned %v, want %v", err, agentic.ErrServiceTierUnsupported)
	}
}

// TestAGoalBoundLaunchAttachesNoStdin is the mode-dependence that separates
// claude from every other ported system.
//
// The source sets cmd.Stdin only when cfg.LaunchGoal is nil. A goal-bound child
// that also received the assignment on stdin would get it twice — once as
// system context through --append-system-prompt-file and once as its user turn
// — which is a different run from the one the source performs.
//
// The prompt-mode half is the narrowing: with the SAME prompt file and the goal
// removed, the bytes must appear. Without it, "no stdin" would be equally
// consistent with a plugin that never attaches anything.
func TestAGoalBoundLaunchAttachesNoStdin(t *testing.T) {
	t.Parallel()
	binDir, workDir := tempSlot(t), tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir, parityGoalRunID, parityGoalTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityGoalBody)
	req.Env = []string{"PATH=" + binDir}
	req.Goal = parityGoal()

	bound := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if bound.Stdin.Attached || len(bound.Stdin.Bytes) != 0 {
		t.Errorf("a goal-bound launch attached %d stdin bytes (attached=%v); the assignment already reached the child as system context", len(bound.Stdin.Bytes), bound.Stdin.Attached)
	}

	req.Goal = nil
	unbound := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if !unbound.Stdin.Attached || string(unbound.Stdin.Bytes) != parityGoalBody {
		t.Errorf("with the goal removed the same prompt file produced attached=%v %q, want the file's bytes; the two modes are not being told apart by the goal", unbound.Stdin.Attached, unbound.Stdin.Bytes)
	}
}

// TestAnUnreadablePromptIsAnErrorNotAnEmptyStdin holds the difference between
// an absence and a failure to read.
//
// Returning "nothing attached" for a permissions error would launch a claude
// child that sits on an empty stdin and reports the model produced no work —
// a failure that looks like the model refusing the task rather than like a file
// nobody could open.
func TestAnUnreadablePromptIsAnErrorNotAnEmptyStdin(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = filepath.Join(workDir, "does-not-exist.md")

	if err := planErrorFor(t, req, agentic.LaunchModeExec); err == nil {
		t.Fatal("a missing assignment prompt produced a plan rather than a refusal")
	}

	unreadable := filepath.Join(workDir, "locked.md")
	if err := os.WriteFile(unreadable, []byte("secret"), 0o200); err != nil {
		t.Fatalf("writing the unreadable prompt: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a 0200 file is readable, so the permissions half of this test cannot measure anything")
	}
	req.PromptPath = unreadable
	if err := planErrorFor(t, req, agentic.LaunchModeExec); err == nil {
		t.Error("an unreadable assignment prompt produced a plan rather than a refusal")
	}
}

// TestNoPromptAtAllAttachesNothing is the state a dry run is in, and it must
// not be an error: BuildPlan calls Stdin for every mode, and a dry run that
// failed because no prompt file exists yet would be a dry run performing work.
func TestNoPromptAtAllAttachesNothing(t *testing.T) {
	t.Parallel()
	req := parityRequest(tempSlot(t), parityPromptRunID, parityPromptTaskID)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req.Env = []string{"PATH=" + binDir}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeDryRun)
	if plan.Stdin.Attached || len(plan.Stdin.Bytes) != 0 {
		t.Errorf("a launch with no prompt attached %d bytes (attached=%v)", len(plan.Stdin.Bytes), plan.Stdin.Attached)
	}
}

// TestPromptBytesAreCopied is the aliasing bound on the byte-carrying path: a
// caller that reuses its buffer must not find the plan's stdin rewritten under
// it.
func TestPromptBytesAreCopied(t *testing.T) {
	t.Parallel()
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	body := []byte("prompt bytes")
	req := parityRequest(tempSlot(t), parityPromptRunID, parityPromptTaskID)
	req.Prompt = body
	req.Env = []string{"PATH=" + binDir}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	body[0] = 'X'
	if string(plan.Stdin.Bytes) != "prompt bytes" {
		t.Errorf("the plan's stdin aliased the caller's buffer: %q", plan.Stdin.Bytes)
	}
}
