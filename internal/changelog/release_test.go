package changelog_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the production entry point end to end:
// .scripts/changelog-release.sh itself, executed inside temporary git
// repositories. Nothing here reimplements the script's checks, because the
// properties under test — validation diagnostics, refusal behavior, add
// ordering, byte-exact assembly — can only be observed through the script.
//
// Git identity is passed per-invocation with -c flags. The script under test
// is located through the module root (the directory carrying go.mod), so the
// suite binds to the checkout it runs in.

// fixtureChangelog is the CHANGELOG.md every temporary repository starts
// from: a title, an Unreleased section with two bullets (one with a
// continuation line), and one previously released section.
const fixtureChangelog = `# Changelog

## Unreleased

- Existing unreleased bullet one.
- Existing unreleased bullet two.
  with a continuation line.

## v0.1.0 — 2026-01-01

- First release bullet.
`

// fixtureReadme is the changelog.d/README.md every temporary repository
// starts from. It deliberately violates every fragment rule (headings, bare
// text) so the tests prove README.md is ignored rather than validated.
const fixtureReadme = `# Fragments

One file per change. This documentation is not a fragment.
`

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	start := dir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found at or above %s", start)
		}
		dir = parent
	}
}

func scriptPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".scripts", "changelog-release.sh")
	// Child behavioral runs exercise isolated narrowing mutants of the real
	// script. This override belongs to the test harness, never production.
	if override := os.Getenv("CHANGELOG_TEST_SCRIPT"); override != "" {
		path = override
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("release script missing at %s: %v", path, err)
	}
	return path
}

// runScript executes the release script in dir and returns its captured
// output and exit code. A failure to launch the script itself is fatal;
// a non-zero script exit is an ordinary result.
func runScript(t *testing.T, dir string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	var out, errOut bytes.Buffer
	c := exec.Command(scriptPath(t), args...)
	c.Dir = dir
	c.Env = fixtureEnv()
	c.Stdout = &out
	c.Stderr = &errOut
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running changelog-release.sh %v in %s: %v", args, dir, err)
		}
		exit = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), exit
}

// runScriptEnv executes the release script in dir with extra environment
// variables (hostile git config injection) and returns its captured output
// and exit code.
func runScriptEnv(t *testing.T, dir string, env []string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	var out, errOut bytes.Buffer
	c := exec.Command(scriptPath(t), args...)
	c.Dir = dir
	c.Stdout = &out
	c.Stderr = &errOut
	c.Env = append(fixtureEnv(), env...)
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running changelog-release.sh %v in %s: %v", args, dir, err)
		}
		exit = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), exit
}

// runGit executes git in dir and returns its stdout. Any failure is fatal;
// tests that assert refusal behavior never reach for failing git commands.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	c := exec.Command("git", append([]string{"-c", "diff.algorithm=myers", "-c", "color.ui=never"}, args...)...)
	c.Dir = dir
	c.Env = fixtureEnv()
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Run(); err != nil {
		t.Fatalf("running git %v in %s: %v\n%s", args, dir, err, out.String())
	}
	return out.String()
}

// Fixtures do not inherit Git identity/configuration or shell startup hooks.
// Hostile inputs are supplied explicitly by runScriptEnv or local config.
func fixtureEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_") || strings.HasPrefix(entry, "TASK_BOARD_") ||
			strings.HasPrefix(entry, "BASH_ENV=") || strings.HasPrefix(entry, "LC_ALL=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GIT_CONFIG_COUNT=0", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C")
}

// commitAll stages the whole tree and commits with a fixed test identity.
// Identity travels on -c flags: the suite never runs git config.
func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir,
		"-c", "user.name=Changelog Test",
		"-c", "user.email=changelog-test@example.com",
		"-c", "commit.gpgsign=false",
		"commit", "-qm", message,
	)
}

// initRepo builds a temporary git repository with the fixture CHANGELOG.md
// and changelog.d/README.md committed, and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), fixtureChangelog)
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d"), 0o755); err != nil {
		t.Fatalf("creating changelog.d in %s: %v", dir, err)
	}
	writeFile(t, filepath.Join(dir, "changelog.d", "README.md"), fixtureReadme)
	commitAll(t, dir, "init")
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func writeBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

// snapshotTree records the bytes of every assertion-relevant path plus the
// git state, so refusal tests can prove the script changed nothing.
type treeSnapshot struct {
	changelog string
	fragments map[string]string
	status    string
	head      string
	tags      string
}

