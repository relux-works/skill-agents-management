package codex

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// TestPlannerReadsUseObservableBoundary prevents direct OS calls from silently
// bypassing the behavioural recorder. It scans every production file in the
// core and Codex packages, including import aliases and function-value uses.
// This is an architectural check for the named IO APIs, not a syscall tracer
// or a claim about arbitrary third-party plugins. Native reads belong to the
// internal Host adapter and the Codex-owned readCatalogFile default.
func TestPlannerReadsUseObservableBoundary(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := gosources.Root(cwd)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"pkg/agentic", "pkg/agentic/systems/codex"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := filepath.Join(root, dir, entry.Name())
			source, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, violation := range unobservedPlannerReads(t, name, source) {
				t.Error(violation)
			}
		}
	}
}

func unobservedPlannerReads(t *testing.T, name string, source []byte) []string {
	t.Helper()
	// Both calls and references must use the reader: assigning os.ReadFile
	// to a variable would otherwise make a CallExpr-only check miss the read.
	forbidden := map[string]string{
		"os":            "ReadFile ReadDir Open OpenFile Stat Lstat Readlink Getwd DirFS",
		"path/filepath": "Abs EvalSymlinks Glob Walk WalkDir",
		"io/fs":         "ReadFile ReadDir Stat Glob WalkDir",
		"io/ioutil":     "ReadFile ReadDir",
		"os/exec":       "LookPath",
		"github.com/relux-works/skill-agents-management/internal/launchenv":   "LookPath IsRegularFile",
		"github.com/relux-works/skill-agents-management/internal/catalogfile": "ReadRegular",
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	// This function-value binding is the sole native bounded-reader default.
	// Calls still go through readCatalogFile; a second reference is refused.
	var catalogDefault ast.Expr
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR || filepath.Base(name) != "catalog.go" {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == "readCatalogFile" && len(value.Values) == 1 {
				catalogDefault = value.Values[0]
			}
		}
	}
	aliases := map[string]string{}
	var violations []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		alias := filepath.Base(path)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "." && forbidden[path] != "" {
			violations = append(violations, fset.Position(spec.Pos()).String()+": dot import bypasses observable planner reads")
		}
		aliases[alias] = path
	}
	ast.Inspect(file, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		for _, symbol := range strings.Fields(forbidden[aliases[ident.Name]]) {
			if sel.Sel.Name == symbol {
				if sel == catalogDefault && aliases[ident.Name] == "github.com/relux-works/skill-agents-management/internal/catalogfile" && symbol == "ReadRegular" {
					continue
				}
				violations = append(violations, fset.Position(sel.Pos()).String()+": unobserved planner read "+ident.Name+"."+symbol+"; use planningio.Default")
			}
		}
		return true
	})
	return violations
}

func TestPlannerReadBoundaryCatchesAliasedAndIndirectReads(t *testing.T) {
	for _, source := range []string{
		`package probe; import disk "os"; func f() { _, _ = disk.ReadFile("config") }`,
		`package probe; import "os"; var read = os.ReadFile; func f() { _, _ = read("config") }`,
		`package probe; import . "os"; func f() { _, _ = ReadFile("config") }`,
	} {
		if len(unobservedPlannerReads(t, "probe.go", []byte(source))) == 0 {
			t.Errorf("unobserved read admitted: %s", source)
		}
	}
	if got := unobservedPlannerReads(t, "probe.go", []byte(`package probe; import "github.com/relux-works/skill-agents-management/pkg/agentic/internal/planningio"; func f() { _, _ = planningio.Default.ReadFile("config") }`)); len(got) != 0 {
		t.Fatalf("observable read refused: %v", got)
	}
}
