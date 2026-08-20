## Status
done

## Review
required

## Task Class
code

## Estimate
estimated(fibonacci(8))

## Blocked By
- TASK-260821-3svtog

## Blocks
- TASK-260821-atcotl

## Checklist
- [x] Interface covers every capability adapterTable declares; deliberate drops/reshapes named in results
- [x] Registry is the only place a binding may live; duplicate registration errors
- [x] go/ast guard catches a shadow table AND an ID-switch, through const/var/funclit indirection, threat model documented
- [x] Guard mutants: at least two spellings beyond the obvious, red/green reported per mutant
- [x] Test-double system registers via public API and drives every dispatch surface with one edit
- [x] Effort transport none vs required-effort model decidable from contract types, shown by test
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
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"The load-bearing contract of the repo; the guard must start from the end of a four-round arms race, not replay it."}
spawn selection rationale for claude-opus-5/high: The load-bearing contract of the repo; the guard must start from the end of a four-round arms race, not replay it.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260821-149c45, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260821-149c45)
Layer-1 contract landed in pkg/agentic, UNCOMMITTED in the story worktree.

Interface: System (ID, Capabilities, ResolveBinary, Argv, ChildEnv, Stdin, ValidateComposition) + Capabilities (launch modes, effort transport, goal/budget/tier, composition grammar, HomeEnvVar/DefaultHome, AuthHint). Full adapterTable coverage map and four named reshapes are in the results artifact: BuildCommand split into Argv/ChildEnv/Stdin so environment and stdin parity is assertable without a process; DryRunArgs folded into LaunchMode so it cannot drift from the real launch; CompositionGrammar enum -> plugin-declared GrammarID + ValidateComposition so a new grammar is a plugin, not a core edit; DefaultHome re-pointed from the source unused cmd.Dir doc string to the harness config home the architecture names.

Registry: Register is the only constructor of a binding; refuses nil, unnormalizable id, duplicate (including via a second spelling), no launch modes, undeclared mode, undeclared effort transport. BuildPlan is the single dispatch site and returns Plan (binary/argv/env/stdin) - the parity surface.

Guard: pkg/agentic/singlesource_guard_test.go. go/ast, walks the whole module discovered from go.mod, resolves const/var/funclit indirection, detects both a shadow binding table and an ID-switch. Two rules are structural (map[SystemID]T as a type expression; switch on sys.ID()) so they fire for system ids nobody has declared yet - the id vocabulary is the second net, not the only one. Threat model and three declared-open residuals are documented in the file and demonstrated still open by a test.

Evidence: 16 mutants, all RED with real exit codes, tree restored green - 2 real on-disk plants (shadow table, ID-switch), 5 guard narrowings, 9 production-gate narrowings. Raw log attached as TASK-260821-21vywo_mutants.log.

CLI: wired. plugins now reads agentic.Default; the scaffolds private registeredPlugins list was deleted because it was itself a second binding for the same fact.

Gates foreground, all exit 0: make vet, make build, go test ./... -count=1, go test -race ./..., gofmt -l pkg tools.

Known limits stated in the artifact: Capabilities immutability is required by the contract but not enforced; StdinPayload carries bytes not a reader; the guard id vocabulary has no live matches until the first concrete plugin lands.
Pre-existing worktree index state, NOT created by this task and not touched by it: git status shows staged deletions (D) for LOGBOOK.md, Makefile, go.mod, go.sum, pkg/.gitkeep and every tools/agents-management file, while the same paths are present on disk as untracked (??). This was already the state at the start of this run (it is a leftover of TASK-260821-3svtog s .gitignore anchoring work, where those paths were ignored and then un-ignored). Whoever extracts the Change Request should take it from the working tree rather than from the index, or refresh the index first - a diff read straight off the index would present the whole scaffold as deleted.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-149c45, pid=26817, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"The repo's most load-bearing CR; coverage must be diffed against the source adapter field-by-field and the guard attacked with fresh mutants."}
spawn selection rationale for claude-opus-5/high: The repo's most load-bearing CR; coverage must be diffed against the source adapter field-by-field and the guard attacked with fresh mutants.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260821-9b9d47, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260821-9b9d47)
REVIEW VERDICT: CHANGES REQUESTED (evidence: TASK-260821-21vywo_review-verdict.md, TASK-260821-21vywo_reviewer-mutants.md). Reviewed CR-TASK-260821-21vywo-1 rev1 at candidate tree 1b2c006c95bd37a756386c2a31f5b6d141f6efd5; CR files verified byte-identical before and after review, all mutation done in a scratch copy or reverted byte-identically. Gates re-run foreground in the story worktree: make vet 0, make build 0, go test ./... -count=1 0, gofmt -l pkg/ tools/ clean.

