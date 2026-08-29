# STORY-260830-o827bx: reconcile-and-release-pi-qwen-mlx-on-current-trunk

## Description
Reapply the independently accepted Pi, Qwen, MLX, generic inference-engine, and registry-backed provenance candidate onto the current v0.4.3 protected trunk, reconciling overlapping refusal-proof graph and documentation changes without weakening either contract.

## Scope
(define story scope)

## Acceptance Criteria
Fresh Story selected base equals fetched origin/main; accepted revision-3 behavior is preserved; current v0.4.3 plugin graph and refusal-proof matrix remain authoritative; overlaps are semantically reconciled rather than overwritten; core dispatch stays identity-invariant and free of model-specific branches; all static/fake negative, race, vet, regress, build, formatting, and no-live-runtime gates pass; independent review accepts the exact current-trunk CR before PR delivery and immutable v0.5.0 tagging.
