package agentic

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file holds the guard's SCAN SCOPE — which directories the walk descends
// into — to the same standard as the guard's rules. The scope section of the
// threat model in singlesource_guard_test.go is the prose; this is what holds
// it to the code.
//
// The defect it exists to prevent is directional, and both directions are
// failures:
//
//   - Too WIDE and the suite's colour depends on the developer's disk. The walk
//     used to carry a denylist of dot-directory names, and a bootstrapped
//     checkout with agents-infra installed at .agents/ reddened
//     `go test ./...` on main over a file the Go build never compiles, while
//     every worktree stayed green.
//   - Too NARROW and the guard reports clean because it looked at nothing. An
//     exclusion that widens by one directory name is an unguarded package that
//     still passes CI, which is the whole failure this guard was written for.
//
// So every case is planted TWICE: once where it must be skipped, once in an
// ordinary package where the identical source must be reported.

// violatingSource is the shape that reddened trunk, reduced to what the guard
// reacts to: an if-chain comparing an identifier against two known plugin ids.
// It is the id-switch rule's own vocabulary net, so a copy of this file is a
// violation wherever the scanner is handed it.
const violatingSource = `package infra

import "fmt"

func BuildChildLaunchComposition(agent string) (string, error) {
	if agent != "codex" && agent != "claude" {
		return "", fmt.Errorf("unsupported agent %q", agent)
	}
	return agent, nil
}
`

// scanScopeCase is one planted copy of violatingSource and the verdict the walk
// must return for it.
type scanScopeCase struct {
	// path is relative to the fixture module root, slash-separated.
	path string
	// scanned is whether walkModuleSources must return this file.
	scanned bool
	// why states the rule under test, and is what a failure prints.
	why string
}

var scanScopeCases = []scanScopeCase{
	{
		path:    "internal/infra/child_launch_composition.go",
		scanned: true,
		why:     "an ordinary package of this module; the guard's whole job is to report this",
	},
	{
		path:    "tools/agents-management/cmd/plugins.go",
		scanned: true,
		why:     "a second ordinary package, so the positive case is not one lucky path",
	},
	{
		// The exact path from the trunk failure, so the regression is pinned by
		// shape and not only by rule.
		path:    ".agents/tools/agents-infra/internal/infra/child_launch_composition.go",
		scanned: false,
		why:     "a dot-directory: machine-local agents-infra, gitignored, never compiled by this module's build",
	},
	{
		path:    ".claude/hooks/dispatch.go",
		scanned: false,
		why:     "a dot-directory the old denylist happened to name; the rule must not depend on the name",
	},
	{
		path:    "_scratch/pick.go",
		scanned: false,
		why:     "an underscore-directory: go/build ignores it verbatim",
	},
	{
		path:    "nested/pick.go",
		scanned: false,
		why:     "a subtree carrying its own go.mod; a nested module's bindings are its own registry's business",
	},
	{
		path:    "nested/deeper/pick.go",
		scanned: false,
		why:     "the nested module's whole subtree, not just its root directory",
	},
	{
		path:    "testdata/pick.go",
		scanned: false,
		why:     "testdata: go build ignores it, and the threat model's scan-scope section takes that decision explicitly",
	},
	{
		path:    "vendor/example.com/dep/pick.go",
		scanned: false,
		why:     "vendor: dependency code, not this module's source",
	},
	{
		path:    "internal/infra/testdata/pick.go",
		scanned: false,
		why:     "testdata nested inside a scanned package, not only at the module root",
	},
}

// writeScanScopeFixture builds a module tree with violatingSource planted at
// every case path and returns its root.
//
// The root is deliberately created UNDER a directory named .temp. This checkout
// habitually lives in one — the story worktree this test was written in is
// .temp/STORY-260821-224xfu/worktree — and a walk that tested the root's own
// name against the dot rule would scan zero files there while reporting clean.
// The fixture would then pass for the worst possible reason.
func writeScanScopeFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".temp", "worktree")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}
	write := func(relative, content string) {
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", full, err)
		}
	}
	write("go.mod", "module example.com/fixture\n\ngo 1.25.5\n")
	write("nested/go.mod", "module example.com/fixture/nested\n\ngo 1.25.5\n")
	for _, tc := range scanScopeCases {
		write(tc.path, violatingSource)
	}
	return root
}

