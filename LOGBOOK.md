# Flight Logbook

> Institutional memory. Concise, factual, high-signal.
> Newest entries first. One block per insight.

## 2026-08-21

### 1717 — git check-ignore is index-aware; a guard naming tracked files goes vacuous
- FINDING: `git check-ignore` never reports a TRACKED file as ignored, whatever `.gitignore` says. `--no-index` is the flag that defeats the index lookup and reports the pattern truth.
- ANOMALY: `TestBuildOutputIsIgnoredAndSourcesAreNot` named six source paths that are untracked only while a Change Request is in flight. On an UNTRACKED tree the anchor-drop mutant is RED; on the same tree with those files TRACKED it is GREEN. The guard against the 1706 defect would have expired at the moment the work it guards landed.
- NOTE: The failure class is a NEW file being silently swallowed, so a guard that only names files already in the index can never observe it. Assert at least one path that does not exist yet.
- FIX: `--no-index` on the `git check-ignore` invocation, `tools/agents-management/cmd/build_integration_test.go:236`; added the non-existent paths `tools/agents-management/cmd/future_command.go` and `pkg/agents-management/foo.go` to the not-ignored list.
- NOTE: The two halves of the fix are independently load-bearing against different mutants, not redundant. On a TRACKED tree with the anchor dropped: `--no-index` alone is RED, the non-existent paths alone are RED, neither alone is GREEN — but against a `pkg/agents-management/` ignore rule, `--no-index` with the old six-path list is GREEN and only the `pkg/` path catches it.
- SCOPE: `tools/agents-management/cmd/build_integration_test.go`. This file is the template later extraction tasks copy; the rationale is commented at the invocation so it is not simplified away.
- STATUS: Resolved (TASK-260821-3svtog, review rev1 blocking finding).

### 1706 — Unanchored .gitignore pattern hid the entire CLI source tree
- FINDING: `.gitignore:2` carried a bare `agents-management` pattern. Slash-free patterns match directories, so git ignored `tools/agents-management/` itself, not just the built binary.
- ANOMALY: Every gate stayed green against invisible files — `make build`, `make vet`, `go test ./...` all passed on a tree git would have carried as empty. Only `git status` showed nothing to add.
- FIX: Anchored both build-output patterns: `/agents-management` and `/tools/agents-management/agents-management` (`.gitignore:4-5`).
- SCOPE: `TestBuildOutputIsIgnoredAndSourcesAreNot` in `tools/agents-management/cmd/build_integration_test.go` asserts both directions — sources not ignored, binary ignored.
- NOTE: Caught before the first Change Request. Would have shipped a CR containing config and README changes only.
- STATUS: Resolved (TASK-260821-3svtog).

### 1705 — Go linker silently ignores a stale -X target
- FINDING: `go build -ldflags "-X <pkg>.Var=v"` naming a package or variable that does not exist produces no error and no warning; the binary just keeps its default.
- NOTE: Consequence — ldflags injection cannot be verified from inside `go test`, which is not built with those flags. The only observation point is running the real build and reading the binary's output.
- SCOPE: `TestMakeBuildInjectsVersionMetadata` invokes `make build` with sentinel VERSION/COMMIT/BUILD_DATE rather than duplicating the ldflags string, so it binds to the shipped Makefile. `TestBuildWithoutLdflagsReportsDefaults` is its narrowing partner against hardcoded sentinels.
- STATUS: Resolved (TASK-260821-3svtog).

### 1704 — One root Go module, not module-per-tool
- DECISION: Single module `github.com/relux-works/skill-agents-management` at the repository root; CLI `main` at `tools/agents-management`, shared packages to come under `pkg/`.
- NOTE: The extraction source (skill-project-management) splits every tool and every `pkg/` package into its own module wired by `replace`. That earns its keep there because `pkg/board`, `pkg/remoteconfig` and `pkg/providerlimits` have independent consumers that must not inherit the CLI's dependency set.
- NOTE: Here `pkg/` is empty until the plugin contracts exist, so the split would be `replace` bookkeeping around nothing. One module keeps `go vet ./...` and `go test ./...` honest repo-wide from one directory. Splitting later is additive.
- SCOPE: `go.mod`, `Makefile`, `spawn.worktree_isolation.validation.commands` in `task-board.config.json` (gate runs from the repo root, not from `tools/agents-management`).
- STATUS: Resolved (TASK-260821-3svtog).

### 1703 — make install copies rather than symlinks
- DECISION: `make install` copies the binary to `~/.local/bin/agents-management`.
- NOTE: A symlink into `tools/agents-management/agents-management` dangles the moment `make clean` runs. Matches the extraction source's `_install-cli`.
- STATUS: Resolved (TASK-260821-3svtog).
