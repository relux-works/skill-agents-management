package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/refusalscan"
)

func TestSwitchDefaultMemberReachabilityUsesOnlyItsCase(t *testing.T) {
	root := t.TempDir()
	packageDirectory := filepath.Join(root, "pkg", "agentic")
	if err := os.MkdirAll(packageDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	const source = `package agentic
var ErrUnknown error
func validate(kind int) error {
	switch kind {
	case 1:
		if !helper() { return nil }
	default:
		return ErrUnknown
	}
	return nil
}
func helper() bool { return false }
`
	path := filepath.Join("pkg", "agentic", "sample.go")
	if err := os.WriteFile(filepath.Join(root, path), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseSource(root, path)
	if err != nil {
		t.Fatal(err)
	}
	site := refusalscan.Site{
		File: "sample.go", Function: "validate", Guard: "switch kind case default",
		Return: "return ErrUnknown", Line: 8,
	}
	_, selected, _, _ := curatorSiteTarget(parsed.fset, parsed.file, site)
	if _, ok := selected.(*ast.CaseClause); !ok {
		t.Fatalf("switch default target = %T, want its CaseClause", selected)
	}
	if containsString(callsFromNodes(selected), "helper") {
		t.Fatalf("switch default reachability includes helper from another case: %v", callsFromNodes(selected))
	}
}

func TestDownstreamEquivalenceRequiresAnIndependentGreenRun(t *testing.T) {
	for _, test := range []struct {
		name            string
		independentExit int
		combinedExit    int
		want            bool
	}{
		{name: "guard only preserves test and pair fails", independentExit: 0, combinedExit: 1, want: true},
		{name: "guard alone already fails test", independentExit: 1, combinedExit: 1, want: false},
		{name: "combined run stays green", independentExit: 0, combinedExit: 0, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := downstreamMutationProvesEquivalence(test.independentExit, test.combinedExit); got != test.want {
				t.Fatalf("downstreamMutationProvesEquivalence(%d, %d) = %v, want %v", test.independentExit, test.combinedExit, got, test.want)
			}
		})
	}
}

func TestBooleanHelperDownstreamProofsAreDerivedFromCallerAST(t *testing.T) {
	root := t.TempDir()
	packageDirectory := filepath.Join(root, "pkg", "agentic")
	if err := os.MkdirAll(packageDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(packageDirectory, "curator_context.go")
	const source = `package agentic
func validates(value rune) bool {
	if !narrowed(value) { return false }
	if !curatorAlphaNumeric(value) && value != '.' { return false }
	return true
}
func narrowed(value rune) bool { return value >= 'a' && value <= 'z' }
func curatorAlphaNumeric(value rune) bool { return value >= 'a' && value <= 'z' }
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseSource(root, filepath.Join("pkg", "agentic", "curator_context.go"))
	if err != nil {
		t.Fatal(err)
	}
	proofs := deriveBooleanHelperDownstreamProofs(curatorValidatorClassMember{
		targetFunction: "narrowed",
		mutationKind:   "bool-return-allow",
		exemption:      "value == '`'",
	}, map[string]*parsedSource{filepath.Join("pkg", "agentic", "curator_context.go"): parsed})
	for _, proof := range proofs {
		if proof.function == "validates" && strings.Contains(proof.guard, "curatorAlphaNumeric(value)") {
			return
		}
	}
	t.Fatalf("AST-derived downstream proofs = %#v, want the caller's boolean rejection guard", proofs)
}