func snapshot(t *testing.T, dir string) treeSnapshot {
	t.Helper()
	snap := treeSnapshot{
		changelog: readFile(t, filepath.Join(dir, "CHANGELOG.md")),
		fragments: map[string]string{},
		status:    runGit(t, dir, "status", "--porcelain"),
		head:      runGit(t, dir, "rev-parse", "HEAD"),
		tags:      runGit(t, dir, "tag", "--list"),
	}
	entries, err := os.ReadDir(filepath.Join(dir, "changelog.d"))
	if err != nil {
		t.Fatalf("reading changelog.d in %s: %v", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		snap.fragments[entry.Name()] = readFile(t, filepath.Join(dir, "changelog.d", entry.Name()))
	}
	return snap
}

func (s treeSnapshot) assertUnchanged(t *testing.T, dir, context string) {
	t.Helper()
	after := snapshot(t, dir)
	if after.changelog != s.changelog {
		t.Errorf("%s: CHANGELOG.md changed by a refusing run:\nbefore: %q\nafter: %q", context, s.changelog, after.changelog)
	}
	if len(after.fragments) != len(s.fragments) {
		t.Errorf("%s: fragment set changed by a refusing run: before %v, after %v", context, keys(s.fragments), keys(after.fragments))
	}
	for name, before := range s.fragments {
		if after.fragments[name] != before {
			t.Errorf("%s: fragment %s changed by a refusing run", context, name)
		}
	}
	if after.head != s.head {
		t.Errorf("%s: HEAD moved on a refusing run: %q -> %q", context, s.head, after.head)
	}
	if after.tags != s.tags {
		t.Errorf("%s: tags changed on a refusing run: %q -> %q", context, s.tags, after.tags)
	}
	if after.status != s.status {
		t.Errorf("%s: worktree/index status changed on a refusing run: %q -> %q", context, s.status, after.status)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// diffLines renders a short line-oriented diff for golden failures.
func diffLines(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	var b strings.Builder
	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}
	shown := 0
	for i := 0; i < max; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			fmt.Fprintf(&b, "line %d:\n  want %q\n  got  %q\n", i+1, w, g)
			if shown++; shown >= 10 {
				b.WriteString("  ... (truncated)\n")
				break
			}
		}
	}
	return b.String()
}

// --- --check mode ----------------------------------------------------------

func TestCheckAcceptsValidFragments(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "good.md"),
		"- First bullet.\n- Second bullet with\n  an indented continuation.\n\n- Third after a blank line.\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "TASK-261002-2w6op9.md"), "- Board-id name.\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- Single-char slug.\n")

	stdout, stderr, exit := runScript(t, dir, "--check")
	if exit != 0 {
		t.Fatalf("--check over valid fragments exited %d\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "3 fragment(s) valid") {
		t.Errorf("--check success message names no count: %q", stdout)
	}
}

func TestCheckIgnoresReadme(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	// The fixture README already violates every fragment rule; with no
	// fragments present --check must still pass.
	_, _, exit := runScript(t, dir, "--check")
	if exit != 0 {
		t.Fatal("--check refused a fragment-free tree whose README.md breaks fragment rules: README.md must be ignored")
	}
}

func TestCheckChangesNothing(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
	before := snapshot(t, dir)
	stdout, stderr, exit := runScript(t, dir, "--check")
	if exit != 0 {
		t.Fatalf("--check exited %d\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}
	before.assertUnchanged(t, dir, "--check")
	if after := runGit(t, dir, "status", "--porcelain"); after != before.status {
		t.Errorf("--check changed git status:\nbefore: %q\nafter: %q", before.status, after)
	}
}

func TestCheckRefusesBadName(t *testing.T) {
	t.Parallel()
	cases := []string{
		"bad_name.md", // mutant M1 admits exactly this member
		"has space.md",
		"UPPER.MD",
		"-lead.md",
		"trail-.md",
		"noext",
		".md",
		".hidden.md",
		"dots.in.name.md",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
			writeFile(t, filepath.Join(dir, "changelog.d", name), "- A bullet.\n")
			_, stderr, exit := runScript(t, dir, "--check")
			if exit == 0 {
				t.Fatalf("--check accepted fragment name %q", name)
			}
			want := "changelog.d/" + name + ":1: invalid fragment name"
			if !strings.Contains(stderr, want) {
				t.Errorf("--check diagnostic for %q names no file:line: stderr %q, want substring %q", name, stderr, want)
			}
			if strings.Contains(stderr, "good.md") {
				t.Errorf("--check blamed the valid fragment too: %q", stderr)
			}
		})
	}
}

func TestCheckRefusesEmptyFragment(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "empty.md"), "")
	_, stderr, exit := runScript(t, dir, "--check")
	if exit == 0 {
		t.Fatal("--check accepted an empty fragment")
	}
	if want := "changelog.d/empty.md:1: empty fragment"; !strings.Contains(stderr, want) {
		t.Errorf("stderr %q wants substring %q", stderr, want)
	}
}

func TestCheckRefusesMissingTrailingNewline(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		line    string
	}{
		{"no-newline.md", "- Bullet without trailing newline", ":1:"},
		{"two-lines.md", "- One\n- Two", ":2:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, "changelog.d", c.name), c.content)
			_, stderr, exit := runScript(t, dir, "--check")
			if exit == 0 {
				t.Fatalf("--check accepted %q without a trailing newline", c.name)
			}
			want := "changelog.d/" + c.name + c.line + " missing trailing newline"
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr %q wants substring %q", stderr, want)
			}
		})
	}
}

func TestCheckRefusesInvalidUTF8(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeBytes(t, filepath.Join(dir, "changelog.d", "bad-utf8.md"), []byte("- Bullet with bad bytes \xff\xfe\n"))
	_, stderr, exit := runScript(t, dir, "--check")
	if exit == 0 {
		t.Fatal("--check accepted a fragment with invalid UTF-8")
	}
	if want := "changelog.d/bad-utf8.md:1: invalid UTF-8"; !strings.Contains(stderr, want) {
		t.Errorf("stderr %q wants substring %q", stderr, want)
	}
}

func TestCheckRefusesHeading(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		line    string
	}{
		{"head.md", "- Ok bullet.\n\n# Heading\n", ":3:"},
		{"head-first.md", "## Sub\n\n- x\n", ":1:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, "changelog.d", c.name), c.content)
			_, stderr, exit := runScript(t, dir, "--check")
			if exit == 0 {
				t.Fatalf("--check accepted a fragment with a heading (%q)", c.name)
			}
			want := "changelog.d/" + c.name + c.line + " heading not allowed"
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr %q wants substring %q", stderr, want)
			}
		})
	}
}

