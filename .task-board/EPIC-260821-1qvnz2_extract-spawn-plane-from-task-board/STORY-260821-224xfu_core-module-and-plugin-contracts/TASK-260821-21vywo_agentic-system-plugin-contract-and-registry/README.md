# TASK-260821-21vywo: agentic-system-plugin-contract-and-registry

## Description
The Layer-1 contract: an AgenticSystem plugin interface declaring binary resolution, argv construction per launch mode, environment filtering, stdin transport, reasoning-effort transport (argv/stdin/none), goal/budget/service-tier support, composition grammar, default home and auth hint - the shape proven by task-board's adapterTable. A registry keyed by system ID, registration-only (no switch statements over IDs anywhere outside the registry), with an ast-based single-source guard that FAILS if a second binding table or an ID-switch appears - the guard class that survived a shadow table in the source repo and had to be rewritten; start from the go/ast form. No concrete plugins in this task: one test double proving that registering a fake system exercises every dispatch surface.

## Scope
(define task scope)

## Acceptance Criteria
1. Interface covers every field task-board's adapterTable declares today. 2. Registry is the only place a binding may live; the ast guard demonstrably fails on a planted second table AND on a planted ID-switch. 3. A test-double system registers and drives every dispatch surface without touching core code. 4. Effort transport none is representable and a required-effort model under it is refusable at the contract level.
