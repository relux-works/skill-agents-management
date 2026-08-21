# TASK-260822-hp5fb4 — port-codex-plugin

Ready for review. Work left **UNCOMMITTED** for the CR snapshot.

## What shipped

`pkg/agentic/systems/codex/` — the Codex CLI agentic-system plugin, plus the
core reshapes it required.

| File | What it holds |
| --- | --- |
| `codex.go` | the `System` implementation, capabilities, stdin transport, `init()` registration into `agentic.Default` |
| `args.go` | **the one argv construction site** (`Args`) + the shared `-c` fragment + the service-tier vocabulary map |
| `binary.go` | the three binary-resolution paths, the platform package table, the shim unwrapper, PATH lookup over a supplied env |
| `env.go` | the exact-key strip family, the credential-pointer resolution, PATH sanitation, `childEnv` |
| `composition.go` | the codex TOML `-c` composition grammar validator |

Core (`pkg/agentic/`): `runcontext.go` (new), `system.go` (three added fields).

## Acceptance

### AC1 — plans byte-match the codex goldens for every captured combination

All four, through the real `agentic.Registry` + `agentic.BuildPlan`, compared
with `parity.ComparePlan`:

```
--- PASS: TestPlansMatchTheCodexGoldens
    --- PASS: .../codex/exec-default-path
    --- PASS: .../codex/exec-managed-npm-path
    --- PASS: .../codex/exec-native-shim
    --- PASS: .../codex/dry-run
```

`TestEveryCodexGoldenIsCovered` fails if a recapture adds a codex case this file
does not build, so the suite cannot cover one combination fewer than it believes.

`TestAWrongPlanFailsAgainstTheCodexGolden` plants one defect per recorded field
family (argv flag / injected env var / one stdin byte) and requires each to be
reported **in the field it was planted in**.

### AC2 — three binary-resolution paths, hermetic stub layouts

Managed npm root → npm shim unwrapped to its native binary → plain PATH. Each
covered by a golden AND by stub-layout tests that attack the ORDER and the
preconditions:

- managed beats PATH; a managed root whose binary is absent falls through; a
  managed candidate that is a DIRECTORY falls through; both candidate layouts
  (npm-parent and package-root) resolve; an unset root resolves nothing.
- the shim unwraps; a shim with no native binary beside it resolves to the shim;
  both shape checks (`codex.js`, `bin/`) narrowed one at a time with a positive
  counterpart.
- PATH: a non-executable candidate does not shadow the real binary; an absent
  PATH and an empty PATH are different errors.
- `TestResolveBinaryIsTheSameAnswerForEveryLaunchMode` — exec, dry-run and
  managed-session resolve the same target, which is the source's own historical
  bug (a hardcoded display string drifting from the launch target).

No `t.Setenv` anywhere; every test runs `t.Parallel()` against a temp layout.

### AC3 — one argv construction site; guard accepts exactly one binding file

- `args.go`'s `Args` is the only place codex flags are spelled, for both
  grammars. `argvguard_test.go` scans **every non-test Go file in the module**
  (the source's guard scanned two packages, because its three drifted sites
  lived in two) and reports any function outside a reasoned allowlist that
  spells one signature literal. Threshold is **one**, not two — the source's
  review proved a copy-pasted fourth site spells only one.
  - `TestTheArgvGuardFiresOnTheRealConstructionSite` narrows the gate: with
    `Args` removed from the allowlist the real `Args` must be reported.
  - 10 mutant spellings caught (full construction, single literal, package const,
    function-local const, package var table, a function referencing it, a closure
    in a table, `init()`, a method, a call-wrapped literal in a body).
  - 3 declared-open residuals demonstrated staying open.
  - `TestTheArgvAllowlistIsJustified` — every allowlisted name carries a reason
    and exists in the module.
- The MODULE guard (`pkg/agentic/singlesource_guard_test.go`) stays green with
  the plugin registered: `bindingHomes["SystemID"]` is still the single file
  `pkg/agentic/registry.go`. The plugin binds through `agentic.Register` and
  declares no table and no id dispatch.

