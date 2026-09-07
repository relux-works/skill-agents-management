package vendorplugin_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// THE ALIAS THE VENDOR DOES NOT HAVE, held against what a binary would launch.
//
// `astra` is the spelling an operator, a spawn ceiling and a board config use
// for the current astra head. `codex debug models` publishes nine slugs and
// none of them is that word, so a launch that put `astra` on argv would ask
// OpenAI for a model it never published — the failure muse-spark was MEASURED
// making before AliasOf existed (see muse_alias_test.go).
//
// Every assertion below drives vendorplugin.BuildLaunch, the one entry point a
// spawn goes through, against the real frozen runtimes and the real codex
// system plugin. Nothing here reads the declaration back and calls that proof:
// a row can be declared, ranked, indexed and displayed while being
// unlaunchable, and the substitution in particular happens inside
// agentic.BuildPlan rather than in anything this package declares.
const (
	astraAlias    vendorplugin.ModelID = "astra"
	astraIdentity vendorplugin.ModelID = "gpt-6-astra"
)

// astraProbedEfforts is the vocabulary as `codex debug models` gave it, written
// down here rather than read off the row under test.
//
// That is the whole point of the list. A test that iterated the row's own
// Effort.Vocabulary would silently shrink with it: drop `ultra` from the
// declaration and such a test would launch five words, pass, and report
// nothing about the sixth having stopped working. The list is therefore an
// independent statement, and TestTheAstraAliasStaysAdmissibleUnderItsOwnSpelling
// is what holds the declaration to it.
var astraProbedEfforts = []string{"low", "medium", "high", "xhigh", "max", "ultra"}

// astraRequest is a real codex launch with a stub `codex` on PATH, so binary
// resolution succeeds for a reason that is not the operator's machine.
func astraRequest(t *testing.T, model vendorplugin.ModelID, effort string) vendorplugin.SpawnRequest {
	t.Helper()
	binDir, workDir := t.TempDir(), t.TempDir()
	writeStubBinary(t, binDir, "codex")
	promptPath := filepath.Join(workDir, "assignment.md")
	if err := os.WriteFile(promptPath, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}
	return vendorplugin.SpawnRequest{
		Runtime: "codex", Model: model, Effort: effort,
		PromptPath: promptPath, Prompt: []byte("do the thing\n"),
		WorkDir: workDir, Env: []string{"PATH=" + binDir},
		Run: agentic.RunContext{
			RunID: "RUN-ASTRA-ALIAS", TaskID: "TASK-260907-2OHM2O",
			BoardDir: filepath.Join(workDir, ".task-board"), ContextID: "CTX-ASTRA-ALIAS",
		},
	}
}

// argvPair reports whether flag is immediately followed by value, as separate
// argv elements.
//
// A POSITION check rather than a containment one, for two reasons. A value that
// landed as the argument to some other flag is a different launch; and `astra`
// is a SUBSTRING of `gpt-6-astra`, so a containment check over the joined argv
// would report the alias as absent from an argv that spells it and as present
// in one that does not. Every assertion in this file that could be fooled by
// that overlap is written against whole elements.
func argvPair(argv []string, flag, value string) bool {
	for i := 1; i < len(argv); i++ {
		if argv[i-1] == flag && argv[i] == value {
			return true
		}
	}
	return false
}

// argvHasElement reports whether any argv element is exactly value.
func argvHasElement(argv []string, value string) bool {
	for _, element := range argv {
		if element == value {
			return true
		}
	}
	return false
}

