# TASK-260823-4f5t1m — review verdict, revision 2

**Verdict: ACCEPTED** (run `RUN-260823-fc0254`, CR `CR-TASK-260823-4f5t1m-2`).

Rev 1 was accepted by `RUN-260823-662486`. This review is scoped to what rev 2
changed on top of it, plus a re-run of every gate and an independent mutation
probe proving the harness is still live in this exact tree.

## Delta shape: docs-only, nothing else moved

`git diff --stat 88a2495b e2a0d3550a274745065d09122d8d416b4b07b2c1` — four files,
91 insertions, 46 deletions:

| File | What changed |
| --- | --- |
| `README.md` | story count 4 → 5, switch-story subsection, `AvailabilityFor` unconsumed |
| `SKILL.md` | F1: process claim scoped to the launch plane |
| `docs/architecture.md` | F2: refusal wording; switch story marked done at `b34aa20` |
| `docs/shipped-state.md` | ledger: story table, switch subsection, section 4 CI row, section 6 |

No Go file, no `Makefile`, no `internal/regress`, no `task-board.config.json`,
no `LOGBOOK.md` changed between rev 1 and rev 2. The byte-identical requirement
holds. Working tree recomputed as `e2a0d3550a274745065d09122d8d416b4b07b2c1`
before and after every check in this review — it matches the candidate tree OID
exactly, and nothing was committed.

## Fact-check of every new claim

Each new sentence was checked against the artefact it describes, not against the
producer's note.

| Claim | Evidence | Verdict |
| --- | --- | --- |
| Switch story integrated at `b34aa20` on the consumer's trunk | `git branch --contains b34aa20` in `skill-project-management` → `main` **and** `remotes/origin/main`; commit subject is `STORY-260823-1sxcmg: consume-agents-management-for-the-spawn-plane` | TRUE |
| `STORY-260823-1sxcmg` is `done`, all four tasks `done` | source board: story `done`; `1tis7o`/`io02de`/`1rj1gf`/`t2xuen` all `done` | TRUE |
| `STORY-260821-1c5o90` on this board carried no tasks and was closed against the mirror | this board: status `done`, `children: []`, notes name the mirror and `b34aa20` | TRUE |
| Five of six stories landed; four on this `main` | epic has exactly six children; `main` carries `224xfu`, `1xppz3`, `3b4ewr`, `2m8cpr`; the fifth is the mirrored switch; the sixth is this story | TRUE |
| `main` tagged `v0.1.0` at `b722ace`, pushed | annotated tag `2a23287` → `b722ace`; `git ls-remote --tags origin v0.1.0` returns the same object | TRUE |
| Consumer requires the tag with **no `replace`** on this module | `git show main:tools/board-cli/go.mod`: `github.com/relux-works/skill-agents-management v0.1.0`; the two surviving `replace` directives point inside the checkout | TRUE |
| `GOPRIVATE` + `url.insteadOf` in consumer CI | `ci.yml:15` `GOPRIVATE: github.com/relux-works/*`; `ci.yml:105` and `release.yml:39` install the rewrite | TRUE |
| `go.work` gitignored and untracked | `.gitignore:70-71`; `git ls-files \| grep -c go.work` → `0`; `ciguard/scan_repo_test.go:241` pins it, and `t.Fatalf`s when git is unavailable rather than passing — a failed read is not treated as an absence | TRUE |
| Pinned `actionlint` `v1.7.12` in the consumer's Makefile and its own CI job | `Makefile:220` `ACTIONLINT_VERSION := v1.7.12`, run via `go run …@$(ACTIONLINT_VERSION)`; `ci.yml:26-27` runs the same pinned linter | TRUE |
| `ciguard` fail-closed credential scan, comments stripped first | `ScanWorkflowForFailOpenModuleAuth`, `WorkflowConfiguresCredentialRewrite`, `ScanGoModForEscapingReplace`, `ScanWorkflowsForLinterJob`, `ScanMakefileForPinnedWorkflowLinter`, `ScanForUncachedGuardRun`; `stripShellComments`/`stripComment` applied at `scan.go:252, 298` | TRUE |
| Guards run uncached | `Makefile:125` and `ci.yml:120`: `go test -count=1 ./internal/ciguard/ -v` | TRUE |
| `RELUX_MODULES_TOKEN` **still** not provisioned | `gh api repos/relux-works/skill-project-management/actions/secrets` → `{"total_count":0,"secrets":[]}` | TRUE |
| `AvailabilityFor` has no production caller in **either** repository | module: every hit is a `_test.go` file or a doc comment (`verdict.go` is the definition, `groups.go` a comment); consumer `main`: `git grep AvailabilityFor` returns nothing at all | TRUE |
| **F1 closed** — process claim scoped to the launch plane | `pkg/providerlimits/liveness_unix.go:43` `ProcessStartTime` forks `ps -p … -o lstart=`; reached from `liveness.go:38` (lease) and `liveness.go:68` (`lockHolderGone`, the lock-acquisition path) plus `state.go:191,552`. `AvailabilityFor` (`verdict.go:114`) takes no lock and reaches none of it | TRUE |
| **F2 closed** — refusal names model/runtime/vocabulary/recommendation, not a dotted config key | `spawn.go:166-167`: `model %q under runtime %s accepts %v and the vendor recommends %q`. Grepped every `Errorf` under `pkg/` — no refusal anywhere names a config key. The old wording was inherited from `main` and was false | TRUE |

