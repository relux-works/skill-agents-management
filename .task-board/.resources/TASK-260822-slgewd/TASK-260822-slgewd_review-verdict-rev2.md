# TASK-260822-slgewd — review verdict, revision 2

**ACCEPTED.** The rev1 blocking finding is closed, verified by the reviewer's own
probes rather than by reading the producer's tests.

Candidate tree: `ba63a0d0ef55871f17ad44f8c4fa3af912383d93` (base
`be44d86e0641ee971f0c14221ea49b7e51c0980b`). Verified by `git write-tree` over a
scratch index at the start of review, after every mutation, after a full
regeneration of all 14 goldens, and after the gates — identical every time. The
working tree is byte-for-byte the CR candidate. Every mutation below was applied
to a scratch copy or a scratch `_test.go` and reverted; both scratch test files
were deleted before the gates ran.

Scope of this pass: the one rev1 blocker plus the four checks in the re-review
brief. Rev1's confirmed strengths (pinned synthetic-env decision, provenance by
full regeneration, comparator and masking mutants) were not re-litigated.

---

## Rev1 blocker, restated

`qwen/exec` was the only exec fixture whose `env_removed` covered 100% of its
recorded `parent_env` — 18 seeded keys, 18 removed, zero survivors. Because
`ComparePlan` feeds the plan `g.Capture.ParentEnv` and nothing else, a qwen port
whose `ChildEnv` discards the whole parent environment and returns only its
injections byte-matched the golden. The goldens proved `filterQwenRuntimeEnv`'s
lower bound and were silent on its upper bound.

## CHECK 1 — the reviewer's own wipe must fail now, and the near-miss must bite

Written fresh in `zz_reviewer_scratch_test.go` / `zz_reviewer_scratch2_test.go`,
independent of the producer's `wholeEnvWipeSystem` and `prefixStripSystem`, each
driven through the real `agentic.BuildPlan` on a real `agentic.Registry`.

**1a. Whole-environment wipe — now CAUGHT.** `revWipeSystem.ChildEnv` returns
only `TASK_BOARD_RUN_ID` / `TASK_BOARD_TASK_ID` and discards the parent
entirely. Against the regenerated `qwen/exec` golden: **1 difference,
`EnvRemoved`**, plan over-removing exactly the four seeded survivors —
`PARITY_BYSTANDER`, `CODEX_LIKE_BUT_NOT`, `CLAUDECODE_LIKE_BUT_NOT`,
`TASK_BOARD_LIKE_BUT_NOT`. The rev1 result on the same probe was zero
differences.

**1a-narrowing.** With those four entries stripped back out of the golden's
recorded `parent_env` — the rev1 fixture shape — the identical wipe byte-matches
again (`0 differences`). The bystanders, and nothing else, are what catches it.

**1b. Prefix strip — CAUGHT, and isolated to one character.** The blunt probe
(all three families by `strings.HasPrefix`) is caught, naming all three
`*_LIKE_BUT_NOT` keys. The sharp form is
`TestReviewerOneCharacterWrongIsCaughtByCodexNearMissAlone`: a port that is
byte-correct in every other respect — exact-key strips for `CLAUDECODE`, both
`*_AUTH_TOKEN_ENV` pointers and their targets, `TASK_BOARD_SESSION_ID`, and the
four `withSpawnEnv` run-context keys — and reaches for
`strings.HasPrefix("CODEX_")` for the codex family only.

- Control (exact-key version of the same port): **byte-matches** the golden, so
  the mutant isolates one character.
- Mutant: **exactly one difference, `EnvRemoved`, sole moved key
  `CODEX_LIKE_BUT_NOT`.** Asserted as exactly one, not merely present.
- Narrowing: drop only `CODEX_LIKE_BUT_NOT` from the recorded `parent_env` and
  the same one-character defect is **invisible** (`0 differences`).

That is the claim's teeth. `CODEX_LIKE_BUT_NOT` is load-bearing for a defect
nothing else in the fixture set can see.

## CHECK 2 — regeneration honesty

Source checkout `~/src/relux-works/skill-project-management` at
`ed4878123061b39fdae67160f6b5632117b48a2f`, clean, verified unmoved from rev1.
Ran `.scripts/capture-parity-goldens.sh --source <src>` once more, foreground,
exit 0. All 14 fixtures rewritten.

