## Status
done

## Review
required

## Task Class
code

## Estimate
estimated(fibonacci(8))

## Blocked By
- (none)

## Blocks
- (none)

## Checklist
- [x] Score-based rank preserves the board ties; position derived; Basis still required
- [x] All 43 rows ported, pinned against a frozen fixture of the board table
- [x] Existing pins green: digests, full-set, F2, availability, guard
- [x] muse effort axes under vendor-unresolved shape
- [x] Tag only after acceptance; work UNCOMMITTED
- [x] Code written per task description and AC
- [x] Relevant tests written for new or changed behavior and passing
- [x] Gating, refusing, validating, authorizing, or attesting behavior covered by negative tests that fail when the gate admits what it must reject, with the production call site named
- [x] Lint clean
- [x] Relevant build/validation commands run after changes and build not broken
- [x] New outcome artifact attached on the board with a task-scoped name when the work produces notes, logs, screenshots, or other deliverables
- [x] Important findings, decisions, anomalies, or regressions recorded in logbook when relevant
- [ ] Implementation matches AC
- [ ] Solution fits project architecture
- [ ] Tests green
- [ ] Gate, refusal, validation, authorization, and attestation behavior attacked, not read — positive-path-only evidence is not accepted
- [ ] If review does not accept the work — verdict evidence added and status routed by the explicit verdict branches

## Notes
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Schema growth with a tie-preserving rank and a transitional transcription pin; the digest must provably ignore the new fields."}
spawn selection rationale for claude-opus-5/high: Schema growth with a tie-preserving rank and a transitional transcription pin; the digest must provably ignore the new fields.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260823-2eda94, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260823-2eda94)
Plan (developer): (1) CapabilityRank grows Score (ties legal) and loses the declared Position; position becomes DERIVED via a new Lineup() over a vendor list, tie-broken by declaration order and documented as presentation. Basis stays required. (2) Model grows Lifecycle, SupersededBy, Recommended, ContextWindowTokens, Pricing, each with a stated emptiness rule and a registration refusal. (3) muse two rows live on RuntimeDeclaration.Models, legal ONLY when Vendor==VendorUnresolved. (4) New TRANSITIONAL fixture pkg/vendorplugin/testdata/board-model-facts.json captured from the BOARD BINARY q models() at trunk dbd905b (board-owned facts only; effort/systems for vendor-owned rows are joined back from this module and would be circular, so they are not pinned from it). (5) New pin asserts the new fields do NOT enter the admitted-pair digest serialization.
Developer handoff. All gates foreground, real exit codes: make vet 0; make build 0; env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 → 0; make regress → 0 (0.4s); gofmt -l pkg/ internal/ → 0 with no output. Mutation harness .temp/TASK-260824-y7gyco/mutants.py → 0, 21 compile-clean NARROWINGS of the new gates, 21 killed, 0 survivors, each naming the test. One real survivor was found and fixed first (the no-plans pricing refusal was masked by the downstream names-this-model refusal sharing one sentinel error; the test now states the phrase each of its 19 cases must see). Three earlier mutants died on Go build failures rather than test failures and were rewritten, because a build-failure kill proves a line exists and nothing about the class it covers. WORK IS UNCOMMITTED AND UNTAGGED - git tag --list v0.2* is empty; v0.2.0 is the release act and belongs after acceptance. Out-of-scope fix, flagged not folded: docs/consuming-the-module.md and SKILL.md still told consumers to set up GOPRIVATE plus a PAT rewrite, which the first consumer ciguard now enforces the ABSENCE of, so a reader following the doc would fail their build.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260823-2eda94, pid=27825, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"A deliberate API break on rank semantics plus a vendor-unresolved row representation; the pin's provenance is from the live binary."}
spawn selection rationale for claude-opus-5/high: A deliberate API break on rank semantics plus a vendor-unresolved row representation; the pin's provenance is from the live binary.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260823-bc9847, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260823-bc9847)
REVIEW ACCEPTED (CR rev 1, RUN-260823-bc9847). Candidate tree independently recomputed to 1bb50d66 (matches). Gates foreground green: gofmt clean, make vet, make build, go test ./... 24 pkgs 0 FAIL, make regress. No v0.2 tag; HEAD still at base 92e6d8b (work uncommitted). ATTACKED not read: (a) fixture re-captured from the board BINARY at dbd905b into a scratch tree -> byte-identical diff, models.go sha256 matches provenance, board checkout untouched; (b) pin mutated on the DECLARATION side (lifecycle, price digit, score, hasFrequencyLimits) - each red naming the exact row+field; (c) planted a real Lifecycle leak into AdmittedModel + CanonicalSerialization - both digest guards fired; (d) muse attacked both directions (rows on a resolved runtime refused; 12 invalid muse rows hit the same Model.Validate); (e) 6 self-written narrowing mutants, 5 killed. Ties are broader than the brief assumed: 8 pairs across 4 vendors (board rank is per-runtime, module lineup per-vendor), all named row by row, openai no-ties explicit. FOLLOW-UPS (non-blocking): 1) narrowing checkRecommendations to model.Systems[:1] (vendor.go:700) SURVIVES the whole suite - no test covers a Recommended row driving >1 system; nil live exposure (no shipped multi-system row). 2) CapabilityRank.Score doc says a tie means the two are equal; google/models.go says 5 of its ties mean no such thing (merged per-runtime scales) - type doc overstates. 3) ErrDuplicateRank removal is a second API break, only in LOGBOOK, not in the consuming-the-module migration note that says one breaking change. Next: orchestrator checkpoints/integrates, commits with commit_ack=scope_committed, then tags v0.2.0.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260823-bc9847, pid=92229, exit=0)

