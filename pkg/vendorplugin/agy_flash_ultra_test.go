package vendorplugin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file holds the 2026-09-23 changes to what BuildLaunch admits: the agy
// lineup is Flash-only with gemini-3.8-flash-high at its head and
// `gemini-flash` floating over it, and the `ultra` effort word is refused by
// every row of every runtime.

func agyRequest(t *testing.T, model vendorplugin.ModelID) vendorplugin.SpawnRequest {
	t.Helper()
	req := astraRequest(t, model, "")
	binDir := t.TempDir()
	writeStubBinary(t, binDir, "agy")
	req.Runtime = "agy"
	req.Env = []string{"PATH=" + binDir}
	return req
}

func TestTheGeminiFlashSpellingLaunchesTheAgyFlashHead(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tc := range []struct{ requested, launched vendorplugin.ModelID }{
		{"gemini-flash", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash-high", "gemini-3.8-flash-high"},
		{"gemini-3.7-flash-high", "gemini-3.7-flash-high"},
		{"gemini-3.6-flash-high", "gemini-3.6-flash-high"},
	} {
		// Dry run only: an exec plan needs the Antigravity preflight to have
		// resolved the runtime, which is the launcher's job and not this file's.
		for _, mode := range []agentic.LaunchMode{agentic.LaunchModeDryRun} {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, agyRequest(t, tc.requested), mode)
			if err != nil {
				t.Errorf("BuildLaunch(agy, %s, %s): %v", tc.requested, mode, err)
				continue
			}
			if !argvHasElement(plan.Argv, string(tc.launched)) {
				t.Errorf("BuildLaunch(agy, %s, %s): argv %v does not carry %q", tc.requested, mode, plan.Argv, tc.launched)
			}
			if tc.requested != tc.launched && argvHasElement(plan.Argv, string(tc.requested)) {
				t.Errorf("BuildLaunch(agy, %s, %s): the alias spelling reached argv: %v", tc.requested, mode, plan.Argv)
			}
			want := agentic.ModelIdentity{Requested: string(tc.requested), Launched: string(tc.launched)}
			if plan.ModelIdentity != want {
				t.Errorf("BuildLaunch(agy, %s, %s): ModelIdentity = %#v, want %#v", tc.requested, mode, plan.ModelIdentity, want)
			}
		}
	}
}

// TestTheAgyLineupIsFlashOnly holds the retirement: the runtime indexes no
// Pro row, and a launch of a retired id is refused as an unknown model.
func TestTheAgyLineupIsFlashOnly(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	resolved, err := registry.ResolveRuntime("agy")
	if err != nil {
		t.Fatalf("ResolveRuntime(agy): %v", err)
	}
	for id := range vendorplugin.RuntimeModels(resolved) {
		if id != "gemini-flash" && !containsWord(string(id), "flash") {
			t.Errorf("the agy runtime indexes %q, which is not a Flash row", id)
		}
	}
	for id := range retiredHereRows {
		if _, err := vendorplugin.BuildLaunch(context.Background(), registry, agyRequest(t, id), agentic.LaunchModeDryRun); !errors.Is(err, vendorplugin.ErrUnknownModel) {
			t.Errorf("BuildLaunch(agy, retired %s) = %v, want ErrUnknownModel", id, err)
		}
	}
}

func containsWord(id, word string) bool {
	for i := 0; i+len(word) <= len(id); i++ {
		if id[i:i+len(word)] == word {
			return true
		}
	}
	return false
}

// TestTheRetiredUltraIsRefusedEverywhere sweeps every row of every runtime:
// none declares a retired effort word, the admission scale does not place one,
// and a launch naming one is refused before any argv exists.
func TestTheRetiredUltraIsRefusedEverywhere(t *testing.T) {
	for _, word := range retiredEffortWords {
		if _, placed := vendorplugin.EffortRank(word); placed {
			t.Errorf("the admission scale still places the retired word %q", word)
		}
	}
	for homeName, home := range benchScope(t) {
		for _, model := range home {
			for _, word := range retiredEffortWords {
				if model.Effort.Accepts(word) {
					t.Errorf("%s row %q accepts the retired effort word %q", homeName, model.ID, word)
				}
			}
		}
	}
	registry := isolatedRegistry(t, nil)
	for _, id := range []vendorplugin.ModelID{"gpt-6-astra", "astra", "gpt-6-sol", "sol", "gpt-5.6-sol", "gpt-5.6-terra"} {
		plan, err := vendorplugin.BuildLaunch(context.Background(), registry, astraRequest(t, id, "ultra"), agentic.LaunchModeExec)
		if !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) || len(plan.Argv) != 0 {
			t.Errorf("BuildLaunch(codex, %s, ultra) = argv %v, err %v; want ErrEffortNotInVocabulary", id, plan.Argv, err)
		}
	}
}
