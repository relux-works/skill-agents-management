# BUG-260821-3o0xr3 — guard walks machine-local dirs outside the build

Status: ready for review. Work left UNCOMMITTED in
`.temp/STORY-260821-224xfu/worktree` on `task-board/story/STORY-260821-224xfu`.

## The defect

`moduleSources` in `pkg/agentic/singlesource_guard_test.go` walked every
directory under the module root, excluding only a hand-written denylist of
directory names: `.git`, `.temp`, `.task-board`, `.claude`, `.codex`, `vendor`,
`node_modules`, `testdata`. That is a list of the machine-local trees somebody
had thought of by then.

A bootstrapped checkout has agents-infra installed at `.agents/` — gitignored,
present only where agents-infra installed it, absent from every worktree. Its
`tools/agents-infra/internal/infra/child_launch_composition.go` dispatches on
`"codex"` and `"claude"`, so the guard reported it and `go test ./...` went red
on main while every worktree stayed green. The suite's colour depended on whose
machine ran it.

A second, identical copy of the same walk lives in
`pkg/vendorplugin/double_test.go` and carried the same bug.

## The fix

`skipModuleDir(root, path, name) (bool, error)` mirrors `go/build`'s own rules
instead of naming directories:

| Rule | Excluded | Why |
| --- | --- | --- |
| name starts with `.` | `.agents`, `.claude`, … | the toolchain's rule verbatim; excludes the next machine-local runtime nobody has installed yet, with no list to edit |
| name starts with `_` | `_scratch` | same rule |
| subtree carries its own `go.mod` | `nested/**` | a nested module is a different module; this module's build compiles none of it |
| name in `{vendor, node_modules}` | dependency trees | neither begins with a dot, neither is this module's source |
| name is `testdata` | `testdata/**` at any depth | **DECISION**, see below |
| `path == root` | never skipped | a worktree under `.temp/` is ordinary; testing the root's own name scans zero files and reports clean forever |

A `stat` failing for any reason other than not-exist is **propagated**, not read
as absence. A directory the walk cannot inspect is an unknown; answering "no
go.mod" for it would turn an unreadable tree into a silently unscanned one.

### testdata: the explicit decision

`testdata` is **excluded**. `go build` ignores it, so a binding planted there is
not code this module runs, and the benefit is that the scan set and the build
set are the same set with no third rule. The cost — a genuine binding parked in
`testdata` goes unreported — is accepted, because a guard fixture that wants a
violating source is better off writing it into a temp tree whose root it also
controls, which is what the new fixture does. Documented in the `# Scan scope`
section of the threat model in `singlesource_guard_test.go` and pinned by
`scanScopeCases`, including a `testdata` nested inside a scanned package.

## Files

| File | Change |
| --- | --- |
| `pkg/agentic/singlesource_guard_test.go` | `# Scan scope` threat-model section; walk split into `walkModuleSources(root)` + `skipModuleDir` + `moduleSkipDirs` |
| `pkg/agentic/singlesource_scanscope_test.go` | NEW — the both-directions fixture (368 lines) |
| `pkg/vendorplugin/double_test.go` | the duplicate walk fixed identically |
| `pkg/vendorplugin/scanscope_test.go` | NEW — anti-drift: same fixture, same verdicts, spelled out again rather than imported |
| `README.md` | scan-scope paragraph under the guard's description |

No production source changed. No rule, residual or mutant touched.

## Proof, both directions

`TestSingleSourceGuardScanScope` plants the SAME violating source at ten paths
in a fixture module tree and requires set-equal results:

- reported: `internal/infra/child_launch_composition.go`,
  `tools/agents-management/cmd/plugins.go`
