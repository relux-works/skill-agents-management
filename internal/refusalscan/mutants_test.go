package refusalscan

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the behavioral tests after each weakening. Searched tokens remain in
// every variant; compiling alone or running a text checker is not a kill.
func TestScopedConsumptionNarrowingMutants(t *testing.T) {
	cases := []struct{ name, file, before, after, pattern, failure string }{
		{"constructor_only", "scan.go", "validateRefusalUseShapes(files, errors, errorTypes, helpers)", "validateRefusalUseShapes(files, errors, errorTypes, constructorOnly(files, errors, errorTypes, helpers))", "^TestDiscoverRejectsZZNewForward$", "TestDiscoverRejectsZZNewForward"},
		{"prefix_only", "scan.go", "object != nil && implementsError(object.Type())", "object != nil && strings.HasPrefix(object.Name(), \"ErrCurator\") && implementsError(object.Type())", "^TestDiscoverRecognizesUnprefixedSentinelNewFile$", "TestDiscoverRecognizesUnprefixedSentinelNewFile"},
		{"panic_not_last", "shapes.go", "block.List[len(block.List)-1] == s", "s != nil", "^TestAcceptedConsumptionShapes$/^panic_not_last$", "TestAcceptedConsumptionShapes/panic_not_last"},
		{"panic_error_result", "shapes.go", " || hasErrorResult(c.results, c.errorTypes)", "", "^TestAcceptedConsumptionShapes$/^panic_error_result$", "TestAcceptedConsumptionShapes/panic_error_result"},
		{"non_protocol_Error", "types.go", "object, ok := info.ObjectOf(function.Name).(*types.Func)", "if function.Recv != nil && function.Name.Name == \"Error\" {return true}; object, ok := info.ObjectOf(function.Name).(*types.Func)", "^TestAcceptedConsumptionShapes$/^non_protocol_Error$", "TestAcceptedConsumptionShapes/non_protocol_Error"},
		{"field_storage", "shapes.go", "if s != init {", "if s != init && !fieldTarget(s) {", "^TestAcceptedConsumptionShapes$/^field_storage$", "TestAcceptedConsumptionShapes/field_storage"},
		{"wrong_wrapped_argument", "shapes.go", "!wrapped[i] || !c.propagation(arg)", "!wrapped[i] && !hasWrappingVerb(constant.StringVal(format.Value)) || !c.propagation(arg)", "^TestAcceptedConsumptionShapes$/^wrong_wrapped_argument$", "TestAcceptedConsumptionShapes/wrong_wrapped_argument"},
		{"classification_to_error", "shapes.go", "if !hasWrappingVerb(constant.StringVal(format.Value)) {", "if len(e.Args)==2 && classificationExpression(e.Args[1], c.errors.info) {return true}; if !hasWrappingVerb(constant.StringVal(format.Value)) {", "^TestAcceptedConsumptionShapes$/^classification_to_error$", "TestAcceptedConsumptionShapes/classification_to_error"},
		{"classification_local_to_error", "shapes.go", "if hasErrorType(c.errors.info.TypeOf(call)) {", "if hasErrorType(c.errors.info.TypeOf(call)) && func()bool {obj:=calledObject(call,c.errors.info);return obj==nil || obj.Pkg()==nil || obj.Pkg().Path()!=\"fmt\" || obj.Name()!=\"Errorf\"}() {", "^TestAcceptedConsumptionShapes$/^classification_local_to_error$", "TestAcceptedConsumptionShapes/classification_local_to_error"},
		{"name_only_identity", "types.go", "return s.objects[s.info.ObjectOf(expression)]", "for object := range s.objects {if object.Name() == expression.Name {return true}};return false", "^TestAcceptedConsumptionShapes$/^(args_names|shadowed_sentinel)$", "TestAcceptedConsumptionShapes/args_names"},
		{"local_not_found", "shapes.go", "if c.consumption(e) && !c.discard(s, i) {\n\t\t\t\t\tif c.convertedBindingsSafe(body, s, i) {", "if (c.consumption(e) || !hasErrorResult(results, c.errorTypes) && c.helperCall(e)) && !c.discard(s, i) {\n\t\t\t\t\tif !hasErrorResult(results, c.errorTypes) && c.helperCall(e) || c.convertedBindingsSafe(body, s, i) {", "^TestAcceptedConsumptionShapes$/^not_found_local$", "TestAcceptedConsumptionShapes/not_found_local"},
		{"diagnostic_error_field", "shapes.go", "if !c.consumption(el) {", "if !c.consumption(el) && !c.containsBound(el) {", "^TestAcceptedConsumptionShapes$/^diagnostic_error_field$", "TestAcceptedConsumptionShapes/diagnostic_error_field"},
		{"blank_drop", "shapes.go", "for i, e := range s.Rhs {\n\t\t\t\tif c.consumption(e) && !c.discard(s, i) {", "for i, e := range s.Rhs {\n\t\t\t\tif (c.consumption(e) || c.helperCall(e) && c.discard(s, i)) {", "^TestAcceptedConsumptionShapes$/^drop_blank$", "TestAcceptedConsumptionShapes/drop_blank"},
		{"classification_field_assignment", "shapes.go", "case *ast.SelectorExpr:\n		return false", "case *ast.SelectorExpr:\n		if types.Identical(c.errors.info.TypeOf(target),types.Typ[types.Bool]) {return true}; return c.convertedTargetSafe(scope, target.X, visited)", "^TestReworkScopedConsumption$/^classification_field$", "TestReworkScopedConsumption/classification_field"},
		{"classification_index_assignment", "shapes.go", "case *ast.IndexExpr:\n		return false", "case *ast.IndexExpr:\n		if types.Identical(c.errors.info.TypeOf(target),types.Typ[types.Bool]) {return true}; return c.convertedTargetSafe(scope, target.X, visited)", "^TestReworkScopedConsumption$/^classification_index$", "TestReworkScopedConsumption/classification_index"},
		{"diagnostic_call_retention", "shapes.go", "if !c.refusalConsumer(e) {", "if !c.refusalConsumer(e) && !diagnosticPack(e,c) {", "^TestReworkScopedConsumption$/^diagnostic_call$", "TestReworkScopedConsumption/diagnostic_call"},
		{"multi_error_blank_sibling", "shapes.go", "if !ok || isBlank(left) || c.errors.info.Defs[id] == nil {\n			safeRHS[ri] = false", "if !ok || isBlank(left) || c.errors.info.Defs[id] == nil {\n			if isBlank(left) && len(assignment.Lhs)==2 {continue};safeRHS[ri] = false", "^TestReworkScopedConsumption$/^second_error_drop$", "TestReworkScopedConsumption/second_error_drop"},
		{"switch_classification_error", "shapes.go", "if !c.tainted(e) {\n		return true", "if switchClassificationError(e,c) {return true};if !c.tainted(e) {\n		return true", "^TestReworkScopedConsumption$/^switch_case_escape$", "TestReworkScopedConsumption/switch_case_escape"},
		{"star_width_wrong_argument", "shapes.go", "!wrapped[i] || !c.propagation(arg)", "!wrapped[i] && !strings.Contains(constant.StringVal(format.Value),\"%*v\") || !c.propagation(arg)", "^TestReworkScopedConsumption$/^width_wrong_arg$", "TestReworkScopedConsumption/width_wrong_arg"},
		{"comparison_call_shortcut", "shapes.go", "return c.comparisonOperand(e.X) && c.comparisonOperand(e.Y)", "if e.Op == token.EQL {return true}; return c.comparisonOperand(e.X) && c.comparisonOperand(e.Y)", "^TestComparisonOperandsRespectClosedConsumers$/^bound_call$", "TestComparisonOperandsRespectClosedConsumers/bound_call"},
		{"parenthesized_consumer", "shapes.go", "function = paren.X", "_ = paren; break", "^TestParenthesizedKnownConsumers$/^classifier$", "TestParenthesizedKnownConsumers/classifier"},
		{"aggregate_pointer_alias", "shapes.go", "case *ast.SelectorExpr:\n\t\treturn false", "case *ast.SelectorExpr:\n\t\tif _, ok := c.errors.info.TypeOf(target.X).(*types.Pointer); ok {return c.convertedTargetSafe(scope,target.X,visited)}; return false", "^TestDerivedStorageClass$/^pointer_alias$/^direct$/^error$", "TestDerivedStorageClass/pointer_alias/direct/error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			copyScanner(t, root)
			path := filepath.Join(root, c.file)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(body), c.before) != 1 {
				t.Fatalf("mutant site count != 1: %s", c.before)
			}
			mutated := strings.Replace(string(body), c.before, c.after, 1)
			if c.name == "star_width_wrong_argument" {
				mutated = strings.Replace(mutated, "import (", "import (\n\"strings\"", 1)
			}
			if err := os.WriteFile(path, []byte(mutated), 0600); err != nil {
				t.Fatal(err)
			}
			extra := `package refusalscan
import("go/ast";"strings")
func constructorOnly(files map[string]*parsedSource,errors,errorTypes,helpers symbolSet) symbolSet {
 result:=newSymbolSet(helpers.info)
 for _,source:=range files {for _,decl:=range source.file.Decls {fn,ok:=decl.(*ast.FuncDecl);if !ok || fn.Body==nil || !helpers.has(fn.Name) {continue};name:=strings.ToLower(fn.Name.Name);if strings.Contains(name,"refusal") || strings.Contains(name,"error") || strings.Contains(name,"malformed") || strings.Contains(name,"invalid") || strings.Contains(name,"conflict") {result.objects[helpers.info.ObjectOf(fn.Name)]=true;continue};if len(fn.Body.List)==1 {if ret,ok:=fn.Body.List[0].(*ast.ReturnStmt);ok && len(ret.Results)==1 && containsRefusalExpression(ret.Results[0],errors,errorTypes,newSymbolSet(helpers.info)) {result.objects[helpers.info.ObjectOf(fn.Name)]=true}}}}
 return result
}
func diagnosticPack(call *ast.CallExpr,c *shapeChecker) bool {
 object:=calledObject(call,c.errors.info)
 return object!=nil && object.Name()=="pack" && !hasErrorType(c.errors.info.TypeOf(call))
}
func switchClassificationError(e ast.Expr,c *shapeChecker) bool {
 call,ok:=e.(*ast.CallExpr);if !ok || len(call.Args)!=2 || !classificationExpression(call.Args[1],c.errors.info) {return false}
 object:=calledObject(call,c.errors.info);if object==nil || object.Pkg()==nil || object.Pkg().Path()!="fmt" || object.Name()!="Errorf" {return false}
 for parent:=c.parents[e];parent!=nil;parent=c.parents[parent] {if _,ok:=parent.(*ast.SwitchStmt);ok {return true};if _,ok:=parent.(*ast.FuncDecl);ok {break}}
 return false
}
func fieldTarget(s *ast.AssignStmt) bool {if len(s.Lhs)!=1 {return false};_,ok:=s.Lhs[0].(*ast.SelectorExpr);return ok}
`
			if err := os.WriteFile(filepath.Join(root, "mutant_support.go"), []byte(extra), 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("go", "test", "-count=1", "-v", "-run", c.pattern, ".")
			command.Dir = root
			command.Env = append(os.Environ(), "GOWORK=off")
			output, err := command.CombinedOutput()
			exit := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					exit = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != 1 || !strings.Contains(string(output), "--- FAIL: "+c.failure) || strings.Contains(string(output), "[build failed]") {
				t.Fatalf("survivor or invalid mutant, exit=%d expected named %s\n%s", exit, c.failure, output)
			}
			t.Logf("KILLED | mutant=%s | exit=%d | test=%s", c.name, exit, c.failure)
		})
	}
}

func copyScanner(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || entry.Name() == "mutants_test.go" || entry.Name() == "guard_attack_test.go" {
			continue
		}
		body, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, entry.Name()), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module refusalscan.mutant\n\ngo 1.25.5\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func copyModule(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".temp" || entry.Name() == ".task-board" || entry.Name() == ".agents" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(destination, relative), 0700)
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, relative), body, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(fmt.Errorf("copy module: %w", err))
	}
}
