---
name: agents-management
description: >
  Spawning and managing heterogeneous agentic systems and the model vendors
  behind them, through the agents-management Go module and CLI. Use for
  agentic-system plugins (claude-code, codex, qwen-code, gemini-cli,
  antigravity, muse), vendor plugins (anthropic, openai, alibaba, google),
  runtime declarations, per-model reasoning-effort vocabularies, capability
  ranking, general plugin graphs, inference engines, multi-node launch plans
  (argv/env/stdin), provider limit state and
  availability verdicts. Not a task board: roles, tasks and reviews live in
  project-management. Russian triggers: агент, спавн, раннтайм, вендор,
  лимиты, модели, эффорт.
triggers:
  - agentic system, agent harness, spawn plane, launch surface, argv parity
  - vendor plugin, model registry, capability rank, reasoning effort vocabulary
  - runtime declaration, admitted pairs, provider limits, rate limit, availability
  - агент, спавн, раннтайм, вендор, лимиты, модели, эффорт
---

# agents-management

One Go module and one CLI for the question *which agent runs, on which model,
and whether it may launch right now*. It is the spawn plane extracted from
`skill-project-management`, behind a general plugin graph.

**It knows how to run agents. It does not know what they should work on** —
roles, tasks, Change Requests, review routing and worktree isolation stay in
`task-board`, and this module never reads a board.

**It builds plans; it does not execute.** Every LAUNCH-plane entry point ends
at a value: a binary, an argv, an environment, stdin bytes. `BuildPlan` and
`BuildLaunch` start no process — that is what makes a dry run safe, and it is
the property to rely on.

Scope it to the launch plane, though, because the limit plane is not silent:
`pkg/providerlimits` shells out to `ps` when it checks whether a lease or probe
claim is still held by a live pid (`liveness_unix.go`, reached from lock
acquisition and from the lease/claim liveness checks), because pid reuse is
otherwise undetectable. The read path this skill documents does not fork —
`AvailabilityFor` takes no lock — but a consumer sandboxing process creation
should know before it wires `providerlimits`, not after.

## The plugin graph

Every plugin declares `ID`, opaque `Kind`, and dependencies. The registry knows
no layer numbers or kind allowlist; it resolves the declared graph and refuses
missing dependencies, kind mismatches and cycles at registration. An **agentic
system** is the harness that runs a turn, a **vendor** owns models,
authentication and quota, and an **inference engine** contributes Process B.
A legacy **runtime** remains a declared system/vendor pair under a stable id.

| Compatibility kind | Package | Plugins |
| --- | --- | --- |
| agentic systems | `pkg/agentic` | `claude-code`, `codex`, `qwen-code`, `gemini-cli`, `antigravity`, `muse`, `pi` |
| model vendors | `pkg/vendorplugin` | `anthropic`, `openai`, `alibaba`, `google`, conditional `local-models` |
| inference engines | `pkg/inferenceengine` | first extensible kind; concrete engines are consumer-selected |
| runtimes (declarations) | `pkg/vendorplugin` | `claude`, `codex`, `qwen`, `gemini`, `agy`, `muse` — frozen |

Existing vendors retain their vendor→system edges, and a vendor naming an
unregistered system is refused with BOTH ids. That is compatibility semantics,
not a registry direction rule: a system may declare an engine dependency, and
future kinds may point whichever way their declarations state.

A plugin id and a runtime id are different facts and often different spellings:
the agentic-system plugin is `claude-code`, `qwen-code`, `antigravity`; the frozen
runtime is `claude`, `qwen`, `agy`. `muse` is the one case where they coincide,
and they are still two facts.

`muse`'s vendor is **unresolved** — looked for in the source's frozen table and
never established. That is a complete declaration and an unlaunchable one,
refused on its own terms. Nothing guesses a vendor for it.

## The CLI

Five commands, and this is the whole surface today. Cobra contributes `help`
and `completion`.

```
agents-management version           # build version, commit, build date
agents-management plugins  [--json] # agentic system plugins compiled in
agents-management vendors  [--json] # vendor plugins compiled in
agents-management runtimes [--json] # declared (system x vendor) pairs
agents-management local-runtime status [--json] # conditional local-model status
```

`plugins` and `vendors` read the package-level default registries. **In the
shipped binary both print an empty list and exit 0**: the binary imports no
self-registering plugin package. It links `local-models` only for the status
command, and that plugin deliberately registers conditionally. An empty list is
a real answer, not a failure to look.
`--json` renders it as `[]`, never `null`.

`runtimes` prints `id⇥system⇥vendor` for all six regardless, because a runtime
is a DECLARATION and does not need its plugins present. `muse` reads
`vendor unresolved`; the JSON form carries the broker provenance — which source
was checked, and that nothing established a vendor.

```bash
make build     # -> tools/agents-management/agents-management
make install   # -> ~/.local/bin/agents-management
make vet ; make test ; make regress
```

There is no `spawn`, no `availability` and no `models` command. The consumer
links the packages; the binary is a listing surface.

## Wiring it into a consumer

```
github.com/relux-works/skill-agents-management v0.4.3   # public module, no replace, no credential
```

A binary gets the plugins it blank-imports; each registers itself from its own
`init`. Importing a vendor pulls in the systems its models declare, so a
half-wired binary is not reachable. Sibling development goes through `go.work`
(gitignored — a committed one applies in CI), never a committed `replace`,
never vendoring.