func TestHelperMembersRequireAReachableCallerRefusalWitness(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	sites, err := refusalscan.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := curatorValidatorPackageSources(root)
	if err != nil {
		t.Fatal(err)
	}
	functions := curatorValidatorFunctions(sources)
	helper := functions["validCuratorIdentifier"]
	var emptyMember *curatorValidatorClassMember
	for _, member := range curatorBooleanHelperMembers(sources, helper) {
		if member.exemption == "len(runes) == 0" && member.targetReturn == "return false" {
			candidate := member
			emptyMember = &candidate
			break
		}
	}
	if emptyMember == nil {
		t.Fatal("AST-derived validCuratorIdentifier empty member is missing")
	}
	checks := []struct {
		name        string
		matches     func(refusalscan.Site) bool
		wantAffects bool
	}{
		{
			name: "preempted profile name",
			matches: func(site refusalscan.Site) bool {
				return site.Function == "ValidateCuratorContext" && strings.Contains(site.Guard, "validCuratorIdentifier(context.Profile.Name)")
			},
			wantAffects: false,
		},
		{
			name: "masked empty path segment",
			matches: func(site refusalscan.Site) bool {
				return site.Function == "validateCuratorDescriptor" && strings.Contains(site.Guard, "validCuratorPortablePath(descriptor.Filename)")
			},
			wantAffects: false,
		},
		{
			name: "direct config key argument",
			matches: func(site refusalscan.Site) bool {
				return site.Function == "validateCuratorDescriptor" && strings.Contains(site.Guard, "validCuratorIdentifier(descriptor.Key)")
			},
			wantAffects: true,
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			for _, site := range sites {
				if !check.matches(site) {
					continue
				}
				source := sources[filepath.Join("pkg", "agentic", filepath.FromSlash(site.File))]
				_, targetNode, targetReturn, _ := curatorSiteTarget(source.fset, source.file, site)
				reachable := reachableCuratorFunctions(callsFromNodes(targetNode, targetReturn), functions)
				got := curatorHelperMemberChangesSite(site, targetNode, *emptyMember, reachable, functions, sources)
				if got != check.wantAffects {
					t.Fatalf("AST callsite membership = %v, want %v for %s", got, check.wantAffects, site.Key())
				}
				return
			}
			t.Fatal("matching hermetic refusal site was not found")
		})
	}
}

func TestRuneHelperMemberMustReachTheMappedStringGate(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	sites, err := refusalscan.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := curatorValidatorPackageSources(root)
	if err != nil {
		t.Fatal(err)
	}
	functions := curatorValidatorFunctions(sources)
	var member *curatorValidatorClassMember
	for _, candidate := range curatorBooleanHelperMembers(sources, functions["curatorIdentifierTail"]) {
		witness, ok := curatorRuneWitnessForMember(functions[candidate.targetFunction], candidate.exemption)
		if ok && witness == '`' && candidate.mutationKind == "bool-return-allow" {
			copy := candidate
			member = &copy
			break
		}
	}
	if member == nil {
		t.Fatal("AST-derived backtick boundary member is missing")
	}
	checks := []struct {
		name        string
		matches     func(refusalscan.Site) bool
		wantAffects bool
	}{
		{
			name: "direct config key gate",
			matches: func(site refusalscan.Site) bool {
				return site.Function == "validateCuratorDescriptor" && strings.Contains(site.Guard, "validCuratorIdentifier(descriptor.Key)")
			},
			wantAffects: true,
		},
		{
			name: "file path conjunction masks backtick",
			matches: func(site refusalscan.Site) bool {
				return site.Function == "validateCuratorDescriptor" && strings.Contains(site.Guard, "validCuratorPortablePath(descriptor.Filename)")
			},
			wantAffects: false,
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			for _, site := range sites {
				if !check.matches(site) {
					continue
				}
				source := sources[filepath.Join("pkg", "agentic", filepath.FromSlash(site.File))]
				_, targetNode, targetReturn, _ := curatorSiteTarget(source.fset, source.file, site)
				reachable := reachableCuratorFunctions(callsFromNodes(targetNode, targetReturn), functions)
				got := curatorHelperMemberChangesSite(site, targetNode, *member, reachable, functions, sources)
				if got != check.wantAffects {
					t.Fatalf("AST call-chain membership = %v, want %v for %s", got, check.wantAffects, site.Key())
				}
				return
			}
			t.Fatal("matching hermetic refusal site was not found")
		})
	}
}

