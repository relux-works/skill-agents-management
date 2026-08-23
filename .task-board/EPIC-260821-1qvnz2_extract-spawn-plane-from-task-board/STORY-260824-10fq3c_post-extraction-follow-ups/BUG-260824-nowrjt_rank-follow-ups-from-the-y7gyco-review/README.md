# BUG-260824-nowrjt: rank-follow-ups-from-the-y7gyco-review

## Description
Three non-blocking findings from the accepted TASK-260824-y7gyco review. 1: checkRecommendations is unproven past a rows first system - narrowing to model.Systems[:1] at vendor.go:700 survives the whole suite; zero live risk today (no multi-system rows exist, cross-runtime is a separate row) but the gate needs a negative test with a Recommended row declaring more than one system. 2: the ties documentation disagrees - CapabilityRank.Score doc says a tie states the two are equal, vendors/google/models.go describes its five ties differently; reconcile so one semantics is stated once. 3: see the review verdict artifact for the third item and full detail.

## Scope
(define bug scope / affected area)

## Acceptance Criteria
(define fix acceptance criteria)
