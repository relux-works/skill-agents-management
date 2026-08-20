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

## Tools

| Tool | Purpose | Entry point | Artifacts |
| --- | --- | --- | --- |
| `task-board` | board tracking for this repo's work | `task-board q/m/spawn ...` | `.task-board/` |
| `agents-management` | the CLI this repo builds (extraction target) | `tools/agents-management` (planned) | binary in repo root, `.temp/` logs |
