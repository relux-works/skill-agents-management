package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These tests drive the production entry point end to end: `make build`
// produces the binary main() lives in, and the binary is executed. Nothing
// here calls an in-process helper, because the property under test — that the
// Makefile's -X targets actually reach the variables in this package — cannot
// be observed from inside a `go test` binary, which is not built with those
// ldflags. A wrong -X path is silently ignored by the Go linker, so only the
// output of a real build can catch the drift.

const (
	probeVersion   = "9.9.9-ldflags-probe"
	probeCommit    = "c0ffee1"
	probeBuildDate = "2026-01-02T03:04:05Z"
)

var testTempDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agents-management-test")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating test temp dir: %v\n", err)
		os.Exit(1)
	}
	testTempDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	start := dir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found at or above %s", start)
		}
		dir = parent
	}
}

var (
	probeBinOnce sync.Once
	probeBinPath string
	probeBinErr  error
)

// probeBinary builds the CLI through the real `make build` target with known
// version metadata, so the assertions bind to the Makefile the repository
// actually ships rather than to a duplicated ldflags string.
func probeBinary(t *testing.T) string {
	t.Helper()
	probeBinOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			probeBinErr = err
			return
		}
		bin := filepath.Join(testTempDir, "agents-management-probe")
		build := exec.Command("make", "build",
			"BIN="+bin,
			"VERSION="+probeVersion,
			"COMMIT="+probeCommit,
			"BUILD_DATE="+probeBuildDate,
		)
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			probeBinErr = fmt.Errorf("make build failed: %v\n%s", err, out)
			return
		}
		probeBinPath = bin
	})
	if probeBinErr != nil {
		t.Fatalf("building probe binary: %v", probeBinErr)
	}
	return probeBinPath
}

func run(t *testing.T, bin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var out, errOut bytes.Buffer
	c := exec.Command(bin, args...)
	c.Stdout = &out
	c.Stderr = &errOut
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %s %v: %v", bin, args, err)
		}
		exitCode = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), exitCode
}

// The gate: `make build` must inject version, commit and build date into the
// binary. If the Makefile drops the -ldflags, or an -X target names a package
// or variable that no longer exists, the binary falls back to its defaults and
// this test goes red.
func TestMakeBuildInjectsVersionMetadata(t *testing.T) {
	bin := probeBinary(t)

	stdout, stderr, code := run(t, bin, "version")
	if code != 0 {
		t.Fatalf("version exited %d, stderr: %s", code, stderr)
	}
	want := fmt.Sprintf("agents-management version %s (commit %s, built %s)\n",
		probeVersion, probeCommit, probeBuildDate)
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	for _, sentinel := range []string{probeVersion, probeCommit, probeBuildDate} {
		if !strings.Contains(stdout, sentinel) {
			t.Errorf("stdout %q is missing injected value %q: ldflags did not reach the binary", stdout, sentinel)
		}
	}
}

// Cobra's own --version flag must report the same string as the subcommand;
// they are two entry points onto one fact.
func TestMakeBuildInjectsVersionMetadataIntoVersionFlag(t *testing.T) {
	bin := probeBinary(t)

	stdout, stderr, code := run(t, bin, "--version")
	if code != 0 {
		t.Fatalf("--version exited %d, stderr: %s", code, stderr)
	}
	want := fmt.Sprintf("agents-management version %s (commit %s, built %s)\n",
		probeVersion, probeCommit, probeBuildDate)
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// The narrowing partner of TestMakeBuildInjectsVersionMetadata: a build with
// no ldflags must report the compiled-in defaults. Without this, hardcoding
// the sentinel values into the source would satisfy the injection test.
func TestBuildWithoutLdflagsReportsDefaults(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locating repo root: %v", err)
	}
	bin := filepath.Join(testTempDir, "agents-management-plain")
	build := exec.Command("go", "build", "-mod=mod", "-o", bin, "./tools/agents-management")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	stdout, stderr, code := run(t, bin, "version")
	if code != 0 {
		t.Fatalf("version exited %d, stderr: %s", code, stderr)
	}
	if stdout != "agents-management version dev\n" {
		t.Errorf("stdout = %q, want %q", stdout, "agents-management version dev\n")
	}
	for _, sentinel := range []string{probeVersion, probeCommit, probeBuildDate} {
		if strings.Contains(stdout, sentinel) {
			t.Errorf("stdout %q carries %q without ldflags: the value is hardcoded, not injected", stdout, sentinel)
		}
	}
}

