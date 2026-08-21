## Status
done

## Review
required

## Task Class
code

## Estimate
estimated(fibonacci(8))

## Blocked By
- TASK-260822-slgewd

## Blocks
- (none)

## Checklist
- [x] Plans byte-match claude goldens for both modes
- [x] Env contract ported exactly; prefix and wipe negatives red against this plugin
- [x] Goal-mode preparation in plan surface or explicitly out with the source behaviour referenced
- [x] Guard sees one binding file; argv single-site discipline matches the codex template
- [x] Work left UNCOMMITTED for the CR snapshot
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
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Claude port: goal-mode preparation boundary and the env contract read from goldens, on the certified codex template."}
spawn selection rationale for claude-opus-5/high: Claude port: goal-mode preparation boundary and the env contract read from goldens, on the certified codex template.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260822-7c80ee, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260822-7c80ee)
Claude Code plugin ported: pkg/agentic/systems/claude (claude.go, args.go, goal.go, env.go, binary.go, composition.go). Both claude goldens byte-match through the real Registry + BuildPlan.

BRIEF CORRECTED BY THE SOURCE: the spawn brief said the codex family is stripped from claude children. It is not. buildClaudeCommand is withSpawnEnv(filterEnv(os.Environ(), "CLAUDECODE"), cfg) - ONE exact key. filterCodexRuntimeEnv is a codex child filter; filterQwenRuntimeEnv is that PLUS CLAUDECODE for a qwen child. Both goldens agree: env_removed is CLAUDECODE plus the four run-context keys, and every seeded CODEX_*/credential key survives. Pinned by TestTheClaudeChildKeepsTheCodexFamily as the other side of the source BUG-260819-3qn52o, not fixed.

GOAL-MODE PREPARATION IS EXPLICITLY OUT OF THE PLAN SURFACE, with the source behaviour recorded in goal.go: PrepareClaudeGoalLaunch validates the board contract, gates on Claude Code >= 2.1.139 by running claude --version, probes /goal by running the harness, and decides a session action. Steps 2-3 start processes and BuildPlan calls every method to build a DRY RUN, so preparation behind any of them makes a dry run execute the harness twice. It belongs to the launch/session plane port; no home invented here. The one piece carried across is the empty-provider-condition refusal, because dropping it ships a bare `/goal ` binding the child to nothing.

CORE RESHAPE: agentic.Goal gained ProviderCondition. The predicate is rendered by the board (RenderGoalProviderCondition) and the board validates a stored contract against a re-render, so a plugin can only carry it - rendering it here would be a second renderer producing a directive the board rejects.

ARGV GUARD UNIFIED, ONE CONVENTION: scanner extracted to internal/argvguard, module walk to internal/gosources (also adopted by pkg/agentic single-source guard, which had a third copy), launch-env/PATH primitives to internal/launchenv. Allowlist keys are now FILE-SCOPED - both plugins name their site Args, so the old bare-name allowlist would have exempted every Args in the module from every guard. All codex mutants stayed green through the move. Claude signature deliberately excludes --dangerously-skip-permissions and --output-format because agy/qwen spell them; the residual is named and held by a test.

GATES (foreground, real exit codes): make vet 0, make build 0, env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 0, gofmt -l pkg/ clean.
MUTATION: 12 mutants, all caught, tree restored green (python3 .temp/TASK-260822-3u97y3/mutants.py, exit 0).

UNCOMMITTED. git log head is still 5653b7d. Note: this worktree arrived with pre-existing staged deletions for the parity harness and codex plugin while the files sit untracked on disk - not mine, index untouched.

