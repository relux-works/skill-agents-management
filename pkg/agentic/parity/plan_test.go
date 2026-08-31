package parity

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file drives the REAL entry point — agentic.BuildPlan, through a real
// agentic.Registry — and compares the plan it returns to a golden captured by
// the source repository. It is the shape every port task will use, demonstrated
// once here so that the harness's ability to bite is established before the
// first port depends on it.
//
// The systems below are PROBES, not ports. They exist to produce a plan this
// package can compare; they are not the claude-code, codex or muse plugins, and
// nothing outside this file registers them. A port lands its own System in its
// own package and proves itself against the same golden through the same
// ComparePlan call.

const probeID agentic.SystemID = "parity-probe"

// probeSystem is a System whose every answer is precomputed by a recipe. The
// launch surface is therefore hand-written from the source's argv grammar,
// which is what makes the comparison meaningful: a probe that read its argv
// back out of the golden would prove only that JSON round-trips.
type probeSystem struct {
	caps agentic.Capabilities

	binary    string
	argv      []string
	stripKeys []string
	inject    []string
	stdin     agentic.StdinPayload
}

func (p *probeSystem) ID() agentic.SystemID                                { return probeID }
func (p *probeSystem) Capabilities() agentic.Capabilities                  { return p.caps }
func (p *probeSystem) ResolveBinary(agentic.LaunchRequest) (string, error) { return p.binary, nil }

func (p *probeSystem) Argv(agentic.LaunchRequest, agentic.LaunchMode) ([]string, error) {
	return append([]string(nil), p.argv...), nil
}

func (p *probeSystem) ChildEnv(parent []string, _ agentic.LaunchRequest) ([]string, error) {
	child := make([]string, 0, len(parent)+len(p.inject))
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		stripped := false
		for _, drop := range p.stripKeys {
			if key == drop {
				stripped = true
				break
			}
		}
		if !stripped {
			child = append(child, entry)
		}
	}
	return append(child, p.inject...), nil
}

func (p *probeSystem) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return p.stdin, nil
}

func (p *probeSystem) ValidateComposition(c agentic.Composition) error {
	if !c.IsZero() {
		return errors.New("parity probe declares no composition grammar")
	}
	return nil
}

// probeRecipe builds one system plus the launch request and machine-local
// directories that reproduce one golden.
type probeRecipe struct {
	goldenID string
	// tempSlots is how many directories the source harness allocated for this
	// case, in the same order. The golden's capture.placeholders block is where
	// a port reads this number off rather than guessing it.
	tempSlots int
	usesStub  bool
	mode      agentic.LaunchMode
	build     func(dirs probeDirs) (*probeSystem, agentic.LaunchRequest)
}

// probeDirs are this test process's stand-ins for the capture's machine-local
// directories. They are real t.TempDir values, so the masking under test is
// doing real work rather than rewriting a literal that was already a
// placeholder.
type probeDirs struct {
	temp []string
	stub string
}

func (d probeDirs) substitutions() Substitutions {
	return Substitutions{TempSlots: d.temp, StubBinDir: d.stub}
}

