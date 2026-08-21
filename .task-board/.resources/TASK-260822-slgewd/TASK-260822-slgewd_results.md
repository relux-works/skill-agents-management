# TASK-260822-slgewd — parity goldens and the comparison harness

Status: ready for review. Work left **uncommitted** for the Change Request
snapshot.

## What landed

| Path | What it is |
| --- | --- |
| `pkg/agentic/parity/testdata/goldens/*.json` | 14 goldens, one per `(system, case)` the source harness captures |
| `pkg/agentic/parity/testdata/goldens/README.md` | the boundary document: coverage, what the source could not capture, what the goldens do not prove |
| `pkg/agentic/parity/{doc,snapshot,mask,golden,compare,capture}.go` | the comparison harness |
| `pkg/agentic/parity/goldens_generate_test.go` | the gated generator (`PARITY_GOLDEN_CAPTURE_IN`) |
| `pkg/agentic/parity/{golden,mask,snapshot,compare,plan}_test.go` | 45 passing tests/subtests |
| `.scripts/capture-parity-goldens.sh` | runs the SOURCE harness under a pinned env, then the generator |
| `README.md` | the tools table plus a "Launch-surface parity goldens" section |

## AC1 — goldens from the real source harness, commit recorded

Captured by `skill-project-management`'s own
`tools/board-cli/internal/spawn/parity_capture_test.go::TestCaptureLaunchSurface`,
gated by `SPAWN_PARITY_CAPTURE_OUT`, at source commit
`ed4878123061b39fdae67160f6b5632117b48a2f` (clean tree, verified before and
after; the source checkout was read-only apart from that one `go test`).

Nothing in this repository captures anything. `pkg/agentic/parity` builds no
command, resolves no binary and filters no environment, and `capture.go` says so
at the top. The trap named in the brief — goldens captured by new code proving
only that new code agrees with itself — is closed by construction rather than by
intention.

Every file records `source_repo`, `source_commit` (full 40 chars, asserted),
`source_harness`, `capture_env_var`, `captured_by`, `parent_env`,
`parent_env_omitted`, `mask_rules` and `placeholders`.

Combinations: `claude/{prompt-mode,goal-mode}`,
`codex/{exec-default-path,exec-managed-npm-path,exec-native-shim,dry-run}`,
`qwen/{exec,dry-run}`, `muse/{exec,dry-run}`, `gemini/{exec,dry-run}`,
`agy/{exec,dry-run}`.

The task asked for one per `(system, mode)`; the fixtures keep the source's own
`(system, case)` granularity instead, which is a superset — three codex exec
cases pin three different branches of `resolveCodexBinary` that one
"codex/exec" fixture would collapse. `launch_mode` is recorded on each file, and
`TestGoldenLaunchModeAgreesWithTheStdinMarker` holds the label to the capture
path that produced it.

### One decision worth reviewing: the capture ran under a PINNED parent env

The source harness diffs the child environment against `os.Environ()`. Captured
under an ordinary shell, `env_removed` records whichever session variables the
operator happened to carry — the source's own `parity-after.json` has
`TASK_BOARD_BOARD_DIR=/Users/alexis/src/relux-works/skill-project-management/.task-board`
in it — and a key the plugin strips is provably stripped only on the machine
that happened to set it.

`.scripts/capture-parity-goldens.sh` therefore runs the harness under `env -i`
with a synthetic parent environment seeding exactly the keys the source's
filters act on (`filterCodexRuntimeEnv`, `filterQwenRuntimeEnv`,
`withSpawnEnv`), plus a stub-executable `PATH` so binary resolution is hermetic.
`TASK_BOARD_DIR` is deliberately absent so the child's injection of it is
visible.

This makes the fixtures reproducible and portable, and it costs one honest
deviation from the source's own artifact, written up in the fixtures README
("Dry-run binary resolution is pinned"): the source's dry-run cases recorded its
operator's installed codex and `executable file not found` errors for qwen/muse;
these record a successful resolution onto the stub. An `error` fixture would pin
a refusal instead of the dry-run argv grammar, which is the thing the ports have
to reproduce.

## AC2 — harness fails on any field difference, masking documented and pinned

`FromPlan(agentic.Plan, parentEnv)` maps a plan onto the source's snapshot
schema; `ComparePlan(golden, plan, subs)` is the one call a port drives. It
takes the parent environment from the GOLDEN, because env added/removed is a
diff and a port supplying its own baseline would be comparing a different
measurement wearing the same schema
(`TestAPlanBuiltWithoutTheGoldensParentEnvIsNotSilentlyAccepted`).

`Compare` reports every differing field and has no tolerance.
`TestCompareReportsEveryField` walks the `Snapshot` type by REFLECTION, mutates
one field at a time, and requires the difference to be reported *and named* — so
a field added to `Snapshot` later and forgotten in `Compare` fails on the day it
is added.

