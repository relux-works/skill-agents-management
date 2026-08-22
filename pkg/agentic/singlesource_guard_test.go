package agentic

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// This file is the mechanical enforcement of invariant 5 in
// docs/architecture.md: the registry is the only place an agentic system
// binding may live.
//
// # Why it is written this way
//
// The extraction source's first single-source guard grepped its own sources
// for a field name. A one-line shadow table that did not spell that field
// walked straight through it, and the guard was rewritten over go/ast. The
// rewritten guard then failed four review rounds because minimal spellings
// still walked through: a single literal where the threshold wanted two, a
// literal moved behind a package-level const, a literal moved behind a
// package-level var, and a literal moved inside a function literal. This
// guard starts at the end of that history rather than the beginning — const
// and var indirection resolve at package level AND inside function bodies,
// function literals are walked wherever they appear, an id copied into a local
// before a switch resolves back to the ID() call, a table assembled key by key
// reads as a table, and the structural rules do not depend on a literal being
// spelled at all.
//
// Four more spellings were added after a review wrote mutants this file's
// author had not: the local copy, the init()-assembled map with plain string
// keys — which is how the extraction source's own adapterTable is built — the
// function-local const, and a value of a binding-table type the registry
// legitimately declares. Each arrived as a hole, not as a hypothesis.
//
// A later review found the two rules that dispatch on an id disagreeing with
// each other. The switch rule carried both nets — the structural one (this
// value is an ID()) and the vocabulary one (this literal is a known id) — while
// the comparison rule carried only the vocabulary net. So `switch id` over an
// id nobody has declared yet was caught and `if id == "opencode"` was admitted,
// which is a strange place to draw a line: an if-chain is not an exotic
// spelling, it is the first thing many people write. Both rules now ask the
// same question through the same helper. The same review found the mirror of
// that asymmetry one level down, in what counted as "an ID() here": a
// call-wrapped tag resolved (`switch strings.TrimSpace(string(sys.ID()))`) and
// the identical tag over a local did not. isIDExpr is now the single answer to
// that question for every rule site.
//
// # Threat model
//
// This is a static, syntactic scan for ORDINARY Go spellings. It exists to
// catch an honest colleague reintroducing a second binding during ordinary
// work — the failure that cost the source repository a whole task to unwind.
// It is deliberately NOT a security boundary against an author trying to
// defeat it: someone who wants to hide a string from an AST scan has
// unbounded room (assemble it at runtime, decode it from bytes, route it
// through reflection, wrap it in a call this scanner does not evaluate), and
// chasing every one of those adds false-positive surface to a gate whose whole
// value is catching the mistake.
//
// Six residual classes are therefore DECLARED OPEN rather than left for a
// reader to discover, and TestSingleSourceGuardResidualGaps demonstrates each
// one staying open rather than asserting it in prose. A class named here but
// not demonstrated there would be this same defect one level up — prose a
// reader trusts, with nothing holding it to the code — so the list and that
// test are meant to be read against each other line for line:
//
//   - Call-wrapped literals: string([]byte("codex")), strings.TrimSpace("codex"),
//     fmt.Sprintf with no verbs. Neither foldStringConst nor
//     collectStringLiterals evaluates a call expression.
//   - Cross-package references: a const or var declared outside the scanned
//     source set and referenced through a qualified selector (other.CodexID).
//     Only the files handed to the scanner are resolved.
//   - Runtime assembly: a map built by appending or assigning in a loop from
//     values computed at runtime, or ids assembled from runes. This covers
//     `table[strings.Join(parts, "-")] = nil`, where the key-by-key assignment
//     rule sees an assignment into a table but has no literal to resolve.
//   - A REWRITTEN id variable: `id := sys.ID(); id = fold(id); switch id`.
//     idCallLocals resolves one binding that is never written to again, and
//     counts anything else — a second assignment, a range clause, taking the
//     address — as a write that drops the name. Approximating dataflow past a
//     rewrite is how a guard starts reporting ordinary code.
//   - A MULTI-HOP id variable: `raw := sys.ID(); id := raw; switch id`, and the
//     converting variant `id := sys.ID(); key := string(id); switch key`. Id
//     resolution is SINGLE-HOP by construction: a name resolves when its one
//     right-hand side reaches an ID() call without passing through another
//     name (isIDCall, which is isIDExpr with the local set withheld). A name
//     whose right-hand side is another local does not resolve, however many
//     conversions wrap it. This is a boundary, not an oversight: following
//     assignment chains is the start of dataflow analysis, and the source
//     repository already refused to enter that regress once. Note the shape it
//     does NOT cover — the hop count, not the conversions. One hop plus any
//     number of one-argument calls resolves and is in the mutant set; two hops
//     does not resolve and is here.
//   - An id that leaves through a HELPER: `func tag(sys System) string { return
//     string(sys.ID()) }` and then `switch tag(sys)`. Resolution is within one
//     function body plus the bodies it encloses; it does not follow returns
//     across function boundaries.
//
// The six split cleanly in two, and which half a residual falls in is the whole
// question of what the guard is still worth against it.
//
// The FIRST THREE — call-wrapped literals, cross-package references, runtime
// assembly — evade the VOCABULARY net only. They hide a string from the folding
// layer. The structural rules are untouched by all three: the same shadow table
// typed `map[SystemID]T`, and the same dispatch spelled on a value's ID(), are
// caught with no literal resolved at all. That is why the structural rules
// carry the weight and the id vocabulary is a second net rather than the only
// one, and the claim is worth exactly what its mutants show: every undeclared-id
// spelling in singlesource_guard_mutants_test.go uses ids outside
// knownPluginIDs, so only the structural rules can be catching them.
//
// The LAST THREE — the rewritten variable, the multi-hop variable, the helper —
// defeat the STRUCTURAL net itself, and all three at the same place: the value
// at the rule site no longer resolves back to an ID() call, so neither a switch
// tag nor a comparison operand reads as an id. Only the vocabulary net can fire
// there, and only for an id this file already knows. All three are demonstrated
// in the gaps test with ids OUTSIDE the vocabulary, so what those subtests show
// is the genuinely uncovered case rather than a spelling some other net happens
// to save.
//
// One boundary is a deliberate NON-goal rather than a residual: a switch on a
// method named something other than ID() — Identifier(), Name(), Slug(). ID()
// is what the System contract declares, and widening to arbitrary
// identity-shaped accessors is false-positive surface on code that has nothing
// to do with this invariant.
//
// # Scan scope
//
// The guard reports on the code THIS module's Go build compiles, and on nothing
// else. That scope is not a convenience; it is what makes a report actionable.
// A violation the toolchain never compiles is not a second binding in this
// module, and reporting one makes the suite's colour depend on what happens to
// be sitting on the developer's disk.
//
// It cost a red trunk to learn. The walk used to carry a denylist of specific
// directory names — .git, .temp, .task-board, .claude, .codex — which is a list
// of the machine-local trees somebody had thought of by then. A bootstrapped
// checkout has agents-infra installed at .agents/, gitignored and absent from
// every worktree, and its own
// tools/agents-infra/internal/infra/child_launch_composition.go dispatches on
// "codex" and "claude". The guard reported it, `go test ./...` went red on main
// and stayed green everywhere else, and the difference was whose machine ran it.
//
// skipModuleDir therefore mirrors go/build's own rules rather than naming
// directories:
//
//   - A directory whose name begins with "." or "_" is skipped, which is the
//     toolchain's rule verbatim. This is what excludes .agents, and it excludes
//     the next machine-local runtime nobody has installed yet without anyone
//     editing a list.
//   - A directory carrying its own go.mod is skipped with its whole subtree. A
//     nested module is a different module; this module's build compiles none of
//     it, and its bindings are its own registry's business.
//   - vendor and node_modules are skipped by name. Neither begins with a dot,
//     and both are dependency trees rather than this module's source.
//   - testdata is skipped by name. This is a DECISION, not an inheritance: go
//     build ignores testdata, so a binding planted there is not code this
//     module runs, and a guard fixture that wants a violating source is better
//     off writing it into a temp tree whose root it also controls — which is
//     exactly what TestSingleSourceGuardScanScope does. The cost is that a
//     genuine binding parked in testdata goes unreported; the benefit is that
//     the scan set and the build set are the same set, with no third rule.
//   - The module root is never skipped on its own name. A worktree under .temp/
//     is an ordinary place for this checkout to live, and testing the root's
//     name would scan zero files while reporting clean.
//
// TestSingleSourceGuardScanScope holds all of it in both directions over a
// fixture tree: the SAME violating source planted inside a dot-directory, an
// underscore-directory, a nested module, testdata and vendor is not reported,
// the identical source in an ordinary package IS, and every planted copy is
// first proven violating by scanning it directly — otherwise "not reported"
// would be equally consistent with a fixture that violates nothing.

