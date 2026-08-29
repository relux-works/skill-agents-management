# TASK-260829-103pbg implementation evidence

Date: 2026-08-29 (Europe/Moscow)

## Exact-base replay

- Story workspace branch: `task-board/story/STORY-260829-2bro8q`
- `HEAD`: `3bec0baf9a0c897b0f76e1182e371a25132fa509`
- local `main`: `3bec0baf9a0c897b0f76e1182e371a25132fa509`
- fetched `origin/main`: `3bec0baf9a0c897b0f76e1182e371a25132fa509`
- GitHub `refs/heads/main` through `git ls-remote origin`: `3bec0baf9a0c897b0f76e1182e371a25132fa509`
- Immutable source patch SHA-256: `87739e445c46446e2698462f1f0dc6eeea106145a82e1436c40629f6981e6109`
- Replayed working-tree binary diff SHA-256: `87739e445c46446e2698462f1f0dc6eeea106145a82e1436c40629f6981e6109`
- Candidate tree: `f88509f0d604424a41972dee42e9e502c92eb357`

The replay changes exactly these eight paths:

1. `LOGBOOK.md`
2. `docs/architecture.md`
3. `docs/shipped-state.md`
4. `pkg/localruntime/decode.go`
5. `pkg/localruntime/decode_test.go`
6. `pkg/localruntime/status.go`
7. `pkg/vendorplugin/vendors/local-models/availability.go`
8. `pkg/vendorplugin/vendors/local-models/availability_test.go`

## Behavioral evidence

The immutable tests drive additive legacy/pre-deadline and complete-current cohorts, nullable deadlines, malformed timestamps/types, and one-field-at-a-time partial cohorts. The production-path tests construct `localruntime.CLIStatusReader` and invoke `vendorplugin.CheckAvailability`.

The asserted mapping is evidence-only:

- a future `quarantined_until` or `restart_not_before` produces validator-safe `Limited` with the wire deadline;
- absent `restart_not_before` in the recognized pre-deadline cohort produces checked `Unknown`;
- malformed or partial reads produce `Unknown` with failure evidence;
- `restart_count`, `half_open`, null deadlines, and elapsed historical deadlines never infer an active limit;
- existing production `CheckAvailability` mapping cases preserve attested/unattested provenance behavior.

Targeted command:

`go test -mod=mod ./pkg/localruntime ./pkg/vendorplugin/vendors/local-models -run 'TestDecodeStatus(PreRestartDeadlineFixture|PostRestartDeadlineFixture|RestartExtensionWrongTypesAreRefused|RestartExtensionPartialCohortsAreRefused)|TestCheckAvailability(ConsumesRestartExtensionFixtures|RefusesMalformedRestartDeadlineAsAReadFailure|RefusesPartialRestartStatusCohorts)' -count=1`

Result: exit code 0.

## Landing validation

| Command | Exit code | Result |
| --- | ---: | --- |
| `make vet` | 0 | pass |
| `make build` | 0 | pass |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 | pass |
| `make regress` | 0 | pass |

`make regress` resolves to a static `go test` invocation for `./internal/regress/...`. No live model, Pi daemon, HTTP endpoint, Unix socket, runtime service, or model harness was contacted. `gofmt -l` over all five changed Go files returned no paths.

## Preservation audit

- The complete SHA-256 manifest of `.task-board/.resources/TASK-260829-1kpj01` is byte-identical before and after replay.
- Historical `STORY-260829-17byw1` worktree diff remains SHA-256 `87739e445c46446e2698462f1f0dc6eeea106145a82e1436c40629f6981e6109`.
- Historical `STORY-260829-2qq4oo` worktree diff remains SHA-256 `ab69661bdb998b6fe5adb69ebd9ccb81bb4e3b53df13a1e2da4067043d2a577a`.
- No historical Task, Story, worktree, resource, Change Request, or move operation was mutated.

The new immutable Change Request is intentionally published by the tracked producer completion hook after the required developer handoff. Its base, candidate tree, changed paths, and patch digest are bound to the values above.
