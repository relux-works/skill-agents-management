package changelog_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rootAddFixture reproduces both panels' root-history case: name order is
// opposite to add order, with the first fragment in the root commit itself.
func rootAddFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), fixtureChangelog)
	writeFile(t, filepath.Join(dir, "changelog.d", "README.md"), fixtureReadme)
	writeFile(t, filepath.Join(dir, "changelog.d", "zzz.md"), "- Root commit first.\n")
	commitAll(t, dir, "root")
	writeFile(t, filepath.Join(dir, "changelog.d", "aaa.md"), "- Later commit second.\n")
	commitAll(t, dir, "later")
	return dir
}

func appendLocalConfig(t *testing.T, dir, config string) {
	t.Helper()
	path := filepath.Join(dir, ".git", "config")
	writeFile(t, path, readFile(t, path)+"\n"+config)
}

func releaseBytes(t *testing.T, dir string) string {
	t.Helper()
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
	if exit != 0 {
		t.Fatalf("release exit %d: %s", exit, stderr)
	}
	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	first, second := strings.Index(got, "- Root commit first."), strings.Index(got, "- Later commit second.")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("root add order changed:\n%s", got)
	}
	return got
}

func TestReleaseOrdersRootAddUnderLocalShowRootFalse(t *testing.T) {
	t.Parallel()
	want := releaseBytes(t, rootAddFixture(t))
	dir := rootAddFixture(t)
	appendLocalConfig(t, dir, "[log]\n showRoot = false\n")
	if got := releaseBytes(t, dir); got != want {
		t.Errorf("local showRoot changed bytes:\n%s", diffLines(want, got))
	}
}

func TestReleasePinsLocalLogOutputEncoding(t *testing.T) {
	t.Parallel()
	want := releaseBytes(t, rootAddFixture(t))
	dir := rootAddFixture(t)
	appendLocalConfig(t, dir, "[i18n]\n logOutputEncoding = UTF-16\n")
	if got := releaseBytes(t, dir); got != want {
		t.Errorf("UTF-16 config changed bytes:\n%s", diffLines(want, got))
	}
}

func TestReleaseHandlesInvalidLocalDiffAlgorithm(t *testing.T) {
	t.Parallel()
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprintf("dirty_fragment=%t", dirty), func(t *testing.T) {
			dir := rootAddFixture(t)
			if dirty {
				writeFile(t, filepath.Join(dir, "changelog.d", "zzz.md"), "- UNCOMMITTED replacement.\n")
			}
			before := snapshot(t, dir)
			configPath := filepath.Join(dir, ".git", "config")
			config := readFile(t, configPath)
			appendLocalConfig(t, dir, "[diff]\n algorithm = invalid\n")
			invalidConfig := readFile(t, configPath)
			// Reproduce the panel's actual Git failure independently of the script's
			// fixed -c overrides. Exit 128 is expected-red, not evidence of cleanliness.
			for _, args := range [][]string{{"status", "--porcelain=v1"}, {"log", "--name-only"}} {
				c := exec.Command("git", args...)
				c.Dir = dir
				c.Env = fixtureEnv()
				out, err := c.CombinedOutput()
				if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 128 {
					t.Fatalf("raw git %v: want 128, got %v: %s", args, err, out)
				}
				t.Logf("expected-red raw git %s exit 128: invalid diff.algorithm", args[0])
			}
			_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
			if exit != 1 || !strings.Contains(stderr, "git status") || !strings.Contains(stderr, "128") {
				t.Errorf("invalid-config release exit %d: %s", exit, stderr)
			}
			if got := readFile(t, configPath); got != invalidConfig {
				t.Error("release changed local config")
			}
			// Restore only this fixture's injected config so snapshot's Git
			// observations can run. Check all release inputs and state afterwards.
			writeFile(t, configPath, config)
			before.assertUnchanged(t, dir, "invalid diff.algorithm with uncommitted fragment")
		})
	}
}

