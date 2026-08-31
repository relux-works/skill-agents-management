package localmodels

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// moduleImportPrefix is this module's own import path. Every package this
// module declares is reachable by stripping this prefix off an import path
// and treating the remainder as a directory relative to the module root.
const moduleImportPrefix = "github.com/relux-works/skill-agents-management"

// thisPackageDir is this test's own package directory, relative to the
// module root, which is also the root of the import graph case 14 walks.
const thisPackageDir = "pkg/vendorplugin/vendors/local-models"

// exemptDir is the ONE package this vendor's import graph is allowed to
// reach that this test does not itself inspect for a forbidden import:
// pkg/localruntime, whose own (read-only, status-subprocess-only) use of
// os/exec is the architecture's own named exception — "its import graph
// contains pkg/localruntime (read-only) and nothing else capable of
// starting, signaling, or leasing Process B."
const exemptDir = "pkg/localruntime"

// forbiddenImports are process-control packages: capable of starting,
// signaling or otherwise controlling an OS process. A local-models vendor
// that reaches any of these anywhere in its OWN import graph (outside the
// one named exception above) could start or signal Process B directly,
// which is exactly the authority this design reserves entirely to
// relux-agents-infra's shared-runtime broker.
var forbiddenImports = map[string]string{
	"os/exec":   "can start, signal and wait on child processes",
	"os/signal": "can send/receive OS process signals",
	"syscall":   "raw process-control primitives (Kill, Wait4, ...)",
}

// packageImports parses every non-test Go file in one directory's sources
// (a subset of gosources.Walk's whole-module map) and returns the set of
// import paths they declare, using parser.ImportsOnly so this stays a fast,
// syntax-level read rather than a full type-checked build.
func packageImports(t *testing.T, root, dir string, sources map[string]string) map[string]bool {
	t.Helper()
	imports := map[string]bool{}
	for relPath, content := range sources {
		if filepath.ToSlash(filepath.Dir(relPath)) != dir {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(root, relPath), content, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s for its import list: %v", relPath, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			imports[path] = true
		}
	}
	return imports
}

// TestLocalModelsImportGraphNeverReachesProcessControl is case 14: a static
// import-graph check fails the build if os/exec or any process-control
// package ever appears in the import graph of
// pkg/vendorplugin/vendors/local-models, with the one named exception
// (pkg/localruntime's own read-only status-subprocess use) left uninspected
// rather than silently allowed everywhere.
func TestLocalModelsImportGraphNeverReachesProcessControl(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := gosources.Root(cwd)
	if err != nil {
		t.Fatalf("finding the module root: %v", err)
	}
	sources, err := gosources.Walk(root)
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(sources) < 20 {
		t.Fatalf("scanned %d sources, too few for this claim to mean anything", len(sources))
	}

	visited := map[string]bool{}
	var walk func(dir string)
	walk = func(dir string) {
		if visited[dir] || dir == exemptDir {
			visited[dir] = true
			return
		}
		visited[dir] = true
		for imp := range packageImports(t, root, dir, sources) {
			if reason, forbidden := forbiddenImports[imp]; forbidden {
				t.Errorf("%s imports %q (%s), reachable from %s's own import graph", dir, imp, reason, thisPackageDir)
				continue
			}
			if strings.HasPrefix(imp, moduleImportPrefix) {
				sub := strings.TrimPrefix(imp, moduleImportPrefix+"/")
				walk(sub)
			}
		}
	}
	walk(thisPackageDir)

	if len(visited) < 2 {
		t.Fatalf("the import walk from %s visited only %v; the walk itself is not working", thisPackageDir, visited)
	}
	if !visited[exemptDir] {
		t.Fatalf("this vendor's import graph does not reach %s at all; the test's own exemption is stale — remove it once no longer needed", exemptDir)
	}
}