- `diff -r` against the committed goldens: **byte-identical, no output.**
- Source `HEAD` + `git status --porcelain` hash identical before and after:
  **source untouched.** The only thing the script does inside it is one
  `go test`.
- The regeneration is not a no-op diff: the raw capture carries fresh randomized
  `t.TempDir` nonces per run
  (`.../TestCaptureLaunchSurfaceclaudeprompt-mode2699885432/001`, different from
  the prior capture's), so the masking collapsed genuinely different paths to
  the same `<TMPDIR:1>`.
- `.temp/parity-capture` is gitignored; tree OID unchanged after the run.

## CHECK 3 — the bystander pin goes red on a dropped key

Scratch mutation, `claude/prompt-mode`, `capture.parent_env`, dropped
`CODEX_LIKE_BUT_NOT` only:

```
--- FAIL: TestEveryGoldenSeedsTheBystanderKeys
    claude/prompt-mode: parent_env does not seed the bystander key
    "CODEX_LIKE_BUT_NOT"; ... Reseed it in PINNED_ENV in
    .scripts/capture-parity-goldens.sh and recapture
```

Stronger variant — all four bystanders dropped from `qwen_exec.json`, restoring
the exact rev1 fixture shape — four tests go red in the documented order, each
naming the right cause:

| Test | Result |
|---|---|
| `TestQwenExecLeavesSomethingToPreserve` | FAIL — "removes all 18 entries of its recorded parent_env" |
| `TestAWholeEnvironmentWipeFailsAgainstQwenExec` | FAIL — "no key proves the filter is SELECTIVE" |
| `TestAPrefixStripFailsAgainstQwenExec` | FAIL — "byte-matched qwen/exec, but the source strips by exact key" |
| reviewer's own wipe probe | FAIL — "0 differences" |

So the fixture-level pin fires *before* the behavioural negatives, with an
actionable message pointing at the script line. Both goldens restored from
backup; tree OID re-verified.

## CHECK 4 — README documents the convention as capture contract

`testdata/goldens/README.md` §"The bystander keys, and why a capture must keep
them" states the lower/upper-bound argument, names qwen as where the gap bites,
carries a per-key table of what each of the four pins, cites the source's own
exact-key mechanism (`filterEnvKeys`, `spawn.go:998`; `appendOrReplaceEnv`), and
warns in bold that **a recapture that drops them silently reverts the fix**. It
lists the four holding tests in failure order and notes both attacks run through
the real `agentic.BuildPlan` with narrowing subtests.

It is not prose-only: `TestFixturesReadmeNamesWhatTheGoldensDoNotProve` iterates
`parityBystanderKeys` and fails if any is missing from the README, alongside the
existing source-commit and uncaptured-surface checks.

## Gates (candidate tree, each a foreground process)

| Gate | Exit |
|---|---:|
| `gofmt -l pkg/` | 0, no paths |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |

Final `git write-tree` = `ba63a0d0ef55871f17ad44f8c4fa3af912383d93`, unchanged.

## Acceptance criteria

1. **Goldens from the real source harness.** 14 fixtures, source commit
   `ed48781` recorded in every file, regenerated by this reviewer byte-clean.
   Nothing in this repo captures anything.
2. **Comparison harness fails on any field difference; masking pinned.**
   `Compare` has no tolerance; `TestCompareReportsEveryField` holds it to the
   `Snapshot` type by reflection. Masking pinned three ways including the
   fails-if-masking-widens test. Verified in rev1, unchanged.
3. **Wrong plan demonstrably fails.** Three producer negatives plus four
   independent reviewer probes, all through `agentic.BuildPlan`, each naming the
   field the defect was planted in.
4. **Uncaptured combinations listed with source reasons.** README boundary
   section, test-enforced.

## Residual, non-blocking

`golden_test.go:50` says "The two `_LIKE_BUT_NOT` keys are near-misses" — there
are three. Prose drift in a doc comment; the README table and
`parityBystanderKeys` both list all three correctly, and no test reads the
sentence. Worth folding into whatever next touches the file; not worth a
revision.

## Consequence

The three port tasks unblock. A port now proves both bounds of its environment
filter — strips at least these, strips no more than these, by exact key — through
one `ComparePlan` call.
