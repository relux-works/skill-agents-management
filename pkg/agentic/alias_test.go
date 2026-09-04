package agentic

import "testing"

// THE ALIAS SUBSTITUTION, MEASURED AT BuildPlan.
//
// The failure this closes was observed live, not reasoned about: a spawn of
// muse `--model muse-spark --reasoning-effort high` put the alias into argv
// verbatim and the backend answered "model muse-spark does not exist or you
// lack access", while the same launch spelled muse-spark-1.3-contributor ran
// end to end. The alias is a name operators and configuration use and the
// provider does not have.
//
// BuildPlan is the production call site and every test here drives it. The
// vendor half — which rows may declare an alias at all — is
// pkg/vendorplugin/alias_test.go; this file is only about what a harness is
// handed once a request carries one.

const (
	aliasSpelling = "pangolin-latest"
	aliasIdentity = "pangolin-large"
)

func aliasRequest() LaunchRequest {
	req := pangolinRequest()
	req.Model = Model{ID: aliasSpelling, Effort: EffortSupportRequired, AliasOf: aliasIdentity}
	return req
}

// TestBuildPlanHandsEveryPluginSurfaceTheAliasIdentity is the positive gate,
// and it asserts on ALL FOUR dispatched surfaces rather than on argv alone.
//
// Argv is the one an operator sees fail, which is exactly why it is the one a
// half-applied fix would cover: a binary lookup, a child environment or a
// stdin payload built from the alias would still be a launch describing a
// model the provider does not have, and nothing downstream would say so.
func TestBuildPlanHandsEveryPluginSurfaceTheAliasIdentity(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)

	plan, err := BuildPlan(registry, aliasRequest(), LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if !argvPair(plan.Argv, "--model", aliasIdentity) {
		t.Errorf("argv names %v, and the identity %q is what the provider answers to", plan.Argv, aliasIdentity)
	}
	for _, argument := range plan.Argv {
		if argument == aliasSpelling {
			t.Errorf("the alias spelling %q survived into argv: %v", aliasSpelling, plan.Argv)
		}
	}
	for _, surface := range []string{"ResolveBinary", "Argv", "ChildEnv", "Stdin"} {
		seen, dispatched := sys.seenModel[surface]
		if !dispatched {
			t.Errorf("%s was never dispatched, so this test proves nothing about it", surface)
			continue
		}
		if seen.ID != aliasIdentity {
			t.Errorf("%s was handed model %q, want the identity %q", surface, seen.ID, aliasIdentity)
		}
		// Cleared, so no plugin can resolve a second hop of its own. A
		// surface still holding AliasOf is a surface that could substitute
		// again, and "one hop" would then be a property of the data rather
		// than of this code.
		if seen.AliasOf != "" {
			t.Errorf("%s was handed AliasOf %q; the substitution must be spent by the time a plugin sees it", surface, seen.AliasOf)
		}
	}
}

// TestThePlanKeepsTheRequestedSpelling is the audit half.
//
// A substitution that overwrote the request would make an alias launch
// indistinguishable from a launch of the identity, and every record built off
// the plan — the board's run notes, a cost attribution, an operator asking
// "what did I actually ask for" — would answer the wrong question.
func TestThePlanKeepsTheRequestedSpelling(t *testing.T) {
	registry := registerPangolin(t, newPangolin())

	plan, err := BuildPlan(registry, aliasRequest(), LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.ModelIdentity.Requested != aliasSpelling {
		t.Errorf("ModelIdentity.Requested = %q, want the spelling the caller used, %q", plan.ModelIdentity.Requested, aliasSpelling)
	}
	if plan.ModelIdentity.Launched != aliasIdentity {
		t.Errorf("ModelIdentity.Launched = %q, want the identity argv carries, %q", plan.ModelIdentity.Launched, aliasIdentity)
	}
	if !plan.ModelIdentity.IsAlias() {
		t.Error("IsAlias() is false for a plan whose two spellings differ")
	}
}

// TestBuildPlanRewritesNothingWithoutADeclaredAlias narrows the gate.
//
// The two tests above are equally consistent with a BuildPlan that rewrote the
// model on every launch, or that stamped ModelIdentity from a rule of its own.
// A request with no AliasOf must come back untouched, and IsAlias must say so.
func TestBuildPlanRewritesNothingWithoutADeclaredAlias(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)

	plan, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if !argvPair(plan.Argv, "--model", aliasIdentity) {
		t.Errorf("argv = %v, want the requested model %q unchanged", plan.Argv, aliasIdentity)
	}
	want := ModelIdentity{Requested: aliasIdentity, Launched: aliasIdentity}
	if plan.ModelIdentity != want {
		t.Errorf("ModelIdentity = %#v, want %#v", plan.ModelIdentity, want)
	}
	if plan.ModelIdentity.IsAlias() {
		t.Error("IsAlias() is true for a request that declared no alias")
	}
	if seen := sys.seenModel["Argv"]; seen.ID != aliasIdentity {
		t.Errorf("Argv was handed %q for a request that named %q", seen.ID, aliasIdentity)
	}
}

