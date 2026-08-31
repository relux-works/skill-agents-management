package codex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Binary resolution is hermetic here in the strict sense: every test builds its
// own layout under a temp directory and hands the plugin an environment it
// constructed. Nothing reads the developer's PATH, nothing calls t.Setenv, and
// nothing depends on codex being installed — which is why these run in parallel
// and produce the same answer on a CI machine with no providers on it.
//
// The three resolution paths are covered twice over: once through
// agentic.BuildPlan in parity_test.go, against the source's own goldens, and
// once here where the ORDER between them can be attacked directly. The parity
// goldens prove each path lands where the source landed; these prove the paths
// beat each other in the right order and that each one's preconditions are
// really required.

// stubLayout is one on-disk arrangement plus the launch environment that sees
// it.
type stubLayout struct {
	binDir string
	env    []string
}

func newStubLayout(t *testing.T) *stubLayout {
	t.Helper()
	binDir := tempSlot(t)
	return &stubLayout{binDir: binDir, env: []string{"PATH=" + binDir}}
}

func (l *stubLayout) withManagedRoot(root string) *stubLayout {
	l.env = append(l.env, managedPackageRootEnv+"="+root)
	return l
}

func TestResolveBinaryPrefersTheManagedPackageOverPath(t *testing.T) {
	t.Parallel()
	layout := newStubLayout(t)
	onPath := writeStubExecutable(t, layout.binDir, executableName)
	packageRoot := tempSlot(t)
	managed := writeManagedPackage(t, packageRoot)
	layout.withManagedRoot(packageRoot)

	got, err := resolveBinary(layout.env)
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != managed {
		t.Errorf("resolveBinary = %q, want the managed package binary %q", got, managed)
	}
	if got == onPath {
		t.Errorf("resolveBinary took the bare PATH entry while a managed package was installed; a managed install exists precisely so the child runs the native binary the parent's package pins")
	}
}

// TestAManagedRootWithoutTheBinaryFallsThroughToPath is the negative that
// makes the preference above mean something.
//
// The root is set and the layout is EMPTY, which is the state of a machine
// whose managed package was removed or half-installed. Resolution must fall
// through rather than return a path to a file that does not exist: the caller
// would exec it and get "no such file", with the launch reporting a binary it
// never checked.
func TestAManagedRootWithoutTheBinaryFallsThroughToPath(t *testing.T) {
	t.Parallel()
	layout := newStubLayout(t)
	onPath := writeStubExecutable(t, layout.binDir, executableName)
	layout.withManagedRoot(tempSlot(t))

	got, err := resolveBinary(layout.env)
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != onPath {
		t.Errorf("resolveBinary = %q, want the PATH entry %q: a managed root naming a layout that is not installed is not a resolution", got, onPath)
	}
}

// TestAManagedRootPointingAtADirectoryIsNotAResolution narrows the stat check
// from "something exists there" to "a FILE exists there".
//
// A directory named codex at the candidate path satisfies os.Stat and cannot be
// executed. Reading existence as resolvability is the shape of the bug the
// source's own dry-run placeholder had: a path reported as the launch target
// that the launch could never use.
func TestAManagedRootPointingAtADirectoryIsNotAResolution(t *testing.T) {
	t.Parallel()
	layout := newStubLayout(t)
	onPath := writeStubExecutable(t, layout.binDir, executableName)
	packageRoot := tempSlot(t)
	platformPackage, targetTriple, ok := platformPackageForHost()
	if !ok {
		t.Skipf("no known codex platform package for %s/%s", hostOS(), hostArch())
	}
	asDirectory := filepath.Join(packageRoot, "node_modules", platformPackage, "vendor", targetTriple, "bin", executableName)
	if err := os.MkdirAll(asDirectory, 0o755); err != nil {
		t.Fatalf("creating the directory-shaped candidate: %v", err)
	}
	layout.withManagedRoot(packageRoot)

	got, err := resolveBinary(layout.env)
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got == asDirectory {
		t.Fatalf("resolveBinary returned a DIRECTORY as the launch binary; the caller would exec a path that cannot be executed")
	}
	if got != onPath {
		t.Errorf("resolveBinary = %q, want the PATH entry %q", got, onPath)
	}
}

