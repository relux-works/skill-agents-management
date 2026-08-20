# TASK-260821-21vywo — agentic-system plugin contract and registry

Layer 1 of `agents-management`: the `AgenticSystem` plugin interface, the
registry that is the only place a binding may live, the go/ast single-source
guard, and one test double proving the seam. No concrete plugins, no vendor
layer, no spawn execution.

Work is **uncommitted** in the story worktree, as instructed.

## What shipped

| Path | Role |
| --- | --- |
| `pkg/agentic/system.go` | contract types: `SystemID` + its single normalization, `System`, `Capabilities`, `LaunchMode`, `EffortTransport`, `EffortSupport`, `GrammarID`, `Composition`, `StdinPayload`, `LaunchRequest`, `Model`, `Goal`, `Budget` |
| `pkg/agentic/registry.go` | `Registry`, `Register`, `Lookup`, `IDs`, package-level `Default`; the **only** file the guard permits to hold a binding |
| `pkg/agentic/plan.go` | `Plan` (the parity surface) and `BuildPlan` — the single dispatch site, plus every contract-level refusal |
| `pkg/agentic/singlesource_guard_test.go` | the go/ast scanner, the whole-module scan, the reach proof, the allowlist-narrowing proof |
| `pkg/agentic/singlesource_guard_mutants_test.go` | 9 shadow-table spellings, 9 id-switch spellings, an ordinary-code control, 3 declared-open residuals |
| `pkg/agentic/double_test.go` | the `pangolin` test double and the every-surface seam proof |
| `pkg/agentic/registry_test.go`, `plan_test.go`, `contract_test.go` | registration and launch refusals, contract-level effort decidability |
| `tools/agents-management/cmd/plugins.go` | `plugins` now reads the registry; its private list is gone |

## AC1 — interface covers every field `adapterTable` declares

Source: `skill-project-management/tools/board-cli/internal/spawn/adapter.go`.

| adapterTable field | Here | Note |
| --- | --- | --- |
| `Agent` | `System.ID() SystemID` | normalized at every boundary |
| `ResolveBinary` | `System.ResolveBinary(LaunchRequest)` | resolver stays per-system code (managed npm, native shim, preflighted executable) |
| `BuildCommand` | decomposed into `Argv` + `ChildEnv` + `Stdin` + `Plan.WorkDir` | **reshape**, see below |
| `DryRunArgs` | `Argv(req, LaunchModeDryRun)` | **reshape**, see below |
| `EffortTransport` | `Capabilities.EffortTransport` (`none`/`argv`/`stdin`) | transport only; vocabulary stays with the vendor layer |
| `SupportsGoal` | `Capabilities.SupportsGoal` | |
| `SupportsServiceTier` | `Capabilities.SupportsServiceTier` | |
| `SupportsBudget` | `Capabilities.SupportsBudget` | |
| `CompositionGrammar` | `Capabilities.CompositionGrammar` + `System.ValidateComposition` | **reshape**, see below |
| `DefaultHome` | `Capabilities.DefaultHome` + `Capabilities.HomeEnvVar` | **reshape**, see below |
| `AuthHint` | `Capabilities.AuthHint` | empty stays a legitimate declaration (Qwen/Muse have no captured remediation) |
| environment filtering (`filterCodexRuntimeEnv`, `filterQwenRuntimeEnv`, `withSpawnEnv`) | `System.ChildEnv(parent, req)` | **new surface**, see below |
| stdin transport (prompt file / stream-JSON init frame) | `System.Stdin(req) StdinPayload` | **new surface**, see below |
| `CodexLaunchMode{Exec,ManagedSession}` | `LaunchMode{Exec,DryRun,ManagedSession}` | argv per launch mode, promoted out of one system into the contract |

### Deliberate reshapes, and why

