// Package refusalscan discovers typed refusal return sites across the agentic
// production package tree. It is shared by the package coverage guard and the
// isolated mutation runner so both use the same source-derived set.
package refusalscan

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const packagePath = "pkg/agentic"

// Site identifies one typed refusal return in a production file.
// File is relative to pkg/agentic. Occurrence distinguishes identical return
// expressions under the same guard without relying on line numbers.
type Site struct {
	File       string
	Function   string
	Guard      string
	Return     string
	Occurrence int
	Line       int
}

func (s Site) Locator() string {
	return s.File + " :: " + s.Function + " :: " + s.Guard
}

func (s Site) Key() string {
	return fmt.Sprintf("%s :: %s :: occurrence %d", s.Locator(), s.Return, s.Occurrence)
}

// SourceFiles returns every non-test Go source file below pkg/agentic. The
// walk starts at the module root and does not depend on Git state, so the same
// set is available in working trees, module caches, and clean checkouts.
func SourceFiles(root string) ([]string, error) {
	packageRoot := filepath.Join(root, filepath.FromSlash(packagePath))
	var result []string
	err := filepath.WalkDir(packageRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		isGoSource := strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go")
		if entry.Type()&fs.ModeSymlink != 0 {
			if isGoSource {
				return fmt.Errorf("refusalscan: symlinked Go source file is not scanned: %s", path)
			}
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("refusalscan: cannot determine symlink target kind for %s: %w", path, err)
			}
			if info.IsDir() {
				return fmt.Errorf("refusalscan: symlinked directory is not scanned: %s", path)
			}
			return nil
		}
		if entry.IsDir() || !isGoSource {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("refusalscan: inspect Go source entry %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusalscan: non-regular Go source entry is not scanned: %s (%s)", path, info.Mode().Type())
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result = append(result, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk agentic source files from module root: %w", err)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil, fmt.Errorf("no non-test Go source under %s", packagePath)
	}
	return result, nil
}

// Discover enumerates every typed refusal return in every function in every
// non-test Go source file below pkg/agentic. Refusal symbols come from package
// declarations and from the returned expressions themselves.
func Discover(root string) ([]Site, error) {
	sourceFiles, err := SourceFiles(root)
	if err != nil {
		return nil, err
	}
	files, err := parsePackageSources(root, sourceFiles)
	if err != nil {
		return nil, err
	}
	errors, errorTypes := refusalSymbols(files)
	helpers := refusalHelpers(files, errors, errorTypes)
	constructors := refusalConstructors(files, helpers, errors, errorTypes)
	if err := validateRefusalUseShapes(files, errors, errorTypes, constructors); err != nil {
		return nil, err
	}

	var sites []Site
	for _, relative := range sourceFiles {
		parsed := files[relative]
		for _, declaration := range parsed.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			if function.Recv != nil && (function.Name.Name == "Error" || function.Name.Name == "Unwrap") {
				continue
			}
			name := declaredFunctionName(function)
			visitor := returnVisitor{
				fset: parsed.fset, file: strings.TrimPrefix(relative, packagePath+"/"),
				function: name, errors: errors, types: errorTypes, helpers: helpers,
				body: function.Body, resultTypes: function.Type.Results,
				ordinals: make(map[string]int), sites: &sites,
			}
			ast.Walk(&visitor, function.Body)
		}
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		if sites[i].Line != sites[j].Line {
			return sites[i].Line < sites[j].Line
		}
		if sites[i].Function != sites[j].Function {
			return sites[i].Function < sites[j].Function
		}
		return sites[i].Return < sites[j].Return
	})
	ordinals := make(map[string]int)
	for index := range sites {
		locator := sites[index].Locator() + " :: " + sites[index].Return
		sites[index].Occurrence = ordinals[locator]
		ordinals[locator]++
	}
	return sites, nil
}

