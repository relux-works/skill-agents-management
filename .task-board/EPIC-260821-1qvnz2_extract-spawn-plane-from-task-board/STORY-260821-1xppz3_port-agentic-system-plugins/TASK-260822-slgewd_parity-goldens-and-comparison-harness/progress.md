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
- TASK-260822-hp5fb4
- TASK-260822-3u97y3
- TASK-260822-xz8rj5

## Checklist
- [x] Goldens from the real source harness with source commit recorded
- [x] Comparison harness fails on any field difference; masking pinned
- [x] Wrong plan demonstrably fails (negative test)
- [x] Uncaptured combinations listed with source reasons
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
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"The story's contract artifact: goldens must come from the source harness, not a reimplementation, or parity proves nothing."}
spawn selection rationale for claude-opus-5/high: The story's contract artifact: goldens must come from the source harness, not a reimplementation, or parity proves nothing.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260822-5e9f5d, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260822-5e9f5d)
Parity goldens + comparison harness ready for review; work left UNCOMMITTED for the CR snapshot.

GOLDENS (AC1): 14 fixtures in pkg/agentic/parity/testdata/goldens/, captured by the SOURCE harness (skill-project-management tools/board-cli/internal/spawn/parity_capture_test.go::TestCaptureLaunchSurface, gated by SPAWN_PARITY_CAPTURE_OUT) at commit ed4878123061b39fdae67160f6b5632117b48a2f, recorded inside every file. Nothing here captures anything. Source checkout read-only, clean before and after; one go test run in it. Kept the source per-case granularity (system/case) rather than collapsing to system/mode: three codex exec cases pin three branches of resolveCodexBinary that one fixture would lose. Reviewable decision: the capture ran under a PINNED synthetic parent env (env -i, .scripts/capture-parity-goldens.sh), because the source diffs against os.Environ() and an unpinned capture only proves a strip on the machine that happened to set the key. Consequence documented in the fixtures README: dry-run binary resolution is pinned to a stub bin dir and so differs from the source artifact, which recorded its operator installed codex and not-found errors.

HARNESS (AC2): FromPlan maps agentic.Plan onto the source schema; ComparePlan is the one call a port drives, and it takes the parent env from the GOLDEN. Compare has no tolerance and TestCompareReportsEveryField holds it to the Snapshot type by reflection, so a field added later fails until Compare learns it. Masking ported from the source (temp-dir noise, PATH excluded from the diff verbatim from its diffEnv) and pinned three ways: TestMaskRuleSetIsFrozen, TestMaskingCoversExactlyTheDeclaredFields (plants a maskable literal in EVERY field, asserts exactly the declared set changed - this is the fails-if-masking-widens pin), TestEveryMaskRuleIsNecessary (narrows one rule at a time). Env exclusion narrowed by TestDiffEnvExcludesPathAndOnlyPath.

NEGATIVES (AC3): TestAWrongPlanFailsAgainstItsGolden drives real agentic.BuildPlan through a real Registry and plants three separate defects on claude/prompt-mode - argv flag changed, env key dropped, one stdin byte changed - each asserted to be reported in the field it was planted in, each preceded by an assertion that the unmutated plan matches. Loader refusals have their own negatives (bad schema version, id/filename mismatch, unparseable json, unknown field, empty dir).

BOUNDARY (AC4): testdata/goldens/README.md lists what the source could not capture with its own reasons - managed-session-args/Site 3 (package cmd, no spawn.CodexArgs to call pre-refactor; own proof is TestManagedCodexSpawnArgsMatchesTheHistoricalConstruction) and the interactive-manager-client argv passthrough (filters user-typed argv). CONSEQUENCE FOR THE PORTS: agentic.LaunchModeManagedSession has NO golden at all. Also documents what the goldens do not prove for covered combinations: PATH content, dry-run env/stdin, the agy placeholder binary, the pinned parent env, darwin/arm64.

GUARD SCAN SCOPE: fixtures sit under testdata/, excluded from the guard walk. Stated explicitly in golden.go and the fixtures README as correct for JSON fixtures, with the rule it costs: a .go file under that directory would be invisible to the guard, so no Go source belongs there. The harness itself lives one level up where the guard does scan it.