func TestCheckRefusesFrontMatter(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "matter.md"), "---\ntitle: x\n---\n\n- x\n")
	_, stderr, exit := runScript(t, dir, "--check")
	if exit == 0 {
		t.Fatal("--check accepted a fragment with front matter")
	}
	if want := "changelog.d/matter.md:1: front matter not allowed"; !strings.Contains(stderr, want) {
		t.Errorf("stderr %q wants substring %q", stderr, want)
	}
}

func TestCheckRefusesNonBulletContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		line    string
		message string
	}{
		{"plain.md", "Just a paragraph.\n", ":1:", "not a bullet or continuation line"},
		{"plain-second.md", "- Ok.\nA bare second line.\n", ":2:", "not a bullet or continuation line"},
		{"indented-first.md", "  Indented before any bullet.\n- x\n", ":1:", "indented line before any bullet"},
		{"dash-only.md", "-\n", ":1:", "not a bullet or continuation line"},
		{"empty-bullet.md", "-   \n", ":1:", "empty bullet"},
		{"no-space.md", "-nospace\n", ":1:", "not a bullet or continuation line"},
		{"blanks-only.md", "\n\n", ":1:", "no bullets"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, "changelog.d", c.name), c.content)
			_, stderr, exit := runScript(t, dir, "--check")
			if exit == 0 {
				t.Fatalf("--check accepted non-bullet content in %q", c.name)
			}
			want := "changelog.d/" + c.name + c.line + " " + c.message
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr %q wants substring %q", stderr, want)
			}
		})
	}
}

func TestCheckRefusesMissingDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), fixtureChangelog)
	commitAll(t, dir, "init without changelog.d")
	_, stderr, exit := runScript(t, dir, "--check")
	if exit == 0 {
		t.Fatal("--check passed with no changelog.d directory")
	}
	if !strings.Contains(stderr, "no changelog.d directory") {
		t.Errorf("stderr %q names no missing directory", stderr)
	}
}

func TestRepoFragmentsPassCheck(t *testing.T) {
	t.Parallel()
	// The repository's own committed fragments must satisfy the grammar the
	// suite enforces. --check changes nothing, so running it against the
	// real checkout is safe.
	_, stderr, exit := runScript(t, repoRoot(t), "--check")
	if exit != 0 {
		t.Fatalf("--check on the repository itself exited %d: %s", exit, stderr)
	}
}

// --- release mode ---------------------------------------------------------

// releaseFixture commits four fragments in a name-defying add order: b
// first, a second, c and d together in one commit. The release must order
// them b, a, c, d: add order wins over name order, and the same-commit tie
// breaks by name.
func releaseFixture(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "b-second.md"),
		"- Second fragment by name, first by add order.\n")
	commitAll(t, dir, "add b-second")
	writeFile(t, filepath.Join(dir, "changelog.d", "a-first.md"),
		"- First fragment by name, second by add order.\n  Continued here.\n")
	commitAll(t, dir, "add a-first")
	writeFile(t, filepath.Join(dir, "changelog.d", "c-tie.md"), "- Tie fragment one.\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "d-tie.md"), "- Tie fragment two.\n")
	commitAll(t, dir, "add c-tie and d-tie")
	return dir
}