1. **`BuildCommand` split into `Argv` / `ChildEnv` / `Stdin`.** In the source
   these three were observable only by constructing an `*exec.Cmd` — the
   environment contract and the stdin bytes were side effects of a function
   whose declared output was a command. Invariant 1 makes argv, environment and
   stdin bytes the parity bar, so they are separate surfaces here and `Plan`
   carries all three as data. Environment parity is now assertable without
   starting a process.
2. **`DryRunArgs` folded into `LaunchMode`.** Its entire contract was "the same
   grammar as the real launch, minus side effects", and the source paid for the
   seam between them: `BuildArgs` reported a hardcoded `codex`/`agy` while the
   real launch executed a managed-npm or preflighted binary. One method with a
   mode argument removes the place the two can drift apart, and
   `TestDryRunMirrorsTheRealLaunchBinary` pins it.
3. **`CompositionGrammar` enum → `GrammarID` + a plugin method.** The source's
   enum meant a harness with a new composition shape was a *core* edit — the
   opposite of a plugin system. The grammar is now an opaque id the plugin
   declares (for grouping and for refusal text) and the plugin validates.
   `GrammarNone` is refused by `BuildPlan` before the plugin's validator runs,
   so a permissive validator cannot accidentally admit a composition.
4. **`DefaultHome` re-pointed.** In the source it was a documentation string
   recording that every adapter's `cmd.Dir` resolved to `Config.ChildWorkDir()`
   — it dispatched nothing. `docs/architecture.md` names "default home" as the
   harness configuration home, which is what invariant 2's
   `IdentityKey(provider, home)` is keyed by, so that is what it carries here
   (`DefaultHome` + `HomeEnvVar`). The child working directory became an
   explicit `LaunchRequest.WorkDir` / `Plan.WorkDir`.

### Deliberately NOT carried across

- `Config`'s 40+ fields. `LaunchRequest` carries only what a *system* plugin can
  act on. Board identity (`TaskID`, `RunID`, `ChangeRequestID`, `Workspace`),
  supervision (`Timeout`, `OnChildStarted`), Session-Manager envelopes and
  Codex caller-goal compatibility fields are consumer or later-layer concerns;
  putting them here would make every plugin a party to task-board's schema.
- `providerAuthHint` lookup. The source composed `AuthHint` from a second table
  at init. Here the plugin states its own hint — one fact, one place.
- The `qwen-codex` cross-runtime row. That is a *runtime* declaration (system ×
  vendor), and this layer only owns the system half. Reusing one system under
  two runtimes is a vendor-layer declaration, which is the whole point of the
  dependency direction in `docs/architecture.md`.

## AC2 — registry is the only place a binding may live

`Register` is the only constructor of a binding. It refuses: a nil system, an
id that does not normalize, a duplicate id (including a duplicate reached
through a different spelling — `"Pangolin"` then `"  PANGOLIN  "`), a
declaration with no launch modes, an undeclared launch mode, and an undeclared
effort transport. Same-binding idempotency is deliberately not offered — that
is F2 runtime-declaration policy at a different layer; a plugin registering
twice is a build-time bug.

### The guard

`pkg/agentic/singlesource_guard_test.go`. It walks every non-test `.go` file in
the module — discovered by walking from `go.mod`, not from a hardcoded
directory list, so a package added later cannot be silently unscanned — and
reports two classes:

- **`binding-table`** — a `map[SystemID]T` type expression anywhere outside
  `pkg/agentic/registry.go` (composite literal, `make`, struct field, var
  declaration, alias target — the *type expression*, not the literal), a
  composite literal of a named type whose underlying type is one, or a
  composite literal spelling a known system id as a key or an element.
- **`id-switch`** — a switch whose tag is a call to `ID()`, a switch case value
  resolving to a known system id, or an `==`/`!=` comparison against one.

String indirection resolves through package-level consts (in either declaration
order, across grouped blocks, including `+` concatenation), package-level vars
(structurally, including indexing into one), and function literals wherever
they appear — `ast.Inspect` runs over the whole declaration, so a literal inside
a closure inside a call argument is visited like any other.

