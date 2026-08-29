# Consuming the module

How another Go program depends on `agents-management`, what it gets, and what
it must still own. Written from the first real consumer — `skill-project-management`,
whose `tools/board-cli` swapped its whole spawn plane onto this module.

## What you depend on

One Go module, one path, one tag:

```
github.com/relux-works/skill-agents-management v0.4.3
```

There is no `replace` on trunk and there must not be one: a committed
sibling-path `replace` is a path that exists on exactly one machine, and CI is
not that machine.

```bash
go get github.com/relux-works/skill-agents-management@v0.4.3
```

**Nothing else.** This repository went PUBLIC on 2026-08-23, so the fetch goes
through the default proxy with sum-db verification and needs no credential at
all. It was private before that, and the arrangement it needed — a `GOPRIVATE`
setting and a `url.insteadOf` rewrite carrying a PAT — was removed the same day.
Do not reintroduce either: the first consumer's own CI guard
(`tools/board-cli/internal/ciguard`) now enforces the ABSENCE of that plumbing,
so a helpful re-addition fails its build rather than helping.

## The version to require

`v0.4.3` is the refusal-proof patch for the general-plugin-graph release. It
adds an exhaustively re-derived 12-path raw registration/resolution matrix to
the mutation-proven graph and multi-node refusal bounds without changing the
v0.4.0 API. The graph remains additive over the v0.3.0
System/Vendor/runtime surface: task-board can upgrade without changing its
registration or launch calls, then migrate deliberately to `pkg/plugin` later.

The release adds `plugin.Declaration{ID, Kind, Dependencies}`, atomic graph
registration/resolution, the `inference-engine` kind, and typed multi-node
launch plans. The rollback is a dependency pin to `v0.3.0`; no persisted board,
runtime or limit-state data is migrated by v0.4.3. Never rewrite a published
tag—publish a patch release if the compatibility surface needs repair.

Historical version note: `v0.1.0` was the first tag.

**`v0.2.0` carries one breaking change.** `CapabilityRank.Position int` became
`CapabilityRank.Score int`: this module now records the vendor's capability
SCORE with its ties intact rather than a tie-free position, because a position
numbering two equal models 7 and 8 asserts an ordering nobody observed. If you
read `.Position`, read `vendorplugin.Lineup(models)` instead — it derives the
total order (score descending, ties broken by declaration order, positions
1..n) and marks every tied row `Tied`, which is the fact the old field could
not carry.

`v0.2.0` also moves six facts INTO the module that a consumer may have been
declaring itself: `Model.Lifecycle`, `Model.SupersededBy`, `Model.Recommended`,
`Model.ContextWindowTokens`, `Model.Pricing`, and the model rows of a
vendor-unresolved runtime (`RuntimeDeclaration.Models`, which is how the two
`muse` rows reach a consumer). Read them from here and delete your own copies —
that is the point of the release. What does NOT move is POLICY: which models a
repository may spawn stays your configuration's decision.

## Local development against a sibling checkout

When you are changing both repositories at once, use a **Go workspace**, never
a `replace`:

```bash
cd /path/to/consumer
go work init ./tools/your-module
go work use ../../skill-agents-management
```

`go.work` and `go.work.sum` belong in `.gitignore`. Two reasons, and the first
one bites in CI:

1. `go.work` is discovered by walking UP from the working directory, so a
   committed root `go.work` applies on Actions runners too — reintroducing the
   sibling-checkout assumption that removing the `replace` just took out.
2. It is per-checkout. A story worktree under `.temp/` needs a different
   relative path to the sibling than the main clone does.

Do not vendor. Go auto-enables `-mod=vendor` the moment a `vendor/modules.txt`
exists, and a stale vendor tree then fails every build rather than falling back;
both repositories dropped vendoring for that reason and pass `-mod=mod`
explicitly everywhere.

## Wiring the compatibility packages

A binary gets the plugins it IMPORTS. Every plugin package registers itself
into the package-level default registry from its own `init`, so a blank import
is the whole wiring step:

```go
import (
    // Agentic systems — import only the harnesses this binary can run.
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"

    // Model vendors. Each blank-imports the systems its models
    // declare, so importing a vendor pulls in its harnesses whether you
    // named them or not.
    _ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/alibaba"
    _ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/anthropic"
    _ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/google"
    _ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/openai"
)
```

Registration order does not matter for DECLARATIONS — the six frozen runtimes
are seeded into `vendorplugin.Default` before any plugin registers — but it
does for the dependency check: a vendor whose declared system is not registered
is refused at `init` with a panic naming both ids. That is why each vendor
package blank-imports its own systems; you cannot get a half-wired binary.

`local-models` is the one vendor that does NOT belong in that blank-import
list: its catalog depends on a machine-local `~/.agents/.configs/local-models.toml`
that may not exist, and `Registry.Register` refuses ANY vendor whose
`Models()` returns zero rows — a blank import would make an absent file on
one operator's machine fail registration for every OTHER vendor and system in
the same binary. Register it conditionally instead, reading
`localmodels.Peek()`'s three-way `{Absent, Err, Config}` result first:

```go
switch result := localmodels.Peek(); {
case result.Absent:
    // no local-models.toml on this machine — register nothing
case result.Err != nil:
    // present but malformed — note it so a later resolution of "local-qwen"
    // gets a distinct, typed error instead of the generic "unknown runtime"
    _ = registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{
        Reason: "malformed", Err: result.Err,
    })
default:
    _ = registry.Register(localmodels.New(result.Config))
    _ = registry.DeclareRuntime(vendorplugin.RuntimeDeclaration{
        ID: "local-qwen", System: "pi", Vendor: localmodels.VendorID,
        Broker: vendorplugin.BrokerProvenance{Checked: []string{"local-models.toml"}, Found: "declared once local-models registers"},
    })
}
```

Prefer isolated registries in tests. `agentic.NewRegistry()` and
`vendorplugin.NewRegistry(systems)` take no globals, and
`vendorplugin.SeedFrozenRuntimes(registry)` gives you the frozen table without
touching the process-wide default.

For a new plugin kind, use the general graph directly. The registry does not
need an edit for the kind:

```go
registry := plugin.NewRegistry()
err := registry.Register(enginePlugin) // declares Kind: inferenceengine.Kind
```

Use `RegisterAll` when a set is intended to arrive together; cycles, missing
dependencies and kind mismatches are refused atomically with named errors.
An inference-engine consumer may resolve the typed declaration but must not
treat graph registration or contract validation as capability evidence:

```go
resolved, err := inferenceengine.ResolveContract(registry, "llama-cpp")
if err != nil {
    return err
}
_ = resolved.Contract // Declaration data only; there are no observed results.
```

A concrete engine implements declaration-only `inferenceengine.Engine` and
returns `inferenceengine.RequiredContract()`. It has no observation method.
`ValidateCandidateValue(fact, raw)` can canonicalize a fact-specific candidate
shape, but its return value remains caller data: it proves neither process
origin nor admission. agents-infra must obtain effective argv, endpoint,
residency, mapping, lifecycle, pressure, and model-harness facts through its
own concrete path before applying the shape validator. Every rule fixes
`refuse` for read-failed, malformed, and unsupported derivations. The contract
binds local executable/argv versus SSH forwarding plus stress/restart policy to
`ExecutionOwner == "agents-infra"`; this module runs no process, SSH, polling,
or supervision operation.

There is no production observation caller in this module or agents-infra at
this revision. `ResolveContract` is therefore not a production gate. Adoption
must add the agents-infra call site and real-entry negatives before any consumer
may document these candidates as observed values.

Existing system plugins can opt into a graph prerequisite without changing the
`System` interface:

```go
_ = systems.RegisterPlugin(enginePlugin)
_ = systems.RegisterWithDependencies(piSystem,
    plugin.Ref{ID: "llama-cpp", Kind: inferenceengine.Kind},
)
```

## Declaring your own runtime