func refusalConstructors(files map[string]*parsedSource, helpers, errors, errorTypes map[string]bool) map[string]bool {
	result := make(map[string]bool)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || !helpers[function.Name.Name] {
				continue
			}
			name := strings.ToLower(function.Name.Name)
			if strings.Contains(name, "refusal") || strings.Contains(name, "error") ||
				strings.Contains(name, "malformed") || strings.Contains(name, "invalid") || strings.Contains(name, "conflict") {
				result[function.Name.Name] = true
				continue
			}
			if len(function.Body.List) == 1 {
				if returned, ok := function.Body.List[0].(*ast.ReturnStmt); ok && len(returned.Results) == 1 {
					if containsRefusalExpression(returned.Results[0], errors, errorTypes, nil) {
						result[function.Name.Name] = true
					}
				}
			}
		}
	}
	return result
}

// validateRefusalUseShapes keeps the source recognizer's accepted subset
// deliberately small: a typed refusal symbol may occur inside a return
// expression, or on the right-hand side of an if-init that returns that local
// from the if body. Aliasing and storing a refusal before a later return would
// make the site inventory depend on data-flow guesses, so those shapes fail
// closed with their source location.
func validateRefusalUseShapes(files map[string]*parsedSource, errors, errorTypes, helpers map[string]bool) error {
	for relative, source := range files {
		allowed := make(map[token.Pos]bool)
		for _, declaration := range source.file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if typed.Body == nil {
					continue
				}
				markAllowedFunctionRefusals(allowed, typed.Body, typed.Type.Results, errors, errorTypes, helpers)
			case *ast.GenDecl:
				for _, specification := range typed.Specs {
					values, ok := specification.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, value := range values.Values {
						if containsTypedRefusal(value, errors, errorTypes, helpers) {
							if position := firstTypedRefusalPosition(value, errors, errorTypes, helpers); position.IsValid() {
								return fmt.Errorf("refusalscan: typed refusal use outside a return expression or refusal if-init at %s:%d", relative, source.fset.Position(position).Line)
							}
						}
					}
				}
			}
		}

		for _, declaration := range source.file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if typed.Body != nil {
					if position := firstForbiddenTypedRefusalPosition(typed.Body, allowed, errors, errorTypes, helpers); position.IsValid() {
						return fmt.Errorf("refusalscan: typed refusal use outside a return expression or refusal if-init at %s:%d", relative, source.fset.Position(position).Line)
					}
				}
			case *ast.GenDecl:
				for _, specification := range typed.Specs {
					values, ok := specification.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, value := range values.Values {
						if position := firstForbiddenTypedRefusalPosition(value, allowed, errors, errorTypes, helpers); position.IsValid() {
							return fmt.Errorf("refusalscan: typed refusal use outside a return expression or refusal if-init at %s:%d", relative, source.fset.Position(position).Line)
						}
					}
				}
			}
		}
	}
	return nil
}

func markAllowedFunctionRefusals(allowed map[token.Pos]bool, body *ast.BlockStmt, results *ast.FieldList, errors, errorTypes, helpers map[string]bool) {
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		if literal, ok := node.(*ast.FuncLit); ok {
			markAllowedFunctionRefusals(allowed, literal.Body, literal.Type.Results, errors, errorTypes, helpers)
			return false
		}
		switch statement := node.(type) {
		case *ast.ReturnStmt:
			for index, result := range statement.Results {
				if isRefusalReturnResult(statement, results, index, result, nil, errors, errorTypes, helpers) {
					markRefusalReferences(allowed, result, errors, errorTypes, helpers)
				}
			}
		case *ast.IfStmt:
			markAllowedIfInitRefusal(allowed, statement, errors, errorTypes, helpers)
		}
		return true
	})
}

func isRefusalReturnResult(statement *ast.ReturnStmt, results *ast.FieldList, index int, expression ast.Expr, stack []ast.Node, errors, errorTypes, helpers map[string]bool) bool {
	if !returnExpressionIsRefusal(expression, stack, errors, errorTypes, helpers) {
		return false
	}
	if refusalResultType(results, index, errorTypes) {
		return true
	}
	if index != 0 || len(statement.Results) != 1 || resultFieldCount(results) < 2 {
		return false
	}
	call, ok := expression.(*ast.CallExpr)
	return ok && helpers[callName(call)]
}

func resultFieldCount(results *ast.FieldList) int {
	if results == nil {
		return 0
	}
	count := 0
	for _, field := range results.List {
		width := len(field.Names)
		if width == 0 {
			width = 1
		}
		count += width
	}
	return count
}

