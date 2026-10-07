package benchdata

import "strconv"

// THE anthropic binding file: the one place this vendor's models, their ranks
// and the agentic systems that drive them are written down.
//
// One binding file per vendor is enforced, not merely intended:
// pkg/agentic/singlesource_guard_test.go names this declaration file as the home of
// the "anthropic models" fact, and a plugin id spelled in a composite literal
// in any other file of this package fails the module guard. A second table of
// "which systems drive which anthropic model" is the shadow-binding disease
// the whole guard exists to prevent.
//
// # Provenance, field by field
//
// PORTED VERBATIM from skill-project-management: the model ids, the agentic
// systems each row declares, the effort vocabularies and the recommended
// efforts. A full-set pin (pkg/vendorplugin/sourceport_test.go) holds every one
// of them against a fixture captured from that repository's own sources, so a
// dropped or drifted row fails rather than passes quietly.
//
// PORTED VERBATIM as well, since v0.2.0: the lineup state, the supersession,
// the display recommendation, the context window and the billing contract.
// The board-owned fields are pinned row by row against a frozen capture of the
// board's own table (pkg/vendorplugin/testdata/board-model-facts.json, read by
// pkg/vendorplugin/boardfacts_test.go), so a slipped digit in a price or a
// swapped lifecycle fails rather than passing quietly.
//
// NOT PORTED ANY MORE: the capability SCORE. Until the Bug Hunt Bench re-rank
// the score was the source registry's own PolicyRank — hand-ordered 85..10
// with nothing behind it but the author's placing. Every score below is now a
// bench point (planted bugs fixed out of 105, vendorplugin.bench.go): the three
// rows the leaderboard measured (fable-5-1 43, opus-5 27, opus-4-8 15, each at
// max) carry the exact count, and every other row carries a value explicitly
// marked interpolated between two named anchors. The registry's PolicyRank is
// still quoted on every row, because it is still true of the registry, but it
// is quoted as the registry's number on its own scale and never as the score.
// pkg/vendorplugin/bughunt_test.go holds each score against an independent
// transcription of the leaderboard.
//
// THE ONE ORDER THE BENCH CHANGED. The registry places opus-4-8 (70) above
// sonnet-5 (60). The leaderboard measured opus-4-8 at 15/105 and did not
// measure sonnet-5, whose interpolated 20 sits between opus-5's measured 27
// and opus-4-8's measured 15. So sonnet-5 now stands above opus-4-8, and that
// is the bench disagreeing with the registry rather than this file re-ordering
// on its own: the measured number is the evidence and the registry order is
// what an interpolated row keeps only where no measurement contradicts it.
//
// THE TIE. claude-haiku-4-5 and its dated snapshot claude-haiku-4-5-20251001
// both score 5, and the frozen v2 admission snapshot puts those two ids in ONE
// tier for exactly that reason — they are one model under two names. That tie
// is VISIBLE to a caller rather than flattened into positions 8 and 9, and the
// presentation order Lineup derives for them is declaration order and nothing
// more. Nothing admission-related reads either the score or the derived
// position: membership comes from the frozen snapshot
// (pkg/vendorplugin/v2snapshot.go), and a capability rank stays evidence.
//
// SIX ROWS ARE NOT PORTS: claude-opus-5-5 / `opus`, claude-sonnet-5-5 /
// `sonnet` (declared the same way on 2026-09-28) and claude-haiku-5-5 /
// `haiku` (2026-10-07). claude-opus-5-5 and its `opus` spelling reached this
// repository before the board's registry, so they rest on the Claude Code CLI
// probe (claudeCLIProbe) through declaredRank and quote no source PolicyRank;
// pkg/vendorplugin/declaredhere_test.go names all six. Each pair ties itself
// (45, 44 and 22), an alias and its identity, and ties no ported row.
//
// AUTHORED HERE, not ported: every Description. The source's rows carry a
// short display string and no what-is-this-model-best-for field at all, while
// the vendor contract requires one and refuses a blank. The descriptions below
// were written for this repository from the models' documented positioning.
// They are the ONLY field in this file that is not a source fact, and they must
// never be cited as one.

