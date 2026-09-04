# skill-agents-management

A skill plus one CLI — **`agents-management`** — for spawning and managing
heterogeneous agentic systems and the model vendors behind them.

Start at [SKILL.md](SKILL.md) if you are an agent wiring this in;
[docs/architecture.md](docs/architecture.md) for the plugin contract,
[docs/consuming-the-module.md](docs/consuming-the-module.md) for depending on
the module, and [docs/shipped-state.md](docs/shipped-state.md) for what has
actually landed and what is deliberately still open. This file is the long
description of the parts.

## Why this repository exists

`skill-project-management`'s `task-board` grew a complete spawn plane: launch
adapters per agentic system, a frozen runtime registry, per-model reasoning
effort vocabularies, capability ranking, provider limit detection and
suppression, health classification. None of that is task management — it is
agent management, and it kept forcing board releases for changes that had
nothing to do with boards.

This repository is that spawn plane, extracted, behind a plugin architecture.
`task-board` becomes a consumer: it keeps roles, tasks, Change Requests,
worktree isolation and review routing, and calls `agents-management` for
everything about *which agent runs, on which model, and whether it may launch
right now*.

## What the tool does

- **Declares runtimes** — an agentic system (the harness that runs a turn:
  claude-code, codex, qwen-code, gemini-cli, antigravity, muse, pi) combined with a
  model vendor (who owns the models, the authentication and the quota:
  anthropic, openai, alibaba, google, local-models).
- **Ranks and describes models** — each vendor plugin publishes its models,
  their capability ranking, their reasoning-effort vocabularies, and guidance
  on what each model is best used for.
- **Answers availability** — is the vendor reachable, are we rate-limited, is
  a launch admissible right now.
- **Spawns** — with the full parameter surface the board's spawn has today:
  model, reasoning effort, environment, stdin, goal/budget/service-tier,
  launch composition.

## What the tool deliberately does not do

Roles, task boards, task tracking wrappers, Change Requests, review routing
and worktree isolation stay in `skill-project-management`. The boundary is:
this tool knows how to run agents; the board knows what they should work on.

## Architecture

One general plugin graph: every plugin declares an opaque kind and dependencies
on other plugin ids. The registry validates missing dependencies, kind
mismatches and cycles atomically; it never assigns layer numbers or assumes an
edge direction. The existing `pkg/agentic` and `pkg/vendorplugin` registries are
source-compatible adapters, so their shipped vendor→system semantics and launch
surfaces remain unchanged. See [docs/architecture.md](docs/architecture.md).

### General graph: `pkg/plugin`

- `Declaration{ID, Kind, Dependencies}` is the registry contract.
- `RegisterAll` admits a transaction or none of it; `Resolve` and
  `TopologicalOrder` materialize declared edges dependency-first.
- `pkg/inferenceengine` declares the first new kind without a registry edit.
- `agentic.BuildMultiNodePlan` adds typed engine and sidecar process nodes while
  preserving the legacy primary `Plan` fields.

### Agentic systems: `pkg/agentic`

The agentic-system plugin contract and its registry.

- `System` is the plugin interface: identity, a static `Capabilities`
  declaration (launch modes, effort transport, goal/budget/service-tier
  support, composition grammar, home, auth hint), and five dispatch surfaces —
  `ResolveBinary`, `Argv`, `ChildEnv`, `Stdin`, `ValidateComposition`.
- `Registry` is the only place a system binding may live. `Register` is the
  only way one comes to exist, and it refuses a duplicate id, an id that does
  not normalize, an id that normalizes to a spelling other than itself, an id
  that is not the same across two consecutive reads, and a declaration that
  could never launch. The middle two are what keep `sys.ID()` and the registry
  key the same string at registration: a plugin answering a second spelling of
  its own id — whether by normalizing to it or by changing its answer — is a
  shadow binding one layer up from the map. Stability AFTER registration stays
  the plugin's contract obligation; `System.ID` says which half is enforced and
  a test demonstrates the half that is not.
- `BuildPlan` is the single dispatch site. It drives every surface through one
  registry lookup and returns a `Plan` — binary, argv, environment, stdin bytes
  — which is the parity surface a refactor has to reproduce. There is no
  switch over system ids anywhere, and
  `pkg/agentic/singlesource_guard_test.go` fails the build if one appears: a
  go/ast scan of the whole module for a second binding table or an id switch,
  with its threat model and its declared-open residuals documented in the file.
  Its rules are held to 58 mutants in `singlesource_guard_mutants_test.go`,
  split into the set its author wrote, the set two reviews wrote against it,
  and the set the vendor layer added when the guard's key-type list grew.
  Four of the first review's walked through, and the rules they forced are
  marked in the file; the second review found the guard catching `switch id`
  over an id nobody has declared yet while admitting `if id == "opencode"`, and
  four mutants now hold both rules to the same question. Seven further cases
  demonstrate the residual classes staying OPEN, so the threat model's list and
  the test can be read against each other. Its SCAN SCOPE is the code this
  module's Go build compiles and nothing else — dot-directories,
  underscore-directories, `testdata`, `vendor`, `node_modules` and any subtree
  carrying its own `go.mod` are excluded, mirroring `go/build`'s own rules —
  because a denylist of directory names let a bootstrapped checkout's
  machine-local `.agents/` redden `go test ./...` on main while every worktree
  stayed green. `pkg/agentic/singlesource_scanscope_test.go` plants one
  violating source in every excluded location AND in ordinary packages, and
  proves that source violating before trusting any silence, so the scope can
  neither miss the machine-local case nor quietly widen into ignoring real
  code.

All seven concrete system plugins ship: `pkg/agentic/systems/{codex,claude,qwen,gemini,muse,agy,pi}`.
Every one of them is proven against the launch-surface goldens its system has,
through the real `Registry` and `BuildPlan`. See below.

The contract is also proven by one test double registered through the public
API, so the interface stays exercisable without any plugin compiled in.

### The codex plugin: `pkg/agentic/systems/codex`

The Codex CLI, ported from `skill-project-management`'s spawn adapter and
proven against all four codex launch-surface goldens through the real
`Registry` and `BuildPlan`.

- **Three binary-resolution paths**, in order: the managed npm package named by
  `CODEX_MANAGED_PACKAGE_ROOT`, the npm shim on `PATH` unwrapped to the native
  binary beside it, and the `PATH` entry itself. Each step that could resolve
  is confirmed with a stat before it is returned, so a layout that is named but
  not installed falls through instead of producing a path to nothing. All three
  are covered by a golden and again by hermetic stub layouts that attack the
  ORDER between them.
