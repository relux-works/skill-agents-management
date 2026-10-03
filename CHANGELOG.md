# Changelog

## v0.5.44 — 2026-10-04

- Declare the verified network tuple for `codex`: codex-env-v1, build
  0.159.0, exec entrypoint. A managed scope bearing exactly that tuple is
  admitted; every other build, entrypoint, adapter or harness still refuses
  with `network_scope_unsupported`.
- Inject the managed patch's set half into each command-backed MCP entry's
  env block on `codex` launches, through the shared `agentic` merge the
  adapters reuse; unset names are removed there too and untouched members
  survive. Coverage is every entry the harness will launch: request-declared
  entries in their pair streams plus the effective home's config and the
  selected profile's entries through per-key config overrides. An entry that
  cannot be covered reliably — an unaddressable name, a configured member no
  override can remove, an unreadable inventory, or verbatim native MCP
  configuration — refuses the whole managed launch with a typed refusal.
  The codex composition grammar admits a caller-supplied per-server `env`
  table, stdio entries only. Zero-network plans and every other plugin stay
  byte-identical.

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

## Unreleased

## v0.5.43 — 2026-10-04

- Add the standalone `agents-management model-check` diagnostic: one bounded
  managed-Pi/model expectation round-trip through the production
  `vendorplugin.BuildLaunch` path, with caller-deadline process-group
  isolation, nonzero exit on unmet expectations, and secret-safe mode-0600
  exclusive evidence. Engine-bound runtimes are observed through the new
  `pkg/engineobservation` adapter over `curator-engines status --json`
  readings (observed-process/v3); failed, stale, malformed or not-observed
  readings refuse before Pi starts. The launch gate now validates v3 adapter
  readings under their declared contract.

- Fix the flaky `model-check` deadline/descendant witnesses: the observation-adapter
  and status-reader witnesses now share the readiness-gated descendant-kill
  proof, so the measured window opens only after the child and its descendant
  both started and hold the pipe, under a hard outer bound, with the
  group-kill close distinguished from the WaitDelay close. Test-only; no
  production behavior changes.

- Bound every `internal/changelog` test child process (mutant child runs,
  script runs, syntax checks, git reads) under a per-run deadline with
  process-group kill and `WaitDelay`; a deadline expiry is a named `TIMEOUT`
  failure with command, elapsed time and captured output, never a kill.
  Process-group cleanup runs after every child return, including `WaitDelay`
  expiry, so no descendant outlives the bound; sticky out-of-band timeout
  attestation propagates
  across child test boundaries, independent of diagnostic truncation; nested
  process groups register under a tree supervisor and shared launch lock.
  A nested timeout is refused, never credited
  as a mutation kill. Fixture git invocations are non-interactive and
  isolated from user, global, system and XDG configuration. Aggregate load
  is bounded too: at most two test children run at once package-wide
  (overridable), mutant subtests run sequentially, and each captured
  stream is capped at 1 MiB keeping its head and tail.

- Add a validated local-provider snapshot input to the Codex plugin:
  `codex.ReadProviderSnapshot` performs one read+parse+resolve of the private
  `config.toml` and returns the resolved provider entry with the SHA-256
  digest of the bytes it was parsed from, or the same typed refusals as the
  ID-only path. `LocalProviderBinding` gains an optional `Snapshot`; when
  present, Exec and DryRun plans use the pinned entry without reading
  `config.toml`, so a config mutated between plan and exec cannot change the
  launch while the manifest records the old digest. The ID-only path and the
  managed-session refusal are byte-identical to before.

- Measure changelog test child elapsed time before arming its deadline, so timeout assertions include command setup and cannot undershoot the configured bound; add a deterministic setup-skew regression.

- Declare preview agy-only `gemini-4-argon` and floating alias `argon`, with no effort axis and interpolated score 45 between Astra and Fable. Evidence is the operator declaration of 2026-10-02, not a public release or an `agy models` read. Keep the Flash display pick; pin the exact agy lineup and test launch identity and effort refusal.

