package argvguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Site is one Go string literal containing a spelling: the file, the
// top-level declaration enclosing it, and the literal's own line. Comments are
// never sites — prose about a flag is not a spelling of it.
type Site struct {
	File string
	Name string
	Line int
}

func (s Site) String() string {
	return fmt.Sprintf("%s:%d %s", s.File, s.Line, s.Name)
}

// LiteralSites reports every string literal containing spelling, across the
// same sources map Scan takes. It answers the narrower question a signature
// cannot: "how many times is THIS literal spelled", for a flag the plugin's
// signature deliberately excludes because a sibling plugin also spells it
// (claude's bypass flag, which agy's exec grammar shares). A malformed source
// is an error, never a silently empty answer — the same absence-versus-failure
// rule Scan follows.
func LiteralSites(sources map[string]string, spelling string) ([]Site, error) {
	fset := token.NewFileSet()
	names := make([]string, 0, len(sources))
	parsed := make(map[string]*ast.File, len(sources))
	for name, src := range sources {
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		names = append(names, name)
		parsed[name] = file
	}
	sort.Strings(names)

	var sites []Site
	literalHere := func(node ast.Node) (int, bool) {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return 0, false
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil || !strings.Contains(value, spelling) {
			return 0, false
		}
		return fset.Position(lit.Pos()).Line, true
	}
	for _, name := range names {
		for _, decl := range parsed[name].Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				ast.Inspect(d.Body, func(n ast.Node) bool {
					if line, ok := literalHere(n); ok {
						sites = append(sites, Site{File: name, Name: d.Name.Name, Line: line})
					}
					return true
				})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vspec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, ident := range vspec.Names {
						if i >= len(vspec.Values) {
							continue
						}
						ast.Inspect(vspec.Values[i], func(n ast.Node) bool {
							if line, ok := literalHere(n); ok {
								sites = append(sites, Site{File: name, Name: ident.Name, Line: line})
							}
							return true
						})
					}
				}
			}
		}
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		if sites[i].Name != sites[j].Name {
			return sites[i].Name < sites[j].Name
		}
		return sites[i].Line < sites[j].Line
	})
	return sites, nil
}
