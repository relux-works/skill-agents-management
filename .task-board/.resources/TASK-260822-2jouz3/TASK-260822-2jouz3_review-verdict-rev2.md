# TASK-260822-2jouz3 — review verdict rev 2: ACCEPTED

Reviewer run `RUN-260822-3b3b8d`. Change Request `CR-TASK-260822-2jouz3-2` rev 2,
`repository_delta=present`, base `8212ae6`, candidate tree
`b724f09dc4968efb3302e8e5c777cb03e22f2a86`.

This is a **verdict-only review of a merge**. Rev 1 (candidate tree `133542d`)
was accepted on substance by `RUN-260822-3932de`; the port itself is not
re-litigated here. Rev 2 is that accepted tree republished on a base that moved
under it, and the only question is whether the republication changed anything
and whether the two stories still hold each other up.

## Binding — what I actually reviewed

| Fact | Value | How verified |
| --- | --- | --- |
| Worktree tree hash, before and after every check | `b724f09d…` | `git write-tree`, identical at start and at end |
| Declared candidate tree OID | `b724f09d…` | matches |
| Merge commit | `4d3c995` | `HEAD^{tree}` == candidate |
| Its parents | `ce816bf` (story) + `8212ae6` (trunk) | `git rev-parse 4d3c995^1 ^2` |
| Merge base | `3e896b1` | `git merge-base` |
| Accepted rev-1 tree | `ce816bf^{tree}` == `133542da9ccbb18d…` == declared rev-1 candidate | the branch commit IS the accepted tree, not a re-derivation |
| CR patch resource | sha256 `4836ecf4…` | matches the declared hash **and** a freshly regenerated `git diff 8212ae6 b724f09` byte for byte |

Every mutation below ran in a throwaway copy or was restored in place and
re-verified against the tree hash. The reviewed worktree ends at
`b724f09d…` with `git status --short` empty.

## Check 1 — the merge resolution is the only delta

Not "the suite passes", which would prove nothing about dropped content. The
merged tree was decomposed against both parents path by path.

- **64 paths changed by the story side, 27 by trunk, 3 by both** (`LOGBOOK.md`,
  `README.md`, `pkg/agentic/singlesource_guard_test.go`). The 64 story-side
  paths are exactly this CR's changed-path list.
- **Every one of the 61 story-only paths is byte-identical in the merge to
  `ce816bf`**, and **every one of the 27 trunk-only paths is byte-identical to
  `8212ae6`** — blob-OID comparison per path, zero mismatches. Concretely
  `git diff ce816bf HEAD -- pkg/providerlimits .scripts/capture-limit-state.sh
  .scripts/limitstate_xrt.go .scripts/writestate` is empty: the limit plane, its
  tests and its fixtures did not move by a byte from what was accepted.
- **No path changed in the merge that neither side changed**, and **no path
  changed by a side is missing from the merge** — set difference both
  directions, empty.

The three combined files, read rather than assumed:

- **`LOGBOOK.md`** — auto-merged, no conflict. The merged file is byte-identical
  to the mechanical 3-way merge (`git merge-file`). Added lines: 28 from the
  story, 21 from trunk, 49 in the merge. Exactly additive.
- **`README.md`** — one conflict, both sides appending rows to the Tools table.
  Reconstructing the mechanical merge and resolving that one hunk as a plain
  concatenation of both sides yields a file **byte-identical to the committed
  merge**. Nothing else in the file was touched. All seven harness rows are
  present: three pre-existing, this task's two, the vendor story's two.
- **`pkg/agentic/singlesource_guard_test.go`** — one conflict, at the tail,
  where both stories appended tests. Verified structurally rather than by
  eyeballing: the file was segmented into its 49 top-level declarations plus the
  header, and **each segment is byte-equal to either the story side or the trunk
  side**. No declaration was dropped, none is duplicated, none is a hand-edited
  blend. The import block is trunk's (it adds `unicode`; the story added no
  import). `TestSingleSourceGuardHomesAreDistinctFacts` was changed by trunk
  only (inline slice → `dispatchKeyTypes`) and the merge took trunk's; the story
  left it at base, so nothing was lost. `bindingHomes`, `knownPluginIDs` and
  `scanSingleSource` were changed by trunk only and carry trunk's text.

**No declaration was modified by both sides.** The merge had nothing to
reconcile beyond ordering, and it did not.

## Check 2 — the combination holds

Gates on the merged tree, first-hand, foreground:

| Gate | Command | Exit |
| --- | --- | ---: |
| build + vet | `go build ./... && go vet ./...` | 0 |
| format | `gofmt -l .` (excluding `.temp/`) | 0, no files |
| suite | `go test ./...` | 0, every package `ok` |

