package vendorplugin_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// The muse HARNESS. This file is about the two halves of the effort
	// contract meeting, so it needs the REAL plugin rather than a double: the
	// vocabulary is the vendor layer's and the flag that carries a word out of
	// it is the harness plugin's, and a test holding only one of them cannot
	// see a word admitted here and dropped there.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
)

// THE END-TO-END GATE FOR muse-spark-1.3-contributor'S EFFORT AXIS.
//
// muse is the runtime with NO vendor plugin, and until 1.3 it was also the
// runtime with no effort axis, so the two absences covered for each other. They
// no longer do. The declaration in pkg/vendorplugin/runtime.go now states a
// required axis over high/xhigh/max, the plugin in pkg/agentic/systems/muse now
// spells `--reasoning-effort`, and NOTHING between them is a vendor plugin.
//
// Every assertion below runs through vendorplugin.BuildLaunch — the production
// entry point — against a registry holding the real frozen declarations and the
// real muse system, so what is measured is what a binary would launch.

const museEffortFlag = "--reasoning-effort"

// museLaunchRegistry is the production shape: the frozen runtime declarations
// seeded into a registry over agentic.Default, which holds the real muse plugin
// because this file imports it.
func museLaunchRegistry(t *testing.T) *vendorplugin.Registry {
	t.Helper()
	registry := vendorplugin.NewRegistry(agentic.Default)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("SeedFrozenRuntimes: %v", err)
	}
	return registry
}

// museRequest is a real muse launch with a stub `muse` on its PATH, so binary
// resolution succeeds for a reason that is not the operator's machine.
func museRequest(t *testing.T, model vendorplugin.ModelID, effort string) vendorplugin.SpawnRequest {
	t.Helper()
	binDir, workDir := t.TempDir(), t.TempDir()
	paritycase.WriteStubExecutable(t, binDir, "muse")
	return vendorplugin.SpawnRequest{
		Runtime:    "muse",
		Model:      model,
		Effort:     effort,
		PromptPath: paritycase.WritePromptFile(t, workDir, "assignment"),
		WorkDir:    workDir,
		Env:        []string{"PATH=" + binDir},
		Run:        agentic.RunContext{RunID: "RUN-MUSE", TaskID: "TASK-MUSE"},
	}
}

func museArgvPair(argv []string, flag, value string) bool {
	for i := 1; i < len(argv); i++ {
		if argv[i-1] == flag && argv[i] == value {
			return true
		}
	}
	return false
}

// TestTheMuseEffortWordSurvivesTheWholeLaunchPath is the positive gate, and it
// is a POSITION assertion rather than a containment one: an effort word that
// landed as the argument to some other flag is a different launch.
func TestTheMuseEffortWordSurvivesTheWholeLaunchPath(t *testing.T) {
	registry := museLaunchRegistry(t)
	// Every word muse-spark-1.3-contributor declares, through both of its
	// names. `max` is included deliberately: the model has it, installed muse
	// 1.0.2's --reasoning-effort help does not, and this module's job is to
	// transport it and let the harness refuse. Narrowing the vocabulary to the
	// installed build would put a CLI version number in the model layer and
	// would keep refusing after muse ships the word.
	//
	// The two names do NOT put the same string in argv, and that asymmetry is
	// the alias contract rather than an inconsistency: `muse-spark` is admitted
	// under its own spelling and executed as museSparkIdentity, so the second
	// column below is what the harness is handed. See alias_test.go.
	for _, model := range []struct {
		requested vendorplugin.ModelID
		launched  vendorplugin.ModelID
	}{
		{requested: museSparkIdentity, launched: museSparkIdentity},
		{requested: museSparkAlias, launched: museSparkIdentity},
	} {
		for _, word := range []string{"high", "xhigh", "max"} {
			t.Run(string(model.requested)+"/"+word, func(t *testing.T) {
				plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, model.requested, word), agentic.LaunchModeExec)
				if err != nil {
					t.Fatalf("BuildLaunch refused a word the model declares: %v", err)
				}
				if !museArgvPair(plan.Argv, museEffortFlag, word) {
					t.Fatalf("the configured effort did not reach argv as a %s pair: %v", museEffortFlag, plan.Argv)
				}
				if !museArgvPair(plan.Argv, "--model", string(model.launched)) {
					t.Errorf("argv names a model other than the launch identity %q: %v", model.launched, plan.Argv)
				}
			})
		}
	}
}

// TestTheMuseEffortGateRefusesWhatItMustReject is the negative half.
//
// Each case is a word or an absence the gate must NOT admit, and each names the
// production error. Without these, the positive test above would be equally
// satisfied by a launch path that passed anything through.
func TestTheMuseEffortGateRefusesWhatItMustReject(t *testing.T) {
	registry := museLaunchRegistry(t)
	tests := []struct {
		name    string
		model   vendorplugin.ModelID
		effort  string
		wantErr error
	}{
		{
			name:    "no word at all under a required axis",
			model:   "muse-spark-1.3-contributor",
			effort:  "",
			wantErr: vendorplugin.ErrEffortMissing,
		},
		{
			name:   "a word from a neighbouring runtime's vocabulary",
			model:  "muse-spark-1.3-contributor",
			effort: "medium",
			// `medium` is legal for codex, for claude and in installed muse
			// 1.0.2's own flag help, and it is NOT in this model's vocabulary.
			// A gate that read the CLI's set, or any other model's, admits it.
			wantErr: vendorplugin.ErrEffortNotInVocabulary,
		},
		{
			name:    "the recommended word of another vendor's row",
			model:   "muse-spark",
			effort:  "low",
			wantErr: vendorplugin.ErrEffortNotInVocabulary,
		},
		{
			name:    "a word under the legacy row, which declares no axis",
			model:   "muse-spark-1.2-contributor",
			effort:  "high",
			wantErr: vendorplugin.ErrEffortNotInVocabulary,
		},
		{
			name:    "a case-folded spelling of a legal word",
			model:   "muse-spark-1.3-contributor",
			effort:  "High",
			wantErr: vendorplugin.ErrEffortNotInVocabulary,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, tt.model, tt.effort), agentic.LaunchModeExec)
			if err == nil {
				t.Fatalf("the launch was ADMITTED and built %v; a gate that answers with a plan here is one that refuses nothing", plan.Argv)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("refused with %v, which is not %v; the two refusals have two different fixes", err, tt.wantErr)
			}
		})
	}
}

