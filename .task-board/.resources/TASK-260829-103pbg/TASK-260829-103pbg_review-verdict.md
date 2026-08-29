# TASK-260829-103pbg review verdict \u2014 accepted (CR-TASK-260829-103pbg-1, revision 1)

## Identity verification

- Local HEAD, local `main`, cached `origin/main`, and verified GitHub `main` all resolve to `3bec0baf9a0c897b0f76e1182e371a25132fa509` at review time.
- `git write-tree` over the dirty worktree produced `f88509f0d604424a41972dee42e9e502c92eb357`, exact match to the CR's candidate tree OID.
- `git diff HEAD` hashes to `87739e445c46446e2698462f1f0dc6eeea106145a82e1436c40629f6981e6109` (sha256), exact match to the immutable revision-3 patch digest and to the resource's declared hash.
- Exactly the 8 named paths are dirty: LOGBOOK.md, docs/architecture.md, docs/shipped-state.md, pkg/localruntime/{decode.go,decode_test.go,status.go}, pkg/vendorplugin/vendors/local-models/{availability.go,availability_test.go}.
- `TASK-260829-1kpj01` (to-review), `STORY-260829-17byw1` (to-review), and `STORY-260829-2qq4oo` (backlog) all remain in their pre-existing states \u2014 no historical-lane mutation.

## Attacks performed (not just read)

1. **Cohort gate mutant**: commented out the `validateRestartStatusCohort` call in `decode.go` and reran the full localruntime + local-models suites. Result: 6 `TestDecodeStatusRestartExtensionPartialCohortsAreRefused` subtests and 6 `TestCheckAvailabilityRefusesPartialRestartStatusCohorts` subtests (driven through the real `vendorplugin.CheckAvailability` \u2192 `localruntime.CLIStatusReader` production path) went red, with one partial-cohort fixture flipping to a false `healthy` verdict. Confirms the gate is real and load-bearing, not decorative. Reverted; tree hash re-verified to match the candidate exactly after restore.
2. **Presence gate mutant**: removed the `!status.RestartNotBeforePresent \u2192 UnknownAfterCheck` short-circuit in `mapAvailability`. Result: `TestCheckAvailabilityConsumesRestartExtensionFixtures/pre-deadline_fixture_cannot_prove_backoff_inactive` immediately flipped from `unknown` to a false `healthy`. Confirms a legacy/pre-extension response (missing `restart_not_before`) cannot be laundered into a clean verdict. Reverted; tree hash re-verified.
3. Confirmed `TestBuildLaunchAdmitsLocalQwenThroughTheRealPiPreflight` / `TestBuildLaunchRefusesLocalQwenWhenPreflightRefuses` exercise the separate Preflight admit/refuse table (`vendorplugin.BuildLaunch`), not `mapAvailability` \u2014 the new presence gate does not regress those, by design (different question, per the code's own divergence note).
4. Verified restart_count/half_open non-influence: `TestCheckAvailabilityConsumesRestartExtensionFixtures/historical_restart_count_and_half-open_do_not_imply_backoff` sets both to non-default/"interesting" values (`restart_count=2`, `half_open=true`) alongside nil deadlines and asserts `Healthy` through the real production entry point \u2014 a proxy-signal inference bug would fail this.
5. Verified null vs. absent vs. malformed are three distinct, individually tested states for every nullable field (`optionalNullableTimestamp`, `optionalBool`, `optionalNonNegativeInt`), each refusing JSON `null` where the wire type is non-nullable and refusing wrong-typed/negative values as `ErrDecodeFailure`, exercised via `TestDecodeStatusRestartExtensionWrongTypesAreRefused` and the production-path `TestCheckAvailabilityRefusesMalformedRestartDeadlineAsAReadFailure`.

## Build/test/regress evidence

- `make vet` \u2014 clean.
- `make build` \u2014 clean.
- `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` \u2014 full suite green, all packages.
- `make regress` \u2014 pass.
- `gofmt -l` on all 5 changed Go files \u2014 no output (clean).
- `go test -cover ./pkg/localruntime/... ./pkg/vendorplugin/vendors/local-models/...` \u2014 91.6% / 81.2% statement coverage.

## AC / DoD

1. Base identity confirmed exact (HEAD/main/origin/GitHub all `3bec0baf...`). \u2714
2. Candidate paths and patch digest match immutable revision 3 exactly. \u2714
3. Legacy and complete-current cohorts decode additively; partial/mixed/null/malformed/unproven-provenance cohorts fail closed as `ErrDecodeFailure` \u2192 `Unknown` with failure evidence through the real production call chain \u2014 proven by mutation, not just by reading. \u2714
4. `CheckAvailability` maps an active `restart_not_before` to `Limited`/Until, maps active `quarantined_until` to `Limited`/Until (checked ahead of the backoff branch), and never infers a verdict from `restart_count`, `half_open`, or an elapsed/historical deadline \u2014 proven by mutation and by the dedicated non-inference test case. \u2714
5. `make vet`, `make build`, `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1`, `make regress` all pass. \u2714
6. No reparent, no mutation of the historical `TASK-260829-1kpj01` / `STORY-260829-17byw1` / `STORY-260829-2qq4oo` lineage \u2014 confirmed via read-only board queries before and unchanged after review. \u2714

No findings. Routing to `accept_cr`.
