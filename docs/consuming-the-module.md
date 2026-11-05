# Consuming the module

How another Go program depends on `agents-management`, what it gets, and what
it must still own. Written from the first real consumer — `skill-project-management`,
whose `tools/board-cli` swapped its whole spawn plane onto this module.

## What you depend on

One Go module, one path, one tag:

```
github.com/relux-works/skill-agents-management v0.1.0
```

It is a **private** repository in the `relux-works` organization. There is no
`replace` on trunk and there must not be one: a committed sibling-path
`replace` is a path that exists on exactly one machine, and CI is not that
machine.

```bash
go get github.com/relux-works/skill-agents-management@v0.1.0
```

For a private module that needs two things in the environment doing the fetch:

```bash
export GOPRIVATE='github.com/relux-works/*'
git config --global \
  url."https://x-access-token:${TOKEN}@github.com/".insteadOf "https://github.com/"
```

`GOPRIVATE` keeps the module away from `proxy.golang.org` and `sum.golang.org`;
the rewrite is what makes the fetch authenticate. A fine-grained PAT with
`Contents:read` on this repository is enough. On GitHub Actions, read the secret
through `env:` and fail loudly if it is missing — the `secrets` context is NOT
available to an `if:` key, and referencing it there is a workflow syntax error
that kills the run before any job starts.

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

## Wiring the two layers

A binary gets the plugins it IMPORTS. Every plugin package registers itself
into the package-level default registry from its own `init`, so a blank import
is the whole wiring step:

```go
import (
    // Layer 1 — the harnesses. Import only the ones this binary can run.
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
    _ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"

    // Layer 2 — the vendors. Each blank-imports the systems its models
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

Prefer isolated registries in tests. `agentic.NewRegistry()` and
`vendorplugin.NewRegistry(systems)` take no globals, and
`vendorplugin.SeedFrozenRuntimes(registry)` gives you the frozen table without
touching the process-wide default.

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

## The three entry points

| You want | Call | It does not |
| --- | --- | --- |
| The launch surface for one (system, mode) | `agentic.BuildPlan(registry, req, mode)` → `Plan{Binary, Argv, Env, Stdin, …}` | execute anything |
| The same, resolved through a runtime and a vendor | `vendorplugin.BuildLaunch(registry, SpawnRequest{…}, mode)` — resolves the pair, validates the effort word against the model's own vocabulary, asks the vendor for a `LaunchRequest`, refuses one that redirects the launch, hands it to `BuildPlan` | execute anything, or inject a default effort |
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
