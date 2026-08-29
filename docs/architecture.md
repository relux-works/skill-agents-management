# Architecture

`agents-management` is a general plugin graph with a deliberately narrow core.
The core owns registration, dependency resolution, admission and observation;
every fact about a concrete harness, vendor or inference engine lives in a
plugin.

This document is the CONTRACT. What is built against it, what is deliberately
still open and who owns each residual is [shipped-state.md](shipped-state.md);
how a program depends on the module is
[consuming-the-module.md](consuming-the-module.md). Where the two disagree with
this file, they are describing reality and this file is describing the rule.

## The plugin graph

`pkg/plugin.Registry` stores `plugin.Declaration{ID, Kind, Dependencies}`.
`Kind` is an opaque normalized string declared by the package that owns the
kind. The registry never switches on it, assigns a layer number, or carries an
allowlist. Adding `pkg/inferenceengine.Kind == "inference-engine"` required no
registry change; a future `weight-artifact` or `agent-environment` kind has the
same contract.

Plugin ids are global within one registry. Every dependency names both an id
and the kind it requires. Registration is atomic and fails before a plugin is
visible when:

- an id or kind is invalid, unstable or duplicated;
- a dependency is missing (`plugin.ErrMissingDependency`);
- the named plugin exists under a different kind
  (`plugin.ErrUnsatisfiableDeclaration`); or
- the candidate graph contains a cycle (`plugin.ErrDependencyCycle`).

`RegisterAll` validates a batch, which distinguishes a genuine cycle from the
missing dependency either half would report if registered alone. `Resolve` and
`TopologicalOrder` follow declared edges dependency-first; no direction is
inferred from kind. A model vendor may depend on an agentic system, an agentic
system may depend on an inference engine, and a future declaration may point
the other way when that is the fact it owns.

The existing `agentic.Registry` and `vendorplugin.Registry` APIs are
compatibility adapters over this graph. Existing concrete plugins keep their
interfaces and registration calls unchanged:

- `agentic.Register` publishes kind `agentic-system` with no new dependencies;
  `RegisterWithDependencies` is the additive path for a system that needs an
  engine or another plugin;
- `vendorplugin.Register` publishes kind `model-vendor` and derives its
  existing vendor→system edges from `Model.Systems`;
- `Graph()` exposes the resolved declarations without asking callers to infer
  a package position.

This compatibility surface is what lets task-board continue using the v0.3.0
System/Vendor/runtime API unchanged while its native graph migration is handled
separately.

### Agentic-system plugins

An **agentic system** is the harness that runs a turn: it owns the binary,
the argv grammar, the environment contract, the stdin protocol, the launch
composition, the default home directory and the authentication hint. One
plugin per system:

| Plugin | Harness |
| --- | --- |
| `claude-code` | Claude Code CLI |
| `codex` | Codex CLI |
| `qwen-code` | Qwen Code CLI |
| `gemini-cli` | Gemini CLI |
| `antigravity` | Antigravity CLI |
| `muse` | Muse CLI |
| `pi` | Process A: `agents-infra pi --profile <name> -- <turn args>`, the short-lived wrapper that holds a shared local-runtime lease and execs the real `pi` binary as its own child |

An agentic-system plugin declares (this mirrors the adapter table the
extraction source already proved out):

- binary resolution (including managed/npm/native shim paths),
- argv construction per launch mode,
- environment filtering — what the child must and must not inherit,
- stdin transport,
- reasoning-effort **transport** (argv / stdin / none),
- goal, budget and service-tier support,
- composition grammar,
- default home and auth hint.

### Model-vendor plugins

A **vendor** owns models, authentication and quota: anthropic, openai,
alibaba, google, and `local-models` — the generic resource plane for
locally-running models (§ below, "The local-model plugin"). Existing vendor
plugins declare dependencies on the agentic systems in their model rows. That
edge keeps its shipped meaning; it is no longer the only direction the
registry can represent. A runtime remains the compatibility pair (agentic
system × vendor), so cross-runtime combinations — Qwen models under the Codex
harness — are still one vendor declaration with no core change.

A vendor plugin's interface, at minimum:

- **Models** — the model list, each with:
  - a capability SCORE (evidence-based; ranking is never policy; ties are legal
    because two models really can be equal, and the total order some callers
    need is derived from the list rather than declared per row),
  - a description of what the model is best used for,
  - provider-lineup state: lifecycle, supersession, and the vendor's display
    recommendation (display and migration evidence only — no admission path
    reads any of them),
  - the context window and the vendor billing contract, each with a stated
    meaning for its empty value: no window recorded is not a window of zero, and
    no contract registered is not free use,
  - its reasoning-effort vocabulary and recommended effort (effort is a
    required per-model axis; no defaults are injected anywhere),
