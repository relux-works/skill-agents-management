# TASK-260822-hp5fb4: port-codex-plugin

## Description
Port the codex agentic system - the hardest one: three binary-resolution paths (managed-npm, native shim, PATH), CodexArgs for both exec and managed-session modes, the -c override grammar for effort and service tier, and the codex env-filtering family. The source collapsed three argv construction sites into one CodexArgs after a three-way drift; that unification must survive the port as ONE construction site, and the module guard must see the plugin's binding as legal in exactly one file.

## Scope
(define task scope)

## Acceptance Criteria
1. Plans byte-match the codex goldens for every captured (mode, resolution-path) combination. 2. All three binary-resolution paths ported and tested hermetically with stub layouts, as the source tests do. 3. One argv construction site; the guard accepts exactly one binding file. 4. Env filtering carries the source contract exactly - the strip families ported, with the open leaks of the source (BUG-260819-3qn52o) staying open and named, not silently fixed or reopened.