// parityProbeRecipes covers one golden of each shape the harness has to handle:
// stdin bytes with an environment diff, no stdin with temp paths in argv, a dry
// run whose binary resolves through the PATH-seeded stub directory, and the
// heaviest environment filter in the source (qwen), whose preservation bound is
// what TestAWholeEnvironmentWipeFailsAgainstQwenExec attacks.
var parityProbeRecipes = []probeRecipe{
	{
		goldenID:  "claude/prompt-mode",
		tempSlots: 1,
		mode:      agentic.LaunchModeExec,
		build: func(dirs probeDirs) (*probeSystem, agentic.LaunchRequest) {
			req := agentic.LaunchRequest{
				System:  probeID,
				Model:   agentic.Model{ID: "claude-opus-5", Effort: agentic.EffortSupportRequired},
				Effort:  "high",
				Prompt:  []byte("claude prompt-mode body"),
				WorkDir: dirs.temp[0],
			}
			sys := &probeSystem{
				caps: agentic.Capabilities{
					LaunchModes:     []agentic.LaunchMode{agentic.LaunchModeExec},
					EffortTransport: agentic.EffortTransportArgv,
					DefaultHome:     "~/.claude",
				},
				binary: dirs.temp[0] + "/claude",
				argv: []string{
					"-p", "--output-format", "json",
					"--model", req.Model.ID,
					"--effort", req.Effort,
					"--dangerously-skip-permissions",
				},
				stripKeys: []string{"CLAUDECODE", "TASK_BOARD_BOARD_DIR", "TASK_BOARD_DELIVERY_GOAL_ID", "TASK_BOARD_RUN_ID", "TASK_BOARD_TASK_ID"},
				inject:    []string{"TASK_BOARD_RUN_ID=RUN-parity-claude", "TASK_BOARD_TASK_ID=TASK-parity-claude"},
				stdin:     agentic.StdinPayload{Attached: true, Bytes: []byte("claude prompt-mode body")},
			}
			return sys, req
		},
	},
	{
		goldenID:  "muse/exec",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(dirs probeDirs) (*probeSystem, agentic.LaunchRequest) {
			req := agentic.LaunchRequest{
				System:     probeID,
				Model:      agentic.Model{ID: "muse-spark"},
				PromptPath: dirs.temp[1] + "/prompt.md",
				WorkDir:    dirs.temp[1],
			}
			sys := &probeSystem{
				caps: agentic.Capabilities{
					LaunchModes: []agentic.LaunchMode{agentic.LaunchModeExec},
				},
				binary: dirs.temp[0] + "/muse",
				argv: []string{
					"exec", "--json", "--yolo",
					"--model", req.Model.ID,
					"--workspace", req.WorkDir,
					"--prompt-file", req.PromptPath,
				},
				stripKeys: []string{"TASK_BOARD_BOARD_DIR", "TASK_BOARD_DELIVERY_GOAL_ID", "TASK_BOARD_RUN_ID", "TASK_BOARD_TASK_ID"},
				inject:    []string{"TASK_BOARD_RUN_ID=RUN-parity-muse", "TASK_BOARD_TASK_ID=TASK-parity-muse"},
			}
			return sys, req
		},
	},
	{
		goldenID:  "muse/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(dirs probeDirs) (*probeSystem, agentic.LaunchRequest) {
			req := agentic.LaunchRequest{
				System:  probeID,
				Model:   agentic.Model{ID: "muse-spark"},
				WorkDir: dirs.temp[0],
			}
			sys := &probeSystem{
				caps: agentic.Capabilities{
					LaunchModes: []agentic.LaunchMode{agentic.LaunchModeDryRun},
				},
				binary: dirs.stub + "/muse",
				argv: []string{
					"exec", "--json", "--yolo",
					"--model", req.Model.ID,
					"--workspace", req.WorkDir,
					"--prompt-file", "<prompt-file>",
				},
			}
			return sys, req
		},
	},
	{
		goldenID:  "qwen/exec",
		tempSlots: 1,
		mode:      agentic.LaunchModeExec,
		build: func(dirs probeDirs) (*probeSystem, agentic.LaunchRequest) {
			req := agentic.LaunchRequest{
				System:  probeID,
				Model:   agentic.Model{ID: "qwen3.7-max", Effort: agentic.EffortSupportRequired},
				Effort:  "high",
				Prompt:  []byte("qwen exec-mode prompt"),
				WorkDir: dirs.temp[0],
			}
			sys := &probeSystem{
				caps: agentic.Capabilities{
					LaunchModes:     []agentic.LaunchMode{agentic.LaunchModeExec},
					EffortTransport: agentic.EffortTransportStdin,
				},
				binary: dirs.temp[0] + "/qwen",
				argv: []string{
					"--model", req.Model.ID,
					"--approval-mode", "yolo",
					"--input-format", "stream-json",
					"--output-format", "stream-json",
				},
				// Written as the source COMPOSES it, not copied off the
				// fixture's env_removed list: filterQwenRuntimeEnv is
				// filterCodexRuntimeEnv with CLAUDECODE added
				// (spawn.go:1096), filterCodexRuntimeEnv strips its eleven
				// named keys plus whatever the two *_AUTH_TOKEN_ENV pointers
				// name (spawn.go:1025), and withSpawnEnv replaces the four
				// run-context keys (spawn.go:1389). A probe that read its
				// answer back out of the golden would prove only that JSON
				// round-trips.
				stripKeys: []string{
					"CLAUDECODE",
					"CODEX_THREAD_ID", "CODEX_SESSION", "CODEX_CI",
					"CODEX_MANAGED_BY_NPM", "CODEX_MANAGED_BY_BUN", "CODEX_MANAGED_PACKAGE_ROOT",
					"TASK_BOARD_CODEX_APP_SERVER_URL", "TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV",
					"TASK_BOARD_SESSION_MANAGER_URL", "TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV",
					"TASK_BOARD_SESSION_ID",
					"PARITY_CODEX_APP_SERVER_TOKEN", "PARITY_SESSION_MANAGER_TOKEN",
					"TASK_BOARD_RUN_ID", "TASK_BOARD_TASK_ID",
					"TASK_BOARD_BOARD_DIR", "TASK_BOARD_DELIVERY_GOAL_ID",
				},
				inject: []string{"TASK_BOARD_RUN_ID=RUN-parity-qwen", "TASK_BOARD_TASK_ID=TASK-parity-qwen"},
				stdin: agentic.StdinPayload{Attached: true, Bytes: []byte(
					`{"request":{"effort":"high","subtype":"initialize"},"request_id":"RUN-parity-qwen-initialize","type":"control_request"}` + "\n" +
						`{"message":{"content":[{"text":"qwen exec-mode prompt","type":"text"}],"role":"user"},"parent_tool_use_id":null,"session_id":"RUN-parity-qwen","type":"user"}` + "\n")},
			}
			return sys, req
		},
	},
}

