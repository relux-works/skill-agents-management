# skill-agents-management

A skill plus one CLI — **`agents-management`** — for spawning and managing
heterogeneous agentic systems and the model vendors behind them.

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
  claude-code, codex, qwen-code, gemini-cli, antigravity, muse) combined with a
  model vendor (who owns the models, the authentication and the quota:
  anthropic, openai, alibaba, google).
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

Two plugin layers — vendor plugins depend on agentic-system plugins and
declare which systems they support. See [docs/architecture.md](docs/architecture.md)
for the contract, the layering rules, and the planned local-model plugin
(load/unload awareness, inference-busy state, memory-pressure sequencing).

### Layer 1: `pkg/agentic`

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

No concrete system plugin ships yet. The contract is proven by one test double
registered through the public API.

### Layer 2: `pkg/vendorplugin`

The vendor plugin contract, its registry, and the runtime declarations.

- `Vendor` is the plugin interface: identity, `Models()`, `Availability()` and
  `Spawn()`. A model row carries its capability rank WITH the evidence behind
  it (a rank with no observation is refused at registration — ranking is never
  policy), a usage description that cannot be silently empty, its
  reasoning-effort vocabulary and the vendor's recommended word, and the
  agentic systems that can drive it. Effort TRANSPORT is not repeated here: it
  belongs to the system plugin, and a launch needs both halves from their own
  owners.
- **The dependency direction is enforced at registration.** A vendor naming an
  agentic system that is not registered is refused, with both ids in the error.
  A registry built without an agentic registry refuses every vendor rather than
  admitting one whose declared systems nobody checked.
- `Availability` is a structured verdict, not a boolean: healthy,
  limited-until(time, evidence), unreachable(evidence), or unknown — with
  "checked and found nothing" distinguishable from "nobody looked" and from "the
  read failed". The zero value is unknown, and only an observed healthy verdict
  is serviceable. The local-model resource plane described in
  `docs/architecture.md` reports through these same fields; a test demonstrates
  each of its five answers fitting with no interface change.
- **Runtimes are declared pairs.** `RuntimeDeclaration` binds a stable id to
  one agentic system and one vendor, and the six historical ids (`claude`,
  `codex`, `qwen`, `gemini`, `agy`, `muse`) are seeded from the extraction
  source's frozen `runtimeid` table into every binary's default registry.
  `muse`'s broker is recorded UNKNOWN with a checked-and-empty evidence list,
  exactly as the source records it — an unresolved-vendor runtime is a complete
  declaration and an unlaunchable one, refused on its own terms rather than
  guessed at. The F2 collision policy applies: a matching redeclaration is
  idempotent, a conflicting one is refused and the first declaration stands.
- `BuildLaunch` is the single Layer-2 dispatch site. It resolves the pair,
  selects the model, validates the effort word against the model's own
  vocabulary (never substituting the recommendation — no default is injected
  anywhere), asks the vendor to build an `agentic.LaunchRequest`, refuses one
  that REDIRECTS the launch (a vendor may add authentication environment; it
  may not change the harness, the model, the effort, the goal, the budget, the
  tier or the composition), and hands it to `agentic.BuildPlan`. Spawn
  EXECUTION is not here — that is a port story.

The single-source guard now covers both layers from ONE list of dispatch key
types (`SystemID`, `VendorID`, `RuntimeID`), each mapped to the single file
permitted to bind it. That is stricter than a flat allowlist: a
`map[VendorID]T` inside the agentic registry is a violation even though that
file legitimately holds a binding map, and a mutant plants exactly that.

No concrete vendor plugin ships yet — that is the next story. The contract is
proven by one test double, registered through the public API, that exists
nowhere else in the module.

Identifier normalization for all three id kinds lives once, in
`internal/ident`. Two folds that agree today and drift tomorrow is the
duplicate-charset failure the extraction source paid for, and a test holds both
layers to the same rule.

## Status

Extraction in progress. The current scope is moving the vendors and agentic
systems that already exist in `task-board` into this tool without behaviour
change: same observable launch surface (argv, environment, stdin bytes, side
effects), same admitted-pair digests, same on-disk limit-state identity. New
plugins come after the extraction proves the seams.

## Development

Work is tracked on this repository's own board (`.task-board/`) through the
`task-board` CLI — see the `project-management` skill. Go test authoring
follows the `go-testing-tools` skill.

### Build

One Go module at the repository root, module path
`github.com/relux-works/skill-agents-management`. The CLI's `main` package
lives in `tools/agents-management`; shared packages will land under `pkg/`
as the extraction moves plugin contracts across.

```
make build     # build tools/agents-management/agents-management
make test      # go test ./... -count=1
make vet       # go vet ./...
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

`make vet`, `make build` and the test command are also this repository's
landing gate: `spawn.worktree_isolation.validation.commands` in
`task-board.config.json` runs exactly that list before a Change Request may
land, so its evidence is bound to the tree being integrated.

### Current CLI surface

Four commands of its own, because this stage is about seams rather than
features (cobra contributes `help` and `completion`):

```
agents-management version           # build version, commit and build date
agents-management plugins [--json]  # the agentic system plugins compiled in
agents-management vendors [--json]  # the vendor plugins compiled in
agents-management runtimes [--json] # the declared (system x vendor) pairs
```

`plugins` and `vendors` read the `pkg/agentic` and `pkg/vendorplugin` default
registries — the same ones a plugin package registers into from its `init` —
and print the registered ids. Both print an empty list and exit 0 today. That
is the answer, not a stub: no plugin has been compiled in yet, which is a
different fact from a failure to look. `--json` renders the empty case as `[]`,
never `null`.

`runtimes` prints the six frozen declarations as `id⇥system⇥vendor`. A runtime
is a DECLARATION, so it lists what is declared rather than what can currently
launch: every id appears whether or not its plugins are compiled in, and
`muse` reads as `vendor unresolved` rather than as a blank column. The JSON
form carries the broker provenance — which source was checked, and that nothing
established a vendor — because "unresolved" is only honest when the search
behind it is visible.

None of these commands keeps a list of its own. A private one would be a
second binding for the same fact, which is exactly what the single-source
guard exists to prevent.

## Tools

| Tool | Purpose | Entry point | Artifacts |
| --- | --- | --- | --- |
| `task-board` | board tracking for this repo's work | `task-board q/m/spawn ...` | `.task-board/` |
| `make` | build, test, vet and install the CLI | `make build` / `test` / `vet` / `install` / `clean` | binary at `tools/agents-management/agents-management` |
| `agents-management` | the CLI this repo builds (extraction target) | `tools/agents-management` (Go `main` package) | installed copy at `~/.local/bin/agents-management`, `.temp/` logs |
