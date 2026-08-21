package parity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sourceHarnessCases is the combination list the SOURCE harness captures,
// transcribed from the capture calls in
// skill-project-management/tools/board-cli/internal/spawn/parity_capture_test.go.
//
// It is written out here rather than derived from the fixtures on purpose. A
// coverage check that reads the fixture directory and reports what it finds
// answers "did I load what is there", which is true of an empty directory too.
// This list is what makes a MISSING golden a failure: a capture run that
// silently dropped a combination — a t.Skipf inside one of the source's own
// cases, a generator that swallowed an id — otherwise leaves the suite green
// and that combination unproven.
var sourceHarnessCases = []string{
	"claude/prompt-mode",
	"claude/goal-mode",
	"codex/exec-default-path",
	"codex/exec-managed-npm-path",
	"codex/exec-native-shim",
	"codex/dry-run",
	"qwen/exec",
	"qwen/dry-run",
	"muse/exec",
	"muse/dry-run",
	"gemini/exec",
	"gemini/dry-run",
	"agy/exec",
	"agy/dry-run",
}

// parityBystanderKeys are the keys the capture seeds into the parent
// environment that NO source filter touches.
//
// They are the goldens' only positive PRESERVATION evidence. Seeding just the
// keys a filter strips proves its lower bound — it removes at least these — and
// says nothing about the upper bound. qwen is where that gap bites: without a
// surviving key its env_removed covers 100% of parent_env, so a port whose
// ChildEnv discards the whole parent environment and returns only its injections
// produces a byte-identical diff. TestAWholeEnvironmentWipeFailsAgainstQwenExec
// is the demonstration; this list is the convention that keeps it able to fail.
//
// The two _LIKE_BUT_NOT keys are near-misses. The source strips by exact key
// (filterEnvKeys, spawn.go:998) and replaces by exact key (appendOrReplaceEnv),
// never by prefix, so a port reaching for strings.HasPrefix("CODEX_") is a
// plausible defect that only a near-miss catches.
//
// Must stay in sync with PINNED_ENV in .scripts/capture-parity-goldens.sh.
var parityBystanderKeys = []string{
	"PARITY_BYSTANDER",
	"CODEX_LIKE_BUT_NOT",
	"CLAUDECODE_LIKE_BUT_NOT",
	"TASK_BOARD_LIKE_BUT_NOT",
}

func loadGoldens(t *testing.T) map[string]Golden {
	t.Helper()
	all, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	return all
}

func goldenByID(t *testing.T, id string) Golden {
	t.Helper()
	g, err := Load(id)
	if err != nil {
		t.Fatalf("Load(%q): %v", id, err)
	}
	return g
}

func TestEverySourceCombinationHasAGolden(t *testing.T) {
	all := loadGoldens(t)
	for _, id := range sourceHarnessCases {
		if _, ok := all[id]; !ok {
			t.Errorf("no golden for %q, which the source harness captures; a port of it would have nothing to prove itself against. present=%v", id, sortedIDs(all))
		}
	}
	// Set equality, not only membership: a fixture nobody transcribed above is
	// either a case the source grew — which the ports need to know about — or a
	// fixture generated inside this repository, which is the one thing a golden
	// may never be.
	expected := map[string]bool{}
	for _, id := range sourceHarnessCases {
		expected[id] = true
	}
	for id := range all {
		if !expected[id] {
			t.Errorf("golden %q corresponds to no capture call in the source harness; either the source grew a case and this list is stale, or something in this repository generated a fixture", id)
		}
	}
}

func TestEveryGoldenRecordsItsProvenance(t *testing.T) {
	for id, g := range loadGoldens(t) {
		if strings.TrimSpace(g.Capture.SourceCommit) == "" {
			t.Errorf("%s: no source commit; a golden whose provenance is unknown cannot settle a dispute about what the source did", id)
		}
		if len(g.Capture.SourceCommit) != 40 {
			t.Errorf("%s: source commit %q is not a full 40-character sha; an abbreviation can turn ambiguous as the source's history grows", id, g.Capture.SourceCommit)
		}
		if !strings.Contains(g.Capture.SourceHarness, "parity_capture_test.go") {
			t.Errorf("%s: source harness %q does not name the source's capture test; the fixture cannot be re-derived from itself", id, g.Capture.SourceHarness)
		}
		if g.Capture.CaptureEnvVar == "" {
			t.Errorf("%s: no capture env var recorded; the harness that produced this is skipped without one, so the fixture is not reproducible from the file alone", id)
		}
		if len(g.Capture.ParentEnv) == 0 {
			t.Errorf("%s: no parent environment recorded; env_added and env_removed are a diff, and a diff with no recorded baseline is a number", id)
		}
		if len(g.Capture.ParentEnvOmitted) == 0 {
			t.Errorf("%s: no parent-environment omissions recorded; the capture process needed variables that are not in parent_env, and an unstated omission is indistinguishable from an absence", id)
		}
		if !equalStrings(g.Capture.MaskRules, maskRuleNames()) {
			t.Errorf("%s: recorded mask rules %v are not this package's rules %v; the fixture was masked by something other than the code that will compare it", id, g.Capture.MaskRules, maskRuleNames())
		}
	}
}