// sourceRegistry is the table the rows' PolicyRank was read from — the order
// the interpolated rows keep where no measurement contradicts it. It names the
// exact commit so a reader chasing a number has a revision to open rather than
// a moving target.
//
// The board-owned facts the rows gained in v0.2.0 — the PolicyRank again, the
// lifecycle, the supersession, the recommendation, the context window and the
// billing contract — were re-read from that same table at a LATER commit and
// pinned against a capture of it; testdata/board-model-facts.json records which.
// The two captures agree on every column they share, which is what makes them
// corroboration rather than two chances to be wrong.
const anthropicSourceRegistry = "skill-project-management tools/board-cli/internal/spawn/models.go (knownModels, commit ed4878123061b39fdae67160f6b5632117b48a2f)"

// lineup is the vendor-side evidence every rank in this file also rests on.
//
// It is a third entry beside the bench claim and the registry number rather
// than a replacement for either: the bench says how many bugs the row fixed or
// which two rows it was interpolated between, the registry says where the
// board's own table ordered it, and this says why that ordering is not merely
// an internal convention.
var anthropicLineup = RankEvidence{
	Source:      "Anthropic's published Claude lineup, as the source registry's own row descriptions record it",
	Observation: "the lineup places fable-5-1 as the most capable model, opus-5 as the everyday complex-task model, sonnet-5 as the efficient one and haiku-4-5 as the fastest; the ordering below is that lineup, with each legacy id sitting where the source's score puts it",
}

// rank builds a capability rank: a bench-anchored score, the bench claim
// behind it, and the source registry's own PolicyRank for the row.
//
// The score and the bench claim are typed separately on purpose — a
// constructor that derived the observation from the score would agree with
// any score at all — and pkg/vendorplugin/bughunt_test.go holds the two
// against each other. policyRank is the registry's number and is quoted as
// such: still true of the registry, the order the interpolated rows keep, and
// never the score, which is why the sentence says whose scale it is on.
func anthropicRank(score int, bench RankEvidence, policyRank int, note string, evidence RankEvidence) CapabilityRank {
	return CapabilityRank{
		Score: score,
		Basis: []RankEvidence{
			bench,
			{
				Source:      anthropicSourceRegistry,
				Observation: "the row carries PolicyRank " + strconv.Itoa(policyRank) + " on the source registry's own per-vendor scale (the score is the bench value, not that number), " + note,
			},
			evidence,
		},
	}
}

// claudeCLIProbe is the vendor surface the rows this file declares FIRST rest
// on — rows no capture of the board's registry contains, so there is no source
// PolicyRank for them to quote.
//
// It is a MACHINE-LOCAL probe rather than a published card, and naming the
// binary version and the date is what makes it checkable: `claude -p --model
// <id> --output-format json` launches one turn and its modelUsage names the
// model the CLI actually ran, so a reader can re-run the one command.
const anthropicClaudeCLIProbe = "the Claude Code CLI's own model resolution, probed with `claude -p --model <id> --output-format json` (Claude Code 2.1.280, 2026-09-22)"

// declaredRank builds a capability rank for a row NO capture of the board's
// registry contains. It is separate from rank for the reason the openai file
// gives: rank's registry entry states "the row carries PolicyRank N" and names
// the source registry at a commit, which for a row that registry never held is
// a sentence a reader would open the file and not find.
func anthropicDeclaredRank(score int, bench RankEvidence, note string, evidence ...RankEvidence) CapabilityRank {
	return anthropicDeclaredRankFrom(anthropicClaudeCLIProbe, score, bench, note, evidence...)
}

// declaredRankFrom is declaredRank against a NAMED probe, for a row read off a
// later CLI than the one claude-opus-5-5 was declared from.
func anthropicDeclaredRankFrom(probe string, score int, bench RankEvidence, note string, evidence ...RankEvidence) CapabilityRank {
	basis := []RankEvidence{
		bench,
		{Source: probe, Observation: note},
	}
	return CapabilityRank{Score: score, Basis: append(basis, evidence...)}
}

// claudeCLIProbeSonnet55 is the probe the claude-sonnet-5-5 rows rest on. It is
// its own constant because it is a different binary: Claude Code 2.1.281
// answered `claude -p --model claude-sonnet-5-5` with
// `[claude-code:unrecognized_model]`, and 2.1.284 ran the turn. On 2.1.284 the
// CLI's own floating `sonnet` still resolves to claude-sonnet-5, which is why
// the `sonnet` row below declares AliasOf rather than trusting the CLI's alias.
const anthropicClaudeCLIProbeSonnet55 = "the Claude Code CLI's own model resolution, probed with `claude -p --model <id> --output-format json` (Claude Code 2.1.284, 2026-09-28)"

