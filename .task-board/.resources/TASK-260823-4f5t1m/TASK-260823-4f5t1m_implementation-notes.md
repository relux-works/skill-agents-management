# TASK-260823-4f5t1m — SKILL.md, doc reconciliation, and the regression harness

Closing task of `EPIC-260821-1qvnz2`. Work is UNCOMMITTED in the story worktree
`.temp/STORY-260821-17pnec/worktree` on `task-board/story/STORY-260821-17pnec`
(0 commits behind `main`, which is `b722ace` = tag `v0.1.0`).

## What was written

| Path | New/changed | What it is |
| --- | --- | --- |
| `SKILL.md` | new | The skill entry point: two-layer model, the CLI surface as it EXISTS, consumer wiring, the five invariants, the landing gate, and a lazy-reference table into `docs/` |
| `docs/shipped-state.md` | new | The honest ledger: six stories with real states, and six outcomes each with a named owner |
| `docs/consuming-the-module.md` | new | Depending on the module: `v0.1.0`, `GOPRIVATE`, the `insteadOf` rewrite, `go.work` (and why it is gitignored), blank-import wiring, declaring a runtime, the three entry points, what stays the consumer's |
| `docs/architecture.md` | changed | Contract-vs-reality pointer at the top; the local-model section opens with "nothing in this section is built"; the boundaries table grows the exec/preflight row with the reason `BuildPlan` cannot host a probe; the extraction plan reports per-step state |
| `README.md` | changed | Intro routes to `SKILL.md` and the two new docs; `Status` replaced by real story state + a pointer to the ledger; the availability bullet stops implying the local-model plane exists; `Current CLI surface` says WHY the lists are empty; new `The regression net` section; `make regress` in the target list and the landing-gate paragraph; Tools table row for the mutation harness |
| `internal/regress/` | new | The harness: `doc.go` plus four test files, 14 tests |
| `Makefile` | changed | `regress` target |
| `task-board.config.json` | changed | `make regress` appended to `spawn.worktree_isolation.validation.commands` |

## The regression harness

`make regress` -> `env -u TASK_BOARD_DIR go test -mod=mod ./internal/regress/... -count=1`.
**0.45s.** It is a separate target from `test` on purpose: `test` is the deep
per-package acceptance, this one sits in front of every landing and has to stay
cheap enough that nobody skips it.

Four classes, each driven through the real entry point, each with a negative:

1. **Registration refusal naming both ids** (`registration_test.go`). The four
   REAL vendor plugins, read out of `vendorplugin.Default`, registered into a
   registry built on an EMPTY `agentic.Registry`. Each must be refused with
   `ErrUnknownAgenticSystem` naming the vendor AND one of the systems its own
   model rows declare (read from the plugin, not listed here). The narrowing:
   the same vendors are ADMITTED once the real system plugins are moved into an
   isolated registry — so "refuses everything" cannot pass. Third state: a
   registry with a nil agentic registry refuses every vendor.
2. **Runtime declaration + F2, both directions** (`declaration_test.go`).
   Production call site first: `vendorplugin.Default` carries the frozen six
   before any plugin registers. Then idempotency (re-seed is a no-op),
   idempotency under REWORDED provenance (`SameBinding` compares the binding,
   not the prose), and conflict on BOTH axes (system and vendor) with ids that
   name nothing in the module. The assertion that matters: after the refusal,
   the stored declaration is still the first one.
3. **Availability from a real state file** (`availability_test.go`). Bytes this
   repository did not write: `pkg/providerlimits/testdata/source-written/`,
   suppressions produced by a program compiled against the SOURCE module
   through its own classify -> Observe chain, with the capture's manifest
   supplying identity, home, group and model. `IdentityKey` is recomputed and
   pinned against the captured filename before the query runs. Then
   `Store.AvailabilityFor` must answer Limited with `Until` equal to the
   `next_probe_at` the SOURCE computed (read out of the fixture, not retyped).
   Three negatives: the same bytes filed one hex digit away read HEALTHY (the
   fail-open disaster, asserted so the positive has bite); past the window the
   verdict is not serviceable (a read may not hand out a probe); a truncated
   file is Unknown with a read failure attached, never Healthy.
4. **BuildPlan parity smoke** (`parity_smoke_test.go`). One golden per Layer-1
   system — `codex/dry-run`, `claude/prompt-mode` (claude captured no dry run),
   `qwen/dry-run`, `gemini/dry-run`, `muse/dry-run`, `agy/dry-run` — rebuilt
   through `paritycase.BuildPlan`, i.e. a real registry and `agentic.BuildPlan`.
   It is a SMOKE and says so: the per-plugin parity files remain the
   acceptance, and what this catches is a CORE change that breaks all six at
   once. Two generic mutants per case (wrong resolved binary, truncated argv),
   each required to be reported in the field it was planted in. A coverage test
   fails if a seventh plugin registers with no smoke case.

### Mutation evidence

