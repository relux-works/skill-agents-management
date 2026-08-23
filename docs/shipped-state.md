# Shipped state and honest outcomes

What this repository actually contains, story by story, and the outcomes that
are true but easy to lose: divergences that were chosen, code that deliberately
stayed behind in `skill-project-management`, defects pinned rather than fixed,
and arrangements that are prepared rather than done.

Each entry names an **owner**: the repository whose next change closes it.
An outcome with no owner is a rumour.

Written at the close of `EPIC-260821-1qvnz2`. Every claim below was read off
the two boards and the two checkouts rather than remembered.

## The stories

`EPIC-260821-1qvnz2` decomposes into six stories. **Five are landed** — four on
this repository's `main`, and the switch on the consumer's trunk, where its work
actually lives.

| Story | State | What landed |
| --- | --- | --- |
| `STORY-260821-224xfu` core-module-and-plugin-contracts | done | The Go module, the `agents-management` command tree, `pkg/agentic`'s system contract and registry, `pkg/vendorplugin`'s vendor contract, the structured `Availability` verdict, and the single-source guards |
| `STORY-260821-1xppz3` port-agentic-system-plugins | done | All six Layer-1 plugins — `codex`, `claude-code`, `qwen-code`, `gemini-cli`, `muse`, `antigravity` — each proven against the extraction source's own launch-surface goldens through the real `Registry` and `BuildPlan`, plus `internal/{runtimeenv,mcpjson,paritycase,argvguard,gosources}` |
| `STORY-260821-3b4ewr` port-vendor-plugins-and-registries | done | The four Layer-2 plugins and all 41 vendor-owned model rows, the frozen six runtime declarations, `ExpandV2Ceiling` and the admitted-pair digests pinned against the source binary's own output |
| `STORY-260821-2m8cpr` port-limit-plane-and-availability | done | `pkg/providerlimits` — identity, state, groups, classifiers, ladder, leases, probe claims, report — and `AvailabilityFor`, the seam that projects limit state as a `vendorplugin.Availability` |
| `STORY-260821-17pnec` docs-skill-and-regress | in progress | This document, `SKILL.md`, the README/architecture reconciliation, and `make regress` |
| `STORY-260821-1c5o90` switch-task-board-to-the-tool | done, via its mirror | No task ever ran on this board: the work is consumer-side and ran on the SOURCE board as `STORY-260823-1sxcmg`, which integrated at `b34aa20`. This story was closed against it — see below |

### Where the switch story actually lives

The switch is consumer-side work, so it ran on `skill-project-management`'s
board as `STORY-260823-1sxcmg` *consume-agents-management-for-the-spawn-plane*,
now `done` and integrated on that repository's trunk at **`b34aa20`**:

| Task | State |
| --- | --- |
| `TASK-260823-1tis7o` wire the module and swap the model registry | done |
| `TASK-260823-io02de` swap launch adapters to system plugins | done |
| `TASK-260823-1rj1gf` swap the limit plane and delete the rest | done |
| `TASK-260823-t2xuen` make CI build the module dependency | done — landed with `b34aa20` |

`STORY-260821-1c5o90` on this board is the same work seen from this side. It
carried no tasks of its own and was closed against the source story, which is
where the commits are. Nothing in this repository's history belongs to it — the
divergence was one of bookkeeping, and it is now reconciled rather than open.

## Honest outcomes

### 1. Model descriptions diverge between the two repositories, by design

Every one of the 41 shared model rows carries a DIFFERENT usage description
here and in `task-board`. Measured, not assumed: `TASK-260823-1tis7o` probed
the module's live registry against the consumer's pre-change `models()` output
field by field, and found the id set, the agentic-system bindings, the effort
support, the vocabularies and the recommended efforts identical — and the
description different on all 41.

They are two authorings of one fact, not a drifted copy. This repository's
texts are marked AUTHORED HERE in every vendor binding file: the extraction
source has no what-is-this-model-best-for field, and `Model.Validate` refuses a
blank description, so they had to be written. The consumer's texts are its own
and its acceptance criteria required them byte-identical across the swap.

**Owner: this repository.** Collapsing them means one side reads the other's
string, and the module is the side that can be read. Until then the divergence
is stated in both places rather than discovered.

Two smaller residues of the same swap, both stated in the consumer's own notes
and neither a duplicated fact: the consumer keeps `PolicyRank` as a SCORE
(scores carry ties that this layer's tie-free `Rank.Position` cannot express,
and two admission call sites compare them), and it keeps the two `muse` rows,
which belong to no vendor here.

### 2. Deliberate keeps in `task-board`

