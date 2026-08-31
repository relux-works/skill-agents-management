package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// childEnvFor drives the production call site — a real registry, BuildPlan —
// and returns the child environment as a map, so a test asserts over the
// environment a launch would ACTUALLY hand the child rather than over a helper
// nobody calls.
func childEnvFor(t *testing.T, parentEnv []string) map[string]string {
	t.Helper()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
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

// TestTheCodexChildDoesNotInheritParentRuntimeState is the strip contract,
// driven through BuildPlan.
//
// Each key is seeded with a value, so "absent from the child" is a measurement
// rather than a coincidence of a key nobody set — an absence and a failure to
// seed are different facts, and only the first one proves a strip.
func TestTheCodexChildDoesNotInheritParentRuntimeState(t *testing.T) {
	t.Parallel()
	parent := []string{
		threadIDEnv + "=thread-parent",
		sessionEnv + "=session-parent",
		ciEnv + "=1",
		managedByNPMEnv + "=1",
		managedByBunEnv + "=1",
		managedPackageRootEnv + "=/parent/managed/root",
		appServerURLEnv + "=ws://127.0.0.1:1234",
		appServerTokenNameEnv + "=PARENT_APP_SERVER_TOKEN",
		"PARENT_APP_SERVER_TOKEN=app-server-secret",
		sessionManagerURLEnv + "=unix:///tmp/manager.sock",
		sessionManagerTokenNameEnv + "=PARENT_SESSION_MANAGER_TOKEN",
		"PARENT_SESSION_MANAGER_TOKEN=manager-secret",
		sessionIDEnv + "=SESSION-parent",
		"JIRA_TOKEN=must-remain-inherited",
	}
	child := childEnvFor(t, parent)

	for _, key := range append(append([]string(nil), runtimeEnvKeys...),
		"PARENT_APP_SERVER_TOKEN", "PARENT_SESSION_MANAGER_TOKEN") {
		if value, present := child[key]; present {
			t.Errorf("the codex child inherited %s=%q; a spawned codex run is a deliberately independent process and attaches to the parent's session if it inherits one", key, value)
		}
	}
	if child["JIRA_TOKEN"] != "must-remain-inherited" {
		t.Errorf("the codex child lost an unrelated variable: JIRA_TOKEN=%q", child["JIRA_TOKEN"])
	}
}

// TestTheIndirectCredentialStripFollowsThePointer narrows the strip above onto
// its least obvious half.
//
// TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV does not hold a credential — it
// holds the NAME of the variable that does. A port that strips the eleven
// named keys and stops leaves the token itself in the child, and the fixed list
// alone cannot show the difference because the token's name is chosen at
// runtime.
func TestTheIndirectCredentialStripFollowsThePointer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pointer string
	}{
		{name: "the app server token", pointer: appServerTokenNameEnv},
		{name: "the session manager token", pointer: sessionManagerTokenNameEnv},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			const named = "A_NAME_NO_FIXED_LIST_COULD_CONTAIN"
			child := childEnvFor(t, []string{
				c.pointer + "=" + named,
				named + "=the-secret",
			})
			if value, present := child[named]; present {
				t.Errorf("the child inherited %s=%q: %s was stripped but the variable it POINTS AT was not, so the credential survived the strip that exists for it", named, value, c.pointer)
			}
		})
	}
}

// TestAnUnresolvablePointerStripsNothingExtra is the other half of the
// pointer's bound: with no pointer set, nothing beyond the fixed list is
// removed. Without it, a filter that blocked every key whose value looked like
// a variable name would pass the test above.
func TestAnUnresolvablePointerStripsNothingExtra(t *testing.T) {
	t.Parallel()
	child := childEnvFor(t, []string{"A_NAME_NO_FIXED_LIST_COULD_CONTAIN=the-secret"})
	if _, present := child["A_NAME_NO_FIXED_LIST_COULD_CONTAIN"]; !present {
		t.Error("a variable no pointer named was stripped anyway; the pointer strip is removing more than the pointers resolve")
	}
}

