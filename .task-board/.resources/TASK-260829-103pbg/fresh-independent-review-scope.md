# TASK-260829-103pbg independent review scope

Review only new Change Request `CR-TASK-260829-103pbg-1` revision 1.

## Immutable identity

- Base: `3bec0baf9a0c897b0f76e1182e371a25132fa509`
- Candidate tree: `f88509f0d604424a41972dee42e9e502c92eb357`
- Patch SHA-256:
  `87739e445c46446e2698462f1f0dc6eeea106145a82e1436c40629f6981e6109`
- Changed paths: exactly the eight paths listed in the Task AC and producer
  evidence.

The historical accepted verdict is semantic evidence only. It is not authority
to accept this replacement CR.

## Required attacks

- Drive the production `vendorplugin.CheckAvailability` entry point with legacy,
  pre-deadline, complete-current, one-field-at-a-time partial, mixed, null,
  malformed timestamp/type, elapsed-deadline, restart-count-only, half-open-only,
  attested, and unattested fixtures.
- Prove malformed or incomplete lifecycle cohorts fail closed as `Unknown` and
  cannot be laundered into `Available`, active `Limited`, or `Quarantined`.
- Prove `restart_count`, `half_open`, null deadlines, and elapsed historical
  deadlines never infer an active availability limit.
- Attack any field-presence or timestamp/provenance gate with a narrowed mutant
  that reaches the production entry point; positive-path-only review is not
  acceptance evidence.
- Re-run `make vet`, `make build`,
  `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1`, and `make regress`.
- Verify the historical Task resources and both historical Story worktree diff
  fingerprints remain byte-identical.

Do not contact a live model, Pi daemon, HTTP endpoint, Unix socket, runtime
service, model harness, or user-owned model state. Record exactly one explicit
verdict through the Change Request lifecycle.
