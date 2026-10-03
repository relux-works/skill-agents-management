// Command codex-provider-mutants derives its inventory from the semantic
// refusal scanner, checks completeness, and executes narrowing members in an
// isolated source copy. Single/undecidable domains remain explicit bounds.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/internal/refusalscan"
)

type mutation struct {
	refusalscan.Member
	Replacement string `json:"replacement,omitempty"`
}

type result struct {
	ID, Expression, Replacement, Disposition, Bound string
	Exit                                            int
	FailingTests                                    []string
}

func discoverMutations(root string) ([]mutation, error) {
	sites, err := refusalscan.LocalCodexSites(root)
	if err != nil {
		return nil, err
	}
	members, err := refusalscan.Members(root, sites)
	if err != nil {
		return nil, err
	}
	if err := refusalscan.CheckMemberCompleteness(root, sites, members); err != nil {
		return nil, err
	}
	rows := make([]mutation, 0, len(members))
	for _, member := range members {
		row := mutation{Member: member}
		// Removing one disjunct narrows the refusal class while keeping both
		// the remaining gate and the searched-for token in the source.
		if member.Kind == "operand" && member.ParentOp == "||" {
			row.Replacement = "(false && (" + member.Expression + "))"
			row.Bound = ""
		}
		if member.Kind == "operand" && strings.Contains(member.Expression, "sha256.Sum256(data)") {
			row.Replacement = "(" + member.Expression + ") && len(data) != 0"
			row.Bound = ""
		}
		// Admit only absence on an empty catalog; populated catalogs still
		// refuse a missing selected slug. The slice is a guarding input in the AST.
		if member.Kind == "operand" && member.Site.Function == "selectCatalogEntry" && member.Expression == "matches == 0" {
			row.Replacement = "(" + member.Expression + ") && len(models) != 0"
			row.Bound = ""
		}
		if member.Kind == "allowed-set-acceptance" {
			expression, err := parser.ParseExpr(member.Expression)
			if err != nil {
				return nil, err
			}
			comparison := expression.(*ast.BinaryExpr)
			left := member.Expression[:strings.Index(member.Expression, "==")]
			right := member.Expression[strings.Index(member.Expression, "==")+2:]
			if comparison.Op == token.EQL {
				row.Replacement = "(" + member.Expression + ") || len(" + strings.TrimSpace(left) + ") == len(" + strings.TrimSpace(right) + ")"
				row.Bound = ""
			}
		}
		rows = append(rows, row)
	}
	extra, err := bindingAndReadMutations(root, sites)
	if err != nil {
		return nil, err
	}
	rows = append(rows, extra...)
	return rows, nil
}