// TestManagedBinaryPathTriesBothLayouts covers the second candidate — the root
// AS the platform package rather than as its npm parent — which is the layout
// nativeBinaryFromShim reaches through and which no golden distinguishes from
// the first on its own.
func TestManagedBinaryPathTriesBothLayouts(t *testing.T) {
	t.Parallel()
	_, targetTriple, ok := platformPackageForHost()
	if !ok {
		t.Skipf("no known codex target triple for %s/%s", hostOS(), hostArch())
	}
	packageRoot := tempSlot(t)
	dir := filepath.Join(packageRoot, "vendor", targetTriple, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the package-root layout: %v", err)
	}
	want := writeStubExecutable(t, dir, executableName)

	if got := managedBinaryPath(packageRoot); got != want {
		t.Errorf("managedBinaryPath = %q, want the second candidate %q", got, want)
	}
}

// TestAnUninstalledManagedRootStillNamesTheExpectedLayout pins the source's
// return-the-first-candidate behaviour, which reads as a bug until the caller
// is read alongside it.
func TestAnUninstalledManagedRootStillNamesTheExpectedLayout(t *testing.T) {
	t.Parallel()
	platformPackage, targetTriple, ok := platformPackageForHost()
	if !ok {
		t.Skipf("no known codex platform package for %s/%s", hostOS(), hostArch())
	}
	root := tempSlot(t)
	want := filepath.Join(root, "node_modules", platformPackage, "vendor", targetTriple, "bin", executableName)
	if got := managedBinaryPath(root); got != want {
		t.Errorf("managedBinaryPath = %q, want the first candidate %q; a root that names no installed binary must still say which layout was expected", got, want)
	}
	if isRegularFile(want) {
		t.Fatal("the candidate exists, so this test is not measuring the uninstalled case")
	}
}

func TestAnEmptyManagedRootResolvesNothing(t *testing.T) {
	t.Parallel()
	for _, root := range []string{"", "   "} {
		if got := managedBinaryPath(root); got != "" {
			t.Errorf("managedBinaryPath(%q) = %q, want no path at all: an unset root is an absence, not a layout under the current directory", root, got)
		}
	}
}

func TestResolveBinaryUnwrapsTheNPMShim(t *testing.T) {
	t.Parallel()
	layout := newStubLayout(t)
	native := writeNativeShim(t, layout.binDir)

	got, err := resolveBinary(layout.env)
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != native {
		t.Errorf("resolveBinary = %q, want the native binary behind the shim %q", got, native)
	}
}

// TestAShimWithoutItsNativeBinaryResolvesToTheShim is the shim path's
// negative: the unwrapping must be confirmed on disk, not inferred from the
// layout's shape.
//
// An npm install whose platform package failed to fetch leaves exactly this
// state — a shim on PATH with no native binary beside it — and the child must
// then run the shim, which works, rather than a native path that is not there.
func TestAShimWithoutItsNativeBinaryResolvesToTheShim(t *testing.T) {
	t.Parallel()
	layout := newStubLayout(t)
	native := writeNativeShim(t, layout.binDir)
	if err := os.Remove(native); err != nil {
		t.Fatalf("removing the native binary: %v", err)
	}

	got, err := resolveBinary(layout.env)
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	want := filepath.Join(layout.binDir, executableName)
	if got != want {
		t.Errorf("resolveBinary = %q, want the PATH entry %q", got, want)
	}
}

// TestNativeBinaryFromShimRequiresBothShapeChecks narrows the unwrapper one
// condition at a time. Each case is a real layout that must NOT be treated as
// a shim, and admitting any of them rewrites an ordinary PATH binary into a
// package path that does not exist.
func TestNativeBinaryFromShimRequiresBothShapeChecks(t *testing.T) {
	t.Parallel()
	root := tempSlot(t)
	cases := []struct {
		name     string
		why      string
		build    func(t *testing.T) string
		wantNone bool
	}{
		{
			name:     "a plain executable on PATH is not a shim",
			why:      "the file is named codex, not codex.js; this is the ordinary PATH resolution path and must be left alone",
			wantNone: true,
			build: func(t *testing.T) string {
				dir := filepath.Join(root, "plain", "bin")
				mkdirAll(t, dir)
				return writeStubExecutable(t, dir, executableName)
			},
		},
		{
			name:     "a codex.js outside a bin directory is not a shim",
			why:      "npm installs the wrapper into the package's bin/; a codex.js somewhere else says nothing about a package root above it",
			wantNone: true,
			build: func(t *testing.T) string {
				dir := filepath.Join(root, "loose", "lib")
				mkdirAll(t, dir)
				return writeStubExecutable(t, dir, shimFileName)
			},
		},
		{
			name: "a codex.js under bin/ is a shim",
			why:  "the positive case, so the two refusals above are not passing for want of a working shape",
			build: func(t *testing.T) string {
				dir := filepath.Join(root, "shim", "bin")
				mkdirAll(t, dir)
				return writeStubExecutable(t, dir, shimFileName)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nativeBinaryFromShim(c.build(t))
			if c.wantNone && got != "" {
				t.Errorf("nativeBinaryFromShim resolved %q, want nothing: %s", got, c.why)
			}
			if !c.wantNone && got == "" {
				t.Errorf("nativeBinaryFromShim resolved nothing: %s", c.why)
			}
		})
	}
}

