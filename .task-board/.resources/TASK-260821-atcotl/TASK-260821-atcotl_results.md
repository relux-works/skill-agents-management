# TASK-260821-atcotl — vendor plugin contract and registry

Layer 2 of `agents-management`: the `Vendor` plugin interface, its registry, and
runtime declarations as (agentic system × vendor) pairs. Built on the accepted
`pkg/agentic` contract at `014a6fa`. **Work is UNCOMMITTED** in the story
worktree `.temp/STORY-260821-224xfu/worktree`.

## What shipped

| Path | What |
| --- | --- |
| `pkg/vendorplugin/vendor.go` | `Vendor` interface, `Model` row (rank + evidence, usage description, effort vocabulary, driving systems), id types and their refusals, `SpawnContext`, `AvailabilityQuery` |
| `pkg/vendorplugin/availability.go` | the structured availability verdict and its validation |
| `pkg/vendorplugin/runtime.go` | `RuntimeDeclaration`, `BrokerProvenance`, `VendorUnresolved`, the six frozen seeds, `SeedFrozenRuntimes` |
| `pkg/vendorplugin/registry.go` | the vendor + runtime registry, the dependency-direction refusal, the F2 collision policy, `ResolveRuntime` |
| `pkg/vendorplugin/spawn.go` | `SpawnRequest`, `BuildLaunch` (the single Layer-2 dispatch site), `CheckAvailability`, the vendor-fidelity contract |
| `internal/ident` | the ONE identifier normalization, now shared by both layers |
| `tools/agents-management/cmd/vendors.go` | `vendors` and `runtimes` commands |
| `pkg/agentic/singlesource_guard*_test.go` | the guard extended to three key types from one list |
| `pkg/agentic/system.go` | `NormalizeSystemID` delegates to `internal/ident`; behaviour and error text unchanged |

## Acceptance criteria

**AC1 — the interface.** `Vendor` is `ID() / Models() / Availability(q) / Spawn(sc)`.
A `Model` carries: `ModelID`, `UsageDescription` (blank is refused at
registration), `CapabilityRank{Position, Basis []RankEvidence}` (a rank with no
observation behind it is refused — ranking is never policy), `EffortDeclaration`
(support + vocabulary + recommended, validated against itself), and
`Systems []agentic.SystemID`. Effort TRANSPORT is not re-declared: it stays with
the agentic system, and `Model.Launchable()` projects the row onto
`agentic.Model` — the only vendor-shaped fact Layer 1 takes. The spawn surface
is `SpawnRequest` carrying model, effort, prompt path/bytes, work dir, home, env,
and `*agentic.Goal`, `*agentic.Budget`, service tier and `agentic.Composition`
— agentic's own types, not copies of them.

**AC2 — the dependency direction, enforced.** `Registry.Register` looks every
declared system up in the agentic registry the vendor registry was built
against, and refuses with both ids plus the model that named it:

```
vendorplugin: vendor declares an agentic system that is not registered: vendor
"narwhal" declares agentic system "opencode" for model "narwhal-deep", and no
plugin is registered for "opencode"; register the agentic system plugin first —
a vendor depends on the systems that drive it, never the other way round
```

Negative tests: `TestRegisteringAVendorNamingAnUnknownSystemIsRefused`,
`TestRegisteringAVendorWhoseOnlySystemIsUnknownIsRefused`,
`TestTheDependencyDirectionIsCheckedForEveryModel`,
`TestARegistryWithNoAgenticRegistryAdmitsNoVendor` (the bypass path — a registry
built with no Layer 1 refuses every vendor rather than skipping the check).

**AC3 — runtimes as declared pairs.** The six frozen ids are seeded into
`Default` at init and pinned as a whole table by
`TestTheSixHistoricalRuntimesAreSeededWithTheirFrozenBindings`:
`claude`=claude-code×anthropic, `codex`=codex×openai, `qwen`=qwen-code×alibaba,
`gemini`=gemini-cli×google, `agy`=antigravity×google, `muse`=muse×UNRESOLVED.
Real binary output:

```
$ agents-management runtimes
agy	antigravity	google
claude	claude-code	anthropic
codex	codex	openai
gemini	gemini-cli	google
muse	muse	vendor unresolved
qwen	qwen-code	alibaba
```

