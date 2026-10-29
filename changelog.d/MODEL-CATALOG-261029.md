- Add the operator model catalog `~/.curator/catalog.toml` (schema
  `model-catalog/1`, `runtimecatalog.LoadModelCatalog`/`ParseModelCatalog`).
  It declares vendor > model > reasoning as plain strings, ADDITIVE to the
  module's own rows: new models for a registered vendor (systems, aliases,
  lifecycle, effort axis) and new effort words for a module row and its aliases
  together, kept apart as `EffortDeclaration.OperatorAccepts` so the module's
  `Vocabulary` and the frozen v2 snapshot never widen. `[[effort_scale]]` places a new effort word directly after a word
  already on the scale, and every catalog effort word must be on that effective
  scale, so a misspelt word refuses. The effective scale travels as
  `EffortDeclaration.Scale`, and `vendorplugin.EffectiveEffortsUnderBound` ranks
  bounds on it. Diagnostics name only validated identifiers. `ModelCatalog.Apply`/`ApplyAll` merge it
  into a vendor's `Models()`. Admission is unchanged: an unknown id is still
  `ErrUnknownModel`, an undeclared effort word is still refused. Malformed,
  unsupported-version, conflicting (redefining a module row, alias collision,
  extending a row with no effort axis) and unknown-vendor catalogs refuse with
  typed `runtimecatalog.Refusal` kinds; a read failure never falls back.
- `vendorplugin.Model.OperatorDeclared` marks catalog rows. `Lineup` leaves them
  out, so a catalog never changes what an unconfigured launch picks; they are
  admitted when named explicitly. Permission grammar, argv spellings and the
  frozen v2 snapshot are unaffected.
