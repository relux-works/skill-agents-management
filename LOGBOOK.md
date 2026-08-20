# Flight Logbook

> Institutional memory. Concise, factual, high-signal.
> Newest entries first. One block per insight.

## 2026-08-21

### 2140 — A bound the caller happens to satisfy is not a pinned bound
- FINDING: `EffortDeclaration.Accepts` trims the word before matching, and so does `BuildLaunch` at `spawn.go:162` BEFORE calling it. With the case-sensitivity test driving only `BuildLaunch`, deleting `Accepts`' own `TrimSpace` left the entire suite green (`go test ./... -count=1` → exit 0). The bound looked pinned and was being satisfied by a caller upstream of it.
- NOTE: The general shape: when two layers apply the same normalization, a test that enters at the outer one cannot observe the inner one at all. Every mutant of the inner check is invisible, so the inner check reads as tested and is not. It is the delete-only-mutant failure one level down — the test proves the PATH works, not that either check does.
- FIX: `TestEffortVocabularyMembershipIsExactOnTheTrimmedWord` drives `Accepts` directly, alongside `TestBuildLaunchDoesNotFoldTheEffortVocabulary` at the entry point. Both are load-bearing against different mutants: M11 (`Accepts` stops trimming) reds only the first, M12 (`BuildLaunch` stops trimming) reds only the second.
- NOTE: `Accepts` is exported and has a second production caller — `EffortDeclaration.Validate` asks it about the vendor's own `Recommended` — so it is contract surface, not a private helper the launch path happens to use.
- SCOPE: `pkg/vendorplugin/spawn_test.go`. Mutation evidence: `TASK-260821-atcotl_rev2-mutant-results.json`.
- STATUS: Resolved (TASK-260821-atcotl rev 2).

### 2135 — Naming a test for distinctness does not test distinctness
- FINDING: `TestResolutionRefusalsAreDistinctFacts` asserted four positive `errors.Is` calls and nothing else. Aliasing `ErrRuntimeVendorUnresolved = ErrRuntimeVendorUnregistered` left the whole suite green — the muse decision (entry 2015) rests entirely on callers branching on that typed difference, and after the collapse muse tells an operator to install a vendor nobody has identified.
- NOTE: `errors.Is(err, want)` proves a branch is REACHABLE. Mutual exclusivity is a separate claim and needs the negative half: `!errors.Is(err, other)` for every other sentinel a caller might branch on. Four positives cannot distinguish four sentinels from one.
- FIX: A table of four resolution refusals × five sentinels, each case asserting its own and refuting the other four. Table-shaped rather than one hand-written negative pair, so collapsing ANY pair goes red — not only the pair that was found.
- NOTE: The unresolved case resolves the real `muse` row through `SeedFrozenRuntimes` rather than a hand-built lookalike declaration, which needed a system registered under the id `muse` — `renamedPangolin` in `double_test.go`, the existing Layer-1 double under a different id rather than a second fake.
- SCOPE: `pkg/vendorplugin/runtime_test.go`, `pkg/vendorplugin/double_test.go`. Related: entry 2015.
- STATUS: Resolved (TASK-260821-atcotl rev 2).

### 2015 — An unresolved vendor is the absence of a vendor, not a degraded one
- DECISION: `muse`'s broker is carried as `VendorUnresolved` (the empty `VendorID`) plus a REQUIRED `BrokerProvenance{Checked, Found}`. Resolution refuses it with its own error, `ErrRuntimeVendorUnresolved`, distinct from "the vendor plugin is not compiled in".
- NOTE: The alternative — a null-object vendor with an empty model list — would have put a fake plugin behind a real interface, and every caller would have had to know which vendors are real. Making the runtime UNLAUNCHABLE through the vendor layer costs nothing today (no spawn execution exists yet) and keeps the interface honest for everyone else.
- NOTE: The provenance is what makes it a finding rather than an unfilled field. A blank vendor with no `Checked` and a blank vendor after a real search are indistinguishable without it, and they are different facts. Validation enforces both directions: a resolved vendor must record what established it, an unresolved one must record where it was looked for AND must not claim a finding.
- SCOPE: `pkg/vendorplugin/runtime.go`, `TestMusesBrokerIsCarriedAsCheckedAndEmpty`, `TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms`.
- STATUS: Resolved (TASK-260821-atcotl).

