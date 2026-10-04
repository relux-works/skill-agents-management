package agentic_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// The module owns no exec: no production file under pkg/agentic may import
// os/exec. Consumers start their own processes after VerifyBeforeExec; a
// process-starting import here would be the seam a hosted consumer inherits.
func TestAgenticProductionImportsExcludeExec(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate this test file")
	}
	root, err := gosources.Root(filepath.Dir(file))
	if err != nil {
		t.Fatalf("module root: %v", err)
	}
	sources, err := gosources.Walk(root)
	if err != nil {
		t.Fatalf("walk module sources: %v", err)
	}
	for _, path := range agenticProductionFilesWithImport(t, root, sources, "os/exec") {
		t.Errorf("production file imports os/exec: %s", path)
	}
}

// TestExecBoundaryDetectsAliasedImports plants the spellings a naive
// substring guard misses: an aliased import keeps the os/exec token but
// changes every other byte on the line, and blank and dot imports change
// the binding too. All three start processes all the same.
func TestExecBoundaryDetectsAliasedImports(t *testing.T) {
	root := t.TempDir()
	writeSealBoundaryFixture(t, root, "pkg/agentic/aliased.go", "package agentic\n\nimport launcher \"os/exec\"\n\nvar _ = launcher.Command\n")
	writeSealBoundaryFixture(t, root, "pkg/agentic/blank.go", "package agentic\n\nimport _ \"os/exec\"\n")
	writeSealBoundaryFixture(t, root, "pkg/agentic/dot.go", "package agentic\n\nimport . \"os/exec\"\n\nvar _ = Command\n")
	writeSealBoundaryFixture(t, root, "pkg/agentic/clean.go", "package agentic\n\nimport \"os\"\n\nvar _ = os.Getenv\n")
	writeSealBoundaryFixture(t, root, "pkg/agentic/execuse_test.go", "package agentic\n\nimport \"os/exec\"\n\nvar _ = exec.Command\n")
	sources, err := gosources.Walk(root)
	if err != nil {
		t.Fatalf("walk fixture sources: %v", err)
	}
	found := agenticProductionFilesWithImport(t, root, sources, "os/exec")
	want := map[string]bool{
		"pkg/agentic/aliased.go": true,
		"pkg/agentic/blank.go":   true,
		"pkg/agentic/dot.go":     true,
	}
	if len(found) != len(want) {
		t.Fatalf("flagged %q, want the three planted imports", found)
	}
	for _, path := range found {
		if !want[path] {
			t.Fatalf("flagged %q, want only the three planted imports", found)
		}
	}
}

func agenticProductionFilesWithImport(t *testing.T, root string, sources map[string]string, importPath string) []string {
	t.Helper()
	var found []string
	for relPath, content := range sources {
		slashed := filepath.ToSlash(relPath)
		if !strings.HasPrefix(slashed, "pkg/agentic/") || !strings.HasSuffix(slashed, ".go") || strings.HasSuffix(slashed, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, relPath), content, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports of %s: %v", relPath, err)
		}
		for _, spec := range parsed.Imports {
			if strings.Trim(spec.Path.Value, `"`) == importPath {
				found = append(found, slashed)
			}
		}
	}
	return found
}

func writeSealBoundaryFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
