# Review verdict — TASK-260821-21vywo, CR revision 3

**Verdict: ACCEPTED.**

Every claim the round was asked to check reconciles against a demonstrated
mutant, a narrowing, or a live residual test. The declared deviation is
justified by the evidence it claimed: my own rev2 B3 spelling is genuinely
caught now, so filing it under "multi-hop, declared open" would have planted
exactly the false sentence the round existed to remove.

## Provenance

- Candidate tree `b2a8125e23982ce2895ff1b7c63dffa9cb238524`, base
  `ada4f02446b1124daf746b19eaa9bbcdee5192c7`, branch
  `task-board/story/STORY-260821-224xfu`.
- The worktree was verified byte-identical to the candidate tree before AND
  after review by hashing every blob in the tree against the file on disk
  (`git hash-object` per path). No mutation touched the candidate. Every
  experiment ran in a throwaway copy: `/tmp/rev3-scratch` (baseline + probes),
  `/tmp/narrow-f6`, `/tmp/narrow-b3`, `/tmp/narrow-empty`, `/tmp/narrow-f7`,
  `/tmp/narrow-f7b`, `/tmp/widen`.
- Probe evidence comes from harness files I wrote (`zz_reviewer_*_test.go`),
  compiled into a scratch copy and calling `scanSingleSource` / the real
  `Registry.Register` directly. No conclusion below depends on the producer's
  own mutant helpers or its test double — the F7 probe uses an independent
  `flipper` plugin, not `pangolinSystem`.

## Gates — foreground, on the candidate tree

| Gate | Result |
| --- | --- |
| `make vet` | PASS |
| `make build` | PASS |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | PASS (`pkg/agentic` ok, `cmd` ok) |
| `gofmt -l pkg/ tools/` | clean |

Baseline: 57 guard subtests, 0 failures. Work left UNCOMMITTED.

## 1. My rev2 B-probes, re-run as written

Exact rev2 spellings, ids outside `knownSystemIDs`:

| Probe | rev 2 | rev 3 | Expected by the brief |
| --- | --- | --- | --- |
| B1 `if id := sys.ID(); … switch id` | CAUGHT | CAUGHT | — |
| B2 `raw := sys.ID(); id := raw; switch id` | UNCAUGHT | **UNCAUGHT** | residual, demonstrated |
| B3 `id := string(sys.ID()); switch strings.TrimSpace(id)` | UNCAUGHT | **CAUGHT** | claimed single-hop and closed |
| B4 `switch strings.TrimSpace(string(sys.ID()))` (control) | CAUGHT | CAUGHT | stays caught |
| C1 `id := sys.ID(); key := string(id); switch key` | UNCAUGHT | **UNCAUGHT** | residual, demonstrated |

**The deviation is justified.** The brief conditioned it on my original B3
spelling being genuinely caught, and it is — by the rule's application site,
not by a widened resolver. `idCallLocals` is byte-identical to rev 2 (no diff
hunk touches it), so `raw := sys.ID(); id := raw` still fails to resolve for
the same reason it did before: `isIDCall` is `isIDExpr` with the local set
withheld, and it does not read names. B2 and C1 remain open and are now the
`MULTI-HOP` residual class, named by the mechanism (hop count) rather than by
the conversions around it.

**NARROW-B3** — the switch tag restored to rev 2's shape (`isIDCall(tag)` OR a
tag that is *directly* a resolved `*ast.Ident`), nothing else touched:
**exactly one** subtest red, `a local copy call-wrapped in the switch tag,
undeclared ids`. The rule bound is where the file says it is.

## 2. F6 — both spellings, future id, plus the carve-out

| Probe | rev 3 |
| --- | --- |
| `id := sys.ID(); if id == "opencode"` | **CAUGHT** (comparison rule) |
| `id := sys.ID(); switch id { case "opencode" }` | **CAUGHT** (switch rule) |
| `if sys.ID() == "opencode" { } else if sys.ID() == "some-future-harness" { }` | CAUGHT, both arms |
| `if sys.ID() == ""` | UNCAUGHT — carve-out holds |
| `id := sys.ID(); if id == ""` / `!= ""` | UNCAUGHT — carve-out holds through a local |
| `a.ID() == b.ID()` | UNCAUGHT — nothing folds on either side |

