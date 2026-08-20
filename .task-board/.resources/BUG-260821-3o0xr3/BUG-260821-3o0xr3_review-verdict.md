# BUG-260821-3o0xr3 — review verdict: ACCEPTED

Change Request `CR-BUG-260821-3o0xr3-1` revision 1.
Base `6223c283d36018b3cf1c7e1c1256e6964b4e3360` → candidate tree `1d24a3e29f8abc1238a0ce4469c9dc07c1b48afb`.

## Candidate tree actually reviewed

The story worktree's working tree was hashed independently and equals the declared
candidate tree byte for byte:

```
GIT_INDEX_FILE=$tmp git read-tree HEAD && git add -A . && git write-tree
  -> 1d24a3e29f8abc1238a0ce4469c9dc07c1b48afb
```

Nothing was committed; the work is uncommitted as the DoD requires. All mutation
work below was done on scratch copies of the candidate tree
(`.temp/BUG-260821-3o0xr3-*`, `.temp/mut-*`, `/tmp/bug3o0xr3`), never in the
worktree.

Scope note: the base→candidate delta spans 42 paths because siblings
`TASK-260821-21vywo` / `TASK-260821-atcotl` are `done` but uncommitted in the same
worktree. This review's findings are the scan-scope delta —
`pkg/agentic/singlesource_guard_test.go`, `pkg/agentic/singlesource_scanscope_test.go`,
`pkg/vendorplugin/double_test.go`, `pkg/vendorplugin/scanscope_test.go`,
plus the threat-model / LOGBOOK / README prose. The rest was gate-verified only.

## 1. The original failure shape — reproduced, then silenced

Scratch = candidate tree + the **real** `.agents/` tree copied from the main checkout
(43 Go files, nested `go.mod` at `.agents/tools/agents-infra/go.mod`,
`child_launch_composition.go:95` = `if agent != "codex" && agent != "claude" {`).

| Walk | Result |
| --- | --- |
| OLD (denylist `.git .temp .task-board .claude .codex vendor node_modules testdata`, restored by scratch mutation) | **FAIL** — 12 violations, `child_launch_composition.go:95` verbatim, plus `primary_session_launch_plan.go`, `primary_session_prepare.go`, `project_config_setup.go`, `main.go` |
| NEW | **ok** — `go test ./pkg/agentic/` green with the same `.agents` present |

Each rule proven **independently sufficient** on the real tree:

- `.agents` with its nested `go.mod` **deleted** (bare dot-dir, 43 Go files still there) → green. The dot rule alone carries it.
- `.agents/tools/agents-infra` copied to `vendored-infra/agents-infra` (no dot anywhere in the path, nested `go.mod` kept) → green. The nested-module rule alone carries it.

Full suite `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` in `/tmp/bug3o0xr3`
(candidate tree + real `.agents`, outside the parent repo) → **all packages ok**.
That is the bug's acceptance: the red that only appeared on a bootstrapped checkout is gone.

Why not the literal main checkout: it sits at trunk `a695580`, which does not contain this
Go module at all — the module exists only on the story branch, uncommitted. Candidate tree
+ the main checkout's own `.agents` is the closest faithful reconstruction, and it was run
both under `.temp/` and outside the repo.

## 2. The root rule — the one that fails catastrophically

Three checkouts of the candidate tree whose **root directory name** is `.dotroot`,
`_underroot`, and `vendor`, each with a violating canary planted at `pkg/canary/d.go`:

| Root name | Canary reported | Verdict |
| --- | ---: | --- |
| `.dotroot` | 1 | scans normally |
| `_underroot` | 1 | scans normally |
| `vendor` | 1 | scans normally |

If the root were skipped, all three would have reported clean over zero files. They did not.
Mutant `if path == root` → `if false` kills `TestSingleSourceGuardScansTheWholeModule`,
`TestSingleSourceGuardRulesFireOnRealCode`, `TestSingleSourceGuardHomesAreDistinctFacts`
and `TestSingleSourceGuardScanScope`, so the rule is pinned by the suite, not only by my probe.

## 3. No widening

Violations planted by **me**, at paths absent from the fixture's ten cases, in the real checkout:

| Planted path | Expected | Guard |
| --- | --- | --- |
| `pkg/thirdparty/dispatch.go` (new top-level package) | reported | **reported** |
| `pkg/relay/sub/dispatch.go` (new nested package) | reported | **reported** |
| `internal/ident/.cache/dispatch.go` (dot-dir, no go.mod) | silent | **silent** |
| `pkg/relay/_old/dispatch.go` (underscore-dir) | silent | **silent** |

The fixture's set-equality assertion is live: every mutant that widened or narrowed the
exclusion produced `the walk returned X, which no case in this file plants or excludes`
or its inverse.

## 4. Nothing weakened — mutation matrix

Eight scratch mutants of `skipModuleDir` / `moduleSkipDirs`, **all killed**, proving the
bound by narrowing and widening, not only by deletion:

| # | Mutant | Killed by |
| --- | --- | --- |
| 1 | dot rule deleted | ScanScope, ReportsOnlyBuiltCode, Narrowed/`.agents/...` |
| 2 | underscore rule deleted | Narrowed/`_scratch/pick.go` |
| 3 | root exemption removed (`path == root` → `false`) | ScansTheWholeModule, RulesFireOnRealCode, HomesAreDistinctFacts, ScanScope |
| 4 | nested-`go.mod` narrowed to never fire | Narrowed/`nested/pick.go` |
| 5 | stat failure read as absence (`default: return false, nil`) | `TestSkipModuleDirUnreadableIsNotAbsent` |
| 6 | exclusion widened to every subdir (`return true` first) | ScansTheWholeModule, RulesFireOnRealCode, ScanScope |
| 7 | `testdata` dropped from `moduleSkipDirs` | Narrowed/`testdata/pick.go` **and** Narrowed/`internal/infra/testdata/pick.go` |
| 8 | `vendor` dropped from `moduleSkipDirs` | Narrowed/`vendor/example.com/dep/pick.go` |

Full matrix counted from `go test -v`, matching the producer's claim exactly:
22 (`CatchesIDSwitches`) + 14 (`AgainstReviewMutants`) + 13 (`CatchesShadowTables`)
+ 6 (`CatchesVendorLayerBindings`) + 3 (`CatchesCrossLayerBindings`) = **58 caught**,
plus 8 declared-open `ResidualGaps` = 66 named subtests. 102 subtests / 63 top-level
tests in `pkg/agentic`, **0 FAIL, 0 SKIP**.

`TestSingleSourceGuardScanScopeFixtureIsViolating` scans all ten planted paths directly
before the walk is asked anything, so no "not reported" is trusted on a fixture that
violates nothing. The production pair is driven, not just the helper:
`scanSingleSource(walkModuleSources(root), bindingHomes)` in
`TestSingleSourceGuardScanScopeReportsOnlyBuiltCode`, and
`scanSingleSource(moduleSources(t), bindingHomes)` in
`TestSingleSourceGuardFindsNoSecondBinding`.

No leftover denylist survives anywhere: `grep -rn "skipDirs|filepath.Walk"` over
`pkg internal tools` returns exactly the two `walkModuleSources` bodies and nothing else.

## 5. The testdata decision

Stated as a DECISION with its cost in the `# Scan scope` threat-model section of
`pkg/agentic/singlesource_guard_test.go`: "the cost is that a genuine binding parked in
testdata goes unreported; the benefit is that the scan set and the build set are the same
set, with no third rule." Repeated in `LOGBOOK.md` (2015) and `README.md`.

The pin is real and exercises testdata **inside a scanned package**: dropping `testdata`
from `moduleSkipDirs` fails `Narrowed/internal/infra/testdata/pick.go`, not only the
root-level `testdata/pick.go`.

## 6. Duplicate walk in pkg/vendorplugin

The two copies are **byte-identical today** (verified by extracting and comparing both
`skipModuleDir` and `walkModuleSources` bodies). They exist because Go cannot share an
unexported test helper across packages. `TestModuleScanScopeMatchesTheGuard` re-plants
the same ten cases with the same verdicts, and `TestSkipModuleDirNeverSkipsTheRoot`
re-pins the root rule locally, so behavioural drift on those paths goes red.

## Gates — each foreground, each standalone

| Gate | Exit |
| --- | ---: |
| `gofmt -l pkg/ internal/ tools/` | 0, no output |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` (worktree) | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` (candidate + real `.agents`, `/tmp/bug3o0xr3`) | 0 |

One environment artifact, not a defect: running the suite from a scratch copy placed
*under* the parent repo's gitignored `.temp/` fails `build_integration_test.go`'s
git-ignored check, because every path there is ignored by the enclosing repo. The same
tree outside the repo is green. Not this change's code, and not reproducible in the
worktree.

## Finding recorded, not blocking

`singlesource_scanscope_test.go:113-119` (and its twin in `pkg/vendorplugin/scanscope_test.go:52-54`)
justifies rooting the fixture under a directory named `.temp` with "a walk that tested the
root's own name against the dot rule would scan zero files there while reporting clean."
That names the wrong mechanism: the fixture root's own name is `worktree`, and the `.temp`
ancestor is never handed to `skipModuleDir` — `filepath.WalkDir` passes `filepath.Base(root)`.
What actually kills the root-exemption mutant *in that fixture* is the nested-module rule,
since the fixture root carries its own `go.mod`.

The bound itself is genuinely held, by `TestSkipModuleDirDoesNotSkipTheRoot` (which drives
roots literally named `.temp`, `.agents`, `_scratch`, `vendor`, `testdata`, `node_modules`)
and `TestSkipModuleDirRootGoModIsNotNested` — and independently by my own probe in §2, which
ran the guard from roots actually named `.dotroot` and `_underroot`. Comment accuracy only;
no behavioural gap and no coverage gap. Worth a one-line fix whenever this file is next
touched, not worth a rework cycle.

## Verdict

ACCEPTED. All four acceptance criteria met, verified by attacking the gate rather than
reading it: the exclusion was widened, narrowed, deleted per-rule, and stripped of its root
exemption, and every mutant died. The original red is reproduced on demand with the old walk
and gone with the new one, against the real `.agents` tree.