- Give changelog test Go-tool children a 10-minute default bound for cold
  compilation, while scripts, Git and already-built test binaries retain the
  2-minute bound; `CHANGELOG_TEST_CHILD_TIMEOUT` overrides both defaults.

- Require the first public `curator-network-profiles` release, `v0.2.0`, instead
  of the retired private `v0.1.0`; preserve the typed network carrier and exact
  adapter admission, including direct unset-only patches and inherited origins.

## v0.5.42 — 2026-10-02

- Declare the verified network tuple for `claude-code`: generic-env-v1,
  build 2.1.287, exec entrypoint. A managed scope bearing exactly that tuple
  is admitted; every other build, entrypoint, adapter or harness still
  refuses with `network_scope_unsupported`.

## v0.5.41 — 2026-10-02

- Add the data-only `vendorplugin/benchdata` Bug Hunt and registry accessors.
  Share model declarations with vendor and unresolved-runtime adapters; retain
  all benchmark keys, aliases, unknown efforts and list-cost evidence.

- Probe Muse Code's running release with the launch binary and curated child
  environment, keeping auto-update disabled. Recognize the 1.4.1 and 1.4.2
  version output and qualify 1.4.2 for native and yolo permission mapping;
  unverified releases remain refused.

- Allow Muse offline echo plans through `Model.ID: "echo"`: interactive, exec
  and dry-run argv select `--provider echo` without model or reasoning-effort
  flags. Preserve Meta argv and interactive permission posture.

- Enforce protected roots throughout quota binary resolution, validate router
  projection digests and enums, require an explicit store clock, and correct
  Codex optional legacy limit ids and Claude unscoped lane validation. Learn
  protected ancestor aliases to a bounded fixed point, refuse unresolved root
  evidence, return absolute plan binaries, and decode quota timestamp strings
  with explicit UTC locations without loading the ambient time zone.

