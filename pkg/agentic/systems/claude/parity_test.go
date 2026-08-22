package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// This file is the acceptance. Every claude golden the source's own harness
// captured is rebuilt here through the REAL entry point — a real
// agentic.Registry holding the real plugin, then agentic.BuildPlan — and
// compared field for field against the fixture.
//
// Nothing in it constructs a Plan by hand and nothing reads an expected value
// out of the golden it is comparing to. The layouts below are built on disk the
// way the source's capture built them, from the same descriptions in
// parity_capture_test.go, so a comparison that passes means this plugin
// reproduces the source's launch surface rather than that JSON round-trips.

// The source's two claude parity configs, field for field
// (parity_capture_test.go). They are spelled here rather than derived from the
// fixture for the reason above.
const (
	parityModel = "claude-opus-5"

	parityEffort = "high"

	parityPromptRunID  = "RUN-parity-claude"
	parityPromptTaskID = "TASK-parity-claude"
	parityPromptBody   = "claude prompt-mode body"

	parityGoalRunID     = "RUN-parity-claude-goal"
	parityGoalTaskID    = "TASK-parity-claude-goal"
	parityGoalBody      = "claude goal-mode assignment"
	parityGoalID        = "GOAL-260717-CLD001"
	parityGoalRevision  = 1
	parityPromptFileArg = "prompt.md"
)

// goldenSystem is the value the fixtures record in their `system` field.
//
// It is "claude", the source's AgentType and this repository's frozen RUNTIME
// id, while this plugin's id is "claude-code" — the Layer-1 plugin id
// docs/architecture.md's table declares. The mapping between the two lives here
// and nowhere else, because a second copy of it is exactly the two-spellings
// disease the registry's normalization exists to prevent. Nothing in the
// comparison depends on it: parity.Compare looks at binary, argv, environment
// and stdin, and never at the system name.
const goldenSystem = "claude"

// parityProviderCondition renders the goal predicate the source splices into
// argv.
//
// The format string is pkgboard.RenderGoalProviderCondition's, spelled here
// rather than copied out of the fixture. That distinction is the whole value of
// this test: a case that read the expected argument back out of the golden it
// is comparing to would prove only that JSON round-trips.
func parityProviderCondition(goalID, runID string) string {
	return fmt.Sprintf(
		"Own the latest active revision of board goal **%s** for run **%s** through its board-defined success predicate. "+
			"Before nested spawn, at every directive checkpoint, and before claiming success, run `task-board spawn goal %s`; "+
			"treat the returned objective, resolved scope, and revision as authoritative and surface them with the required board acceptance evidence. "+
			"A stale revision, cancelled/superseded goal, or missing scoped acceptance evidence is not success.",
		goalID, runID, runID,
	)
}

// parityDirs are this process's stand-ins for the machine-local directories the
// capture allocated, in the SAME order the capture allocated them: the source's
// captureBuildCommandSurface takes a bin directory and then a work directory.
// The golden's capture.placeholders block is where that count is read off
// rather than guessed — and claude/prompt-mode lists only <TMPDIR:1> because
// its work directory never reaches the surface at all, the assignment being
// streamed on stdin rather than named in argv.
type parityDirs struct {
	temp []string
	stub string
}

func (d parityDirs) substitutions() parity.Substitutions {
	return parity.Substitutions{TempSlots: d.temp, StubBinDir: d.stub}
}

// tempSlot allocates one machine-local directory, canonicalized.
//
// The canonicalization is not cosmetic on macOS: t.TempDir hands back a path
// under /var, which is a symlink to /private/var. Nothing in THIS plugin
// resolves symlinks — that was codex's shim unwrapping — but the substitution
// literal and the path under test have to be the same string for the mask to
// rewrite anything, and a future check that canonicalizes would otherwise fail
// as a port bug rather than as a test measuring its own temp directory wrong.
func tempSlot(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalizing a temp slot: %v", err)
	}
	return resolved
}

// writeStubExecutable is the source's writeFakeExecutable: a runnable file with
// the right name, so binary resolution lands on a layout this test built rather
// than on whatever the developer's machine has installed.
func writeStubExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the stub %s: %v", name, err)
	}
	return path
}

func writePromptFile(t *testing.T, workDir, body string) string {
	t.Helper()
	path := filepath.Join(workDir, parityPromptFileArg)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the prompt file: %v", err)
	}
	return path
}

// withPathEntry returns the golden's recorded parent environment with a PATH
// pointing at dir.
//
// The golden records no PATH at all — the capture omitted it deliberately, and
// the harness excludes it from the environment diff on both sides — so adding
// one here changes no measurement. It is what makes binary resolution hermetic:
// this plugin resolves from the environment it is handed, so the launch
// environment IS the PATH under test.
func withPathEntry(parent []string, dir string) []string {
	return append(append([]string(nil), parent...), "PATH="+dir)
}