// TestEveryGoldenEnvEntryIsAccountedFor holds the parent environment a golden
// records to the diff it is supposed to explain.
//
// Every env_removed entry must appear in parent_env: removal means "the parent
// had it, the child does not", so an entry that was never in the recorded
// parent means the diff was taken against a baseline the fixture does not
// carry — the case where a port reproduces the plan correctly and fails anyway,
// with nothing in the failure saying why.
func TestEveryGoldenEnvEntryIsAccountedFor(t *testing.T) {
	for id, g := range loadGoldens(t) {
		parent := map[string]bool{}
		for _, entry := range g.Capture.ParentEnv {
			parent[entry] = true
		}
		for _, entry := range g.Surface.EnvRemoved {
			if !parent[entry] {
				t.Errorf("%s: env_removed carries %q, which is not in the recorded parent_env; this fixture's diff was taken against a baseline it does not record", id, entry)
			}
		}
		for _, entry := range g.Surface.EnvAdded {
			if parent[entry] {
				t.Errorf("%s: env_added carries %q, which the recorded parent already had; an entry present on both sides is not an addition", id, entry)
			}
		}
	}
}

// TestEveryGoldenSeedsTheBystanderKeys is the pin that keeps a recapture from
// silently reverting the preservation bound.
//
// A regenerated golden set whose PINNED_ENV lost the bystanders would look
// perfectly healthy: every fixture still parses, every strip is still named,
// every port still passes. What it would lose is the only evidence that any
// filter is SELECTIVE, and TestAWholeEnvironmentWipeFailsAgainstQwenExec would
// go back to passing a whole-environment wipe. This is the test that fails
// first, and it names the script line to fix.
//
// It checks three separate facts, because two of them can hold while the third
// does not: the key is seeded, no filter removed it, and no filter re-added it
// under a changed value.
func TestEveryGoldenSeedsTheBystanderKeys(t *testing.T) {
	for id, g := range loadGoldens(t) {
		parentKeys := map[string]bool{}
		for _, entry := range g.Capture.ParentEnv {
			key, _, _ := strings.Cut(entry, "=")
			parentKeys[key] = true
		}
		for _, bystander := range parityBystanderKeys {
			if !parentKeys[bystander] {
				t.Errorf("%s: parent_env does not seed the bystander key %q; without a key no filter strips, this fixture proves only that its system removes AT LEAST the keys listed in env_removed. Reseed it in PINNED_ENV in .scripts/capture-parity-goldens.sh and recapture", id, bystander)
			}
		}
		for _, entry := range g.Surface.EnvRemoved {
			key, _, _ := strings.Cut(entry, "=")
			for _, bystander := range parityBystanderKeys {
				if key == bystander {
					t.Errorf("%s: env_removed carries the bystander key %q, so the source now strips a key chosen because nothing strips it. Either the source's filters grew, or the bystander was badly chosen; pick another key rather than deleting the convention", id, bystander)
				}
			}
		}
		for _, entry := range g.Surface.EnvAdded {
			key, _, _ := strings.Cut(entry, "=")
			for _, bystander := range parityBystanderKeys {
				if key == bystander {
					t.Errorf("%s: env_added carries the bystander key %q, so the source now writes a key chosen because nothing touches it", id, bystander)
				}
			}
		}
	}
}

// TestQwenExecLeavesSomethingToPreserve is the same bound stated as the
// property that actually blocked review, on the one fixture where it was
// violated.
//
// TestEveryGoldenSeedsTheBystanderKeys checks the convention; this checks the
// consequence, and it would still bite if the convention were kept but every
// bystander happened to be stripped by a filter that grew. qwen/exec is the
// subject because it is the strictest filter the source has — the only exec
// fixture that ever reached zero survivors.
func TestQwenExecLeavesSomethingToPreserve(t *testing.T) {
	g := goldenByID(t, "qwen/exec")
	removed := map[string]bool{}
	for _, entry := range g.Surface.EnvRemoved {
		removed[entry] = true
	}
	var survivors []string
	for _, entry := range g.Capture.ParentEnv {
		if !removed[entry] {
			survivors = append(survivors, entry)
		}
	}
	if len(survivors) == 0 {
		t.Fatalf("qwen/exec removes all %d entries of its recorded parent_env, so a port whose ChildEnv discards the WHOLE parent environment and returns only its injections byte-matches it; the golden proves the filter's lower bound and is silent on its upper bound", len(g.Capture.ParentEnv))
	}
	t.Logf("qwen/exec preserves %d of %d parent entries: %v", len(survivors), len(g.Capture.ParentEnv), survivors)
}