- Disable Claude Code prompt suggestions in every launch mode by setting
  `CLAUDE_CODE_ENABLE_PROMPT_SUGGESTION=false`, overriding inherited values (#46).
- Add advisory `providerquota` records, atomic storage, independent O_EXCL
  locks, bounded timed failures and pull/push/failure merge rules. Optional
  system Readers declare command plans and pure parsers for Codex, Claude,
  Antigravity and Muse; no quota process is executed by this module. Preserve
  over-quota percentages, vendor scopes and individual measurement times;
  router projections exclude operator home and account diagnostics. Muse's
  MSP plan uses verified JSONL framing and the pinned 1.4.2 schema; a fresh
  host's absent usage returns `no_observation`. Observation before key mint
  remains an explicit decision; no session start or inference is added.
  Consumers adding `quota_snapshot` to their frozen fields will change
  existing preflight digests once; that consumer change and the module release
  tag belong to the follow-up lane.

- Decode the released curator-engines v0.1.0 runtime.start_time object as
  Unix seconds and microseconds, refusing missing, non-integer, or out-of-range
  components. Pin a runtime-present status payload captured from the producer.

- Declare `gpt-6.1-sol` in `openai` (codex-cli 0.159.0 catalog, 2026-09-29:
  priority 1, "Latest workhorse model for coding and everyday work.") and move
  the floating `sol` alias from `gpt-6-sol` to it. Effort: required
  `low`..`max` (the catalog's `ultra` stays retired), recommended `medium`;
  `sol` mirrors that axis, so it now recommends `medium` too. `gpt-6-sol` stays
  a current row under its own id with its own `max` recommendation. Score 47,
  interpolated between `gpt-6-astra` (48) and `gpt-6-sol` (46). Not
  `Recommended`, not pi-native.

- Declare `claude-sonnet-5-5` and its floating alias `sonnet` →
  `claude-sonnet-5-5` in `anthropic`, ahead of the board's registry. Read off
  Claude Code 2.1.284 on 2026-09-28 (2.1.281 answered
  `unrecognized_model`); the CLI's own `sonnet` still resolves to
  `claude-sonnet-5`, which is why the alias is declared rather than trusted.
  Effort: required `low`..`max`, recommended `high`. Score interpolated at 44
  between `claude-opus-5-5` (45) and `claude-fable-5-1` (43), from Anthropic's
  published agentic benchmarks placing it level with opus-5-5. Not
  `Recommended`, not pi-native.

- Add the Curator Launch Context Bridge, carrying the validated typed fragment
  through `LaunchRequest` and `BuildPlan` into Claude or Codex channel rendering.
  Contract v1.1.0 adds caller-selected append/replace intent; the plugin applies
  exactly one matching descriptor, keeps the full descriptor list in plan
  provenance, and refuses missing intent, no match, ambiguous channels, stale
  identity and incompatible launch inputs. Expected release v0.5.32; the
  orchestrator cuts the tag.

- Add a public release-versioned native-argument classifier for interactive
  launchers. It identifies Claude and Pi print forms and Codex `exec` through
  the plugin-owned grammar, returns that grammar version, stops at `--`, and
  fails closed for unknown systems or unverified releases. Add the
  pre-admission `Registry.PermissionMapping` API so launchers can obtain the
  plugin-owned mapping and grammar for a verified system, release and
  permission mode. Expected release v0.5.22; the orchestrator cuts the tag.

- Carry interactive permission mode, verified tool release and caller native
  arguments through vendor admission into the launch plan; add best-effort
  Claude and Codex stored-policy inspection with explicit inspected and
  not-inspected sources. Expected release v0.5.21; the orchestrator cuts the
  tag.

- Retire the `ultra` effort word everywhere. No row declares it any more
  (gpt-6-astra/astra, gpt-6-sol/sol, gpt-5.6-sol, gpt-5.6-terra and
  qwen3.7-plus-via-codex now stop at `max`), and the admission scale
  (`EffortOrder`) no longer places it, so a ceiling bound at `ultra` is
  unorderable. In Codex, Ultra is a sub-agent mode rather than a reasoning
  depth of one model. The port pins compare vocabularies with retired words
  taken out (`retiredEffortWords`).
- The agy (antigravity) lineup is Flash-only: `gemini-3.1-pro-high` and
  `gemini-3.1-pro-low` are retired (`retiredHereRows`), and
  `gemini-3.8-flash-high` (measured: 20/105 at high as `gemini-3.8-flash`),
  `gemini-3.7-flash-high` (interpolated 18) and the floating alias
  `gemini-flash` → `gemini-3.8-flash-high` are declared ahead of the board's
  registry. The ids come from the operator; `agy models` was not re-read
  because agy is not installed on the declaring machine. The agy display
  pick stays `gemini-3.6-flash-high`.

- Add the versioned provider-capability table keyed (environment, tool
  release) to a permission-grammar version (curator-spec Decision 0018
  choices 3 and 6; expected release v0.5.18 (v0.5.17 carried the 2026-09-22 model declarations), tag cut by the orchestrator).
  Each plugin holds its own environment's rows — `claude-code` 2.1.261,
  `codex` 0.153.2, `pi`/`pi-native` 0.84.2 (unsupported) — and
  `LookupReleaseCapability` is the single reader. The grammar version in
  force is `permission-grammar-v1`, the token the launcher must cite; a
  row naming a grammar this module does not implement selects no mapping.
  The caller establishes the running release with `ProbeToolRelease`
  (`<binary> --version` against the launch environment; `pi` has no
  probe because its binary is the wrapper) and passes it on the new
  `LaunchRequest.ToolRelease`. On drift — an unpinned or newer release,
  or none established — yolo fails closed first with the named
  `ErrPermissionModeUnverifiedRelease`, while native still forwards
  verbatim with no claims. The new `LaunchRequest.NativeArgs` (refused
  outside interactive launches with `ErrNativeArgsNotInteractive`) are
  forwarded verbatim after the module-spelled argv, so the yolo bypass
  flag lands before them; under yolo the plugin scans flag positions
  against the looked-up release's closed grammar and refuses an unknown
  codex `-c` key or an unknown claude `--permission-mode` value as usage
  (`ErrNativePolicyUnknown`, which the caller maps to exit 2), never
  resolved into a policy claim, while native performs no inspection at
  all. Prompt text is never parsed as a flag (`internal/nativeargs` owns
  the rule: `--` ends flag parsing, `=`-forms read as their flag).
  Known policy selectors under yolo are forwarded verbatim — Decision
  0018 item 4's conflict table is a later leaf.
- Refuse known conflicting native policy selectors under yolo (curator-spec
  Decision 0018 choice 4) with `ErrNativePolicyConflict` and exported
  `*NativePolicyConflictError{Selector, Placement}`. Claude conflicts include
  `--permission-mode`, `--allow-dangerously-skip-permissions`, and
  `--restricted`; Codex conflicts include `-a`/`--ask-for-approval`,
  `-s`/`--sandbox`, `--approve-for-me`, every `--dangerously-bypass-*`
  selector except the module-mapped bypass flag (which keeps
  `ErrPermissionModeDuplicate`), and `-c`/`--config` keys `approval_policy`,
  `sandbox_mode`, and `sandbox_permissions`. Declare
  `permission-grammar-v2` for Claude and Codex; Pi remains on
  `permission-grammar-v1`. Expected release v0.5.20, tag cut by the
  orchestrator.
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

- Carry Curator fragments and semantic context descriptors through vendor
  launches without changing their agentic types or typed refusals. Snapshot
  nested context at entry and isolate vendor callbacks, with fidelity checks
  against caller-selected context. Nil context preserves existing plans.

- Add profile policy value contracts v2 under `observed-process/v3`.
  `ValidateReadings` takes an optional contract version; v3 selects
  `stress-policy/v2` and `restart-policy/v2` for the profile stress and
  restart-supervision facts while every other fact keeps its contract. Both
  policies carry the engine catalog's keys and ranges with exact closed
  decoding, including zero restart delay and an explicit
  `{"configured":false}` form. `observed-process/v2` and the v1 value
  contracts are unchanged; three-argument callers keep v2.

- Add the typed network carrier (D4 per tb-R148, decided: no callbacks):
  optional `Network{Patch, Record}` on `agentic.LaunchRequest` and
  `vendorplugin.SpawnRequest`, with types from
  `github.com/relux-works/curator-network-profiles` v0.1.0 consumed by tag.
  `BuildPlan` applies the patch once after `ChildEnv`, joins the set half
  into `OwnedEnv`, and exposes the Record via
  `Plan.NetworkProvenanceSnapshot` (never in env). Every production entry
  point runs the same shared gate first: a malformed carrier refuses typed
  `network_profile_invalid` before the first plugin invocation of any kind,
  and a scope no verified adapter tuple names refuses typed
  `network_scope_unsupported` right after the `Capabilities` declaration
  read — the single permitted plugin call before the gate — ahead of any
  vendor dispatch, preparation, observation, preflight or optional surface.
  A patch touching a reserved run-context key or an owned `ChildEnv(nil)`
  key refuses typed `network_configuration_conflict`. No tuple
  is declared yet (the contract verifies none), so every harness refuses;
  `muse` keeps a second-line `ChildEnv` refusal until D8. Zero Network leaves
  every existing plan byte-identical.

## v0.5.24

- Publish typed launch-context descriptors for MCP servers, additional
  system-prompt text and interactive permission posture. Claude and Codex own
  descriptor validation, conflict refusals and provider-specific rendering;
  `BuildPlan` runs validation before launch preparation and starts no process.

## Unreleased — v0.5.13

- Add `vendorplugin.BuildLaunchWithEnvironment` and
  `agentic.BuildPlanWithEnvironment` to expose sorted owned environment literals
  from the same prepared, alias-projected request used to build the plan.
  Existing planning APIs, Plan values and JSON contracts remain unchanged.
- Make Codex golden binary-path expectations architecture-neutral using the
  running Go toolchain, preserving captured fixtures and exact path comparisons.