### 2014 — A per-key-type home list is strictly stronger than a file allowlist
- FINDING: Extending the single-source guard to the vendor layer had two shapes. A flat allowlist ("these files may hold binding tables") would have let `pkg/agentic/registry.go` bind vendors and `pkg/vendorplugin/registry.go` bind systems — a shadow table in the one place nobody would look, because the file legitimately holds a binding map.
- DECISION: `bindingHomes` maps each dispatch KEY TYPE (`SystemID`, `VendorID`, `RuntimeID`) to the one file permitted to bind it. The invariant is one home per FACT, and "which plugin implements system X" and "which plugin implements vendor Y" are two facts.
- NOTE: The narrowing mutant is the proof and nothing else is: flattening `misplaced()` into "is this file any home" reds ONLY `TestSingleSourceGuardCatchesCrossLayerBindings` and leaves all 58 other guard mutants green. Without that test the two designs are indistinguishable from a green suite.
- NOTE: `TestSingleSourceGuardRulesFireOnRealCode` had to change with it. Passing an EMPTY home list now resolves no key types at all, so an empty violation list would mean "the scanner had nothing to look for" — vacuously green. It displaces every home to a nonexistent path instead.
- SCOPE: `pkg/agentic/singlesource_guard_test.go`, `pkg/agentic/singlesource_guard_mutants_test.go`.
- STATUS: Resolved (TASK-260821-atcotl).

### 2013 — A vendor may add to a launch; it may not redirect one
- DECISION: `BuildLaunch` checks the `agentic.LaunchRequest` a vendor returns against what was admitted — system, model id, effort support, effort word, goal, budget, service tier, composition — and refuses a mismatch as a plugin-contract violation. Environment additions pass through untouched.
- NOTE: Without the check, a vendor plugin could silently move a run to another harness, another model or an unbounded budget, and the result would look exactly like the run that was asked for. That is the same failure class as the source repo's dropped-effort launch, one layer up.
- NOTE: The control matters as much as the gate: `TestBuildLaunchKeepsTheVendorsOwnAdditions` fails an implementation that ignores the vendor's answer and rebuilds the request itself, which would pass all nine redirection mutants.
- SCOPE: `pkg/vendorplugin/spawn.go` (`checkLaunchFidelity`).
- STATUS: Resolved (TASK-260821-atcotl).

### 2012 — The vendor's recommended effort is a declaration, not a default
- FINDING: A model row carries both an effort vocabulary and a recommended word. The obvious convenience — fill in the recommendation when the caller supplies nothing — is exactly the wrong-cost launch invariant 4 exists to close.
- DECISION: A missing effort is a refusal whose text names the vocabulary AND the recommendation, so an agent hitting it mid-run can fix the call from the error alone. The recommendation reaches the operator through the refusal, never through the argv.
- NOTE: The gate is proved by where the vendor is NOT called: `TestAVendorNeverSeesAnUnadmittedEffort` fails if `Spawn` is reached with an unvalidated effort, which closes the path on which a plugin could supply its own default downstream of the check.
- SCOPE: `pkg/vendorplugin/spawn.go` (`resolveEffort`).
- STATUS: Resolved (TASK-260821-atcotl).

### 2011 — A map[SystemID]bool dedup set trips the single-source guard, and should
- ANOMALY: `Model.Validate` used `map[agentic.SystemID]bool` to catch a model declaring one system twice. The guard reported it as a shadow binding table.
- DECISION: Rewritten as a slice scan rather than widening the guard to exempt set-shaped maps. A set of "systems this thing supports" is one refactor away from being a table of what each of them does, and the model list is a handful of entries.
- NOTE: Worth recording because the first instinct is to call it a false positive. The rule's bluntness is the reason it caught four spellings two reviews wrote against it.
- SCOPE: `pkg/vendorplugin/vendor.go`.
- STATUS: Resolved (TASK-260821-atcotl).

