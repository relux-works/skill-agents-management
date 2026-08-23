# TASK-260824-y7gyco — extend-model-rows-with-the-board-facts

Module half of single-sourcing the model facts. **Work is UNCOMMITTED and NOT
tagged**; `v0.2.0` is the release act and belongs after acceptance.

## What moved

The vendor `Model` row now carries every fact the board's `modelRegistrations`
still owned. FACTS moved; POLICY (the frozen v2 tier table, the configured
ceilings) stayed on the board, untouched.

| Fact | Shape here | Empty value means |
| --- | --- | --- |
| capability rank | `CapabilityRank.Score int`, **ties legal** | zero refused — an unplaced row is not the bottom of the scale |
| lineup state | `Model.Lifecycle` (`current`/`preview`/`legacy`) | no legal empty — blank and unlisted both refused |
| supersession | `Model.SupersededBy ModelID` | no unambiguous same-family replacement is recorded |
| display pick | `Model.Recommended bool` | `false` is a real value, not an unset one |
| context window | `Model.ContextWindowTokens int` | `0` = none recorded; NOT a window of zero |
| billing | `Model.Pricing *Pricing` | `nil` = none registered; NOT free use |
| muse's two rows | `RuntimeDeclaration.Models`, legal only when `Vendor == VendorUnresolved` | — |

## The rank design question, as decided

`CapabilityRank.Position` became `CapabilityRank.Score`. The board's scores
carry genuine ties and its ranking consumers read them; a position numbering two
equal models 7 and 8 asserts an ordering nobody observed. The declaration is now
the score with ties intact, and the total order some callers need is DERIVED by
the new `vendorplugin.Lineup` — score descending, ties broken by declaration
order, positions 1..n, every tied row marked `Tied` so a caller cannot mistake
presentation for evidence. The `Basis` requirement did not move; it was never
about the position.

Three real ties survive the port and are named (not merely counted) in
`TestTheSourceTiesSurviveThePort`: `anthropic`'s alias pair, `alibaba`'s
cross-runtime mirror, and five `google` gemini-cli/antigravity pairs.
`openai` is asserted to have NONE — checked, not skipped.

`Registry.Register` no longer refuses two models at one rank (a tie is a
statement). It gained refusals that are real gates: two display picks for one
agentic system, a successor no model in the lineup answers to, a non-legacy row
that has already been replaced, a self-successor, a negative context window, and
a billing contract that is unquotable or does not price its own model.

## muse under the vendor-unresolved shape

Extended deliberately, not special-cased. `RuntimeDeclaration` grew `Models`,
legal ONLY when the vendor is unresolved; a RESOLVED runtime declaring rows is
refused as "two declarations of one fact". The rows are held to exactly the
standard a registered vendor's rows are (`Model.Validate` plus the two rules
only a declaration can state: the row must name the runtime's own harness, and
ids must be unique), and they survive `SeedFrozenRuntimes` deeply copied. The
runtime stays UNLAUNCHABLE — `ResolveRuntime` still returns
`ErrRuntimeVendorUnresolved`.

The single-source guard gained one entry: `"muse models"` → `pkg/vendorplugin/runtime.go`.
`TestSingleSourceGuardRulesFireOnRealCode` now requires that file to be reported
under displacement, so the new home is a permission the guard can see being used
rather than a silent grant.

## The transcription pin

`pkg/vendorplugin/testdata/board-model-facts.json`, captured by
`.scripts/capture-board-model-facts.sh` from the **board binary's own**
`q 'models()'` projection at commit `dbd905b9259fba229f623a560140a049c47a2a5c`.
43 rows. Read by `pkg/vendorplugin/boardfacts_test.go`.

Two deliberate properties:

- **Circularity is segregated, not assumed away.** The projection joins the
  board's rows back to this module's vendor plugins, so `agenticSystems`,
  `reasoning`, `supportedEfforts` and `recommendedEffort` come BACK from here
  for every row with an established broker. The fixture parks them under
  `joined_from_vendor_module` and the pin does NOT compare them — those stay
  pinned against the older pre-join capture. The two `muse` rows are the
  exception the board itself names: no vendor owns them, so their effort axis is
  the board's own declaration and IS pinned.
- **It is TRANSITIONAL and says so in machine-readable form.**
  `TestTheBoardFixtureDeclaresItsOwnMortality` fails if the provenance stops
  saying `TRANSITIONAL` / `dies with the board` / `delete this fixture`. When
  the board reads these facts from here, the fixture and its test are DELETED,
  not regenerated.
