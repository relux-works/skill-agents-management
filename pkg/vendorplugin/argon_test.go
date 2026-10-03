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
	for id, score := range map[vendorplugin.ModelID]int{"gemini-4-argon-high": 45, "gemini-4-argon-medium": 42, "gemini-4-argon-low": 39, "gemini-4-argon": 45, "argon": 45} {
		row, ok := rows[id]
		if !ok {
			t.Fatalf("missing %s", id)
		}
		if row.Lifecycle != vendorplugin.LifecyclePreview || row.Recommended || row.ContextWindowTokens != 0 || row.Pricing != nil || !reflect.DeepEqual(row.Systems, []agentic.SystemID{"antigravity"}) || row.Effort.Support != agentic.EffortSupportNone || len(row.Effort.Vocabulary) != 0 || row.Effort.Recommended != "" {
			t.Errorf("%s declaration drift: %#v", id, row)
		}
		if row.Rank.Score != score || len(row.Rank.Basis) != 2 {
			t.Fatalf("%s rank = %#v", id, row.Rank)
		}
		want := vendorplugin.BughuntInterpolated("gpt-6-astra", "claude-fable-5-1", "unmeasured; operator places Argon at frontier level between Astra and Fable")
		if score != 45 {
			effort := strings.TrimPrefix(string(id), "gemini-4-argon-")
			want = vendorplugin.BughuntInterpolated("gemini-4-argon-high", "gemini-3.8-flash-high", "unmeasured; "+effort+" Argon interpolated between high Argon and measured Flash high (20)")
		}
		if row.Rank.Basis[0] != want {
			t.Errorf("%s bench claim = %#v", id, row.Rank.Basis[0])
		}
		source := row.Rank.Basis[1].Source
		for _, token := range []string{"operator declaration", "2026-10-02", "gemini-4-argon", "agy runtime", "not publicly released yet", "`agy models` was not read", "agy is not installed on the declaring machine", "2026-10-04", "(base, effort)", "they were not read from `agy models`"} {
			if !strings.Contains(source, token) {
				t.Errorf("%s evidence missing %q: %s", id, token, source)
			}
		}
	}
	head := rows["gemini-4-argon-high"]
	for _, id := range []vendorplugin.ModelID{"gemini-4-argon-high", "gemini-4-argon-medium", "gemini-4-argon-low"} {
		if rows[id].AliasOf != "" {
			t.Errorf("effort id %s is an alias", id)
		}
	}
	for _, id := range []vendorplugin.ModelID{"gemini-4-argon", "argon"} {
		alias := rows[id]
		if alias.AliasOf != head.ID || !strings.Contains(string(alias.Description), "executes as gemini-4-argon-high") || !reflect.DeepEqual(head.Rank, alias.Rank) || !reflect.DeepEqual(head.Effort, alias.Effort) || !reflect.DeepEqual(head.Systems, alias.Systems) || head.Lifecycle != alias.Lifecycle {
			t.Errorf("Argon alias %s differs from head", id)
		}
	}
	if !rows["gemini-3.6-flash-high"].Recommended {
		t.Fatal("Flash display pick moved")
	}
}

func TestArgonBuildLaunchIdentityAndEffortRefusal(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for id, launched := range map[vendorplugin.ModelID]string{
		"argon": "gemini-4-argon-high", "gemini-4-argon": "gemini-4-argon-high",
		"gemini-4-argon-high": "gemini-4-argon-high", "gemini-4-argon-medium": "gemini-4-argon-medium", "gemini-4-argon-low": "gemini-4-argon-low",
	} {
		t.Run(string(id), func(t *testing.T) {
			request := agyRequest(t, id)
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
			if err != nil {
				t.Fatalf("BuildLaunch: %v", err)
			}
			if !argvHasElement(plan.Argv, launched) || (string(id) != launched && argvHasElement(plan.Argv, string(id))) || argvHasElement(plan.Argv, "--reasoning-effort") || plan.ModelIdentity != (agentic.ModelIdentity{Requested: string(id), Launched: launched}) {
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
