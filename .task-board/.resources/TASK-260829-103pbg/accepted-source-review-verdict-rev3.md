# TASK-260829-1kpj01 review verdict — accepted (revision 3)

Reviewed Change Request `CR-TASK-260829-1kpj01-3` revision 3 against base `3bec0baf9a0c897b0f76e1182e371a25132fa509` and candidate tree `f88509f0d604424a41972dee42e9e502c92eb357` (verified by computing `git write-tree` over the worktree — exact match).

## Verdict

Accepted.

## What changed since revision 2 (changes-requested)

Revision 2 was blocked on: a valid current-cohort response missing one field (e.g. `quarantined_until` omitted while `restart_not_before`/`half_open` were present) was laundered through the legacy-absence gate into a false `Healthy` verdict, because `RestartNotBeforePresent` alone gated the "is this a legacy response" branch.

Revision 3 adds `validateRestartStatusCohort` in `pkg/localruntime/decode.go`: the six restart/quarantine lifecycle fields must appear as one of exactly three recognized cohorts (zero fields / the four-field pre-deadline set / the complete six-field current set); any other mix is `ErrDecodeFailure`. This is backed by:
- `TestDecodeStatusRestartExtensionPartialCohortsAreRefused` — narrows the current cohort one field at a time at the decode layer.
- `TestCheckAvailabilityRefusesPartialRestartStatusCohorts` — the same narrowing driven through `localruntime.CLIStatusReader` and the real `vendorplugin.CheckAvailability` production call site, asserting `Unknown` + failure evidence (not just a decode error).

## Independent verification

- Confirmed tree OID by staging the worktree and running `git write-tree`: `f88509f0d604424a41972dee42e9e502c92eb357`, exact match to the CR.
- Checked the actual landed `relux-agents-infra` wire contract at `origin/main` `675f77ed63376320ed1213f46f9462a299c0abaf` (`SharedRuntimeStatus` in `pi_shared_operator_darwin.go`): all six restart/quarantine fields (`restart_count`, `restart_not_before`, `quarantined_until`, `last_readiness_match`, `manual_quarantine`, `half_open`) serialize unconditionally, no `omitempty` — the candidate's cohort-gate assumption matches production reality exactly. PR #10 reference in `docs/architecture.md` is accurate (`Merge pull request #10 from relux-works/codex/infra-restart-status`).
- Reproduced the exact rev2 failure scenario independently with a scratch in-package test (not committed) driving `CheckAvailability` with a fixture missing only `quarantined_until`: before this fix that would have returned `Healthy`; against the candidate it returns `Unknown` with `Failures:[{Source:local-runtime status Reason:"...partial cohort (pre-extension 3/4, current-only 2/2)"}]`. Confirms the fix is real, not just self-consistent with its own new tests.
- `go build ./...` — clean.
- `go test -count=1 ./...` — full suite green (all packages, including `pkg/localruntime`, `pkg/vendorplugin/...`, `internal/regress`).
- `go vet ./...` / `make vet` — clean.
- `make regress` — pass.
- `gofmt -l .` — no tracked file in the candidate's 8-path delta is unformatted (the one flagged path is an untracked `.temp/` scratch file from a prior reviewer, not part of this CR).
- `go test -count=1 -cover ./pkg/localruntime ./pkg/vendorplugin/vendors/local-models` — 91.6% / 81.2% statement coverage.

## AC / DoD check

- AC1 (blocked until infra Handoff A lands): infra `675f77e` (PR #10) landed the persisted ledger and public `restart_not_before`/`quarantined_until`/`last_readiness_match`/`manual_quarantine`/`half_open` fields; this task correctly started only after that.
- AC2: `pkg/localruntime` decodes the fields per the presence/type matrix, additive-safe, now with a sound cohort gate.
- AC3: `local-models.Availability()` reaches `Limited` (this codebase has no separate `Backoff`/`Quarantined` enum — confirmed by reading `pkg/vendorplugin/availability.go`) only when evidenced by a future `quarantined_until` or `restart_not_before`, driven through the real `CheckAvailability` entry point (`TestCheckAvailabilityConsumesRestartExtensionFixtures`). `restart_count`/`half_open` never mint a verdict on their own (explicitly tested).
- AC4: case 27a (`TestDecodeStatusPreRestartDeadlineFixture`) and 27b (`TestDecodeStatusPostRestartDeadlineFixture`, `TestCheckAvailabilityConsumesRestartExtensionFixtures`) both pass.
- AC5: `docs/architecture.md` and `docs/shipped-state.md` describe the mapping accurately (`Limited` with a source-labeled deadline), no overclaim of a "Backoff"/"Quarantined" state that doesn't exist in the enum.
- Negative/gating coverage: wrong-type, null, partial-cohort, and malformed-deadline cases all named against the real production call site, not just helper-minted `Status` structs.
- `LOGBOOK.md` carries three dated entries: the rev3 cohort fix, the presence/absence distinction, and the JSON-null-into-scalar root cause.

No further findings. Routing to `accept_cr`.
