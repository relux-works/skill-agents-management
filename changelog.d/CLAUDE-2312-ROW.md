- Qualify Claude Code 2.1.312 against its installed binary and admit a
  `permission-grammar-v3` yolo row mapping to `--dangerously-skip-permissions`.
  V3 retains every V2 refusal and also refuses `--inherit-permission-mode`,
  `--plan-mode-required`, `--permission-prompts` and `--enable-auto-mode` under
  yolo, since their interaction with the bypass is not qualified. It also
  refuses `--managed-settings`, `--project-config-root`, an explicit
  `--settings` source with a top-level member outside `$schema`, `model`,
  `permissions` and an empty `env` (for example `hooks`, whose PreToolUse
  decision overrides the bypass per call), and one whose `permissions` object carries anything besides
  `allow`, `deny`, `ask` and `additionalDirectories` (for example
  `disableBypassPermissionsMode` or `defaultMode`), or a non-empty top-level
  `env` (2.1.312 applies it before resolving the mode, and
  `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` forces mode `default`). Native forwards them.
  The option and eager tables, renamed `claude-options.tsv` and
  `claude-eager.tsv`, gain the hidden required options `--fast-mode` and
  `--runner-declares`, and describe in-window releases only. The pinned native
  comparisons are keyed by release and run against the installed 2.1.312;
  2.1.288 is out of the window and has no comparison row.
- Retire the Claude 2.1.261 yolo row under the owner's harness support window
  (releases from about the last two weeks, 2026-10-29). The Claude option
  tables describe in-window releases only, so yolo on 2.1.261 and every other
  unlisted release now fails closed as unverified; native forwards verbatim.
  Stay on an older module release to run an older harness.
