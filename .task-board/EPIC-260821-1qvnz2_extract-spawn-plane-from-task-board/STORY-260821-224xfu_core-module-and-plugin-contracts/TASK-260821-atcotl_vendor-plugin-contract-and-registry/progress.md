## Status
done

## Review
required

## Task Class
code

## Estimate
estimated(fibonacci(8))

## Blocked By
- TASK-260821-21vywo

## Blocks
- (none)

## Checklist
- [x] Vendor interface covers models+rank+description+efforts+supported-systems, structured availability, full spawn surface without re-declaring agentic types
- [x] Registering a vendor naming an unknown agentic system refused with both ids; negative test
- [x] Six historical runtimes seeded from the frozen table; muse vendor honestly UNKNOWN
- [x] F2 collision policy on runtime declarations, both directions tested
- [x] Availability verdict expresses healthy, limited-until, unreachable, unknown without interface break
- [x] Guard extended, not duplicated; one dispatch-type list
- [x] Fake vendor drives every dispatch surface with one edit
- [x] Gates green foreground; work left UNCOMMITTED
- [x] Code written per task description and AC
- [x] Relevant tests written for new or changed behavior and passing
- [x] Gating, refusing, validating, authorizing, or attesting behavior covered by negative tests that fail when the gate admits what it must reject, with the production call site named
- [x] Lint clean
- [x] Relevant build/validation commands run after changes and build not broken
- [x] New outcome artifact attached on the board with a task-scoped name when the work produces notes, logs, screenshots, or other deliverables
- [x] Important findings, decisions, anomalies, or regressions recorded in logbook when relevant
- [x] Implementation matches AC
- [x] Solution fits project architecture
- [x] Tests green
- [x] Gate, refusal, validation, authorization, and attestation behavior attacked, not read — positive-path-only evidence is not accepted
- [x] If review does not accept the work — verdict evidence added and status routed by the explicit verdict branches

## Notes
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Layer-2 contract completing the story; dependency direction and honest-unknown vendor handling are the architectural risks."}
spawn selection rationale for claude-opus-5/high: Layer-2 contract completing the story; dependency direction and honest-unknown vendor handling are the architectural risks.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260821-f0d146, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260821-f0d146)
Layer-2 vendor contract complete and UNCOMMITTED in .temp/STORY-260821-224xfu/worktree. New: pkg/vendorplugin (Vendor interface, model rows with evidence-based rank + non-empty usage description + effort vocabulary, structured Availability verdict, runtime declarations with the six frozen seeds and F2 policy, BuildLaunch as the single Layer-2 dispatch site), internal/ident (the one identifier normalization, now shared by both layers - agentic.NormalizeSystemID delegates, error text unchanged), CLI vendors + runtimes commands. The single-source guard was EXTENDED, not duplicated: one bindingHomes list mapping SystemID/VendorID/RuntimeID each to the single file permitted to bind it, which is stricter than a flat allowlist and proved so by a narrowing mutant. muse carries VendorUnresolved with checked-and-empty provenance; resolving it is refused on its own terms (ErrRuntimeVendorUnresolved, distinct from not-registered). 19 gate mutants run, 0 survivors, table in the results artifact. Gates foreground: make vet=0, make build=0, go test ./... -count=1 =0, gofmt -l pkg/ tools/ =clean.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-f0d146, pid=43401, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Story-closing contract; availability semantics and the unknown-vendor representation become the port stories' ground truth."}
spawn selection rationale for claude-opus-5/high: Story-closing contract; availability semantics and the unknown-vendor representation become the port stories' ground truth.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260821-c5fa11, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260821-c5fa11)
REVIEW: changes requested -> to-dev. Gates all green in the candidate worktree (vet/build/test/gofmt = 0) and the four ACs are met by the implementation; the findings are about what the SUITE proves, not what the code does. No production change expected - three test additions.

Ran 10 mutants of my own (distinct from the producers 19). Three SURVIVED the shipped suite:
(1) ErrRuntimeVendorUnresolved = ErrRuntimeVendorUnregistered passes ./... exit 0. TestResolutionRefusalsAreDistinctFacts is named for mutual exclusivity and only asserts each branch positively. This is the claim decision #2 rests on and the one the port stories inherit - highest priority.
(2) EffortDeclaration.Accepts widened to strings.EqualFold passes exit 0. The type comment states the exact-match rule and its reason (invariant 5); a widening sends the vendor a word it never published. The producers mutant deletes the check, it does not narrow it.
(3) CapabilityRank.Basis per-entry blank checks narrowed from TrimSpace(x)=="" to x=="" passes exit 0. The same discipline is applied and pinned for UsageDescription and Broker.Checked; rank evidence was missed.

