//go:build unix

package providerlimits

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	processStartTimePackageImportPath = "github.com/relux-works/skill-agents-management/pkg/providerlimits"
	processStartTimeGuardMutantEnv    = "PROVIDERLIMITS_STARTTIME_GUARD_MUTANT"
)

type processStartTimeBuildVariant struct {
	goos   string
	goarch string
}

type listedGoPackage struct {
	ImportPath string
	Dir        string
	Export     string
	GoFiles    []string
	CgoFiles   []string
	Error      *struct{ Err string }
}

type checkedProviderlimitsFile struct {
	name string
	file *ast.File
}

type processStartTimeSourceRoot struct {
	node     ast.Node
	function string
	err      error
}

func newProcessStartTimeGuardTypesInfo() *types.Info {
	return &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
}

func typeResolvedProcessStartTimeErrorSites(
	t *testing.T,
	root string,
	sources map[string]string,
) ([]discoveredProcessStartTimeErrorSite, int, error) {
	t.Helper()
	// Darwin and Linux are the acceptance targets. FreeBSD and Windows select
	// the package's other Unix and non-Unix liveness files, so every platform
	// variant in providerlimits is type-checked after the source walk.
	variants := []processStartTimeBuildVariant{
		{goos: "darwin", goarch: "arm64"},
		{goos: "linux", goarch: "amd64"},
		{goos: "freebsd", goarch: "amd64"},
		{goos: "windows", goarch: "amd64"},
	}
	allSites := make(map[string]discoveredProcessStartTimeErrorSite)
	coveredSources := make(map[string]bool, len(sources))
	constructorCount := -1
	mutant := os.Getenv(processStartTimeGuardMutantEnv)
	for _, variant := range variants {
		files, info, fset, err := typeCheckProviderlimitsSources(root, sources, variant)
		if err != nil {
			return nil, 0, fmt.Errorf("type-check providerlimits for %s/%s: %w", variant.goos, variant.goarch, err)
		}
		for _, source := range files {
			coveredSources[source.name] = true
		}
		sites, gotConstructors, err := discoverTypedProcessStartTimeErrorSites(
			files,
			info,
			fset,
			mutant,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("discover typed errors for %s/%s: %w", variant.goos, variant.goarch, err)
		}
		if constructorCount < 0 {
			constructorCount = gotConstructors
		} else if constructorCount != gotConstructors {
			return nil, 0, fmt.Errorf("typed error constructor count differs by platform: %d vs %d", constructorCount, gotConstructors)
		}
		variantSites := make(map[string]discoveredProcessStartTimeErrorSite, len(sites))
		for _, site := range sites {
			key := site.position
			if processStartTimeGuardSkipsMutant(mutant, "duplicate-site-id") {
				// This mutant restores the old last-wins key so the regression test
				// proves the source-position enumeration is doing real work.
				key = fmt.Sprintf("%s:%s:%s", site.file, site.function, site.id)
			}
			variantSites[key] = site
		}
		if len(variantSites) != len(sites) && !processStartTimeGuardSkipsMutant(mutant, "duplicate-site-id") {
			return nil, 0, fmt.Errorf("typed error return enumeration count %d differs from source-position map size %d for %s/%s", len(sites), len(variantSites), variant.goos, variant.goarch)
		}
		for _, site := range variantSites {
			if previous, ok := allSites[site.position]; ok &&
				(previous.id != site.id || previous.kind != site.kind || previous.file != site.file || previous.function != site.function) {
				return nil, 0, fmt.Errorf("typed error return at %s differs across platform variants", site.position)
			}
			allSites[site.position] = site
		}
	}
	var unselected []string
	for name := range sources {
		if !coveredSources[name] {
			unselected = append(unselected, name)
		}
	}
	if len(unselected) > 0 {
		sort.Strings(unselected)
		return nil, 0, fmt.Errorf("module-root source walk found providerlimits files not type-checked by any supported build variant: %s", strings.Join(unselected, ", "))
	}
	sites := make([]discoveredProcessStartTimeErrorSite, 0, len(allSites))
	for _, site := range allSites {
		sites = append(sites, site)
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].id != sites[j].id {
			return sites[i].id < sites[j].id
		}
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		if sites[i].function != sites[j].function {
			return sites[i].function < sites[j].function
		}
		return sites[i].position < sites[j].position
	})
	return sites, constructorCount, nil
}

func typeCheckProviderlimitsSources(
	root string,
	sources map[string]string,
	variant processStartTimeBuildVariant,
) ([]checkedProviderlimitsFile, *types.Info, *token.FileSet, error) {
	cmd := exec.Command("go", "list", "-deps", "-export", "-json", "./pkg/providerlimits")
	cmd.Dir = root
	cmd.Env = replaceProcessStartTimeEnv(os.Environ(), "GOOS", variant.goos)
	cmd.Env = replaceProcessStartTimeEnv(cmd.Env, "GOARCH", variant.goarch)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, nil, nil, fmt.Errorf("go list: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, nil, nil, fmt.Errorf("go list: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var target *listedGoPackage
	exports := make(map[string]string)
	for {
		var item listedGoPackage
		if err := decoder.Decode(&item); err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, nil, fmt.Errorf("decode go list output: %w", err)
		}
		if item.Error != nil {
			return nil, nil, nil, fmt.Errorf("go list package %s: %s", item.ImportPath, item.Error.Err)
		}
		if item.Export != "" {
			exports[item.ImportPath] = item.Export
		}
		if item.ImportPath == processStartTimePackageImportPath {
			copy := item
			target = &copy
		}
	}
	if target == nil {
		return nil, nil, nil, fmt.Errorf("go list did not include %s", processStartTimePackageImportPath)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve module root %s: %w", root, err)
	}
	resolvedPackageDir, err := filepath.EvalSymlinks(target.Dir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve package directory %s: %w", target.Dir, err)
	}
	fset := token.NewFileSet()
	files := make([]checkedProviderlimitsFile, 0, len(target.GoFiles)+len(target.CgoFiles))
	asts := make([]*ast.File, 0, cap(files))
	for _, filename := range append(append([]string(nil), target.GoFiles...), target.CgoFiles...) {
		absolute := filepath.Join(resolvedPackageDir, filename)
		relative, err := filepath.Rel(resolvedRoot, absolute)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("make source path relative for %s: %w", absolute, err)
		}
		name := filepath.ToSlash(relative)
		source, ok := sources[name]
		if !ok {
			return nil, nil, nil, fmt.Errorf("go list selected %s, which the module-root source walk did not return", name)
		}
		parsed, err := parser.ParseFile(fset, name, source, parser.AllErrors)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parse %s: %w", name, err)
		}
		files = append(files, checkedProviderlimitsFile{name: name, file: parsed})
		asts = append(asts, parsed)
	}

	lookup := func(importPath string) (io.ReadCloser, error) {
		if importPath == "unsafe" {
			return nil, fmt.Errorf("unsafe uses its special importer path")
		}
		archive, ok := exports[importPath]
		if !ok {
			return nil, fmt.Errorf("go list export archive for import %q is missing", importPath)
		}
		return os.Open(archive)
	}
	info := newProcessStartTimeGuardTypesInfo()
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	if _, err := conf.Check(target.ImportPath, fset, asts, info); err != nil {
		return nil, nil, nil, fmt.Errorf("type check: %w", err)
	}
	return files, info, fset, nil
}

func replaceProcessStartTimeEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	found := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !found {
				result = append(result, prefix+value)
				found = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !found {
		result = append(result, prefix+value)
	}
	return result
}

func discoverTypedProcessStartTimeErrorSites(
	files []checkedProviderlimitsFile,
	info *types.Info,
	fset *token.FileSet,
	mutant string,
) ([]discoveredProcessStartTimeErrorSite, int, error) {
	factory, err := processStartTimeFactoryObject(files, info)
	if err != nil {
		return nil, 0, err
	}
	roots := processStartTimeSourceRoots(files)
	parents := make(map[ast.Node]ast.Node)
	for _, root := range roots {
		for node, parent := range processStartTimeParents(root.node) {
			parents[node] = parent
		}
	}
	aliases := processStartTimeFactoryAliases(roots, info, factory)
	closureAliases := processStartTimeFactoryAliasesUsedInClosures(roots, info, aliases)
	var sites []discoveredProcessStartTimeErrorSite
	constructors := 0
	for _, root := range roots {
		walkProcessStartTimeNodes(root.node, root.function, func(node ast.Node, function string) {
			if node == nil || root.err != nil {
				return
			}
			position := fset.Position(node.Pos())
			switch current := node.(type) {
			case *ast.CompositeLit:
				if !isProcessStartTimeErrorType(info.TypeOf(current)) {
					return
				}
				constructors++
				if function != "newProcessStartTimeError" {
					shape := "typed-error-construction"
					if function == "" {
						shape = "package-level-construction"
					}
					if !processStartTimeGuardSkipsMutant(mutant, shape) {
						site := processStartTimeCompositeLiteralSite(current, info)
						returnError := fmt.Sprintf("%s:%d %s constructs ProcessStartTimeError outside newProcessStartTimeError", root.fileName(files, current), position.Line, function)
						if site != "" {
							returnError += fmt.Sprintf(" at site %q", site)
						}
						root.err = fmt.Errorf("%s [%s]", returnError, shape)
					}
				}
			case *ast.CallExpr:
				if !isProcessStartTimeErrorType(info.TypeOf(current)) {
					return
				}
				if isBuiltinNewProcessStartTimeError(current, info) {
					constructors++
					shape := "new-and-field"
					if !processStartTimeGuardSkipsMutant(mutant, shape) {
						root.err = fmt.Errorf("%s:%d new(ProcessStartTimeError) is a construction site without one mapped factory origin [%s]", root.fileName(files, current), position.Line, shape)
					}
					return
				}
				callee := processStartTimeExprObject(current.Fun, info)
				if factory != nil && callee == factory {
					if !processStartTimeFactoryCallIsDirectReturn(current, parents, function) {
						shape := processStartTimeFactoryCallShape(current, parents, function, aliases, closureAliases, info)
						if !processStartTimeGuardSkipsMutant(mutant, shape) {
							site := processStartTimeCallSite(current, info)
							root.err = fmt.Errorf("%s:%d factory call must be a direct return result in processStartTimeWithReaders", root.fileName(files, current), position.Line)
							root.err = withProcessStartTimeGuardShape(root.err, shape, site)
						}
						return
					}
					site, err := processStartTimeFactoryReturnSite(current, root.fileName(files, current), function, position, info)
					if err != nil {
						root.err = err
						return
					}
					sites = append(sites, site)
					return
				}
				shape := "typed-error-call"
				if aliases[callee] {
					shape = "factory-alias"
					if closureAliases[callee] || function == "<closure>" {
						shape = "closure-capture"
					}
				} else if isProcessStartTimeTypeConversion(current, info) {
					shape = "error-conversion"
				}
				if !processStartTimeGuardSkipsMutant(mutant, shape) {
					root.err = withProcessStartTimeGuardShape(
						fmt.Errorf("%s:%d typed-error call is not the canonical factory", root.fileName(files, current), position.Line),
						shape,
						processStartTimeCallSite(current, info),
					)
				}
			case *ast.Ident:
				if factory == nil || info.Uses[current] != factory {
					return
				}
				call, isCall := parents[current].(*ast.CallExpr)
				if isCall && call.Fun == current {
					return
				}
				shape := "factory-alias"
				aliasObject := processStartTimeAliasObjectForReference(current, roots, info)
				if aliasObject != nil && closureAliases[aliasObject] {
					shape = "closure-capture"
				}
				if !processStartTimeGuardSkipsMutant(mutant, shape) {
					root.err = withProcessStartTimeGuardShape(
						fmt.Errorf("%s:%d factory value escapes its direct return call", root.fileName(files, current), position.Line),
						shape,
						processStartTimeAliasSite(aliasObject, roots, parents, info),
					)
				}
			case *ast.AssignStmt:
				for _, left := range current.Lhs {
					selector, ok := left.(*ast.SelectorExpr)
					if !ok || !isProcessStartTimeErrorType(info.TypeOf(selector.X)) {
						continue
					}
					shape := "typed-error-field-assignment"
					if !processStartTimeGuardSkipsMutant(mutant, shape) {
						root.err = fmt.Errorf("%s:%d field assignment mutates ProcessStartTimeError outside its factory [%s]", root.fileName(files, current), position.Line, shape)
					}
					return
				}
			case *ast.ReturnStmt:
				for _, result := range current.Results {
					if _, isCall := result.(*ast.CallExpr); isCall || !isProcessStartTimeErrorType(info.TypeOf(result)) {
						continue
					}
					if function == "newProcessStartTimeError" && isProcessStartTimeFactoryLiteralReturn(result, info) {
						continue
					}
					shape := "typed-error-return"
					if processStartTimeGuardSkipsMutant(mutant, shape) || processStartTimeGuardSkipsMutant(mutant, "new-and-field") {
						continue
					}
					root.err = fmt.Errorf("%s:%d ProcessStartTimeError return is not a direct factory call [%s]", root.fileName(files, current), position.Line, shape)
					return
				}
			}
		})
		if root.err != nil {
			return nil, constructors, root.err
		}
	}
	if err := rejectProcessStartTimeErrorObjects(roots, files, info, fset, factory, mutant); err != nil {
		return nil, constructors, err
	}
	if err := rejectProcessStartTimeErrorExpressions(roots, files, info, fset, factory, mutant); err != nil {
		return nil, constructors, err
	}
	return sites, constructors, nil
}