The sweep in `TASK-260823-1rj1gf` walked the whole spawn plane for code still
present in both repositories. Four things stay in the consumer on purpose:

- **Exec mechanics** — `*exec.Cmd`, working directory, process group, deadline
  plumbing. This module owns no execution: `BuildPlan` and `BuildLaunch` end at
  a plan. Expected, and not a residual.
- **Preflights that start processes** — the Antigravity probe (`agy --version`
  / `agy --help`, validating a minimum version and seven headless flags) and
  Claude goal preparation (a version gate, a `/goal` capability probe, a
  session-state decision). Their RESULTS cross the boundary — `agy.NewWithRuntime`
  takes the preflighted executable — but the probes cannot: `BuildPlan` calls
  every plugin method to build a DRY RUN, so a probe behind any of them would
  make a dry run execute the harness. `agy/runtime.go` and `claude/goal.go`
  carry the whole boundary in prose.
- **The dev-only fault injector** — launch-plane behaviour, and its captured
  payload trips the codex argv guard on a field it did not construct. It is now
  `tools/board-cli/internal/limitsimulate`, landing on the `Layout.SimulateFile()`
  paths this module reserved for it.
- **The launch-composition prefix validators** — `validateCodexLaunchCompositionPrefix`
  and `validateClaudeLaunchCompositionPrefix` in the consumer, and
  `codex/composition.go` / `claude/composition.go` here, which say they are the
  source's rules ported rule for rule. The consumer's gate is board-facing and
  validates against MCP metadata recorded on the board; the plugin validates the
  same shape again inside `BuildPlan`.

  **FLAGGED: nothing tests that the two rule sets agree.** ~150 lines of rules
  in two places, with no cross-repository agreement check. An equality test is
  the wrong instrument at that size; the right fix is for the board-facing gate
  to call `System.ValidateComposition` for the shape and keep only the metadata
  cross-check locally. **Owner: `skill-project-management`** — it is the caller,
  and the fix is on its side of the seam.

  The same shape one size down was closed: the auth-hint remediation text is
  duplicated between the consumer's `providerAuthHint` and each plugin's
  `Capabilities().AuthHint`, and `TestLocalAuthHintsAgreeWithTheAgenticPlugins`
  now fails on any inequality.

### 3. Open leak pins

`BUG-260819-3qn52o` *child-env-filters-are-incomplete-and-asymmetric* is in
**backlog on the source board, unfixed**, and was unfixed at the commit the
parity goldens were captured from. Its shape is that each system's environment
filter names only the runtimes its author knew about:

| Plugin | What the child inherits that it should not |
| --- | --- |
| `codex` | the two board credentials the bug records |
| `claude-code` | the whole `CODEX_*` family, the app-server and session-manager URLs, and the credentials their pointers name — the bug seen from the other side |
| `qwen-code` | the two board credentials, plus `QWEN_CODE_SESSION_ID`, which nothing strips, so a qwen child spawned from inside a qwen session inherits its parent's session id |
| `gemini-cli`, `muse`, `antigravity` | everything — their filters are EMPTY, and `env.go` says so rather than expressing it by omitting the file |

Every one is pinned by a test asserting the CURRENT behaviour, so closing one
has to be a deliberate edit to the pin and its comment. That is the port's
contract: a parity port that quietly fixed a leak would be a behaviour change
wearing a refactor's clothes, and the goldens would have to be recaptured to
prove it.

**Owner: `skill-project-management`**, which owns the bug. When it closes, the
pins here are what tells the next reader which filters to widen and in what
order.

A second gap, found by the same sweep and outside this module: **qwen-codex has
no `providerAuthHint` row**, so its authentication failures are not classified
as capability failures and take the autonomous-recovery path — retrying into a
locked-out account. Closing it changes an operator-visible outcome for that
runtime (a fail-closed refusal instead of a retry), so it was named rather than
patched: the consumer's `authHintGapsFoundByTheSwapSweep` makes a SECOND such
gap fail its suite. **Owner: `skill-project-management`.**

`qwen-codex` itself is not declared here and must not become a frozen runtime:
it is the operator-declared cross-runtime (alibaba models under the codex
harness), declared from the consumer's config. This repository proves the
pairing works — `pkg/vendorplugin/admissionpin_test.go` builds a real launch
through it — without seeding it.

### 4. The CI arrangement: landed on both sides, one human step left

Since the consumer's first swap, `tools/board-cli/go.mod` replaced this module
with a SIBLING checkout path. GitHub Actions cannot check out a second
repository above the workspace, so the consumer's `test-cli` and `test-tui`
jobs could not build at all. `TASK-260823-1rj1gf` flagged it as needing a
decision rather than a patch; `TASK-260823-t2xuen` is that decision, and it
landed with `STORY-260823-1sxcmg` at **`b34aa20`** on the consumer's trunk:

