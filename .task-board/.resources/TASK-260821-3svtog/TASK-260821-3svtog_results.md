# TASK-260821-3svtog — scaffold module, CLI and Makefile

Work is **uncommitted** in the story worktree
`.temp/STORY-260821-224xfu/worktree` (branch `task-board/story/STORY-260821-224xfu`),
for the Change Request snapshot.

## What landed

| Path | What it is |
| --- | --- |
| `go.mod`, `go.sum` | one root module, `github.com/relux-works/skill-agents-management`, go 1.25.5, cobra v1.10.2 (matching the extraction source) |
| `Makefile` | `build`, `test`, `vet`, `install`, `clean`; `-mod=mod` on every Go call |
| `tools/agents-management/main.go` | `main` package, `cmd.Execute()` — mirrors `tools/board-cli/main.go` |
| `tools/agents-management/cmd/root.go` | root command, ldflags-injected `Version`/`Commit`/`BuildDate`, `formatVersion` |
| `tools/agents-management/cmd/version.go` | `version` subcommand |
| `tools/agents-management/cmd/plugins.go` | `plugins [--json]`, the compiled-in plugin list |
| `tools/agents-management/cmd/*_test.go` | unit tests plus build/exec integration tests |
| `pkg/.gitkeep` | the seam for shared packages; deliberately no packages yet |
| `task-board.config.json` | landing gate: `validation.commands` replaced with the real list |
| `README.md` | Tools table, Build section, current CLI surface |
| `.gitignore` | build-output patterns anchored (see Findings) |

## Layout decision: one root module, not module-per-tool

The extraction source gives every tool and every `pkg/` package its own
`go.mod`, wired together with `replace` directives. That split earns its keep
there because `pkg/board`, `pkg/remoteconfig` and `pkg/providerlimits` are
consumed independently and must not drag the CLI's dependency set along.

Here `pkg/` is empty by design until the contract tasks define what a plugin
is, so a multi-module layout today would be `replace` bookkeeping around
nothing. One root module keeps `go test ./...` and `go vet ./...` honest across
the whole repository from a single directory, and the split stays available
later — moving a `pkg/` subtree into its own module is additive.

The task brief's suggested gate command `cd tools/agents-management && go test
./...` was adjusted accordingly: the module root is the repository root, so the
gate runs `go test ./...` from there and covers `pkg/` for free as it fills in.

## `make install` copies rather than symlinks

The task description says symlink, the spawn brief says "symlink or copy".
Copy, because `make clean` removes the build output and `~/.local/bin` would be
left holding a dangling symlink; a copy keeps the installed binary working
until it is deliberately replaced. This matches the extraction source's
`_install-cli`.

## Landing gate

`spawn.worktree_isolation.validation.commands` went from `[]` to:

```
make vet
make build
env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1
```

`TASK_BOARD_DIR` is unset for the suite so no test can silently bind to the
developer's board. The "empty by decision" framing survives only in commit
6223c28's message, which correctly describes what was true when it was
written; no prose in `README.md`, `docs/` or `AGENTS.md` carried it.

## Findings

**`.gitignore` was ignoring the entire CLI source tree.** The pre-existing
`agents-management` pattern has no slash, so git applied it to directories as
well as files and matched `tools/agents-management/` itself — every file this
task created was invisible to git. `make build`, `make vet` and the suite all
passed against files that would never have reached a commit; the Change
Request would have landed empty. Both build-output patterns are now anchored
(`/agents-management`, `/tools/agents-management/agents-management`) and
`TestBuildOutputIsIgnoredAndSourcesAreNot` asserts both directions.

## Gates

Run standalone from a clean build state (`make clean` first), real exit codes:

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `make test` | 0 |
| `make install` | 0 |
| `gofmt -l tools/` | clean (no output) |

`~/.local/bin/agents-management version` →
`agents-management version 6223c28 (commit 6223c28, built 2026-08-21T14:04:26Z)`
(`VERSION` falls back to the commit hash because the repository has no tags yet).
`~/.local/bin/agents-management plugins --json` → `[]`, exit 0.

## Negative evidence

Eight mutants applied one at a time, each reverted byte-for-byte; every one
turned its gate RED, baseline green afterwards. Harness:
`.temp/TASK-260821-3svtog/mutants.py`, full output:
`.temp/TASK-260821-3svtog/negative-evidence.md`.

| Mutant | Gate that caught it |
| --- | --- |
| M1 `-X` package path drift in the Makefile | `TestMakeBuildInjectsVersionMetadata` |
| M2 `-ldflags` dropped from `make build` | `TestMakeBuildInjectsVersionMetadata` |
| M3 `formatVersion` drops injected commit/date | version tests, unit + built binary |
| M4 version output moved to stderr | version tests |
| M5 empty plugin registry returns an error | `TestPluginsCommandEmptyListSucceeds`, `TestBuiltBinaryListsEmptyPluginsWithoutError` |
| M6 `pluginNames` returns the registry slice (nil → JSON `null`) | empty-array + aliasing tests |
| M7 `.gitignore` anchor dropped | `TestBuildOutputIsIgnoredAndSourcesAreNot` |
| M8 build-output ignore rules deleted | `TestBuildOutputIsIgnoredAndSourcesAreNot` |

M1/M2 are the ones that matter most: the Go linker silently ignores an `-X`
target naming a package or variable that does not exist, so a broken ldflags
binding produces no error anywhere. The only way to observe it is to run the
real `make build` and read the binary's output, which is what the test does —
it invokes `make build` with sentinel `VERSION`/`COMMIT`/`BUILD_DATE` rather
than duplicating the ldflags string, so it binds to the Makefile the
repository actually ships.

`TestBuildWithoutLdflagsReportsDefaults` is the narrowing partner: a plain
`go build` must report `dev`, so hardcoding the sentinels into the source
cannot satisfy the injection test.

An earlier M7 was written as a glob change (`tools/*/agents-management`) and
came back GREEN — that mutant did not reproduce the defect, since a
three-component glob never matches the two-component source directory. It was
corrected to drop the anchor, which is the actual bug, and then went RED.

## Deliberately not built

No plugin interfaces, registry types or runtime declarations — those are the
two contract tasks. `registeredPlugins` is a package-level `[]string` with no
type around it, so the contract tasks are free to define the real shape
without unwinding a guess made here.