func TestReleaseWritesSectionRemovesFragments(t *testing.T) {
	t.Parallel()
	dir := releaseFixture(t)
	headBefore := runGit(t, dir, "rev-parse", "HEAD")

	stdout, stderr, exit := runScript(t, dir, "v0.2.0", "2026-10-02")
	if exit != 0 {
		t.Fatalf("release exited %d\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}

	// Byte-exact golden: carry-over plus fragments in add order, an empty
	// Unreleased heading, and the previously released section intact.
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "changelog", "testdata", "golden-release.md"))
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	if got != string(want) {
		t.Errorf("CHANGELOG.md differs from the golden:\n%s", diffLines(string(want), got))
	}

	// Consumed fragments are removed; the grammar doc stays.
	for _, name := range []string{"a-first.md", "b-second.md", "c-tie.md", "d-tie.md"} {
		if _, err := os.Stat(filepath.Join(dir, "changelog.d", name)); !os.IsNotExist(err) {
			t.Errorf("fragment %s survived the release", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "changelog.d", "README.md")); err != nil {
		t.Errorf("changelog.d/README.md did not survive the release: %v", err)
	}

	// The script commits nothing and tags nothing: HEAD and the tag list
	// are untouched, and every change sits in the working tree.
	if head := runGit(t, dir, "rev-parse", "HEAD"); head != headBefore {
		t.Errorf("HEAD moved during release: %q -> %q", headBefore, head)
	}
	if tags := runGit(t, dir, "tag", "--list"); tags != "" {
		t.Errorf("release created tags: %q", tags)
	}
	status := runGit(t, dir, "status", "--porcelain")
	for _, wantLine := range []string{
		"M CHANGELOG.md",
		"D changelog.d/a-first.md",
		"D changelog.d/b-second.md",
		"D changelog.d/c-tie.md",
		"D changelog.d/d-tie.md",
	} {
		if !strings.Contains(status, wantLine) {
			t.Errorf("git status lacks %q:\n%s", wantLine, status)
		}
	}
	if lines := strings.Count(strings.TrimSpace(status), "\n") + 1; lines != 5 {
		t.Errorf("git status carries %d lines, want exactly 5:\n%s", lines, status)
	}
}

func TestReleaseStacksNewestFirst(t *testing.T) {
	t.Parallel()
	dir := releaseFixture(t)
	if _, _, exit := runScript(t, dir, "v0.2.0", "2026-10-02"); exit != 0 {
		t.Fatalf("first release exited %d", exit)
	}
	commitAll(t, dir, "release v0.2.0")
	writeFile(t, filepath.Join(dir, "changelog.d", "e-lone.md"), "- Lone second-release fragment.\n")
	commitAll(t, dir, "add e-lone")
	if _, _, exit := runScript(t, dir, "v0.3.0", "2026-10-09"); exit != 0 {
		t.Fatalf("second release exited %d", exit)
	}

	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	order := []string{
		"## v0.3.0 — 2026-10-09",
		"## Unreleased",
		"## v0.2.0 — 2026-10-02",
		"## v0.1.0 — 2026-01-01",
	}
	at := -1
	for _, header := range order {
		next := strings.Index(got, header)
		if next < 0 {
			t.Fatalf("released file lacks header %q:\n%s", header, got)
		}
		if next <= at {
			t.Fatalf("header %q out of order in:\n%s", header, got)
		}
		at = next
	}
	if !strings.Contains(got, "- Lone second-release fragment.\n\n## Unreleased") {
		t.Errorf("second release section misassembled:\n%s", got)
	}
	// Between the empty Unreleased heading and the next section: blanks only.
	unreleased := strings.Index(got, "## Unreleased\n")
	rest := got[unreleased+len("## Unreleased\n"):]
	nextHeader := strings.Index(rest, "## ")
	for _, line := range strings.Split(rest[:nextHeader], "\n") {
		if strings.TrimSpace(line) != "" {
			t.Errorf("Unreleased heading not left empty: %q", rest[:nextHeader])
			break
		}
	}
}

func TestReleaseRefusesInvalidVersion(t *testing.T) {
	t.Parallel()
	versions := []string{"0.5.39", "v0.5", "v1.2.3.4", "v", "latest", "", "vv1.2.3", "v1.2.x", "v1..2"}
	for _, version := range versions {
		t.Run(fmt.Sprintf("version=%q", version), func(t *testing.T) {
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
			commitAll(t, dir, "add good")
			before := snapshot(t, dir)
			_, stderr, exit := runScript(t, dir, version, "2026-10-02")
			if exit == 0 {
				t.Fatalf("release accepted version %q", version)
			}
			if !strings.Contains(stderr, "invalid version") {
				t.Errorf("stderr %q names no invalid version", stderr)
			}
			before.assertUnchanged(t, dir, "invalid version")
		})
	}
}

func TestReleaseRefusesInvalidDate(t *testing.T) {
	t.Parallel()
	dates := []string{
		"2026-13-01", // mutant M8 admits exactly this member
		"2026-00-10",
		"2026-02-30",
		"2026-02-29",
		"2023-02-29",
		"1900-02-29",
		"2026-04-31",
		"2026-1-1",
		"01/02/2026",
		"not-a-date",
		"",
		"2026-10-02 ",
	}
	for _, date := range dates {
		t.Run(fmt.Sprintf("date=%q", date), func(t *testing.T) {
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
			commitAll(t, dir, "add good")
			before := snapshot(t, dir)
			_, stderr, exit := runScript(t, dir, "v0.2.0", date)
			if exit == 0 {
				t.Fatalf("release accepted date %q", date)
			}
			if !strings.Contains(stderr, "invalid date") {
				t.Errorf("stderr %q names no invalid date", stderr)
			}
			before.assertUnchanged(t, dir, "invalid date")
		})
	}
}

func TestReleaseAcceptsValidDates(t *testing.T) {
	t.Parallel()
	dates := []string{"2026-10-02", "2024-02-29", "2000-02-29"}
	for _, date := range dates {
		t.Run(fmt.Sprintf("date=%q", date), func(t *testing.T) {
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
			commitAll(t, dir, "add good")
			stdout, stderr, exit := runScript(t, dir, "v0.2.0", date)
			if exit != 0 {
				t.Fatalf("release refused valid date %q:\nstdout: %s\nstderr: %s", date, stdout, stderr)
			}
			if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); !strings.Contains(got, "## v0.2.0 — "+date) {
				t.Errorf("released file lacks the dated header for %q", date)
			}
		})
	}
}