- **One argv construction site.** `args.go`'s `Args` is the only place codex CLI
  flags are spelled, for both the `codex exec` grammar (shared verbatim by the
  dry-run mirror) and the managed-session provider-args fragment.
  `argvguard_test.go` scans every non-test Go file in the module and fails if a
  second site appears; it narrows itself onto the real `Args` to prove it can
  fire, holds nine mutant spellings, and demonstrates its three declared-open
  residuals staying open.
- **Effort transport is argv** — a `-c model_reasoning_effort="..."` override —
  and the vocabulary stays with the vendor layer. The service-tier override
  travels the same way.
- **Environment filtering by EXACT KEY**, never by prefix, including two
  pointers whose VALUES name the credential variables to block. The parity
  package's two permanent negatives — a whole-environment wipe and a
  family-prefix strip — are re-run against this plugin rather than against a
  probe, each with the narrowing that shows which seeded key catches it.
- **Managed-session mode has no golden** (the source's capture harness could not
  reach that surface), so it is proven the source's way instead: against a
  frozen copy of the pre-refactor construction across every combination of
  profile, effort and tier.

Three behaviours are carried over from the source deliberately and pinned by
tests that assert the CURRENT behaviour rather than a better one: the two open
child-environment leaks the source tracks as `BUG-260819-3qn52o`, a
service-tier export that lets an inherited value pass through when the launch
configured none, and an unrecognized tier that is dropped rather than refused.
Each pin names why changing it inside a parity port would be wrong.

### The claude plugin: `pkg/agentic/systems/claude`

The Claude Code CLI, ported from the same spawn adapter and proven against both
claude launch-surface goldens through the real `Registry` and `BuildPlan`. Its
plugin id is `claude-code` — the Layer-1 name — while the goldens record the
frozen RUNTIME id `claude`; `parity_test.go` maps between them in one place.

- **Two launch surfaces from one grammar.** Prompt mode streams the assignment
  on stdin. Goal mode appends the assignment as SYSTEM context
  (`--append-system-prompt-file`), spends the child's one user turn on a
  `/goal <predicate>` directive, and therefore attaches NO stdin at all — which
  is what `claude/goal-mode`'s `stdin_kind: none` records.
- **Goal-mode launch PREPARATION is explicitly out of the plan surface.** The
  source runs a provider preflight before construction: a version gate at Claude
  Code 2.1.139, a `/goal` capability probe classified into
  unavailable/untrusted/hooks-disabled, and a session-state decision. Steps two
  and three START PROCESSES, and `BuildPlan` calls every plugin method to build a
  DRY RUN — so preparation behind any of them would make a dry run execute the
  harness twice. `goal.go` states the whole of what the source does and where it
  has to land instead (the launch/session plane, when that is ported); this task
  invents no home for it. What IS carried across is the one refusal that belongs
  to the argv: a goal carrying no provider condition is rejected rather than
  shipped as a bare `/goal` binding the child to nothing.
- **The environment contract is ONE exact key.** Claude strips `CLAUDECODE` and
  nothing else. It does NOT strip the codex family — that is a codex child's
  filter, and the qwen filter is the one that composes both — so a claude child
  inherits `CODEX_*`, the app-server and session-manager URLs, and the
  credentials their pointers name. That is the source's `BUG-260819-3qn52o` seen
  from the other side; it is pinned by a test rather than left implicit. PATH is
  not sanitized either, which no golden can see, so it has its own test.
- **Binary resolution is plain `PATH`.** No managed package, no shim to unwrap,
  and none of codex's resolution machinery imported.
- **The budget ceiling is claude's alone.** `--max-budget-usd` is the one adapter
  capability no other agent in the source's table declares, and neither capture
  configured a budget — so it has no golden and is proven against the source's
  construction instead, including the `%.2f` rendering and the `> 0` guard that
  silently drops a zero ceiling.
- **One argv construction site**, guarded the same way codex's is and by the same
  scanner.

### The qwen plugin: `pkg/agentic/systems/qwen`

The Qwen Code CLI, proven against both qwen launch-surface goldens. Its plugin
id is `qwen-code` while the goldens record the frozen RUNTIME id `qwen`;
`parity_test.go` maps between them in one place.

- **The environment filter is what this plugin exists to get right.** It is the
  codex family PLUS `CLAUDECODE`, and that last key is a fix the source paid for
  (its `TASK-260817-2eo4ok`): before it, a qwen child launched from inside a
  Claude Code session inherited the nesting marker and refused to start. The
  strip is narrowed one key at a time against the golden, `CLAUDECODE` included,
  so the twelve entries are told apart from entries that were never there.
- **The codex half is single-sourced**, in `internal/runtimeenv`, shared with the
  codex plugin — because the source shares it too, and says so on
  `filterQwenRuntimeEnv`. A copy of the eleven keys per plugin is the drift that
  comment closed, and it would be invisible: a child that inherits one key too
  many still launches.
- **Effort travels on STDIN**, as a field of a two-frame stream-json control
  protocol, not in argv. An argv comparison alone cannot see it going missing,
  so the goldens compare the frames byte for byte and a mutant that empties the
  field is required to fail in `StdinData`. The encoding choices — maps rather
  than structs, so key order matches; `SetEscapeHTML(false)`, which no fixture's
  prompt exercises — are pinned by their own tests.
- **Three source leaks stay OPEN and named**: the two board credentials
  `BUG-260819-3qn52o` records, plus `QWEN_CODE_SESSION_ID`, which nothing here
  strips — so a qwen child spawned from inside a qwen session inherits its
  parent's session id. All three are pinned by a test asserting the CURRENT
  behaviour, so closing one has to be a deliberate edit to the comment too.

### The gemini plugin: `pkg/agentic/systems/gemini`

The Gemini CLI, proven against both gemini goldens. Plugin id `gemini-cli`,
runtime id `gemini`. It is the simplest of the six and the simplicity is the
contract: plain `PATH` resolution, no effort transport, no composition grammar,
no goal, budget or service tier.

- **The prompt reaches the child ONCE.** gemini appends what it reads on stdin
  to the `-p` value, so a real launch passes an EMPTY `-p` and streams the
  assignment; a dry run has no file and substitutes the source's `<prompt>`
  placeholder. That single argument is the only difference between the two
  modes, and the mutant that puts the text in argv as well is the first one in
  the file.
- **The environment filter is EMPTY**, and `env.go` says so rather than
  expressing it by omitting the file. A gemini child inherits `CLAUDECODE`, the
  whole `CODEX_*` family and every credential in the parent — the extreme case
  of `BUG-260819-3qn52o`, whose shape is that each filter names only the
  runtimes it knew about. The bound that makes the emptiness load-bearing is a
  negative: a plugin carrying the codex-family filter must FAIL the golden.