// TestSingleSourceGuardScanScopeFixtureIsViolating runs FIRST in reading order
// on purpose: it proves the planted source is something the guard reports at
// all.
//
// Without it, every "not reported" assertion below is equally consistent with a
// fixture that violates nothing, and the exclusion could widen to swallow the
// whole module while this file stayed green. Each case path is scanned directly
// — bypassing the walk — and every one must be reported.
func TestSingleSourceGuardScanScopeFixtureIsViolating(t *testing.T) {
	direct := map[string]string{}
	for _, tc := range scanScopeCases {
		direct[tc.path] = violatingSource
	}
	violations, err := scanSingleSource(direct, bindingHomes)
	if err != nil {
		t.Fatalf("scanSingleSource: %v", err)
	}
	reported := map[string]bool{}
	for _, v := range violations {
		if v.Class == bindingClassIDSwitch {
			reported[v.File] = true
		}
	}
	for _, tc := range scanScopeCases {
		if !reported[tc.path] {
			t.Errorf("the planted source at %s is not reported when scanned directly; every skip assertion in this file would then prove nothing. violations=%v", tc.path, violations)
		}
	}
}

// TestSingleSourceGuardScanScope is the walk, both directions at once.
func TestSingleSourceGuardScanScope(t *testing.T) {
	root := writeScanScopeFixture(t)
	sources, err := walkModuleSources(root)
	if err != nil {
		t.Fatalf("walkModuleSources(%s): %v", root, err)
	}
	for _, tc := range scanScopeCases {
		_, got := sources[tc.path]
		switch {
		case tc.scanned && !got:
			t.Errorf("the walk did not reach %s — %s; a file the guard never reads is a file it never guards. scanned=%v", tc.path, tc.why, sortedKeys(sources))
		case !tc.scanned && got:
			t.Errorf("the walk reached %s — %s; this module's build compiles none of it, so reporting it makes the suite's colour depend on the developer's disk", tc.path, tc.why)
		}
	}
	// Set equality, not just per-case membership: a walk that also picked up
	// something nobody planted has an exclusion this file does not describe.
	want := map[string]bool{}
	for _, tc := range scanScopeCases {
		if tc.scanned {
			want[tc.path] = true
		}
	}
	for name := range sources {
		if !want[name] {
			t.Errorf("the walk returned %s, which no case in this file plants or excludes; the scan set and the case list have drifted", name)
		}
	}
}

// TestSingleSourceGuardScanScopeReportsOnlyBuiltCode drives the guard itself
// over the fixture rather than the walk in isolation, because the walk is not
// the production call site — scanSingleSource(moduleSources(t), bindingHomes)
// is, and that is the pair this test reproduces.
func TestSingleSourceGuardScanScopeReportsOnlyBuiltCode(t *testing.T) {
	root := writeScanScopeFixture(t)
	sources, err := walkModuleSources(root)
	if err != nil {
		t.Fatalf("walkModuleSources(%s): %v", root, err)
	}
	violations, err := scanSingleSource(sources, bindingHomes)
	if err != nil {
		t.Fatalf("scanSingleSource: %v", err)
	}
	reported := map[string]bool{}
	for _, v := range violations {
		reported[v.File] = true
	}
	for _, tc := range scanScopeCases {
		if tc.scanned && !reported[tc.path] {
			t.Errorf("the guard stayed silent on %s — %s. violations=%v", tc.path, tc.why, violations)
		}
		if !tc.scanned && reported[tc.path] {
			t.Errorf("the guard reported %s — %s. This is the trunk failure verbatim.", tc.path, tc.why)
		}
	}
	if len(violations) == 0 {
		t.Fatal("the guard reported nothing at all over a fixture built entirely from violating sources; the scan is vacuous")
	}
}

