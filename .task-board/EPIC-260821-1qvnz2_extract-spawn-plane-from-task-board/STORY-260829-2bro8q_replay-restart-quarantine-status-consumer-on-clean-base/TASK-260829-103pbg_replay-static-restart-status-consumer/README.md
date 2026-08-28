# TASK-260829-103pbg: replay-static-restart-status-consumer

## Description
Apply immutable TASK-260829-1kpj01 Change Request revision 3 patch SHA-256 87739e445c46446e2698462f1f0dc6eeea106145a82e1436c40629f6981e6109 to exact clean selected base 3bec0baf. This is a replacement delivery lane only; do not edit move abort clean integrate or close the historical Task and Stories.

## Scope
Exactly LOGBOOK.md docs/architecture.md docs/shipped-state.md pkg/localruntime/decode.go pkg/localruntime/decode_test.go pkg/localruntime/status.go pkg/vendorplugin/vendors/local-models/availability.go and availability_test.go. Static fixtures only. No live model Pi daemon HTTP endpoint Unix socket runtime service or model harness.

## Acceptance Criteria
1. Workspace selected base HEAD local main origin main and GitHub main equal 3bec0baf before replay. 2. Applied candidate changes exactly the named eight paths and patch digest matches immutable revision 3. 3. Legacy pre-extension and complete current lifecycle cohorts decode additively while partial mixed null malformed timestamp and unproven provenance cohorts fail closed. 4. CheckAvailability maps active restart_not_before to validator-safe Limited Until and quarantined evidence to the reviewed verdict without inferring from restart_count half_open or historical fields. 5. make vet make build env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 and make regress pass. 6. New independent review and managed integration complete with no reparent and no historical-lane mutation.
