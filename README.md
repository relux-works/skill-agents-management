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

Deliberately two commands of its own, because this stage is about seams
rather than features (cobra contributes `help` and `completion`):

```
agents-management version          # build version, commit and build date
agents-management plugins [--json] # the plugins compiled into this binary
```

`plugins` prints an empty list and exits 0 today. That is the answer, not a
stub: no plugin has been compiled in yet, which is a different fact from a
failure to look. `--json` renders the empty case as `[]`, never `null`.

## Tools

| Tool | Purpose | Entry point | Artifacts |
| --- | --- | --- | --- |
| `task-board` | board tracking for this repo's work | `task-board q/m/spawn ...` | `.task-board/` |
| `make` | build, test, vet and install the CLI | `make build` / `test` / `vet` / `install` / `clean` | binary at `tools/agents-management/agents-management` |
| `agents-management` | the CLI this repo builds (extraction target) | `tools/agents-management` (Go `main` package) | installed copy at `~/.local/bin/agents-management`, `.temp/` logs |
