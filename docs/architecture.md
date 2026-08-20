# Architecture

`agents-management` is a plugin system with two layers and a deliberately
narrow core. The core owns registration, admission and observation; every
fact about a concrete harness or a concrete vendor lives in a plugin.

## The two layers

### Layer 1 — agentic-system plugins

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

### Layer 2 — vendor plugins

A **vendor** owns models, authentication and quota: anthropic, openai,
alibaba, google. A vendor plugin **depends on agentic-system plugins** and
declares which systems can drive its models. That dependency direction is the
load-bearing decision: a runtime is the *pair* (agentic system × vendor), so
cross-runtime combinations — Qwen models under the Codex harness — are just a
vendor declaring support for one more system, with no core change.

A vendor plugin's interface, at minimum:

- **Models** — the model list, each with:
  - capability ranking (evidence-based; ranking is never policy),
  - a description of what the model is best used for,
  - its reasoning-effort vocabulary and recommended effort (effort is a
    required per-model axis; no defaults are injected anywhere),
- **Availability** — limit state (if the vendor rate-limits), and a health
  check that answers "can requests actually be made right now",
- **Spawn** — launching a model under one of its supported agentic systems
  with the full parameter surface: model, effort, environment, stdin,
  goal/budget/tier, composition.

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
refused and the first declaration stands.

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
   row. Refusals name the exact dotted config key and the accepted values, so
   an agent hitting one mid-run can fix the config from the error text alone.
5. **Single source per fact.** One adapter table, one runtime registry, one
   normalization for identifiers. The extraction source paid repeatedly for
   shadow tables and duplicate charsets; the plugin registry is the only
   place a binding may live, and guards should make a second one fail a test.

## Planned: the local-model plugin (design only, not in current scope)

Local models (muse today; local qwen and others next) need what remote
vendors do not: **resource awareness**. A local vendor cannot answer
"available?" without knowing:

- is the model currently loaded,
- is inference running on it right now, or is it idle,
- is there memory to load another model, or must one be evicted first,
- if eviction is needed — has the running model finished, or must we wait for
  its current work before unloading.

The design direction: one **local-models plugin** owning the shared resource
plane (memory accounting, load/unload sequencing, busy/idle observation), with
per-vendor **sub-plugins** beneath it for model-specific facts. The
availability answer for a local runtime then composes the vendor's own state
with the resource plane's verdict, and spawn admission can *queue on* an
eviction rather than failing. This layer is documented now so the plugin
interfaces leave room for it (availability as a structured verdict rather
than a boolean; admission as an async decision), but nothing of it is built
in the current extraction scope.

## Boundaries with task-board

| Stays in task-board | Moves here |
| --- | --- |
| roles, role archetypes | runtime registry, adapter table |
| tasks, stories, epics, bugs | model registries, ranking, effort vocabularies |
| Change Requests, review routing | limit detection, classification, suppression, backoff |
| worktree isolation, integration | health/availability checks |
| goals, handoff, progress records | launch: argv/env/stdin/composition, spawn parameters |

`task-board` calls this tool for spawn decisions and launches; it never
reaches into plugin internals. This tool never reads a board.

## Extraction plan (current scope)

Move the already-existing vendors and agentic systems out of
`skill-project-management` behind the plugin seams, proving at each step:

1. parity of the observable launch surface per (system, mode),
2. byte-stable admitted-pair digests over real configs,
3. unchanged on-disk limit-state identity, demonstrated on real state files,
4. task-board consuming the tool with its own spawn surface observably
   unchanged.

Everything beyond that — new vendors, new agentic systems (opencode and
others), the local-model resource plane — is deliberately after.
