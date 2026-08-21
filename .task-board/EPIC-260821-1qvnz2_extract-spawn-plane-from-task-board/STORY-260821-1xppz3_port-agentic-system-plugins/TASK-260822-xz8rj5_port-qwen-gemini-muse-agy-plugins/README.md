# TASK-260822-xz8rj5: port-qwen-gemini-muse-agy-plugins

## Description
Port the four simpler prompt-mode systems: qwen-code (with its CLAUDECODE and codex-family env strips - the leak fixed in the source must stay fixed), gemini-cli, muse, antigravity (agy encodes effort in the model ID suffix and rejects the effort flag; executable comes from AgyRuntime preflight with a placeholder fallback - port that honestly). Four plugins, one task, because they share the simple shape; if any of them turns out not to, stop and say so rather than forcing it into the batch.

## Scope
(define task scope)

## Acceptance Criteria
1. Plans byte-match each system's goldens. 2. Qwen env strips preserved - the source's fixed leak stays fixed, its open leaks stay open and named. 3. Agy's effort-in-model-ID and preflight-resolved binary ported honestly, including the no-side-effect dry-run behaviour. 4. Guard sees one binding file per system; each plugin registered through the public API only.