func recipeFor(t *testing.T, goldenID string) probeRecipe {
	t.Helper()
	for _, recipe := range parityProbeRecipes {
		if recipe.goldenID == goldenID {
			return recipe
		}
	}
	t.Fatalf("no probe recipe for %q", goldenID)
	return probeRecipe{}
}

func makeProbeDirs(t *testing.T, recipe probeRecipe) probeDirs {
	t.Helper()
	dirs := probeDirs{}
	for i := 0; i < recipe.tempSlots; i++ {
		dirs.temp = append(dirs.temp, t.TempDir())
	}
	if recipe.usesStub {
		dirs.stub = t.TempDir()
	}
	return dirs
}

// buildProbePlan drives the production entry point: register into a real
// registry, then agentic.BuildPlan. Nothing here constructs a Plan by hand — a
// helper that is unit-tested but called from nowhere promises nothing, and the
// thing the ports need proved is that a plan produced the way production
// produces one lands on the golden.
func buildProbePlan(t *testing.T, sys agentic.System, req agentic.LaunchRequest, mode agentic.LaunchMode, parentEnv []string) agentic.Plan {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register(parity probe): %v", err)
	}
	req.Env = parentEnv
	plan, err := agentic.BuildPlan(registry, req, mode)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

// TestProbePlansMatchTheirGoldens is the positive half: a plan built through
// agentic.BuildPlan, snapshotted, masked and compared, matches the surface the
// source harness captured. It proves the harness is REACHABLE and that a
// correct plan passes. It does not prove the harness bites; that is the next
// test's job, and one without the other is worthless.
func TestProbePlansMatchTheirGoldens(t *testing.T) {
	for _, recipe := range parityProbeRecipes {
		t.Run(recipe.goldenID, func(t *testing.T) {
			g := goldenByID(t, recipe.goldenID)
			dirs := makeProbeDirs(t, recipe)
			sys, req := recipe.build(dirs)
			plan := buildProbePlan(t, sys, req, recipe.mode, g.Capture.ParentEnv)
			if diffs := ComparePlan(g, plan, dirs.substitutions()); len(diffs) != 0 {
				for _, d := range diffs {
					t.Errorf("%s does not match its golden:\n  %s", recipe.goldenID, d)
				}
			}
		})
	}
}

