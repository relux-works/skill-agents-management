package runtimecatalog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	agySystem "github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	claudeSystem "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	codexSystem "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	geminiSystem "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
	museSystem "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	pinativeSystem "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
	"github.com/relux-works/skill-agents-management/pkg/runtimecatalog"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/anthropic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/openai"
)

// validCatalog adds one OpenAI model with an alias, extends a module model with
// a scale word, and places a new scale word after max.
const validCatalog = `schema_version = 1

[[effort_scale]]
word  = "deepest"
after = "max"

[vendors.openai.models."gpt-6-9-test"]
description = "Operator test model"
systems     = ["codex"]
aliases     = ["t69"]
effort      = { support = "required", accepts = ["low", "high", "deepest"], recommended = "low" }

[vendors.openai.models."gpt-6-astra"]
effort = { accepts = ["minimal"] }
`

func catalogRegistry(t *testing.T, catalog runtimecatalog.ModelCatalog) *vendorplugin.Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	// The launcher's own system set (curator-agent-launcher internal/defaults).
	for _, system := range []agentic.System{claudeSystem.New(), codexSystem.New(), pinativeSystem.New(), geminiSystem.New(), agySystem.New(), museSystem.New()} {
		if err := systems.Register(system); err != nil {
			t.Fatalf("registering system: %v", err)
		}
	}
	registry := vendorplugin.NewRegistry(systems)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding runtimes: %v", err)
	}
	vendors, err := catalog.ApplyAll([]vendorplugin.Vendor{openai.New(), anthropic.New()})
	if err != nil {
		t.Fatalf("applying catalog: %v", err)
	}
	for _, vendor := range vendors {
		if err := registry.Register(vendor); err != nil {
			t.Fatalf("registering %s: %v", vendor.ID(), err)
		}
	}
	return registry
}

func parse(t *testing.T, text string) runtimecatalog.ModelCatalog {
	t.Helper()
	catalog, err := runtimecatalog.ParseModelCatalog([]byte(text))
	if err != nil {
		t.Fatalf("parsing catalog: %v", err)
	}
	return catalog
}

func codexLaunch(t *testing.T, registry *vendorplugin.Registry, model vendorplugin.ModelID, effort string) (agentic.Plan, error) {
	t.Helper()
	binDir, workDir := t.TempDir(), t.TempDir()
	stub := filepath.Join(binDir, "codex")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	promptPath := filepath.Join(workDir, "assignment.md")
	if err := os.WriteFile(promptPath, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}
	return vendorplugin.BuildLaunch(context.Background(), registry, vendorplugin.SpawnRequest{
		Runtime: "codex", Model: model, Effort: effort,
		PromptPath: promptPath, Prompt: []byte("do the thing\n"),
		WorkDir: workDir, Env: []string{"PATH=" + binDir},
		Run: agentic.RunContext{RunID: "RUN-CATALOG", TaskID: "TASK-261029-CATLG1", BoardDir: filepath.Join(workDir, ".task-board"), ContextID: "CTX-CATALOG"},
	}, agentic.LaunchModeDryRun)
}

func argvPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

func refusalKind(err error) runtimecatalog.RefusalKind {
	var refusal *runtimecatalog.Refusal
	if errors.As(err, &refusal) {
		return refusal.Kind
	}
	return ""
}

// A model and effort word added only in catalog.toml launch through the real
// codex argv path, including through the catalog alias; a module model and its
// module alias gain a scale word together. Limit: dry-run plans with a stub binary, not a live
// vendor call.
func TestModelCatalogAdditionsLaunchThroughTheRealArgv(t *testing.T) {
	registry := catalogRegistry(t, parse(t, validCatalog))
	cases := []struct {
		model, launched vendorplugin.ModelID
		effort          string
	}{
		{"gpt-6-9-test", "gpt-6-9-test", "high"},
		{"gpt-6-9-test", "gpt-6-9-test", "deepest"},
		{"t69", "gpt-6-9-test", "low"},
		{"gpt-6-astra", "gpt-6-astra", "minimal"},
		{"astra", "gpt-6-astra", "minimal"},
	}
	for _, c := range cases {
		plan, err := codexLaunch(t, registry, c.model, c.effort)
		if err != nil {
			t.Fatalf("launch %s/%s refused: %v", c.model, c.effort, err)
		}
		if !argvPair(plan.Argv, "-m", string(c.launched)) {
			t.Fatalf("launch %s/%s argv %q does not carry -m %s", c.model, c.effort, plan.Argv, c.launched)
		}
		if !argvPair(plan.Argv, "-c", `model_reasoning_effort="`+c.effort+`"`) {
			t.Fatalf("launch %s/%s argv %q does not carry the effort", c.model, c.effort, plan.Argv)
		}
	}
}

