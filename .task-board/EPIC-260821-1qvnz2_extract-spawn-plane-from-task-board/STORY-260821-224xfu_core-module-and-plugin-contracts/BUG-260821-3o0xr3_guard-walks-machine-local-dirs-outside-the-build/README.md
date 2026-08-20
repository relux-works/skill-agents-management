# BUG-260821-3o0xr3: guard-walks-machine-local-dirs-outside-the-build

## Description
The single-source guard walks every directory under the module root, including directories the Go toolchain itself excludes from the build: dot-directories (.agents - the machine-local agents-infra runtime, gitignored but on disk in the main checkout), underscore-directories, and nested modules with their own go.mod. On a bootstrapped developer checkout the guard reports an id-switch violation at .agents/tools/agents-infra/.../child_launch_composition.go and the suite goes red, while worktrees and scratch copies stay green because .agents exists only where agents-infra installed it. The guard must mirror go build exclusion semantics: skip dot-dirs, underscore-dirs, and any subtree with its own go.mod; testdata stays scanned or its exclusion is stated in the threat model. Needs a fixture test with a planted violation inside a dot-dir (must NOT be reported) and inside a normal dir (must be reported), so the exclusion cannot silently widen.

## Scope
(define bug scope / affected area)

## Acceptance Criteria
1. Guard skips dot-dirs, underscore-dirs and nested go.mod subtrees, mirroring go build exclusion semantics; testdata decision made explicitly and documented in the threat model. 2. Fixture proves both directions: a planted violation inside a dot-dir is NOT reported, the same violation in a normal dir IS reported; nested-go.mod case likewise. 3. The original failure shape (an id-comparison under .agents/) reproduced in scratch and confirmed silent, suite green with it present. 4. No rule, residual or mutant weakened: the full 58-mutant matrix stays green.
