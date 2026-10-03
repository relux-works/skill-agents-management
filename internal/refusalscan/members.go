package refusalscan

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// Member is one AST-derived member of an enumerated semantic refusal site.
// Bound records sites without a decidable boolean guard (constructors and
// forwarding returns). These are included in completeness, never erased.
type Member struct {
	Site       Site   `json:"site"`
	ID         string `json:"id"`
	File       string `json:"file"`
	Start, End int
	Expression string `json:"expression"`
	ParentOp   string `json:"parent_op"`
	Kind       string `json:"kind"`
	Bound      string `json:"bound,omitempty"`
}

func Members(root string, sites []Site) ([]Member, error) {
	var members []Member
	paths, err := SourceFiles(root)
	if err != nil {
		return nil, err
	}
	sources, err := parsePackageSources(root, paths)
	if err != nil {
		return nil, err
	}
	for _, site := range sites {
		path := filepath.Join(root, "pkg/agentic", filepath.FromSlash(site.File))
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		parsed := sources["pkg/agentic/"+site.File]
		if parsed == nil {
			return nil, fmt.Errorf("member source missing: %s", site.File)
		}
		fset, file := parsed.fset, parsed.file
		var stack []ast.Node
		var ancestors []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			stack = append(stack, n)
			if ret, ok := n.(*ast.ReturnStmt); ok && fset.Position(ret.Pos()).Line == site.Line && normalized(fset, ret) == site.Return {
				ancestors = append([]ast.Node(nil), stack...)
			}
			return true
		})
		var guard *ast.IfStmt
		for i := len(ancestors) - 2; i >= 0; i-- {
			if branch, ok := ancestors[i].(*ast.IfStmt); ok {
				guard = branch
				break
			}
		}
		count := 0
		appendMember := func(expr ast.Expr, kind, parent, bound string) {
			count++
			start, end := 0, 0
			text := ""
			if expr != nil {
				start = fset.Position(expr.Pos()).Offset
				end = fset.Position(expr.End()).Offset
				text = string(body[start:end])
			}
			members = append(members, Member{Site: site, ID: fmt.Sprintf("%s/member-%d", site.Key(), count), File: "pkg/agentic/" + site.File, Start: start, End: end, Expression: text, Kind: kind, ParentOp: parent, Bound: bound})
		}
		var leaves func(ast.Expr, string)
		leaves = func(expr ast.Expr, parent string) {
			if p, ok := expr.(*ast.ParenExpr); ok {
				leaves(p.X, parent)
				return
			}
			if b, ok := expr.(*ast.BinaryExpr); ok && (b.Op == token.LOR || b.Op == token.LAND) {
				leaves(b.X, b.Op.String())
				leaves(b.Y, b.Op.String())
				return
			}
			bound := ""
			if parent == "single" {
				bound = "Single boolean guard: disabling it is deletion, not evidence of narrowing. A domain-specific exemption is required."
			}
			if parent == "&&" {
				bound = "Conjunct of a refusal condition: neutralization strengthens refusal rather than narrows it. Requires a domain-specific exemption."
			}
			appendMember(expr, "operand", parent, bound)
			ast.Inspect(expr, func(n ast.Node) bool {
				if b, ok := n.(*ast.BinaryExpr); ok {
					switch b.Op {
					case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
						for _, side := range []ast.Expr{b.X, b.Y} {
							isConstant := false
							if _, ok := side.(*ast.BasicLit); ok {
								isConstant = true
							}
							switch expr := side.(type) {
							case *ast.Ident:
								_, isConstant = parsed.info.ObjectOf(expr).(*types.Const)
							case *ast.SelectorExpr:
								_, isConstant = parsed.info.ObjectOf(expr.Sel).(*types.Const)
							}
							if isConstant {
								appendMember(side, "compared-constant", parent, "Compared constant inventoried; arbitrary replacement need not narrow this domain.")
							}
						}
					}
				}
				return true
			})
		}
		if guard != nil {
			leaves(guard.Cond, "single")
		} else {
			appendMember(nil, "unconditional", "", "Unconditional typed constructor/forwarder/fallthrough: no boolean guard at this return.")
		}
		// A terminal refusal after a membership loop is controlled by the
		// preceding acceptance comparison too. Inventory its string operands
		// through type information, rather than hiding the fallthrough as unguarded.
		if guard == nil {
			for _, ancestor := range ancestors {
				fn, ok := ancestor.(*ast.FuncDecl)
				if !ok {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					loop, ok := n.(*ast.RangeStmt)
					if !ok {
						return true
					}
					ast.Inspect(loop.Body, func(n ast.Node) bool {
						branch, ok := n.(*ast.IfStmt)
						if !ok {
							return true
						}
						comparison, ok := branch.Cond.(*ast.BinaryExpr)
						if !ok || comparison.Op != token.EQL {
							return true
						}
						left, right := parsed.info.TypeOf(comparison.X), parsed.info.TypeOf(comparison.Y)
						if left == nil || right == nil || left.String() != "string" || right.String() != "string" {
							return true
						}
						for _, statement := range branch.Body.List {
							ret, ok := statement.(*ast.ReturnStmt)
							if ok && len(ret.Results) == 1 {
								id, ok := ret.Results[0].(*ast.Ident)
								if ok && id.Name == "nil" {
									appendMember(comparison, "allowed-set-acceptance", "==", "")
								}
							}
						}
						return true
					})
					return false
				})
			}
		}
		// Include static members of a surrounding credential/allowed-set loop.
		for _, ancestor := range ancestors {
			if loop, ok := ancestor.(*ast.RangeStmt); ok {
				if literal, ok := loop.X.(*ast.CompositeLit); ok {
					for _, element := range literal.Elts {
						if expr, ok := element.(ast.Expr); ok {
							appendMember(expr, "set-element", "", "Set element inventoried; replacing it requires a typed domain-specific runner.")
						}
					}
				}
			}
		}
		if count == 0 {
			return nil, fmt.Errorf("member completeness: no members for %s", site.Key())
		}
	}
	return members, nil
}

// CheckMemberCompleteness rejects missing sites, missing member identities,
// duplicate identities and spurious members against fresh AST enumeration.
func CheckMemberCompleteness(root string, sites []Site, got []Member) error {
	want, err := Members(root, sites)
	if err != nil {
		return err
	}
	expected := map[string]bool{}
	for _, m := range want {
		expected[m.ID] = true
	}
	seen := map[string]bool{}
	for _, m := range got {
		if !expected[m.ID] || seen[m.ID] {
			return fmt.Errorf("member completeness: unexpected or duplicate %s", m.ID)
		}
		seen[m.ID] = true
	}
	for id := range expected {
		if !seen[id] {
			return fmt.Errorf("member completeness: missing %s", id)
		}
	}
	return nil
}

func LocalCodexSites(root string) ([]Site, error) {
	sites, err := DiscoverType(root, "github.com/relux-works/skill-agents-management/pkg/agentic", "LocalProviderRefusal")
	if err != nil {
		return nil, err
	}
	var local []Site
	for _, site := range sites {
		if strings.HasPrefix(site.File, "systems/codex/") {
			local = append(local, site)
		}
	}
	return local, nil
}
