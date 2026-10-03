- Carry local-model effort vocabularies and recommendations from `ModelEntry`
  and `local-models.toml` into vendor declarations. Required rows must declare
  both a nonempty vocabulary and an in-vocabulary recommendation to register,
  and launch with an explicit supported effort; effortless rows remain strict.
  Recommendations are guidance only and are never injected as defaults.
- Refuse local Codex rows whose declared vocabulary exceeds native catalog
  reasoning levels on both ID and snapshot paths, with consumer-shaped launch
  regressions and narrowing mutation evidence.