func TestReleaseLocalConfigCannotChangeDiscoveryOrOrder(t *testing.T) {
	t.Parallel()
	want := releaseBytes(t, rootAddFixture(t))
	configs := map[string]string{
		"diff-algorithm":         "[diff]\n algorithm = histogram\n",
		"color":                  "[color]\n ui = always\n diff = always\n status = always\n",
		"quote-path":             "[core]\n quotePath = true\n",
		"rename-copies":          "[diff]\n renames = copies\n",
		"external-diff-textconv": "[diff]\n external = /nonexistent-changelog-diff\n[diff \"changelog\"]\n textconv = /nonexistent-changelog-textconv\n",
	}
	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			dir := rootAddFixture(t)
			if name == "external-diff-textconv" {
				writeFile(t, filepath.Join(dir, ".gitattributes"), "changelog.d/*.md diff=changelog\n")
				commitAll(t, dir, "attributes")
			}
			appendLocalConfig(t, dir, config)
			before := snapshot(t, dir)
			stdout, stderr, exit := runScript(t, dir, "--check")
			if exit != 0 || !strings.Contains(stdout, "2 fragment(s) valid") {
				t.Fatalf("check exit %d: %s %s", exit, stdout, stderr)
			}
			before.assertUnchanged(t, dir, "config-independent discovery")
			if got := releaseBytes(t, dir); got != want {
				t.Errorf("local %s changed bytes:\n%s", name, diffLines(want, got))
			}
		})
	}
}

