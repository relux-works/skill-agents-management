package codex

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/launchenv"
	"github.com/relux-works/skill-agents-management/internal/runtimeenv"
)

// Codex has THREE binary-resolution paths and they are tried in this order:
//
//  1. the managed npm package named by CODEX_MANAGED_PACKAGE_ROOT, when the
//     platform binary it should contain actually exists on disk;
//  2. whatever `codex` resolves to on PATH — and if that is the npm shim
//     (a codex.js under a bin/ directory), the native binary reachable beside
//     it, again only when that file exists;
//  3. the PATH entry itself.
//
// The order is the source's (resolveCodexBinary, spawn.go) and it is not
// arbitrary: the shim is a node wrapper, so a launcher that execs it pays a
// node startup and loses the process identity the native binary would have
// had. Every step that *could* resolve is confirmed with a stat before it is
// returned, so a layout that is named but not installed falls through to the
// next path rather than producing a path to nothing.
//
// The whole family reads the environment it was HANDED. The source read the
// ambient process environment (os.Getenv, exec.LookPath), which is the one
// reshape here, named on lookPath below.

// managedPackageRootEnv names the npm package root a managed codex install
// records. It is also stripped from every child environment (see env.go): a
// child that inherited the parent's package root would resolve the parent's
// install rather than its own.
//
// The literal is internal/runtimeenv's, because that package is where the
// strip list lives and this is the one key BOTH halves of the plugin read.
// Spelling it here as well would be two spellings of one variable name, and
// the day one of them changed the filter and the resolver would disagree about
// which install a child is pointed at.
const managedPackageRootEnv = runtimeenv.ManagedPackageRootEnv

// executableName is the codex program name, spelled once. On Windows the
// managed layouts carry the .exe suffix; PATH lookup uses the bare name
// because that is what the shim is installed as.
const executableName = "codex"

// shimFileName is the npm wrapper script a PATH entry may point at. Unwrapping
// it is what path 2 above does.
const shimFileName = executableName + ".js"

// ErrNoPathInLaunchEnvironment is returned when the launch environment carries
// no PATH at all.
//
// It is a REFUSAL rather than a fallback onto the ambient process PATH. A
// caller that curated an environment without PATH did not ask this plugin to
// substitute its own, and resolving through a PATH the child will never see is
// how a launcher reports a binary the child cannot run.
//
// The sentinel is internal/launchenv's, re-exported under this plugin's own
// name so a caller holding only this package can match on it. It is an alias
// rather than a wrapper on purpose: two sentinels for one condition is two
// things a caller has to know about, and errors.Is would answer differently
// depending on which one a future edit returned.
var ErrNoPathInLaunchEnvironment = launchenv.ErrNoPath

// resolveBinary returns the exact executable a launch will run.
//
// env is the launch environment — LaunchRequest.Env — not the ambient process
// environment. That is the port's one behavioural reshape in this file, and it
// is required rather than preferred: the System contract states that a plugin
// reaches for no ambient state and that a dry run must report the same target
// a real launch would use, and both are unprovable while resolution reads a
// process-global. It also makes every resolution path testable in parallel
// against a stub layout, which is how the three paths below are covered.
func resolveBinary(env []string) (string, error) {
	if managed := managedBinaryPath(envValue(env, managedPackageRootEnv)); managed != "" {
		if isRegularFile(managed) {
			return managed, nil
		}
	}

	binary, err := lookPath(env, executableName)
	if err != nil {
		return "", err
	}
	if native := nativeBinaryFromShim(binary); native != "" {
		if isRegularFile(native) {
			return native, nil
		}
	}
	return binary, nil
}

// managedBinaryPath returns the platform binary inside a managed npm package
// root, or "" when the root is unset or this platform has no known package.
//
// Two candidate layouts are tried, in the source's order: the root as an npm
// PARENT directory (its node_modules holds the platform package), and the root
// as the platform package itself. The first is what a `npm i -g @openai/codex`
// install produces; the second is what a shim's own package root looks like,
// which is why nativeBinaryFromShim can reuse this function instead of
// spelling a third layout.
//
// When NEITHER candidate exists it returns the FIRST candidate rather than "".
// That is deliberate and it is the source's: the caller stats the result and
// falls through on failure, so returning a path preserves the ability to name
// the layout that was expected in a later error, while returning "" would
// discard it. Nothing here treats a returned path as evidence the file exists.
func managedBinaryPath(packageRoot string) string {
	packageRoot = strings.TrimSpace(packageRoot)
	if packageRoot == "" {
		return ""
	}

	platformPackage, targetTriple, ok := platformPackageForHost()
	if !ok {
		return ""
	}

	exe := executableName
	if runtime.GOOS == "windows" {
		exe = executableName + ".exe"
	}

	candidates := []string{
		filepath.Join(packageRoot, "node_modules", platformPackage, "vendor", targetTriple, "bin", exe),
		filepath.Join(packageRoot, "vendor", targetTriple, "bin", exe),
	}
	for _, candidate := range candidates {
		if isRegularFile(candidate) {
			return candidate
		}
	}
	return candidates[0]
}

