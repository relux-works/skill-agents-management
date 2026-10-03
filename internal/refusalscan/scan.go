// Package refusalscan discovers typed refusal return sites across the agentic
// production package tree. It is shared by the package coverage guard and the
// isolated mutation runner so both use the same source-derived set.
//
// This is the third review round. After this revision the guard's accepted
// subset is FROZEN as: rework-brief-03 + rework-brief-04 + rework-brief-06.
// Any further construction that is NOT present in the module's production or
// test code goes to an "out of contract / future hardening" list in results.md,
// with the rule clause that excludes it. It is not a blocking finding.
// Only constructions that actually occur in this repository, or a regression
// in what the subset promises, block.
//
// In particular, derived values cannot be assigned to fields, indices or
// dereferenced targets. Local non-error conversions remain allowed, provided
// they never become errors again. Generics-instantiated helpers and reflection
// calls are outside the accepted subset; this is an intraprocedural checker.
package refusalscan

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
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
func Discover(root string) ([]Site, error) { return discover(root, "", "") }

// DiscoverType uses the same go/types fixed-point propagation as Discover,
// starting from one named error type rather than all module error symbols.
// It never matches call spelling or constructor names.
func DiscoverType(root, packageID, typeName string) ([]Site, error) {
	return discover(root, packageID, typeName)
}

func discover(root, packageID, typeName string) ([]Site, error) {
	sourceFiles, err := SourceFiles(root)
	if err != nil {
		return nil, err
	}
	files, err := parsePackageSources(root, sourceFiles)
	if err != nil {
		return nil, err
	}
	errors, errorTypes := refusalSymbols(files)
	if typeName != "" {
		errors.objects = make(map[types.Object]bool)
		selected := make(map[types.Object]bool)
		for object := range errorTypes.objects {
			if object.Pkg() != nil && object.Pkg().Path() == packageID && object.Name() == typeName {
				selected[object] = true
			}
		}
		if len(selected) != 1 {
			return nil, fmt.Errorf("refusalscan: named error type not uniquely resolved: %s.%s", packageID, typeName)
		}
		errorTypes.objects = selected
	}
	helpers := refusalHelpers(files, errors, errorTypes)
	if err := validateRefusalUseShapes(files, errors, errorTypes, helpers); err != nil {
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
			if errorImplementation(function, errors.info) {
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

func isRefusalReturnResult(statement *ast.ReturnStmt, results *ast.FieldList, index int, expression ast.Expr, stack []ast.Node, errors, errorTypes, helpers symbolSet) bool {
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
	return ok && helpers.has(call.Fun)
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

func expressionContainsIdentifier(expression ast.Expr, object types.Object, info *types.Info) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if found || node == nil {
			return !found
		}
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if identifier, ok := node.(*ast.Ident); ok && object != nil && info.ObjectOf(identifier) == object {
			found = true
			return false
		}
		return true
	})
	return found
}

func markRefusalReferences(allowed map[token.Pos]bool, node ast.Node, errors, errorTypes, helpers symbolSet) {
	ast.Inspect(node, func(candidate ast.Node) bool {
		if position, ok := typedRefusalReference(candidate, errors, errorTypes, helpers); ok {
			allowed[position] = true
		}
		return true
	})
}

func containsTypedRefusal(node ast.Node, errors, errorTypes, helpers symbolSet) bool {
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

func firstTypedRefusalPosition(node ast.Node, errors, errorTypes, helpers symbolSet) token.Pos {
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

func typedRefusalReference(node ast.Node, errors, errorTypes, helpers symbolSet) (token.Pos, bool) {
	switch typed := node.(type) {
	case *ast.Ident:
		if errors.has(typed) || helpers.has(typed) {
			return typed.Pos(), true
		}
	case *ast.CallExpr:
		if errorTypes.has(typed.Fun) {
			return typed.Pos(), true
		}
	case *ast.CompositeLit:
		if errorTypes.has(typed.Type) {
			return typed.Pos(), true
		}
	}
	return token.NoPos, false
}

func refusalSymbols(files map[string]*parsedSource) (symbolSet, symbolSet) {
	info := symbolInfo(files)
	errors, errorTypes := newSymbolSet(info), newSymbolSet(info)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range general.Specs {
				switch value := spec.(type) {
				case *ast.ValueSpec:
					if general.Tok == token.VAR || general.Tok == token.CONST {
						for _, name := range value.Names {
							object := info.Defs[name]
							if object != nil && implementsError(object.Type()) {
								errors.objects[object] = true
							}
						}
					}
				case *ast.TypeSpec:
					object := info.Defs[value.Name]
					if object != nil && (implementsError(object.Type()) || implementsError(types.NewPointer(object.Type()))) {
						errorTypes.objects[object] = true
					}
				}
			}
		}
	}
	return errors, errorTypes
}

func refusalHelpers(files map[string]*parsedSource, errors, errorTypes symbolSet) symbolSet {
	functions := make([]*ast.FuncDecl, 0)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				functions = append(functions, function)
			}
		}
	}
	helpers := newSymbolSet(symbolInfo(files))
	changed := true
	for changed {
		changed = interfaceRefusalHelpers(files, helpers)
		for _, function := range functions {
			if functionReturnsRefusal(function, errors, errorTypes, helpers) {
				if !helpers.objects[helpers.info.ObjectOf(function.Name)] {
					helpers.objects[helpers.info.ObjectOf(function.Name)] = true
					changed = true
				}
			}
		}
	}
	return helpers
}

func functionReturnsRefusal(function *ast.FuncDecl, errors, errorTypes, helpers symbolSet) bool {
	if errorImplementation(function, errors.info) {
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

func refusalResultType(results *ast.FieldList, resultIndex int, errorTypes symbolSet) bool {
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

func errorResultType(expression ast.Expr, errorTypes symbolSet) bool {
	return implementsError(errorTypes.info.TypeOf(expression))
}

func containsRefusalExpression(expression ast.Expr, errors, errorTypes, helpers symbolSet) bool {
	return containsTypedRefusal(expression, errors, errorTypes, helpers)
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
	errors       symbolSet
	types        symbolSet
	helpers      symbolSet
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
			v.stack = v.stack[:len(v.stack)-1]
			return nil
		}
		line := v.fset.Position(literal.Pos()).Line
		nested := returnVisitor{
			fset: v.fset, file: v.file, function: fmt.Sprintf("%s.func-literal@%d", v.function, line),
			errors: v.errors, types: v.types, helpers: v.helpers, ordinals: v.ordinals, sites: v.sites,
			body: literal.Body, resultTypes: literal.Type.Results,
		}
		ast.Walk(&nested, literal.Body)
		v.stack = v.stack[:len(v.stack)-1]
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

func returnExpressionIsRefusal(expression ast.Expr, stack []ast.Node, errors, errorTypes, helpers symbolSet) bool {
	if containsRefusalExpression(expression, errors, errorTypes, helpers) {
		return true
	}
	identifiers := make(map[types.Object]bool)
	ast.Inspect(expression, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			identifiers[errors.info.ObjectOf(identifier)] = true
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
			if !ok || !identifiers[errors.info.ObjectOf(identifier)] {
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
