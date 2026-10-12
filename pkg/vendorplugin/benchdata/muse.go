package benchdata

// boardRegistry is where the muse rows' facts were read from. It names the
// exact commit so a reader chasing a score or a context window has a revision
// to open rather than a moving target.
const boardRegistry = "skill-project-management tools/board-cli/internal/spawn/models.go (modelRegistrations, commit dbd905b9259fba229f623a560140a049c47a2a5c)"

// museCatalog146 is where the muse-wonder rows' context window and effort axis
// were read: the provider catalog Muse Code 1.4.6 (1.4.6-R6000.1) caches at
// ~/.local/share/muse/model-catalog/6d657461__p746268.json (schema_version 2,
// provider_id meta, profile_id tbh, source provider_catalog), read on the Mac
// mini on 2026-10-12. It is NOT boardRegistry: these rows were never on the
// board's table.
const museCatalog146 = "Muse Code 1.4.6 (1.4.6-R6000.1) provider catalog, ~/.local/share/muse/model-catalog/6d657461__p746268.json (schema_version 2, provider_id meta, profile_id tbh, source provider_catalog), read 2026-10-12"

// museWonderContextWindow is context_limit for both muse-wonder contributor
// rows in museCatalog146 (output_limit 128000). It is deliberately not the
// 1_048_576 the ported spark rows carry, and those rows are not retro-changed.
const museWonderContextWindow = 1_007_997