const (
	bindingClassTable    = "binding-table"
	bindingClassIDSwitch = "id-switch"
)

// bindingHomes is THE list of the FACTS this module binds by, each mapped to
// the ONE file permitted to write that fact down.
//
// It is one list rather than one guard per layer. The vendor layer
// (pkg/vendorplugin) has the same invariant for the same reason — its registry
// is the only place a vendor or runtime binding may live — and a second guard
// file would be two implementations of one rule, drifting the moment either is
// extended. That is the failure this whole file exists to prevent, and
// reproducing it in the guard would be a poor joke.
//
// # Two kinds of entry, and what each one buys
//
// The first three entries are DISPATCH KEY TYPES. They are resolved as Go type
// names, so a `map[VendorID]T` — or a named type whose underlying type is one,
// or a make() of it, or a field of it — is a violation anywhere except that key
// type's home. The mapping is per KEY TYPE, not per file, and that is stricter
// than a flat allowlist: the file that binds systems is not thereby allowed to
// bind vendors. A map[VendorID]T inside pkg/agentic/registry.go is a shadow
// table even though that file legitimately holds a binding map, and
// TestSingleSourceGuardCatchesCrossLayerBindings plants exactly that.
//
// The rest are ID-SPELLING homes: tables that must name plugin ids as literals
// to say what they say. Each vendor's model rows are one such fact, and the
// guard's composite-literal rule reports a plugin id spelled in a composite
// literal in any file that is not one of these homes. Their keys are
// deliberately not legal Go identifiers, so naming one here can never
// accidentally teach the scanner a new key type;
// TestSingleSourceGuardHomesSplitByKind holds that line.
//
// pkg/vendorplugin/v2snapshot.go spells runtime ids too and is deliberately NOT
// listed. Its ids are STRUCT FIELD VALUES rather than bare composite-literal
// elements, which the literal rule does not reach — the same shape the frozen
// runtime table in pkg/vendorplugin/registry.go has — so an entry for it would
// grant a permission the file never uses and that no mutant could show
// mattering. What does hold that file is the structural rule: written as the
// map[RuntimeID][][]string it obviously wants to be, it fails the guard, which
// is exactly why it is a slice.
//
// Why the vendor rows need one at all: a vendor plugin's model list DECLARES
// which agentic systems drive each model, which is a binding in the plain sense
// — it is the thing Registry.Register checks the dependency direction against.
// Giving each vendor exactly one such file is AC5 of TASK-260822-3cknas, and
// this list is where "exactly one" is enforced rather than hoped for.
var bindingHomes = map[string]string{
	"SystemID":  "pkg/agentic/registry.go",
	"VendorID":  "pkg/vendorplugin/registry.go",
	"RuntimeID": "pkg/vendorplugin/registry.go",

	"anthropic models": "pkg/vendorplugin/vendors/anthropic/models.go",
	"openai models":    "pkg/vendorplugin/vendors/openai/models.go",
	"alibaba models":   "pkg/vendorplugin/vendors/alibaba/models.go",
	"google models":    "pkg/vendorplugin/vendors/google/models.go",
}

// dispatchKeyTypes are the entries of bindingHomes that name a Go type the
// scanner resolves as a binding key. Everything else in that map is an
// id-spelling home; see the comment above.
var dispatchKeyTypes = []string{"SystemID", "VendorID", "RuntimeID"}

// vendorBindingHomes is the per-vendor half, one entry per registered vendor
// plugin. It is written out separately so "one binding file per vendor" is a
// checkable claim rather than a pattern a reader has to notice.
var vendorBindingHomes = map[string]string{
	"anthropic": "pkg/vendorplugin/vendors/anthropic/models.go",
	"openai":    "pkg/vendorplugin/vendors/openai/models.go",
	"alibaba":   "pkg/vendorplugin/vendors/alibaba/models.go",
	"google":    "pkg/vendorplugin/vendors/google/models.go",
}