func firstForbiddenTypedRefusalPosition(root ast.Node, allowed map[token.Pos]bool, errors, errorTypes, helpers map[string]bool) token.Pos {
	var result token.Pos
	ast.Inspect(root, func(node ast.Node) bool {
		if result.IsValid() {
			return false
		}
		if position, ok := typedRefusalReference(node, errors, errorTypes, helpers); ok && !allowed[position] {
			result = position
			return false
		}
		return true
	})
	return result
}

func markAllowedIfInitRefusal(allowed map[token.Pos]bool, branch *ast.IfStmt, errors, errorTypes, helpers map[string]bool) {
	assignment, ok := branch.Init.(*ast.AssignStmt)
	if !ok {
		return
	}
	for leftIndex, left := range assignment.Lhs {
		identifier, ok := left.(*ast.Ident)
		if !ok || !blockReturnsIdentifier(branch.Body, identifier.Name) {
			continue
		}
		rightIndex := leftIndex
		if len(assignment.Rhs) == 1 && len(assignment.Lhs) > 1 {
			rightIndex = 0
		}
		if rightIndex < len(assignment.Rhs) && containsTypedRefusal(assignment.Rhs[rightIndex], errors, errorTypes, helpers) {
			markRefusalReferences(allowed, assignment.Rhs[rightIndex], errors, errorTypes, helpers)
		}
	}
}

func blockReturnsIdentifier(block *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(block, func(node ast.Node) bool {
		if found || node == nil {
			return !found
		}
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		returned, ok := node.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range returned.Results {
			if expressionContainsIdentifier(result, name) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func expressionContainsIdentifier(expression ast.Expr, name string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if found || node == nil {
			return !found
		}
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if identifier, ok := node.(*ast.Ident); ok && identifier.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func markRefusalReferences(allowed map[token.Pos]bool, node ast.Node, errors, errorTypes, helpers map[string]bool) {
	ast.Inspect(node, func(candidate ast.Node) bool {
		if position, ok := typedRefusalReference(candidate, errors, errorTypes, helpers); ok {
			allowed[position] = true
		}
		return true
	})
}

func containsTypedRefusal(node ast.Node, errors, errorTypes, helpers map[string]bool) bool {
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if _, ok := typedRefusalReference(candidate, errors, errorTypes, helpers); ok {
			found = true
			return false
		}
		return !found
	})
	return found
}

func firstTypedRefusalPosition(node ast.Node, errors, errorTypes, helpers map[string]bool) token.Pos {
	var result token.Pos
	ast.Inspect(node, func(candidate ast.Node) bool {
		if result.IsValid() {
			return false
		}
		if position, ok := typedRefusalReference(candidate, errors, errorTypes, helpers); ok {
			result = position
			return false
		}
		return true
	})
	return result
}

func typedRefusalReference(node ast.Node, errors, errorTypes, helpers map[string]bool) (token.Pos, bool) {
	switch typed := node.(type) {
	case *ast.Ident:
		if errors[typed.Name] || errorTypes[typed.Name] || helpers[typed.Name] || strings.HasPrefix(typed.Name, "ErrCurator") || strings.HasSuffix(typed.Name, "Refusal") {
			if typed.Name != "" {
				return typed.Pos(), true
			}
		}
	case *ast.SelectorExpr:
		name := typed.Sel.Name
		if errors[name] || errorTypes[name] || helpers[name] || strings.HasPrefix(name, "ErrCurator") || strings.HasSuffix(name, "Refusal") {
			return typed.Sel.Pos(), true
		}
	case *ast.CallExpr:
		if helpers[callName(typed)] {
			return typed.Pos(), true
		}
	}
	return token.NoPos, false
}

type parsedSource struct {
	fset *token.FileSet
	file *ast.File
}

func parsePackageSources(root string, sourceFiles []string) (map[string]*parsedSource, error) {
	result := make(map[string]*parsedSource)
	for _, relative := range sourceFiles {
		parsed, err := parseSource(root, relative)
		if err != nil {
			return nil, err
		}
		result[relative] = parsed
	}
	return result, nil
}

func parseSource(root, relative string) (*parsedSource, error) {
	path := filepath.Join(root, filepath.FromSlash(relative))
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse source %s: %w", relative, err)
	}
	return &parsedSource{fset: fset, file: parsed}, nil
}

func refusalSymbols(files map[string]*parsedSource) (map[string]bool, map[string]bool) {
	errors := make(map[string]bool)
	errorTypes := make(map[string]bool)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			switch typed := declaration.(type) {
			case *ast.GenDecl:
				for _, spec := range typed.Specs {
					switch value := spec.(type) {
					case *ast.ValueSpec:
						if typed.Tok == token.VAR {
							for _, name := range value.Names {
								if strings.HasPrefix(name.Name, "ErrCurator") {
									errors[name.Name] = true
								}
							}
						}
					case *ast.TypeSpec:
						if strings.HasSuffix(value.Name.Name, "Refusal") || strings.HasSuffix(value.Name.Name, "Error") {
							errorTypes[value.Name.Name] = true
						}
					}
				}
			}
		}
	}
	return errors, errorTypes
}

