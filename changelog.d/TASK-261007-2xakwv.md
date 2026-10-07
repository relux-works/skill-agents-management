- Declare `claude-haiku-5-5` and its floating alias `haiku` → `claude-haiku-5-5`
  in `anthropic` (claude-code only, current, `low`..`max` recommended `medium`,
  rank 22 interpolated between `claude-opus-5` and `claude-opus-4-8`, read off
  Claude Code 2.1.290). Both rows declare the owner-imposed 100K operating
  context window, and the claude-code plugin now exports a declared
  `agentic.Model.ContextWindowTokens` as `CLAUDE_CODE_AUTO_COMPACT_WINDOW`;
  a row declaring none leaves the child environment byte-identical.
  `Launchable` carries the window and `BuildLaunch` refuses a vendor that
  rewrites it.
