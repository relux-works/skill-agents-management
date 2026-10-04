- Seal every Muse interactive plan at `BuildPlan` time: the sealer binds
  the absolute binary path plus its SHA-256, the exact final argv, the
  probed full build identity (1.4.1-R4503.1 or 1.4.2-R4684.1), and the
  `XDG_*`/`MUSE_NO_AUTO_UPDATE` identity, and `VerifyBeforeExec` refuses
  typed on any swap, change, mismatch, revision drift or duplicate. A
  binary attesting no verified build refuses typed at plan time instead
  of shipping unsealed. The seal exports through `ExportSeal` and
  reimports through `ImportSeal` with exact parity — including finalized
  plans, whose verifier keeps the release re-probe plus the updater-pin,
  sealed-identity and duplicate checks, and whose plugin selectors commit
  under the finalization key so a consumer holding that same explicit key
  re-imports cross-process — so hosted Muse plans verify from sealed
  data instead of recomposing; the unsealed guard stays refused for
  Muse. `FinalizePlan` refuses any overlay touching the updater pin or
  any sealed `XDG_*` selector, and the finalized verifier re-checks the
  full sealed XDG/pin identity before the release re-probe. Exported
  environment selectors carry keyed commitments, never literal values.
- Pin the Muse resume grammar for 1.4.2-R4684.1 alongside 1.4.1: both
  releases' help captures agree on every root option arity, so
  `ElevateResumeIntent` now classifies under both verified releases, and a
  future arity contradiction fails loud at load instead of guessing. Bare
  `resume` stays refused as the picker form per contract r2 section 2.