- **Availability** — limit state (if the vendor rate-limits), and a health
  check that answers "can requests actually be made right now",
- **Spawn** — launching a model under one of its supported agentic systems
  with the full parameter surface: model, effort, environment, stdin,
  goal/budget/tier, composition.

### Inference-engine plugins

`pkg/inferenceengine` owns the first post-v0.3.0 kind. An inference engine is
Process B: the executable that serves a model to an agent harness. It is a
plugin identity and plan contributor, not process-lifecycle authority.
`inferenceengine.NewPlanNode` converts an engine declaration and its
`agentic.ProcessPlan` into a typed launch node; the consumer still starts,
supervises, stops and attests the process.

Both `pi` and `local-models` can depend on the same engine node while retaining
the existing vendor→system edge: engine → system → vendor in dependency-first
order, with an optional direct vendor→engine edge. No cycle or third registry
layer is needed.

### Runtimes

A **runtime** is a declared (agentic system × vendor) pair with a stable ID.
Existing IDs (`claude`, `codex`, `qwen`, `gemini`, `agy`, `muse`) remain valid
forever — they feed admitted-pair digests, limit-state filenames and free-text
records downstream, and renaming any of them orphans state silently. New
runtimes are declarations, not code.

A declaration is a naming fact, not a dependency check: neither the system nor
the vendor plugin has to be compiled in for one to exist, and the six frozen
ids are seeded into every binary before any plugin registers. Resolution is
where the pair is materialized, and it names precisely what is missing — the
runtime was never declared, the system plugin is absent, the vendor plugin is
absent, or the vendor was **looked for and never established**.

That last case is `muse`, whose broker the extraction source records as UNKNOWN
with a checked-and-empty evidence list. It is carried as an unresolved vendor
with the search that failed to establish one, which makes the declaration
complete and the runtime unlaunchable through the vendor layer. Nothing guesses
a vendor for it: the binding keys limit state, and a plausible guess there is a
fabrication with consequences. This does not weaken the vendor interface for
anyone else — an unresolved vendor is not a degraded vendor with empty models,
it is the absence of one, and every other runtime resolves to a full plugin.

Declaration collisions follow the extraction source's F2 policy: a declaration
matching an existing binding is legal and idempotent, a conflicting one is
refused and the first declaration stands. For an established-vendor runtime,
the binding remains `(ID, System, Vendor)` and broker-provenance wording is not
authority. For a system-only runtime, `Models` replaces the missing vendor
plugin as launch authority, so idempotency additionally requires semantic
equality of every model field, including effort, rank/evidence, lifecycle,
context, pricing, provenance and system membership. Model row order is ignored:
rows are keyed by ID and ranked explicitly, while declaration order is only a
presentation tie-break. Comparison canonicalizes copies and never rewrites the
stored first declaration. Ordering within a row's own slices remains part of
the published value.

## `BuildLaunch` — the module's unified launch dispatch API

`vendorplugin.BuildLaunch(ctx context.Context, r *Registry, req SpawnRequest,
mode agentic.LaunchMode) (agentic.Plan, error)` is the module's single dispatch
API for launch bindings, for every `RuntimeID` a registry declares — resolve,
select the model, resolve effort, then either `Vendor.Spawn` plus a fidelity
check for an established vendor or a lossless declaration-owned projection for
an explicit system-only runtime. The latter is generic: it branches on the
validated declaration shape (`VendorUnresolved` plus non-empty `Models`), not
on `muse` or any other runtime ID, and therefore invents no broker. Both paths set
`LaunchRequest.Runtime` uniformly, then the optional Preflight step below,
then `agentic.BuildPlan`. The intended consumer contract is that a caller does
not hand-build a `LaunchRequest` and call `BuildPlan` directly for a
registry-declared runtime. End-to-end production reachability is pending the
coordinated `skill-project-management` candidate (`TASK-260828-3hultd`); this
repository alone cannot prove that consumer call site.

`Registry.ResolveRuntime` deliberately remains the strict fully-materialized
pair API: it still returns `ErrRuntimeVendorUnresolved` for system-only
declarations and never hands callers a `Runtime` with a nil vendor. The private
launch binding inside `BuildLaunch` is the only broader materialization. It
admits a system-only declaration only after `DeclareRuntime` has validated its
declaration-owned model rows and only when the declared system plugin is
registered; an unresolved declaration with no rows keeps the strict refusal.