// astraLaunchProblems is the launch proof itself, returning disagreements
// instead of failing, so a mutant can drive the SAME assertions and be required
// to break them.
//
// A test body that called t.Errorf directly would be unmutatable: the only way
// to show these checks bind is to run them against a registry where the
// substitution is wrong and see them fire. This is the shape sourceport_test.go
// already uses for its own pins (comparePortedLineup + requireReport).
func astraLaunchProblems(t *testing.T, registry *vendorplugin.Registry, requested, wantLaunched vendorplugin.ModelID) []string {
	t.Helper()
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	// Both modes. Dry run is not a decoration: it is what an operator reads
	// before committing to a spawn, and a dry run that previewed the alias
	// while an exec ran the identity would be a preview of a different launch.
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeDryRun, agentic.LaunchModeExec} {
		for _, effort := range astraProbedEfforts {
			plan, err := vendorplugin.BuildLaunch(context.Background(), registry, astraRequest(t, requested, effort), mode)
			if err != nil {
				report("BuildLaunch(codex, %s, %s, %s) refused the launch: %v", requested, effort, mode, err)
				continue
			}
			if plan.System != "codex" {
				report("%s/%s: plan.System = %q, want the harness the row declares", requested, effort, plan.System)
			}
			if !argvPair(plan.Argv, "-m", string(wantLaunched)) {
				report("%s/%s/%s: argv does not carry `-m %s`: %v; a row whose launch identity does not reach argv launches something else",
					requested, effort, mode, wantLaunched, plan.Argv)
			}
			// The alias spelling must be absent from EVERY observable surface,
			// not only from argv. agentic.BuildPlan substitutes once, before
			// the first plugin surface, so binary resolution, argv, the child
			// environment and stdin are all built from the identity; a
			// substitution applied to argv alone would still hand the provider
			// a model it does not have through a config value or an env var.
			if requested != wantLaunched {
				if argvHasElement(plan.Argv, string(requested)) {
					report("%s/%s/%s: the alias spelling reached argv: %v", requested, effort, mode, plan.Argv)
				}
				for _, entry := range plan.Env {
					if _, value, found := strings.Cut(entry, "="); found && value == string(requested) {
						report("%s/%s/%s: the alias spelling reached the child environment: %q", requested, effort, mode, entry)
					}
				}
				if strings.Contains(string(plan.Stdin.Bytes), string(requested)) &&
					!strings.Contains(string(plan.Stdin.Bytes), string(wantLaunched)) {
					report("%s/%s/%s: the alias spelling reached stdin without the identity", requested, effort, mode)
				}
			}
			// codex's own argv grammar for the effort, quotes included, as the
			// harness actually spells it. The word an operator chose has to
			// reach the child: an argv that drops it runs the vendor's default
			// depth, silently and at a different cost.
			if want := `model_reasoning_effort="` + effort + `"`; !argvHasElement(plan.Argv, "-c") ||
				!strings.Contains(strings.Join(plan.Argv, " "), want) {
				report("%s/%s/%s: argv = %v, want a -c pair carrying %q", requested, effort, mode, plan.Argv, want)
			}
			// The provenance pair, which is the audit answer to "what did I ask
			// for, and what ran". Collapsing it onto one field would make an
			// alias launch indistinguishable from a launch of the identity.
			want := agentic.ModelIdentity{Requested: string(requested), Launched: string(wantLaunched)}
			if plan.ModelIdentity != want {
				report("%s/%s/%s: ModelIdentity = %#v, want %#v", requested, effort, mode, plan.ModelIdentity, want)
			}
			if plan.ModelIdentity.IsAlias() != (requested != wantLaunched) {
				report("%s/%s/%s: IsAlias() = %v for requested %q launched %q",
					requested, effort, mode, plan.ModelIdentity.IsAlias(), plan.ModelIdentity.Requested, plan.ModelIdentity.Launched)
			}
		}
	}
	return problems
}

// TestTheAstraAliasLaunchesAsGPT6AstraAtEveryProbedEffort is the acceptance
// criterion: `--model astra --reasoning-effort <any of six>` executes
// gpt-6-astra, in both launch modes.
func TestTheAstraAliasLaunchesAsGPT6AstraAtEveryProbedEffort(t *testing.T) {
	for _, problem := range astraLaunchProblems(t, isolatedRegistry(t, nil), astraAlias, astraIdentity) {
		t.Error(problem)
	}
}