// parityCase is one golden and the machine state that reproduces it.
type parityCase struct {
	goldenID string
	// tempSlots is how many directories the capture allocated for this case, in
	// its order. usesStub says whether the case also resolved through the
	// capture's PATH-seeded stub directory.
	tempSlots int
	usesStub  bool
	mode      agentic.LaunchMode
	// build lays the case's layout down on disk and returns the launch request,
	// minus the environment, which prepareParityCase supplies from the golden.
	build func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string)
}

// parityCases covers every claude golden: both launch surfaces the source's
// capture recorded.
var parityCases = []parityCase{
	{
		goldenID:  "claude/prompt-mode",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string) {
			binDir, workDir := dirs.temp[0], dirs.temp[1]
			writeStubExecutable(t, binDir, executableName)
			req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
			req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
			return req, []string{binDir}
		},
	},
	{
		goldenID:  "claude/goal-mode",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string) {
			binDir, workDir := dirs.temp[0], dirs.temp[1]
			writeStubExecutable(t, binDir, executableName)
			req := parityRequest(workDir, parityGoalRunID, parityGoalTaskID)
			req.PromptPath = writePromptFile(t, workDir, parityGoalBody)
			req.Goal = parityGoal()
			return req, []string{binDir}
		},
	},
}

// parityRequest is the source's claude parity configs expressed as a
// LaunchRequest. Every field it sets is one the source's config set — and the
// ones it does NOT set matter as much: no BoardDir, no service tier, no profile
// and no budget, because neither claude capture configured any of them.
func parityRequest(workDir, runID, taskID string) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
		Effort:  parityEffort,
		WorkDir: workDir,
		Run: agentic.RunContext{
			RunID:  runID,
			TaskID: taskID,
		},
	}
}

// parityGoal is the source's testClaudeGoal reduced to the three facts this
// layer holds. Objective is deliberately empty: the source's rendered objective
// never reaches claude's argv or environment at all, and inventing one here
// would put a value in the fixture's blast radius that the source does not
// carry.
func parityGoal() *agentic.Goal {
	return &agentic.Goal{
		ID:                parityGoalID,
		Revision:          parityGoalRevision,
		ProviderCondition: parityProviderCondition(parityGoalID, parityGoalRunID),
	}
}

func makeParityDirs(t *testing.T, c parityCase) parityDirs {
	t.Helper()
	dirs := parityDirs{}
	for i := 0; i < c.tempSlots; i++ {
		dirs.temp = append(dirs.temp, tempSlot(t))
	}
	if c.usesStub {
		dirs.stub = tempSlot(t)
	}
	return dirs
}

// buildParityPlan drives production: register the real plugin into a real
// registry and call agentic.BuildPlan. Nothing here reaches into the plugin's
// methods directly — a helper that is unit-tested but called from nowhere
// promises nothing about the launch a caller would actually get.
func buildParityPlan(t *testing.T, sys agentic.System, req agentic.LaunchRequest, mode agentic.LaunchMode) agentic.Plan {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register: %v", err)
	}
	plan, err := agentic.BuildPlan(registry, req, mode)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

// prepareParityCase lays the case down and returns everything a comparison
// needs: the golden, the directories, and the request with its environment.
func prepareParityCase(t *testing.T, c parityCase) (parity.Golden, parityDirs, agentic.LaunchRequest) {
	t.Helper()
	g, err := parity.Load(c.goldenID)
	if err != nil {
		t.Fatalf("Load(%q): %v", c.goldenID, err)
	}
	dirs := makeParityDirs(t, c)
	req, pathDirs := c.build(t, dirs)
	req.Env = withPathEntry(g.Capture.ParentEnv, strings.Join(pathDirs, string(os.PathListSeparator)))
	return g, dirs, req
}

// TestPlansMatchTheClaudeGoldens is the acceptance: both captured launch
// surfaces, byte for byte.
func TestPlansMatchTheClaudeGoldens(t *testing.T) {
	for _, c := range parityCases {
		t.Run(c.goldenID, func(t *testing.T) {
			g, dirs, req := prepareParityCase(t, c)
			plan := buildParityPlan(t, New(), req, c.mode)
			if diffs := parity.ComparePlan(g, plan, dirs.substitutions()); len(diffs) != 0 {
				for _, d := range diffs {
					t.Errorf("%s does not match its golden:\n  %s", c.goldenID, d)
				}
			}
		})
	}
}

// TestEveryClaudeGoldenIsCovered fails if the fixture set grows a claude case
// this file does not build.
//
// Without it, a recapture that adds a third launch surface leaves this suite
// green while covering one combination fewer than it believes — which is the
// exact failure mode the parity package exists to prevent, one level up.
func TestEveryClaudeGoldenIsCovered(t *testing.T) {
	all, err := parity.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	covered := map[string]bool{}
	for _, c := range parityCases {
		covered[c.goldenID] = true
	}
	seen := 0
	for id, g := range all {
		if g.System != goldenSystem {
			continue
		}
		seen++
		if !covered[id] {
			t.Errorf("golden %q is a claude case with no parity case in this file; a suite that covers one combination fewer than it believes is the failure this package exists to prevent", id)
		}
	}
	if seen == 0 {
		t.Fatalf("no fixture records system %q, so this coverage check ranged over nothing; the goldens' system field and this file's mapping have drifted", goldenSystem)
	}
	for id := range covered {
		if _, ok := all[id]; !ok {
			t.Errorf("this file builds %q, which is not a captured golden", id)
		}
	}
}

