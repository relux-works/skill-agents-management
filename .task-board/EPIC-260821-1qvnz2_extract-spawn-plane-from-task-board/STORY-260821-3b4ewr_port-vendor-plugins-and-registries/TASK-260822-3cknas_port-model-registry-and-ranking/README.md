# TASK-260822-3cknas: port-model-registry-and-ranking

## Description
Port the model layer from the source's internal/spawn/models.go and pkg/remoteconfig/reasoningeffort into vendor plugins: anthropic, openai, alibaba, google - each vendor's model rows with capability rank (the source's PolicyRank evidence carries over as rank basis), per-model reasoning-effort vocabularies and recommended efforts, usage descriptions (the source rows lack them - WRITE them from the models' documented positioning, marked as authored-here so nobody mistakes them for ported facts), and the agentic systems that can drive each model. The 43-row registry count and every model-runtime binding must match the source exactly; admitted-pair digests over real configs are the release gate exactly as they were in the source epic.

## Scope
(define task scope)

## Acceptance Criteria
1. Every source model row ported: same IDs, same runtime bindings, same effort vocabularies and recommendations; a count-and-binding test pins the full set against the source table. 2. Capability ranks carry the source's evidence as basis; no rank without observation. 3. Usage descriptions present for every model and marked authored-here. 4. Admitted-pair digests byte-stable over the source repo's own task-board.config.json (capture the digest from the source binary, pin it here). 5. Guard: vendor bindings in one file per vendor.