// knownPluginIDs is the identifier vocabulary a reintroduced binding would
// spell. It holds the agentic system ids docs/architecture.md names, the
// frozen runtime ids that feed admitted-pair digests and limit-state
// filenames, and the vendor ids those runtimes are bound to.
//
// Matching is EXACT on the trimmed, lowercased value rather than by substring.
// Substring matching would flag ordinary prose that happens to contain "codex"
// or "muse" — a guard with that many false positives is a guard somebody
// deletes — while an accidental reintroduction spells the id exactly, because
// it has to for the lookup to work.
var knownPluginIDs = map[string]bool{
	"claude-code": true,
	"codex":       true,
	"qwen-code":   true,
	"gemini-cli":  true,
	"antigravity": true,
	"muse":        true,
	"claude":      true,
	"qwen":        true,
	"gemini":      true,
	"agy":         true,
	"qwen-codex":  true,
	"anthropic":   true,
	"openai":      true,
	"alibaba":     true,
	"google":      true,
}

// bindingViolation is one place a second binding was found.
type bindingViolation struct {
	File   string
	Where  string
	Line   int
	Class  string
	Detail string
}

func (v bindingViolation) String() string {
	return fmt.Sprintf("%s:%d [%s] %s: %s", v.File, v.Line, v.Class, v.Where, v.Detail)
}

// scanSingleSource parses every source and reports each second binding it
// finds. sources maps a display filename to Go source text, so the same
// scanner runs over the real tree and over synthetic mutant corpora.
//
// homes is the key-type-to-file mapping described on bindingHomes: each
// dispatch key type, and the one display filename permitted to bind it. A file
// that is the home of ANY key type may also spell plugin ids in a literal,
// because that is what a registry and its seed declarations do. Nothing
// exempts a file from the id-switch rules: a registry has no business
// dispatching on identifiers either.
func scanSingleSource(sources map[string]string, homes map[string]string) ([]bindingViolation, error) {
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
	consts := resolveConstStrings(all)
	vars := resolveVarStrings(all, consts)
	keyTypes := resolveKeyTypeNames(all, homes)
	bindingTypes := resolveBindingTableTypeNames(all, keyTypes)

	// A binding is a violation unless it is in the home declared for ITS key
	// type. spellsIDs is the weaker permission a home file also carries: the
	// right to write a plugin id as a literal, which the seed declarations of
	// the frozen runtime table need and no other file does.
	misplaced := func(key, file string) bool { return homes[key] != file }
	spellsIDs := func(file string) bool {
		for _, home := range homes {
			if home == file {
				return true
			}
		}
		return false
	}

	var violations []bindingViolation
	seen := map[string]bool{}
	record := func(file, where string, pos token.Pos, class, detail string) {
		line := fset.Position(pos).Line
		key := fmt.Sprintf("%s\x00%d\x00%s", file, line, class)
		if seen[key] {
			return
		}
		seen[key] = true
		violations = append(violations, bindingViolation{File: file, Where: where, Line: line, Class: class, Detail: detail})
	}

	for _, name := range names {
		file := parsed[name]
		literalsAllowed := spellsIDs(name)
		idIdents := resolveIDCallIdents(file)
		// dispatchesOnID is the structural half of the id-switch rules, shared
		// by the switch tag and the comparison operands so that the same fact —
		// "this expression delivers a system's ID() here" — cannot be true for
		// one spelling and false for the other. It was not shared once, and the
		// asymmetry that produced is recorded in the threat model above.
		dispatchesOnID := func(expr ast.Expr) bool { return isIDExpr(expr, idIdents) }
		for _, decl := range file.Decls {
			where := declName(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.MapType:
					if key, ok := bindingKeyRef(node.Key, keyTypes); ok && misplaced(key, name) {
						record(name, where, node.Pos(), bindingClassTable,
							fmt.Sprintf("a map keyed by %s; %s is the only place a %s binding may live", key, homeLabel(homes, key), key))
					}
				case *ast.CompositeLit:
					if node.Type != nil {
						if key, ok := bindingTableKeyRef(node.Type, bindingTypes); ok && misplaced(key, name) {
							record(name, where, node.Pos(), bindingClassTable,
								fmt.Sprintf("a composite literal of a named type whose underlying type maps %s to behaviour", key))
						}
					}
					if literalsAllowed {
						return true
					}
					for _, elt := range node.Elts {
						target, position := elt, "element"
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							target, position = kv.Key, "key"
						}
						if id, ok := knownPluginIDOf(target, consts, vars); ok {
							record(name, where, node.Pos(), bindingClassTable,
								fmt.Sprintf("a composite literal with the plugin id %q as a %s; a registry is the only place the set of plugins may be spelled", id, position))
						}
					}
				case *ast.SwitchStmt:
					if node.Tag != nil && dispatchesOnID(node.Tag) && node.Body != nil && len(node.Body.List) > 0 {
						record(name, where, node.Pos(), bindingClassIDSwitch,
							"a switch on a system's ID(); dispatch belongs in the registry, not in a switch")
					}
					if node.Body == nil {
						return true
					}
					for _, stmt := range node.Body.List {
						clause, ok := stmt.(*ast.CaseClause)
						if !ok {
							continue
						}
						for _, value := range clause.List {
							if id, ok := knownPluginIDOf(value, consts, vars); ok {
								record(name, where, node.Pos(), bindingClassIDSwitch,
									fmt.Sprintf("a switch case on the plugin id %q", id))
							}
						}
					}
				case *ast.CallExpr:
					// make(bindings) — a binding table brought into existence
					// with no map type and no literal anywhere in this file,
					// because the type it instantiates is legitimately
					// declared in the registry. Found by a review mutant that
					// walked through the composite-literal rule.
					if len(node.Args) == 0 {
						return true
					}
					if fn, ok := unparenExpr(node.Fun).(*ast.Ident); ok && fn.Name == "make" {
						if key, ok := bindingTableKeyRef(node.Args[0], bindingTypes); ok && misplaced(key, name) {
							record(name, where, node.Pos(), bindingClassTable,
								fmt.Sprintf("make() of a named type whose underlying type maps %s to behaviour", key))
						}
					}
				case *ast.ValueSpec:
					// var shadow bindings — declared, never initialized, so
					// there is nothing for a literal rule to see.
					if node.Type != nil {
						if key, ok := bindingTableKeyRef(node.Type, bindingTypes); ok && misplaced(key, name) {
							record(name, where, node.Pos(), bindingClassTable,
								fmt.Sprintf("a variable of a named type whose underlying type maps %s to behaviour", key))
						}
					}
				case *ast.Field:
					// The same type held as a struct field or returned from a
					// signature. A binding reachable from a type is a binding.
					if node.Type != nil {
						if key, ok := bindingTableKeyRef(node.Type, bindingTypes); ok && misplaced(key, name) {
							record(name, where, node.Pos(), bindingClassTable,
								fmt.Sprintf("a field or signature of a named type whose underlying type maps %s to behaviour", key))
						}
					}
				case *ast.AssignStmt:
					// A table assembled key by key rather than written as a
					// literal. This is not a hypothetical spelling: the
					// extraction source's own adapterTable is built this way,
					// from an init(), for a documented compile-time reason —
					// a var initializer there would have created a spurious
					// initialization cycle. A colleague reintroducing a
					// binding in this module has a live precedent for exactly
					// the shape a composite-literal rule cannot see.
					if literalsAllowed {
						return true
					}
					for _, lhs := range node.Lhs {
						index, ok := unparenExpr(lhs).(*ast.IndexExpr)
						if !ok {
							continue
						}
						if id, ok := knownPluginIDOf(index.Index, consts, vars); ok {
							record(name, where, node.Pos(), bindingClassTable,
								fmt.Sprintf("an assignment into a table at the plugin id %q; a table filled in key by key is a binding table spelled without a literal", id))
						}
					}
				case *ast.BinaryExpr:
					if node.Op != token.EQL && node.Op != token.NEQ {
						return true
					}
					for _, side := range []ast.Expr{node.X, node.Y} {
						if id, ok := knownPluginIDOf(side, consts, vars); ok {
							record(name, where, node.Pos(), bindingClassIDSwitch,
								fmt.Sprintf("a comparison against the plugin id %q; an if-else chain over ids is a switch spelled differently", id))
						}
					}
					// The structural half. Without it this rule resolved only through
					// the id vocabulary, so dispatch on an id nobody has declared yet
					// was caught spelled `switch` and missed spelled
					// `if id == "opencode"` — and an if-chain is not an exotic
					// spelling, it is the first thing half of programmers write. A
					// review found the asymmetry; its own mutant set had missed it
					// because every if-chain mutant used a KNOWN id, which the
					// vocabulary net caught for the wrong reason.
					//
					// The other operand must fold to a non-blank compile-time string.
					// `sys.ID() == ""` is an emptiness check, not dispatch, and
					// flagging it is the false-positive surface that gets a guard
					// deleted; TestSingleSourceGuardAcceptsOrdinaryCode holds that
					// carve-out in place. Comparing two ID() calls to each other folds
					// to nothing on either side and stays silent for the same reason.
					if !dispatchesOnID(node.X) && !dispatchesOnID(node.Y) {
						return true
					}
					for _, side := range []ast.Expr{node.X, node.Y} {
						value, ok := foldStringConst(side, consts)
						if !ok || strings.TrimSpace(value) == "" {
							continue
						}
						record(name, where, node.Pos(), bindingClassIDSwitch,
							fmt.Sprintf("a comparison of a system's ID() against %q; dispatch on an id belongs in the registry, and an id nobody has declared yet is still an id", value))
					}
				}
				return true
			})
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		if violations[i].Line != violations[j].Line {
			return violations[i].Line < violations[j].Line
		}
		return violations[i].Class < violations[j].Class
	})
	return violations, nil
}

