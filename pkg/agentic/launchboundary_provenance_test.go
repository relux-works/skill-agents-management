package agentic_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lbProvenanceEntry is one GENERATED property-test case over the
// provenance-wiring-claims invariant: source and documentation state actual
// ownership and current integration truthfully (round-2 finding, repeat of
// round-1 F3). Each entry binds a claim family to a claim site: every
// forbidden phrase must be absent and every required disclosure present.
// A point fix that corrects one sentence while leaving another false claim
// (or dropping a disclosure) in any site fails here.
type lbProvenanceEntry struct {
	family    string
	file      string // relative to the module root; README.md is scoped to the launch-boundary subsection
	forbidden []string
	required  []string
}

func lbProvenanceCatalog() []lbProvenanceEntry {
	wiringForbidden := []string{
		"daemon and the launcher call this one",
		"daemon and the launcher call this API",
		"launcher call this one",
		"so the daemon re-checks",
	}
	wiringRequired := []string{"do not call", "not wired yet", "follow", "future"}
	inlineForbidden := []string{
		"grammar curator run applies",
		"spawn-plane build validates the",
		"inline --mcp-config grammar at the spawn-plane build",
		"validators curator run",
		"curator run applies earlier",
		"Curator run admits MCP configuration",
	}
	inlineRequired := []string{
		"internal/mcpjson", "v0.5.37", "plan.go", "fbcdbaf0",
		"explicitly requested", "module's own", "unset",
	}
	return []lbProvenanceEntry{
		{family: "wiring", file: "pkg/agentic/launchboundary.go", forbidden: wiringForbidden, required: wiringRequired},
		{family: "wiring", file: "pkg/agentic/launchboundary_admission.go", forbidden: wiringForbidden, required: wiringRequired},
		{family: "wiring", file: "README.md", forbidden: wiringForbidden, required: wiringRequired},
		{family: "wiring", file: "changelog.d/TASK-261004-26qvc0.md", forbidden: wiringForbidden, required: wiringRequired},
		{family: "inline-attribution", file: "pkg/agentic/launchboundary.go", forbidden: inlineForbidden, required: inlineRequired},
		{family: "inline-attribution", file: "pkg/agentic/launchboundary_admission.go", forbidden: inlineForbidden, required: inlineRequired},
		{family: "inline-attribution", file: "README.md", forbidden: inlineForbidden, required: inlineRequired},
		{family: "inline-attribution", file: "changelog.d/TASK-261004-26qvc0.md", forbidden: inlineForbidden, required: []string{"module's own", "internal/mcpjson", "v0.5.37"}},
	}
}

// lbProvenanceFragment is the unreleased changelog claim site. Release
// consumes it into CHANGELOG.md (.scripts/changelog-release.sh), so the site
// reader resolves it to the fragment or, once consumed, to the released
// CHANGELOG.md section carrying the same text (BUG-261005-28qxur).
const lbProvenanceFragment = "changelog.d/TASK-261004-26qvc0.md"

// lbProvenanceReleasedAnchor locates the consumed fragment's released
// CHANGELOG.md section. It is the fragment's first-line distinctive phrase,
// preserved verbatim by changelog-release.sh.
const lbProvenanceReleasedAnchor = "native launch-boundary adapter"

