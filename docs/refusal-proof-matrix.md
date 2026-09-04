# Registration, Resolution and Planning Refusal Proof Matrix

This matrix is the exhaustive refusal inventory for the general plugin graph,
typed multi-node plans, and the vendor registration path. Every test drives the
public production entry point. `.scripts/verify-refusal-matrix.py` copies the
current tree, narrows exactly one refusal in each copy, and runs only the named
owner test with `-count=1`.

The current run contains 41 compile-clean mutants: the 37 of the authoritative
v0.4.3 run plus the four `ErrAliasInvalid` rows the alias contract added. Every
named test exits `1`; a zero exit, build failure, or failure in a different test
makes the harness fail. The nine-mutant v0.4.1 report was accurate; its older
logbook count of eight was not. The 37-row run superseded that partial
inventory rather than adding to it, and this one supersedes the 37.

`ErrAliasInvalid`'s two per-row cases — a self-referential target and a target
that is not a usable model id — are raised by `Model.Validate` rather than by
`checkAliases` and are covered by the same owning test. They are not separate
rows here because they are not separate call sites: `Model.Validate` already
owns a row in this table, and both call sites below run it before
`checkAliases`, which is why narrowing `checkAliases` alone still leaves them
refused.

## Raw plugin graph

| Error value | Production call site | Owning negative | Strictly narrower mutant |
| --- | --- | --- | --- |
| unnamed nil-registry error | `Registry.Register` -> `RegisterAll` | `TestNilRegistryRefusesRegistration` | Refuse a nil receiver only for an empty registration batch. |
| `plugin.ErrNilPlugin` | `Registry.Register` -> `RegisterAll` -> `nilPlugin` | `TestRegisterRefusesTypedNilPlugin` | Refuse only a nil interface, admitting typed nil plugins. |
| `plugin.ErrInvalidDeclaration` | `RegisterAll` -> `validateDeclaration` | `TestRegisterAllRefusesUnnormalizedDependencyDeclaration` | Validate dependency kind spelling but silently normalize dependency IDs. |
| `plugin.ErrUnstableDeclaration` | `RegisterAll` declaration double-read | `TestRegisterAllRefusesDependencyChangesAcrossDeclarationReads` | Compare only ID and kind, admitting plugins whose dependency list changes. |
| `plugin.ErrDuplicatePlugin` | `RegisterAll` candidate insertion | `TestRegisterRefusesSameKindDuplicate` | Refuse only cross-kind collisions, admitting same-kind duplicate IDs. |
| `plugin.ErrDuplicateDependency` | `RegisterAll` -> `validateDeclaration` | `TestRegisterAllRefusesRepeatedNonSelfDependency` | Refuse repeated dependencies only when they are self-dependencies. |
| `plugin.ErrMissingDependency` | `RegisterAll` -> `validateEdges` | `TestRegisterRefusesALaterMissingDependencyInANonEmptyGraph` | Check only dependency index zero. |
| `plugin.ErrUnsatisfiableDeclaration` | `RegisterAll` -> `validateEdges` | `TestRegisterAllRefusesALaterKindMismatchAgainstABatchNode` | Refuse kind mismatch only when the target is the historical `model-vendor` kind. |
| `plugin.ErrDependencyCycle` | `RegisterAll` -> `detectCycle` | `TestRegisterAllRefusesSelfAndMultiHopCycles` | Admit direct self edges while retaining longer-cycle refusal. |
| unnamed nil-registry error | `Registry.Resolve` | `TestNilRegistryRefusesResolution` | Refuse a nil receiver only when the requested ID is empty. |
| `plugin.ErrInvalidDeclaration` | `Registry.Resolve` -> `normalize` | `TestResolveRefusesInvalidPluginID` | Propagate normalization failure only for the empty ID, admitting other invalid spellings to lookup. |
| `plugin.ErrPluginNotRegistered` | `Registry.Resolve` | `TestResolveRefusesMissingPluginInNonEmptyRegistry` | Refuse a missing ID only when the entire registry is empty. |

### Exhaustive derivation

The registration side has nine error-producing paths: the `RegisterAll` nil
receiver guard; `nilPlugin`; the unstable declaration double-read;
`validateDeclaration` returning `ErrInvalidDeclaration` or
`ErrDuplicateDependency`; candidate insertion returning `ErrDuplicatePlugin`;
`validateEdges` returning `ErrMissingDependency` or
`ErrUnsatisfiableDeclaration`; and `detectCycle` returning
`ErrDependencyCycle`. Those nine paths are exactly the nine registration rows
above.

