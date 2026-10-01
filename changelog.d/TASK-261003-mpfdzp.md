- Add a validated local-provider snapshot input to the Codex plugin:
  `codex.ReadProviderSnapshot` performs one read+parse+resolve of the private
  `config.toml` and returns the resolved provider entry with the SHA-256
  digest of the bytes it was parsed from, or the same typed refusals as the
  ID-only path. `LocalProviderBinding` gains an optional `Snapshot`; when
  present, Exec and DryRun plans use the pinned entry without reading
  `config.toml`, so a config mutated between plan and exec cannot change the
  launch while the manifest records the old digest. The ID-only path and the
  managed-session refusal are byte-identical to before.
