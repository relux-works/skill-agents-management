package claude

import (
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/launchenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// childEnvFor drives the production call site — a real registry, BuildPlan —
// and returns the child environment as a map, so a test asserts over the
// environment a launch would ACTUALLY hand the child rather than over a helper
// nobody calls.
func childEnvFor(t *testing.T, parentEnv []string) map[string]string {
	t.Helper()
	binDir, workDir := tempSlot(t), tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, "env contract prompt")
	req.Env = append(append([]string(nil), parentEnv...), "PATH="+binDir)

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	out := make(map[string]string, len(plan.Env))
	for _, entry := range plan.Env {
		key, value, _ := strings.Cut(entry, "=")
		out[key] = value
	}
	return out
}

// TestTheClaudeChildDoesNotInheritTheSessionMarker is the strip contract,
// driven through BuildPlan.
//
// The key is SEEDED with a value, so "absent from the child" is a measurement
// rather than a coincidence of a key nobody set — an absence and a failure to
// seed are different facts, and only the first one proves a strip.
func TestTheClaudeChildDoesNotInheritTheSessionMarker(t *testing.T) {
	t.Parallel()
	child := childEnvFor(t, []string{
		sessionMarkerEnv + "=1",
		"JIRA_TOKEN=must-remain-inherited",
	})
	if value, present := child[sessionMarkerEnv]; present {
		t.Errorf("the child inherited %s=%q; Claude Code reads it to detect nesting and refuses to start inside another session, which is the launch this strip exists to make possible", sessionMarkerEnv, value)
	}
	if child["JIRA_TOKEN"] != "must-remain-inherited" {
		t.Errorf("an unrelated parent variable did not survive; the filter is removing more than the one key it declares")
	}
}

// TestTheStripIsAnExactKeyNotAPrefix is the same bound stated directly, without
// a fixture in the way.
//
// It is not a duplicate of TestAPrefixStripFailsAgainstTheClaudeGolden: that
// one proves the GOLDEN can catch a prefix port, this one proves the shipped
// filter is not one. A fixture that lost its near-miss key would fail the first
// and leave the second untouched.
func TestTheStripIsAnExactKeyNotAPrefix(t *testing.T) {
	t.Parallel()
	child := childEnvFor(t, []string{
		sessionMarkerEnv + "=1",
		sessionMarkerEnv + "_LIKE_BUT_NOT=keep-me",
		"NOT_" + sessionMarkerEnv + "=keep-me-too",
	})
	for _, near := range []string{sessionMarkerEnv + "_LIKE_BUT_NOT", "NOT_" + sessionMarkerEnv} {
		if child[near] != "keep-me" && child[near] != "keep-me-too" {
			t.Errorf("the child lost %q, which only a prefix or substring match could remove", near)
		}
	}
}

// TestTheClaudeChildKeepsTheCodexFamily pins the ASYMMETRY this port's most
// plausible defect would erase.
//
// A port author who read the codex plugin first will reach for
// filterRuntimeEnv's eleven keys. The source does not: filterCodexRuntimeEnv is
// a CODEX child's filter, filterQwenRuntimeEnv composes it with CLAUDECODE for a
// QWEN child, and claude's is the CLAUDECODE strip alone. The goldens agree —
// every one of these keys is seeded in parent_env and none appears in
// env_removed — and this test says so where a reader will look for it.
//
// It is a residual, not a virtue: the credentials the two *_AUTH_TOKEN_ENV
// pointers name reach every claude child. That is the source's own
// BUG-260819-3qn52o, in backlog and unfixed at the captured commit, and closing
// it here would diverge this plugin from the source while the source's bug
// stayed open.
func TestTheClaudeChildKeepsTheCodexFamily(t *testing.T) {
	t.Parallel()
	parent := []string{
		"CODEX_THREAD_ID=thread-parent",
		"CODEX_SESSION=session-parent",
		"CODEX_CI=1",
		"CODEX_MANAGED_BY_NPM=1",
		"CODEX_MANAGED_BY_BUN=1",
		"CODEX_MANAGED_PACKAGE_ROOT=/parent/managed/root",
		"TASK_BOARD_CODEX_APP_SERVER_URL=ws://127.0.0.1:1234",
		"TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV=PARENT_APP_SERVER_TOKEN",
		"PARENT_APP_SERVER_TOKEN=app-server-secret",
		"TASK_BOARD_SESSION_MANAGER_URL=unix:///tmp/manager.sock",
		"TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV=PARENT_SESSION_MANAGER_TOKEN",
		"PARENT_SESSION_MANAGER_TOKEN=manager-secret",
		"TASK_BOARD_SESSION_ID=SESSION-parent",
	}
	child := childEnvFor(t, parent)
	for _, entry := range parent {
		key, value, _ := strings.Cut(entry, "=")
		got, present := child[key]
		if !present {
			t.Errorf("the claude child lost %q; the source's claude filter strips CLAUDECODE and nothing else, so a strip here is a divergence from the behaviour the goldens pin", key)
			continue
		}
		if got != value {
			t.Errorf("the claude child rewrote %q from %q to %q", key, value, got)
		}
	}
}