// TestTheAstraIdentityLaunchesUnderItsOwnSpelling is the other half of the
// substitution's scope: the identity row is not an alias and must come back
// untouched, reporting IsAlias() false.
//
// It runs the SAME assertions with requested == launched, so a substitution
// that had started rewriting everything would fail here rather than pass by
// agreeing with itself.
func TestTheAstraIdentityLaunchesUnderItsOwnSpelling(t *testing.T) {
	for _, problem := range astraLaunchProblems(t, isolatedRegistry(t, nil), astraIdentity, astraIdentity) {
		t.Error(problem)
	}
}

// astraEffortRefusals is the negative half, returning disagreements for the
// same reason astraLaunchProblems does.
//
// A vocabulary is only a vocabulary if something outside it is refused, and
// these are the words that would plausibly be tried: `minimal`, which this
// vendor's five LEGACY rows do accept and this pair does not; the empty effort,
// which Required forbids and which nothing may fill in from Recommended; and a
// word nobody published.
func astraEffortRefusals(t *testing.T, registry *vendorplugin.Registry, requested vendorplugin.ModelID) []string {
	t.Helper()
	var problems []string
	for _, refusal := range []struct {
		effort string
		want   error
		why    string
	}{
		{effort: "minimal", want: vendorplugin.ErrEffortNotInVocabulary,
			why: "the vocabulary is per MODEL, and admitting a sibling row's word sends the vendor one it never published for this model"},
		{effort: "ludicrous", want: vendorplugin.ErrEffortNotInVocabulary,
			why: "a word no row anywhere declares must not reach the harness"},
		{effort: "", want: vendorplugin.ErrEffortMissing,
			why: "nothing may substitute the recommended word for an unstated one"},
	} {
		_, err := vendorplugin.BuildLaunch(context.Background(), registry, astraRequest(t, requested, refusal.effort), agentic.LaunchModeDryRun)
		if !errors.Is(err, refusal.want) {
			problems = append(problems, fmt.Sprintf("BuildLaunch(codex, %s, %q) = %v, want %v; %s",
				requested, refusal.effort, err, refusal.want, refusal.why))
		}
	}
	return problems
}

// TestTheAstraAliasEffortGateRefusesWhatItMustReject drives the negatives
// through the alias spelling.
//
// The effort word is validated against the REQUESTED row — the alias — and then
// transported to a process running the identity. A gate that resolved the alias
// first and validated against the identity would be a different gate that
// happens to agree today, and would stop agreeing the moment the two axes drift.
func TestTheAstraAliasEffortGateRefusesWhatItMustReject(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, requested := range []vendorplugin.ModelID{astraAlias, astraIdentity} {
		t.Run(string(requested), func(t *testing.T) {
			for _, problem := range astraEffortRefusals(t, registry, requested) {
				t.Error(problem)
			}
		})
	}
}

// astraNonAliasProblems requires every codex-driven openai row EXCEPT the alias
// to launch under its own id.
//
// This is the narrowing direction for the substitution: openai declares
// fourteen codex rows and exactly ONE is an alias. A resolution that fired on a
// shared prefix, a shared score or a shared context window would redirect a
// pinned launch somewhere else entirely, and on the legacy rows it would be
// silent — an operator pinning gpt-5.3-codex is pinning it on purpose.
func astraNonAliasProblems(t *testing.T, registry *vendorplugin.Registry) []string {
	t.Helper()
	var problems []string

	resolved, err := registry.ResolveRuntime("codex")
	if err != nil {
		t.Fatalf("ResolveRuntime(codex): %v", err)
	}
	rows := vendorplugin.RuntimeModels(resolved)
	ids := make([]vendorplugin.ModelID, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		if id == astraAlias {
			continue
		}
		row := rows[id]
		effort := row.Effort.Recommended
		if row.Effort.Support == agentic.EffortSupportNone {
			effort = ""
		}
		plan, err := vendorplugin.BuildLaunch(context.Background(), registry, astraRequest(t, id, effort), agentic.LaunchModeExec)
		if err != nil {
			problems = append(problems, fmt.Sprintf("BuildLaunch(codex, %s, %s): %v", id, effort, err))
			continue
		}
		if !argvPair(plan.Argv, "-m", string(id)) {
			problems = append(problems, fmt.Sprintf("argv = %v, want the requested row %q untouched", plan.Argv, id))
		}
		if plan.ModelIdentity.IsAlias() {
			problems = append(problems, fmt.Sprintf("%q was launched as %q and declares no alias", id, plan.ModelIdentity.Launched))
		}
	}
	return problems
}

