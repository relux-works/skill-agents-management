# Changelog

## Unreleased

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
