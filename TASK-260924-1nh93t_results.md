# TASK-260924-1nh93t results

## Audit basis and Story base

Read launcher SPEC `0.5.0-draft` §4 from the assigned launcher Story worktree and curator-spec Decision 0018. Revision 2 identified the public native-argument classifier as missing and added it using the verified release rows and `internal/nativeargs`. The revision 2 review then found a separate missing §4.3 `mapped=` reporting API; this revision adds that capability and its audit row.

The task start reminder required verifying the Story base before trusting it. After `git fetch origin main`, the worktree `HEAD`, fresh `origin/main`, and task-board `selected_base_oid` all matched `850ac393250606d57188540ef569103c413bd539` (`main`). No rebase or merge was needed.

## Launcher capability audit

The table covers every §4 behavior that the launcher must obtain from agents-management. The environment-ID aliases, closed mapping to `ax` provider IDs, Curator fragment parsing, launcher defaults and configuration files, channel application, file probes, process handoff, and launcher diagnostics are launcher-owned by SPEC §4.1–§4.7; they do not need duplicate module APIs.

| SPEC clause | Behavior the launcher obtains from the module | Public module API | Status |
|---|---|---|---|
| §4.1 | Identify the adapter-declared home variable, environment contract, modes, and system identity after the launcher maps its env-id. | `agentic.Registry.Lookup`; `agentic.System.ID`, `Capabilities`, `ResolveBinary`, `ChildEnv`. | Satisfied before this task. Curator fragment parsing, repair, digesting, aliases, and env-id/system-id/`ax`-provider mapping remain launcher-owned. |
| §4.2 | Resolve the system's declared runtime/vendor compatibility and model rows. | `vendorplugin.Registry.ResolveRuntime`, `RuntimeModels`, system/runtime declarations and model rows. | Satisfied before this task. The closed env-id and provider-id maps are launcher-owned. |
| §4.3 | Rank the module's admitted lineup, get each model's declared effort support/recommendation, and bind Pi fallback to one runtime's rows. | `vendorplugin.Lineup`, `LineupOf`, runtime model declarations and model metadata. | Satisfied before this task. Ordered Pi runtime preference and launcher config precedence are launcher-owned. |
| §4.3–§4.5 | Carry resolved `native`/`yolo`, exact tool release, and opaque native suffix into the interactive request; map mode and reject unsupported modes, duplicate bypass, unknown/conflicting native policy, and release drift. | `agentic.LaunchRequest.PermissionMode`, `ToolRelease`, `NativeArgs`; `agentic.LookupReleaseCapability`; plugin-owned release rows and argv builders; typed permission errors; `vendorplugin.BuildLaunch`. | The launch-plan path was satisfied before this task. The separate pre-admission `mapped=` reporting value was not publicly available before Revision 3; see the next row. Native mode forwards the suffix without policy inspection. |
| §4.3 | Print the permission provenance line before admission, including the module-supplied `mapped=<flag or none>` value. | **Added:** `agentic.Registry.PermissionMapping(systemID, toolRelease, mode)`; optional `PermissionMappingCapability`; `PermissionMapping{Flag, Grammar}` from the selected system plugin. | **MISSING before Revision 3; added.** It can be called before `BuildLaunch`; it returns no flag for verified native mode, the plugin mapping for supported verified yolo, and typed errors for unknown systems, unknown modes, unsupported mappings, or unverified releases. |
| §4.3–§4.4 | Establish the running release from the launch environment. | `agentic.ProbeToolRelease`, optional `ToolReleaseProber`, `ErrToolReleaseUndetected`. | Satisfied before this task. The local Pi wrapper has no native Pi release probe; an absent result cannot establish a permission mapping. |
| §4.4 | Admit the resolved runtime/model/effort through the vendor and system boundary, then return an interactive value plan. | `vendorplugin.BuildLaunch` / `BuildLaunchWithEnvironment`, `SpawnRequest`, `agentic.BuildPlan`, `agentic.Plan`; typed admission and launch errors. | Satisfied before this task. Launcher code must not bypass vendor admission with a bare model id. |
| §4.4 | Interpret provider-limit evidence for the same runtime/model/managed-home tuple and preserve read failures. | `providerlimits.Store.AvailabilityFor`, `VerdictQuery`, `Availability.Serviceable`, state and failure evidence. | Satisfied before this task. The check is a separate launcher-invoked read; `BuildLaunch` does not perform it. |
| §4.4–§4.5 | Obtain complete argv, filtered child environment, stdin, workdir, binary, plus the module-owned environment literals for a tracked document. | `agentic.Plan` fields; `System.ChildEnv(nil, req)`; `BuildPlanWithEnvironment`; `vendorplugin.BuildLaunchWithEnvironment`. | Satisfied before this task. The launcher composes fragment channels and must not re-overlay inherited names beneath `Plan.Env`. |
| §4.5–§4.6 | Inspect known stored-policy relaxations and distinguish inspected, unavailable, unsupported, and failed sources. | `agentic.InspectStoredPolicy(system, plan)`, `StoredPolicyInspection`, typed source/support results. | Satisfied before this task for declared inspectors. An unsupported or unreadable source is not a clean scan. |
| §4.6 | Determine whether the verified native suffix selects a non-interactive form for the headless default; return the form and grammar version, and keep prompt text after `--` opaque. | **Added in Revision 2:** `agentic.Registry.ClassifyNonInteractiveArgs`; `NonInteractiveArgumentClassifier`; `NativeArgsClassification{Form, Grammar}`; plugin classifiers reusing `internal/nativeargs` and each plugin's `ReleaseCapability` rows. | **MISSING before Revision 2; added and kept by review.** Unknown systems, absent/unverified releases, unsupported plugins, and indeterminate suffix grammar return typed errors. |
| §4.1, §4.5–§4.7 | Parse/repair fragments, own defaults and `ax.json`, compose channels and environment, run file probes, select tracked handoff vs exec, and emit launcher diagnostics. | No module API is required beyond the systems, declarations, value plans, and typed failures above. | Launcher-owned by SPEC. No duplicate launcher API was added. |