// declName labels a violation with the declaration it was found in, so a
// report points at a name a reader can grep for rather than only a line.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			return "method " + d.Name.Name
		}
		return "func " + d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				if len(s.Names) > 0 {
					return d.Tok.String() + " " + s.Names[0].Name
				}
			case *ast.TypeSpec:
				if s.Name != nil {
					return "type " + s.Name.Name
				}
			}
		}
		return d.Tok.String() + " declaration"
	default:
		return "declaration"
	}
}

// bindingKeyRef reports whether a type expression denotes one of the dispatch
// key types in bindingHomes — directly, through a qualified selector, or
// through a name declared to be one — and which key type it is.
//
// The qualified spelling is accepted on the selector name alone: only this
// module declares SystemID, VendorID and RuntimeID, and a scan that insisted
// on resolving the qualifier would miss `agentic.SystemID` in every file that
// imports the package under any name.
func bindingKeyRef(expr ast.Expr, keyTypes map[string]string) (string, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		key, ok := keyTypes[typed.Name]
		return key, ok
	case *ast.SelectorExpr:
		if typed.Sel == nil {
			return "", false
		}
		key, ok := keyTypes[typed.Sel.Name]
		return key, ok
	case *ast.ParenExpr:
		return bindingKeyRef(typed.X, keyTypes)
	default:
		return "", false
	}
}

// bindingTableKeyRef reports whether a type expression names a type whose
// underlying type maps a dispatch key type to something — the alias spelling
// of a shadow table, where the map type itself is declared once (possibly in
// the home file) and instantiated elsewhere.
func bindingTableKeyRef(expr ast.Expr, bindingTypes map[string]string) (string, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		key, ok := bindingTypes[typed.Name]
		return key, ok
	case *ast.ParenExpr:
		return bindingTableKeyRef(typed.X, bindingTypes)
	default:
		return "", false
	}
}

// homeLabel renders the file a key type may be bound in, so a violation says
// where the binding belongs rather than only where it must not be.
func homeLabel(homes map[string]string, key string) string {
	if home, ok := homes[key]; ok && home != "" {
		return home
	}
	return "no file in this scan"
}