func TestBooleanGuardMemberDerivesCallerArgumentRefusalProof(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	sources, err := curatorValidatorPackageSources(root)
	if err != nil {
		t.Fatal(err)
	}
	validator := sources[filepath.Join("pkg", "agentic", "curator_context.go")]
	helper := findFunctionAndBody(validator.file, "validCuratorIdentifier")
	caller := findFunctionAndBody(validator.file, "ValidateCuratorContext")
	localValues := helperLocalExpressionValues(helper, validator.fset)
	localExpr, localErr := replaceExpressionIdentifiers("len(runes) == 0", localValues)
	if localErr != nil || localExpr != "len([]rune(value)) == 0" {
		t.Fatalf("helper-local witness = %q, err=%v", localExpr, localErr)
	}
	var profileCallWitness string
	ast.Inspect(caller.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && callName(call) == "validCuratorIdentifier" {
			profileCallWitness, _ = helperWitnessAtCall(validator.fset, helper, call, localValues, "len(runes) == 0")
		}
		return true
	})
	if profileCallWitness != "len([]rune(context.Profile.Name)) == 0" {
		t.Fatalf("profile-call witness = %q", profileCallWitness)
	}
	proofs := deriveBooleanHelperDownstreamProofs(curatorValidatorClassMember{
		targetFunction: "validCuratorIdentifier",
		exemption:      "len(runes) == 0",
	}, sources)
	for _, proof := range proofs {
		if proof.function == "ValidateCuratorContext" && proof.returned == "return ErrCuratorProfileMissing" && strings.Contains(proof.exemption, "context.Profile.Name") {
			return
		}
	}
	t.Fatalf("AST-derived caller refusal proofs = %#v, want the missing-profile refusal guard", proofs)
}

func TestEmptySplitMemberDerivesSameFunctionDownstreamProof(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	sources, err := curatorValidatorPackageSources(root)
	if err != nil {
		t.Fatal(err)
	}
	relative := filepath.Join("pkg", "agentic", "curator_context.go")
	source := sources[relative]
	function := findFunctionAndBody(source.file, "validCuratorPortablePath")
	if function == nil {
		t.Fatal("validCuratorPortablePath is missing from the validator tree")
	}
	targetLine := source.fset.Position(function.Pos()).Line + 1
	for _, exemption := range []string{
		"utf8.RuneCountInString(value) == 0",
		`value == "/__unmapped__"`,
		`value == "__unmapped__//__member__"`,
	} {
		proofs := deriveBooleanHelperDownstreamProofs(curatorValidatorClassMember{
			targetFile: relative, targetFunction: "validCuratorPortablePath",
			targetLine: targetLine, exemption: exemption,
		}, sources)
		found := false
		for _, proof := range proofs {
			if proof.function == "validateCuratorDescriptor" && strings.Contains(proof.guard, "validCuratorPortablePath") {
				t.Fatalf("same-helper narrowing was incorrectly accepted through its direct consumer: %#v", proof)
			}
			if proof.function == "validCuratorPortablePath" && strings.Contains(proof.guard, `segment == ""`) && proof.exemption == exemption {
				found = true
			}
		}
		if !found {
			t.Fatalf("AST-derived proof for %q = %#v, want the downstream empty segment guard", exemption, proofs)
		}
	}
}

