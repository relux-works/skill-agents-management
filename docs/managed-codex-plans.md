# Managed Codex plan inputs

`agentic.BuildPlan(registry, request, agentic.LaunchModeInteractive)` accepts
three optional semantic inputs through the Codex plugin. These are direct
BuildPlan primary/client inputs, intentionally absent from the worker-oriented
`vendorplugin.SpawnRequest` / `BuildLaunch` API; that API's field inventory
records their manager ownership explicitly. Plans remain values:
the module does not launch a client, contact an app-server, install manager
tools, create runtime directories, or inspect a board.

## Exact visible-client target

```go
request.CodexRemoteClient = &agentic.CodexRemoteClientTarget{
    Endpoint: "unix:///tmp/manager.sock",
    ThreadID: ownedNativeThreadID,
}
request.Home = privateCodexHome
plan, err := agentic.BuildPlan(registry, request, agentic.LaunchModeInteractive)
```

The resulting argv is `--remote ENDPOINT resume THREAD_ID`, followed by the
validated display suffix. It never chooses `--last`, a picker, or a new thread.
`ThreadID` must be a UUID, without whitespace; the caller must obtain it from
the manager-owned native thread. Syntax validation cannot prove existence,
ownership, visible history, or that an app-server honors the selector.
Endpoints accept absolute `unix:///path` or `ws://host:port` / `wss://host:port`
addresses without userinfo, query, fragment, or whitespace. `Home` must be a
clean absolute private home and is pinned in the child as `CODEX_HOME`.

The existing BuildPlan model/effort requirements still apply to the request's
identity metadata. The remote client restores execution settings from the
selected thread; it emits no model, sandbox, or approval flags automatically.
`NativeArgs` may carry `--no-alt-screen` and string-valued `-c KEY=VALUE`,
`--config KEY=VALUE`, or `--config=KEY=VALUE` overrides for `model`,
`model_reasoning_effort`, and `features.sleep_tool.mode`. Each config token must
contain exactly that key. Every other suffix, including competing resume
selectors and permission overrides, refuses. Supply any permitted suffix before
BuildPlan; do not compose provider grammar after planning.

Remote targets are interactive-only. They cannot accompany PermissionMode,
Curator/context descriptors, composition, local-provider selection, network
scope, profile, service tier, or additional writable roots. The primary's thread
policy remains authoritative. Nil targets keep the ordinary interactive plan.

## Manager executable directory

```go
managerBin := "/opt/manager/bin"
request.ManagerToolDir = &managerBin
```

ChildEnv prepends this directory to the **sanitized** inherited PATH, collapsing
all PATH entries to one. Both `task-board` and `tb-sessiond` therefore resolve
there first when installed there. Empty inherited PATH produces just that
directory. A present directory must be clean and absolute, with no PATH-list
separator, NUL, CR/LF, or invalid UTF-8. Nil leaves the old PATH behavior intact.
This changes child tool lookup; provider binary resolution still uses the
original request environment. The caller owns installation, executable identity,
and directory trust; the module does not attest file bytes or prevent later
replacement of executables.

## Additional runtime writable roots

```go
request.AdditionalWritableRoots = []string{
    "/project/.temp/spawn-runs",
}
// A primary app-server can be selected before planning:
request.NativeArgs = []string{"app-server"}
```

For an interactive primary, the plugin renders exactly the ordered list as one
`-c sandbox_workspace_write.writable_roots=[...]` override. It adds no parent,
home, control-root, or guessed runtime directory. Quoting preserves literal path
bytes. Each path must be clean, absolute, valid UTF-8 and free of NUL/CR/LF;
no expansion, cleaning, existence checks, deduplication, or symlink resolution
occurs. Empty lists add no override and preserve existing behavior.

The caller selects minimal directories and engages workspace-write in its
permission policy. This field configures that sandbox's additional roots; it
does not remove the normal workspace's write access, activate sandboxing on its
own, or prove operating-system enforcement. An explicit yolo posture refuses.
Other launch modes and remote clients refuse roots. NativeArgs must be empty
or exactly `app-server`, so a second configuration writer cannot replace the
list or change sandbox policy through this suffix. Curator context permission
conflict checks still apply. Native Codex enforcement and real spawn writes,
including denied sibling writes, must be validated by the consuming host.

## Refusals, finalization, and seals

Malformed, conflicting or unsupported managed inputs unwrap to
`agentic.ErrManagedLaunchInvalid`. Validation runs before request preparation
and provider binary resolution. Plugins without `ManagedLaunchPlanner` refuse
present inputs rather than ignoring them. The Codex plugin rechecks its argv
inputs for callers holding the plugin directly.

Any present managed input produces a seal binding binary and exact argv. PATH
is bound when ManagerToolDir is present; CODEX_HOME is bound for remote clients.
Owned selectors reject changed, removed or duplicate entries. Catalog artifact
and TMPDIR guards remain active underneath the managed guard. Exported env
selectors are keyed commitments, never literal values. Base guards use the
process-local key, matching the existing Codex TMPDIR contract. For portable
process verification, call `FinalizePlan` with an explicit commitment key and
supply that key separately to `ImportSeal`.

FinalizePlan permits unrelated fragment/prompt env overlays, but refuses
replacement of the managed PATH/private home, native tails, and new-session
reservations. All provider arguments must be present in the initial request.
Finalized guards bind the full final process using the existing wire schema.
Immediately before Start, consumers must call `plan.VerifyBeforeExec()` (or the
imported verifier) and start exactly its binary/argv/env. Artifact swaps refuse;
finalization never re-mints their digest. No seal attests app-server identity,
filesystem semantics, process execution, or provider behavior.

## Validation tools

Run the repository's normal `make vet`, `make build`, `make test`, and
`make regress` gates. Focused behavioral tests are:

```sh
env -u TASK_BOARD_DIR go test -mod=mod ./pkg/agentic/systems/codex -run '^TestManaged' -count=1
env -u TASK_BOARD_DIR go test -mod=mod ./pkg/agentic -run '^TestManagedPlannerContract$' -count=1
```

Narrowing mutants live in
`tools/launchcontext-mutants/codex_managed_plan_mutants.go` and use the existing
isolated-source runner. For example:

```sh
env -u TASK_BOARD_DIR go run ./tools/launchcontext-mutants \
  --name codex-managed-wrong-exact-thread \
  --table-out .temp/managed-thread-mutant.md
```

The table goes to the supplied path; complete test output and real mutant
process exit codes go under `.temp/launch-context-mutants/process-evidence/`.
A mutant is killed only by the named behavioral assertion, not a setup failure
or the static refusal census. These mutants preserve searched-for tokens while
changing semantics, including a display config token hiding a permission key.
