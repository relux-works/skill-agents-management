- Add exec-free `Plan.ExportSeal`, `DecodeSeal`, `ImportSeal`, and
  `FinalizePlan` APIs with versioned guards and typed refusals. Validate
  pre-overlay process snapshots, preserve original artifact digests, and bind
  exact finalized binary, argv and full ordered env across JSON round trips.
  Use environment names and keyed commitments instead of credential values;
  cross-process imports require a separately supplied ephemeral key. Deep-copy
  exported/imported collections; reject duplicate/unknown members, invalid
  Unicode, explicit nulls and kind-crossing members, excessive depth and oversized wire data. Keep the frozen Claude
  unsealed marker hosted-admissible; finalized bindings and sealed/local-provider
  projections remain local-only. Muse refuses unsealed guards. Required empty
  collections export as []/{}, never null, so untouched empty environments
  round-trip. BuildPlan, FinalizePlan, ExportSeal, DecodeSeal and typed ImportSeal share
  representability validation. Typed import checks all original strings before
  marshal; all entry points refuse strings that
  cannot survive guard JSON typed before acceptance; valid Unicode
  round-trips exactly. Public digests
  are transient integrity, never authentication.