// TestBuildPlanNeverDerivesAnAliasFromASpelling is the refusal-shaped half of
// the same narrowing, aimed at the class the vendor layer most plausibly grows
// into: a "-latest" suffix rule, a shared-prefix rule, a longest-match rule.
//
// Nothing here holds a catalogue, so nothing here may resolve a name. A
// request naming a model no declaration mentions must launch under exactly the
// string it carried, however alias-shaped that string looks.
func TestBuildPlanNeverDerivesAnAliasFromASpelling(t *testing.T) {
	registry := registerPangolin(t, newPangolin())

	for _, spelling := range []string{"pangolin-latest", "pangolin", "pangolin-large-20260904", "PANGOLIN-LARGE"} {
		req := pangolinRequest()
		req.Model = Model{ID: spelling, Effort: EffortSupportRequired}

		plan, err := BuildPlan(registry, req, LaunchModeExec)
		if err != nil {
			t.Fatalf("BuildPlan(%q): %v", spelling, err)
		}
		if !argvPair(plan.Argv, "--model", spelling) {
			t.Errorf("BuildPlan(%q) put %v in argv; an undeclared spelling must reach the harness untouched", spelling, plan.Argv)
		}
		if plan.ModelIdentity.IsAlias() {
			t.Errorf("BuildPlan(%q) reported an alias resolution nobody declared", spelling)
		}
	}
}

// TestTheSubstitutionSurvivesIntoAMultiNodePlan closes the second consumer.
//
// BuildMultiNodePlan carries the primary process into a node graph, and a
// consumer reading the launch off that node rather than off the plan must see
// the same identity. A field the graph dropped would be an audit trail that
// disagreed with itself depending on which shape somebody read.
func TestTheSubstitutionSurvivesIntoAMultiNodePlan(t *testing.T) {
	registry := registerPangolin(t, newPangolin())
	primary, err := BuildPlan(registry, aliasRequest(), LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	plan, err := BuildMultiNodePlan(primary, nil)
	if err != nil {
		t.Fatalf("BuildMultiNodePlan: %v", err)
	}
	if plan.ModelIdentity != primary.ModelIdentity {
		t.Errorf("multi-node plan reports %#v, want the primary's %#v", plan.ModelIdentity, primary.ModelIdentity)
	}
	for _, node := range plan.Nodes {
		if node.ID != PrimaryPlanNodeID {
			continue
		}
		if !argvPair(node.Process.Argv, "--model", aliasIdentity) {
			t.Errorf("the primary node's argv is %v, want the identity %q", node.Process.Argv, aliasIdentity)
		}
	}
}

// TestLaunchIdentityResolvesExactlyOneHop pins the helper the substitution is
// built on, including the whitespace case a hand-written declaration produces.
func TestLaunchIdentityResolvesExactlyOneHop(t *testing.T) {
	for _, test := range []struct {
		name  string
		model Model
		want  string
	}{
		{name: "no alias", model: Model{ID: "a"}, want: "a"},
		{name: "an alias", model: Model{ID: "a", AliasOf: "b"}, want: "b"},
		{name: "a blank alias is not an alias", model: Model{ID: "a", AliasOf: "   "}, want: "a"},
		{name: "both spellings are trimmed", model: Model{ID: " a ", AliasOf: " b "}, want: "b"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.model.LaunchIdentity(); got != test.want {
				t.Errorf("LaunchIdentity() = %q, want %q", got, test.want)
			}
		})
	}
}

// argvPair reports whether value appears as the argument immediately after
// flag. It is a POSITION assertion: a model id that landed as the argument to
// some other flag is a different launch, and containment could not tell.
func argvPair(argv []string, flag, value string) bool {
	for i := 1; i < len(argv); i++ {
		if argv[i-1] == flag && argv[i] == value {
			return true
		}
	}
	return false
}