func TestValidatorMutantCompletenessRejectsDeletedNegativeTest(t *testing.T) {
	root := t.TempDir()
	packageDirectory := filepath.Join(root, "pkg", "agentic")
	if err := os.MkdirAll(packageDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(packageDirectory, "curator_context_test.go")
	const testSource = `package agentic
import "testing"
func TestBuildPlanCuratorContextRefusesMalformedFragment(t *testing.T) {}
`
	if err := os.WriteFile(testFile, []byte(testSource), 0o600); err != nil {
		t.Fatal(err)
	}
	available, err := curatorBuildPlanTestFunctions(root)
	if err != nil || !available["TestBuildPlanCuratorContextRefusesMalformedFragment"] {
		t.Fatalf("initial negative-test inventory = %#v, err=%v", available, err)
	}
	if err := os.WriteFile(testFile, []byte("package agentic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	available, err = curatorBuildPlanTestFunctions(root)
	if err != nil {
		t.Fatal(err)
	}
	member := curatorValidatorClassMember{
		identity: "site\x00helper\x00reserved-name",
		testName: "TestBuildPlanCuratorContextRefusesMalformedFragment",
	}
	candidate := mutant{validatorMemberID: member.identity, testName: member.testName}
	err = validateValidatorMutantCompleteness([]curatorValidatorClassMember{member}, []mutant{candidate}, available)
	if err == nil || !strings.Contains(err.Error(), "AST-derived member has no named BuildPlan negative test") {
		t.Fatalf("completeness error after deleting the negative test = %v", err)
	}
}

func TestValidatorMutantCompletenessRejectsMissingASTDerivedMember(t *testing.T) {
	first := curatorValidatorClassMember{identity: "site\x00bound-zero", testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"}
	second := curatorValidatorClassMember{identity: "site\x00bound-one", testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"}
	candidate := mutant{validatorMemberID: first.identity}
	err := validateValidatorMutantCompleteness(
		[]curatorValidatorClassMember{first, second},
		[]mutant{candidate},
		map[string]bool{first.testName: true},
	)
	if err == nil || !strings.Contains(err.Error(), "AST-derived validator member has no narrowing mutant") {
		t.Fatalf("completeness error = %v, want missing AST-derived member refusal", err)
	}
}

func TestValidatorMutantCompletenessRejectsDeletedNegativeSubtest(t *testing.T) {
	candidate := mutant{
		name:              "curator-validator-profile-reserved-name",
		validatorMemberID: "site\x00profile-CON",
		runPattern:        `^TestBuildPlanCuratorContextRefusesMalformedFragment$/^(claude|codex)$/^profile-identifier-CON$`,
	}
	deletedNegativeSubtestOutput := strings.Join([]string{
		"=== RUN   TestBuildPlanCuratorContextRefusesMalformedFragment",
		"=== RUN   TestBuildPlanCuratorContextRefusesMalformedFragment/codex",
		"=== RUN   TestBuildPlanCuratorContextRefusesMalformedFragment/codex/profile-identifier-other",
		"--- PASS: TestBuildPlanCuratorContextRefusesMalformedFragment/codex/profile-identifier-other (0.00s)",
		"--- PASS: TestBuildPlanCuratorContextRefusesMalformedFragment/codex (0.00s)",
		"PASS",
	}, "\n")
	err := validateMutantNamedTestExecution(candidate, deletedNegativeSubtestOutput)
	if err == nil || !strings.Contains(err.Error(), "named BuildPlan negative subtest") {
		t.Fatalf("completeness error = %v, want deleted named negative subtest refusal", err)
	}
}

func TestMutantFailureTestNameReportsTheNamedBuildPlanSubtest(t *testing.T) {
	candidate := mutant{
		name:              "curator-validator-profile-reserved-name",
		testName:          "TestBuildPlanCuratorContextRefusesMalformedFragment",
		validatorMemberID: "site\x00profile-CON",
		runPattern:        `^TestBuildPlanCuratorContextRefusesMalformedFragment$/^(claude|codex)$/^profile-identifier-CON$`,
	}
	output := strings.Join([]string{
		"        --- FAIL: TestBuildPlanCuratorContextRefusesMalformedFragment/claude/profile-identifier-CON (0.00s)",
		"    --- FAIL: TestBuildPlanCuratorContextRefusesMalformedFragment/claude (0.00s)",
		"--- FAIL: TestBuildPlanCuratorContextRefusesMalformedFragment (0.00s)",
	}, "\n")
	if got := mutantFailureTestName(candidate, output); got != "TestBuildPlanCuratorContextRefusesMalformedFragment/claude/profile-identifier-CON" {
		t.Fatalf("mutantFailureTestName() = %q", got)
	}
}

func TestMutantRunPatternMatchesSlashNestedBuildPlanSubtests(t *testing.T) {
	pattern := `^TestBuildPlanCuratorContextRefusesMalformedFragment$/^(claude|codex)$/^(profile-identifier-.*|profile-name-malformed)$`
	path := "TestBuildPlanCuratorContextRefusesMalformedFragment/claude/profile-identifier-/a"
	matched, err := mutantRunPatternMatches(pattern, path)
	if err != nil || !matched {
		t.Fatalf("mutantRunPatternMatches() = %t, %v for %q", matched, err, path)
	}
}

func TestCuratorValidatorRunPatternUsesBothPluginsAndChannelWitnesses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		member      curatorValidatorClassMember
		wantSubtest string
	}{
		{
			name: "malformed helper reason",
			member: curatorValidatorClassMember{
				testName:       "TestBuildPlanCuratorContextRefusesMalformedFragment",
				member:         `malformed reason "flag descriptor has an invalid argument kind"`,
				targetFunction: "curatorMalformed",
			},
			wantSubtest: "flag_argument_kind_invalid",
		},
		{
			name: "unselected append channel",
			member: curatorValidatorClassMember{
				testName:       "TestBuildPlanCuratorContextRefusesMalformedFragment",
				subtestPattern: "system-prompt-unselected-append-.*",
			},
			wantSubtest: "system-prompt-unselected-append-flag-whitespace",
		},
		{
			name: "unselected replace channel",
			member: curatorValidatorClassMember{
				testName:       "TestBuildPlanCuratorContextRefusesMalformedFragment",
				subtestPattern: "system-prompt-unselected-replace-.*",
			},
			wantSubtest: "system-prompt-unselected-replace-argument-kind-path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pattern := curatorValidatorRunPattern(tc.member)
			for _, plugin := range []string{"claude", "codex"} {
				matched, err := mutantRunPatternMatches(pattern, tc.member.testName+"/"+plugin+"/"+tc.wantSubtest)
				if err != nil || !matched {
					t.Errorf("curatorValidatorRunPattern() = %q, match=%t, err=%v for %s", pattern, matched, err, plugin)
				}
			}
		})
	}
}

func TestEveryGeneratedCuratorValidatorRunPatternSelectsBothPlugins(t *testing.T) {
	// Best-effort matrix check only: t.Skip and aliased plugin conditions are
	// known blind spots tracked by TASK-260930-3txv44. This test does not claim
	// per-plugin mutation completeness.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parsedFiles, err := curatorValidatorPackageSources(root)
	if err != nil {
		t.Fatal(err)
	}
	sites, err := refusalscan.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	var validatorSites []refusalscan.Site
	for _, site := range sites {
		if site.File == "curator_context.go" {
			validatorSites = append(validatorSites, site)
		}
	}
	catalogPath := filepath.Join(root, "pkg", "agentic", "curator_refusal_guard_test.go")
	catalog, err := parser.ParseFile(token.NewFileSet(), catalogPath, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseCatalogSlice(catalog, "curatorRefusalCoverageTable")
	if err != nil {
		t.Fatal(err)
	}
	coverage := make(map[string]string)
	for _, row := range rows {
		if row["file"] != "curator_context.go" {
			continue
		}
		occurrence, err := strconv.Atoi(row["occurrence"])
		if err != nil {
			t.Fatal(err)
		}
		coverage[catalogSiteKey(row["file"], row["function"], row["guard"], row["returned"], occurrence)] = row["testName"]
	}
	members, err := deriveCuratorValidatorClassMembers(root, validatorSites, coverage, curatorValidatorFunctions(parsedFiles), parsedFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) == 0 {
		t.Fatal("curator validator enumeration returned no members")
	}
	if err := curatorValidatorMatrixPluginRestriction(root); err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		pattern := curatorValidatorRunPattern(member)
		if member.testName != "TestBuildPlanCuratorContextRefusesMalformedFragment" {
			continue
		}
		segments := strings.Split(pattern, "/")
		if len(segments) > 1 && segments[1] != "^(claude|codex)$" {
			t.Errorf("generated run pattern restricts plugin coverage: member=%q pattern=%q", member.identity, pattern)
		}
	}
}

func curatorValidatorMatrixPluginRestriction(root string) error {
	path := filepath.Join(root, "pkg", "agentic", "curator_context_acceptance_test.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return err
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name == nil || function.Name.Name != "TestBuildPlanCuratorContextRefusesMalformedFragment" {
			continue
		}
		var restriction error
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if restriction != nil {
				return false
			}
			if identifier, ok := node.(*ast.Ident); ok && identifier.Name == "skipPlugin" {
				restriction = fmt.Errorf("generated curator validator test matrix contains skipPlugin; every selected case must run for claude and codex")
				return false
			}
			branch, ok := node.(*ast.IfStmt)
			if !ok || branch.Cond == nil {
				return true
			}
			hasPlugin := false
			ast.Inspect(branch.Cond, func(condition ast.Node) bool {
				if identifier, ok := condition.(*ast.Ident); ok && identifier.Name == "plugin" {
					hasPlugin = true
				}
				return true
			})
			if hasPlugin && blockHasReturnOrContinue(branch.Body) {
				restriction = fmt.Errorf("generated curator validator test matrix has a plugin-conditional early exit")
				return false
			}
			return true
		})
		return restriction
	}
	return fmt.Errorf("malformed Curator BuildPlan test was not found")
}

