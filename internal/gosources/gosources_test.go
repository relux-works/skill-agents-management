package gosources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The skip rules themselves are held over a fixture tree by
// pkg/agentic/singlesource_scanscope_test.go, which drives them from the
// production call site — the single-source guard — in both directions. What is
// here is what that test does not reach: Root, and the file-level half of Walk.

// TestRootFindsTheModuleRoot walks up from a nested directory, which is the
// only shape a guard ever calls it with: a test's working directory is its own
// package, several levels below go.mod.
func TestRootFindsTheModuleRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	nested := filepath.Join(root, "pkg", "deep", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got, err := Root(nested)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if got != root {
		t.Errorf("Root = %q, want %q", got, root)
	}
}

// TestRootRefusesWhenThereIsNoModule is the absence reported as one.
//
// A guard whose Root silently returned the filesystem root would scan an
// operator's whole disk and report violations in code this module never
// compiles — the red-trunk failure one size larger.
func TestRootRefusesWhenThereIsNoModule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Root(dir); err == nil {
		t.Fatal("Root found a module where no go.mod exists")
	}
}

// TestWalkCollectsOnlyCompiledGoFiles pins the file-level rule: test sources
// and non-Go files are excluded, and paths come back slash-separated and
// relative so an allowlist key means the same thing on every platform.
func TestWalkCollectsOnlyCompiledGoFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/x\n",
		"pkg/a/a.go":        "package a\n",
		"pkg/a/a_test.go":   "package a\n",
		"pkg/a/notes.md":    "not go\n",
		"pkg/b/sub/deep.go": "package sub\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	sources, err := Walk(root)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	want := map[string]bool{"pkg/a/a.go": true, "pkg/b/sub/deep.go": true}
	for name := range sources {
		if !want[name] {
			t.Errorf("Walk returned %q, which the Go build of this module does not compile into a package under test scan", name)
		}
		if strings.Contains(name, "\\") {
			t.Errorf("Walk returned a platform-separated path %q; allowlist keys are written with slashes", name)
		}
	}
	for name := range want {
		if _, ok := sources[name]; !ok {
			t.Errorf("Walk did not return %q; a file the scan never reads is a file no guard guards", name)
		}
	}
}
