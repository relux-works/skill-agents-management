// Package gosources answers one question for every static guard in this
// module: which Go files does this module's build compile?
//
// It exists because the answer was written twice before it was written once.
// pkg/agentic's single-source guard and pkg/agentic/systems/codex's argv guard
// each carried their own walk, with their own skip rules, and a third copy was
// about to land with the second system plugin. Invariant 5 of
// docs/architecture.md names that shape — one fact, several sources — and the
// divergence would have been invisible: two guards that disagree about what
// "the whole module" means both report clean.
//
// # Why it is a normal package rather than test helpers
//
// Go has no way to share test-only code across packages. The guards that need
// this walk live in three different packages' test binaries, so the choice is
// one ordinary package or one copy per guard. Nothing in the shipped command
// tree imports this, so it is compiled and never linked into the binary.
//
// # The skip rules, and the red trunk that produced them
//
// The rules mirror go/build's own rather than naming directories. The earlier
// version carried a denylist of specific machine-local names — .git, .temp,
// .task-board, .claude, .codex — which is a list of the trees somebody had
// thought of by then. A bootstrapped checkout has agents-infra installed at
// .agents/: gitignored, absent from every worktree, carrying its own go.mod,
// and dispatching on "codex" and "claude". The guard reported it, `go test
// ./...` went red on main and stayed green everywhere else, and the difference
// was whose machine ran it.
package gosources

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SkipDirNames are the directory names excluded on top of the toolchain's own
// dot/underscore rule.
//
// vendor and node_modules hold dependencies rather than this module's source.
// testdata is a DECISION rather than an inheritance: go build ignores it, so a
// violation planted there is not code this module runs, and a guard fixture
// that wants a violating source is better off writing it into a temp tree whose
// root it also controls. The cost is that a genuine violation parked in
// testdata goes unreported; the benefit is that the scan set and the build set
// are the same set, with no third rule.
//
// It is a var rather than an inline check so a test can narrow it and show
// which tree each entry covers.
var SkipDirNames = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"testdata":     true,
}

// SkipDir answers whether a walk rooted at root refuses to descend into path,
// which is the one question that decides what "the whole module" means.
//
// root itself is never skipped on its own name. The module root is routinely a
// dot-nested path — a worktree under .temp/, a checkout under .cache/ — and
// testing its own name would silently scan zero files while reporting clean.
//
// A stat that fails for a reason other than "not there" is PROPAGATED, not read
// as absence. A directory the walk cannot inspect is an unknown, and answering
// "no go.mod" for it would turn an unreadable tree into a silently unscanned
// one. An absence and a failure to read are different facts.
func SkipDir(root, path, name string) (bool, error) {
	if path == root {
		return false, nil
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true, nil
	}
	if SkipDirNames[name] {
		return true, nil
	}
	switch _, err := os.Stat(filepath.Join(path, "go.mod")); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("deciding whether %s is a nested module: %w", path, err)
	}
}

// Walk collects every non-test Go source under root that the Go build of the
// module rooted there would compile, keyed by slash-separated path relative to
// root.
//
// It takes root as an argument rather than finding it, so the exclusion rules
// can be driven over a fixture tree with planted violations instead of only
// over the real checkout, where a dot-directory may or may not exist depending
// on whose machine is running the suite.
func Walk(root string) (map[string]string, error) {
	sources := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			skip, err := SkipDir(root, path, entry.Name())
			if err != nil {
				return err
			}
			if skip {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sources[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sources, nil
}

// Root walks up from dir to the directory holding go.mod.
//
// Callers pass their own working directory, so a guard running in any package
// of this module reaches the whole module rather than one directory.
func Root(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("gosources: no go.mod at or above %s; a guard rooted there has nothing to scan", dir)
		}
		dir = parent
	}
}