### AC4 — env filtering exact; the source's open leaks stay open and named

- Strips are an exact-key map. `TestFilterEnvKeysMatchesWholeKeys` states it at
  unit level; `TestEveryStrippedKeyIsCarriedByTheGolden` narrows the blocked
  list one key at a time (11 subtests) and requires the golden to fail for each.
- The two credential POINTERS are resolved and what they name is blocked, with
  the negative that nothing beyond them is stripped.
- PATH sanitation has its own test in both directions, because the goldens
  exclude PATH from the diff entirely — the fixtures' README says so and says a
  port changing what it strips from PATH needs its own evidence.
- **Open leaks stay open and are pinned**, not silently fixed:
  `TestTheSourcesOpenEnvLeaksStayOpen` asserts `TASK_BOARD_TOKEN`,
  `TASK_BOARD_BUILDER_GATEWAY_TOKEN` and `QWEN_CODE_SESSION_ID` still reach the
  codex child, naming `BUG-260819-3qn52o` and why it cannot be closed one plugin
  at a time (its own AC3: the construction must make a seventh runtime safe
  without editing N filters).

### The permanent negatives, pointed at this plugin

Both parity-package negatives re-run against the **real registered plugin**
rather than a probe:

- `TestAWholeEnvironmentWipeFailsAgainstTheCodexGolden` + two narrowings.
- `TestAPrefixStripFailsAgainstTheCodexGolden` + narrowing.
- `TestThePluginPreservesEveryBystander` — the same bound stated positively.
- The parity package's own `TestAWholeEnvironmentWipeFailsAgainstQwenExec` and
  `TestAPrefixStripFailsAgainstQwenExec` still pass unchanged.

## Negative evidence: remove, red, restore, report

`.temp/TASK-260822-hp5fb4/mutants.py` — 31 mutants, each narrowing or removing
ONE production gate, full suite run per mutant, sources restored from a pristine
in-memory copy, clean suite re-verified at the end.

| Run | Result | Log |
| --- | --- | --- |
| 1 | **29/31** — 2 survivors | `.temp/TASK-260822-hp5fb4/mutants-01.log` |
| 2 | **31/31**, checkout restored GREEN | `.temp/TASK-260822-hp5fb4/mutants-02.log` |

Both survivors were tests passing for the wrong reason, and both are recorded in
the logbook (entry 2132):

1. **The composition closed-field check.** The negative substituted an unknown
   field FOR the url, so the per-transport shape rule refused it and the field
   check was never exercised. Fixed by ADDING the unknown field to an otherwise
   valid composition — only the closed-set check can refuse that.
2. **`childEnv`'s filter-then-inject order.** The comment claimed the order was
   load-bearing; swapping it left the suite green, because no key in the codex
   blocked set is also a run-context key. **The comment was wrong, not the
   tests.** Corrected to state what is true today, plus
   `TestNoStrippedKeyCollidesWithAnInjectedOne` pinning the non-overlap, so the
   day a collision arrives it is a failing test naming the comment rather than a
   child that silently lost its run id.

## Reshapes from the source — every one named

1. **Binary resolution reads `LaunchRequest.Env`**, not `os.Getenv` /
   `exec.LookPath`. Required, not preferred: the `System` contract promises a
   plugin holds nothing ambient and that a dry run reports the launch's real
   target, and neither survives a process-global. Invisible in the goldens —
   the capture's own managed-npm case set the package root with `t.Setenv`
   AFTER recording its baseline, so the fixture carries the pinned placeholder
   while the code under capture saw a temp layout; a port test that repoints the
   launch environment reproduces exactly that, and the key is stripped from the
   child either way.