All three are killed by the attached TASK-260821-atcotl_reviewer-attack-tests.go.txt.

HELD under attack: dependency direction incl. registration order, the nil-Layer-1 bypass, and post-registration vendor mutation (refused at ResolveRuntime on ErrRuntimeSystemUnregistered); zero-value-unknown is load-bearing; 8 forged availability verdicts all refused at the production path; the 5 local-plane answers reproduce; all six seeds byte-exact against runtimeid/builtins.go read directly, muse honestly unresolved; F2 both directions on frozen ids incl. refusing a guessed muse vendor; defensive copying at every exit; the guard narrowing mutant reproduces (only the cross-layer test dies) and caught 5 fresh evasions I planted incl. type alias, derived type and struct field; internal/ident is genuinely the only normalization and the error text is byte-identical (verified empirically, though unpinned); scope clean - no exec/net/syscall anywhere in production sources.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-c5fa11, pid=47365, exit=0)
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Three test-only pins; the sentinel-distinctness one is what port stories would inherit broken."}
spawn selection rationale for claude-opus-5/high: Three test-only pins; the sentinel-distinctness one is what port stories would inherit broken.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260821-42e4cc, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260821-42e4cc)
Rev 2 — the three surviving mutants killed, TEST-ONLY. No production source under pkg/, internal/ or tools/ was edited. M5: TestResolutionRefusalsAreDistinctFacts gained a subtest asserting mutual exclusivity — 4 resolution refusals x 5 sentinels, each refuting the other four; the unresolved case resolves the real muse row through SeedFrozenRuntimes (renamedPangolin + systemsNamed added to double_test.go). M3: TestBuildLaunchDoesNotFoldTheEffortVocabulary drives BuildLaunch with Deep/DEEP/dEeP/Shallow/de ep (all ErrEffortNotInVocabulary, vendor never reached) and still admits deep-with-surrounding-whitespace; TestEffortVocabularyMembershipIsExactOnTheTrimmedWord pins the same rule at Accepts itself, which the launch path could NOT observe because BuildLaunch trims first (demonstrated: with only the BuildLaunch test, deleting Accepts TrimSpace left the suite at exit 0). M2: TestRegisterRefusesARankWithNoEvidence gained whitespace-only source, whitespace-only observation, both-blank, empty basis, negative position, and one-sound-plus-one-blank, asserted at Model.Validate AND at Registry.Register. Mutation run: 13 mutants (the reviewer ten with M2 split into 2a/2b, plus M11/M12 for the trim bounds), 13 KILLED, 0 SURVIVORS. Gates foreground standalone: make vet 0, make build 0, env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 0, gofmt -l pkg/ tools/ 0 no output. Logbook entries 2140 and 2135. Work left UNCOMMITTED.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-42e4cc, pid=50670, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Verify three test-only kills and the cross-table's generality beyond the found pair."}
spawn selection rationale for claude-opus-5/high: Verify three test-only kills and the cross-table's generality beyond the found pair.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260821-4e4a39, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260821-4e4a39)
REVIEW rev 2: ACCEPTED. (1) Diff discipline verified: rev1 tree 01251f97 (rebuilt from base+rev1 patch) vs candidate e6beb5b6 = 4 _test.go files + LOGBOOK.md, 212 insertions and ZERO deletions, no production file differs, no t.Skip added. Candidate worktree hashes to e6beb5b6 before and after the review; rev2 patch sha256 matches 5eb324a1. (2) My ten rev-1 mutants re-run in original spelling, M2 split: 11/11 KILLED, 0 survivors. The three survivors are dead - M5 by TestResolutionRefusalsAreDistinctFacts, M3 by BOTH the BuildLaunch and Accepts tests, M2/M2b by TestRegisterRefusesARankWithNoEvidence. M7 re-run as a faithful SEMANTIC flattening (my first spelling was a weaker mutant) is again killed by TestSingleSourceGuardCatchesCrossLayerBindings and nothing else, as rev 1 recorded. (3) Cross-table generality: four sentinel pairs I did NOT use in rev1 collapsed - unknown-runtime/unresolved, system-unregistered/unknown-runtime, vendor-not-registered/vendor-unregistered, vendor-unregistered/unknown-runtime - ALL red inside the new subtest, each from both sides. ErrVendorNotRegistered is a refutation-only column entry and still load-bearing. The unresolved case drives the shipped muse row through SeedFrozenRuntimes and guards its own setup. (4) Trim boundary both directions: deleting trim in resolveEffort reds only TestBuildLaunchDoesNotFoldTheEffortVocabulary (accepted-spellings half), deleting it in Accepts reds only TestEffortVocabularyMembershipIsExactOnTheTrimmedWord - disjoint, so neither test can be satisfied by the other layer, and the bound reads as exactness not removed-trim. Five FRESH attacks on the new test code: rank Position narrowed <1 to <0 KILLED, basis emptiness narrowed to nil-only KILLED, frozen muse given a guessed anthropic vendor KILLED by 5 tests incl. the CLI JSON one, unresolved check deleted KILLED, and muse provenance claiming a Found is REFUSED BY PRODUCTION at package init - checked-and-empty is enforced in registry.go, not merely asserted. Gates foreground in the candidate worktree: make vet 0, make build 0, env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 0, gofmt -l pkg/ tools/ internal/ clean. All four ACs met. Work left UNCOMMITTED; integration and the done transition with commit_ack are the orchestrator step. Carried forward for the port stories, not a finding: internal/ident error text is byte-identical to the pre-extraction wording but unpinned by a test.
REVIEW rev 2 addendum — two process findings, deliberately NOT written to LOGBOOK.md. LOGBOOK.md is inside the accepted candidate tree e6beb5b6; editing it would both breach the reviewer read-only constraint and invalidate the tree the orchestrator is about to integrate. Recorded here instead. (a) BOARD CONTRACT: task-board resource update on a name a PREVIOUS run produced destroys that run payload, and accept_cr then refuses it as evidence with change_request_evidence_missing — correctly, since this run cannot be shown to have produced it. A reviewer verdict for a new revision must be a NEW task-scoped name (TASK-x_review-verdict-revN.md), never an update of the prior revision verdict. Hit and repaired during this review: TASK-260821-atcotl_review-verdict.md was overwritten and has been restored from the full text read earlier in this same session — a faithful reconstruction, not the byte-preserved original, and it carries a provenance note saying so. The rev2 verdict is TASK-260821-atcotl_review-verdict-rev2.md. (b) TEST SHAPE: a negative-assertion TABLE (N cases x M sentinels, each case refuting every sentinel but its own) is a materially stronger pin than the one or two hardcoded negative pairs a reviewer happens to find. Verified empirically here by collapsing FOUR sentinel pairs that were not the reported one; all four went red inside the same subtest. Two hand-written negatives would have caught one of them.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-4e4a39, pid=54231, exit=0)

