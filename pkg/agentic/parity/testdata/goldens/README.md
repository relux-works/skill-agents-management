# Launch-surface goldens

These fixtures are the parity contract every agentic-system plugin port in this
repository proves itself against. They were captured by the **extraction
source's own harness**, not by anything in this repository.

- Source repo: `skill-project-management`
- Source commit: `ed4878123061b39fdae67160f6b5632117b48a2f`
- Source harness: `tools/board-cli/internal/spawn/parity_capture_test.go`,
  `TestCaptureLaunchSurface`, skipped unless `SPAWN_PARITY_CAPTURE_OUT` is set
- Captured under: `TASK-260822-slgewd`

Each file records that provenance inside itself. A golden whose provenance is
unknown cannot settle a dispute about what the source actually did — it becomes
a number somebody once believed.

## Why the source captures them and this repository only compares

A golden captured by code that lives next to the port proves only that the new
code agrees with itself. The source's harness produced the parity evidence for
the source's own adapter refactor (`TASK-260817-1v9v95`), and that provenance is
the whole reason these files are worth anything. Nothing in `pkg/agentic/parity`
builds a command, resolves a binary or filters an environment, and nothing in it
may start to.

## Regenerating

```sh
.scripts/capture-parity-goldens.sh [--source /path/to/skill-project-management]
```

The script runs the source harness under a **pinned, synthetic parent
environment** and hands the result to `TestWriteGoldensFromCapture`, which
applies this repository's masking and writes these files. It refuses to run
against a dirty source checkout: a golden recording a commit that does not
describe the code that produced it is worse than one recording nothing.

The source checkout is read-only in that flow. The only thing the script does
inside it is run one `go test`.

## What is captured

Fourteen combinations, one file per `(system, case)`:

| System | Case | Launch mode | Stdin | Notes |
|---|---|---|---|---|
| claude | `prompt-mode` | exec | bytes | the assignment prompt on stdin |
| claude | `goal-mode` | exec | none | goal text is an argv argument, not stdin |
| codex | `exec-default-path` | exec | bytes | binary resolved from `PATH` |
| codex | `exec-managed-npm-path` | exec | bytes | binary resolved from the managed npm package |
| codex | `exec-native-shim` | exec | bytes | npm shim unwrapped to the native binary |
| codex | `dry-run` | dry-run | — | |
| qwen | `exec` | exec | bytes | stdin is a two-line stream-json control protocol |
| qwen | `dry-run` | dry-run | — | |
| muse | `exec` | exec | none | prompt passed as `--prompt-file` |
| muse | `dry-run` | dry-run | — | |
| gemini | `exec` | exec | bytes | |
| gemini | `dry-run` | dry-run | — | |
| agy | `exec` | exec | none | |
| agy | `dry-run` | dry-run | — | binary is the literal `agy` placeholder, see below |

## What the source could NOT capture

These are the source's own stated reasons, from its `TASK-260817-1v9v95` results
artifact. A port touching one of these surfaces has **no golden** and must find
other evidence; it must not read the absence as permission.

### 1. The managed-session-args surface (the source's "Site 3")

> the managed-session-args (Site 3) surface lives in package `cmd`, and
> pre-refactor it had no `spawn.CodexArgs` to call from `package spawn`'s test.

`TestCaptureLaunchSurface` lives in `package spawn` and can only reach what that
package exports. The source gave that surface its own proof instead:
`TestManagedCodexSpawnArgsMatchesTheHistoricalConstruction` in
`tools/board-cli/cmd/codex_managed_session_args_parity_test.go`, which compares
the new call against the frozen original `managedCodexSpawnArgs` body across a
range of configs.

**Consequence here**: `agentic.LaunchModeManagedSession` has **no golden at
all**. The contract declares the mode; the goldens say nothing about it. A port
declaring `LaunchModeManagedSession` needs its own frozen-reference proof, in
the source's style, and should say so where its capability is declared.

### 2. The interactive-manager-client argv passthrough

