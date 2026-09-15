# Changelog

## Unreleased — v0.5.13

- Add `vendorplugin.BuildLaunchWithEnvironment` and
  `agentic.BuildPlanWithEnvironment` to expose sorted owned environment literals
  from the same prepared, alias-projected request used to build the plan.
  Existing planning APIs, Plan values and JSON contracts remain unchanged.
- Make Codex golden binary-path expectations architecture-neutral using the
  running Go toolchain, preserving captured fixtures and exact path comparisons.
