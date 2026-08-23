# TASK-260824-y7gyco — Review verdict: ACCEPTED

Reviewer run `RUN-260823-bc9847`. Change Request `CR-TASK-260824-y7gyco-1` revision 1.

## Candidate under review

Working tree recomputed independently and matches the declared candidate exactly:

```
$ GIT_INDEX_FILE=... git read-tree HEAD && git add -A && git write-tree
1bb50d66b8685abad86b32350903398c79dbde6e   == declared candidate tree
```

Base `92e6d8b7…`, branch `task-board/story/STORY-260824-2x7gmp`, 30 changed paths,
+4603/−325. `git log --oneline -1` is still the base commit: the work is
UNCOMMITTED as AC5 requires. `git tag --list 'v0.2*'` returns 0 rows — no tag was
minted ahead of acceptance.

Board checkout `/Users/alexis/src/relux-works/skill-project-management` was read
only. Its `git status --short` before and after my capture run is unchanged, and
`tools/board-cli/internal/spawn/models.go` never appears in it. Every mutation I
made was in this worktree, reverted, and the tree OID re-verified back to
`1bb50d66…` after each batch.

## Gates — all foreground, all green

| Gate | Result |
| --- | --- |
| `gofmt -l pkg/ internal/` | clean (no output) |
| `make vet` | exit 0 |
| `make build` | exit 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 24 packages ok, 0 FAIL |
| `make regress` | ok, 0.43s |

## What I attacked, not read

### 1. Fixture provenance — re-captured and byte-diffed

The claim is that `testdata/board-model-facts.json` came from the board BINARY at
commit `dbd905b`, not from a source parser. Verified by re-running the producer's
own script into a scratch tree that cannot touch the candidate:

```
$ git -C <board> rev-parse HEAD          -> dbd905b9259fba229f623a560140a049c47a2a5c  (matches provenance)
$ shasum -a 256 <board>/tools/board-cli/internal/spawn/models.go
  c3dd57e5…8cd4c5                        (matches provenance.registry_sha256 exactly)
$ .temp/review-recapture/.scripts/capture-board-model-facts.sh --source <board>
$ diff <recaptured> pkg/vendorplugin/testdata/board-model-facts.json
  IDENTICAL
```

The circularity handling is sound and non-trivial: the board's `q 'models()'`
projection JOINS four columns back out of this module, and the fixture segregates
them under `joined_from_vendor_module` and does not pin them. The two `muse` rows
are the stated exception (no vendor owns them, so their effort axis is the board's
own declaration) and ARE pinned. A port pinned against its own output would have
proved only self-agreement; this one does not do that.

The fixture header states TRANSITIONAL / "dies with the board" / "delete this
fixture", and `TestTheBoardFixtureDeclaresItsOwnMortality` fails if any of those
three phrases is removed. Corroboration is real too: a second, older capture taken
by a different means at an earlier commit is cross-checked on every shared column
(`TestTheBoardFixtureAndTheOlderSourceCaptureAgree`).

### 2. The transcription pin's teeth — mutated the DECLARATIONS, not the fixture

The producer's own mutants drift the fixture. I drifted the other side — the
shipped declarations — which is the direction a real transcription slip takes.
Three at once, each red with the exact row and field named:

```
model "gpt-5.3-codex" is "current" and the board records "legacy"        (lifecycle flip)
model "qwen3.7-plus"'s plan "pro" lists 110/month and the board lists 100 (price digit)
model "gemini-3.1-pro-high" scores 61 and the board scores it 60          (score)
```

A fourth, on the one field with no producer mutant behind it:

```
model "qwen3.7-plus"'s contract states frequency limits=true and the board states false
```

Also worth recording: my first lifecycle mutant (`claude-opus-4-8` legacy→current)
never reached the pin — it was refused earlier by the new supersession-consistency
gate, at registration, with a named error. That is a stronger gate firing, not a
weaker pin.

