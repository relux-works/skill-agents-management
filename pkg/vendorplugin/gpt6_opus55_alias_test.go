package vendorplugin_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file holds the 2026-09-22 heads — gpt-6-sol, gpt-6-luna and
// claude-opus-5-5 — and their floating short spellings `sol`, `luna` and
// `opus` to the launch they promise. Like openai_astra_alias_test.go it drives
// vendorplugin.BuildLaunch, the one entry point a spawn goes through, and never
// calls reading the declaration back a proof.

// gpt6CodexAliases is every codex alias this file owns, alias to target.
// astraNonAliasProblems reads it so the "no other codex row is rewritten"
// sweep holds these two to their targets instead of refusing them.
var gpt6CodexAliases = map[vendorplugin.ModelID]vendorplugin.ModelID{
	"sol":  "gpt-6-sol",
	"luna": "gpt-6-luna",
}

// The probed vocabularies, written down from `codex debug models` (codex-cli
// 0.155.1, 2026-09-22) rather than read off the rows under test, so a word
// dropped from a declaration fails here instead of shrinking the loop.
var (
	gpt6SolProbedEfforts  = []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	gpt6LunaProbedEfforts = []string{"low", "medium", "high", "xhigh", "max"}
	opus55ProbedEfforts   = []string{"low", "medium", "high", "xhigh", "max"}
)

func TestTheGPT6SolAndLunaSpellingsLaunchAsTheirHeadsAtEveryProbedEffort(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tc := range []struct {
		alias, identity vendorplugin.ModelID
		efforts         []string
	}{
		{"sol", "gpt-6-sol", gpt6SolProbedEfforts},
		{"gpt-6-sol", "gpt-6-sol", gpt6SolProbedEfforts},
		{"luna", "gpt-6-luna", gpt6LunaProbedEfforts},
		{"gpt-6-luna", "gpt-6-luna", gpt6LunaProbedEfforts},
	} {
		t.Run(string(tc.alias), func(t *testing.T) {
			for _, problem := range codexLaunchProblems(t, registry, tc.alias, tc.identity, tc.efforts) {
				t.Error(problem)
			}
		})
	}
}

// TestTheGPT6LunaAxisRefusesUltraAndMinimal holds the narrower luna axis: the
// catalog stops it at max, so ultra is refused for both spellings, and neither
// gpt-6 row takes the legacy minimal.
func TestTheGPT6LunaAxisRefusesUltraAndMinimal(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tc := range []struct {
		model  vendorplugin.ModelID
		effort string
	}{
		{"luna", "ultra"}, {"gpt-6-luna", "ultra"},
		{"luna", "minimal"}, {"gpt-6-luna", "minimal"},
		{"sol", "minimal"}, {"gpt-6-sol", "minimal"},
		{"sol", ""}, {"luna", ""},
	} {
		if _, err := vendorplugin.BuildLaunch(context.Background(), registry, astraRequest(t, tc.model, tc.effort), agentic.LaunchModeExec); err == nil {
			t.Errorf("BuildLaunch(codex, %s, %q) was admitted; the row's declared axis does not carry that word", tc.model, tc.effort)
		}
	}
}

// opusRequest is a real claude launch with a stub `claude` on PATH.
func opusRequest(t *testing.T, model vendorplugin.ModelID, effort string) vendorplugin.SpawnRequest {
	t.Helper()
	binDir, workDir := t.TempDir(), t.TempDir()
	writeStubBinary(t, binDir, "claude")
	promptPath := filepath.Join(workDir, "assignment.md")
	if err := os.WriteFile(promptPath, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}
	return vendorplugin.SpawnRequest{
		Runtime: "claude", Model: model, Effort: effort,
		PromptPath: promptPath, Prompt: []byte("do the thing\n"),
		WorkDir: workDir, Env: []string{"PATH=" + binDir}, Home: binDir,
		Run: agentic.RunContext{
			RunID: "RUN-OPUS-ALIAS", TaskID: "TASK-260922-0PUS55",
			BoardDir: filepath.Join(workDir, ".task-board"), ContextID: "CTX-OPUS-ALIAS",
		},
	}
}

func opusLaunchProblems(t *testing.T, registry *vendorplugin.Registry, requested, wantLaunched vendorplugin.ModelID) []string {
	t.Helper()
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeDryRun, agentic.LaunchModeExec} {
		for _, effort := range opus55ProbedEfforts {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, opusRequest(t, requested, effort), mode)
			if err != nil {
				report("BuildLaunch(claude, %s, %s, %s) refused the launch: %v", requested, effort, mode, err)
				continue
			}
			if plan.System != "claude-code" {
				report("%s/%s: plan.System = %q, want claude-code", requested, effort, plan.System)
			}
			if !argvHasElement(plan.Argv, string(wantLaunched)) {
				report("%s/%s/%s: argv does not carry %q: %v", requested, effort, mode, wantLaunched, plan.Argv)
			}
			if requested != wantLaunched {
				// `opus` is a whole-element check: it is a substring of
				// claude-opus-5-5, so a joined-argv search would lie both ways.
				if argvHasElement(plan.Argv, string(requested)) {
					report("%s/%s/%s: the alias spelling reached argv: %v", requested, effort, mode, plan.Argv)
				}
				for _, entry := range plan.Env {
					if _, value, found := strings.Cut(entry, "="); found && value == string(requested) {
						report("%s/%s/%s: the alias spelling reached the child environment: %q", requested, effort, mode, entry)
					}
				}
			}
			want := agentic.ModelIdentity{Requested: string(requested), Launched: string(wantLaunched)}
			if plan.ModelIdentity != want {
				report("%s/%s/%s: ModelIdentity = %#v, want %#v", requested, effort, mode, plan.ModelIdentity, want)
			}
		}
	}
	return problems
}

func TestTheOpusSpellingLaunchesAsClaudeOpus55AtEveryProbedEffort(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, requested := range []vendorplugin.ModelID{"opus", "claude-opus-5-5"} {
		t.Run(string(requested), func(t *testing.T) {
			for _, problem := range opusLaunchProblems(t, registry, requested, "claude-opus-5-5") {
				t.Error(problem)
			}
		})
	}
}

// TestTheOpusAliasBindsTheDeclaredHeadNotAnotherOpusRow is the narrowing
// direction: the older opus rows keep launching under their own ids.
func TestTheOpusAliasBindsTheDeclaredHeadNotAnotherOpusRow(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, id := range []vendorplugin.ModelID{"claude-opus-5", "claude-opus-4-8"} {
		plan, err := vendorplugin.BuildLaunch(context.Background(), registry, opusRequest(t, id, "high"), agentic.LaunchModeExec)
		if err != nil {
			t.Fatalf("BuildLaunch(claude, %s): %v", id, err)
		}
		if plan.ModelIdentity.IsAlias() || !argvHasElement(plan.Argv, string(id)) {
			t.Errorf("%q launched as %q (argv %v); a pinned opus row must not be rewritten", id, plan.ModelIdentity.Launched, plan.Argv)
		}
	}
}