GATES (each a standalone foreground process): make vet 0, make build 0, env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 0, gofmt -l pkg/ 0 with no paths. 45 tests/subtests in pkg/agentic/parity. Six mutants applied and reverted, each caught by the test written for it - listed in the results artifact.

Artifacts: TASK-260822-slgewd_results.md, TASK-260822-slgewd_source-raw-capture.json (the unmasked source output the goldens were generated from).
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-5e9f5d, pid=74757, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"The synthetic-env capture decision may hide exactly the strips the goldens exist to protect; that question decides the story."}
spawn selection rationale for claude-opus-5/high: The synthetic-env capture decision may hide exactly the strips the goldens exist to protect; that question decides the story.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260822-95c1b6, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260822-95c1b6)
REVIEW VERDICT: changes requested -> to-dev. Evidence: TASK-260822-slgewd_review-verdict.md.

ACCEPTED AS CORRECT. The flagged pinned-synthetic-env decision is right and the feared hole does NOT exist: .scripts/capture-parity-goldens.sh seeds CLAUDECODE, the CODEX_* family, TASK_BOARD_SESSION_ID and both token-pointer pairs, so every strip is positively named in env_removed and the per-system sets discriminate (claude strips CLAUDECODE only; codex strips the codex family only; qwen strips both; muse/gemini/agy strip nothing) - matching spawn.go:934/1072/1096/1108/1130/1145. A probe that drops the CLAUDECODE strip is caught and reported in EnvRemoved. Provenance re-verified: ed48781 is HEAD of the sibling checkout, clean; I re-ran the source harness through the script and regenerated all 14 goldens - diff -r byte-for-byte identical, with fresh t.TempDir allocations so masking did real work. Comparator and masking pins bit on five reverted scratch mutants (new Snapshot field, mask widened to StdinKind, mask narrowed off Error, 4th mask rule, README commit abbreviated) and on four reviewer-planted plan defects driven through real agentic.BuildPlan (binary basename, binary directory, CLAUDECODE leak, stdin detach), each naming the right field. Fixture boundary README and testdata placement verified against the guard scan-scope decision; no .go under testdata. Gates green in the candidate tree: gofmt -l pkg/ empty, make vet 0, make build 0, go test ./... -count=1 all ok. Working tree write-tree = ac967f8b28915fddc6f2070b217d6d211267d6be, identical to the CR candidate OID before and after all mutations; source checkout read-only throughout.

BLOCKING FINDING. qwen/exec is the only exec fixture whose env_removed covers 100% of its recorded parent_env - 18 seeded keys, 18 removed, ZERO survivors. Every other exec fixture leaves bystanders (claude 13, codex 1 = CLAUDECODE, muse/gemini/agy 14). Because ComparePlan feeds the plan g.Capture.ParentEnv and nothing else, a qwen port whose ChildEnv DISCARDS THE WHOLE PARENT ENVIRONMENT and returns only its injections byte-matches the golden. I built that port as a scratch wipeSystem, drove it through real agentic.BuildPlan, and ComparePlan returned ZERO differences. The identical defect on codex/exec-default-path IS caught, because CLAUDECODE survives there. So the goldens prove filterQwenRuntimeEnv lower bound (strips at least these) and are silent on its upper bound (strips at most these) - the exact property the source wrote its own preservation test for (spawn_test.go:940, dropped unrelated environment). The golden shape actively invites the defect: a port author reading env_removed = everything would reasonably write return-only-injections and pass. The fixtures README, which is the test-enforced boundary document, does not name this residual, so a port reads the absence as permission - the failure that README exists to prevent. Three port tasks are blocked on this element and would inherit it.

FIX (small, mechanical, strengthens every fixture at once): add one or two keys to PINNED_ENV in .scripts/capture-parity-goldens.sh that no source filter touches - e.g. PARITY_BYSTANDER=keep-me plus a near-miss such as CODEX_LIKE_BUT_NOT=keep-me that narrows the strip to the exact key set rather than a prefix - then re-run the script. Regeneration is proven reproducible, the source stays read-only, and every system gains a positive preservation bound. Update the fixtures README coverage prose. Weaker alternative if recapture is judged too costly: state the residual explicitly in What the goldens do not prove, which closes the absence-as-permission gap but leaves the bypass live. Re-run the four gates and hand back a new CR revision.

