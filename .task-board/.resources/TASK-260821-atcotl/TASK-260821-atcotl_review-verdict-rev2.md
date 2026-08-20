# TASK-260821-atcotl rev 2 — review verdict: ACCEPTED

Re-review of `CR-TASK-260821-atcotl-2` revision 2 by the reviewer who wrote the
rev 1 verdict. The rev 1 verdict requested three test additions and asserted no
production change was expected. This review checks that claim first, then
re-runs its own ten mutants, then attacks the two properties rev 2 claims are
GENERAL rather than hardcoded.

- Candidate tree: `e6beb5b6bd51f15c29a0974685f04d3dbcc6261e` — the story
  worktree hashes to exactly this, verified before and after the review
  (`git read-tree --empty && git add -A && git write-tree` into a scratch index).
- Rev 1 candidate tree: `01251f9786ee82cb721f589d27c6e0cdecd33273`, reconstructed
  from base `6223c283` + the attached rev1 patch and `git write-tree`.
- The rev2 patch resource applied to base reproduces `e6beb5b…` exactly, and its
  sha256 matches the declared `5eb324a1…`.
- All mutation was done in a throwaway copy at `/tmp/atcotl-s2`. Nothing in the
  candidate was edited.

## Gates — run foreground in the candidate worktree

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 (4 ok, `main` has no test files) |
| `gofmt -l pkg/ tools/ internal/` | 0, no output |

Scratch baseline at `/tmp/atcotl-s2` green before any mutation, so every red
below is the mutant and not the copy.

## CHECK 1 — diff discipline: zero production-file changes

`git diff --stat 01251f97… e6beb5b6…`:

```
 LOGBOOK.md                        | 16 +++++++
 pkg/vendorplugin/double_test.go   | 26 +++++++++++
 pkg/vendorplugin/registry_test.go | 17 ++++++++
 pkg/vendorplugin/runtime_test.go  | 92 +++++++++++++++++++++++++++++++++++++++
 pkg/vendorplugin/spawn_test.go    | 61 ++++++++++++++++++++++++++
 5 files changed, 212 insertions(+)
```

Four `_test.go` files and the logbook. 212 insertions, **0 deletions** — nothing
was removed either, so no test was weakened to make a mutant die. No file under
`pkg/*/[^_]*.go`, `internal/` or `tools/` differs from rev 1. The claim holds.
The LOGBOOK delta is entries 2140 and 2135, and both describe the finding
accurately rather than restating the fix; 2140 in particular records the
two-layer normalization shape honestly, including that the inner check "read as
tested and was not". No `t.Skip` was introduced.

## CHECK 2 — my ten rev-1 mutants: 0 survivors

Re-run in their ORIGINAL spelling, M2 split into its two halves as the producer
did. Full harness `/tmp/atcotl-mutate2.py`, results attached as
`TASK-260821-atcotl_rev2-review-mutants.json`.

| # | Mutation | Rev 1 | Rev 2 | Killed by |
| --- | --- | --- | --- | --- |
| M1 | zero-value Availability becomes healthy | KILLED | KILLED | zero-verdict + plugin-error tests |
| M2 | rank evidence `Source` blank narrowed to `== ""` | **SURVIVED** | **KILLED** | `TestRegisterRefusesARankWithNoEvidence` |
| M2b | rank evidence `Observation` blank narrowed to `== ""` | — | KILLED | `TestRegisterRefusesARankWithNoEvidence` |
| M3 | effort vocabulary widened to `EqualFold` | **SURVIVED** | **KILLED** | `TestBuildLaunchDoesNotFoldTheEffortVocabulary` AND `TestEffortVocabularyMembershipIsExactOnTheTrimmedWord` |
| M4 | duplicate rank position admitted | KILLED | KILLED | `TestRegisterRefusesTwoModelsAtOneRank` |
| M5 | `ErrRuntimeVendorUnresolved` aliased to `…Unregistered` | **SURVIVED** | **KILLED** | `TestResolutionRefusalsAreDistinctFacts` |
| M6 | `CheckAvailability` validates state only | KILLED | KILLED | `TestCheckAvailabilityRefusesAVendorsDishonestVerdict` |
| M7 | `bindingHomes` flattened to a flat allowlist | KILLED | KILLED | `TestSingleSourceGuardCatchesCrossLayerBindings` ONLY |
| M8 | `Broker.Checked` blank narrowed to `== ""` | KILLED | KILLED | `TestRuntimeDeclarationValidation/a_blank_checked_source` |
| M9 | unresolved check moved below the vendor lookup | KILLED | KILLED | `TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms` + the new table |
| M10 | unknown may carry a clear time | KILLED | KILLED | `TestValidateRefusesVerdictsThatContradictTheirEvidence` |

