# TASK-260822-3u97y3 — port-claude-plugin

Ready for review. Work left UNCOMMITTED for the CR snapshot.

## What shipped

`pkg/agentic/systems/claude` — the Claude Code agentic-system plugin, ported
from `skill-project-management/tools/board-cli/internal/spawn`
(`buildClaudeArgs` / `buildClaudeCommand`, the `AgentClaude` adapter row,
`claude_goal.go`, `validateClaudeLaunchCompositionPrefix`).

| File | What it owns |
|---|---|
| `claude.go` | plugin identity, registration, `Capabilities`, dispatch, mode-dependent stdin |
| `args.go` | THE single argv construction site, both modes |
| `goal.go` | the `/goal` directive, its refusal, and the preparation boundary |
| `env.go` | the one-key child-environment contract |
| `binary.go` | plain `PATH` resolution over the launch environment |
| `composition.go` | the `--mcp-config` JSON grammar validator |

Shared infrastructure extracted so the second plugin did not duplicate the
first: `internal/argvguard` (the argv scanner), `internal/gosources` (the
module-source walk), `internal/launchenv` (launch-env lookup and `PATH`
resolution).

Core reshape: `agentic.Goal` gained `ProviderCondition`.

## Acceptance criteria

### AC1 — Plans byte-match the claude goldens for both modes

`TestPlansMatchTheClaudeGoldens/claude/prompt-mode` and `/claude/goal-mode`,
both through a real `agentic.Registry` and `agentic.BuildPlan`, compared by
`parity.ComparePlan` (binary, argv, env added, env removed, stdin kind, stdin
data — no field forgiven). Both passed on first run.

`TestEveryClaudeGoldenIsCovered` fails if a recapture adds a claude case this
file does not build, and fails if the fixture set records no claude system at
all — so it cannot range over nothing.

`TestAWrongPlanFailsAgainstTheClaudeGolden` is what makes those mean anything:
four defects, each required to be reported in the field it was planted in — an
argv flag misspelled, the goal directive stripped of its condition, the goal id
never injected, one stdin byte changed.

Nothing in the parity file reads an expected value out of the golden it
compares to. The goal predicate is rendered from
`pkgboard.RenderGoalProviderCondition`'s format string, spelled in the test.

### AC2 — Env contract ported exactly

**The contract is ONE exact key: `CLAUDECODE`.** The spawn brief asserted the
opposite ("the codex family is stripped from claude children") and named the
goldens as the authority. The goldens settle it against the brief:

- source: `buildClaudeCommand` is `withSpawnEnv(filterEnv(os.Environ(),
  "CLAUDECODE"), cfg)` — `spawn.go:934`;
- `filterCodexRuntimeEnv` is a CODEX child's filter; `filterQwenRuntimeEnv` is
  that plus `CLAUDECODE` for a QWEN child (`adapter.go:376`);
- both claude goldens' `env_removed` is `CLAUDECODE` plus the four run-context
  keys `withSpawnEnv` replaces. Every seeded `CODEX_*`, both app-server /
  session-manager URLs, both `*_AUTH_TOKEN_ENV` pointers, the credentials they
  name, and `TASK_BOARD_SESSION_ID` all survive into the child.

That is the source's `BUG-260819-3qn52o` seen from the other side. It is pinned
(`TestTheClaudeChildKeepsTheCodexFamily`) rather than fixed: closing it here is
a behaviour change no golden covers, inside a port whose acceptance is
byte-identical launch surfaces.

Negatives aimed at this plugin, each with its narrowing:

| Test | Attack | Narrowing |
|---|---|---|
| `TestAWholeEnvironmentWipeFailsAgainstTheClaudeGolden` | `ChildEnv` returns only its injections | remove every survivor → bypass restored; remove only the INCIDENTAL survivors → the 4 seeded bystanders still catch it |
| `TestAPrefixStripFailsAgainstTheClaudeGolden` | strip `CLAUDECODE` by prefix | remove `CLAUDECODE_LIKE_BUT_NOT` → bypass restored, so the seeded near-miss is the sole catcher |
| `TestARunContextPrefixStripFailsAgainstTheClaudeGolden` | strip `TASK_BOARD_` by prefix (the shared `WithRunContext`) | two narrowings, because this defect over-strips several keys: all surviving `TASK_BOARD_` keys removed → bypass restored; only the incidental ones removed → the seeded near-miss alone still catches it |

Per-key narrowing battery: `TestEveryStrippedKeyIsCarriedByTheGolden` puts one
blocked key at a time back into the child and requires the golden to fail. The
list has one entry, and the measurement is worth exactly that: without it,
`CLAUDECODE=1` in `env_removed` would be equally consistent with the
run-context injection having removed it.