// platformPackageForHost names the npm platform package and the rust target triple
// for the running GOOS/GOARCH, reporting ok=false for a platform codex
// publishes no managed package for.
//
// A platform that is not listed is an ABSENCE, reported as one: the managed
// path is simply unavailable and resolution falls through to PATH. Guessing a
// triple would produce a path that cannot exist and an error naming a layout
// nobody ships.
func platformPackageForHost() (platformPackage, targetTriple string, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "@openai/codex-darwin-x64", "x86_64-apple-darwin", true
		case "arm64":
			return "@openai/codex-darwin-arm64", "aarch64-apple-darwin", true
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "@openai/codex-linux-x64", "x86_64-unknown-linux-musl", true
		case "arm64":
			return "@openai/codex-linux-arm64", "aarch64-unknown-linux-musl", true
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "@openai/codex-win32-x64", "x86_64-pc-windows-msvc", true
		case "arm64":
			return "@openai/codex-win32-arm64", "aarch64-pc-windows-msvc", true
		}
	}
	return "", "", false
}

// nativeBinaryFromShim maps an npm shim on PATH to the native binary beside
// it, or "" when the path is not a shim.
//
// The shape it requires is exact: after following symlinks the file must be
// named codex.js and must sit directly inside a directory named bin. Both
// checks are load-bearing. A PATH entry that is the real native binary
// resolves to a file named codex, fails the first check, and is returned
// unchanged by the caller — which is what keeps the plain-PATH path from being
// rewritten into a package layout that does not exist.
//
// Symlinks are followed because that is how npm installs a shim: the PATH
// entry is a link into the package's bin directory, and the package root is
// only reachable from the LINK TARGET. A resolution failure falls back to the
// literal path rather than erroring — an unresolvable link still has a name,
// and the two checks below will reject it if it is not a shim.
func nativeBinaryFromShim(shimPath string) string {
	shimPath = strings.TrimSpace(shimPath)
	if shimPath == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(shimPath)
	if err != nil {
		resolved = shimPath
	}
	if filepath.Base(resolved) != shimFileName {
		return ""
	}
	binDir := filepath.Dir(resolved)
	if filepath.Base(binDir) != "bin" {
		return ""
	}
	return managedBinaryPath(filepath.Dir(binDir))
}

// lookPath resolves name against the PATH carried by env.
//
// The rule — exec.LookPath's, over a SUPPLIED environment instead of the
// process's own — lives in internal/launchenv, because every system plugin
// needs the identical reshape and two copies that disagree about an empty PATH
// element both launch. The distinction this file cares about survives the move:
// "PATH is absent" and "PATH contains no codex" are different facts and produce
// different errors, so a caller that curated an environment without PATH is not
// told its codex install is missing.
func lookPath(env []string, name string) (string, error) {
	return launchenv.LookPath(env, name)
}

// isRegularFile reports whether path names an existing file rather than a
// directory. A stat error of any kind reads as "not usable here", which is the
// source's `if _, err := os.Stat(...); err == nil` verbatim; the caller's next
// move — try the following resolution path — is the same for a missing file
// and an unreadable one.
func isRegularFile(path string) bool { return launchenv.IsRegularFile(path) }

// lookupEnv returns the value of key in a KEY=VALUE environment and whether it
// was present at all.
//
// The two-result shape is the point: an unset PATH and an empty PATH are
// different facts. An empty PATH is a caller saying "resolve nothing", and it
// produces a not-found refusal; an absent one produces
// ErrNoPathInLaunchEnvironment.
func lookupEnv(env []string, key string) (string, bool) { return launchenv.Lookup(env, key) }

// envValue returns the trimmed value of key, or "" when it is unset.
//
// It is the source's envValue, and it trims because the values it reads —
// package roots, the NAMES of other variables — are path- and identifier-
// shaped, where surrounding whitespace is always a typo rather than data.
func envValue(env []string, key string) string { return launchenv.Value(env, key) }

// hostOS and hostArch report the platform the resolution table was consulted
// for. They exist so a test that SKIPS on an unlisted platform can name which
// one it skipped for: "no managed package here" and "the table is wrong" look
// identical in a skip message that does not say where it ran.
func hostOS() string   { return runtime.GOOS }
func hostArch() string { return runtime.GOARCH }