// TestEveryStrippedKeyIsCarriedByTheGolden narrows the blocked list itself: one
// key at a time is removed from it and the codex golden must then FAIL.
//
// This is what tells the eleven entries apart. Without it, the whole list could
// be replaced by any subset that happens to cover the fixture, and the suite
// would stay green while an inherited key reached every codex child.
func TestEveryStrippedKeyIsCarriedByTheGolden(t *testing.T) {
	c := parityCaseFor(t, "codex/exec-default-path")

	for _, dropped := range runtimeEnvKeys {
		t.Run(dropped, func(t *testing.T) {
			g, dirs, req := prepareParityCase(t, c)
			narrowed := narrowedFilterSystem{System: New(), stopStripping: dropped}
			plan := buildParityPlan(t, narrowed, req, c.mode)
			diffs := parity.ComparePlan(g, plan, dirs.substitutions())
			if len(diffs) == 0 {
				t.Fatalf("a codex plugin that stops stripping %s still byte-matches the golden, so nothing in the fixture distinguishes that entry from an entry that was never there", dropped)
			}
			if !namesField(diffs, "EnvRemoved") {
				t.Errorf("narrowing the filter was reported as %v rather than as an environment difference", diffs)
			}
		})
	}
}

// narrowedFilterSystem is the real plugin with ONE key put back into the
// child, which is the smallest possible weakening of the filter.
type narrowedFilterSystem struct {
	*System
	stopStripping string
}

func (n narrowedFilterSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := n.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	value, present := lookupEnv(parent, n.stopStripping)
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
		agentic.EnvRunID:  parityRunID,
		agentic.EnvTaskID: parityTaskID,
		ServiceTierEnv:    parityTier,
	}
	for key, value := range want {
		if child[key] != value {
			t.Errorf("child[%s] = %q, want %q", key, child[key], value)
		}
	}
	if child[agentic.EnvBoardDir] == "/parent/.task-board" {
		t.Error("the child kept the PARENT's board selector; a run routed at its parent's board writes its status into somebody else's tree")
	}
	if !filepath.IsAbs(child[agentic.EnvBoardDir]) {
		t.Errorf("%s = %q is not absolute; a relative selector is resolved against the CHILD's working directory, which is the split this export exists to close", agentic.EnvBoardDir, child[agentic.EnvBoardDir])
	}
}

// TestAGoallessLaunchRemovesAnInheritedGoal holds the difference between
// exporting an empty value and exporting nothing.
//
// A child that inherits TASK_BOARD_DELIVERY_GOAL_ID from its parent is bound to
// a goal this launch never chose. Removing the key is what the source does and
// what the golden records; exporting a blank would be a goal id that is present
// and meaningless.
func TestAGoallessLaunchRemovesAnInheritedGoal(t *testing.T) {
	t.Parallel()
	child := childEnvFor(t, []string{agentic.EnvDeliveryGoalID + "=GOAL-parent"})
	if value, present := child[agentic.EnvDeliveryGoalID]; present {
		t.Errorf("%s survived as %q on a launch that carries no goal", agentic.EnvDeliveryGoalID, value)
	}
}

// TestSanitizePathDropsOnlyTheCodexRuntimeEntries is the ONLY evidence this
// plugin has for what it strips from PATH.
//
// The goldens cannot see it: the parity harness excludes PATH from its diff
// entirely, because the capture seeds it with a temp directory. The goldens'
// own README says so and says what it costs — "a port that changes what it
// strips from PATH is outside what these goldens prove. That needs its own
// test." This is that test, and both directions are asserted, because a
// sanitizer that dropped everything would satisfy the removal half alone.
func TestSanitizePathDropsOnlyTheCodexRuntimeEntries(t *testing.T) {
	t.Parallel()
	sep := string(os.PathListSeparator)
	dropped := []string{
		"/opt/homebrew/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/codex-path",
		"/Users/someone/.codex/tmp/arg0/codex-arg0abc",
	}
	kept := []string{"/Users/someone/.local/bin", "/usr/bin", "/bin", "/opt/codex-tools"}

	got := sanitizePath([]string{"PATH=" + strings.Join(append(append([]string(nil), dropped...), kept...), sep)})
	if len(got) != 1 {
		t.Fatalf("sanitizePath returned %d entries, want 1", len(got))
	}
	value, _ := lookupEnv(got, "PATH")
	parts := filepath.SplitList(value)

	for _, part := range dropped {
		if containsString(parts, part) {
			t.Errorf("sanitizePath kept the parent codex runtime entry %q; a child resolving through the parent's arg0 shim runs a wrapper whose parent is gone", part)
		}
	}
	for _, part := range kept {
		if !containsString(parts, part) {
			t.Errorf("sanitizePath dropped the ordinary PATH entry %q; over-stripping PATH breaks every tool the child needs and no golden would see it", part)
		}
	}
	if len(parts) != len(kept) {
		t.Errorf("sanitizePath produced %v, want exactly %v", parts, kept)
	}
}