// Without the catalog the same model and the same added word are refused, so
// the positive cases above prove the catalog, not a permissive registry.
func TestModelCatalogAbsenceKeepsTodaysRefusals(t *testing.T) {
	registry := catalogRegistry(t, runtimecatalog.ModelCatalog{})
	if _, err := codexLaunch(t, registry, "gpt-6-9-test", "high"); !errors.Is(err, vendorplugin.ErrUnknownModel) {
		t.Fatalf("uncatalogued model: err = %v, want ErrUnknownModel", err)
	}
	if _, err := codexLaunch(t, registry, "gpt-6-astra", "minimal"); err == nil {
		t.Fatal("module model accepted an effort word only the catalog declares")
	}
}

// A typo is refused, never mapped to a neighbour: a misspelt model id, an
// effort word outside the row's vocabulary, and a misspelt word in the file.
func TestModelCatalogRefusesTypos(t *testing.T) {
	registry := catalogRegistry(t, parse(t, validCatalog))
	if _, err := codexLaunch(t, registry, "gpt-6-9-tset", "high"); !errors.Is(err, vendorplugin.ErrUnknownModel) {
		t.Fatalf("misspelt model: err = %v, want ErrUnknownModel", err)
	}
	if _, err := codexLaunch(t, registry, "gpt-6-9-test", "medium"); err == nil {
		t.Fatal("an effort word the row does not declare was launched")
	}
	misspelt := strings.Replace(validCatalog, `accepts = ["low", "high", "deepest"]`, `accepts = ["low", "hgih"]`, 1)
	_, err := parse(t, misspelt).ApplyAll([]vendorplugin.Vendor{openai.New(), anthropic.New()})
	if refusalKind(err) != runtimecatalog.RefusalUnsupported {
		t.Fatalf("misspelt effort word: err = %v, want unsupported", err)
	}
}

// Catalog rows never enter a lineup, so an unconfigured launch picks exactly
// what it picked before; the module rows keep their order.
func TestModelCatalogRowsAreNeverAutoSelected(t *testing.T) {
	plain := vendorplugin.LineupOf(openai.New())
	applied, err := parse(t, validCatalog).Apply(openai.New())
	if err != nil {
		t.Fatalf("applying: %v", err)
	}
	withCatalog := vendorplugin.LineupOf(applied)
	if len(plain) != len(withCatalog) {
		t.Fatalf("lineup length %d with the catalog, %d without", len(withCatalog), len(plain))
	}
	for i := range plain {
		if plain[i].Model.ID != withCatalog[i].Model.ID {
			t.Fatalf("lineup position %d: %s with the catalog, %s without", i+1, withCatalog[i].Model.ID, plain[i].Model.ID)
		}
		if withCatalog[i].Model.OperatorDeclared {
			t.Fatalf("operator-declared %s is in the lineup", withCatalog[i].Model.ID)
		}
	}
	found := false
	for _, model := range applied.Models() {
		if model.ID == "gpt-6-9-test" && model.OperatorDeclared {
			found = true
		}
	}
	if !found {
		t.Fatal("the catalog row is not admitted as an operator-declared model")
	}
}