NON-BLOCKING NITS: golden_test.go raw table in TestLoadDirRefusesAMalformedFixture has an unread name field, one entry of which is a copy of its whole JSON body - drop it. TestGoldenSurfacesCarryNoMachineLocalPaths derives its forbidden home from os.UserHomeDir() on the RUNNING machine, so off the capture operator machine it checks a literal that could never have leaked; the /var/folders/ and .temp/parity-capture literals still bite, so it is a weakening rather than a hole.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-95c1b6, pid=78822, exit=0)
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Bystander seeding gives every golden an upper bound; the wipeSystem shape becomes a permanent negative."}
spawn selection rationale for claude-opus-5/high: Bystander seeding gives every golden an upper bound; the wipeSystem shape becomes a permanent negative.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260822-62f90d, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260822-62f90d)
Rev 2. Blocking finding resolved: qwen/exec now leaves 4 of 22 parent entries surviving. Four bystander keys added to PINNED_ENV (PARITY_BYSTANDER plus three *_LIKE_BUT_NOT near-misses pinning exact-key vs prefix strip), all 14 goldens regenerated at the same source commit ed4878123061b39fdae67160f6b5632117b48a2f (clean, re-verified byte-identical across two captures with fresh temp dirs). Two new permanent negatives through the real agentic.BuildPlan: TestAWholeEnvironmentWipeFailsAgainstQwenExec and TestAPrefixStripFailsAgainstQwenExec, each with a narrowing subtest that removes the relevant bystanders and shows the same defect walking through. Convention pinned by TestEveryGoldenSeedsTheBystanderKeys, TestQwenExecLeavesSomethingToPreserve and the fixtures README check. Both non-blocking nits fixed. Gates: gofmt 0, make vet 0, make build 0, go test ./... 0. UNCOMMITTED. See TASK-260822-slgewd_results-rev2.md.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-62f90d, pid=80560, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Verify the bystander fix with the reviewer's own wipeSystem and a prefix-strip probe."}
spawn selection rationale for claude-opus-5/high: Verify the bystander fix with the reviewer's own wipeSystem and a prefix-strip probe.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260822-68e6b1, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260822-68e6b1)
REVIEW VERDICT rev2: ACCEPTED (accept_cr, CR-TASK-260822-slgewd-2 -> accepted, element parked at to-review). Evidence: TASK-260822-slgewd_review-verdict-rev2.md.

Rev1 blocker CLOSED, verified by the reviewers OWN probes, not by reading the producers tests. (1) My wipeSystem, rewritten from scratch and driven through real agentic.BuildPlan, now fails against the regenerated qwen/exec: 1 EnvRemoved difference, over-removing exactly the four seeded survivors. Strip those four back out of the recorded parent_env and it byte-matches again - so the bystanders and nothing else are the catch. (2) The near-miss claim isolated to ONE CHARACTER: a port byte-correct in every other respect that reaches for HasPrefix(CODEX_) instead of the exact key set produces EXACTLY ONE difference whose sole moved key is CODEX_LIKE_BUT_NOT; the exact-key control matches first, and dropping only that key makes the defect invisible. (3) Regeneration honesty: re-ran the capture script myself, all 14 goldens rewritten, diff -r byte-identical, source ed48781 HEAD+porcelain hash unchanged before/after; the raw capture carries fresh randomized t.TempDir nonces, so masking collapsed genuinely different paths rather than rewriting placeholders. (4) Bystander pin reds on a scratch delete of one key from claude/prompt-mode; dropping all four from qwen/exec reds four tests in the documented order, fixture-level pin firing before the behavioural negatives with an actionable message. (5) README documents the convention as capture contract with the per-key table, the source exact-key citation and the bold recapture warning, and TestFixturesReadmeNamesWhatTheGoldensDoNotProve iterates parityBystanderKeys so it cannot drift. Both rev1 nits fixed.

Gates in the candidate tree, each foreground: gofmt -l pkg/ 0 no paths, make vet 0, make build 0, env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 0. Working tree write-tree = ba63a0d0ef55871f17ad44f8c4fa3af912383d93 = CR candidate OID, verified before review, after every scratch mutation, after the full regeneration and after the gates. All mutations scratch-only and reverted; both reviewer scratch test files deleted before the gates. Source checkout read-only throughout.