## Public classifier

`agentic.Registry.ClassifyNonInteractiveArgs(systemID, toolRelease, suffix)` resolves the system through the existing registry, then dispatches through the optional `NonInteractiveArgumentClassifier` plugin capability. It copies the suffix before dispatch. The result carries `Form` and the same `permission-grammar-v1` or `permission-grammar-v2` token verified for that tool release; `IsNonInteractive()` is false for an empty form.

| Launcher env-id | Module system | Verified release | Grammar | Non-interactive form and tested placements |
|---|---|---:|---|---|
| `claude_code` | `claude-code` | `2.1.261` | `permission-grammar-v2` | print (`-p`, `--print`): short flag, `--print=true`, and flag followed by a separate prompt |
| `codex_cli` | `codex` | `0.153.2` | `permission-grammar-v2` | `exec` and `e`: first command, after an equals-form root option, and after separate-value root options |
| `pi` | `pi-native` | `0.84.2` | `permission-grammar-v1` | print (`-p`, `--print`): short flag, `--print=true`, and flag followed by a separate prompt |

The plugin packages own their tool-specific spellings. All classifiers stop at the shared `--` boundary. Codex skips root option values whose arity is declared in the pinned plugin grammar before identifying its first command token, so a value equal to `exec` is not treated as a subcommand. A root flag whose arity the classifier does not model returns `ErrNativeArgsClassificationIndeterminate` because its following token could be a value; callers must not treat the error as interactive. The `pi` system is a local-runtime wrapper rather than the native Pi CLI and returns `ErrNativeArgsClassifierUnsupported`; the launcher's Pi mapping uses `pi-native`.

Typed refusals are `ErrUnknownSystem` for an unregistered system, `ErrNativeArgsClassifierUnsupported` for a registered system without the optional capability, `ErrPermissionModeUnverifiedRelease` for an absent or unverified tool release, and `ErrNativeArgsClassificationIndeterminate` when the plugin cannot establish the suffix grammar.

## Tests, mutation evidence, and validation

The public registry entry point is driven with fake argument suffixes; no provider or `ax` process was run. Positive rows cover each mapped environment/form and `=`/separate placement, including Codex's `e` alias and an exec command after `--local-provider`. Negative rows cover each print/exec spelling after `--`, a print-looking token after a prior flag and separator, `exec` as a Codex model value, an unmodeled Codex root flag before `exec` returning `ErrNativeArgsClassificationIndeterminate`, unknown systems, an unsupported wrapper system, and empty/unverified releases tested with otherwise classifiable suffixes (`exec` for Codex, print for Claude and Pi).