| Half | State |
| --- | --- |
| **This module is tagged** `v0.1.0`, at `b722ace`, pushed to `origin` | **done** |
| Consumer requires the tag with **no `replace`** on trunk | **done**, on `b34aa20` |
| ~~`GOPRIVATE` and a `url.insteadOf` rewrite in the consumer's CI~~ | **retired 2026-08-23**: the repo went public, the consumer fetches via the default proxy with sum-db on, and its guard now refuses leftover credential plumbing |
| Local sibling development via a root `go.work`, gitignored | **done**, on `b34aa20` |
| The guard pair: pinned `actionlint` (`v1.7.12`, in the consumer's Makefile and its own CI job) plus `tools/board-cli/internal/ciguard` | **done**, on `b34aa20` |
| ~~The Actions credential (`RELUX_MODULES_TOKEN`)~~ | **not needed**: the owner made this repository public on 2026-08-23 instead |

The guard pair is what keeps the arrangement from silently unravelling again,
and the split between its two halves is deliberate. `actionlint` owns workflow
GRAMMAR — including the `secrets`-in-`if` class in every spelling, folded
scalars and bracket indexing included, which a line regex admits. `ciguard`
owns what is a fact about that repository rather than about YAML: no `go.mod`
may `replace` outside the checkout, `go.work` must stay untracked, CI must
actually run the pinned linter, and the credential-rewrite step must FAIL
CLOSED when the secret is absent — asked of the run block with its `#` comments
stripped first, because a note explaining a rewrite is neither a rewrite nor a
failure. The guards themselves run uncached (`-count=1`), which is its own
scanner.

Two traps are worth carrying rather than rediscovering:

- **A committed root `go.work` applies in CI.** `go.work` is discovered by
  walking UP from the working directory, so committing it reintroduces exactly
  the sibling-checkout assumption the `replace` removal took out of `go.mod`.
  It is also per-checkout: a story worktree under `.temp/` needs a different
  relative path to the sibling than the main clone does. It stays gitignored.
- **`secrets` is not available to a workflow `if:` key.** Referencing it there
  is a syntax error that kills the whole run before a single job starts, which
  is what happened to the consumer's CI file between 2026-08-19 and 2026-08-23.
  The token check reads the secret through `env:` and fails loudly with the fix
  in the message instead.

Alternatives closed earlier and not reopened: vendoring was deliberately
dropped (`TASK-260819-3vr8j3` — Go auto-enables `-mod=vendor` the moment
`vendor/modules.txt` exists, and a stale tree then fails every build), and
committing the `replace` means CI can never build.

The workflow, the `go.mod` and the `go.work` documentation are all on the
consumer's trunk, and nothing is open: the owner made this repository public on
2026-08-23, which retired the credential half entirely. Nothing in THIS
repository's CI depended on any of it — see
[consuming-the-module.md](consuming-the-module.md) for what a consumer writes
today.

### 5. The local-model plugin is design only

[architecture.md](architecture.md) describes a local-models plugin owning a
shared resource plane — load/unload awareness, inference-busy observation,
memory-pressure sequencing, eviction ordering. **None of it is built.**

What exists is the SEAM it has to fit through, and only that: `Availability` is
a structured verdict rather than a boolean, so a local runtime's answer
composes without an interface change. `pkg/vendorplugin/availability_test.go`
demonstrates five such answers fitting — it exercises the verdict type, not a
resource plane, and there is no local-models package, no sub-plugin contract
and no admission queue anywhere in this module.

**Owner: this repository**, after the extraction proves the seams. The section
in `architecture.md` is titled *design only, not in current scope* and should
keep saying so until something is built.

### 6. What the CLI answers today, and what that answer means

`agents-management plugins` and `agents-management vendors` print an empty list
and exit 0 in the shipped binary. That is the truthful answer, not a stub: the
command tree reads `agentic.Default` and `vendorplugin.Default`, and
`tools/agents-management` imports no plugin package, so none is compiled in.
An empty list and a failure to look are different facts, and only the first is
being reported.

`agents-management runtimes` prints all six frozen declarations regardless,
because a runtime is a DECLARATION and does not need its plugins present.

**Owner: this repository.** Whether the shipped binary should carry the plugins
was left open for the switch story to settle, and the answer it gave is that
nothing forces the question: the consumer links the packages directly and never
runs this binary. The empty list stays truthful and unused until something
other than the consumer needs the binary to enumerate.
