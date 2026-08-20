# Reviewer guard mutants — raw results (TASK-260821-21vywo)

Independent of the producer's 16. Run against the candidate tree
`1b2c006c95bd37a756386c2a31f5b6d141f6efd5` in a scratch copy at `/tmp/rev21vywo`
via `go test ./pkg/agentic/ -run TestReviewerMutants -count=1 -v`.

```
RESULT R1 type alias to SystemID as map key                 -> CAUGHT: pkg/launcher/r1.go:9 [binding-table] var shadow: a map keyed by SystemID; the registry is the only place a system binding may live
RESULT R2 named type wrapping SystemID as map key           -> CAUGHT: pkg/launcher/r2.go:9 [binding-table] var shadow: a map keyed by SystemID; the registry is the only place a system binding may live
RESULT R3 if/else-if chain with == on known ids             -> CAUGHT: pkg/launcher/r3.go:6 [id-switch] func pick: a comparison against the system id "codex"; an if-else chain over ids is a switch spelled differently | pkg/launcher/r3.go:8 [id-switch] func pick: a comparison against the system id "claude-code"; an if-else chain over ids is a switch spelled differently
RESULT R4 init() successive assignment, map[string]T        -> UNCAUGHT
RESULT R5 init() successive assignment, map[SystemID]T      -> CAUGHT: pkg/launcher/r5.go:7 [binding-table] var shadow: a map keyed by SystemID; the registry is the only place a system binding may live | pkg/launcher/r5.go:12 [binding-table] func init: a map keyed by SystemID; the registry is the only place a system binding may live
RESULT R6 switch on LOCAL copy of sys.ID(), unknown ids     -> UNCAUGHT
RESULT R7 switch on LOCAL copy of sys.ID(), known ids       -> CAUGHT: pkg/launcher/r7.go:7 [id-switch] func dispatch: a switch case on the system id "codex"
RESULT R8 local const indirection in ==                     -> UNCAUGHT
RESULT R9 local var slice indirection in switch case        -> CAUGHT: pkg/launcher/r9.go:6 [binding-table] func pick: a composite literal with the system id "codex" as a element; the registry is the only place the set of systems may be spelled
RESULT R10 make() of named binding type declared in registry -> CAUGHT: pkg/agentic/r10.go:3 [binding-table] type bindings: a map keyed by SystemID; the registry is the only place a system binding may live
RESULT R11 var of named binding type, no literal            -> CAUGHT: pkg/agentic/r11a.go:3 [binding-table] type bindings: a map keyed by SystemID; the registry is the only place a system binding may live
RESULT R12 struct field map[SystemID]T outside registry     -> CAUGHT: pkg/launcher/r12.go:8 [binding-table] type holder: a map keyed by SystemID; the registry is the only place a system binding may live
RESULT R13 type switch-free: map literal keyed by known id, plain string -> CAUGHT: pkg/launcher/r13.go:5 [binding-table] var shadow: a composite literal with the system id "codex" as a key; the registry is the only place the set of systems may be spelled
RESULT R14 local map var with composite literal keyed by id -> CAUGHT: pkg/launcher/r14.go:6 [binding-table] func boot: a composite literal with the system id "codex" as a key; the registry is the only place the set of systems may be spelled
RESULT R15 method named Identifier() not ID(), switch on it -> UNCAUGHT
--- PASS: TestReviewerMutants (0.00s)
PASS
ok  	github.com/relux-works/skill-agents-management/pkg/agentic	1.263s
```

## Module-walk proof — violation planted in a package directory that does not exist today

`pkg/brandnew/deeper/shadow.go` containing `var shadow = map[agentic.SystemID]builder{}`:

```
--- FAIL: TestSingleSourceGuardFindsNoSecondBinding (0.00s)
    singlesource_guard_test.go:678: a second agentic system binding exists outside the registry:
          pkg/brandnew/deeper/shadow.go:7 [binding-table] var shadow: a map keyed by SystemID; the registry is the only place a system binding may live
FAIL
```

## Residual-honesty proof — residual class 1 closed, residual test goes RED

Four-line addition to `foldStringConst` unwrapping a one-argument call:

```
=== RUN   TestSingleSourceGuardResidualGaps/a_call-wrapped_literal_key
    singlesource_guard_mutants_test.go:454: this residual is no longer open: pkg/launcher/residual.go:5 [binding-table] var shadow: a composite literal with the system id "claude-code" as a key; the registry is the only place the set of systems may be spelled. Update the threat model on singlesource_guard_test.go and move this case into the mutant set.
--- FAIL: TestSingleSourceGuardResidualGaps (0.00s)
```

Reverted byte-identically (`diff` against the candidate blob: clean).

## Axolotl proof — the single registration line deleted from registerPangolin

25 tests red across every dispatch surface:

```
--- FAIL: TestRegisteringOneSystemDrivesEveryDispatchSurface
--- FAIL: TestPlanCarriesTheDoublesObservableLaunchSurface
--- FAIL: TestDryRunMirrorsTheRealLaunchBinary
--- FAIL: TestBuildPlanRefusesUndeclaredLaunchMode
--- FAIL: TestBuildPlanRefusesRequiredEffortUnderTransportNone
--- FAIL: TestBuildPlanRefusesAnEffortValueUnderTransportNone
--- FAIL: TestBuildPlanRefusesRequiredEffortWithNoValue
--- FAIL: TestBuildPlanAdmitsAnEffortlessModelUnderEveryTransport
--- FAIL: TestBuildPlanRefusesUnsupportedLaunchParameters/{goal,budget,service_tier}
--- FAIL: TestBuildPlanAdmitsSupportedLaunchParameters
--- FAIL: TestBuildPlanRefusesCompositionUnderGrammarNone
--- FAIL: TestBuildPlanPropagatesThePluginsCompositionRefusal
--- FAIL: TestBuildPlanSkipsCompositionValidationWhenNoneIsAttached
--- FAIL: TestBuildPlanRefusesPluginContractViolations/{empty_binary_with_no_error,stdin_bytes_while_reporting_nothing_attached}
--- FAIL: TestBuildPlanAdmitsAnAttachedEmptyStdin
--- FAIL: TestBuildPlanPropagatesEachSurfaceFailure/{ResolveBinary,Argv,ChildEnv,Stdin}
--- FAIL: TestBuildPlanPrefersAnExplicitHomeOverTheDeclaredDefault
--- FAIL: TestLookupNormalizesAndRefusesGarbage
```

Restored byte-identically.

## F4 probe — `System.ID()`'s documented self-normalization is not enforced

```
registry key IDs()=[pangolin]
sys.ID() as returned by the plugin = "  PANGOLIN  "
plan.System = "pangolin"
```

`Register` accepted a plugin whose `ID()` does not normalize to itself
(`system.go:354-356` says it must). Registry-emitted values are all normalized;
the plugin's own accessor is not.