// TestTheOtherCodexRowsAreNotRewritten is that narrowing direction as a test.
func TestTheOtherCodexRowsAreNotRewritten(t *testing.T) {
	for _, problem := range astraNonAliasProblems(t, isolatedRegistry(t, nil)) {
		t.Error(problem)
	}
}

// TestTheAstraAliasStaysAdmissibleUnderItsOwnSpelling states what the
// substitution deliberately does NOT do, and holds the declaration to the
// probed vocabulary.
//
// The alias is a real row: it is indexed by the codex runtime, it is ranked, it
// is displayed, and the effort word a caller supplies is validated against ITS
// vocabulary. Resolving it at admission time instead of at launch time would
// silently change which configured pairs a board admits.
func TestTheAstraAliasStaysAdmissibleUnderItsOwnSpelling(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	resolved, err := registry.ResolveRuntime("codex")
	if err != nil {
		t.Fatalf("ResolveRuntime(codex): %v", err)
	}
	rows := vendorplugin.RuntimeModels(resolved)
	alias, indexed := rows[astraAlias]
	if !indexed {
		t.Fatalf("the codex runtime does not index %q; a row its harness cannot reach is a declaration nothing can launch", astraAlias)
	}
	identity, indexed := rows[astraIdentity]
	if !indexed {
		t.Fatalf("the codex runtime does not index %q", astraIdentity)
	}
	if alias.AliasOf != astraIdentity {
		t.Errorf("%q declares AliasOf %q, want %q", alias.ID, alias.AliasOf, astraIdentity)
	}
	if identity.AliasOf != "" {
		t.Errorf("%q declares AliasOf %q; resolution is one hop and a chain would launch the middle of it", identity.ID, identity.AliasOf)
	}
	// The independent list, not the identity's copy of it: comparing the alias
	// against its target would pass on a pair that drifted together, which is
	// exactly what a single careless edit to astraEffort() produces.
	if !equalStrings(alias.Effort.Vocabulary, astraProbedEfforts) {
		t.Errorf("%q accepts %v and the catalog probe recorded %v; the vocabulary is what the vendor published for this head",
			alias.ID, alias.Effort.Vocabulary, astraProbedEfforts)
	}
	if !equalStrings(identity.Effort.Vocabulary, astraProbedEfforts) {
		t.Errorf("%q accepts %v and the catalog probe recorded %v", identity.ID, identity.Effort.Vocabulary, astraProbedEfforts)
	}
	if alias.Effort.Support != agentic.EffortSupportRequired {
		t.Errorf("%q declares effort support %v, want Required", alias.ID, alias.Effort.Support)
	}
	// The display pick is sol's and this row must not take it. Admitting a more
	// capable model is not a decision to change what an operator is steered
	// towards by default, and an alias taking it would do so under a name the
	// vendor does not publish.
	if alias.Recommended {
		t.Errorf("%q is marked Recommended; gpt-5.6-sol holds this vendor's display pick", alias.ID)
	}
	if alias.ContextWindowTokens != identity.ContextWindowTokens {
		t.Errorf("%q declares a %d-token window and %q declares %d; it is one model, so a different window describes a run nobody can have",
			alias.ID, alias.ContextWindowTokens, identity.ID, identity.ContextWindowTokens)
	}
}

// ---------------------------------------------------------------------------
// The mutants.
//
// Everything above passes on a tree where the substitution works. That is
// equally consistent with assertions that bind and with assertions that cannot
// fail, so each gate below is WEAKENED — kept in place and made to admit
// exactly one member of the class it must reject — and a named assertion is
// required to fire. A mutant that merely DELETES the alias proves the row
// exists and says nothing about the class, so `the alias declaration removed`
// is the least of these rather than the argument.
// ---------------------------------------------------------------------------

