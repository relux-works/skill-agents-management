package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// modelCheckGuardPackages is the guard's file set: ALL non-test .go files of
// the changed packages, walked from the module root. Package directories are
// constants, stable after landing; the file set is discovered by walking,
// never derived from git diff or HEAD (a diff-derived set is empty once
// committed and the guard would fail on main after landing).
var modelCheckGuardPackages = []string{
	"tools/agents-management/cmd",
	"pkg/vendorplugin",
	"pkg/engineobservation",
	"pkg/localruntime",
}

// modelCheckGuardSite maps one enumerated refusal site to its negative
// coverage and narrowing mutants.
//
// A site is one branch that sets report.Status to a non-passed value, or one
// classifyModelCheckLaunchError arm. The enumeration is mechanical (AST walk
// over the file set); this table supplies the test mapping and the witness
// predicates singleton mutants narrow on. Both directions are checked: an
// enumerated site without a table entry fails, and a table entry matching no
// site fails.
//
// Mutant convention: compounds generate one drop-operand mutant per operand,
// killed by tests[i] in operand order. Singletons generate one mutant per
// witness (`(<cond>) && (<witness>)`), killed by tests[i] in witness order.
// Tests beyond the mutant count are additional coverage pins.
type modelCheckGuardSite struct {
	id string
	// reason is the refusal reason literal, "deadlineReason" for the
	// deadline/cancelled branches, or "classify-default".
	reason string
	// kind is "if", "case", "arm" or "default".
	kind string
	// function disambiguates the deadlineReason sites.
	function string
	// condExact disambiguates same-function deadlineReason branches.
	condExact string
	// operands is the expected top-level ||/&& operand count (1 singletons).
	operands int
	// witnesses holds one narrowing conjunct per singleton mutant.
	witnesses []string
	// tests are Parent or Parent/subtest names; tests[i] kills mutant i.
	tests []string
	// equivalent documents a surviving mutant with its downstream guard.
	equivalent string
	// prependCase is the narrowing case inserted before the default arm.
	prependCase string
}