// The gate: the shipped binary answers `plugins` with an empty list and exit
// 0. If the empty case starts erroring — or starts writing a complaint to
// stderr that a consumer would read as a failure — this goes red.
func TestBuiltBinaryListsEmptyPluginsWithoutError(t *testing.T) {
	bin := probeBinary(t)

	stdout, stderr, code := run(t, bin, "plugins")
	if code != 0 {
		t.Errorf("plugins exited %d, want 0; stderr: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty: an empty registry is an answer, not a diagnostic", stderr)
	}
}

func TestBuiltBinaryEncodesEmptyPluginsAsEmptyArray(t *testing.T) {
	bin := probeBinary(t)

	stdout, stderr, code := run(t, bin, "plugins", "--json")
	if code != 0 {
		t.Errorf("plugins --json exited %d, want 0; stderr: %s", code, stderr)
	}
	if stdout != "[]\n" {
		t.Errorf("stdout = %q, want %q", stdout, "[]\n")
	}
}

// An unknown subcommand must still fail: exit 0 on `plugins` has to mean the
// command ran, not that this binary exits 0 for everything.
func TestBuiltBinaryFailsOnUnknownCommand(t *testing.T) {
	bin := probeBinary(t)

	_, _, code := run(t, bin, "definitely-not-a-command")
	if code == 0 {
		t.Error("unknown command exited 0, want non-zero")
	}
}

// gitIgnored reports whether the .gitignore rules ignore path, relative to the
// repo root — regardless of whether git already tracks it.
func gitIgnored(t *testing.T, root, path string) bool {
	t.Helper()
	// --no-index is load-bearing; do not helpfully simplify it away. Without
	// it, check-ignore consults the index first and never reports a TRACKED
	// file as ignored, whatever .gitignore says. The source paths asserted
	// below are untracked only while a Change Request is in flight; the moment
	// it lands they become tracked, the not-ignored half of the assertion goes
	// unconditionally true, and this guard expires at exactly the moment it
	// starts mattering. --no-index reports the pattern truth either way, which
	// is also what lets us assert paths that do not exist yet.
	c := exec.Command("git", "check-ignore", "-q", "--no-index", "--", path)
	c.Dir = root
	err := c.Run()
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false
	}
	t.Fatalf("git check-ignore %s: %v", path, err)
	return false
}

// A .gitignore pattern with no slash matches directories as well as files, so
// an unanchored `agents-management` entry — meant for the built binary —
// silently swallows the whole tools/agents-management source tree. Nothing
// else notices: the build and the tests keep passing against files git will
// never carry, and the change lands empty. Both directions are asserted, so
// deleting the ignore rules cannot make this pass.
func TestBuildOutputIsIgnoredAndSourcesAreNot(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locating repo root: %v", err)
	}

	// The last two do not exist yet, and that is deliberate: the class under
	// test is a new file being swallowed by a pattern nobody re-read, and a
	// list of paths that already exist can never observe it. pkg/ is the one
	// that bites hardest — a future package directory named after the binary
	// is the same unanchored-pattern bug waiting to happen.
	notIgnored := []string{
		"Makefile",
		"go.mod",
		"tools/agents-management/main.go",
		"tools/agents-management/cmd/root.go",
		"tools/agents-management/cmd/version.go",
		"tools/agents-management/cmd/plugins.go",
		"tools/agents-management/cmd/future_command.go",
		"pkg/agents-management/foo.go",
	}
	for _, path := range notIgnored {
		if gitIgnored(t, root, path) {
			t.Errorf("%s is git-ignored; it would never reach a commit", path)
		}
	}

	ignored := []string{
		"agents-management",
		"tools/agents-management/agents-management",
	}
	for _, path := range ignored {
		if !gitIgnored(t, root, path) {
			t.Errorf("%s is not git-ignored; the build output would be committed", path)
		}
	}
}