// sonnet55Effort is the claude-sonnet-5-5 effort axis, shared by the identity
// row and its `sonnet` alias. Anthropic publishes low/medium/high/xhigh/max,
// the platform default is high, and high is the recommendation.
func anthropicSonnet55Effort() EffortDeclaration {
	return anthropicEffortRequired("high", []string{"low", "medium", "high", "xhigh", "max"})
}

// sonnet55Rank is claude-sonnet-5-5's rank, handed to both the identity and its
// alias. Unmeasured on the leaderboard, so INTERPOLATED: Anthropic's own card
// puts it level with opus-5-5 on agentic work (Terminal-Bench 4.0 70.6% against
// 66.4%, GDPval-AA 1844 against 1846, CursorBench 4.0 55.5% against 57.8%), so
// it sits just under opus-5-5's 45 and above fable-5-1's measured 43.
func anthropicSonnet55Rank(note string, evidence ...RankEvidence) CapabilityRank {
	return anthropicDeclaredRankFrom(anthropicClaudeCLIProbeSonnet55, 44,
		BughuntInterpolated("claude-opus-5-5", "claude-fable-5-1", "unmeasured; Anthropic's card places sonnet-5-5 level with opus-5-5 on agentic benchmarks, so it sits just under it and above fable-5-1's measured 43"),
		note, evidence...)
}

// anthropicClaudeCLIProbeHaiku55 is the probe the claude-haiku-5-5 rows rest on.
// Read on Claude Code 2.1.290 the day Anthropic released the model: `claude -p
// --model claude-haiku-5-5` ran the turn (stderr carried the CLI's
// `[claude-code:unrecognized_model]` notice, exit 0), modelUsage named
// claude-haiku-5-5 and reported contextWindow 200000, the CLI's own assumption
// and not the provider's 1M. `claude -p --model haiku` on the same binary ran
// claude-haiku-4-5-20251001, so the CLI's floating `haiku` does NOT yet float
// to 5.5; the `haiku` row below declares AliasOf either way, so argv carries
// the full id whatever a later CLI floats to.
const anthropicClaudeCLIProbeHaiku55 = "the Claude Code CLI's own model resolution, probed with `claude -p --model <id> --output-format json` (Claude Code 2.1.290, 2026-10-07)"

// haiku55ContextWindow is the OPERATING window the owner imposed on
// claude-haiku-5-5 (2026-10-07). The provider's native window is 1M tokens;
// 100K is also the boundary of Anthropic's cheap pricing tier ($0.10/$0.50 per
// MTok up to 100K prompt tokens, $0.50/$2.50 above). The claude-code plugin
// reads this fact and exports CLAUDE_CODE_AUTO_COMPACT_WINDOW so the harness
// compacts at it, rather than the number being a label nothing enforces.
const haiku55ContextWindow = 100_000

// haiku55Effort is the claude-haiku-5-5 effort axis, shared by the identity row
// and its `haiku` alias. Anthropic publishes all five levels and recommends
// `medium` as the default starting point for this model, unlike the sonnet and
// opus rows, which recommend `high`.
func anthropicHaiku55Effort() EffortDeclaration {
	return anthropicEffortRequired("medium", []string{"low", "medium", "high", "xhigh", "max"})
}

// haiku55Rank is claude-haiku-5-5's rank, handed to both the identity and its
// alias. Unmeasured on the leaderboard, so INTERPOLATED between two MEASURED
// anchors: claude-opus-5 (27) above and claude-opus-4-8 (15) below. 22 sits
// strictly inside and is unique among the anthropic scores (sonnet-5 holds 20).
func anthropicHaiku55Rank(note string, evidence ...RankEvidence) CapabilityRank {
	return anthropicDeclaredRankFrom(anthropicClaudeCLIProbeHaiku55, 22,
		BughuntInterpolated("claude-opus-5", "claude-opus-4-8", "unmeasured; a current-generation fast tier placed above sonnet-5's interpolated 20 and below opus-5's measured 27, strictly inside the two measured anchors"),
		note, evidence...)
}

// opus55Effort is the claude-opus-5-5 effort axis, shared by the identity row
// and its `opus` alias: checkAliases refuses a pair whose axes disagree, so the
// vocabulary is written once. It is opus-5's axis unchanged.
func anthropicOpus55Effort() EffortDeclaration {
	return anthropicEffortRequired("high", []string{"low", "medium", "high", "xhigh", "max"})
}