### 1830 — Two rules answering one question two different ways is a hole neither rule looks like
- FINDING: The `pkg/agentic` guard's switch rule carried two nets — structural (this value is an `ID()` call) and vocabulary (this literal is a known id) — while its `if`/comparison rule carried only the vocabulary net. Consequence: dispatch on an id nobody has declared yet was CAUGHT spelled `switch id` and ADMITTED spelled `if id == "opencode"`. An if-chain is not an exotic spelling; it is the first thing many people write.
- ANOMALY: The mutant matrix could not see it. Every if-chain mutant in the file used a KNOWN id, so the vocabulary net answered for all of them and the structural net was never asked — a full green matrix over a rule that was half missing.
- NOTE: Reusable lesson, independent of this guard: when two code paths are supposed to decide the same fact, they must call ONE helper, and each path needs at least one test whose input only the shared half can answer. Here that means undeclared ids in both spellings.
- FIX: Both rules now route through `dispatchesOnID`/`isIDExpr`; four permanent mutants with undeclared ids. NARROW-F6 (remove only the structural half) reds exactly those three comparison mutants and leaves the known-id if-chains green, which is the proof the nets are independent.
- STATUS: Resolved (TASK-260821-21vywo rev 3).

### 1829 — A residual declared with the wrong boundary is worse than an undeclared one
- FINDING: A review asked for `id := string(sys.ID()); switch strings.TrimSpace(id)` to be documented as a residual under the class "single-hop resolved, multi-hop declared open". That local is ONE hop. Filing it there would have put a false sentence into a file whose entire defect history is prose promising more than the rules deliver.
- DECISION: Closed it in code instead — the switch tag now looks through one-argument calls to a resolved local, which is what it already did for the identical tag WITHOUT the local. The resolver was not extended: `idCallLocals` is unchanged and still single-hop, because chasing assignment chains is the dataflow regress the source repository already refused to enter. The residual is now named by what the code actually keys on — the hop count, not the conversions around it.
- NOTE: A residual class is a claim about a mechanism. If the class name does not match the mechanism, a reader who trusts the list is misled more precisely than by no list at all.
- STATUS: Resolved (TASK-260821-21vywo rev 3).

### 1828 — "Can never happen" over a single read is a promise the code cannot pay
- FINDING: `System.ID`'s docstring said Register's normalization refusal meant a caller holding the plugin and a caller reading the registry "can never see two spellings of one system". `Register` read `ID()` once. A plugin whose `ID()` is unstable registered under its first answer, served its second to anyone holding it, and `BuildPlan` planned around the disagreement silently.
- FIX: Both halves, because either alone still lies. `Register` reads `ID()` twice and refuses a mismatch (`ErrUnstableSystemID`) — cheap, and it catches the accidental version: an id computed from mutable state or memoized on the wrong call. The promise was then rewritten to what the code backs: the registry key IS the spelling `ID()` returned at registration, for as long as the plugin keeps its word; "forever" is the plugin's obligation, not the registry's check.
- NOTE: The unenforceable half is a TEST, not a caveat in prose — `TestRegisterCannotSeeAnIDThatFlipsAfterRegistration` demonstrates a post-registration flip being admitted, so if a future change starts catching it, the test fails and the promise gets rewritten with it. Same pattern as the declared-open residuals in entry 1814.
- NOTE: Narrowing beats deleting here too. Comparing NORMALIZED values instead of raw ones admits exactly the case class the reviewer's probe walked through (`pangolin` → `Pangolin`), and reds the gate test — proof the gate covers the class, not just literal inequality.
- STATUS: Resolved (TASK-260821-21vywo rev 3).

### 1815 — A guard is worth what an outsider failed to defeat, not what its author tried
- FINDING: Revision 1 of the `pkg/agentic` single-source guard shipped 18 author-written mutants, all green. A reviewer wrote 15 spellings the author had not, and 4 walked straight through: an id copied into a local before the switch (`id := sys.ID(); switch id`), a table assembled key by key from an `init()`, a `const` declared inside a function body, and a value of a binding-table type the registry legitimately declares (`make(bindings)`).
- NOTE: None was obfuscation. Each is the ordinary way to write the thing — which is the class the guard exists for. The author's mutants clustered around the rules the author had just written; the reviewer's did not.
- FIX: Both sets now live in the suite permanently (`TestSingleSourceGuardCatchesShadowTables`, `...IDSwitches`, `TestSingleSourceGuardAgainstReviewMutants`), so the matrix in a review verdict is reproducible instead of a claim about a scratch copy.
- STATUS: Resolved (TASK-260821-21vywo rev 2).