func blockHasReturnOrContinue(block *ast.BlockStmt) bool {
	if block == nil {
		return false
	}
	found := false
	ast.Inspect(block, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt, *ast.BranchStmt:
			found = true
		}
		return !found
	})
	return found
}

func TestPerPluginKillAccountingRequiresSeparateNamedFailures(t *testing.T) {
	c := mutant{name: "witness", validatorMemberID: "member", testName: "TestBuildPlan", runPattern: "^TestBuildPlan$/^(claude|codex)$/^negative$", requiredPlugins: []string{"claude", "codex"}}
	process := func(plugin string, code int, name string) string {
		return fmt.Sprintf("PLUGIN_PROCESS | mutant=witness | plugin=%s | exit=%d | pattern=unused\n=== RUN   %s\n--- FAIL: %s (0.00s)\n", plugin, code, name, name)
	}
	for _, tc := range []struct {
		name, output string
		red          bool
	}{
		{"both", process("claude", 1, "TestBuildPlan/claude/negative") + process("codex", 1, "TestBuildPlan/codex/negative"), false},
		{"one_plugin", process("claude", 1, "TestBuildPlan/claude/negative"), true},
		{"green_codex", process("claude", 1, "TestBuildPlan/claude/negative") + process("codex", 0, "TestBuildPlan/codex/negative"), true},
		{"wrong_named_test", process("claude", 1, "TestBuildPlan/claude/negative") + process("codex", 1, "TestBuildPlan/codex/other"), true},
		{"borrowed_failure", process("claude", 1, "TestBuildPlan/claude/negative") + "--- FAIL: TestBuildPlan/codex/negative (0.00s)\n", true},
		{"unnamed_compilation_error", process("claude", 1, "TestBuildPlan/claude/negative") + "PLUGIN_PROCESS | mutant=witness | plugin=codex | exit=1 | pattern=unused\n[build failed]\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePerPluginKills(c, tc.output)
			if (err != nil) != tc.red {
				t.Fatalf("accounting err=%v, red=%t", err, tc.red)
			}
		})
	}
}