// TestTheClaudeChildPathIsNotSanitized is the one environment claim the parity
// goldens CANNOT hold.
//
// The capture harness excludes PATH from its diff — it is seeded with a temp
// directory and differs run to run — and the goldens' own README names the
// consequence: "a port that changes what it strips from PATH is outside what
// these goldens prove. That needs its own test." This is that test, and its
// claim is the opposite of codex's: claude sanitizes NOTHING, because
// filterEnv touches only the key it is handed and no claude path in the source
// ever calls sanitizeCodexPath.
//
// The entries below are exactly the two shapes codex's isRuntimePathEntry
// removes, so a port that reused codex's filter fails here rather than passing
// every golden while silently rewriting the child's PATH.
func TestTheClaudeChildPathIsNotSanitized(t *testing.T) {
	t.Parallel()
	binDir, workDir := tempSlot(t), tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	codexShaped := []string{
		"/parent/home/.codex/tmp/arg0/session-1",
		"/opt/tooling/codex-path",
		binDir,
	}
	want := strings.Join(codexShaped, string(os.PathListSeparator))

	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, "path contract prompt")
	req.Env = []string{"PATH=" + want}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	got, present := launchenv.Lookup(plan.Env, "PATH")
	if !present {
		t.Fatal("the child inherited no PATH at all; the filter dropped the entry rather than passing it through")
	}
	if got != want {
		t.Errorf("the claude child's PATH was rewritten:\n  parent: %q\n  child:  %q\nthe source's claude filter rewrites no PATH entry, and the goldens cannot see this because PATH is excluded from their diff", want, got)
	}
}

// TestEveryStrippedKeyIsCarriedByTheGolden narrows the blocked list itself: one
// key at a time is removed from it and the claude golden must then FAIL.
//
// The list has one entry today, and the narrowing is worth exactly as much for
// that: without it, CLAUDECODE=1 appearing in the golden's env_removed would be
// equally consistent with the run-context injection having removed it, and the
// only strip this plugin performs would have no evidence naming it. The loop is
// over the list rather than over its single element so a later addition arrives
// with the same measurement rather than with none.
func TestEveryStrippedKeyIsCarriedByTheGolden(t *testing.T) {
	c := parityCaseFor(t, "claude/prompt-mode")

	if len(runtimeEnvKeys) == 0 {
		t.Fatal("the blocked list is empty, so this narrowing ranges over nothing")
	}
	for _, dropped := range runtimeEnvKeys {
		t.Run(dropped, func(t *testing.T) {
			g, dirs, req := prepareParityCase(t, c)
			narrowed := narrowedFilterSystem{System: New(), stopStripping: dropped}
			plan := buildParityPlan(t, narrowed, req, c.mode)
			diffs := parity.ComparePlan(g, plan, dirs.substitutions())
			if len(diffs) == 0 {
				t.Fatalf("a claude plugin that stops stripping %s still byte-matches the golden, so nothing in the fixture distinguishes that entry from an entry that was never there", dropped)
			}
			if !namesField(diffs, "EnvRemoved") {
				t.Errorf("narrowing the filter was reported as %v rather than as an environment difference", diffs)
			}
		})
	}
}