// lbProvenanceModuleRoot resolves the module root from the test working
// directory so the mutant harness (which runs this test in an isolated
// module copy) observes mutated bytes.
func lbProvenanceModuleRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("provenance invariant violated: get test package directory: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}

// lbProvenanceSiteBody reads one claim site and reports the site the bytes
// came from. README.md is scoped to the launch-boundary subsection so
// unrelated sections cannot satisfy or break this leaf's disclosures. The
// changelog site resolves to the fragment or its released section.
func lbProvenanceSiteBody(t *testing.T, moduleRoot, file string) (body, source string) {
	t.Helper()
	if file == lbProvenanceFragment {
		return lbProvenanceChangelogBody(t, moduleRoot)
	}
	raw, err := os.ReadFile(filepath.Join(moduleRoot, file))
	if err != nil {
		t.Fatalf("provenance invariant violated: read %s: %v", file, err)
	}
	if file != "README.md" {
		return string(raw), file
	}
	const anchor = "#### Native launch boundary"
	start := strings.Index(string(raw), anchor)
	if start < 0 {
		t.Fatalf("provenance invariant violated: launch-boundary README subsection missing")
	}
	rest := string(raw)[start:]
	if end := strings.Index(rest[len(anchor):], "\n### "); end >= 0 {
		return rest[:len(anchor)+end], file
	}
	return rest, file
}

// lbProvenanceChangelogBody resolves the changelog claim site to whichever
// exists: the unreleased fragment on a candidate tree, or the released
// CHANGELOG.md section once changelog-release.sh has consumed the fragment.
// A read failure that is not absence fails closed and never falls through
// to the other site.
func lbProvenanceChangelogBody(t *testing.T, moduleRoot string) (body, source string) {
	t.Helper()
	fragment, err := os.ReadFile(filepath.Join(moduleRoot, lbProvenanceFragment))
	if err == nil {
		return string(fragment), lbProvenanceFragment
	}
	if !os.IsNotExist(err) {
		t.Fatalf("provenance invariant violated: read %s: %v", lbProvenanceFragment, err)
	}
	return lbProvenanceReleasedSection(t, moduleRoot)
}

// lbProvenanceReleasedSection returns the CHANGELOG.md "## " section carrying
// the consumed launch-boundary fragment text, located by
// lbProvenanceReleasedAnchor. Absence fails closed: a released tree whose
// changelog lost the wording is a provenance violation, not a vacuous pass.
func lbProvenanceReleasedSection(t *testing.T, moduleRoot string) (body, source string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("provenance invariant violated: fragment %s absent and CHANGELOG.md unreadable: %v", lbProvenanceFragment, err)
	}
	lines := strings.Split(string(raw), "\n")
	header := ""
	var current []string
	consider := func() (string, string, bool) {
		if header == "" {
			return "", "", false
		}
		text := header + "\n" + strings.Join(current, "\n")
		if strings.Contains(text, lbProvenanceReleasedAnchor) {
			return text, "CHANGELOG.md (" + header + ")", true
		}
		return "", "", false
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if body, source, found := consider(); found {
				return body, source
			}
			header = line
			current = current[:0]
			continue
		}
		if header != "" {
			current = append(current, line)
		}
	}
	if body, source, found := consider(); found {
		return body, source
	}
	t.Fatalf("provenance invariant violated: fragment %s absent and no CHANGELOG.md section contains %q", lbProvenanceFragment, lbProvenanceReleasedAnchor)
	return "", ""
}

// lbCheckProvenance runs the GENERATED property test over the
// provenance-wiring-claims class against moduleRoot, parametrised over claim
// families × claim sites. The invariant test runs it against the checkout;
// the release regression runs it against a temporary released tree.
func lbCheckProvenance(t *testing.T, moduleRoot string) {
	t.Helper()
	for _, entry := range lbProvenanceCatalog() {
		t.Run(entry.family+"/"+entry.file, func(t *testing.T) {
			body, source := lbProvenanceSiteBody(t, moduleRoot, entry.file)
			for _, phrase := range entry.forbidden {
				t.Run("forbidden", func(t *testing.T) {
					if strings.Contains(body, phrase) {
						t.Fatalf("provenance invariant violated: forbidden wiring claim %q present in %s", phrase, source)
					}
				})
			}
			for _, disclosure := range entry.required {
				t.Run("required", func(t *testing.T) {
					if !strings.Contains(body, disclosure) {
						t.Fatalf("provenance invariant violated: required disclosure %q missing from %s", disclosure, source)
					}
				})
			}
			if entry.family == "wiring" {
				t.Run("no-unnegated-call", func(t *testing.T) {
					for _, token := range []string{"call this", "calls this"} {
						for offset := 0; ; {
							at := strings.Index(body[offset:], token)
							if at < 0 {
								break
							}
							absolute := offset + at
							contextStart := absolute - 20
							if contextStart < 0 {
								contextStart = 0
							}
							if context := body[contextStart:absolute]; !strings.Contains(context, "not ") {
								t.Fatalf("provenance invariant violated: unnegated wiring claim %q in %s", token, source)
							}
							offset = absolute + len(token)
						}
					}
				})
			}
		})
	}
}