2. **`LaunchRequest` gained `Run RunContext` and `Profile`.** The goldens record
   `TASK_BOARD_RUN_ID`, `TASK_BOARD_TASK_ID`, `TASK_BOARD_BOARD_DIR`,
   `TASK_BOARD_DIR` in `env_added` and `-p <profile>` / `--add-dir <board>` in
   the argv. A plugin that reaches for nothing ambient cannot produce any of
   them unless they arrive on the request.
3. **`CompositionServer` gained `BearerTokenEnvVar`.** Without it the codex
   validator can only DROP its two bearer checks — a validating gate quietly
   getting weaker across a move.
4. **`agentic.WithRunContext` / `SetEnvValue` live in the CORE.** The source had
   exactly one `withSpawnEnv` shared by all six adapters; six copies here would
   be six sources for one fact and the drift would be invisible.
5. **`GrammarID` is a plugin-local opaque string**, per the core's own reshape
   note, so a novel composition shape is a plugin rather than a core edit.
6. **Dry-run is a MODE of `Args`, not a sibling function** — the source's
   `codexDryRunArgs` body is a call to the exec construction, and
   `TestTheExecAndDryRunGrammarsAreOneGrammar` holds that.
7. **New dependency: `github.com/pelletier/go-toml/v2 v2.4.3`** — the same
   library and version the source uses for `validateCodexTOMLStringArray`. A
   hand-rolled array reader would disagree with the real parser at the edges
   (multi-line arrays, literal strings, trailing commas), and a validating gate
   that disagrees with what it validates for is worse than none.

## Carried-over behaviours, pinned rather than improved

Three source behaviours that read as defects are kept and asserted as-is, each
with the reason changing them inside a parity port would be wrong:

| Behaviour | Test |
| --- | --- |
| `BUG-260819-3qn52o`'s two open child-env leaks | `TestTheSourcesOpenEnvLeaksStayOpen` |
| an unconfigured tier lets the parent's inherited value pass through | `TestAnUnconfiguredTierLeavesTheInheritedValueAlone` |
| an unrecognized tier is DROPPED, not refused | `TestAnUnknownServiceTierIsDropped` |

The third sits awkwardly beside this repo's own `ErrServiceTierUnsupported` rule
("a dropped parameter produces a run that looks like the one that was asked for
and is not"). It is pinned where a reader will find it; changing it is a
behaviour decision for whoever owns the tier vocabulary, not for this port.

## Managed-session mode

Declared, and proven the source's way because **no golden covers it** — the
source's capture harness lived in `package spawn` and that surface lived in
`package cmd`. `TestManagedSessionArgsMatchTheFrozenConstruction` compares
`Args(..., LaunchModeManagedSession)` against a frozen byte-for-byte copy of the
pre-refactor `managedCodexSpawnArgs` across 36 combinations of profile × effort ×
tier. `TestTheFrozenConstructionAndArgsAreNotTheSameCode` guards that the frozen
copy is never "cleaned up" into a call to the thing it checks.

## Gates — all foreground, real exit codes

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `gofmt -l pkg/` | 0 (no files listed) |

```
ok  github.com/relux-works/skill-agents-management/internal/ident
ok  github.com/relux-works/skill-agents-management/pkg/agentic
ok  github.com/relux-works/skill-agents-management/pkg/agentic/parity
ok  github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex
ok  github.com/relux-works/skill-agents-management/pkg/vendorplugin
ok  github.com/relux-works/skill-agents-management/tools/agents-management/cmd
```

## Notes for review

- **The CLI is NOT wired to the plugin.** `tools/agents-management/cmd` does not
  import it, so `TestBuiltBinaryListsEmptyPluginsWithoutError` and its JSON
  sibling still pass and the shipped binary still answers `plugins` with `[]`.
  Wiring it is a behaviour change to the shipped CLI that no task in this story
  asks for; it should be its own decision, not a side effect of a port.
- The pre-existing staged-deletion state of `pkg/agentic/parity/*` and
  `.scripts/` in `git status` was in the checkout before this task and was not
  touched.
- Source checkout (`skill-project-management`) is clean and untouched; nothing
  was run inside it.