The two spellings now ask the same question, which was the finding.

**NARROW-F6** — the structural half of the comparison rule removed
(`if !dispatchesOnID(node.X) && !dispatchesOnID(node.Y)` → `if true`), the
vocabulary half untouched: **exactly the three new comparison mutants red**,
and every known-id if-chain mutant stayed green. That is sieve independence
demonstrated on this rule the same way NARROW-3 demonstrated it on the switch
rule at rev 2 — the two nets are not answering for each other.

**NARROW-EMPTY** — the blank carve-out removed
(`if !ok || strings.TrimSpace(value) == ""` → `if !ok`):
`TestSingleSourceGuardAcceptsOrdinaryCode` goes red naming *both* spellings in
`func registered` (clean.go:29 direct, clean.go:33 through a local). The
carve-out is held by a test, not asserted in a comment.

I also swept the comparison rule across contexts the producer did not name.
All CAUGHT with an undeclared id: tagless `switch { case sys.ID() == "…" }`,
`if x := sys.ID(); x == "…"`, `return sys.ID() == "…"`, `ok := sys.ID() == "…"`,
comparison against a package-level or function-local `const`. No false
positive on `const marker = "ready"; if marker == "ready"` or on
`switch mode` in a body that also binds an id.

## 3. Prose-vs-matrix reconciliation — the accept condition

Read line for line against the threat model. Every claim carries something.

| Claim in the guard file | Evidence |
| --- | --- |
| "Six residual classes … `TestSingleSourceGuardResidualGaps` demonstrates each one" | 8 subtests: 7 residual (multi-hop carries 2) + 1 NON-goal. Counted, named, all green. |
| "One hop plus ANY NUMBER of one-argument calls resolves and is in the mutant set" | Verified at 2 and 3 nested calls, both as a switch tag and as a comparison operand: CAUGHT. Permanent mutant `a local copy call-wrapped in the switch tag, undeclared ids`. |
| "two hops does not resolve and is here" | B2 and C1 UNCAUGHT; both are gaps subtests. |
| FIRST THREE residuals "evade the VOCABULARY net only … the structural rules are untouched by all three" | Each of the three residual sources retyped `map[SystemID]builder`: all three CAUGHT `[binding-table]`, with no literal resolved. |
| LAST THREE residuals: "Only the vocabulary net can fire there, and only for an id this file already knows" | All four spellings (rewritten, two-hop, two-hop-converting, helper) re-run with `"codex"`: all CAUGHT by the vocabulary net. With `"opencode"`: all UNCAUGHT. |
| "All three are demonstrated in the gaps test with ids OUTSIDE the vocabulary" | Every gaps source uses `"opencode"`; not in `knownSystemIDs`. |
| Residuals are LIVE, not decorative | WIDEN experiment: `idCallLocals` given a fixpoint over assignment chains (multi-hop resolution). Both new residual subtests go red **naming themselves**: "this residual is no longer open: … (declared open because id resolution is single-hop …). Update the threat model". Nothing else red. |
| "const and var indirection resolve at package level AND inside function bodies" | package-var, local-var and package-const behind a table key: CAUGHT. Package-var and local-var as a comparison operand: CAUGHT. |
| Mutants header: "Every one of them is red under a narrowing that removes only the rule it names" | NARROW-F6 reds three; NARROW-B3 reds the fourth. Union = all four new mutants, disjointly. |
| README: "49 mutants" | 13 + 22 + 14 = 49. Exact. |
| README: "Seven further cases demonstrate the residual classes staying OPEN" | 7 residual subtests + 1 NON-goal = 8 total. Exact. |
| `TestSingleSourceGuardRulesFireOnRealCode` | Present and green — the module scan is not vacuously silent. |

This is the condition both prior rounds lost on. It holds.

## 4. F7 — resolved BOTH ways, and both halves verified

The producer took enforcement *and* rewording, which is the right call: either
alone still leaves a sentence the code does not back.