// Malformed, unsupported-version and conflicting files refuse with their typed
// kind; the valid catalog is the positive control.
func TestModelCatalogFileRefusals(t *testing.T) {
	if _, err := parse(t, validCatalog).ApplyAll([]vendorplugin.Vendor{openai.New(), anthropic.New()}); err != nil {
		t.Fatalf("valid catalog refused: %v", err)
	}
	base := "schema_version = 1\n"
	cases := []struct {
		name, text string
		want       runtimecatalog.RefusalKind
		atApply    bool
	}{
		{"not toml", "schema_version = = 1", runtimecatalog.RefusalMalformed, false},
		{"no schema version", "[vendors.openai.models.x]\nsystems = [\"codex\"]\n", runtimecatalog.RefusalMalformed, false},
		{"newer schema version", "schema_version = 2\n", runtimecatalog.RefusalUnsupported, false},
		{"unknown top key", base + "models = 1\n", runtimecatalog.RefusalMalformed, false},
		{"unknown model key", base + "[vendors.openai.models.x]\nprice = 1\n", runtimecatalog.RefusalMalformed, false},
		{"unknown effort key", base + "[vendors.openai.models.x]\neffort = { order = 1 }\n", runtimecatalog.RefusalMalformed, false},
		{"bad support", base + "[vendors.openai.models.x]\neffort = { support = \"optional\" }\n", runtimecatalog.RefusalMalformed, false},
		{"new row without systems", base + "[vendors.openai.models.x]\neffort = { support = \"none\" }\n", runtimecatalog.RefusalMalformed, true},
		{"new row without effort", base + "[vendors.openai.models.x]\nsystems = [\"codex\"]\n", runtimecatalog.RefusalMalformed, true},
		{"redefines a module row", base + "[vendors.openai.models.\"gpt-6-astra\"]\ndescription = \"other\"\n", runtimecatalog.RefusalConflicting, true},
		{"extends a row with no effort axis", base + extendNoneRow(t), runtimecatalog.RefusalConflicting, true},
		{"alias collides with a module row", base + "[vendors.openai.models.x]\nsystems = [\"codex\"]\naliases = [\"gpt-6-astra\"]\neffort = { support = \"none\" }\n", runtimecatalog.RefusalConflicting, true},
		{"unknown vendor", base + "[vendors.nobody.models.x]\nsystems = [\"codex\"]\neffort = { support = \"none\" }\n", runtimecatalog.RefusalUnsupported, true},
	}
	for _, c := range cases {
		catalog, err := runtimecatalog.ParseModelCatalog([]byte(c.text))
		if c.atApply {
			if err != nil {
				t.Fatalf("%s: parse refused early: %v", c.name, err)
			}
			_, err = catalog.ApplyAll([]vendorplugin.Vendor{openai.New(), anthropic.New()})
		}
		if got := refusalKind(err); got != c.want {
			t.Fatalf("%s: refusal %q (%v), want %q", c.name, got, err, c.want)
		}
	}
}

// extendNoneRow names an Anthropic module row without an effort axis, read
// from the module so the case does not depend on a hard-coded id.
func extendNoneRow(t *testing.T) string {
	t.Helper()
	for _, model := range anthropic.New().Models() {
		if model.Effort.Support == agentic.EffortSupportNone {
			return "[vendors.anthropic.models.\"" + string(model.ID) + "\"]\neffort = { accepts = [\"low\"] }\n"
		}
	}
	t.Skip("the anthropic vendor declares no row without an effort axis")
	return ""
}

// [[effort_scale]] places a word only after a word already on the scale, once;
// an unknown or repeated position, or an existing word, refuses the file.
func TestModelCatalogEffortScale(t *testing.T) {
	scale, err := parse(t, validCatalog).EffectiveEffortScale()
	if err != nil {
		t.Fatalf("valid scale: %v", err)
	}
	if strings.Join(scale, " ") != "minimal low medium high xhigh max deepest" {
		t.Fatalf("effective scale %v", scale)
	}
	cases := map[string]runtimecatalog.RefusalKind{
		"[[effort_scale]]\nword = \"deep\"\nafter = \"maximal\"\n":                                           runtimecatalog.RefusalMalformed,
		"[[effort_scale]]\nword = \"high\"\nafter = \"max\"\n":                                               runtimecatalog.RefusalConflicting,
		"[[effort_scale]]\nword = \"a\"\nafter = \"max\"\n[[effort_scale]]\nword = \"b\"\nafter = \"max\"\n": runtimecatalog.RefusalConflicting,
		"[[effort_scale]]\nword = \"a\"\nafter = \"b\"\n[[effort_scale]]\nword = \"b\"\nafter = \"max\"\n":   runtimecatalog.RefusalMalformed,
		"[[effort_scale]]\nword = \"a\"\n":                                                                   runtimecatalog.RefusalMalformed,
	}
	for text, want := range cases {
		_, err := runtimecatalog.ParseModelCatalog([]byte("schema_version = 1\n" + text))
		if got := refusalKind(err); got != want {
			t.Fatalf("scale %q: refusal %q (%v), want %q", text, got, err, want)
		}
	}
}