// TestLaunchBoundaryProvenanceInvariant is the GENERATED property test over
// the provenance-wiring-claims class, parametrised over claim families ×
// claim sites. It is the named regression test for the round-2 repeat of F3
// and the named kill test for the launch-boundary-provenance-wiring-restored
// mutant: any reintroduced present-tense wiring claim, any reattributed
// inline grammar, or any dropped ownership disclosure fails by name.
func TestLaunchBoundaryProvenanceInvariant(t *testing.T) {
	lbCheckProvenance(t, lbProvenanceModuleRoot(t))
}

// lbCopyFile stages one working-tree file into the release rehearsal,
// creating parent directories. The rehearsal reads working-tree bytes so the
// invariant under test observes the candidate, not the last commit.
func lbCopyFile(t *testing.T, src, dst string) {
	t.Helper()
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("provenance invariant violated: stage %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("provenance invariant violated: stage %s: %v", dst, err)
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		t.Fatalf("provenance invariant violated: stage %s: %v", dst, err)
	}
}

// lbRunCommand runs argv in dir with a bounded wait and returns its combined
// output and exit code. A launch failure is fatal; a non-zero exit is an
// ordinary result the caller asserts on. Every inherited GIT_* variable is
// dropped: the launchcontext-mutants census exports GIT_DIR, GIT_WORK_TREE
// and GIT_INDEX_FILE into inner test runs, and an unscrubbed rehearsal
// would commit into the real repository instead of the staged copy
// (BUG-261005-28qxur incident, rogue commit 4d3b66c). Git then reads no
// user, global or system configuration, so a foreign commit hook or signing
// default cannot change the rehearsal either. Every subprocess also runs
// with a throwaway HOME under t.TempDir() (BUG-261005-28qxur F2): the
// caller HOME is dropped from the environment and replaced, so no git
// subprocess can observe the caller's global config, hooks, or identity.
func lbRunCommand(t *testing.T, dir string, timeout time.Duration, argv ...string) (string, int) {
	t.Helper()
	callerHome := os.Getenv("HOME")
	rehearsalParent := t.TempDir()
	rehearsalHome := filepath.Join(rehearsalParent, "home")
	if err := os.MkdirAll(rehearsalHome, 0o700); err != nil {
		t.Fatalf("provenance invariant violated: create rehearsal HOME in %s: %v", rehearsalParent, err)
	}
	if callerHome != "" && rehearsalHome == callerHome {
		t.Fatalf("provenance invariant violated: rehearsal HOME %q equals caller HOME; refusing to run", rehearsalHome)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_") {
			continue
		}
		if entry == "HOME" || strings.HasPrefix(entry, "HOME=") {
			continue
		}
		if strings.HasPrefix(entry, "XDG_CONFIG_HOME=") {
			continue
		}
		env = append(env, entry)
	}
	for _, entry := range env {
		if callerHome != "" && entry == "HOME="+callerHome {
			t.Fatalf("provenance invariant violated: rehearsal environment leaks caller HOME %q", callerHome)
		}
	}
	cmd.Env = append(env,
		"HOME="+rehearsalHome,
		"XDG_CONFIG_HOME="+rehearsalHome,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("provenance invariant violated: release rehearsal %v timed out in %s", argv, dir)
	}
	if err == nil {
		return string(output), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode()
	}
	t.Fatalf("provenance invariant violated: run %v in %s: %v", argv, dir, err)
	return "", -1
}