FOUR BLOCKING FINDINGS, all in the guard/registry gate surface (~40 lines plus mutants):

F1 pkg/agentic/singlesource_guard_test.go:177 - the structural id-switch rule fires only when the switch tag is literally the ID() call. Copying it into a local first walks through: `id := sys.ID(); switch id { case "opencode": ... }` (reviewer mutant R6 UNCAUGHT). Not in the declared residuals, and it falsifies the claim at singlesource_guard_test.go:59-61 that the structural rules keep the guard working for ids nobody has declared yet. The same shape with a KNOWN id is caught (R7), so exposure is bounded to a system id outside knownSystemIDs - which is exactly the case the structural rule exists for. Asked: resolve function-local `x := <isIDCall expr>` and report a switch on that identifier; ship R6 as a mutant plus a narrowing mutant.

F2 pkg/agentic/singlesource_guard_test.go - an init()-assembled shadow table with literal ids walks through: `var shadow map[string]builder` + `func init(){ shadow["codex"]=...; shadow["claude-code"]=... }` (R4 UNCAUGHT). No IndexExpr-assignment rule and the key type is plain string. Declared residual 3 does not cover it - no loop, nothing computed at runtime. The extraction source own binding table (spawn/adapter.go:96-104) is populated from an init() for a documented compile-time reason, so this spelling has live precedent. Asked: report binding-table when an assignment LHS is an index expression whose key resolves via knownSystemIDOf; verify TestSingleSourceGuardResidualGaps stays green.

F3 pkg/agentic/singlesource_guard_test.go:438 - resolveConstStrings walks file.Decls only, so a function-local `const codexID = "codex"` used in a comparison walks through (R8 UNCAUGHT). Doc is honest about package-level scope but the residual list does not mention local declarations. Asked: fold function-local const strings, or add local declarations to the declared-open residuals with a case in TestSingleSourceGuardResidualGaps.

F4 pkg/agentic/system.go:354-356 vs pkg/agentic/registry.go:83 - System.ID() is documented as having to normalize to itself; Register only requires that it normalizes successfully. Demonstrated: a plugin whose ID() returns "  PANGOLIN  " registers, and sys.ID() keeps returning that while the registry emits pangolin everywhere. No state orphaned today, but six port tasks inherit this contract. Asked: refuse when SystemID(raw) != normalized, naming both spellings, with a negative test.