Two claims no golden can hold, each given its own test:
`TestTheClaudeChildKeepsTheCodexFamily` (the goldens show it only as an absence
from `env_removed`) and `TestTheClaudeChildPathIsNotSanitized` (the capture
excludes `PATH` from its diff entirely — the fixtures README names this exact
residual).

### AC3 — Goal-mode preparation: EXPLICITLY OUT, with the source behaviour referenced

`pkg/agentic/systems/claude/goal.go` carries the full statement. In summary:

**In the plan surface** — `--append-system-prompt-file <assignment>` plus the
positional `/goal <provider condition>` (`ClaudeGoalDirective`), and the absence
of stdin. Both are byte-compared against `claude/goal-mode`.

**Out of the plan surface** — `PreflightClaudeGoalLaunch` →
`PrepareClaudeGoalLaunch` (`spawn/claude_goal.go`):

1. validate the board goal contract (`pkgboard.ValidateGoalContract`), refuse an
   empty provider condition;
2. run `claude --version`, refuse below `ClaudeGoalMinimumVersion` = 2.1.139;
3. run `claude -p --output-format json /goal` as a capability probe, classified
   into `claude_goal_unavailable` / `claude_goal_workspace_untrusted` /
   `claude_goal_hooks_disabled` / `claude_goal_preflight_failed`;
4. decide the session action: `native_goal_bound` / `binding_retained` /
   `successor_required`.

Why it cannot be here: steps 2 and 3 START PROCESSES, and `agentic.BuildPlan`
calls every `System` method to build a DRY RUN. Preparation behind
`ResolveBinary`, `Argv` or `Stdin` would make a dry run execute the harness
twice — the exact drift dry-run-as-a-mode exists to prevent. Step 4 is not a
plan fact at all: it decides about a session that already exists, which the
source itself records by making `ClaudeGoalBinding.Rollback` a documented no-op.

Where it lands instead: the launch/session plane when that is ported, in the
position `PreflightClaudeGoalLaunch` holds in the source's command layer. This
task invents no home for it.

**One piece IS carried across**: step 1's empty-provider-condition refusal,
narrowed to what a plugin holding no board can check. Dropping it with the rest
would ship the literal argument `/goal ` — a child told it is goal-bound and
bound to nothing. `TestAGoalWithNoProviderConditionIsRefused`, with the
narrowing that a condition PRESENT is admitted.

### AC4 — Guard sees one binding file; argv single-site discipline

`TestSingleSourceGuardFindsNoSecondBinding` is green with the claude package in
scope, and the guard's reach list now names
`pkg/agentic/systems/claude/claude.go` and `.../codex/codex.go` — a scan that
did not reach the plugins would leave the guard's whole subject unguarded while
every other assertion stayed green. Mutant: a `map[agentic.SystemID]string` in
the claude package is reported at `[binding-table] pkg/agentic/registry.go is
the only place a SystemID binding may live`.