// TestSanitizePathReachesTheChildThroughBuildPlan proves the sanitizer is
// CALLED from production rather than only unit-tested. A helper that is tested
// but reached from nowhere promises nothing.
func TestSanitizePathReachesTheChildThroughBuildPlan(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	shim := "/Users/someone/.codex/tmp/arg0/codex-arg0abc"

	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, "path sanitation prompt")
	req.Env = []string{"PATH=" + shim + string(os.PathListSeparator) + binDir}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	value, present := lookupEnv(plan.Env, "PATH")
	if !present {
		t.Fatal("the child environment carries no PATH at all")
	}
	if containsString(filepath.SplitList(value), shim) {
		t.Errorf("the child PATH still carries %q; sanitizePath is not on the production path", shim)
	}
	if !containsString(filepath.SplitList(value), binDir) {
		t.Errorf("the child PATH lost %q", binDir)
	}
}

// TestTheSourcesOpenEnvLeaksStayOpen pins the two leaks the source records as
// BUG-260819-3qn52o, which is in BACKLOG — unfixed — at the commit these
// goldens were captured from.
//
// This test asserts the leaks are STILL OPEN, which is an unusual thing to want
// and is deliberate. Closing either one here would be a behaviour change no
// golden covers, made inside a port whose acceptance is byte-identical launch
// surfaces, in one of six plugins that share the defect. The bug's own third
// acceptance criterion says why it cannot be fixed one plugin at a time: the
// construction has to make a seventh runtime safe without editing N filters.
//
// So the residual is pinned rather than described. If someone closes it, this
// test goes red and points at the bug, which is the conversation that should
// happen — instead of the leak quietly changing state in a port nobody reviewed
// for it.
func TestTheSourcesOpenEnvLeaksStayOpen(t *testing.T) {
	t.Parallel()
	const (
		boardToken   = "TASK_BOARD_TOKEN"
		gatewayToken = "TASK_BOARD_BUILDER_GATEWAY_TOKEN"
		qwenSession  = "QWEN_CODE_SESSION_ID"
	)
	child := childEnvFor(t, []string{
		boardToken + "=board-credential",
		gatewayToken + "=gateway-credential",
		qwenSession + "=qwen-session-parent",
	})

	for _, leak := range []struct {
		key  string
		what string
	}{
		{boardToken, "a board credential, reaching every provider child (BUG-260819-3qn52o, leak 1)"},
		{gatewayToken, "a board credential, reaching every provider child (BUG-260819-3qn52o, leak 1)"},
		{qwenSession, "another runtime's session state, which this filter predates (BUG-260819-3qn52o, leak 2)"},
	} {
		if _, present := child[leak.key]; !present {
			t.Errorf("%s no longer reaches the codex child. That is %s, and the source's bug is still open at the commit these goldens were captured from. If this was fixed on purpose, fix it for all six runtimes through the construction BUG-260819-3qn52o's AC3 asks for, close the bug, and delete this test with the evidence — do not leave two implementations of one contract disagreeing about it.", leak.key, leak.what)
		}
	}
}

// TestAnUnconfiguredTierLeavesTheInheritedValueAlone pins the second residual
// carried from the source: the service-tier export is written only when this
// launch resolved a tier, so a child of a parent that had one inherits it.
//
// It reads as a bug and it is at least a surprise — a launch that configured no
// tier hands the child the parent's. It is the source's behaviour
// (withSpawnEnv's `if tier != ""` guard), no golden covers it, and changing it
// silently inside a parity port is how a port stops being one.
func TestAnUnconfiguredTierLeavesTheInheritedValueAlone(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.ServiceTier = ""
	req.PromptPath = writePromptFile(t, workDir, "tier passthrough prompt")
	req.Env = []string{"PATH=" + binDir, ServiceTierEnv + "=priority"}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	value, present := lookupEnv(plan.Env, ServiceTierEnv)
	if !present {
		t.Fatalf("%s was REMOVED by a launch that configured no tier. That is a deviation from the source, whose export is guarded by `if tier != \"\"`; if it is an intended improvement it needs its own evidence, not a silent change inside a parity port.", ServiceTierEnv)
	}
	if value != "priority" {
		t.Errorf("%s = %q, want the inherited %q", ServiceTierEnv, value, "priority")
	}
}