// astraMutantRegistry is isolatedRegistry with the registration error returned
// rather than fatal.
//
// It exists for one mutant: a vocabulary carrying a blank word is refused by
// Model.Validate at REGISTRATION, before any launch. isolatedRegistry would
// t.Fatalf on that and the report would say nothing about which layer refused,
// which is the fact that mutant establishes.
func astraMutantRegistry(t *testing.T, mutate func(vendorplugin.Model) vendorplugin.Model) (*vendorplugin.Registry, error) {
	t.Helper()
	registry := vendorplugin.NewRegistry(agentic.Default)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding the frozen runtimes: %v", err)
	}
	ids := make([]vendorplugin.VendorID, 0, len(portedVendors))
	for vendor := range portedVendors {
		ids = append(ids, vendor)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, vendor := range ids {
		plugin, ok := vendorplugin.Default.Lookup(vendor)
		if !ok {
			t.Fatalf("vendor %s is not registered in the default registry", vendor)
		}
		transform := mutate
		if vendor != "openai" {
			transform = nil
		}
		if err := registry.Register(mutantVendor{inner: plugin, mutate: transform}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// astraPair applies a transform to the astra head and its alias TOGETHER.
//
// It has to be both rows. checkAliases refuses a pair whose effort axes
// disagree, so a mutant that widened only the alias would be refused at
// registration and would report on that gate instead of on the launch one.
// TestTheAstraAliasAxisMayNotDriftFromTheHead is where the one-sided drift is
// attacked on purpose.
func astraPair(transform func(vendorplugin.Model) vendorplugin.Model) func(vendorplugin.Model) vendorplugin.Model {
	return func(m vendorplugin.Model) vendorplugin.Model {
		if m.ID != astraAlias && m.ID != astraIdentity {
			return m
		}
		return transform(m)
	}
}

func withVocabulary(m vendorplugin.Model, words []string) vendorplugin.Model {
	m.Effort.Vocabulary = append([]string(nil), words...)
	return m
}

// TestTheAstraLaunchProofFiresOnEveryWayTheSubstitutionCouldBreak is the
// narrowing harness for the launch proof and the effort gate.
func TestTheAstraLaunchProofFiresOnEveryWayTheSubstitutionCouldBreak(t *testing.T) {
	for _, tt := range []struct {
		name    string
		narrows string
		mutate  func(vendorplugin.Model) vendorplugin.Model
		drive   func(*testing.T, *vendorplugin.Registry) []string
		expect  string
	}{
		{
			// The delete-only one, kept for completeness and claimed as
			// nothing more: it shows the substitution is what puts the identity
			// on argv, not that the substitution covers any particular class.
			name:    "the alias declaration removed",
			narrows: "the alias resolves to nothing and reaches the provider under a spelling it never published",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == astraAlias {
					m.AliasOf = ""
				}
				return m
			},
			drive: func(t *testing.T, r *vendorplugin.Registry) []string {
				return astraLaunchProblems(t, r, astraAlias, astraIdentity)
			},
			expect: "the alias spelling reached argv",
		},
		{
			// The gate STAYS: `astra` still never reaches argv, and the plan
			// still reports an alias. Only the identity is wrong. An assertion
			// that had settled for "the alias is absent from argv" would pass
			// this and would have proved nothing about what actually ran.
			name:    "the alias retargeted to another real row",
			narrows: "the substitution resolves to a declared row that is not the head",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == astraAlias {
					m.AliasOf = "gpt-5.6-sol"
					// The alias-mirror rule (sameSystems) would refuse the
					// mutant otherwise; the mutant is about the TARGET, so the
					// systems follow the new target as the rule demands.
					m.Systems = []agentic.SystemID{"codex", "pi-native"}
				}
				return m
			},
			drive: func(t *testing.T, r *vendorplugin.Registry) []string {
				return astraLaunchProblems(t, r, astraAlias, astraIdentity)
			},
			expect: "argv does not carry `-m gpt-6-astra`",
		},
		{
			// Exactly one of the six words stops launching. This is what the
			// hard-coded astraProbedEfforts list buys: a test that iterated the
			// row's own vocabulary would shrink with the mutant and pass.
			name:    "one effort word dropped from the pair",
			narrows: "five of the six probed efforts still launch and `ultra` does not",
			mutate: astraPair(func(m vendorplugin.Model) vendorplugin.Model {
				return withVocabulary(m, dropWord(m.Effort.Vocabulary, "ultra"))
			}),
			drive: func(t *testing.T, r *vendorplugin.Registry) []string {
				return astraLaunchProblems(t, r, astraAlias, astraIdentity)
			},
			expect: "BuildLaunch(codex, astra, ultra",
		},
		{
			// The vocabulary gate stays and admits exactly one more word.
			// `ludicrous` is still refused, so this is a narrowing rather than
			// a removal.
			name:    "one refused word admitted by the pair",
			narrows: "`minimal` is admitted while every other unpublished word stays refused",
			mutate: astraPair(func(m vendorplugin.Model) vendorplugin.Model {
				return withVocabulary(m, append(append([]string(nil), m.Effort.Vocabulary...), "minimal"))
			}),
			drive:  func(t *testing.T, r *vendorplugin.Registry) []string { return astraEffortRefusals(t, r, astraAlias) },
			expect: `BuildLaunch(codex, astra, "minimal")`,
		},
		{
			// The unstated effort. There is no EffortSupportOptional in this
			// module — the axis is Required or absent — so removing the axis is
			// the narrowest weakening the row can express, and it admits
			// exactly the empty effort.
			name:    "the pair's effort axis removed",
			narrows: "an unstated effort is admitted and the vendor's own default depth runs silently",
			mutate: astraPair(func(m vendorplugin.Model) vendorplugin.Model {
				m.Effort = vendorplugin.EffortDeclaration{Support: agentic.EffortSupportNone}
				return m
			}),
			drive:  func(t *testing.T, r *vendorplugin.Registry) []string { return astraEffortRefusals(t, r, astraAlias) },
			expect: `BuildLaunch(codex, astra, "")`,
		},
		{
			// The other direction of the substitution's scope: exactly one
			// identity row starts being rewritten.
			name:    "one identity row declared an alias",
			narrows: "gpt-5.6-sol is redirected while the other twelve codex rows are not",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == "gpt-5.6-sol" {
					m.AliasOf = astraIdentity
					// Mirror the target's systems so the mutant registers
					// (sameSystems); the fact under test is the redirect.
					m.Systems = []agentic.SystemID{"codex"}
				}
				return m
			},
			drive:  func(t *testing.T, r *vendorplugin.Registry) []string { return astraNonAliasProblems(t, r) },
			expect: `"gpt-5.6-sol" was launched as "gpt-6-astra"`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := astraMutantRegistry(t, tt.mutate)
			if err != nil {
				t.Fatalf("the mutant did not register, so it proves nothing about the launch gate: %v", err)
			}
			problems := tt.drive(t, registry)
			if len(problems) == 0 {
				t.Fatalf("the mutant survived: %s, and no assertion fired; the gate's silence on the real tree therefore proves nothing (wanted %q)", tt.narrows, tt.expect)
			}
			for _, problem := range problems {
				if strings.Contains(problem, tt.expect) {
					return
				}
			}
			t.Fatalf("the assertions fired but never named the drift: wanted a report containing %q, got %v", tt.expect, problems)
		})
	}
}