func TestReleaseLocalConfigCannotHideDirtyFragment(t *testing.T) {
	t.Parallel()
	dir := rootAddFixture(t)
	writeFile(t, filepath.Join(dir, ".gitattributes"), "changelog.d/*.md diff=changelog\n")
	commitAll(t, dir, "attributes")
	appendLocalConfig(t, dir, "[log]\n showRoot = false\n[i18n]\n logOutputEncoding = UTF-16\n[diff]\n algorithm = histogram\n renames = copies\n external = /nonexistent-changelog-diff\n[diff \"changelog\"]\n textconv = /nonexistent-changelog-textconv\n[color]\n ui = always\n[core]\n quotePath = true\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "zzz.md"), "- UNCOMMITTED replacement.\n")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
	if exit != 1 || !strings.Contains(stderr, "dirty tree") {
		t.Fatalf("hostile local config dirty gate exit %d: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "hostile local config dirty fragment")
}

func TestReleaseRefusesDuplicateVersionInLargeChangelog(t *testing.T) {
	t.Parallel()
	dir := rootAddFixture(t)
	var history strings.Builder
	history.WriteString(fixtureChangelog)
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&history, "\n## v0.0.%d — 2025-01-01\n\n- Old.\n", i)
	}
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), history.String())
	commitAll(t, dir, "large history")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v0.1.0", "2026-10-02")
	if exit != 1 || !strings.Contains(stderr, "already present") {
		t.Fatalf("large duplicate release exit %d: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "duplicate in large history")
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// gitReadShim records every production Git invocation in order, optionally
// failing one read with exit 128 after plausible partial stdout. The counter
// and trace live outside the fixture so the shim cannot dirty the repository.
func gitReadShim(t *testing.T, failAt int, failCommand string) ([]string, string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	trace, count := filepath.Join(bin, "trace"), filepath.Join(bin, "count")
	body := "#!/bin/sh\ncommand=unknown\nfor arg do\n case \"$arg\" in rev-parse|status|ls-files|check-ignore|log|cat-file|ls-tree|hash-object) command=$arg; break;; esac\ndone\n" +
		"n=0\nif [ -f " + shellQuote(count) + " ]; then read -r n < " + shellQuote(count) + "; fi\nn=$((n + 1))\nprintf '%s\\n' \"$n\" > " + shellQuote(count) + "\nprintf '%s\\n' \"$command\" >> " + shellQuote(trace) + "\n" +
		fmt.Sprintf("if [ \"$n\" -eq %d ] || [ \"$command\" = %s ]; then\n", failAt, shellQuote(failCommand)) +
		" case \"$command\" in\n" +
		" rev-parse) case \"$*\" in *HEAD:*|*--verify*) printf '0000000000000000000000000000000000000000\\n';; *) printf '%s\\n' \"$PWD\";; esac;;\n" +
		" ls-files) case \"$*\" in *-v*) printf 'H changelog.d/zzz.md\\n';; *) printf '100644 0000000000000000000000000000000000000000 0\\tchangelog.d/zzz.md\\n';; esac;;\n" +
		" ls-tree) printf '100644 blob 0000000000000000000000000000000000000000\\tchangelog.d/zzz.md\\n';;\n" +
		" hash-object) printf '0000000000000000000000000000000000000000\\n';;\n" +
		" cat-file) printf '%s\\n' \"- Plausible blob.\";;\n log) printf 'COMMIT partial\\nchangelog.d/zzz.md\\nCOMMIT partial2\\nchangelog.d/aaa.md\\n';;\n esac\n echo \"injected git $command read failure\" >&2\n exit 128\nfi\nexec " + shellQuote(git) + " \"$@\"\n"
	path := filepath.Join(bin, "git")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}, trace
}

func TestReleaseRefusesEachFailedGitRead(t *testing.T) {
	t.Parallel()
	baseline := rootAddFixture(t)
	env, trace := gitReadShim(t, 0, "")
	_, stderr, exit := runScriptEnv(t, baseline, env, "v1.0.0", "2026-10-02")
	if exit != 0 {
		t.Fatalf("read inventory release exit %d: %s", exit, stderr)
	}
	reads := strings.Fields(readFile(t, trace))
	// Independent expected inventory prevents a bypass/removal from shrinking
	// the denominator unnoticed. One root read, one status gate, three
	// CHANGELOG byte-gate reads (flag, worktree hash, HEAD hash), then per
	// fragment in discovery order: tracked/mode, HEAD mode, flag, worktree
	// hash, HEAD hash, committed blob; finally the add-order history.
	want := "rev-parse status ls-files hash-object rev-parse " +
		"ls-files ls-tree ls-files hash-object rev-parse cat-file " +
		"ls-files ls-tree ls-files hash-object rev-parse cat-file log"
	if strings.Join(reads, " ") != want {
		t.Fatalf("Git read inventory %v, want %s", reads, want)
	}
	for i, command := range reads {
		t.Run(fmt.Sprintf("read_%d_%s", i+1, command), func(t *testing.T) {
			dir := rootAddFixture(t)
			before := snapshot(t, dir)
			env, trace := gitReadShim(t, i+1, "")
			_, stderr, exit := runScriptEnv(t, dir, env, "v1.0.0", "2026-10-02")
			if exit != 1 || !strings.Contains(stderr, "git "+command) || !strings.Contains(stderr, "128") {
				t.Errorf("read %d (%s) exit %d; want command-named refusal of exit 128: %s", i+1, command, exit, stderr)
			}
			if got := len(strings.Fields(readFile(t, trace))); got != i+1 {
				t.Errorf("continued after failed read: %d calls, want %d", got, i+1)
			}
			before.assertUnchanged(t, dir, "failed git "+command)
		})
	}
	t.Run("ignored_branch_check_ignore", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, ".gitignore"), "changelog.d/ign.md\n")
		commitAll(t, dir, "ignore")
		writeFile(t, filepath.Join(dir, "changelog.d", "ign.md"), "- Ignored.\n")
		before := snapshot(t, dir)
		env, trace := gitReadShim(t, 7, "")
		_, stderr, exit := runScriptEnv(t, dir, env, "v1.0.0", "2026-10-02")
		if exit != 1 || !strings.Contains(stderr, "git check-ignore") || !strings.Contains(stderr, "128") {
			t.Errorf("check-ignore read exit %d: %s", exit, stderr)
		}
		if got := strings.Join(strings.Fields(readFile(t, trace)), " "); got != "rev-parse status ls-files hash-object rev-parse ls-files check-ignore" {
			t.Errorf("ignored read inventory: %s", got)
		}
		before.assertUnchanged(t, dir, "failed git check-ignore")
	})
}

// These are the full panel's explicit status/log shims. Status fails while an
// untracked dirty file is present; log fails on the clean, ordered fixture.
func TestReleaseRefusesPanelStatusAndLogFailures(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"status", "log"} {
		t.Run(command, func(t *testing.T) {
			dir := rootAddFixture(t)
			if command == "status" {
				writeFile(t, filepath.Join(dir, "dirty.txt"), "untracked\n")
			}
			before := snapshot(t, dir)
			env, _ := gitReadShim(t, 0, command)
			_, stderr, exit := runScriptEnv(t, dir, env, "v1.0.0", "2026-10-02")
			if exit != 1 || !strings.Contains(stderr, "git "+command) || !strings.Contains(stderr, "128") {
				t.Errorf("panel %s failure exit %d: %s", command, exit, stderr)
			}
			before.assertUnchanged(t, dir, "panel failed git "+command)
		})
	}
}