func TestReleaseRefusesDuplicateVersion(t *testing.T) {
	t.Parallel()
	t.Run("second run with the same version refuses", func(t *testing.T) {
		dir := releaseFixture(t)
		if _, _, exit := runScript(t, dir, "v9.9.9", "2026-10-02"); exit != 0 {
			t.Fatalf("first release exited %d", exit)
		}
		commitAll(t, dir, "release v9.9.9")
		before := snapshot(t, dir)
		_, stderr, exit := runScript(t, dir, "v9.9.9", "2026-11-11")
		if exit == 0 {
			t.Fatal("second run with the same version succeeded")
		}
		if !strings.Contains(stderr, "already present") {
			t.Errorf("stderr %q names no duplicate version", stderr)
		}
		before.assertUnchanged(t, dir, "duplicate version")
	})
	t.Run("legacy bare header collides", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), fixtureChangelog+"\n## v8.8.8\n\n- Old.\n")
		writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
		commitAll(t, dir, "legacy header")
		before := snapshot(t, dir)
		_, _, exit := runScript(t, dir, "v8.8.8", "2026-10-02")
		if exit == 0 {
			t.Fatal("release accepted a version carried by a legacy bare header")
		}
		before.assertUnchanged(t, dir, "duplicate legacy version")
	})
	t.Run("legacy unreleased-dash header collides", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), fixtureChangelog+"\n## Unreleased — v7.7.7\n\n- Old.\n")
		writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
		commitAll(t, dir, "legacy unreleased header")
		_, _, exit := runScript(t, dir, "v7.7.7", "2026-10-02")
		if exit == 0 {
			t.Fatal("release accepted a version carried by a legacy Unreleased header")
		}
	})
}

func TestReleaseDuplicateCheckIgnoresBulletsAndSubstrings(t *testing.T) {
	t.Parallel()
	t.Run("bullet citing a future version does not collide", func(t *testing.T) {
		dir := initRepo(t)
		withCitation := strings.Replace(fixtureChangelog,
			"- Existing unreleased bullet one.",
			"- Existing unreleased bullet one. Expected release v9.9.9; the orchestrator cuts the tag.", 1)
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), withCitation)
		writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
		commitAll(t, dir, "bullet cites future version")
		_, stderr, exit := runScript(t, dir, "v9.9.9", "2026-10-02")
		if exit != 0 {
			t.Fatalf("release refused v9.9.9 over a bullet citation: %s", stderr)
		}
	})
	t.Run("released longer version does not collide with its prefix", func(t *testing.T) {
		dir := initRepo(t)
		withLonger := strings.Replace(fixtureChangelog,
			"## v0.1.0 — 2026-01-01", "## v2.0.39 — 2026-01-01", 1)
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), withLonger)
		writeFile(t, filepath.Join(dir, "changelog.d", "good.md"), "- A bullet.\n")
		commitAll(t, dir, "longer version released")
		_, stderr, exit := runScript(t, dir, "v2.0.3", "2026-10-02")
		if exit != 0 {
			t.Fatalf("release refused v2.0.3 over a released v2.0.39: %s", stderr)
		}
	})
}

func TestReleaseRefusesDirtyTree(t *testing.T) {
	t.Parallel()
	t.Run("tracked modification", func(t *testing.T) {
		dir := releaseFixture(t)
		before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), before+"- Uncommitted bullet.\n")
		_, stderr, exit := runScript(t, dir, "v0.2.0", "2026-10-02")
		if exit == 0 {
			t.Fatal("release ran on a dirty tree")
		}
		if !strings.Contains(stderr, "dirty tree") {
			t.Errorf("stderr %q names no dirty tree", stderr)
		}
		if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != before+"- Uncommitted bullet.\n" {
			t.Error("refusing release touched CHANGELOG.md")
		}
		if _, err := os.Stat(filepath.Join(dir, "changelog.d", "a-first.md")); err != nil {
			t.Error("refusing release removed a fragment")
		}
	})
	t.Run("untracked file", func(t *testing.T) {
		dir := releaseFixture(t)
		writeFile(t, filepath.Join(dir, "untracked.txt"), "untracked\n")
		before := snapshot(t, dir)
		_, stderr, exit := runScript(t, dir, "v0.2.0", "2026-10-02")
		if exit == 0 {
			t.Fatal("release ran with an untracked file present")
		}
		if !strings.Contains(stderr, "dirty tree") {
			t.Errorf("stderr %q names no dirty tree", stderr)
		}
		before.assertUnchanged(t, dir, "dirty tree")
	})
}

func TestReleaseRefusesEmptyWithoutAllowEmpty(t *testing.T) {
	t.Parallel()
	t.Run("carry-over only still refuses", func(t *testing.T) {
		dir := initRepo(t)
		before := snapshot(t, dir)
		_, stderr, exit := runScript(t, dir, "v0.2.0", "2026-10-02")
		if exit == 0 {
			t.Fatal("release ran with no fragments and no --allow-empty")
		}
		if !strings.Contains(stderr, "no fragments") {
			t.Errorf("stderr %q names no empty fragment set", stderr)
		}
		before.assertUnchanged(t, dir, "empty without --allow-empty")
	})
	t.Run("totally empty still refuses", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n## Unreleased\n")
		commitAll(t, dir, "empty unreleased")
		before := snapshot(t, dir)
		_, _, exit := runScript(t, dir, "v0.2.0", "2026-10-02")
		if exit == 0 {
			t.Fatal("release ran totally empty with no --allow-empty")
		}
		before.assertUnchanged(t, dir, "totally empty without --allow-empty")
	})
}

