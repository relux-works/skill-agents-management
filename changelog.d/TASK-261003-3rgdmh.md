- Local-provider Codex exec and dry-run launches now carry validated native
  metadata from the private operator catalog: Lite off, direct tools,
  multi-agent v1, no search or reasoning-summary parameter, and an explicit
  in-vocabulary effort. Missing metadata reports typed `absent`. The complete
  native-readable catalog subset is checked over every row from a single
  exact-key parse: duplicate recognized fields refuse typed `malformed`,
  matching native Codex, and case-variant keys are ignored rather than
  overriding protocol values. String spelling is checked before token
  normalization: invalid UTF-8, unpaired surrogate escapes, invalid escapes
  and raw control characters refuse typed `malformed` in keys and values,
  including ignored fields. Non-regular and oversized inputs refuse through
  bounded nonblocking reads. Snapshots retain catalog bytes, and plans use
  private content-addressed copies verified by the exec-free
  `Plan.VerifyBeforeExec` hook immediately before consumer-owned exec. Local
  managed-session and interactive launches remain typed `unsupported`; hosted
  launch argv and environment are unchanged. The semantic refusal census
  includes propagation, and the bounded mutation runner reports kills,
  survivors and source bounds.