### The muse plugin: `pkg/agentic/systems/muse`

The Muse CLI, proven against both muse goldens. It is the one system whose
plugin id and runtime id are the same spelling.

- **The assignment is a PATH in argv** (`--prompt-file`), so muse attaches no
  stdin at all — `muse/exec` records `stdin_kind: none`, and the mutant that
  streams the assignment as well must fail in `StdinKind`. Because the path is
  in argv, the dry run has something to substitute: the source's
  `<prompt-file>`.
- **An unreadable assignment is NOT this plugin's refusal.** Every other system
  reads the file; muse only names it. A plugin that stat'ed it would be
  performing work in a method `BuildPlan` calls for a dry run.
- **Nothing about muse's VENDOR reaches this plugin.** The extraction source
  records muse's broker as unknown; that is Layer-2's business, settled there,
  and a test holds the declaration to carrying no opinion about it.
- The environment filter is EMPTY, with the same bound gemini's has.
- **The effort transport is ARGV** (`--reasoning-effort`), and it is the one
  capability row that is no longer the extraction source's. The source declared
  `EffortTransportNone` because every muse model was effort-none;
  `muse-spark-1.3-contributor` is not, so declaring none would refuse the
  runtime's own current model. The word is transported VERBATIM: the model's
  vocabulary is `high`/`xhigh`/`max` and installed `muse 1.0.2` documents
  `none|minimal|low|medium|high|xhigh|ultra`, so `max` is a word this plugin
  passes through and that CLI refuses — a harness-side refusal until muse ships
  it, and deliberately not pre-empted here, because a plugin enumerating the
  CLI's set would keep refusing afterwards.
- **No golden covers the effort run.** muse carried no effort when the source's
  harness captured, and a hand-written fixture claiming that provenance would be
  a forged capture. The parity evidence is instead the SHIPPED `muse/exec` and
  `muse/dry-run` goldens extended in memory by exactly the two-argument
  insertion, bounded both ways: the effort plan must match the extended golden
  and must NOT match the unextended one, and a mutant that emits the same pair
  at the end of argv must fail.

### The agy plugin: `pkg/agentic/systems/agy`

The Antigravity CLI, proven against both agy goldens. Plugin id `antigravity`,
runtime id `agy`.

- **The binary comes from a PREFLIGHT, and agy has no `PATH` fallback at all.**
  The source's probe runs `agy --version` and `agy --help` and validates a
  minimum version and seven required headless flags — it START PROCESSES, so it
  cannot live behind any method here, exactly as claude's goal preparation
  cannot. `runtime.go` carries the whole boundary. The result reaches the plugin
  at CONSTRUCTION (`agy.NewWithRuntime`), which makes this the one ported plugin
  that holds state; the alternative was an agy-shaped field on the core request
  that five plugins would ignore.
- **The source's two resolution functions become one resolution plus one
  mode-aware refusal.** `ResolveBinary` takes no mode — that is the core's
  anti-drift guarantee — so it answers the preflighted executable when there is
  one and the `agy` display placeholder when there is not, and `Argv` REFUSES an
  exec launch in the placeholder state. `agy/dry-run`'s golden is that
  placeholder branch and `agy/exec`'s is the other, so both are fixture-backed.
  The dry run's no-side-effect promise is measured, not asserted: a plan builds
  over an assignment file that does not exist and an environment with no `PATH`.
- **Effort is encoded in the MODEL ID** (`gemini-3.6-flash-high`) and the effort
  flag is REJECTED. The plugin never parses the suffix — that vocabulary is the
  vendor layer's — and declares `EffortTransportNone`, which makes the refusal
  of a required-effort model decidable from the contract alone. Neither golden
  configures an effort, so both refusals have their own tests.
- **The only ARG_MAX budget in the module**, because agy is the only system that
  puts the whole assignment in argv. The gate is proven by NARROWING as well as
  by an oversize prompt: a prompt is sized into the window where the arguments
  alone fit and the arguments plus a long executable path do not, so a port
  measuring only the arguments is caught.
- The environment filter is EMPTY, with the same bound gemini's has.

### Shared plugin internals

Three packages exist because a second plugin needed the same rule, which is the
only reason any of them should:

- `internal/runtimeenv` — the codex-family parent-runtime strip, its exact-key
  filter, its credential-pointer resolution and its `PATH` sanitizer, shared by
  the codex and qwen plugins. The source shares it too.
- `internal/mcpjson` — the `--mcp-config <json>` composition grammar, shared by
  claude, qwen and agy. The source validates all three with one function, and
  the claude port said this validator would have to be shared before there was
  anything to share it with. Its refusal messages name the GRAMMAR rather than a
  system, which is the one deliberate divergence from the source's text: the
  source says "Claude" even when refusing an agy composition.
- `internal/paritycase` — the machine state a golden was captured under,
  reproduced on the machine running the test, used by the four plugins ported
  last. The codex and claude plugins keep their own copies of these helpers,
  deliberately: rewriting the harness under an accepted parity proof would put
  the proof and the change in one commit. That residual is stated in the package
  comment rather than hidden.

### One argv guard, shared

`internal/argvguard` is the scanner all six plugins' guards drive; `internal/gosources`
is the single answer to "which files does this module's build compile", used by
those guards and by the single-source binding guard. Each plugin supplies only
what is genuinely its own — the signature literals and the allowlist — so the
threshold (ONE co-occurring literal), the resolution depth and the declared-open
residuals are one fact rather than one per plugin.

Allowlist keys are FILE-SCOPED. Every plugin names its construction site `Args`,
so a bare-name allowlist would have exempted every `Args` in the module from
every guard — a weakening that arrives silently the moment a second plugin lands.

Each signature deliberately EXCLUDES the literals a sibling plugin also spells,
because a guard that fires on legitimate neighbouring code is a guard somebody
deletes: `claude` and `agy` both drop `--dangerously-skip-permissions` and
`--output-format`, `muse` drops `--model` and `--json`, `gemini` drops its
single-letter flags. `agy` also drops `--add-dir` — and that one was NOT
predicted. It was in the signature until the cross-plugin bound reported the
codex plugin's own `Args`, which is that test doing exactly what it exists for.
Every plugin carries a cross-plugin false-positive check against all five of its
siblings' real sources, and the residual each exclusion leaves is named on the
signature and demonstrated staying open.

### Model vendors: `pkg/vendorplugin`