func TestReleaseRefusesUnknownAddHistory(t *testing.T) {
	t.Parallel()
	dir := rootAddFixture(t)
	before := snapshot(t, dir)
	env, trace := gitReadShim(t, 0, "log")
	shim := strings.TrimPrefix(env[0], "PATH=")
	shim = filepath.Join(strings.Split(shim, string(os.PathListSeparator))[0], "git")
	// Return a successful empty history read, not a failed read: the formatter
	// must also refuse unknown order rather than assign a filename sentinel.
	body := readFile(t, shim)
	body = strings.Replace(body, "printf 'COMMIT partial\\nchangelog.d/zzz.md\\nCOMMIT partial2\\nchangelog.d/aaa.md\\n';;", ":;;", 1)
	body = strings.Replace(body, "exit 128", "exit 0", 1)
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr, exit := runScriptEnv(t, dir, env, "v1.0.0", "2026-10-02")
	if exit != 1 || !strings.Contains(stderr, "no add history") {
		t.Errorf("unknown add history exit %d: %s", exit, stderr)
	}
	if len(strings.Fields(readFile(t, trace))) != 18 {
		t.Error("history gate was not reached")
	}
	before.assertUnchanged(t, dir, "successful read with unknown add history")
}

func TestCheckRefusesFailedRootRead(t *testing.T) {
	t.Parallel()
	dir := rootAddFixture(t)
	before := snapshot(t, dir)
	env, _ := gitReadShim(t, 1, "")
	_, stderr, exit := runScriptEnv(t, dir, env, "--check")
	if exit != 1 || !strings.Contains(stderr, "git rev-parse") || !strings.Contains(stderr, "128") {
		t.Errorf("check root read failure exit %d: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "check failed root read")
}

func TestReleaseRefusesMissingUnreleasedSection(t *testing.T) {
	t.Parallel()
	dir := rootAddFixture(t)
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n## v0.1.0 — 2026-01-01\n\n- Existing.\n")
	commitAll(t, dir, "no unreleased")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
	if exit != 1 || !strings.Contains(stderr, "no ## Unreleased") {
		t.Errorf("missing Unreleased exit %d: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "missing Unreleased")
}

// The full panel also carried these grammar probes. Keep their exact shapes
// as named regressions rather than inferring them from unrelated positives.
func TestCheckPanelGrammarProbes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, file, body string
		exit             int
	}{
		{"crlf", "crlf.md", "- CRLF.\r\n", 0},
		{"continuation", "c.md", "- Bullet\n  continuation\n", 0},
		{"blank", "b.md", "- One\n\n- Two\n", 0},
		{"uppercase", "Upper.md", "- Upper\n", 0},
		{"dot", "a.b.md", "- Dot\n", 1},
		{"symlink", "link.md", "", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := initRepo(t)
			path := filepath.Join(dir, "changelog.d", c.file)
			if c.name == "symlink" {
				writeFile(t, filepath.Join(dir, "target"), "- Linked\n")
				if err := os.Symlink("../target", path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, path, c.body)
			}
			before := snapshot(t, dir)
			_, stderr, exit := runScript(t, dir, "--check")
			if exit != c.exit {
				t.Errorf("panel grammar %s exit %d, want %d: %s", c.name, exit, c.exit, stderr)
			}
			// Symlink fragments refuse outright in both modes: target bytes
			// are not committed content (round-3 N2). The predecessor shape
			// (an admitted working-tree symlink) is kept as a refusal probe.
			if c.name == "dot" && !strings.Contains(stderr, "changelog.d/a.b.md:1: invalid fragment name") {
				t.Errorf("missing dot-name diagnostic: %s", stderr)
			}
			if c.name == "symlink" && !strings.Contains(stderr, "changelog.d/link.md:1: symlink") {
				t.Errorf("missing symlink diagnostic: %s", stderr)
			}
			before.assertUnchanged(t, dir, "panel grammar "+c.name)
		})
	}
}

// --- round-3 N1/N2 regressions ----------------------------------------------
// Named reproducers for the round-3 panel findings on CR rev 1 (verdict
// newline-fragment-discovery and symlink-uncommitted-content). Each drives
// the production entry point in a temporary git repository.

// newlineFixture reproduces the N1 panel fixture exactly: a valid a.md plus
// a second file whose literal name carries a trailing newline and whose
// body violates the grammar. Both are committed.
func newlineFixture(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- Real.\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md\n"), "# INVALID heading in invalid name\n")
	commitAll(t, dir, "newline fixture")
	return dir
}

func TestCheckRefusesNewlineFragmentName(t *testing.T) {
	t.Parallel()
	dir := newlineFixture(t)
	before := snapshot(t, dir)
	stdout, stderr, exit := runScript(t, dir, "--check")
	if exit != 1 {
		t.Fatalf("--check over an LF-bearing name exited %d, want 1\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, "invalid fragment name") {
		t.Errorf("stderr names no invalid fragment name: %q", stderr)
	}
	if strings.Contains(stdout, "fragment(s) valid") {
		t.Errorf("--check reported valid fragments for an LF-bearing name: %q", stdout)
	}
	before.assertUnchanged(t, dir, "LF-bearing fragment name")
}

func TestReleaseRefusesNewlineFragmentName(t *testing.T) {
	t.Parallel()
	dir := newlineFixture(t)
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.2.3", "2026-10-02")
	if exit != 1 {
		t.Fatalf("release over an LF-bearing name exited %d, want 1: %s", exit, stderr)
	}
	if !strings.Contains(stderr, "invalid fragment name") {
		t.Errorf("stderr names no invalid fragment name: %q", stderr)
	}
	before.assertUnchanged(t, dir, "LF-bearing fragment name release")
	for _, name := range []string{"a.md", "a.md\n"} {
		if _, err := os.Stat(filepath.Join(dir, "changelog.d", name)); err != nil {
			t.Errorf("fragment %q did not survive the refusal: %v", name, err)
		}
	}
}

func TestCheckRefusesTabFragmentName(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "a\tb.md"), "- Tab in name.\n")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "--check")
	if exit != 1 || !strings.Contains(stderr, "invalid fragment name") {
		t.Errorf("tab-name check exit %d, want invalid-name refusal: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "tab fragment name")
}

// symlinkIgnoredFixture reproduces the N2 panel fixtures: a committed
// symlink fragment pointing at an ignored, never-committed target whose
// content is then replaced. The link text is unchanged, so the tree stays
// clean while the target holds bytes git never saw.
func symlinkIgnoredFixture(t *testing.T, ignoreLine, target string) string {
	t.Helper()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, ".gitignore"), ignoreLine+"\n")
	writeFile(t, filepath.Join(dir, target), "- Original.\n")
	if err := os.Symlink("../"+target, filepath.Join(dir, "changelog.d", "link.md")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "symlink fixture")
	writeFile(t, filepath.Join(dir, target), "- UNCOMMITTED ignored target content.\n")
	return dir
}