`SpawnRequest.Run` carries `agentic.RunContext` without flattening it into
environment strings. `PassthroughLaunch` and contributing vendors preserve it,
and the fidelity check refuses a vendor that changes or drops any of `RunID`,
`TaskID`, `BoardDir`, or `ContextID`; the resolved system remains the one owner
that exports those values through `agentic.WithRunContext`.

## Typed multi-node launch plans

`agentic.BuildPlan` remains source- and behavior-compatible: it returns the
same primary `Plan{System, Mode, Binary, Argv, Env, Stdin, WorkDir, Home}` and
leaves `Plan.Nodes` empty. `agentic.BuildMultiNodePlan` adds a validated node
graph while preserving those legacy fields verbatim.

Each `PlanNode` carries a node id, the contributing `plugin.Ref`, declared
node dependencies and a `ProcessPlan`. The builder refuses duplicate nodes,
missing dependencies, cycles, invalid process values and stdin contradictions,
then stores nodes dependency-first. The stable primary node is `agent`; engine
and sidecar nodes are ordinary declared plugin contributions. This module still
returns a value only — ordering is evidence for a consumer, not an executor or
supervisor hidden inside the plan.

## `Preflightable` — the second generic extension point

`agentic.Preflightable` (`Preflight(ctx context.Context, req LaunchRequest)
(PreflightEvidence, error)`) is an OPTIONAL interface a `System` plugin may
implement: a fail-fast, advisory readiness check `BuildLaunch` runs once per
launch, after resolution and before `BuildPlan`, skipped entirely in
`LaunchModeDryRun`. `BuildLaunch` type-asserts the resolved runtime's `System`
against it — a plugin that does not implement it launches exactly as it did
before this interface existed, with no per-system-identifier branch anywhere
in the dispatcher. A non-nil error refuses the launch before `BuildPlan` is
ever called. `pi` (below) is this module's one implementation today.

## Invariants carried from the extraction source

These were established empirically in `skill-project-management` and are
contracts here, not suggestions:

1. **Observable launch surface is the parity bar.** A launch is defined by
   argv, environment, stdin bytes and side effects. Any refactor must prove
   parity on that surface; argv-string equality is not parity.
2. **Limit state fails open.** An absent state file reads as "provider
   healthy" with no error anywhere. Therefore the on-disk identity —
   `IdentityKey(provider, home)` feeding the state filename — must never move
   without a demonstrated migration on real state files.
3. **Admitted-pair digests are frozen.** They are canonical serializations
   hashed over the admitted set; downstream snapshots pin them. A silent
   digest change is a compatibility break.
4. **Effort is per-model and required.** Vocabularies live with the model
   row. A refusal names the model, the runtime, the accepted vocabulary and
   the vendor's recommendation, so an agent hitting one mid-run can pick a
   valid effort from the error text alone. It does NOT name a dotted config
   key: this module has no config file, and the key that has to change lives
   in whichever consumer supplied the effort. Naming it is the consumer's
   half of the same error.
5. **Single source per fact.** One general plugin graph, one runtime
   compatibility registry, one normalization for identifiers. The extraction
   source paid repeatedly for shadow tables and duplicate charsets; a plugin
   declaration is the only place its kind and dependency edges may live, and
   guards should make a second binding fail a test.

## The local-model plugin: module-side M1 candidate, end-to-end M1 pending

**Module-side M1 candidate (this repository):**
`pkg/vendorplugin/vendors/local-models` is
a real vendor plugin — unlike every other vendor plugin, it does NOT
self-register in `init()`, because its catalog depends on a machine-local
`~/.agents/.configs/local-models.toml` that may not exist, and
`Registry.Register`'s unconditional `ErrNoModels` refusal would otherwise
poison registry construction for every unrelated runtime the moment that file
is absent. `localmodels.Peek()` exposes the lazy, memoized-forever load result
so a caller building the shared registry (`launchRegistry`, on the consumer's
side) can decide whether to register it at all — absent and malformed are
two DISTINCT, typed outcomes (`ConfigResult{Absent: true}` vs
`ConfigResult{Err: ...}`), and a malformed file's diagnostic is carried by
`Registry.NoteUnregistered`/`ErrRuntimeConfigMalformed` so the coordinated
consumer's `ResolveRuntime` call can surface it, not only a separate status
CLI. `pkg/agentic/systems/pi` is
Process A's harness plugin (see the agentic-system table above) and implements
`Preflightable`: a context-bounded, fail-closed admit/refuse check against
`pkg/localruntime`'s machine-local `StatusReader` — `("absent", "determined")`
is the sole non-attested admit; every other unattested/indeterminate broker
read refuses. Neither this vendor nor `pi` nor `pkg/localruntime` ever starts,
stops or signals the local model server itself (Process B); that authority
belongs entirely to `relux-agents-infra`'s own shared-runtime broker.