func TestValidatorPluginPatternRejectsRestrictedPatterns(t *testing.T) {
	for _, plugin := range []string{"claude", "codex"} {
		pattern, err := validatorPluginPattern("^TestBuildPlan$", plugin)
		if err != nil || pattern != "^TestBuildPlan$/^"+plugin+"$" {
			t.Fatalf("pattern=%s err=%v", pattern, err)
		}
	}
	if _, err := validatorPluginPattern("^TestBuildPlan$/^claude$/^negative$", "codex"); err == nil {
		t.Fatal("plugin-restricted pattern admitted")
	}
}

func TestRunCandidateTestsUsesTwoRealPluginProcesses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module plugin.fixture\n\ngo 1.25.5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := `package fixture
import("testing";"os";"fmt")
func TestBuildPlan(t *testing.T) {for _,plugin:=range []string{"claude","codex"} {t.Run(plugin,func(t *testing.T){t.Run("negative",func(t *testing.T){fmt.Printf("REAL_PROCESS | plugin=%s | pid=%d\n",plugin,os.Getpid());t.Fatal("BuildPlan admitted refusal")})})}}
`
	if err := os.WriteFile(filepath.Join(root, "plugin_test.go"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	c := mutant{name: strings.Repeat("real-process-", 30), validatorMemberID: "member", testPackage: ".", testName: "TestBuildPlan", runPattern: "^TestBuildPlan$/^(claude|codex)$/^negative$", requiredPlugins: []string{"claude", "codex"}}
	output, code := runCandidateTests(root, root, "", "", c)
	if code != 1 {
		t.Fatalf("exit=%d\n%s", code, output)
	}
	if err := validatePerPluginKills(c, output); err != nil {
		t.Fatal(err)
	}
	pids := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "REAL_PROCESS | ") {
			_, pid, ok := strings.Cut(line, "pid=")
			if ok {
				pids[pid] = true
			}
		}
	}
	if len(pids) != 2 {
		t.Fatalf("expected two real processes, pids=%v\n%s", pids, output)
	}
}