Masking is ported from the source's own: temp-dir noise, and PATH excluded from
the env diff (`diffEnv`'s rule verbatim). Three rules, pinned three ways:

- `TestMaskRuleSetIsFrozen` — the rule set is a frozen literal; adding one costs
  an edit plus a `Why` sentence.
- `TestMaskingCoversExactlyTheDeclaredFields` — plants a maskable literal in
  EVERY snapshot field and asserts exactly the declared fields changed. This is
  the "fails if masking widens" pin.
- `TestEveryMaskRuleIsNecessary` — narrows one rule at a time and requires at
  least one real golden to stop matching, so a rule that stopped carrying weight
  is visible.
- `TestDiffEnvExcludesPathAndOnlyPath` — narrows the env exclusion: `PATHEXT`,
  `MANPATH`, `GOPATH`, lowercase `path` must all still be reported.

## AC3 — a wrong plan demonstrably fails

`TestAWrongPlanFailsAgainstItsGolden` drives the REAL entry point —
`agentic.BuildPlan` through a real `agentic.Registry` — and plants three
separate defects in an otherwise-correct plan for `claude/prompt-mode`:

| Mutant | Defect | Field the harness must name |
| --- | --- | --- |
| one argv flag changed | `--dangerously-skip-permissions` → `--dangerously-skip-permission` | `Args` |
| one env key dropped | `TASK_BOARD_TASK_ID` never injected | `EnvAdded` |
| one stdin byte changed | last byte of the prompt flipped, argv identical | `StdinData` |

Each subtest first asserts the UNMUTATED plan matches, so a mutant cannot
"fail" for an unrelated reason.

The positive side (`TestProbePlansMatchTheirGoldens`) covers three shapes:
stdin bytes with an env diff (`claude/prompt-mode`), no stdin with temp paths in
argv (`muse/exec`), and a dry run resolving through the stub bin dir
(`muse/dry-run`).

The systems in that file are PROBES, not ports — documented as such at the top
of `plan_test.go`. They produce a plan through the production path so the
harness's ability to bite is established before the first port depends on it.

## AC4 — uncaptured combinations listed with the source's own reasons

`pkg/agentic/parity/testdata/goldens/README.md`, quoting the source's
`TASK-260817-1v9v95` results artifact:

1. **managed-session-args (the source's "Site 3")** — lives in package `cmd`;
   pre-refactor it had no `spawn.CodexArgs` to call from `package spawn`'s test.
   Its own proof is
   `cmd/codex_managed_session_args_parity_test.go::TestManagedCodexSpawnArgsMatchesTheHistoricalConstruction`.
   **Consequence: `agentic.LaunchModeManagedSession` has no golden at all.**
2. **the interactive-manager-client argv passthrough** (`cmd/codex_manager.go`,
   `cmd/claude_manager.go`) — a structurally different surface that filters
   user-typed argv rather than constructing it; out of scope for the source
   task's three named sites.

Plus what the goldens do not prove for the combinations they DO cover: PATH
content (the source recorded that qwen's child PATH value changed via
`sanitizeCodexPath` and its own harness could not see it); dry-run goldens carry
no env and no stdin because the source's `BuildArgs` path builds no command;
`agy/dry-run`'s binary is the literal `agy` placeholder because `BuildArgs` must
never trigger the Antigravity preflight; the parent env is pinned, so a key not
in it cannot appear in `env_removed`; the capture is darwin/arm64 and the two
codex binary-layout cases embed the platform triple.

`TestFixturesReadmeNamesWhatTheGoldensDoNotProve` holds that document to the
fixtures: the commit it quotes must match every golden, and the uncovered
surfaces must be named.

## Guard scan scope — stated explicitly, as asked

The goldens live under `pkg/agentic/parity/testdata/`, which
`singlesource_scanscope_test.go` excludes from the guard's walk. For these
fixtures that is correct: they are JSON captured by another repository, they
declare no binding, and the guard reports on what this module's Go build
compiles.

The rule it costs is the general one, and it is written in both `golden.go` and
the fixtures README: **a `.go` file placed under that directory would be
invisible to the guard**, so no Go source belongs there. The harness itself sits
one directory up, in `pkg/agentic/parity`, where the guard does scan it.

## Evidence

Gates, each run as a standalone foreground process:

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `gofmt -l pkg/` | 0, no paths listed |

45 tests/subtests pass in `pkg/agentic/parity`.

### Mutation evidence — the suite was checked for bite, not only for green

Six mutants applied and reverted; each was caught by the test written for it:

| Mutant | Caught by |
| --- | --- |
| masking widened to `StdinKind` | `TestMaskingCoversExactlyTheDeclaredFields` |
| env-diff exclusion widened to `HOME` | `TestDiffEnvExcludesPathAndOnlyPath` |
| `Compare` stopped looking at `StdinData` | `TestCompareReportsEveryField/StdinData`, `TestAWrongPlanFailsAgainstItsGolden/one_stdin_byte_changed` |
| a golden file removed | `TestEverySourceCombinationHasAGolden` |
| the stub-bin substitution disabled | `TestEveryMaskRuleIsNecessary/capture-stub-bin-dir` |
| masking stopped covering `Args` | `TestMaskingCoversExactlyTheDeclaredFields`, `TestEveryMaskRuleIsNecessary`, `TestProbePlansMatchTheirGoldens` |

The loader's refusals also have their own negatives
(`TestLoadDirRefusesAMalformedFixture`): wrong schema version, id/filename
disagreement, unparseable JSON, an unknown field, and an empty directory are
each an ERROR rather than a skipped entry — a suite silently covering one
combination fewer than it believes is the failure this package exists to
prevent, and a failed read is not a legitimate absence.

## For the three port tasks

1. Read `pkg/agentic/parity/testdata/goldens/README.md` first, especially "What
   the goldens do not prove".
2. Build the plan through `agentic.BuildPlan`, then:
   `parity.ComparePlan(golden, plan, parity.Substitutions{TempSlots: ..., StubBinDir: ...})`.
3. Feed `LaunchRequest.Env` from `golden.Capture.ParentEnv`. Do not invent a
   baseline.
4. `golden.Capture.Placeholders` tells you how many temp directories to allocate
   and in what order.
5. `LaunchModeManagedSession` has no golden. If your plugin declares that mode,
   it needs a frozen-reference proof of its own, in the source's style.
