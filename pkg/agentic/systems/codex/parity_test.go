package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// This file is the acceptance. Every codex golden the source's own harness
// captured is rebuilt here through the REAL entry point — a real
// agentic.Registry holding the real plugin, then agentic.BuildPlan — and
// compared field for field against the fixture.
//
// Nothing in it constructs a Plan by hand and nothing reads an expected value
// out of the golden it is comparing to. The layouts below are built on disk
// the way the source's capture built them, from the same descriptions in
// parity_capture_test.go, so a comparison that passes means this plugin
// reproduces the source's launch surface rather than that JSON round-trips.

// parityModel and the rest are the source's codexParityConfig, field for
// field (parity_capture_test.go). They are spelled here rather than derived
// from the fixture for the reason above.
const (
	parityModel   = "gpt-5.6-terra"
	parityEffort  = "high"
	parityTier    = "priority"
	parityProfile = "parity-profile"
	parityRunID   = "RUN-parity-codex"
	parityTaskID  = "TASK-parity-codex"
)

// parityDirs are this process's stand-ins for the machine-local directories
// the capture allocated, in the SAME order the capture allocated them: the
// source's captureBuildCommandSurface takes a bin directory and then a work
// directory, and a case that needs a third allocates it inside its own build
// function. The golden's capture.placeholders block is where that count is
// read off rather than guessed.
type parityDirs struct {
	temp []string
	stub string
}

func (d parityDirs) substitutions() parity.Substitutions {
	return parity.Substitutions{TempSlots: d.temp, StubBinDir: d.stub}
}

// tempSlot allocates one machine-local directory, canonicalized.
//
// The canonicalization is load-bearing on macOS and is not cosmetic: t.TempDir
// hands back a path under /var, which is a symlink to /private/var, and
// nativeBinaryFromShim resolves symlinks by design. Without this, the shim case
// would produce a /private/var path while the substitution literal said /var,
// the mask would rewrite nothing, and the failure would read as a port bug in
// binary resolution rather than as a test that measured its own temp directory
// wrong.
func tempSlot(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalizing a temp slot: %v", err)
	}
	return resolved
}

// writeStubExecutable is the source's writeFakeExecutable: a runnable file
// with the right name, so binary resolution lands on a layout this test built
// rather than on whatever the developer's machine has installed.
func writeStubExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the stub %s: %v", name, err)
	}
	return path
}

// writeManagedPackage lays out a fake @openai/codex-<platform> npm package
// under packageRoot/node_modules, exactly the layout managedBinaryPath's FIRST
// candidate expects — the source's writeManagedNPMCodexBinary.
func writeManagedPackage(t *testing.T, packageRoot string) string {
	t.Helper()
	platformPackage, targetTriple, ok := platformPackageForHost()
	if !ok {
		t.Skipf("no known codex platform package for %s/%s", hostOS(), hostArch())
	}
	dir := filepath.Join(packageRoot, "node_modules", platformPackage, "vendor", targetTriple, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the managed package directory: %v", err)
	}
	return writeStubExecutable(t, dir, executableName)
}

// writeNativeShim replaces binDir's codex entry with the npm shim layout
// nativeBinaryFromShim unwraps — the source's writeNativeShimSibling.
//
// The PATH entry is a SYMLINK to a codex.js under a bin/ directory, which is
// how npm actually installs the shim, with the native binary reachable through
// managedBinaryPath's second (non-node_modules) candidate. Writing a plain file
// named codex.js on PATH instead would exercise a layout npm never produces and
// would skip the symlink resolution entirely.
func writeNativeShim(t *testing.T, binDir string) string {
	t.Helper()
	_, targetTriple, ok := platformPackageForHost()
	if !ok {
		t.Skipf("no known codex target triple for %s/%s", hostOS(), hostArch())
	}
	packageRoot := filepath.Join(binDir, "codex-shim-package")
	shimBinDir := filepath.Join(packageRoot, "bin")
	if err := os.MkdirAll(shimBinDir, 0o755); err != nil {
		t.Fatalf("creating the shim bin directory: %v", err)
	}
	shimPath := filepath.Join(shimBinDir, shimFileName)
	if err := os.WriteFile(shimPath, []byte("#!/usr/bin/env node\n// npm shim placeholder\n"), 0o755); err != nil {
		t.Fatalf("writing the shim: %v", err)
	}
	nativeDir := filepath.Join(packageRoot, "vendor", targetTriple, "bin")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("creating the native binary directory: %v", err)
	}
	nativePath := writeStubExecutable(t, nativeDir, executableName)
	onPath := filepath.Join(binDir, executableName)
	_ = os.Remove(onPath)
	if err := os.Symlink(shimPath, onPath); err != nil {
		t.Fatalf("linking the PATH entry at the shim: %v", err)
	}
	return nativePath
}