func symlinkIgnoredCases() []struct{ name, ignore, target string } {
	return []struct{ name, ignore, target string }{
		{"panel1", "ignored-body.txt", "ignored-body.txt"},
		{"panel2", "/ignored-payload", "ignored-payload"},
	}
}

func TestCheckRefusesSymlinkFragment(t *testing.T) {
	t.Parallel()
	// The link name is exactly link.md in both fixtures: the narrowing
	// mutant admits that one name past the symlink gate, and this witness
	// must fail there.
	for _, c := range symlinkIgnoredCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := symlinkIgnoredFixture(t, c.ignore, c.target)
			before := snapshot(t, dir)
			_, stderr, exit := runScript(t, dir, "--check")
			if exit != 1 {
				t.Fatalf("--check admitted a symlink fragment, exit %d: %s", exit, stderr)
			}
			if !strings.Contains(stderr, "changelog.d/link.md:1: symlink") {
				t.Errorf("missing symlink file:line diagnostic: %q", stderr)
			}
			before.assertUnchanged(t, dir, "symlink --check "+c.name)
		})
	}
}

func TestReleaseRefusesSymlinkIgnoredTarget(t *testing.T) {
	t.Parallel()
	for _, c := range symlinkIgnoredCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := symlinkIgnoredFixture(t, c.ignore, c.target)
			// The bypass shape, asserted before the refusal: status is
			// clean while the target holds uncommitted bytes. Without the
			// symlink gate the release would publish exactly these bytes
			// and delete the link.
			if status := runGit(t, dir, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
				t.Fatalf("fixture is not clean: %q", status)
			}
			before := snapshot(t, dir)
			_, stderr, exit := runScript(t, dir, "v1.2.3", "2026-10-02")
			if exit != 1 {
				t.Fatalf("release admitted a symlink ignored-target, exit %d: %s", exit, stderr)
			}
			if !strings.Contains(stderr, "symlink") {
				t.Errorf("stderr names no symlink: %q", stderr)
			}
			before.assertUnchanged(t, dir, "symlink ignored-target release "+c.name)
			if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); strings.Contains(got, "UNCOMMITTED") {
				t.Error("release published uncommitted ignored target bytes")
			}
			link := filepath.Join(dir, "changelog.d", "link.md")
			if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
				t.Errorf("committed link was removed or replaced: %v", err)
			}
		})
	}
}

