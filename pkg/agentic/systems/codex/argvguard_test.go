package codex

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the mechanical half of "ONE argv construction site".
//
// It is ported from the source's codex_argv_guard_test.go
// (skill-project-management, tools/board-cli/internal/spawn), including the
// history that shaped it. The source's guard began with a threshold of two
// co-occurring signature literals, on the theory that any single one could
// appear incidentally. A review demonstrated the theory wrong in practice: a
// fourth site copy-pasted from either historical split spells only ONE of them
// — "danger-full-access" without also restating "--ask-for-approval" — and
// walked straight through. The threshold is one, and every legitimate
// non-construction touch is named in the allowlist with a reason instead.
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
// Three residual classes are DECLARED OPEN and demonstrated staying open in
// TestCodexArgvGuardResidualGaps rather than only described here:
//
//   - A call-wrapped literal behind a package-level NAME:
//     `var flags = []string{string([]byte("--skip-git-repo-check"))}`, then a
//     function spreading flags. Neither foldConstStrings nor
//     collectStringLiterals evaluates a call expression, so the var resolves to
//     no strings and the function that references it touches nothing. The same
//     call-wrapped literal written INSIDE a function body is still caught —
//     matchedSignatures walks every node and sees the literal — so the gap is
//     the indirection, not the wrapping.
//   - Cross-package references: a const or var declared outside the scanned
//     source set and referenced through a qualified selector. Only the files
//     handed to the scanner are resolved.
//   - Reassignment inside an ALLOWLISTED function: a package-level var declared
//     empty and appended to from inside Args itself. The scanner reads
//     non-allowlisted bodies and package-level initializers; statements inside
//     an allowlisted body are the allowlist's own blind spot, which is a
//     property of having an allowlist at all.
//
// # Scan scope
//
// Every non-test Go file this module's build compiles, using the same rules
// pkg/agentic's single-source guard settled on after a red trunk: skip
// dot- and underscore-directories (which is what excludes a machine-local
// agents-infra install that dispatches on "codex" and has nothing to do with
// this module), skip nested modules, skip vendor, node_modules and testdata.
//
// Scanning the whole module rather than one package is the port's own widening,
// and it is the point: in the source the three drifted sites lived in two
// different packages, so a guard that watched only its own directory would have
// seen one of them.

// codexArgvSignature is the set of literal strings that identify a function as
// spelling codex CLI argv when even one of them appears in its body — directly,
// or through a package-level const or var it references.
//
// Every entry is codex-CLI-specific prose rather than a word likely to appear
// by accident, which is what makes a threshold of one affordable.
var codexArgvSignature = []string{
	"--dangerously-bypass-approvals-and-sandbox",
	"--skip-git-repo-check",
	"model_reasoning_effort",
	"service_tier",
	"--ask-for-approval",
	"danger-full-access",
}

// codexArgvConstructionAllowlist names the only functions permitted to spell a
// signature literal, each with the reason it is not a second construction.
//
// Args is THE construction site. appendReasoningAndTier is the fragment Args
// splices into both of its grammars: it carries no mode-specific flag, only the
// effort/service-tier pair both modes share, and inlining it into both branches
// is what would create a second spelling one branch at a time.
//
// A name must not be added here without a reason written down. The source's
// allowlist carries six such names — display mirrors, config readers, a flag
// classifier — each audited and each explained where it is listed.
var codexArgvConstructionAllowlist = map[string]string{
	"Args":                   "the single construction site",
	"appendReasoningAndTier": "the -c override fragment both of Args' grammars splice in identically",
	"NormalizeServiceTier":   "maps the runtime's tier vocabulary onto the catalog id; it names the config KEY nowhere and constructs no argv",
	"ServiceTierEnv":         "the environment variable a child reads its resolved tier from; a variable name, not an argv flag",
}

type argvViolation struct {
	File    string
	Func    string
	Line    int
	Matched []string
}

func (v argvViolation) String() string {
	return fmt.Sprintf("%s:%d func %s spells codex flags %s", v.File, v.Line, v.Func, strings.Join(v.Matched, ", "))
}