var modelCheckGuardTable = []modelCheckGuardSite{
	{
		id: "invalid-arguments", reason: "invalid_arguments", kind: "if", operands: 6,
		tests: []string{
			"TestModelCheckInvalidArgumentsRefuse/zero_deadline",
			"TestModelCheckInvalidArgumentsRefuse/empty_runtime",
			"TestModelCheckInvalidArgumentsRefuse/empty_model",
			"TestModelCheckInvalidArgumentsRefuse/empty_prompt",
			"TestModelCheckInvalidArgumentsRefuse/empty_expect",
			"TestModelCheckInvalidArgumentsRefuse/empty_evidence",
		},
	},
	{
		id: "evidence-path", reason: "evidence_path_unavailable", kind: "if", operands: 1,
		witnesses: []string{"!errors.Is(err, os.ErrExist)"},
		tests:     []string{"TestModelCheckExistingEvidenceRefusesWithoutStartingPi"},
	},
	{
		id: "evidence-permissions", reason: "evidence_permissions_unavailable", kind: "if", operands: 1,
		witnesses: []string{"!errors.Is(err, os.ErrClosed)"},
		tests:     []string{"TestModelCheckEvidencePermissionsUnavailableRefuses"},
	},
	{
		id: "evidence-write", reason: "evidence_write_failed", kind: "if", operands: 1,
		witnesses: []string{"report.PiStarted"},
		tests: []string{
			"TestModelCheckEvidenceWriteFailedRefuses/pi_not_started",
			"TestModelCheckEvidenceWriteFailedRefuses/pi_started",
		},
	},
	{
		id: "deadline-pre-config", reason: "deadlineReason", kind: "if", function: "runModelCheck", condExact: "err != nil", operands: 1,
		witnesses: []string{"!errors.Is(err, context.Canceled)"},
		tests:     []string{"TestModelCheckCancelledParentRefusesBeforeConfig"},
		equivalent: "classify contextErr arm: admission under a cancelled parent returns " +
			"Canceled from BuildLaunch and reports caller_cancelled identically",
	},
	{
		id: "local-models-absent", reason: "local_models_absent", kind: "case", operands: 1,
		witnesses: []string{"len(loaded.Config.Runtimes) == 0"},
		tests: []string{
			"TestModelCheckLocalModelsAbsentRefuses/absent_with_config_bytes",
			"TestModelCheckLocalModelsAbsentRefuses/absent",
		},
	},
	{
		id: "local-models-unreadable", reason: "local_models_unreadable", kind: "case", operands: 1,
		witnesses: []string{"loaded.Config.Runtimes == nil"},
		tests: []string{
			"TestModelCheckLocalModelsUnreadableRefuses/read_error_with_partial_runtimes",
			"TestModelCheckLocalModelsUnreadableRefuses/read_error",
		},
	},
	{
		id: "working-directory", reason: "working_directory_unavailable", kind: "if", operands: 1,
		witnesses: []string{"!errors.Is(err, os.ErrNotExist)"},
		tests:     []string{"TestModelCheckWorkingDirectoryUnavailableRefuses"},
	},
	{
		id: "configured-model", reason: "configured_model_unavailable", kind: "if", operands: 1,
		witnesses: []string{`!strings.Contains(err.Error(), "model is not declared")`},
		tests: []string{
			"TestModelCheckConfiguredModelUnavailableRefuses/unknown_model",
			"TestModelCheckConfiguredModelUnavailableRefuses/unknown_runtime",
			"TestModelCheckConfiguredModelUnavailableRefuses/non-pi_system",
		},
	},
	{
		id: "deadline-post-process", reason: "deadlineReason", kind: "if", function: "runModelCheck", condExact: "ctx.Err() != nil", operands: 1,
		witnesses: []string{"!errors.Is(ctx.Err(), context.Canceled)"},
		tests: []string{
			"TestModelCheckCancelledDuringProcessReportsCancelled",
			"TestModelCheckCallerDeadlineIsHonoured",
		},
	},
	{
		id: "pi-process-failed", reason: "pi_process_failed", kind: "if", operands: 2,
		tests: []string{
			"TestModelCheckPiProcessErrorRefuses",
			"TestModelCheckPiNonzeroExitRefuses",
		},
	},
	{
		id: "pi-output-limit", reason: "pi_output_limit_exceeded", kind: "if", operands: 1,
		witnesses: []string{"process.ExitCode != 0"},
		tests:     []string{"TestModelCheckPiOutputLimitRefuses"},
	},
	{
		id: "expected-not-found", reason: "expected_result_not_found", kind: "if", operands: 1,
		witnesses: []string{"len(process.Output) > 0"},
		tests: []string{
			"TestModelCheckEmptyOutputRefuses",
			"TestModelCheckUnmetExpectationExitsNonzero",
		},
	},
	{
		id: "classify-context", reason: "deadlineReason", kind: "arm", function: "classifyModelCheckLaunchError", operands: 1,
		witnesses: []string{
			"!errors.Is(contextErr, context.Canceled)",
			"!errors.Is(contextErr, context.DeadlineExceeded)",
		},
		tests: []string{
			"TestModelCheckCancelledDuringStatusReadReportsCancelled",
			"TestModelCheckDeadlineDuringStatusReadReportsDeadline",
		},
	},
	{
		id: "classify-adapter-missing", reason: "engine_observation_adapter_unavailable", kind: "arm", operands: 1,
		witnesses: []string{"!errors.Is(err, inferenceengine.ErrEngineContractMissing)"},
		tests:     []string{"TestModelCheckUnknownEngineRefusesBeforePiStarts"},
	},
	{
		id: "classify-status", reason: "engine_status_unavailable", kind: "arm", operands: 3,
		tests: []string{
			"TestModelCheckReadFailedStatusRefusesBeforePiStarts",
			"TestModelCheckEngineStatusDecodeFailureRefusesBeforePiStarts/decode_failure",
			"TestModelCheckEngineStatusDecodeFailureRefusesBeforePiStarts/query_invalid",
		},
	},
	{
		id: "classify-observation", reason: "engine_observation_unavailable", kind: "arm", operands: 3,
		tests: []string{
			"TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/not_JSON",
			"TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/readiness_value_violates_its_contract",
			"TestModelCheckEngineNotObservedReadingsRefuseBeforePiStarts",
			"TestModelCheckEngineReadFailureRefusesBeforePiStarts/subprocess_error",
			"TestModelCheckEngineReadFailureRefusesBeforePiStarts/stale_typed_failure",
			"TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/wrong_readings_version",
			"TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/empty_readings_version",
			"TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/truncated_fact_set",
			"TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/reordered_facts",
			"TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/observed_value_without_value",
		},
	},
	{
		id: "classify-preflight", reason: "engine_preflight_refused", kind: "arm", operands: 1,
		witnesses: []string{"errors.Is(err, localruntime.ErrStatusReadFailed)"},
		tests:     []string{"TestModelCheckEnginePreflightRefusedBeforePiStarts"},
	},
	{
		id: "classify-default", reason: "launch_plan_refused", kind: "default", operands: 1,
		prependCase: "case errors.Is(err, pi.ErrSystemModelIdentityInvalid):\n\t\treturn modelCheckStatusPassed, \"expectation_met\"\n",
		tests: []string{
			"TestModelCheckInvalidPiIdentityRefusedBeforePiStarts",
			"TestModelCheckLaunchPlanRefusedBeforePiStarts",
		},
	},
}

