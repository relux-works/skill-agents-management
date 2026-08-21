# TASK-260822-xz8rj5 — review verdict: ACCEPTED

Change Request `CR-TASK-260822-xz8rj5-1` revision 1, `repository_delta=present`.

- Candidate tree: `c863fc61479f6d9a76b8b5aee959a7d4979340b3`, verified by
  re-deriving the tree from the working checkout — it matches the CR OID exactly,
  so what was reviewed is what was snapshotted. Re-verified after every mutation
  below: the tree is back at `c863fc6`.
- Base: `be44d86`. That base spans three already-accepted commits, so the delta
  this task actually owns is `HEAD (aea0fb2) → worktree`: six modified files and
  41 new ones. The eight goldens for these four systems were committed by
  `TASK-260822-slgewd` and are **untouched** by this delta — the ports were made
  to match pre-existing fixtures rather than fixtures regenerated to match ports.
- Source checkout `skill-project-management` at `ed48781` — the exact commit the
  goldens record — clean, read-only, used only for comparison.

## Gates, re-run in this review, foreground

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 (16 packages ok) |
| `gofmt -l pkg/ internal/` | 0, no output |
| `python3 .temp/TASK-260822-xz8rj5/mutants.py` | 0 — **33/33 caught, tree restored green**, the producer's claim reproduces |

## The three judgement calls

### 1. AGY — the preflight boundary, and the measurement

The contract decision holds and the justification is the source's own, not a
rationalization. `resolveAgyBinaryForDisplay` (adapter.go:236-251) says in as
many words that it "must never trigger the Antigravity preflight (BuildArgs
promises no side effects)", and it returns the bare `"agy"` placeholder rather
than failing. The source's *other* function, `resolveAgyBinary` (adapter.go:224-
234), hard-fails with `AgyCapabilityMissingBinary` and is reached only from
`buildAgyCommand` (spawn.go:1113). The port maps those two onto one mode-less
`ResolveBinary` plus one mode-aware refusal in `Argv`, and argues it in
`runtime.go` rather than burying it. That is the same boundary the accepted
claude port drew for goal preparation.

**BuildPlan cannot touch the preflight, and it is not asserted — it is
structural.** The preflight is not ported into this module at all, and the agy
package contains no `exec.LookPath`, no `exec.Command` and no `os.Stat`; the
only filesystem call is `os.ReadFile` in `args.go:136`, reachable exclusively
from the `LaunchModeExec` branch of `Args`. Independently confirmed: neither
`pkg/agentic` nor `internal/launchenv` calls `os.Environ`, `os.Getenv` or
`exec.LookPath` anywhere — `launchenv` reimplements PATH lookup over the
environment it is *handed*, so there is no ambient read on any plan path.

Both halves of the measurement run: `TestADryRunPerformsNoWork` builds a plan
over a `t.TempDir()` path that was never written, with `Env: nil`, and requires
the identical request under exec to fail. `paritycase.TryBuildPlan` drives a
real `agentic.NewRegistry()` + `agentic.BuildPlan`, and `BuildPlan` has no
`os.Environ()` fallback for a nil `req.Env`, so "no PATH at all" is literally
true rather than nominal.

Gates attacked by **narrowing**, not deleting:

| Mutant | Result |
| --- | --- |
| `mode == exec && runtime.IsZero()` → `… && req.WorkDir == ""` (narrowed) | CAUGHT — `TestAnExecLaunchWithoutThePreflightIsRefused`, `TestWhitespaceIsNotEvidence`, `TestTheDefaultRegistryHoldsThePreflightlessValue` |
| `EffortTransportNone` → `EffortTransportArgv` (widened) | CAUGHT — `TestARequiredEffortModelIsRefused`, `TestAnEffortValueAloneIsRefused`, `TestTheDeclarationIsWhatTheSourceRegistered` |
| empty-assignment refusal → `return "", nil` | CAUGHT — `TestAnExecLaunchWithNoAssignmentIsRefused` |
| `IsZero` trim dropped | CAUGHT |
| `--mode accept-edits` → `accept_edits` | CAUGHT in the argv field, both goldens |