// isIDExpr reports whether an expression delivers a system's ID() at the point
// it appears — the call itself (`sys.ID()`, `plugin.ID()`), a one-argument call
// wrapped around it (`string(sys.ID())`, `strings.TrimSpace(string(sys.ID()))`),
// or an identifier idIdents resolved to such a call. It is the structural half
// of the id-switch rules and, unlike the vocabulary half, keeps working for
// system ids nobody has declared yet.
//
// A one-argument call is looked through because the conversion spelling is as
// ordinary as the bare one and dispatching on a converted id is still
// dispatching on the id. That look-through applies to a resolved LOCAL for the
// same reason: `switch strings.TrimSpace(string(sys.ID()))` and
// `id := string(sys.ID()); switch strings.TrimSpace(id)` are the same code, and
// a review found the guard catching the first and admitting the second.
//
// It is deliberately NOT widened to other accessor names: `ID()` is what the
// System contract declares, and matching arbitrary identity-shaped method names
// is pure false-positive surface on code that has nothing to do with this
// invariant.
func isIDExpr(expr ast.Expr, idIdents map[*ast.Ident]bool) bool {
	switch typed := expr.(type) {
	case *ast.ParenExpr:
		return isIDExpr(typed.X, idIdents)
	case *ast.Ident:
		return idIdents[typed]
	case *ast.CallExpr:
		if len(typed.Args) == 1 {
			return isIDExpr(typed.Args[0], idIdents)
		}
		if len(typed.Args) != 0 {
			return false
		}
		selector, ok := typed.Fun.(*ast.SelectorExpr)
		return ok && selector.Sel != nil && selector.Sel.Name == "ID"
	default:
		return false
	}
}

// isIDCall is isIDExpr with NO local resolution: an expression that reaches an
// ID() call without passing through a name. idCallLocals resolves a local's
// right-hand side with this rather than with isIDExpr, and that is the single
// line that keeps id resolution SINGLE-HOP — `raw := sys.ID(); id := raw` does
// not resolve, because `raw` is a name and this function does not read names.
// Chasing transitive assignment chains is the unbounded regress the source
// repository already refused to enter; the multi-hop class is declared open in
// the threat model above and demonstrated in TestSingleSourceGuardResidualGaps.
func isIDCall(expr ast.Expr) bool { return isIDExpr(expr, nil) }

// unparenExpr strips redundant parentheses so a rule reads the expression a
// human sees rather than the punctuation around it.
func unparenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

// resolveIDCallIdents reports every identifier OCCURRENCE in one file that
// holds the result of an ID() call — the set isIDExpr consults so that a name
// bound to an id reads as the id wherever it is used.
//
// Without this, `switch sys.ID()` was caught and the one-line variant
//
//	id := sys.ID()
//	switch id { case "opencode": ... }
//
// walked straight through — for an id outside the guard's vocabulary, with no
// net at all. Binding the call to a name first is at least as idiomatic as
// switching on it, so it is an ORDINARY spelling, not obfuscation, and the
// structural rule has to survive it or the claim that the rule works for
// undeclared ids is false.
//
// The result is keyed by the *ast.Ident node rather than by name so that a rule
// asking about one operand cannot be answered about a same-named identifier
// somewhere else in the file. Occurrences that are not operands of a rule — the
// binding site itself, a selector's field name that happens to match — are
// marked and never consulted, which costs nothing and keeps this walk free of
// scope-shaped special cases.
//
// Resolution is deliberately shallow: one binding, directly from an ID() call,
// never rewritten. See idCallLocals for what that excludes and why.
func resolveIDCallIdents(file *ast.File) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body != nil {
				collectIDCallIdents(fn.Body, nil, out)
			}
			continue
		}
		// A function literal in a package-level declaration —
		// `var boot = register(func(sys System) { ... })` — is a function body
		// too, and the source repository's guard was defeated once by exactly
		// that placement. Nested literals are reached by the recursion below,
		// so this stops at the outermost one.
		ast.Inspect(decl, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok {
				return true
			}
			if lit.Body != nil {
				collectIDCallIdents(lit.Body, nil, out)
			}
			return false
		})
	}
	return out
}

// collectIDCallIdents walks one function body with the names its enclosing
// bodies resolved, so a closure dispatching on an id its parent bound reads the
// same as the parent doing it inline.
func collectIDCallIdents(body *ast.BlockStmt, inherited map[string]bool, out map[*ast.Ident]bool) {
	locals := idCallLocals(body, inherited)
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			if node.Body != nil {
				collectIDCallIdents(node.Body, locals, out)
			}
			return false
		case *ast.Ident:
			if locals[node.Name] {
				out[node] = true
			}
		}
		return true
	})
}

// idCallLocals reports the names a function body binds EXACTLY ONCE, to an
// ID() call, and never writes to again — the ordinary spelling of copying an
// id into a variable before switching on it.
//
// Every other flow is counted as a write and drops the name: a second
// assignment, `++`, a range clause binding it, taking its address. That is
// intentionally not dataflow analysis. A name that is rewritten no longer
// certainly holds the id at the switch, and a guard that guessed would report
// ordinary code. Those flows, and a value that leaves through a helper
// function, stay in the declared-open residuals rather than being approximated
// here; TestSingleSourceGuardResidualGaps demonstrates them.
func idCallLocals(body *ast.BlockStmt, inherited map[string]bool) map[string]bool {
	writes := map[string]int{}
	fromIDCall := map[string]bool{}
	note := func(name string, value ast.Expr) {
		if name == "" || name == "_" {
			return
		}
		writes[name]++
		if value != nil && isIDCall(value) {
			fromIDCall[name] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			// A nested body binds its own names and gets its own pass.
			return false
		case *ast.AssignStmt:
			paired := len(node.Lhs) == len(node.Rhs)
			for i, lhs := range node.Lhs {
				ident, ok := unparenExpr(lhs).(*ast.Ident)
				if !ok {
					continue
				}
				if paired && (node.Tok == token.DEFINE || node.Tok == token.ASSIGN) {
					note(ident.Name, node.Rhs[i])
					continue
				}
				// A multi-value right-hand side, or an operator assignment:
				// the name is written, and by nothing this reads as an id.
				note(ident.Name, nil)
			}
		case *ast.GenDecl:
			if node.Tok != token.VAR {
				return true
			}
			for _, spec := range node.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				paired := len(vs.Names) == len(vs.Values)
				for i, ident := range vs.Names {
					if paired {
						note(ident.Name, vs.Values[i])
						continue
					}
					note(ident.Name, nil)
				}
			}
		case *ast.IncDecStmt:
			if ident, ok := unparenExpr(node.X).(*ast.Ident); ok {
				note(ident.Name, nil)
			}
		case *ast.RangeStmt:
			for _, expr := range []ast.Expr{node.Key, node.Value} {
				if expr == nil {
					continue
				}
				if ident, ok := unparenExpr(expr).(*ast.Ident); ok {
					note(ident.Name, nil)
				}
			}
		case *ast.UnaryExpr:
			// &id hands the name to something that can write through it.
			if node.Op == token.AND {
				if ident, ok := unparenExpr(node.X).(*ast.Ident); ok {
					note(ident.Name, nil)
				}
			}
		}
		return true
	})
	locals := map[string]bool{}
	for name := range inherited {
		locals[name] = true
	}
	for name, count := range writes {
		if count == 1 && fromIDCall[name] {
			locals[name] = true
			continue
		}
		// Shadowing or rewriting a name an enclosing body resolved makes it
		// stop reading as that id here.
		delete(locals, name)
	}
	return locals
}