The resolution side has three error-producing paths: the `Resolve` nil receiver
guard; `normalize` returning `ErrInvalidDeclaration`; and the map lookup
returning `ErrPluginNotRegistered`. Those three paths are exactly the three
resolution rows above. Dependency materialization has no error branch because
registration already established every edge before it could enter the stored
graph. There are no other error-producing paths in raw registration or
resolution; the two enumerated sets are therefore equal to the 12 raw-plugin
rows in this matrix.

## Typed multi-node plan

| Error value | Production call site | Owning negative | Strictly narrower mutant |
| --- | --- | --- | --- |
| `agentic.ErrPlanInvalid` | `BuildMultiNodePlan` -> `validatePlanNode` | `TestBuildMultiNodePlanRefusesExplicitNodeWithEmptyBinary` | Reject an empty binary only on the implicit primary node. |
| `agentic.ErrDuplicatePlanNode` | `BuildMultiNodePlan` node insertion | `TestBuildMultiNodePlanRefusesDuplicateExplicitAndPrimaryNodeIDs` | Refuse only collisions with the implicit primary ID, admitting two equal explicit IDs. |
| `agentic.ErrPlanDependencyMissing` | `BuildMultiNodePlan` dependency resolution | `TestBuildMultiNodePlanRefusesMissingDependenciesFromPrimaryAndLaterEdges` | Check only dependency index zero. |
| `agentic.ErrPlanDependencyCycle` | `BuildMultiNodePlan` -> `planNodeOrder` | `TestBuildMultiNodePlanRefusesASelfCycle` | Admit a DFS back-edge to the current node while retaining multi-node cycle refusal. |

## Vendor graph bridge

| Error value | Production call site | Owning negative | Strictly narrower mutant |
| --- | --- | --- | --- |
| `vendorplugin.ErrNoAgenticRegistry` | `Registry.Register` and `syncAgenticGraph` | `TestARegistryWithNoAgenticRegistryAdmitsNoVendor` | Bypass both early and sync-time nil-system checks; the declaration reaches the later missing-edge refusal. |
| `vendorplugin.ErrUnknownAgenticSystem` wrapping `plugin.ErrMissingDependency` | `Registry.Register` model scan -> graph registration | `TestTheDependencyDirectionIsCheckedForEveryModel` | Classify unknown systems only on model index zero. |
| `plugin.ErrUnsatisfiableDeclaration` | `Registry.Register` -> `syncAgenticGraph` exact shadow comparison | `TestVendorRegistrationRefusesShadowDeclarationsWithEqualWidthButDifferentData` | Compare only dependency count, admitting equal-width dependency/kind shadows. |
| `plugin.ErrDuplicatePlugin` | `Registry.Register` -> `plugin.Registry.Register` | `TestVendorRegistrationRefusesVendorIDCollidingWithSyncedPluginKind` | Refuse only same-kind ID collisions, admitting a vendor over an arbitrary-kind synced node. |

`plugin.ErrDependencyCycle` is not reachable through vendor registration. The
agentic source graph must already validate every dependency before sync. A
source node cannot depend on the not-yet-registered vendor unless that vendor
ID is first present in the source graph; sync then imports that ID and vendor
registration stops at `ErrDuplicatePlugin` before cycle detection. The same
construction makes raw nil, unstable, invalid, and repeated-dependency errors
unreachable through this adapter: every imported node was already accepted by
`plugin.Registry`, and the vendor node is built from normalized, deduplicated
data. The defensive propagation remains, but there is no public input class to
narrow without manufacturing impossible internal state.

## Vendor declaration validation