// modelCheckSpecifiedMutant is a narrowing mutant outside the F3 AST
// enumeration: the F2 group-kill drop, the A1 readings-version admission,
// the G1 admission group-kill drops, the G2 duplicate admission, and the
// R3 outer-alias admission. Each names its file, an exact-once splice
// anchor, and its killing test.
type modelCheckSpecifiedMutant struct {
	id      string
	file    string // package-relative path from the module root
	find    string
	replace string
	test    string
}

var modelCheckSpecifiedMutants = []modelCheckSpecifiedMutant{
	{
		id:      "F2-group-kill",
		file:    "tools/agents-management/cmd/modelcheck_process_unix.go",
		find:    "\t_ = syscall.Kill(-pid, syscall.SIGKILL)\n",
		replace: "\t_ = pid\n",
		test:    "TestReviewReproDeadlineWithDescendantHoldingStdout",
	},
	{
		id:      "A1-empty-version",
		file:    "pkg/engineobservation/adapter.go",
		find:    "\tif version != inferenceengine.ContractVersionV3 {\n",
		replace: "\tif version != inferenceengine.ContractVersionV3 && version != \"\" {\n",
		test:    "TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts/empty_readings_version",
	},
	{
		id:      "G1-adapter-group-kill",
		file:    "pkg/engineobservation/exec_unix.go",
		find:    "\t_ = syscall.Kill(-pid, syscall.SIGKILL)\n",
		replace: "\t_ = pid\n",
		test:    "TestModelCheckAdapterDeadlineDescendantRefuses",
	},
	{
		id:      "G1-status-group-kill",
		file:    "pkg/localruntime/exec_unix.go",
		find:    "\t_ = syscall.Kill(-pid, syscall.SIGKILL)\n",
		replace: "\t_ = pid\n",
		test:    "TestModelCheckStatusReaderDeadlineDescendantRefuses",
	},
	{
		id:      "G2-last-member-wins",
		file:    "pkg/engineobservation/strict.go",
		find:    "\t\tif seen[name] {\n",
		replace: "\t\tif false {\n",
		test:    "TestModelCheckEngineStrictEnvelopeRefusesBeforePiStarts/duplicate_contract_version",
	},
	{
		id:      "R3-outer-alias",
		file:    "pkg/engineobservation/adapter.go",
		find:    "\touter, err := decodeStrictObject(output, nil, []string{\"readings\"}, outerOwned)\n",
		replace: "\touter, err := decodeStrictObject(output, nil, []string{\"readings\"}, nil)\n",
		test:    "TestModelCheckOuterReadingsAliasRefusesBeforePiStarts/READINGS_null_before",
	},
}

// --- Enumeration ---

type modelCheckSourceSpan struct {
	start int
	end   int
	text  string
}

type modelCheckEnumeratedSite struct {
	file     string // absolute path
	line     int
	function string
	reason   string
	kind     string
	cond     modelCheckSourceSpan
	operands []modelCheckSourceSpan
	operator token.Token // invalid for singletons and default
	// insertBefore is the default-clause offset for the prepend mutant.
	insertBefore int
}

func modelCheckModuleRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := gosources.Root(cwd)
	if err != nil {
		t.Fatalf("finding the module root: %v", err)
	}
	return root
}

func modelCheckGuardFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	for _, dir := range modelCheckGuardPackages {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("read guard package %s: %v", dir, err)
		}
		packageFiles := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			files = append(files, filepath.Join(root, dir, entry.Name()))
			packageFiles++
		}
		if packageFiles == 0 {
			t.Fatalf("guard package %s contributes no non-test Go files; the package list is stale", dir)
		}
	}
	sort.Strings(files)
	return files
}

