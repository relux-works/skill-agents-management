package vendorplugin_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	"sol":  "gpt-6.1-sol",
	"luna": "gpt-6-luna",
}

// The probed vocabularies, written down from `codex debug models` (codex-cli
// 0.155.1, 2026-09-22) rather than read off the rows under test, so a word
// dropped from a declaration fails here instead of shrinking the loop.
var (
	gpt6SolProbedEfforts  = withoutRetiredEfforts([]string{"low", "medium", "high", "xhigh", "max", "ultra"})
	gpt6LunaProbedEfforts = []string{"low", "medium", "high", "xhigh", "max"}
	opus55ProbedEfforts   = []string{"low", "medium", "high", "xhigh", "max"}
)

func TestTheGPT6SolAndLunaSpellingsLaunchAsTheirHeadsAtEveryProbedEffort(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tc := range []struct {
		alias, identity vendorplugin.ModelID
		efforts         []string
	}{
		// `sol` floats to gpt-6.1-sol since codex-cli 0.159.0 (2026-09-29);
		// gpt-6-sol keeps launching under its own id.
		{"sol", "gpt-6.1-sol", gpt6SolProbedEfforts},
		{"gpt-6.1-sol", "gpt-6.1-sol", gpt6SolProbedEfforts},
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
		{"sol", "ultra"}, {"gpt-6-sol", "ultra"},
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

// TestTheSonnetSpellingLaunchesAsClaudeSonnet55AtEveryProbedEffort holds the
// `sonnet` alias to claude-sonnet-5-5 through the same launch proof as `opus`.
// Probed vocabulary: Anthropic's card lists low..max for sonnet-5-5.
func TestTheSonnetSpellingLaunchesAsClaudeSonnet55AtEveryProbedEffort(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, requested := range []vendorplugin.ModelID{"sonnet", "claude-sonnet-5-5"} {
		t.Run(string(requested), func(t *testing.T) {
			for _, problem := range opusLaunchProblems(t, registry, requested, "claude-sonnet-5-5") {
				t.Error(problem)
			}
		})
	}
	// The narrowing direction: the previous sonnet keeps its own id.
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, opusRequest(t, "claude-sonnet-5", "high"), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(claude, claude-sonnet-5): %v", err)
	}
	if plan.ModelIdentity.IsAlias() || !argvHasElement(plan.Argv, "claude-sonnet-5") {
		t.Errorf("claude-sonnet-5 launched as %q (argv %v); a pinned sonnet row must not be rewritten", plan.ModelIdentity.Launched, plan.Argv)
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

// TestTheSolHeadRecommendsMedium pins the 2026-09-29 retarget: the floating
// `sol` executes as gpt-6.1-sol and, as an alias must, mirrors its axis —
// recommended medium — while the superseded gpt-6-sol keeps its own max.
func TestTheSolHeadRecommendsMedium(t *testing.T) {
	plugin, ok := vendorplugin.Default.Lookup("openai")
	if !ok {
		t.Fatal("openai vendor is not registered")
	}
	want := map[vendorplugin.ModelID]struct {
		aliasOf     vendorplugin.ModelID
		recommended string
	}{
		"sol":         {"gpt-6.1-sol", "medium"},
		"gpt-6.1-sol": {"", "medium"},
		"gpt-6-sol":   {"", "max"},
	}
	seen := 0
	for _, m := range plugin.Models() {
		w, named := want[m.ID]
		if !named {
			continue
		}
		seen++
		if m.AliasOf != w.aliasOf || m.Effort.Recommended != w.recommended {
			t.Errorf("%s: AliasOf=%q recommended=%q, want AliasOf=%q recommended=%q", m.ID, m.AliasOf, m.Effort.Recommended, w.aliasOf, w.recommended)
		}
	}
	if seen != len(want) {
		t.Fatalf("found %d of the %d sol rows", seen, len(want))
	}
}

// haiku55Window is the operating window the owner imposed on claude-haiku-5-5,
// written down here rather than read off the rows under test so a declaration
// that drifts fails instead of moving the expectation with it.
const haiku55Window = 100_000

const claudeCompactWindowKey = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"

// TestTheHaikuSpellingLaunchesAsClaudeHaiku55AtEveryProbedEffort holds the
// `haiku` alias to claude-haiku-5-5 through the same launch proof as `opus`.
// Probed vocabulary: Anthropic's effort page lists low..max for haiku-5-5.
func TestTheHaikuSpellingLaunchesAsClaudeHaiku55AtEveryProbedEffort(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, requested := range []vendorplugin.ModelID{"haiku", "claude-haiku-5-5"} {
		t.Run(string(requested), func(t *testing.T) {
			for _, problem := range opusLaunchProblems(t, registry, requested, "claude-haiku-5-5") {
				t.Error(problem)
			}
		})
	}
	// The narrowing direction: the previous haiku keeps its own id, and the
	// CLI's own floating `haiku` (still 4.5 on Claude Code 2.1.290) is not what
	// argv carries.
	for _, id := range []vendorplugin.ModelID{"claude-haiku-4-5", "claude-haiku-4-5-20251001"} {
		// Both previous-generation rows declare no effort axis, so none is passed.
		plan, err := vendorplugin.BuildLaunch(context.Background(), registry, opusRequest(t, id, ""), agentic.LaunchModeExec)
		if err != nil {
			t.Fatalf("BuildLaunch(claude, %s): %v", id, err)
		}
		if plan.ModelIdentity.IsAlias() || !argvHasElement(plan.Argv, string(id)) {
			t.Errorf("%q launched as %q (argv %v); a pinned haiku row must not be rewritten", id, plan.ModelIdentity.Launched, plan.Argv)
		}
	}
}

// TestTheHaikuRowsDeclareTheOwnerCapAndTheProbedAxis pins the declaration the
// launch proofs above and below stand on.
func TestTheHaikuRowsDeclareTheOwnerCapAndTheProbedAxis(t *testing.T) {
	plugin, ok := vendorplugin.Default.Lookup("anthropic")
	if !ok {
		t.Fatal("anthropic vendor is not registered")
	}
	rows := map[vendorplugin.ModelID]vendorplugin.Model{}
	for _, model := range plugin.Models() {
		rows[model.ID] = model
	}
	identity, alias := rows["claude-haiku-5-5"], rows["haiku"]
	if identity.ID == "" || alias.ID == "" {
		t.Fatalf("anthropic declares claude-haiku-5-5 = %v and haiku = %v; both rows are required", identity.ID != "", alias.ID != "")
	}
	for _, model := range []vendorplugin.Model{identity, alias} {
		if model.Lifecycle != vendorplugin.LifecycleCurrent {
			t.Errorf("%s lifecycle = %s, want current", model.ID, model.Lifecycle)
		}
		if model.SupersededBy != "" {
			t.Errorf("%s names successor %q; the head has none", model.ID, model.SupersededBy)
		}
		if model.Recommended {
			t.Errorf("%s is the vendor display pick; claude-opus-5 keeps it", model.ID)
		}
		if !slices.Equal(model.Systems, []agentic.SystemID{"claude-code"}) {
			t.Errorf("%s systems = %v, want exactly [claude-code]", model.ID, model.Systems)
		}
		if model.Effort.Support != agentic.EffortSupportRequired || model.Effort.Recommended != "medium" ||
			!slices.Equal(model.Effort.Vocabulary, []string{"low", "medium", "high", "xhigh", "max"}) {
			t.Errorf("%s effort = %+v, want required low..max recommended medium", model.ID, model.Effort)
		}
		if model.ContextWindowTokens != haiku55Window {
			t.Errorf("%s ContextWindowTokens = %d, want the owner cap %d", model.ID, model.ContextWindowTokens, haiku55Window)
		}
		if model.Rank.Score != 22 {
			t.Errorf("%s rank = %d, want 22 (interpolated between claude-opus-5 and claude-opus-4-8)", model.ID, model.Rank.Score)
		}
	}
	if identity.AliasOf != "" || alias.AliasOf != "claude-haiku-5-5" {
		t.Errorf("AliasOf: claude-haiku-5-5 = %q, haiku = %q; want empty and claude-haiku-5-5", identity.AliasOf, alias.AliasOf)
	}
	// The tie is the alias and its identity and nothing else.
	for id, model := range rows {
		if id != "claude-haiku-5-5" && id != "haiku" && model.Rank.Score == 22 {
			t.Errorf("%s also scores 22; the haiku pair must tie only itself", id)
		}
	}
}

// compactWindowOf returns the variable's entries in a plan's child environment.
func compactWindowOf(env []string) []string {
	var out []string
	for _, entry := range env {
		if key, _, _ := strings.Cut(entry, "="); key == claudeCompactWindowKey {
			out = append(out, entry)
		}
	}
	return out
}

// claudeRowEffort is an effort the row's own axis accepts.
func claudeRowEffort(model vendorplugin.Model) string {
	if model.Effort.Recommended != "" {
		return model.Effort.Recommended
	}
	if len(model.Effort.Vocabulary) > 0 {
		return model.Effort.Vocabulary[0]
	}
	return ""
}

// TestOnlyTheHaikuRowsCarryTheCompactWindowIntoTheClaudeChild drives the real
// launch entry point, vendorplugin.BuildLaunch, for EVERY row the claude-code
// harness drives. The two haiku spellings must hand the child
// CLAUDE_CODE_AUTO_COMPACT_WINDOW=100000 — the alias under its resolved
// identity — and no other row may gain the variable, so a window typed onto the
// wrong row, or an export that fired for every row, fails here by name.
func TestOnlyTheHaikuRowsCarryTheCompactWindowIntoTheClaudeChild(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	resolved, err := registry.ResolveRuntime("claude")
	if err != nil {
		t.Fatalf("resolving claude: %v", err)
	}
	rows := vendorplugin.RuntimeModels(resolved)
	if len(rows) == 0 {
		t.Fatal("the claude runtime drives no rows, so this sweep ranges over nothing")
	}
	windowed := map[vendorplugin.ModelID]bool{"claude-haiku-5-5": true, "haiku": true}
	for id := range windowed {
		if _, present := rows[id]; !present {
			t.Fatalf("the claude runtime does not drive %q", id)
		}
	}
	for id, model := range rows {
		for _, mode := range []agentic.LaunchMode{agentic.LaunchModeDryRun, agentic.LaunchModeExec} {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, opusRequest(t, id, claudeRowEffort(model)), mode)
			if err != nil {
				t.Errorf("BuildLaunch(claude, %s, %s) refused the launch: %v", id, mode, err)
				continue
			}
			got := compactWindowOf(plan.Env)
			if windowed[id] {
				if want := fmt.Sprintf("%s=%d", claudeCompactWindowKey, haiku55Window); len(got) != 1 || got[0] != want {
					t.Errorf("%s/%s: child env carries %v, want exactly [%s]", id, mode, got, want)
				}
				continue
			}
			if len(got) != 0 {
				t.Errorf("%s/%s: child env carries %v; only the haiku-5-5 rows declare a window, so no other row may gain the variable", id, mode, got)
			}
		}
	}
}