// lbAssertRehearsalRepo proves the rehearsal git commands operate inside the
// staged copy: the top level git resolves in staged must be staged itself.
// A leaked GIT_DIR/GIT_WORK_TREE would otherwise redirect the staging commit
// (and the release script's git reads) at the real checkout, so a mismatch
// fails closed before anything is staged.
func lbAssertRehearsalRepo(t *testing.T, staged string) {
	t.Helper()
	output, exit := lbRunCommand(t, staged, time.Minute, "git", "rev-parse", "--show-toplevel")
	if exit != 0 {
		t.Fatalf("provenance invariant violated: release rehearsal is not a git checkout (rev-parse exit %d):\n%s", exit, output)
	}
	want, err := filepath.EvalSymlinks(staged)
	if err != nil {
		t.Fatalf("provenance invariant violated: resolve rehearsal dir %s: %v", staged, err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(output))
	if err != nil {
		t.Fatalf("provenance invariant violated: resolve rehearsal top level %q: %v", strings.TrimSpace(output), err)
	}
	if got != want {
		t.Fatalf("provenance invariant violated: rehearsal git top level is %s, want staged copy %s; refusing to touch a foreign checkout", got, want)
	}
}

// lbAssertRehearsalHomeIsolated proves no rehearsal subprocess observes the
// caller HOME (BUG-261005-28qxur F2): it runs printenv through the same
// lbRunCommand environment every rehearsal git and release subprocess uses
// and fails when the observed HOME equals the caller HOME or is empty. The
// rehearsal HOME is a throwaway under t.TempDir(), so an empty caller HOME
// still requires a non-empty throwaway.
func lbAssertRehearsalHomeIsolated(t *testing.T, dir string) {
	t.Helper()
	callerHome := os.Getenv("HOME")
	output, exit := lbRunCommand(t, dir, time.Minute, "printenv", "HOME")
	if exit != 0 {
		t.Fatalf("provenance invariant violated: rehearsal HOME probe failed (exit %d):\n%s", exit, output)
	}
	observed := strings.TrimSpace(output)
	if observed == "" {
		t.Fatalf("provenance invariant violated: rehearsal subprocess observes empty HOME; want throwaway under t.TempDir()")
	}
	if callerHome != "" && observed == callerHome {
		t.Fatalf("provenance invariant violated: rehearsal subprocess observes caller HOME %q; want throwaway", callerHome)
	}
}

// lbStageRehearsalFiles stages the invariant inputs the release rehearsal
// needs, reading working-tree bytes so the invariant observes the candidate.
// The provenance fragment is staged only when it exists: on a released tree
// changelog-release.sh has already consumed it into CHANGELOG.md, and an
// unconditional copy would fail the rehearsal on exactly the trees this bug
// exists to keep green (BUG-261005-28qxur F1). It reports whether the
// fragment was staged so the caller can choose the release mode. A stat
// failure that is not absence fails closed and never silently rehearses the
// released section.
func lbStageRehearsalFiles(t *testing.T, moduleRoot, staged string) (fragmentStaged bool) {
	t.Helper()
	for _, file := range []string{
		"CHANGELOG.md",
		"changelog.d/README.md",
		"README.md",
		"pkg/agentic/launchboundary.go",
		"pkg/agentic/launchboundary_admission.go",
		".scripts/changelog-release.sh",
	} {
		lbCopyFile(t, filepath.Join(moduleRoot, file), filepath.Join(staged, file))
	}
	fragmentSrc := filepath.Join(moduleRoot, lbProvenanceFragment)
	if _, err := os.Stat(fragmentSrc); err == nil {
		lbCopyFile(t, fragmentSrc, filepath.Join(staged, lbProvenanceFragment))
		return true
	} else if !os.IsNotExist(err) {
		t.Fatalf("provenance invariant violated: stat %s: %v", lbProvenanceFragment, err)
	}
	return false
}

// lbInitRehearsalRepo initialises a git checkout in the staged copy, proves
// the rehearsal commands operate inside it, and commits the staged bytes so
// changelog-release.sh observes a clean tree with committed fragments.
func lbInitRehearsalRepo(t *testing.T, staged, message string) {
	t.Helper()
	if output, exit := lbRunCommand(t, staged, time.Minute, "git", "init", "-q"); exit != 0 {
		t.Fatalf("provenance invariant violated: stage release rehearsal (git init) exit %d:\n%s", exit, output)
	}
	lbAssertRehearsalRepo(t, staged)
	lbCommitRehearsalState(t, staged, message)
}

// lbCommitRehearsalState commits the current staged worktree. The release
// script refuses a dirty tree, so the double-release rehearsal commits the
// first release before running the second.
func lbCommitRehearsalState(t *testing.T, staged, message string) {
	t.Helper()
	if output, exit := lbRunCommand(t, staged, time.Minute, "git", "add", "-A"); exit != 0 {
		t.Fatalf("provenance invariant violated: stage release rehearsal (git add) exit %d:\n%s", exit, output)
	}
	commit := []string{"git", "-c", "user.name=launch-boundary-rehearsal", "-c", "user.email=rehearsal@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", message}
	if output, exit := lbRunCommand(t, staged, time.Minute, commit...); exit != 0 {
		t.Fatalf("provenance invariant violated: stage release rehearsal (git commit) exit %d:\n%s", exit, output)
	}
}

// lbAssertRehearsalReleasedTree proves the staged copy is a released tree
// carrying the provenance wording: the fragment is absent and CHANGELOG.md
// holds the anchor section. It is the shared post-release gate for both the
// single and the double rehearsal.
func lbAssertRehearsalReleasedTree(t *testing.T, staged string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(staged, lbProvenanceFragment)); !os.IsNotExist(err) {
		if err == nil {
			t.Fatalf("provenance invariant violated: release rehearsal left %s in place; the invariant would read the fragment, not the released tree", lbProvenanceFragment)
		}
		t.Fatalf("provenance invariant violated: stat rehearsal %s: %v", lbProvenanceFragment, err)
	}
	released, err := os.ReadFile(filepath.Join(staged, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("provenance invariant violated: read rehearsal CHANGELOG.md: %v", err)
	}
	if !strings.Contains(string(released), lbProvenanceReleasedAnchor) {
		t.Fatalf("provenance invariant violated: rehearsal CHANGELOG.md carries no %q section; the release consumed nothing", lbProvenanceReleasedAnchor)
	}
}

// lbHeaderHasVersion reports whether one CHANGELOG.md "## " header line
// already carries version as a delimited token, mirroring the production
// duplicate_version gate in .scripts/changelog-release.sh (a version matches
// only when both neighbours are absent or outside [0-9.], so v9.9.9 never
// collides with a released v9.9.10). The rehearsal uses it to choose a
// version proven absent instead of a fixed one.
func lbHeaderHasVersion(header, version string) bool {
	for offset := 0; offset+len(version) <= len(header); {
		at := lbIndexAt(header, version, offset)
		if at < 0 {
			return false
		}
		beforeOK := at == 0 || !lbIsVersionChar(header[at-1])
		after := at + len(version)
		afterOK := after == len(header) || !lbIsVersionChar(header[after])
		if beforeOK && afterOK {
			return true
		}
		offset = at + 1
	}
	return false
}

// lbIndexAt is strings.Index on header[offset:] shifted back to header
// coordinates, or -1 when version is absent from the suffix.
func lbIndexAt(header, version string, offset int) int {
	idx := strings.Index(header[offset:], version)
	if idx < 0 {
		return -1
	}
	return offset + idx
}

// lbIsVersionChar reports whether b continues a version token (a digit or a
// dot), matching the production awk class [0-9.].
func lbIsVersionChar(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.'
}

// lbChangelogHasVersion reports whether the CHANGELOG.md at changelogPath
// already carries version in any "## " header. Only headers are searched, so
// an Unreleased bullet citing a future version never collides.
func lbChangelogHasVersion(t *testing.T, changelogPath, version string) bool {
	t.Helper()
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("provenance invariant violated: read rehearsal CHANGELOG.md for version selection: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") && lbHeaderHasVersion(line, version) {
			return true
		}
	}
	return false
}