func enumerateModelCheckRefusalSites(t *testing.T, root string) ([]modelCheckEnumeratedSite, map[string][]byte) {
	t.Helper()
	files := modelCheckGuardFiles(t, root)
	sources := map[string][]byte{}
	var sites []modelCheckEnumeratedSite
	classifyCalls := 0
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sources[path] = source
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		span := func(node ast.Node) modelCheckSourceSpan {
			start := fset.Position(node.Pos()).Offset
			end := fset.Position(node.End()).Offset
			return modelCheckSourceSpan{start: start, end: end, text: string(source[start:end])}
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			// If-statement refusal branches: a block that names a refusal
			// reason without passing. Status may be assigned failed/unknown
			// or inherited unknown; only the passed branch is excluded.
			ast.Inspect(function.Body, func(node ast.Node) bool {
				statement, ok := node.(*ast.IfStmt)
				if !ok {
					return true
				}
				if modelCheckBlockCallsClassify(statement.Body) {
					return true
				}
				reason, found := modelCheckBlockReason(statement.Body)
				if !found {
					return true
				}
				if modelCheckBlockStatus(statement.Body) == "modelCheckStatusPassed" {
					return true
				}
				site := modelCheckEnumeratedSite{
					file: path, line: fset.Position(statement.Pos()).Line,
					function: function.Name.Name, reason: reason, kind: "if",
					cond: span(statement.Cond),
				}
				site.operands, site.operator = modelCheckTopOperands(statement.Cond, span)
				sites = append(sites, site)
				return true
			})
			// Switch-case branches assigning a refused status, including the
			// classifyModelCheckLaunchError arms (kind "arm"/"default").
			ast.Inspect(function.Body, func(node ast.Node) bool {
				clause, ok := node.(*ast.CaseClause)
				if !ok {
					return true
				}
				kind := "case"
				if function.Name.Name == "classifyModelCheckLaunchError" {
					kind = "arm"
				}
				if len(clause.List) == 0 {
					if function.Name.Name != "classifyModelCheckLaunchError" {
						return true
					}
					kind = "default"
					reason, status := modelCheckReturnStatusReason(clause.Body)
					if status != "modelCheckStatusFailed" && status != "modelCheckStatusUnknown" {
						t.Fatalf("classify default arm returns status %q, want a refused status", status)
					}
					sites = append(sites, modelCheckEnumeratedSite{
						file: path, line: fset.Position(clause.Pos()).Line,
						function: function.Name.Name, reason: reason, kind: kind,
						insertBefore: fset.Position(clause.Pos()).Offset,
					})
					return true
				}
				reason, found := modelCheckClauseReason(clause.Body)
				if !found {
					return true
				}
				// Case branches name a refusal reason; runModelCheck cases
				// may inherit unknown instead of assigning it.
				if function.Name.Name != "classifyModelCheckLaunchError" {
					if modelCheckBlockStatus(&ast.BlockStmt{List: clause.Body}) == "modelCheckStatusPassed" {
						return true
					}
				} else if status := modelCheckClauseStatus(clause.Body, true); status != "modelCheckStatusFailed" && status != "modelCheckStatusUnknown" {
					return true
				}
				if len(clause.List) == 1 {
					sites = append(sites, modelCheckEnumeratedSite{
						file: path, line: fset.Position(clause.Pos()).Line,
						function: function.Name.Name, reason: reason, kind: kind,
						cond: span(clause.List[0]),
					})
					return true
				}
				operands := make([]modelCheckSourceSpan, 0, len(clause.List))
				for _, expr := range clause.List {
					operands = append(operands, span(expr))
				}
				sites = append(sites, modelCheckEnumeratedSite{
					file: path, line: fset.Position(clause.Pos()).Line,
					function: function.Name.Name, reason: reason, kind: kind,
					cond: modelCheckSourceSpan{
						start: operands[0].start, end: operands[len(operands)-1].end,
						text: strings.Join(modelCheckSpanTexts(operands), ", "),
					},
					operands: operands, operator: token.COMMA,
				})
				return true
			})
			// The classify call site is covered by the arm enumeration, not
			// by its own mutant; require exactly one such call.
			ast.Inspect(function.Body, func(node ast.Node) bool {
				assignment, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				if modelCheckIsStatusAssign(assignment) && modelCheckIsClassifyCall(assignment) {
					classifyCalls++
				}
				return true
			})
		}
	}
	if classifyCalls != 1 {
		t.Fatalf("classifyModelCheckLaunchError call assignments = %d, want exactly 1 (covered by the arm enumeration)", classifyCalls)
	}
	return sites, sources
}

func modelCheckSpanTexts(spans []modelCheckSourceSpan) []string {
	texts := make([]string, 0, len(spans))
	for _, span := range spans {
		texts = append(texts, span.text)
	}
	return texts
}