// TestAnUnknownServiceTierIsDropped pins the third residual: a tier outside the
// vocabulary normalizes to nothing and simply does not appear, in the argv or
// in the environment.
//
// The operator configured a tier and the launch runs on the account default,
// with no refusal anywhere. That is the source's NormalizeCodexServiceTier, and
// it sits awkwardly beside this repository's own rule that a parameter a system
// cannot honour is refused rather than dropped — which is exactly why it is
// pinned here where a reader will find it, rather than changed in a port.
func TestAnUnknownServiceTierIsDropped(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.ServiceTier = "platinum-unlimited"
	req.PromptPath = writePromptFile(t, workDir, "unknown tier prompt")
	req.Env = []string{"PATH=" + binDir}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if _, present := lookupEnv(plan.Env, ServiceTierEnv); present {
		t.Errorf("%s was exported for an unrecognized tier", ServiceTierEnv)
	}
	if containsString(plan.Argv, `service_tier="platinum-unlimited"`) {
		t.Error("an unrecognized tier reached the argv verbatim")
	}
	for _, arg := range plan.Argv {
		if strings.HasPrefix(arg, "service_tier=") {
			t.Errorf("an unrecognized tier produced the override %q", arg)
		}
	}
}

// TestFilterEnvKeysMatchesWholeKeys is the unit-level statement of the property
// the parity negatives attack from the outside: a key that merely STARTS WITH a
// blocked key is not blocked.
func TestFilterEnvKeysMatchesWholeKeys(t *testing.T) {
	t.Parallel()
	got := filterEnvKeys([]string{
		"CODEX_SESSION=blocked",
		"CODEX_SESSION_EXTRA=kept",
		"CODEX_SESSIO=kept",
		"BARE_ENTRY_WITH_NO_EQUALS",
	}, "CODEX_SESSION")

	want := []string{"CODEX_SESSION_EXTRA=kept", "CODEX_SESSIO=kept", "BARE_ENTRY_WITH_NO_EQUALS"}
	if len(got) != len(want) {
		t.Fatalf("filterEnvKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filterEnvKeys = %v, want %v", got, want)
		}
	}
}

// TestFilterEnvKeysIgnoresBlankKeys holds a boundary the pointer resolution
// depends on: an unresolvable pointer contributes an empty key name, and a
// filter that treated "" as a key would drop every entry with no `=` in it.
func TestFilterEnvKeysIgnoresBlankKeys(t *testing.T) {
	t.Parallel()
	got := filterEnvKeys([]string{"KEPT=1", "BARE"}, "", "   ")
	if len(got) != 2 {
		t.Errorf("filterEnvKeys with blank keys = %v, want both entries kept", got)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// TestNoStrippedKeyCollidesWithAnInjectedOne holds the precondition childEnv's
// ordering rests on.
//
// While the two sets are disjoint, filtering before injecting and injecting
// before filtering produce the same child environment — a mutation run proved
// exactly that by swapping them and finding nothing red. The order is still the
// correct one, and this test is what makes that claim honest: the moment a
// blocked key collides with an injected one, the order becomes load-bearing and
// this fails, pointing at childEnv, instead of a child silently losing its run
// id to its own filter.
func TestNoStrippedKeyCollidesWithAnInjectedOne(t *testing.T) {
	t.Parallel()
	injected := map[string]bool{
		agentic.EnvRunID:          true,
		agentic.EnvTaskID:         true,
		agentic.EnvLegacyBoardDir: true,
		agentic.EnvBoardDir:       true,
		agentic.EnvDeliveryGoalID: true,
		agentic.EnvContextID:      true,
		ServiceTierEnv:            true,
	}
	for _, stripped := range runtimeEnvKeys {
		if injected[stripped] {
			t.Errorf("%s is both stripped by this filter and injected as run context. childEnv filters BEFORE it injects, so this still works — but the order is now load-bearing rather than conventional, and reversing it would silently drop the variable from the child. Say so where childEnv documents the order.", stripped)
		}
	}
	if injected[ServiceTierEnv] && containsString(runtimeEnvKeys, ServiceTierEnv) {
		t.Error("the service-tier variable is stripped by the filter that runs before it is written")
	}
}