// narrowedFilterSystem is the real plugin with ONE key put back into the child,
// which is the smallest possible weakening of the filter.
type narrowedFilterSystem struct {
	*System
	stopStripping string
}

func (n narrowedFilterSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := n.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	value, present := launchenv.Lookup(parent, n.stopStripping)
	if !present {
		return env, nil
	}
	return append(env, n.stopStripping+"="+value), nil
}

// TestTheRunContextReachesTheChild is the injection half of the contract.
func TestTheRunContextReachesTheChild(t *testing.T) {
	t.Parallel()
	child := childEnvFor(t, []string{
		agentic.EnvRunID + "=RUN-parent",
		agentic.EnvTaskID + "=TASK-parent",
		agentic.EnvLegacyBoardDir + "=/parent/.task-board",
	})
	want := map[string]string{
		agentic.EnvRunID:  parityPromptRunID,
		agentic.EnvTaskID: parityPromptTaskID,
	}
	for key, value := range want {
		if child[key] != value {
			t.Errorf("child[%s] = %q, want %q", key, child[key], value)
		}
	}
	// The parity request carries no board directory, so the parent's must be
	// REMOVED rather than inherited. A child that kept it would resolve the
	// parent's board while reporting the run it was launched for.
	if value, present := child[agentic.EnvLegacyBoardDir]; present {
		t.Errorf("child[%s] = %q; this launch configured no board directory, so the parent's must not survive", agentic.EnvLegacyBoardDir, value)
	}
}

// TestAGoalBoundLaunchExportsItsGoalID is the injection the goal-mode golden
// records and the prompt-mode one does not.
//
// It is the environment half of goal binding: the directive tells the child to
// run `task-board spawn goal <run>`, and TASK_BOARD_DELIVERY_GOAL_ID is how the
// child knows which goal it is bound to. A launch with no goal must EXPORT
// NOTHING rather than a blank, because a child reading an empty goal id has a
// goal that is present and meaningless.
func TestAGoalBoundLaunchExportsItsGoalID(t *testing.T) {
	t.Parallel()
	binDir, workDir := tempSlot(t), tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir, parityGoalRunID, parityGoalTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityGoalBody)
	req.Goal = parityGoal()
	req.Env = []string{"PATH=" + binDir, agentic.EnvDeliveryGoalID + "=GOAL-parent"}

	bound := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if value, _ := launchenv.Lookup(bound.Env, agentic.EnvDeliveryGoalID); value != parityGoalID {
		t.Errorf("a goal-bound child got %s=%q, want %q", agentic.EnvDeliveryGoalID, value, parityGoalID)
	}

	req.Goal = nil
	unbound := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if value, present := launchenv.Lookup(unbound.Env, agentic.EnvDeliveryGoalID); present {
		t.Errorf("an unbound child inherited %s=%q from its parent; absence and a blank are different facts and only absence says 'this launch has no goal'", agentic.EnvDeliveryGoalID, value)
	}
}

// TestNoStrippedKeyCollidesWithAnInjectedOne pins the non-overlap env.go's
// filter-then-inject ordering comment rests on.
//
// The two orders produce identical environments today, which is exactly why the
// claim needs a test rather than a sentence: the day a blocked key collides with
// an injected one, the wrong order deletes what the injection just wrote, and
// this fails naming the comment instead of a child losing its run id.
func TestNoStrippedKeyCollidesWithAnInjectedOne(t *testing.T) {
	t.Parallel()
	injected := map[string]bool{
		agentic.EnvRunID:          true,
		agentic.EnvTaskID:         true,
		agentic.EnvBoardDir:       true,
		agentic.EnvLegacyBoardDir: true,
		agentic.EnvDeliveryGoalID: true,
		agentic.EnvContextID:      true,
	}
	for _, key := range runtimeEnvKeys {
		if injected[key] {
			t.Errorf("%q is both stripped and injected; env.go's filter-then-inject order is now load-bearing and its comment says it is not", key)
		}
	}
}