// modelCheckTopOperands flattens the top-level || or && operands of a branch
// condition. A non-compound condition yields no operands and an invalid
// operator.
func modelCheckTopOperands(cond ast.Expr, span func(ast.Node) modelCheckSourceSpan) ([]modelCheckSourceSpan, token.Token) {
	binary, ok := cond.(*ast.BinaryExpr)
	if !ok || (binary.Op != token.LOR && binary.Op != token.LAND) {
		return nil, token.ILLEGAL
	}
	var operands []modelCheckSourceSpan
	var flatten func(expr ast.Expr)
	flatten = func(expr ast.Expr) {
		if nested, ok := expr.(*ast.BinaryExpr); ok && nested.Op == binary.Op {
			flatten(nested.X)
			flatten(nested.Y)
			return
		}
		operands = append(operands, span(expr))
	}
	flatten(binary)
	return operands, binary.Op
}

func modelCheckIsStatusAssign(assignment *ast.AssignStmt) bool {
	if len(assignment.Lhs) == 0 {
		return false
	}
	selector, ok := assignment.Lhs[0].(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Status" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == "report"
}

func modelCheckIsClassifyCall(assignment *ast.AssignStmt) bool {
	if len(assignment.Rhs) == 0 {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	name, ok := call.Fun.(*ast.Ident)
	return ok && name.Name == "classifyModelCheckLaunchError"
}

// modelCheckBlockCallsClassify reports whether one block assigns from a
// classifyModelCheckLaunchError call (the S10 call site, covered by the arm
// enumeration rather than its own mutant).
func modelCheckBlockCallsClassify(block *ast.BlockStmt) bool {
	called := false
	ast.Inspect(block, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if ok && modelCheckIsClassifyCall(assignment) {
			called = true
			return false
		}
		return true
	})
	return called
}

// modelCheckBlockReason finds `report.Reason = <literal>` or
// `report.Reason = deadlineReason(...)` in one block.
func modelCheckBlockReason(block *ast.BlockStmt) (string, bool) {
	for _, statement := range block.List {
		assignment, ok := statement.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for i, lhs := range assignment.Lhs {
			selector, ok := lhs.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Reason" {
				continue
			}
			identifier, ok := selector.X.(*ast.Ident)
			if !ok || identifier.Name != "report" || i >= len(assignment.Rhs) {
				continue
			}
			switch rhs := assignment.Rhs[i].(type) {
			case *ast.BasicLit:
				if rhs.Kind == token.STRING {
					return strings.Trim(rhs.Value, `"`), true
				}
			case *ast.CallExpr:
				if name, ok := rhs.Fun.(*ast.Ident); ok && name.Name == "deadlineReason" {
					return "deadlineReason", true
				}
			}
		}
	}
	return "", false
}

// modelCheckBlockStatus finds `report.Status = <ident>` in one block.
func modelCheckBlockStatus(block *ast.BlockStmt) string {
	for _, statement := range block.List {
		assignment, ok := statement.(*ast.AssignStmt)
		if !ok || !modelCheckIsStatusAssign(assignment) || len(assignment.Rhs) == 0 {
			continue
		}
		if identifier, ok := assignment.Rhs[0].(*ast.Ident); ok {
			return identifier.Name
		}
	}
	return ""
}

// modelCheckClauseReason finds the refusal reason in one case body: either a
// report.Reason assignment (runModelCheck switch) or a return tuple
// (classify arms).
func modelCheckClauseReason(body []ast.Stmt) (string, bool) {
	if reason, found := modelCheckBlockReason(&ast.BlockStmt{List: body}); found {
		return reason, true
	}
	reason, _ := modelCheckReturnStatusReason(body)
	if reason == "" {
		return "", false
	}
	return reason, true
}

func modelCheckClauseStatus(body []ast.Stmt, returnsTuple bool) string {
	if !returnsTuple {
		return modelCheckBlockStatus(&ast.BlockStmt{List: body})
	}
	_, status := modelCheckReturnStatusReason(body)
	return status
}

// modelCheckReturnStatusReason reads `return <status>, <reason>` in one
// classify arm body.
func modelCheckReturnStatusReason(body []ast.Stmt) (reason, status string) {
	for _, statement := range body {
		returned, ok := statement.(*ast.ReturnStmt)
		if !ok || len(returned.Results) != 2 {
			continue
		}
		statusIdent, ok := returned.Results[0].(*ast.Ident)
		if !ok {
			continue
		}
		switch rhs := returned.Results[1].(type) {
		case *ast.BasicLit:
			if rhs.Kind == token.STRING {
				return strings.Trim(rhs.Value, `"`), statusIdent.Name
			}
		case *ast.CallExpr:
			if name, ok := rhs.Fun.(*ast.Ident); ok && name.Name == "deadlineReason" {
				return "deadlineReason", statusIdent.Name
			}
		}
	}
	return "", ""
}

// --- Guard test ---

