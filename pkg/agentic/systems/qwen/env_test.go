package qwen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/internal/runtimeenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// childEnvFor drives the real plugin over a parent environment and returns the
// child as a map.
//
// It goes through agentic.BuildPlan rather than calling childEnv directly: a
// filter that is unit-tested but not reached from the dispatch surface promises
// nothing about the environment a launch would actually get.
func childEnvFor(t *testing.T, parent []string) map[string]string {
	t.Helper()
	// The plugin resolves its binary from the environment it is HANDED, so the
	// launch environment has to carry a PATH with a qwen on it. Nothing here
	// reads the ambient process environment.
	binDir := t.TempDir()
	paritycase.WriteStubExecutable(t, binDir, executableName)
	req := agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
		Effort:  parityEffort,
		Env:     paritycase.WithPathEntry(parent, binDir),
		Prompt:  []byte("assignment"),
		WorkDir: t.TempDir(),
		Run:     agentic.RunContext{RunID: parityRunID, TaskID: parityTaskID},
	}
	plan := paritycase.BuildPlan(t, New(), req, agentic.LaunchModeExec)
	return paritycase.EnvMap(plan.Env)
}

// TestTheQwenChildLosesTheParentClaudeMarker is the fix this plugin exists to
// preserve, stated directly.
//
// The source's TASK-260817-2eo4ok added CLAUDECODE to qwen's filter because a
// qwen child launched from inside a Claude Code session inherited the nesting
// marker and refused to start. The golden narrowing below proves the fixture
// can see it; this proves the shipped plugin does it.
func TestTheQwenChildLosesTheParentClaudeMarker(t *testing.T) {
	t.Parallel()
	child := childEnvFor(t, []string{sessionMarkerEnv + "=1", "KEEP=me"})
	if _, present := child[sessionMarkerEnv]; present {
		t.Errorf("the qwen child inherited %s; that is the fixed leak reopening — a qwen child that sees it believes it is nested and refuses to start", sessionMarkerEnv)
	}
	if _, present := child["KEEP"]; !present {
		t.Error("an unrelated variable was stripped, so this test measured a wipe rather than a strip")
	}
}

// TestTheQwenChildLosesTheParentCodexRuntime is the other half of the filter:
// every key the codex family contributes.
func TestTheQwenChildLosesTheParentCodexRuntime(t *testing.T) {
	t.Parallel()
	var parent []string
	for _, key := range runtimeenv.Keys() {
		parent = append(parent, key+"=parent-value")
	}
	child := childEnvFor(t, parent)
	for _, key := range runtimeenv.Keys() {
		if _, present := child[key]; present {
			t.Errorf("the qwen child inherited %q; filterQwenRuntimeEnv wraps the codex filter and that key is in it", key)
		}
	}
}

// TestTheCredentialPointersAreResolvedThroughThePlugin proves the indirection
// survives the port at the plugin's own surface: the two token-NAME variables
// hold the NAME of the variable that holds the token, and stripping only the
// pointer would leave the credential in the child.
func TestTheCredentialPointersAreResolvedThroughThePlugin(t *testing.T) {
	t.Parallel()
	child := childEnvFor(t, []string{
		runtimeenv.AppServerTokenNameEnv + "=QWEN_APP_TOKEN",
		"QWEN_APP_TOKEN=app-secret",
		runtimeenv.SessionManagerTokenNameEnv + "=QWEN_MANAGER_TOKEN",
		"QWEN_MANAGER_TOKEN=manager-secret",
	})
	for _, leaked := range []string{"QWEN_APP_TOKEN", "QWEN_MANAGER_TOKEN"} {
		if _, present := child[leaked]; present {
			t.Errorf("the credential %q reached the qwen child; the pointer was stripped but what it named was not", leaked)
		}
	}
}

// TestTheSourcesOpenEnvLeaksStayOpen pins the residuals env.go names, so they
// are visible in the suite rather than only in prose.
//
// All three are the source's BUG-260819-3qn52o, in backlog and unfixed at the
// commit these goldens were captured from. If one of these starts failing, the
// leak has been closed — which may well be right, but it is a behaviour change
// no golden covers, and the comment in env.go has to change with it or the code
// and the prose disagree.
func TestTheSourcesOpenEnvLeaksStayOpen(t *testing.T) {
	t.Parallel()
	leaks := map[string]string{
		"TASK_BOARD_TOKEN":                 "a board credential, not a vendor one; the blast radius is the board itself",
		"TASK_BOARD_BUILDER_GATEWAY_TOKEN": "the session-manager MCP shim token, reaching a child that was never meant to hold it",
		"QWEN_CODE_SESSION_ID":             "qwen's OWN session marker: nothing here strips it, so a qwen child spawned from inside a qwen session inherits its parent's session id",
	}
	var parent []string
	for key := range leaks {
		parent = append(parent, key+"=parent-value")
	}
	child := childEnvFor(t, parent)
	for key, why := range leaks {
		if _, present := child[key]; !present {
			t.Errorf("%q no longer reaches the qwen child (%s); the leak env.go declares OPEN has been closed, which is a behaviour change no golden covers", key, why)
		}
	}
}