Six narrowing mutants were applied one at a time in a disposable copy, then the copy was removed. Each attack used a standalone `go test ./pkg/agentic -run '^<test-name>$' -count=1` command; all six expected-red commands exited 1, so the measured kill ratio was 6/6:

| Mutant | Standalone attack command | Exit | Observed failure |
|---|---|---:|---|
| Narrow print classification to exact flag elements, losing `=` recognition. | `go test ./pkg/agentic -run '^TestRegistryClassifiesVersionedNonInteractiveFormsByEnvironmentAndPlacement$' -count=1` | 1 | Claude `--print=true` became interactive. |
| Remove the separator stop from shared flag parsing. | `go test ./pkg/agentic -run '^TestRegistryClassifierStopsBeforePromptText$' -count=1` | 1 | `--print` after `--verbose --` was classified. |
| Remove `--model` from Codex's pinned value-taking root options. | `go test ./pkg/agentic -run '^TestCodexClassifierDoesNotTreatAnOptionValueAsTheExecCommand$' -count=1` | 1 | The valid `--model exec` suffix became indeterminate. |
| Treat every unmodeled Codex root flag as boolean. | `go test ./pkg/agentic -run '^TestCodexClassifierFailsClosedForAnUnmodeledRootFlag$' -count=1` | 1 | The unmodeled flag before `exec` stopped returning the indeterminate error. |
| Reject Codex's pinned release row. | `go test ./pkg/agentic -run '^TestRegistryClassifiesVersionedNonInteractiveFormsByEnvironmentAndPlacement$' -count=1` | 1 | All Codex positive release rows refused. |
| Substitute Codex's pinned release for caller input. | `go test ./pkg/agentic -run '^TestNonInteractiveClassifierFailsClosedForUnknownOrUnverifiedInputs$' -count=1` | 1 | The unverified Codex release with valid suffix `exec` returned an exec classification. |

`--yolo` is a Codex-native root boolean needed to reach a later `exec`; the spelling lives in Codex's plugin. Codex's argv guard narrowly allowlists the parser-only root-option helpers, and Muse's cross-plugin guard allowlists only that Codex alias constant and parser read. The full lint command reports 20 findings in untouched files; `--new` reports no findings in this task diff.

Validation commands and real exit codes:

| Command | Exit |
|---|---:|
| `go test ./pkg/agentic ./pkg/agentic/systems/claude ./pkg/agentic/systems/codex ./pkg/agentic/systems/muse ./pkg/agentic/systems/pinative` | 0 |
| `go test ./...` | 0 |
| `go vet ./...` | 0 |
| `go build ./...` | 0 |
| `gofmt -d pkg/agentic/noninteractive_test.go pkg/agentic/noninteractive.go pkg/agentic/systems/claude/policy.go pkg/agentic/systems/codex/policy.go pkg/agentic/systems/pinative/policy.go pkg/agentic/systems/codex/args.go` | 0 |
| `golangci-lint run --new ./...` | 0 — 0 new issues |
| `golangci-lint run ./...` | 1 — 20 findings in untouched files; none in this task's changed lines |
| `git diff --check` | 0 |

The release note is under `## Unreleased` and identifies expected v0.5.22. Per the campaign rules, the orchestrator cuts the release tag after landing; this producer did not tag. Repository-wide lint cleanup is outside this task; the new diff is lint-clean. Campaign rules prohibit editing `LOGBOOK.md`; this task-scoped outcome records the relevant findings for the handoff.

## Revision 3 — F-M1e rework

### Revision 2 review finding and response

The revision 2 verdict (`TASK-260924-1nh93t_review-verdict-rev2.md`) was
CHANGES_REQUESTED because the audit omitted the §4.3 `mapped=` value printed
before admission. The SPEC says agents-management supplies it and does not name
a provider flag. `ReleaseCapability`, `Plan`, and `LaunchProvenance` did not
expose that mapping. The audit table above now has a separate §4.3 row, and the
new `agentic.Registry.PermissionMapping(systemID, toolRelease, mode)` API
returns `PermissionMapping{Flag, Grammar}` through an optional
`PermissionMappingCapability`, before any plan is built or admitted.