// TestAWrongPlanFailsAgainstItsGolden is the evidence that makes the positive
// test above mean anything.
//
// Three deliberate defects, one per field family the source harness records —
// an argv flag changed, an environment key dropped, one stdin byte changed —
// each applied to a plan that is otherwise correct, and each must be reported
// IN THE FIELD IT WAS PLANTED IN. A mutant that fails for the wrong reason
// proves nothing about the field it was written for.
func TestAWrongPlanFailsAgainstItsGolden(t *testing.T) {
	// claude/prompt-mode is the mutation subject because it is the one golden
	// carrying all three at once: an argv grammar, an environment diff with
	// both a strip and an injection, and attached stdin bytes.
	const subject = "claude/prompt-mode"
	recipe := parityProbeRecipes[0]
	if recipe.goldenID != subject {
		t.Fatalf("the mutation subject moved: expected %s, got %s", subject, recipe.goldenID)
	}

	mutants := []struct {
		name      string
		defect    string
		wantField string
		apply     func(sys *probeSystem)
	}{
		{
			name:      "one argv flag changed",
			defect:    "--dangerously-skip-permissions spelled in the singular, the kind of typo a port introduces while retyping a grammar",
			wantField: "Args",
			apply: func(sys *probeSystem) {
				for i, arg := range sys.argv {
					if arg == "--dangerously-skip-permissions" {
						sys.argv[i] = "--dangerously-skip-permission"
					}
				}
			},
		},
		{
			name:      "one env key dropped",
			defect:    "TASK_BOARD_TASK_ID never injected, so the child cannot report against its own task",
			wantField: "EnvAdded",
			apply: func(sys *probeSystem) {
				kept := sys.inject[:0]
				for _, entry := range sys.inject {
					if !strings.HasPrefix(entry, "TASK_BOARD_TASK_ID=") {
						kept = append(kept, entry)
					}
				}
				sys.inject = kept
			},
		},
		{
			name:      "one stdin byte changed",
			defect:    "a single byte of the prompt differs; the argv is identical, which is exactly the divergence argv-string equality cannot see",
			wantField: "StdinData",
			apply: func(sys *probeSystem) {
				body := []byte(string(sys.stdin.Bytes))
				body[len(body)-1] = 'Y'
				sys.stdin = agentic.StdinPayload{Attached: true, Bytes: body}
			},
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			g := goldenByID(t, subject)
			dirs := makeProbeDirs(t, recipe)

			// The unmutated plan must match first. Without this, a mutant that
			// "failed" could be failing for a reason that has nothing to do
			// with the defect it was written to plant.
			clean, cleanReq := recipe.build(dirs)
			cleanPlan := buildProbePlan(t, clean, cleanReq, recipe.mode, g.Capture.ParentEnv)
			if diffs := ComparePlan(g, cleanPlan, dirs.substitutions()); len(diffs) != 0 {
				t.Fatalf("the unmutated plan already differs, so this mutant proves nothing: %v", diffs)
			}

			sys, req := recipe.build(dirs)
			mutant.apply(sys)
			plan := buildProbePlan(t, sys, req, recipe.mode, g.Capture.ParentEnv)
			diffs := ComparePlan(g, plan, dirs.substitutions())
			if len(diffs) == 0 {
				t.Fatalf("the harness admitted a plan with a real defect (%s). A golden that a wrong plan satisfies is a golden that proves nothing, and every port after this one would ship on it", mutant.defect)
			}
			named := false
			for _, d := range diffs {
				if d.Field == mutant.wantField {
					named = true
				}
			}
			if !named {
				t.Errorf("the defect was planted in %s but the harness reported %v; a failure that names the wrong surface sends the next reader to the wrong file", mutant.wantField, diffs)
			}
		})
	}
}

// wholeEnvWipeSystem is the port defect the qwen golden's SHAPE invites: a
// ChildEnv that discards the parent environment entirely and returns only its
// own injections.
//
// It is a plausible mistake rather than a contrived one. A port author reading
// qwen/exec's env_removed — which names every key filterQwenRuntimeEnv strips —
// can reasonably conclude "qwen keeps nothing" and write exactly this. Under a
// parent environment seeded only with keys that get stripped, the resulting diff
// is byte-identical to the correct one: same env_added, same env_removed, zero
// differences reported. The bystander keys in the capture script's PINNED_ENV
// are what make the two distinguishable, and this test is what proves they do.
//
// This is the UPPER bound the source guards with its own test
// (spawn_test.go:940, "filterQwenRuntimeEnv dropped unrelated environment").
// The lower bound — that the filter strips at least the named keys — is already
// covered by the ordinary comparison.
type wholeEnvWipeSystem struct {
	*probeSystem
}

func (w wholeEnvWipeSystem) ChildEnv(_ []string, _ agentic.LaunchRequest) ([]string, error) {
	return append([]string(nil), w.inject...), nil
}