// TestAWrongPlanFailsAgainstTheClaudeGolden is what makes the test above mean
// something.
//
// Four defects, spread over every field family the parity harness records — an
// argv flag, an argv ARGUMENT, an injected environment variable, one byte of
// stdin — each planted on an otherwise-correct plan built through BuildPlan,
// and each required to be reported IN THE FIELD IT WAS PLANTED IN. A mutant
// that fails for the wrong reason proves nothing about the surface it was
// written for.
func TestAWrongPlanFailsAgainstTheClaudeGolden(t *testing.T) {
	mutants := []struct {
		name      string
		subject   string
		defect    string
		wantField string
		system    func(*System) agentic.System
	}{
		{
			name:      "one argv flag misspelled",
			subject:   "claude/prompt-mode",
			defect:    "--dangerously-skip-permissions spelled --dangerously-skip-permission, the kind of typo a port makes while retyping a grammar",
			wantField: "Args",
			system:    func(s *System) agentic.System { return renamedFlagSystem{System: s} },
		},
		{
			name:      "the goal directive loses its condition",
			subject:   "claude/goal-mode",
			defect:    "the child is launched with a bare `/goal` and no predicate; the flag list is identical, so a port that only counted arguments would see nothing wrong",
			wantField: "Args",
			system:    func(s *System) agentic.System { return bareGoalSystem{System: s} },
		},
		{
			name:      "the goal id never reaches the child",
			subject:   "claude/goal-mode",
			defect:    "TASK_BOARD_DELIVERY_GOAL_ID is not injected, so the child cannot resolve the goal the directive tells it to own",
			wantField: "EnvAdded",
			system:    func(s *System) agentic.System { return noGoalEnvSystem{System: s} },
		},
		{
			name:      "one stdin byte changed",
			subject:   "claude/prompt-mode",
			defect:    "a single byte of the assignment prompt differs; the argv is identical, which is exactly the divergence argv-string equality cannot see",
			wantField: "StdinData",
			system:    func(s *System) agentic.System { return mutatedStdinSystem{System: s} },
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			c := parityCaseFor(t, mutant.subject)
			g, dirs, req := prepareParityCase(t, c)

			// The unmutated plan must match first, or a "failing" mutant could
			// be failing for a reason unrelated to the defect it plants.
			clean := buildParityPlan(t, New(), req, c.mode)
			if diffs := parity.ComparePlan(g, clean, dirs.substitutions()); len(diffs) != 0 {
				t.Fatalf("the unmutated plan already differs, so this mutant proves nothing: %v", diffs)
			}

			plan := buildParityPlan(t, mutant.system(New()), req, c.mode)
			diffs := parity.ComparePlan(g, plan, dirs.substitutions())
			if len(diffs) == 0 {
				t.Fatalf("the golden admitted a plan with a real defect (%s); a golden a wrong plan satisfies proves nothing", mutant.defect)
			}
			named := false
			for _, d := range diffs {
				if d.Field == mutant.wantField {
					named = true
				}
			}
			if !named {
				t.Errorf("the defect was planted in %s but the harness reported %v; a failure naming the wrong surface sends the next reader to the wrong file", mutant.wantField, diffs)
			}
		})
	}
}

func parityCaseFor(t *testing.T, goldenID string) parityCase {
	t.Helper()
	for _, c := range parityCases {
		if c.goldenID == goldenID {
			return c
		}
	}
	t.Fatalf("no parity case for %q", goldenID)
	return parityCase{}
}

// The mutant systems. Each embeds the real plugin and overrides exactly one
// surface, so everything else about the plan stays correct and the comparison
// isolates the planted defect.

type renamedFlagSystem struct{ agentic.System }

func (m renamedFlagSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			args[i] = "--dangerously-skip-permission"
		}
	}
	return args, nil
}

type bareGoalSystem struct{ agentic.System }

func (m bareGoalSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i, arg := range args {
		if len(arg) > len(goalDirectivePrefix) && arg[:len(goalDirectivePrefix)] == goalDirectivePrefix {
			args[i] = "/goal"
		}
	}
	return args, nil
}

type noGoalEnvSystem struct{ agentic.System }

func (m noGoalEnvSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := m.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	return agentic.SetEnvValue(env, agentic.EnvDeliveryGoalID, ""), nil
}

type mutatedStdinSystem struct{ agentic.System }

func (m mutatedStdinSystem) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	payload, err := m.System.Stdin(req)
	if err != nil || !payload.Attached || len(payload.Bytes) == 0 {
		return payload, err
	}
	mutated := append([]byte(nil), payload.Bytes...)
	mutated[len(mutated)-1] = 'X'
	return agentic.StdinPayload{Attached: true, Bytes: mutated}, nil
}
