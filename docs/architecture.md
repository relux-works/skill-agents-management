# Architecture

`agents-management` is a plugin system with two layers and a deliberately
narrow core. The core owns registration, admission and observation; every
fact about a concrete harness or a concrete vendor lives in a plugin.

This document is the CONTRACT. What is built against it, what is deliberately
still open and who owns each residual is [shipped-state.md](shipped-state.md);
how a program depends on the module is
[consuming-the-module.md](consuming-the-module.md). Where the two disagree with
this file, they are describing reality and this file is describing the rule.

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
   row. A refusal names the model, the runtime, the accepted vocabulary and
   the vendor's recommendation, so an agent hitting one mid-run can pick a
   valid effort from the error text alone. It does NOT name a dotted config
   key: this module has no config file, and the key that has to change lives
   in whichever consumer supplied the effort. Naming it is the consumer's
   half of the same error.
5. **Single source per fact.** One adapter table, one runtime registry, one
   normalization for identifiers. The extraction source paid repeatedly for
   shadow tables and duplicate charsets; the plugin registry is the only
   place a binding may live, and guards should make a second one fail a test.

## Planned: the local-model plugin (design only, not in current scope)

**Nothing in this section is built.** There is no local-models package, no
sub-plugin contract, no resource plane and no admission queue anywhere in this
module. What exists is the SEAM it has to fit through — availability as a
structured verdict rather than a boolean, admission as a decision that can be
deferred — and a test in `pkg/vendorplugin` demonstrating five such answers
fitting the existing fields with no interface change. That test exercises the
verdict type. Read the rest as a design note.

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
   All three swaps and the CI arrangement landed together; the only step left
   is the `RELUX_MODULES_TOKEN` secret, which is the repository owner's to
   provision.

Everything beyond that — new vendors, new agentic systems (opencode and
others), the local-model resource plane — is deliberately after.

[shipped-state.md](shipped-state.md) carries the per-story detail and the
outcomes that are true but easy to lose.