`python3 .temp/TASK-260823-4f5t1m/mutants.py` — 10 mutants, 1 delete-shaped and
9 narrowing, each applied to PRODUCTION code, `make regress` run alone, and the
named test required to fire. Per-mutant logs in `.temp/TASK-260823-4f5t1m/`.

| Mutant | Shape | Caught by |
| --- | --- | --- |
| `Registry.Register` skips the declared-system lookup | delete | `TestAVendorNamingAnUnregisteredSystemIsRefusedNamingBothIDs` |
| the refusal message drops the system id | narrow | same |
| `DeclareRuntime` compares only the id | narrow | `TestAConflictingRedeclarationIsRefusedAndTheFirstStands` |
| `SameBinding` also compares the provenance | narrow | `TestARedeclarationDifferingOnlyInProvenanceIsStillIdempotent` |
| the conflict path writes before returning the error | narrow | `TestAConflictingRedeclarationIsRefusedAndTheFirstStands` |
| `AvailabilityFor` treats a failed read as an absence | narrow | `TestAnUnreadableStateFileIsUnknownAndNeverHealthy` |
| `probeGatedVerdict` answers healthy | narrow | `TestAnElapsedWindowIsNotServiceable` |
| `suppressedIsLimited` shifts its window by a minute | narrow | `TestASourceWrittenSuppressionReadsAsLimitedAtItsOwnWindow` |
| `parity.Compare` stops comparing the binary | narrow | `TestAWrongPlanFailsTheSmoke` |
| the gemini plugin loses `--skip-trust` | narrow | `TestOneGoldenPerSystemStillBuilds` |

All 10 caught. The script restores every file it touches; `git diff` over
`pkg/` and `internal/` (excluding the new package) is empty afterwards.

## Reconciliation findings — what the docs now say that they did not

- **Four stories are on `main`, not five.** `STORY-260821-224xfu`,
  `-1xppz3`, `-3b4ewr`, `-2m8cpr`. This story is the fifth.
- **The switch story is on the SOURCE board.** `STORY-260821-1c5o90` here is
  `backlog` with no tasks; the work is `STORY-260823-1sxcmg` in
  `skill-project-management`, in `development`: three swaps done,
  `TASK-260823-t2xuen` (CI) in development. Recorded as a board-hygiene
  divergence with this repository named as owner.
- **The CI arrangement is half done, and the half that is missing is human.**
  `v0.1.0` is tagged at `b722ace` and PUSHED to origin (verified with
  `git ls-remote --tags`). The consumer side — `require` with no `replace`,
  `GOPRIVATE`, the `insteadOf` rewrite, the gitignored root `go.work` — is
  implemented and UNCOMMITTED in the switch story's worktree. The Actions
  credential (repository secret `RELUX_MODULES_TOKEN`, a fine-grained PAT with
  `Contents:read` here) is **not provisioned**, and no agent can provision it.
- **Two traps recorded rather than left to be rediscovered**: a committed root
  `go.work` applies in CI because discovery walks UP; and the `secrets` context
  is not available to a workflow `if:` key — referencing it there is a syntax
  error that killed the consumer's whole CI run between 2026-08-19 and
  2026-08-23.
- **Model descriptions differ on all 41 shared rows, by design**, measured
  field by field in `TASK-260823-1tis7o`. Owner of collapsing them: this
  repository, because the module is the side that can be read.
- **The composition-validator duplication is still unguarded.** ~150 lines of
  rules in both repositories with no cross-repository agreement check. Carried
  as FLAGGED with `skill-project-management` as owner — it is the caller, and
  the fix is for its board-facing gate to call `System.ValidateComposition`.
- **`BUG-260819-3qn52o` is still `backlog`** on the source board; every leak it
  covers is pinned here against CURRENT behaviour. Same for the qwen-codex
  auth-hint gap.
- **The local-model section drifted nowhere**, verified by grep across
  `pkg/`, `internal/`, `docs/`, `README.md` and `tools/`: every mention is
  either the design note or the seam. Both `architecture.md` and the README
  bullet were tightened anyway, because the README's old wording ("a test
  demonstrates each of its five answers fitting") could be read as the plane
  existing.

## Gates — all foreground, real exit codes

| Gate | Exit | Result |
| --- | ---: | --- |
| `make vet` | 0 | clean |
| `make build` | 0 | binary built |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 | 18/18 packages ok, 5 with no test files |
| `make regress` | 0 | 14 tests, 0.45s |
| `gofmt -l pkg/ internal/` | 0 | no output |
| `python3 .temp/TASK-260823-4f5t1m/mutants.py` | 0 | 10/10 mutants caught |

## One thing a reviewer should know

`make regress` was added to `spawn.worktree_isolation.validation.commands` in
this candidate, but publication and integration resolve that list from the MAIN
checkout — so this landing is gated by the OLD three-command list and the new
command starts gating the next one. That is the same bootstrapping shape the
original gate installation had (and the shape that produced the rev-2 republish
in the source's `TASK-260823-1rj1gf`). It is stated in the README's Development
section rather than left to be discovered.
