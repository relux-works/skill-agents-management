# TASK-260821-atcotl — review verdict: CHANGES REQUESTED

**Verdict:** changes requested → `to-dev`. Three test-coverage gaps, no production
code change required. The implementation is correct today in all three cases;
what is missing is the negative assertion that would notice if it stopped being.

- Candidate tree: `01251f9786ee82cb721f589d27c6e0cdecd33273` (verified: the
  worktree hashes to exactly this, before and after the review).
- Base: `6223c283d36018b3cf1c7e1c1256e6964b4e3360`; task delta measured against
  the accepted Layer-1 commit `014a6fa`.
- All mutation was done in a throwaway copy at `/tmp/atcotl-scratch`. Nothing in
  the candidate was edited.

## Gates — run foreground in the candidate worktree

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 (4 ok, `main` has no test files) |
| `gofmt -l pkg/ tools/ internal/` | 0, no output |

Real binary smoke: `runtimes`, `runtimes --json`, `vendors --json`, `plugins --json`
all behave as the producer's artifact documents. `muse` prints `vendor unresolved`
with its provenance carried into the JSON.

## What must change

Three narrowing mutants SURVIVED the shipped suite. Each is a gate whose
existence is proven and whose BOUND is not — the delete-only-mutant shape. All
three are killed by tests attached as
`TASK-260821-atcotl_reviewer-attack-tests.go.txt`; lifting the relevant cases out
of that file (or writing equivalents) is the whole fix.

### 1. `ErrRuntimeVendorUnresolved` and `ErrRuntimeVendorUnregistered` are not proven distinct

**Shape: positive-path-only evidence on the claim the story hands downstream.**

```
-	ErrRuntimeVendorUnresolved = errors.New("vendorplugin: runtime's vendor was never established")
+	ErrRuntimeVendorUnresolved = ErrRuntimeVendorUnregistered
```

`env -u TASK_BOARD_DIR go test ./... -count=1` → **exit 0**. Nothing noticed.

After that edit `errors.Is(err, ErrRuntimeVendorUnresolved)` and
`errors.Is(err, ErrRuntimeVendorUnregistered)` both answer true for BOTH cases,
so a caller can no longer branch on the difference — and the muse row starts
telling an operator to install a vendor nobody has identified, which is the exact
failure `runtime.go:34-56` and results-artifact decision #2 exist to prevent.

`TestResolutionRefusalsAreDistinctFacts` (runtime_test.go:109) is named for this
property and does not test it: each of its four subtests makes a POSITIVE
`requireErrorIs` assertion and none asserts the branches are mutually exclusive.
`TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms` is likewise
positive-only.

Note the related ordering mutant — moving the `VendorResolved()` check below the
`r.Lookup` — IS killed (`TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms`).
So the realistic regression shape is covered and the sentinel identity is not.
This is the highest of the three because the story integrates to main next and
the muse representation becomes the port stories' ground truth.

**Fix:** in `TestResolutionRefusalsAreDistinctFacts`, add the negative half —
resolving muse must NOT be `ErrRuntimeVendorUnregistered`, and resolving a
runtime whose vendor plugin is simply absent must NOT be
`ErrRuntimeVendorUnresolved`. Reference: `TestAttackMuseRefusalIsATypedDifference`
and `TestAttackRuntimeNamingAnUnregisteredVendor` in the attached file.

### 2. The effort vocabulary's exact-match bound is unpinned

**Shape: the check is proven to exist, not the class it covers.**

```
 	for _, declared := range e.Vocabulary {
-		if declared == trimmed {
+		if strings.EqualFold(declared, trimmed) {
```

→ **exit 0**. `EffortDeclaration.Accepts` (vendor.go:228-243) documents the rule
and its reason in its own comment: folding `"High"` into `"high"` here would be a
second normalization of a value this package does not own — invariant 5. After
the widening, `BuildLaunch` admits `"Deep"`/`"DEEP"` and the vendor is handed a
word it never published.

The producer's `effort-vocabulary-unchecked` mutant DELETES the check (killed by
two tests). A widening survives, which is the case the rule is actually about.

**Fix:** drive `BuildLaunch` with `"Deep"`, `"DEEP"` and expect
`ErrEffortNotInVocabulary`. Reference: `TestAttackEffortVocabularyIsNotFolded`.

### 3. Rank evidence accepts whitespace-only source/observation once narrowed

**Shape: the same narrowing discipline applied everywhere else, missed here.**

```
 	for i, evidence := range r.Basis {
-		if strings.TrimSpace(evidence.Source) == "" {
+		if evidence.Source == "" {
 		...
-		if strings.TrimSpace(evidence.Observation) == "" {
+		if evidence.Observation == "" {
```

→ **exit 0**. `RankEvidence{Source: "   ", Observation: "\t"}` then satisfies
"a rank with no observation behind it is policy wearing a number".

`TestRegisterRefusesARankWithNoEvidence` covers empty/nil `Basis` only. The
producer applied exactly this narrowing discipline in two other places and it
holds there — `description-narrowed-to-nil-only` for `UsageDescription`, and my
own narrowing of `Broker.Checked`'s blank test was killed by
`TestRuntimeDeclarationValidation/a_blank_checked_source`. `CapabilityRank.Basis`
is the one that was missed.