// resolveKeyTypeNames collects every type name that IS one of the dispatch key
// types, whether spelled canonically, defined (`type sid SystemID`) or aliased
// (`type sid = SystemID`), as a bounded fixpoint so a chain of them resolves
// regardless of declaration order. The canonical names seed the set, so a key
// type resolves even in a corpus that never declares it.
func resolveKeyTypeNames(files []*ast.File, homes map[string]string) map[string]string {
	names := map[string]string{}
	for key := range homes {
		names[key] = key
	}
	type pending struct {
		name string
		expr ast.Expr
	}
	var queue []pending
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil || ts.Type == nil {
					continue
				}
				queue = append(queue, pending{ts.Name.Name, ts.Type})
			}
		}
	}
	for pass := 0; pass <= len(queue); pass++ {
		changed := false
		for _, item := range queue {
			if _, done := names[item.name]; done {
				continue
			}
			if key, ok := bindingKeyRef(item.expr, names); ok {
				names[item.name] = key
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return names
}

// resolveBindingTableTypeNames collects the type names whose underlying type
// maps a dispatch key type to something, following named types to their
// declarations as a bounded fixpoint, and reports which key type each binds.
func resolveBindingTableTypeNames(files []*ast.File, keyTypes map[string]string) map[string]string {
	type pending struct {
		name string
		expr ast.Expr
	}
	var queue []pending
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil || ts.Type == nil {
					continue
				}
				queue = append(queue, pending{ts.Name.Name, ts.Type})
			}
		}
	}
	names := map[string]string{}
	for pass := 0; pass <= len(queue); pass++ {
		changed := false
		for _, item := range queue {
			if _, done := names[item.name]; done {
				continue
			}
			switch typed := item.expr.(type) {
			case *ast.MapType:
				if key, ok := bindingKeyRef(typed.Key, keyTypes); ok {
					names[item.name] = key
					changed = true
				}
			case *ast.Ident:
				if key, ok := names[typed.Name]; ok {
					names[item.name] = key
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return names
}

// knownPluginIDOf resolves an expression to a plugin id from the vocabulary,
// through a string literal, a package-level const, a package-level var, or a
// concatenation of those. It is the indirection layer four review rounds in
// the source repository were spent adding.
func knownPluginIDOf(expr ast.Expr, consts map[string]string, vars map[string][]string) (string, bool) {
	if value, ok := foldStringConst(expr, consts); ok {
		if id := normalizeCandidate(value); knownPluginIDs[id] {
			return id, true
		}
	}
	// A bare identifier reads as any string its package-level var can hold;
	// an index into one — supported[0] — reads the same way, because the
	// index is exactly the spelling that hides which element was taken.
	var name string
	switch typed := expr.(type) {
	case *ast.Ident:
		name = typed.Name
	case *ast.IndexExpr:
		if base, ok := typed.X.(*ast.Ident); ok {
			name = base.Name
		}
	case *ast.ParenExpr:
		return knownPluginIDOf(typed.X, consts, vars)
	}
	if name == "" {
		return "", false
	}
	for _, value := range vars[name] {
		if id := normalizeCandidate(value); knownPluginIDs[id] {
			return id, true
		}
	}
	return "", false
}

func normalizeCandidate(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// resolveConstStrings folds every string const across the given files, in
// either declaration order and across grouped blocks, so a literal hidden
// behind a named constant reads the same as a literal spelled inline.
//
// The walk is over every node rather than over file.Decls, so a const declared
// INSIDE a function body resolves too. Restricting it to package level was a
// hole: `func pick(id SystemID) { const codexID = "codex"; if id == codexID }`
// is the same evasion one scope down, and a reader of the threat model would
// have concluded const indirection was closed.
//
// Names are folded into one flat scope rather than per-function, which is
// imprecise in exactly one direction: two function-local consts sharing a name
// with different values resolve to whichever the fixpoint reaches first, so a
// key or case value written as that name can be reported against the other
// one's value. That trades a rare false positive for closing the evasion, and
// the false positive is a violation report naming a real line a reader can
// judge — not a silent miss.
func resolveConstStrings(files []*ast.File) map[string]string {
	type pending struct {
		name string
		expr ast.Expr
	}
	var queue []pending
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			gen, ok := n.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				return true
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, name := range vs.Names {
					queue = append(queue, pending{name.Name, vs.Values[i]})
				}
			}
			return true
		})
	}
	consts := map[string]string{}
	for pass := 0; pass <= len(queue); pass++ {
		changed := false
		for _, item := range queue {
			if _, done := consts[item.name]; done {
				continue
			}
			if value, ok := foldStringConst(item.expr, consts); ok {
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

// foldStringConst resolves an expression to one compile-time string when it is
// a literal, a reference to an already-folded const, or a `+` chain of those.
// It stops at anything else — a call, a non-ADD operator, an index, a selector
// — which is the declared boundary: this folds literal aliasing, not dataflow.
func foldStringConst(expr ast.Expr, consts map[string]string) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(typed.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := consts[typed.Name]
		return value, ok
	case *ast.ParenExpr:
		return foldStringConst(typed.X, consts)
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return "", false
		}
		left, ok := foldStringConst(typed.X, consts)
		if !ok {
			return "", false
		}
		right, ok := foldStringConst(typed.Y, consts)
		if !ok {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

// resolveVarStrings extends const resolution to vars: every string reachable
// inside a var's initializer is attributed to that var's name, so a case
// clause or a map key written as a bare identifier reads as the values that
// identifier can hold.
//
// Like resolveConstStrings this walks every node, so a `var` declared inside a
// function body is resolved as well. Unlike consts the mapping is a set rather
// than a single value, so two same-named locals widen the set instead of
// racing for it: a name reads as any string any declaration of it can hold.
func resolveVarStrings(files []*ast.File, consts map[string]string) map[string][]string {
	vars := map[string][]string{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			gen, ok := n.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				return true
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, name := range vs.Names {
					if values := collectStringLiterals(vs.Values[i], consts); len(values) > 0 {
						vars[name.Name] = append(vars[name.Name], values...)
					}
				}
			}
			return true
		})
	}
	return vars
}

// collectStringLiterals walks an initializer structurally for the strings it
// contains: slice and array elements, map and struct keys and values, and both
// sides of a concatenation that did not fold whole. It does not evaluate calls
// or index expressions — the first of the declared-open residuals.
func collectStringLiterals(expr ast.Expr, consts map[string]string) []string {
	if value, ok := foldStringConst(expr, consts); ok {
		return []string{value}
	}
	var out []string
	switch typed := expr.(type) {
	case *ast.ParenExpr:
		out = append(out, collectStringLiterals(typed.X, consts)...)
	case *ast.BinaryExpr:
		out = append(out, collectStringLiterals(typed.X, consts)...)
		out = append(out, collectStringLiterals(typed.Y, consts)...)
	case *ast.CompositeLit:
		for _, elt := range typed.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				out = append(out, collectStringLiterals(kv.Key, consts)...)
				out = append(out, collectStringLiterals(kv.Value, consts)...)
				continue
			}
			out = append(out, collectStringLiterals(elt, consts)...)
		}
	}
	return out
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod. A guard that scanned only its own package directory would
// leave every other package unguarded, which is how a shadow table gets in.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := gosources.Root(dir)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return root
}

