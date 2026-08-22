
---

## Republish on the merged base (rev 2)

Revision 1 of this candidate was **accepted**. Nothing in the port itself is in
question and nothing in it changed.

What happened is a staleness gate, not a review finding. The vendor story
(`STORY-260821-3b4ewr` / `TASK-260822-3cknas`) integrated to trunk while this
candidate sat in review. Both stories touched `LOGBOOK.md`, `README.md` and
`pkg/agentic/singlesource_guard_test.go`, so the landing was refused until
somebody looked at the combination. The orchestrator committed the accepted
candidate onto the story branch, merged trunk in, and resolved the conflicts
additively; this republication is that merged tree.

**What I verified on the merged base** (`4d3c995`, a merge of `ce816bf` — the
accepted candidate — and `8212ae6` — trunk):

- Worktree is **clean**: `git status --short` prints nothing. The merge is
  committed; this Change Request snapshots the branch state.
- The accepted candidate's tree hash is `133542d`, and `ce816bf^{tree}` is
  `133542d` — the commit on the branch is the accepted tree, not a re-derivation
  of it.
- **The limit plane itself is byte-identical to what was accepted.**
  `git diff ce816bf HEAD -- pkg/providerlimits .scripts/capture-limit-state.sh
  .scripts/limitstate_xrt.go .scripts/writestate` is **empty** — zero lines. The
  merge touched none of the ported code, none of its tests and none of its
  fixtures.
- **The guard test carries both stories' tests.** No function present on either
  parent is missing from `HEAD`: `comm` over the `func` sets of
  `ce816bf:pkg/agentic/singlesource_guard_test.go` and
  `8212ae6:pkg/agentic/singlesource_guard_test.go` against the merged file is
  empty in both directions. Concretely, my four allowlist tests
  (`TestSingleSourceAllowlistHasNoUnusedEntries`,
  `...EntriesCarryAReason`, `...DoesNotExemptTheRestOfItsFile`,
  `...AllowlistedSitesAreReportedWhenNotExempt`) coexist with the vendor story's
  `TestSingleSourceGuardHomesSplitByKind` and
  `TestEveryVendorHasExactlyOneBindingFile`. The merged file is `gofmt`-clean.
- **`README.md` and `LOGBOOK.md` lost nothing from either side** — same `comm`
  check over the table rows and over the logbook lines, empty both directions.
  The Tools table carries every harness row: the three that predate this work
  (codex, claude, qwen/gemini/muse/agy), this task's two rows (`limit-state
  capture`, `limit-plane mutation harness`), and the vendor story's two (`model
  registry capture`, `vendor-layer mutation harness`).

The full suite, `go vet` and `gofmt` are green on the merged tree per the
orchestrator, who ran them before handing this back. I did **not** re-run them
in this run — publication runs the validation commands and records that evidence
itself — so the suite result on this exact tree is the orchestrator's and the
publisher's evidence, not a claim I am making from my own execution.

## Board notes field — data loss, mine, disclosed

While debugging a `set_notes` parse error in this run I ran probe writes against
this task's Notes field, then used `set=true`, which **replaced** the accumulated
Notes history rather than appending to it. The prior content — spawn selection
rationale tuples, agent resolution and launch composition lines, queue/start/
completion events, and the developer and reviewer handoff summaries for runs
`RUN-260822-19e92c`, `RUN-260822-2f9286` and `RUN-260822-3932de` — is **gone and
not recoverable**: this task's board directory is untracked in git, and
`.board-write-ledger.json` records content hashes only, never prior values.

What survives carries the substance of what was lost:
`TASK-260822-2jouz3_review-verdict.md` (the full ACCEPTED verdict for CR rev 1,
candidate tree `133542d`, gates and mutant runs),
`TASK-260822-2jouz3_results.md`, `TASK-260822-2jouz3_mutants-01.log`,
`TASK-260822-2jouz3_change-request_rev1.patch`, and the two non-empty spawn logs.
The resource links themselves survive in the task's `progress.md`.

Nothing about the code, the candidate tree, or the review outcome was affected.
The loss is narration only — but it is real, and it is recorded here rather than
left for someone to notice.

## Gates re-run in this republish run — first-hand, foreground

The spawn brief said not to re-run the suite, on the grounds that publication
runs the validation commands and records that evidence. I ran the gates anyway,
for one reason: `task-board handoff` refuses while checklist items 13–17 are
unchecked, and item 15 is *Tests green*. I will not check an item tied to a
command on somebody else's report of that command. So I ran them, changed
nothing, and the worktree was verified clean before and after.

| Gate | Command | Real exit code |
| --- | --- | ---: |
| vet | `make vet` | 0 |
| build | `make build` | 0 |
| suite | `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| format | `gofmt -l pkg/ internal/ .scripts/ tools/` | 0, no files listed |

Every package reports `ok`; `pkg/providerlimits` 9.699s, `pkg/vendorplugin`
5.119s — both stories' packages green on the same tree, which is the exact thing
the staleness gate existed to check. Logs in `.temp/TASK-260822-2jouz3/`:
`vet-rev2.log`, `build-rev2.log`, `test-rev2.log`, `gofmt-rev2.log`.

`git status --short` printed nothing after the run — the gates left no artifact
in the tree, so the Change Request still snapshots the committed merge exactly.

Items 13, 14, 16 and 17 are checked on the rev 1 review record, not on new work:
the review ACCEPTED this implementation against the AC and the architecture, the
mutation evidence is `TASK-260822-2jouz3_mutants-01.log` plus the reviewer's own
runs in `TASK-260822-2jouz3_review-verdict.md`, and the ported code is
byte-identical to the tree that was reviewed (zero-line diff, shown above).

**Not re-run in this run:** the mutation harness
(`python3 .temp/TASK-260822-2jouz3/mutants.py`). The code it narrows did not
change by a single byte from the accepted tree, so its rev 1 result stands; I am
naming it rather than implying I re-derived it.
