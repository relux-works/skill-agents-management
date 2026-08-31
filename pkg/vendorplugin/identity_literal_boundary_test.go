package vendorplugin

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This boundary is intentionally small and honest. It catches exact shipped
// identity literals placed in generic-core production declarations. Values
// assembled at runtime, imported, decoded, reflected, or otherwise obfuscated
// remain review concerns; production-entry metamorphic tests own observable
// behavior regardless of its source representation.
var shippedCoreIdentities = map[string]struct{}{
	"pi":                    {},
	"local-models":          {},
	"local-qwen":            {},
	"alibaba":               {},
	"qwen":                  {},
	"mlx":                   {},
	"qwen-3.8-27b-mlx-8bit": {},
}

type shippedLiteralSite struct {
	File        string
	Declaration string
	Literal     string
}

func (s shippedLiteralSite) String() string {
	return fmt.Sprintf("%s::%s contains shipped identity literal %q", s.File, s.Declaration, s.Literal)
}

var shippedLiteralAllowlist = map[shippedLiteralSite]struct{}{
	{File: "pkg/vendorplugin/runtime.go", Declaration: "var frozenRuntimes", Literal: "qwen"}:    {},
	{File: "pkg/vendorplugin/runtime.go", Declaration: "var frozenRuntimes", Literal: "alibaba"}: {},
	{File: "pkg/vendorplugin/v2snapshot.go", Declaration: "var v2RolloutTiers", Literal: "qwen"}: {},
}

func TestGenericCoreContainsNoMisplacedShippedIdentityLiterals(t *testing.T) {
	root := moduleRootForIdentityBoundary(t)
	sources, err := loadGenericCoreProductionSources(root)
	if err != nil {
		t.Fatalf("load generic-core production sources: %v", err)
	}
	violations, exercised, err := scanShippedIdentityLiteralPlacement(sources)
	if err != nil {
		t.Fatalf("scan shipped identity placement: %v", err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
	for allowed := range shippedLiteralAllowlist {
		if !exercised[allowed] {
			t.Errorf("stale shipped-identity exception was not exercised: %s", allowed)
		}
	}
}

func TestShippedIdentityLiteralBoundaryRejectsEveryListedIdentity(t *testing.T) {
	identities := make([]string, 0, len(shippedCoreIdentities))
	for identity := range shippedCoreIdentities {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		t.Run(identity, func(t *testing.T) {
			sources := map[string]string{
				"pkg/vendorplugin/mutant.go": "package vendorplugin\n\nvar misplaced = map[string]string{\"axis\": " + strconv.Quote(identity) + "}\n",
			}
			violations, _, err := scanShippedIdentityLiteralPlacement(sources)
			if err != nil {
				t.Fatalf("scan mutant: %v", err)
			}
			if len(violations) != 1 || violations[0].Literal != identity {
				t.Fatalf("exact shipped literal %q was admitted; violations=%v", identity, violations)
			}
		})
	}
}

func TestShippedIdentityLiteralBoundaryRejectsConcreteMapValuesWithoutInterpretingTheMap(t *testing.T) {
	sources := map[string]string{
		"pkg/vendorplugin/map_mutant.go": `package vendorplugin

var familyToEngine = map[string]string{"qwen": "mlx"}
`,
	}
	violations, _, err := scanShippedIdentityLiteralPlacement(sources)
	if err != nil {
		t.Fatalf("scan map mutant: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("concrete map values were admitted; violations=%v", violations)
	}
}

func loadGenericCoreProductionSources(root string) (map[string]string, error) {
	dirs := []string{"pkg/plugin", "pkg/inferenceengine", "pkg/vendorplugin"}
	sources := make(map[string]string)
	for _, relativeDir := range dirs {
		absoluteDir := filepath.Join(root, filepath.FromSlash(relativeDir))
		pkg, err := build.Default.ImportDir(absoluteDir, 0)
		if err != nil {
			return nil, fmt.Errorf("resolve build-owned files in %s: %w", relativeDir, err)
		}
		files := append(append([]string(nil), pkg.GoFiles...), pkg.CgoFiles...)
		sort.Strings(files)
		for _, name := range files {
			data, err := os.ReadFile(filepath.Join(absoluteDir, name))
			if err != nil {
				return nil, fmt.Errorf("read %s/%s: %w", relativeDir, name, err)
			}
			sources[relativeDir+"/"+name] = string(data)
		}
	}
	return sources, nil
}

func scanShippedIdentityLiteralPlacement(sources map[string]string) ([]shippedLiteralSite, map[shippedLiteralSite]bool, error) {
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var violations []shippedLiteralSite
	exercised := make(map[shippedLiteralSite]bool)
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, sources[path], 0)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, declaration := range file.Decls {
			name, node := enclosingDeclaration(declaration)
			if node == nil {
				continue
			}
			ast.Inspect(node, func(candidate ast.Node) bool {
				literal, ok := candidate.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					return true
				}
				if _, shipped := shippedCoreIdentities[value]; !shipped {
					return true
				}
				site := shippedLiteralSite{File: filepath.ToSlash(path), Declaration: name, Literal: value}
				if _, allowed := shippedLiteralAllowlist[site]; allowed {
					exercised[site] = true
					return true
				}
				violations = append(violations, site)
				return true
			})
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		if violations[i].Declaration != violations[j].Declaration {
			return violations[i].Declaration < violations[j].Declaration
		}
		return violations[i].Literal < violations[j].Literal
	})
	return violations, exercised, nil
}

func enclosingDeclaration(declaration ast.Decl) (string, ast.Node) {
	switch value := declaration.(type) {
	case *ast.FuncDecl:
		return "func " + value.Name.Name, value.Body
	case *ast.GenDecl:
		if len(value.Specs) != 1 {
			return value.Tok.String(), value
		}
		switch spec := value.Specs[0].(type) {
		case *ast.ValueSpec:
			names := make([]string, 0, len(spec.Names))
			for _, name := range spec.Names {
				names = append(names, name.Name)
			}
			return value.Tok.String() + " " + strings.Join(names, ","), spec
		case *ast.TypeSpec:
			return "type " + spec.Name.Name, spec
		}
		return value.Tok.String(), value
	default:
		return "", nil
	}
}

func moduleRootForIdentityBoundary(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above package directory")
		}
		dir = parent
	}
}