// MuseModels is THE data-only muse model list.
//
// # Why these facts have no vendor owner
//
// Because no vendor owns it. The frozen runtimeid table records muse's broker
// as looked-for and never established, and the board that declares the same
// rows says the same thing in its own words: they are "the only ones that may
// declare their own effort axis", because there is no plugin to read one from.
// The runtime adapter projects these facts onto its VendorUnresolved
// declaration; this leaf owns the declarations without loading that adapter.
//
// # The muse-wonder rows
//
// The board's table holds only the spark set (a contributor model, its alias
// and the contributor release it superseded); the wonder rows below are not
// part of it, and the shared board-quoting evidence on the spark rows is left
// byte-for-byte as it was.
//
// muse-wonder-1.0-contributor and muse-wonder-full-1.0-contributor (released
// 2026-10-12) are DECLARED HERE, not ported: the board's table never carried
// them, so they belong to the declared-here bridge and not to the frozen board
// capture. Like muse-spark-1.3 they are registered under their contributor
// harness ids only; the visible muse-wonder-1.0 and muse-wonder-full-1.0 ids
// stay unregistered. The standard row alone has a short alias, `muse-wonder`,
// in the shape of `muse-spark`; the full variant has none. Both are current, not Recommended (the spark rows keep the
// display pick and every spark/1.2 row is unchanged), muse-only, with the
// minimal..max axis recommending xhigh (the owner's decision 2026-10-12: max is offered but not recommended), read from the provider catalog
// (museCatalog146) as refreshed 2026-10-12 17:29Z: the first read of it listed
// no `max` tier for these rows, the refreshed one does, and the declaration
// follows the refreshed catalog. A word outside the axis is refused at admission.
//
// # Provenance, field by field
//
// PORTED from skill-project-management's board table: the model ids, the
// lineup states, the display recommendation, the context windows and the
// effort axis. pkg/vendorplugin/boardfacts_test.go pins every one of them
// against a frozen capture of that table, so a slipped digit fails rather than
// passes quietly.
//
// NOT PORTED ANY MORE: the capability SCORE. The board's PolicyRank 10 for
// every muse row is still quoted, because it is still true of the board, but
// the score is a Bug Hunt Bench point (bench.go): muse-spark-1.3-contributor
// is MEASURED at 33/105 at max, its alias carries the identity's number, and
// the legacy 1.2 row keeps its 10 as an interpolated value below the measured
// 1.3 and above the bench floor. bughunt_test.go holds each score against an
// independent transcription of the leaderboard.
//
// AUTHORED HERE, not ported: every Description, exactly as in the four vendor
// binding files. The board's rows carry a short display string and no
// what-is-this-model-best-for field, the model contract requires one and
// refuses a blank, and the board's own texts die with its half of this swap.
// They are the ONLY field here that is not a source fact, and they must never
// be cited as one.
//
// # The tie, and the row that left it
//
// muse-spark-1.3-contributor and muse-spark both score 33 and that is not a
// transcription accident: muse-spark is an ALIAS of the contributor row, so
// those two are the same model reached by two names and no observation could
// separate them. The bench measured that model at 33/105 at max (its high
// setting fixed 19, which is why the measured claim names max). The legacy 1.2
// row no longer ties them: the leaderboard measured no 1.2 run, so it keeps
// the 10 it was ported with as an INTERPOLATED value between the measured 1.3
// and the bench floor — a demotion is a lifecycle change, not a capability
// correction, and inventing a fresh number for a deprecated build nobody
// measured would be a capability observation nobody made. The presentation
// position that Lineup derives is declaration order for the tied pair and
// carries no claim, which is what RankedModel.Tied reports.
//
// # The 1.3 effort axis, and the recommendation it carries
//
// muse-spark-1.3-contributor and its alias declare EffortSupportRequired over
// high/xhigh/max, recommending max. That vocabulary is the MODEL's, from the
// Muse Spark 1.3 API, and this package owns the model half of the effort
// contract by design (see EffortDeclaration).
//
// The recommendation is the bench's, not a taste: the leaderboard measured
// this model at 33/105 at max against 19 at high (see the tie note above), so
// `max` is the setting the score was observed at and the one an operator
// following the recommendation reproduces. It also matches the consumer's
// policy — normal board-managed work uses `max` as its highest recommendation
// (skill-project-management `.specs/spawn-model-selection.md`) — rather than
// steering muse launches one step below every other runtime's head.
//
// The installed harness used to be behind the vocabulary, and the history is
// worth keeping because the bound it states still holds. `muse 1.0.2`
// (1.0.2-R2040.1) documented `--reasoning-effort <EFFORT>` as
// none|minimal|low|medium|high|xhigh|ultra, default high: it had `xhigh` but no
// `max`, so a launch configured at `max` was admitted by this declaration,
// transported verbatim by the muse system plugin, and REFUSED harness-side by
// that CLI. `muse 1.3.0` (1.3.0-R3057.1) documents
// none|minimal|low|medium|high|xhigh|max|ultra, default high, and accepts the
// word, so the refusal is gone on current harnesses. The shape stays deliberate
// either way: `max` is API truth, the transport is a pass-through by invariant
// 4 of docs/architecture.md, and a plugin that enumerated the CLI's vocabulary
// to pre-refuse it would be putting a harness build number in the layer that
// must not hold one. Narrowing the MODEL's vocabulary to what 1.0.2 accepted
// would have been the same mistake stated in this file instead.
func MuseModels() []Model {
	source := RankEvidence{
		Source:      boardRegistry,
		Observation: "every muse row carries PolicyRank 10 on the board's own per-vendor scale (the score is the bench value, not that number), the only number the table gives this runtime, and the set is a contributor model, its alias and the contributor release it superseded",
	}
	// The bench claims, typed separately from the scores below so that
	// bughunt_test.go can hold the two against each other.
	// The leaderboard spells the model `muse-spark-1.3`, without the
	// harness suffix, and both rows say so: the contributor row because that
	// is the spelling its count was recorded under, the alias because it is
	// that same model.
	measured13 := BughuntMeasuredAs("muse-spark-1.3", "max", 33)
	interpolated12 := BughuntInterpolated("muse-spark-1.3-contributor", BugHuntBenchFloor, "the leaderboard measured no 1.2 run; the ported 10 is kept below the measured 1.3")
	alias := RankEvidence{
		Source:      "the rows' own ids and the board's descriptions of them",
		Observation: "muse-spark is recorded as an alias of muse-spark-1.3-contributor rather than as a second model, so the equal scores are an identity rather than a judgement nobody could defend",
	}
	// 1_048_576 is the board's own figure for every muse row, the same 1M-token
	// window its google rows carry. It is transcribed, not derived from a
	// vendor page: no vendor was ever established for this runtime, so there
	// is no page to derive it from.
	const contextWindow = 1_048_576
	// The 1.3 axis, declared once and shared by the versioned row and its
	// alias: they are one model reached by two names, so a second literal here
	// would be a second place for the vocabulary to drift.
	//
	// `max` is in this vocabulary and was NOT in muse 1.0.2's
	// `--reasoning-effort` set (none|minimal|low|medium|high|xhigh|ultra). See
	// this function's doc comment: the word is API truth, it passes through the
	// system plugin verbatim, and the refusal it earned on that build was the
	// harness's. muse 1.3.0 accepts it.
	spark13Effort := func() EffortDeclaration {
		return EffortDeclaration{
			Support:     EffortRequired,
			Vocabulary:  []string{"high", "xhigh", "max"},
			Recommended: "max",
		}
	}
	// The wonder axis, shared by both wonder rows like spark13Effort above.
	// minimal..max in the provider catalog's order (refreshed 2026-10-12
	// 17:29Z). Recommended is xhigh by the owner's decision, though max is offered.
	wonderEffort := func() EffortDeclaration {
		return EffortDeclaration{
			Support:     EffortRequired,
			Vocabulary:  []string{"minimal", "low", "medium", "high", "xhigh", "max"},
			Recommended: "xhigh",
		}
	}
	// The wonder rank. The leaderboard measured no wonder run, so the score is
	// INTERPOLATED, not measured: strictly inside claude-fable-5-1 (measured 43
	// at max) above and the muse-spark-1.3-contributor row (measured 33 at max)
	// below. 34 is the lowest value strictly inside that interval; it places the
	// newer release directly above its predecessor in the same line and asserts
	// no measured gap. Both wonder rows share it: the leaderboard cannot
	// separate them, no source says either is stronger, and equal scores are
	// legal ties (RankedModel.Tied reports them), not a claim of equal ability.
	wonderRank := func() CapabilityRank {
		return CapabilityRank{Score: 34, Basis: []RankEvidence{
			BughuntInterpolated("claude-fable-5-1", "muse-spark-1.3-contributor", "unmeasured; a later release of the muse line placed just above the measured muse-spark-1.3 (33) and below the measured claude-fable-5-1 (43), with the score only its position, not a measurement"),
			{
				Source:      museCatalog146,
				Observation: "released 2026-10-12, later than muse-spark-1.3 (2026-09-02); the catalog publishes no capability score and the bench has no run of either wonder id",
			},
		}}
	}
	return []Model{
		{
			ID:                  "muse-spark-1.3-contributor",
			Description:         "The Muse Spark 1.3 contributor harness: a local-first runtime whose broker this module has looked for and never established; pick it only where that unresolved binding is acceptable",
			Rank:                CapabilityRank{Score: 33, Basis: []RankEvidence{measured13, source, alias}},
			Lifecycle:           LifecycleCurrent,
			Effort:              spark13Effort(),
			Recommended:         true,
			ContextWindowTokens: contextWindow,
			Systems:             []string{"muse"},
		},
		{
			ID:          "muse-spark",
			Description: "The short alias of muse-spark-1.3-contributor, for an invocation that spells the runtime's model without its version",
			// The alias is DECLARED here, not inferred from the shared prefix
			// or the equal score. Until it was, a spawn of `muse-spark` put
			// that spelling straight into muse's argv and the backend refused
			// the run — "model muse-spark does not exist or you lack access" —
			// while the identical launch under the contributor id succeeded.
			// agentic.BuildPlan substitutes the target before argv; this row
			// stays admissible, rankable and auditable under its own id.
			AliasOf:             "muse-spark-1.3-contributor",
			Rank:                CapabilityRank{Score: 33, Basis: []RankEvidence{measured13, source, alias}},
			Lifecycle:           LifecycleCurrent,
			Effort:              spark13Effort(),
			ContextWindowTokens: contextWindow,
			Systems:             []string{"muse"},
		},
		{
			ID:           "muse-spark-1.2-contributor",
			Description:  "Previous-generation Muse Spark contributor harness, for a run pinned to it; prefer muse-spark-1.3-contributor for new work",
			Rank:         CapabilityRank{Score: 10, Basis: []RankEvidence{interpolated12, source, alias}},
			Lifecycle:    LifecycleLegacy,
			SupersededBy: "muse-spark-1.3-contributor",
			// EffortSupportNone, kept exactly as ported. 1.2 has no
			// reasoning-effort axis and gaining one retroactively because its
			// successor has one would be inventing a capability for a
			// deprecated build nobody measured.
			Effort:              EffortDeclaration{Support: EffortNone},
			ContextWindowTokens: contextWindow,
			Systems:             []string{"muse"},
		},
		{
			ID:          "muse-wonder-1.0-contributor",
			Description: "The standard Muse Wonder 1.0 contributor harness: a local-first runtime whose broker this module has looked for and never established, the same unresolved muse binding as the spark rows; pick it only where that binding is acceptable",
			Rank:        wonderRank(),
			Lifecycle:   LifecycleCurrent,
			Effort:      wonderEffort(),
			// NOT Recommended: muse-spark-1.3-contributor keeps the display pick.
			ContextWindowTokens: museWonderContextWindow,
			Systems:             []string{"muse"},
		},
		{
			ID:          "muse-wonder",
			Description: "The short alias of muse-wonder-1.0-contributor, for an invocation that spells the runtime's model without its version; there is no alias for the full variant",
			// Declared with AliasOf for the reason muse-spark is: until it is,
			// the short spelling goes straight into muse's argv and the backend
			// refuses it. agentic.BuildPlan substitutes the target before argv;
			// this row stays admissible, rankable and auditable under its own
			// id, and it carries the identity's score and axis because it is
			// the same model reached by two names.
			AliasOf: "muse-wonder-1.0-contributor",
			Rank: CapabilityRank{Score: 34, Basis: append(wonderRank().Basis, RankEvidence{
				Source:      "the rows' own ids",
				Observation: "muse-wonder is recorded as an alias of muse-wonder-1.0-contributor rather than as a second model, so the equal scores are an identity rather than a judgement nobody could defend",
			})},
			Lifecycle:           LifecycleCurrent,
			Effort:              wonderEffort(),
			ContextWindowTokens: museWonderContextWindow,
			Systems:             []string{"muse"},
		},
		{
			ID:                  "muse-wonder-full-1.0-contributor",
			Description:         "The larger Muse Wonder Full 1.0 contributor harness, the full-size variant of muse-wonder-1.0-contributor, on the same unresolved muse binding as the spark rows; pick it only where that binding is acceptable",
			Rank:                wonderRank(),
			Lifecycle:           LifecycleCurrent,
			Effort:              wonderEffort(),
			ContextWindowTokens: museWonderContextWindow,
			Systems:             []string{"muse"},
		},
	}
}
