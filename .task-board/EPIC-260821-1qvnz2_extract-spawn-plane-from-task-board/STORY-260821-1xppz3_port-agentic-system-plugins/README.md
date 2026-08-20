# STORY-260821-1xppz3: port-agentic-system-plugins

## Description
Move the six agentic-system adapters (claude-code, codex, qwen-code, gemini-cli, antigravity, muse) out of task-board internal/spawn into plugins here, without behaviour change. Parity proven as the OBSERVABLE SURFACE per (system, mode): argv, environment added/removed, stdin bytes, side effects such as goal launch preparation - captured before and after, diffed, exclusions named. Env filtering contracts carry over exactly (the qwen CLAUDECODE strip, the codex family strips); the known open leaks stay open and tracked, not silently fixed or reopened.

## Scope
(define story scope)

## Acceptance Criteria
(define acceptance criteria)