// moduleSources reads every non-test Go source in the module, discovered by
// walking rather than from a list of directories: a package added later must
// not be silently unscanned, and a hardcoded list is exactly how that happens.
func moduleSources(t *testing.T) map[string]string {
	t.Helper()
	root := moduleRoot(t)
	sources, err := walkModuleSources(root)
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return sources
}

// skipModuleDir answers whether the walk refuses to descend into dir, which is
// the one question that decides what "the whole module" means.
//
// It delegates to internal/gosources, which is where the rule now lives for the
// whole repository. It was here first, and it moved when the second system
// plugin's argv guard needed the same answer: two walks with two skip lists are
// two definitions of "the whole module", and both report clean when they
// disagree. The rules themselves, and the red trunk that produced them, are
// documented on gosources.SkipDir; the scan-scope section of this file's threat
// model states what they buy this guard.
func skipModuleDir(root, path, name string) (bool, error) {
	return gosources.SkipDir(root, path, name)
}

// walkModuleSources collects every non-test Go source under root that the Go
// build of the module rooted there would compile, keyed by slash-separated path
// relative to root.
//
// It takes root as an argument rather than finding it, so the exclusion rules
// can be driven over a fixture tree with planted violations instead of only
// over the real checkout, where a dot-directory may or may not exist depending
// on whose machine is running the suite.
func walkModuleSources(root string) (map[string]string, error) {
	return gosources.Walk(root)
}

// The allowlist that used to live here — one file, permitted to hold a system
// binding — is now bindingHomes at the top of this file, one entry per
// dispatch key type.
//
// It gained entries, and that was the change this comment demanded be argued
// rather than absorbed. The argument: the invariant is one home per FACT, and
// "which plugin implements system X", "which plugin implements vendor Y" and
// "which pair is runtime Z" are three facts, not one. A single flat allowlist
// would have been the weaker rule — it would let the systems registry bind
// vendors — so the list is keyed by the fact, and each fact still has exactly
// one home. What did NOT change is that a file is never allowed to dispatch on
// an identifier, in any layer.

// TestSingleSourceGuardScansTheWholeModule proves the scan reaches what it
// claims to, so a clean result means "nothing found" rather than "nothing
// looked at". A guard that silently scans zero files reports clean forever.
func TestSingleSourceGuardScansTheWholeModule(t *testing.T) {
	sources := moduleSources(t)
	for _, required := range []string{
		"pkg/agentic/registry.go",
		"pkg/agentic/plan.go",
		"pkg/agentic/system.go",
		// The system plugins, ONE BINDING FILE EACH. They are the files most
		// likely to grow a second binding — a plugin is where somebody reaches
		// for "switch on the id" — so a scan that did not reach them would
		// leave the guard's whole subject unguarded while every other assertion
		// here stayed green.
		//
		// Every plugin in this module is listed, and the list is what makes the
		// claim checkable: a seventh plugin added without a line here would be
		// a package the guard walks but nothing insists it walks, and the day
		// the scan scope narrows for some other reason, that plugin would go
		// unguarded silently.
		"pkg/agentic/systems/codex/codex.go",
		"pkg/agentic/systems/claude/claude.go",
		"pkg/agentic/systems/qwen/qwen.go",
		"pkg/agentic/systems/gemini/gemini.go",
		"pkg/agentic/systems/muse/muse.go",
		"pkg/agentic/systems/agy/agy.go",
		"pkg/vendorplugin/registry.go",
		"pkg/vendorplugin/runtime.go",
		"pkg/vendorplugin/spawn.go",
		"pkg/vendorplugin/vendor.go",
		"pkg/vendorplugin/admission.go",
		"pkg/vendorplugin/v2snapshot.go",
		// The vendor plugins, ONE BINDING FILE EACH, for the same reason the
		// system plugins are listed above: a model table is where somebody
		// reaches for a second copy of "which harness drives this", and a scan
		// that did not reach these files would leave four tables unguarded
		// while every other assertion here stayed green.
		"pkg/vendorplugin/vendors/anthropic/models.go",
		"pkg/vendorplugin/vendors/openai/models.go",
		"pkg/vendorplugin/vendors/alibaba/models.go",
		"pkg/vendorplugin/vendors/google/models.go",
		"tools/agents-management/cmd/plugins.go",
		"tools/agents-management/cmd/root.go",
		"tools/agents-management/main.go",
	} {
		if _, ok := sources[required]; !ok {
			t.Errorf("the module scan did not reach %s; a file the guard never reads is a file it never guards", required)
		}
	}
	if len(sources) < 6 {
		t.Fatalf("scanned %d sources, which is too few for the guard's claim to mean anything", len(sources))
	}
}