CONFIRMED BY ATTACK, DO NOT DISTURB IN REWORK:
- AC1 coverage read off the extraction source independently: all 11 adapterTable fields land, no unlisted drop. Nothing homeless from the *exec.Cmd path - inside BuildCommand only argv/Env/Stdin/Dir are set; SysProcAttr and process-group setup are runner-layer (process_group_unix.go:13-20, deadline_unix.go:26-29, spawn.go:913), never adapter facts. agy ARG_MAX refusal and claude conditional-stdin remain expressible.
- Dry-run/real binary parity holds BY CONSTRUCTION, not convention: ResolveBinary receives no mode and plan.go:126 is its only call site, so a plugin cannot diverge. Mode enum closed: Register refuses empty/unknown modes, BuildPlan refuses undeclared ones.
- No core enum of grammars survives; GrammarNone refused by BuildPlan before the plugin validator runs.
- DefaultHome semantic upgrade is grounded in docs/architecture.md:13,35 and invariant 2, and does not prejudge vendor-layer identity (LaunchRequest.Home overrides; two vendors can share one home).
- AC4 met: effortAdmission compiles against Capabilities+Model alone, full 3x2 matrix plus EffortTransport(42) refused; BuildPlan is the named production caller with three refusals.
- Guard module walk CONFIRMED: violation planted in pkg/brandnew/deeper/ (a directory that does not exist today) was found and named.
- Residual tests are honest, not decoration: closing residual class 1 with a four-line foldStringConst change turned TestSingleSourceGuardResidualGaps RED with the update-the-threat-model message. Reverted byte-identically.
- Axolotl property DEMONSTRATED: deleting the single registry.Register(sys) line reddened 25 tests across every dispatch surface. Restored byte-identically.
- Registry normalization is THE shared one: NormalizeSystemID is the only normalization, three call sites, no local lowercase+trim anywhere; explicit ASCII fold plus [a-z0-9-] validation rather than strings.ToLower, so one charset not two.
- CLI goes through the registry, not around it: registeredPlugins deleted, TestPluginsCommandReportsWhatTheRegistryHolds drives the real root command with "Claude-Code" and asserts claude-code on stdout. Wiring it was correct - the private list would itself have been a second binding.
- 12 of 15 reviewer mutants caught, including alias/named-type keys, make() of a named binding type, split-file var declarations, struct fields, and if/else-if chains. R15 (a method named Identifier()) is uncaught and correctly out of scope.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-9b9d47, pid=30573, exit=0)
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Four concrete guard/registry holes, one of which is the exact shape the extraction source uses; claims and rules must be reconciled."}
spawn selection rationale for claude-opus-5/high: Four concrete guard/registry holes, one of which is the exact shape the extraction source uses; claims and rules must be reconciled.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260821-741789, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260821-741789)
Rev 2 (rework of CR-TASK-260821-21vywo-1). F1 local-copy-of-ID() switch tag: resolved via idCallLocals (one binding, never rewritten, inherited into closures); isIDCall also looks through a one-argument conversion. F2 key-by-key table assembly: new AssignStmt/IndexExpr rule; residual class 3 (strings.Join key) verified still open. F3 function-local const: resolveConstStrings now walks every node, same for resolveVarStrings; the flat-scope imprecision is documented as a false-positive-direction corner. F4 ID() normalization: Register refuses ErrUnnormalizedSystemID naming both spellings. F5 found during rework: reconstructing reviewer mutant R10 (make() of a binding-table type declared in the allowed file) showed it UNCAUGHT despite the verdict listing it CAUGHT - closed with make/ValueSpec/Field rules over bindingTypes. Threat model rewritten: 3 declared-open residuals -> 5, plus one declared NON-goal (Identifier() accessor). Two confirmed tests changed shape because F4 makes them unreachable (Pangolin/PANGOLIN duplicate collision; the CLI Claude-Code normalization test) - replacements named in the artifact and strictly stronger. Both mutant sets re-run in full: original 16 all RED with control green and byte-identical restoration, reviewer R1-R14 all CAUGHT, R15 correctly uncaught as a declared non-goal. 7 new-rule narrowings each proven load-bearing. 51 mutant subtests. Axolotl property re-measured: 27 tests red on deleting the one Register line. Gates: make vet 0, make build 0, go test ./... -count=1 0, gofmt -l clean, go test -race 0. Work UNCOMMITTED. Artifact: TASK-260821-21vywo_results-rev2.md
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-741789, pid=32980, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Verification of four fixes plus settling a wrong row in the reviewer's own rev1 verdict."}
spawn selection rationale for claude-opus-5/high: Verification of four fixes plus settling a wrong row in the reviewer's own rev1 verdict.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260821-30c540, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260821-30c540)
Review rev2: CHANGES REQUESTED. All four rev1 findings verified closed by re-running my own mutants against rev1 and rev2 trees (F1/R6, F2/R4, F3/R8 all UNCAUGHT->CAUGHT; F4 gate refuses, proven by narrowing not deleting). R10 SETTLED in the producer favour: my rev1 R10 mutant redeclared type bindings in the non-allowed file so the MapType rule fired on the redeclaration (r10.go:3, where=type bindings); the true shape was UNCAUGHT at rev1 and is CAUGHT at rev2. My rev1 verdict row is wrong; no accept criterion relied on it. R11 has the same defect. Reshaped tests verified strictly stronger: registry.go:127 is the only map write and it sits downstream of the gate; probes P1/P2/P5 refuse; N2 turns all three subtests red. Matrix R1-R14 green with rule attribution proved by NARROW-1 and NARROW-3 (R6 red / R7 green = the two nets are independent); R15 residual demonstrably fails when isIDCall is widened. NEW: F5 two of three named F1-boundary probes open AND undeclared (B2 two-hop local, B3/C1 conversion hop; B3 uncaught while B4 the same tag without the hop is caught) - the five declared residuals do not cover them. F6 the BinaryExpr rule never consults isIDCall, so an if-chain on sys.ID() with undeclared ids is uncaught while the switch spelling is caught; R3 only covers known ids, so the mutant matrix never exposed it. F7 the rev2-added System.ID docstring claims a caller can never see two spellings, but Register reads ID() once and an unstable ID() registers then answers a second spelling (probe P3), with BuildPlan planning happily. F8 stale unstableCaps in double_test.go:38. Gates green on the candidate: make vet, make build, go test ./... , gofmt clean. Worktree verified byte-identical to candidate 8723bd2f before and after; all mutation done in scratch copies. AC1/AC3/AC4 untouched by this delta and re-verified. Verdict artifact: TASK-260821-21vywo_review-verdict-rev2.md
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-30c540, pid=37370, exit=0)
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Final guard round by decision: close the if/switch asymmetry, residualize two-hop with demonstration, make the prose match the rules."}
spawn selection rationale for claude-opus-5/high: Final guard round by decision: close the if/switch asymmetry, residualize two-hop with demonstration, make the prose match the rules.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260821-bdf01b, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260821-bdf01b)
CR rev3 (final guard round) ready for review. All four verdict items closed; artifact TASK-260821-21vywo_results-rev3.md, verbose matrix TASK-260821-21vywo_matrix-rev3.log.