Effort-in-model-id is ported honestly: `Args` passes `req.Model.ID` through
verbatim and the package never parses a suffix, which matches every agy model
row in the source being `ReasoningNone` and `buildAgyArgs` never reading an
effort value. `HomeEnvVar`/`DefaultHome` are empty, and that is correct rather
than lazy — `providerHomeRules` (pkg/providerlimits/identity.go:88-92) has rows
for codex, claude and qwen-codex only, and its own comment refuses to guess.

### 2. QWEN — the fixed leak, the shared half, the pinned-open leaks

`filterQwenRuntimeEnv` in the source is `filterCodexRuntimeEnv(filterEnv(environ,
"CLAUDECODE"))`, and its comment states the sharing is deliberate. The port
single-sources the codex half in `internal/runtimeenv` (11 keys, matching
spawn.go:1026-1038 exactly, including both credential-*pointer* resolutions and
`sanitizeCodexPath`) and adds `CLAUDECODE` in `qwen/env.go`, where a reader can
see the difference without following an import. The single-source guard treats
that as **one** binding home.

| Mutant | Result |
| --- | --- |
| CLAUDECODE strip removed (fixed leak reintroduced) | CAUGHT — `TestPlansMatchTheQwenGoldens/qwen/exec` plus both permanent negatives |
| `QWEN_CODE_SESSION_ID` **added** to the strip | CAUGHT — `TestTheSourcesOpenEnvLeaksStayOpen` reds. The pin bites in the closing direction, which is the whole point: the fix belongs to the source's `BUG-260819-3qn52o`, not to one plugin |
| extra env var injected into the child | CAUGHT in the env field |
| session-id fallback chain reordered (RunID↔TaskID) | CAUGHT — the golden pins the stdin frames byte for byte, including `RUN-parity-qwen-initialize` |
| stdin read failure returned as an absence | CAUGHT — `TestAnUnreadablePromptIsAnErrorRatherThanAnAbsence` |
| `EffortTransportStdin` → `None` | CAUGHT |

The stdin stream-json payload is golden-pinned in full (`qwen_exec.json`
`stdin_data`), so the effort field, the frame order and the JSON key order are
all under fixture control rather than under a hand-written expectation.

### 3. GEMINI / MUSE / AGY — a no-strip system is where a wipe hides

Confirmed for **each of the three**, not just by reading the negative tests but
by mutating the shipped `childEnv` and watching the parity comparison red:

| System | wipe (`WithRunContext(nil, req)`) | borrowed codex filter (`runtimeenv.Filter(parent)`) |
| --- | --- | --- |
| gemini | CAUGHT — golden + `TestTheEmptyFilterIsDeliberate` | CAUGHT |
| muse | CAUGHT | CAUGHT |
| agy | CAUGHT | CAUGHT |

The seeded bystanders (`CODEX_LIKE_BUT_NOT`, `CLAUDECODE_LIKE_BUT_NOT`,
`TASK_BOARD_LIKE_BUT_NOT`) do the work a prefix-strip port would need them to do,
and `runtimeenv`'s whole-key matching is separately pinned.

## Standard battery

- **Goldens re-derived**, all four systems, both cases each, through the real
  `Registry` + `BuildPlan`. Argv, binary, stdin kind, stdin bytes, env added and
  env removed hand-compared against the source's `buildAgyArgs` (spawn.go:1177),
  `buildGeminiArgs` (1202), `buildQwenArgs` (1211), `buildMuseArgs` (1157) and
  the four adapter rows (adapter.go:100-179) — flag spelling, argument order and
  the conditional `--add-dir` / `--include-directories` / `--workspace` tails all
  match.
- **Argv perturbation**: agy `--mode` value misspelled, gemini `-y`/`--skip-trust`
  order swapped → both CAUGHT in the argv field, in both modes.
- **Env addition**: muse and qwen each given one extra child variable → both
  CAUGHT in the env field.
- **One binding file per system**: a shadow `map[agentic.SystemID]…` planted in
  each of qwen, gemini, muse and agy → all four CAUGHT by
  `TestSingleSourceGuardFindsNoSecondBinding`. The guard's scan-scope list now
  names all six plugin binding files.