A runtime is a **declaration**, not code. An operator-configured cross-runtime
— alibaba models under the codex harness, say — is one call:

```go
err := vendorplugin.Default.DeclareRuntime(vendorplugin.RuntimeDeclaration{
    ID:     "qwen-codex",
    System: "codex",
    Vendor: "alibaba",
    Broker: vendorplugin.BrokerProvenance{
        Checked: []string{"your-repo config (spawn.runtimes.qwen-codex)"},
        Found:   "the operator declared this pair",
    },
})
```

`BrokerProvenance` is required, always. A blank vendor with no provenance and a
blank vendor after a real search are different facts, and the type refuses to
let them look the same.

The collision policy is F2: a declaration matching an existing binding is
idempotent, a conflicting one is refused and the FIRST declaration stands. Do
not rebind a frozen id — it names limit-state files and feeds admitted-pair
digests, and a rebind orphans that state with no error anywhere.

## Launch and availability entry points

| You want | Call | It does not |
| --- | --- | --- |
| The launch surface for one (system, mode) | `agentic.BuildPlan(registry, req, mode)` → `Plan{Binary, Argv, Env, Stdin, …}` | execute anything |
| Validate an engine specification | `inferenceengine.ResolveContract(graph, engineID)` → declaration only | treat the result as observed state, call plugin derivation, execute or supervise a process |
| Add engine/sidecar process nodes | `agentic.BuildMultiNodePlan(primary, primaryDependencies, nodes...)` → the same primary fields plus dependency-ordered `Plan.Nodes` | execute, supervise or attest a process |
| The same, resolved through a runtime launch binding | `vendorplugin.BuildLaunch(ctx, registry, SpawnRequest{…}, mode)` — resolves either an established vendor binding or an explicit declaration-owned system-only binding, validates the effort word against the owning model row, preserves typed `RunContext`, asks a vendor for a `LaunchRequest` when one exists (refusing redirects, including changed/dropped tracked-run identity), otherwise projects the validated declaration losslessly, runs the resolved system's `Preflightable` check unless dry-run, then hands it to `BuildPlan` | execute anything, fabricate a vendor for a system-only runtime, inject a default effort, or ask callers to duplicate run identity in `Env` |
| Whether a launch is admissible right now | `providerlimits.Store.AvailabilityFor(VerdictQuery{Runtime, Model, Home})` → `vendorplugin.Availability` | write anything — not the state file, not the index, not a probe claim |

## What stays yours

The module ends at a plan. A consumer still owns:

- **Execution.** `*exec.Cmd`, working directory, process group, deadlines,
  output capture, exit classification.
- **Preflights that start processes.** The Antigravity binary probe and Claude
  goal preparation both run the harness. `BuildPlan` calls every plugin method
  to build a DRY RUN, so neither can live behind one; their RESULTS cross the
  boundary instead — `agy.NewWithRuntime(agy.Runtime{Executable: …})` takes the
  preflighted path.
- **Everything about work.** Roles, tasks, Change Requests, review routing,
  worktree isolation. This module never reads a board.
- **Recording an observation.** `AvailabilityFor` is a read. Turning a
  provider's refusal into a suppression is `providerlimits`' write path, and
  claiming a probe is an atomic claim, not a read.

## Verifying the swap

Two things are worth checking on the consumer's side after wiring, because both
fail silently:

- **Limit state survives.** The plane fails open: if the post-swap reader
  cannot find a pre-swap suppression, nothing errors and every exhausted group
  reads available. Pin `IdentityKey(provider, home)` against state FILENAMES a
  different binary chose, and assert the fail-open disaster too — the same
  bytes under a key one digit away must read as available, or the positive test
  proves nothing.
- **The launch surface did not move.** Compare argv, environment added/removed
  and stdin BYTES, not an argv string. `pkg/agentic/parity` holds the goldens
  and the comparator; `pkg/agentic/parity/testdata/goldens/README.md` states
  what the capture could not reach.

See [shipped-state.md](shipped-state.md) for what the first consumer chose to
keep on its own side and why.
