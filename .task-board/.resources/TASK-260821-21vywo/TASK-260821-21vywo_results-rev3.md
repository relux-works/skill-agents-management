# TASK-260821-21vywo — CR revision 3 (final guard round)

Scope was exactly the four items in `TASK-260821-21vywo_review-verdict-rev2.md`:
F6 in code, F5/B2/B3 residualized-with-demonstration, F7's promise, F8's
comment. AC1/AC3/AC4 and the four closed rev1 findings were not touched.

## Gates — foreground, real exit codes, no pipes

| Gate | Exit | Result |
| --- | ---: | --- |
| `make vet` | 0 | PASS |
| `make build` | 0 | PASS |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 | PASS (`pkg/agentic` ok, `cmd` ok) |
| `gofmt -l pkg/ tools/` | 0 | clean, no output |

53 top-level tests, 85 subtests, 0 failures. Work left UNCOMMITTED.

## F6 — the if/switch asymmetry, CLOSED IN CODE

`*ast.BinaryExpr` now carries the structural net as well as the vocabulary one.
Both id rules ask one shared question — `dispatchesOnID`, over `isIDExpr` — so
"this expression delivers a system's ID() here" cannot be true for a `switch`
and false for an `if`. The comparison fires when one operand resolves to an
ID() and the other folds to a NON-BLANK compile-time string.

The blank carve-out is the false-positive surface the verdict asked me to weigh.
`sys.ID() == ""` is an emptiness check, not dispatch, and flagging it is what
gets a guard deleted. It is not left to a reader either: three shapes are now in
`TestSingleSourceGuardAcceptsOrdinaryCode` — `sys.ID() == ""` direct, the same
through a resolved local, and `a.ID() == b.ID()` where nothing folds on either
side.

Four permanent mutants added, all with ids OUTSIDE `knownSystemIDs`, which is
the precise defect the verdict named in its own matrix (every earlier if-chain
mutant used a known id, so the vocabulary net answered and the structural net
was never asked):

| Mutant | Shape |
| --- | --- |
| `an if-else chain on ID() naming ids nobody has declared yet` | C2 — `if sys.ID() == "opencode" { } else if sys.ID() == "some-future-harness" { }` |
| `the id copied into a local first, then compared, undeclared ids` | C3 — `id := sys.ID(); if id == "opencode"` |
| `the same comparison negated, over a converted local, undeclared id` | `id := string(sys.ID()); if id != "opencode"` |
| `a local copy call-wrapped in the switch tag, undeclared ids` | B3 — `id := string(sys.ID()); switch strings.TrimSpace(id)` |

### Non-vacuity, by NARROWING

**NARROW-F6** (scratch `/tmp/nv-21vywo`) — removed ONLY the structural half of
the BinaryExpr rule, left the vocabulary half and every other rule intact:

```
--- FAIL: .../an_if-else_chain_on_ID()_naming_ids_nobody_has_declared_yet
--- FAIL: .../the_id_copied_into_a_local_first,_then_compared,_undeclared_ids
--- FAIL: .../the_same_comparison_negated,_over_a_converted_local,_undeclared_id
54 subtests still PASS
```

Exactly the three new comparison mutants go red. **R3 and the two original
if-chain mutants stay green** — they use known ids, so the vocabulary net still
answers for them. That is the direct proof the two nets are independent on the
comparison rule, the same way NARROW-3 proved it on the switch rule last round.

**NARROW-B3** (scratch `/tmp/nv2-21vywo`) — restored the pre-fix switch-tag
condition (a local resolves only when the tag is a BARE ident), nothing else
changed:

```
--- FAIL: .../a_local_copy_call-wrapped_in_the_switch_tag,_undeclared_ids
```

Exactly one mutant red. The look-through is attributable and is not a blanket
widening — no other mutant depends on it.

## F5/B2/B3 — DEVIATION FROM THE BRIEF, stated plainly

The brief said to residualize B2 **and B3**. I residualized B2 and C1 and
**closed B3 in code** instead. The reason is the brief's own proposed threat-
model wording: *"single-hop resolved, multi-hop declared open."* B3's local is
`id := string(sys.ID())` — that is ONE hop. Filing it under a multi-hop residual
would have put a false sentence back into the file on the exact round whose
whole point is that the prose must be true line for line. Closing it does not
extend the resolver: `idCallLocals` is byte-identical, still single-hop, still
resolving RHS with `isIDCall`. What changed is the RULE SITE — the switch tag
now looks through one-argument calls to a resolved local, which is what it
already did for `switch strings.TrimSpace(string(sys.ID()))` without the local.
The asymmetry the verdict called "the sharp one" is gone in the direction that
removes a hole rather than the one that documents it.

The residual class is therefore named by hop count, which is what the code
actually keys on:

- **B2** `raw := sys.ID(); id := raw; switch id` — declared open, demonstrated.
- **C1** `id := sys.ID(); key := string(id); if key == "opencode"` — declared
  open, demonstrated. Both use undeclared ids so no other net rescues them.

The threat model now says six residual classes, splits them by which net they
defeat, and states the shape the multi-hop boundary does NOT cover: the hop
count, not the conversions — one hop plus any number of one-argument calls
resolves and is in the mutant set.

