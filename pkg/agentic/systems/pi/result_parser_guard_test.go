package pi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTurnResultHasOneProductionJSONParser(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sources[entry.Name()] = string(body)
	}
	violations := resultParserViolations(t, sources)
	if len(violations) != 0 {
		t.Fatalf("schema-1 parser bypasses ValidateTurnResult: %v", violations)
	}
}

// This narrowed mutant adds a second permissive production parser while
// retaining the authoritative parser. The guard must reject that bypass shape;
// deleting ValidateTurnResult entirely is not the mutation being proved.
func TestTurnResultParserGuardRejectsNarrowPermissiveBypass(t *testing.T) {
	tests := map[string]string{
		"ordinary import": `package pi
import "encoding/json"
func consumeProcessAResult(b []byte) { var v any; _ = json.Unmarshal(b, &v) }
`,
		"aliased import": `package pi
import j "encoding/json"
func consumeProcessAResult(b []byte) { var v any; _ = j.Unmarshal(b, &v) }
`,
		"dot import": `package pi
import . "encoding/json"
func consumeProcessAResult(b []byte) { var v any; _ = Unmarshal(b, &v) }
`,
	}
	for name, consumer := range tests {
		t.Run(name, func(t *testing.T) {
			sources := map[string]string{
				"result.go": `package pi
import "encoding/json"
func decodeTurnResultDocument([]byte) { json.NewDecoder(nil) }
`,
				"consumer.go": consumer,
			}
			violations := resultParserViolations(t, sources)
			if len(violations) != 1 || !strings.Contains(violations[0], "consumer.go") {
				t.Fatalf("narrow permissive bypass admitted: %v", violations)
			}
		})
	}
}

func resultParserViolations(t *testing.T, sources map[string]string) []string {
	t.Helper()
	var violations []string
	for name, source := range sources {
		file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		jsonQualifiers := map[string]bool{}
		dotJSON := false
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			if importPath != "encoding/json" {
				continue
			}
			qualifier := path.Base(importPath)
			if imported.Name != nil {
				qualifier = imported.Name.Name
			}
			if qualifier == "." {
				dotJSON = true
			} else if qualifier != "_" {
				jsonQualifiers[qualifier] = true
			}
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				callName := ""
				switch called := call.Fun.(type) {
				case *ast.SelectorExpr:
					owner, ok := called.X.(*ast.Ident)
					if ok && jsonQualifiers[owner.Name] {
						callName = called.Sel.Name
					}
				case *ast.Ident:
					if dotJSON {
						callName = called.Name
					}
				}
				if callName != "NewDecoder" && callName != "Unmarshal" {
					return true
				}
				if name != "result.go" || function.Name.Name != "decodeTurnResultDocument" || callName != "NewDecoder" {
					violations = append(violations, name+":"+function.Name.Name+":"+callName)
				}
				return true
			})
		}
	}
	return violations
}
