package qwen

import (
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// This file is the acceptance. Every qwen golden the source's own harness
// captured is rebuilt here through the REAL entry point — a real
// agentic.Registry holding the real plugin, then agentic.BuildPlan — and
// compared field for field against the fixture.
//
// Nothing in it constructs a Plan by hand and nothing reads an expected value
// out of the golden it is comparing to. The layouts below are built on disk the
// way the source's capture built them, from the same descriptions in
// parity_capture_test.go, so a comparison that passes means this plugin
// reproduces the source's launch surface rather than that JSON round-trips.

// The source's two qwen parity configs, field for field
// (parity_capture_test.go). They are spelled here rather than derived from the
// fixture for the reason above.
const (
	parityModel  = "qwen3.7-max"
	parityEffort = "high"

	parityRunID      = "RUN-parity-qwen"
	parityTaskID     = "TASK-parity-qwen"
	parityPromptBody = "qwen exec-mode prompt"
)

// goldenSystem is the value the fixtures record in their `system` field.
//
// It is "qwen", the source's AgentType and this repository's frozen RUNTIME id,
// while this plugin's id is "qwen-code" — the Layer-1 plugin id
// docs/architecture.md's table declares. The mapping between the two lives here
// and nowhere else, because a second copy of it is exactly the two-spellings
// disease the registry's normalization exists to prevent. Nothing in the
// comparison depends on it: parity.Compare looks at binary, argv, environment
// and stdin, and never at the system name.
const goldenSystem = "qwen"

// parityCase is one golden and the machine state that reproduces it.
type parityCase struct {
	goldenID string
	// tempSlots is how many directories the capture allocated for this case, in
	// its order. usesStub says whether the case also resolved through the
	// capture's PATH-seeded stub directory.
	tempSlots int
	usesStub  bool
	mode      agentic.LaunchMode
	// build lays the case's layout down on disk and returns the launch request
	// minus the environment, plus the directories PATH must carry.
	build func(t *testing.T, dirs paritycase.Dirs) (agentic.LaunchRequest, []string)
}

// parityCases covers every qwen golden.
//
// qwen/exec allocates a bin directory and a work directory, in that order,
// exactly as captureBuildCommandSurface does — but its golden lists only
// <TMPDIR:1> in capture.placeholders, because qwen's argv names no directory
// at all and its assignment travels on stdin. The work directory is still
// allocated so the slot indices line up with the capture's.
//
// qwen/dry-run allocates the work directory alone (captureBuildArgsSurface
// takes no bin directory) and resolves its binary through the PATH-seeded stub
// directory, which is why usesStub is set for it and not for exec.
var parityCases = []parityCase{
	{
		goldenID:  "qwen/exec",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.LaunchRequest, []string) {
			binDir, workDir := dirs.Temp[0], dirs.Temp[1]
			paritycase.WriteStubExecutable(t, binDir, executableName)
			req := parityRequest(workDir)
			req.PromptPath = paritycase.WritePromptFile(t, workDir, parityPromptBody)
			return req, []string{binDir}
		},
	},
	{
		goldenID:  "qwen/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.LaunchRequest, []string) {
			paritycase.WriteStubExecutable(t, dirs.Stub, executableName)
			// The source's dry-run config carries the model, the effort and the
			// work directory, and NO prompt file, no run id and no task id. The
			// absent prompt is what makes this a dry run that performs no work:
			// System.Stdin attaches nothing rather than failing on a file that
			// does not exist yet.
			req := agentic.LaunchRequest{
				System:  New().ID(),
				Model:   agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
				Effort:  parityEffort,
				WorkDir: dirs.Temp[0],
			}
			return req, []string{dirs.Stub}
		},
	},
}

// parityRequest is the source's qwen exec config expressed as a LaunchRequest.
// Every field it sets is one the source's config set — and the ones it does NOT
// set matter as much: no BoardDir, no service tier, no profile, no budget and
// no goal, because the qwen capture configured none of them and qwen's adapter
// supports the last three not at all.
func parityRequest(workDir string) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
		Effort:  parityEffort,
		WorkDir: workDir,
		Run: agentic.RunContext{
			RunID:  parityRunID,
			TaskID: parityTaskID,
		},
	}
}