**Fix:** add whitespace-only `Source` and `Observation` subtests, asserting both
`Model.Validate` and the production call site `Registry.Register`. Reference:
`TestAttackRankAndDescriptionWhitespaceAndRange`.

## What was attacked and HELD

Nothing below is a request; it is the evidence that the rest of the contract is
sound. Every item was executed, not read.

### ATTACK 1 — dependency direction and its bypasses

- AC2 refusal reproduces with both ids and the model that named them.
- The nil-Layer-1 bypass is genuinely closed: a registry built without an agentic
  registry refuses EVERY vendor (`ErrNoAgenticRegistry`) rather than skipping.
- **Registration order** (vendor first, system second): refused with
  `ErrUnknownAgenticSystem` naming both ids, then admitted once the system
  arrives. No deadlock, no silent accept.
- **Runtime declaring an unregistered vendor**: legal as a DECLARATION, refused at
  resolution with `ErrRuntimeVendorUnregistered`. Correct — declaration and
  resolution are deliberately different.
- **Post-registration vendor mutation**: the registry stores the plugin and
  `selectModel` re-reads `Models()`, so a mutated vendor CAN present a row that
  `Register` would have refused. This does NOT breach the dependency direction:
  bolting an unregistered system onto a model and declaring a runtime on it is
  refused at `ResolveRuntime` with `ErrRuntimeSystemUnregistered`, because a
  runtime cannot resolve on a system no plugin registered. The residue — a
  mutated row with a blank description reaching a plan — is the same bound the
  package already states for `Vendor.ID` stability ("contract, not enforcement")
  and is consistent with `pkg/agentic`. Not a finding; recorded so the port
  stories know the seam is where the comment says it is.

### ATTACK 2 — availability semantics

- **Zero-value-unknown is load-bearing.** Reordering the iota so healthy becomes
  the zero value is killed by `TestTheZeroVerdictIsUnknownAndNotServiceable` and
  `TestCheckAvailabilityDoesNotConvertAPluginErrorIntoUnknown`. The fail-open
  disease is genuinely closed.
- The fields ARE exported, so a struct literal can mint any verdict. That is the
  right call — the verdict is a comparable, loggable value and the gate belongs at
  the boundary. Eight forged verdicts driven through the PRODUCTION path
  (`CheckAvailability`) were all refused with `ErrVendorContract`: healthy with
  nothing checked; healthy over a failed read; limited with no clear time; limited
  with no observation; unknown carrying a clear time; unreachable with nothing
  behind it; healthy with a blank checked source; an undeclared state. Narrowing
  the gate to `State.Valid()` only is killed by
  `TestCheckAvailabilityRefusesAVendorsDishonestVerdict`. Dropping the
  clear-time-only-on-limited rule is killed too.
- The five future local-plane answers reproduce and validate today, unchanged
  interface: loaded/idle → Healthy; busy-until and waiting-on-eviction →
  LimitedUntil; daemon down → Unreachable; plane could not tell → Unknown with the
  plane in `Failures` (NOT Unreachable, NOT Healthy). Only the first is
  `Serviceable()`. Checked-and-empty, never-looked and read-failed stay three
  distinguishable facts.

### ATTACK 3 — muse and the six seeds

Read the extraction source directly
(`skill-project-management/pkg/remoteconfig/runtimeid/builtins.go`) and compared
every binding. **All six byte-exact:** claude=claude-code×anthropic,
codex=codex×openai, qwen=qwen-code×alibaba, gemini=gemini-cli×google,
agy=antigravity×google, muse=muse×UNKNOWN. `BrokerUnknown BrokerID = ""` in the
source and `VendorUnresolved VendorID = ""` here are the same representation, and
muse's provenance is checked-and-empty exactly as the source records it.

F2 attacked in both directions on FROZEN ids, not just on the test double:
matching redeclaration with completely different provenance prose is idempotent;
rebinding `claude` to (codex × openai) is refused naming both bindings and the
first stands; rebinding only the vendor half of `agy` is refused; **guessing a
vendor for muse over the frozen unresolved binding is refused** — an upgrade path
that would otherwise be a fabrication keyed into limit state.

Defensive copying holds at every exit: `FrozenRuntimes()`, `RuntimeDeclarations()`,
`RuntimeDeclarationOf()` all deep-copy the provenance slice, and a caller that
keeps a reference to the declaration it passed into `DeclareRuntime` cannot edit
the stored one through it.

### ATTACK 4 — rank without observation

`Position` 0 and negative, nil `Basis`, empty `Basis`, blank and whitespace-only
description, whitespace-only and empty model id, model id with an inner space, no
systems, unnormalized system spelling, the same system twice — all refused by
`Model.Validate` AND by the production call site `Registry.Register`. Duplicate
rank positions within one vendor ARE refused (`ErrDuplicateRank`, naming both
models); the mutant that admits them is killed. The only hole is finding 3 above.

The recommended effort is never substituted: a blank required effort is refused
naming the vocabulary and the recommendation, and the vendor's `Spawn` is not
reached at all, so there is no downstream point at which a plugin could inject a
default.