func TestPerPluginAccountingNarrowingMutant(t *testing.T) {
	source, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := copyTree(source, root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tools", "launchcontext-mutants", "main.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := "records := strings.Split(output, \"PLUGIN_PROCESS | \")\n\tfor _, plugin := range candidate.requiredPlugins {"
	after := "records := strings.Split(output, \"PLUGIN_PROCESS | \")\n\tfor _, plugin := range candidate.requiredPlugins[:1] {"
	if strings.Count(string(body), before) != 1 {
		t.Fatal("per-plugin narrowing site no longer unique")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(body), before, after, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./tools/launchcontext-mutants", "-count=1", "-v", "-run", "^TestPerPluginKillAccountingRequiresSeparateNamedFailures$")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if code != 1 || !strings.Contains(string(output), "--- FAIL: TestPerPluginKillAccountingRequiresSeparateNamedFailures/one_plugin") || strings.Contains(string(output), "[build failed]") {
		t.Fatalf("accounting narrowing survived or was invalid, exit=%d\n%s", code, output)
	}
	t.Log("KILLED | mutant=one_plugin_accounting | exit=1 | test=TestPerPluginKillAccountingRequiresSeparateNamedFailures/one_plugin")
}

func TestPartialPluginSurvivorRequiresRealDownstreamBound(t *testing.T) {
	sourceRoot := t.TempDir()
	relative := filepath.Join("pkg", "agentic", "curator_context.go")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(sourceRoot, relative)), 0700); err != nil {
		t.Fatal(err)
	}
	source := `package agentic
import "errors"
func validate(argument, name string) error {
 if argument != "path" && argument != "name" {
  return errors.New("argument")
 }
 if argument != "name" && name != "" {
  return errors.New("name")
 }
 return nil
}
`
	testSource := `package agentic
import "testing"
func TestBuildPlanProof(t *testing.T) {for _,plugin:=range []string{"claude","codex"} {t.Run(plugin,func(t *testing.T) {name:="";if plugin=="codex" {name="managed"};if err:=validate("future",name);err==nil {t.Fatal("BuildPlan admitted future argument")}})}}
`
	for path, body := range map[string]string{"go.mod": "module validator.proof\n\ngo 1.25.5\n", relative: source, filepath.Join("pkg", "agentic", "proof_test.go"): testSource} {
		if err := os.WriteFile(filepath.Join(sourceRoot, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relative, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	member := curatorValidatorClassMember{targetFile: relative, targetFunction: "validate", targetLine: 5, targetGuard: `if argument != "path" && argument != "name"`, targetReturn: `return errors.New("argument")`, exemption: `argument == "future"`}
	proofs := deriveSameFunctionScalarDownstreamProofs(member, map[string]*parsedSource{relative: {fset: fset, file: file}})
	if len(proofs) != 1 || proofs[0].guard != `if argument != "name" && name != ""` {
		t.Fatalf("proofs=%+v", proofs)
	}
	for _, tc := range []struct {
		name      string
		effective bool
	}{{"proved", true}, {"reject_ineffective_downstream", false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := copyTree(sourceRoot, root); err != nil {
				t.Fatal(err)
			}
			candidate := mutant{name: "partial-plugin", validatorMemberID: "member", file: relative, astFunction: "validate", astGuard: member.targetGuard, astSiteLine: 5, astSiteReturn: member.targetReturn, exemption: member.exemption, testPackage: "./pkg/agentic", testName: "TestBuildPlanProof", runPattern: "^TestBuildPlanProof$/^(claude|codex)$", failureText: "BuildPlan", requiredPlugins: []string{"claude", "codex"}, downstreamProofs: append([]downstreamGuardProof(nil), proofs...)}
			if !tc.effective {
				candidate.downstreamProofs[0].exemption = "false"
			}
			if err := applyASTNarrowingAtSite(filepath.Join(root, relative), "validate", candidate.astGuard, 5, candidate.astSiteReturn, candidate.exemption); err != nil {
				t.Fatal(err)
			}
			primary, code := runCandidateTests(root, sourceRoot, "", "", candidate)
			if code != 1 || validatePerPluginKills(candidate, primary) == nil || !strings.Contains(primary, "plugin=claude | exit=1") || !strings.Contains(primary, "plugin=codex | exit=0") {
				t.Fatalf("partial mutant incorrectly classified: exit=%d\n%s", code, primary)
			}
			proof, combined, err := proveValidatorEquivalentMutant(root, sourceRoot, "", "", candidate)
			if err != nil {
				t.Fatal(err)
			}
			if tc.effective {
				if proof == nil || validatePerPluginKills(candidate, combined) != nil {
					t.Fatalf("missing separate-process downstream bound: %+v\n%s", proof, combined)
				}
			} else if proof != nil {
				t.Fatalf("ineffective downstream weakening claimed a bound: %+v", proof)
			}
		})
	}
}

func TestDownstreamProofRejectsAmbiguousRelocation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "guard.go")
	source := `package guard
func validate(name string) error {
 if name != "" {return refused()}
 if name != "" {return refused()}
 return nil
}
`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	proof := downstreamGuardProof{function: "validate", guard: `if name != ""`, returned: "return refused()", exemption: `name == "future"`}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = applyDownstreamNarrowing(path, proof)
	if err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("ambiguous downstream triple admitted: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("ambiguity rejection changed the source")
	}
}

