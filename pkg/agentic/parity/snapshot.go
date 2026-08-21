package parity

import (
	"sort"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Snapshot is the source harness's launch-surface schema, field for field.
//
// The JSON tags are the source's own (launchSurfaceSnapshot in
// parity_capture_test.go), including its omitempty choices, because the
// goldens in testdata/goldens are that harness's output and a renamed field
// would silently read as an absent one.
type Snapshot struct {
	// Binary is the executable the launch actually resolved — the managed-npm
	// path, the unwrapped native shim, the preflighted runtime — not a display
	// placeholder. The source's own BuildArgs bug was a display placeholder
	// drifting from the launch target, so this field is the one that caught it.
	Binary string `json:"binary"`
	// Args is argv excluding the binary itself.
	Args []string `json:"args"`
	// EnvAdded and EnvRemoved are whole KEY=VALUE entries present in exactly
	// one of the parent and child environments, sorted. A value replacement
	// therefore appears in BOTH lists, which is what makes
	// TASK_BOARD_RUN_ID=<parent> removed and TASK_BOARD_RUN_ID=<child> added
	// legible as one fact rather than lost as a no-op.
	EnvAdded   []string `json:"env_added,omitempty"`
	EnvRemoved []string `json:"env_removed,omitempty"`
	// StdinKind is a closed vocabulary: StdinNone, StdinBytes, StdinDryRun.
	// It is separate from StdinData because an absent stdin and an attached
	// empty stream are different facts — the child sees a closed descriptor in
	// one and EOF in the other — and one empty string cannot say which.
	StdinKind string `json:"stdin_kind"`
	StdinData string `json:"stdin_data,omitempty"`
	// Error is the failure the capture recorded instead of a surface. An
	// errored capture has no binary, argv, env or stdin at all, so a golden
	// carrying one pins a refusal rather than a launch.
	Error string `json:"error,omitempty"`
}

// The StdinKind vocabulary, spelled as the source spelled it.
const (
	// StdinNone means the child gets no stdin at all.
	StdinNone = "none"
	// StdinBytes means the child reads StdinData.
	StdinBytes = "bytes"
	// StdinDryRun is what the source's dry-run capture path records. It is not
	// "the dry run attaches no stdin": that path calls BuildArgs, which builds
	// no command and therefore observes no stdin and no environment either. A
	// dry-run golden proves the binary and the argv, and says nothing about
	// the other two. See testdata/goldens/README.md.
	StdinDryRun = "n/a-dry-run"
)

// envDiffExcludedKeys are the environment keys DiffEnv refuses to compare.
//
// PATH and only PATH, ported from the source harness's diffEnv, where the
// justification was that the capture seeds PATH with a testing.T.TempDir so
// the harness resolves stub binaries instead of whatever the developer's
// machine happens to have installed. That seeding makes the value differ
// between two runs of the same code, which is noise, not a signal.
//
// The cost is real and is not hidden: the source recorded that qwen's child
// PATH VALUE changed in the refactor this capture proved — sanitizeCodexPath
// strips .codex/tmp/arg0 shim entries — and its own harness could not see it.
// A port that changes what it strips from PATH is therefore outside what these
// goldens prove. testdata/goldens/README.md carries that boundary.
//
// The list is a var rather than an inline check so TestDiffEnvExcludesPathAndOnlyPath
// can narrow it and show which class each entry covers.
var envDiffExcludedKeys = []string{"PATH"}

func envDiffExcluded(entry string) bool {
	key, _, _ := strings.Cut(entry, "=")
	for _, excluded := range envDiffExcludedKeys {
		if key == excluded {
			return true
		}
	}
	return false
}

// DiffEnv reports the whole KEY=VALUE entries present in exactly one of parent
// and child, sorted, with envDiffExcludedKeys dropped from both sides.
//
// It is the source's diffEnv, ported. The set semantics are the source's too:
// two identical entries in one list collapse, because the child environment is
// a set as far as the harness that reads it is concerned.
func DiffEnv(parent, child []string) (added, removed []string) {
	parentSet := entrySet(parent)
	childSet := entrySet(child)
	for entry := range childSet {
		if !parentSet[entry] {
			added = append(added, entry)
		}
	}
	for entry := range parentSet {
		if !childSet[entry] {
			removed = append(removed, entry)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func entrySet(env []string) map[string]bool {
	set := make(map[string]bool, len(env))
	for _, entry := range env {
		if envDiffExcluded(entry) {
			continue
		}
		set[entry] = true
	}
	return set
}

// FromPlan maps a plan onto the snapshot schema, against the parent
// environment the plan was built from.
//
// parent must be the same environment that was handed to the launch request as
// LaunchRequest.Env: EnvAdded and EnvRemoved are a diff, and a diff against a
// different baseline is a different measurement wearing the same name. The
// golden records the exact parent environment its capture ran under
// (Golden.Capture.ParentEnv) so a port test has no reason to invent one.
//
// The dry-run split mirrors the source, which had two capture functions rather
// than one: its dry-run path called BuildArgs, which returns a binary and argv
// and never constructs a command, so there is no environment and no stdin to
// observe. Reporting an empty env diff for a dry-run plan would be reporting an
// absence that was never measured, so this returns the source's StdinDryRun
// marker and leaves both env lists nil instead.
func FromPlan(plan agentic.Plan, parent []string) Snapshot {
	snap := Snapshot{
		Binary: plan.Binary,
		Args:   append([]string(nil), plan.Argv...),
	}
	if plan.Mode == agentic.LaunchModeDryRun {
		snap.StdinKind = StdinDryRun
		return snap
	}
	snap.EnvAdded, snap.EnvRemoved = DiffEnv(parent, plan.Env)
	if plan.Stdin.Attached {
		snap.StdinKind = StdinBytes
		snap.StdinData = string(plan.Stdin.Bytes)
	} else {
		snap.StdinKind = StdinNone
	}
	return snap
}