**AC4 — availability without an interface break.** One struct, four states,
evidence-defined: `Healthy` needs at least one checked source and no failed
read; `Limited` needs a clear time AND an observation; `Unreachable` needs an
observation or a failure; `Unknown` covers the rest and keeps
"checked-and-found-nothing" (`UnknownAfterCheck`) distinguishable from "nobody
looked" (`Unchecked`) and from "the read failed" (`Failures`). The zero value is
Unknown; only Healthy is `Serviceable()`.
`TestTheLocalResourcePlaneFitsBehindTheVerdict` demonstrates all five answers
the planned local-model plane needs — loaded/idle, busy-until, waiting on an
eviction, plane not running, plane could not tell — expressed with no interface
change. The plane itself is NOT built.

## Decisions that a reviewer should weigh

1. **Package name `pkg/vendorplugin`.** `pkg/vendor` is unusable: Go treats any
   directory named `vendor` as a vendor tree.

2. **How an unknown-vendor runtime is represented.** `VendorUnresolved` (the
   empty `VendorID`) plus a REQUIRED `BrokerProvenance`. It is not a vendor and
   nothing implements it; resolution refuses with `ErrRuntimeVendorUnresolved`,
   which is a different error from "the vendor plugin is not compiled in"
   (`ErrRuntimeVendorUnregistered`) because the two have different fixes. So the
   muse runtime is a complete declaration and an unlaunchable one through the
   vendor layer, and the vendor interface is unchanged for everyone else. The
   alternative — a null-object vendor with an empty model list — would have put
   a fake plugin behind a real interface. Recorded as logbook 2015.

3. **F2 compares the BINDING, not the prose.** `SameBinding` is
   (id, system, vendor); provenance wording is documentation. Two callers
   declaring the same pair with differently worded evidence have not disagreed.
   Both directions tested, plus rebinding a frozen id, plus double seeding.

4. **The guard was extended to a per-KEY-TYPE home list, not a flat allowlist.**
   `bindingHomes` maps `SystemID`/`VendorID`/`RuntimeID` each to the one file
   permitted to bind it, so a `map[VendorID]T` inside `pkg/agentic/registry.go`
   is still a violation. ONE list, one guard file, no second guard. The old
   comment demanded that a second allowlist entry be argued rather than
   absorbed; the argument is in the file and in logbook 2014.

5. **`internal/ident`.** Invariant 5 names "one normalization for identifiers".
   Three id kinds folding through three copies of the same loop is the
   duplicate-charset failure the source repo paid for, so the loop moved to
   `internal/ident` and `agentic.NormalizeSystemID` delegates to it. Its error
   text and behaviour are byte-identical (the existing agentic tests are
   unchanged and green); `TestBothLayersNormalizeIdentifiersIdentically` holds
   the layers together.

6. **A vendor may ADD to a launch and may not REDIRECT it.** `BuildLaunch`
   checks the returned `agentic.LaunchRequest` against what was admitted.
   Environment additions pass through — `TestBuildLaunchKeepsTheVendorsOwnAdditions`
   is the control that would fail an implementation that ignores the vendor's
   answer and rebuilds the request itself.

7. **The recommended effort is never substituted.** A missing effort is a
   refusal naming the vocabulary and the recommendation.
   `TestAVendorNeverSeesAnUnadmittedEffort` proves the vendor is not reached, so
   there is no downstream path on which a plugin could inject its own default.

8. **`Model.Validate` dedups declared systems with a slice, not a
   `map[SystemID]bool`** — the guard reported the map, correctly. Not widened
   to exempt set-shaped maps. Logbook 2011.

9. **CLI.** `vendors` and `runtimes` were added (`--json` on both). `runtimes`
   lists DECLARATIONS, so every id appears whether or not its plugins are
   compiled in, and the JSON carries the broker provenance so "unresolved" shows
   the search behind it. `plugins` is untouched and its pinned output is
   unchanged.

## The test double

One fake vendor, `narwhal`, and one runtime declaration, `tusk`, registered
through the public API only. `TestVendorDoubleExistsOnlyInTests` walks every
non-test source in the module and fails if either id appears.
`TestRegisteringOneVendorDrivesEveryDispatchSurface` reads the surface list off
the `Vendor` interface BY REFLECTION, so a method added to the contract and not
driven from the package's entry points fails on the next run.