Artifacts: TASK-260822-3u97y3_results.md, TASK-260822-3u97y3_mutation-run.tgz.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-7c80ee, pid=945, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"The env dispute must be settled by building the brief's wrong port and watching it fail; goal-mode side-effect freedom is the other axis."}
spawn selection rationale for claude-opus-5/high: The env dispute must be settled by building the brief's wrong port and watching it fail; goal-mode side-effect freedom is the other axis.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260822-3a1c76, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260822-3a1c76)
REVIEW rev1 = CHANGES REQUESTED -> to-dev. Evidence: TASK-260822-3u97y3_review-verdict.md. All four ACs MET and the ported code is correct - no behavioural defect found under attack. Both goldens re-derived independently of the parity comparator; all 23 stdin bytes perturbed plus truncation/append caught; goal-mode plan proven to launch nothing (os/exec absent from the dep closure AND a recording stub left untouched, with the recorder proven live); the four goal.go claims spot-checked exact against tools/board-cli/internal/spawn/claude_goal.go; both guards proven to bite (shadow binding red, scan-scope narrowing reds 8 tests); producer 12-mutant harness re-run 12/12. ENV DISPUTE SETTLED IN THE PRODUCER FAVOUR: I built the port MY brief described (strip the codex family too) and it FAILS BOTH goldens - the deviation was NECESSARY, not merely permitted. No duplicate keys in plan.Env; run-context keys replaced not appended. REWORK IS THREE TESTS, NOT THE PORT. (1) claude.go:127-128 HomeEnvVar/DefaultHome can move silently - the identical mutant is CAUGHT in codex_test.go:95 and SURVIVES here, while the code comment and AGENTS.md both call the value load-bearing for on-disk identity. (2) composition.go:100-104 unknown-server refusal never exercised: composition_test.go:102 is caught by the transport-shape rule instead, so deleting the refusal leaves the suite green and admits {"smuggled":{"type":"","command":"..."}} - fix by giving the smuggled entry the ZERO-VALUE type. (3) composition.go:79-81 duplicate-server refusal never exercised: composition_test.go:171 varies the TRANSPORT so the shape rule fires - fix by keeping the transport identical and varying BearerTokenEnvVar, which admits a duplicate that swaps the credential the prefix reads. (2) and (3) are the same class as logbook 2026-08-22 2132 finding 1 from the codex task. Optional freebie: dropping strings.TrimSpace(req.Model.ID) in args.go also survives.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-3a1c76, pid=8350, exit=0)
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Three missing pins on shadowed refusal branches; sweep for a third before handoff."}
spawn selection rationale for claude-opus-5/high: Three missing pins on shadowed refusal branches; sweep for a third before handoff.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260822-a2a0a9, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260822-a2a0a9)
REWORK for CR-TASK-260822-3u97y3-1 rev 1. Evidence-only rework: NO production behaviour changed. Diffed against the rev1 candidate tree b4f647e — exactly five files differ, three test files and two comment-only.

THE THREE REQUESTED PINS, all added and proved by mutant:
1. claude.go:127-128 HomeEnvVar/DefaultHome pinned in TestTheDeclaredCapabilities, mirroring codex_test.go. Proved by NARROWING as well: each field moved ALONE is caught (exit 1), not just the pair.
2. composition.go:100-104 unknown-server refusal — probe type changed to "" so the refusal itself is the only rule that can fire. An undeclared name resolves to the ZERO CompositionServer whose Transport is "", which is why any named transport was refused by the shape rule first.
3. composition.go:79-81 duplicate refusal — both declarations now share Transport http and differ only in BearerTokenEnvVar (REVIEWED then SNEAKY), the dangerous form.

THE SWEEP FOUND FOUR SHADOWS, NOT TWO, PLUS FIVE UNCOVERED SUB-CONDITIONS. I mutated every refusal branch and sub-condition in composition.go rather than reading for the pattern. rev1 left NINE branches unbound: the reviewer two, plus (a) the unnamed-server refusal, shadowed by the unknown-server rule, (b) entry.Type != server.Transport, shadowed by the stdio shape rule, and (c) five sub-conditions with no case at all — http with no url, http with args, stdio with headers, stdio with no command, root.MCPServers == nil. All nine now bound.