The vendor plugin contract, its registry, and the runtime declarations.

- `Vendor` is the plugin interface: identity, `Models()`, `Availability()` and
  `Spawn()`. A model row carries its capability score WITH the evidence behind
  it (a score with no observation is refused at registration — ranking is never
  policy), a usage description that cannot be silently empty, its lineup state,
  its supersession, the vendor's display recommendation, its context window, its
  optional positive cache budget, its billing contract, its reasoning-effort
  vocabulary and the vendor's recommended word, and the agentic systems that
  can drive it. Every field with a legal
  empty value states what that empty MEANS, and each is refused where it cannot
  hold together: a successor no model answers to, a non-legacy row that has
  already been replaced, two display picks for one harness, a negative context
  window, a "promotion" at or above list price. Effort TRANSPORT is not repeated here: it
  belongs to the system plugin, and a launch needs both halves from their own
  owners.
- **The shipped vendor→system dependency is enforced at registration.** A vendor naming an
  agentic system that is not registered is refused, with both ids in the error.
  A registry built without an agentic registry refuses every vendor rather than
  admitting one whose declared systems nobody checked.
- **Inference engines are graph identities, not provenance strings.**
  `inferenceengine.NewConfigured(id)` creates a generic identity-only plugin;
  `inferenceengine/engines/mlx.New()` supplies the concrete configured `mlx`
  plugin without adding an id switch to the registry;
  `RuntimeDeclaration.Engine` and `Model.Engine` carry the same typed ref,
  vendor registration publishes the model-to-engine edge, and `BuildLaunch`
  resolves it before returning a plan. `Plan.Provenance` records requested and
  resolved engine refs together with system/broker/runtime/profile/model/
  publisher/family. `Plan.ConsumerProvenance()` publishes those axes as
  `agents-management.launch-provenance` schema v1, spelling the
  engine pair `configured_engine` and `resolved_engine`. Its mandatory
  `engine_binding` discriminator separates genuine `none` legacy launches from
  `required` engine-bound records. A `none` record must carry only its system
  identity; broker, publisher, family, runtime, profile, model, or either
  engine ref makes it inconsistent. Missing, downgraded, malformed,
  wrong-kind, or mismatched graph evidence therefore refuses after persistence.
  Persisted consumers call `Registry.ValidateLaunchProvenance`, which derives
  the configured ref from trusted runtime/model declarations, resolves it
  through that registry's plugin graph, and then calls
  `LaunchProvenanceV1.ValidateAgainst`; equality between the two persisted refs
  alone is never authority.
  No concrete engine, runtime, profile, publisher, family or model name is a
  core dispatch case, and declarations with no engine retain legacy behavior.
  Genericity is bounded by production-entry identity-renaming tests plus an
  exact shipped-literal placement boundary; it does not claim whole-program Go
  semantic detection.
- Engine-bound production launches obtain evidence only from adapters supplied
  at trusted registry assembly with
  `NewRegistryWithEngineObservationAdapters`. The constructor reads each
  declaration once, copies its identity/version/kind, and installs an immutable
  engine-ref map. `BuildLaunch` first resolves the vendor-owned profile as a
  pure value, then makes one cooperative 10-second observation, and validates
  exact version, query identity, freshness and the closed inference-engine
  readings before preflight or plan construction. `NewRegistry` installs no
  positive adapter, so non-dry-run engine launches fail closed while legacy
  zero-engine and dry-run plans remain observation-free. Process B lifecycle
  stays wholly in agents-infra.
- `Availability` is a structured verdict, not a boolean: healthy,
  limited-until(time, evidence), unreachable(evidence), or unknown — with
  "checked and found nothing" distinguishable from "nobody looked" and from "the
  read failed". The zero value is unknown, and only an observed healthy verdict
  is serviceable. `AvailabilityQuery` carries a `Runtime` field so a vendor
  serving more than one `RuntimeID` (`local-models` today, `google` for
  `gemini`/`agy` already) can disambiguate which declared pair a caller means.
  The local-model resource plane described in `docs/architecture.md` is
  implemented in this branch as the module-side M1 candidate:
  `pkg/vendorplugin/vendors/local-models` is a real vendor plugin,
  `pkg/agentic/systems/pi` is Process A's harness plugin, and `pkg/localruntime`
  is the machine-local `StatusReader` contract. `local-models`, unlike every
  other vendor plugin, does NOT self-register in `init()` — its catalog depends
  on a machine-local `~/.agents/.configs/local-models.toml` that may not exist,
  so a caller building the shared registry decides whether to register it at
  all via `localmodels.Peek()`'s three-way absent/malformed/valid result (see
  `docs/architecture.md`). End-to-end M1 is not shipped by this module alone.
  A model row may declare `cache_budget_bytes` as a positive integer. The
  generic catalog exposes it as `Model.CacheBudgetBytes *int64`: `nil` means
  unrecorded, while an explicit zero or negative value is malformed. The fact
  is copied from configuration only; model/publisher/family names, context
  size, argv, availability and live runtime status never infer it, and it does
  not alter launch plans or admitted-pair digests.
  The `local-qwen` `RuntimeDeclaration` and `skill-project-management`'s
  production migration onto `vendorplugin.BuildLaunch(ctx, ...)` belong to the
  coordinated consumer task (`TASK-260828-3hultd`) and remain pending review;
  see
  [docs/shipped-state.md](docs/shipped-state.md).
- **Runtimes are declared pairs.** `RuntimeDeclaration` binds a stable id to
  one agentic system and one vendor, and the six historical ids (`claude`,
  `codex`, `qwen`, `gemini`, `agy`, `muse`) are seeded from the extraction
  source's frozen `runtimeid` table into every binary's default registry.
  `muse`'s broker is recorded UNKNOWN with a checked-and-empty evidence list,
  exactly as the source records it. `ResolveRuntime` remains strict and reports
  that unresolved broker on its own terms. `BuildLaunch` additionally supports
  the explicit system-only declaration shape: when such a declaration carries
  validated model rows, those rows own model/effort facts and launch directly
  through the declared system, with no fabricated vendor and no runtime-ID
  special case. An unresolved declaration with no rows still refuses. The F2
  collision policy applies: an established-vendor redeclaration matches on its
  binding as before; a system-only redeclaration must also carry semantically
  equal model/effort authority. Model row order is presentation-only and may
  differ, but changing any model field is a typed conflict; the first complete
  authority stands unchanged.
