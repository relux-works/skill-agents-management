# TASK-260822-xz8rj5 — port-qwen-gemini-muse-agy-plugins

Four agentic-system plugins ported from `skill-project-management`'s spawn
adapter table, proven against the eight launch-surface goldens that repository's
own harness captured at `ed4878123061b39fdae67160f6b5632117b48a2f`.

Work is UNCOMMITTED, as the task requires.

## The batch rule: all four held the simple shape

The task allowed one task for four plugins "because they share the simple shape;
if any of them turns out not to, stop and say so rather than forcing it into the
batch." None of them broke it. Two came close enough to be worth naming:

- **agy** needed one contract decision that the other three did not: where the
  Antigravity preflight's result lives. It is NOT a forced fit — the source's own
  `resolveAgyBinaryForDisplay` comment states the same constraint this port ran
  into ("must never trigger the Antigravity preflight"), and the resolution is
  the same boundary the accepted claude port already drew for goal preparation.
  It is written up in `pkg/agentic/systems/agy/runtime.go` rather than buried.
- **muse** is the system whose VENDOR the source records as unknown. That is
  Layer-2's business and already settled there; nothing about it reached this
  plugin, and `TestNothingAboutTheVendorReachesThisPlugin` holds the declaration
  to that.

## Acceptance

### 1. Plans byte-match each system's goldens

Eight goldens, eight cases, all through the real `agentic.Registry` and
`agentic.BuildPlan` — no plan is constructed by hand and no expected value is
read out of the fixture it is compared to.

| System | Goldens | Test |
| --- | --- | --- |
| qwen | `qwen/exec`, `qwen/dry-run` | `TestPlansMatchTheQwenGoldens` |
| gemini | `gemini/exec`, `gemini/dry-run` | `TestPlansMatchTheGeminiGoldens` |
| muse | `muse/exec`, `muse/dry-run` | `TestPlansMatchTheMuseGoldens` |
| agy | `agy/exec`, `agy/dry-run` | `TestPlansMatchTheAgyGoldens` |

Each plugin also carries `TestEvery<X>GoldenIsCovered` (a recapture that adds a
case cannot leave the suite green while covering less) and a mutant set spread
across every field family the harness records — argv flag, argv ARGUMENT,
injected environment variable, stdin — each required to fail IN THE FIELD IT WAS
PLANTED IN.

### 2. Qwen env strips preserved

- The FIXED leak stays fixed. `CLAUDECODE` is stripped, and the strip is proved
  three ways: directly (`TestTheQwenChildLosesTheParentClaudeMarker`), by
  narrowing against the golden (it is one of the twelve keys in
  `TestEveryStrippedKeyIsCarriedByTheGolden`, all twelve of which must redden the
  fixture when put back), and by the mutation harness.
- The codex half is SINGLE-SOURCED in `internal/runtimeenv`, shared with the
  codex plugin — because the source shares it too and says the sharing is
  deliberate on `filterQwenRuntimeEnv`.
- THREE open leaks stay open and are named in `env.go` and pinned by
  `TestTheSourcesOpenEnvLeaksStayOpen`: `TASK_BOARD_TOKEN`,
  `TASK_BOARD_BUILDER_GATEWAY_TOKEN`, and `QWEN_CODE_SESSION_ID` — the last of
  which is the reverse leak pointing at qwen ITSELF, not only at its neighbours.
- Both permanent parity negatives (whole-environment wipe, family-prefix strip)
  are driven through the shipped plugin, each with the narrowing that shows
  which seeded bystander catches it, plus the pointer-resolution consequence.

### 3. Agy's effort-in-model-ID and preflight binary, ported honestly

- Effort is a SUFFIX of the model id (`gemini-3.6-flash-high`); every agy model
  row in the source is `ReasoningNone` and `buildAgyArgs` reads no effort value.
  The plugin declares `EffortTransportNone` and never parses the suffix. Both
  refusals — required-effort model, and an explicit effort value — are decidable
  from the contract and driven through `BuildPlan`. A made-up suffix pins the
  pass-through.
- The binary comes from `AgyRuntime` preflight evidence when populated, with the
  `agy` placeholder fallback when it is not. Evidence travels on the plugin value
  (`agy.NewWithRuntime`), because the core request has no field for it and must
  not grow an agy-shaped one.
