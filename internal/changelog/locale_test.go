package changelog_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the production script with /bin/bash explicitly: on macOS this
// is bash 3.2, whose UTF-8 collation makes [a-z] include the tracked tag H.
func utf8ReleaseEnv(t *testing.T) []string {
	t.Helper()
	locale := "en_US.UTF-8"
	if runtime.GOOS != "darwin" {
		locale = "C.UTF-8"
	}
	env := changelogSetEnv(fixtureEnv(t), "LC_ALL", locale)
	stdout, stderr, exit, err := runChangelogChild("", env, 0, "locale", "charmap")
	if err != nil || exit != 0 || !strings.EqualFold(strings.TrimSpace(stdout), "UTF-8") {
		t.Fatalf("UTF-8 locale %s unavailable: exit %d err %v stdout %q stderr %q", locale, exit, err, stdout, stderr)
	}
	t.Logf("production release under /bin/bash, LC_ALL=%s", locale)
	return env
}

func runUTF8Release(t *testing.T, dir string) (string, string, int) {
	t.Helper()
	stdout, stderr, exit, err := runChangelogChild(dir, utf8ReleaseEnv(t), 0,
		"/bin/bash", scriptPath(t), "v1.0.0", "2026-10-07")
	if err != nil {
		t.Fatal(err)
	}
	return stdout, stderr, exit
}

func TestReleaseUTF8LocaleAcceptsNormalTrackedFiles(t *testing.T) {
	dir := hiddenEditFixture(t)
	for _, path := range []string{"CHANGELOG.md", "changelog.d/a.md"} {
		if tag := runGit(t, dir, "ls-files", "-v", "--", path); !strings.HasPrefix(tag, "H ") {
			t.Fatalf("normal tracked fixture %s has unexpected tag %q", path, tag)
		}
	}
	_, stderr, exit := runUTF8Release(t, dir)
	if exit != 0 {
		t.Fatalf("UTF-8 release refused normal tracked files: exit %d: %s", exit, stderr)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "CHANGELOG.md")), "## v1.0.0") {
		t.Fatal("release did not write version section")
	}
	if _, err := os.Stat(filepath.Join(dir, "changelog.d/a.md")); !os.IsNotExist(err) {
		t.Fatalf("release did not consume fragment: %v", err)
	}
}

func assertUTF8ReleaseRefusesFlag(t *testing.T, flag, tag string) {
	t.Helper()
	for _, path := range []string{"CHANGELOG.md", "changelog.d/a.md"} {
		t.Run(path, func(t *testing.T) {
			dir := hiddenEditFixture(t)
			runGit(t, dir, "update-index", flag, "--", path)
			if got := runGit(t, dir, "ls-files", "-v", "--", path); !strings.HasPrefix(got, tag+" ") {
				t.Fatalf("real index flag %s has unexpected tag %q", flag, got)
			}
			// Leave bytes unchanged: only the flag gate can refuse this member.
			assertPorcelainClean(t, dir, flag)
			before := snapshot(t, dir)
			_, stderr, exit := runUTF8Release(t, dir)
			if exit != 1 || !strings.Contains(stderr, path+" carries hidden index state ("+tag+":") {
				t.Fatalf("UTF-8 release admitted %s on %s or refused wrong member: exit %d: %s", flag, path, exit, stderr)
			}
			before.assertUnchanged(t, dir, flag)
		})
	}
}

func TestReleaseUTF8LocaleRefusesAssumeUnchanged(t *testing.T) {
	assertUTF8ReleaseRefusesFlag(t, "--assume-unchanged", "h")
}

func TestReleaseUTF8LocaleRefusesSkipWorktree(t *testing.T) {
	assertUTF8ReleaseRefusesFlag(t, "--skip-worktree", "S")
}