// A missing catalog is the empty catalog; a catalog path that cannot be read is
// read_failed, never absence.
func TestLoadModelCatalogAbsenceAndReadFailure(t *testing.T) {
	catalog, err := runtimecatalog.LoadModelCatalog(t.TempDir())
	if err != nil || len(catalog.Vendors) != 0 || len(catalog.EffortScale) != 0 {
		t.Fatalf("absent catalog: %+v, %v", catalog, err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, runtimecatalog.CatalogFile), []byte(validCatalog), 0o600); err != nil {
		t.Fatal(err)
	}
	if catalog, err := runtimecatalog.LoadModelCatalog(dir); err != nil || len(catalog.Vendors) != 1 {
		t.Fatalf("present catalog: %+v, %v", catalog, err)
	}
	unreadable := t.TempDir()
	if err := os.Mkdir(filepath.Join(unreadable, runtimecatalog.CatalogFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimecatalog.LoadModelCatalog(unreadable); refusalKind(err) != runtimecatalog.RefusalReadFailed {
		t.Fatalf("unreadable catalog: %v, want read_failed", err)
	}
}

// A catalog extension of a frozen-tier module row never widens the frozen v2
// policy: the admitted pair set and its digest are identical with and without
// the catalog, while the added word still launches outside that policy, and a
// catalog-only model never enters an ordered v2 ceiling. (Review finding 1.)
func TestModelCatalogNeverWidensTheFrozenV2Policy(t *testing.T) {
	extension := `schema_version = 1
[vendors.openai.models."gpt-5.6-sol"]
effort = { accepts = ["minimal"] }

[vendors.openai.models."gpt-6-9-test"]
systems = ["codex"]
effort  = { support = "required", accepts = ["low"], recommended = "low" }
`
	with := catalogRegistry(t, parse(t, extension))
	without := catalogRegistry(t, runtimecatalog.ModelCatalog{})
	for _, ceiling := range []vendorplugin.V2Ceiling{
		{Runtime: "codex", Model: "gpt-5.6-sol", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual, Effort: "low"},
		{Runtime: "codex", Model: "gpt-5.6-sol", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual, Effort: ""},
		{Runtime: "codex", Model: "gpt-5.6-sol", ModelCriterion: vendorplugin.ModelCriterionGreaterOrEqual, Effort: ""},
	} {
		got, err := vendorplugin.ExpandV2Ceiling(with, ceiling)
		if err != nil {
			t.Fatalf("%+v with catalog: %v", ceiling, err)
		}
		want, err := vendorplugin.ExpandV2Ceiling(without, ceiling)
		if err != nil {
			t.Fatalf("%+v without catalog: %v", ceiling, err)
		}
		if got.Digest() != want.Digest() || strings.Join(got.PinnedPairs(), " ") != strings.Join(want.PinnedPairs(), " ") {
			t.Fatalf("%+v: catalog changed the v2 pair set\nwith    %v\nwithout %v", ceiling, got.PinnedPairs(), want.PinnedPairs())
		}
		if got.Contains("gpt-5.6-sol", "minimal") || got.ContainsModel("gpt-6-9-test") {
			t.Fatalf("%+v: v2 set admits a catalog fact: %v", ceiling, got.PinnedPairs())
		}
	}
	plan, err := codexLaunch(t, with, "gpt-5.6-sol", "minimal")
	if err != nil {
		t.Fatalf("the added word does not launch outside frozen policy: %v", err)
	}
	if !argvPair(plan.Argv, "-c", `model_reasoning_effort="minimal"`) {
		t.Fatalf("argv %q does not carry the added word", plan.Argv)
	}
}

// Bounds over a catalog-extended scale order the added words by their declared
// position, in both directions and through a chain of added words; an unknown
// bound admits nothing, and the module-only helper is unchanged. (Review
// finding 2.)
func TestModelCatalogEffectiveScaleOrdersBounds(t *testing.T) {
	catalog := parse(t, `schema_version = 1
[[effort_scale]]
word  = "deep"
after = "high"
[[effort_scale]]
word  = "deeper"
after = "deep"

[vendors.openai.models."gpt-6-9-test"]
systems = ["codex"]
effort  = { support = "required", accepts = ["high", "deep", "deeper", "xhigh"], recommended = "high" }
`)
	applied, err := catalog.Apply(openai.New())
	if err != nil {
		t.Fatal(err)
	}
	var model vendorplugin.Model
	for _, candidate := range applied.Models() {
		if candidate.ID == "gpt-6-9-test" {
			model = candidate
		}
	}
	cases := []struct {
		bound     string
		criterion vendorplugin.EffortCriterion
		want      string
	}{
		{"xhigh", vendorplugin.EffortCriterionLessOrEqual, "high deep deeper xhigh"},
		{"deep", vendorplugin.EffortCriterionLessOrEqual, "high deep"},
		{"deep", vendorplugin.EffortCriterionGreaterOrEqual, "deep deeper xhigh"},
		{"deeper", vendorplugin.EffortCriterionLessOrEqual, "high deep deeper"},
		{"", vendorplugin.EffortCriterionLessOrEqual, "high deep deeper xhigh"},
		{"nope", vendorplugin.EffortCriterionLessOrEqual, ""},
	}
	for _, c := range cases {
		got := strings.Join(vendorplugin.EffectiveEffortsUnderBound(model, c.bound, c.criterion), " ")
		if got != c.want {
			t.Fatalf("bound %q %s: %q, want %q", c.bound, c.criterion, got, c.want)
		}
	}
	if got := vendorplugin.EffortsUnderBound(model, "deep", vendorplugin.EffortCriterionLessOrEqual); got != nil {
		t.Fatalf("the module-scale helper ordered a catalog word: %v", got)
	}
}

// Diagnostics carry only validated identifiers: a key with a line break, a
// path-like key, an invalid vendor id and an invalid model id never reach the
// refusal text; a valid id is named as the control. (Review finding 3.)
func TestModelCatalogDiagnosticsNeverEchoUntrustedText(t *testing.T) {
	const marker = "confidential-marker"
	for name, text := range map[string]string{
		"newline key":      "schema_version = 1\n\"note:\\n" + marker + "\" = 1\n",
		"path-like key":    "schema_version = 1\n\"/Users/x/" + marker + "\" = 1\n",
		"nested key":       "schema_version = 1\n[vendors.openai.models.x]\n\"" + marker + "\\nx\" = 1\n",
		"invalid vendor":   "schema_version = 1\n[vendors.\"bad\\n" + marker + "\".models.x]\nsystems = [\"codex\"]\n",
		"invalid model id": "schema_version = 1\n[vendors.openai.models.\"bad\\u001b" + marker + "\"]\nsystems = [\"codex\"]\n",
	} {
		_, err := runtimecatalog.ParseModelCatalog([]byte(text))
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if strings.Contains(err.Error(), marker) || strings.ContainsAny(err.Error(), "\n\x1b") {
			t.Fatalf("%s: diagnostic echoes untrusted text: %q", name, err.Error())
		}
	}
	_, err := runtimecatalog.ParseModelCatalog([]byte("schema_version = 1\n[vendors.openai.models.\"gpt-6-9-test\"]\nprice = 1\n"))
	if err == nil || !strings.Contains(err.Error(), "openai/gpt-6-9-test") {
		t.Fatalf("valid-id control: %v", err)
	}
}