## Precondition Resources
(none)

## Outcome Resources
- [TASK-260821-atcotl_spawn-log_-implementer--developer--claude-_RUN-260821-f0d146.log](file://TASK-260821-atcotl/TASK-260821-atcotl_spawn-log_-implementer--developer--claude-_RUN-260821-f0d146.log) — System spawn log captured by task-board
- [TASK-260821-atcotl_results.md](file://TASK-260821-atcotl/TASK-260821-atcotl_results.md) — Layer-2 vendor contract: what shipped, the four ACs, design decisions, the 19-mutant evidence table and gate exit codes
- [TASK-260821-atcotl_mutant-results.json](file://TASK-260821-atcotl/TASK-260821-atcotl_mutant-results.json) — Machine-readable mutant run: 19 gate mutations, exit codes and the tests each turned red (0 survivors)
- [TASK-260821-atcotl_mutate.py](file://TASK-260821-atcotl/TASK-260821-atcotl_mutate.py) — The mutation harness itself: applies one gate mutation, runs the package tests, restores the file
- [TASK-260821-atcotl_change-request_rev1.patch](file://TASK-260821-atcotl/TASK-260821-atcotl_change-request_rev1.patch) — Change Request CR-TASK-260821-atcotl-1 revision 1 candidate patch (repository_delta=present, 40 changed paths)
- [TASK-260821-atcotl_spawn-log_-reviewer--reviewer--claude-_RUN-260821-c5fa11.log](file://TASK-260821-atcotl/TASK-260821-atcotl_spawn-log_-reviewer--reviewer--claude-_RUN-260821-c5fa11.log) — System spawn log captured by task-board
- [TASK-260821-atcotl_review-verdict.md](file://TASK-260821-atcotl/TASK-260821-atcotl_review-verdict.md) — Reviewer verdict rev 1: changes requested. 10 independent mutants, 3 survivors (unresolved/unregistered sentinel identity, effort vocabulary exact-match bound, rank evidence whitespace); everything else attacked and held. RESTORED after an accidental overwrite by the rev 2 re-review — see the provenance note at the end.
- [TASK-260821-atcotl_reviewer-attack-tests.go.txt](file://TASK-260821-atcotl/TASK-260821-atcotl_reviewer-attack-tests.go.txt) — The reviewer's own attack suite (16 tests). Kills all three surviving mutants; drop into pkg/vendorplugin as a _test.go to close the findings.
- [TASK-260821-atcotl_reviewer-mutate.sh](file://TASK-260821-atcotl/TASK-260821-atcotl_reviewer-mutate.sh) — The reviewer's mutation harness: applies one mutation to a scratch copy, runs the full suite, reports survivors, reverts.
- [TASK-260821-atcotl_spawn-log_-implementer--developer--claude-_RUN-260821-42e4cc.log](file://TASK-260821-atcotl/TASK-260821-atcotl_spawn-log_-implementer--developer--claude-_RUN-260821-42e4cc.log) — System spawn log captured by task-board
- [TASK-260821-atcotl_results-rev2.md](file://TASK-260821-atcotl/TASK-260821-atcotl_results-rev2.md) — Rev 2: the three surviving mutants killed, test-only; 13/13 mutants killed, 0 survivors; four gates green
- [TASK-260821-atcotl_rev2-mutants.py](file://TASK-260821-atcotl/TASK-260821-atcotl_rev2-mutants.py) — Rev 2 mutation harness: the reviewer's ten plus two trim mutants, exact-text anchors, real exit codes
- [TASK-260821-atcotl_rev2-mutant-results.json](file://TASK-260821-atcotl/TASK-260821-atcotl_rev2-mutant-results.json) — Rev 2 mutation results: 13 mutants, 13 killed, 0 survivors, with the tests that went red for each
- [TASK-260821-atcotl_rev2-mutant-run.log](file://TASK-260821-atcotl/TASK-260821-atcotl_rev2-mutant-run.log) — Rev 2 mutation run transcript
- [TASK-260821-atcotl_change-request_rev2.patch](file://TASK-260821-atcotl/TASK-260821-atcotl_change-request_rev2.patch) — Change Request CR-TASK-260821-atcotl-2 revision 2 candidate patch (repository_delta=present, 40 changed paths)
- [TASK-260821-atcotl_spawn-log_-reviewer--reviewer--claude-_RUN-260821-4e4a39.log](file://TASK-260821-atcotl/TASK-260821-atcotl_spawn-log_-reviewer--reviewer--claude-_RUN-260821-4e4a39.log) — System spawn log captured by task-board
- [TASK-260821-atcotl_rev2-review-mutants.json](file://TASK-260821-atcotl/TASK-260821-atcotl_rev2-review-mutants.json) — Rev 2 re-review mutation results: 11 rev-1 mutants + 7 generality probes + 5 fresh attacks, 0 survivors, with the tests that went red for each
- [TASK-260821-atcotl_rev2-review-mutate.py](file://TASK-260821-atcotl/TASK-260821-atcotl_rev2-review-mutate.py) — The rev 2 re-review mutation harness: applies one mutation to the scratch copy /tmp/atcotl-s2, runs the full suite, reports survivors and the failing tests, reverts
- [TASK-260821-atcotl_review-verdict-rev2.md](file://TASK-260821-atcotl/TASK-260821-atcotl_review-verdict-rev2.md) — Reviewer verdict rev 2: ACCEPTED. Diff discipline (test-only, 0 production changes, 0 deletions), 11 rev-1 mutants re-run with 0 survivors, 4 previously-unused sentinel pairs collapsed to prove the cross-table generalizes, the trim boundary load-bearing in both directions on disjoint tests, and 5 fresh attacks including one refused by production at package init.

## Created
2026-08-21T13:54:47Z

## Last Update
2026-08-20T20:30:00Z

## Assigned To
[reviewer] reviewer (claude)