func TestReleaseRefusesSymlinkUnderNoSymlinksConfig(t *testing.T) {
	t.Parallel()
	// core.symlinks=false checks the committed link out as a regular file
	// holding the target text: the working-tree symlink gate cannot see it,
	// so the git mode 120000 gate must refuse. The tree is clean; only the
	// mode branch can fire.
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "target.txt"), "- Target.\n")
	if err := os.Symlink("../target.txt", filepath.Join(dir, "changelog.d", "link.md")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "symlink fixture")
	if err := os.Remove(filepath.Join(dir, "changelog.d", "link.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "changelog.d", "link.md"), "../target.txt")
	appendLocalConfig(t, dir, "[core]\n symlinks = false\n")
	if status := runGit(t, dir, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("fixture is not clean: %q", status)
	}
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
	if exit != 1 || !strings.Contains(stderr, "120000") {
		t.Errorf("no-symlinks release exit %d, want mode-120000 refusal: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "symlink under core.symlinks=false")
}

func TestCheckNestedCwdAndOutsideRepo(t *testing.T) {
	t.Parallel()
	dir := rootAddFixture(t)
	nested := filepath.Join(dir, "nested", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	stdout, stderr, exit := runScript(t, nested, "--check")
	if exit != 0 || !strings.Contains(stdout, "2 fragment(s) valid") {
		t.Errorf("nested check exit %d: %s %s", exit, stdout, stderr)
	}
	before.assertUnchanged(t, dir, "nested check under C locale")
	outside := t.TempDir()
	// Task-scoped TMPDIR may itself live under the candidate checkout. Bound
	// discovery at the fixture parent so this negative has no ancestor repo.
	_, stderr, exit = runScriptEnv(t, outside,
		[]string{"GIT_CEILING_DIRECTORIES=" + filepath.Dir(outside)}, "--check")
	if exit != 1 || !strings.Contains(stderr, "git rev-parse") {
		t.Errorf("outside repo check exit %d: %s", exit, stderr)
	}
}

// --- round-4 P1/P2 regressions -----------------------------------------------
// Named reproducers for the round-4 panel findings on CR rev 2 (verdict
// hidden-index-dirty-gate and symlink-git-mode-check). Each drives the
// production entry point in a temporary git repository. The suite never runs
// git config; local config is appended directly and index flags via
// update-index.

// hiddenEditFixture is the P1 panel shape: one committed fragment plus a
// CHANGELOG with carry-over, both committed and clean.
func hiddenEditFixture(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- Committed fragment.\n")
	commitAll(t, dir, "hidden fixture")
	return dir
}

func assertPorcelainClean(t *testing.T, dir, context string) {
	t.Helper()
	if status := runGit(t, dir, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("%s: fixture is not clean: %q", context, status)
	}
}

// TestReleaseRefusesHiddenFragmentEdits reproduces P1 (372n2g F1 steps 2-3,
// 30ij2i F1): assume-unchanged or skip-worktree hides an edited fragment
// from git status, but release must refuse and preserve the edit.
func TestReleaseRefusesHiddenFragmentEdits(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			dir := hiddenEditFixture(t)
			runGit(t, dir, "update-index", flag, "--", "changelog.d/a.md")
			writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- UNCOMMITTED fragment.\n")
			assertPorcelainClean(t, dir, "hidden fragment "+flag)
			before := snapshot(t, dir)
			_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
			if exit != 1 {
				t.Fatalf("release admitted hidden fragment edit (%s), exit %d: %s", flag, exit, stderr)
			}
			if !strings.Contains(stderr, "changelog.d/a.md") {
				t.Errorf("stderr names no fragment path: %q", stderr)
			}
			before.assertUnchanged(t, dir, "hidden fragment "+flag)
			if got := readFile(t, filepath.Join(dir, "changelog.d", "a.md")); !strings.Contains(got, "UNCOMMITTED") {
				t.Error("hidden fragment edit was deleted despite the refusal")
			}
			if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); strings.Contains(got, "## v1.0.0") {
				t.Error("refusing release wrote a version section")
			}
		})
	}
	t.Run("flag-without-edit-still-refuses", func(t *testing.T) {
		// The flag gate fires even when bytes currently match: hidden
		// state alone is unsafe. The byte mutant cannot kill here because
		// bytes match; only the flag gate refuses.
		dir := hiddenEditFixture(t)
		runGit(t, dir, "update-index", "--assume-unchanged", "--", "changelog.d/a.md")
		assertPorcelainClean(t, dir, "flag without edit")
		before := snapshot(t, dir)
		_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
		if exit != 1 || !strings.Contains(stderr, "hidden index state") {
			t.Errorf("flag-without-edit exit %d, want hidden-state refusal: %s", exit, stderr)
		}
		before.assertUnchanged(t, dir, "flag without edit")
	})
}

