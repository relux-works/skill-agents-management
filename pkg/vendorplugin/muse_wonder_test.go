package vendorplugin_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// THE MUSE WONDER 1.0 CONTRIBUTOR ROWS (declared 2026-10-12).
//
// Two rows, declared here from the Muse Code 1.4.6 provider catalog and the
// refreshed (2026-10-12 17:29Z) catalog: muse-wonder-1.0-contributor and
// muse-wonder-full-1.0-contributor. Every launch below goes through
// vendorplugin.BuildLaunch, the production entry point, over the frozen muse
// declaration and the real muse system, so what is measured is the argv a
// binary would run.

var museWonderIDs = []vendorplugin.ModelID{
	"muse-wonder-1.0-contributor",
	"muse-wonder-full-1.0-contributor",
}

func museDeclaredRow(t *testing.T, id vendorplugin.ModelID) vendorplugin.Model {
	t.Helper()
	declaration, declared := museLaunchRegistry(t).RuntimeDeclarationOf("muse")
	if !declared {
		t.Fatal("the muse runtime is not declared after seeding")
	}
	for _, model := range declaration.Models {
		if model.ID == id {
			return model
		}
	}
	t.Fatalf("the muse runtime declares no model %q", id)
	return vendorplugin.Model{}
}

// TestAWonderLaunchCarriesTheModelIdAndEffortInArgv is the positive gate, a
// POSITION assertion: the id must follow --model and the word must follow
// --reasoning-effort, not merely appear somewhere in argv.
func TestAWonderLaunchCarriesTheModelIdAndEffortInArgv(t *testing.T) {
	registry := museLaunchRegistry(t)
	for _, id := range museWonderIDs {
		for _, word := range []string{"minimal", "low", "medium", "high", "xhigh", "max"} {
			t.Run(string(id)+"/"+word, func(t *testing.T) {
				plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, id, word), agentic.LaunchModeExec)
				if err != nil {
					t.Fatalf("BuildLaunch refused a word the row declares: %v", err)
				}
				if !museArgvPair(plan.Argv, "--model", string(id)) {
					t.Errorf("argv does not carry --model %s: %v", id, plan.Argv)
				}
				if !museArgvPair(plan.Argv, museEffortFlag, word) {
					t.Errorf("argv does not carry %s %s: %v", museEffortFlag, word, plan.Argv)
				}
			})
		}
	}
}

// TestTheMuseWonderAliasLaunchesUnderTheContributorIdentity is the alias
// contract in the shape of muse-spark's: admitted under its own spelling,
// executed as the identity, so argv never carries the bare `muse-wonder` the
// backend does not know.
func TestTheMuseWonderAliasLaunchesUnderTheContributorIdentity(t *testing.T) {
	registry := museLaunchRegistry(t)
	const alias, identity vendorplugin.ModelID = "muse-wonder", "muse-wonder-1.0-contributor"
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, alias, "max"), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch refused the muse-wonder alias: %v", err)
	}
	if !museArgvPair(plan.Argv, "--model", string(identity)) {
		t.Errorf("argv does not carry --model %s: %v", identity, plan.Argv)
	}
	if museArgvPair(plan.Argv, "--model", string(alias)) {
		t.Errorf("the alias reached argv verbatim: %v", plan.Argv)
	}
	if !museArgvPair(plan.Argv, museEffortFlag, "max") {
		t.Errorf("argv does not carry %s max: %v", museEffortFlag, plan.Argv)
	}
	if plan.ModelIdentity.Requested != string(alias) || plan.ModelIdentity.Launched != string(identity) {
		t.Errorf("model identity = %+v, want requested %s launched %s", plan.ModelIdentity, alias, identity)
	}
	if _, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, alias, "ultra"), agentic.LaunchModeExec); !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) {
		t.Errorf("`ultra` under the alias returned %v, want ErrEffortNotInVocabulary", err)
	}
}

// TestTheWonderAxisAdmitsMaxAndRefusesAWordOutsideIt is the negative gate and
// its control. `max` is inside the wonder vocabulary (refreshed catalog) and
// must launch; `ultra`, outside it, must be refused by vendor admission before
// any argv is built. The control is that the same word is refused for spark
// 1.3 too while `max` is admitted there, so the refusal is the vocabulary check
// and not a launch path that rejects everything.
func TestTheWonderAxisAdmitsMaxAndRefusesAWordOutsideIt(t *testing.T) {
	registry := museLaunchRegistry(t)
	for _, id := range museWonderIDs {
		t.Run("admitted-max/"+string(id), func(t *testing.T) {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, id, "max"), agentic.LaunchModeExec)
			if err != nil {
				t.Fatalf("`max` is in the wonder vocabulary and was refused: %v", err)
			}
			if !museArgvPair(plan.Argv, museEffortFlag, "max") {
				t.Errorf("argv does not carry max: %v", plan.Argv)
			}
		})
		for _, word := range []string{"ultra", "forged"} {
			t.Run("refused-"+word+"/"+string(id), func(t *testing.T) {
				plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, id, word), agentic.LaunchModeExec)
				if err == nil {
					t.Fatalf("`%s` was ADMITTED for %s and built %v", word, id, plan.Argv)
				}
				if !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) {
					t.Fatalf("refused with %v, which is not ErrEffortNotInVocabulary", err)
				}
			})
		}
		t.Run("no word/"+string(id), func(t *testing.T) {
			_, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, id, ""), agentic.LaunchModeExec)
			if !errors.Is(err, vendorplugin.ErrEffortMissing) {
				t.Fatalf("a required axis with no word returned %v, want ErrEffortMissing", err)
			}
		})
	}
	t.Run("control/muse-spark-1.3-contributor", func(t *testing.T) {
		if _, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, "muse-spark-1.3-contributor", "max"), agentic.LaunchModeExec); err != nil {
			t.Fatalf("`max` is in the spark 1.3 vocabulary and was refused: %v", err)
		}
		if _, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, "muse-spark-1.3-contributor", "ultra"), agentic.LaunchModeExec); !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) {
			t.Fatalf("`ultra` under spark 1.3 returned %v, want ErrEffortNotInVocabulary", err)
		}
	})
}

