# Review verdict — TASK-260821-21vywo, CR revision 2

**Verdict: CHANGES REQUESTED → `to-dev`.**

All four rev1 findings are genuinely closed. Two of the three F1-boundary
probes the brief named by hand are open and **undeclared**, which is the one
condition the brief attached to ACCEPT: *"Ordinary spellings must be caught or
explicitly residualized — two-hop locals may legitimately be a residual, but
the file must SAY so."* They are not said. A third, adjacent, hole has no
mutant covering it at all. The rework is small and fully specified below.

## Provenance of this evidence

- Candidate tree: `8723bd2f9ddc63fb83cfd0eb727874ba01c6d0f9`, base
  `ada4f02446b1124daf746b19eaa9bbcdee5192c7`.
- The worktree was verified byte-identical to the candidate tree before and
  after review (`git write-tree` over a scratch index → `8723bd2f…` both
  times). Every mutant ran in throwaway copies: `/tmp/rev-21vywo-scratch`
  (rev 2) and `/tmp/rev1-tree` (rev 1, reconstructed by applying
  `TASK-260821-21vywo_change-request_rev1.patch` to the base). Nothing was
  mutated in the candidate.
- Comparative results come from ONE harness file compiled unchanged into both
  trees, calling `scanSingleSource` directly, so no claim depends on either
  revision's own mutant helpers.

## Gates — foreground, on the candidate tree

| Gate | Result |
| --- | --- |
| `make vet` | PASS |
| `make build` | PASS |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | PASS (`pkg/agentic` ok, `cmd` ok) |
| `gofmt -l pkg/ tools/` | clean |

Work left UNCOMMITTED.

## 1. The four rev1 findings — re-run as written, rev1 vs rev2

Same file, both trees:

| Mutant | rev 1 | rev 2 |
| --- | --- | --- |
| F1/R6 `id := sys.ID(); switch id`, undeclared ids | UNCAUGHT | **CAUGHT** — id-switch, `f1.go:7` |
| F2/R4 init()-assembled `map[string]T`, the adapterTable shape | UNCAUGHT | **CAUGHT** — binding-table ×2, `f2.go:9,10` |
| F3/R8 function-local const in `==` | UNCAUGHT | **CAUGHT** — id-switch, `f3.go:7` |
| F4 raw-ID registration | admitted | **REFUSED** — `ErrUnnormalizedSystemID`, names both spellings |

All four RED→GREEN. Non-vacuity proved by narrowing, not deleting:

- **NARROW-1** — removed only the `*ast.AssignStmt`/`IndexExpr` rule. Exactly
  four mutants went red (`initassign`, `localconstassign`, `fieldassign`, R4);
  R5 stayed green on the `map[SystemID]T` rule. The rule discriminates.
- **NARROW-3** — removed only `idTagSwitches` from the switch rule. R6
  (undeclared ids) red, **R7 (known ids) stayed green**. That is the direct
  proof that the structural net and the vocabulary net are independent, which
  is the claim the whole threat model rests on.
- **N1 (F4, narrowing)** — gate weakened to `raw != id && TrimSpace(raw) !=
  raw`, i.e. whitespace-only. `Pangolin` went RED; `  PANGOLIN  ` and
  `pangolin ` stayed green. The gate covers the case class, not just trim.
- **N2 (F4, regression)** — refusal removed so the id folds silently: all
  three subtests of `TestRegisterRefusesAnIDThatDoesNotNormalizeToItself` red.

## 2. R10 — settled. The producer is right; my rev1 verdict carries a wrong row.

My rev1 log line records its own refutation:

```
RESULT R10 ... -> CAUGHT: pkg/agentic/r10.go:3 [binding-table] type bindings: a map keyed by SystemID
```

`where: type bindings`, line 3 — my rev1 mutant file **redeclared**
`type bindings map[SystemID]System` inside the non-allowed file, and the
`MapType` rule fired on that redeclaration. `make(bindings)` was never what got
caught. Re-running both spellings settles it:

| Spelling | rev 1 | rev 2 |
| --- | --- | --- |
| R10 as I wrote it (mutant file redeclares the type) | CAUGHT — on the redeclaration, `r10.go:3` | CAUGHT |
| R10 true (type declared ONLY in the allowed file, `make()` elsewhere) | **UNCAUGHT** | **CAUGHT** — `r10true.go:3` |

**My rev1 verdict row "R10 … CAUGHT" is wrong** — the mutant did not test the
claim its name made. The producer reconstructing it, finding it uncaught, and
*reporting the discrepancy instead of rewriting the mutant* is the correct
handling. R11 has the same defect in my rev1 log (`r11a.go:3 type bindings`)
and is likewise now genuinely covered. No ACCEPT criterion this round relies on
either row.

## 3. The reshaped tests — the old assertions really are unreachable

`r.systems[id] = sys` (registry.go:127) is the **only** write to the map in the
whole module — `grep -rn --include='*.go' -e '\.systems' -e 'systems\['`
returns eight hits, one write, downstream of the gate. The field is unexported,
there is no exported constructor taking a map, and no test helper touches it:
`withRegisteredPlugins` in `cmd` registers through the public `Register`.
Driven, not just read:

| Probe | Result |
| --- | --- |
| P1 zero-value `Registry{}`.Register(unnormalized) | REFUSED |
| P2 package-level `Register` → `Default` | REFUSED; `Default.Lookup` misses |
| P5 second spelling after a good registration | REFUSED, `Len` stays 1 |

