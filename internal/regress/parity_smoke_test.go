package regress

import (
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"
)

// CLASS 4 — one launch-surface golden per Layer-1 system, rebuilt through the
// real registry and agentic.BuildPlan.
//
// This is a SMOKE, and saying so is not a hedge. Each plugin's own
// parity_test.go remains the acceptance: it covers every golden its system
// has, with the mutants that hold each field. What this file covers is the
// failure those files are structurally bad at seeing — a change in the CORE
// that breaks all six at once and is diagnosed six times in six packages
// before anyone notices it is one bug. Six cases, one per system, under a
// second.
//
// Nothing here reads an expected value out of the golden it compares to. The
// requests are the source capture's own configs, spelled out, so a comparison
// that passes means the plan reproduces the captured surface rather than that
// JSON round-trips.

// smokeCase is one golden and the machine state that reproduces it.
type smokeCase struct {
	goldenID  string
	tempSlots int
	usesStub  bool
	// noPath withholds the PATH entry entirely. agy's dry run resolves nothing
	// from PATH, and an environment with none is what proves it.
	noPath bool
	mode   agentic.LaunchMode
	build  func(t *testing.T, dirs paritycase.Dirs) (agentic.System, agentic.LaunchRequest, []string)
}

// smokeCases is one case per Layer-1 plugin. The dry-run golden is preferred
// where a system has one — it is the cheapest surface that still exercises
// binary resolution and the whole argv grammar. claude has no dry-run capture,
// so its exec prompt-mode golden stands in.
var smokeCases = []smokeCase{
	{
		goldenID:  "codex/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.System, agentic.LaunchRequest, []string) {
			paritycase.WriteStubExecutable(t, dirs.Stub, "codex")
			workDir := dirs.Temp[0]
			return codex.New(), agentic.LaunchRequest{
				System:      codex.New().ID(),
				Model:       agentic.Model{ID: "gpt-5.6-terra", Effort: agentic.EffortSupportRequired},
				Effort:      "high",
				ServiceTier: "priority",
				Profile:     "parity-profile",
				WorkDir:     workDir,
				Run: agentic.RunContext{
					RunID:    "RUN-parity-codex",
					TaskID:   "TASK-parity-codex",
					BoardDir: filepath.Join(workDir, ".task-board"),
				},
			}, []string{dirs.Stub}
		},
	},
	{
		// claude captured no dry run, so the smoke drives its exec surface:
		// a stub binary, an assignment on disk, and the environment strip.
		goldenID:  "claude/prompt-mode",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.System, agentic.LaunchRequest, []string) {
			binDir, workDir := dirs.Temp[0], dirs.Temp[1]
			paritycase.WriteStubExecutable(t, binDir, "claude")
			return claude.New(), agentic.LaunchRequest{
				System:     claude.New().ID(),
				Model:      agentic.Model{ID: "claude-opus-5", Effort: agentic.EffortSupportRequired},
				Effort:     "high",
				WorkDir:    workDir,
				PromptPath: paritycase.WritePromptFile(t, workDir, "claude prompt-mode body"),
				Run: agentic.RunContext{
					RunID:  "RUN-parity-claude",
					TaskID: "TASK-parity-claude",
				},
			}, []string{binDir}
		},
	},
	{
		goldenID:  "qwen/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.System, agentic.LaunchRequest, []string) {
			paritycase.WriteStubExecutable(t, dirs.Stub, "qwen")
			return qwen.New(), agentic.LaunchRequest{
				System:  qwen.New().ID(),
				Model:   agentic.Model{ID: "qwen3.7-max", Effort: agentic.EffortSupportRequired},
				Effort:  "high",
				WorkDir: dirs.Temp[0],
			}, []string{dirs.Stub}
		},
	},
	{
		goldenID:  "gemini/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.System, agentic.LaunchRequest, []string) {
			paritycase.WriteStubExecutable(t, dirs.Stub, "gemini")
			return gemini.New(), agentic.LaunchRequest{
				System:  gemini.New().ID(),
				Model:   agentic.Model{ID: "gemini-3.5-flash"},
				WorkDir: dirs.Temp[0],
			}, []string{dirs.Stub}
		},
	},
	{
		goldenID:  "muse/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.System, agentic.LaunchRequest, []string) {
			paritycase.WriteStubExecutable(t, dirs.Stub, "muse")
			return muse.New(), agentic.LaunchRequest{
				System:  muse.New().ID(),
				Model:   agentic.Model{ID: "muse-spark"},
				WorkDir: dirs.Temp[0],
			}, []string{dirs.Stub}
		},
	},
	{
		// agy's dry run carries NO preflight evidence, which is why its
		// golden's binary is the bare placeholder rather than a path — and why
		// this case seeds neither a stub nor a PATH.
		goldenID:  "agy/dry-run",
		tempSlots: 1,
		noPath:    true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs paritycase.Dirs) (agentic.System, agentic.LaunchRequest, []string) {
			return agy.New(), agentic.LaunchRequest{
				System:  agy.New().ID(),
				Model:   agentic.Model{ID: "gemini-3.6-flash-high"},
				WorkDir: dirs.Temp[0],
			}, nil
		},
	},
}