**The residuals are live, not decorative.** WIDEN-MULTIHOP (scratch
`/tmp/nv3-21vywo`) extended `idCallLocals` to a transitive fixpoint over
assignment chains — the extension the file says it does not do:

```
--- FAIL: TestSingleSourceGuardResidualGaps/a_two-hop_local:_the_id_copied,_then_copied_again
--- FAIL: TestSingleSourceGuardResidualGaps/a_two-hop_local_whose_second_hop_converts
    this residual is no longer open: pkg/launcher/residual.go:8 [id-switch] ...
    Update the threat model on singlesource_guard_test.go and move this case
    into the mutant set.
```

Both fail by name. The second fires through the NEW comparison rule, which
independently confirms F6's structural half is doing work.

The verdict's charge that the file's "declared open rather than left for a
reader to discover" claim had become the defect is answered in the file itself:
the list now says a class named there but not demonstrated in the gaps test
"would be this same defect one level up", and the gaps test says six classes
carry seven subtests and why the multi-hop class carries two.

## F7 — the promise: ENFORCED and REWORDED, both

I chose both halves rather than either, because each alone leaves something
untrue. `Register` now reads `ID()` twice and refuses a disagreement with a new
`ErrUnstableSystemID`, before normalization — an unstable plugin whose first
answer is also unnormalized should be told which rule it actually broke.

Then the promise was rewritten to what the code guarantees. `System.ID` no
longer says two spellings "can never" be seen. It says: the registry key IS the
spelling `ID()` returned at registration, for as long as the plugin keeps its
word; "forever" is the plugin's obligation, not the registry's check; and the
earlier wording claimed something a single read could not back. `Register`'s
doc and `ErrUnnormalizedSystemID`'s ("...the same string **at registration**")
were corrected the same way, and the error text's absolute clause is gone.

Three tests, negative first:

- `TestRegisterRefusesAnUnstableID` — the double with `unstableID` set answers
  `pangolin` once then `Pangolin`. Refused with `ErrUnstableSystemID`; the
  error names BOTH answers; `Len` stays 0; `Lookup` misses under both
  spellings; and `BuildPlan` at the real entry point returns `ErrUnknownSystem`,
  so the refusal reaches the planner rather than stopping at the registry.
- `TestRegisterAcceptsTheSameDoubleWhenItsIDIsStable` — the same double with
  the field unset registers, and asserts `calls["ID"] >= 2`, so the test cannot
  be satisfied by a Register that refuses everything or by one that never made
  the second read.
- `TestRegisterCannotSeeAnIDThatFlipsAfterRegistration` — the OTHER side of the
  bound, as a demonstration rather than a hope: a plugin mutated after a good
  registration is admitted, registry and plugin disagree, `BuildPlan` plans on
  the registry's spelling. If a future change starts catching this, the test
  fails and the promise gets rewritten with it.

### Non-vacuity of the new gate

| Mutant | Effect |
| --- | --- |
| **N-F7a delete** — stability check disabled | `TestRegisterRefusesAnUnstableID` RED, rest of `pkg/agentic` green |
| **N-F7b NARROW** — compare NORMALIZED values instead of raw, i.e. the gate covers only ids that differ after folding | `TestRegisterRefusesAnUnstableID` RED, rest green |

N-F7b is the narrowing that matters: `pangolin` → `Pangolin` folds to one value,
so a normalized comparison admits exactly the case class the reviewer's P3 probe
walked through. The gate covers the class, not just literal inequality.

(Both scratch runs also show `cmd/TestBuildOutputIsIgnoredAndSourcesAreNot` red;
that is the tarball copy having no `.git`, not a real failure — it is green in
the worktree, where the suite exits 0.)

## F8 — comment fixed

`double_test.go` named `unstableCaps`, which did not exist. It now names
`unstableID`, which does — the field F7's negative test needed, so the comment
was corrected by making it true rather than by deleting the third name.

## Docs

`README.md`: the registry bullet now lists the stability refusal and says which
half of `System.ID`'s promise is enforced; the guard bullet's mutant count is
corrected from 32 to 49 (13 shadow-table + 22 id-switch + 14 review) and names
the second review's finding and the seven residual demonstrations.

## Files changed

- `pkg/agentic/singlesource_guard_test.go` — `isIDExpr`/`isIDCall` split,
  `resolveIDCallIdents`, `dispatchesOnID`, the BinaryExpr structural half, and
  the threat model.
- `pkg/agentic/singlesource_guard_mutants_test.go` — four mutants, two
  residuals, three ordinary-code control shapes, header history.
- `pkg/agentic/registry.go` — `ErrUnstableSystemID`, the double read, corrected
  doc claims.
- `pkg/agentic/registry_test.go` — three tests for the gate and its bound.
- `pkg/agentic/double_test.go` — `unstableID` field, F8 comment.
- `pkg/agentic/system.go` — `System.ID`'s promise.
- `README.md` — the two claims above.

`plan.go`, `contract_test.go`, `plan_test.go`: zero changed lines.