func writePromptFile(t *testing.T, workDir, body string) string {
	t.Helper()
	path := filepath.Join(workDir, "prompt.md")
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

// withManagedPackageRoot repoints CODEX_MANAGED_PACKAGE_ROOT at a real layout.
//
// The source's capture did the same thing by a different mechanism and the
// difference is worth stating precisely, because it looks at first like the
// test rewriting its own fixture. The capture recorded its baseline
// environment BEFORE the managed-npm subtest ran t.Setenv, so the golden's
// parent_env carries the pinned placeholder root while the code under capture
// saw the temp one. Repointing the launch environment reproduces exactly that
// state — and it is invisible in the comparison either way, because
// filterRuntimeEnv strips this key from the child, so the diff records the
// GOLDEN's value as removed regardless of what the launch environment held.
func withManagedPackageRoot(parent []string, root string) []string {
	out := make([]string, 0, len(parent))
	for _, entry := range parent {
		if strings.HasPrefix(entry, managedPackageRootEnv+"=") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, managedPackageRootEnv+"="+root)
}

// parityCase is one golden and the machine state that reproduces it.
type parityCase struct {
	goldenID string
	// tempSlots is how many directories the capture allocated for this case,
	// in its order. usesStub says whether the case also resolved through the
	// capture's PATH-seeded stub directory.
	tempSlots int
	usesStub  bool
	mode      agentic.LaunchMode
	// build lays the case's layout down on disk and returns the launch
	// request, minus the environment, which buildParityPlan supplies from the
	// golden.
	build func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string)
}

// parityCases covers every codex golden: all three binary-resolution paths
// under exec, plus the dry-run mirror.
var parityCases = []parityCase{
	{
		goldenID:  "codex/exec-default-path",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string) {
			binDir, workDir := dirs.temp[0], dirs.temp[1]
			writeStubExecutable(t, binDir, executableName)
			req := parityRequest(workDir)
			req.PromptPath = writePromptFile(t, workDir, "codex exec-mode prompt")
			return req, []string{binDir}
		},
	},
	{
		goldenID:  "codex/exec-managed-npm-path",
		tempSlots: 3,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string) {
			binDir, workDir, packageRoot := dirs.temp[0], dirs.temp[1], dirs.temp[2]
			// The bare PATH binary exists and must LOSE to the managed
			// package. A case where only the managed layout existed would pass
			// for a plugin that had no ordering at all.
			writeStubExecutable(t, binDir, executableName)
			writeManagedPackage(t, packageRoot)
			req := parityRequest(workDir)
			req.PromptPath = writePromptFile(t, workDir, "codex managed-npm prompt")
			return req, []string{binDir}
		},
	},
	{
		goldenID:  "codex/exec-native-shim",
		tempSlots: 2,
		mode:      agentic.LaunchModeExec,
		build: func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string) {
			binDir, workDir := dirs.temp[0], dirs.temp[1]
			writeNativeShim(t, binDir)
			req := parityRequest(workDir)
			req.PromptPath = writePromptFile(t, workDir, "codex native-shim prompt")
			return req, []string{binDir}
		},
	},
	{
		goldenID:  "codex/dry-run",
		tempSlots: 1,
		usesStub:  true,
		mode:      agentic.LaunchModeDryRun,
		build: func(t *testing.T, dirs parityDirs) (agentic.LaunchRequest, []string) {
			writeStubExecutable(t, dirs.stub, executableName)
			// No prompt file: the source's dry-run config carries none, which
			// is the whole point of the mode. A dry run that had to read a
			// file that does not exist yet would be a dry run doing work.
			return parityRequest(dirs.temp[0]), []string{dirs.stub}
		},
	},
}