## Precondition Resources
(none)

## Outcome Resources
- [TASK-260824-y7gyco_spawn-log_-implementer--developer--claude-_RUN-260823-2eda94.log](file://TASK-260824-y7gyco/TASK-260824-y7gyco_spawn-log_-implementer--developer--claude-_RUN-260823-2eda94.log) — System spawn log captured by task-board
- [TASK-260824-y7gyco_results.md](file://TASK-260824-y7gyco/TASK-260824-y7gyco_results.md) — Developer results: score-based rank with ties, board facts on the Model row, muse under vendor-unresolved, the transitional board-facts pin, digest byte-stability, 21/21 mutants killed, all gates green
- [TASK-260824-y7gyco_mutants-01.log](file://TASK-260824-y7gyco/TASK-260824-y7gyco_mutants-01.log) — Mutation harness log: 21 compile-clean narrowings of the new gates, all killed, each naming the test that killed it
- [TASK-260824-y7gyco_change-request_rev1.patch](file://TASK-260824-y7gyco/TASK-260824-y7gyco_change-request_rev1.patch) — Change Request CR-TASK-260824-y7gyco-1 revision 1 candidate patch (repository_delta=present, 30 changed paths)
- [TASK-260824-y7gyco_spawn-log_-reviewer--reviewer--claude-_RUN-260823-bc9847.log](file://TASK-260824-y7gyco/TASK-260824-y7gyco_spawn-log_-reviewer--reviewer--claude-_RUN-260823-bc9847.log) — System spawn log captured by task-board
- [TASK-260824-y7gyco_review-verdict.md](file://TASK-260824-y7gyco/TASK-260824-y7gyco_review-verdict.md) — Reviewer verdict for CR revision 1: ACCEPTED, with attack evidence (fixture re-capture byte-diff, declaration-side pin mutants, planted digest leak, 6 narrowing mutants) and 3 non-blocking follow-up findings

## Created
2026-08-23T21:14:16Z

## Last Update
2026-08-23T19:40:00Z

## Assigned To
[reviewer] reviewer (claude)