Two rules are structural rather than vocabulary-based (`map[SystemID]T`, and
`switch sys.ID()`), which is what keeps the guard working for system ids nobody
has declared yet — demonstrated by the `opencode` / `some-future-harness`
mutant.

**Threat model** (also stated in the file): a static syntactic scan for
*ordinary* Go spellings, built to catch accidental reintroduction during
ordinary work. Not a boundary against an author actively evading it. Three
residual classes are declared open and each is *demonstrated* still open by
`TestSingleSourceGuardResidualGaps`, so a future change that closes one fails
that test and forces the threat model to be updated:

1. call-wrapped literals — `string([]byte("claude-code"))`, `strings.TrimSpace`,
   `fmt.Sprintf` with no verbs;
2. cross-package references — `otherpkg.CodexID`;
3. runtime assembly — keys computed and assigned in a loop.

`TestSingleSourceGuardAcceptsOrdinaryCode` is the control: a switch over
`LaunchMode`, a `map[string]int`, an `id == ""` comparison and a prose string
containing "codex exec" all pass clean, so the mutant set is not satisfied by a
scanner that reports everything.

## AC3 — a test double drives every surface with one edit

`pangolin` (`pkg/agentic/double_test.go`) exists in no non-test source —
`TestTestDoubleExistsOnlyInTests` proves that by scanning the same module
sources the guard scans, so "it needed no second edit" is a measurement rather
than a claim. It registers through the public `Register` and is driven across
every surface by `BuildPlan`.

The surface set is read off the interface by reflection
(`reflect.TypeOf((*System)(nil)).Elem()`) rather than written out by hand: a
method added to `System` and not driven from `BuildPlan` fails
`TestRegisteringOneSystemDrivesEveryDispatchSurface` on the next run. That is
the only version of "every dispatch surface" that survives the contract growing.

## AC4 — effort transport none is refusable at the contract level

`EffortTransport.CanCarry(EffortSupport) bool` decides it from the contract
types alone. `contract_test.go`'s `effortAdmission` is a caller that compiles
against nothing but `Capabilities` and `Model` — no registry, no plugin, no
vendor — and refuses `EffortTransportNone` × `EffortSupportRequired` across the
full 3×2 matrix.

`BuildPlan` is the production caller. It refuses three related shapes, each
naming the system, the model and the transport so an agent can fix the
declaration from the error text:

- a required-effort model under a system that cannot carry effort;
- an operator-supplied effort value under transport `none` (the source's
  silent-drop wrong-cost launch);
- a required-effort model with no effort value — no default is injected
  anywhere.

The *admission* refusal (vendor availability, limit state, admitted pairs)
remains a later layer, as briefed.

## Mutants — red/green per mutant, real exit codes

Log: `.temp/TASK-260821-21vywo/mutants.log`. Every run is a foreground
`go test`; the exit code is the process's own.

### Planted on disk, then removed (the AC's literal ask)

| # | Mutant | Test | Exit |
| --- | --- | --- | ---: |
| M1 | `var shadowBindings = map[agentic.SystemID]launchBuilder{}` written into `tools/agents-management/cmd/shadow_binding.go` | `TestSingleSourceGuardFindsNoSecondBinding` | **1 (RED)** |
| M2 | `switch id { case "claude-code": …; case "codex": … }` written into `tools/agents-management/cmd/id_switch.go` | `TestSingleSourceGuardFindsNoSecondBinding` | **1 (RED)** |
| M0 | both files removed — control | `TestSingleSourceGuard*` | 0 (GREEN) |

### Guard narrowed, not deleted (each resolution step proven load-bearing)

| # | Narrowing | Mutants that went undetected | Exit |
| --- | --- | --- | ---: |
| M3 | const indirection no longer resolves | const-hidden table keys, concatenated const key, const-hidden switch case | **1 (RED)** |
| M4 | package-level var indirection no longer resolves | var-hidden case, indexed-var case, var-held id comparison | **1 (RED)** |
| M5 | `map[SystemID]T` detected only inside composite literals | plain map var, `make(...)`, struct field, SystemID alias — *and* `TestSingleSourceGuardRulesFireOnRealCode` | **1 (RED)** |
| M6 | `switch sys.ID()` structural rule removed | switch naming only ids nobody has declared yet | **1 (RED)** |
| M7 | allowlist widened to every file | all 9 table mutants + the real-code narrowing proof | **1 (RED)** |

