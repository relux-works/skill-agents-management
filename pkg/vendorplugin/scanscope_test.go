package vendorplugin

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// pkg/vendorplugin carries a second copy of the module walk, for the reason
// double_test.go's moduleSources states: both walks are test-only and Go cannot
// share an unexported test helper across packages.
//
// Two copies of one rule drift, and a drifted copy here is not a cosmetic
// problem. TestVendorDoubleExistsOnlyInTests asserts the test doubles appear in
// no non-test source of this module; that claim is worth exactly the scan
// behind it. A copy that widened its exclusions would keep passing while
// scanning less, and a copy that narrowed them would go red on whatever a
// developer happens to have installed under a dot-directory — which is the
// trunk failure this scope was written for.
//
// So the same fixture tree pkg/agentic's TestSingleSourceGuardScanScope plants
// is planted here, with the same expected verdicts, deliberately spelled out
// again rather than imported. If the two lists disagree, one of the two walks
// has drifted and this fails.

// scanScopeCase is one planted path and the verdict this walk must return.
type scanScopeCase struct {
	path    string
	scanned bool
	why     string
}

var scanScopeCases = []scanScopeCase{
	{"internal/infra/child_launch_composition.go", true, "an ordinary package of this module"},
	{"tools/agents-management/cmd/plugins.go", true, "a second ordinary package"},
	{".agents/tools/agents-infra/internal/infra/child_launch_composition.go", false, "a dot-directory: machine-local agents-infra"},
	{".claude/hooks/dispatch.go", false, "a dot-directory the old denylist happened to name"},
	{"_scratch/pick.go", false, "an underscore-directory"},
	{"nested/pick.go", false, "a subtree carrying its own go.mod"},
	{"nested/deeper/pick.go", false, "the nested module's whole subtree"},
	{"testdata/pick.go", false, "testdata, which go build ignores"},
	{"vendor/example.com/dep/pick.go", false, "vendor: dependency code"},
	{"internal/infra/testdata/pick.go", false, "testdata nested inside a scanned package"},
}

// TestModuleScanScopeMatchesTheGuard drives this package's copy of the walk over
// the guard's fixture tree, in both directions.
func TestModuleScanScopeMatchesTheGuard(t *testing.T) {
	// Rooted under a directory named .temp on purpose: this checkout habitually
	// lives in one, and a walk that tested the root's own name against the dot
	// rule would scan zero files here while reporting clean.
	root := filepath.Join(t.TempDir(), ".temp", "worktree")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}
	write := func(relative, content string) {
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", full, err)
		}
	}
	write("go.mod", "module example.com/fixture\n\ngo 1.25.5\n")
	write("nested/go.mod", "module example.com/fixture/nested\n\ngo 1.25.5\n")
	// The content is irrelevant to the walk and deliberately kept trivial; what
	// this test holds is which files the walk returns, not what a scanner then
	// makes of them.
	for _, tc := range scanScopeCases {
		write(tc.path, "package p\n")
	}

	sources, err := walkModuleSources(root)
	if err != nil {
		t.Fatalf("walkModuleSources(%s): %v", root, err)
	}
	for _, tc := range scanScopeCases {
		_, got := sources[tc.path]
		switch {
		case tc.scanned && !got:
			t.Errorf("the walk did not reach %s — %s; this copy scans less than pkg/agentic's, so its no-trace claim covers less than it says. scanned=%v", tc.path, tc.why, sortedNames(sources))
		case !tc.scanned && got:
			t.Errorf("the walk reached %s — %s; this module's build compiles none of it", tc.path, tc.why)
		}
	}
	want := map[string]bool{}
	for _, tc := range scanScopeCases {
		if tc.scanned {
			want[tc.path] = true
		}
	}
	for name := range sources {
		if !want[name] {
			t.Errorf("the walk returned %s, which no case here plants or excludes", name)
		}
	}
}

// TestSkipModuleDirNeverSkipsTheRoot is the one case in which every exclusion
// must stay silent. A checkout sitting in .temp/ or _build/ is ordinary, and
// skipping its root turns this package's no-trace claim into a scan of nothing.
func TestSkipModuleDirNeverSkipsTheRoot(t *testing.T) {
	for _, name := range []string{".temp", ".agents", "_scratch", "vendor", "testdata"} {
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		skip, err := skipModuleDir(root, root, name)
		if err != nil {
			t.Fatalf("skipModuleDir(%s): %v", root, err)
		}
		if skip {
			t.Errorf("skipModuleDir skipped the module root %q", name)
		}
	}
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
