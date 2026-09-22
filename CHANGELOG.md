# Changelog

## Unreleased

- Declare the 2026-09-22 heads and their floating short spellings:
  `gpt-6-sol` (+ alias `sol`) and `gpt-6-luna` (+ alias `luna`) in `openai`,
  `claude-opus-5-5` (+ alias `opus`) in `anthropic`. All six rows are declared
  ahead of the board's registry (`declaredHereRows`), rest on a machine-local
  vendor probe (`codex debug models`, codex-cli 0.155.1; `claude -p --model`,
  Claude Code 2.1.280) and carry interpolated Bug Hunt Bench scores (sol 46,
  luna 44 between `gpt-6-astra` 48 and `gpt-5.6-sol` 42; opus-5-5 45 between
  `gpt-6-astra` 48 and `claude-fable-5-1` 43). Effort axes: sol
  `low`..`ultra`, luna `low`..`max`, opus-5-5 `low`..`max` (opus-5's axis);
  every alias shares its identity's axis. Each alias executes as its identity
  through `AliasOf` (`Plan.ModelIdentity` keeps the requested spelling). No
  display recommendation moved; no row is pi-native (the Pi 0.84.2 catalog
  carries none of them).
- Add `LaunchRequest.PermissionMode` (curator-spec Decision 0018): the
  interactive permission posture, `native` (zero value: pass nothing, the
  provider's stored settings decide) or `yolo` (the single provider bypass
  flag, emitted exactly once after model and effort). `claude-code` maps yolo
  to `--dangerously-skip-permissions` and `codex` to
  `--dangerously-bypass-approvals-and-sandbox`, each spelled once per plugin;
  `pi` and `pi-native` refuse yolo with `ErrPermissionModeUnsupported` (pi
  0.84.2 documents no bypass flag). `BuildPlan` refuses an unknown value
  (`ErrPermissionModeUnknown`) and any non-zero value outside interactive
  launches (`ErrPermissionModeNotInteractive`). Through `BuildPlan` a yolo
  launch carrying any composition is refused earlier with
  `ErrCompositionNotInteractive` (compositions never reach a terminal); the
  duplicate-prefix refusal (`ErrPermissionModeDuplicate`) fires on the direct
  plugin `Argv` path, where a composition prefix already carrying the flag is
  refused rather than emitted twice.
  Native/zero-value plans are byte-identical to before. The
  (environment, tool release) capability table is a follow-up (F-M1b); no
  release is cut here (it ships in the release carrying the gpt-6 sol/luna and
  opus-5-5 rows).
- Recommend `max` instead of `high` for `muse-spark-1.3-contributor` and its
  `muse-spark` alias. The vocabulary stays `high`/`xhigh`/`max` and required;
  only the recommended word moved, to the setting the Bug Hunt Bench score
  (33/105 at max, 19 at high) was observed at. Admitted-pair digests are
  unchanged.
- Note that `muse` 1.3.0 accepts the `max` effort word the 1.0.2 CLI refused
  harness-side; the verbatim effort transport is unchanged.

## Unreleased — v0.5.13

- Add `vendorplugin.BuildLaunchWithEnvironment` and
  `agentic.BuildPlanWithEnvironment` to expose sorted owned environment literals
  from the same prepared, alias-projected request used to build the plan.
  Existing planning APIs, Plan values and JSON contracts remain unchanged.
- Make Codex golden binary-path expectations architecture-neutral using the
  running Go toolchain, preserving captured fixtures and exact path comparisons.