// TestGoldenSurfacesCarryNoMachineLocalPaths is the leak check on masking.
//
// It scans the SURFACE only. The provenance block legitimately names the source
// harness file and its test function, and a check that swept the whole file
// would either fail on that or be weakened until it passed — which is how a
// leak check stops checking.
// The forbidden literals are machine-INDEPENDENT on purpose. An earlier
// revision derived one of them from os.UserHomeDir() on the running machine,
// which checks a string that could never have leaked unless the suite happens
// to run on the capture operator's account — a check that weakens itself the
// moment it travels. "/Users/" and "/home/" are the home-directory ROOTS on the
// platforms this repository builds for, so they bite wherever the suite runs,
// and the pinned parent environment uses /parity/pinned/... precisely so no
// legitimate value collides with them.
func TestGoldenSurfacesCarryNoMachineLocalPaths(t *testing.T) {
	forbidden := []string{"/Users/", "/home/", "/var/folders/", "TestCaptureLaunchSurface", ".temp/parity-capture"}
	for id, g := range loadGoldens(t) {
		for _, field := range snapshotStrings(g.Surface) {
			for _, literal := range forbidden {
				if literal != "" && strings.Contains(field, literal) {
					t.Errorf("%s: surface carries machine-local literal %q inside %q; this fixture reproduces on one machine only", id, literal, field)
				}
			}
		}
	}
}

// TestGoldensAreAlreadyFullyMasked proves the fixtures sit in the masked
// vocabulary rather than merely near it: masking an already-masked surface with
// no substitutions must change nothing, and a golden must compare equal to
// itself.
//
// The second half is not a tautology worth skipping. Compare reporting
// differences between two identical values would make every port failure
// unreadable; Compare reporting NOTHING between two different values is what
// TestCompareReportsEveryField covers.
func TestGoldensAreAlreadyFullyMasked(t *testing.T) {
	for id, g := range loadGoldens(t) {
		if diffs := Compare(g.Surface, Mask(g.Surface, Substitutions{})); len(diffs) != 0 {
			t.Errorf("%s: re-masking changed the surface: %v", id, diffs)
		}
		if diffs := Compare(g.Surface, g.Surface); len(diffs) != 0 {
			t.Errorf("%s: a golden does not compare equal to itself: %v", id, diffs)
		}
	}
}

// TestGoldenLaunchModeAgreesWithTheStdinMarker re-checks on load what
// launchModeForCase checked at generation. The two happen at different times —
// generation runs once on an operator's machine, this runs on every suite — and
// a fixture hand-edited into disagreement would otherwise be a mislabelled
// contract that nothing notices.
func TestGoldenLaunchModeAgreesWithTheStdinMarker(t *testing.T) {
	for id, g := range loadGoldens(t) {
		dryRun := g.LaunchMode == "dry-run"
		marker := g.Surface.StdinKind == StdinDryRun
		if dryRun != marker {
			t.Errorf("%s: launch_mode %q and stdin_kind %q disagree about which source capture path produced this", id, g.LaunchMode, g.Surface.StdinKind)
		}
		if dryRun && (len(g.Surface.EnvAdded) > 0 || len(g.Surface.EnvRemoved) > 0) {
			t.Errorf("%s: a dry-run golden carries an environment diff, but the source's dry-run capture path builds no command and observes no environment; this fixture claims a measurement that was never taken", id)
		}
	}
}

// TestLoadRefusesAnUnknownID is the refusal half of Load. Returning a zero
// Golden for a typo'd id would give a port a surface of empty strings to
// compare against, and empty compares equal to an empty plan.
func TestLoadRefusesAnUnknownID(t *testing.T) {
	if _, err := Load("claude/prompt_mode"); err == nil {
		t.Fatal("Load accepted an id no golden carries; a zero Golden would let a port prove itself against an empty surface")
	}
}