- `BuildPlan` NEVER triggers the preflight, and that is measured rather than
  asserted: `TestADryRunPerformsNoWork` builds a dry-run plan over an assignment
  file that does not exist and an environment carrying no PATH, and requires the
  same request under exec to fail.
- The source's hard failure survives the move: `Argv` refuses an EXEC launch with
  no evidence (`ErrRuntimeNotPreflighted`), because `ResolveBinary` is
  deliberately mode-less. `agy/dry-run` pins the placeholder branch and
  `agy/exec` pins the evidence branch, so both are fixture-backed.
- The ARG_MAX budget is ported with it, and proved by NARROWING as well as by an
  oversize prompt.

### 4. One binding file per system, public API registration only

- Every plugin registers through `agentic.Register` in `init()` and nowhere else.
  `TestThePluginRegistersIntoTheDefaultRegistry` drives the production path for
  each; the harness's `plugin-not-registered` mutant confirms it would fail.
- `pkg/agentic/singlesource_guard_test.go` now names all six plugin binding files
  in the list the scan must reach, so a seventh plugin added without a line there
  is a gap somebody has to notice.
- The shadow-binding mutant (`map[agentic.SystemID]` inside the agy package) is
  reported by `TestSingleSourceGuardFindsNoSecondBinding`.

## Two shared internals extracted, one test harness

| Package | Why | Shared by |
| --- | --- | --- |
| `internal/runtimeenv` | the source single-sources the codex-family strip and says so | codex, qwen |
| `internal/mcpjson` | the source validates three systems' compositions with ONE function, and the claude port said this validator would have to be shared before there was anything to share it with | claude, qwen, agy |
| `internal/paritycase` | golden machine-state scaffolding, 2 copies becoming 6 | qwen, gemini, muse, agy |

`internal/mcpjson`'s refusal messages name the GRAMMAR rather than a system —
the one deliberate divergence from the source's text, which says "Claude" even
when refusing an agy composition.

`internal/paritycase` leaves a stated residual: the codex and claude plugins keep
their own copies of these helpers rather than being rewritten under this task,
because rewriting a harness beneath an accepted parity proof would put the proof
and the change in one commit.

## Two findings worth the logbook

1. **The cross-plugin argv bound found a collision nobody predicted.**
   `--add-dir` was in agy's signature until the guard reported the CODEX plugin's
   own `Args`. Codex spells it too. The hand-applied rule from the claude port
   ("check each literal against the other agents' builders") had already been
   applied and still missed it.
2. **An aliasing test that lets the append reallocate proves nothing.** The
   first mutation run had exactly one survivor: `qwen-composition-prefix-not-copied`.
   The test compared `args[0]` against the caller's `prefix[0]`, and qwen's eight
   appended arguments overflowed the capacity, so Go reallocated and the aliasing
   plugin looked correct. Rewritten against the caller's BACKING ARRAY and added
   to all four plugins.

## Negative evidence

`python3 .temp/TASK-260822-xz8rj5/mutants.py` — **33 mutants, 33 caught, 0
survivors**, tree restored green. Every gate this task wrote is in it: the qwen
env strip and its stdin protocol, gemini's and muse's placeholder substitution
and composition refusals, agy's preflight gate, placeholder binary, ARG_MAX
budget (deleted AND narrowed), assignment refusals, both shared internals' every
refusal branch, the argv guards and the single-source guard.

No mutant is caught by the compiler: three that originally failed to build were
rewritten to compile so that a TEST has to catch them. Per-mutant logs naming the
first failing test are in `.temp/TASK-260822-xz8rj5/mutants-*.log`.

## Gates

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `gofmt -l pkg/` | 0 (no output) |
| `gofmt -l internal/` | 0 (no output) |
| `python3 .temp/TASK-260822-xz8rj5/mutants.py` | 0 |

## Scope

New: `internal/{runtimeenv,mcpjson,paritycase}/`,
`pkg/agentic/systems/{qwen,gemini,muse,agy}/`.

Modified: `pkg/agentic/systems/codex/{env.go,binary.go}` (delegate to the shared
strip; the accepted codex test file compiles and passes unchanged),
`pkg/agentic/systems/claude/composition.go` (delegate to the shared grammar; the
accepted claude test file compiles and passes unchanged),
`pkg/agentic/singlesource_guard_test.go`, `README.md`, `LOGBOOK.md`.
