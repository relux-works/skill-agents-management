# TASK-260822-slgewd: parity-goldens-and-comparison-harness

## Description
The parity mechanism for the whole story. Generate launch-surface goldens from the extraction source using its own proven capture harness (TestCaptureLaunchSurface in tools/board-cli/internal/spawn/parity_capture_test.go, gated by SPAWN_PARITY_CAPTURE_OUT), one JSON per (system, mode): binary, argv, env added/removed, stdin bytes. Land those goldens as fixtures here plus a comparison harness that asserts an agentic.Plan byte-matches its golden, with machine-local noise (PATH seeding, temp dirs) masked the way the source masked it. The goldens are the contract every port task proves against; a port without a golden is a port on trust.

## Scope
(define task scope)

## Acceptance Criteria
1. Goldens generated from the real source repo capture harness, one per (system, mode) it covers, committed as fixtures with the source commit recorded inside each file. 2. A comparison harness maps agentic.Plan to the golden schema and fails on any field difference, with masking rules documented and pinned by a test that fails if masking widens. 3. A deliberately wrong plan demonstrably fails against its golden (negative test). 4. Combinations the source could not capture are listed in the fixtures README with the source's own stated reasons.