> The interactive-manager-client argv passthrough path (`cmd/codex_manager.go`,
> `cmd/claude_manager.go`) is a fourth, structurally different surface (filters
> user-typed argv rather than constructing it) not in scope for this task's
> three named sites.

It filters argv a human typed rather than constructing argv from a config, so
the capture harness — which builds a command from a `*spawn.Config` — has no way
to express it. Nothing in these goldens covers it.

## What the goldens do not prove about the combinations they DO cover

### PATH content is not compared

`path-env-excluded` is ported verbatim from the source harness's `diffEnv`. PATH
is seeded with a `t.TempDir` so binary resolution is hermetic, so its value
differs run to run for reasons unrelated to any port.

The source recorded the cost of this exclusion explicitly rather than leaving it
implied: qwen's child PATH **value** changed in the refactor these captures
proved — `filterQwenRuntimeEnv` routes through `filterCodexRuntimeEnv` →
`sanitizeCodexPath`, which strips `.codex/tmp/arg0`-style shim entries — and its
own harness could not see it.

**A port that changes what it strips from PATH is outside what these goldens
prove.** That needs its own test.

### Dry-run goldens prove the binary and the argv, and nothing else

The source's dry-run capture path calls `BuildArgs`, which returns a binary and
an argv and never constructs a command. There is no environment and no stdin to
observe, so a dry-run golden carries neither. `FromPlan` mirrors that: for
`LaunchModeDryRun` it records the `n/a-dry-run` marker and leaves both
environment lists empty rather than reporting an absence that was never
measured.

### `agy/dry-run` records the placeholder binary on purpose

Its binary is the literal string `agy`, not a resolved path. The source's
`BuildArgs` resolves Antigravity through `cfg.AgyRuntime.Executable` when a
preflight has already populated it and falls back to the placeholder when it has
not — because `BuildArgs` promises no side effects and must never itself trigger
the preflight. The dry-run capture supplies no `AgyRuntime`, so the placeholder
is the correct captured value.

### The parent environment is pinned and synthetic

Every golden records the exact parent environment its capture ran under, in
`capture.parent_env`. The source harness diffs the child environment against
`os.Environ()`, so an unpinned capture would record whichever session variables
the operator's shell happened to carry — and a key the plugin strips would be
provably stripped only on the machine that happened to set it.

The pinned set seeds the keys the source's filters act on
(`filterCodexRuntimeEnv`, `filterQwenRuntimeEnv`, `withSpawnEnv`) so each strip
and each replacement is observable. **A key that is not in `parent_env` cannot
appear in `env_removed`**: the goldens prove the strips they seed and are silent
about any other.

#### The bystander keys, and why a capture must keep them

Seeding only the keys a filter strips proves the filter's **lower** bound — it
removes at least these — and says nothing about its **upper** bound. `qwen` is
where that gap bites. `filterQwenRuntimeEnv` is `filterCodexRuntimeEnv` plus
`CLAUDECODE`, which between them strip every other key in the pinned set, so
without a surviving key `qwen/exec`'s `env_removed` covers 100% of its
`parent_env` — and a port whose `ChildEnv` **discards the whole parent
environment** and returns only its injections produces a byte-identical diff.
The shape of such a golden actively invites that defect: a port author reading
"everything removed" writes "return only the injections" and passes.

Four keys in `PINNED_ENV` exist to close that, and **a recapture that drops them
silently reverts the fix**:

| Key | What it pins |
|---|---|
| `PARITY_BYSTANDER` | a plain unrelated key every system must pass through untouched |
| `CODEX_LIKE_BUT_NOT` | the `CODEX_*` strip is an exact key set, not a prefix match |
| `CLAUDECODE_LIKE_BUT_NOT` | the `CLAUDECODE` strip is an exact key, not a prefix match |
| `TASK_BOARD_LIKE_BUT_NOT` | `withSpawnEnv`'s `appendOrReplaceEnv` replaces exact keys, not a prefix |

The exact-key claim is the source's: `filterEnvKeys` builds a blocked-key map
(`spawn.go:998`) and `appendOrReplaceEnv` matches whole keys, so a port reaching
for `strings.HasPrefix("CODEX_")` is a plausible defect that only a near-miss
catches.

