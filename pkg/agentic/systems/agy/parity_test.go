package agy

import (
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// This file is the acceptance. Both agy goldens the source's own harness
// captured are rebuilt here through the REAL entry point — a real
// agentic.Registry holding the real plugin, then agentic.BuildPlan — and
// compared field for field against the fixture.
//
// The two cases are built from DIFFERENT plugin values, and that is the whole
// point of agy's port rather than an accident of the harness:
//
//   - agy/exec's config carries AgyRuntime evidence, so its case registers
//     NewWithRuntime and the binary is the preflighted executable.
//   - agy/dry-run's config carries none, so its case registers New() and the
//     binary is the literal "agy" placeholder.
//
// The fixtures record exactly that difference — <TMPDIR:1>/agy against the bare
// string "agy" — which makes them the evidence that the preflight branch and
// the placeholder branch are both real and are not the same code path wearing
// two hats.

// The source's two agy parity configs, field for field
// (parity_capture_test.go).
const (
	// parityModel carries its effort as a SUFFIX. The source's model row for it
	// is Reasoning: ReasoningNone with the description "Gemini 3.6 Flash with
	// high effort", which is the vendor layer's business; this plugin passes
	// the id through and never reads the suffix.
	parityModel = "gemini-3.6-flash-high"

	parityRunID      = "RUN-parity-agy"
	parityTaskID     = "TASK-parity-agy"
	parityPromptBody = "agy exec-mode prompt"
)

// goldenSystem is the value the fixtures record in their `system` field.
//
// It is "agy", the source's AgentType and this repository's frozen RUNTIME id,
// while this plugin's id is "antigravity" — the Layer-1 plugin id
// docs/architecture.md's table declares. The mapping between the two lives here
// and nowhere else.
const goldenSystem = "agy"

// parityCase is one golden and the machine state that reproduces it.
type parityCase struct {
	goldenID  string
	tempSlots int
	usesStub  bool
	mode      agentic.LaunchMode
	// build lays the case's layout down on disk and returns the plugin value
	// the case's config implies, the launch request minus the environment, and
	// the directories PATH must carry.
	build func(t *testing.T, dirs paritycase.Dirs) (*System, agentic.LaunchRequest, []string)
}

// parityCases covers every agy golden.
var parityCases = []parityCase{
	{
		goldenID:  "agy/exec",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs paritycase.Dirs) (*System, agentic.LaunchRequest, []string) {
			binDir, workDir := dirs.Temp[0], dirs.Temp[1]
			// The source's capture writes the stub into the bin directory and
			// hands its path to the config as AgyRuntime.Executable. It is on
			// PATH as well, incidentally, and that is worth noting rather than
			// relying on: this plugin never looks at PATH, so the binary in the
			// golden is the EVIDENCE's path and would be identical if the stub
			// were somewhere PATH never mentioned.
			executable := paritycase.WriteStubExecutable(t, binDir, "agy")
			system := NewWithRuntime(Runtime{Executable: executable})
			req := parityRequest(workDir)
			req.PromptPath = paritycase.WritePromptFile(t, workDir, parityPromptBody)
			return system, req, []string{binDir}
		},
	},
	{
		goldenID:  "agy/dry-run",
		tempSlots: 1,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (*System, agentic.LaunchRequest, []string) {
			// NO stub and NO evidence. The source's dry-run config supplies no
			// AgyRuntime, which is why its golden's binary is the bare
			// placeholder — and why this case allocates no stub bin directory
			// at all, unlike every other dry-run case in this repository.
			// Nothing here resolves anything from PATH.
			return New(), agentic.LaunchRequest{
				System:  New().ID(),
				Model:   agentic.Model{ID: parityModel},
				WorkDir: dirs.Temp[0],
			}, nil
		},
	},
}

// parityRequest is the source's agy exec config expressed as a LaunchRequest.
// The fields it does NOT set matter as much as the ones it does: no effort
// (agy carries none and the effort is in the model id), no BoardDir, no goal,
// no budget, no service tier, no composition.
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

