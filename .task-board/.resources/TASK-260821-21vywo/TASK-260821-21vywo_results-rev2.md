# TASK-260821-21vywo rev 2 — rework against CR-TASK-260821-21vywo-1

Four blocking findings closed, plus one more hole the rework found while
reconstructing the reviewer's mutants. Work is **uncommitted** in the story
worktree.

Nothing on the reviewer's "confirmed, do not disturb" list was touched except
where F4 made a test's SHAPE unreachable; both such tests are named below with
what replaced them and why the replacement is stronger.

## Gates — foreground, real exit codes

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `gofmt -l pkg/ tools/` | 0, empty output |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 -race` | 0 |

---

# The four findings

## F1 — a local copy of `ID()` defeated the structural switch rule

`id := sys.ID(); switch id { case "opencode": }` fired nothing: for an id
outside `knownSystemIDs` there was no net at all, which falsified the file's own
claim that the structural rules work for undeclared ids.

**Fix.** `resolveIDCallSwitchTags` / `collectIDCallSwitchTags` / `idCallLocals`
in `singlesource_guard_test.go`. A name bound EXACTLY ONCE in a function body,
to an `ID()` call, and never written to again, resolves back to that call; a
switch whose tag is such a name reports `id-switch`. Resolution is inherited
into nested function literals, so a closure switching on an id its parent bound
reads the same as the parent doing it inline.

Anything that writes the name again — a second assignment, `++`, a range clause
binding it, taking its address — drops it rather than guessing. `isIDCall` also
now looks through a one-argument call, so `switch string(sys.ID())` and
`id := string(sys.ID())` are the same fact as the bare spelling.

**Claim/rule disagreement resolved, not papered over.** The threat model
(previously lines 59-61) said the structural rules remain live against all
residuals. That was false at rev 1 for this spelling. The prose is rewritten,
the residual list grew from three classes to five, and the two new residuals —
a REWRITTEN id variable and an id that leaves through a HELPER — are
demonstrated in `TestSingleSourceGuardResidualGaps`, not asserted.

## F2 — an `init()`-assembled table with plain string keys walked through

**Fix.** A new `*ast.AssignStmt` rule: an assignment whose left-hand side is an
index expression whose key resolves, through the existing `knownSystemIDOf`, to
a known system id, reports `binding-table`. Covers `shadow["codex"] = ...` from
an `init()`, from an ordinary function, and through a receiver's field.

**Residual class 3 stays open, as the reviewer required.**
`table[strings.Join(parts, "-")] = nil` is still not reported — the rule sees
the assignment but has no literal to resolve — and
`TestSingleSourceGuardResidualGaps/ids_assembled_at_runtime` is green.

## F3 — a function-local `const` escaped resolution

**Fix.** `resolveConstStrings` walks every node rather than `file.Decls`, so a
`const` inside a function body folds. `resolveVarStrings` got the same
treatment for symmetry, which closes the sibling hole (a function-local `var`
holding an id) that was open for the same reason.

**The corner is named, not hidden.** Names fold into ONE flat scope rather than
per-function. For consts that is imprecise in exactly one direction: two
function-local consts sharing a name with different values resolve to whichever
the fixpoint reaches first, so a key written as that name can be reported
against the other one's value. That is a rare FALSE POSITIVE — a violation
report naming a real line a reader can judge — not a silent miss, and it is
documented on `resolveConstStrings`. Vars are a set rather than a single value,
so same-named locals widen the set instead of racing for it.

## F4 — `System.ID()`'s "must normalize to itself" was prose

**Fix.** `Register` refuses when `NormalizeSystemID(sys.ID()) != sys.ID()`
(`ErrUnnormalizedSystemID`, `registry.go`), naming both spellings and telling
the author what to return.

**Two confirmed tests changed shape because F4 makes their old shape
unreachable.** Both replacements assert a strictly stronger fact:

| Was | Why it cannot stand | Now |
| --- | --- | --- |
| `TestRegisterNormalizesBeforeCheckingForDuplicates` — register `Pangolin`, expect `  PANGOLIN  ` to collide as `ErrDuplicateSystem` | neither spelling registers at all now | `TestRegisterRefusesAnIDThatDoesNotNormalizeToItself` (3 spellings; asserts both names in the error, `Len()==0`, and that the refused plugin is not reachable under its normalized id) + `TestRegisterAcceptsTheNormalizedSpellingOfTheSameID` as the control, which also asserts `sys.ID() == registry.IDs()[0]` |
| `TestPluginsCommandReportsWhatTheRegistryHolds` — register `Claude-Code` through the CLI's registry, assert `claude-code` on stdout | that registration is refused | the test keeps its role with canonical ids; the normalization property moves to `TestCLICannotSurfaceAnUnnormalizedPluginID`, which registers `Claude-Code` through the same public API from OUTSIDE the agentic package, asserts `ErrUnnormalizedSystemID`, and then drives the real root command and requires empty stdout |

Normalized `Lookup` (`"  PanGolin "` → the one registration) and duplicate
refusal are untouched and still covered.

---

# F5 (found during rework) — `make(bindings)` of a type the registry declares

Reconstructing the reviewer's **R10** — "make() of a named type declared in the
allowed file" — showed it **UNCAUGHT** against rev 2's first cut. The verdict
listed R10 as CAUGHT; on the spelling as described, with the type declared
inside `registry.go` and instantiated elsewhere, it is not. Reported rather
than quietly reconstructed as something else.

It is the same class as F2: an ordinary spelling with no map type and no
literal in the offending file. `var shadow bindings` and
`func boot() bindings` are the same hole.

**Fix.** Three rules over `bindingTypes`, which `resolveBindingTableTypeNames`
already computed: `make(T)` (`*ast.CallExpr`), a variable of type `T`
(`*ast.ValueSpec`), and a field or signature of type `T` (`*ast.Field`).

---

# Mutant matrix

## Set A — the guard's own rules, narrowed one at a time (the original 16)

Re-run in full against the reworked code by
`.temp/TASK-260821-21vywo/rerun_mutants.py`, which patches, runs a foreground
`go test`, and restores. Every source file was verified byte-identical to its
backup afterwards, and the post-restore control is green.

| # | Narrowing | Exit | What went red |
| --- | --- | ---: | --- |
| M0 | control, nothing mutated | 0 | — |
| M1 | shadow table planted in `tools/agents-management/cmd` | **1** | `TestSingleSourceGuardFindsNoSecondBinding` |
| M2 | id switch planted in `tools/agents-management/cmd` | **1** | `TestSingleSourceGuardFindsNoSecondBinding` |
| M3 | const indirection no longer resolves | **1** | 5 mutants, now including both function-local const spellings and R8 |
| M4 | var indirection no longer resolves | **1** | 5 mutants, now including the function-local var and R9 |
| M5 | `map[SystemID]T` only inside composite literals | **1** | 5 mutants + R1, R2, R12 |
| M6 | the `ID()` structural rule removed | **1** | 5 mutants: the direct spelling, both conversions, the closure copy, R6 |
| M7 | allowlist widened to every file | **1** | 13 table mutants + `TestSingleSourceGuardRulesFireOnRealCode` |
| M8 | `Register` stops refusing a duplicate id | **1** | `TestRegisterRefusesDuplicateID` |
| M9 | `Register` skips id normalization | **1** | 10 `RefusesUnnormalizableIDs` subtests + the 3 F4 subtests + the CLI refusal test |
| M10 | `Register` admits no launch modes | **1** | `TestRegisterRefusesSystemWithNoLaunchModes` |
| M11 | `CanCarry` true for every transport | **1** | `...DecidableFromTheContractAlone/none/required`, `TestCanCarryRefusesAnUndeclaredTransport`, `TestBuildPlanRefusesRequiredEffortUnderTransportNone` |
| M12 | composition admitted under `GrammarNone` | **1** | `TestBuildPlanRefusesCompositionUnderGrammarNone` |
| M13 | detached stdin bytes admitted | **1** | `...RefusesPluginContractViolations/stdin_bytes_while_reporting_nothing_attached` |
| M14 | empty resolved binary admitted | **1** | `...RefusesPluginContractViolations/empty_binary_with_no_error` |
| M15 | mode validity checked, declared support not | **1** | `TestBuildPlanRefusesUndeclaredLaunchMode` |
| M16 | required effort with no value admitted | **1** | `TestBuildPlanRefusesRequiredEffortWithNoValue` |
| M0' | control after restoring all 16 | 0 | — |

## Set B — the reviewer's 15 spellings, now permanent tests

`TestSingleSourceGuardAgainstReviewMutants` holds R1–R14 verbatim in shape, so
the verdict's matrix is reproducible instead of a claim about a scratch copy.

| # | Spelling | rev 1 | rev 2 |
| --- | --- | --- | --- |
| R1 | `type sysid = agentic.SystemID` alias as map key | CAUGHT | CAUGHT |
| R2 | `type sysid agentic.SystemID` named type as map key | CAUGHT | CAUGHT |
| R3 | `if/else if` chain with `==` on known ids | CAUGHT | CAUGHT |
| R4 | `init()` successive assignment, `map[string]T` | **UNCAUGHT (F2)** | **CAUGHT** |
| R5 | `init()` successive assignment, `map[SystemID]T` | CAUGHT | CAUGHT |
| R6 | switch on local copy of `sys.ID()`, unknown ids | **UNCAUGHT (F1)** | **CAUGHT** |
| R7 | switch on local copy of `sys.ID()`, known ids | CAUGHT | CAUGHT |
| R8 | function-local `const` in `==` | **UNCAUGHT (F3)** | **CAUGHT** |
| R9 | local slice indirection in a switch case | CAUGHT | CAUGHT |
| R10 | `make(bindings)` of a type declared in the allowed file | listed CAUGHT | **was UNCAUGHT on reconstruction (F5) — now CAUGHT** |
| R11 | `var shadow bindings`, no literal, split across two files | CAUGHT | CAUGHT (via the type declaration's own map type, which is outside the registry) |
| R12 | struct field `map[SystemID]T` outside the registry | CAUGHT | CAUGHT |
| R13 | `map[string]T` composite literal keyed by known ids | CAUGHT | CAUGHT |
| R14 | function-local map literal keyed by a known id | CAUGHT | CAUGHT |
| R15 | switch on a method named `Identifier()`, not `ID()` | UNCAUGHT, out of scope | UNCAUGHT — now a **declared NON-goal** with a case in `TestSingleSourceGuardResidualGaps`, so widening has to be argued |

## Set C — the new rules, each proven load-bearing by narrowing

Each new rule was disabled in isolation and the package re-run. Exactly the
mutants that rule exists for went red; the source file was restored
byte-identically after each.

| Narrowing | Went red |
| --- | --- |
| local-tag resolution off (F1) | the 4 undeclared-id local-copy spellings — the known-id one stays caught by the vocabulary net, as documented |
| one-argument conversion unwrap off | `string(sys.ID())` bound to a local, and inline in the tag |
| key-by-key assignment rule off (F2) | `init()`-assembled, ordinary-function with local consts, receiver-field |
| const walk back to package level (F3) | local const in `==`, local const as a case value, key-by-key with local consts |
| var walk back to package level | local var as a case value |
| named-binding-type rules off (F5) | R10 and `var shadow bindings` / `func boot() bindings` |
| `Register`'s normalization-identity refusal off (F4) | the 3 F4 subtests + `TestCLICannotSurfaceAnUnnormalizedPluginID` |

## New spellings caught, by set

51 mutant subtests green: 13 shadow-table, 18 id-switch, 14 reviewer,
6 declared-open residuals/non-goal. Plus the ordinary-code control, the
whole-module reach proof, and the empty-allowlist proof that the rules fire on
real code.

## The axolotl property, re-measured

Deleting the single `registry.Register(sys)` line in `registerPangolin`
(`double_test.go`) now reddens **27 tests** (18 top-level), up from 25 at rev 1
because rev 2 added subtests, spanning every dispatch surface and every
`BuildPlan` refusal. Restored byte-identically; the one-edit property holds.

---

# What was NOT changed

Everything the verdict confirmed by attack: the contract types and all 11
adapterTable fields, the four reforms (`Argv`/`ChildEnv`/`Stdin`,
`LaunchModeDryRun`, `GrammarID`, `DefaultHome`), `BuildPlan` as the single
dispatch site with its refusals, the module walk, the residual mechanism, the
CLI wiring, and the `pangolin` double. `NormalizeSystemID` remains the only
normalization in the module.

`docs/architecture.md` is unchanged: no invariant moved. `README.md` gained the
third `Register` refusal and the mutant-set split. `LOGBOOK.md` carries the
three entries the verdict asked for (1813, 1814, 1815) and amends 1812's
overstated claim in place rather than deleting it.