func processStartTimeSourceRoots(files []checkedProviderlimitsFile) []processStartTimeSourceRoot {
	var roots []processStartTimeSourceRoot
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				if function.Body != nil {
					// Keep the declaration and signature in the same typed walk as
					// the body: named parameters/results are objects too.
					roots = append(roots, processStartTimeSourceRoot{node: function, function: function.Name.Name})
				}
				continue
			}
			if _, ok := declaration.(*ast.GenDecl); ok {
				roots = append(roots, processStartTimeSourceRoot{node: declaration})
			}
		}
	}
	return roots
}

func (root processStartTimeSourceRoot) fileName(files []checkedProviderlimitsFile, positioner ast.Node) string {
	for _, file := range files {
		if file.file.Pos() <= positioner.Pos() && positioner.End() <= file.file.End() {
			return file.name
		}
	}
	return "providerlimits.go"
}

func processStartTimeParents(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func walkProcessStartTimeNodes(root ast.Node, function string, visit func(ast.Node, string)) {
	var contexts []string
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			if len(contexts) > 0 {
				contexts = contexts[:len(contexts)-1]
			}
			return false
		}
		current := function
		if len(contexts) > 0 {
			current = contexts[len(contexts)-1]
		}
		if _, closure := node.(*ast.FuncLit); closure {
			current = "<closure>"
		}
		visit(node, current)
		contexts = append(contexts, current)
		return true
	})
}

func processStartTimeFactoryObject(files []checkedProviderlimitsFile, info *types.Info) (*types.Func, error) {
	var factory *types.Func
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != "newProcessStartTimeError" {
				continue
			}
			object, ok := info.Defs[function.Name].(*types.Func)
			if !ok {
				return nil, fmt.Errorf("newProcessStartTimeError has no resolved function object in %s", source.name)
			}
			if factory != nil {
				return nil, fmt.Errorf("multiple newProcessStartTimeError declarations found")
			}
			factory = object
		}
	}
	return factory, nil
}

func processStartTimeFactoryAliases(roots []processStartTimeSourceRoot, info *types.Info, factory *types.Func) map[types.Object]bool {
	aliases := make(map[types.Object]bool)
	if factory == nil {
		return aliases
	}
	for changed := true; changed; {
		changed = false
		for _, root := range roots {
			walkProcessStartTimeNodes(root.node, root.function, func(node ast.Node, _ string) {
				var names []*ast.Ident
				var values []ast.Expr
				switch current := node.(type) {
				case *ast.ValueSpec:
					names, values = current.Names, current.Values
				case *ast.AssignStmt:
					values = current.Rhs
					for _, left := range current.Lhs {
						identifier, ok := left.(*ast.Ident)
						if !ok {
							names = append(names, nil)
						} else {
							names = append(names, identifier)
						}
					}
				default:
					return
				}
				for index, name := range names {
					if name == nil || index >= len(values) {
						continue
					}
					from := processStartTimeExprObject(values[index], info)
					if from != factory && !aliases[from] {
						continue
					}
					object := info.Defs[name]
					if object == nil {
						object = info.Uses[name]
					}
					if object != nil && !aliases[object] {
						aliases[object] = true
						changed = true
					}
				}
			})
		}
	}
	return aliases
}

func processStartTimeFactoryAliasesUsedInClosures(
	roots []processStartTimeSourceRoot,
	info *types.Info,
	aliases map[types.Object]bool,
) map[types.Object]bool {
	used := make(map[types.Object]bool)
	for _, root := range roots {
		walkProcessStartTimeNodes(root.node, root.function, func(node ast.Node, function string) {
			identifier, ok := node.(*ast.Ident)
			if !ok || function != "<closure>" {
				return
			}
			object := info.Uses[identifier]
			if aliases[object] {
				used[object] = true
			}
		})
	}
	return used
}