| Error value | Production call site | Owning negative | Strictly narrower mutant |
| --- | --- | --- | --- |
| unnamed nil-registry error | `vendorplugin.Registry.Register` | `TestNilRegistryRefusesVendorRegistration` | Nil receiver is a singleton input class; removing its one guard reaches a panic and kills the test. |
| `vendorplugin.ErrNilVendor` | `Registry.Register` -> `nilVendor` | `TestRegisterRefusesTypedNilVendor` | Refuse only a nil interface, admitting a typed nil vendor. |
| `vendorplugin.ErrUnstableVendorID` | `Registry.Register` ID double-read | `TestRegisterRefusesAnUnstableVendorID` | Refuse disagreement only when the first ID is empty. |
| `*vendorplugin.InvalidIDError` (vendor ID) | `Registry.Register` -> `NormalizeVendorID` | `TestRegisterRefusesAnUnusableVendorID` | Preserve the typed error only for a strict subset; other invalid spellings lose the typed refusal. |
| `vendorplugin.ErrUnnormalizedVendorID` | `Registry.Register` normalized spelling check | `TestRegisterRefusesAnUnnormalizedVendorID` | Refuse only one whitespace spelling, admitting uppercase non-canonical IDs. |
| `vendorplugin.ErrNoModels` | `Registry.Register` model-list validation | `TestRegisterRefusesUnusableModelRows` | Refuse only a nil list, admitting an allocated empty list. |
| `vendorplugin.ErrDuplicateModel` | `Registry.Register` model scan | `TestRegisterRefusesUnusableModelRows` | Refuse duplicates only at model index one, admitting a non-adjacent repeat. |
| `*vendorplugin.InvalidIDError` (model ID) | `Registry.Register` -> `Model.Validate` -> `ValidateModelID` | `TestRegisterRefusesUnusableModelRows` | Refuse only leading/trailing whitespace, admitting an internal-space model ID. |
| `vendorplugin.ErrDescriptionEmpty` | `Registry.Register` -> `Model.Validate` -> `UsageDescription.Validate` | `TestRegisterRefusesAModelWithNoUsageDescription` | Refuse only the empty string, admitting whitespace-only descriptions. |
| `vendorplugin.ErrRankInvalid` | `Registry.Register` -> `Model.Validate` -> `CapabilityRank.Validate` | `TestRegisterRefusesARankWithNoEvidence` | Refuse only an exactly empty evidence source, admitting whitespace-only evidence. |
| `vendorplugin.ErrLifecycleInvalid` | `Registry.Register` -> `Model.Validate` -> `Lifecycle.Validate` | `TestRegisterRefusesAModelWithNoLineupState` | Retain refusal for one undeclared word while admitting the rest of the undeclared set. |
| `vendorplugin.ErrEffortDeclaration` | `Registry.Register` -> `Model.Validate` -> `EffortDeclaration.Validate` | `TestRegisterRefusesAContradictoryEffortDeclaration` | Refuse only an empty repeated vocabulary word, admitting ordinary repeats. |
| `vendorplugin.ErrSupersessionInvalid` | `Registry.Register` -> `Model.Validate` / `checkSupersession` | `TestRegisterRefusesAnUnusableSupersession` | Retain per-row checks but admit a validly spelled successor absent from the lineup. |
| `vendorplugin.ErrAliasInvalid` (dangling target) | `Registry.Register` / `Registry.DeclareRuntime` -> `checkAliases` | `TestTheAliasGateRefusesWhatItMustReject` | Refuse a target absent from the lineup only when it equals the alias's own id, admitting every other dangling target. |
| `vendorplugin.ErrAliasInvalid` (alias chain) | `Registry.Register` / `Registry.DeclareRuntime` -> `checkAliases` | `TestTheAliasGateRefusesWhatItMustReject` | Refuse a chained target only when it points back at the alias, admitting every longer chain. |
| `vendorplugin.ErrAliasInvalid` (effort mirror) | `Registry.Register` / `Registry.DeclareRuntime` -> `checkAliases` | `TestTheAliasGateRefusesWhatItMustReject` | Compare only effort SUPPORT, admitting a vocabulary or recommendation that differs from the identity's. |
| `vendorplugin.ErrAliasInvalid` (systems mirror) | `Registry.Register` / `Registry.DeclareRuntime` -> `checkAliases` | `TestTheAliasGateRefusesWhatItMustReject` | Compare the system sets only when they are the same length, admitting an alias that declares an extra harness. |
| `vendorplugin.ErrRecommendationAmbiguous` | `Registry.Register` -> `checkRecommendations` | `TestRegisterRefusesTwoRecommendationsForOneSystem` | Refuse a repeated pick only when it comes from the same model. |
| `vendorplugin.ErrPricingInvalid` | `Registry.Register` -> `Model.Validate` -> `Pricing.Validate` | `TestRegisterRefusesAnUnusablePricingContract` | Refuse `NaN` list prices while admitting infinities. |
| `vendorplugin.ErrModelInvalid` | `Registry.Register` -> `Model.Validate` | `TestRegisterRefusesANegativeContextWindow` | Refuse context windows below `-1`, admitting `-1`. |
| `vendorplugin.ErrDuplicateVendor` | `Registry.Register` vendor-map insertion | `TestRegisterRefusesNilAndDuplicates` | Refuse only an impossible sentinel ID, admitting an ordinary duplicate vendor. |

## Reproduction

```bash
python3 .scripts/verify-refusal-matrix.py
```

The script writes one full test log per mutant plus `summary.tsv` under
`.temp/TASK-260830-1jpse1/mutants/`. It never mutates the source checkout: each
case runs in a fresh copy that excludes `.git` and `.temp`.
