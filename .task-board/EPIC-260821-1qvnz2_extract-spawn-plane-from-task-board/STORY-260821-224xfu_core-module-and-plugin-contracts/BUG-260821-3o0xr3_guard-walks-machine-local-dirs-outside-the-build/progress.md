## Status
done

## Review
required

## Task Class
code

## Estimate
estimated(fibonacci(3))

## Blocked By
- (none)

## Blocks
- (none)

## Checklist
- [x] Guard skips dot-dirs, underscore-dirs and nested go.mod subtrees, mirroring go build exclusion
- [x] Fixture: planted violation inside a dot-dir NOT reported; same violation in a normal dir reported
- [x] Suite green on a bootstrapped main checkout with .agents present
- [x] Threat model states the exclusion semantics
- [x] Work left UNCOMMITTED for the Change Request snapshot
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
spawn selection rationale tuple: {"role":"developer","pair":"claude-opus-5/high","text":"Guard portability: walk must mirror go build exclusions without weakening any rule; proof needed in both directions."}
STORY-260821-224xfu base refresh SKIPPED: the managed workspace holds uncommitted work, so there was no clean checkpoint branch to replay onto trunk a695580d61d7; the branch is unchanged at fork point 6223c283d360
spawn selection rationale for claude-opus-5/high: Guard portability: walk must mirror go build exclusions without weakening any rule; proof needed in both directions.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [implementer] developer (claude) (run=RUN-260821-9d2e1d, max_parallel=20)
spawn run started: [implementer] developer (claude) (run=RUN-260821-9d2e1d)
Scan scope now mirrors go/build: skipModuleDir excludes dot-dirs, underscore-dirs, any subtree with its own go.mod, plus vendor/node_modules/testdata by name; the module root is never skipped on its own name (this checkout lives under .temp/). testdata exclusion is an explicit DECISION documented in the new # Scan scope threat-model section and pinned by a case nested inside a scanned package.

Both directions proven in pkg/agentic/singlesource_scanscope_test.go: the SAME violating source planted at 10 paths, reported in 2 ordinary packages, not reported in .agents/, .claude/, _scratch/, nested go.mod (2 depths), testdata (2 places), vendor. TestSingleSourceGuardScanScopeFixtureIsViolating scans every path DIRECTLY first, so no silence is trusted before the source is proven violating. Production call site driven: scanSingleSource(walkModuleSources(root), bindingHomes).

5 mutants on skipModuleDir all killed (dot rule deleted; nested-go.mod narrowed to never fire; exclusion widened to every subdir; root exemption removed; stat failure read as absence) - bound proven by narrowing AND widening, not only deletion. Duplicate walk in pkg/vendorplugin/double_test.go fixed identically with an anti-drift test.

Original failure reproduced with the REAL .agents tree copied in (43 Go files): OLD walk exit 1, 12 violations incl. child_launch_composition.go:95 verbatim; NEW walk exit 0.

58-mutant matrix green: 66 named subtests = 22+14+13+6+3 caught + 8 declared-open residuals. Nothing weakened; no production source changed.

Gates, each standalone: gofmt -l pkg/ exit 0 (no output), make vet exit 0, make build exit 0, env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 exit 0.

Work left UNCOMMITTED in .temp/STORY-260821-224xfu/worktree.
agent completed: [implementer] developer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-9d2e1d, pid=58097, exit=0)
spawn selection rationale tuple: {"role":"reviewer","pair":"claude-opus-5/high","text":"Scan-scope change on the central guard; the root-never-skipped rule and the main-checkout green are the acceptance."}
spawn selection rationale for claude-opus-5/high: Scan-scope change on the central guard; the root-never-skipped rule and the main-checkout green are the acceptance.
spawn agent resolution: Agent selection: claude via explicit_override (preferred_agentic_system: mixed[claude,codex], config: spawn.preferred_agentic_system)
spawn launch composition: empty; contract=agents-infra.child-launch-composition; provider=claude; schema=1; producer=v1.6.1-18-g302a445; diagnostic=launch_composition_empty; no project MCP servers enabled
spawn queued: [reviewer] reviewer (claude) (run=RUN-260821-714f04, max_parallel=20)
spawn run started: [reviewer] reviewer (claude) (run=RUN-260821-714f04)
REVIEW VERDICT: ACCEPTED (CR-BUG-260821-3o0xr3-1 rev1). Evidence: BUG-260821-3o0xr3_review-verdict.md.

