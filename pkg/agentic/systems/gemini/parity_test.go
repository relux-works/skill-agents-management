package gemini

import (
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// This file is the acceptance. Every gemini golden the source's own harness
// captured is rebuilt here through the REAL entry point — a real
// agentic.Registry holding the real plugin, then agentic.BuildPlan — and
// compared field for field against the fixture.
//
// Nothing in it constructs a Plan by hand and nothing reads an expected value
// out of the golden it is comparing to.

// The source's two gemini parity configs, field for field
// (parity_capture_test.go).
const (
	parityModel = "gemini-3.5-flash"

	parityRunID      = "RUN-parity-gemini"
	parityTaskID     = "TASK-parity-gemini"
	parityPromptBody = "gemini exec-mode prompt"
)

// goldenSystem is the value the fixtures record in their `system` field.
//
// It is "gemini", the source's AgentType and this repository's frozen RUNTIME
// id, while this plugin's id is "gemini-cli" — the Layer-1 plugin id
// docs/architecture.md's table declares. The mapping between the two lives here
// and nowhere else, because a second copy of it is exactly the two-spellings
// disease the registry's normalization exists to prevent.
const goldenSystem = "gemini"

// parityCase is one golden and the machine state that reproduces it.
type parityCase struct {
	goldenID  string
	tempSlots int
	usesStub  bool
	mode      agentic.LaunchMode
	build     func(t *testing.T, dirs paritycase.Dirs) (agentic.LaunchRequest, []string)
}

// parityCases covers every gemini golden.
//
// gemini/exec allocates a bin directory and a work directory, in that order,
// exactly as captureBuildCommandSurface does — and BOTH reach the surface,
// which is why its golden lists two placeholders: the binary is in the first
// and `--include-directories` names the second.
//
// gemini/dry-run allocates the work directory alone and resolves its binary
// through the PATH-seeded stub directory, so <TMPDIR:1> is the WORK directory
// there and the binary is <PARITY-BIN>. Reading that mapping off the fixture
// rather than assuming slot 1 is always the bin directory is the whole reason
// capture.placeholders exists.
var parityCases = []parityCase{
	{
		goldenID:  "gemini/exec",
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
		goldenID:  "gemini/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.LaunchRequest, []string) {
			paritycase.WriteStubExecutable(t, dirs.Stub, executableName)
			// The source's dry-run config carries the model and the work
			// directory and NOTHING else — no prompt file, no run id, no task
			// id. The absent prompt is what makes this a dry run that performs
			// no work: System.Stdin attaches nothing rather than failing on a
			// file that does not exist yet, and Args substitutes the readable
			// placeholder into `-p`.
			return agentic.LaunchRequest{
				System:  New().ID(),
				Model:   agentic.Model{ID: parityModel},
				WorkDir: dirs.Temp[0],
			}, []string{dirs.Stub}
		},
	},
}

// parityRequest is the source's gemini exec config expressed as a
// LaunchRequest. The fields it does NOT set matter as much as the ones it does:
// no effort (gemini carries none and every model under it is effort-none), no
// BoardDir, no goal, no budget, no service tier, no composition.
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

// TestPlansMatchTheGeminiGoldens is the acceptance: both captured launch
// surfaces, byte for byte.
func TestPlansMatchTheGeminiGoldens(t *testing.T) {
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

// TestEveryGeminiGoldenIsCovered fails if the fixture set grows a gemini case
// this file does not build. Without it, a recapture that adds a third launch
// surface leaves this suite green while covering one combination fewer than it
// believes.
func TestEveryGeminiGoldenIsCovered(t *testing.T) {
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
			t.Errorf("golden %q is a gemini case with no parity case in this file", id)
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

// TestAWrongPlanFailsAgainstTheGeminiGolden is what makes the test above mean
// something.
//
// Four defects, spread over every field family a gemini plan has, each planted
// on an otherwise-correct plan built through BuildPlan and each required to be
// reported IN THE FIELD IT WAS PLANTED IN.
//
// The first one is the defect this system is most likely to suffer: putting the
// assignment in `-p` as well as on stdin. The child would receive it twice,
// because gemini appends what it reads on stdin to the `-p` value — and a port
// author who did not read that comment would think an empty `-p` was a bug.
func TestAWrongPlanFailsAgainstTheGeminiGolden(t *testing.T) {
	mutants := []struct {
		name      string
		subject   string
		defect    string
		wantField string
		system    func(*System) agentic.System
	}{
		{
			name:      "the prompt is put in argv as well as on stdin",
			subject:   "gemini/exec",
			defect:    "-p carries the assignment text while stdin also streams it; gemini appends stdin to the -p value, so the child would read the assignment twice",
			wantField: "Args",
			system:    func(s *System) agentic.System { return promptInArgvSystem{System: s} },
		},
		{
			name:      "the workspace is not passed",
			subject:   "gemini/exec",
			defect:    "--include-directories is dropped, so the child cannot read the workspace it was launched for",
			wantField: "Args",
			system:    func(s *System) agentic.System { return noWorkspaceSystem{System: s} },
		},
		{
			name:      "the dry run invents its own placeholder",
			subject:   "gemini/dry-run",
			defect:    "-p carries <assignment> rather than the source's <prompt>; the argument COUNT is identical, which is exactly what an argv-length check cannot see",
			wantField: "Args",
			system:    func(s *System) agentic.System { return otherPlaceholderSystem{System: s} },
		},
		{
			name:      "the task id never reaches the child",
			subject:   "gemini/exec",
			defect:    "TASK_BOARD_TASK_ID is not injected, so the child cannot report against the task it was launched for",
			wantField: "EnvAdded",
			system:    func(s *System) agentic.System { return noTaskIDSystem{System: s} },
		},
		{
			name:      "one stdin byte changed",
			subject:   "gemini/exec",
			defect:    "a single byte of the assignment prompt differs; the argv is identical, which is exactly the divergence argv-string equality cannot see",
			wantField: "StdinData",
			system:    func(s *System) agentic.System { return mutatedStdinSystem{System: s} },
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

type promptInArgvSystem struct{ agentic.System }

func (m promptInArgvSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	payload, err := m.System.Stdin(req)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(args); i++ {
		if args[i-1] == "-p" {
			args[i] = string(payload.Bytes)
		}
	}
	return args, nil
}

type noWorkspaceSystem struct{ agentic.System }

func (m noWorkspaceSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	req.WorkDir = ""
	return m.System.Argv(req, mode)
}

type otherPlaceholderSystem struct{ agentic.System }

func (m otherPlaceholderSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i, arg := range args {
		if arg == promptPlaceholder {
			args[i] = "<assignment>"
		}
	}
	return args, nil
}

type noTaskIDSystem struct{ agentic.System }

func (m noTaskIDSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := m.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	return agentic.SetEnvValue(env, agentic.EnvTaskID, ""), nil
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
