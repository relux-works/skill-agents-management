// Command gen enumerates Boolean operands guarding typed local-provider
// refusals. It follows errors returned by validateLoopbackBaseURL because
// resolvePrivateProvider maps them to LocalProviderRefusal at provider.go:126.
//
// Run from the module root with:
//
//	go run ./tools/codex-provider-mutants > .temp/codex-provider-mutants.json
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

type mutation struct {
	ID      string `json:"id"`
	File    string `json:"file"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Line    int    `json:"line"`
	Operand string `json:"operand"`
	Source  string `json:"typed_refusal_source"`
}

func main() {
	fset := token.NewFileSet()
	paths := []string{
		"pkg/agentic/systems/codex/provider.go",
		"pkg/agentic/localprovider.go",
	}
	all := discoverMutations(fset, paths)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(all); err != nil {
		fatal(err)
	}
}

func discoverMutations(fset *token.FileSet, paths []string) []mutation {
	var all []mutation
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			fn, ok := node.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			ast.Inspect(fn.Body, func(candidate ast.Node) bool {
				stmt, ok := candidate.(*ast.IfStmt)
				if !ok {
					return true
				}
				source := ""
				switch {
				case fn.Name.Name == "validateLoopbackBaseURL":
					// Its errors are converted into typed local-provider
					// refusals by resolvePrivateProvider.
					source = "validateLoopbackBaseURL -> resolvePrivateProvider"
				case hasTypedRefusalReturn(stmt.Body):
					source = "typed LocalProviderRefusal return"
				default:
					return true
				}
				var operands []ast.Expr
				leaves(stmt.Cond, &operands)
				for index, operand := range operands {
					position := fset.Position(operand.Pos())
					all = append(all, mutation{
						ID:      fmt.Sprintf("M-%s-L%d-O%d", filepath.Base(path), fset.Position(stmt.If).Line, index+1),
						File:    path,
						Start:   fset.Position(operand.Pos()).Offset,
						End:     fset.Position(operand.End()).Offset,
						Line:    position.Line,
						Operand: formatNode(fset, operand),
						Source:  source,
					})
				}
				return true
			})
			return false
		})
	}
	return all
}

func hasTypedRefusalReturn(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			ast.Inspect(result, func(candidate ast.Node) bool {
				switch value := candidate.(type) {
				case *ast.CallExpr:
					if ident, ok := value.Fun.(*ast.Ident); ok && ident.Name == "localProviderRefusal" {
						found = true
					}
				case *ast.UnaryExpr:
					if value.Op == token.AND {
						if typ, ok := value.X.(*ast.CompositeLit); ok {
							if selector, ok := typ.Type.(*ast.SelectorExpr); ok && selector.Sel.Name == "LocalProviderRefusal" {
								found = true
							}
						}
					}
				}
				return !found
			})
		}
		return true
	})
	return found
}

func leaves(expr ast.Expr, out *[]ast.Expr) {
	if binary, ok := expr.(*ast.BinaryExpr); ok && (binary.Op == token.LAND || binary.Op == token.LOR) {
		leaves(binary.X, out)
		leaves(binary.Y, out)
		return
	}
	*out = append(*out, expr)
}

func formatNode(fset *token.FileSet, node ast.Node) string {
	start, end := fset.Position(node.Pos()).Offset, fset.Position(node.End()).Offset
	file := fset.File(node.Pos())
	body, err := os.ReadFile(file.Name())
	if err != nil {
		fatal(err)
	}
	return string(body[start:end])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
