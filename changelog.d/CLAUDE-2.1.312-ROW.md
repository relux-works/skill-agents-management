- Qualify Claude Code 2.1.312 against its installed binary and admit a
  `permission-grammar-v3` yolo row mapping to `--dangerously-skip-permissions`.
  V3 retains every V2 refusal and also refuses `--inherit-permission-mode`,
  `--plan-mode-required`, `--permission-prompts` and `--enable-auto-mode` under
  yolo, since their interaction with the bypass is not qualified; native
  forwards them and the 2.1.261 row is unchanged. The option and eager tables
  gain the hidden required options `--fast-mode` and `--runner-declares`, and
  the pinned native comparisons are release-keyed (2.1.288, 2.1.312) so they
  run against the installed 2.1.312. Neighbouring releases still refuse.
