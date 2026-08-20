# TASK-260821-3svtog — rework rev2: the landing gate no longer expires at landing

Addresses the single blocking finding in `TASK-260821-3svtog_review-verdict.md`
(CR-TASK-260821-3svtog-1 rev1). Nothing the reviewer verified was touched: the
Makefile, `.gitignore`, `task-board.config.json`, `README.md`, `docs/`, and all
CLI sources are byte-identical to candidate `f8a26642`. Work left UNCOMMITTED.

Two files changed: `tools/agents-management/cmd/build_integration_test.go` and
`LOGBOOK.md`.

## The fix

1. `--no-index` added to the `git check-ignore` invocation
   (`build_integration_test.go:236`). Without it check-ignore consults the index
   first and never reports a TRACKED file as ignored, so the "sources are not
   ignored" half of the guard would go unconditionally true the moment this CR
   lands.
2. `tracked` renamed to `notIgnored` and extended with two paths that do not
   exist: `tools/agents-management/cmd/future_command.go` and
   `pkg/agents-management/foo.go`. The failure class is a NEW file being
   swallowed, and a list of paths that already exist can never observe it.
3. The WHY is commented at the invocation itself, not only in the doc comment,
   because this file is the template every later extraction task will copy.

## The matrix, re-run on TRACKED trees

Harness: `tracked-gate-matrix.sh` (attached). Each row rsyncs the candidate file
set (`git ls-files -co --exclude-standard`, so the built binary and `.temp` are
excluded by construction) into a throwaway repo under
`.temp/TASK-260821-3svtog/scratch/`, `git init`s it, commits for the TRACKED
rows, applies mutants, and runs
`go test -mod=mod ./tools/agents-management/cmd -run TestBuildOutputIsIgnoredAndSourcesAreNot -count=1`.
The story worktree is never touched. Full output: `tracked-gate-matrix-01.log`.

Mutants: `bare` = drop the `/` anchors from `.gitignore` (the founding-commit
1706 bug); `pkgdir` = append `pkg/agents-management/` (a plausible future edit);
`noflag` = revert `--no-index`; `oldpaths` = revert to the original six-path
list.

| Row | Sources | Mutant | Test code | Result | Meaning |
| --- | --- | --- | --- | --- | --- |
| A | tracked | bare | fixed | **RED** | the blocker's case, now caught |
| B | tracked | bare | noflag | **RED** | the new paths alone also catch it |
| C | tracked | none | fixed | GREEN | baseline, shipped tree is clean when tracked |
| D | untracked | bare | fixed | **RED** | still bites pre-landing |
| E | tracked | pkgdir | fixed | **RED** | the `pkg/` path earns its keep |
| F | tracked | pkgdir | fixed + oldpaths | GREEN | narrowing partner for E |
| G | tracked | bare | noflag + oldpaths | **GREEN** | the reviewer's finding, reproduced |
| H | tracked | bare | fixed + oldpaths | **RED** | `--no-index` alone restores the gate |

G is the reviewer's blocking finding reproduced exactly: rev1's code on a tracked
tree with the anchor dropped is vacuous. H is the same tree with only
`--no-index` added — RED. That is the requested proof.

### One correction to my own prediction

I predicted B would be GREEN and it came back RED. The reason is worth recording
rather than filing off: the two halves of the fix are **independently
load-bearing against different mutants**, not redundant belt-and-braces.

- Against `bare` on a tracked tree, either half suffices (B and H both RED); only
  removing both goes vacuous (G).
- Against `pkgdir`, `--no-index` with the old six-path list is GREEN (F) and only
  the `pkg/agents-management/foo.go` entry catches it (E).

So neither half is redundant. The script's row-B expectation was corrected to RED
and the matrix re-run; the attached log has 0 mismatches across all 8 rows.

## Class scan: is anything else tracked/untracked dependent?

Asked of every assertion in the module, not just the one that was reported.

| Site | Depends on the index? | Evidence |
| --- | --- | --- |
| `gitIgnored` (`build_integration_test.go:236`) | **was yes — fixed** | the matrix above |
| `repoRoot()` (`:51`) | no | `os.Stat` on `go.mod`; filesystem, not the index |
| `probeBinary` → `make build` (`:80`) | no | Go reads the working tree; an untracked `.go` file compiles identically |
| `TestBuildWithoutLdflagsReportsDefaults` → `go build` (`:163`) | no | same |
| `run()` (`:102`) | no | executes a built binary |
| `cmd_test.go`, `plugins_test.go`, `version_test.go` | no | wholly in-process against package state; no git, no filesystem, no env |

`grep -rn 'exec.Command\|"git"\|os.Stat\|Getenv' tools/ --include='*.go'` returns
exactly five call sites, all in the table. **Nothing else in this class.** Two
notes that fall out of the scan:

- The `ignored` half of the same test had the mirror-image dependency in rev1,
  but in the fail-safe direction: a binary accidentally committed would report
  not-ignored and go RED. With `--no-index` both halves are now pure pattern
  truth, index-independent in both directions.
- There is no `os.Getenv` anywhere under `tools/`, so no test's truth turns on
  `TASK_BOARD_DIR` or any ambient variable today. The `env -u TASK_BOARD_DIR` on
  the recorded gate is precautionary, not currently load-bearing.

## Gates — foreground, from a clean build state

`make clean` first, each command run standalone, real exit codes:

| Command | Exit |
| --- | ---: |
| `make clean` | 0 |
| `gofmt -l tools/` | 0, no output |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `make test` | 0 |

The three commands in `spawn.worktree_isolation.validation.commands` are among
these and pass. AC1 holds.

AC2 and AC4 re-verified against the installed binary after `make clean`:

```
$ make install
Installed agents-management -> /Users/alexis/.local/bin/agents-management   exit=0
$ ~/.local/bin/agents-management version
agents-management version 6223c28 (commit 6223c28, built 2026-08-21T14:23:22Z)  exit=0
$ ~/.local/bin/agents-management plugins        # no output                     exit=0
$ ~/.local/bin/agents-management plugins --json
[]                                                                              exit=0
```

AC3 unchanged from rev1 and not touched.

Candidate tree still carries all eight CLI sources and no binary:
`git ls-files -co --exclude-standard | grep tools/` lists 8 `.go` files, none of
them `agents-management`.

## The reviewer's non-blocking observation — deliberately not actioned

Folding `env -u TASK_BOARD_DIR` into `make test` and recording `make test` as the
third gate command was raised as "worth doing; not a condition of acceptance".
The rework brief says of everything that survived attack, gates included, "do not
touch any of it", and changing `validation.commands` would reopen a verified AC
for a cleanup that is not load-bearing today (see the `os.Getenv` note above).
Left for a follow-up so it is a decision on the record, not an oversight.
