// Package argvguard is the mechanical half of "ONE argv construction site",
// shared by every agentic-system plugin in this module.
//
// It is extracted from the codex plugin's guard, which was itself ported from
// the extraction source's codex_argv_guard_test.go (skill-project-management,
// tools/board-cli/internal/spawn). The extraction is the point: the second
// plugin needs the same discipline, and a second scanner — even one written by
// copying the first — is two implementations of one rule that drift the moment
// either is extended. That is the failure this whole package exists to
// prevent, and reproducing it in the guard would be a poor joke.
//
// A caller supplies the two things that ARE per-plugin: the signature literals
// that identify its harness's argv, and the allowlist of sites permitted to
// spell them. The rule, the threshold and the resolution depth are the same for
// everyone.
//
// # Why it is a normal package rather than test helpers
//
// Go has no way to share test-only code across packages, and these guards live
// in one test binary per plugin. Nothing in the shipped command tree imports
// this, so it is compiled and never linked into the binary.
//
// # Threshold
//
// ONE co-occurring signature literal is a violation. The source's guard began
// at two, on the theory that any single literal could appear incidentally. A
// review demonstrated the theory wrong in practice: a fourth site copy-pasted
// from either historical split spells only ONE of them — "danger-full-access"
// without also restating "--ask-for-approval" — and walked straight through.
// The threshold is one, and every legitimate non-construction touch is named in
// the caller's allowlist with a reason instead.
//
// # Threat model
//
// A static, syntactic scan for ORDINARY Go spellings, to catch an honest
// colleague reintroducing a second construction site during ordinary work —
// which happened three times in the source before it was unwound. It is
// deliberately NOT a boundary against an author trying to defeat it: someone
// who wants to hide a literal from an AST scan has unbounded room (assemble it
// at runtime, decode it from bytes, wrap it in a call this scanner does not
// evaluate), and chasing that adds false-positive surface to a gate whose value
// is entirely in catching the mistake.
//
// Three residual classes are DECLARED OPEN. They belong to THIS scanner rather
// than to any one plugin's signature, so they are demonstrated once, by
// TestCodexArgvGuardResidualGaps, rather than restated per plugin: the same
// scanner reached through a second signature would report the same three
// classes, and a duplicate would grow the maintenance without adding evidence.
// Each plugin separately demonstrates the residual its own SIGNATURE leaves
// (claude's is TestTheDeclaredResidualStaysOpen). They are demonstrated at all
// rather than only described because a class named in prose and not
// demonstrated is a comment a reader trusts with nothing holding it to the
// code:
//
//   - A call-wrapped literal behind a package-level NAME:
//     `var flags = []string{string([]byte("--skip-git-repo-check"))}`, then a
//     function spreading flags. Neither foldStringExpr nor CollectStringLiterals
//     evaluates a call expression, so the var resolves to no strings and the
//     function that references it touches nothing. The same call-wrapped literal
//     written INSIDE a function body is still caught — the body walk sees the
//     literal — so the gap is the indirection, not the wrapping.
//   - Cross-package references: a const or var declared outside the scanned
//     source set and referenced through a qualified selector. Only the files
//     handed to the scanner are resolved.
//   - Reassignment inside an ALLOWLISTED site: a package-level var declared
//     empty and appended to from inside the construction function itself. The
//     scanner reads non-allowlisted bodies and package-level initializers;
//     statements inside an allowlisted body are the allowlist's own blind spot,
//     which is a property of having an allowlist at all.
//
// # Allowlist keys are FILE-SCOPED
//
// An allowlist key is "<module-relative file>::<name>", not a bare name. That
// is stricter than the source's, and the second plugin is why: both plugins
// name their construction site Args, so a bare-name allowlist would exempt
// EVERY function called Args in the module from EVERY plugin's guard — the
// codex guard would stop being able to report a second codex construction the
// moment somebody named it Args in another package. Scoping the exemption to
// the file that earned it removes that.
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

