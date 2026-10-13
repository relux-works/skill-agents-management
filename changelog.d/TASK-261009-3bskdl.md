- Add `LocalProviderBinding.Settings`, a typed, module-owned input that carries
  operator settings into the Codex local-provider argv before `SealExecPlan`,
  so `VerifyBeforeExec` holds for a plan built with them. The Codex plugin
  validates every carried key against its own closed allow-list — only the
  cosmetic/output-shape keys `hide_agent_reasoning` and
  `show_raw_agent_reasoning` (boolean display switches, exactly `true` or
  `false`) are admitted — and refuses credential, provider, model-identity,
  sandbox, approval, network, tool and unknown keys typed at a pre-planning
  boundary before binary resolution and any file read. Note that
  `show_raw_agent_reasoning` surfaces raw reasoning content on stderr, which
  consumers that capture stderr may persist in logs or transcripts. The
  settings ride the sealed argv, so a post-seal argv change still refuses as
  conflicting; the subscription (non-local) path is unchanged.
  `codex.AllowedLocalProviderSettings` reports the allow-list.
