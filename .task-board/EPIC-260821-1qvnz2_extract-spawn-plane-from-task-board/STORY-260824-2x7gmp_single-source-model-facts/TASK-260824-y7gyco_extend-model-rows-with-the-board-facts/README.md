# TASK-260824-y7gyco: extend-model-rows-with-the-board-facts

## Description
Module half: extend the vendor Model row to carry every fact the board's modelRegistrations still owns - rank SCORE (CapabilityRank grows a score the ranking consumers can observe, preserving the board's genuine ties: haiku and its dated snapshot, the two gemini rows under one score; position stays derived and tie-broken only where a consumer needs a total order), Lifecycle, SupersededBy, Recommended, ContextWindowTokens, Pricing, and the muse rows' effort axes represented under the vendor-unresolved runtime shape. Port the 43 rows' values from the board table at its current trunk, pinned by a full-set test against a frozen fixture of that table (so a transcription slip cannot pass), keeping every existing module pin green (digest, full-set, registration refusals). Tag v0.2.0 when green.

## Scope
(define task scope)

## Acceptance Criteria
1. Model row carries score-based rank preserving ties, lifecycle, supersession, recommended, context window, pricing; refusal semantics extended deliberately (a rank still requires Basis; new fields state their emptiness rules). 2. All 43 rows' values ported and pinned against a frozen fixture of the board table; muse effort axes live under vendor-unresolved. 3. Existing pins stay green: digests byte-stable, full-set count, F2, availability. 4. v0.2.0 tagged and pushed after green. 5. Work UNCOMMITTED until review; the tag only after acceptance.
