# TASK-260822-3u97y3: port-claude-plugin

## Description
Port the claude-code agentic system: prompt mode and goal mode, stdin transport, the CLAUDECODE/session env contract, binary resolution. Goal-mode launch preparation is a side effect the source's parity harness captures - it must appear in the plan or be explicitly out of plan with the reason recorded.

## Scope
(define task scope)

## Acceptance Criteria
1. Plans byte-match the claude goldens for both modes. 2. Env contract ported exactly. 3. Goal-mode preparation either in the plan surface or explicitly documented out with the source's behaviour referenced. 4. Guard sees one binding file.
