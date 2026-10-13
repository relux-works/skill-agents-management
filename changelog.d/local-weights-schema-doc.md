- Document the exact-local-weights extension of operator schema 1 in
  `docs/operator-file-schema-v1.md`: the optional engine-owned
  `engines.<name>.weights.id` and the optional runtime-model pin
  `expected_weights_id` (exactly `gguf-sha256:<64 lowercase hex>`, never
  trimmed), mandatory only at the certified local launch gate. This version
  certifies single-file GGUF only; MLX rows stay legacy and unpinned. Unknown
  `weights` members refuse certified execution. Documentation only; readers,
  validation and the gate land with the `~/.curator` catalog launch path.