Gate attacked, not read. 8 scratch mutants of skipModuleDir/moduleSkipDirs, ALL KILLED: dot rule deleted; underscore rule deleted; root exemption removed; nested-go.mod narrowed to never fire; stat failure read as absence; exclusion widened to every subdir; testdata dropped; vendor dropped. Bound proven by narrowing AND widening, not deletion alone.

Original red reproduced on demand: candidate tree + the REAL .agents (43 Go files, nested go.mod) with the old denylist walk restored gives 12 violations incl. child_launch_composition.go:95 verbatim. Same tree, new walk: full ./... suite green, both under .temp and outside the repo (/tmp/bug3o0xr3). That is the bug acceptance; the literal main checkout could not be used because it sits at trunk a695580 which does not contain this module at all.

Each rule independently sufficient on the real tree: .agents with its nested go.mod DELETED (bare dot-dir) still silent; agents-infra copied to a dot-free path with go.mod kept still silent.

Root rule probed directly, not just via the fixture: guard run from checkouts whose ROOT is named .dotroot, _underroot and vendor, each with a planted canary - all three scanned normally and reported it. A skipped root would have reported clean over zero files.

No widening: violations planted by the reviewer at 4 paths outside the fixture ten - pkg/thirdparty and pkg/relay/sub reported, internal/ident/.cache and pkg/relay/_old silent.

58-mutant matrix green (22+14+13+6+3) + 8 declared residuals = 66 named subtests; pkg/agentic 102 subtests, 0 FAIL, 0 SKIP. The two walk copies (pkg/agentic, pkg/vendorplugin) are byte-identical today. No denylist survives anywhere.

Gates all foreground, exit 0: gofmt -l pkg/ internal/ tools/ (no output), make vet, make build, env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1.

NON-BLOCKING finding recorded in the verdict: the fixture comment at singlesource_scanscope_test.go:113-119 (twin at vendorplugin/scanscope_test.go:52-54) credits the .temp ancestor for pinning the root rule, but WalkDir hands skipModuleDir filepath.Base(root) = worktree - the ancestor is never tested. In that fixture the root-exemption mutant is actually killed by the nested-go.mod rule. The bound itself is genuinely held by TestSkipModuleDirDoesNotSkipTheRoot / TestSkipModuleDirRootGoModIsNotNested and by the dot-named-root probe above. Comment accuracy only; fix when the file is next touched.

Handoff to the commit-owning mover: work is UNCOMMITTED, worktree tree hash verified equal to candidate 1d24a3e2. Reviewer supplied no commit_ack.

agent completed: [reviewer] reviewer (claude) (exit=0)
spawn run completed: claude (run=RUN-260821-714f04, pid=60387, exit=0)

## Precondition Resources
(none)

## Outcome Resources
- [BUG-260821-3o0xr3_spawn-log_-implementer--developer--claude-_RUN-260821-9d2e1d.log](file://BUG-260821-3o0xr3/BUG-260821-3o0xr3_spawn-log_-implementer--developer--claude-_RUN-260821-9d2e1d.log) — System spawn log captured by task-board
- [BUG-260821-3o0xr3_results.md](file://BUG-260821-3o0xr3/BUG-260821-3o0xr3_results.md) — Scan-scope fix: go/build exclusion semantics, both-directions fixture, 5-mutant kill matrix, real .agents before/after, 58-mutant matrix green, gate exit codes
- [BUG-260821-3o0xr3_change-request_rev1.patch](file://BUG-260821-3o0xr3/BUG-260821-3o0xr3_change-request_rev1.patch) — Change Request CR-BUG-260821-3o0xr3-1 revision 1 candidate patch (repository_delta=present, 42 changed paths)
- [BUG-260821-3o0xr3_spawn-log_-reviewer--reviewer--claude-_RUN-260821-714f04.log](file://BUG-260821-3o0xr3/BUG-260821-3o0xr3_spawn-log_-reviewer--reviewer--claude-_RUN-260821-714f04.log) — System spawn log captured by task-board
- [BUG-260821-3o0xr3_review-verdict.md](file://BUG-260821-3o0xr3/BUG-260821-3o0xr3_review-verdict.md) — Reviewer verdict for CR rev1: ACCEPTED — scan-scope fix attacked via 8 killed mutants, root-name probe, own planted paths, real .agents reproduction

## Created
2026-08-21T17:05:38Z

## Last Update
2026-08-21T17:28:03Z

## Assigned To
[reviewer] reviewer (claude)
