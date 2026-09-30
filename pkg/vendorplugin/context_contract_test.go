package vendorplugin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Discover the inventory from declarations and the production projection AST,
// rather than enumerating a second LaunchRequest field list by hand.
func TestSpawnRequestLaunchContextASTInventory(t *testing.T) {
	fs := token.NewFileSet()
	source, err := parser.ParseFile(fs, "../agentic/system.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := parser.ParseFile(fs, "plugin.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	reachable := map[string][]string{}
	ast.Inspect(projection, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "passthroughLaunchRequest" {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				return true
			}
			ast.Inspect(kv.Value, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				recv, ok := sel.X.(*ast.Ident)
				if ok && recv.Name == "req" {
					reachable[key.Name] = append(reachable[key.Name], sel.Sel.Name)
				}
				return true
			})
			return true
		})
		return false
	})
	// These are resolution-owned identities, not caller payloads. The AST still
	// discovers every field; a new unmapped field fails closed.
	owned := map[string]string{
		"System": "resolved runtime system", "SystemModelIdentity": "vendor-owned native model identity",
		"Model": "admitted vendor model", "Effort": "admitted model effort",
		"Runtime": "resolved runtime identity", "Vendor": "resolved broker identity",
	}
	count := 0
	ast.Inspect(source, func(n ast.Node) bool {
		decl, ok := n.(*ast.TypeSpec)
		if !ok || decl.Name.Name != "LaunchRequest" {
			return true
		}
		for _, field := range decl.Type.(*ast.StructType).Fields.List {
			for _, name := range field.Names {
				count++
				if inputs := reachable[name.Name]; len(inputs) > 0 {
					for _, input := range inputs {
						if _, ok := reflect.TypeOf(SpawnRequest{}).FieldByName(input); !ok {
							t.Errorf("unreachable SpawnRequest.%s", input)
						}
					}
					t.Logf("LaunchRequest.%s <- SpawnRequest.%v", name.Name, inputs)
				} else if owner, ok := owned[name.Name]; ok {
					t.Logf("LaunchRequest.%s: vendor-owned (%s)", name.Name, owner)
				} else {
					t.Errorf("LaunchRequest.%s has no SpawnRequest path or declared owner", name.Name)
				}
			}
		}
		return false
	})
	if count != reflect.TypeOf(agentic.LaunchRequest{}).NumField() {
		t.Fatal("AST inventory missed fields")
	}
	for _, name := range []string{"Context", "ContextDescriptors"} {
		a, _ := reflect.TypeOf(agentic.LaunchRequest{}).FieldByName(name)
		s, ok := reflect.TypeOf(SpawnRequest{}).FieldByName(name)
		if !ok || a.Type != s.Type {
			t.Errorf("%s carrier has a parallel or missing type", name)
		}
	}
}

func TestPassthroughNilContextPlansAcrossRegisteredPlugins(t *testing.T) {
	for _, id := range agentic.Default.IDs() {
		t.Run(string(id), func(t *testing.T) {
			req := SpawnRequest{PromptPath: "/tmp/assignment.md", Prompt: []byte("assignment"), WorkDir: "/work", Home: "/managed/home", Env: []string{"PATH=/usr/bin"}, Profile: "", ServiceTier: "", ToolRelease: "", NativeArgs: nil}
			model := Model{ID: "model"}
			// Frozen pre-carrier projection. Entire request equality proves no new
			// values reach ANY registered plugin when both context carriers are nil.
			legacy := agentic.LaunchRequest{System: id, Model: model.Launchable(), Effort: "", PromptPath: req.PromptPath, Prompt: req.Prompt, WorkDir: req.WorkDir, Home: req.Home, LocalProvider: cloneLocalProvider(req.LocalProvider), Env: req.Env, Run: req.Run, Profile: req.Profile, Goal: req.Goal, Budget: req.Budget, ServiceTier: req.ServiceTier, Composition: req.Composition, Deadline: req.Deadline, PermissionMode: req.PermissionMode, ToolRelease: req.ToolRelease, NativeArgs: append([]string(nil), req.NativeArgs...)}
			got := passthroughLaunchRequest(id, model, "", req)
			if !reflect.DeepEqual(got, legacy) {
				t.Fatalf("nil context changed %s launch input", id)
			}
			// Drive the production planner with the same request and compare both
			// plans and typed refusals for plugins whose grammar rejects this shape.
			gotPlan, gotErr := agentic.BuildPlan(agentic.Default, got, agentic.LaunchModeDryRun)
			wantPlan, wantErr := agentic.BuildPlan(agentic.Default, legacy, agentic.LaunchModeDryRun)
			if !reflect.DeepEqual(gotPlan, wantPlan) || !reflect.DeepEqual(gotErr, wantErr) {
				t.Fatalf("nil context changed %s plan/refusal", id)
			}
		})
	}
}