func TestAnEmptyShimPathResolvesNothing(t *testing.T) {
	t.Parallel()
	if got := nativeBinaryFromShim("   "); got != "" {
		t.Errorf("nativeBinaryFromShim(blank) = %q, want nothing", got)
	}
}

// TestResolveBinaryReadsTheSuppliedEnvironmentAndNotTheProcess is the reshape's
// own evidence.
//
// The plugin is handed an environment whose PATH points at a stub layout, and
// the answer must come from THERE. If resolution ever falls back on the
// process's PATH, this test resolves whatever the developer happens to have
// installed — or the ambient codex — and the failure names the reason.
func TestResolveBinaryReadsTheSuppliedEnvironmentAndNotTheProcess(t *testing.T) {
	t.Parallel()
	layout := newStubLayout(t)
	want := writeStubExecutable(t, layout.binDir, executableName)

	got, err := resolveBinary(layout.env)
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != want {
		t.Fatalf("resolveBinary = %q, want the stub at %q; resolution read something other than the launch environment", got, want)
	}
	if strings.HasPrefix(got, "/usr/") || strings.HasPrefix(got, "/opt/") {
		t.Fatalf("resolveBinary landed on %q, which is an installed binary rather than this test's stub", got)
	}
}

// TestAnAbsentPathIsNotAMissingBinary holds the distinction the whole
// resolution family rests on: an absence and a failure to find are different
// facts, and a caller that curated an environment without PATH is not told its
// codex install is missing.
func TestAnAbsentPathIsNotAMissingBinary(t *testing.T) {
	t.Parallel()
	_, err := resolveBinary([]string{"HOME=/nowhere"})
	if !errors.Is(err, ErrNoPathInLaunchEnvironment) {
		t.Errorf("resolveBinary with no PATH returned %v, want %v", err, ErrNoPathInLaunchEnvironment)
	}

	_, emptyErr := resolveBinary([]string{"PATH="})
	if emptyErr == nil {
		t.Fatal("resolveBinary with an empty PATH found a binary")
	}
	if errors.Is(emptyErr, ErrNoPathInLaunchEnvironment) {
		t.Error("an EMPTY PATH was reported as an ABSENT one; the caller asked to resolve against nothing and got told it supplied no environment")
	}
}

// TestALookPathCandidateMustBeExecutable narrows PATH resolution: a
// non-executable file with the right name is not a resolution, or a directory
// full of source files could shadow the real binary.
func TestALookPathCandidateMustBeExecutable(t *testing.T) {
	t.Parallel()
	shadow := tempSlot(t)
	if err := os.WriteFile(filepath.Join(shadow, executableName), []byte("not executable\n"), 0o644); err != nil {
		t.Fatalf("writing the non-executable candidate: %v", err)
	}
	installed := tempSlot(t)
	want := writeStubExecutable(t, installed, executableName)

	got, err := resolveBinary([]string{"PATH=" + shadow + string(os.PathListSeparator) + installed})
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != want {
		t.Errorf("resolveBinary = %q, want %q: a non-executable file with the right name shadowed the real binary", got, want)
	}
}

// TestResolveBinaryIsTheSameAnswerForEveryLaunchMode is the contract the
// source's own bug broke: its dry-run path reported a hardcoded display string
// while the real launch resolved a managed-npm path, and nothing compared them.
func TestResolveBinaryIsTheSameAnswerForEveryLaunchMode(t *testing.T) {
	t.Parallel()
	layout := newStubLayout(t)
	writeStubExecutable(t, layout.binDir, executableName)
	packageRoot := tempSlot(t)
	managed := writeManagedPackage(t, packageRoot)
	layout.withManagedRoot(packageRoot)

	workDir := tempSlot(t)
	req := parityRequest(workDir)
	req.Env = layout.env
	req.PromptPath = writePromptFile(t, workDir, "resolution parity prompt")

	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeManagedSession} {
		plan := buildParityPlan(t, New(), req, mode)
		if plan.Binary != managed {
			t.Errorf("%s resolved %q, want the same managed binary %q every other mode resolves; a display placeholder that drifts from the launch target is the source's own historical bug", mode, plan.Binary, managed)
		}
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
}