- **Public-API-only registration**: `agentic.Register` in `init()` is the only
  registry call in any plugin, and every `agentic.*` identifier the four packages
  reference is exported. Deleting the `Register` call in each of the four →
  `TestThePluginRegistersIntoTheDefaultRegistry` reds for that system (agy also
  reds `TestTheDefaultRegistryHoldsThePreflightlessValue`).
- **Cross-plugin argv guard**: a second muse-signature site planted in gemini and
  a second agy-signature site planted in qwen → both CAUGHT, and the
  "does-not-fire-on-the-others" companion reds too.
- **Composition gates, four branches narrowed rather than deleted** — and each
  reaches every plugin that declares the grammar, through `BuildPlan`:

| Mutant | claude | qwen | agy | mcpjson |
| --- | --- | --- | --- | --- |
| `len(prefix) != 2` → `len(prefix) < 2` (admits a second top-level argument) | red | red | red | red |
| bearer `Authorization` value check dropped, count check kept | red | red | red | red |
| `DisallowUnknownFields()` removed | red | — | — | red |
| trailing-JSON check disabled (compiling mutant) | red | — | — | red |

- **Delegation to the shared internals does not weaken the accepted plugins.**
  `claude/composition_test.go`, `codex/env_test.go` and `codex/binary_test.go`
  are **byte-unchanged** in this delta and pass against the extracted code, which
  is the only evidence that mattered here. The one stated divergence — mcpjson's
  refusals naming the grammar instead of "Claude" — is not masked: the accepted
  claude test asserts only that the grammar id appears in the wrapped error.
- **Batch-rule honesty**: no forced fit found. The four share PATH-or-preflight
  resolution, a single argv site, a declared composition posture and a stated
  env posture; agy's one extra contract decision is named in the results and
  argued in `runtime.go`, and muse's unknown vendor genuinely does not reach the
  plugin. No divergence surfaced in the code that the results artifact fails to
  name.

## Residuals — accepted, not blocking, named for the next toucher

Two mutants **survived** my battery. Neither is a shipped defect: I checked both
values directly against the pinned source and both are correct. What is missing
is the assertion that would keep them correct.

1. **`pkg/agentic/systems/agy/args.go:51` — `argvBudgetBytes = 96 * 1024` is not
   pinned by value.** Widening it to `192 * 1024` leaves the whole suite green.
   Every budget test (`TestTheArgvBudgetRefusesAnOversizePrompt`,
   `…AdmitsAPromptJustUnderIt`, `TestTheArgvBudgetCountsTheBinaryToo`) sizes its
   prompt *relative to the constant*, so the gate's operator, its measurement and
   the binary being counted are all proven, but the bound itself is asserted only
   against itself. The value does match the source's `agyArgvBudgetBytes`
   (spawn.go:1174), and no golden covers it because both agy fixtures carry short
   prompts. One assertion of the literal closes it.
2. **`pkg/agentic/systems/qwen/stdin.go:52` — `defaultSessionID = "task-board-qwen"`
   is not pinned by value.** `TestTheSessionIDFallbackChain` pins the chain order
   and the whitespace rule but expects `defaultSessionID` symbolically, so any
   literal survives. The value matches the source (spawn.go:1231); the golden
   uses the RunID branch, so the fallback literal never reaches a fixture.

Both are the same shape — *a source literal outside golden coverage, asserted
only against itself* — and they are the only two instances in the four plugins.
Every other constant here (`trackedPrintTimeout`, `promptPlaceholder`,
`promptFilePlaceholder`, `displayPlaceholder`, `initializeRequestSuffix`, all
flag spellings) reaches a golden and is fixture-pinned. Recorded rather than
sent back: the AC does not cover the bound values, the gates themselves are
attacked and hold, and both values are verified correct in this review.

## Acceptance

All four acceptance criteria are met and were attacked rather than read.
Recorded with `accept_cr(TASK-260822-xz8rj5, revision=1)`. Work remains
uncommitted; the commit and the `done` transition with `commit_ack` belong to the
orchestrator.