**11 mutants, 11 killed, 0 survivors.** The three findings are closed and nothing
that held in rev 1 was broken by the additions.

On M7: my first spelling of it changed the home STRINGS and was killed by the
wrong tests, which would have been a weaker proof than rev 1's. Re-run as a
faithful SEMANTIC flattening — `misplaced` rewritten so any home file may bind
any key type, key ignored — and it is again killed by
`TestSingleSourceGuardCatchesCrossLayerBindings` and by nothing else, exactly as
rev 1 recorded. The per-key-type narrowing proof stands.

## CHECK 3 — the cross-table generalizes past the pair I found

The added value claimed for a 4×5 table over two hand-written negatives is that
collapsing ANY pair reds, not only the one that was reported. Tested by
collapsing four sentinel pairs I did **not** use in rev 1:

| Probe | Collapse | Result | Red at |
| --- | --- | --- | --- |
| P1a | `ErrUnknownRuntime` = `ErrRuntimeVendorUnresolved` | KILLED | `…/never_declared`, `…/the_frozen_muse_binding_was_never_established` |
| P1b | `ErrRuntimeSystemUnregistered` = `ErrUnknownRuntime` | KILLED | `…/never_declared`, `…/the_system_plugin_is_not_compiled_in` |
| P1c | `ErrVendorNotRegistered` = `ErrRuntimeVendorUnregistered` | KILLED | `…/the_vendor_plugin_is_not_compiled_in` |
| P1d | `ErrRuntimeVendorUnregistered` = `ErrUnknownRuntime` | KILLED | `…/never_declared`, `…/the_vendor_plugin_is_not_compiled_in` |

Every one reds inside
`TestResolutionRefusalsAreDistinctFacts/no_refusal_answers_to_another_refusal's_sentinel`,
and each reds from BOTH sides of the collapsed pair rather than from one
hardcoded assertion. That is the property, not a restatement of it. `P1c` is
worth calling out: `ErrVendorNotRegistered` appears in the sentinel column with
no case wanting it — a refutation-only entry — and it is still load-bearing.

The unresolved case is driven through `SeedFrozenRuntimes` against the shipped
`muse` row, and the `renamedPangolin`/`systemsNamed` helper registers a system
under the id the frozen row names rather than inventing a second fake whose
drift nobody would notice. If that helper ever silently failed to register, the
case would answer `ErrRuntimeSystemUnregistered` and red on its own `want` — the
test guards its own setup.

## CHECK 4 — the trim boundary is load-bearing in BOTH directions

The rev-2 claim is that the launch-level and `Accepts`-level tests are not
duplicates: each catches a mutant the other cannot see.

| Probe | Mutation | Result | Red at |
| --- | --- | --- | --- |
| M3 | `Accepts` widened to `strings.EqualFold` | KILLED | BOTH `TestBuildLaunchDoesNotFoldTheEffortVocabulary` and `TestEffortVocabularyMembershipIsExactOnTheTrimmedWord` |
| P3 | `resolveEffort` (the `BuildLaunch` path) stops trimming | KILLED | `TestBuildLaunchDoesNotFoldTheEffortVocabulary` (the accepted-spellings half) + `TestBuildLaunchRefusesAMissingEffortAndNamesTheVocabulary` |
| P4 | `Accepts` stops trimming | KILLED | `TestEffortVocabularyMembershipIsExactOnTheTrimmedWord` ONLY |