// TestSingleSourceGuardFindsNoSecondBinding is the guard itself.
func TestSingleSourceGuardFindsNoSecondBinding(t *testing.T) {
	violations, err := scanSingleSource(moduleSources(t), bindingHomes)
	if err != nil {
		t.Fatalf("scanSingleSource: %v", err)
	}
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("a second agentic system binding exists outside the registry:\n  %s", strings.Join(lines, "\n  "))
}

// TestSingleSourceGuardRulesFireOnRealCode narrows the gate instead of
// deleting it: the same real sources are rescanned with every key type's home
// moved to a file that does not exist, and the real registries must then be
// reported. Without this, a green result above is equally consistent with "the
// rule never matches anything", and the guard would be worth nothing while
// looking healthy.
//
// Displacing the homes rather than emptying the list is the load-bearing
// detail. An empty list resolves NO key types at all, so an empty result would
// mean "the scanner had nothing to look for" — which is precisely the
// vacuously-green failure this test exists to rule out.
func TestSingleSourceGuardRulesFireOnRealCode(t *testing.T) {
	displaced := map[string]string{}
	for key := range bindingHomes {
		displaced[key] = "no/such/file.go"
	}
	violations, err := scanSingleSource(moduleSources(t), displaced)
	if err != nil {
		t.Fatalf("scanSingleSource: %v", err)
	}
	// Every layer's real registry, so the extension to the vendor key types is
	// held to the same standard as the original: a rule that fires only on
	// pkg/agentic would leave the vendor bindings unguarded while this test
	// stayed green.
	// Every layer's real registry AND every id-spelling home. The vendor
	// tables are here for the same reason the registries are: displaced, each
	// one's `[]agentic.SystemID{"claude-code"}` is a plugin id spelled outside
	// a home, so a file that stays silent under displacement is a file the
	// composite-literal rule cannot see — and its silence in the real run
	// would then mean nothing at all.
	required := []string{"pkg/agentic/registry.go", "pkg/vendorplugin/registry.go"}
	for _, home := range vendorBindingHomes {
		required = append(required, home)
	}
	sort.Strings(required)
	for _, file := range required {
		found := false
		for _, v := range violations {
			if v.File == file && v.Class == bindingClassTable {
				found = true
			}
		}
		if !found {
			t.Errorf("with the homes displaced, %s's own binding table was not reported; the binding-table rule does not fire on that file's real code, so its silence elsewhere proves nothing. violations=%v", file, violations)
		}
	}
}

// TestSingleSourceGuardHomesAreDistinctFacts holds the shape of bindingHomes
// itself: every key type has exactly one home, and a home named there is a
// file that actually exists in the module scan.
//
// A home pointing at a path nobody writes to would silently turn its key type
// into "bindable nowhere", which reads as a stricter guard and is in fact an
// unguarded one the day the file is renamed.
func TestSingleSourceGuardHomesAreDistinctFacts(t *testing.T) {
	sources := moduleSources(t)
	for key, home := range bindingHomes {
		if _, ok := sources[home]; !ok {
			t.Errorf("bindingHomes[%q] = %q, which the module scan never reached", key, home)
		}
	}
	for _, key := range dispatchKeyTypes {
		if _, ok := bindingHomes[key]; !ok {
			t.Errorf("%s is a dispatch key type of this module and has no home in bindingHomes; it is bindable anywhere", key)
		}
	}
}

// TestSingleSourceGuardHomesSplitByKind holds the line between the two kinds of
// entry bindingHomes carries.
//
// A dispatch key type is resolved as a Go TYPE NAME, so adding an entry whose
// key happens to spell one would silently teach the scanner a fourth key type
// and, worse, declare its home in one move. The id-spelling homes are therefore
// keyed by strings that cannot be Go identifiers, and this test is what keeps
// that from being a convention somebody breaks by writing the obvious thing.
func TestSingleSourceGuardHomesSplitByKind(t *testing.T) {
	declared := map[string]bool{}
	for _, key := range dispatchKeyTypes {
		declared[key] = true
	}
	for key := range bindingHomes {
		if declared[key] {
			continue
		}
		if isGoIdentifier(key) {
			t.Errorf("bindingHomes[%q] is not a declared dispatch key type but spells a legal Go identifier; the scanner resolves such a key as a TYPE NAME, so this entry silently declares a fourth binding key type and its home at the same time", key)
		}
	}
	for _, key := range dispatchKeyTypes {
		if !isGoIdentifier(key) {
			t.Errorf("dispatchKeyTypes names %q, which is not a legal Go identifier and therefore cannot be the type the scanner resolves", key)
		}
	}
}

// TestEveryVendorHasExactlyOneBindingFile is AC5 of TASK-260822-3cknas held to
// the code.
//
// It checks three things a reader would otherwise have to check by eye: every
// vendor named here has a home, every home is a file the module scan reaches,
// and no two vendors share one. The last is the one that matters — two vendors
// pointing at one file would give each of them permission to spell the other's
// bindings, which is the flat allowlist this guard deliberately is not.
func TestEveryVendorHasExactlyOneBindingFile(t *testing.T) {
	sources := moduleSources(t)
	seen := map[string]string{}
	for vendor, home := range vendorBindingHomes {
		if _, ok := sources[home]; !ok {
			t.Errorf("vendor %q declares its binding file as %q, which the module scan never reached", vendor, home)
		}
		if other, taken := seen[home]; taken {
			t.Errorf("vendors %q and %q share the binding file %q; one file per vendor is the rule, and a shared one lets each spell the other's bindings", other, vendor, home)
		}
		seen[home] = vendor
		if bindingHomes[vendor+" models"] != home {
			t.Errorf("vendor %q's binding file %q is not the home bindingHomes records for it (%q); the guard would not permit that file to spell plugin ids", vendor, home, bindingHomes[vendor+" models"])
		}
	}
}

// isGoIdentifier reports whether s could be a Go identifier, which is exactly
// the question "could the scanner resolve this key as a type name".
func isGoIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}
