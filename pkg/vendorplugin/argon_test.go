package vendorplugin_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

func TestArgonDeclarationAndOperatorEvidence(t *testing.T) {
	google, ok := vendorplugin.Default.Lookup("google")
	if !ok {
		t.Fatal("google missing")
	}
	rows := map[vendorplugin.ModelID]vendorplugin.Model{}
	for _, row := range google.Models() {
		rows[row.ID] = row
	}
	for _, id := range []vendorplugin.ModelID{"gemini-4-argon", "argon"} {
		row, ok := rows[id]
		if !ok {
			t.Fatalf("missing %s", id)
		}
		if row.Lifecycle != vendorplugin.LifecyclePreview || row.Recommended || row.ContextWindowTokens != 0 || row.Pricing != nil || !reflect.DeepEqual(row.Systems, []agentic.SystemID{"antigravity"}) || row.Effort.Support != agentic.EffortSupportNone || len(row.Effort.Vocabulary) != 0 || row.Effort.Recommended != "" {
			t.Errorf("%s declaration drift: %#v", id, row)
		}
		if row.Rank.Score != 45 || len(row.Rank.Basis) != 2 {
			t.Fatalf("%s rank = %#v", id, row.Rank)
		}
		want := vendorplugin.BughuntInterpolated("gpt-6-astra", "claude-fable-5-1", "unmeasured; operator places Argon at frontier level between Astra and Fable")
		if row.Rank.Basis[0] != want {
			t.Errorf("%s bench claim = %#v", id, row.Rank.Basis[0])
		}
		source := row.Rank.Basis[1].Source
		for _, token := range []string{"operator declaration", "2026-10-02", "gemini-4-argon", "agy runtime", "not publicly released yet", "`agy models` was not read", "agy is not installed on the declaring machine"} {
			if !strings.Contains(source, token) {
				t.Errorf("%s evidence missing %q: %s", id, token, source)
			}
		}
	}
	head, alias := rows["gemini-4-argon"], rows["argon"]
	if head.AliasOf != "" || alias.AliasOf != head.ID || !strings.Contains(string(alias.Description), "executes as gemini-4-argon") || !reflect.DeepEqual(head.Rank, alias.Rank) || !reflect.DeepEqual(head.Effort, alias.Effort) {
		t.Fatal("Argon alias differs from head")
	}
	if !rows["gemini-3.6-flash-high"].Recommended {
		t.Fatal("Flash display pick moved")
	}
}

func TestArgonBuildLaunchIdentityAndEffortRefusal(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, id := range []vendorplugin.ModelID{"argon", "gemini-4-argon"} {
		t.Run(string(id), func(t *testing.T) {
			request := agyRequest(t, id)
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
			if err != nil {
				t.Fatalf("BuildLaunch: %v", err)
			}
			if !argvHasElement(plan.Argv, "gemini-4-argon") || argvHasElement(plan.Argv, "argon") || plan.ModelIdentity != (agentic.ModelIdentity{Requested: string(id), Launched: "gemini-4-argon"}) {
				t.Fatalf("launch identity: %#v", plan)
			}
			for _, effort := range []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra", "forged"} {
				request.Effort = effort
				plan, err := vendorplugin.BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
				if !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) || len(plan.Argv) != 0 {
					t.Errorf("BuildLaunch(%s, %s) = argv %v err %v", id, effort, plan.Argv, err)
				}
			}
		})
	}
}
