# STORY-260824-2x7gmp: single-source-model-facts

## Description
Eliminate the dual source of truth for model rows, per the owner directive of 2026-08-24. Today task-board keeps a hybrid modelRegistrations table (PolicyRank score, Lifecycle, SupersededBy, Description, Recommended flag, ContextWindowTokens, Pricing, and muse-only local effort axes) beside the module vendor rows, with an init-panic keeping the two aligned. Every fact that is a MODEL or VENDOR fact moves here: rank SCORES with genuine ties (the module CapabilityRank is position-based and tie-free - it must grow a score representation that preserves the board ties, since ranking consumers observe them), lifecycle and supersession, context window, pricing, recommended flag, and the muse rows effort axes under the vendor-unresolved shape. Descriptions become single-sourced too: the owner accepted that consumer projections change to the module texts (the digests are unaffected - they hash admitted pairs, not prose). What stays in the board is POLICY, not model facts: the frozen v2 admission tier table and ceilings. Done when the board modelRegistrations table is deleted and buildRegisteredModels maps module rows alone, with projection changes limited to description text and proven so by capture diff.

## Scope
(define story scope)

## Acceptance Criteria
(define acceptance criteria)