// lbNextAbsentRehearsalVersion derives a rehearsal version proven absent from
// the staged CHANGELOG.md (BUG-261005-28qxur round-2 F1): fixed rehearsal
// versions collide with a released history that already contains them, and
// the production script correctly refuses the duplicate. It probes v9.9.N
// upward from N=9 and returns the first version no "## " header carries. The
// caller re-derives after each release commit so the second rehearsal
// observes the first release's new header. The production
// duplicate-version refusal is untouched; this only selects the fixture
// version.
func lbNextAbsentRehearsalVersion(t *testing.T, staged string) string {
	t.Helper()
	changelogPath := filepath.Join(staged, "CHANGELOG.md")
	for patch := 9; patch < 10009; patch++ {
		candidate := fmt.Sprintf("v9.9.%d", patch)
		if !lbChangelogHasVersion(t, changelogPath, candidate) {
			return candidate
		}
	}
	t.Fatalf("provenance invariant violated: no absent rehearsal version found in staged CHANGELOG.md")
	return ""
}

// lbInjectCollidingHistory prepends released sections for versions to the
// staged CHANGELOG.md, directly under the "# Changelog" title, so the
// rehearsal stages a history that already contains the previously fixed
// rehearsal versions. Each injected section carries one bullet and never the
// provenance anchor, so the anchor search still observes the real released
// section. It must run before lbInitRehearsalRepo so the colliding history
// is committed fixture history, not a dirty tree.
func lbInjectCollidingHistory(t *testing.T, staged string, versions ...string) {
	t.Helper()
	changelogPath := filepath.Join(staged, "CHANGELOG.md")
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("provenance invariant violated: read staged CHANGELOG.md for colliding history: %v", err)
	}
	title := "# Changelog\n"
	if !strings.HasPrefix(string(raw), title) {
		t.Fatalf("provenance invariant violated: staged CHANGELOG.md has no %q title; refusing to inject colliding history", "# Changelog")
	}
	var injected strings.Builder
	for _, version := range versions {
		injected.WriteString(fmt.Sprintf("## %s — 2026-01-01\n\n- colliding rehearsal history for %s (no provenance wording)\n\n", version, version))
	}
	updated := title + "\n" + injected.String() + strings.TrimPrefix(string(raw), title)
	if err := os.WriteFile(changelogPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("provenance invariant violated: inject colliding history into staged CHANGELOG.md: %v", err)
	}
	for _, version := range versions {
		if !lbChangelogHasVersion(t, changelogPath, version) {
			t.Fatalf("provenance invariant violated: staged CHANGELOG.md lacks injected colliding version %s; the collision regression would be vacuous", version)
		}
	}
}

