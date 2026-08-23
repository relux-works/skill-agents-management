# Republication note — rev 2 (docs-only delta over the ACCEPTED rev 1)

Rev 1 was ACCEPTED. It published as `task_delta` only because
`STORY-260821-1c5o90` was still open on this board, which blocked the story.
That story is now `done` and its mirror `STORY-260823-1sxcmg` integrated on
`skill-project-management`'s trunk at `b34aa20`, so this task is the story's
final leaf and the republication carries `kind=story_final`.

**No production code changed.** The rev-1 regress harness, its four classes and
its 10/10 narrowing-mutant evidence are untouched and still the standing proof;
this delta edits documentation only.

## What changed, and why the ledger required it

**1. The CI arrangement (shipped-state §4) was stale in the direction that
matters.** It said the consumer half was UNCOMMITTED and the switch story open.
Both moved. Verified, not assumed:

| Claim | Evidence |
| --- | --- |
| Switch story done | `list(type=story)` here: `1c5o90` = `done`; source board: `STORY-260823-1sxcmg` and `TASK-260823-t2xuen` both `done` |
| Landed on the consumer's trunk | `git log` in `skill-project-management`: `b34aa20` on trunk, `0d76bef` records its board state |
| No escaping `replace`, tag required | `tools/board-cli/go.mod:10` requires `v0.1.0`; the only `replace`s are in-checkout (`../../pkg/board`, `../../pkg/remoteconfig`) |
| `go.work` untracked | `git check-ignore -v go.work` → `.gitignore:70` |
| Guard pair landed | `Makefile:220` pins `ACTIONLINT_VERSION := v1.7.12`; `ci.yml:120` runs `go test -count=1 ./internal/ciguard/ -v`; five scanners in `scan.go` with five repo tests in `scan_repo_test.go` |
| Secret still absent | `ci.yml:96-105` still carries the fail-closed check and its remediation message; nothing provisions it |

The heading changed from *"module side done, consumer side in flight"* to
*"landed on both sides, one human step left"*, the per-half table now says where
each half landed, and the guard pair is described including the property that is
easy to lose: the credential-rewrite step is asked to fail closed **with its `#`
comments stripped first**, because a note explaining a rewrite is neither a
rewrite nor a failure.

**2. Story count: four → five.** README and `architecture.md` §4 of the
extraction plan now say five of six are landed — four on this repository's
`main`, the fifth on the consumer's trunk, because the switch is consumer-side
work and its commits were never going to be here. The two rows in
shipped-state's story table say the same from this side, and the
board-hygiene divergence that §"Where the switch story actually lives" recorded
is described as reconciled rather than open, which is what the board now shows.

**3. Two consequences of the switch landing, which the reviewer could not have
caught because they were not yet true.** Both are the same class of honesty the
ledger exists for:

- README said consuming the `Availability` verdict "belongs to the switch
  story". The switch landed and did not wire it: `grep -r AvailabilityFor` over
  `skill-project-management` returns nothing outside its board directory. The
  seam is proven and **unconsumed**, and the README now says that instead of
  deferring to a story that has closed.
- shipped-state §6 deferred "whether the shipped binary should carry the
  plugins" to the switch story's shape. The shape it gave is that nothing
  forces the question — the consumer links the packages directly and never runs
  this binary. Stated as settled-by-not-arising rather than left pending.

## The reviewer's accepted follow-ups, closed in this delta

The rev-1 verdict recorded F1 and F2 as non-blocking documentation-accuracy
items and named this edit as where F1 belongs. Both are closed:

- **F1.** `SKILL.md`'s *"Nothing here starts a process"* was true of the launch
  plane and false in the absolute. `pkg/providerlimits/liveness_unix.go` runs
  `ps` from lock acquisition and the lease/probe-claim liveness checks, because
  pid reuse is otherwise undetectable. The claim is now scoped to `BuildPlan` /
  `BuildLaunch`, with the limit plane's fork named and the read path
  (`AvailabilityFor`, which takes no lock) distinguished from it — the
  distinction a consumer sandboxing process creation needs BEFORE it wires
  `providerlimits`.
- **F2.** Invariant 4 in both `SKILL.md` and `architecture.md` claimed a refusal
  *"names the exact dotted config key"*. No refusal in this module does, and it
  cannot: the module has no config file. What `spawn.go:166` actually names is
  the model, the runtime, the accepted vocabulary and the vendor's
  recommendation — enough to pick a valid effort from the error text. Both docs
  now say that, and say the config key is the consumer's half of the same error.

F3 was the drift this republication exists to remove.

## Gates re-run after the edits

Each run standalone; the exit code below is the command's own.

| Command | Exit |
| --- | ---: |
| `make build` | 0 |
| `make vet` | 0 |
| `make test` | 0 (18 packages `ok`, 0 `FAIL`) |
| `make regress` | 0 |
| `gofmt -l .` | no files listed |

`git status --short` shows the same 5 modified + 4 untracked paths as rev 1 and
nothing staged or committed. **Work remains UNCOMMITTED.**