func TestReleaseAllowEmpty(t *testing.T) {
	t.Parallel()
	t.Run("carry-over only", func(t *testing.T) {
		dir := initRepo(t)
		headBefore := runGit(t, dir, "rev-parse", "HEAD")
		_, stderr, exit := runScript(t, dir, "--allow-empty", "v0.2.0", "2026-10-02")
		if exit != 0 {
			t.Fatalf("--allow-empty release exited %d: %s", exit, stderr)
		}
		want := "# Changelog\n\n## v0.2.0 — 2026-10-02\n\n" +
			"- Existing unreleased bullet one.\n" +
			"- Existing unreleased bullet two.\n" +
			"  with a continuation line.\n\n" +
			"## Unreleased\n\n## v0.1.0 — 2026-01-01\n\n- First release bullet.\n"
		if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != want {
			t.Errorf("carry-over-only release differs:\n%s", diffLines(want, got))
		}
		if head := runGit(t, dir, "rev-parse", "HEAD"); head != headBefore {
			t.Errorf("HEAD moved during --allow-empty release")
		}
	})
	t.Run("totally empty writes an empty section", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n## Unreleased\n")
		commitAll(t, dir, "empty unreleased")
		_, stderr, exit := runScript(t, dir, "v0.2.0", "2026-10-02", "--allow-empty")
		if exit != 0 {
			t.Fatalf("--allow-empty release on a totally empty tree exited %d: %s", exit, stderr)
		}
		want := "# Changelog\n\n## v0.2.0 — 2026-10-02\n\n## Unreleased\n"
		if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != want {
			t.Errorf("empty release differs:\n%s", diffLines(want, got))
		}
	})
}

func TestReleaseRefusesInvalidFragment(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "plain.md"), "Just a paragraph.\n")
	commitAll(t, dir, "landed an invalid fragment before gating")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v0.2.0", "2026-10-02")
	if exit == 0 {
		t.Fatal("release consumed an invalid fragment")
	}
	if !strings.Contains(stderr, "changelog.d/plain.md:1:") {
		t.Errorf("stderr %q names no fragment violation with file:line", stderr)
	}
	before.assertUnchanged(t, dir, "invalid fragment")
}

func TestReleaseCommitsNorTagsNothing(t *testing.T) {
	t.Parallel()
	dir := releaseFixture(t)
	headBefore := runGit(t, dir, "rev-parse", "HEAD")
	logBefore := runGit(t, dir, "log", "--oneline")
	if _, _, exit := runScript(t, dir, "v0.2.0", "2026-10-02"); exit != 0 {
		t.Fatalf("release exited %d", exit)
	}
	if head := runGit(t, dir, "rev-parse", "HEAD"); head != headBefore {
		t.Errorf("HEAD moved during release: %q -> %q", headBefore, head)
	}
	if log := runGit(t, dir, "log", "--oneline"); log != logBefore {
		t.Errorf("commit log changed during release")
	}
	if tags := runGit(t, dir, "tag", "--list"); tags != "" {
		t.Errorf("release created tags: %q", tags)
	}
	// Nothing staged either: the fragment removals sit unstaged next to the
	// CHANGELOG edit, for the tagger to review and commit as one unit.
	status := runGit(t, dir, "status", "--porcelain")
	for _, line := range strings.Split(status, "\n") {
		if line == "" {
			continue
		}
		if len(line) < 2 || line[0] != ' ' {
			t.Errorf("release staged index changes: %q in:\n%s", line, status)
		}
	}
}

// --- usage -----------------------------------------------------------------

func TestUsageErrors(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	cases := [][]string{
		{},
		{"v0.2.0"},
		{"v0.2.0", "2026-10-02", "extra"},
		{"--bogus"},
		{"--check", "v0.2.0"},
		{"--check", "--allow-empty"},
		{"--allow-empty"},
	}
	for _, args := range cases {
		t.Run(fmt.Sprintf("args=%q", args), func(t *testing.T) {
			_, stderr, exit := runScript(t, dir, args...)
			if exit != 2 {
				t.Errorf("args %q exited %d, want 2 (usage error); stderr %q", args, exit, stderr)
			}
			if !strings.Contains(stderr, "usage:") {
				t.Errorf("args %q printed no usage; stderr %q", args, stderr)
			}
		})
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	for _, flag := range []string{"--help", "-h"} {
		stdout, _, exit := runScript(t, dir, flag)
		if exit != 0 {
			t.Errorf("%s exited %d, want 0", flag, exit)
		}
		if !strings.Contains(stdout, "usage:") {
			t.Errorf("%s printed no usage; stdout %q", flag, stdout)
		}
	}
}

// --- rework rev2 regressions -----------------------------------------------
// Each test below is the named reproducer for one blocking finding from
// TASK-261002-2w6op9_review-verdict-rev1.md. They drive the production entry
// point (.scripts/changelog-release.sh via runScript/runScriptEnv) in
// temporary git repositories.

// hostileStatusEnv hides untracked files from a naive "git status
// --porcelain" read. The dirty gate must refuse anyway.
func hostileStatusEnv() []string {
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=status.showUntrackedFiles",
		"GIT_CONFIG_VALUE_0=no",
	}
}