### 1814 — Declared-open residuals are honest only because closing one turns a test red
- NOTE: `TestSingleSourceGuardResidualGaps` asserts that specific spellings are NOT reported. A review closed one of them (call-wrapped literals) with a four-line change to `foldStringConst`, and the residual test went red naming the case and telling the author to move it into the mutant set. The mechanism is demonstrated, not asserted.
- DECISION: Reusable pattern, independent of this task: a guard's threat model belongs in executable form. Prose saying "X is out of scope" drifts silently; a test that fails when X comes into scope cannot.
- NOTE: The same test now also carries a declared NON-goal (a switch on an accessor named `Identifier()` rather than `ID()`), so widening the guard to arbitrary identity-shaped method names has to be argued rather than happening.
- STATUS: Resolved (TASK-260821-21vywo rev 2).

### 1813 — A documented normalization contract with nothing enforcing it is two names for one system
- FINDING: `System.ID()` documented "must normalize to itself"; `Registry.Register` only checked that the id normalized SUCCESSFULLY. A plugin returning `"  PANGOLIN  "` registered fine — `IDs()` and `Plan.System` carried the folded spelling — while the plugin kept answering the raw one to anyone holding it from `Lookup`.
- FIX: `Register` refuses when `NormalizeSystemID(sys.ID()) != sys.ID()` (`ErrUnnormalizedSystemID`), naming both spellings.
- NOTE: This makes one earlier test's shape unreachable: "register `Pangolin`, expect `  PANGOLIN  ` to collide as a duplicate" cannot happen when neither spelling registers at all. The replacement asserts the stronger fact. The same applies to the CLI test that proved normalization by registering `Claude-Code` and watching `claude-code` print — it now proves the CLI can never surface a non-canonical spelling because such a plugin cannot be registered.
- NOTE: This is the source repo's two-normalizations disease caught one layer up: not two folding functions disagreeing, but one folding function applied on one side of a boundary only.
- STATUS: Resolved (TASK-260821-21vywo rev 2).