// TestTheLegacyMuseRowLaunchesWithoutAnEffortFlag is the legacy row's
// reachability half, and the narrower bound on the argv conditional: 1.2
// declares no axis, so its launch must carry no effort flag at all rather than
// one naming an empty value.
func TestTheLegacyMuseRowLaunchesWithoutAnEffortFlag(t *testing.T) {
	registry := museLaunchRegistry(t)
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, museRequest(t, "muse-spark-1.2-contributor", ""), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("the legacy muse row could not launch: %v", err)
	}
	for _, arg := range plan.Argv {
		if arg == museEffortFlag {
			t.Fatalf("an effort-none model emitted %s: %v", museEffortFlag, plan.Argv)
		}
	}
	if !museArgvPair(plan.Argv, "--model", "muse-spark-1.2-contributor") {
		t.Errorf("the legacy model did not reach argv: %v", plan.Argv)
	}
}

// TestTheMuseLegacyRowStillResolvesRatherThanBeingRefused holds the point of
// demoting 1.2 instead of deleting it: a run pinned to the old id keeps
// launching, with the supersession as the deprecation signal.
func TestTheMuseLegacyRowStillResolvesRatherThanBeingRefused(t *testing.T) {
	declaration, declared := museLaunchRegistry(t).RuntimeDeclarationOf("muse")
	if !declared {
		t.Fatal("the muse runtime is not declared after seeding")
	}
	byID := map[vendorplugin.ModelID]vendorplugin.Model{}
	for _, model := range declaration.Models {
		byID[model.ID] = model
	}
	legacy, held := byID["muse-spark-1.2-contributor"]
	if !held {
		t.Fatalf("muse-spark-1.2-contributor was dropped rather than demoted; a run pinned to it would get a hard refusal with nothing naming the successor. rows=%v", declaration.Models)
	}
	if legacy.Lifecycle != vendorplugin.LifecycleLegacy {
		t.Errorf("muse-spark-1.2-contributor is %q, want legacy", legacy.Lifecycle)
	}
	if legacy.SupersededBy != "muse-spark-1.3-contributor" {
		t.Errorf("muse-spark-1.2-contributor names successor %q, want muse-spark-1.3-contributor", legacy.SupersededBy)
	}
	if legacy.Recommended {
		t.Error("a legacy row is still recommended for display")
	}

	// The alias points at the CURRENT identity, not at the row it was ported
	// with. This is the fact a repointed alias exists to state, and the one a
	// reader cannot get from the id alone.
	alias, held := byID["muse-spark"]
	if !held {
		t.Fatal("the muse-spark alias was dropped")
	}
	if !strings.Contains(string(alias.Description), "muse-spark-1.3-contributor") {
		t.Errorf("the alias's description does not name the identity it resolves to: %q", alias.Description)
	}
	current := byID["muse-spark-1.3-contributor"]
	if alias.Effort.Support != current.Effort.Support ||
		!equalStrings(alias.Effort.Vocabulary, current.Effort.Vocabulary) ||
		alias.Effort.Recommended != current.Effort.Recommended {
		t.Errorf("the alias's effort axis (%v) differs from the identity it names (%v); one model reached by two names must cost the same either way",
			alias.Effort, current.Effort)
	}
}

// TestNoVersionedShortMuseIDIsDeclared is the naming bound this release
// carries.
//
// `muse-spark-1.3` — the version without `-contributor` — is a spelling nobody
// ships, and a row or an alias carrying it would resolve for callers who then
// depend on an id that does not exist upstream. The alias is `muse-spark`, the
// identity is `muse-spark-1.3-contributor`, and there is deliberately nothing
// in between.
func TestNoVersionedShortMuseIDIsDeclared(t *testing.T) {
	declaration, declared := museLaunchRegistry(t).RuntimeDeclarationOf("muse")
	if !declared {
		t.Fatal("the muse runtime is not declared after seeding")
	}
	seen := 0
	for _, model := range declaration.Models {
		id := string(model.ID)
		if id == "muse-spark-1.3" || id == "muse-spark-1.2" {
			t.Errorf("the declaration carries the versioned-short id %q, which upstream does not ship", id)
		}
		if strings.HasPrefix(id, "muse-spark-1.") {
			seen++
			if !strings.HasSuffix(id, "-contributor") {
				t.Errorf("versioned muse id %q does not end in -contributor; every versioned row upstream ships does", id)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no versioned muse row was found at all, so this bound ranged over nothing")
	}
}