Driven through the real `Registry.Register` with **my own** unstable plugin,
not the producer's double:

```
F7 P3 Register(unstable) err=agentic: system id is not stable across reads:
      ID() returned "pangolin2" and then "Pangolin2"; …   is(ErrUnstableSystemID)=true
F7 P3 registry IDs=[] Len=0
F7 P3 Lookup("pangolin2") ok=false ; Lookup("Pangolin2") ok=false
F7 P4 BuildPlan System="" err=agentic: no system registered: pangolin2
F7 NEVER-AGREES err=…ErrUnstableSystemID…
```

That is my exact rev2 bypass — `Register` admitting, `Lookup` serving a second
name, `BuildPlan` planning around the disagreement — now closed. Contrast rev2:
`Register(unstable ID) err -> <nil>`.

The unenforceable half is a demonstrated residual, not a caveat:

```
F7 THIRD-CALL Register err=<nil> ; registry key=[pangolin3] ; plugin now answers "Pangolin3"
```

and `TestRegisterCannotSeeAnIDThatFlipsAfterRegistration` asserts exactly that,
so a future change that starts catching it fails the test and forces the
docstring to be rewritten with it. `System.ID`'s comment now says which half is
enforced and which is the plugin's obligation, and explicitly retracts the
earlier absolute claim. `ErrUnnormalizedSystemID`'s error text no longer
carries it either.

Non-vacuity, by narrowing rather than deleting:

- **NARROW-F7** — gate weakened to `again != raw && again == ""` (only a blank
  second answer): `TestRegisterRefusesAnUnstableID` red. The gate covers the
  case class, not one spelling.
- **NARROW-F7b** — the logbook's own claimed narrowing, comparing NORMALIZED
  values instead of raw ones: `TestRegisterRefusesAnUnstableID` red. The
  logbook entry reproduces.
- Positive control is in the delta: `TestRegisterAcceptsTheSameDoubleWhenItsID
  IsStable` registers the same double with `unstableID` unset and asserts
  `calls["ID"] >= 2`, so "refuses everything" cannot satisfy the gate test.

F8 (the `unstableCaps` comment naming a field that did not exist) is fixed —
the three fields the comment names all exist now, and `unstableID` is the one
the new gate refuses.

## 5. Escapes found this round — for the threat model, not for rework

Per the brief, escapes beyond this round live in the threat model. I attacked
past the four checks; three ordinary spellings walk through and are in no
declared class. Recording them so the next person inherits a known quantity:

| Spelling | Undeclared id | Known id | Class it belongs to |
| --- | --- | --- | --- |
| `strings.EqualFold(string(sys.ID()), "opencode")`, `HasPrefix` — dispatch through a comparison FUNCTION rather than `==` | UNCAUGHT | UNCAUGHT | new: neither net has a rule site at a call argument |
| `var shadow = []pair{{id: "codex", b: nil}}` — a binding table as a slice of id/behaviour structs, no map anywhere | UNCAUGHT | UNCAUGHT | new: the composite-literal rule keys on map KEYS; a struct field VALUE is not a rule site |
| `want := "opencode"; if id == want` — the literal side behind a local var | UNCAUGHT | CAUGHT | structural half resolves the id side, but `foldStringConst` folds consts, not vars |

None is an AC failure. AC2 asks the guard to fail on a planted second table and
a planted ID-switch; it does, 49 mutants deep, and `struct{ byID map[SystemID]
builder }` — the field-typed table — is caught. These are the next boundary,
and the honest place for them is the residual list on the next pass over this
file, exactly as the multi-hop class landed there this round.

## 6. What was NOT re-litigated

AC1, AC3, AC4 stand as accepted at rev 1 and re-verified at rev 2; the rev3
delta touches `double_test.go` only to add `unstableID` and fix the F8 comment
(`contract_test.go`, `plan.go`, `plan_test.go`: zero changed lines rev2→rev3).
The full suite green on the candidate covers them.

Handoff: acceptance evidence only. This run supplied no `commit_ack`; the
commit and the `done` transition are the orchestrator's.