func refusalHelpers(files map[string]*parsedSource, errors, errorTypes map[string]bool) map[string]bool {
	functions := make([]*ast.FuncDecl, 0)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				functions = append(functions, function)
			}
		}
	}
	helpers := make(map[string]bool)
	changed := true
	for changed {
		changed = false
		for _, function := range functions {
			if functionReturnsRefusal(function, errors, errorTypes, helpers) {
				if !helpers[function.Name.Name] {
					helpers[function.Name.Name] = true
					changed = true
				}
			}
		}
	}
	return helpers
}

func functionReturnsRefusal(function *ast.FuncDecl, errors, errorTypes, helpers map[string]bool) bool {
	if function.Recv != nil && (function.Name.Name == "Error" || function.Name.Name == "Unwrap") {
		return false
	}
	found := false
	visitor := returnVisitor{
		function: function.Name.Name, errors: errors, types: errorTypes, helpers: helpers,
		body: function.Body, resultTypes: function.Type.Results, found: &found, skipLiterals: true,
	}
	ast.Walk(&visitor, function.Body)
	return found
}

func refusalResultType(results *ast.FieldList, resultIndex int, errorTypes map[string]bool) bool {
	if results == nil {
		return false
	}
	index := 0
	for _, result := range results.List {
		width := len(result.Names)
		if width == 0 {
			width = 1
		}
		if resultIndex >= index && resultIndex < index+width {
			return errorResultType(result.Type, errorTypes)
		}
		index += width
	}
	return false
}

func errorResultType(expression ast.Expr, errorTypes map[string]bool) bool {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name == "error" || errorTypes[typed.Name] || strings.HasSuffix(typed.Name, "Refusal")
	case *ast.StarExpr:
		return errorResultType(typed.X, errorTypes)
	case *ast.SelectorExpr:
		return errorTypes[typed.Sel.Name] || strings.HasSuffix(typed.Sel.Name, "Refusal") || strings.HasSuffix(typed.Sel.Name, "Error")
	case *ast.ParenExpr:
		return errorResultType(typed.X, errorTypes)
	default:
		return false
	}
}

func containsRefusalExpression(expression ast.Expr, errors, errorTypes, helpers map[string]bool) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		switch typed := node.(type) {
		case *ast.Ident:
			found = errors[typed.Name] || errorTypes[typed.Name] || strings.HasSuffix(typed.Name, "Refusal") || strings.HasSuffix(typed.Name, "Error")
		case *ast.SelectorExpr:
			name := typed.Sel.Name
			found = errors[name] || errorTypes[name] || strings.HasPrefix(name, "ErrCurator") || strings.HasSuffix(name, "Refusal") || strings.HasSuffix(name, "Error")
		case *ast.CallExpr:
			found = helpers[callName(typed)]
		}
		return !found
	})
	return found
}

func callName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		return function.Sel.Name
	default:
		return ""
	}
}

func declaredFunctionName(function *ast.FuncDecl) string {
	if function.Recv == nil {
		return function.Name.Name
	}
	if receiver := receiverTypeName(function.Recv); receiver != "" {
		return receiver + "." + function.Name.Name
	}
	return function.Name.Name
}