func TestModelCheckASTMapsEveryRefusalSiteToNegativeCoverage(t *testing.T) {
	root := modelCheckModuleRoot(t)
	sites, _ := enumerateModelCheckRefusalSites(t, root)
	matched := modelCheckMatchSites(t, sites)
	testFuncs := modelCheckTestFunctions(t, root)
	for _, entry := range modelCheckGuardTable {
		if len(entry.tests) == 0 {
			t.Fatalf("table entry %s has no mapped negative test", entry.id)
		}
		for _, name := range entry.tests {
			parent := name
			if index := strings.Index(name, "/"); index >= 0 {
				parent = name[:index]
			}
			decl, ok := testFuncs[parent]
			if !ok {
				t.Fatalf("table entry %s maps to missing test %s", entry.id, name)
			}
			if strings.Contains(name, "/") {
				sub := strings.ReplaceAll(strings.TrimPrefix(name, parent+"/"), "_", " ")
				if !strings.Contains(decl, `"`+sub+`"`) {
					t.Fatalf("table entry %s maps to subtest %s, no %q in %s", entry.id, name, sub, parent)
				}
			}
		}
		_ = matched
	}
	t.Logf("guard file set: %d files across %d packages; enumerated %d refusal sites, table holds %d entries",
		len(modelCheckGuardFiles(t, root)), len(modelCheckGuardPackages), len(sites), len(modelCheckGuardTable))
}

// modelCheckMatchSites checks the table against the enumeration in both
// directions and verifies operand counts. It returns the sites each entry
// matched, in table order.
func modelCheckMatchSites(t *testing.T, sites []modelCheckEnumeratedSite) [][]modelCheckEnumeratedSite {
	t.Helper()
	matched := make([][]modelCheckEnumeratedSite, len(modelCheckGuardTable))
	consumed := make([]bool, len(sites))
	for i, entry := range modelCheckGuardTable {
		for j, site := range sites {
			if site.reason != entry.reason || site.kind != entry.kind {
				continue
			}
			if entry.function != "" && site.function != entry.function {
				continue
			}
			if entry.condExact != "" && site.cond.text != entry.condExact {
				continue
			}
			matched[i] = append(matched[i], site)
			consumed[j] = true
		}
		if len(matched[i]) == 0 {
			t.Fatalf("table entry %s (%s %s) matches no enumerated site", entry.id, entry.kind, entry.reason)
		}
		if len(matched[i]) > 1 {
			t.Fatalf("table entry %s matches %d sites; add a disambiguator", entry.id, len(matched[i]))
		}
		site := matched[i][0]
		if entry.kind == "default" {
			if entry.prependCase == "" {
				t.Fatalf("table entry %s is a default arm without a prepend-case mutant", entry.id)
			}
			continue
		}
		gotOperands := len(site.operands)
		if gotOperands == 0 {
			gotOperands = 1
		}
		if gotOperands != entry.operands {
			t.Fatalf("table entry %s expects %d operands, enumerated %d at %s:%d (%s)",
				entry.id, entry.operands, gotOperands, site.file, site.line, site.cond.text)
		}
		if entry.operands == 1 && len(entry.witnesses) == 0 {
			t.Fatalf("table entry %s is a singleton without a witness mutant", entry.id)
		}
		if entry.operands > 1 && len(entry.witnesses) != 0 {
			t.Fatalf("table entry %s is compound (%d operands) and must not carry witnesses", entry.id, entry.operands)
		}
		if len(entry.tests) < len(entry.witnesses) || (entry.operands > 1 && len(entry.tests) < entry.operands) {
			t.Fatalf("table entry %s has %d tests for %d mutants; tests[i] must kill mutant i",
				entry.id, len(entry.tests), max(len(entry.witnesses), entry.operands))
		}
	}
	for j, site := range sites {
		if !consumed[j] {
			t.Fatalf("enumerated site at %s:%d (%s %s %q) has no table entry",
				site.file, site.line, site.kind, site.reason, site.cond.text)
		}
	}
	return matched
}

// modelCheckTestFunctions parses the cmd test files for top-level test
// function sources, keyed by name.
func modelCheckTestFunctions(t *testing.T, root string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "tools/agents-management/cmd"))
	if err != nil {
		t.Fatalf("read cmd test dir: %v", err)
	}
	functions := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(root, "tools/agents-management/cmd", entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			functions[function.Name.Name] = string(source[fset.Position(function.Pos()).Offset:fset.Position(function.End()).Offset])
		}
	}
	return functions
}

// --- Mutant harness ---