The Layer-1 double it declares support for is `pangolin` — the same id
`pkg/agentic` uses, a separate minimal implementation. It has to be: that double
is unexported test-only code and `pkg/agentic`'s own
`TestTestDoubleExistsOnlyInTests` requires it to stay that way, so exporting it
through a helper package would have failed that test and put a fake harness in a
production package.

## Evidence: every gate mutated, every mutant killed

`.temp/TASK-260821-atcotl/mutate.py` applies one gate mutation, runs the
package's tests, and restores the file. 19 mutants, 0 survivors. Results JSON:
`.temp/TASK-260821-atcotl/mutant-results.json`.

| Mutant | What it removes or narrows | Tests that go red |
| --- | --- | --- |
| `ac2-delete` | the declared-system lookup | 3 dependency-direction tests |
| `ac2-narrow-first-model-only` | check only `models[0]` | `TestTheDependencyDirectionIsCheckedForEveryModel` only |
| `ac2-bypass-nil-agentic-registry` | admit when Layer 1 is absent | `TestARegistryWithNoAgenticRegistryAdmitsNoVendor` |
| `f2-conflict-admitted` | conflicting declaration overwrites | 3 conflict subtests |
| `f2-match-refused` | matching declaration refused | 3 idempotency subtests |
| `f2-last-write-wins` | `SameBinding` compares the id only | 3 conflict subtests |
| `description-empty-admitted` | the blank-description refusal | `TestRegisterRefusesAModelWithNoUsageDescription` |
| `description-narrowed-to-nil-only` | refuse `""` but admit `"   "` | same test (the narrowing, not just the delete) |
| `rank-without-evidence-admitted` | the empty-`Basis` refusal | `TestRegisterRefusesARankWithNoEvidence/no_basis_at_all` |
| `availability-healthy-without-checking` | healthy-needs-a-source | verdict + `CheckAvailability` tests |
| `availability-gate-not-called-from-production` | the `Validate` call in `CheckAvailability` | `TestCheckAvailabilityRefusesAVendorsDishonestVerdict` |
| `effort-recommended-substituted` | fill in the recommendation | missing-effort + never-sees-unadmitted tests |
| `effort-vocabulary-unchecked` | the vocabulary check | 2 effort tests |
| `fidelity-not-checked` | the whole fidelity gate | 9 redirection subtests |
| `fidelity-narrowed-to-system-only` | check the system but not the model | `a_different_model` only |
| `model-system-pairing-unchecked` | model-declares-this-harness | `TestBuildLaunchRefusesAModelTheRuntimesSystemCannotDrive` |
| `unresolved-vendor-resolves` | the unresolved-broker refusal | `TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms` |
| `guard-extension-removed` | `VendorID`/`RuntimeID` from `bindingHomes` | 6 vendor-layer + 4 cross-layer subtests |
| `guard-homes-flattened-to-an-allowlist` | per-key-type precision → flat allowlist | `TestSingleSourceGuardCatchesCrossLayerBindings` ONLY |

The last row is the one that matters most: it is the only test that distinguishes
the design chosen from the weaker one, and every other guard mutant stays green
under it.

## Gates — run foreground, real exit codes

| Command | Exit | Note |
| --- | ---: | --- |
| `make vet` | 0 | |
| `make build` | 0 | binary at `tools/agents-management/agents-management` |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 | 4 packages ok, 1 with no test files (`main`) |
| `gofmt -l pkg/ tools/` | 0 | no output; `gofmt -l internal/` also clean |

Smoke on the real binary: `runtimes`, `runtimes --json`, `vendors --json` and
`plugins --json` all behave as documented (the last two print `[]` — no plugin
is compiled in yet, which is the answer, not a stub).

## Scope deliberately not entered

No concrete vendor plugins (next story). No spawn EXECUTION (port stories). No
limit plane or resource plane — only the verdict types they will report through.
No second guard file.

## Working-tree note

`git status` in this worktree shows the repository's tracked files staged as
deleted while present as untracked. That state predates this task (it is in the
session's opening snapshot) and nothing here touched the index. Every file this
task wrote is uncommitted in the working tree, as instructed.