func receiverTypeName(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	typeName := fields.List[0].Type
	if pointer, ok := typeName.(*ast.StarExpr); ok {
		typeName = pointer.X
	}
	if identifier, ok := typeName.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

type returnVisitor struct {
	fset         *token.FileSet
	file         string
	function     string
	errors       map[string]bool
	types        map[string]bool
	helpers      map[string]bool
	body         *ast.BlockStmt
	resultTypes  *ast.FieldList
	ordinals     map[string]int
	sites        *[]Site
	found        *bool
	skipLiterals bool
	stack        []ast.Node
}

func (v *returnVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		v.stack = v.stack[:len(v.stack)-1]
		return nil
	}
	v.stack = append(v.stack, node)
	if literal, ok := node.(*ast.FuncLit); ok {
		if v.skipLiterals {
			return nil
		}
		line := v.fset.Position(literal.Pos()).Line
		nested := returnVisitor{
			fset: v.fset, file: v.file, function: fmt.Sprintf("%s.func-literal@%d", v.function, line),
			errors: v.errors, types: v.types, helpers: v.helpers, ordinals: v.ordinals, sites: v.sites,
			body: literal.Body, resultTypes: literal.Type.Results,
		}
		ast.Walk(&nested, literal.Body)
		return nil
	}
	returned, ok := node.(*ast.ReturnStmt)
	if !ok || len(returned.Results) == 0 {
		return v
	}
	refusal := false
	for index, result := range returned.Results {
		if isRefusalReturnResult(returned, v.resultTypes, index, result, v.stack, v.errors, v.types, v.helpers) {
			refusal = true
			break
		}
	}
	if !refusal {
		return v
	}
	if v.found != nil {
		*v.found = true
		return v
	}
	guard := guardForStack(v.fset, v.stack)
	returnedText := normalized(v.fset, returned)
	locator := v.file + " :: " + v.function + " :: " + guard + " :: " + returnedText
	occurrence := v.ordinals[locator]
	v.ordinals[locator]++
	*v.sites = append(*v.sites, Site{
		File: v.file, Function: v.function, Guard: guard, Return: returnedText,
		Occurrence: occurrence, Line: v.fset.Position(returned.Pos()).Line,
	})
	return v
}

func returnExpressionIsRefusal(expression ast.Expr, stack []ast.Node, errors, errorTypes, helpers map[string]bool) bool {
	if containsRefusalExpression(expression, errors, errorTypes, helpers) {
		return true
	}
	identifiers := make(map[string]bool)
	ast.Inspect(expression, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			identifiers[identifier.Name] = true
		}
		return true
	})
	for index := len(stack) - 2; index >= 0; index-- {
		branch, ok := stack[index].(*ast.IfStmt)
		if !ok || branch.Init == nil {
			continue
		}
		assignment, ok := branch.Init.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for leftIndex, left := range assignment.Lhs {
			identifier, ok := left.(*ast.Ident)
			if !ok || !identifiers[identifier.Name] {
				continue
			}
			rightIndex := leftIndex
			if len(assignment.Rhs) == 1 && len(assignment.Lhs) > 1 {
				rightIndex = 0
			}
			if rightIndex < len(assignment.Rhs) && containsRefusalExpression(assignment.Rhs[rightIndex], errors, errorTypes, helpers) {
				return true
			}
		}
	}
	return false
}

func guardForStack(fset *token.FileSet, stack []ast.Node) string {
	for index := len(stack) - 2; index >= 0; index-- {
		switch parent := stack[index].(type) {
		case *ast.IfStmt:
			return "if " + normalized(fset, parent.Cond)
		case *ast.CaseClause:
			tag := "<expression>"
			for parentIndex := index - 1; parentIndex >= 0; parentIndex-- {
				if statement, ok := stack[parentIndex].(*ast.SwitchStmt); ok {
					if statement.Tag != nil {
						tag = normalized(fset, statement.Tag)
					}
					break
				}
			}
			members := "default"
			if len(parent.List) > 0 {
				values := make([]string, 0, len(parent.List))
				for _, expression := range parent.List {
					values = append(values, normalized(fset, expression))
				}
				members = strings.Join(values, ", ")
			}
			return "switch " + tag + " case " + members
		}
	}
	return "unconditional"
}

func normalized(fset *token.FileSet, node any) string {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, fset, node); err != nil {
		return fmt.Sprintf("<format-error:%v>", err)
	}
	return strings.Join(strings.Fields(buffer.String()), " ")
}
