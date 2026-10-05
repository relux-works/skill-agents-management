- `agentic.Plan` carries a typed `Session *PlanSession` — the native session
  `Name` (nil when the argv sets none), `RCEnabled`, and `RCIndices`, the exact
  positions of the remote-control tokens in the SAME plan's `Argv`. The
  claude-code plugin fills it from its own pinned argv grammar (no second
  parser, no plugin-id comparison) through the optional `SessionPlanner`
  capability while `BuildPlan` runs, so the record travels inside the
  registry-built plan: there is no projector function, registry projector
  binding or registration call to reach around, and the retired
  `ProjectSession`/`RegisterSessionProjector` surface is gone. The registry's
  registration is the trust boundary: `SessionPlanner.FillPlanSession` takes
  only an `agentic.SessionFill`, which only this module can issue (no exported
  field or constructor; the zero value is refused), so no exported function
  derives a Session from caller-supplied argv; a registered plugin that embeds
  `*claude.System` owns its own plans (accepted scope). `Session` is nil for a
  system with no native session surface (never a guess) and present for every
  Claude plan: separated, `=` and `-nNAME` forms record, an enabled RC with
  empty-but-present indices is the settings origin (an explicit `--settings`
  source carrying `remoteControlAtStartup: true`, selected exactly as the tool
  policy selects it), and a silent argv records disabled with no indices.
  Repeated selector classes, names that disagree across classes, required
  selectors dangling without a value, and a settings key that is not a boolean
  refuse typed with `ErrSessionInvalid` at `BuildPlan`. `BuildPlan` also refuses
  a plugin record that is incoherent — nil or out-of-bounds or repeated
  indices, or a disabled RC carrying indices — as `ErrPluginContract`. The
  record rides the exec seal IN-PROCESS ONLY: a `Session` changed after sealing,
  dropped included, refuses `FinalizePlan` and a finalized in-process verifier
  with `ErrFinalizedProcessChanged`, presence (nil versus present), name,
  `RCEnabled` and `RCIndices` bound, collection presence (nil versus empty
  `RCIndices`) included; `FinalizePlan` derives the record again over the final
  argv when a `NativeTail` extends it. ACROSS `ExportSeal`/`ImportSeal`, `Session`
  is UNVERIFIED: payload contract 1.0.0 is frozen with the Claude exec guard
  unsealed and carries no Session, and the settings-origin remote control
  depends on inputs outside argv, so `ImportSeal` derives nothing, binds nothing
  and reads no settings, and the plan an imported process rebuilds
  (`ImportedProcess.Process`) carries a nil `Session`. A consumer that needs one
  across a process boundary carries it itself and treats it as unverified. Other
  bounds: an unfinalized base guard carries no argv, so nothing binds its Session
  across import; RC intent is what the argv and the explicit settings source
  declare, not what the provider honors under managed policy.