### 3. Digest discipline — planted an actual leak

Rather than trust the property test, I made a new field reach the hash: added
`Lifecycle` to `AdmittedModel`, populated it in `ExpandV2Ceiling`
(`v2snapshot.go:283`) and wrote it into `CanonicalSerialization`
(`admission.go:208`). Both guards fired —
`TestThePresentationFieldsDoNotReachTheDigest` on all 8 subtests, and the
structural `TestTheCanonicalSerializationCarriesOnlyPairs`. The narrowing half is
there too: `TestTheDigestStillMovesOnTheFactsItCovers` proves the digest is not a
constant. Digests remain byte-stable against the board binary's own pinned values.

### 4. The muse representation — both directions

- Rows on a RESOLVED runtime: refused, `ErrRuntimeInvalid`, refusal names the
  runtime, the vendor and "two declarations of one fact"
  (`TestDeclaringModelsOnAResolvedRuntimeIsRefused`), with the "drop the rows and
  the same declaration is legal" control beside it.
- Rows on an unresolved runtime go through the SAME `Model.Validate`, not a weaker
  path: 12 planted-invalid cases, including a blank lifecycle, a zero score, an
  evidence-free score, a negative context window and an unquotable billing
  contract — every one refused, plus the two rules only a declaration can state
  (row must name the runtime's own harness; ids unique).
- The production call site is named and driven: `registry.go:63` →
  `SeedFrozenRuntimes` → `RegisterRuntime` → `Validate`
  (`TestSeedingCarriesTheUnresolvedRuntimesRowsThroughTheRegistry`), which also
  confirms muse stays UNLAUNCHABLE (`ErrRuntimeVendorUnresolved`) and its rows are
  deep-copied out.

### 5. The API break — blast radius inside the module

`git grep 'Rank.Position' 92e6d8b` gives the full old-consumer set: the registry
uniqueness refusal, the four vendor binding files, and six test files. There was
no production CLI or regress consumer of `Position`. All migrated;
`ErrDuplicateRank` and `seenRanks` are gone from the codebase with no dangling
reference — so no leftover position-uniqueness refusal survives to reject a legal
tie. Confirmed by `TestRegisterAdmitsTwoModelsAtOneScoreAndTheDerivedOrderStaysTotal`.

Ties survive the port NAMED, and the naming is more complete than the brief
assumed. The board's score is per-RUNTIME; this module's lineup is per-VENDOR, so
merging the two google harnesses' scales produces five collisions, not one, plus
one on alibaba. `TestTheSourceTiesSurviveThePort` names all eight pairs
independently of the fixture and asserts its own vendor coverage
(`len(ties) == len(portedVendors)`), so a vendor cannot be silently skipped.
openai's absence of ties is an explicit empty entry — checked, not skipped.

`Lineup`'s order is deterministic by construction: the declaration index travels
with each row, so the tie break does not rest on `sort.SliceStable`'s promise.
Tested over a shuffled input with a three-way tie, positions 1..n with no gaps,
`Tied` asserted per row, and no aliasing back into the declaration. Basis is still
required on every score, and `TestEveryRankCarriesTheSourceEvidence` holds the
basis to the source's actual number rather than to non-emptiness.

### 6. Narrowing mutants I wrote myself

Six compile-clean narrowings of the new gates, run against the whole suite:

| Mutant | Outcome |
| --- | --- |
| `promotional >= list` → `> list` | killed — `…/a_promotion_equal_to_the_list_one` |
| `Lifecycle.Validate` admits blank | killed — 2 named subtests |
| `ContextWindowTokens < 0` → `< -1` | killed — 2 named subtests |
| `checkSupersession` skips prefix-sharing successors | killed — 2 named subtests |
| supersession contradiction narrowed to `preview` only | killed — `…/a_current_model_that_has_already_been_replaced` |
| `checkRecommendations` inspects only `model.Systems[:1]` | **SURVIVED** — see finding 1 |

## Findings — none blocking, all recorded as follow-ups

**1. `checkRecommendations` is unproven beyond each row's first agentic system.**
Narrowing the loop at `pkg/vendorplugin/vendor.go:700` to `model.Systems[:1]`
leaves the ENTIRE suite green (verified the edit landed before trusting the
result). The gate itself is correct; what is missing is a negative test where a
`Recommended` row drives more than one system and the collision falls on a
non-first one. Live exposure today is nil — no shipped row declares more than one
system, and the cross-runtime case is modelled as a separate row
(`qwen3.7-plus-via-codex`, `Systems: {"codex"}`) rather than a two-system row. It
is the one dimension the producer's own 21-mutant pass did not cover, and it
should get a case in `TestRegisterRefusesTwoRecommendationsForOneSystem` before
anything ships a multi-system row.

**2. The type doc and the google binding file describe a tie differently.**
`CapabilityRank.Score` says "A tie is a statement that the two are equal", while
`vendors/google/models.go` says its five ties are "NOT claims that the two rows
are interchangeable — they are two harnesses' independent placements on one
broker's scale". Five of the eight shipped ties are the second thing, not the
first: they are an artifact of merging two per-runtime scales into one per-vendor
lineup. Nothing behaves wrongly — `RankedModel.Tied`'s own doc is accurate, the
google file states the truth where the data lives, and the merged scale predates
this change (v0.1.0 derived a tie-free 1..15 over the same rows, which was worse).
But in a module whose whole argument is that an unobserved number must not pass
for an observed one, the type-level sentence overstates what a tie means. Worth a
one-line correction on `Score`.

**3. `ErrDuplicateRank`'s removal is a second breaking change and is not in the
migration note.** `docs/consuming-the-module.md:35` says v0.2.0 "carries one
breaking change" and documents only `Position`→`Score`. Deleting an exported
sentinel breaks any consumer doing `errors.Is(err, vendorplugin.ErrDuplicateRank)`.
It is recorded in `LOGBOOK.md` entry 0048 but not where a consumer upgrading would
look.

## Acceptance criteria

| AC | Verdict |
| --- | --- |
| 1. Score-based rank preserving ties + lifecycle/supersession/recommended/context window/pricing; refusal semantics extended deliberately, Basis still required, new fields state their emptiness rules | Met. `Model`'s doc carries all four emptiness rules in one place, and the two fields with no legal empty (`Lifecycle`, `Rank`) refuse their zero value at registration. |
| 2. All 43 rows ported and pinned against a frozen fixture; muse effort axes under vendor-unresolved | Met. Pinned in BOTH directions (a board row this module lacks, and a module row the board lacks), across both homes — 41 vendor rows + 2 muse rows — with an independently stated `boardModelCount = 43` landing gate in `internal/regress`. |
| 3. Existing pins green: digests byte-stable, full-set count, F2, availability | Met. Full suite green; digest stability proven by a planted leak rather than asserted. |
| 4. v0.2.0 tagged and pushed after green | Correctly NOT done — this is the release act after acceptance, and AC5 governs. |
| 5. Work UNCOMMITTED until review; tag only after acceptance | Met. HEAD is still the base commit; no `v0.2*` tag exists. |

## Verdict

**ACCEPTED.** The port is correct, the provenance reproduces byte-for-byte from
the board binary at the named commit, the transcription pin bites in both
directions on every ported field, the digest stays a frozen surface under a real
planted leak, and the API break is argued rather than asserted — with the tie
evidence turning out broader (8 pairs across 4 vendors) than the brief assumed and
named row by row rather than counted. The three findings above are follow-up work,
not rework: one negative-test dimension on a gate with no live exposure, and two
documentation precision items.

Next step belongs to the orchestrator: checkpoint or integrate, commit the scope
with `commit_ack=scope_committed`, then tag `v0.2.0` as the release act.