- `BuildLaunch` is the module's runtime/vendor compatibility dispatch API. It resolves an
  explicit launch binding, selects the model, validates the effort word against
  the model's own vocabulary (never substituting the recommendation — no
  default is injected anywhere), asks a resolved vendor to build an
  `agentic.LaunchRequest` or projects an explicit system-only declaration
  losslessly, refuses a vendor answer
  that REDIRECTS the launch (a vendor may add authentication environment; it
  may not change the harness, the model, the effort, the tracked `RunContext`,
  the goal, the budget, the tier or the composition), and hands it to
  `agentic.BuildPlan`, then attaches the resolved provenance snapshot. Spawn
  EXECUTION is not here — that is a consumer responsibility. This branch does
  not claim production reachability until the coordinated consumer candidate
  calls this API from its real `buildLaunchPlan` path.
- Pi Process-A plans are exact:
  `agents-infra pi spawn --profile <exact> --prompt <one UTF-8 argv value>
  --deadline 30m --result-schema 1`. Stdin is detached/EOF; dry-run substitutes
  `<prompt>` without reading `PromptPath`; agents-management emits no inner Pi
  flags. Consumers classify bounded schema-1 stdout, actual Process-A exit,
  intervention and cleanup only through `pi.ValidateTurnResult`. The closed
  classifier rejects duplicate/unknown fields, unknown versions/codes,
  exit/document disagreement and documents over 1 MiB, while preserving
  cancellation and cleanup precedence. `pi_turn_lifecycle_integrity_unknown`
  is a sanitized Process-A refusal (exit 1) for the case where Process A cannot
  establish the integrity of its own lifecycle evidence; the wire document
  carries the code alone, never a path, identity, raw child error or cache
  contents, and every other spelling of that fact stays result-invalid.

The single-source guard covers both layers from ONE list, keyed by the FACT
being bound. Three entries are dispatch key types (`SystemID`, `VendorID`,
`RuntimeID`), each mapped to the single file permitted to bind it — stricter
than a flat allowlist, since a `map[VendorID]T` inside the agentic registry is a
violation even though that file legitimately holds a binding map, and a mutant
plants exactly that. Four more are the vendors' binding files, one per vendor:
a model row declares which harnesses drive it, which is a binding, and spelling
a plugin id in a composite literal anywhere else fails the guard. Displacing
every home and requiring the real tables to be reported is what keeps a green
run from meaning "the rule never matched anything".

### The vendor plugins: `pkg/vendorplugin/vendors/{anthropic,openai,alibaba,google}`

All 45 rows of the extraction source's model registry, carried across. 42 land
in the four vendors; the three `muse` rows belong to no vendor, because the
source records that runtime's broker as checked-and-never-established, and the
port accounts for them explicitly rather than dropping them.

- **Ported verbatim**: model ids, the agentic systems each row declares, the
  per-model effort vocabularies and the recommended efforts. A full-set pin
  compares both directions against a fixture captured from the source's own
  sources, and six drift mutants prove the pin fires.
- **Ported verbatim, since v0.2.0**: the capability SCORE, the lifecycle, the
  supersession, the display recommendation, the context window and the vendor
  billing contract. Until v0.2.0 the score was reshaped here into a tie-free
  rank POSITION, which asserted an ordering nobody had observed wherever the
  source's scores tied. The score is now carried as it stands and the total
  order some callers need is DERIVED by `vendorplugin.Lineup` — see *Rank: a
  score, and a position derived from it* below.
- **Authored here**: the usage descriptions. The source has no
  what-is-this-model-best-for field and the contract refuses a blank one, so
  these were written for this repository — marked as such in every binding file,
  with a test that fails if the note is removed.
- **The three `muse` rows** live on the vendor-unresolved runtime declaration
  rather than in a vendor plugin, because no vendor owns them. "No plugin owns
  this model" is not evidence that the model has no lifecycle, no context window
  and no effort axis, so the unresolved declaration carries the rows and
  `Validate` holds them to exactly the standard a registered vendor's rows are
  held to. A resolved runtime declaring model rows is REFUSED — that would be
  the second table. Since `muse-spark-1.3-contributor` the effort axis is no
  longer hypothetical: that row and its `muse-spark` alias declare
  `high`/`xhigh`/`max` with `high` recommended, and `muse-spark-1.2-contributor`
  stays effort-none as a legacy row superseded by 1.3. `muse-spark` carries
  `AliasOf: muse-spark-1.3-contributor`, so it is admitted and audited under its
  own spelling and EXECUTES under the contributor id — see *Alias identity
  resolution* below.
- `alibaba` is the architecture's own argument: five rows under the `qwen-code`
  harness and one under `codex`. A cross-runtime pair is one vendor declaring
  one more system, and a test builds a real launch through it.
- `google` is one vendor over two harnesses (`gemini-cli` and `antigravity`),
  which is why a rank is comparable within a broker rather than within a
  harness.

### Alias identity resolution

A model row may declare `AliasOf`: the identity it is a short spelling of.
`muse-spark` is the only one in the module today, and it exists because the
alias is a name operators, configuration and spawn ceilings use and the
provider does not have. Measured, not assumed: a spawn of `--model muse-spark
--reasoning-effort high` put the alias into muse's argv verbatim and the
backend answered `model muse-spark does not exist or you lack access`, while
the identical launch spelled `muse-spark-1.3-contributor` ran end to end.

- `agentic.BuildPlan` substitutes the identity ONCE, after every contract
  refusal and before the first plugin surface, so binary resolution, argv, the
  child environment and stdin are all built from the identity — a substitution
  applied to argv alone would still describe a model the provider does not have.
- `Plan.ModelIdentity` keeps both spellings for every plan. The requested one is
  what an audit trail, a cost attribution and an operator asking "what did I ask
  for" need; the launched one is what argv, the provider and its logs carry.
- The alias stays a real row: ranked, displayed, admitted under its own id, and
  the effort word a caller supplies is validated against ITS vocabulary.
  Resolving at admission time instead would silently change which configured
  pairs a board admits.
- It is DECLARED, never derived. No prefix, suffix, version, score or
  description rule resolves a name anywhere in this module; a launch redirected
  by a spelling rule is a launch nobody authorized.
- Registration refuses a target the same lineup does not declare, a target that
  is itself an alias (resolution is one hop), and an alias whose effort axis or
  agentic systems do not mirror the identity. It deliberately does NOT hold the
  two rows' rank, lifecycle, context window, pricing, recommendation or
  description equal — none of those reaches argv or an admitted-pair digest.
- `checkLaunchFidelity` refuses a vendor plugin that sets or clears `AliasOf` on
  the way out of `Spawn`: that would let a plugin choose which model actually
  executes after admission passed on a different one.

### Admitted-pair digests

