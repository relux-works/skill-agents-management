# TASK-260821-3svtog: scaffold-module-cli-and-makefile

## Description
The Go module, the agents-management CLI entry point, the Makefile (build, test, vet, install), and the tightened landing gate. Layout mirrors the extraction source where it earns it: a CLI under tools/agents-management and shared packages under pkg/, module path github.com/relux-works/skill-agents-management. The CLI needs only a version command and a plugins command stub that lists registered plugins (empty is a valid answer) - the point is the seams, not features. Acceptance: make build, make test, make vet green from a clean checkout; make install symlinks the binary into ~/.local/bin; spawn.worktree_isolation.validation.commands in task-board.config.json replaced from the recorded-empty decision to the real command list in the SAME change; README Tools table updated to match reality.

## Scope
(define task scope)

## Acceptance Criteria
1. make build, test, vet green from clean checkout. 2. make install places agents-management into ~/.local/bin. 3. validation.commands carries the real gate and the empty-list decision note is gone. 4. CLI prints version and an empty plugin list without error.
