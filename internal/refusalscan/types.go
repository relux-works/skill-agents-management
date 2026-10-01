package refusalscan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
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
)

// A set carries the shared type-checker's objects, never spelling-based keys.
type symbolSet struct {
	info    *types.Info
	objects map[types.Object]bool
}

func newSymbolSet(info *types.Info) symbolSet {
	return symbolSet{info: info, objects: make(map[types.Object]bool)}
}

func (s symbolSet) has(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		return s.objects[s.info.ObjectOf(expression)]
	case *ast.SelectorExpr:
		return s.objects[s.info.ObjectOf(expression.Sel)]
	case *ast.ParenExpr:
		return s.has(expression.X)
	case *ast.IndexExpr:
		return s.has(expression.X)
	case *ast.IndexListExpr:
		return s.has(expression.X)
	}
	return false
}

func implementsError(t types.Type) bool {
	return t != nil && types.Implements(t, types.Universe.Lookup("error").Type().Underlying().(*types.Interface))
}

func symbolInfo(files map[string]*parsedSource) *types.Info {
	for _, source := range files {
		return source.info
	}
	panic("refusalscan: empty source inventory")
}

func sentinelDeclaration(values *ast.ValueSpec, errors symbolSet) bool {
	for _, name := range values.Names {
		if !errors.has(name) {
			return false
		}
	}
	return true
}

// Match the error protocol by receiver method-set membership and exact signature.
func errorImplementation(function *ast.FuncDecl, info *types.Info) bool {
	object, ok := info.ObjectOf(function.Name).(*types.Func)
	if !ok {
		return false
	}
	sig := object.Type().(*types.Signature)
	if sig.Recv() == nil || !(implementsError(sig.Recv().Type()) || implementsError(types.NewPointer(sig.Recv().Type()))) {
		return false
	}
	errorType := types.Universe.Lookup("error").Type()
	boolType := types.Typ[types.Bool]
	stringType := types.Typ[types.String]
	params, results := sig.Params(), sig.Results()
	if results.Len() != 1 || sig.Variadic() {
		return false
	}
	switch object.Name() {
	case "Error":
		return params.Len() == 0 && types.Identical(results.At(0).Type(), stringType)
	case "Is":
		return params.Len() == 1 && types.Identical(params.At(0).Type(), errorType) && types.Identical(results.At(0).Type(), boolType)
	case "As":
		return params.Len() == 1 && types.Identical(params.At(0).Type(), types.NewInterfaceType(nil, nil).Complete()) && types.Identical(results.At(0).Type(), boolType)
	case "Unwrap":
		return params.Len() == 0 && (types.Identical(results.At(0).Type(), errorType) || types.Identical(results.At(0).Type(), types.NewSlice(errorType)))
	}
	return false
}

type parsedSource struct {
	fset *token.FileSet
	file *ast.File
	info *types.Info
}

// sourceImporter checks scanned packages from source so selector uses share
// exactly the objects recorded at their declarations. Other dependencies use
// compiler export data; a missing or invalid import is a scan failure.
type sourceImporter struct {
	fset     *token.FileSet
	info     *types.Info
	files    map[string][]*ast.File
	packages map[string]*types.Package
	external types.Importer
}

func (i *sourceImporter) Import(path string) (*types.Package, error) {
	if pkg := i.packages[path]; pkg != nil {
		return pkg, nil
	}
	files, local := i.files[path]
	if !local {
		return i.external.Import(path)
	}
	config := types.Config{Importer: i}
	pkg, err := config.Check(path, i.fset, files, i.info)
	if err != nil {
		return nil, fmt.Errorf("refusalscan: type-check %s: %w", path, err)
	}
	i.packages[path] = pkg
	return pkg, nil
}

func parsePackageSources(root string, sourceFiles []string) (map[string]*parsedSource, error) {
	fset := token.NewFileSet()
	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection)}
	module := "refusalscan.fixture"
	moduleBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		for _, line := range strings.Split(string(moduleBytes), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" {
				module = strings.Trim(fields[1], `"`)
				break
			}
		}
	}
	result := make(map[string]*parsedSource)
	loader := &sourceImporter{fset: fset, info: info, files: make(map[string][]*ast.File), packages: make(map[string]*types.Package), external: importer.Default()}
	for _, relative := range sourceFiles {
		file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(relative)), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse source %s: %w", relative, err)
		}
		result[relative] = &parsedSource{fset: fset, file: file, info: info}
		path := module + "/" + filepath.ToSlash(filepath.Dir(relative))
		loader.files[path] = append(loader.files[path], file)
	}
	if moduleBytes != nil {
		command := exec.Command("go", "list", "-mod=readonly", "-export", "-deps", "-json", "./pkg/agentic/...")
		command.Dir = root
		command.Env = append(os.Environ(), "GOWORK=off")
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("refusalscan: load dependency exports: %w: %s", err, stderr.String())
		}
		exports := make(map[string]string)
		decoder := json.NewDecoder(bytes.NewReader(output))
		for {
			var pkg struct {
				ImportPath, Export, Dir string
				GoFiles                 []string
			}
			if err := decoder.Decode(&pkg); err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("refusalscan: decode dependency exports: %w", err)
			}
			exports[pkg.ImportPath] = pkg.Export
			if strings.HasPrefix(pkg.ImportPath, module+"/") && loader.files[pkg.ImportPath] == nil {
				for _, name := range pkg.GoFiles {
					file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, 0)
					if err != nil {
						return nil, err
					}
					loader.files[pkg.ImportPath] = append(loader.files[pkg.ImportPath], file)
				}
			}
		}
		loader.external = importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
			if exports[path] == "" {
				return nil, fmt.Errorf("no compiler export for %s", path)
			}
			return os.Open(exports[path])
		})
	}
	var paths []string
	for path := range loader.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if _, err := loader.Import(path); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Interface method objects have no body. Connect them to refusal-producing
// implementations by the complete interface method set, not a spelling match.
func interfaceRefusalHelpers(files map[string]*parsedSource, helpers symbolSet) bool {
	changed := false
	for _, source := range files {
		ast.Inspect(source.file, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			selection := helpers.info.Selections[selector]
			if selection == nil {
				return true
			}
			iface, ok := selection.Recv().Underlying().(*types.Interface)
			if !ok {
				return true
			}
			method, ok := selection.Obj().(*types.Func)
			if !ok || helpers.objects[method] {
				return true
			}
			for obj := range helpers.objects {
				implementation, ok := obj.(*types.Func)
				if !ok {
					continue
				}
				sig := implementation.Type().(*types.Signature)
				if sig.Recv() == nil || !types.Implements(sig.Recv().Type(), iface) {
					continue
				}
				selected := types.NewMethodSet(sig.Recv().Type()).Lookup(method.Pkg(), method.Name())
				if selected != nil && selected.Obj() == implementation {
					helpers.objects[method] = true
					changed = true
					break
				}
			}
			return true
		})
	}
	return changed
}