// opus55Rank is claude-opus-5-5's rank, handed to both the identity and its
// alias. The leaderboard has no run of it, so the value is INTERPOLATED: above
// claude-fable-5-1's measured 43, the vendor's strongest measured row, and below
// gpt-6-astra's measured 48, the leaderboard's top row. 45 sits strictly inside
// and lands on no ported anthropic score.
func anthropicOpus55Rank(note string, evidence ...RankEvidence) CapabilityRank {
	return anthropicDeclaredRank(45,
		BughuntInterpolated("gpt-6-astra", "claude-fable-5-1", "unmeasured; Anthropic presents opus-5-5 as its new most capable general model, above fable-5-1, and the leaderboard's top row is the only measured anchor above that"),
		note, evidence...)
}

// effortRequired declares a model that must be given an explicit effort, with
// the words it accepts and the one the vendor recommends. Recommended is a
// DECLARATION and never a default: nothing substitutes it for a missing
// effort, and it reaches an operator through the refusal text instead.
func anthropicEffortRequired(recommended string, vocabulary []string) EffortDeclaration {
	return EffortDeclaration{
		Support:     EffortRequired,
		Vocabulary:  vocabulary,
		Recommended: recommended,
	}
}

// effortNone declares a model with no reasoning-effort axis at all. Handing one
// an effort is refused rather than dropped, so a caller learns the parameter
// could not be honoured instead of paying for a turn that ignored it.
func anthropicEffortNone() EffortDeclaration {
	return EffortDeclaration{Support: EffortNone}
}

