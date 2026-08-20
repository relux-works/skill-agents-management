# EPIC-260821-1qvnz2: extract-spawn-plane-from-task-board

## Description
Extract the agentic-system spawn plane from skill-project-management into this repository behind a two-layer plugin architecture: agentic-system plugins (claude-code, codex, qwen-code, gemini-cli, antigravity, muse - the harness that runs a turn) and vendor plugins (anthropic, openai, alibaba, google - who owns models, auth and quota), where vendor plugins depend on agentic-system plugins and declare which systems they support. Current scope is ONLY the vendors and systems that already exist in task-board, moved without behaviour change: same observable launch surface (argv, environment, stdin bytes, side effects), byte-stable admitted-pair digests over real configs, unchanged on-disk limit-state identity demonstrated on real state files, and task-board consuming the tool with its own spawn surface observably unchanged. Boundaries: roles, tasks, Change Requests, review routing and worktree isolation STAY in task-board; this tool owns runtime registry, adapter table, model registries and ranking, effort vocabularies, limit plane, health checks and launch. The local-model resource plane (loaded/busy/memory-aware sub-plugins) is documented in docs/architecture.md as design only, not built in this scope. See docs/architecture.md for the contract.

## Scope
(define epic scope)

## Acceptance Criteria
(define acceptance criteria)