func prepareParityCase(t *testing.T, c parityCase) (parity.Golden, paritycase.Dirs, *System, agentic.LaunchRequest) {
	t.Helper()
	g, err := parity.Load(c.goldenID)
	if err != nil {
		t.Fatalf("Load(%q): %v", c.goldenID, err)
	}
	dirs := paritycase.Make(t, c.tempSlots, c.usesStub)
	system, req, pathDirs := c.build(t, dirs)
	if len(pathDirs) == 0 {
		// The dry-run case deliberately carries no PATH: agy resolves nothing
		// from it, and an empty environment is what proves that.
		req.Env = append([]string(nil), g.Capture.ParentEnv...)
	} else {
		req.Env = paritycase.WithPathEntry(g.Capture.ParentEnv, pathDirs...)
	}
	return g, dirs, system, req
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

// TestPlansMatchTheAgyGoldens is the acceptance: both captured launch surfaces,
// byte for byte.
func TestPlansMatchTheAgyGoldens(t *testing.T) {
	for _, c := range parityCases {
		t.Run(c.goldenID, func(t *testing.T) {
			g, dirs, system, req := prepareParityCase(t, c)
			plan := paritycase.BuildPlan(t, system, req, c.mode)
			if diffs := parity.ComparePlan(g, plan, dirs.Substitutions()); len(diffs) != 0 {
				for _, d := range diffs {
					t.Errorf("%s does not match its golden:\n  %s", c.goldenID, d)
				}
			}
		})
	}
}

// TestTheDryRunGoldenPinsThePlaceholderBranch states what the fixture is doing
// for a reader who might otherwise take "agy" for a resolved path.
//
// The goldens' README says it explicitly — "agy/dry-run records the placeholder
// binary on purpose" — and this is that claim held to the code: the fixture's
// binary is the LITERAL, not a path, and the plugin that produced it carries no
// evidence.
func TestTheDryRunGoldenPinsThePlaceholderBranch(t *testing.T) {
	g, err := parity.Load("agy/dry-run")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.Surface.Binary != displayPlaceholder {
		t.Fatalf("agy/dry-run records binary %q, want the bare placeholder %q; the fixture and this plugin disagree about which branch it pins", g.Surface.Binary, displayPlaceholder)
	}
	if New().binary() != displayPlaceholder {
		t.Errorf("a plugin with no preflight evidence resolves %q rather than the placeholder", New().binary())
	}
}

// TestEveryAgyGoldenIsCovered fails if the fixture set grows an agy case this
// file does not build.
func TestEveryAgyGoldenIsCovered(t *testing.T) {
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
			t.Errorf("golden %q is an agy case with no parity case in this file", id)
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

// TestAWrongPlanFailsAgainstTheAgyGolden is what makes the acceptance mean
// something.
//
// Five defects, spread over every field family an agy plan has, each planted on
// an otherwise-correct plan built through BuildPlan and each required to be
// reported IN THE FIELD IT WAS PLANTED IN.
//
// The first is the source's OWN historical bug, reproduced deliberately:
// BuildArgs used to hardcode "agy" unconditionally, never consulting
// cfg.AgyRuntime even when it was already known. That is what the parity
// capture was written to catch, and it must still be caught here.
func TestAWrongPlanFailsAgainstTheAgyGolden(t *testing.T) {
	mutants := []struct {
		name      string
		subject   string
		defect    string
		wantField string
		system    func(*System) agentic.System
	}{
		{
			name:      "the resolved binary is replaced by the display placeholder",
			subject:   "agy/exec",
			defect:    "the plan reports the bare `agy` string while the preflight had already resolved an executable; this is the source's own AC3 bug, and a launcher acting on it would exec whatever PATH happened to hold rather than the binary whose headless contract was validated",
			wantField: "Binary",
			system:    func(s *System) agentic.System { return placeholderBinarySystem{System: s} },
		},
		{
			name:      "one argv flag misspelled",
			subject:   "agy/exec",
			defect:    "--disable-slash-commands spelled --disable-slash-command, the kind of typo a port makes while retyping a grammar",
			wantField: "Args",
			system:    func(s *System) agentic.System { return renamedFlagSystem{System: s} },
		},
		{
			name:      "the effort suffix is stripped from the model id",
			subject:   "agy/exec",
			defect:    "the plugin parses `-high` off the model id and passes `gemini-3.6-flash`; the flag list is identical, and the child would run at whatever effort Antigravity defaults that model to — which is the wrong-cost launch this system's whole effort story exists to prevent",
			wantField: "Args",
			system:    func(s *System) agentic.System { return suffixStrippingSystem{System: s} },
		},
		{
			name:      "the print timeout is changed",
			subject:   "agy/dry-run",
			defect:    "--print-timeout carries 10m rather than 30m; the argument COUNT is identical, and a tracked run would be killed mid-task by a limit nobody configured",
			wantField: "Args",
			system:    func(s *System) agentic.System { return shortTimeoutSystem{System: s} },
		},
		{
			name:      "the task id never reaches the child",
			subject:   "agy/exec",
			defect:    "TASK_BOARD_TASK_ID is not injected, so the child cannot report against the task it was launched for",
			wantField: "EnvAdded",
			system:    func(s *System) agentic.System { return noTaskIDSystem{System: s} },
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			c := parityCaseFor(t, mutant.subject)
			g, dirs, system, req := prepareParityCase(t, c)

			clean := paritycase.BuildPlan(t, system, req, c.mode)
			if diffs := parity.ComparePlan(g, clean, dirs.Substitutions()); len(diffs) != 0 {
				t.Fatalf("the unmutated plan already differs, so this mutant proves nothing: %v", diffs)
			}

			plan := paritycase.BuildPlan(t, mutant.system(system), req, c.mode)
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

// placeholderBinarySystem is the source's pre-refactor BuildArgs: the display
// string, unconditionally, whether or not a preflight has run.
type placeholderBinarySystem struct{ agentic.System }

func (m placeholderBinarySystem) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return displayPlaceholder, nil
}

type renamedFlagSystem struct{ agentic.System }

func (m renamedFlagSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i, arg := range args {
		if arg == "--disable-slash-commands" {
			args[i] = "--disable-slash-command"
		}
	}
	return args, nil
}

// suffixStrippingSystem is the plugin as it would behave if it had learned the
// vendor layer's effort vocabulary — the exact thing agy.go says it must never
// do.
type suffixStrippingSystem struct{ agentic.System }

func (m suffixStrippingSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	for _, suffix := range []string{"-high", "-medium", "-low"} {
		if len(req.Model.ID) > len(suffix) && req.Model.ID[len(req.Model.ID)-len(suffix):] == suffix {
			req.Model.ID = req.Model.ID[:len(req.Model.ID)-len(suffix)]
			break
		}
	}
	return m.System.Argv(req, mode)
}

type shortTimeoutSystem struct{ agentic.System }

func (m shortTimeoutSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(args); i++ {
		if args[i-1] == "--print-timeout" {
			args[i] = "10m"
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
