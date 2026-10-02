package benchdata

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/relux-works/skill-agents-management"

// Source scanning deliberately ignores build constraints and filename platform
// suffixes, following imports recursively in ALL production Go files. This is
// a conservative union of every production build's import closure. Standard
// library build-ignored code generators are excluded, but all module-owned
// files are checked even under ignore. Test imports
// are outside the accessor's production boundary. No go subprocess is needed.
func scanClosure(root, start string) ([]string, int, error) {
	return inspectClosure(start, func(path string) (map[string]string, error) {
		var dir string
		if strings.HasPrefix(path, modulePath+"/") {
			dir = filepath.Join(root, strings.TrimPrefix(path, modulePath+"/"))
		} else {
			pkg, err := build.Default.Import(path, "", build.FindOnly)
			if err != nil {
				return nil, err
			}
			if !pkg.Goroot {
				return nil, fmt.Errorf("non-standard dependency %q", path)
			}
			dir = pkg.Dir
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		sources := map[string]string{}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			// The standard library ships go:build ignore generator programs and
			// cgo input templates beside its package files. They are not package
			// dependencies. Never apply this exclusion to module-owned sources.
			if !strings.HasPrefix(path, modulePath+"/") && strings.Contains(string(data), "//go:build ignore\n") {
				continue
			}
			sources[name] = string(data)
		}
		if len(sources) == 0 {
			return nil, fmt.Errorf("empty production scope %q", path)
		}
		return sources, nil
	})
}

func forbiddenDependency(path string) bool {
	if path == modulePath+"/pkg/vendorplugin" || strings.HasPrefix(path, modulePath+"/pkg/vendorplugin/vendors/") || path == modulePath+"/pkg/agentic" || strings.HasPrefix(path, modulePath+"/pkg/agentic/") {
		return true
	}
	// os is denied entirely, which is stronger than forbidding only its file APIs.
	for _, denied := range []string{"os", "net", "time", "syscall"} {
		if path == denied || strings.HasPrefix(path, denied+"/") {
			return true
		}
	}
	return path == "C"
}

func inspectClosure(start string, load func(string) (map[string]string, error)) ([]string, int, error) {
	var problems []string
	visited := map[string]bool{}
	var walk func(string) error
	walk = func(path string) error {
		if visited[path] {
			return nil
		}
		visited[path] = true
		if forbiddenDependency(path) {
			problems = append(problems, "forbidden dependency: "+path)
			return nil
		}
		sources, err := load(path)
		if err != nil {
			return err
		}
		for name, source := range sources {
			file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
			if err != nil {
				return err
			}
			local := strings.HasPrefix(path, modulePath+"/")
			if local {
				for _, decl := range file.Decls {
					if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "init" {
						problems = append(problems, path+"/"+name+": init function")
					}
				}
			}
			for _, imported := range file.Imports {
				dependency, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				if local && !strings.HasPrefix(dependency, modulePath+"/pkg/vendorplugin/benchdata/") && dependency != "strconv" && dependency != "strings" {
					problems = append(problems, path+"/"+name+": undeclared data dependency "+dependency)
				}
				if err := walk(dependency); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(start)
	return problems, len(visited), err
}

func TestImportClosureAllBuildTags(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	problems, packages, err := scanClosure(root, modulePath+"/pkg/vendorplugin/benchdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("data-only closure violations: %v", problems)
	}
	if packages < 2 {
		t.Fatal("guard did not traverse standard-library dependencies")
	}
	t.Logf("all-build-tags production import closure: %d packages, no forbidden capabilities or init registration", packages)
}

func TestImportClosureRejectsTransitiveAndTaggedCapabilities(t *testing.T) {
	for _, dependency := range []string{modulePath + "/pkg/vendorplugin", modulePath + "/pkg/agentic", modulePath + "/pkg/vendorplugin/vendors/openai", "os/exec", "net/http", "os", "time"} {
		t.Run(dependency, func(t *testing.T) {
			sources := map[string]map[string]string{
				modulePath + "/pkg/vendorplugin/benchdata":        {"entry.go": "package benchdata\nimport _ \"" + modulePath + "/pkg/vendorplugin/benchdata/helper\"\n"},
				modulePath + "/pkg/vendorplugin/benchdata/helper": {"helper_windows.go": "//go:build future_tag && windows\n\npackage helper\nimport capability \"" + dependency + "\"\nvar _ = capability.X\n"},
			}
			problems, _, err := inspectClosure(modulePath+"/pkg/vendorplugin/benchdata", func(path string) (map[string]string, error) {
				if files, ok := sources[path]; ok {
					return files, nil
				}
				return nil, fmt.Errorf("unexpected traversal %q", path)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(problems, "\n"), "forbidden dependency: "+dependency) {
				t.Fatalf("transitive tagged capability admitted: %v", problems)
			}
		})
	}
}

func TestImportClosureRejectsTaggedInitAndMissingScope(t *testing.T) {
	problems, _, err := inspectClosure(modulePath+"/pkg/vendorplugin/benchdata", func(string) (map[string]string, error) {
		return map[string]string{"hidden.go": "//go:build future_tag\n\npackage benchdata\nfunc init() {}\n"}, nil
	})
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "init function") {
		t.Fatalf("tagged init admitted: %v, %v", problems, err)
	}
	_, _, err = scanClosure(t.TempDir(), modulePath+"/pkg/vendorplugin/benchdata")
	if err == nil {
		t.Fatal("missing source scope admitted")
	}
}
