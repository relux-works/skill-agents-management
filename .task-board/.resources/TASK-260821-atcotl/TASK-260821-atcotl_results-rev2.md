# TASK-260821-atcotl — rev 2: the three surviving mutants, killed

Rework of the REJECTED rev 1. The verdict was explicit: all four ACs implemented,
production code passed every attack, three gaps were TEST-ONLY. **No production
code was changed in this revision.** Only `_test.go` files were touched.

## What was wrong and what now pins it

### M5 — `ErrRuntimeVendorUnresolved` collapsed into `ErrRuntimeVendorUnregistered`

The highest of the three, because the port stories inherit this branch: after the
collapse muse tells an operator to install a vendor nobody has identified.

`TestResolutionRefusalsAreDistinctFacts` was named for the property and proved
only that each branch is REACHABLE — four positive `requireErrorIs` calls, and
aliasing any two sentinels together leaves all four green.

**Fix** (`pkg/vendorplugin/runtime_test.go`): a new subtest
`no refusal answers to another refusal's sentinel`. Four resolution refusals ×
five sentinels: each case asserts `errors.Is(err, want)` AND
`!errors.Is(err, other)` for every other sentinel in the set
(`ErrUnknownRuntime`, `ErrRuntimeSystemUnregistered`,
`ErrRuntimeVendorUnregistered`, `ErrRuntimeVendorUnresolved`,
`ErrVendorNotRegistered`). The exclusion is the assertion; the inclusion was
already there.

The unresolved case is driven through `SeedFrozenRuntimes` and resolves the real
**muse** row — the shipped decision, not a shape that resembles it. That needed
an agentic system registered under the id `muse`, so `double_test.go` gained
`renamedPangolin` (the same Layer-1 double under a different id — no second
fake) and `systemsNamed`.

Table-shaped rather than four hand-written negatives on purpose: collapsing ANY
pair of the five now goes red, not just the pair the reviewer named.

### M3 — the effort vocabulary's exact-match bound was unpinned

The producer's own mutant DELETED the membership check. The reviewer WIDENED it
(`strings.EqualFold`), which is what the rule is actually about — invariant 5,
a second normalization of a value this package does not own — and it survived.

**Fix** (`pkg/vendorplugin/spawn_test.go`), two tests:

- `TestBuildLaunchDoesNotFoldTheEffortVocabulary` drives the production entry
  point `BuildLaunch` with `Deep`, `DEEP`, `dEeP`, `Shallow`, `de ep` — each must
  be `ErrEffortNotInVocabulary`, name the word and the vocabulary, and the vendor
  must not be reached. Then `deep `, ` deep`, `\tdeep\n` must still be ADMITTED,
  so the bound reads as exactness and not as "delete the trim".
- `TestEffortVocabularyMembershipIsExactOnTheTrimmedWord` pins the same rule at
  `EffortDeclaration.Accepts` itself. This is not redundant: `BuildLaunch` trims
  at `spawn.go:162` BEFORE asking, so the launch path alone leaves `Accepts`'
  own trim resting on a caller that happens to trim first — demonstrated, see
  M11 below. `Accepts` is exported and `EffortDeclaration.Validate` calls it on
  the vendor's own recommendation.

### M2 — rank evidence accepted whitespace-only source/observation once narrowed

Trim-discipline was pinned for `UsageDescription` and `Broker.Checked` and
missed for `CapabilityRank.Basis`: narrowed to `== ""`,
`RankEvidence{Source: "   ", Observation: "\t"}` satisfies "a rank with no
observation behind it is policy wearing a number".

**Fix** (`pkg/vendorplugin/registry_test.go`): `TestRegisterRefusesARankWithNoEvidence`
grew whitespace-only source, whitespace-only observation, both-blank, an empty
basis slice, a negative position, and a mixed row (one sound evidence + one
blank — the blank must still sink it). Each case asserts at `Model.Validate`
AND at the production call site `Registry.Register`.

