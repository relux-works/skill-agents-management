package agentic

import (
	"path/filepath"
	"strings"
	"testing"
)

func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		out[key] = value
	}
	return out
}

func countKey(env []string, key string) int {
	n := 0
	for _, entry := range env {
		if k, _, _ := strings.Cut(entry, "="); k == key {
			n++
		}
	}
	return n
}

// TestSetEnvValueMatchesWholeKeys is the property every plugin's injection
// depends on and the one a port gets wrong by writing one plausible line.
//
// A prefix match would rewrite TASK_BOARD_RUN_ID_SUFFIX while replacing
// TASK_BOARD_RUN_ID, and the parity goldens seed TASK_BOARD_LIKE_BUT_NOT
// precisely because that defect is otherwise invisible.
func TestSetEnvValueMatchesWholeKeys(t *testing.T) {
	t.Parallel()
	got := SetEnvValue([]string{
		"TASK_BOARD_RUN_ID=old",
		"TASK_BOARD_RUN_ID_SUFFIX=keep",
		"TASK_BOARD_RUN_I=keep",
		"TASK_BOARD_LIKE_BUT_NOT=keep",
	}, "TASK_BOARD_RUN_ID", "new")

	env := envMap(got)
	if env["TASK_BOARD_RUN_ID"] != "new" {
		t.Errorf("TASK_BOARD_RUN_ID = %q, want %q", env["TASK_BOARD_RUN_ID"], "new")
	}
	for _, key := range []string{"TASK_BOARD_RUN_ID_SUFFIX", "TASK_BOARD_RUN_I", "TASK_BOARD_LIKE_BUT_NOT"} {
		if env[key] != "keep" {
			t.Errorf("%s was rewritten to %q; the replacement matched a PREFIX rather than the whole key", key, env[key])
		}
	}
}

// TestSetEnvValueReplacesRatherThanDuplicates holds what a child would see if
// one key appeared twice: the answer depends on which entry the harness reads
// first, which is not a thing anyone should have to know.
func TestSetEnvValueReplacesRatherThanDuplicates(t *testing.T) {
	t.Parallel()
	got := SetEnvValue([]string{"K=one", "OTHER=x", "K=two"}, "K", "three")
	if n := countKey(got, "K"); n != 1 {
		t.Errorf("K appears %d times in %v, want exactly once", n, got)
	}
	if envMap(got)["K"] != "three" {
		t.Errorf("K = %q, want the new value", envMap(got)["K"])
	}
}

// TestAnEmptyValueRemovesTheKey is the difference between exporting nothing and
// exporting a blank. A child that inherits `TASK_BOARD_DELIVERY_GOAL_ID=` reads
// a goal id that is present and meaningless.
func TestAnEmptyValueRemovesTheKey(t *testing.T) {
	t.Parallel()
	got := SetEnvValue([]string{"K=one", "OTHER=x"}, "K", "")
	if _, present := envMap(got)["K"]; present {
		t.Errorf("K survived an empty value in %v", got)
	}
	if envMap(got)["OTHER"] != "x" {
		t.Error("an unrelated entry was dropped")
	}
}

// TestSetEnvValueDropsABareKey covers a malformed entry with no `=`: it names
// the key, so a replacement must remove it rather than leave a second,
// value-less declaration of the same name behind.
func TestSetEnvValueDropsABareKey(t *testing.T) {
	t.Parallel()
	got := SetEnvValue([]string{"K", "OTHER=x"}, "K", "one")
	if n := countKey(got, "K"); n != 1 {
		t.Errorf("K appears %d times in %v, want exactly once", n, got)
	}
}

// TestSetEnvValueDoesNotAliasItsInput keeps one launch's environment from being
// rewritten by the construction of another's.
func TestSetEnvValueDoesNotAliasItsInput(t *testing.T) {
	t.Parallel()
	parent := []string{"A=1", "B=2"}
	got := SetEnvValue(parent, "C", "3")
	got[0] = "REWRITTEN=1"
	if parent[0] != "A=1" {
		t.Errorf("the caller's environment was mutated to %v", parent)
	}
}