### Production gates narrowed

| # | Narrowing | Failing tests | Exit |
| --- | --- | --- | ---: |
| M8 | `Register` stops refusing a duplicate id | `TestRegisterRefusesDuplicateID`, `TestRegisterNormalizesBeforeCheckingForDuplicates` | **1 (RED)** |
| M9 | `Register` skips id normalization | the two above + all 10 `TestRegisterRefusesUnnormalizableIDs` subtests | **1 (RED)** |
| M10 | `Register` admits a system declaring no launch modes | `TestRegisterRefusesSystemWithNoLaunchModes` | **1 (RED)** |
| M11 | `CanCarry` returns true for every transport | `TestEffortTransportIsDecidableFromTheContractAlone/none/required`, `TestCanCarryRefusesAnUndeclaredTransport`, `TestBuildPlanRefusesRequiredEffortUnderTransportNone` | **1 (RED)** |
| M12 | `BuildPlan` admits a composition under `GrammarNone` | `TestBuildPlanRefusesCompositionUnderGrammarNone` | **1 (RED)** |
| M13 | `BuildPlan` admits stdin bytes reported as detached | `TestBuildPlanRefusesPluginContractViolations/stdin_bytes_while_reporting_nothing_attached` | **1 (RED)** |
| M14 | `BuildPlan` admits an empty resolved binary | `TestBuildPlanRefusesPluginContractViolations/empty_binary_with_no_error` | **1 (RED)** |
| M15 | `BuildPlan` checks mode validity but not declared support | `TestBuildPlanRefusesUndeclaredLaunchMode` | **1 (RED)** |
| M16 | `BuildPlan` admits a required-effort model with no effort value | `TestBuildPlanRefusesRequiredEffortWithNoValue` | **1 (RED)** |

All 16 mutants restored; `gofmt -l pkg tools` clean and `go test ./... -count=1`
back to exit 0 after restoration (verified in the same run).

## CLI decision

**Wired.** `plugins` now reads the `pkg/agentic` default registry and prints the
registered system ids; its private `registeredPlugins []string` is **deleted**.
Keeping it would have been a second binding for the same fact — the exact thing
this task's guard exists to forbid — so leaving it was not a neutral
scope-saving choice. The four existing `plugins` tests still hold (an empty
registry prints nothing, exits 0, and renders as `[]` not `null`); the cmd test
helper now registers through the same public API a plugin's `init` would use,
which incidentally proves the interface is implementable from outside its own
package.

## Gates — foreground, real exit codes

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod -race ./... -count=1` | 0 |
| `gofmt -l pkg tools` | 0, empty output |

## Not built, on purpose

No concrete system plugins (next story). No vendor layer (next task). No spawn
execution — `BuildPlan` constructs the observable launch surface and does not
run anything. No admission: availability, limit state and admitted-pair digests
are later layers, and `BuildPlan`'s refusals are contract-level only.

## Known limits, stated rather than implied

- The guard's id vocabulary is guard-local test data, not a production
  declaration. It has no live matches today because no concrete plugin exists;
  the structural rules carry the weight until one does. Both halves are proven
  by mutants rather than by the real tree.
- `Capabilities` is a value that the contract *requires* not to vary between
  calls. Nothing enforces that — a plugin returning different capabilities on
  successive calls would not be caught. Enforcing it means caching at
  registration, which changes what a plugin may do lazily; worth deciding when
  the first real plugin lands rather than guessing now.
- `StdinPayload` carries bytes rather than a reader. That is what makes stdin
  parity assertable, and prompt payloads are kilobytes; a system that ever needs
  to stream unbounded stdin would need this revisited.