// scanArgvConstructionSites reports every function outside the allowlist whose
// body spells a signature literal — inline, through a resolvable package-level
// const or var, or inside a function literal reachable from a package-level
// var's initializer.
//
// sources maps a display filename to Go source text, so the same scanner runs
// over the real module and over synthetic mutant corpora.
func scanArgvConstructionSites(sources map[string]string, allowlist map[string]string) ([]argvViolation, error) {
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
	consts := foldConstStrings(all)
	vars := collectVarStrings(all, consts)

	var violations []argvViolation
	for _, name := range names {
		for _, decl := range parsed[name].Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				if _, allowed := allowlist[d.Name.Name]; allowed {
					continue
				}
				if matched := matchedSignatures(d.Body, consts, vars); len(matched) >= 1 {
					violations = append(violations, newArgvViolation(name, d.Name.Name, fset.Position(d.Pos()).Line, matched))
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
						if _, allowed := allowlist[ident.Name]; allowed {
							continue
						}
						for _, lit := range collectFuncLits(vspec.Values[i]) {
							if matched := matchedSignatures(lit.Body, consts, vars); len(matched) >= 1 {
								violations = append(violations, newArgvViolation(name, ident.Name, fset.Position(lit.Pos()).Line, matched))
							}
						}
						if matched := matchedValues(collectStringLiterals(vspec.Values[i], consts)); len(matched) >= 1 {
							violations = append(violations, newArgvViolation(name, ident.Name, fset.Position(vspec.Pos()).Line, matched))
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
		if violations[i].Func != violations[j].Func {
			return violations[i].Func < violations[j].Func
		}
		return violations[i].Line < violations[j].Line
	})
	return violations, nil
}

func newArgvViolation(file, fn string, line int, matched map[string]bool) argvViolation {
	names := make([]string, 0, len(matched))
	for s := range matched {
		names = append(names, s)
	}
	sort.Strings(names)
	return argvViolation{File: file, Func: fn, Line: line, Matched: names}
}

// matchedSignatures walks a body and returns the signature values it spells,
// directly or through a name the const/var resolution already folded.
func matchedSignatures(body ast.Node, consts map[string]string, vars map[string][]string) map[string]bool {
	matched := map[string]bool{}
	record := func(value string) {
		for _, signature := range codexArgvSignature {
			if strings.Contains(value, signature) {
				matched[signature] = true
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

func matchedValues(values []string) map[string]bool {
	matched := map[string]bool{}
	for _, value := range values {
		for _, signature := range codexArgvSignature {
			if strings.Contains(value, signature) {
				matched[signature] = true
			}
		}
	}
	return matched
}

// foldConstStrings resolves every package-level const that folds to a
// compile-time string, as a bounded fixpoint so declaration order and grouping
// do not matter.
func foldConstStrings(files []*ast.File) map[string]string {
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
					if values := collectStringLiterals(vspec.Values[i], consts); len(values) > 0 {
						vars[name.Name] = append(vars[name.Name], values...)
					}
				}
			}
		}
	}
	return vars
}

// collectStringLiterals walks an initializer's structure — composite literal
// elements and keys, concatenation operands — collecting strings. It does not
// evaluate calls or index expressions; that is the call-wrapped residual.
func collectStringLiterals(expr ast.Expr, consts map[string]string) []string {
	if value, ok := foldStringExpr(expr, consts); ok {
		return []string{value}
	}
	var out []string
	switch v := expr.(type) {
	case *ast.ParenExpr:
		out = append(out, collectStringLiterals(v.X, consts)...)
	case *ast.BinaryExpr:
		out = append(out, collectStringLiterals(v.X, consts)...)
		out = append(out, collectStringLiterals(v.Y, consts)...)
	case *ast.CompositeLit:
		for _, elt := range v.Elts {
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

// moduleGoSources reads every non-test Go file this module's build compiles.
func moduleGoSources(t *testing.T) map[string]string {
	t.Helper()
	root := moduleRoot(t)
	sources := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			skip, err := skipScanDir(root, path, entry.Name())
			if err != nil {
				return err
			}
			if skip {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sources[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	return sources
}

var scanSkipDirs = map[string]bool{"vendor": true, "node_modules": true, "testdata": true}

// skipScanDir mirrors go/build's own rules rather than naming machine-local
// directories. A directory list is a list of the trees somebody had thought of;
// the toolchain's rule is what keeps the scan set and the build set identical.
func skipScanDir(root, path, name string) (bool, error) {
	if path == root {
		return false, nil
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true, nil
	}
	if scanSkipDirs[name] {
		return true, nil
	}
	switch _, err := os.Stat(filepath.Join(path, "go.mod")); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("deciding whether %s is a nested module: %w", path, err)
	}
}

// moduleRoot walks up from this package to the directory holding go.mod, so the
// scan reaches the whole module rather than this one directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// TestCodexArgvHasExactlyOneConstructionSite is the gate.
func TestCodexArgvHasExactlyOneConstructionSite(t *testing.T) {
	violations, err := scanArgvConstructionSites(moduleGoSources(t), codexArgvConstructionAllowlist)
	if err != nil {
		t.Fatalf("scanArgvConstructionSites: %v", err)
	}
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("found %d codex argv construction site(s) outside Args:\n%s", len(violations), strings.Join(lines, "\n"))
}

// TestTheArgvGuardScansTheWholeModule proves the scan reaches what it claims
// to. A guard that silently scans zero files reports clean forever.
func TestTheArgvGuardScansTheWholeModule(t *testing.T) {
	sources := moduleGoSources(t)
	for _, required := range []string{
		"pkg/agentic/systems/codex/args.go",
		"pkg/agentic/systems/codex/codex.go",
		"pkg/agentic/registry.go",
		"tools/agents-management/cmd/plugins.go",
	} {
		if _, ok := sources[required]; !ok {
			t.Errorf("the scan did not reach %s; a file the guard never reads is a file it never guards", required)
		}
	}
}

// TestTheArgvGuardFiresOnTheRealConstructionSite narrows the gate instead of
// deleting it: the same real sources are rescanned with Args taken OUT of the
// allowlist, and the real Args must then be reported.
//
// Without this, a green result above is equally consistent with "the rule never
// matches anything", and the guard would be worth nothing while looking healthy.
func TestTheArgvGuardFiresOnTheRealConstructionSite(t *testing.T) {
	narrowed := map[string]string{}
	for name, reason := range codexArgvConstructionAllowlist {
		if name == "Args" {
			continue
		}
		narrowed[name] = reason
	}
	violations, err := scanArgvConstructionSites(moduleGoSources(t), narrowed)
	if err != nil {
		t.Fatalf("scanArgvConstructionSites: %v", err)
	}
	found := false
	for _, v := range violations {
		if v.Func == "Args" && v.File == "pkg/agentic/systems/codex/args.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("with Args removed from the allowlist the real construction site was not reported; the rule does not fire on this module's own code, so its silence elsewhere proves nothing. violations=%v", violations)
	}
}

// TestTheArgvGuardCatchesASecondConstructionSite is the mutant set: every
// ordinary Go spelling a fourth site could take, each planted in a synthetic
// corpus and each required to be reported.
//
// The single-literal cases are the ones that matter most. The source's guard
// used a threshold of two and a review showed a copy-pasted site spelling only
// one signature walked through it.
func TestTheArgvGuardCatchesASecondConstructionSite(t *testing.T) {
	mutants := []struct {
		name    string
		source  string
		wantFn  string
		because string
	}{
		{
			name:    "a full second construction",
			wantFn:  "buildCodexArgsAgain",
			because: "the shape the source unwound three times",
			source: `package other
func buildCodexArgsAgain(model string) []string {
	return []string{"exec", "-m", model, "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"}
}`,
		},
		{
			name:    "a single signature literal",
			wantFn:  "mirrorSandboxPolicy",
			because: "a copy-paste from the managed-session split spells only danger-full-access, which a threshold of two admits",
			source: `package other
func mirrorSandboxPolicy() []string { return []string{"--sandbox", "danger-full-access"} }`,
		},
		{
			name:    "the literal behind a package-level const",
			wantFn:  "effortOverride",
			because: "a const is the first place a second site moves a literal to avoid spelling it twice",
			source: `package other
const reasoningKey = "model_reasoning_effort"
func effortOverride(effort string) []string { return []string{"-c", reasoningKey + "=" + effort} }`,
		},
		{
			name:    "the literal behind a function-local const",
			wantFn:  "localConstSite",
			because: "the same move one scope down",
			source: `package other
func localConstSite(effort string) []string {
	const key = "model_reasoning_effort"
	return []string{"-c", key + "=" + effort}
}`,
		},
		{
			name:    "the literals inside a package-level var table",
			wantFn:  "codexFlagTable",
			because: "a table of flags is a construction waiting for a caller",
			source: `package other
var codexFlagTable = []string{"--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"}`,
		},
		{
			name:    "a function referencing that var",
			wantFn:  "spliceFlags",
			because: "the caller of such a table spells the flags as surely as the table does",
			source: `package other
var codexFlags = []string{"--skip-git-repo-check"}
func spliceFlags(args []string) []string { return append(args, codexFlags...) }`,
		},
		{
			name:    "a closure inside a package-level table",
			wantFn:  "adapterTable",
			because: "this is exactly how the source's own adapter table was built, so a construction hidden in one has a live precedent",
			source: `package other
var adapterTable = map[string]func() []string{
	"x": func() []string { return []string{"--ask-for-approval", "never"} },
}`,
		},
		{
			name:    "a literal inside init()",
			wantFn:  "init",
			because: "init is an ordinary body and a site that runs at startup is still a site",
			source: `package other
var flags []string
func init() { flags = append(flags, "--dangerously-bypass-approvals-and-sandbox") }`,
		},
		{
			name:    "a literal inside a method",
			wantFn:  "Argv",
			because: "a second System implementation spelling its own codex flags is the port-shaped version of this defect",
			source: `package other
type other struct{}
func (other) Argv() []string { return []string{"--skip-git-repo-check"} }`,
		},
		{
			name:    "a call-wrapped literal inside a function body",
			wantFn:  "wrapped",
			because: "the wrapping alone hides nothing: the body walk sees the literal. Only the indirection through a package-level name is a residual, and this case is what draws that line",
			source: `package other
func wrapped() []string { return []string{string([]byte("--skip-git-repo-check"))} }`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			violations, err := scanArgvConstructionSites(
				map[string]string{"other/mutant.go": mutant.source},
				codexArgvConstructionAllowlist,
			)
			if err != nil {
				t.Fatalf("scanArgvConstructionSites: %v", err)
			}
			found := false
			for _, v := range violations {
				if v.Func == mutant.wantFn {
					found = true
				}
			}
			if !found {
				t.Errorf("the guard admitted a second construction site (%s): %s\nviolations=%v", mutant.name, mutant.because, violations)
			}
		})
	}
}

// TestTheArgvGuardAcceptsOrdinaryCode is the false-positive bound. A guard that
// fires on unrelated code is a guard somebody deletes, and then nothing is
// watching the thing it was written for.
func TestTheArgvGuardAcceptsOrdinaryCode(t *testing.T) {
	violations, err := scanArgvConstructionSites(map[string]string{
		"other/ordinary.go": `package other

import "strings"

// A comment mentioning model_reasoning_effort and danger-full-access is prose,
// not code, and must not be reported.
func unrelated(s string) string { return strings.TrimSpace(s) }

func otherFlags() []string { return []string{"--verbose", "--json", "--model", "gpt"} }

func serviceTierFieldName() string { return "ServiceTier" }
`,
	}, codexArgvConstructionAllowlist)
	if err != nil {
		t.Fatalf("scanArgvConstructionSites: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("the guard reported ordinary code: %v", violations)
	}
}

// TestCodexArgvGuardResidualGaps demonstrates each declared-open class STAYING
// open, rather than asserting it in prose.
//
// A class named in the threat model but not demonstrated here would be this
// same defect one level up: a comment a reader trusts with nothing holding it
// to the code. Read the two against each other.
func TestCodexArgvGuardResidualGaps(t *testing.T) {
	gaps := []struct {
		name   string
		source string
	}{
		{
			name: "a call-wrapped literal behind a package-level name",
			source: `package other
var hiddenFlags = []string{string([]byte("--skip-git-repo-check"))}
func hidden() []string { return append([]string{}, hiddenFlags...) }`,
		},
		{
			name: "cross-package references",
			source: `package other
import "example.com/elsewhere"
func hidden() []string { return append([]string{}, elsewhere.CodexFlags...) }`,
		},
		{
			name: "reassignment inside an allowlisted function",
			source: `package other
var stash []string
func Args() []string {
	stash = append(stash, "--skip-git-repo-check")
	return stash
}
func user() []string { return stash }`,
		},
	}
	for _, gap := range gaps {
		t.Run(gap.name, func(t *testing.T) {
			violations, err := scanArgvConstructionSites(
				map[string]string{"other/gap.go": gap.source},
				codexArgvConstructionAllowlist,
			)
			if err != nil {
				t.Fatalf("scanArgvConstructionSites: %v", err)
			}
			if len(violations) != 0 {
				t.Errorf("%q is documented as a residual gap but the guard now reports it (%v); close the gap in the threat model too, or the comment and the code disagree", gap.name, violations)
			}
		})
	}
}

// TestTheArgvAllowlistIsJustified holds the allowlist's shape: every name in it
// carries a reason, and every name in it exists in this module.
//
// An allowlist entry for a function nobody wrote is an exemption waiting for
// somebody to claim it by naming a new function that way.
func TestTheArgvAllowlistIsJustified(t *testing.T) {
	sources := moduleGoSources(t)
	declared := map[string]bool{}
	fset := token.NewFileSet()
	for name, src := range sources {
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declared[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if vspec, ok := spec.(*ast.ValueSpec); ok {
						for _, ident := range vspec.Names {
							declared[ident.Name] = true
						}
					}
				}
			}
		}
	}
	for name, reason := range codexArgvConstructionAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is allowlisted with no reason; an exemption nobody can argue with is an exemption nobody reviews", name)
		}
		if !declared[name] {
			t.Errorf("%q is allowlisted but is not declared anywhere in this module; the exemption is waiting for somebody to claim it", name)
		}
	}
}