// TestWithRunContextWritesEveryIdentifier is the injection contract.
func TestWithRunContextWritesEveryIdentifier(t *testing.T) {
	t.Parallel()
	req := LaunchRequest{
		Run: RunContext{
			RunID:     "  RUN-child  ",
			TaskID:    "TASK-child",
			BoardDir:  "/board/.task-board",
			ContextID: "CTX-child",
		},
		Goal: &Goal{ID: "GOAL-child"},
	}
	env := envMap(WithRunContext([]string{
		EnvRunID + "=RUN-parent",
		EnvTaskID + "=TASK-parent",
		EnvLegacyBoardDir + "=/parent/.task-board",
		EnvDeliveryGoalID + "=GOAL-parent",
		"UNRELATED=keep",
	}, req))

	want := map[string]string{
		EnvRunID:          "RUN-child",
		EnvTaskID:         "TASK-child",
		EnvLegacyBoardDir: "/board/.task-board",
		EnvBoardDir:       "/board/.task-board",
		EnvDeliveryGoalID: "GOAL-child",
		EnvContextID:      "CTX-child",
		"UNRELATED":       "keep",
	}
	for key, value := range want {
		if env[key] != value {
			t.Errorf("%s = %q, want %q", key, env[key], value)
		}
	}
}

// TestWithRunContextRemovesWhatThisLaunchDoesNotCarry is the negative: an
// absent identifier must take the inherited one WITH it.
//
// A child that keeps its parent's run id reports its work against the parent's
// run, and nothing anywhere reports a conflict.
func TestWithRunContextRemovesWhatThisLaunchDoesNotCarry(t *testing.T) {
	t.Parallel()
	env := envMap(WithRunContext([]string{
		EnvRunID + "=RUN-parent",
		EnvTaskID + "=TASK-parent",
		EnvLegacyBoardDir + "=/parent/.task-board",
		EnvBoardDir + "=/parent/.task-board",
		EnvDeliveryGoalID + "=GOAL-parent",
		EnvContextID + "=CTX-parent",
	}, LaunchRequest{}))

	for _, key := range []string{EnvRunID, EnvTaskID, EnvLegacyBoardDir, EnvBoardDir, EnvDeliveryGoalID, EnvContextID} {
		if value, present := env[key]; present {
			t.Errorf("%s survived as %q on a launch that carries no run context; the child would act under its parent's identity", key, value)
		}
	}
}

// TestTheBoardSelectorIsAbsolute holds the source's fix for the split a story
// worktree creates: a relative selector is resolved by the CHILD against its
// own working directory, which is a checkout artifact rather than the
// authoritative board.
func TestTheBoardSelectorIsAbsolute(t *testing.T) {
	t.Parallel()
	env := envMap(WithRunContext(nil, LaunchRequest{Run: RunContext{BoardDir: "relative/.task-board"}}))
	if !filepath.IsAbs(env[EnvBoardDir]) {
		t.Errorf("%s = %q is not absolute", EnvBoardDir, env[EnvBoardDir])
	}
	if env[EnvLegacyBoardDir] != "relative/.task-board" {
		t.Errorf("%s = %q, want the value as given: the legacy run-context variable is separate from the selector and the source does not rewrite it", EnvLegacyBoardDir, env[EnvLegacyBoardDir])
	}
}

// TestAbsoluteBoardDirKeepsWhatItCannotResolve pins the fallback: exporting a
// worse selector is recoverable, exporting none silently re-enables the
// cwd-relative resolution the absolute path exists to prevent.
func TestAbsoluteBoardDirKeepsWhatItCannotResolve(t *testing.T) {
	t.Parallel()
	if got := AbsoluteBoardDir("   "); got != "" {
		t.Errorf("AbsoluteBoardDir(blank) = %q, want nothing: an unset board directory is an absence", got)
	}
	if got := AbsoluteBoardDir("/already/absolute"); got != "/already/absolute" {
		t.Errorf("AbsoluteBoardDir = %q, want the path unchanged", got)
	}
}

// TestRunContextIsZero holds the one thing the type answers about itself.
func TestRunContextIsZero(t *testing.T) {
	t.Parallel()
	if !(RunContext{}).IsZero() {
		t.Error("the zero RunContext does not report itself as zero")
	}
	for _, c := range []RunContext{
		{RunID: "x"}, {TaskID: "x"}, {BoardDir: "x"}, {ContextID: "x"},
	} {
		if c.IsZero() {
			t.Errorf("%+v reports itself as zero", c)
		}
	}
}