## Mutation run — 13 mutants, 13 KILLED, **0 SURVIVORS**

The reviewer's ten (M2 split into its two independent halves 2a/2b), plus two
mine for the trim bounds the M3 work exposed. Harness:
`TASK-260821-atcotl_rev2-mutants.py` — applies one exact-text mutation to a
throwaway copy at `/tmp/atcotl-mut`, runs the full suite, records the real exit
code and the tests that went red, restores. A mutation whose anchor does not
match exactly once is reported as a HARNESS BUG, never as a survivor.

| # | Mutation | Rev 1 | Rev 2 | Killed by |
| --- | --- | --- | --- | --- |
| M1 | zero-value Availability becomes healthy | KILLED | KILLED | zero-verdict + plugin-error tests |
| M2a | rank evidence SOURCE blank check → `== ""` | **SURVIVED** | **KILLED** | `…RankWithNoEvidence/a_source_that_is_only_whitespace` |
| M2b | rank evidence OBSERVATION blank check → `== ""` | **SURVIVED** | **KILLED** | `…RankWithNoEvidence/an_observation_that_is_whitespace` |
| M3 | effort vocabulary widened to `EqualFold` | **SURVIVED** | **KILLED** | `…DoesNotFoldTheEffortVocabulary`, `…ExactOnTheTrimmedWord` |
| M4 | duplicate rank position admitted | KILLED | KILLED | `TestRegisterRefusesTwoModelsAtOneRank` |
| M5 | `ErrRuntimeVendorUnresolved` aliased to `…Unregistered` | **SURVIVED** | **KILLED** | `…DistinctFacts/no_refusal_answers…` (muse + vendor-unregistered cases) |
| M6 | `CheckAvailability` validates state only | KILLED | KILLED | `TestCheckAvailabilityRefusesAVendorsDishonestVerdict` |
| M7 | `bindingHomes` flattened to a flat allowlist | KILLED | KILLED | `TestSingleSourceGuardCatchesCrossLayerBindings` ONLY |
| M8 | `Broker.Checked` blank check → `== ""` | KILLED | KILLED | `TestRuntimeDeclarationValidation/a_blank_checked_source` |
| M9 | unresolved check moved below the vendor lookup | KILLED | KILLED | `…OnItsOwnTerms` + the new distinctness subtest |
| M10 | unknown may carry a clear time | KILLED | KILLED | `TestValidateRefusesVerdictsThatContradictTheirEvidence` |
| M11 | `Accepts` stops trimming | — | **KILLED** | `…ExactOnTheTrimmedWord` |
| M12 | `BuildLaunch` stops trimming the requested effort | — | **KILLED** | `…DoesNotFoldTheEffortVocabulary`, `…NamesTheVocabulary` |

M11 is recorded because it SURVIVED an intermediate state of this revision: with
only the `BuildLaunch`-driven test in place, removing `Accepts`' own `TrimSpace`
left the whole suite green (`go test ./... -count=1` → exit 0), because
`BuildLaunch` trims first. That is what motivated the second test rather than a
guess that one would be nice to have.

## Gates — foreground, standalone processes, real exit codes

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `gofmt -l pkg/ tools/` | 0, no output |

Suite: `internal/ident` ok, `pkg/agentic` ok, `pkg/vendorplugin` ok,
`tools/agents-management/cmd` ok, `tools/agents-management` has no test files.
Mutation-harness baseline in the throwaway copy: exit 0 before every mutant.

## Scope

Four files, all tests:

- `pkg/vendorplugin/runtime_test.go` — the distinctness subtest
- `pkg/vendorplugin/spawn_test.go` — the two effort tests
- `pkg/vendorplugin/registry_test.go` — the rank-evidence whitespace cases
- `pkg/vendorplugin/double_test.go` — `renamedPangolin`, `systemsNamed`

No production source under `pkg/`, `internal/` or `tools/` was edited. Work is
left UNCOMMITTED in the story worktree for the Change Request snapshot.