Two claims rev 2 *corrected* rather than added — the `AvailabilityFor`
production-caller line and section 6's shipped-binary question — are both
downgrades from an optimistic claim to the weaker true one. That is the right
direction, and both were verified independently above rather than taken on the
producer's word.

## Gates re-run, foreground, in this worktree

| Gate | Result |
| --- | --- |
| `make build` | exit 0 |
| `make vet` | exit 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | exit 0, 18 packages `ok`, 0 `FAIL`, no panic |
| `make regress` | exit 0, `internal/regress` ok 0.418s |
| `gofmt -l pkg/ internal/` | silent |

## Attacked, not read: two independent narrowing mutants

Rev 2 changed no code, so the accepted rev 1 harness and its 10/10 narrowing-
mutant evidence carry. To prove the harness is still live *in this tree* rather
than trusting a prior run, two mutants were derived independently here, planted
against the production call site, and the file restored byte-for-byte after each.

1. **Refusal narrowed to name one id.** `pkg/vendorplugin/registry.go:209` —
   message reduced from vendor id + system id + model + remedy to
   `agentic system %q is not registered`. `make regress` exit 2:
   `TestAVendorNamingAnUnregisteredSystemIsRefusedNamingBothIDs` red on all four
   real vendors (`alibaba`, `anthropic`, `google`, `openai`). The gate is bound
   to *both ids*, not merely to the refusal existing.
2. **Failed read downgraded to absence.** `pkg/providerlimits/verdict.go` —
   `readFailure` branch rewired to return `absentIsHealthy` instead of
   `unknownAfterFailedRead`, i.e. the exact "a failure to read is not an
   absence" rule the ledger claims is pinned. `make regress` exit 2:
   `TestAnUnreadableStateFileIsUnknownAndNeverHealthy` red for `claude` and
   `codex`.

Both mutants are narrowings, not deletions. After restoring, the tree hashes
back to `e2a0d3550a274745065d09122d8d416b4b07b2c1`.

## Follow-ups

None blocking. Rev 1's F1 and F2 are closed by this revision and re-verified
above; F3 (the consumer CI task not landed) is what rev 2 exists to correct and
is now true in the other direction.

Work is UNCOMMITTED. `HEAD` is `b722ace`, five paths modified, four untracked —
the same shape rev 1 was accepted at.