ALSO: the model-trim freebie pinned (TestTheModelIDIsTrimmedBeforeItReachesArgv, M18c red). Both documentation nits fixed — goal.go/args_test.go source citations corrected to tools/board-cli/internal/spawn/…, and internal/argvguard package doc corrected rather than duplicated (the three residual classes belong to the scanner, demonstrated once by TestCodexArgvGuardResidualGaps; each plugin demonstrates its own SIGNATURE residual).

MUTATION RUN: 21 mutants, 21 CAUGHT, 0 SURVIVORS. Baseline exit 0, restored exit 0, both logged. Per-mutant log carries the applied diff and the real exit code.

GATES (foreground, standalone, real exit codes): make vet 0 | make build 0 | env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 → 0 | gofmt -l pkg/ → empty | gofmt -l pkg/ internal/ tools/ → empty.

Artifacts: TASK-260822-3u97y3_rework-rev2-evidence.md, TASK-260822-3u97y3_mutation-run-rev2.tgz. Logbook entry 2026-08-22 2136.

NOTE for the reviewer, so it is not chased: the LOGBOOK.md entries above mine claim times 2305-2325 while the machine clock read 2136 when I wrote mine. I used the real clock and placed my entry first per the newest-first convention, so the timestamps above it read as later. Not my edit and not touched.

Work left UNCOMMITTED for the CR snapshot.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-a2a0a9, pid=14191, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Verify three pins plus sample the exhaustive sweep that found nine unpinned branches where the review had found two."}
spawn selection rationale for claude-opus-5/high: Verify three pins plus sample the exhaustive sweep that found nine unpinned branches where the review had found two.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260822-be1e7e, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260822-be1e7e)
Rev 2 review: ACCEPTED (CR-TASK-260822-3u97y3-2, reviewer RUN-260822-be1e7e). Candidate tree reconstructed via scratch index and confirmed = 7ec0e09; same OID re-verified after all mutation work, so nothing leaked. (1) Diff discipline holds: b4f647e -> 7ec0e09 touches three test files, LOGBOOK.md, and two production files that are comment-only - proven mechanically, zero non-comment changed lines in argvguard.go and goal.go. (2) All three rev-1 probes red: empty-type smuggled server (unknown-server refusal deleted -> admitted); same-transport duplicate swapping BearerTokenEnvVar (duplicate refusal deleted -> admitted); capabilities pin red under single-field narrowing of HomeEnvVar and DefaultHome INDEPENDENTLY, each the sole failure. (3) Sampled the sweep with four narrowing mutants of my own choosing - entry.Type != server.Transport (the shadowed one), len(entry.Headers)!=0 in the stdio branch, len(entry.Args)!=0 in the http branch, root.MCPServers==nil - all red, and the last three each fail exactly one case, which is the evidence those sub-conditions had no prior holder. TrimSpace pin also red, sole failure. Six mutants sampled from 21 claimed, zero survivors. (4) Residual ownership fixed at the contract: argvguard.go now assigns the three scanner classes to TestCodexArgvGuardResidualGaps once and each plugin demonstrates only its own signature residual (claude: TestTheDeclaredResidualStaysOpen); both tests exist and run, and claude/argvguard_test.go does not re-promise the scanner classes. Gates foreground on the pristine tree: gofmt -l pkg/ clean, make vet clean, make build clean, go test ./... 11/11 ok. Verdict artifact: TASK-260822-3u97y3_review-verdict-rev2.md (rev-1 verdict preserved under its own name). Next: orchestrator checkpoints/integrates and makes the done transition with commit_ack=scope_committed.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-be1e7e, pid=23465, exit=0)

## Precondition Resources
(none)