// Violation is one place outside the allowlist that spells a signature literal.
type Violation struct {
	File    string
	Name    string
	Line    int
	Matched []string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d %s spells %s", v.File, v.Line, v.Name, strings.Join(v.Matched, ", "))
}

// AllowlistKey is the file-scoped name of an exempted site, as it must appear
// in an allowlist map.
func AllowlistKey(file, name string) string { return file + "::" + name }

// Scan reports every site outside allowlist whose body or initializer spells a
// signature literal — inline, through a resolvable const or var, or inside a
// function literal reachable from a package-level var's initializer.
//
// sources maps a display filename to Go source text, so the same scanner runs
// over the real module and over synthetic mutant corpora. allowlist maps an
// AllowlistKey to the reason that site is not a second construction; the reason
// is never read here, and exists so that a caller's own test can refuse an
// exemption nobody can argue with.
func Scan(sources map[string]string, signature []string, allowlist map[string]string) ([]Violation, error) {
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
	all := make([]*ast.File, 0, len(parsed))
	for _, name := range names {
		all = append(all, parsed[name])
	}
	consts := FoldConstStrings(all)
	vars := collectVarStrings(all, consts)

	allowed := func(file, name string) bool {
		_, ok := allowlist[AllowlistKey(file, name)]
		return ok
	}

	var violations []Violation
	for _, name := range names {
		for _, decl := range parsed[name].Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				if allowed(name, d.Name.Name) {
					continue
				}
				if matched := matchedIn(d.Body, signature, consts, vars); len(matched) >= 1 {
					violations = append(violations, newViolation(name, d.Name.Name, fset.Position(d.Pos()).Line, matched))
				}
			case *ast.GenDecl:
				// A package-level declaration is scanned two ways: any function
				// literal reachable from its initializer is a body (a table of
				// closures is how the source's own adapter table is built), and
				// the declaration's own literals are attributed to its name (a
				// const or var holding a flag is a construction waiting for a
				// caller).
				for _, spec := range d.Specs {
					vspec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, ident := range vspec.Names {
						if i >= len(vspec.Values) {
							continue
						}
						if allowed(name, ident.Name) {
							continue
						}
						for _, lit := range collectFuncLits(vspec.Values[i]) {
							if matched := matchedIn(lit.Body, signature, consts, vars); len(matched) >= 1 {
								violations = append(violations, newViolation(name, ident.Name, fset.Position(lit.Pos()).Line, matched))
							}
						}
						if matched := matchedValues(CollectStringLiterals(vspec.Values[i], consts), signature); len(matched) >= 1 {
							violations = append(violations, newViolation(name, ident.Name, fset.Position(vspec.Pos()).Line, matched))
						}
					}
				}
			}
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		if violations[i].Name != violations[j].Name {
			return violations[i].Name < violations[j].Name
		}
		return violations[i].Line < violations[j].Line
	})
	return violations, nil
}

// DeclaredNames returns every top-level function, const and var name declared
// per file, keyed by AllowlistKey.
//
// A caller's allowlist-justification test reads it to refuse an entry naming a
// site nobody wrote: an exemption for a function that does not exist is an
// exemption waiting for somebody to claim it by naming a new function that way.
func DeclaredNames(sources map[string]string) (map[string]bool, error) {
	declared := map[string]bool{}
	fset := token.NewFileSet()
	for name, src := range sources {
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declared[AllowlistKey(name, d.Name.Name)] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if vspec, ok := spec.(*ast.ValueSpec); ok {
						for _, ident := range vspec.Names {
							declared[AllowlistKey(name, ident.Name)] = true
						}
					}
				}
			}
		}
	}
	return declared, nil
}

func newViolation(file, name string, line int, matched map[string]bool) Violation {
	values := make([]string, 0, len(matched))
	for s := range matched {
		values = append(values, s)
	}
	sort.Strings(values)
	return Violation{File: file, Name: name, Line: line, Matched: values}
}