// models is the declaration itself, most capable first.
//
// `pi-native` membership is CATALOG-VERIFIED, not inferred from the vendor: a
// row names it only when the id is present in the installed Pi built-in
// catalog (Pi 0.84.2, pi-ai providers/data/anthropic.json, api
// anthropic-messages), because native Pi resolves `anthropic/<id>` against
// that file and an absent id silently takes the custom-model fallback with a
// warning — an unverified launch. claude-fable-5-1 is absent from that
// catalog and therefore deliberately does NOT name pi-native; the launch is
// refused as ErrModelNotDrivenBySystem rather than guessed at. Re-verify the
// membership against the installed catalog bytes before changing it.
var anthropicModels = []Model{
	{
		ID:          "claude-opus-5-5",
		Description: "The strongest Claude for complex engineering and long agentic runs: multi-file changes, design work and reviews that need the most judgement",
		Rank:        anthropicOpus55Rank("`claude -p --model claude-opus-5-5` ran one turn whose modelUsage names claude-opus-5-5; the source registry contains NO row for claude-opus-5-5 and this score is therefore not a ported one"),
		Lifecycle:   LifecycleCurrent,
		Effort:      anthropicOpus55Effort(),
		// NOT Recommended: claude-opus-5 keeps this vendor's display pick, and
		// at most one anthropic row may carry it. Admitting a newer model is
		// not a decision to move what an operator is steered to by default.
		//
		// NOT pi-native: the installed Pi catalog (0.84.2) carries no
		// claude-opus-5-5, and an absent id takes Pi's unverified fallback.
		Systems: []string{"claude-code"},
	},
	{
		// The floating short spelling of the current opus head, declared with
		// AliasOf so the launch executes as claude-opus-5-5 by THIS row's say-so
		// rather than by whatever `opus` the installed Claude Code CLI happens
		// to float to. The CLI resolves `opus` itself today (to claude-opus-5-5
		// on 2.1.280), but a declared alias keeps argv, the audit trail and cost
		// attribution pinned to one id across CLI upgrades.
		ID:          "opus",
		Description: "The short spelling of the current opus head, for an invocation that names the model without its generation; it executes as claude-opus-5-5",
		Rank: anthropicOpus55Rank("this row is a short spelling of claude-opus-5-5 and carries that row's score; there is no second capability to score",
			RankEvidence{
				Source:      anthropicClaudeCLIProbe,
				Observation: "`claude -p --model opus` ran one turn whose modelUsage names claude-opus-5-5, the same model the full id runs; this row declares AliasOf so argv carries the full id",
			}),
		AliasOf:   "claude-opus-5-5",
		Lifecycle: LifecycleCurrent,
		Effort:    anthropicOpus55Effort(),
		Systems:   []string{"claude-code"},
	},
	{
		ID:          "claude-sonnet-5-5",
		Description: "Near-Opus judgement at Sonnet cost and speed: everyday agentic coding, multi-file changes and reviews where opus-5-5 is more than the task needs",
		Rank:        anthropicSonnet55Rank("`claude -p --model claude-sonnet-5-5` on Claude Code 2.1.284 ran one turn whose modelUsage names claude-sonnet-5-5; the source registry contains NO row for it and this score is therefore not a ported one"),
		Lifecycle:   LifecycleCurrent,
		Effort:      anthropicSonnet55Effort(),
		// NOT Recommended: claude-opus-5 keeps this vendor's display pick.
		// NOT pi-native: the installed Pi catalog (0.84.2) carries no
		// claude-sonnet-5-5.
		Systems: []string{"claude-code"},
	},
	{
		// The floating short spelling of the current sonnet head, declared with
		// AliasOf so argv carries claude-sonnet-5-5. The installed Claude Code
		// CLI resolves its own `sonnet` to claude-sonnet-5 on 2.1.284, so
		// relying on the CLI's alias would run the previous generation.
		ID:          "sonnet",
		Description: "The short spelling of the current sonnet head, for an invocation that names the model without its generation; it executes as claude-sonnet-5-5",
		Rank: anthropicSonnet55Rank("this row is a short spelling of claude-sonnet-5-5 and carries that row's score; there is no second capability to score",
			RankEvidence{
				Source:      anthropicClaudeCLIProbeSonnet55,
				Observation: "`claude -p --model sonnet` on Claude Code 2.1.284 ran one turn whose modelUsage names claude-sonnet-5, the PREVIOUS generation; this row declares AliasOf so argv carries claude-sonnet-5-5 instead of the CLI's own floating alias",
			}),
		AliasOf:   "claude-sonnet-5-5",
		Lifecycle: LifecycleCurrent,
		Effort:    anthropicSonnet55Effort(),
		Systems:   []string{"claude-code"},
	},
	{
		ID:          "claude-fable-5-1",
		Description: "The hardest and longest-running work: deep refactors and multi-hour agent runs where a cheaper model stalls",
		Rank:        anthropicRank(43, BughuntMeasured("max", 43), 85, "the highest of the nine anthropic rows, above the fable-5 it supersedes; its high setting fixed 33", anthropicLineup),
		Lifecycle:   LifecycleCurrent,
		Effort:      anthropicEffortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []string{"claude-code"},
	},
	{
		ID:           "claude-fable-5",
		Description:  "Previous-generation Fable, for runs pinned to it; prefer claude-fable-5-1 for new work",
		Rank:         anthropicRank(36, BughuntInterpolated("claude-fable-5-1", "claude-opus-5", "unmeasured; the predecessor of 5.1, placed above opus-5 as the registry orders it"), 80, "below fable-5-1 and above opus-5", anthropicLineup),
		Lifecycle:    LifecycleLegacy,
		SupersededBy: "claude-fable-5-1",
		Effort:       anthropicEffortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:      []string{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-opus-5",
		Description: "The default for complex day-to-day engineering: multi-file changes, design work and reviews that need judgement",
		Rank:        anthropicRank(27, BughuntMeasured("max", 27), 75, "below fable-5 and above opus-4-8; its high setting fixed 21", anthropicLineup),
		Lifecycle:   LifecycleCurrent,
		Effort:      anthropicEffortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Recommended: true,
		Systems:     []string{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-haiku-5-5",
		Description: "Fast, cheap high-volume turns: classification, extraction, routing and simple sub-agent work, run inside a 100K-token operating window",
		Rank:        anthropicHaiku55Rank("`claude -p --model claude-haiku-5-5` on Claude Code 2.1.290 ran one turn whose modelUsage names claude-haiku-5-5; the source registry contains NO row for it and this score is therefore not a ported one"),
		Lifecycle:   LifecycleCurrent,
		Effort:      anthropicHaiku55Effort(),
		// Owner-imposed operating window, not the provider's native 1M; see
		// haiku55ContextWindow. Enforced at launch by the claude-code plugin.
		ContextWindowTokens: haiku55ContextWindow,
		// NOT Recommended: claude-opus-5 keeps this vendor's display pick.
		// NOT pi-native: the installed Pi catalog (0.84.2) carries no
		// claude-haiku-5-5.
		Systems: []string{"claude-code"},
	},
	{
		// The floating short spelling of the current haiku head, declared with
		// AliasOf so argv carries claude-haiku-5-5. The installed Claude Code
		// CLI resolves its own `haiku` to claude-haiku-4-5-20251001 on 2.1.290,
		// so relying on the CLI's alias would run the previous generation.
		ID:          "haiku",
		Description: "The short spelling of the current haiku head, for an invocation that names the model without its generation; it executes as claude-haiku-5-5",
		Rank: anthropicHaiku55Rank("this row is a short spelling of claude-haiku-5-5 and carries that row's score; there is no second capability to score",
			RankEvidence{
				Source:      anthropicClaudeCLIProbeHaiku55,
				Observation: "`claude -p --model haiku` on Claude Code 2.1.290 ran one turn whose modelUsage names claude-haiku-4-5-20251001, the PREVIOUS generation; this row declares AliasOf so argv carries claude-haiku-5-5 instead of the CLI's own floating alias",
			}),
		AliasOf:             "claude-haiku-5-5",
		Lifecycle:           LifecycleCurrent,
		Effort:              anthropicHaiku55Effort(),
		ContextWindowTokens: haiku55ContextWindow,
		Systems:             []string{"claude-code"},
	},
	{
		ID:          "claude-sonnet-5",
		Description: "Routine work at lower cost: scoped edits, tests and summaries where Opus-level judgement is not the bottleneck",
		Rank:        anthropicRank(20, BughuntInterpolated("claude-opus-5", "claude-opus-4-8", "unmeasured; placed between the two measured opus rows, which puts it above opus-4-8 where the registry had it below"), 60, "below opus-4-8 and above opus-4-6", anthropicLineup),
		Lifecycle:   LifecycleCurrent,
		Effort:      anthropicEffortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []string{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-opus-4-8",
		Description:  "Previous-generation Opus, for runs pinned to it; prefer claude-opus-5 for new work",
		Rank:         anthropicRank(15, BughuntMeasured("max", 15), 70, "below opus-5 and above sonnet-5 on the registry's scale; the bench measured this row below sonnet-5's interpolated 20, and the measured number wins", anthropicLineup),
		Lifecycle:    LifecycleLegacy,
		SupersededBy: "claude-opus-5",
		Effort:       anthropicEffortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:      []string{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-opus-4-6",
		Description:  "Older top-tier Opus kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:         anthropicRank(12, BughuntInterpolated("claude-opus-4-8", "claude-sonnet-4-6", "legacy and unmeasured; the registry order is kept"), 30, "below sonnet-5 and above sonnet-4-6", anthropicLineup),
		Lifecycle:    LifecycleLegacy,
		SupersededBy: "claude-opus-5",
		Effort:       anthropicEffortRequired("high", []string{"low", "medium", "high", "max"}),
		Systems:      []string{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-sonnet-4-6",
		Description:  "Older balanced Sonnet kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:         anthropicRank(9, BughuntInterpolated("claude-opus-4-6", "claude-haiku-4-5", "legacy and unmeasured; the registry order is kept"), 20, "below opus-4-6 and above the two haiku rows", anthropicLineup),
		Lifecycle:    LifecycleLegacy,
		SupersededBy: "claude-sonnet-5",
		Effort:       anthropicEffortRequired("medium", []string{"low", "medium", "high", "max"}),
		Systems:      []string{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-haiku-4-5",
		Description: "Fast, cheap turns with no reasoning-effort axis: classification, extraction and short answers",
		Rank:        anthropicRank(5, BughuntInterpolated("claude-sonnet-4-6", BugHuntBenchFloor, "unmeasured; the lowest row of the registry order, tied with its dated snapshot claude-haiku-4-5-20251001"), 10, "the lowest of the nine anthropic rows, tied with its dated snapshot", anthropicLineup),
		Lifecycle:   LifecycleCurrent,
		Effort:      anthropicEffortNone(),
		Systems:     []string{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-haiku-4-5-20251001",
		Description:  "Dated snapshot of claude-haiku-4-5, for a run that must pin one exact build",
		Rank:         anthropicRank(5, BughuntInterpolated("claude-sonnet-4-6", BugHuntBenchFloor, "unmeasured; tied with claude-haiku-4-5, which it is a dated snapshot of"), 10, "the lowest of the nine anthropic rows, tied with claude-haiku-4-5, which it is a dated snapshot of", anthropicLineup),
		Lifecycle:    LifecycleLegacy,
		SupersededBy: "claude-haiku-4-5",
		Effort:       anthropicEffortNone(),
		Systems:      []string{"claude-code", "pi-native"},
	},
}

// AnthropicModels returns independent copies of this vendor's declarations.
func AnthropicModels() []Model { return cloneModels(anthropicModels) }