// TestTheWonderRowsAreDeclaredExactlyAndTheSparkRowsAreUntouched pins the
// declaration shape and the "previous settings do not change" rule.
func TestTheWonderRowsAreDeclaredExactlyAndTheSparkRowsAreUntouched(t *testing.T) {
	wantVocabulary := []string{"minimal", "low", "medium", "high", "xhigh", "max"}
	for _, id := range museWonderIDs {
		model := museDeclaredRow(t, id)
		if !reflect.DeepEqual(model.Systems, []agentic.SystemID{"muse"}) {
			t.Errorf("%s systems = %v, want [muse] only", id, model.Systems)
		}
		if model.Lifecycle != vendorplugin.LifecycleCurrent || model.SupersededBy != "" || model.AliasOf != "" {
			t.Errorf("%s lifecycle=%q supersededBy=%q aliasOf=%q, want current with neither", id, model.Lifecycle, model.SupersededBy, model.AliasOf)
		}
		if model.Recommended {
			t.Errorf("%s is Recommended; the display pick stays with muse-spark-1.3-contributor", id)
		}
		if model.Effort.Support != agentic.EffortSupportRequired || !reflect.DeepEqual(model.Effort.Vocabulary, wantVocabulary) || model.Effort.Recommended != "xhigh" {
			t.Errorf("%s effort = %+v, want required %v recommending xhigh", id, model.Effort, wantVocabulary)
		}
		if model.ContextWindowTokens != 1_007_997 {
			t.Errorf("%s window = %d, want 1007997", id, model.ContextWindowTokens)
		}
		if model.Rank.Score != 34 {
			t.Errorf("%s scores %d, want the interpolated 34", id, model.Rank.Score)
		}
	}
	alias := museDeclaredRow(t, "muse-wonder")
	identity := museDeclaredRow(t, "muse-wonder-1.0-contributor")
	if alias.AliasOf != identity.ID || alias.Rank.Score != identity.Rank.Score || alias.ContextWindowTokens != identity.ContextWindowTokens ||
		alias.Recommended || alias.Lifecycle != vendorplugin.LifecycleCurrent || !reflect.DeepEqual(alias.Effort, identity.Effort) ||
		!reflect.DeepEqual(alias.Systems, identity.Systems) {
		t.Errorf("muse-wonder is not the identity's short alias: alias=%+v identity=%+v", alias, identity)
	}
	if a, b := museDeclaredRow(t, museWonderIDs[0]).Rank.Score, museDeclaredRow(t, museWonderIDs[1]).Rank.Score; a != b {
		t.Errorf("the wonder rows score %d and %d; no source separates them, so the tie is the honest value", a, b)
	}

	spark := museDeclaredRow(t, "muse-spark-1.3-contributor")
	if spark.Rank.Score != 33 || !spark.Recommended || spark.ContextWindowTokens != 1_048_576 ||
		!reflect.DeepEqual(spark.Effort.Vocabulary, []string{"high", "xhigh", "max"}) || spark.Effort.Recommended != "max" {
		t.Errorf("muse-spark-1.3-contributor moved: %+v", spark)
	}
	sparkAlias := museDeclaredRow(t, "muse-spark")
	if sparkAlias.AliasOf != "muse-spark-1.3-contributor" || sparkAlias.Rank.Score != 33 || sparkAlias.ContextWindowTokens != 1_048_576 {
		t.Errorf("muse-spark moved: %+v", sparkAlias)
	}
	legacy := museDeclaredRow(t, "muse-spark-1.2-contributor")
	if legacy.Rank.Score != 10 || legacy.Lifecycle != vendorplugin.LifecycleLegacy || legacy.Effort.Support != agentic.EffortSupportNone || legacy.ContextWindowTokens != 1_048_576 {
		t.Errorf("muse-spark-1.2-contributor moved: %+v", legacy)
	}
}

// TestTheNonContributorWonderIdsStayUnregistered keeps the scope: exactly as
// muse-spark-1.3 is unregistered today, only the contributor harness ids are,
// and only the standard row has a short alias.
func TestTheNonContributorWonderIdsStayUnregistered(t *testing.T) {
	declaration, _ := museLaunchRegistry(t).RuntimeDeclarationOf("muse")
	for _, model := range declaration.Models {
		switch model.ID {
		case "muse-wonder-1.0", "muse-wonder-full-1.0", "muse-spark-1.3":
			t.Errorf("%s is registered; only contributor harness ids are in scope", model.ID)
		}
		if model.AliasOf == "muse-wonder-full-1.0-contributor" {
			t.Errorf("%s aliases the full variant, which has no alias", model.ID)
		}
	}
}
