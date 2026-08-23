# TASK-260823-4f5t1m: skill-docs-and-regress-harness

## Description
The closing task of the epic: SKILL.md for the new skill (what agents-management is, the CLI surface, the plugin model, how a consumer wires it), README and docs/architecture.md reconciled with what actually shipped across five integrated stories, and a regression harness. The docs must carry the honest outcomes: the description-divergence with task-board (two authors, one fact - state the owner), the sweep's deliberate keeps (exec ownership, preflights, goal preparation, composition validators without a cross-repo agreement check), the open leak pins, the CI arrangement from the switch story. Regress covers: a plugin registration refusal naming both ids, a runtime declaration and its F2 collision, an availability verdict from a real state file, and a BuildPlan parity smoke against one golden per layer-1 system.

## Scope
(define task scope)

## Acceptance Criteria
1. SKILL.md exists and follows the house skill conventions; an agent reading only it can wire and call the CLI. 2. README/architecture reconciled with shipped reality, including the local-model design-only section staying design-only. 3. The honest-outcomes list above present with owners. 4. Regress harness runs green in make regress or equivalent, covering the four named classes, each mutation-checked. 5. Suite green; work UNCOMMITTED.