- Descriptions are **not captured at all**. This module authors its own; a copy
  of the board's texts in testdata would invite citing one as a ported fact.

**Corroboration.** `TestTheBoardFixtureAndTheOlderSourceCaptureAgree` holds the
new binary-derived capture against the older source-text capture at `ed48781` on
every column they share — 43 ids, 43 scores, 43 lifecycles, 5 supersessions, 6
recommendations, 43 runtimes. They agree. Two captures taken by different means
at different commits is what makes a stale one visible; one capture cannot
notice.

## Digest byte-stability, proved rather than asserted

`pkg/vendorplugin/digeststability_test.go`. Eight mutants apply a presentation
change to EVERY row of EVERY vendor and re-expand the real frozen ceilings; the
canonical serialization must come back byte-identical and the digest must still
equal the value the board's own binary printed. Every row marked legacy, every
supersession cleared, every recommendation withdrawn, every row given a context
window, every price doubled, every contract dropped, every score flattened,
every description replaced.

That claim would be satisfied by a serializer covering nothing, so
`TestTheDigestStillMovesOnTheFactsItCovers` narrows it: a respelled model id and
a narrowed effort vocabulary MUST move the digest.
`TestTheCanonicalSerializationCarriesOnlyPairs` reads the bytes and refuses any
lifecycle word, currency, URL or retrieval date in them.

## Regress net: a fifth class

`internal/regress/modelfacts_test.go`. Cross-cutting and cheap
(`make regress` = 0.4s):

- both homes add up to 43, no row in both, every row passes `Validate` — a
  vendors-only walk would report green on 41 of 43 forever;
- the two alias ties (one per home) are still equal scores;
- correcting a lifecycle / recommendation / score / price cannot move an
  admitted set, paired IN THE SAME TEST with a vocabulary narrowing that must.

`internal/regress/doc.go` documents the fifth class and the incident behind it.

## Mutation evidence

`python3 .temp/TASK-260824-y7gyco/mutants.py` → `mutants-01.log`.
**21 mutants, 21 killed, 0 survivors**, each naming the test that killed it.

Every mutant is a compile-clean **NARROWING**, not a deletion — a first pass
used `if false` blocks and three of them died on a Go "declared and not used"
build failure, which proves the line exists and nothing about the class it
covers. Those were rewritten (`|| true`, `&& false` on a used condition,
`score-1`, `len(...) > 99`, a regexp relaxed to `.`) so every kill is a test
failing on behaviour.

**One real survivor was found and fixed.** Narrowing the "a contract with no
plans" refusal left the suite green: with `Plans` nil the loop never runs, so
`pricesThisModel` stays false and the *names-this-model* refusal fires instead —
same sentinel error, different fact. `TestRegisterRefusesAnUnusablePricingContract`
now states the phrase each of its 19 cases must see in its own refusal, and
asserts the case count matches the reason count so a case cannot be added
without one. That mutant is now killed.

## Gates (all foreground, real exit codes)

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `make regress` | 0 (0.4s) |
| `gofmt -l pkg/ internal/` | 0, no output |
| `python3 .temp/TASK-260824-y7gyco/mutants.py` | 0, 21/21 killed |

## Docs

`README.md` (ported-facts list, the rank section, digest byte-stability, the
pending-`v0.2.0` breaking-change note, two new tools rows), `SKILL.md`,
`docs/architecture.md` (the model-list contract), `docs/shipped-state.md` (both
residues of the consumer swap recorded as CLOSED, with the note that the first
was a design defect this layer owned rather than a scheduling gap),
`docs/consuming-the-module.md` (the `v0.2.0` migration).

## Found while working, fixed, outside the task's scope

`docs/consuming-the-module.md` and `SKILL.md` still told a consumer this is a
**private** module needing `GOPRIVATE` and a `url.insteadOf` PAT rewrite. That
went stale on 2026-08-23 when the repo went public and the credential half was
retired — and it is worse than merely stale: the first consumer's own
`ciguard` now enforces the ABSENCE of that plumbing, so a reader following the
doc would fail their build. Corrected in both, plus one dangling
`tag/GOPRIVATE/go.work` phrase in `README.md`. Flagged rather than silently
folded in.

## Not done, deliberately

- **No commit, no tag.** `v0.2.0` is the release act and belongs after review
  accepts. `git tag --list 'v0.2*'` is empty.
- The board's own half — deleting its `modelRegistrations` columns and reading
  them from here — is the consumer task, not this one.
