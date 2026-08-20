# TASK-260821-3svtog — review verdict, revision 2

**Verdict: ACCEPTED.**

Narrow re-review of the single blocking finding from rev1. Everything passed in rev1 —
gates from clean state, the layout and install decisions, the anchored ignore fix, the
version/plugins mutants — was not re-litigated.

## Evidence anchor

- Base OID: `6223c283d36018b3cf1c7e1c1256e6964b4e3360`
- Candidate tree OID: `0aa4f4e3165858ca1ae91dadd4d320d33e8d7795` — recomputed from the
  working tree (`read-tree HEAD` + `add -A` + `write-tree`) and byte-identical to the
  CR's declared candidate. All findings below were gathered at that exact tree.
- Matrix trees: disposable `git init` + `add -A` + commit copies of the candidate file
  set, so `tools/agents-management/**` and `pkg/**` are TRACKED — the post-landing state
  the rev1 finding was about.

## Gates (foreground, this turn)

| Gate | Result |
| --- | --- |
| `make vet` | clean |
| `make build` | clean |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | ok, 2.751s |
| `gofmt -l tools/` | no output |

## Check 1 — the blocker: rev1 reproduction against rev2 code

The rev1 finding was that `git check-ignore` consults the index first and never reports a
TRACKED file as ignored, so the ignore guard would go unconditionally true — and silently
stop guarding — at exactly the moment the candidate landed and the sources became tracked.

| Row | Code | Tree | `.gitignore` | Result |
| --- | --- | --- | --- | --- |
| G | rev1 (`check-ignore` without `--no-index`, unextended list) | TRACKED | bare `agents-management` mutant | **GREEN** — the hole, reproduced |
| A | rev2 | TRACKED | bare `agents-management` mutant | **RED** |
| H | rev2 | TRACKED | clean | **GREEN** |

Row A fails on six assertions, including `tools/agents-management/main.go is git-ignored;
it would never reach a commit`. Row G is my rev1 reproduction and is still green, which
confirms the fix is what closes it rather than an unrelated change. Row H shows the fix
introduces no false positive.

## Check 2 — the nonexistent-path assertions bite, and bite alone

Neither `tools/agents-management/cmd/future_command.go` nor `pkg/agents-management/foo.go`
exists on disk (verified). `git check-ignore --no-index` still answers for them, which is
what makes the assertion possible at all.

Two narrowing mutants, each touching only paths that do not exist:

| Mutant | rev2 extended list | rev1 unextended list |
| --- | --- | --- |
| P: append `pkg/agents-management/` — the foo.go concern from rev1 | **RED** (`pkg/agents-management/foo.go is git-ignored`) | **GREEN** |
| F: append `*_command.go` | **RED** (`cmd/future_command.go is git-ignored`) | **GREEN** |

Each mutant is caught by exactly one assertion, and it is one of the two new ones. Against
the rev1 list both land green. So the two halves of the fix are load-bearing against
*different* mutants, as the producer claimed — `--no-index` catches the tracked-tree
bare-pattern case (row A vs G), the extended list catches the future-file swallow (P, F).
Neither is redundant with the other.

## Check 3 — the comment at the invocation

The comment names the mechanism, not just the flag: `check-ignore` consults the index
first and never reports a tracked file as ignored; the asserted sources are untracked only
while the CR is in flight; the guard would expire at exactly the moment it starts
mattering; and `--no-index` is also what permits asserting paths that do not exist yet. It
opens with "load-bearing; do not helpfully simplify it away". A future copier reading this
has every reason the flag must stay, well past the one-sentence bar.

## Check 4 — the sweep's conclusion, confirmed not assumed

Grepped all 480 lines of `*_test.go` for index/repo/filesystem-state dependence.
`exec.Command` appears at five sites: `make build` (line 80), the built binary (102),
`go build` (163), and `git check-ignore` (236). Only line 236 consults the git index. The
one `os.Stat` (line 51) walks for `go.mod` to find the repo root, which is a filesystem
fact independent of tracking. The class — an assertion whose truth flips when the
candidate lands — has exactly one site in this suite, and it is the fixed one.

Confirmed empirically as well: the full suite is green on both the tracked disposable tree
(post-landing state) and the untracked candidate worktree (pre-landing state). No other
test changes answer between the two.

## Notes

No repository file was modified, reverted, stashed, cleaned or checked out by this review.
All mutants were applied to disposable copies under `/tmp`.