func TestDownstreamRelocationNarrowingMutant(t *testing.T) {
	source, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := copyTree(source, root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tools", "launchcontext-mutants", "main.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := `if len(lines) != 1 {
		return fmt.Errorf("downstream function/guard/return triple is not unique: %d matches", len(lines))`
	after := `if len(lines) == 0 {
		return fmt.Errorf("downstream function/guard/return triple is not unique: %d matches", len(lines))`
	if strings.Count(string(body), before) != 1 {
		t.Fatal("relocation narrowing site is not unique")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(body), before, after, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./tools/launchcontext-mutants", "-count=1", "-v", "-run", "^TestDownstreamProofRejectsAmbiguousRelocation$")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if code != 1 || !strings.Contains(string(output), "--- FAIL: TestDownstreamProofRejectsAmbiguousRelocation") || strings.Contains(string(output), "[build failed]") {
		t.Fatalf("relocation mutant survived or invalid, exit=%d\n%s", code, output)
	}
	t.Log("KILLED | mutant=ambiguous_downstream_relocation | exit=1 | test=TestDownstreamProofRejectsAmbiguousRelocation")
}

func TestLocalEffortSubsetRefusalIsInFullCensus(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generatedCuratorConflictMutants(root); err != nil {
		t.Fatalf("full refusal census: %v", err)
	}
	sites, err := refusalscan.LocalCodexSites(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, site := range sites {
		if site.Function == "checkLocalEffortVocabulary" {
			found = true
		}
	}
	if !found {
		t.Fatal("local declared vocabulary gate missing from semantic census")
	}
	for _, candidate := range narrowingMutants() {
		if candidate.name == "local-effort-declared-max-admitted" {
			if candidate.testPackage != "./pkg/vendorplugin/vendors/local-models" || !strings.Contains(candidate.replacements[0].after, "slices.Contains(native, word)") {
				t.Fatal("local narrowing mutant must preserve the inspected token and run consumer behavior")
			}
			return
		}
	}
	t.Fatal("local declared vocabulary gate has no consumer narrowing mutant")
}