`ExpandV2Ceiling` turns a decided spawn-policy-v2 ceiling into its exact
`(model, effort)` pair set and hashes a canonical serialization of it. Model
MEMBERSHIP comes from the extraction source's frozen v2 compatibility snapshot
(`pkg/vendorplugin/v2snapshot.go`); the per-model effort vocabularies come from
the vendor rows. Keeping those two apart is the source's own hard-won split — a
capability rank is evidence, and the day it decides admission, a truthful
re-rank silently moves who may spawn.

The digests are pinned against values captured from the SOURCE BINARY run over
the source repository's own `task-board.config.json`, not recomputed here: a
digest computed by the port and pinned by the port proves only that the port
agrees with itself. Three vocabulary-drift mutants prove the digest moves when
the rows do, and one lineup-reversal mutant proves it does not move when the
ranks do.

The digest is **byte-stable across every presentation fact**, which v0.2.0 had
to prove rather than assert: eight mutants — every row marked legacy, every
recommendation withdrawn, every score flattened, every price doubled, every
contract dropped, every description replaced — re-expand the real ceilings and
must produce a byte-identical canonical serialization. Two more mutants move the
model id and the effort vocabulary and MUST change the digest, so the stability
claim cannot be satisfied by a serializer that covers nothing.

### Rank: a score, and a position derived from it

A `CapabilityRank` declares a SCORE. Higher is more capable, comparison is only
ever within one vendor, and **ties are legal** — they are the vendor stating
that two models are equal. Three real ties exist in the ported table: an alias
and its dated snapshot under `anthropic`, a cross-runtime mirror under
`alibaba`, and five gemini-cli/antigravity pairs under `google`, whose two
harnesses were ranked from two catalogues against one broker.

The total order some callers need — printing a picker, numbering a listing — is
DERIVED: `vendorplugin.Lineup` orders by score descending, breaks ties by
declaration order, numbers 1..n, and marks every tied row `Tied`. A position is
presentation, not evidence: swapping two equal declarations swaps their
positions without a single observation having changed, which is exactly why it
is not a field. A score with no `Basis` behind it is still refused — that rule
did not move, because it was never about the position.

Identifier normalization for all three id kinds lives once, in
`internal/ident`. Two folds that agree today and drift tomorrow is the
duplicate-charset failure the extraction source paid for, and a test holds both
layers to the same rule.

### The limit plane: `pkg/providerlimits`

The machine-scoped record of which subscription groups are believed exhausted,
the classifiers that produce that belief from real provider output, the atomic
probe claim that lets exactly one spawn test a suppressed group, and the seam
that reports all of it as a `vendorplugin.Availability`.

- **The on-disk identity is the thing that must not move.**
  `IdentityKey(provider, home) = hex(sha256(provider‖0x00‖home))[:16]` names the
  state file, and the plane fails open: a state file nobody can find reads as
  "provider healthy" with no error anywhere. So the port is checked against
  bytes this repository did not write — the operator's live pre-extraction state
  files, a suppression written here and now by a program compiled against the
  SOURCE module, and the source's own projection of a file this code wrote.
  Every fixture round-trips byte for byte, and the two live filenames are pinned
  as literals.
- **Classification is owned by the BROKER, state is keyed by the RUNTIME.** A
  429 shape is a property of the API behind an account, not of the harness that
  drives it, so `HasClassifier` keys on the vendor id; `IdentityKey`,
  `UnmappedGroup` and every persisted group key keep keying on the runtime id,
  because those are already on disk. The runtime→broker binding is resolved
  through `vendorplugin.FrozenRuntimes` — never a second table here.