// TestAWholeEnvironmentWipeFailsAgainstQwenExec is the permanent negative for
// the preservation bound.
//
// qwen/exec is the subject because it is the strictest filter the source has:
// it strips every seeded key the other systems' filters strip and CLAUDECODE on
// top, so it is the one fixture where "strips everything" and "strips exactly
// these" could otherwise look the same. If this test ever passes again, the
// bystander keys have been dropped from a recapture and every port of a
// filtering system can ship a whole-environment wipe unnoticed.
func TestAWholeEnvironmentWipeFailsAgainstQwenExec(t *testing.T) {
	const subject = "qwen/exec"
	recipe := recipeFor(t, subject)
	g := goldenByID(t, subject)
	dirs := makeProbeDirs(t, recipe)

	// The correct port must match first, or a "failing" wipe would prove
	// nothing about wiping.
	clean, cleanReq := recipe.build(dirs)
	cleanPlan := buildProbePlan(t, clean, cleanReq, recipe.mode, g.Capture.ParentEnv)
	if diffs := ComparePlan(g, cleanPlan, dirs.substitutions()); len(diffs) != 0 {
		t.Fatalf("the correctly-filtered plan already differs, so the wipe below proves nothing: %v", diffs)
	}

	// The golden has to leave something to preserve. Without this the test
	// below could only ever pass by accident, and its failure message would
	// send the reader hunting a comparator bug rather than a fixture that lost
	// its bystanders.
	removed := map[string]bool{}
	for _, entry := range g.Surface.EnvRemoved {
		removed[entry] = true
	}
	survivors := 0
	for _, entry := range g.Capture.ParentEnv {
		if !removed[entry] {
			survivors++
		}
	}
	if survivors == 0 {
		t.Fatalf("%s records %d parent entries and removes all %d of them, so no key proves the filter is SELECTIVE; reseed the bystander keys in .scripts/capture-parity-goldens.sh and recapture",
			subject, len(g.Capture.ParentEnv), len(g.Surface.EnvRemoved))
	}

	sys, req := recipe.build(dirs)
	plan := buildProbePlan(t, wholeEnvWipeSystem{probeSystem: sys}, req, recipe.mode, g.Capture.ParentEnv)
	diffs := ComparePlan(g, plan, dirs.substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a port that discards the WHOLE parent environment byte-matched %s. The golden proves only the filter's lower bound, and three port tasks would inherit that hole with no reason to look for it", subject)
	}
	named := false
	for _, d := range diffs {
		if d.Field == "EnvRemoved" {
			named = true
		}
	}
	if !named {
		t.Errorf("the wipe was planted in the environment but the harness reported %v; a failure that names the wrong surface sends the next reader to the wrong file", diffs)
	}

	// And the narrowing that shows WHICH part of the fixture is doing the
	// catching. Strip the bystander entries back out of the recorded parent
	// environment — the shape this fixture had before they were seeded — and
	// the identical wipe walks straight through. Without this, the assertion
	// above would be satisfied by a fixture that catches the wipe for some
	// unrelated reason, and the convention the capture script maintains would
	// have no evidence behind it.
	t.Run("and would not be caught without the bystanders", func(t *testing.T) {
		prefix := g
		prefix.Capture.ParentEnv = withoutBystanders(g.Capture.ParentEnv)
		if len(prefix.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("no bystander entry was removed, so this narrowing changed nothing; parityBystanderKeys and the fixture's parent_env disagree")
		}
		wiped := buildProbePlan(t, wholeEnvWipeSystem{probeSystem: sys}, req, recipe.mode, prefix.Capture.ParentEnv)
		if diffs := ComparePlan(prefix, wiped, dirs.substitutions()); len(diffs) != 0 {
			t.Fatalf("removing the bystanders did NOT restore the bypass (%v); something other than the bystander keys is catching the wipe, and the capture script's convention is not what this test is protecting", diffs)
		}
	})
}

// withoutBystanders drops the named bystander entries from a recorded parent
// environment, reproducing an earlier fixture shape. Passing none drops all of
// them.
func withoutBystanders(parent []string, only ...string) []string {
	drop := only
	if len(drop) == 0 {
		drop = parityBystanderKeys
	}
	out := make([]string, 0, len(parent))
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		bystander := false
		for _, name := range drop {
			if key == name {
				bystander = true
				break
			}
		}
		if !bystander {
			out = append(out, entry)
		}
	}
	return out
}

// prefixStripSystem is the second port defect the bystanders exist to catch: a
// ChildEnv that filters by PREFIX where the source filters by exact key.
//
// The source builds a blocked-KEY map (filterEnvKeys, spawn.go:998) and
// replaces whole keys (appendOrReplaceEnv), so "strip the CODEX_ family" is a
// reading a port author can arrive at honestly and implement one character
// wrong. A plain bystander cannot see it — the over-strip lands only on keys
// that look like the filtered families — which is why three of the four seeded
// bystanders are near-misses rather than one plain key repeated.
type prefixStripSystem struct {
	*probeSystem
	prefixes []string
}