The replacements catch the regression that re-opens direct insertion (N2 above,
three subtests red), and `TestCLICannotSurfaceAnUnnormalizedPluginID` drives
the real `plugins` command and asserts the refused plugin surfaces under
*neither* spelling. Both replacements are strictly stronger than what they
replaced.

## 4. The matrix — spot-checked as re-run

R1–R14 all green on re-run. Three checked by re-running my own spellings
against both trees rather than reading the table (R4, R6/R7, R8 — section 1),
plus R10/R11 in section 2. Attribution proved by NARROW-1 and NARROW-3.

R15 is in `TestSingleSourceGuardResidualGaps` as a declared NON-goal, and it is
live, not decorative: widening `isIDCall` to accept `Identifier` makes it fail
by name —

```
this residual is no longer open: pkg/launcher/residual.go:6 [id-switch] func argv:
a switch on a system's ID()... Update the threat model...
```

## What has to change

### F5 — two of the three named F1-boundary probes are open and undeclared

| Probe | rev 2 |
| --- | --- |
| B1 `if id := sys.ID(); …` then `switch id` | CAUGHT |
| **B2** `raw := sys.ID(); id := raw; switch id` | **UNCAUGHT** |
| **B3** `id := string(sys.ID()); switch strings.TrimSpace(id)` | **UNCAUGHT** |
| B4 `switch strings.TrimSpace(string(sys.ID()))` (no local) | CAUGHT |
| C1 `id := sys.ID(); key := string(id); switch key` | **UNCAUGHT** |

All use ids outside `knownSystemIDs`, so no net fires. B3 vs B4 is the sharp
one: the *same* call-wrapped tag is caught without the local hop and missed
with it.

None of these is covered by the five declared residuals. Residual 4 is about a
**rewritten** name — "a second assignment, a range clause, taking the address."
In B2/C1 the name is bound exactly once and never written again; it is dropped
because its RHS is another local rather than a literal `ID()` call, a mechanism
the file never states as a boundary. Residual 5 is about crossing a **function**
boundary; B2/B3/C1 are all inside one body.

The file's claim is *"Five residual classes are therefore DECLARED OPEN rather
than left for a reader to discover."* A reader who trusts that list concludes
these are handled. That is the same failure the file was rewritten to avoid,
one level up.

Either catch them or declare them — residualizing is legitimate per the brief,
but then the threat model must name the class and
`TestSingleSourceGuardResidualGaps` must demonstrate it, so a future fix trips
the test the way the call-wrapped-literal residual already does.

### F6 — the structural net has no if-chain rule at all (no mutant covers this)

| Probe | rev 2 |
| --- | --- |
| C2 `if sys.ID() == "opencode" { } else if sys.ID() == "some-future-harness" { }` | **UNCAUGHT** |
| C3 `id := sys.ID(); if id == "opencode" { }` | **UNCAUGHT** |
| C4 same shape, `"codex"` (a known id) | CAUGHT — vocabulary net |

The `*ast.BinaryExpr` rule resolves only through `knownSystemIDOf`; it never
consults `isIDCall`. So dispatch on an id nobody has declared yet is caught
when spelled `switch` and missed when spelled `if`. R3 covers if-chains only
with *known* ids, which is exactly why the mutant set never exposed the
asymmetry — this is a hole in the matrix, not only in the prose.

This one carries code weight, and closing it looks cheap: also test
`isIDCall(node.X)`/`isIDCall(node.Y)` in the `BinaryExpr` case. Weigh the
false-positive surface yourself — `if sys.ID() == ""` is the shape to think
about — and if you decide against catching it, say so as an explicit NON-goal
alongside the `Identifier()` one, with a mutant that has undeclared ids in an
if-chain so the boundary is demonstrated rather than assumed.

### F7 — `System.ID`'s new docstring claims more than the gate enforces

rev 2 added to `system.go`: Register's refusal means "a caller holding the
plugin and a caller reading the registry **can never** see two spellings of one
system." `Register` reads `sys.ID()` once. A plugin whose `ID()` is unstable
walks through:

```
RESULT P3 Register(unstable ID) err -> <nil>
RESULT P3 registry key=[pangolin] ; plugin now answers "Pangolin" ; lookup ok=true
RESULT P3 -> BYPASS: one system, two names (registry "pangolin" vs plugin "Pangolin")
RESULT P4 BuildPlan under an unstable plugin -> plan.System="pangolin" err=<nil>
```

That is precisely the outcome the sentence promises is impossible, and the
identical sentence is in `ErrUnnormalizedSystemID`'s own error text. Full
enforcement is not achievable — a plugin can flip on the third call — so the
honest fixes are either to soften the claim to what the gate does cover (the
spelling `ID()` returns at registration), or to read `ID()` twice and refuse a
disagreement and then say that is what "stable" means here. Do not leave the
absolute claim standing over a single read.

### F8 — trivial

`double_test.go:38` names three fields — "emptyBinary, detachedStdinBytes and
`unstableCaps`" — and only two exist.

## What is NOT being asked for

AC1, AC3 and AC4 stand as accepted at rev 1 and are untouched by this delta
(`plan.go`, `contract_test.go`, `double_test.go`, `plan_test.go`: zero changed
lines rev1→rev2). Re-verified anyway this round: the dispatch-surface proof is
reflection-driven off the `System` interface so it cannot go stale;
`effortAdmission` compiles against `Capabilities` and `Model` alone and refuses
required-effort under `EffortTransportNone`; `TestTestDoubleExistsOnlyInTests`
holds. The registry design, the CLI's move onto it, and the guard's
architecture are all right. This is a bounded fourth pass over one test file's
threat model, one `BinaryExpr` rule, and two comments.