Publication's own evidence (`TASK-260822-2jouz3_test-rev2.log`) also reports
every package `ok`, `pkg/providerlimits` 9.699s and `pkg/vendorplugin` 5.119s.
I re-ran it rather than only confirming its exit code, and it agrees.

**This task's mutation harness, run in full on the merged tree** —
`.temp/TASK-260822-2jouz3/mutants.py`, baseline `go test ./...` exit 0, then all
**41/41 mutants KILLED**, zero survivors and **zero anchor-not-found skips**.
The brief asked for 5 sampled; the whole set was cheap enough to run and the
skip count is the point — the merge rewrote the guard file this harness's last
four mutants anchor into, and every anchor still resolves. Log:
`TASK-260822-2jouz3_mutants-rev2.log`.

**The vendor story's three module-guard mutants, verbatim, on this tree** —
all three anchors resolve against the merged guard file and all three go red.
Read at the failure line rather than at the exit code:
`"google models" → nowhere.go` reds `TestSingleSourceGuardHomesAreDistinctFacts`
and `TestEveryVendorHasExactlyOneBindingFile` with the right messages; the
shared-binding-file mutant reds `TestEveryVendorHasExactlyOneBindingFile`.

**The cross-check I added — do the two stories' rules mask each other?** Two
stories extending one guard file can each be red for the other's reason, and a
harness that only asks "did the suite go red" cannot tell. So each mutant was
scored per test, over both stories' tests at once, in a scratch copy
(`crosscheck.py`, attached):

| Mutant | Reds | Other story's tests |
| --- | --- | --- |
| limit: exemption pointed at a file with no such site | `AllowlistHasNoUnusedEntries`, `AllowlistedSitesAreReportedWhenNotExempt` | all 3 green |
| limit: exemption reason narrowed to a label | `AllowlistEntriesCarryAReason` | all 3 green |
| vendor: a vendor loses its binding-file home | `HomesAreDistinctFacts`, `EveryVendorHasExactlyOneBindingFile` | all 4 green |
| vendor: two vendors share one binding file | `EveryVendorHasExactlyOneBindingFile` | all 4 green |
| vendor: an id-spelling home keyed as a legal Go identifier | `HomesSplitByKind` | all 4 green |

Baseline: all seven tests PASS. Every mutant reds exactly its own story's rule
and leaves the other story's tests green. **No cross-masking.**

## Finding — the vendor story's mutation harness has an unconditional red

Out of scope for this Change Request, recorded because it is real and because it
is what the cross-check turned up.

`.temp/TASK-260822-3cknas/mutants.py` (the vendor story's harness) runs each
mutant in a `shutil.copytree` scratch copy that excludes `.git`, at scope
`./...`, with **no baseline run**, and treats any non-zero exit as "red (good)".
In that copy `TestBuildOutputIsIgnoredAndSourcesAreNot` fails with
`git check-ignore Makefile: exit status 128` because the copy is not a git
repository. I ran that scratch copy **with no mutant applied at all**: exit 1.

So that harness's pass/fail signal carries no information — a mutant that
changed nothing would also print `red (good)`. Its individual mutants are not
all worthless: I read the failure lines, and its first two guard mutants do
produce the specific guard-test failures they claim. Its third
(`SystemIDAlias`) does not — it is a **build failure** (`FAIL
pkg/agentic [build failed]`: a duplicate constant map key), so it proves the
package stops compiling and says nothing about
`TestSingleSourceGuardHomesSplitByKind` firing. That rule therefore had no valid
mutant of its own; the `"anthropicmodels"` mutant in my cross-check above is
one, and it reds that test and only that test.

This belongs to `TASK-260822-3cknas`, which is accepted and integrated, and it
does not touch this candidate: the file lives in `.temp/`, is not in this CR's
64 paths, and is not in the repository at all. Routing it is the orchestrator's
call. This task's own harness does not share the defect — it runs a baseline
(`exit 0`, verified) and scopes each mutant to `./pkg/providerlimits/` or
`./pkg/agentic/`.

## Verdict

**ACCEPTED.** The republication changed nothing but the merge, the merge is
additive in all three combined files with nothing dropped or duplicated, and the
two stories' guard rules are independently load-bearing on the combined tree.

Recorded with `accept_cr(TASK-260822-2jouz3, revision=2,
evidence=TASK-260822-2jouz3_review-verdict-rev2.md)`. No `commit_ack` from this
run: the element parks at `to-review` and the commit-owning mover makes the
`done` transition.