The capability implementations in Claude, Codex, and native Pi use the same
plugin-owned `verifiedReleases` rows as the permission grammar. `native`
returns an empty flag, supported verified `yolo` returns the same plugin
constant used by interactive argv construction, and the verified Pi row
returns `ErrPermissionModeUnsupported` for `yolo`. Unknown systems return
`ErrUnknownSystem`, an unimplemented system capability returns
`ErrPermissionModeUnsupported`, unknown modes return `ErrPermissionModeUnknown`,
and missing or unverified releases return `ErrPermissionModeUnverifiedRelease`.
The API is independently callable before `BuildLaunch`; native plan argv
continues to forward its suffix without requiring a release pin.

### Final §4 audit sweep

Re-read all of launcher Story `STORY-260922-39hxog`'s `SPEC.md` §4 from its
assigned worktree. The separate row above covers the only additional phrase
requiring a module-supplied value: the `mapped=` field in §4.3. **No further
§4 phrase says that agents-management supplies a value or that the module
returns a capability missing from the audit table.** The remaining module
dependencies are represented in the table: system declarations and home/env
contracts (§4.1), runtime and model compatibility plus lineup (§4.2–§4.3),
release probing, permission request and policy handling, `BuildLaunch` and
`BuildPlan`, provider-limit verdicts, the complete plan and owned environment
values (§4.4–§4.5), stored-policy inspection, and the non-interactive native
argument classifier (§4.6). Fragment handling, defaults and configuration,
channel composition, file probes, handoff/exec, and launcher diagnostics stay
launcher-owned.

### Permission-mapping matrix and regression evidence

`TestRegistryPermissionMappingMatrixByEnvironmentModeAndRelease` drives the
public registry API for all 12 system × mode × release-verification rows:

| System | Mode | Release status | Result |
|---|---|---|---|
| `claude-code` | `native` | verified `2.1.261` | empty flag; `permission-grammar-v2` |
| `claude-code` | `native` | unverified `2.1.262` | `ErrPermissionModeUnverifiedRelease` |
| `claude-code` | `yolo` | verified `2.1.261` | Claude plugin mapping; `permission-grammar-v2` |
| `claude-code` | `yolo` | unverified `2.1.262` | `ErrPermissionModeUnverifiedRelease` |
| `codex` | `native` | verified `0.153.2` | empty flag; `permission-grammar-v2` |
| `codex` | `native` | unverified `0.153.3` | `ErrPermissionModeUnverifiedRelease` |
| `codex` | `yolo` | verified `0.153.2` | Codex plugin mapping; `permission-grammar-v2` |
| `codex` | `yolo` | unverified `0.153.3` | `ErrPermissionModeUnverifiedRelease` |
| `pi-native` | `native` | verified `0.84.2` | empty flag; `permission-grammar-v1` |
| `pi-native` | `native` | unverified `0.84.3` | `ErrPermissionModeUnverifiedRelease` |
| `pi-native` | `yolo` | verified `0.84.2` | `ErrPermissionModeUnsupported` |
| `pi-native` | `yolo` | unverified `0.84.3` | `ErrPermissionModeUnverifiedRelease` |

`TestRegistryPermissionMappingRefusesUnknownSystemAndMode` covers unknown
system, registered `pi` (the local-runtime wrapper without the optional
capability), and an unsupported mode. Plugin tests
`TestPermissionMappingUsesTheClaudeArgvFlagOnlyForVerifiedYolo`,
`TestPermissionMappingUsesTheCodexArgvFlagOnlyForVerifiedYolo`, and
`TestPermissionMappingKeepsVerifiedPiNativeEmptyAndYoloUnsupported` verify
each plugin's mapping against its own argv constant or unsupported release
fact. The provider flag spellings remain in their owning plugin packages; the
Codex argv guard allows only the read-only pre-admission mapping method in
addition to its existing parser helpers.

Two narrowing mutants were applied separately and each was killed by the
named public matrix test (both attack commands were standalone, expected-red
`go test` runs):