Four tests hold this, and they fail in this order:
`TestEveryGoldenSeedsTheBystanderKeys` (the convention — every fixture seeds all
four, and none is stripped or rewritten), `TestQwenExecLeavesSomethingToPreserve`
(the consequence, on the fixture where it was actually violated),
`TestAWholeEnvironmentWipeFailsAgainstQwenExec` (a port that discards the parent
environment entirely), and `TestAPrefixStripFailsAgainstQwenExec` (a port that
strips the right families by prefix instead of by key). Both attacks are driven
through the real `agentic.BuildPlan`, and both carry a narrowing subtest that
takes the relevant bystanders back out of the recorded parent environment and
shows the same defect walking through — so the four keys are evidence rather
than decoration.

`capture.parent_env_omitted` names the four variables the capture process needed
that are not recorded, each with its reason. All four are pass-through — no
filter under capture touches them — so omitting them changes no diff, while
keeping them would write an operator's home directory into a fixture.

### Dry-run binary resolution is pinned, and differs from the source's artifact

The source's `parity-after.json` recorded, for the dry-run cases, whatever its
operator's machine happened to have installed: `codex/dry-run` resolved a real
managed-npm path, and `qwen/dry-run` and `muse/dry-run` recorded
`exec: "qwen": executable file not found in $PATH`.

This capture pins `PATH` to a directory of stub executables instead, so the
dry-run goldens record a *successful* resolution that lands on the stub. The
deviation is deliberate: a fixture whose binary field is "whatever this
developer installed" cannot be compared on another machine, and an `error`
fixture pins a refusal rather than an argv — the dry-run argv grammar, which is
the thing the ports have to reproduce, would not be captured at all.

One consequence to read precisely: `codex/dry-run` pins the **PATH-resolution
branch** of the source's `resolveCodexBinary`, because `CODEX_MANAGED_PACKAGE_ROOT`
is pinned to a directory that does not exist. The managed-npm branch and the
native-shim branch are pinned by `codex/exec-managed-npm-path` and
`codex/exec-native-shim`, which build their layouts under `t.TempDir` and do not
depend on the operator's machine.

### The capture is `darwin/arm64`

`codex/exec-managed-npm-path` and `codex/exec-native-shim` embed the platform
package and target triple the source's `codexPlatformPackage()` resolved
(`@openai/codex-darwin-arm64`, `aarch64-apple-darwin`). On another platform the
source's own harness skips those cases. A port proving itself against them on a
different platform will differ in the binary field for a reason that is not a
defect.

## Masking

Three rules, frozen in `mask.go` and pinned by `TestMaskRuleSetIsFrozen` and
`TestMaskingCoversExactlyTheDeclaredFields`:

| Rule | What it hides |
|---|---|
| `capture-temp-slot` | `t.TempDir` paths → `<TMPDIR:N>`, N being allocation order |
| `capture-stub-bin-dir` | the PATH-seeded stub directory → `<PARITY-BIN>` |
| `path-env-excluded` | the `PATH` entry, dropped from the environment diff entirely |

`capture.placeholders` in each file lists the placeholders that file actually
uses, so a port test knows how many temp directories to allocate without
grepping the fixture.

Masking is where a real difference can be made invisible while the suite stays
green. Adding a rule costs a deliberate edit to a frozen literal plus a `Why`
sentence, and `TestEveryMaskRuleIsNecessary` narrows one rule at a time to show
which fixture each one carries.

## Why these live under `testdata/`

The single-source guard's scan scope excludes `testdata/`
(`pkg/agentic/singlesource_scanscope_test.go`), so nothing here is scanned for a
reintroduced system binding.

That is **correct for these fixtures** and it is stated rather than assumed: they
are JSON captured by another repository, they declare no binding, and the guard
reports on what this module's Go build compiles. The rule it costs is the general
one — **a `.go` file placed under this directory would be invisible to the
guard**, so no Go source belongs here. The harness that reads these files lives
one directory up, in `pkg/agentic/parity`, where the guard does scan it.