func (p prefixStripSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	kept := make([]string, 0, len(parent))
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		over := false
		for _, prefix := range p.prefixes {
			if strings.HasPrefix(key, prefix) {
				over = true
				break
			}
		}
		if !over {
			kept = append(kept, entry)
		}
	}
	return p.probeSystem.ChildEnv(kept, req)
}

// TestAPrefixStripFailsAgainstQwenExec is the permanent negative for the
// near-miss bystanders.
//
// The port under test strips exactly the right families by prefix instead of by
// key, which over-strips three keys the source keeps. The narrowing subtest
// then removes those three from the recorded parent environment — leaving the
// plain PARITY_BYSTANDER, which such a port preserves — and shows the defect
// walking straight through. That is what makes the three near-misses evidence
// rather than decoration.
func TestAPrefixStripFailsAgainstQwenExec(t *testing.T) {
	const subject = "qwen/exec"
	recipe := recipeFor(t, subject)
	g := goldenByID(t, subject)
	dirs := makeProbeDirs(t, recipe)
	prefixes := []string{"CODEX_", "CLAUDECODE", "TASK_BOARD_"}

	sys, req := recipe.build(dirs)
	clean := buildProbePlan(t, sys, req, recipe.mode, g.Capture.ParentEnv)
	if diffs := ComparePlan(g, clean, dirs.substitutions()); len(diffs) != 0 {
		t.Fatalf("the correctly-filtered plan already differs, so the prefix strip below proves nothing: %v", diffs)
	}

	defective, req := recipe.build(dirs)
	plan := buildProbePlan(t, prefixStripSystem{probeSystem: defective, prefixes: prefixes}, req, recipe.mode, g.Capture.ParentEnv)
	diffs := ComparePlan(g, plan, dirs.substitutions())
	if len(diffs) == 0 {
		t.Fatalf("a port that strips %v by PREFIX byte-matched %s, but the source strips by exact key; the near-miss bystanders are not seeded", prefixes, subject)
	}
	named := false
	for _, d := range diffs {
		if d.Field == "EnvRemoved" {
			named = true
		}
	}
	if !named {
		t.Errorf("the over-strip was planted in the environment but the harness reported %v", diffs)
	}

	t.Run("and would not be caught by a plain bystander alone", func(t *testing.T) {
		plain := g
		plain.Capture.ParentEnv = withoutBystanders(g.Capture.ParentEnv,
			"CODEX_LIKE_BUT_NOT", "CLAUDECODE_LIKE_BUT_NOT", "TASK_BOARD_LIKE_BUT_NOT")
		if len(plain.Capture.ParentEnv) == len(g.Capture.ParentEnv) {
			t.Fatalf("no near-miss entry was removed, so this narrowing changed nothing")
		}
		over, req := recipe.build(dirs)
		plan := buildProbePlan(t, prefixStripSystem{probeSystem: over, prefixes: prefixes}, req, recipe.mode, plain.Capture.ParentEnv)
		if diffs := ComparePlan(plain, plan, dirs.substitutions()); len(diffs) != 0 {
			t.Fatalf("removing the near-misses did NOT restore the bypass (%v); something other than those keys is catching the prefix strip, and three of the four seeded bystanders are unaccounted for", diffs)
		}
	})
}

// TestAPlanBuiltWithoutTheGoldensParentEnvIsNotSilentlyAccepted holds the one
// way a port could get a green result from a measurement it never took.
//
// EnvAdded and EnvRemoved are a diff. A port that builds its plan from some
// other baseline is comparing a different measurement wearing the same schema,
// and the failure has to be loud: ComparePlan takes the parent environment from
// the GOLDEN for exactly this reason, so the only way to reach this state is to
// feed the request a different environment than the golden records.
func TestAPlanBuiltWithoutTheGoldensParentEnvIsNotSilentlyAccepted(t *testing.T) {
	recipe := parityProbeRecipes[0]
	g := goldenByID(t, recipe.goldenID)
	dirs := makeProbeDirs(t, recipe)
	sys, req := recipe.build(dirs)
	plan := buildProbePlan(t, sys, req, recipe.mode, []string{"UNRELATED=1"})
	if diffs := ComparePlan(g, plan, dirs.substitutions()); len(diffs) == 0 {
		t.Fatal("a plan built from an unrelated parent environment matched the golden; the environment half of the parity bar is not being measured")
	}
}
