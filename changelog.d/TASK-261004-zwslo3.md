- `FinalizePlan` accepts a typed managed-session reservation: exactly two
  typed slots carrying server-supplied values at module-fixed positions, env
  `TASK_BOARD_MANAGED_SESSION_ID` (the SES handle, last entry of the final
  environment) and argv `--session-id` (the native UUID, first two tokens of
  the final argv). The reservation is the opaque `FinalizeOverlays.Reservation`;
  the module never generates a value. Any other binding, a third slot, a slot
  value that differs from the reservation, a forged SES or UUID shape, a
  resume or adoption intent or an identity-bearing new intent (the
  reservation's `Intent` obeys `ValidateResumeIntent`, an empty `Kind` is
  new, or a selector the
  plugin's own resume grammar finds in the sealed base or native tail), an
  env that already carries the slot, and a system with no native session
  grammar refuse typed (`ErrFinalizeBindingUnknown`,
  `ErrFinalizeReservationRefused`). The finalized verifier binds both slots
  and names a changed one; `Plan.Session` is derived again over the shifted
  argv. The slot binding is in-process only; Phase 1 is new launches only,
  and the reserve endpoint stays the daemon's.