type modelCheckMutant struct {
	id       string
	file     string // absolute path
	splice   func(source []byte) []byte
	test     string // Parent or Parent/subtest -run pattern
	kill     bool   // false for documented-equivalent mutants (must survive)
	reviewID string // M1/M2/M3/F2/A1 label for the required kills
}

func TestModelCheckGeneratedNarrowingMutantsAreKilledByBehavioralTests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Log("F2/G1 group-kill mutants are unix-only; the remaining mutants still run on windows")
	}
	root := modelCheckModuleRoot(t)
	sites, sources := enumerateModelCheckRefusalSites(t, root)
	matched := modelCheckMatchSites(t, sites)
	mutants := modelCheckGenerateMutants(t, sources, matched)
	mutants = append(mutants, modelCheckSpecifiedMutantRuns(t, root)...)
	modelCheckRequireCleanTree(t, root)
	for _, mutant := range mutants {
		if (mutant.id == "F2-group-kill" || mutant.id == "G1-adapter-group-kill" || mutant.id == "G1-status-group-kill") && runtime.GOOS == "windows" {
			t.Logf("mutant %s skipped on windows (unix-only mechanism)", mutant.id)
			continue
		}
		modelCheckRunMutant(t, root, mutant)
	}
}

func modelCheckGenerateMutants(t *testing.T, sources map[string][]byte, matched [][]modelCheckEnumeratedSite) []modelCheckMutant {
	t.Helper()
	var mutants []modelCheckMutant
	reviewIDs := map[string][]string{
		"pi-process-failed":       {"M1", ""},
		"pi-output-limit":         {"M2"},
		"local-models-unreadable": {"M3"},
	}
	for i, entry := range modelCheckGuardTable {
		site := matched[i][0]
		switch {
		case entry.kind == "default":
			capture := site.insertBefore
			prepend := entry.prependCase
			indent := "\t"
			mutants = append(mutants, modelCheckMutant{
				id:   entry.id + "/prepend-pass",
				file: site.file,
				splice: func(source []byte) []byte {
					out := append([]byte(nil), source[:capture]...)
					out = append(out, indent+prepend...)
					return append(out, source[capture:]...)
				},
				test: entry.tests[0],
				kill: true,
			})
		case entry.operands > 1:
			operator := " || "
			switch site.operator {
			case token.LAND:
				operator = " && "
			case token.COMMA:
				operator = ", "
			}
			for index := range site.operands {
				kept := make([]string, 0, len(site.operands)-1)
				for j, operand := range site.operands {
					if j != index {
						kept = append(kept, operand.text)
					}
				}
				replacement := strings.Join(kept, operator)
				start, end := site.cond.start, site.cond.end
				reviewID := ""
				if ids, ok := reviewIDs[entry.id]; ok && index < len(ids) {
					reviewID = ids[index]
				}
				mutants = append(mutants, modelCheckMutant{
					id:       fmt.Sprintf("%s/drop-operand-%d", entry.id, index),
					file:     site.file,
					splice:   modelCheckReplaceSpan(start, end, replacement),
					test:     entry.tests[index],
					kill:     true,
					reviewID: reviewID,
				})
			}
		default:
			for w, witness := range entry.witnesses {
				replacement := "(" + site.cond.text + ") && (" + witness + ")"
				start, end := site.cond.start, site.cond.end
				reviewID := ""
				if ids, ok := reviewIDs[entry.id]; ok && w < len(ids) {
					reviewID = ids[w]
				}
				mutants = append(mutants, modelCheckMutant{
					id:       fmt.Sprintf("%s/witness-%d", entry.id, w),
					file:     site.file,
					splice:   modelCheckReplaceSpan(start, end, replacement),
					test:     entry.tests[w],
					kill:     entry.equivalent == "",
					reviewID: reviewID,
				})
			}
		}
	}
	// M1-M3 must exist by name.
	for _, want := range []string{"M1", "M2", "M3"} {
		found := false
		for _, mutant := range mutants {
			if mutant.reviewID == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("required mutant %s was not generated", want)
		}
	}
	return mutants
}

func modelCheckReplaceSpan(start, end int, replacement string) func([]byte) []byte {
	return func(source []byte) []byte {
		out := append([]byte(nil), source[:start]...)
		out = append(out, replacement...)
		return append(out, source[end:]...)
	}
}