func processStartTimeAliasObjectForReference(
	identifier *ast.Ident,
	roots []processStartTimeSourceRoot,
	info *types.Info,
) types.Object {
	for _, root := range roots {
		var found types.Object
		walkProcessStartTimeNodes(root.node, root.function, func(node ast.Node, _ string) {
			if node != identifier {
				return
			}
			parents := processStartTimeParents(root.node)
			if spec, ok := parents[identifier].(*ast.ValueSpec); ok {
				for _, name := range spec.Names {
					if object := info.Defs[name]; object != nil {
						found = object
						return
					}
				}
			}
			if assignment, ok := parents[identifier].(*ast.AssignStmt); ok {
				for _, left := range assignment.Lhs {
					if id, ok := left.(*ast.Ident); ok {
						found = processStartTimeExprObject(id, info)
						return
					}
				}
			}
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func processStartTimeFactoryCallShape(
	call *ast.CallExpr,
	parents map[ast.Node]ast.Node,
	function string,
	aliases map[types.Object]bool,
	closureAliases map[types.Object]bool,
	info *types.Info,
) string {
	callee := processStartTimeExprObject(call.Fun, info)
	if aliases[callee] {
		if closureAliases[callee] || function == "<closure>" {
			return "closure-capture"
		}
		return "factory-alias"
	}
	if function == "<closure>" {
		return "closure-capture"
	}
	for parent := parents[call]; parent != nil; parent = parents[parent] {
		switch current := parent.(type) {
		case *ast.AssignStmt:
			for _, left := range current.Lhs {
				if selector, ok := left.(*ast.SelectorExpr); ok && selector.Sel != nil {
					return "struct-field-holder"
				}
				if _, ok := left.(*ast.IndexExpr); ok {
					return "container-element"
				}
			}
		case *ast.CompositeLit:
			if valueType := info.TypeOf(current); valueType != nil {
				switch types.Unalias(valueType).Underlying().(type) {
				case *types.Slice, *types.Array:
					return "slice-element"
				case *types.Map:
					return "map-element"
				case *types.Struct:
					return "struct-field-holder"
				}
			}
		case *ast.ReturnStmt:
			return "wrapped-factory-return"
		case *ast.FuncLit:
			return "closure-capture"
		}
	}
	if function != "processStartTimeWithReaders" {
		return "non-production-factory-return"
	}
	return "factory-non-return-use"
}

func processStartTimeFactoryCallIsDirectReturn(call *ast.CallExpr, parents map[ast.Node]ast.Node, function string) bool {
	if function != "processStartTimeWithReaders" {
		return false
	}
	returned, ok := parents[call].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, result := range returned.Results {
		if result == call {
			return true
		}
	}
	return false
}

func processStartTimeFactoryReturnSite(
	call *ast.CallExpr,
	file string,
	function string,
	position token.Position,
	info *types.Info,
) (discoveredProcessStartTimeErrorSite, error) {
	line := position.Line
	if len(call.Args) != 5 {
		return discoveredProcessStartTimeErrorSite{}, fmt.Errorf("%s:%d typed error factory return has %d arguments, want 5", file, line, len(call.Args))
	}
	kindValue, err := processStartTimeStringConstant(call.Args[1], info)
	if err != nil {
		return discoveredProcessStartTimeErrorSite{}, fmt.Errorf("%s:%d read typed error kind: %w", file, line, err)
	}
	siteID, err := processStartTimeStringConstant(call.Args[2], info)
	if err != nil {
		return discoveredProcessStartTimeErrorSite{}, fmt.Errorf("%s:%d read typed error site id: %w", file, line, err)
	}
	return discoveredProcessStartTimeErrorSite{
		id:       siteID,
		kind:     ProcessStartTimeErrorKind(kindValue),
		file:     file,
		function: function,
		position: position.String(),
	}, nil
}

func processStartTimeGuardSkipsMutant(mutant, shape string) bool {
	if mutant == shape {
		return true
	}
	// This compound shape constructs an error with new and then mutates its
	// fields. Its narrowing mutant admits that one complete escape route.
	return mutant == "new-and-field" && (shape == "typed-error-field-assignment" || shape == "typed-error-return")
}

func withProcessStartTimeGuardShape(err error, shape, site string) error {
	message := err.Error()
	if site != "" {
		message += fmt.Sprintf(" at site %q", site)
	}
	return fmt.Errorf("%s [%s]", message, shape)
}

func processStartTimeCallSite(call *ast.CallExpr, info *types.Info) string {
	if len(call.Args) < 3 {
		return ""
	}
	value, err := processStartTimeStringConstant(call.Args[2], info)
	if err != nil {
		return ""
	}
	return value
}

func processStartTimeAliasSite(
	object types.Object,
	roots []processStartTimeSourceRoot,
	parents map[ast.Node]ast.Node,
	info *types.Info,
) string {
	if object == nil {
		return ""
	}
	for _, root := range roots {
		var site string
		walkProcessStartTimeNodes(root.node, root.function, func(node ast.Node, _ string) {
			call, ok := node.(*ast.CallExpr)
			if !ok || processStartTimeExprObject(call.Fun, info) != object {
				return
			}
			if _, direct := parents[call].(*ast.ReturnStmt); direct {
				site = processStartTimeCallSite(call, info)
			}
		})
		if site != "" {
			return site
		}
	}
	return ""
}

func processStartTimeCompositeLiteralSite(literal *ast.CompositeLit, info *types.Info) string {
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := field.Key.(*ast.Ident)
		if !ok || name.Name != "site" {
			continue
		}
		if value, err := processStartTimeStringConstant(field.Value, info); err == nil {
			return value
		}
	}
	return ""
}

func processStartTimeExprObject(expression ast.Expr, info *types.Info) types.Object {
	switch value := expression.(type) {
	case *ast.Ident:
		if object := info.Uses[value]; object != nil {
			return object
		}
		return info.Defs[value]
	case *ast.SelectorExpr:
		return info.Uses[value.Sel]
	default:
		return nil
	}
}

func isProcessStartTimeTypeConversion(call *ast.CallExpr, info *types.Info) bool {
	value, ok := info.Types[call.Fun]
	return ok && value.IsType() && isProcessStartTimeErrorType(info.TypeOf(call))
}

func isProcessStartTimeFactoryLiteralReturn(expression ast.Expr, info *types.Info) bool {
	if unary, ok := expression.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		_, isLiteral := unary.X.(*ast.CompositeLit)
		return isLiteral && isProcessStartTimeErrorType(info.TypeOf(unary.X))
	}
	_, isLiteral := expression.(*ast.CompositeLit)
	return isLiteral && isProcessStartTimeErrorType(info.TypeOf(expression))
}

func isProcessStartTimeErrorType(value types.Type) bool {
	if value == nil {
		return false
	}
	value = types.Unalias(value)
	if pointer, ok := value.(*types.Pointer); ok {
		value = types.Unalias(pointer.Elem())
	}
	named, ok := value.(*types.Named)
	return ok && named.Obj().Name() == "ProcessStartTimeError" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == processStartTimePackageImportPath
}

// processStartTimeTypeContainsError closes the guard over types, not AST
// construction forms. Named wrappers and every aggregate path are followed so
// a zero value, an embedded field, or a value hidden in a container cannot
// disappear from the guard's view.
func processStartTimeTypeContainsError(value types.Type) bool {
	return processStartTimeTypeContainsErrorSeen(value, make(map[types.Type]bool))
}

func processStartTimeTypeContainsErrorSeen(value types.Type, seen map[types.Type]bool) bool {
	if value == nil {
		return false
	}
	value = types.Unalias(value)
	if isProcessStartTimeErrorType(value) {
		return true
	}
	if seen[value] {
		return false
	}
	seen[value] = true
	switch current := value.(type) {
	case *types.Named:
		return processStartTimeTypeContainsErrorSeen(current.Underlying(), seen)
	case *types.Pointer:
		return processStartTimeTypeContainsErrorSeen(current.Elem(), seen)
	case *types.Struct:
		for index := 0; index < current.NumFields(); index++ {
			if processStartTimeTypeContainsErrorSeen(current.Field(index).Type(), seen) {
				return true
			}
		}
	case *types.Array:
		return processStartTimeTypeContainsErrorSeen(current.Elem(), seen)
	case *types.Slice:
		return processStartTimeTypeContainsErrorSeen(current.Elem(), seen)
	case *types.Map:
		return processStartTimeTypeContainsErrorSeen(current.Key(), seen) || processStartTimeTypeContainsErrorSeen(current.Elem(), seen)
	case *types.Chan:
		return processStartTimeTypeContainsErrorSeen(current.Elem(), seen)
	case *types.Signature:
		if current.TypeParams() != nil {
			for index := 0; index < current.TypeParams().Len(); index++ {
				if processStartTimeTypeContainsErrorSeen(current.TypeParams().At(index).Constraint(), seen) {
					return true
				}
			}
		}
		return processStartTimeTypeContainsErrorSeen(current.Params(), seen) || processStartTimeTypeContainsErrorSeen(current.Results(), seen)
	case *types.Tuple:
		for index := 0; index < current.Len(); index++ {
			if processStartTimeTypeContainsErrorSeen(current.At(index).Type(), seen) {
				return true
			}
		}
	case *types.TypeParam:
		return processStartTimeTypeContainsErrorSeen(current.Constraint(), seen)
	case *types.Interface:
		current.Complete()
		for index := 0; index < current.NumMethods(); index++ {
			if processStartTimeTypeContainsErrorSeen(current.Method(index).Type(), seen) {
				return true
			}
		}
		for index := 0; index < current.NumEmbeddeds(); index++ {
			if processStartTimeTypeContainsErrorSeen(current.EmbeddedType(index), seen) {
				return true
			}
		}
	case *types.Union:
		for index := 0; index < current.Len(); index++ {
			if processStartTimeTypeContainsErrorSeen(current.Term(index).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func rejectProcessStartTimeErrorObjects(
	roots []processStartTimeSourceRoot,
	files []checkedProviderlimitsFile,
	info *types.Info,
	fset *token.FileSet,
	factory *types.Func,
	mutant string,
) error {
	for _, root := range roots {
		var found error
		walkProcessStartTimeNodes(root.node, root.function, func(node ast.Node, function string) {
			if found != nil {
				return
			}
			var definitionObject types.Object
			if identifier, ok := node.(*ast.Ident); ok {
				definitionObject = info.Defs[identifier]
			}
			implicitObject := info.Implicits[node]
			objects := make([]types.Object, 0, 2)
			if definitionObject != nil {
				objects = append(objects, definitionObject)
			}
			if implicitObject != nil && implicitObject != definitionObject {
				objects = append(objects, implicitObject)
			}
			for _, object := range objects {
				if object == nil || !processStartTimeTypeContainsError(object.Type()) {
					continue
				}
				if processStartTimeErrorObjectAllowed(object, function, files, info, factory) {
					continue
				}
				shape := processStartTimeErrorObjectShape(object, root.node, info)
				if _, typeSwitchClause := node.(*ast.CaseClause); typeSwitchClause && object == implicitObject && implicitObject != nil {
					shape = "implicit-object"
				}
				if processStartTimeGuardSkipsMutant(mutant, shape) {
					continue
				}
				position := fset.Position(node.Pos())
				found = fmt.Errorf("%s:%d declared object %q has type %s containing ProcessStartTimeError outside its allowed declaration [%s]",
					root.fileName(files, node), position.Line, object.Name(), types.TypeString(object.Type(), nil), shape)
				return
			}
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func processStartTimeErrorObjectAllowed(
	object types.Object,
	function string,
	files []checkedProviderlimitsFile,
	info *types.Info,
	factory *types.Func,
) bool {
	if object == factory {
		return true
	}
	if typeName, ok := object.(*types.TypeName); ok && typeName.Name() == "ProcessStartTimeError" &&
		typeName.Pkg() != nil && typeName.Pkg().Path() == processStartTimePackageImportPath {
		return true
	}
	if method, ok := processStartTimeErrorReceiverMethod(object, files, info); ok && method == function {
		return true
	}
	return function == "newProcessStartTimeError"
}

func processStartTimeErrorReceiverMethod(
	object types.Object,
	files []checkedProviderlimitsFile,
	info *types.Info,
) (string, bool) {
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil {
				continue
			}
			for _, field := range function.Recv.List {
				if !isProcessStartTimeErrorType(info.TypeOf(field.Type)) {
					continue
				}
				if len(field.Names) == 0 && info.Implicits[field] == object {
					return function.Name.Name, true
				}
				for _, name := range field.Names {
					if info.Defs[name] == object {
						return function.Name.Name, true
					}
				}
			}
		}
	}
	return "", false
}

func processStartTimeErrorObjectShape(object types.Object, root ast.Node, info *types.Info) string {
	if isProcessStartTimeErrorType(object.Type()) {
		if processStartTimeObjectInitializedByNew(object, root, info) {
			return "new-and-field"
		}
		return "zero-value-object"
	}
	if processStartTimeTypeDirectlyEmbedsError(object.Type()) {
		return "embedded-container-object"
	}
	return "nested-container-object"
}

func processStartTimeObjectInitializedByNew(object types.Object, root ast.Node, info *types.Info) bool {
	initializedByNew := false
	ast.Inspect(root, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.ValueSpec:
			for index, name := range current.Names {
				if info.Defs[name] == object && index < len(current.Values) {
					call, ok := current.Values[index].(*ast.CallExpr)
					initializedByNew = ok && isBuiltinNewProcessStartTimeError(call, info)
					return false
				}
			}
		case *ast.AssignStmt:
			for index, left := range current.Lhs {
				leftIdentifier, ok := left.(*ast.Ident)
				if !ok || (info.Defs[leftIdentifier] != object && info.Uses[leftIdentifier] != object) || index >= len(current.Rhs) {
					continue
				}
				call, ok := current.Rhs[index].(*ast.CallExpr)
				initializedByNew = ok && isBuiltinNewProcessStartTimeError(call, info)
				return false
			}
		}
		return true
	})
	return object != nil && initializedByNew
}

func processStartTimeTypeDirectlyEmbedsError(value types.Type) bool {
	if value == nil {
		return false
	}
	value = types.Unalias(value)
	for {
		pointer, ok := value.(*types.Pointer)
		if !ok {
			break
		}
		value = types.Unalias(pointer.Elem())
	}
	named, ok := value.(*types.Named)
	if ok {
		value = named.Underlying()
	}
	structure, ok := value.(*types.Struct)
	if !ok {
		return false
	}
	for index := 0; index < structure.NumFields(); index++ {
		field := structure.Field(index)
		if field.Embedded() && isProcessStartTimeErrorType(field.Type()) {
			return true
		}
	}
	return false
}

func rejectProcessStartTimeErrorExpressions(
	roots []processStartTimeSourceRoot,
	files []checkedProviderlimitsFile,
	info *types.Info,
	fset *token.FileSet,
	factory *types.Func,
	mutant string,
) error {
	for _, root := range roots {
		parents := processStartTimeParents(root.node)
		var found error
		walkProcessStartTimeNodes(root.node, root.function, func(node ast.Node, function string) {
			if found != nil {
				return
			}
			expression, ok := node.(ast.Expr)
			if !ok {
				return
			}
			typed, ok := info.Types[expression]
			if !ok || typed.IsType() || !isProcessStartTimeErrorType(typed.Type) {
				return
			}
			if function == "newProcessStartTimeError" || processStartTimeErrorReceiverValueAllowed(expression, parents, function, files, info) {
				return
			}
			if call, ok := expression.(*ast.CallExpr); ok && processStartTimeFactoryCallIsDirectReturn(call, parents, function) &&
				processStartTimeExprObject(call.Fun, info) == factory {
				return
			}
			shape := processStartTimeErrorExpressionShape(expression, root.node, files, info)
			if processStartTimeGuardSkipsMutant(mutant, shape) {
				return
			}
			position := fset.Position(expression.Pos())
			found = fmt.Errorf("%s:%d expression of type %s carries ProcessStartTimeError outside its allowed position [%s]",
				root.fileName(files, expression), position.Line, types.TypeString(typed.Type, nil), shape)
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func processStartTimeErrorReceiverValueAllowed(
	expression ast.Expr,
	parents map[ast.Node]ast.Node,
	function string,
	files []checkedProviderlimitsFile,
	info *types.Info,
) bool {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	object := info.Uses[identifier]
	method, isReceiver := processStartTimeErrorReceiverMethod(object, files, info)
	if object == nil || !isReceiver || method != function || function == "<closure>" {
		return false
	}
	switch parent := parents[identifier].(type) {
	case *ast.SelectorExpr:
		return parent.X == identifier
	case *ast.BinaryExpr:
		if parent.Op != token.EQL && parent.Op != token.NEQ {
			return false
		}
		return parent.X == identifier && isNilIdentifier(parent.Y) || parent.Y == identifier && isNilIdentifier(parent.X)
	default:
		return false
	}
}

func isNilIdentifier(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "nil"
}

func processStartTimeErrorExpressionShape(
	expression ast.Expr,
	root ast.Node,
	files []checkedProviderlimitsFile,
	info *types.Info,
) string {
	if call, ok := expression.(*ast.CallExpr); ok && isBuiltinNewProcessStartTimeError(call, info) {
		return "new-and-field"
	}
	if unary, ok := expression.(*ast.UnaryExpr); ok {
		if selector, ok := unary.X.(*ast.SelectorExpr); ok && processStartTimeSelectionEmbedsError(selector, info) {
			return "embedded-container-object"
		}
	}
	if selector, ok := expression.(*ast.SelectorExpr); ok && processStartTimeSelectionEmbedsError(selector, info) {
		return "embedded-container-object"
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		object := info.Uses[identifier]
		if object != nil {
			if _, receiver := processStartTimeErrorReceiverMethod(object, files, info); receiver {
				return "typed-error-expression"
			}
			shape := processStartTimeErrorObjectShape(object, root, info)
			if shape == "zero-value-object" || shape == "embedded-container-object" || shape == "nested-container-object" || shape == "new-and-field" {
				return shape
			}
		}
	}
	return "typed-error-expression"
}

func processStartTimeSelectionEmbedsError(selector *ast.SelectorExpr, info *types.Info) bool {
	selection := info.Selections[selector]
	if selection == nil {
		return false
	}
	field, ok := selection.Obj().(*types.Var)
	return ok && field.Embedded() && isProcessStartTimeErrorType(field.Type())
}

func isBuiltinNewProcessStartTimeError(call *ast.CallExpr, info *types.Info) bool {
	identifier, ok := call.Fun.(*ast.Ident)
	if !ok || info.Uses[identifier] != types.Universe.Lookup("new") {
		return false
	}
	return isProcessStartTimeErrorType(info.TypeOf(call))
}

func processStartTimeStringConstant(expression ast.Expr, info *types.Info) (string, error) {
	value, ok := info.Types[expression]
	if !ok || value.Value == nil || value.Value.Kind() != constant.String {
		return "", fmt.Errorf("%T is not a resolved string constant", expression)
	}
	return constant.StringVal(value.Value), nil
}

func validateProcessStartTimeErrorSiteSet(
	sites []discoveredProcessStartTimeErrorSite,
	constructorCount int,
	testNames map[string]bool,
) error {
	if constructorCount != 1 {
		return fmt.Errorf("ProcessStartTimeError constructors = %d, want exactly one factory-owned construction site", constructorCount)
	}
	byPosition := make(map[string]discoveredProcessStartTimeErrorSite, len(sites))
	byID := make(map[string]discoveredProcessStartTimeErrorSite, len(sites))
	for _, site := range sites {
		if previous, ok := byPosition[site.position]; ok {
			return fmt.Errorf("typed error return enumeration has duplicate source position %q for %q and %q", site.position, previous.id, site.id)
		}
		byPosition[site.position] = site
		if previous, ok := byID[site.id]; ok {
			return fmt.Errorf("duplicate typed error site id %q at %s and %s", site.id, previous.position, site.position)
		}
		byID[site.id] = site
	}
	if len(byPosition) != len(sites) {
		return fmt.Errorf("typed error return enumeration count %d differs from source-position map size %d", len(sites), len(byPosition))
	}
	wantByID := make(map[string]processStartTimeErrorSite, len(requiredProcessStartTimeErrorSites))
	for _, want := range requiredProcessStartTimeErrorSites {
		wantByID[want.id] = want
	}
	for _, got := range sites {
		want, ok := wantByID[got.id]
		if !ok {
			return fmt.Errorf("unmapped typed error return site %q at %s:%s", got.id, got.file, got.function)
		}
		if got.kind != want.kind {
			return fmt.Errorf("typed error site %q returns kind %q, want %q", got.id, got.kind, want.kind)
		}
		if got.function != "processStartTimeWithReaders" {
			return fmt.Errorf("typed error site %q moved to %s; expected the shared production lookup", got.id, got.function)
		}
		if !testNames[want.testName] {
			return fmt.Errorf("typed error site %q has no named negative test %s", got.id, want.testName)
		}
		delete(wantByID, got.id)
	}
	if len(sites) != len(requiredProcessStartTimeErrorSites) {
		return fmt.Errorf("typed error return sites = %d, want %d: %v", len(sites), len(requiredProcessStartTimeErrorSites), sites)
	}
	if len(wantByID) > 0 {
		missing := make([]string, 0, len(wantByID))
		for id := range wantByID {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return fmt.Errorf("expected typed error return sites were not found: %s", strings.Join(missing, ", "))
	}
	return nil
}

func TestProcessStartTimeTypedErrorGuardRejectsAliasedFactory(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func processStartTimeWithReaders() error {
    makeError := newProcessStartTimeError
    return makeError(10, ProcessStartTimeInvalidPID, "invalid_pid", 0, nil)
}`
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "[factory-alias]") || !strings.Contains(err.Error(), `"invalid_pid"`) {
		t.Fatalf("type-resolved guard did not reject an aliased factory return: %v", err)
	}
}

func TestProcessStartTimeTypedErrorGuardRejectsWrappedFactoryReturn(t *testing.T) {
	source := `package providerlimits
import "errors"
type ProcessStartTimeErrorKind string
type ProcessStartTimeError struct { Kind ProcessStartTimeErrorKind; site string; Err error }
func (*ProcessStartTimeError) Error() string { return "" }
const ProcessStartTimeInvalidPID ProcessStartTimeErrorKind = "invalid_pid"
func newProcessStartTimeError(int, ProcessStartTimeErrorKind, string, int, error) *ProcessStartTimeError { return &ProcessStartTimeError{} }
func processStartTimeWithReaders() error {
    return errors.Join(newProcessStartTimeError(10, ProcessStartTimeInvalidPID, "unmapped_wrapped_site", 0, nil))
}`
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "[wrapped-factory-return]") || !strings.Contains(err.Error(), `"unmapped_wrapped_site"`) {
		t.Fatalf("type-resolved guard did not reject a wrapped factory return: %v", err)
	}
}

func TestProcessStartTimeTypedErrorGuardRejectsNewAndFieldAssignment(t *testing.T) {
	source := `package providerlimits
type ProcessStartTimeErrorKind string
type ProcessStartTimeError struct { PID int; Kind ProcessStartTimeErrorKind; site string; Err error }
func (*ProcessStartTimeError) Error() string { return "" }
const ProcessStartTimeInvalidPID ProcessStartTimeErrorKind = "invalid_pid"
func newProcessStartTimeError(int, ProcessStartTimeErrorKind, string, int, error) *ProcessStartTimeError { return &ProcessStartTimeError{} }
func processStartTimeWithReaders(pid int) error {
    failure := new(ProcessStartTimeError)
    failure.PID = pid
    failure.Kind = ProcessStartTimeInvalidPID
    failure.site = "unmapped_new_site"
    return failure
}`
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "[new-and-field]") || !strings.Contains(err.Error(), "without one mapped factory origin") {
		t.Fatalf("type-resolved guard did not reject new(ProcessStartTimeError) plus field assignment: %v", err)
	}
}

func TestProcessStartTimeTypedErrorGuardRejectsStructFieldHolder(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
type errorHolder struct { err error }
func makeHolderError() error {
    var holder errorHolder
    holder.err = newProcessStartTimeError(10, ProcessStartTimeInvalidPID, "unmapped_field_site", 0, nil)
    return holder.err
}
func processStartTimeWithReaders() error { return makeHolderError() }`
	assertProcessStartTimeGuardFixtureRejected(t, source, "struct-field-holder")
}

func TestProcessStartTimeTypedErrorGuardRejectsSliceElement(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func helper() []error {
    return []error{newProcessStartTimeError(10, ProcessStartTimeInvalidPID, "unmapped_slice_site", 0, nil)}
}
func processStartTimeWithReaders() error { return helper()[0] }`
	assertProcessStartTimeGuardFixtureRejected(t, source, "slice-element")
}

func TestProcessStartTimeTypedErrorGuardRejectsMapElement(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func processStartTimeWithReaders() error {
    failures := map[string]error{"x": newProcessStartTimeError(10, ProcessStartTimeInvalidPID, "unmapped_map_site", 0, nil)}
    return failures["x"]
}`
	assertProcessStartTimeGuardFixtureRejected(t, source, "map-element")
}

func TestProcessStartTimeTypedErrorGuardRejectsPackageLevelConstruction(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
var reviewPackageLevelErr error = &ProcessStartTimeError{Kind: ProcessStartTimeInvalidPID, site: "unmapped_pkg_var_site"}
func processStartTimeWithReaders() error { return reviewPackageLevelErr }`
	assertProcessStartTimeGuardFixtureRejected(t, source, "package-level-construction")
}

func TestProcessStartTimeTypedErrorGuardRejectsClosureCapture(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func processStartTimeWithReaders() error {
    makeError := newProcessStartTimeError
    return func() error {
        return makeError(10, ProcessStartTimeInvalidPID, "unmapped_closure_site", 0, nil)
    }()
}`
	assertProcessStartTimeGuardFixtureRejected(t, source, "closure-capture")
}

func TestProcessStartTimeTypedErrorGuardRejectsFieldMutation(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func mutate(errorValue *ProcessStartTimeError) {
    errorValue.Kind = ProcessStartTimeInvalidPID
}`
	assertProcessStartTimeGuardFixtureRejected(t, source, "typed-error-field-assignment")
}

func TestProcessStartTimeTypedErrorGuardRejectsTypedErrorParameterReturn(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func relay(errorValue *ProcessStartTimeError) *ProcessStartTimeError {
    return errorValue
}`
	assertProcessStartTimeGuardFixtureRejected(t, source, "typed-error-return")
}

func TestProcessStartTimeTypedErrorGuardRejectsEmbeddedWrapperPromotedFieldWrite(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
type wrapper struct{ ProcessStartTimeError }
func processStartTimeWithReaders(pid int) error {
    var w wrapper
    w.PID = pid
    w.Kind = ProcessStartTimeInvalidPID
    w.site = "unmapped_embedded_site"
    var err error = &w.ProcessStartTimeError
    return err
}`
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "[embedded-container-object]") {
		t.Fatalf("type-closed guard admitted the embedded-wrapper promoted-field evasion: %v", err)
	}
}

func TestProcessStartTimeTypedErrorGuardRejectsZeroValuePointerWrite(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func processStartTimeWithReaders(pid int) error {
    var e ProcessStartTimeError
    kind := &e.Kind
    *kind = ProcessStartTimeInvalidPID
    return error(&e)
}`
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "[zero-value-object]") {
		t.Fatalf("type-closed guard admitted the zero-value pointer-write evasion: %v", err)
	}
}

func TestProcessStartTimeTypedErrorGuardRejectsUnreferencedZeroValueDeclaration(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
var reviewZeroValue ProcessStartTimeError
func processStartTimeWithReaders() error { return nil }`
	assertProcessStartTimeGuardFixtureRejected(t, source, "zero-value-object")
}

func TestProcessStartTimeTypedErrorGuardRejectsEmbeddedTypeDeclaration(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
type reviewEmbedded struct{ ProcessStartTimeError }
var reviewEmbeddedValue reviewEmbedded
func processStartTimeWithReaders() error { return nil }`
	assertProcessStartTimeGuardFixtureRejected(t, source, "embedded-container-object")
}

func TestProcessStartTimeTypedErrorGuardRejectsTransitiveTypeContainer(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
type reviewNested struct {
    Routes [2]map[string]func() chan []*ProcessStartTimeError
}
var reviewNestedValue reviewNested
func processStartTimeWithReaders() error { return nil }`
	assertProcessStartTimeGuardFixtureRejected(t, source, "nested-container-object")
}

func TestProcessStartTimeTypedErrorGuardRejectsTypedValueInArgumentPosition(t *testing.T) {
	source := `package providerlimits
import "fmt"
type ProcessStartTimeErrorKind string
type ProcessStartTimeError struct { PID int; Kind ProcessStartTimeErrorKind; site string; Err error }
func (e *ProcessStartTimeError) Error() string {
    _ = fmt.Sprint(e)
    return ""
}
const ProcessStartTimeInvalidPID ProcessStartTimeErrorKind = "invalid_pid"
func newProcessStartTimeError(pid int, kind ProcessStartTimeErrorKind, site string, attempts int, cause error) *ProcessStartTimeError {
    return &ProcessStartTimeError{PID: pid, Kind: kind, site: site, Err: cause}
}
func processStartTimeWithReaders() error { return nil }`
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "[typed-error-expression]") {
		t.Fatalf("type-closed guard admitted a typed error value in an argument position: %v", err)
	}
}

func TestProcessStartTimeTypedErrorGuardRejectsTypeSwitchImplicitObject(t *testing.T) {
	source := processStartTimeGuardFixturePrefix + `
func processStartTimeWithReaders(input error) error {
    switch value := input.(type) {
    case *ProcessStartTimeError:
        _ = value
    }
    return nil
}`
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "[implicit-object]") {
		t.Fatalf("type-closed guard did not reject the implicit type-switch object through Info.Implicits: %v", err)
	}
}

func TestProcessStartTimeTypedErrorGuardNarrowingMutantsAreKilled(t *testing.T) {
	mutants := []struct {
		shape    string
		testName string
	}{
		{shape: "duplicate-site-id", testName: "TestProcessStartTimeTypedErrorGuardRejectsDuplicateSiteID"},
		{shape: "implicit-object", testName: "TestProcessStartTimeTypedErrorGuardRejectsTypeSwitchImplicitObject"},
		{shape: "factory-alias", testName: "TestProcessStartTimeTypedErrorGuardRejectsAliasedFactory"},
		{shape: "wrapped-factory-return", testName: "TestProcessStartTimeTypedErrorGuardRejectsWrappedFactoryReturn"},
		{shape: "new-and-field", testName: "TestProcessStartTimeTypedErrorGuardRejectsNewAndFieldAssignment"},
		{shape: "struct-field-holder", testName: "TestProcessStartTimeTypedErrorGuardRejectsStructFieldHolder"},
		{shape: "slice-element", testName: "TestProcessStartTimeTypedErrorGuardRejectsSliceElement"},
		{shape: "map-element", testName: "TestProcessStartTimeTypedErrorGuardRejectsMapElement"},
		{shape: "package-level-construction", testName: "TestProcessStartTimeTypedErrorGuardRejectsPackageLevelConstruction"},
		{shape: "closure-capture", testName: "TestProcessStartTimeTypedErrorGuardRejectsClosureCapture"},
		{shape: "typed-error-field-assignment", testName: "TestProcessStartTimeTypedErrorGuardRejectsFieldMutation"},
		{shape: "typed-error-return", testName: "TestProcessStartTimeTypedErrorGuardRejectsTypedErrorParameterReturn"},
		{shape: "zero-value-object", testName: "TestProcessStartTimeTypedErrorGuardRejectsUnreferencedZeroValueDeclaration"},
		{shape: "embedded-container-object", testName: "TestProcessStartTimeTypedErrorGuardRejectsEmbeddedTypeDeclaration"},
		{shape: "nested-container-object", testName: "TestProcessStartTimeTypedErrorGuardRejectsTransitiveTypeContainer"},
		{shape: "typed-error-expression", testName: "TestProcessStartTimeTypedErrorGuardRejectsTypedValueInArgumentPosition"},
	}
	root := processStartTimeModuleRoot(t)
	for _, mutant := range mutants {
		mutant := mutant
		t.Run(mutant.shape, func(t *testing.T) {
			command := exec.Command("go", "test", "-count=1", "-run", "^"+mutant.testName+"$", "./pkg/providerlimits")
			command.Dir = root
			command.Env = replaceProcessStartTimeEnv(os.Environ(), processStartTimeGuardMutantEnv, mutant.shape)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("narrowing mutant %q survived %s", mutant.shape, mutant.testName)
			}
			if !bytes.Contains(output, []byte("--- FAIL: "+mutant.testName)) {
				t.Fatalf("narrowing mutant %q did not fail named test %s (command error %v):\n%s", mutant.shape, mutant.testName, err, output)
			}
			t.Logf("narrowing mutant %q admitted its forbidden shape; named test %s failed as required", mutant.shape, mutant.testName)
		})
	}
}

func assertProcessStartTimeSourceNarrowingMutantKilled(
	t *testing.T,
	sourceName string,
	oldText string,
	mutantText string,
	testName string,
) {
	t.Helper()
	root := processStartTimeModuleRoot(t)
	sourcePath := filepath.Join(root, filepath.FromSlash(sourceName))
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source for narrowing mutant %s: %v", sourceName, err)
	}
	if strings.Count(string(source), oldText) != 1 {
		t.Fatalf("narrowing mutant anchor in %s occurred %d times, want exactly once", sourceName, strings.Count(string(source), oldText))
	}
	mutated := strings.Replace(string(source), oldText, mutantText, 1)
	artifactDir := filepath.Join(root, ".temp", "BUG-260929-326q2f", "narrowing-mutants")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create task-scoped mutant directory: %v", err)
	}
	scratch, err := os.MkdirTemp(artifactDir, "run-")
	if err != nil {
		t.Fatalf("create mutant scratch directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(scratch); err != nil {
			t.Errorf("remove mutant scratch directory: %v", err)
		}
	}()
	mutantPath := filepath.Join(scratch, filepath.Base(sourceName))
	if err := os.WriteFile(mutantPath, []byte(mutated), 0o644); err != nil {
		t.Fatalf("write mutated source: %v", err)
	}
	overlay, err := json.Marshal(struct {
		Replace map[string]string
	}{Replace: map[string]string{sourcePath: mutantPath}})
	if err != nil {
		t.Fatalf("encode go overlay: %v", err)
	}
	overlayPath := filepath.Join(scratch, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o644); err != nil {
		t.Fatalf("write go overlay: %v", err)
	}
	command := exec.Command("go", "test", "-count=1", "-overlay", overlayPath, "-run", "^"+testName+"$", "./pkg/providerlimits")
	command.Dir = root
	command.Env = replaceProcessStartTimeEnv(os.Environ(), processStartTimeGuardMutantEnv, "")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("narrowing mutant in %s survived named test %s", sourceName, testName)
	}
	if !bytes.Contains(output, []byte("--- FAIL: "+testName)) {
		t.Fatalf("narrowing mutant in %s did not fail named test %s (command error %v):\n%s", sourceName, testName, err, output)
	}
	t.Logf("narrowing mutant in %s was killed by named test %s", sourceName, testName)
}

const processStartTimeGuardFixturePrefix = `package providerlimits
type ProcessStartTimeErrorKind string
type ProcessStartTimeError struct { PID int; Kind ProcessStartTimeErrorKind; site string; Err error }
func (*ProcessStartTimeError) Error() string { return "" }
const ProcessStartTimeInvalidPID ProcessStartTimeErrorKind = "invalid_pid"
func newProcessStartTimeError(pid int, kind ProcessStartTimeErrorKind, site string, attempts int, cause error) *ProcessStartTimeError {
    return &ProcessStartTimeError{PID: pid, Kind: kind, site: site, Err: cause}
}`

func assertProcessStartTimeGuardFixtureRejected(t *testing.T, source, shape string) {
	t.Helper()
	err := processStartTimeGuardFixtureError(source)
	if err == nil || !strings.Contains(err.Error(), "["+shape+"]") {
		t.Fatalf("typed-error guard did not reject forbidden shape %q: %v", shape, err)
	}
}

func processStartTimeGuardFixtureError(source string) error {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "fixture.go", source, parser.AllErrors)
	if err != nil {
		return err
	}
	info := newProcessStartTimeGuardTypesInfo()
	if _, err := (&types.Config{Importer: importer.Default()}).Check(processStartTimePackageImportPath, fset, []*ast.File{parsed}, info); err != nil {
		return err
	}
	_, _, err = discoverTypedProcessStartTimeErrorSites(
		[]checkedProviderlimitsFile{{name: "fixture.go", file: parsed}},
		info,
		fset,
		os.Getenv(processStartTimeGuardMutantEnv),
	)
	return err
}