// TestReleaseRefusesDirtyTreeUnderHostileStatusConfig is the regression for
// finding dirty-gate-config-and-ignored-fragments (verdict a3.sh section B):
// an untracked fragment plus status.showUntrackedFiles=no must still refuse,
// and the untracked fragment must survive (never rm'd without a git copy).
func TestReleaseRefusesDirtyTreeUnderHostileStatusConfig(t *testing.T) {
	t.Parallel()
	t.Run("untracked fragment", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "changelog.d", "aaa.md"), "- Committed.\n")
		commitAll(t, dir, "add aaa")
		writeFile(t, filepath.Join(dir, "changelog.d", "bbb.md"), "- Untracked.\n")
		before := snapshot(t, dir)
		_, stderr, exit := runScriptEnv(t, dir, hostileStatusEnv(), "v1.0.0", "2026-02-02")
		if exit == 0 {
			t.Fatal("release ran with an untracked fragment hidden by status.showUntrackedFiles=no")
		}
		if !strings.Contains(stderr, "dirty tree") {
			t.Errorf("stderr %q names no dirty tree", stderr)
		}
		before.assertUnchanged(t, dir, "hostile dirty tree")
		if _, err := os.Stat(filepath.Join(dir, "changelog.d", "bbb.md")); err != nil {
			t.Errorf("untracked fragment bbb.md was consumed despite the refusal: %v", err)
		}
	})
	t.Run("untracked non-fragment", func(t *testing.T) {
		// Proves the dirty gate itself is config-proof, independent of the
		// fragment-tracked check (which only sees changelog.d entries).
		dir := releaseFixture(t)
		writeFile(t, filepath.Join(dir, "untracked.txt"), "untracked\n")
		before := snapshot(t, dir)
		_, stderr, exit := runScriptEnv(t, dir, hostileStatusEnv(), "v0.2.0", "2026-10-02")
		if exit == 0 {
			t.Fatal("release ran with an untracked file hidden by status.showUntrackedFiles=no")
		}
		if !strings.Contains(stderr, "dirty tree") {
			t.Errorf("stderr %q names no dirty tree", stderr)
		}
		before.assertUnchanged(t, dir, "hostile dirty tree (non-fragment)")
	})
	t.Run("untracked hidden by local config file", func(t *testing.T) {
		// Hostile status.showUntrackedFiles=no via the repository-local
		// .git/config file (written directly; the suite never runs git
		// config). GIT_CONFIG_COUNT=0 cannot clear a file, so only the
		// explicit --untracked-files=all flag blocks this member.
		dir := releaseFixture(t)
		writeFile(t, filepath.Join(dir, "untracked.txt"), "untracked\n")
		cfgPath := filepath.Join(dir, ".git", "config")
		cfg := readFile(t, cfgPath)
		writeFile(t, cfgPath, cfg+"\n[status]\n\tshowUntrackedFiles = no\n")
		before := snapshot(t, dir)
		_, stderr, exit := runScript(t, dir, "v0.2.0", "2026-10-02")
		if exit == 0 {
			t.Fatal("release ran with an untracked file hidden by local status.showUntrackedFiles=no")
		}
		if !strings.Contains(stderr, "dirty tree") {
			t.Errorf("stderr %q names no dirty tree", stderr)
		}
		before.assertUnchanged(t, dir, "hostile local dirty tree")
	})
}

// TestReleaseRefusesIgnoredFragment is the regression for the ignored half of
// finding dirty-gate-config-and-ignored-fragments (verdict a3.sh section E):
// a fragment matching .gitignore is invisible to "git status" and must be
// refused explicitly, never consumed and deleted.
func TestReleaseRefusesIgnoredFragment(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "aaa.md"), "- Committed.\n")
	writeFile(t, filepath.Join(dir, ".gitignore"), "changelog.d/ign.md\n")
	commitAll(t, dir, "add aaa and gitignore")
	writeFile(t, filepath.Join(dir, "changelog.d", "ign.md"), "- Ignored frag.\n")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-02-02")
	if exit == 0 {
		t.Fatal("release consumed a gitignored fragment")
	}
	if !strings.Contains(stderr, "ignored by .gitignore") {
		t.Errorf("stderr %q names no ignored fragment", stderr)
	}
	if !strings.Contains(stderr, "changelog.d/ign.md") {
		t.Errorf("stderr %q names no fragment path", stderr)
	}
	before.assertUnchanged(t, dir, "ignored fragment")
	if _, err := os.Stat(filepath.Join(dir, "changelog.d", "ign.md")); err != nil {
		t.Errorf("ignored fragment was deleted despite the refusal: %v", err)
	}
}

// TestCheckValidatesIgnoredFragmentOnDisk pins the documented split: --check
// validates on-disk fragments regardless of git state (so landings can
// validate before committing), while release is the gate that demands
// committed fragments.
func TestCheckValidatesIgnoredFragmentOnDisk(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, ".gitignore"), "changelog.d/ign.md\n")
	commitAll(t, dir, "gitignore")
	writeFile(t, filepath.Join(dir, "changelog.d", "ign.md"), "- Ignored but valid.\n")
	stdout, stderr, exit := runScript(t, dir, "--check")
	if exit != 0 {
		t.Fatalf("--check refused a valid on-disk ignored fragment:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

// TestReleaseOrdersReaddedFragmentByReadd is the regression for finding
// add-order-stale-readd (verdict a4.sh section F): a name consumed by an
// earlier release and later re-added orders by its re-add, after fragments
// added since.
func TestReleaseOrdersReaddedFragmentByReadd(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "docs.md"), "- OLD docs fragment (released in v0.9.0).\n")
	commitAll(t, dir, "c1 add docs")
	if _, _, exit := runScript(t, dir, "v0.9.0", "2026-01-05"); exit != 0 {
		t.Fatalf("first release exited %d", exit)
	}
	commitAll(t, dir, "release v0.9.0")
	writeFile(t, filepath.Join(dir, "changelog.d", "mmm.md"), "- mmm added earlier.\n")
	commitAll(t, dir, "c3 add mmm")
	writeFile(t, filepath.Join(dir, "changelog.d", "docs.md"), "- NEW docs fragment added LAST.\n")
	commitAll(t, dir, "c4 re-add docs")
	if _, _, exit := runScript(t, dir, "v1.0.0", "2026-02-02"); exit != 0 {
		t.Fatalf("second release exited %d", exit)
	}
	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	earlier := strings.Index(got, "- mmm added earlier.")
	last := strings.Index(got, "- NEW docs fragment added LAST.")
	if earlier < 0 || last < 0 {
		t.Fatalf("released file lacks a re-add fixture bullet:\n%s", got)
	}
	if earlier > last {
		t.Errorf("stale add order: re-added docs.md sorts before mmm.md:\n%s", got)
	}
}

