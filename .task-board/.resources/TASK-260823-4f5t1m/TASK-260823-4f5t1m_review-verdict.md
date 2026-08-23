# TASK-260823-4f5t1m — review verdict: ACCEPTED

Change Request `CR-TASK-260823-4f5t1m-1` revision 1, `repository_delta=present`,
13 changed paths.

Candidate tree confirmed before and after every check:
`88a2495b8c78988b38691c1e7a8d7efb6d8b3050`, recomputed from the working tree
against `HEAD=b722ace`. It matched at the start of the review, after the
producer's own ten-mutant harness ran in this worktree, and at the end. Nothing
in this review mutated the candidate: all mutation work ran either in a scratch
`rsync` copy or through a harness that restores in a `finally`.

## Gates, foreground, in this worktree

| Command | Result |
| --- | --- |
| `make vet` | clean |
| `make build` | clean |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | all 24 packages ok, 13.9s |
| `make regress` | ok, **0.454s** (repeat runs 0.41–0.50s) |
| `gofmt -l pkg/ internal/` | no output |

`make regress` is 14 top-level tests / 60 including subtests. It is in
`spawn.worktree_isolation.validation.commands` in the candidate; the gate list
resolves from the MAIN checkout, which still carries the old three — so the new
command starts gating the NEXT landing. The README states this bootstrapping
shape explicitly rather than leaving it to be discovered, and I ran the command
by hand for this review, so the disclosure is honest and the evidence exists.

## The regress harness — attacked, not read

The producer's harness (`.temp/TASK-260823-4f5t1m/mutants.py`) reproduces from
scratch in this worktree: **10/10 mutants caught, 9 of them narrowing a
production gate rather than deleting it**, each naming the test that fired.

I re-derived three mutants independently, by hand, in a scratch copy:

| Mutant | Shape | Result |
| --- | --- | --- |
| `absentIsHealthy` returns Unknown instead of Healthy — i.e. someone silently FIXES fail-open | behaviour change, opposite direction | RED — `TestAMovedIdentityKeyMakesTheSuppressionVanishFailOpen`, "the same bytes filed one hex digit away still read unknown; this test no longer demonstrates the fail-open behaviour it exists to demonstrate" |
| Refusal message names only the SYSTEM, dropping the vendor (the complement of the producer's own only-the-vendor mutant) | narrow | RED — `TestAVendorNamingAnUnregisteredSystemIsRefusedNamingBothIDs`, per vendor, quoting the truncated message |
| Conflicting redeclaration returns `ErrRuntimeConflict` **and writes anyway** | narrow | RED — `TestAConflictingRedeclarationIsRefusedAndTheFirstStands`, "the refused declaration won anyway", on both axes for all six frozen ids |

On the specific question the brief raised: the fail-open pin is phrased as
DOCUMENTATION of current behaviour, not as an endorsement — the comment calls
it "THE DISASTER, asserted rather than described", the failure text says the
test has stopped demonstrating what it exists to demonstrate — and it does go
red when fail-open is fixed, so a silent fix cannot pass. Behaviour-change
detection cuts both ways, as required.

The four classes are each present and each carries a real negative, not a
positive-path pair:
- **Registration** drives the four REAL vendor plugins into a registry built on
  an EMPTY `agentic.Registry`, and closes the "refuses everything" loophole with
  an admission half using the real plugin values from `agentic.Default`. The
  third state (nil agentic registry → refuse, never admit) is covered too.
- **Declaration** covers both directions plus the assertion that actually
  matters — the stored binding is unchanged after the refusal.
- **Availability** derives the verdict from bytes a DIFFERENT binary wrote
  (`pkg/providerlimits/testdata/source-written/`, with a manifest), recomputes
  `IdentityKey` rather than trusting the manifest, and reads the window out of
  the fixture's own bytes. Elapsed-window and unreadable-file arms narrow the
  bound from both sides; the read-failure arm keeps absence and failure-to-read
  separate facts.
- **Parity** rebuilds one golden per Layer-1 system through the real registry
  and `agentic.BuildPlan`, with two planted defects applied to all six and each
  required to be reported in the field it was planted in, plus a test that fails
  if a seventh registered plugin lands with no smoke case.

Both manifest-driven and registry-driven loops `t.Fatal` on an empty set, so
none of them can range over nothing and report green.

## SKILL.md followed cold, as a consumer

Wired a scratch module from SKILL.md and `docs/consuming-the-module.md` alone:
`require github.com/relux-works/skill-agents-management v0.1.0`, `go work init`
+ `go work use <sibling>`, the blank-import block verbatim.

- Works as written. Registries populate: 6 systems, 4 vendors, **41 model rows**
  — counted independently, matching the ledger's 41.
- The doc's own `DeclareRuntime` example for `qwen-codex` compiles and succeeds
  verbatim, and the declaration reads back.
- "Importing a vendor pulls in the systems its models declare, so a half-wired
  binary is not reachable" — attacked by importing ONLY `vendors/anthropic`:
  `systems: [claude-code]`. Claim holds.
- The effort gate refuses blank ("model requires an explicit reasoning effort …
  accepts [low medium high xhigh max]") and out-of-vocabulary ("not one of …"),
  so "no default is injected at any call site" holds.
- One documented step is load-bearing and easy to miss: without
  `GOPRIVATE=github.com/relux-works/*` the build fails at sum.golang.org even in
  workspace mode. It IS documented in `consuming-the-module.md`; noting it only
  because SKILL.md's short form does not repeat it and links out instead.
- `-mod=mod` and an active `go.work` are mutually exclusive ("-mod may only be
  set to readonly or vendor when in workspace mode"). Both are documented, in
  different sections, without noting they cannot be combined. Not wrong; a
  consumer meets it in five seconds and Go's message says the fix.

**CLI surface matches the doc exactly.** Four own commands plus cobra's
`help`/`completion`. `plugins` and `vendors` print nothing and exit 0; `--json`
prints `[]` not `null`; `runtimes` prints all six as `id⇥system⇥vendor` with
`muse` reading `vendor unresolved`, and the JSON form carries the broker
provenance. No `spawn`, no `availability`, no `models`, as stated.

Frontmatter matches the house convention byte-for-byte in shape against
`skill-project-management/SKILL.md`: folded `description:` carrying the
Russian triggers, plus a `triggers:` list. 185 lines. All 5 relative links from
SKILL.md and all links across the four docs resolve.

## The ledger, item by item, against reality

Every one of the six honest outcomes names an owner. Five were verified against
the actual repos, boards and remote; the sixth against the shipped binary.

1. **41-row description divergence — TRUE and MEASURED.** The source board's
   `TASK-260823-1tis7o_implementation-notes.md` carries the field-by-field
   probe: id set / `agenticSystems` / effort support / `supportedEfforts` /
   `recommendedEffort` identical, `description` **DIFFERENT on all 41**. I
   counted 41 rows in the module independently. The two residues (PolicyRank as
   a score, the two muse rows) are stated in the consumer's own notes, as the
   ledger says. Owner: this repository.
2. **Deliberate keeps — TRUE.** Exec ownership, the process-starting preflights,
   the fault injector (`tools/board-cli/internal/limitsimulate` exists;
   `Layout.SimulateFile()` reserved here at `identity.go:280`), and the
   composition validators.
3. **The FLAGGED composition-validator duplication — TRUE, and the gap is
   real.** Consumer: `launch_composition.go:493-562` (codex) and `587-631`
   (claude), ~115 lines plus dispatch; module: `codex/composition.go` 150 lines
   + `claude/composition.go` 37. **No production caller of
   `System.ValidateComposition` exists anywhere in the consumer** — the only hit
   is a test stub implementing the interface. So "nothing tests that the two
   rule sets agree" is exact. Owner `skill-project-management` is the right
   side of the seam. The auth-hint counterpart is real too:
   `TestLocalAuthHintsAgreeWithTheAgenticPlugins` and
   `authHintGapsFoundByTheSwapSweep` exist, and `providerAuthHint` genuinely has
   no `qwen-codex` row (claude/codex/gemini/agy only).
4. **Leak pins — TRUE.** `BUG-260819-3qn52o` is `backlog` on the source board.
   All six plugins carry an `env.go` naming the bug, `gemini`/`muse`/`agy` say
   in prose that their filter is EMPTY rather than omitting the file, and each
   has a pin test (`TestTheSourcesOpenEnvLeaksStayOpen`,
   `TestTheEmptyFilterIsDeliberate`, `TestAPrefixStripAlsoLeaksThePointedAtCredentials`).
5. **CI arrangement — TRUE on every row.**
   - Tag: `git ls-remote --tags origin` → `v0.1.0` annotated, `^{}` at
     `b722ace`, which is `main`'s HEAD. It is a **valid Go module version**:
     `go list -m github.com/relux-works/skill-agents-management@v0.1.0` resolves
     from the remote, and `go get` + `go mod tidy` + `go run` against a CLEAN
     module cache builds and runs. No `/vN` mismatch.
   - Consumer half: UNCOMMITTED in
     `skill-project-management/.temp/STORY-260823-1sxcmg/worktree` — `go.mod`
     diff shows `v0.0.0` → `v0.1.0` with the sibling `replace` deleted,
     `GOPRIVATE` at `ci.yml:15`, the token read through `env:` at line 72 (NOT
     an `if:` key) with `url.insteadOf` at 81, `go.work`/`go.work.sum` added to
     `.gitignore` with the walking-up rationale, and no `go.work` tracked.
   - Secret: `gh api repos/relux-works/skill-project-management/actions/secrets`
     answered 200 with `{"total_count":0,"secrets":[]}`. **Not provisioned** —
     and that is a read that succeeded and found nothing, not a failed read.
6. **Local-model section stays design only — TRUE.** Grepped every doc for text
   implying existence: `architecture.md:118` opens the section with "**Nothing
   in this section is built.**", README's old "reports through these same
   fields" was rewritten to "is DESIGN ONLY and none of it is built … That test
   exercises the verdict type, not a resource plane". `grep -il localmodel` over
   `pkg/ internal/ tools/` returns nothing.
7. **CLI empty list — TRUE**, verified against the binary above.

## README / architecture reconciliation

- **Four, not five.** README: "four of the epic's six stories are on `main`".
  Verified twice: the board lists exactly four `done` stories, and `git log main`
  carries exactly those four story commits.
- `STORY-260821-1c5o90` is `backlog` with **zero** children on this board, as
  the ledger states, and is called a board-hygiene divergence with an owner and
  two concrete closes rather than being quietly closed.
- `architecture.md` now declares itself the CONTRACT and points at
  shipped-state for reality, states the exec/preflight boundary row, and marks
  extraction steps 1–3 done with step 4 in flight.

## Findings — recorded, none blocking

**F1 (accuracy, follow-up). "Nothing here starts a process" is not literally
true.** `pkg/providerlimits/liveness_unix.go:49` runs `exec.CommandContext(ctx,
"ps", "-p", …, "-o", "lstart=")`, reached from production at `liveness.go:38`,
`liveness.go:68`, `state.go:191` and `state.go:552` — the lease/probe-claim
liveness checks and lock acquisition, where it exists to make pid reuse
detectable. The surrounding launch-plane claims are exact and hold: `BuildPlan`
and `BuildLaunch` end at a value, and I confirmed the documented READ entry
point does not fork — `AvailabilityFor` → `LoadIdentityState` →
`loadIdentityFile(…, false)` takes no lock. So the sentence's intended scope is
right and only its absolute form overreaches. It matters because a consumer
sandboxing forks would read the absolute form and wire `providerlimits` anyway.
Owner: this repository, next edit to `SKILL.md` (and the same phrase in
`shipped-state.md` §2, where the colon already scopes it to `BuildPlan`/`BuildLaunch`).

**F2 (inherited, not introduced).** SKILL.md invariant 4 restates
`docs/architecture.md` — "a refusal names the exact dotted config key and the
accepted values". No refusal in this module names a config key; the effort
refusal names model, runtime and vocabulary. The line is verbatim from
`architecture.md:103` **on `main`**, so SKILL.md is faithfully carrying an older
contract-doc claim rather than inventing one. Under this delta's own framing —
architecture states the rule, shipped-state states reality — this is a rule the
module does not meet and that the honest-outcomes list does not carry. Small,
and the operator-actionable half (the accepted values) is present.

**F3 (drift, minutes old, non-load-bearing).** `shipped-state.md:31,38,165` and
`README.md:493` call `TASK-260823-t2xuen` / `STORY-260823-1sxcmg`
`development`; the source board moved both to `to-review` at 04:13–04:14Z, and
`shipped-state.md` was written at 04:02Z. Accurate when written, and
`shipped-state.md` hedges with "at the time of writing" / "State at the time of
writing" — README does not. The load-bearing claim is unaffected and I verified
it independently: **the switch has not landed** (consumer half uncommitted, CR
not integrated, secret absent). Documents naming another board's live status
will always drift; the conclusion is what has to stay true, and it does.

## Acceptance criteria

| AC | Verdict |
| --- | --- |
| 1. SKILL.md exists, house conventions, wireable from it alone | MET — followed cold end to end, module wired and called |
| 2. README/architecture reconciled; local-model stays design-only | MET — four-not-five verified against board and `git log main`; design-only reinforced in three places, no package exists |
| 3. Honest-outcomes list with owners | MET — six outcomes, six owners, each verified against the repos/boards/remote/API rather than restated |
| 4. `make regress` green, four classes, each mutation-checked | MET — 0.454s, 14 tests, 10/10 mutants reproduced (9 narrowing) plus 3 derived independently |
| 5. Suite green; work UNCOMMITTED | MET — 24 packages ok; tree `88a2495b…` == candidate, `git status` shows 5 modified + 4 untracked, nothing committed |

Definition of Done items on gating behaviour are satisfied by narrowing mutants
with production call sites named, not by positive-path evidence.

**ACCEPTED.** F1 and F2 are documentation-accuracy follow-ups with owners named,
not rework: they change no gate, no wiring step and no AC, and blocking the
epic's closing docs on one over-absolute sentence would cost more than it buys.
The next edit to `SKILL.md` should scope the process claim to the launch plane.