func prepareSmokeCase(t *testing.T, c smokeCase) (parity.Golden, paritycase.Dirs, agentic.System, agentic.LaunchRequest) {
	t.Helper()
	g, err := parity.Load(c.goldenID)
	if err != nil {
		t.Fatalf("Load(%q): %v", c.goldenID, err)
	}
	dirs := paritycase.Make(t, c.tempSlots, c.usesStub)
	system, req, pathDirs := c.build(t, dirs)
	if c.noPath {
		req.Env = append([]string(nil), g.Capture.ParentEnv...)
	} else {
		req.Env = paritycase.WithPathEntry(g.Capture.ParentEnv, pathDirs...)
	}
	return g, dirs, system, req
}

// TestOneGoldenPerSystemStillBuilds is the smoke.
func TestOneGoldenPerSystemStillBuilds(t *testing.T) {
	for _, c := range smokeCases {
		t.Run(c.goldenID, func(t *testing.T) {
			g, dirs, system, req := prepareSmokeCase(t, c)
			plan := paritycase.BuildPlan(t, system, req, c.mode)
			if diffs := parity.ComparePlan(g, plan, dirs.Substitutions()); len(diffs) != 0 {
				for _, d := range diffs {
					t.Errorf("%s does not match its golden:\n  %s", c.goldenID, d)
				}
			}
		})
	}
}

// TestEveryLayerOneSystemHasASmokeCase fails if a seventh plugin lands and
// this file does not grow with it.
//
// Without it the smoke would silently stop covering the newest system, which
// is the one most likely to need it. The plugin ids come from the default
// registry — every plugin registers into it from its own init — rather than
// from a list here.
func TestEveryLayerOneSystemHasASmokeCase(t *testing.T) {
	covered := map[agentic.SystemID]string{}
	for _, c := range smokeCases {
		g, err := parity.Load(c.goldenID)
		if err != nil {
			t.Fatalf("Load(%q): %v", c.goldenID, err)
		}
		_, _, system, _ := prepareSmokeCase(t, c)
		if existing, already := covered[system.ID()]; already {
			t.Errorf("system %q has two smoke cases (%s and %s); the smoke is one per system by design", system.ID(), existing, c.goldenID)
		}
		covered[system.ID()] = g.ID
	}
	ids := agentic.Default.IDs()
	if len(ids) == 0 {
		t.Fatal("no agentic system plugin is compiled into this test binary, so this check ranged over nothing")
	}
	for _, id := range ids {
		if _, ok := covered[id]; !ok {
			t.Errorf("agentic system %q is registered and has no smoke case in this file", id)
		}
	}
}

// TestAWrongPlanFailsTheSmoke is what makes the smoke mean something.
//
// Two defects, planted on an otherwise-correct plan built through the real
// BuildPlan, applied to EVERY case: the binary the launch would actually run,
// and the argv it would run with. Each must be reported in the field it was
// planted in — a failure naming the wrong surface sends the next reader to the
// wrong file.
//
// They are deliberately generic rather than system-specific. A per-system
// defect is the per-plugin suite's job; what this one has to prove is that the
// comparison in this file is wired to a real plan at all, for all six.
func TestAWrongPlanFailsTheSmoke(t *testing.T) {
	mutants := []struct {
		name      string
		wantField string
		defect    string
		wrap      func(agentic.System) agentic.System
	}{
		{
			name:      "the resolved binary is not the one that would run",
			wantField: "Binary",
			defect:    "ResolveBinary answers a plausible-looking path that is not the launch target — the source's own display-placeholder bug, which no argv comparison can see",
			wrap:      func(s agentic.System) agentic.System { return wrongBinarySystem{System: s} },
		},
		{
			name:      "the last argument is dropped",
			wantField: "Args",
			defect:    "Argv loses its final element, the shape a retyped grammar produces; for most systems that is the model, the workspace or the prompt",
			wrap:      func(s agentic.System) agentic.System { return truncatedArgvSystem{System: s} },
		},
	}
	for _, c := range smokeCases {
		for _, mutant := range mutants {
			t.Run(c.goldenID+"/"+mutant.name, func(t *testing.T) {
				g, dirs, system, req := prepareSmokeCase(t, c)
				plan := paritycase.BuildPlan(t, mutant.wrap(system), req, c.mode)
				diffs := parity.ComparePlan(g, plan, dirs.Substitutions())
				if len(diffs) == 0 {
					t.Fatalf("%s: %s, and the smoke reported no difference", c.goldenID, mutant.defect)
				}
				if !paritycase.NamesField(diffs, mutant.wantField) {
					t.Errorf("%s: the defect was planted in %s and the smoke reported %v", c.goldenID, mutant.wantField, diffs)
				}
			})
		}
	}
}

// wrongBinarySystem answers a path that is not the one the launch resolved.
type wrongBinarySystem struct{ agentic.System }

func (w wrongBinarySystem) ResolveBinary(req agentic.LaunchRequest) (string, error) {
	resolved, err := w.System.ResolveBinary(req)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(resolved), "not-"+filepath.Base(resolved)), nil
}

// truncatedArgvSystem drops the last argument.
type truncatedArgvSystem struct{ agentic.System }

func (a truncatedArgvSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	argv, err := a.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return argv, nil
	}
	return argv[:len(argv)-1], nil
}