// TestReleaseRefusesHiddenChangelogEdits reproduces P1 (372n2g F1 steps 4-5):
// hidden CHANGELOG edits must refuse before the carry-over is published.
func TestReleaseRefusesHiddenChangelogEdits(t *testing.T) {
	t.Parallel()
	t.Run("assume-changelog", func(t *testing.T) {
		dir := hiddenEditFixture(t)
		runGit(t, dir, "update-index", "--assume-unchanged", "--", "CHANGELOG.md")
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n## Unreleased\n\n- UNCOMMITTED carry.\n")
		assertPorcelainClean(t, dir, "assume changelog")
		before := snapshot(t, dir)
		_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
		if exit != 1 {
			t.Fatalf("release admitted hidden CHANGELOG edit, exit %d: %s", exit, stderr)
		}
		if !strings.Contains(stderr, "CHANGELOG.md") {
			t.Errorf("stderr names no CHANGELOG path: %q", stderr)
		}
		before.assertUnchanged(t, dir, "assume changelog")
		if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); strings.Contains(got, "## v1.0.0") {
			t.Error("refusing release published hidden carry-over")
		}
	})
	t.Run("ignorestat-changelog", func(t *testing.T) {
		dir := hiddenEditFixture(t)
		appendLocalConfig(t, dir, "[core]\n ignorestat = true\n")
		runGit(t, dir, "update-index", "--really-refresh")
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n## Unreleased\n\n- UNCOMMITTED carry.\n")
		assertPorcelainClean(t, dir, "ignorestat changelog")
		before := snapshot(t, dir)
		_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
		if exit != 1 {
			t.Fatalf("release admitted ignorestat CHANGELOG edit, exit %d: %s", exit, stderr)
		}
		before.assertUnchanged(t, dir, "ignorestat changelog")
		if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); strings.Contains(got, "## v1.0.0") {
			t.Error("refusing release published hidden carry-over")
		}
	})
}