- not reported: `.agents/tools/agents-infra/internal/infra/child_launch_composition.go`
  (the trunk failure's exact path), `.claude/hooks/dispatch.go`,
  `_scratch/pick.go`, `nested/pick.go`, `nested/deeper/pick.go`,
  `testdata/pick.go`, `vendor/example.com/dep/pick.go`,
  `internal/infra/testdata/pick.go`

Supporting tests:

- `TestSingleSourceGuardScanScopeFixtureIsViolating` — scans every planted path
  DIRECTLY, bypassing the walk, and requires all ten reported. Without it,
  every "not reported" assertion is equally consistent with a fixture that
  violates nothing, and the exclusion could widen to swallow the module while
  this file stayed green.
- `TestSingleSourceGuardScanScopeReportsOnlyBuiltCode` — drives the production
  pair `scanSingleSource(walkModuleSources(root), bindingHomes)`, not the walk
  in isolation, and fails if the guard reports nothing at all over a fixture
  built entirely from violating sources.
- `TestSingleSourceGuardScanScopeNarrowed` — attributes each skipped path to
  exactly ONE rule, so a later edit collapsing two rules into one loose one is
  visible. Fails if no rule accounts for a path.
- `TestSkipModuleDirDoesNotSkipTheRoot` / `TestSkipModuleDirRootGoModIsNotNested`
- `TestSkipModuleDirUnreadableIsNotAbsent` — a 0000 directory; the stat failure
  must not be read as "no go.mod". Skips itself if the deny did not take effect,
  so it cannot pass vacuously.

The fixture root is built at `<tmp>/.temp/worktree` on purpose: this checkout
habitually lives under `.temp/`, and a walk testing the ROOT's name against the
dot rule would scan zero files while reporting clean.

## Mutation evidence — the new tests are load-bearing

Five mutants applied to `skipModuleDir`, each killed by the test that claims
that class. Guard file restored bit-identically after each (`diff -q`).

| Mutant | Killed by |
| --- | --- |
| dot/underscore rule deleted | `ScanScope` (3 paths reached + set-equality), `ReportsOnlyBuiltCode` ("This is the trunk failure verbatim"), `Narrowed` |
| nested-go.mod rule narrowed to never fire | `ScanScope`, `ReportsOnlyBuiltCode`, `Narrowed` ("no directory … is excluded") |
| exclusion widened to every subdirectory | `ScanScope` (positives unreached), `ReportsOnlyBuiltCode` ("the scan is vacuous"), `UnreadableIsNotAbsent` |
| root no longer exempt | `ScanScope` (scanned=[]), `DoesNotSkipTheRoot` (4 names) |
| stat failure read as absence | `UnreadableIsNotAbsent` |

Note the third and fourth: the bound is proven by NARROWING and by WIDENING,
not only by deleting the gate.

## Original failure shape, reproduced and confirmed silent

The whole real `.agents` tree (43 Go files, itself carrying
`.agents/tools/agents-infra/go.mod`) was copied from the bootstrapped main
checkout into the worktree.

- OLD walk restored over that tree: **FAIL, exit 1**, 12 violations including
  `.agents/tools/agents-infra/internal/infra/child_launch_composition.go:95
  [id-switch] func BuildChildLaunchComposition: a comparison against the plugin
  id "codex"` — the bug report's line verbatim, plus
  `primary_session_launch_plan.go`, `primary_session_prepare.go`,
  `project_config_setup.go` and `main.go`.
- NEW walk over the same tree: `go test ./... -count=1` → **exit 0**.

Both the dot rule and the nested-go.mod rule exclude that tree independently.

The scratch copy was removed afterwards (it is a copy of a machine-local tree,
not a deliverable). To recreate:

    cp -R /path/to/main-checkout/.agents ./.agents
    env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1
    rm -rf ./.agents

On a real bootstrapped main checkout no copy is needed — `.agents` is already
there.

## 58-mutant matrix

Run explicitly, exit 0, 66 named subtests = **58 caught mutants** + 8
residual-gap demonstrations:

| Test | Subtests |
| ---: | ---: |
| `CatchesIDSwitches` | 22 |
| `AgainstReviewMutants` | 14 |
| `CatchesShadowTables` | 13 |
| `CatchesVendorLayerBindings` | 6 |
| `CatchesCrossLayerBindings` | 3 |
| **caught-mutant total** | **58** |
| `ResidualGaps` (declared-open) | 8 |

`AcceptsOrdinaryCode` passes as a flat test. Nothing weakened.

## Gates — each run standalone, real exit codes

| Command | Exit |
| --- | ---: |
| `gofmt -l pkg/` (no output) | 0 |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |

Same four also run green with the real `.agents` present. The build artifact
`tools/agents-management/agents-management` was removed after the last build.