func modelCheckSpecifiedMutantRuns(t *testing.T, root string) []modelCheckMutant {
	t.Helper()
	var mutants []modelCheckMutant
	for _, specified := range modelCheckSpecifiedMutants {
		path := filepath.Join(root, specified.file)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read mutant file %s: %v", specified.file, err)
		}
		if bytes.Count(source, []byte(specified.find)) != 1 {
			t.Fatalf("mutant %s anchor matches %d times in %s, want exactly once",
				specified.id, bytes.Count(source, []byte(specified.find)), specified.file)
		}
		find, replace := specified.find, specified.replace
		reviewID := ""
		if specified.id == "F2-group-kill" {
			reviewID = "F2"
		}
		if specified.id == "A1-empty-version" {
			reviewID = "A1"
		}
		if specified.id == "G1-adapter-group-kill" || specified.id == "G1-status-group-kill" {
			reviewID = "G1"
		}
		if specified.id == "G2-last-member-wins" {
			reviewID = "G2"
		}
		if specified.id == "R3-outer-alias" {
			reviewID = "R3"
		}
		mutants = append(mutants, modelCheckMutant{
			id:   specified.id,
			file: path,
			splice: func(source []byte) []byte {
				return bytes.Replace(source, []byte(find), []byte(replace), 1)
			},
			test:     specified.test,
			kill:     true,
			reviewID: reviewID,
		})
	}
	return mutants
}

// modelCheckRequireCleanTree proves every mapped test passes without mutation
// before any mutant runs, so a typo'd test name fails here instead of
// masquerading as a surviving mutant.
func modelCheckRequireCleanTree(t *testing.T, root string) {
	t.Helper()
	parents := map[string]bool{}
	collect := func(names []string) {
		for _, name := range names {
			parent := name
			if index := strings.Index(name, "/"); index >= 0 {
				parent = name[:index]
			}
			parents[parent] = true
		}
	}
	for _, entry := range modelCheckGuardTable {
		collect(entry.tests)
	}
	for _, specified := range modelCheckSpecifiedMutants {
		collect([]string{specified.test})
	}
	alternatives := make([]string, 0, len(parents))
	for parent := range parents {
		alternatives = append(alternatives, "^"+parent+"$")
	}
	sort.Strings(alternatives)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "-run", strings.Join(alternatives, "|"), "./tools/agents-management/cmd")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("mapped tests fail on the clean tree:\n%s", output)
	}
	t.Logf("clean tree: %d mapped test functions pass", len(parents))
}

func modelCheckRunMutant(t *testing.T, root string, mutant modelCheckMutant) {
	t.Helper()
	source, err := os.ReadFile(mutant.file)
	if err != nil {
		t.Fatalf("mutant %s: read %s: %v", mutant.id, mutant.file, err)
	}
	mutated := mutant.splice(source)
	if bytes.Equal(mutated, source) {
		t.Fatalf("mutant %s: splice changed nothing", mutant.id)
	}
	directory := t.TempDir()
	mutantPath := filepath.Join(directory, "mutant.go")
	if err := os.WriteFile(mutantPath, mutated, 0o600); err != nil {
		t.Fatalf("mutant %s: write: %v", mutant.id, err)
	}
	overlay, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: map[string]string{mutant.file: mutantPath}})
	if err != nil {
		t.Fatalf("mutant %s: encode overlay: %v", mutant.id, err)
	}
	overlayPath := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatalf("mutant %s: write overlay: %v", mutant.id, err)
	}
	pattern := modelCheckRunPattern(mutant.test)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "-overlay", overlayPath, "-run", pattern, "./tools/agents-management/cmd")
	command.Dir = root
	output, runErr := command.CombinedOutput()
	cancel()
	parent := mutant.test
	if index := strings.Index(mutant.test, "/"); index >= 0 {
		parent = mutant.test[:index]
	}
	failed := strings.Contains(string(output), "--- FAIL: "+parent)
	label := mutant.id
	if mutant.reviewID != "" {
		label += " (" + mutant.reviewID + ")"
	}
	if mutant.kill {
		if runErr == nil || !failed {
			t.Fatalf("narrowing mutant %s survived behavioral test %s (err=%v):\n%s", label, mutant.test, runErr, output)
		}
		t.Logf("narrowing mutant %s killed by %s", label, mutant.test)
		return
	}
	if runErr != nil || failed {
		t.Fatalf("documented-equivalent mutant %s was killed by %s; the equivalence claim is wrong:\n%s", label, mutant.test, output)
	}
	t.Logf("equivalent mutant %s survives %s as documented", label, mutant.test)
}

func modelCheckRunPattern(test string) string {
	parent := test
	sub := ""
	if index := strings.Index(test, "/"); index >= 0 {
		parent = test[:index]
		sub = test[index+1:]
	}
	if sub == "" {
		return "^" + regexp.QuoteMeta(parent) + "$"
	}
	return "^" + regexp.QuoteMeta(parent) + "$/^" + regexp.QuoteMeta(sub) + "$"
}