// TestReleaseRefusesHiddenFragmentIgnorestat reproduces the 30ij2i F1 local
// ignorestat variant for fragments: repository-local core.ignorestat hides
// an edited fragment, but the byte gate compares working-tree bytes directly.
func TestReleaseRefusesHiddenFragmentIgnorestat(t *testing.T) {
	t.Parallel()
	dir := hiddenEditFixture(t)
	appendLocalConfig(t, dir, "[core]\n ignorestat = true\n")
	runGit(t, dir, "update-index", "--really-refresh")
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- UNCOMMITTED fragment.\n")
	assertPorcelainClean(t, dir, "ignorestat fragment")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
	if exit != 1 {
		t.Fatalf("release admitted ignorestat fragment edit, exit %d: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "ignorestat fragment")
	if got := readFile(t, filepath.Join(dir, "changelog.d", "a.md")); !strings.Contains(got, "UNCOMMITTED") {
		t.Error("hidden fragment edit was deleted despite the refusal")
	}
}

// TestReleaseRefusesStagedChanges pins the B10 staged claim: staged but
// uncommitted edits refuse via the status gate before any byte comparison.
func TestReleaseRefusesStagedChanges(t *testing.T) {
	t.Parallel()
	dir := hiddenEditFixture(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- Staged edit.\n")
	runGit(t, dir, "add", "--", "changelog.d/a.md")
	if status := runGit(t, dir, "status", "--porcelain=v1", "--untracked-files=all"); status == "" {
		t.Fatal("staged fixture is unexpectedly clean")
	}
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
	if exit != 1 || !strings.Contains(stderr, "dirty tree") {
		t.Errorf("staged exit %d, want dirty-tree refusal: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "staged changes")
}

// TestCheckRefusesSymlinkModeUnderNoSymlinksConfig reproduces P2 (c1yxnp F1,
// 30ij2i F2): a committed mode-120000 fragment materialized as a regular
// file under core.symlinks=false must refuse in --check via git modes, not
// the filesystem. The link target is a valid bullet so the filesystem
// validator passes and only the mode gate can fire.
func TestCheckRefusesSymlinkModeUnderNoSymlinksConfig(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	if err := os.Symlink("- Accepted as a bullet.\n", filepath.Join(dir, "changelog.d", "link.md")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "symlink fixture")
	appendLocalConfig(t, dir, "[core]\n symlinks = false\n")
	if err := os.Remove(filepath.Join(dir, "changelog.d", "link.md")); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "checkout-index", "-f", "--", "changelog.d/link.md")
	if st, err := os.Lstat(filepath.Join(dir, "changelog.d", "link.md")); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("fixture did not materialize a regular file: %v", err)
	}
	if mode := runGit(t, dir, "ls-files", "-s", "--", "changelog.d/link.md"); !strings.HasPrefix(mode, "120000") {
		t.Fatalf("fixture lost mode 120000: %q", mode)
	}
	assertPorcelainClean(t, dir, "symlink-less check")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "--check")
	if exit != 1 || !strings.Contains(stderr, "changelog.d/link.md:1:") || !strings.Contains(stderr, "120000") {
		t.Errorf("symlink-less check exit %d, want file:line mode-120000 refusal: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "symlink-less check")
}

// TestCheckRefusesHeadOnlySymlinkMode covers the HEAD branch of the P2 gate:
// the index holds a regular file (staged conversion) while HEAD still
// carries mode 120000. --check has no dirty gate, so only the HEAD mode
// read can refuse.
func TestCheckRefusesHeadOnlySymlinkMode(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	if err := os.Symlink("- Valid body.\n", filepath.Join(dir, "changelog.d", "link.md")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "symlink fixture")
	if err := os.Remove(filepath.Join(dir, "changelog.d", "link.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "changelog.d", "link.md"), "- Valid body.\n")
	runGit(t, dir, "add", "--", "changelog.d/link.md")
	if mode := runGit(t, dir, "ls-files", "-s", "--", "changelog.d/link.md"); strings.HasPrefix(mode, "120000") {
		t.Fatalf("index still carries 120000: %q", mode)
	}
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "--check")
	if exit != 1 || !strings.Contains(stderr, "120000") {
		t.Errorf("HEAD-only check exit %d, want mode-120000 refusal: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "HEAD-only symlink check")
}

// TestCheckRefusesSubdirectoryFragment and TestReleaseRefusesSubdirectoryFragment
// pin the B11 subdir claim: a nested directory under changelog.d is visited
// as one entry and refuses in both modes.
func TestCheckRefusesSubdirectoryFragment(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- Valid.\n")
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d", "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "changelog.d", "subdir", "b.md"), "- Nested.\n")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "--check")
	if exit != 1 || !strings.Contains(stderr, "changelog.d/subdir:1:") {
		t.Errorf("subdir check exit %d, want file:line refusal: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "subdir check")
}

func TestReleaseRefusesSubdirectoryFragment(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "changelog.d", "a.md"), "- Valid.\n")
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d", "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "changelog.d", "subdir", "b.md"), "- Nested.\n")
	commitAll(t, dir, "subdir fixture")
	assertPorcelainClean(t, dir, "subdir release")
	before := snapshot(t, dir)
	_, stderr, exit := runScript(t, dir, "v1.0.0", "2026-10-02")
	if exit != 1 {
		t.Fatalf("release admitted a subdirectory fragment, exit %d: %s", exit, stderr)
	}
	before.assertUnchanged(t, dir, "subdir release")
	if _, err := os.Stat(filepath.Join(dir, "changelog.d", "subdir", "b.md")); err != nil {
		t.Errorf("subdir content did not survive the refusal: %v", err)
	}
}