| Mutant | Command | Exit | Observed failure |
|---|---|---:|---|
| Return Claude's provider flag for `native`. | `go test ./pkg/agentic -run '^TestRegistryPermissionMappingMatrixByEnvironmentModeAndRelease$' -count=1` | 1 | The `claude-code/native/verified` row observed a non-empty flag. |
| Substitute Claude's pinned row after release lookup fails. | `go test ./pkg/agentic -run '^TestRegistryPermissionMappingMatrixByEnvironmentModeAndRelease$' -count=1` | 1 | Both `claude-code/native/unverified` and `claude-code/yolo/unverified` returned nil errors. |

The classifier matrix and six classifier mutants recorded above were retained
from revision 2; the revision 2 reviewer independently verified that
classifier surface and its release-substitution mutant.

### Revision 3 verification

| Command | Exit |
|---|---:|
| `go test ./pkg/agentic -run '^(TestRegistryPermissionMappingMatrixByEnvironmentModeAndRelease|TestRegistryPermissionMappingRefusesUnknownSystemAndMode)$' -count=1` | 0 |
| `go test ./pkg/agentic ./pkg/agentic/systems/claude ./pkg/agentic/systems/codex ./pkg/agentic/systems/pinative` | 0 |
| `go test ./...` | 0 |
| `go vet ./...` | 0 |
| `go build ./...` | 0 |
| `golangci-lint run --new ./...` | 0 — 0 new issues |
| `golangci-lint run ./...` | 1 — 20 findings in untouched files; no findings in this task's changed lines |
| `git diff --check` | 0 |

The full lint findings are pre-existing in unrelated tests, CLI output,
provider-limit helpers, and vendor-plugin files. The task diff passes the
repository's `--new` lint check. No cross-platform claim was made from these
local checks. Expected release remains v0.5.22; the orchestrator cuts the tag
after landing. No `LOGBOOK.md` edit was made; this outcome records the review
finding and decision for the task.

Checklist item 13's rejection branch was triggered by the revision 2
CHANGES_REQUESTED verdict. Its evidence is attached as
`TASK-260924-1nh93t_review-verdict-rev2.md`, and the task was routed back to
development for this rework. That evidence and routing satisfy the conditional
item for the prior rejection; revision 3 is now awaiting review.

### Revision 3 rework — fresh verification

Re-read all of launcher Story `STORY-260922-39hxog`'s SPEC §4 (lines 194–911)
from its assigned worktree. The only additional value explicitly supplied by
agents-management is §4.3's pre-admission `mapped=` permission provenance;
the table above now binds it to `Registry.PermissionMapping`. **No further §4
phrase says that agents-management supplies a value or that the module returns
a capability missing from the audit table.**

I reran the named revision 3 regression against two narrowing mutants. Both
mutant runs were standalone `go test` processes with `-count=1`; each returned
exit 1 as expected, for a 2/2 kill ratio:

| Mutant | Regression command | Exit | Observed failure |
|---|---|---:|---|
| Return a non-empty Codex mapping for verified `native`. | `go test ./pkg/agentic -run '^TestRegistryPermissionMappingMatrixByEnvironmentModeAndRelease$' -count=1` | 1 | `codex/native/verified` got a flag instead of an empty mapping. |
| Substitute Codex's verified release when an unverified release is requested. | `go test ./pkg/agentic -run '^TestRegistryPermissionMappingMatrixByEnvironmentModeAndRelease$' -count=1` | 1 | `codex/native/unverified` and `codex/yolo/unverified` returned nil errors. |

For each mutant I restored `pkg/agentic/systems/codex/policy.go` from a saved
copy in the worktree and verified the restoration with `diff -u` (exit 0)
before running the clean-candidate gates.

Fresh clean-candidate verification (all commands were run directly, without
pipes):

| Command | Exit | Result |
|---|---:|---|
| `go test ./...` | 0 | All packages passed. |
| `go vet ./...` | 0 | No findings. |
| `go build ./...` | 0 | All packages built. |
| `golangci-lint run --new ./...` | 0 | 0 new issues. |
| `gofmt -d` on all changed Go files | 0 | No formatting diff. |
| `git diff --check` | 0 | No whitespace errors. |

These checks were run on the current host; no cross-platform result is claimed.