// prepareParityCase lays the case down and returns everything a comparison
// needs: the golden, the directories, and the request with its environment.
func prepareParityCase(t *testing.T, c parityCase) (parity.Golden, paritycase.Dirs, agentic.LaunchRequest) {
	t.Helper()
	g, err := parity.Load(c.goldenID)
	if err != nil {
		t.Fatalf("Load(%q): %v", c.goldenID, err)
	}
	dirs := paritycase.Make(t, c.tempSlots, c.usesStub)
	req, pathDirs := c.build(t, dirs)
	req.Env = paritycase.WithPathEntry(g.Capture.ParentEnv, pathDirs...)
	return g, dirs, req
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

// TestPlansMatchTheQwenGoldens is the acceptance: both captured launch
// surfaces, byte for byte.
func TestPlansMatchTheQwenGoldens(t *testing.T) {
	for _, c := range parityCases {
		t.Run(c.goldenID, func(t *testing.T) {
			g, dirs, req := prepareParityCase(t, c)
			plan := paritycase.BuildPlan(t, New(), req, c.mode)
			if diffs := parity.ComparePlan(g, plan, dirs.Substitutions()); len(diffs) != 0 {
				for _, d := range diffs {
					t.Errorf("%s does not match its golden:\n  %s", c.goldenID, d)
				}
			}
		})
	}
}

// TestEveryQwenGoldenIsCovered fails if the fixture set grows a qwen case this
// file does not build.
//
// Without it, a recapture that adds a third launch surface leaves this suite
// green while covering one combination fewer than it believes — which is the
// exact failure mode the parity package exists to prevent, one level up.
func TestEveryQwenGoldenIsCovered(t *testing.T) {
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
			t.Errorf("golden %q is a qwen case with no parity case in this file; a suite that covers one combination fewer than it believes is the failure this package exists to prevent", id)
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

// TestAWrongPlanFailsAgainstTheQwenGolden is what makes the test above mean
// something.
//
// Four defects, spread over every field family a qwen plan has — an argv flag,
// an argv ARGUMENT, an injected environment variable, and the stdin frame that
// carries the effort — each planted on an otherwise-correct plan built through
// BuildPlan, and each required to be reported IN THE FIELD IT WAS PLANTED IN. A
// mutant that fails for the wrong reason proves nothing about the surface it
// was written for.
func TestAWrongPlanFailsAgainstTheQwenGolden(t *testing.T) {
	mutants := []struct {
		name      string
		subject   string
		defect    string
		wantField string
		system    func(*System) agentic.System
	}{
		{
			name:      "one argv flag misspelled",
			subject:   "qwen/exec",
			defect:    "--approval-mode spelled --approvals-mode, the kind of typo a port makes while retyping a grammar",
			wantField: "Args",
			system:    func(s *System) agentic.System { return renamedFlagSystem{System: s} },
		},
		{
			name:      "the output format silently changed",
			subject:   "qwen/dry-run",
			defect:    "--output-format carries json rather than stream-json; the flag list is identical, so a port that only counted arguments would see nothing wrong, and the child would answer in a shape the launcher cannot parse",
			wantField: "Args",
			system:    func(s *System) agentic.System { return jsonOutputSystem{System: s} },
		},
		{
			name:      "the run id never reaches the child",
			subject:   "qwen/exec",
			defect:    "TASK_BOARD_RUN_ID is not injected, so the child reports against nothing",
			wantField: "EnvAdded",
			system:    func(s *System) agentic.System { return noRunIDSystem{System: s} },
		},
		{
			name:      "the effort is dropped from the initialize frame",
			subject:   "qwen/exec",
			defect:    "the configured reasoning effort leaves the initialize frame empty; the argv is IDENTICAL because qwen carries no effort flag at all, which is exactly the divergence an argv comparison cannot see",
			wantField: "StdinData",
			system:    func(s *System) agentic.System { return effortlessStdinSystem{System: s} },
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			c := parityCaseFor(t, mutant.subject)
			g, dirs, req := prepareParityCase(t, c)

			// The unmutated plan must match first, or a "failing" mutant could
			// be failing for a reason unrelated to the defect it plants.
			clean := paritycase.BuildPlan(t, New(), req, c.mode)
			if diffs := parity.ComparePlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
				t.Fatalf("the unmutated plan already differs, so this mutant proves nothing: %v", diffs)
			}

			plan := paritycase.BuildPlan(t, mutant.system(New()), req, c.mode)
			diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
			if len(diffs) == 0 {
				t.Fatalf("the golden admitted a plan with a real defect (%s); a golden a wrong plan satisfies proves nothing", mutant.defect)
			}
			if !paritycase.NamesField(diffs, mutant.wantField) {
				t.Errorf("the defect was planted in %s but the harness reported %v; a failure naming the wrong surface sends the next reader to the wrong file", mutant.wantField, diffs)
			}
		})
	}
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
		if arg == "--approval-mode" {
			args[i] = "--approvals-mode"
		}
	}
	return args, nil
}

type jsonOutputSystem struct{ agentic.System }

func (m jsonOutputSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(args); i++ {
		if args[i-1] == "--output-format" {
			args[i] = "json"
		}
	}
	return args, nil
}

type noRunIDSystem struct{ agentic.System }

func (m noRunIDSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := m.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	return agentic.SetEnvValue(env, agentic.EnvRunID, ""), nil
}

// effortlessStdinSystem is the plugin as it would behave if a port had read
// buildQwenArgs, found no effort flag, and concluded qwen carries no effort at
// all. It builds the frames from a request with the effort cleared.
type effortlessStdinSystem struct{ agentic.System }

func (m effortlessStdinSystem) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	req.Effort = ""
	return m.System.Stdin(req)
}