// matchedIn walks a body and returns the signature values it spells, directly
// or through a name the const/var resolution already folded.
func matchedIn(body ast.Node, signature []string, consts map[string]string, vars map[string][]string) map[string]bool {
	matched := map[string]bool{}
	record := func(value string) {
		for _, s := range signature {
			if strings.Contains(value, s) {
				matched[s] = true
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			if value, err := strconv.Unquote(node.Value); err == nil {
				record(value)
			}
		case *ast.Ident:
			if value, ok := consts[node.Name]; ok {
				record(value)
			}
			for _, value := range vars[node.Name] {
				record(value)
			}
		}
		return true
	})
	return matched
}

func matchedValues(values, signature []string) map[string]bool {
	matched := map[string]bool{}
	for _, value := range values {
		for _, s := range signature {
			if strings.Contains(value, s) {
				matched[s] = true
			}
		}
	}
	return matched
}

// FoldConstStrings resolves every package-level const that folds to a
// compile-time string, as a bounded fixpoint so declaration order and grouping
// do not matter.
func FoldConstStrings(files []*ast.File) map[string]string {
	type pending struct {
		name string
		expr ast.Expr
	}
	var queue []pending
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok || len(vspec.Names) != len(vspec.Values) {
					continue
				}
				for i, name := range vspec.Names {
					queue = append(queue, pending{name.Name, vspec.Values[i]})
				}
			}
		}
	}
	consts := map[string]string{}
	for pass := 0; pass <= len(queue); pass++ {
		changed := false
		for _, item := range queue {
			if _, done := consts[item.name]; done {
				continue
			}
			if value, ok := foldStringExpr(item.expr, consts); ok {
				consts[item.name] = value
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return consts
}

// foldStringExpr resolves an expression to one compile-time string: a literal,
// a name already folded, or a `+` chain of those. It stops at anything else —
// a call, an index, a selector — which is the declared-open boundary above.
func foldStringExpr(expr ast.Expr, consts map[string]string) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.ParenExpr:
		return foldStringExpr(v.X, consts)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := foldStringExpr(v.X, consts)
		if !ok {
			return "", false
		}
		r, ok := foldStringExpr(v.Y, consts)
		if !ok {
			return "", false
		}
		return l + r, true
	default:
		return "", false
	}
}

// collectVarStrings extends the same resolution to package-level vars: every
// string reachable structurally from an initializer is attributed to the var's
// name, so a function referencing that var reads as touching all of them.
func collectVarStrings(files []*ast.File, consts map[string]string) map[string][]string {
	vars := map[string][]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok || len(vspec.Names) != len(vspec.Values) {
					continue
				}
				for i, name := range vspec.Names {
					if values := CollectStringLiterals(vspec.Values[i], consts); len(values) > 0 {
						vars[name.Name] = append(vars[name.Name], values...)
					}
				}
			}
		}
	}
	return vars
}

// CollectStringLiterals walks an initializer's structure — composite literal
// elements and keys, concatenation operands — collecting strings. It does not
// evaluate calls or index expressions; that is the call-wrapped residual.
func CollectStringLiterals(expr ast.Expr, consts map[string]string) []string {
	if value, ok := foldStringExpr(expr, consts); ok {
		return []string{value}
	}
	var out []string
	switch v := expr.(type) {
	case *ast.ParenExpr:
		out = append(out, CollectStringLiterals(v.X, consts)...)
	case *ast.BinaryExpr:
		out = append(out, CollectStringLiterals(v.X, consts)...)
		out = append(out, CollectStringLiterals(v.Y, consts)...)
	case *ast.CompositeLit:
		for _, elt := range v.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				out = append(out, CollectStringLiterals(kv.Key, consts)...)
				out = append(out, CollectStringLiterals(kv.Value, consts)...)
				continue
			}
			out = append(out, CollectStringLiterals(elt, consts)...)
		}
	}
	return out
}

// collectFuncLits returns every function literal reachable from an expression,
// however deeply nested — a table of closures is how the source's own adapter
// table was built, so a construction hidden in one is not exotic.
func collectFuncLits(expr ast.Expr) []*ast.FuncLit {
	var out []*ast.FuncLit
	ast.Inspect(expr, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			out = append(out, lit)
		}
		return true
	})
	return out
}
