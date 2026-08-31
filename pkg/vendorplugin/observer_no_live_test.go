package vendorplugin

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var observerBoundaryRequiredTestFiles = []string{
	"engine_launch_test.go",
	"observer_api_test.go",
	"observer_api_external_test.go",
}

var observerBoundaryAllowedImports = map[string]bool{
	"context": true, "crypto/sha256": true, "encoding/hex": true,
	"errors": true, "fmt": true, "math": true, "math/rand": true,
	"reflect": true, "regexp": true, "sort": true, "strings": true, "sync": true,
	"testing": true, "time": true,
	"github.com/relux-works/skill-agents-management/internal/ident":      true,
	"github.com/relux-works/skill-agents-management/pkg/agentic":         true,
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine": true,
	"github.com/relux-works/skill-agents-management/pkg/plugin":          true,
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin":    true,
}

// TestObserverBoundaryHasNoLiveProbeSurface keeps this task's production and
// attack tests static/fake. Concrete status/process/socket/model reads belong
// to the agents-infra adapter, not this module or its observer tests.
func TestObserverBoundaryHasNoLiveProbeSurface(t *testing.T) {
	productionFiles := observerBoundaryProductionFiles(t, ".")
	requiredFiles := append(append([]string(nil), productionFiles...), observerBoundaryRequiredTestFiles...)
	violations := observerBoundaryViolations(t, ".", requiredFiles, requiredFiles)
	if len(violations) != 0 {
		t.Fatalf("observer production boundary permits live probes: %v", violations)
	}
}

// The review mutant narrows only the composed scan scope while leaving the
// live-probe rules intact. Omitting either real production entry must make the
// guard itself red instead of silently reducing what "no live probes" means.
func TestObserverBoundaryGuardRejectsNarrowedProductionScope(t *testing.T) {
	productionFiles := observerBoundaryProductionFiles(t, ".")
	requiredFiles := append(append([]string(nil), productionFiles...), observerBoundaryRequiredTestFiles...)
	for _, omitted := range []string{"spawn.go", "registry.go"} {
		t.Run(omitted, func(t *testing.T) {
			var narrowed []string
			for _, name := range requiredFiles {
				if name != omitted {
					narrowed = append(narrowed, name)
				}
			}
			violations := observerBoundaryViolations(t, ".", narrowed, requiredFiles)
			if len(violations) != 1 || !strings.Contains(violations[0], omitted) {
				t.Fatalf("narrowed scope admitted: %v", violations)
			}
		})
	}
}

// This is the exact called-helper reviewer mutant. A newly added production
// file is discovered without being named by the guard, and its aliased
// user-config read is rejected even though the observer entry contains only a
// same-package helper call.
func TestObserverBoundaryGuardRejectsCalledProductionHelperUserConfigRead(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"engine.go": `package vendorplugin
func resolveEngineObservations() { reviewerLiveProbe() }
`,
		"observer_live.go": `package vendorplugin
import hostos "os"
func reviewerLiveProbe() { _, _ = hostos.ReadFile("/user/config") }
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatalf("write static mutant %s: %v", name, err)
		}
	}

	productionFiles := observerBoundaryProductionFiles(t, dir)
	violations := observerBoundaryViolations(t, dir, productionFiles, productionFiles)
	if len(violations) == 0 || !strings.Contains(strings.Join(violations, " "), "observer_live.go imports os") {
		t.Fatalf("called production-helper user-config read bypass admitted: %v", violations)
	}
}

// This is the exact reviewer mutant: the production observer reaches into the
// user's agents-infra config with os.ReadFile. The import allowlist must make
// that narrower live-access bypass red without relying on the qualifier name.
func TestObserverBoundaryGuardRejectsUserConfigRead(t *testing.T) {
	source := []byte(`package vendorplugin
import hostos "os"
func resolveEngineObservations() { _, _ = hostos.ReadFile(hostos.Getenv("HOME") + "/.config/agents-infra/config.toml") }
`)
	violations := observerFileViolations(t, "engine.go", source)
	if len(violations) == 0 || !strings.Contains(strings.Join(violations, " "), "imports os") {
		t.Fatalf("user-config read bypass admitted: %v", violations)
	}
}

func observerBoundaryProductionFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read observer package %s: %v", dir, err)
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, name)
		}
	}
	return files
}

func observerBoundaryViolations(t *testing.T, dir string, files, requiredFiles []string) []string {
	t.Helper()
	included := make(map[string]bool, len(files))
	for _, name := range files {
		included[name] = true
	}
	var violations []string
	for _, required := range requiredFiles {
		if !included[required] {
			violations = append(violations, fmt.Sprintf("scope omits %s", required))
		}
	}
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		violations = append(violations, observerParsedFileViolations(t, name, file)...)
	}
	return violations
}

func observerFileViolations(t *testing.T, name string, source []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return observerParsedFileViolations(t, name, file)
}

func observerParsedFileViolations(t *testing.T, name string, file *ast.File) []string {
	t.Helper()
	var violations []string
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("unquote import in %s: %v", name, err)
		}
		if !observerBoundaryAllowedImports[path] {
			violations = append(violations, fmt.Sprintf("%s imports %s", name, path))
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		key := owner.Name + "." + selector.Sel.Name
		switch key {
		case "exec.Command", "exec.CommandContext", "net.Dial", "net.DialTimeout", "http.Get", "http.Post", "os.FindProcess", "os.StartProcess", "syscall.Kill":
			violations = append(violations, fmt.Sprintf("%s calls %s", name, key))
		}
		return true
	})
	return violations
}