**One convention, not two scanners.** The codex guard's scanner moved to
`internal/argvguard`, and the module-source walk to `internal/gosources` (also
now used by `pkg/agentic`'s single-source guard, which had a third copy). Each
plugin supplies only the signature literals and the allowlist; the threshold
(ONE co-occurring literal), the resolution depth and the declared-open
residuals are one fact. Every codex mutant stayed green through the move.

**Allowlist keys are now FILE-SCOPED.** Both plugins name their construction
site `Args`, so the previous bare-name allowlist would have exempted every
`Args` in the module from every guard — the codex guard would have silently
lost the ability to report a second codex construction one directory over.
`TestTheAllowlistIsScopedToItsOwnFile` (codex) and the corresponding mutant
(claude) hold it.

**Signature selection.** `--dangerously-skip-permissions` and `--output-format`
are deliberately EXCLUDED: `buildAgyArgs` spells the first (`spawn.go:1185`) and
qwen and agy both spell the second, so including either makes claude's guard
fire on a legitimate sibling plugin the day one is ported. The residual that
leaves is named on `claudeArgvSignature` and held by
`TestTheDeclaredResidualStaysOpen`. `TestTheClaudeGuardDoesNotFireOnTheCodexPlugin`
measures the cross-plugin bound against the real neighbouring code.

## Divergences from the codex template, each named

| Divergence | Why |
|---|---|
| Plugin id `claude-code`, not `claude` | `claude-code` is the Layer-1 plugin id `docs/architecture.md` declares; `claude` is the frozen RUNTIME id and the source's `AgentType`, which is what the goldens record. `parity_test.go` maps between them in one place. |
| TWO launch modes, no managed session | The source registers no managed-session args builder for claude. `cmd/claude_manager.go` FILTERS argv a human typed rather than constructing it — the goldens' README names that surface as covered by nothing. Declaring the mode would be a capability claim with no construction behind it. |
| Stdin is MODE-DEPENDENT | A goal-bound launch attaches nothing; the assignment already reached the child as system context. No other ported system does this. |
| Binary resolution is one line | No managed package, no shim. Codex's machinery is not imported: three ordered candidates for a harness with one would all be dead code. |
| `SupportsBudget` true | Claude is the only agent in the source's table whose adapter sets it. No golden covers it, so it is proven against the source's construction. |
| Env filter is `agentic.SetEnvValue`, not a local loop | Whole-key matching already has one implementation in this module. |

## Residuals ported deliberately, each pinned

- The codex-family / credential leak into a claude child (`BUG-260819-3qn52o`).
- `PATH` is not sanitized (invisible to the goldens).
- A `Budget` of zero or less is DROPPED rather than refused, while `BuildPlan`
  has already admitted the launch as budget-bearing — the source's `> 0` guard.
  `TestAZeroBudgetIsDroppedRatherThanRefused`.

`HomeEnvVar` / `DefaultHome` are declarations for a caller, not something this
plugin writes: the source sets no home variable on a claude child, its adapter's
`DefaultHome` field held the documentation string `"Config.ChildWorkDir()"`, and
the goldens confirm the child inherits none. They are outside what the goldens
prove and that is stated where they are declared.

## Evidence

### Gates — all foreground, real exit codes

| Command | Exit |
|---|---:|
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `gofmt -l pkg/` | 0 (no output) |

### Mutation run — 12 mutants, all caught, tree restored green

`python3 .temp/TASK-260822-3u97y3/mutants.py` → exit 0. Logs at
`.temp/TASK-260822-3u97y3/mutants-*.log` (one per mutant, each recording the
real exit code and the tests that failed).

| Mutant | Caught by |
|---|---|
| `prefix-strip` | `TestAWholeEnvironmentWipeFails…`, `TestAPrefixStripFails…`, `TestPlansMatchTheClaudeGoldens`, `TestTheStripIsAnExactKeyNotAPrefix` |
| `no-strip-at-all` | `TestEveryStrippedKeyIsCarriedByTheGolden` |
| `goal-condition-refusal-dropped` | `TestAGoalWithNoProviderConditionIsRefused` |
| `goal-mode-also-streams-stdin` | `TestPlansMatchTheClaudeGoldens/goal-mode`, `TestAGoalBoundLaunchAttachesNoStdin` |
| `placeholder-binary` | `TestPlansMatchTheClaudeGoldens`, `TestTheDryRunReportsTheLaunchTarget` |
| `assignment-file-refusal-dropped` | `TestAGoalBoundLaunchWithoutAnAssignmentFileIsRefused` |
| `budget-flag-dropped` | `TestTheBudgetCeilingReachesArgv` |
| `composition-prefix-not-copied` | `TestThePluginDoesNotHandOutItsCallersBackingArray` |
| `composition-second-argument-admitted` | `TestAnInvalidCompositionIsRefusedByBuildPlan` |
| `composition-trailing-json-admitted` | `TestTheCompositionValidatorRefuses` |
| `second-argv-construction-site` | `TestClaudeArgvHasExactlyOneConstructionSite` |
| `shadow-binding-table` | `TestSingleSourceGuardFindsNoSecondBinding` |

The harness verifies every restore with `filecmp.cmp(shallow=False)` and re-runs
the suite at the end; it exits non-zero if the tree does not come back green.

## Notes for the reviewer

- **Uncommitted, as instructed.** `git log` head is still
  `5653b7d TASK-260822-hp5fb4`.
- **Pre-existing checkout artifact**: this worktree arrived with staged
  deletions (`D`) for the parity harness and codex plugin while the files are
  present on disk as untracked. It predates this task; I did not touch the index
  and did not restore, stash or clean anything. Every gate above runs against
  the files on disk.
- **Accepted files I edited, and why**:
  `pkg/agentic/systems/codex/argvguard_test.go` (delegate to the shared
  scanner; all mutants kept, allowlist keys file-scoped),
  `pkg/agentic/systems/codex/binary.go` (delegate the PATH/env primitives),
  `pkg/agentic/singlesource_guard_test.go` +
  `singlesource_scanscope_test.go` (delegate the walk; reach list widened),
  `pkg/agentic/system.go` (`Goal.ProviderCondition`).
- The parity package's claude PROBE (`pkg/agentic/parity/plan_test.go`) is left
  alone. That package documents its probes as not-ports, and `claude/prompt-mode`
  is its own mutation subject.
