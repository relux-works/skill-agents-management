package providerquota

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

func quotaSources(t *testing.T) map[string]string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := gosources.Root(dir)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := gosources.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

// Walk providerquota and imports of the four quota entry points, across all
// platform arms. Reader packages themselves are not blanket allowlisted.
var quotaExecExceptions = map[string]bool{
	// Task AC exception: legacy providerlimits process liveness, outside quota reads.
	"pkg/providerlimits/liveness_unix.go": true,
	// Task AC exception: local inference runtime client, outside subscription reads.
	"pkg/localruntime/client.go": true,
	// Existing offline tool preflight; plans never call Probe from quota entry points.
	"internal/toolprobe/toolprobe.go": true,
	// Same preflight's unix process-group arm: setProbeProcessGroup and
	// killProbeChild take *exec.Cmd, as the excepted runner does.
	"internal/toolprobe/toolprobe_unix.go": true,
	// Same preflight's non-unix arm: direct-child teardown takes
	// *exec.Cmd, as the excepted runner does.
	"internal/toolprobe/toolprobe_other.go": true,
	// Existing providerlimits start-time liveness, not quota store liveness.
	"pkg/providerlimits/liveness_starttime_unix.go": true,
}

func forbiddenImports(sources map[string]string) []string {
	files := map[string]*ast.File{}
	packages := map[string][]string{}
	out := []string{}
	for path, source := range sources {
		f, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly)
		if err != nil {
			out = append(out, path+":invalid")
			continue
		}
		files[path] = f
		dir := path[:strings.LastIndex(path, "/")]
		packages[dir] = append(packages[dir], path)
	}
	visited := map[string]bool{}
	queue := []string{"pkg/providerquota"}
	// Seed exactly the quota files' imports, then walk their whole helper packages.
	for _, harness := range []string{"codex", "claude", "agy", "muse"} {
		path := "pkg/agentic/systems/" + harness + "/quota.go"
		queue = append(queue, "pkg/agentic/systems/"+harness)
		if f := files[path]; f != nil {
			for _, imp := range f.Imports {
				name, _ := strconv.Unquote(imp.Path.Value)
				if name == "os/exec" {
					out = append(out, path)
				}
				if strings.HasPrefix(name, "github.com/relux-works/skill-agents-management/") {
					queue = append(queue, strings.TrimPrefix(name, "github.com/relux-works/skill-agents-management/"))
				}
			}
		}
	}
	const prefix = "github.com/relux-works/skill-agents-management/"
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if visited[pkg] {
			continue
		}
		visited[pkg] = true
		for _, path := range packages[pkg] {
			for _, imp := range files[path].Imports {
				name, _ := strconv.Unquote(imp.Path.Value)
				if name == "os/exec" && !quotaExecExceptions[path] {
					out = append(out, path)
				}
				if strings.HasPrefix(name, prefix) {
					dependency := strings.TrimPrefix(name, prefix)
					queue = append(queue, dependency)
					if pkg == "pkg/providerquota" && strings.HasPrefix(dependency, "pkg/agentic/systems/") {
						out = append(out, path)
					}
				}
			}
		}
	}
	return out
}

func TestImportGraphPlanBoundary(t *testing.T) {
	sources := quotaSources(t)
	if got := forbiddenImports(sources); len(got) > 0 {
		t.Fatalf("forbidden imports: %v", got)
	}
	for _, mutant := range []struct{ path, body string }{{"pkg/providerquota/escape.go", `package providerquota; import "os/exec"`}, {"pkg/providerquota/table.go", `package providerquota; import "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"`}} {
		if len(forbiddenImports(map[string]string{mutant.path: mutant.body})) != 1 {
			t.Fatal("boundary guard missed planted import")
		}
	}
}
func TestDispatcherHasNoHarnessIdentifiers(t *testing.T) {
	source, err := os.ReadFile("reader.go")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "reader.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Dispatch" {
			continue
		}
		checked = true
		ast.Inspect(fn, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING {
				v, _ := strconv.Unquote(lit.Value)
				for _, name := range []string{"codex", "claude", "agy", "antigravity", "muse", "gemini", "qwen", "pi"} {
					if v == name {
						t.Errorf("dispatcher spells harness %s", name)
					}
				}
			}
			return true
		})
	}
	if !checked {
		t.Fatal("dispatcher not inspected")
	}
}
func TestReview12ParsersHaveNoFilesystemCapability(t *testing.T) {
	count := 0
	for path, source := range quotaSources(t) {
		if !strings.HasPrefix(path, "pkg/agentic/systems/") || !strings.HasSuffix(path, "/quota.go") {
			continue
		}
		count++
		f, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if name == "os" || name == "io/fs" || name == "os/exec" || name == "net" || name == "net/http" {
				t.Errorf("parser/planner %s imports %s", path, name)
			}
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "ParseQuota" {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "resolveBinary" {
					t.Errorf("%s parser resolves binary", path)
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NormalizeProviderHome" {
					t.Errorf("%s parser stats a home via normalization", path)
				}
				return true
			})
		}
	}
	if count != 4 {
		t.Fatalf("checked %d parsers, want four", count)
	}
}
func TestPackageDocNeverOpenRule(t *testing.T) {
	b, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"never opens, stats or", "lists a file under a harness home", "only thing that reads", "its own credential"} {
		if !strings.Contains(string(b), part) {
			t.Fatalf("doc lost rule %q", part)
		}
	}
}
