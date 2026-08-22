# TASK-260822-3cknas review verdict — ACCEPTED

Reviewer run `RUN-260822-0679ea`. Change Request `CR-TASK-260822-3cknas-1` revision 1.
Candidate tree `96b28ffe0511252ed24c02542b5f9a7f6c4783f1`, base `3e896b1`, branch
`task-board/story/STORY-260821-3b4ewr`. Working tree re-hashed at the start and end of
review: `96b28ff` both times, so nothing below was measured against a tree the producer
did not hand over. Every mutation in this review was made in a scratch copy under
`.temp/TASK-260822-3cknas-review/scratch`; the extraction source checkout
(`~/src/relux-works/skill-project-management`, `ed48781`) was read-only throughout and
`git status` there is still clean.

## The design decision the brief said to examine hardest

**Admitted-set membership does not read capability ranks. Verified structurally, by
mutation, not by reading.**

- `grep` over `pkg/vendorplugin/*.go` (non-test): `admission.go` and `v2snapshot.go`
  contain no read of `CapabilityRank` or `Rank.Position`. The only production reader of
  `Rank.Position` is `registry.go:202`, and it uses it for per-vendor uniqueness
  validation, never for membership. `EffortRank` is a different thing — the shared
  effort scale — and is not a capability rank.
- **Rank mutant (must NOT move the digest):** swapped openai positions 1 and 12
  (`gpt-5.6-sol` ↔ `gpt-5.1-codex-mini`) in a scratch copy. Both digests bit-identical:
  codex `sha256:0e8a5455…`, claude `sha256:604689aa…`. The suite reds on exactly one
  test — `TestCapabilityRankOrderingMatchesTheSourceScores` — and on none of the digest
  tests. That is the right shape: rank drift is caught as rank drift and cannot reach
  admission.
- **Tier mutant (must move the digest):** swapped `gpt-5.6-sol` and `gpt-5.6-terra` in
  `v2RolloutTiers`. codex digest moved to `sha256:9db6449e…` and the pair count fell
  29 → 27. Membership genuinely comes from the frozen table.

Not blocking. The separation is real.

## 1. The full-set pin — re-derived independently

I did not read the producer's table or trust its fixture. I wrote my own parser over
`skill-project-management tools/board-cli/internal/spawn/models.go` at `ed48781`,
resolved the vocabulary variables from `pkg/remoteconfig/reasoningeffort` and the broker
bindings from `pkg/remoteconfig/runtimeid/builtins.go`, and dumped the port's registry
with my own Go program driving `vendorplugin.Default` through the four plugins' real
registration path.

- Source rows: **43**. By broker: google 15, openai 12, anthropic 8, alibaba 6, and 2
  with `Broker: BrokerUnknown` (the muse pair). 41 + 2 = 43 confirms.
- Port rows: **41**, split google 15 / openai 12 / anthropic 8 / alibaba 6.
- Mechanical diff of all 41 on id, vendor, agentic systems, effort support, effort
  vocabulary and recommended effort: **zero disagreements, zero missing, zero extra**.
- Runtime scoping through `RuntimeModels`: gemini 7, agy 8, qwen 5, codex 12, claude 8,
  plus qwen-codex 1 = 41. The single alibaba row bound to the `codex` harness does NOT
  leak into runtime `codex`'s set (12, not 13), which is what keeps two runtimes over one
  vendor apart.
- The checked-in fixture `testdata/source-model-registry.json` also matched my own
  extraction field-for-field on all 43 rows, so the pin will keep detecting drift rather
  than describing a table that has moved.

The pin's own gate is not delete-only: `TestTheFullSetPinFiresOnDrift` drifts the source
table by one field six ways (row invented, row dropped, vocabulary narrowed by one word,
recommendation moved, model rebound to the other harness of the same vendor, effort axis
appearing where the source has none) and requires the pin to name the exact row. I
confirmed the comparison is bidirectional — covering only the source would admit an
invented model, grounding only the port would admit a dropped one; `comparePort` does
both.

