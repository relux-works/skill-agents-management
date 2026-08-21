package muse

import (
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// This file is the acceptance. Every muse golden the source's own harness
// captured is rebuilt here through the REAL entry point — a real
// agentic.Registry holding the real plugin, then agentic.BuildPlan — and
// compared field for field against the fixture.

// The source's two muse parity configs, field for field
// (parity_capture_test.go).
const (
	parityModel = "muse-spark"

	parityRunID      = "RUN-parity-muse"
	parityTaskID     = "TASK-parity-muse"
	parityPromptBody = "muse exec-mode prompt"
)

// goldenSystem is the value the fixtures record in their `system` field.
//
// For muse it is the same spelling as this plugin's id, which is the one place
// in this repository where those two facts coincide. They are still two facts:
// `muse` here is the frozen RUNTIME id the goldens record, and systemID is the
// Layer-1 plugin id docs/architecture.md declares. Keeping them as separate
// constants is what makes the day one of them moves a compile error rather than
// a silent agreement.
const goldenSystem = "muse"

// parityCase is one golden and the machine state that reproduces it.
type parityCase struct {
	goldenID  string
	tempSlots int
	usesStub  bool
	mode      agentic.LaunchMode
	build     func(t *testing.T, dirs paritycase.Dirs) (agentic.LaunchRequest, []string)
}

// parityCases covers every muse golden.
//
// muse/exec allocates a bin directory and a work directory, in that order, and
// BOTH reach the surface: the binary is in the first, and `--workspace` and
// `--prompt-file` both name the second. That is why its golden's
// `--prompt-file` argument reads <TMPDIR:2>/prompt.md — the assignment is
// written INSIDE the work directory, so a port that put it anywhere else would
// differ from the fixture in a way no amount of masking could hide.
//
// muse/dry-run allocates the work directory alone and resolves its binary
// through the PATH-seeded stub directory, so <TMPDIR:1> is the WORK directory
// there and the prompt file is the source's placeholder.
var parityCases = []parityCase{
	{
		goldenID:  "muse/exec",
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
		goldenID:  "muse/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.LaunchRequest, []string) {
			paritycase.WriteStubExecutable(t, dirs.Stub, executableName)
			// The source's dry-run config carries the model and the work
			// directory and nothing else — no prompt file, no run id, no task
			// id — which is what makes Args substitute its placeholder.
			return agentic.LaunchRequest{
				System:  New().ID(),
				Model:   agentic.Model{ID: parityModel},
				WorkDir: dirs.Temp[0],
			}, []string{dirs.Stub}
		},
	},
}

// parityRequest is the source's muse exec config expressed as a LaunchRequest.
// The fields it does NOT set matter as much as the ones it does: no effort
// (muse carries none), no BoardDir, no goal, no budget, no service tier, no
// composition.
func parityRequest(workDir string) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel},
		WorkDir: workDir,
		Run: agentic.RunContext{
			RunID:  parityRunID,
			TaskID: parityTaskID,
		},
	}
}

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

// TestPlansMatchTheMuseGoldens is the acceptance: both captured launch
// surfaces, byte for byte.
func TestPlansMatchTheMuseGoldens(t *testing.T) {
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

// TestEveryMuseGoldenIsCovered fails if the fixture set grows a muse case this
// file does not build.
func TestEveryMuseGoldenIsCovered(t *testing.T) {
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
			t.Errorf("golden %q is a muse case with no parity case in this file", id)
		}
	}
	if seen == 0 {
		t.Fatalf("no fixture records system %q, so this coverage check ranged over nothing", goldenSystem)
	}
	for id := range covered {
		if _, ok := all[id]; !ok {
			t.Errorf("this file builds %q, which is not a captured golden", id)
		}
	}
}

// TestAWrongPlanFailsAgainstTheMuseGolden is what makes the test above mean
// something.
//
// Five defects, spread over every field family a muse plan has, each planted on
// an otherwise-correct plan built through BuildPlan and each required to be
// reported IN THE FIELD IT WAS PLANTED IN.
//
// The stdin one is muse-specific and is the reason it is here: muse attaches
// NOTHING, so a port that streamed the assignment as well as naming it would
// hand the child two copies through two transports. StdinKind is a field the
// harness compares, and this is the only ported system for which "none" is the
// contract in exec mode.
func TestAWrongPlanFailsAgainstTheMuseGolden(t *testing.T) {
	mutants := []struct {
		name      string
		subject   string
		defect    string
		wantField string
		system    func(*System) agentic.System
	}{
		{
			name:      "one argv flag misspelled",
			subject:   "muse/exec",
			defect:    "--yolo spelled --yes, the kind of typo a port makes while retyping a grammar; the child would then stop and ask a human that nobody is there to answer",
			wantField: "Args",
			system:    func(s *System) agentic.System { return renamedFlagSystem{System: s} },
		},
		{
			name:      "the assignment is also streamed on stdin",
			subject:   "muse/exec",
			defect:    "the prompt file is named in argv AND streamed on stdin; the argv is identical, so only the stdin field can see it, and the child would read the assignment through two transports",
			wantField: "StdinKind",
			system:    func(s *System) agentic.System { return streamingStdinSystem{System: s} },
		},
		{
			name:      "the workspace is not passed",
			subject:   "muse/exec",
			defect:    "--workspace is dropped, so the child works in whatever directory the launcher happened to be in",
			wantField: "Args",
			system:    func(s *System) agentic.System { return noWorkspaceSystem{System: s} },
		},
		{
			name:      "the dry run invents its own placeholder",
			subject:   "muse/dry-run",
			defect:    "--prompt-file carries <assignment-prompt-file> — claude's placeholder — rather than muse's <prompt-file>; the argument COUNT is identical, which is exactly what an argv-length check cannot see",
			wantField: "Args",
			system:    func(s *System) agentic.System { return otherPlaceholderSystem{System: s} },
		},
		{
			name:      "the run id never reaches the child",
			subject:   "muse/exec",
			defect:    "TASK_BOARD_RUN_ID is not injected, so the child reports against nothing",
			wantField: "EnvAdded",
			system:    func(s *System) agentic.System { return noRunIDSystem{System: s} },
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			c := parityCaseFor(t, mutant.subject)
			g, dirs, req := prepareParityCase(t, c)

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
				t.Errorf("the defect was planted in %s but the harness reported %v", mutant.wantField, diffs)
			}
		})
	}
}

// The mutant systems. Each embeds the real plugin and overrides exactly one
// surface.

type renamedFlagSystem struct{ agentic.System }

func (m renamedFlagSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i, arg := range args {
		if arg == "--yolo" {
			args[i] = "--yes"
		}
	}
	return args, nil
}

// streamingStdinSystem is the plugin as it would behave if a port had assumed
// every harness takes its assignment on stdin.
type streamingStdinSystem struct{ agentic.System }

func (m streamingStdinSystem) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{Attached: true, Bytes: []byte(parityPromptBody)}, nil
}

type noWorkspaceSystem struct{ agentic.System }

func (m noWorkspaceSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--workspace" {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out, nil
}

type otherPlaceholderSystem struct{ agentic.System }

func (m otherPlaceholderSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i, arg := range args {
		if arg == promptFilePlaceholder {
			args[i] = "<assignment-prompt-file>"
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