// TestTheAstraAliasAxisMayNotDriftFromTheHead attacks the one gate the mutants
// above have to work around: checkAliases holds the alias's effort axis equal
// to its target's.
//
// The drift is ONE WORD, which is the whole point. The word is validated
// against the requested row and transported to a process running the target, so
// a one-word gap is an effort the module accepts and the executing model
// refuses — or the reverse. Registration is where that has to fail, because by
// launch time the substitution has already been spent.
func TestTheAstraAliasAxisMayNotDriftFromTheHead(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(vendorplugin.Model) vendorplugin.Model
	}{
		{
			name: "the alias accepts one word the head does not",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == astraAlias {
					return withVocabulary(m, append(append([]string(nil), m.Effort.Vocabulary...), "minimal"))
				}
				return m
			},
		},
		{
			name: "the head accepts one word the alias does not",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == astraAlias {
					return withVocabulary(m, dropWord(m.Effort.Vocabulary, "ultra"))
				}
				return m
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := astraMutantRegistry(t, tt.mutate)
			if !errors.Is(err, vendorplugin.ErrAliasInvalid) {
				t.Fatalf("registering the drifted pair returned %v, want ErrAliasInvalid; a one-word gap between an alias and its identity is an effort this module admits and the executing model refuses", err)
			}
		})
	}
}