// TestSingleSourceGuardScanScopeNarrowed proves the bound by NARROWING the
// exclusion rather than deleting it, which is the difference between showing
// the rule exists and showing what class it covers.
//
// Deleting the dot rule would make .agents reported again and prove only that
// something excludes it. Narrowing it — asking whether the walk would have
// reached each skipped path had ONLY that one rule not applied — pins which
// rule is doing the work for which path, so a later edit that collapses two
// rules into one loose one is visible here.
func TestSingleSourceGuardScanScopeNarrowed(t *testing.T) {
	root := writeScanScopeFixture(t)
	for _, tc := range scanScopeCases {
		if tc.scanned {
			continue
		}
		t.Run(tc.path, func(t *testing.T) {
			// The first excluded ancestor of this path, walking down from the
			// root. Exactly one rule must account for it; if none does, the
			// path is being skipped by something this file does not model.
			parts := strings.Split(tc.path, "/")
			var excludedBy string
			dir := root
			for _, part := range parts[:len(parts)-1] {
				dir = filepath.Join(dir, part)
				skip, err := skipModuleDir(root, dir, part)
				if err != nil {
					t.Fatalf("skipModuleDir(%s): %v", dir, err)
				}
				if skip {
					switch {
					case strings.HasPrefix(part, "."):
						excludedBy = "dot-directory"
					case strings.HasPrefix(part, "_"):
						excludedBy = "underscore-directory"
					case moduleSkipDirs[part]:
						excludedBy = "named directory " + part
					default:
						excludedBy = "nested go.mod"
					}
					break
				}
			}
			if excludedBy == "" {
				t.Fatalf("no directory on the way to %s is excluded by skipModuleDir, yet the walk does not return it — %s", tc.path, tc.why)
			}
			t.Logf("%s excluded by %s — %s", tc.path, excludedBy, tc.why)
		})
	}
}

// TestSkipModuleDirDoesNotSkipTheRoot pins the one case where every rule above
// must NOT fire. A root named .temp, _build or vendor is an ordinary place for
// a checkout to sit, and skipping it turns the guard into a permanently green
// scan of nothing.
func TestSkipModuleDirDoesNotSkipTheRoot(t *testing.T) {
	for _, name := range []string{".temp", ".agents", "_scratch", "vendor", "testdata", "node_modules"} {
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", root, err)
		}
		skip, err := skipModuleDir(root, root, name)
		if err != nil {
			t.Fatalf("skipModuleDir(%s): %v", root, err)
		}
		if skip {
			t.Errorf("skipModuleDir skipped the module root %q; the scan would then read zero files and report clean forever", name)
		}
	}
}

// TestSkipModuleDirRootGoModIsNotNested is the same boundary for the nested-
// module rule specifically: every module root has a go.mod, and a rule that
// did not exempt the root would skip the entire module on sight.
func TestSkipModuleDirRootGoModIsNotNested(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	skip, err := skipModuleDir(root, root, filepath.Base(root))
	if err != nil {
		t.Fatalf("skipModuleDir: %v", err)
	}
	if skip {
		t.Fatal("skipModuleDir treated the module root's own go.mod as a nested module")
	}
}

// TestSkipModuleDirUnreadableIsNotAbsent holds the difference between "this
// directory has no go.mod" and "this directory could not be read".
//
// They are different facts, and collapsing them is how an unreadable subtree
// becomes a silently unscanned one — the guard would descend, the walk would
// fail or the tree would be half-read, and the answer would look like a clean
// scan. skipModuleDir propagates instead.
func TestSkipModuleDirUnreadableIsNotAbsent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits do not deny a stat")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "opaque")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	// Confirm the deny actually took effect on this filesystem before drawing a
	// conclusion from it; a stat that succeeds here would make the assertion
	// below vacuous.
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); errors.Is(err, fs.ErrNotExist) {
		t.Skip("this filesystem still reports not-exist inside a 0000 directory; nothing is being denied")
	}
	skip, err := skipModuleDir(root, dir, "opaque")
	if err == nil {
		t.Fatalf("skipModuleDir read an unstattable directory as a settled answer (skip=%v); a failure to read is not an absence", skip)
	}
	if !strings.Contains(err.Error(), "nested module") {
		t.Errorf("skipModuleDir error does not say what it failed to decide: %v", err)
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
