package vendorplugin_test

import (
	"context"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// The muse HARNESS, for the same reason muse_effort_test.go imports it:
	// the substitution is only worth anything if the string it produces is the
	// one the real argv builder puts after `--model`.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
)

// THE MEASURED FAILURE, PINNED.
//
// skill-project-management spawned muse with `--model muse-spark
// --reasoning-effort high`. The alias reached argv verbatim and the Meta
// backend refused the run: "model muse-spark does not exist or you lack
// access". The identical launch spelled muse-spark-1.3-contributor succeeded
// end to end. Every assertion below runs through vendorplugin.BuildLaunch
// against the real frozen declarations and the real muse plugin, so what is
// measured is what a binary would launch.
const (
	museSparkAlias    vendorplugin.ModelID = "muse-spark"
	museSparkIdentity vendorplugin.ModelID = "muse-spark-1.3-contributor"
)

// TestTheMuseAliasLaunchesUnderTheContributorIdentity is the acceptance
// criterion, in both launch modes.
//
// Dry run is not a decoration here: it is the mode an operator reads before
// committing to a spawn, and a dry run that printed the alias while an exec ran
// the identity would be a preview of a different launch.
func TestTheMuseAliasLaunchesUnderTheContributorIdentity(t *testing.T) {
	registry := museLaunchRegistry(t)
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeDryRun, agentic.LaunchModeExec} {
		t.Run(mode.String(), func(t *testing.T) {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, museSparkAlias, "high"), mode)
			if err != nil {
				t.Fatalf("BuildLaunch: %v", err)
			}
			if !museArgvPair(plan.Argv, "--model", string(museSparkIdentity)) {
				t.Errorf("argv does not name the contributor identity: %v", plan.Argv)
			}
			for _, argument := range plan.Argv {
				if argument == string(museSparkAlias) {
					t.Errorf("the alias spelling the backend refuses reached argv: %v", plan.Argv)
				}
			}
			// The effort word rides on the substituted model and must still be
			// the operator's. A substitution that rebuilt the request from the
			// identity row would be free to lose it.
			if !museArgvPair(plan.Argv, museEffortFlag, "high") {
				t.Errorf("the configured effort did not survive the substitution: %v", plan.Argv)
			}
			want := agentic.ModelIdentity{Requested: string(museSparkAlias), Launched: string(museSparkIdentity)}
			if plan.ModelIdentity != want {
				t.Errorf("ModelIdentity = %#v, want %#v", plan.ModelIdentity, want)
			}
		})
	}
}

// TestTheMuseIdentityRowsAreNotRewritten narrows it to the alias.
//
// muse declares three rows and exactly ONE of them is an alias. A substitution
// that fired on the other two — because they share a prefix, a score or a
// context window — would launch a pinned request somewhere else entirely, and
// the legacy row is the one where that would be silent: an operator pinning
// muse-spark-1.2-contributor is pinning it on purpose.
func TestTheMuseIdentityRowsAreNotRewritten(t *testing.T) {
	registry := museLaunchRegistry(t)
	for _, test := range []struct {
		model  vendorplugin.ModelID
		effort string
	}{
		{model: museSparkIdentity, effort: "high"},
		{model: "muse-spark-1.2-contributor", effort: ""},
	} {
		t.Run(string(test.model), func(t *testing.T) {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, test.model, test.effort), agentic.LaunchModeExec)
			if err != nil {
				t.Fatalf("BuildLaunch: %v", err)
			}
			if !museArgvPair(plan.Argv, "--model", string(test.model)) {
				t.Errorf("argv = %v, want the requested row %q untouched", plan.Argv, test.model)
			}
			if plan.ModelIdentity.IsAlias() {
				t.Errorf("%q was reported as an alias of %q and declares none", test.model, plan.ModelIdentity.Launched)
			}
		})
	}
}

// TestTheMuseAliasStaysAdmissibleUnderItsOwnSpelling states what the
// substitution deliberately does NOT do.
//
// The alias is a real row: it is spelled in this repository's spawn ceilings,
// it is ranked, it is displayed, and the effort word a caller supplies is
// validated against ITS vocabulary. Resolving it at admission time instead of
// at launch time would silently change which configured pairs a board admits.
func TestTheMuseAliasStaysAdmissibleUnderItsOwnSpelling(t *testing.T) {
	registry := museLaunchRegistry(t)
	runtime, err := registry.ResolveRuntime("muse")
	if err == nil {
		t.Fatalf("muse resolved a vendor: %#v", runtime)
	}
	declaration, declared := registry.RuntimeDeclarationOf("muse")
	if !declared {
		t.Fatal("the frozen table declares no muse runtime")
	}
	var alias vendorplugin.Model
	for _, model := range declaration.Models {
		if model.ID == museSparkAlias {
			alias = model
		}
	}
	if alias.ID != museSparkAlias {
		t.Fatalf("the muse declaration no longer carries %q", museSparkAlias)
	}
	if alias.AliasOf != museSparkIdentity {
		t.Errorf("%q declares AliasOf %q, want %q", alias.ID, alias.AliasOf, museSparkIdentity)
	}
	if !alias.Effort.Accepts("max") {
		t.Errorf("%q no longer accepts the vocabulary it is admitted under: %v", alias.ID, alias.Effort.Vocabulary)
	}
}