F6 CLOSED IN CODE: BinaryExpr rule now routes through isIDCall/idCallLocals via a shared dispatchesOnID helper, so switch and if ask one question. Other operand must fold to a NON-BLANK compile-time string; the blank carve-out (sys.ID() == "") is held by three new shapes in the ordinary-code control. Four permanent mutants added, all with ids OUTSIDE knownSystemIDs. NARROW-F6 (structural half removed only) reds exactly those three comparison mutants; the known-id if-chains R3 and friends stay green — the two nets are independent on this rule too.

DEVIATION, stated: the brief said residualize B2 AND B3. I residualized B2 and C1 and CLOSED B3 in code. B3s local is one hop, so filing it under a multi-hop residual would have put a false sentence back into the file on the round whose point is that the prose must be true. idCallLocals is unchanged and still single-hop; only the rule site now looks through one-argument calls to a resolved local, which it already did without the local. NARROW-B3 (restore bare-ident-only tags) reds exactly the one mutant.

B2 and C1 declared open and DEMONSTRATED: WIDEN-MULTIHOP (transitive fixpoint over assignment chains) reds both residual subtests by name, and C1 fires through the new comparison rule. Threat model now lists six classes, splits them by which net each defeats, and the gaps test says six classes / seven subtests and why.

F7 ENFORCED AND REWORDED: Register reads ID() twice before normalization and refuses with new ErrUnstableSystemID. System.ID no longer claims two spellings can never be seen — it says the key is the spelling returned AT registration and that forever is the plugins obligation. Three tests: the refusal (error names both answers, Len 0, Lookup misses both, BuildPlan returns ErrUnknownSystem), the same double stable as control (asserts calls[ID] >= 2), and the post-registration flip demonstrated as admitted. N-F7b narrowing (compare normalized instead of raw) reds the gate test.

F8: double_test.go now names unstableID, the field F7 needed.

README corrected: stability refusal listed, mutant count 32 -> 49.

