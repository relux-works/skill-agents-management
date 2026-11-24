package vendorplugin_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

func TestNeonRowsResolveWithEffortPinnedInTheModelID(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	google, ok := vendorplugin.Default.Lookup("google")
	if !ok {
		t.Fatal("google missing")
	}
	rows := make(map[vendorplugin.ModelID]vendorplugin.Model)
	for _, row := range google.Models() {
		rows[row.ID] = row
	}

	for _, id := range []vendorplugin.ModelID{
		"gemini-4-neon-xhigh",
		"gemini-4-neon-high",
		"gemini-4-neon-medium",
		"gemini-4-neon-low",
	} {
		t.Run(string(id), func(t *testing.T) {
			row, ok := rows[id]
			if !ok {
				t.Fatalf("missing Google row %q", id)
			}
			if row.Unmeasured == nil || row.Rank != nil {
				t.Fatalf("%s must remain availability-only until measured: %+v", id, row)
			}
			if row.Effort.Support != agentic.EffortSupportNone || len(row.Effort.Vocabulary) != 0 || row.Effort.Recommended != "" {
				t.Fatalf("%s must have no separate effort axis: %+v", id, row.Effort)
			}

			request := agyRequest(t, id)
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
			if err != nil {
				t.Fatalf("BuildLaunch(agy, %s): %v", id, err)
			}
			if !argvHasElement(plan.Argv, string(id)) || argvHasElement(plan.Argv, "--reasoning-effort") {
				t.Fatalf("the exact suffix must be in argv without a separate effort flag: %v", plan.Argv)
			}
			wantIdentity := agentic.ModelIdentity{Requested: string(id), Launched: string(id)}
			if plan.ModelIdentity != wantIdentity {
				t.Fatalf("model identity = %#v, want %#v", plan.ModelIdentity, wantIdentity)
			}

			request.Effort = "high"
			plan, err = vendorplugin.BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
			if !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) || !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("separate effort = argv %v, err %v; want empty plan and ErrEffortNotInVocabulary", plan.Argv, err)
			}
		})
	}
}

func TestUnknownNeonSuffixIsRefusedWithPositiveControl(t *testing.T) {
	registry := isolatedRegistry(t, nil)

	known := agyRequest(t, "gemini-4-neon-xhigh")
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, known, agentic.LaunchModeDryRun)
	if err != nil || !argvHasElement(plan.Argv, "gemini-4-neon-xhigh") {
		t.Fatalf("known Neon id positive control: argv %v err %v", plan.Argv, err)
	}

	unknown := agyRequest(t, "gemini-4-neon-max")
	plan, err = vendorplugin.BuildLaunch(context.Background(), registry, unknown, agentic.LaunchModeDryRun)
	if !errors.Is(err, vendorplugin.ErrUnknownModel) || !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("unknown Neon suffix: argv %v err %v; want empty plan and ErrUnknownModel", plan.Argv, err)
	}
}

func TestNeonRowsAreNotResolvedOnTheGeminiRuntime(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	request := agyRequest(t, "gemini-4-neon-high")
	request.Runtime = "gemini"

	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
	if !errors.Is(err, vendorplugin.ErrModelNotDrivenBySystem) || !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("Gemini runtime: argv %v err %v; want empty plan and ErrModelNotDrivenBySystem", plan.Argv, err)
	}
}
