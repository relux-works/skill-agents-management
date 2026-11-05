---
name: agents-management
description: >
  Spawning and managing heterogeneous agentic systems and the model vendors
  behind them, through the agents-management Go module and CLI. Use for
  agentic-system plugins (claude-code, codex, qwen-code, gemini-cli,
  antigravity, muse), vendor plugins (anthropic, openai, alibaba, google),
  runtime declarations, per-model reasoning-effort vocabularies, capability
  ranking, launch plans (argv/env/stdin), provider limit state and
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
`skill-project-management`, behind a two-layer plugin architecture.

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

## The two layers

An **agentic system** is the harness that runs a turn. A **vendor** owns
models, authentication and quota. A **runtime** is a declared pair of the two,
under a stable id.

| Layer | Package | Plugins |
| --- | --- | --- |
| 1 — agentic systems | `pkg/agentic` | `claude-code`, `codex`, `qwen-code`, `gemini-cli`, `antigravity`, `muse` |
| 2 — vendors | `pkg/vendorplugin` | `anthropic`, `openai`, `alibaba`, `google` |
| runtimes (declarations) | `pkg/vendorplugin` | `claude`, `codex`, `qwen`, `gemini`, `agy`, `muse` — frozen |

**Vendors depend on systems, never the reverse**, and the registry enforces it:
a vendor naming an unregistered system is refused with BOTH ids in the message.
A cross-runtime combination — Qwen models under the Codex harness — is one
vendor declaring one more system, with no core change.

A plugin id and a runtime id are different facts and often different spellings:
the Layer-1 plugin is `claude-code`, `qwen-code`, `antigravity`; the frozen
runtime is `claude`, `qwen`, `agy`. `muse` is the one case where they coincide,
and they are still two facts.

`muse`'s vendor is **unresolved** — looked for in the source's frozen table and
never established. That is a complete declaration and an unlaunchable one,
refused on its own terms. Nothing guesses a vendor for it.

## The CLI

Four commands, and this is the whole surface today. Cobra contributes `help`
and `completion`.

```
agents-management version           # build version, commit, build date
agents-management plugins  [--json] # agentic system plugins compiled in
agents-management vendors  [--json] # vendor plugins compiled in
agents-management runtimes [--json] # declared (system x vendor) pairs
```

`plugins` and `vendors` read the package-level default registries. **In the
shipped binary both print an empty list and exit 0**, because
`tools/agents-management` imports no plugin package. That is the answer, not a
stub: nothing is compiled in, which is a different fact from a failure to look.
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
github.com/relux-works/skill-agents-management v0.1.0   # private module, no replace
```

A binary gets the plugins it blank-imports; each registers itself from its own
`init`. Importing a vendor pulls in the systems its models declare, so a
half-wired binary is not reachable. Sibling development goes through `go.work`
(gitignored — a committed one applies in CI), never a committed `replace`,
never vendoring.

Full recipe, including `GOPRIVATE` and the Actions credential:
**[docs/consuming-the-module.md](docs/consuming-the-module.md)**.

## The three entry points

| Question | Call |
| --- | --- |
| What exactly would this launch run? | `agentic.BuildPlan(registry, req, mode)` → `Plan{Binary, Argv, Env, Stdin, …}` |
| The same, through a runtime and its vendor | `vendorplugin.BuildLaunch(registry, req, mode)` |
| May it launch right now? | `providerlimits.Store.AvailabilityFor(VerdictQuery{…})` → `vendorplugin.Availability` |

`BuildPlan` is the single Layer-1 dispatch site and `BuildLaunch` the single
Layer-2 one. There is no switch over system ids anywhere, and a static guard
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
   stay apart — a capability rank is evidence, and the day it decides
   admission a truthful re-rank silently moves who may spawn.
4. **Effort is per-model and required.** Vocabularies live on the model row;
   TRANSPORT (argv / stdin / none) lives on the system plugin. No default is
   injected at any call site, and a refusal names the model, the runtime, the
   accepted vocabulary and the recommendation — enough to pick a valid effort
   from the error text. It cannot name a config key, because the config is the
   consumer's; naming it is the consumer's half of the same error.
5. **One source per fact.** One registry per layer, one identifier
   normalization, one argv construction site per plugin. Guards make a second
   one fail a test rather than fail a review.

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
| The plugin contract, layering rules, invariants, boundaries with task-board | [docs/architecture.md](docs/architecture.md) |
| Depending on the module, `GOPRIVATE`, `go.work`, declaring a runtime | [docs/consuming-the-module.md](docs/consuming-the-module.md) |
| What shipped, what deliberately stayed behind, open pins, owners | [docs/shipped-state.md](docs/shipped-state.md) |
| What the goldens prove and what the capture could NOT reach | [pkg/agentic/parity/testdata/goldens/README.md](pkg/agentic/parity/testdata/goldens/README.md) |

The **local-model plugin** — load/unload awareness, inference-busy state,
memory-pressure sequencing — is DESIGN ONLY in `docs/architecture.md`. None of
it is built. What exists is the seam it must fit through: availability as a
structured verdict rather than a boolean.

Work in this repository is tracked on its own `.task-board/` through the
`task-board` CLI; follow the `project-management` skill, and `go-testing-tools`
for test authoring.