### 1812 — Single-source guard: structural rules outlive the id vocabulary
- DECISION: The `pkg/agentic` guard detects a shadow binding two ways — structurally (`map[SystemID]T` as a TYPE EXPRESSION anywhere outside `registry.go`; a `switch` on a value's `ID()`, including one copied into a local first), and by a local vocabulary of known system ids resolved through const/var/funclit indirection at package AND function-body scope.
- NOTE: The structural half is what matters. This module has no concrete plugins yet, so the vocabulary half has zero live matches today and would have looked healthy while guarding nothing. `map[SystemID]T` and a switch on `ID()` fire against ids nobody has declared — proven by mutants naming `opencode` / `some-future-harness`.
- AMENDED (rev 2): at revision 1 that claim was true only for the literal `switch sys.ID()` spelling. `id := sys.ID(); switch id { case "opencode": }` fired NOTHING — no structural rule, and no vocabulary entry either, so an undeclared id had no net at all. `idCallLocals` now resolves a local bound once to an `ID()` call and never rewritten; a rewritten variable and an id that leaves through a helper are declared-open residuals rather than approximated dataflow. See 1815.
- NOTE: Detecting the TYPE EXPRESSION rather than the composite literal is what catches `make(map[SystemID]T)`, a struct field, and a var declaration with no literal anywhere. Narrowing it to composite literals only (mutant M5) leaves four spellings undetected. The same lesson applied twice more in rev 2: a table filled in key by key (`t["codex"] = ...`) has no literal either — and that is exactly how the extraction source's own `adapterTable` is built, from an `init()`, because a var initializer there would have created an initialization cycle.
- SCOPE: `pkg/agentic/singlesource_guard_test.go`; 32 in-suite mutants across two sets in `singlesource_guard_mutants_test.go`.
- STATUS: Resolved (TASK-260821-21vywo rev 2).

### 1811 — A guard that scans a hardcoded directory list expires silently
- FINDING: The extraction source's argv guard names its two package directories explicitly. A third package added later is unguarded and nothing says so.
- FIX: `moduleSources` walks from `go.mod` and takes every non-test `.go` file. `TestSingleSourceGuardScansTheWholeModule` asserts named files were actually reached, so a clean result means "nothing found" rather than "nothing looked at".
- NOTE: The companion is `TestSingleSourceGuardRulesFireOnRealCode` — it rescans the real tree with the allowlist EMPTY and requires the registry's own map to be reported. Without it, green is equally consistent with a rule that never matches anything.
- STATUS: Resolved (TASK-260821-21vywo).

### 1810 — "Every dispatch surface" has to be read off the interface, not listed
- DECISION: `TestRegisteringOneSystemDrivesEveryDispatchSurface` enumerates `System`'s methods by reflection and requires the test double to have recorded a call on each after `BuildPlan` runs.
- NOTE: The source repo's axolotl test lists its five dispatch sites in a comment. That list is correct exactly once. A method added to the contract and not wired into `BuildPlan` fails the reflection version on the next run and cannot fail the hand-written one.
- SCOPE: `pkg/agentic/double_test.go`.
- STATUS: Resolved (TASK-260821-21vywo).

### 1809 — DryRunArgs was a seam, not a surface
- DECISION: The source adapter's `DryRunArgs` becomes `Argv(req, LaunchModeDryRun)` rather than a sibling method.
- NOTE: Its whole contract was "the same grammar as the real launch, minus side effects" — and the source paid for the gap: `BuildArgs` reported a hardcoded `codex`/`agy` while the real launch executed a managed-npm or preflighted binary. Two functions that must agree will eventually not. One function with a mode argument has nowhere to drift.
- SCOPE: `pkg/agentic/system.go` (`LaunchMode`), `TestDryRunMirrorsTheRealLaunchBinary`.
- STATUS: Resolved (TASK-260821-21vywo).

### 1808 — CompositionGrammar as a core enum contradicts the plugin model
- DECISION: `GrammarID` is an opaque string the plugin declares plus `System.ValidateComposition`, not a core enum with a core-side switch.
- NOTE: The source's enum meant a harness with a novel composition shape required editing core — the opposite of what a plugin seam is for. `GrammarNone` is still refused by `BuildPlan` BEFORE the plugin's validator runs, so a permissive validator cannot admit a composition the system declared no grammar for.
- STATUS: Resolved (TASK-260821-21vywo).

### 1807 — DefaultHome in the source adapter dispatched nothing
- FINDING: `Adapter.DefaultHome` was the literal string `"Config.ChildWorkDir()"` on all seven rows — documentation that `BuildCommand` overwrote `cmd.Dir` unconditionally after dispatch.
- DECISION: `Capabilities.DefaultHome` + `HomeEnvVar` now carry the harness CONFIG home, which is what `docs/architecture.md` names and what invariant 2's `IdentityKey(provider, home)` is keyed by. The child working directory became an explicit `LaunchRequest.WorkDir` / `Plan.WorkDir`.
- NOTE: Reusing the source's field name for a different fact would have been the worse option; the name is the same, the meaning is not, and it is called out in the task results.
- STATUS: Resolved (TASK-260821-21vywo).

### 1806 — The CLI's private plugin list WAS a second binding
- FINDING: `cmd.registeredPlugins []string` (scaffolded in TASK-260821-3svtog as a deliberate placeholder) is a second source for "which systems exist", which is exactly what this task's guard forbids.
- FIX: Deleted. `plugins` reads `agentic.Default` through a swappable `systemRegistry` var; the cmd test helper registers through the same public API a plugin's `init` would use.
- NOTE: Wiring the CLI was optional in the brief. Leaving it was not neutral — the placeholder would have survived as a shadow table the guard does not scan for by vocabulary (it holds no system ids today) and would have drifted the moment the first plugin landed.
- STATUS: Resolved (TASK-260821-21vywo).

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