## 2. Digests — reproduced from the source binary myself

I built `skill-project-management`'s own CLI into scratch and ran
`env -u TASK_BOARD_DIR … q 'project_config()'` from inside that checkout:

| ceiling | source binary | pinned fixture | this port |
| --- | --- | --- | --- |
| codex (`gpt-5.6-sol` / `less_or_equal` / `medium`) | `sha256:0e8a5455bd71…eccda7c` | same | same |
| claude (`claude-opus-5` / `less_or_equal` / `high`) | `sha256:604689aa7954…6929757` | same | same |

`config_sha256` matched the fixture's `sha256:3607d48f…`, and the projected `config_path`
was the source repository's own file — the capture script verifies that and refuses
otherwise, which the LOGBOOK records as a real near-miss (`TASK_BOARD_DIR` had silently
redirected an earlier capture at the source's own config).

**The digest can move.** Dropping `gpt-5.4-mini` from the openai rows in scratch did not
produce a quietly-narrower digest — it produced a hard refusal
(`ErrV2Unexpandable: the snapshot admits "gpt-5.4-mini" … and vendor openai does not
register it`), and reds 8 tests including the pin. Three further narrowing mutants on
`admission.go` production code were all killed:

| narrowed gate | mutation | killed by |
| --- | --- | --- |
| unplaceable effort bound admits nothing | `return nil` → `return supported` | `TestExpandRefusesRatherThanNarrowing`, `TestEffortsUnderBoundRefusesToWidenOnAnUnplaceableBound` |
| no-effort model admits the empty pair | `return []string{""}` → `return nil` | the digest pin + 4 more |
| unplaced vocabulary word kept in the digest | tail comparator → `false` | `TestSortEffortsKeepsAWordTheScaleCannotPlace` |

I also confirmed byte-fidelity of `CanonicalSerialization` (provider line, tab, efforts
comma-joined, trailing newline) against the source's `spawn_admission.go`, and that
`v2RolloutTiers` is byte-identical to the source's `v2AdmissionSnapshotRolloutTiers()`.

## 3. Rank basis — no rank without observation

Recomputed the expected ordering myself: per vendor, source `PolicyRank` descending, ties
broken by the source's declaration order. **All 41 positions match, all four vendors are
`1..n` with no gaps or duplicates, and every rank's basis carries the source score
verbatim** (`the row carries PolicyRank N, …`) plus a second vendor-lineup observation.

Tie pairs spot-checked, all three kinds:

- `claude-haiku-4-5` (#7) / `claude-haiku-4-5-20251001` (#8), both source score 10 — and
  the frozen tier puts both ids in ONE tier, so the derived position split does not narrow
  the alias pair's admission. `TestAliasTierAdmitsBothDirections` holds that.
- `qwen3.7-plus` (#3) / `qwen3.7-plus-via-codex` (#4), both 30, each basis naming the
  other.
- `gemini-3.1-pro-preview` (#3, gemini-cli) / `gemini-3.6-flash-high` (#4, antigravity),
  both 50 — one of five google cross-harness collisions the LOGBOOK names (50/45/40/35/30).
  I verified all five exist in the source.

The rank test is not vacuous: the rank-swap mutant above reds it and nothing else.

## 4. Authored-here descriptions

- Marker mutant: replacing the note in `vendors/google/models.go` reds
  `TestUsageDescriptionsAreMarkedAuthoredHere` naming the file and the missing string.
- Sampled well beyond three. They are operator guidance, not restated model names:
  `gpt-5.4-mini` → "Small and cheap: simple edits, boilerplate and mechanical work";
  `claude-sonnet-5` → "Routine work at lower cost: scoped edits, tests and summaries where
  Opus-level judgement is not the bottleneck"; `gemini-3.1-flash-lite` → "Low-latency,
  high-volume lightweight work: classification, extraction, short answers". The
  `qwen3.7-plus-via-codex` row is the honest one — it says outright that it is wiring
  evidence rather than a model to choose, and that no vendor-captured Alibaba-via-codex id
  has ever been observed.

## 5. Muse rows — named, counted, reachable

Not silently absent. `TestEverySourceRowIsAccountedFor` names both ids and requires the
source table's unresolved-broker rows to be exactly those two. At runtime, with the muse
agentic system compiled in, `ResolveRuntime("muse")` returns
`ErrRuntimeVendorUnresolved`: *"runtime muse was recorded with an unknown broker after
checking [skill-project-management pkg/remoteconfig/runtimeid (frozen table)]; nothing
here will guess one, because the binding keys limit state."* A checked-and-empty finding
carried as a typed refusal that names what was checked — which is what the negative-
evidence rule asks for, and the opposite of inferring a vendor from a proxy signal.

## 6. Guard — one binding file per vendor

Three mutants, all killed:

- A second file in `vendors/anthropic/` holding `[]agentic.SystemID{"claude-code"}` →
  `TestSingleSourceGuardFindsNoSecondBinding` reports it by file, line and class.
- The `v2snapshot.go` claim that the tier table "written as the `map[RuntimeID][][]string`
  it obviously wants to be … fails the guard" is TRUE: adding one reds the guard naming
  `pkg/vendorplugin/registry.go` as the only legal home.
- Pointing two vendors at one binding home reds `TestEveryVendorHasExactlyOneBindingFile`
  on both the shared-file rule and the `bindingHomes` mismatch.
- The producer's own weakest mutant ("an id-spelling home keyed as a type name") was
  killed in their log by a *compile* error, which proves little. I re-ran it properly with
  a non-duplicate identifier key (`"BrokerID"`) and
  `TestSingleSourceGuardHomesSplitByKind` reds correctly.

`TestSingleSourceGuardRulesFireOnRealCode` displaces every home and requires each vendor
file's real table to be reported, so a green run cannot mean "the rule never matched".

## Gates — run in the foreground on the candidate tree

| gate | result |
| --- | --- |
| `make vet` | clean, exit 0 |
| `make build` | exit 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | all packages ok, no failures |
| `gofmt -l pkg/ internal/` | no output |

## Carry-forward note (not blocking, not a defect in this delta)

`V2AdmittedModels` REFUSES a v2 ordered ceiling naming a runtime the frozen snapshot never
captured (gemini, agy, muse, qwen-codex). The extraction source's production adapter
(`tools/board-cli/internal/spawn/admission_registry.go:69-90`) instead FALLS BACK to the
live registry for such a provider. I verified this against the source binary rather than
by reading it: a scratch config carrying `ceilings.gemini = {model: gemini-3.5-flash,
model_criterion: less_or_equal}` makes the source emit a `v2_snapshot` admission of all
seven live gemini rows — note that its fallback discards the bound entirely — while this
port refuses the same ceiling.

This does not touch AC4: the source repository's own config carries only codex and claude
ceilings, both captured in the snapshot, and both digests reproduce byte-exactly. The port
states the choice explicitly in `v2snapshot.go` and its refusal is the safer of the two
behaviours. It is recorded here so the config/preflight port story meets it as a known,
argued divergence rather than as a surprise regression.

## Verdict

All five acceptance criteria met, every gating behaviour attacked rather than read, and
every attack that should have broken something did. Definition of Done satisfied:
full-set pin, rank basis with source evidence, authored-here descriptions with a marker
test, digests captured from the source binary and byte-stable, one binding file per
vendor enforced by the guard, tests green, lint clean, build unbroken, outcome artifacts
on the board, LOGBOOK and README updated, work uncommitted.

**ACCEPTED.** Reviewer-archetype run supplies no `commit_ack`; the commit-owning mover
commits the scope and makes the `done` transition.