// TestLoadDirRefusesAMalformedFixture is the negative side of the loader.
//
// Its contract is that a fixture which does not parse, declares the wrong
// schema version, carries a field the reader does not know, or sits under the
// wrong name is an ERROR rather than a skipped entry — a parity suite silently
// covering one combination fewer than it believes is the exact failure this
// package exists to prevent, and a failed read is not a legitimate absence.
// Loading the real fixtures proves the reading path; only these prove the
// refusals.
func TestLoadDirRefusesAMalformedFixture(t *testing.T) {
	valid := goldenByID(t, "muse/exec")

	structured := []struct {
		name    string
		mutate  func(g *Golden) string
		wantErr string
	}{
		{
			name:    "wrong schema version",
			mutate:  func(g *Golden) string { g.SchemaVersion = GoldenSchemaVersion + 1; return g.FileName() },
			wantErr: "schema version",
		},
		{
			name:    "id disagrees with file name",
			mutate:  func(g *Golden) string { return "muse_something-else.json" },
			wantErr: "belongs in",
		},
	}
	for _, tc := range structured {
		t.Run(tc.name, func(t *testing.T) {
			g := valid
			name := tc.mutate(&g)
			dir := t.TempDir()
			data, err := json.MarshalIndent(g, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := loadDir(os.DirFS(dir), "."); err == nil {
				t.Fatalf("the loader accepted a fixture with %s", tc.name)
			} else if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("the refusal does not say what was wrong: got %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}

	raw := []struct {
		body string
		why  string
	}{
		{
			body: "{not json",
			why:  "a file that cannot be read is not a combination that is absent",
		},
		{
			body: `{"schema_version":1,"id":"muse/exec","system":"muse","case":"exec","launch_mode":"exec","capture":{},"surface":{"binary":"x","args":[],"stdin_kind":"none"},"tolerance":"0.5"}`,
			why:  "a field the reader does not know is a contract term silently dropped",
		},
	}
	for _, tc := range raw {
		t.Run(tc.why, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "muse_exec.json"), []byte(tc.body), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := loadDir(os.DirFS(dir), "."); err == nil {
				t.Fatalf("the loader accepted the fixture; %s", tc.why)
			}
		})
	}

	t.Run("no fixtures at all", func(t *testing.T) {
		if _, err := loadDir(os.DirFS(t.TempDir()), "."); err == nil {
			t.Fatal("the loader reported success over a directory holding no fixtures; a suite proving nothing must not look like a suite that passed")
		}
	})
}

// TestFixturesReadmeNamesWhatTheGoldensDoNotProve holds the boundary document
// to the fixtures it describes.
//
// The port tasks read that README to learn where the goldens stop. A README
// that drifts from the fixtures — a stale source commit, an uncaptured surface
// nobody wrote down — is prose a reader trusts with nothing holding it to the
// code, which is the same defect as an unpinned mask one level up.
func TestFixturesReadmeNamesWhatTheGoldensDoNotProve(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "goldens", "README.md"))
	if err != nil {
		t.Fatalf("reading the fixtures README: %v", err)
	}
	text := string(body)

	// The commit the README quotes must be the commit the fixtures carry.
	for id, g := range loadGoldens(t) {
		if !strings.Contains(text, g.Capture.SourceCommit) {
			t.Fatalf("the README does not name the commit %s was captured at (%s); a boundary document quoting a different capture than the fixtures is worse than none", id, g.Capture.SourceCommit)
		}
	}

	// The two surfaces the source could not capture, with the reason each is
	// missing rather than only the fact.
	mustName := []string{
		"managed-session-args",
		"TestManagedCodexSpawnArgsMatchesTheHistoricalConstruction",
		"interactive-manager-client argv passthrough",
		"LaunchModeManagedSession",
		"sanitizeCodexPath",
		"testdata",
	}
	for _, needle := range mustName {
		if !strings.Contains(text, needle) {
			t.Errorf("the fixtures README does not mention %q; a port would read the absence of a golden for it as permission rather than as an uncovered surface", needle)
		}
	}

	// The bystander convention has to be written down where the next capture
	// operator reads it. A recapture that drops these keys reverts the only
	// preservation bound the goldens carry, and the tests that catch it name a
	// script line rather than explaining why the line is there.
	for _, bystander := range parityBystanderKeys {
		if !strings.Contains(text, bystander) {
			t.Errorf("the fixtures README does not name the bystander key %q; the next capture would drop it as noise and take the filters' upper bound with it", bystander)
		}
	}

	// And every combination that IS captured must appear, so the coverage table
	// cannot quietly fall behind the directory.
	for _, id := range sourceHarnessCases {
		system, kase, _ := strings.Cut(id, "/")
		if !strings.Contains(text, "`"+kase+"`") || !strings.Contains(text, system) {
			t.Errorf("the README's coverage table does not list %q", id)
		}
	}
}