P3 and P4 red on disjoint tests. That is the two-layer shape closed: the outer
test can no longer be satisfied by the inner check and vice versa, and the
boundary reads as EXACTNESS rather than as removed-trim, because the
accepted-spellings half (`"deep "`, `" deep"`, `"\tdeep\n"` must still launch)
is what P3 kills. Note `Accepts` is exported and has a second production caller —
`EffortDeclaration.Validate` asks it about the vendor's own `Recommended` — so
pinning it directly is contract surface, not a private-helper test.

## Fresh attacks on the new test code

Rev 2 is test-only, so the risk is a vacuous addition. Five further mutants, all
new to this review:

| # | Mutation | Result |
| --- | --- | --- |
| X1 | rank `Position` bound narrowed `< 1` → `< 0` (0 admitted) | KILLED by `TestRegisterRefusesARankWithNoEvidence` |
| X2 | basis emptiness narrowed `len(…) == 0` → `== nil` | KILLED by `TestRegisterRefusesARankWithNoEvidence` |
| X3 | the frozen `muse` row given a guessed `anthropic` vendor | KILLED by 5 tests incl. `TestTheSixHistoricalRuntimesAreSeededWithTheirFrozenBindings` and the CLI's `TestRuntimesJSONCarriesTheUnresolvedBrokerHonestly` |
| X4 | the unresolved check deleted outright | KILLED by the new table + `TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms` |
| X5 | `muse` provenance claims a `Found` while staying unresolved | REFUSED BY PRODUCTION at package init: `an unresolved broker is a checked-and-EMPTY finding` |

X1 and X2 matter because the new `-1` and empty-basis cases could have been
decorative; they are not. X5 is not a test kill at all — the frozen table cannot
even be seeded with a dishonest provenance, so checked-and-empty is enforced by
`registry.go` rather than merely asserted about.

## Acceptance criteria

| AC | State |
| --- | --- |
| AC1 — interface covers models + rank + description + efforts, structured availability, full spawn surface | met (rev 1), unchanged |
| AC2 — unknown agentic system refused with both ids | met (rev 1), unchanged |
| AC3 — six historical ids as seed declarations with their runtimeid bindings | met (rev 1), now also pinned against a guessed muse binding (X3) |
| AC4 — verdict admits limited-until and unknown without an interface change | met (rev 1), unchanged |

The implementation was already correct at rev 1; rev 2 changes only what the
suite PROVES about it. All four ACs remain satisfied and every negative-shape
requirement in the DoD now has a mutant behind it.

## Verdict

**ACCEPTED.** All four checks hold. Recorded with
`accept_cr(TASK-260821-atcotl, revision=2, …)`, which parks the element at
`to-review` as the accepted handoff. The work stays UNCOMMITTED in the story
worktree; integration and the `done` transition with `commit_ack` are the
orchestrator's step.

### One line for the port stories, carried forward from rev 1

Not a finding and not a change here: `internal/ident`'s error text is
byte-identical to the pre-extraction `pkg/agentic` wording — verified
empirically at rev 1 — but that equivalence is not pinned by a test, so
rewording `ident.Rule` would silently change `pkg/agentic`'s refusal string.
Callers branch on the `*InvalidSystemIDError` type and its `Reason`, not on the
composed string, so no gate depends on it. Worth a pin when a port story next
touches that file.

---

**Artifact provenance note.** This verdict was first written to
`TASK-260821-atcotl_review-verdict.md` with `resource update`, which overwrote
the rev 1 verdict payload. `accept_cr` correctly refused that name as evidence
(`change_request_evidence_missing`) because this run cannot be shown to have
produced a resource that already existed. The rev 1 verdict has been restored
under its own name from the full text read earlier in this same session — a
faithful reconstruction, not the byte-preserved original, and it carries a note
saying so — and this rev 2 verdict lives at its own name.