// TestTheQwenChildPathIsSanitized is the only evidence the PATH strip has: the
// parity harness excludes PATH from its environment diff on both sides, so no
// golden can see it either way.
//
// qwen gets this strip because its filter wraps the codex one, and the source
// recorded precisely this as the change its own capture could not observe.
func TestTheQwenChildPathIsSanitized(t *testing.T) {
	t.Parallel()
	sep := string(os.PathListSeparator)
	shim := filepath.Join("/home/op", ".codex", "tmp", "arg0", "17")
	binDir := t.TempDir()
	paritycase.WriteStubExecutable(t, binDir, executableName)

	req := agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
		Effort:  parityEffort,
		Prompt:  []byte("assignment"),
		WorkDir: t.TempDir(),
		Env:     []string{"PATH=" + strings.Join([]string{shim, binDir, "/usr/bin"}, sep)},
	}
	plan := paritycase.BuildPlan(t, New(), req, agentic.LaunchModeExec)

	childPath, present := paritycase.Lookup(plan.Env, "PATH")
	if !present {
		t.Fatal("the child carries no PATH at all; the sanitizer removed the whole entry")
	}
	parts := filepath.SplitList(childPath)
	for _, part := range parts {
		if part == shim {
			t.Errorf("the child PATH still carries %q; a child resolving through the parent's arg0 shim runs a wrapper whose parent is gone", shim)
		}
	}
	for _, kept := range []string{binDir, "/usr/bin"} {
		found := false
		for _, part := range parts {
			if part == kept {
				found = true
			}
		}
		if !found {
			t.Errorf("the child PATH lost the ordinary entry %q; over-stripping PATH breaks every tool the child needs and no golden would see it", kept)
		}
	}
}

// TestNoStrippedKeyCollidesWithAnInjectedOne pins the assumption childEnv's
// comment rests on: filter-then-inject and inject-then-filter agree today only
// because the two key sets are disjoint.
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
	for _, stripped := range runtimeEnvKeys {
		if injected[stripped] {
			t.Errorf("%q is both stripped and injected; the order in childEnv is now load-bearing and its comment says it is not", stripped)
		}
	}
}

// TestTheFilterOrderDoesNotChangeTheResult pins the other assumption env.go
// states: composing the CLAUDECODE strip before the codex filter, or after it,
// produces the same environment today.
//
// The interesting half is the credential pointers. The codex filter resolves
// them against whatever it is handed, so an order that removed a pointer first
// would leave the credential it names in the child. CLAUDECODE is not a
// pointer, which is why the orders agree — and this test is what would notice
// if a future key added to either half were.
func TestTheFilterOrderDoesNotChangeTheResult(t *testing.T) {
	t.Parallel()
	parent := []string{
		sessionMarkerEnv + "=1",
		runtimeenv.AppServerTokenNameEnv + "=QWEN_APP_TOKEN",
		"QWEN_APP_TOKEN=app-secret",
		runtimeenv.ThreadIDEnv + "=thread",
		"KEEP=me",
		"PATH=/usr/bin",
	}
	sourceOrder := filterRuntimeEnv(parent)
	reversed := runtimeenv.FilterKeys(runtimeenv.Filter(parent), sessionMarkerEnv)
	if strings.Join(sourceOrder, "\x00") != strings.Join(reversed, "\x00") {
		t.Errorf("the two composition orders disagree:\n  source order: %v\n  reversed:     %v\nchildEnv's comment claims they cannot", sourceOrder, reversed)
	}
}

// TestEveryStrippedKeyIsCarriedByTheGolden narrows the blocked list itself: one
// key at a time is put back into the child and the qwen golden must then FAIL.
//
// This is what tells the twelve entries apart. Without it, the whole list could
// be replaced by any subset that happens to cover the fixture, and the suite
// would stay green while an inherited key reached every qwen child. CLAUDECODE
// is in this list, so the fix the source paid for is narrowed here too rather
// than only asserted directly.
func TestEveryStrippedKeyIsCarriedByTheGolden(t *testing.T) {
	c := parityCaseFor(t, "qwen/exec")

	for _, dropped := range runtimeEnvKeys {
		t.Run(dropped, func(t *testing.T) {
			g, dirs, req := prepareParityCase(t, c)
			narrowed := narrowedFilterSystem{System: New(), stopStripping: dropped}
			plan := paritycase.BuildPlan(t, narrowed, req, c.mode)
			diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
			if len(diffs) == 0 {
				t.Fatalf("a qwen plugin that stops stripping %s still byte-matches the golden, so nothing in the fixture distinguishes that entry from an entry that was never there", dropped)
			}
			if !paritycase.NamesField(diffs, "EnvRemoved") {
				t.Errorf("narrowing the filter was reported as %v rather than as an environment difference", diffs)
			}
		})
	}
}

// narrowedFilterSystem is the real plugin with ONE key put back into the child,
// which is the smallest possible weakening of the filter: everything else about
// the plan — argv, binary, stdin, every other stripped key — stays correct.
type narrowedFilterSystem struct {
	*System
	stopStripping string
}

func (n narrowedFilterSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := n.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	value, present := paritycase.Lookup(parent, n.stopStripping)
	if !present {
		return env, nil
	}
	return append(env, n.stopStripping+"="+value), nil
}