// TestTheAstraAliasMayNotBeChainedOrSelfReferential closes the two remaining
// shapes AliasOf can take on this pair, at registration.
//
// Resolution is ONE HOP. A chain would launch the middle of it, and a
// self-reference would launch nothing at all while looking like a declared
// redirection.
func TestTheAstraAliasMayNotBeChainedOrSelfReferential(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(vendorplugin.Model) vendorplugin.Model
	}{
		{
			name: "the head becomes an alias, making astra a two-hop chain",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == astraIdentity {
					m.AliasOf = "gpt-5.6-sol"
				}
				return m
			},
		},
		{
			name: "the alias points at itself",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == astraAlias {
					m.AliasOf = astraAlias
				}
				return m
			},
		},
		{
			name: "the alias points at a row no lineup declares",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID == astraAlias {
					m.AliasOf = "gpt-7-nobody-published-this"
				}
				return m
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := astraMutantRegistry(t, tt.mutate); err == nil {
				t.Fatal("the drifted alias registered; a launch would then resolve to something this registry cannot vouch for")
			}
		})
	}
}

// TestTheAstraMutantHarnessMirrorsProduction keeps the mutants honest.
//
// astraMutantRegistry is a second construction path, and without this a mutant
// "firing" could be that path differing from production rather than the drift
// under test. With no transform applied, every assertion the mutants drive must
// be silent.
func TestTheAstraMutantHarnessMirrorsProduction(t *testing.T) {
	registry, err := astraMutantRegistry(t, nil)
	if err != nil {
		t.Fatalf("the unmutated harness did not register: %v", err)
	}
	for _, problem := range astraLaunchProblems(t, registry, astraAlias, astraIdentity) {
		t.Errorf("unmutated harness: %s", problem)
	}
	for _, problem := range astraEffortRefusals(t, registry, astraAlias) {
		t.Errorf("unmutated harness: %s", problem)
	}
	for _, problem := range astraNonAliasProblems(t, registry) {
		t.Errorf("unmutated harness: %s", problem)
	}
}

// TestTheEmptyEffortCannotBeAdmittedThroughTheVocabulary records where the
// unstated-effort class is actually closed, because it is not where a reader
// would look.
//
// The obvious way to make `--reasoning-effort ""` launchable is to put the
// empty word in the vocabulary and leave the axis Required. That never reaches
// the launch gate: EffortDeclaration.Validate refuses a blank vocabulary word
// at REGISTRATION. So the empty effort is refused twice over — once by
// EffortSupportRequired in agentic.BuildPlan, and once by a declaration that
// cannot legally offer it — and the mutant that removes the axis entirely is
// the only weakening the row can express.
//
// STATED BOUND: this is a registration refusal, not a launch one. It says
// nothing about a caller who reaches BuildPlan with a hand-built request; that
// path is covered by the empty-effort case in astraEffortRefusals.
func TestTheEmptyEffortCannotBeAdmittedThroughTheVocabulary(t *testing.T) {
	_, err := astraMutantRegistry(t, astraPair(func(m vendorplugin.Model) vendorplugin.Model {
		return withVocabulary(m, append(append([]string(nil), m.Effort.Vocabulary...), ""))
	}))
	if err == nil {
		t.Fatal("a vocabulary carrying the blank word registered; the empty effort would then be a published word for this pair")
	}
	if !strings.Contains(err.Error(), "blank word") {
		t.Errorf("registration refused for a reason other than the blank word: %v", err)
	}
}