GATES foreground, real exit codes: make vet 0, make build 0, go test ./... -count=1 0 (53 tests / 85 subtests / 0 failures), gofmt -l pkg/ tools/ 0 and silent. Work left UNCOMMITTED.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-bdf01b, pid=39204, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Final round: verify the B3 deviation against the reviewer's own original spelling and the prose-matrix reconciliation."}
spawn selection rationale for claude-opus-5/high: Final round: verify the B3 deviation against the reviewer's own original spelling and the prose-matrix reconciliation.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260821-86df97, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260821-86df97)
Review rev3 (RUN-260821-86df97): ACCEPTED via accept_cr, element parked at to-review for the orchestrator to commit and transition to done with commit_ack=scope_committed. No commit_ack supplied by this run; work left UNCOMMITTED. Gates green foreground on candidate b2a8125: make vet, make build, go test ./... -count=1, gofmt -l clean; 57 guard subtests, 0 failures. Candidate tree verified byte-identical before and after review; all mutation in /tmp scratch copies. Evidence: TASK-260821-21vywo_review-verdict-rev3.md plus TASK-260821-21vywo_reviewer-probes-rev3.tgz (runnable harnesses). Checks: B3 caught (declared deviation justified), B2/C1 open and demonstrated, F6 both spellings caught with a future id and the emptiness carve-out held by NARROW-EMPTY, prose-vs-matrix reconciled line by line, F7 closed both ways and verified through an independent flipper plugin. NARROW-F6 reds exactly 3 mutants, NARROW-B3 exactly 1, WIDEN reds exactly the 2 new residuals by name, NARROW-F7 and NARROW-F7b both red the unstable-ID gate test. Three escapes found beyond the round and recorded for the next threat-model pass, none an AC failure: dispatch via a comparison FUNCTION (strings.EqualFold/HasPrefix over sys.ID()), a binding table spelled as a slice of id/behaviour structs rather than a map, and an undeclared id held in a local var on the literal side of a comparison (foldStringConst folds consts, not vars).
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-86df97, pid=41324, exit=0)

## Precondition Resources
(none)