// TestReleaseOrdersRenamedFragmentByNewName is the regression for finding
// add-order-rename-config-dependent (verdict a4.sh section G): a renamed
// fragment orders by the commit that created the new name, identically under
// default config, diff.renames=false and diff.renames=copies.
func TestReleaseOrdersRenamedFragmentByNewName(t *testing.T) {
	t.Parallel()
	build := func(t *testing.T) string {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "changelog.d", "zzz.md"), "- A first-added then renamed.\n")
		commitAll(t, dir, "add zzz")
		runGit(t, dir, "mv", "changelog.d/zzz.md", "changelog.d/yyy.md")
		runGit(t, dir,
			"-c", "user.name=Changelog Test",
			"-c", "user.email=changelog-test@example.com",
			"-c", "commit.gpgsign=false",
			"commit", "-qm", "rename zzz to yyy",
		)
		writeFile(t, filepath.Join(dir, "changelog.d", "mmm.md"), "- B added after rename.\n")
		commitAll(t, dir, "add mmm")
		return dir
	}
	release := func(t *testing.T, dir string, env []string) string {
		t.Helper()
		var stdout, stderr string
		var exit int
		if env == nil {
			stdout, stderr, exit = runScript(t, dir, "v1.0.0", "2026-02-02")
		} else {
			stdout, stderr, exit = runScriptEnv(t, dir, env, "v1.0.0", "2026-02-02")
		}
		if exit != 0 {
			t.Fatalf("release exited %d\nstdout: %s\nstderr: %s", exit, stdout, stderr)
		}
		return readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	}
	configs := map[string][]string{
		"default": nil,
		"renames-false": {
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=diff.renames",
			"GIT_CONFIG_VALUE_0=false",
		},
		"renames-copies": {
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=diff.renames",
			"GIT_CONFIG_VALUE_0=copies",
		},
	}
	var baseline string
	for name, env := range configs {
		t.Run(name, func(t *testing.T) {
			got := release(t, build(t), env)
			first := strings.Index(got, "- A first-added then renamed.")
			second := strings.Index(got, "- B added after rename.")
			if first < 0 || second < 0 {
				t.Fatalf("released file lacks a rename fixture bullet:\n%s", got)
			}
			if first > second {
				t.Errorf("rename orders by the stale old name under %s config:\n%s", name, got)
			}
			if baseline == "" {
				baseline = got
			} else if got != baseline {
				t.Errorf("order differs by git config (%s vs baseline):\n%s", name, diffLines(baseline, got))
			}
		})
	}
}

// TestReleaseHostileConfigYieldsIdenticalBytes injects the full hostile set
// from rework-brief-01 (status.showUntrackedFiles=no, diff.renames=copies,
// core.quotePath, plus a .gitignore matching a tracked fragment name) and
// proves the release bytes are identical to the clean-config run. The
// gitignored-but-tracked fragment stays committed by git design (tracked
// paths are never ignored), so the release succeeds in both runs.
func TestReleaseHostileConfigYieldsIdenticalBytes(t *testing.T) {
	t.Parallel()
	hostile := []string{
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=status.showUntrackedFiles",
		"GIT_CONFIG_VALUE_0=no",
		"GIT_CONFIG_KEY_1=diff.renames",
		"GIT_CONFIG_VALUE_1=copies",
		"GIT_CONFIG_KEY_2=core.quotePath",
		"GIT_CONFIG_VALUE_2=true",
	}
	build := func(t *testing.T) string {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, ".gitignore"), "changelog.d/force-added.md\n")
		commitAll(t, dir, "gitignore")
		writeFile(t, filepath.Join(dir, "changelog.d", "b-second.md"), "- Second by name, first by add order.\n")
		commitAll(t, dir, "add b-second")
		writeFile(t, filepath.Join(dir, "changelog.d", "force-added.md"), "- Tracked despite the gitignore pattern.\n")
		runGit(t, dir, "add", "-f", "changelog.d/force-added.md")
		runGit(t, dir,
			"-c", "user.name=Changelog Test",
			"-c", "user.email=changelog-test@example.com",
			"-c", "commit.gpgsign=false",
			"commit", "-qm", "force-add ignored-name fragment",
		)
		return dir
	}
	cleanDir := build(t)
	stdout, stderr, exit := runScript(t, cleanDir, "v0.2.0", "2026-10-02")
	if exit != 0 {
		t.Fatalf("clean-config release exited %d\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}
	want := readFile(t, filepath.Join(cleanDir, "CHANGELOG.md"))

	hostileDir := build(t)
	stdout, stderr, exit = runScriptEnv(t, hostileDir, hostile, "v0.2.0", "2026-10-02")
	if exit != 0 {
		t.Fatalf("hostile-config release exited %d\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}
	got := readFile(t, filepath.Join(hostileDir, "CHANGELOG.md"))
	if got != want {
		t.Errorf("hostile config changed the release bytes:\n%s", diffLines(want, got))
	}
}