## Outcome Resources
- [TASK-260822-3u97y3_spawn-log_-implementer--developer--claude-_RUN-260822-7c80ee.log](file://TASK-260822-3u97y3/TASK-260822-3u97y3_spawn-log_-implementer--developer--claude-_RUN-260822-7c80ee.log) — System spawn log captured by task-board
- [TASK-260822-3u97y3_results.md](file://TASK-260822-3u97y3/TASK-260822-3u97y3_results.md) — Claude Code plugin port: parity evidence, env-contract findings, goal-mode preparation boundary, argv-guard unification, 12-mutant run
- [TASK-260822-3u97y3_mutation-run.tgz](file://TASK-260822-3u97y3/TASK-260822-3u97y3_mutation-run.tgz) — 12-mutant run: harness plus one log per mutant with real exit codes and the tests that failed; baseline and restored logs included
- [TASK-260822-3u97y3_change-request_rev1.patch](file://TASK-260822-3u97y3/TASK-260822-3u97y3_change-request_rev1.patch) — Change Request CR-TASK-260822-3u97y3-1 revision 1 candidate patch (repository_delta=present, 27 changed paths)
- [TASK-260822-3u97y3_spawn-log_-reviewer--reviewer--claude-_RUN-260822-3a1c76.log](file://TASK-260822-3u97y3/TASK-260822-3u97y3_spawn-log_-reviewer--reviewer--claude-_RUN-260822-3a1c76.log) — System spawn log captured by task-board
- [TASK-260822-3u97y3_review-verdict.md](file://TASK-260822-3u97y3/TASK-260822-3u97y3_review-verdict.md) — Reviewer verdict rev1: changes requested. All 4 ACs met; three surviving mutants (home identity unpinned, unknown-server and duplicate-server refusals never exercised).
- [TASK-260822-3u97y3_spawn-log_-implementer--developer--claude-_RUN-260822-a2a0a9.log](file://TASK-260822-3u97y3/TASK-260822-3u97y3_spawn-log_-implementer--developer--claude-_RUN-260822-a2a0a9.log) — System spawn log captured by task-board
- [TASK-260822-3u97y3_rework-rev2-evidence.md](file://TASK-260822-3u97y3/TASK-260822-3u97y3_rework-rev2-evidence.md) — Rework for CR rev1: three requested pins added; sweep found FOUR shadowed refusals in composition.go (not two) plus five untested sub-conditions. 21/21 mutants caught, 0 survivors. No production behaviour changed.
- [TASK-260822-3u97y3_mutation-run-rev2.tgz](file://TASK-260822-3u97y3/TASK-260822-3u97y3_mutation-run-rev2.tgz) — Rev2 mutation run: 21 mutants over every composition.go refusal branch plus the home/model pins; harness, per-mutant log with applied diff and real exit code, summary.json, baseline and restored logs. 21 CAUGHT, 0 SURVIVORS.
- [TASK-260822-3u97y3_change-request_rev2.patch](file://TASK-260822-3u97y3/TASK-260822-3u97y3_change-request_rev2.patch) — Change Request CR-TASK-260822-3u97y3-2 revision 2 candidate patch (repository_delta=present, 27 changed paths)
- [TASK-260822-3u97y3_spawn-log_-reviewer--reviewer--claude-_RUN-260822-be1e7e.log](file://TASK-260822-3u97y3/TASK-260822-3u97y3_spawn-log_-reviewer--reviewer--claude-_RUN-260822-be1e7e.log) — System spawn log captured by task-board
- [TASK-260822-3u97y3_review-verdict-rev2.md](file://TASK-260822-3u97y3/TASK-260822-3u97y3_review-verdict-rev2.md) — Reviewer verdict, CR revision 2: ACCEPTED. Diff discipline verified mechanically (zero non-comment production lines changed between b4f647e and 7ec0e09); three rev-1 probes plus four sweep pins re-derived as narrowing mutants, all red, zero survivors; residual-ownership doc fix confirmed as a contract fix not a duplicate; gofmt/vet/build/full suite green at the candidate tree.

## Created
2026-08-22T15:30:38Z

## Last Update
2026-08-22T18:45:40Z

## Assigned To
[reviewer] reviewer (claude)
