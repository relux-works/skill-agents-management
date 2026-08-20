# STORY-260821-224xfu: core-module-and-plugin-contracts

## Description
The Go module, the agents-management CLI skeleton, and the two plugin contracts: the agentic-system plugin interface (binary resolution, argv per launch mode, env filtering, stdin transport, effort transport, goal/budget/tier support, composition grammar, default home, auth hint - mirroring the adapter table proven in the extraction source) and the vendor plugin interface (models with ranking, per-model description and effort vocabulary, availability as a STRUCTURED verdict not a boolean so the local-model plane fits later, spawn with the full parameter surface). Vendor plugins declare which agentic systems they support; the registry enforces that dependency direction. Single-source guards: a second binding table anywhere fails a test.

## Scope
(define story scope)

## Acceptance Criteria
(define acceptance criteria)
