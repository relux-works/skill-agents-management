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
- [x] Plans byte-match codex goldens for every captured mode and resolution path
- [x] Three binary-resolution paths ported, tested hermetically with stub layouts
- [x] One argv construction site; guard accepts exactly one binding file
- [x] Env filtering exact: strip families ported; source open leaks stay open and named
- [x] Prefix-strip and whole-env-wipe negatives stay red against this plugin
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
- [ ] If review does not accept the work — verdict evidence added and status routed by the explicit verdict branches

## Notes
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Hardest port: three resolution paths, one argv site, exact-key env filters, all golden-bound."}
spawn selection rationale for claude-opus-5/high: Hardest port: three resolution paths, one argv site, exact-key env filters, all golden-bound.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260822-43cc58, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260822-43cc58)
Ready for review, UNCOMMITTED. All 4 codex goldens byte-match through the real Registry + BuildPlan. Three binary-resolution paths ported and tested hermetically (no t.Setenv, all parallel). One argv construction site with its own module-wide go/ast guard (narrowed onto the real Args, 10 mutants, 3 residuals demonstrated open); the module single-source guard stays green with SystemID bound in exactly one file. Env strips are an exact-key map with credential-pointer resolution; the source open leaks (BUG-260819-3qn52o) stay OPEN and are pinned by a test that names the bug. Both parity permanent negatives (wipe, prefix) re-run against the real plugin with narrowings. Negative evidence: 31 production-gate mutants, run 1 = 29/31 with two survivors (a composition field check never exercised, and a WRONG comment claiming childEnv ordering was load-bearing), run 2 = 31/31 after fixing both. Reshapes named in results: resolution reads LaunchRequest.Env; LaunchRequest gained Run/Profile; CompositionServer gained BearerTokenEnvVar; WithRunContext/SetEnvValue added to core (one source for what the source had once); new dep pelletier/go-toml v2.4.3, same version the source uses. CLI deliberately NOT wired to the plugin - the shipped binary still answers plugins with [] and those pins still pass. Gates all foreground exit 0: make vet, make build, go test ./... , gofmt -l pkg/.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-43cc58, pid=84897, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Template port for the story; resolution order and module-scope of the argv scanner are where a quiet defect would propagate."}
spawn selection rationale for claude-opus-5/high: Template port for the story; resolution order and module-scope of the argv scanner are where a quiet defect would propagate.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260822-fbd6a1, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260822-fbd6a1)
Review ACCEPTED (CR rev1). Parity independently re-derived: 4/4 goldens through real Registry+BuildPlan; coverage tripwire attacked with a planted fifth codex golden and went RED. Argv scanner confirmed MODULE-scoped and threshold-one by planting real second sites in pkg/agentic and tools/agents-management/cmd (both RED, single-literal RED). Resolution order attacked: managed beats PATH through BuildPlan with two distinct binaries; wrong target triple and wrong platform package both fall through to PATH with no error the source would not raise. Env exact-key re-proven, plus a reviewer probe injecting an extra variable - caught in EnvAdded - and a rewritten bystander value caught in both diff lists. Open-leak pin verified controllable: appending TASK_BOARD_TOKEN to runtimeEnvKeys closes the leak, so a one-sided fix reds the pin. 12 reviewer mutants against the shipped source files: 11 RED in the right test, 1 GREEN (childEnv order swap) which env.go and logbook 2132 already declare. Producer harness re-run independently: 31/31, checkout restored GREEN. 17 composition-gate attacks, all correct. Source cross-checked rule-for-rule at ed4878123061b39fdae67160f6b5632117b48a2f. Eight non-blocking findings in TASK-260822-hp5fb4_review-verdict.md: F1 module-scan rules now spelled twice (argvguard_test.go copies singlesource_guard_test.go) and F2 ~150 lines of system-agnostic parity harness stranded in the codex test package - both template debt the five remaining ports inherit; F3 Plan.Home is a declaration nothing honours and req.Home never reaches the child; F4 Plan carries no side-effect surface and collapses ExecutionRoot into WorkDir; F5 README says nine argv mutants, there are ten; F6 binary.go calls isRegularFile the source verbatim when it narrows os.Stat to IsRegular; F7 codex.Args splices an unvalidated composition prefix when called outside BuildPlan; F8 the shipped CLI does not import the plugin so init() never runs - needs a wiring task. Gates foreground on the restored checkout: make vet 0, make build 0, go test ./... 0, gofmt -l pkg/ clean. Worktree verified byte-identical to candidate tree 920849b60e21123cf19d27c230ab7f3f4fcdbc18 before and after every scratch mutation; source checkout left clean. No commit_ack supplied - commit and the done transition are the orchestrator step.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-fbd6a1, pid=95485, exit=0)

## Precondition Resources
(none)

## Outcome Resources
- [TASK-260822-hp5fb4_spawn-log_-implementer--developer--claude-_RUN-260822-43cc58.log](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_spawn-log_-implementer--developer--claude-_RUN-260822-43cc58.log) — System spawn log captured by task-board
- [TASK-260822-hp5fb4_results.md](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_results.md) — Port results: 4/4 codex goldens byte-match, 3 resolution paths, one argv site, env contract, 31/31 mutants
- [TASK-260822-hp5fb4_mutants.py](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_mutants.py) — Negative-evidence harness: 31 production-gate mutants, remove/red/restore
- [TASK-260822-hp5fb4_mutants-01.log](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_mutants-01.log) — Mutation run 1: 29/31, two survivors named
- [TASK-260822-hp5fb4_mutants-02.log](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_mutants-02.log) — Mutation run 2 after fixing both survivors: 31/31, checkout restored green
- [TASK-260822-hp5fb4_change-request_rev1.patch](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_change-request_rev1.patch) — Change Request CR-TASK-260822-hp5fb4-1 revision 1 candidate patch (repository_delta=present, 20 changed paths)
- [TASK-260822-hp5fb4_spawn-log_-reviewer--reviewer--claude-_RUN-260822-fbd6a1.log](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_spawn-log_-reviewer--reviewer--claude-_RUN-260822-fbd6a1.log) — System spawn log captured by task-board
- [TASK-260822-hp5fb4_review-verdict.md](file://TASK-260822-hp5fb4/TASK-260822-hp5fb4_review-verdict.md) — Reviewer verdict for CR rev1: ACCEPTED, with independently re-derived parity, 12 own mutants, 17 composition-gate attacks and 8 named residuals

## Created
2026-08-22T15:30:38Z

## Last Update
2026-08-22T17:25:50Z

## Assigned To
[reviewer] reviewer (claude)