func main() {
	run := flag.Bool("run", false, "execute narrowing members; otherwise print full AST census")
	start := flag.Int("start", 0, "zero-based inventory offset")
	limit := flag.Int("limit", 0, "bounded inventory range; zero means remaining")
	out := flag.String("out", "", "write structured result evidence")
	flag.Parse()
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	rows, err := discoverMutations(root)
	if err != nil {
		fatal(err)
	}
	if *run {
		fmt.Fprintf(os.Stderr, "SOURCE_IDENTITY %s | %s | %s/%s | GOWORK=off | TASK_BOARD_DIR unset by invocation\n", sourceIdentity(root), runtime.Version(), runtime.GOOS, runtime.GOARCH)
	}
	if !*run {
		encode(os.Stdout, rows)
		return
	}
	if *start < 0 || *start > len(rows) || *limit < 0 {
		fatal(fmt.Errorf("invalid bounded range"))
	}
	end := len(rows)
	if *limit > 0 && *start+*limit < end {
		end = *start + *limit
	}
	scratch := filepath.Join(root, ".temp", "codex-provider-mutants")
	if err := os.MkdirAll(scratch, 0700); err != nil {
		fatal(err)
	}
	copy, err := os.MkdirTemp(scratch, "candidate-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(copy)
	if err := copyTree(root, copy); err != nil {
		fatal(err)
	}
	var results []result
	// Sequential execution is the package-wide cap: one child, never more
	// than the required maximum of two. Every child has a timeout and capped
	// output. No command is detached from this run.
	for _, row := range rows[*start:end] {
		r := result{ID: row.ID, Expression: row.Expression, Replacement: row.Replacement, Disposition: "bound", Bound: row.Bound, Exit: -1}
		if row.Replacement != "" {
			path := filepath.Join(copy, filepath.FromSlash(row.File))
			original, err := os.ReadFile(path)
			if err != nil {
				fatal(err)
			}
			mutated := string(original[:row.Start]) + row.Replacement + string(original[row.End:])
			if err := os.WriteFile(path, []byte(mutated), 0600); err != nil {
				fatal(err)
			}
			r = execute(copy, r)
			if err := os.WriteFile(path, original, 0600); err != nil {
				fatal(err)
			}
		}
		results = append(results, r)
		fmt.Printf("%s | exit=%d | %s | %s | %v\n", r.ID, r.Exit, r.Disposition, r.Bound, r.FailingTests)
	}
	if *out != "" {
		file, err := os.Create(*out)
		if err != nil {
			fatal(err)
		}
		encode(file, results)
		if err := file.Close(); err != nil {
			fatal(err)
		}
	}
}

const maxCapturedOutput = 256 << 10

type cappedOutput struct {
	bytes     []byte
	truncated bool
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maxCapturedOutput - len(w.bytes)
	if n > remaining {
		w.truncated = true
	}
	if remaining > n {
		remaining = n
	}
	w.bytes = append(w.bytes, p[:remaining]...)
	return n, nil
}

func execute(root string, r result) result {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pattern := behaviorPattern
	if strings.HasPrefix(r.ID, "coverage-guard/") {
		pattern += "|^TestLocalRefusalGuardFiresOnAnUnmappedSite$"
	}
	cmd := exec.CommandContext(ctx, "go", "test", "-mod=mod", "./pkg/agentic/systems/codex", "-count=1", "-run", pattern)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output := &cappedOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	logName := fmt.Sprintf("%x.log", sha256.Sum256([]byte(r.ID)))
	os.WriteFile(filepath.Join(filepath.Dir(root), logName), output.bytes, 0600)
	r.Exit = 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			r.Exit = e.ExitCode()
		} else {
			r.Exit = -1
		}
	}
	failures := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^\s*--- FAIL: (Test\S+)`).FindAllStringSubmatch(string(output.bytes), -1) {
		if !failures[match[1]] {
			r.FailingTests = append(r.FailingTests, match[1])
			failures[match[1]] = true
		}
	}
	r.Disposition = "survivor"
	r.Bound = "No named behavioral failure; no kill claimed."
	if output.truncated || ctx.Err() != nil || strings.Contains(string(output.bytes), "[build failed]") {
		r.Disposition = "unrun"
		r.Bound = "Build failure, timeout or truncated evidence; not behavioral kill."
	} else if r.Exit != 0 && len(r.FailingTests) > 0 {
		r.Disposition = "killed"
		r.Bound = ""
	}
	return r
}

const behaviorPattern = `^(TestLocal(Launch|Effort|ProviderEmptyModel|Catalog)|TestCatalog|TestIDPlan|TestHosted|TestSnapshot|TestReadProviderSnapshot|TestManagedSessionRefuses|TestBuildPlan.*LocalProvider|TestPluginArgvRefusesAnEmptyLocalProvider|TestProviderArgv|TestTomlDecodeShape)`

func copyTree(root, destination string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".git" || relative == ".temp" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(destination, relative), 0700)
		}
		if relative == ".git" || entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, relative), data, 0600)
	})
}
func encode(file *os.File, value any) {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

// These integrity and read-policy decisions are not typed return sites:
// they flow into the typed catalog gate. Derive them from production AST,
// record them separately, and execute the same behavioral suite.
func bindingAndReadMutations(root string, sites []refusalscan.Site) ([]mutation, error) {
	var result []mutation
	paths, err := refusalscan.SourceFiles(root)
	if err != nil {
		return nil, err
	}
	paths = append(paths, "pkg/agentic/systems/codex/localrefusal_guard_test.go")
	entries, err := os.ReadDir(filepath.Join(root, "internal/catalogfile"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			paths = append(paths, "internal/catalogfile/"+entry.Name())
		}
	}
	for _, file := range paths {
		if !strings.HasPrefix(file, "pkg/agentic/systems/codex/") && !strings.HasPrefix(file, "internal/catalogfile/") && file != "pkg/agentic/exec.go" {
			continue
		}

		path := filepath.Join(root, file)
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, path, body, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if branch, ok := node.(*ast.IfStmt); ok && file == "pkg/agentic/systems/codex/exact_catalog.go" {
				text := string(body[fset.Position(branch.Cond.Pos()).Offset:fset.Position(branch.Cond.End()).Offset])
				if text == "!exactCatalogStringsValid(data)" {
					m := refusalscan.Member{ID: "parse/drop-string-lexical-check", File: file, Start: fset.Position(branch.Cond.Pos()).Offset, End: fset.Position(branch.Cond.End()).Offset, Expression: text, Kind: "parse-policy"}
					// Required removal control; the narrowing member below is
					// the class evidence, not this delete-only control.
					result = append(result, mutation{Member: m, Replacement: "(false && (" + text + "))"})
				}
			}
			if branch, ok := node.(*ast.IfStmt); ok && file == "pkg/agentic/systems/codex/catalog_strings.go" {
				binary, ok := branch.Cond.(*ast.BinaryExpr)
				if !ok || binary.Op != token.LAND {
					return true
				}
				text := string(body[fset.Position(binary.Pos()).Offset:fset.Position(binary.End()).Offset])
				if text == "word >= 0xd800 && word <= 0xdbff" {
					missingPair, ok := branch.Body.List[0].(*ast.IfStmt)
					if !ok {
						return true
					}
					text = string(body[fset.Position(missingPair.Pos()).Offset:fset.Position(missingPair.End()).Offset])
					m := refusalscan.Member{ID: "parse/admit-high-surrogate-lower-member", File: file, Start: fset.Position(missingPair.Pos()).Offset, End: fset.Position(missingPair.End()).Offset, Expression: text, Kind: "parse-policy"}
					// Keep every other surrogate refusal, but admit U+D800
					// immediately followed by the closing quote. Valid pairs
					// still follow the original branch. The AST supplies the
					// actual compared endpoint and missing-pair branch.
					lower := binary.X.(*ast.BinaryExpr).Y
					endpoint := string(body[fset.Position(lower.Pos()).Offset:fset.Position(lower.End()).Offset])
					result = append(result, mutation{Member: m, Replacement: "if word == " + endpoint + " && i+1 < len(data) && data[i+1] == '\"' { return i+1, true }; " + text})
				}
			}
			// Restoring Go struct-decoding semantics (case-insensitive field
			// matching) inside the exact-key comparator re-admits the Q2
			// class: aliases count as recognized occurrences. The named
			// alias negatives must fail.
			if fn, ok := node.(*ast.FuncDecl); ok && file == "pkg/agentic/systems/codex/exact_catalog.go" && fn.Name.Name == "exactKeyEqual" {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					binary, ok := n.(*ast.BinaryExpr)
					if ok && binary.Op == token.EQL {
						text := string(body[fset.Position(binary.Pos()).Offset:fset.Position(binary.End()).Offset])
						if text == "key == name" {
							m := refusalscan.Member{ID: "parse/exact-key-case-folds-like-struct", File: file, Start: fset.Position(binary.Pos()).Offset, End: fset.Position(binary.End()).Offset, Expression: text, Kind: "parse-policy"}
							result = append(result, mutation{Member: m, Replacement: "((" + text + ") || strings.EqualFold(key, name))"})
						}
					}
					return true
				})
			}
			if branch, ok := node.(*ast.IfStmt); ok && file == "pkg/agentic/exec.go" {
				text := string(body[fset.Position(branch.Cond.Pos()).Offset:fset.Position(branch.Cond.End()).Offset])
				if text == "p.execVerifier != nil" {
					member := refusalscan.Member{ID: "exec-hook/admit-dry-run-with-invalid-artifact", File: file, Start: fset.Position(branch.Cond.Pos()).Offset, End: fset.Position(branch.Cond.End()).Offset, Expression: text, Kind: "exec-hook"}
					result = append(result, mutation{Member: member, Replacement: "(" + text + ") && p.Mode != LaunchModeDryRun"})
				}
			}
			call, ok := node.(*ast.CallExpr)
			if ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "materializeCatalog" && len(call.Args) > 0 {
					expr := call.Args[0]
					text := string(body[fset.Position(expr.Pos()).Offset:fset.Position(expr.End()).Offset])
					if strings.Contains(text, "snapshot.catalogData") {
						member := refusalscan.Member{ID: "binding/snapshot-byte-source", File: file, Start: fset.Position(expr.Pos()).Offset, End: fset.Position(expr.End()).Offset, Expression: text, Kind: "byte-binding"}
						result = append(result, mutation{Member: member, Replacement: `func() []byte { retained := ` + text + `; if data, err := os.ReadFile(snapshot.catalogPath); err == nil { return data }; return retained }()`})
					}
				}
			}
			binary, ok := node.(*ast.BinaryExpr)
			if ok && binary.Op == token.LOR {
				text := string(body[fset.Position(binary.X.Pos()).Offset:fset.Position(binary.X.End()).Offset])
				if strings.Contains(text, "info.Mode().IsRegular()") && strings.Contains(string(body[fset.Position(binary.Y.Pos()).Offset:fset.Position(binary.Y.End()).Offset]), "maxCatalogBytes") {
					m := refusalscan.Member{ID: "read-policy/nonregular-class", File: file, Start: fset.Position(binary.X.Pos()).Offset, End: fset.Position(binary.X.End()).Offset, Expression: text, Kind: "read-policy"}
					result = append(result, mutation{Member: m, Replacement: "(false && (" + text + "))"})
				}
			}
			if branch, ok := node.(*ast.IfStmt); ok && file == "pkg/agentic/systems/codex/localrefusal_guard_test.go" {
				text := string(body[fset.Position(branch.Cond.Pos()).Offset:fset.Position(branch.Cond.End()).Offset])
				if strings.HasPrefix(text, "!keys[") && len(sites) > 0 {
					m := refusalscan.Member{ID: "coverage-guard/admit-one-unmapped-site", File: file, Start: fset.Position(branch.Cond.Pos()).Offset, End: fset.Position(branch.Cond.End()).Offset, Expression: text, Kind: "coverage-guard"}
					result = append(result, mutation{Member: m, Replacement: "(" + text + ") && s.function != " + strconv.Quote(sites[0].Function)})
				}
			}
			return true
		})
	}
	return result, nil
}

func sourceIdentity(root string) string {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".temp" || relative == ".git" || relative == ".task-board" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !(strings.HasSuffix(relative, ".go") || strings.HasSuffix(relative, ".json") || strings.HasSuffix(relative, ".toml") || strings.HasSuffix(relative, ".sh") || relative == "go.mod" || relative == "go.sum" || relative == "Makefile") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(relative), len(data))
		hash.Write(data)
		return nil
	})
	if err != nil {
		fatal(err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}