**End-to-end M1 is not yet claimed here.** It additionally requires the
coordinated consumer migration in `TASK-260828-3hultd`: the real
`launchRegistry -> buildLaunchPlan -> vendorplugin.BuildLaunch ->
Registry.ResolveRuntime` production chain, including abort-derived context and
zero-side-effect refusal coverage. M2 remains separately scheduled and is not
part of this candidate.
`pkg/localruntime`'s read-only status subprocess call is the one exception to
"no `os/exec` in this vendor's import graph", and a static test enforces it.

Pi's own turn-argument/stdin wire protocol for a real turn is a NAMED OPEN
ITEM, not an oversight: it depends on a pinned `earendil-works/pi` binary/docs
fixture nothing in this repository supplies yet, so `pkg/agentic/systems/pi`
builds only the pinned argv prefix (`["pi", "--profile", <profile>, "--"]`,
resolved against `agents-infra` on PATH) and attaches the assignment on stdin
using this module's existing precedented fallback, rather than inventing an
unpinned grammar.

**Module-side M2 status-consumer candidate:** relux-agents-infra PR #10
publishes the persisted `restart_not_before`, `quarantined_until`, restart and
readiness facts. `pkg/localruntime` consumes that additive shape with explicit
presence/type checks and refuses partial lifecycle cohorts, while preserving
the complete legacy and pre-deadline fixtures. `local-models` maps a
future quarantine or restart-backoff deadline to validator-safe `Limited` via
the real `CheckAvailability` entry point. It never derives a limit from
`restart_count` or `half_open`; a legacy response missing
`restart_not_before` is `Unknown`, while an explicit `null` or elapsed deadline
continues through the live broker mapping above. Log rotation and the
coordinated `skill-project-management` consumer remain separately owned work.

## Boundaries with task-board

| Stays in task-board | Moves here |
| --- | --- |
| roles, role archetypes | runtime registry, adapter table |
| tasks, stories, epics, bugs | model registries, ranking, effort vocabularies |
| Change Requests, review routing | limit detection, classification, suppression, backoff |
| worktree isolation, integration | health/availability checks |
| goals, handoff, progress records | launch: argv/env/stdin/composition, spawn parameters |
| exec mechanics, preflights that start processes, fault injection | the launch PLAN and the availability verdict |

`task-board` calls this tool for spawn decisions and launches; it never
reaches into plugin internals. This tool never reads a board.

The last row is the boundary that is easiest to get wrong, because both halves
are called "launch". This module ends at a VALUE — a binary, an argv, an
environment, stdin bytes — and a consumer turns it into a process. That is why
the Antigravity binary probe and Claude goal preparation stay on the consumer's
side: both START PROCESSES, and `BuildPlan` calls every plugin method to build
a DRY RUN, so a probe behind any of them would make a dry run execute the
harness. Their RESULTS cross the boundary instead — `agy.NewWithRuntime` takes
the preflighted executable at construction. `agy/runtime.go` and
`claude/goal.go` state the whole of what stays behind and where it has to land
if it ever moves.

## Extraction plan (current scope)

Move the already-existing vendors and agentic systems out of
`skill-project-management` behind the plugin seams, proving at each step:

1. parity of the observable launch surface per (system, mode) — **done**, one
   golden set per system, captured by the source's own harness and only
   compared here;
2. byte-stable admitted-pair digests over real configs — **done**, pinned
   against values the SOURCE BINARY produced over the source repository's own
   config;
3. unchanged on-disk limit-state identity, demonstrated on real state files —
   **done**, against the operator's live pre-extraction state, a suppression
   written by a program compiled against the source module, and the source's
   own projection of a file this code wrote;
4. task-board consuming the tool with its own spawn surface observably
   unchanged — **done**, on the consumer's trunk rather than this one, since
   the work is consumer-side: `STORY-260823-1sxcmg`, integrated at `b34aa20`.
   All three swaps and the CI arrangement landed together; the credential
   half was retired the same day when the owner made this repository public,
   and the consumer's guard now enforces that no credential plumbing returns.

Everything beyond that — new vendors, new agentic systems (opencode and
others), the local-model resource plane — is deliberately after.

[shipped-state.md](shipped-state.md) carries the per-story detail and the
outcomes that are true but easy to lose.