// TestLaunchBoundaryProvenanceAfterChangelogRelease is the named regression
// for BUG-261005-28qxur: the invariant read the unreleased fragment only, so
// it was green on the candidate and red on any release commit once
// changelog-release.sh consumed the fragment. It rehearses the release in a
// temporary git copy through the production .scripts/changelog-release.sh
// entry point (never a reimplementation), asserts the released tree carries
// the provenance wording, then runs the same invariant core against the
// released tree. It passes on both a candidate tree (fragment present: the
// release consumes it) and a released tree (fragment already consumed: the
// release runs with --allow-empty over the released CHANGELOG section). The
// rehearsal version is derived proven absent from the staged CHANGELOG, so a
// released history that already contains an older rehearsal version stays
// green (round-2 F1).
func TestLaunchBoundaryProvenanceAfterChangelogRelease(t *testing.T) {
	moduleRoot := lbProvenanceModuleRoot(t)
	staged := t.TempDir()
	fragmentStaged := lbStageRehearsalFiles(t, moduleRoot, staged)
	lbInitRehearsalRepo(t, staged, "stage launch-boundary provenance rehearsal")
	lbAssertRehearsalHomeIsolated(t, staged)
	version := lbNextAbsentRehearsalVersion(t, staged)
	var release []string
	if fragmentStaged {
		release = []string{"bash", ".scripts/changelog-release.sh", version, "2026-10-05"}
	} else {
		release = []string{"bash", ".scripts/changelog-release.sh", "--allow-empty", version, "2026-10-05"}
	}
	releaseOutput, releaseExit := lbRunCommand(t, staged, time.Minute, release...)
	if releaseExit != 0 {
		t.Fatalf("provenance invariant violated: changelog-release.sh rehearsal failed (exit %d):\n%s", releaseExit, releaseOutput)
	}
	lbAssertRehearsalReleasedTree(t, staged)
	lbCheckProvenance(t, staged)
}