NON-BLOCKING RESIDUAL: golden_test.go:50 says two _LIKE_BUT_NOT keys are near-misses; there are three. Doc-comment drift only - the README table and parityBystanderKeys both list all three, no test reads the sentence. Fold in on the next touch.

The three port tasks unblock: a port now proves BOTH bounds of its environment filter - strips at least these, strips no more than these, by exact key - through one ComparePlan call.
agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260822-68e6b1, pid=83252, exit=0)

## Precondition Resources
(none)

## Outcome Resources
- [TASK-260822-slgewd_spawn-log_-implementer--developer--claude-_RUN-260822-5e9f5d.log](file://TASK-260822-slgewd/TASK-260822-slgewd_spawn-log_-implementer--developer--claude-_RUN-260822-5e9f5d.log) — System spawn log captured by task-board
- [TASK-260822-slgewd_results.md](file://TASK-260822-slgewd/TASK-260822-slgewd_results.md) — Parity goldens + comparison harness: what landed, AC evidence, gate exit codes, mutation evidence, and the port-task contract
- [TASK-260822-slgewd_source-raw-capture.json](file://TASK-260822-slgewd/TASK-260822-slgewd_source-raw-capture.json) — Unmasked output of skill-project-management TestCaptureLaunchSurface at ed4878123061b39fdae67160f6b5632117b48a2f, before this repo's masking; the input the goldens were generated from
- [TASK-260822-slgewd_change-request_rev1.patch](file://TASK-260822-slgewd/TASK-260822-slgewd_change-request_rev1.patch) — Change Request CR-TASK-260822-slgewd-1 revision 1 candidate patch (repository_delta=present, 30 changed paths)
- [TASK-260822-slgewd_spawn-log_-reviewer--reviewer--claude-_RUN-260822-95c1b6.log](file://TASK-260822-slgewd/TASK-260822-slgewd_spawn-log_-reviewer--reviewer--claude-_RUN-260822-95c1b6.log) — System spawn log captured by task-board
- [TASK-260822-slgewd_review-verdict.md](file://TASK-260822-slgewd/TASK-260822-slgewd_review-verdict.md) — Reviewer verdict: changes requested. Pinned-env decision judged correct and strips positively visible; provenance re-verified by regenerating all 14 goldens byte-identically; comparator and masking pins bit on 5 scratch mutants plus 4 reviewer-planted plan defects. Blocking: demonstrated bypass — a qwen port that wipes the whole parent env byte-matches qwen/exec, the only fixture with zero surviving parent keys.
- [TASK-260822-slgewd_spawn-log_-implementer--developer--claude-_RUN-260822-62f90d.log](file://TASK-260822-slgewd/TASK-260822-slgewd_spawn-log_-implementer--developer--claude-_RUN-260822-62f90d.log) — System spawn log captured by task-board
- [TASK-260822-slgewd_results-rev2.md](file://TASK-260822-slgewd/TASK-260822-slgewd_results-rev2.md) — Revision 2: qwen/exec preservation-bound fix — bystander keys, regenerated goldens, two new BuildPlan negatives with narrowings, gate results
- [TASK-260822-slgewd_change-request_rev2.patch](file://TASK-260822-slgewd/TASK-260822-slgewd_change-request_rev2.patch) — Change Request CR-TASK-260822-slgewd-2 revision 2 candidate patch (repository_delta=present, 30 changed paths)
- [TASK-260822-slgewd_spawn-log_-reviewer--reviewer--claude-_RUN-260822-68e6b1.log](file://TASK-260822-slgewd/TASK-260822-slgewd_spawn-log_-reviewer--reviewer--claude-_RUN-260822-68e6b1.log) — System spawn log captured by task-board
- [TASK-260822-slgewd_review-verdict-rev2.md](file://TASK-260822-slgewd/TASK-260822-slgewd_review-verdict-rev2.md) — Reviewer verdict for CR revision 2: ACCEPTED. Rev1 preservation-bound blocker closed, verified by independent reviewer probes through agentic.BuildPlan.

## Created
2026-08-22T15:30:38Z

## Last Update
2026-08-22T16:25:29Z

## Assigned To
[reviewer] reviewer (claude)