- **The verdict mapping is where the corrupt-state fix lands.** An absent state
  file is Healthy (invariant 2, carried and not re-decided — a proven absence IS
  a read, which is what satisfies the contract's evidence discipline). Every
  INDETERMINATE read — unreadable, corrupt, schema-ahead, or degraded by a loss
  tombstone — is Unknown with the read failure attached, and never Healthy. A
  probe-eligible or probing group is not Healthy either: it is admitted to one
  caller by winning an atomic claim, and a claim is a write the read path may
  not take.
- **Backoff ladders are keyed by broker**, with the extraction source's
  one-release runtime-id aliases (`claude`, `codex`, `qwen` and nothing else)
  carried as-is. The alias policy is pinned against the source's own decode
  rather than against a re-typed list.
- **The provider home comes from the harness plugin.** The source kept a
  per-runtime home table inside this package; here `DefaultProviderHome`
  resolves the runtime's agentic system and reads the `HomeEnvVar` /
  `DefaultHome` that system declares. A runtime whose harness declares neither —
  gemini, agy, muse, qwen — has no home, and that is reported rather than
  guessed.

Not ported: the dev-only fault injector (launch-plane behaviour, and its
captured payload trips the codex argv guard on a field it did not construct),
and the source's home table. Nothing is wired into a live vendor: the switch
landed without wiring it, so `AvailabilityFor` has no production caller in
either repository today. The seam is proven and unconsumed, and saying so is
cheaper than discovering it.

## Status

`v0.4.3` is the exhaustive refusal-proof patch for the general-plugin-graph
release. Its checked-in matrix independently enumerates all nine raw
registration and three raw resolution error-producing paths, plus every typed
multi-node plan and vendor-registration error class. A reproducible harness
proves all 37 rows with compile-clean narrowing mutants. It also refuses
typed-nil vendor plugins instead of panicking. The release retains the seven shipped
agentic systems, five model vendors (including conditional `local-models`),
frozen runtime ids, launch-surface goldens, admitted-pair digests and on-disk
limit-state identity. The new graph and multi-node plan are additive; task-board
keeps its existing v0.3.0-facing registration and launch calls until its
separate migration task adopts the native graph.

Rollback is a consumer pin to `v0.3.0`; this patch performs no persisted data
migration. Published tags are immutable, so any compatibility repair ships as a
new signed patch tag.

**[docs/shipped-state.md](docs/shipped-state.md) is the honest ledger**: what
each story landed, the model-description divergence between the two
repositories and who owns collapsing it, the spawn-plane code deliberately kept
in `task-board` (exec ownership, the process-starting preflights, the
composition validators with no cross-repository agreement check), the open
child-environment leak pins and the qwen-codex auth-hint gap, and the exact
state of the tag/`go.work` CI arrangement — whose credential half was RETIRED
when this repository went public on 2026-08-23, and whose absence the consumer's
own guard now enforces.

## Development

Go test authoring follows the `go-testing-tools` skill.

### Build

One Go module at the repository root, module path
`github.com/relux-works/skill-agents-management`. The CLI's `main` package
lives in `tools/agents-management`; shared packages will land under `pkg/`
as the extraction moves plugin contracts across.

```
make build     # build tools/agents-management/agents-management
make test      # go test ./... -count=1
make vet       # go vet ./...
make regress   # the landing-gate regression net (internal/regress), ~1s
make install   # copy the binary to ~/.local/bin/agents-management
make clean     # remove the built binary
```

`make build` injects `VERSION`, `COMMIT` and `BUILD_DATE` through `-ldflags`;
all three are overridable on the command line, which is how the build tests
assert that the injection actually reaches the binary. `BIN=` redirects the
output path so a build can avoid touching the checkout.

Every Go invocation passes `-mod=mod` explicitly. Go silently switches to
`-mod=vendor` as soon as a `vendor/modules.txt` exists, and a stale vendor
tree then fails every build — the extraction source dropped vendoring for
that reason.

`make vet`, `make build`, the test command and `make regress` are the
repository's required local validation set. Run them from the exact candidate
commit so the evidence is bound to the tree being reviewed.

### The regression net

`make regress` runs `internal/regress`, which is not more unit tests. Each
per-package suite proves one port against its own fixtures, mutant by mutant,
and takes as long as that deserves. This one crosses the layers and stays under
a second, because it sits in front of every landing and a slow gate taxes every
future Change Request.

Four classes, one per failure this repository has already paid for, each with
the negative that makes it mean something:

| Class | Driven through | The negative |
| --- | --- | --- |
| A vendor naming an unregistered agentic system is refused, with BOTH ids | the four real vendor plugins into a registry built on an EMPTY agentic registry | the same vendors are ADMITTED once their systems are registered, so "refuses everything" cannot pass; and a message naming only the vendor fails |
| Runtime declaration and the F2 collision, both directions | `SeedFrozenRuntimes` plus `DeclareRuntime` on a fresh registry | a conflicting redeclaration must be refused AND the stored binding must be unchanged afterwards — refuse-and-write-anyway is caught; a redeclaration differing only in broker provenance must still be idempotent; system-only model/effort authority conflicts and concurrent contenders must leave exactly one complete winner |
| An availability verdict derived from a real state file | `providerlimits.Store.AvailabilityFor` over `testdata/source-written/`, written by a binary compiled against the SOURCE module | the same bytes filed one hex digit away read HEALTHY — the fail-open disaster, asserted; an elapsed window is not serviceable; an unreadable file is Unknown, never Healthy |
| A `BuildPlan` parity smoke over the six original golden-backed systems | the real `Registry` and `agentic.BuildPlan` | a wrong resolved binary and a truncated argv must each be reported in the field it was planted in; Pi's separately pinned prefix/preflight suite remains outside this historical smoke until its turn grammar is frozen |

The whole net is held to ten mutants that narrow the production gates one at a
time and require `make regress` to go red naming the right test:
`python3 .temp/TASK-260823-4f5t1m/mutants.py`.

### Current CLI surface

Five commands of its own, because this stage is about seams rather than
features (cobra contributes `help` and `completion`):

```
agents-management version                    # build version, commit and build date
agents-management plugins [--json]            # the agentic system plugins compiled in
agents-management vendors [--json]            # the vendor plugins compiled in
agents-management runtimes [--json]           # the declared (system x vendor) pairs
agents-management local-runtime status [--json]  # local-models.toml's registration state and live pair status
```

`plugins` and `vendors` read the `pkg/agentic` and `pkg/vendorplugin` default
registries — the same ones a plugin package registers into from its `init` —
and print the registered ids. **Both print an empty list and exit 0 in the
shipped binary.** That is the answer, not a stub: every plugin package exists
and none is compiled into `tools/agents-management`, which imports no plugin
package (`local-models` is the one deliberate exception the binary links
directly, for `local-runtime status` below, and it still does not self-register
— see the conditional-registration contract in `docs/architecture.md`), and
"nothing registered" is a different fact from a failure to look. `--json`
renders the empty case as `[]`, never `null`. A consumer links the packages it
needs directly — see
[docs/consuming-the-module.md](docs/consuming-the-module.md) — and gets a
populated registry in its own binary.

`runtimes` prints the six frozen declarations as `id⇥system⇥vendor`. A runtime
is a DECLARATION, so it lists what is declared rather than what can currently
launch: every id appears whether or not its plugins are compiled in, and
`muse` reads as `vendor unresolved` rather than as a blank column. The JSON
form carries the broker provenance — which source was checked, and that nothing
established a vendor — because "unresolved" is only honest when the search
behind it is visible.

`local-runtime status` reads `localmodels.Peek()`'s memoized-forever result —
the SAME loader a launch's conditional registration decision reads — and
reports whether `~/.agents/.configs/local-models.toml` was found and parsed,
distinctly for absence versus a malformed file
(`{"registered": false, "reason": "absent"}` vs
`{"registered": false, "reason": "malformed", "error": "<msg>"}`), and when
valid, each declared `(runtime, model)` pair's live broker status via
`pkg/localruntime`'s `StatusReader`. It never registers, declares, or launches
anything itself.

None of these commands keeps a list of its own. A private one would be a
second binding for the same fact, which is exactly what the single-source
guard exists to prevent.

### Observed inference-engine contract

`pkg/inferenceengine` exposes the static, no-runtime-contact schemas used to
validate engine reads. Production authorization lives at
`vendorplugin.BuildLaunch`: every non-dry-run engine launch first constructs the
vendor-owned authoritative profile as a pure value, then asks the immutable
adapter selected by the resolved engine ref before system `Preflight` or plan
materialization. Trusted assembly installs adapters only through
`NewRegistryWithEngineObservationAdapters`; `NewRegistry` installs none.
Configured IDs and implementation kinds remain separate identities, with no
concrete engine branch in generic core.

`inferenceengine.ValidateReadings` is only the closed schema validator used by
that gate. Callers may construct its untrusted `Reading` inputs, but doing so
does not install an engine source or authorize a launch. The built-in MLX
path therefore returns the typed missing-adapter refusal until agents-infra
ships the concrete observation adapter; it never invents positive runtime facts.

Every measured fact has a closed value grammar. Readiness requires resident
weights, inference-busy is boolean, lifecycle transitions carry matching
resident state, memory pressure records the ordered load/unload/busy
consultation, and artifact/memory/speculation/stress/restart/SSH facts have
their own schemas. Results preserve three typed outcomes: observed value,
positively observed absence, and not observed. Read failure, malformed input,
and unsupported input are distinct refusing causes. Profile expansion is
either local executable plus argv with observed SSH absence, or SSH forwarding
with both local facts observed absent; simultaneous and partial shapes refuse.

The module validates observations and orders the pre-launch refusal only.
Process launch, SSH, polling, signals, memory-pressure action, and restart
supervision remain owned and executed by agents-infra.

### Launch-surface parity goldens

`pkg/agentic/parity` holds the contract every agentic-system plugin port proves
itself against: one golden per `(system, case)` recording the binary, argv,
environment keys added and removed, and stdin bytes that the launch produced.

The goldens are captured by the EXTRACTION SOURCE's own harness
(`skill-project-management`, `tools/board-cli/internal/spawn/parity_capture_test.go`,
`TestCaptureLaunchSurface`) and only compared here. A golden captured by code
living next to the port proves only that the port agrees with itself.

```
.scripts/capture-parity-goldens.sh [--source /path/to/skill-project-management]
```

The script runs the source harness under a pinned synthetic parent environment,
then hands the result to this repository's masking and writes
`pkg/agentic/parity/testdata/goldens/`. It refuses a dirty source checkout. The
source checkout is otherwise read-only: the only thing the script does inside it
is run one `go test`.

That pinned environment seeds two kinds of key, and a recapture must keep both.
The keys the source's filters act on make each strip observable — the filter's
lower bound. Four **bystander** keys that no filter touches make the upper bound
observable too: without them a system that strips everything and a system that
strips exactly the right keys produce the same diff, and a port that discards
the whole parent environment passes. `PARITY_BYSTANDER` covers the wipe; the
three `*_LIKE_BUT_NOT` near-misses cover a port that filters by prefix where the
source filters by exact key.

`pkg/agentic/parity/testdata/goldens/README.md` is the boundary document — what
is captured, what the source could NOT capture and why, and what the goldens do
not prove even for the combinations they cover. A port task reads it before
concluding that a missing golden is permission.

## Tools

| Tool | Purpose | Entry point | Artifacts |
| --- | --- | --- | --- |
| `make` | build, test, vet, regress and install the CLI | `make build` / `test` / `vet` / `regress` / `install` / `clean` | binary at `tools/agents-management/agents-management` |
| `agents-management` | the CLI this repo builds (extraction target) | `tools/agents-management` (Go `main` package) | installed copy at `~/.local/bin/agents-management`, `.temp/` logs |
| parity capture | regenerate the launch-surface goldens from the extraction source | `.scripts/capture-parity-goldens.sh` | `pkg/agentic/parity/testdata/goldens/*.json`, scratch in `.temp/parity-capture/` |
| model registry capture | regenerate the vendor fixtures: the source's model rows and frozen v2 tiers (read from its Go sources) and the admitted-pair digests (read from its own binary) | `.scripts/capture-model-registry.sh` | `pkg/vendorplugin/testdata/source-model-registry.json`, `pkg/vendorplugin/testdata/source-admitted-pairs.json`, scratch in `.temp/capture-model-registry/` |
| board model-facts capture (TRANSITIONAL) | regenerate the frozen capture of the BOARD table's own model facts — score, lifecycle, supersession, recommendation, context window, pricing — read from the board binary's `q 'models()'` projection at a named commit. Dies with the board's half of the swap: when the board reads these facts from this module, delete the fixture and `pkg/vendorplugin/boardfacts_test.go` rather than regenerating them | `.scripts/capture-board-model-facts.sh [--source /path/to/skill-project-management]` | `pkg/vendorplugin/testdata/board-model-facts.json`, scratch in `.temp/capture-board-model-facts/` |
| codex mutation harness | narrow every codex gate one at a time and confirm the suite goes red | `python3 .temp/TASK-260822-hp5fb4/mutants.py` | `.temp/TASK-260822-hp5fb4/mutants-*.log` |
| claude mutation harness | narrow every claude gate one at a time and confirm the suite goes red | `python3 .temp/TASK-260822-3u97y3/mutants.py` | `.temp/TASK-260822-3u97y3/mutants-*.log` |
| qwen/gemini/muse/agy mutation harness | narrow every gate the four remaining ports wrote, plus the two shared internals, and confirm the suite goes red | `python3 .temp/TASK-260822-xz8rj5/mutants.py` | `.temp/TASK-260822-xz8rj5/mutants-*.log` |
| limit-state capture | capture the operator's live limit state, a suppression written by the SOURCE module, and the source's own report over bytes this repo wrote | `.scripts/capture-limit-state.sh [SOURCE_REPO=/path/to/skill-project-management]` | `pkg/providerlimits/testdata/{real-state,source-written,source-read,source-tables.json}`, scratch in `.temp/TASK-260822-2jouz3/xrt/` |
| limit-plane mutation harness | narrow every identity, schema, verdict, ladder and guard gate the limit-plane port wrote and confirm the suite goes red | `python3 .temp/TASK-260822-2jouz3/mutants.py` | `.temp/TASK-260822-2jouz3/mutants-*.log` |
| inference-engine and Pi contract tests | validate public observation adapters, production-entry refusal, exact Pi Process-A plans, and closed result classification without runtime contact | `go test ./pkg/vendorplugin ./pkg/agentic/systems/pi -count=1` | terminal output; task evidence under `.temp/` and board outcome resources |
| vendor-layer mutation harness | narrow every gate the vendor port wrote — the admission expansion, the digest serialization, the ported rows and the per-vendor guard homes — and confirm the suite goes red | `python3 .temp/TASK-260822-3cknas/mutants.py` | `.temp/TASK-260822-3cknas/mutants-*.log` |
| board-facts mutation harness | narrow every gate the board-facts port added — lifecycle, score, supersession, recommendation, context window, pricing, the vendor-unresolved runtime's rows, the derived lineup and the digest serialization — and confirm the suite goes red naming the right test. Every mutant is a compile-clean NARROWING rather than a deletion, so a kill proves the class is covered rather than the line is present | `python3 .temp/TASK-260824-y7gyco/mutants.py` | `.temp/TASK-260824-y7gyco/mutants-01.log` |
| regress mutation harness | narrow every gate `make regress` claims to hold, one at a time, and confirm the net goes red naming the right test | `python3 .temp/TASK-260823-4f5t1m/mutants.py` | `.temp/TASK-260823-4f5t1m/mutants-*.log` |
| refusal-matrix harness | copy the current tree, narrow each raw plugin, typed plan and vendor-registration error class independently, and require its named production-entry negative to fail with exit `1` | `python3 .scripts/verify-refusal-matrix.py` | `.temp/TASK-260830-1jpse1/mutants/{summary.tsv,*.log}` |