## Outcome Resources
- [TASK-260821-21vywo_spawn-log_-implementer--developer--claude-_RUN-260821-149c45.log](file://TASK-260821-21vywo/TASK-260821-21vywo_spawn-log_-implementer--developer--claude-_RUN-260821-149c45.log) — System spawn log captured by task-board
- [TASK-260821-21vywo_results.md](file://TASK-260821-21vywo/TASK-260821-21vywo_results.md) — Layer-1 agentic-system contract, registry, go/ast single-source guard: design decisions, adapterTable coverage map, reshapes, 16 mutants with exit codes, gate evidence
- [TASK-260821-21vywo_mutants.log](file://TASK-260821-21vywo/TASK-260821-21vywo_mutants.log) — Raw go test output and exit codes for all 16 guard/gate mutants plus the restored-tree control
- [TASK-260821-21vywo_change-request_rev1.patch](file://TASK-260821-21vywo/TASK-260821-21vywo_change-request_rev1.patch) — Change Request CR-TASK-260821-21vywo-1 revision 1 candidate patch (repository_delta=present, 14 changed paths)
- [TASK-260821-21vywo_spawn-log_-reviewer--reviewer--claude-_RUN-260821-9b9d47.log](file://TASK-260821-21vywo/TASK-260821-21vywo_spawn-log_-reviewer--reviewer--claude-_RUN-260821-9b9d47.log) — System spawn log captured by task-board
- [TASK-260821-21vywo_review-verdict.md](file://TASK-260821-21vywo/TASK-260821-21vywo_review-verdict.md) — Reviewer verdict for CR revision 3: ACCEPTED. B-probe rerun, F6 both spellings + carve-out, prose-vs-matrix reconciliation, F7 enforcement+rewording, NARROW-F6/B3/EMPTY/F7/F7b and WIDEN evidence, three newly found escapes for the threat model.
- [TASK-260821-21vywo_reviewer-mutants.md](file://TASK-260821-21vywo/TASK-260821-21vywo_reviewer-mutants.md) — Raw reviewer mutant output: 15 independent guard mutants, module-walk proof in a new package dir, residual-closure red proof, axolotl deletion proof, F4 unnormalized-ID probe
- [TASK-260821-21vywo_spawn-log_-implementer--developer--claude-_RUN-260821-741789.log](file://TASK-260821-21vywo/TASK-260821-21vywo_spawn-log_-implementer--developer--claude-_RUN-260821-741789.log) — System spawn log captured by task-board
- [TASK-260821-21vywo_results-rev2.md](file://TASK-260821-21vywo/TASK-260821-21vywo_results-rev2.md) — Rev 2 rework: F1-F4 closed plus F5 found during rework; full mutant matrix (16 guard narrowings + 15 reviewer spellings + 7 new-rule narrowings), gates with real exit codes
- [TASK-260821-21vywo_mutants-rev2.log](file://TASK-260821-21vywo/TASK-260821-21vywo_mutants-rev2.log) — Verbose go test -run TestSingleSourceGuard: 51 mutant subtests, rev 2
- [TASK-260821-21vywo_rerun-mutants-rev2.py](file://TASK-260821-21vywo/TASK-260821-21vywo_rerun-mutants-rev2.py) — The harness that re-ran the original 16 guard/gate narrowings against rev 2 and restored every file byte-identically
- [TASK-260821-21vywo_change-request_rev2.patch](file://TASK-260821-21vywo/TASK-260821-21vywo_change-request_rev2.patch) — Change Request CR-TASK-260821-21vywo-2 revision 2 candidate patch (repository_delta=present, 14 changed paths)
- [TASK-260821-21vywo_spawn-log_-reviewer--reviewer--claude-_RUN-260821-30c540.log](file://TASK-260821-21vywo/TASK-260821-21vywo_spawn-log_-reviewer--reviewer--claude-_RUN-260821-30c540.log) — System spawn log captured by task-board
- [TASK-260821-21vywo_review-verdict-rev2.md](file://TASK-260821-21vywo/TASK-260821-21vywo_review-verdict-rev2.md) — Reviewer verdict for CR rev2: changes requested. Four rev1 findings verified closed (RED->GREEN + narrowing mutants); R10 settled in the producer's favour; four new findings (F5-F8).
- [TASK-260821-21vywo_reviewer-rerun-rev2.log](file://TASK-260821-21vywo/TASK-260821-21vywo_reviewer-rerun-rev2.log) — Raw reviewer mutant output: same harness run against rev1 and rev2 trees, plus the runtime bypass probes P1-P5.
- [TASK-260821-21vywo_spawn-log_-implementer--developer--claude-_RUN-260821-bdf01b.log](file://TASK-260821-21vywo/TASK-260821-21vywo_spawn-log_-implementer--developer--claude-_RUN-260821-bdf01b.log) — System spawn log captured by task-board
- [TASK-260821-21vywo_results-rev3.md](file://TASK-260821-21vywo/TASK-260821-21vywo_results-rev3.md) — CR rev3: F6 closed in code with narrowing evidence, B2/C1 residualized and demonstrated live, F7 enforced by double-read plus reworded promise, F8 fixed; four gates green
- [TASK-260821-21vywo_matrix-rev3.log](file://TASK-260821-21vywo/TASK-260821-21vywo_matrix-rev3.log) — Full verbose pkg/agentic run at rev3: 53 tests, 85 subtests, 0 failures
- [TASK-260821-21vywo_change-request_rev3.patch](file://TASK-260821-21vywo/TASK-260821-21vywo_change-request_rev3.patch) — Change Request CR-TASK-260821-21vywo-3 revision 3 candidate patch (repository_delta=present, 14 changed paths)
- [TASK-260821-21vywo_spawn-log_-reviewer--reviewer--claude-_RUN-260821-86df97.log](file://TASK-260821-21vywo/TASK-260821-21vywo_spawn-log_-reviewer--reviewer--claude-_RUN-260821-86df97.log) — System spawn log captured by task-board
- [TASK-260821-21vywo_reviewer-probes-rev3.tgz](file://TASK-260821-21vywo/TASK-260821-21vywo_reviewer-probes-rev3.tgz) — Reviewer's rev3 probe harnesses (zz_reviewer_{probe,prose,attack,f7}_test.go). Drop into pkg/agentic of a scratch copy to reproduce every table in the verdict: B-probes, F6 spellings and carve-out, prose-claim reconciliation, the three escapes, and the F7 independent flipper plugin.
- [TASK-260821-21vywo_review-verdict-rev3.md](file://TASK-260821-21vywo/TASK-260821-21vywo_review-verdict-rev3.md) — Reviewer verdict for CR revision 3: ACCEPTED. B-probe rerun, F6 both spellings + carve-out, line-by-line prose-vs-matrix reconciliation, F7 enforcement+rewording verified through an independent plugin, NARROW-F6/B3/EMPTY/F7/F7b and WIDEN evidence, three newly found escapes recorded for the threat model.

## Created
2026-08-21T13:54:47Z

## Last Update
2026-08-21T15:57:29Z

## Assigned To
[reviewer] reviewer (claude)