// TestLaunchBoundaryProvenanceAfterSecondChangelogRelease is the named
// regression for BUG-261005-28qxur F1: the round-1 rehearsal required the
// fragment, so it stayed red on a released checkout. It starts from
// whatever the checkout holds (fragment or released section), runs the
// production changelog-release.sh entry point twice in the temporary copy
// (the second run proves a tree that has already been released once stays
// green), and runs the invariant core against the twice-released tree.
func TestLaunchBoundaryProvenanceAfterSecondChangelogRelease(t *testing.T) {
	moduleRoot := lbProvenanceModuleRoot(t)
	staged := t.TempDir()
	fragmentStaged := lbStageRehearsalFiles(t, moduleRoot, staged)
	lbInitRehearsalRepo(t, staged, "stage launch-boundary provenance double rehearsal")
	lbAssertRehearsalHomeIsolated(t, staged)
	firstVersion := lbNextAbsentRehearsalVersion(t, staged)
	var first []string
	if fragmentStaged {
		first = []string{"bash", ".scripts/changelog-release.sh", firstVersion, "2026-10-05"}
	} else {
		first = []string{"bash", ".scripts/changelog-release.sh", "--allow-empty", firstVersion, "2026-10-05"}
	}
	if output, exit := lbRunCommand(t, staged, time.Minute, first...); exit != 0 {
		t.Fatalf("provenance invariant violated: first changelog-release.sh rehearsal failed (exit %d):\n%s", exit, output)
	}
	lbAssertRehearsalReleasedTree(t, staged)
	lbCommitRehearsalState(t, staged, "commit first rehearsal release")
	secondVersion := lbNextAbsentRehearsalVersion(t, staged)
	secondOutput, secondExit := lbRunCommand(t, staged, time.Minute, "bash", ".scripts/changelog-release.sh", "--allow-empty", secondVersion, "2026-10-05")
	if secondExit != 0 {
		t.Fatalf("provenance invariant violated: second changelog-release.sh rehearsal failed (exit %d):\n%s", secondExit, secondOutput)
	}
	lbAssertRehearsalReleasedTree(t, staged)
	lbCheckProvenance(t, staged)
}

// TestLaunchBoundaryProvenanceAfterChangelogReleaseWithCollidingHistory is the
// named regression for BUG-261005-28qxur round-2 F1
// (rehearsal-fixed-version-collision): the round-2 rehearsals used fixed
// versions v9.9.8/v9.9.9, so a released history already containing v9.9.9
// failed both rehearsals at the production duplicate-version refusal while
// the invariant itself stayed green. It stages the same inputs, injects
// committed released sections for the previously fixed versions v9.9.9 and
// v9.9.10, derives a version proven absent from that history, and runs the
// production changelog-release.sh entry point plus the invariant core. A
// fixed-version mutant (rehearsal pinned back to v9.9.9) fails here by name
// at the production duplicate-version refusal, which is the oracle proving
// the derived version was absent.
func TestLaunchBoundaryProvenanceAfterChangelogReleaseWithCollidingHistory(t *testing.T) {
	moduleRoot := lbProvenanceModuleRoot(t)
	staged := t.TempDir()
	fragmentStaged := lbStageRehearsalFiles(t, moduleRoot, staged)
	lbInjectCollidingHistory(t, staged, "v9.9.9", "v9.9.10")
	lbInitRehearsalRepo(t, staged, "stage launch-boundary provenance colliding-history rehearsal")
	lbAssertRehearsalHomeIsolated(t, staged)
	version := lbNextAbsentRehearsalVersion(t, staged)
	var release []string
	if fragmentStaged {
		release = []string{"bash", ".scripts/changelog-release.sh", version, "2026-10-05"}
	} else {
		release = []string{"bash", ".scripts/changelog-release.sh", "--allow-empty", version, "2026-10-05"}
	}
	releaseOutput, releaseExit := lbRunCommand(t, staged, time.Minute, release...)
	if releaseExit != 0 {
		t.Fatalf("provenance invariant violated: changelog-release.sh colliding-history rehearsal failed (exit %d):\n%s", releaseExit, releaseOutput)
	}
	lbAssertRehearsalReleasedTree(t, staged)
	lbCheckProvenance(t, staged)
}

// TestLaunchBoundaryProvenanceRehearsalHomeIsolated is the named regression
// for BUG-261005-28qxur F2: the round-1 rehearsal subprocess environment
// inherited the caller HOME. It proves through the production lbRunCommand
// path that no rehearsal subprocess observes the caller HOME.
func TestLaunchBoundaryProvenanceRehearsalHomeIsolated(t *testing.T) {
	lbAssertRehearsalHomeIsolated(t, t.TempDir())
}