Full recipe, including the `v0.2.0` breaking change (`CapabilityRank.Position`
became `.Score`; the total order is derived by `vendorplugin.Lineup`):
**[docs/consuming-the-module.md](docs/consuming-the-module.md)**.

## Entry points

| Question | Call |
| --- | --- |
| What exactly would this launch run? | `agentic.BuildPlan(registry, req, mode)` → `Plan{Binary, Argv, Env, Stdin, …}` |
| Add engine/sidecar nodes | `agentic.BuildMultiNodePlan(primary, dependencies, nodes...)` |
| The same, through a runtime and its vendor | `vendorplugin.BuildLaunch(registry, req, mode)` |
| Publish and validate launch identity metadata | `plan.ConsumerProvenance()` → versioned `engine_binding` plus configured/resolved engine projection; after persistence call `registry.ValidateLaunchProvenance(record)` so registry declarations and graph resolution, not the record itself, supply authority; `none` is valid only for a system-only legacy shape |
| Validate observed engine facts before launch | `vendorplugin.BuildLaunch(ctx, registry, request, mode)` invokes the package-owned engine source before `Spawn`/`Preflight`; `inferenceengine.ValidateReadings` is schema-only and cannot install evidence |
| May it launch right now? | `providerlimits.Store.AvailabilityFor(VerdictQuery{…})` → `vendorplugin.Availability` |

`BuildPlan` is the system dispatch site and `BuildLaunch` the legacy runtime
dispatch site. There is no switch over system ids anywhere, and a static guard
fails the build if one appears.

`Availability` is a structured verdict, never a boolean: healthy,
limited-until(time, evidence), unreachable(evidence), or unknown. Only an
OBSERVED healthy verdict is serviceable, and "checked and found nothing",
"nobody looked" and "the read failed" stay three different answers.

## Invariants — contracts, not suggestions

1. **The observable launch surface is the parity bar.** A launch is argv,
   environment, stdin bytes and side effects. Argv-string equality is not
   parity. Goldens live in `pkg/agentic/parity/testdata/goldens/` and are
   captured by the EXTRACTION SOURCE's harness, never here.
2. **Limit state fails open.** An absent state file reads as "provider
   healthy" with no error anywhere, so `IdentityKey(provider, home)` — which
   names the state file — must never move without a demonstrated migration on
   real state files. An indeterminate read is Unknown, never Healthy: an
   absence and a failure to read are different facts.
3. **Admitted-pair digests are frozen.** Downstream snapshots pin them; a
   silent change is a compatibility break. Model MEMBERSHIP comes from the
   frozen v2 snapshot, effort vocabularies from the vendor rows, and the two
   stay apart — a capability score is evidence, and the day it decides
   admission a truthful re-rank silently moves who may spawn. The same rule
   covers every other presentation fact a row carries (lifecycle, supersession,
   display recommendation, context window, price): none of them reaches the
   digest, and eight mutants prove it rather than a comment asserting it.
4. **Effort is per-model and required.** Vocabularies live on the model row;
   TRANSPORT (argv / stdin / none) lives on the system plugin. No default is
   injected at any call site, and a refusal names the model, the runtime, the
   accepted vocabulary and the recommendation — enough to pick a valid effort
   from the error text. It cannot name a config key, because the config is the
   consumer's; naming it is the consumer's half of the same error.
5. **One source per fact.** One general graph, one compatibility runtime registry, one identifier
   normalization, one argv construction site per plugin. Guards make a second
   one fail a test rather than fail a review.
6. **Observed facts come from a bound concrete engine kind.** Resolver calls do
   not accept a registry or observation implementation. Fact-specific closed
   schemas reject plausible arbitrary JSON, and local execution and SSH
   forwarding are mutually exclusive observed profile variants.

## Landing gate

```bash
make vet
make build
env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1
make regress
gofmt -l pkg/ internal/
```

`make regress` (`internal/regress`) is the fast cross-cutting net: a plugin
registration refusal naming both ids, a runtime declaration and its F2
collision in both directions, an availability verdict derived from a real state
file, and a `BuildPlan` parity smoke against one golden per Layer-1 system —
each with the negative that shows it bites. It runs in about a second because
it sits in front of every landing.

`env -u TASK_BOARD_DIR` is not decoration: an inherited board directory reaches
the test process and is not this module's to read.

## Lazy reference routes

| Need | Read |
| --- | --- |
| What the tool is, per-plugin behaviour, guards, fixtures, capture scripts | [README.md](README.md) |
| The plugin graph contract, compatibility edges, invariants, boundaries with task-board | [docs/architecture.md](docs/architecture.md) |
| Depending on the module, the version to require, `go.work`, declaring a runtime | [docs/consuming-the-module.md](docs/consuming-the-module.md) |
| What shipped, what deliberately stayed behind, open pins, owners | [docs/shipped-state.md](docs/shipped-state.md) |
| What the goldens prove and what the capture could NOT reach | [pkg/agentic/parity/testdata/goldens/README.md](pkg/agentic/parity/testdata/goldens/README.md) |

The local-model plane has a real `pi` System, conditional `local-models`
Vendor, read-only status consumer, and the generic inference-engine kind plus
typed multi-node plan. Process lifecycle, load/unload, supervision and
attestation remain consumer-owned; the plan is a value, never an executor.

Work in this repository is tracked on its own `.task-board/` through the
`task-board` CLI; follow the `project-management` skill, and `go-testing-tools`
for test authoring.