### ATTACK 5 — the guard extension

- The headline narrowing mutant reproduces exactly as claimed: flattening
  `bindingHomes` from per-key-type to a flat allowlist is killed by
  `TestSingleSourceGuardCatchesCrossLayerBindings` and by NOTHING else. That is a
  real narrowing proof, not a delete-only one.
- Planted five fresh violations the guard had never seen. All five caught, each
  naming the correct home file:
  1. `map[vendorplugin.VendorID]T` in a brand-new package directory
  2. `map[vendorplugin.RuntimeID]T` in the same
  3. a type ALIAS (`type vid = vendorplugin.VendorID`) used as a map key
  4. a DERIVED named type (`type derivedVendor vendorplugin.VendorID`)
  5. a named binding-table type, its composite literal, and a table held in a
     STRUCT FIELD rather than a package var
  Plus an `ID()` switch over vendor ids in `tools/agents-management/cmd/`.
  Ids outside `knownPluginIDs` were used throughout, so only the STRUCTURAL rules
  could have been firing — the vocabulary net cannot account for these.
- `internal/ident` really is the single normalization:
  `grep -rn "ToLower\|strings.Map\|'a' - 'A'"` over all non-test sources returns
  the one loop in `internal/ident/ident.go` and nothing else.
- The error-text byte-compatibility claim is TRUE — verified by running both the
  base (`014a6fa`) format string and the candidate's side by side over four
  inputs including one with embedded quotes: byte-identical in every case. It is
  not pinned by a test, so rewording `ident.Rule` would silently change
  `pkg/agentic`'s refusal text. Minor: callers branch on the
  `*InvalidSystemIDError` type and its `Reason`, not on the composed string, so
  no gate depends on it. Worth a line in the port stories, not a change here.

### ATTACK 6 — scope

Clean. `grep` over all production sources in `pkg/`, `internal/` and `tools/`
finds no `os/exec`, `exec.Command`, `net/http`, `syscall` or `os.StartProcess`.
`BuildLaunch` returns an `agentic.Plan` — data only. No concrete vendor plugin is
registered anywhere in production code (`vendors --json` → `[]`, which is the
answer, not a stub). No limit plane, no `providerlimits` leakage. Nothing
prejudges the port stories.

## Mutants run by this review

Ten, distinct from the producer's nineteen. Full harness attached as
`TASK-260821-atcotl_reviewer-mutate.sh`.

| # | Mutation | Result | Killed by |
| --- | --- | --- | --- |
| M1 | zero-value Availability becomes healthy | KILLED | zero-verdict + plugin-error tests |
| M2 | rank evidence blank check narrowed to `== ""` | **SURVIVED** | — (finding 3) |
| M3 | effort vocabulary widened to `EqualFold` | **SURVIVED** | — (finding 2) |
| M4 | duplicate rank position admitted | KILLED | `TestRegisterRefusesTwoModelsAtOneRank` |
| M5 | `ErrRuntimeVendorUnresolved` aliased to `…Unregistered` | **SURVIVED** | — (finding 1) |
| M6 | `CheckAvailability` validates state only | KILLED | `TestCheckAvailabilityRefusesAVendorsDishonestVerdict` |
| M7 | `bindingHomes` flattened to a flat allowlist | KILLED | `TestSingleSourceGuardCatchesCrossLayerBindings` ONLY |
| M8 | `Broker.Checked` blank check narrowed to `== ""` | KILLED | `TestRuntimeDeclarationValidation/a_blank_checked_source` |
| M9 | unresolved check moved below the vendor lookup | KILLED | `TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms` |
| M10 | unknown may carry a clear time | KILLED | `TestValidateRefusesVerdictsThatContradictTheirEvidence` |

All three survivors are killed by the attached attack tests.

## Acceptance criteria

| AC | State |
| --- | --- |
| AC1 — interface covers models + rank + description + efforts, structured availability, full spawn surface | met; no agentic type re-declared, `Launchable()` is the single projection |
| AC2 — unknown agentic system refused with both ids | met; refusal, per-model coverage and the nil-Layer-1 bypass all verified independently |
| AC3 — six historical ids as seed declarations with their runtimeid bindings | met; byte-exact against the source table, muse honestly unresolved |
| AC4 — verdict admits limited-until and unknown without an interface change | met; the five local-plane answers reproduce today |

All four ACs are satisfied by the implementation. The three findings are about
what the SUITE proves, not about what the code does — which is why this is
`to-dev` for three test additions rather than `analysis` or `blocked`.

## Route

`to-dev`. No production code change is expected. Add the three negative
assertions, re-run the four gates, and hand back. The work stays UNCOMMITTED in
the story worktree.

---

**Provenance note (added by the rev 2 re-review, RUN-260821-4e4a39):** this file
was briefly overwritten by the rev 2 verdict and restored from the full text read
in that same session before the overwrite. It is a faithful reconstruction of the
rev 1 verdict, not the byte-preserved original. The rev 2 verdict now lives at
`TASK-260821-atcotl_review-verdict-rev2.md`.