// parityRequest is the source's codexParityConfig expressed as a
// LaunchRequest. Every field it sets is one the source's config set.
func parityRequest(workDir string) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System:      New().ID(),
		Model:       agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired},
		Effort:      parityEffort,
		ServiceTier: parityTier,
		Profile:     parityProfile,
		WorkDir:     workDir,
		Run: agentic.RunContext{
			RunID:    parityRunID,
			TaskID:   parityTaskID,
			BoardDir: filepath.Join(workDir, ".task-board"),
		},
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
	env := withPathEntry(g.Capture.ParentEnv, strings.Join(pathDirs, string(os.PathListSeparator)))
	if c.goldenID == "codex/exec-managed-npm-path" {
		env = withManagedPackageRoot(env, dirs.temp[2])
	}
	req.Env = env
	return g, dirs, req
}

// TestPlansMatchTheCodexGoldens is the acceptance: every captured
// (mode, resolution-path) combination, byte for byte.
func TestPlansMatchTheCodexGoldens(t *testing.T) {
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

// TestEveryCodexGoldenIsCovered fails if the fixture set grows a codex case
// this file does not build.
//
// Without it, a recapture that adds a fourth resolution path leaves this suite
// green while covering one combination fewer than it believes — which is the
// exact failure mode the parity package exists to prevent, one level up.
func TestEveryCodexGoldenIsCovered(t *testing.T) {
	all, err := parity.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	covered := map[string]bool{}
	for _, c := range parityCases {
		covered[c.goldenID] = true
	}
	for id, g := range all {
		if g.System != string(New().ID()) {
			continue
		}
		if !covered[id] {
			t.Errorf("golden %q is a codex case with no parity case in this file; a suite that covers one combination fewer than it believes is the failure this package exists to prevent", id)
		}
	}
	for id := range covered {
		if _, ok := all[id]; !ok {
			t.Errorf("this file builds %q, which is not a captured golden", id)
		}
	}
}

// TestAWrongPlanFailsAgainstTheCodexGolden is what makes the test above mean
// something.
//
// Three defects, one per field family the parity harness records — an argv
// flag, an injected environment variable, one byte of stdin — each planted on
// an otherwise-correct plan built through BuildPlan, and each required to be
// reported IN THE FIELD IT WAS PLANTED IN. A mutant that fails for the wrong
// reason proves nothing about the surface it was written for.
func TestAWrongPlanFailsAgainstTheCodexGolden(t *testing.T) {
	const subject = "codex/exec-default-path"
	c := parityCaseFor(t, subject)

	mutants := []struct {
		name      string
		defect    string
		wantField string
		system    func(*System) agentic.System
	}{
		{
			name:      "one argv flag misspelled",
			defect:    "--skip-git-repo-check spelled --skip-git-repo-checks, the kind of typo a port makes while retyping a grammar",
			wantField: "Args",
			system:    func(s *System) agentic.System { return renamedFlagSystem{System: s} },
		},
		{
			name:      "the service tier never reaches the child",
			defect:    "TASK_BOARD_CODEX_SERVICE_TIER is not injected, so the child runs on the account default while the launch reports the configured tier",
			wantField: "EnvAdded",
			system:    func(s *System) agentic.System { return noTierEnvSystem{System: s} },
		},
		{
			name:      "one stdin byte changed",
			defect:    "a single byte of the assignment prompt differs; the argv is identical, which is exactly the divergence argv-string equality cannot see",
			wantField: "StdinData",
			system:    func(s *System) agentic.System { return mutatedStdinSystem{System: s} },
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
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
		if arg == "--skip-git-repo-check" {
			args[i] = "--skip-git-repo-checks"
		}
	}
	return args, nil
}

type noTierEnvSystem struct{ agentic.System }

func (m noTierEnvSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := m.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	return agentic.SetEnvValue(env, ServiceTierEnv, ""), nil
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
