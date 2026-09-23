# TASK-260923-3e4i3n — native-policy-conflict-refusal results

## Implementation

Decision 0018 item 4 is enforced in the existing Claude and Codex yolo native-argument scanners. The scanners run from the plugins' `Args` paths, reached through `BuildPlan`. They use `nativeargs.FlagIndexes` and `SplitFlagValue`; Codex also recognizes attached `-c` values and scans selectors after the `exec` subcommand.

Native mode skips these policy scans and forwards the original native arguments. Known non-conflicting selectors remain forwarded under yolo. Malformed or invalid policy forms fail closed.

The capability grammar is `permission-grammar-v2` for Claude and Codex because it adds the Decision 0018 conflict selectors, aliases, and Codex `exec` placement. Pi retains `permission-grammar-v1`. The module has no independent release-version constant; the changelog marks the next release as v0.5.20. Tagging remains the orchestrator's post-landing step.

Exported errors for launcher mapping:

- `agentic.ErrNativePolicyConflict`: stable `errors.Is` classification.
- `*agentic.NativePolicyConflictError`: `errors.As` details in `Selector` and `Placement`.
- `agentic.NativePolicyPlacementFlag`, `...Equals`, `...SeparateToken`, and `...AttachedShort`: stable placement values.
- `agentic.ErrPermissionModeDuplicate`: existing sentinel for a repeated module-mapped bypass flag.
- `agentic.ErrNativePolicyUnknown`: malformed or invalid values and unrecognized policy grammar.

README Permission-mode docs now describe the refusal and exported errors; the “later leaf” wording is removed. CHANGELOG identifies v0.5.20.

## Executed selector matrix

Every row below drove the plugin through `BuildPlan`. Each yolo row refused; the native twin preserved its original argv. Codex rows ran both at top level and after `exec`.

| Provider / selector family | Forms / placements | Yolo rows | Native rows |
|---|---|---:|---:|
| Claude `--permission-mode` | Six verified values × separate-token and `=` | 12 conflict | 12 forwarded |
| Claude `--allow-dangerously-skip-permissions` | Bare and `=` | 2 conflict | 2 forwarded |
| Claude `--restricted` | Bare and `=` | 2 conflict | 2 forwarded |
| Claude mapped bypass duplicate | Bare and `=`; `ErrPermissionModeDuplicate` | 2 refused | 2 forwarded |
| **Claude total** | 18 selector/form rows per mode | **18** | **18** |
| Codex `-a` / `--ask-for-approval` | Two spellings × separate-token and `=` × top-level and `exec` | 8 conflict | 8 forwarded |
| Codex `-s` / `--sandbox` | Two spellings × separate-token and `=` × top-level and `exec` | 8 conflict | 8 forwarded |
| Codex `--approve-for-me` | Bare and `=` × top-level and `exec` | 4 conflict | 4 forwarded |
| Codex `--dangerously-bypass-*` | Known hook selector, both forms, plus a family-prefix selector × top-level and `exec` | 6 conflict | 6 forwarded |
| Codex mapped bypass duplicate | Bare and `=` × top-level and `exec`; `ErrPermissionModeDuplicate` | 4 refused | 4 forwarded |
| Codex config keys `approval_policy`, `sandbox_mode`, `sandbox_permissions` | Five encodings (`-c` separate/`=`, `--config` separate/`=`, attached short) × top-level and `exec` | 30 conflict | 30 forwarded |
| **Codex total** | 30 selector/form/command rows per mode | **60** | **60** |
| **Combined total** | One BuildPlan subtest per selector/form/mode; Codex also includes command placement | **78** | **78** |

Additional yolo tests forward Claude `--debug`, `--verbose`, and `-d`; Codex `model_reasoning_effort`, `service_tier`, `mcp_servers.*`, and `--search`. Six invalid Codex `-a` / `-s` inputs fail with `ErrNativePolicyUnknown`. Native-mode tests also forward unknown policy forms with missing and unverified releases.

## Narrowing mutants

Each source mutation ran in a disposable copy under the Story worktree, never against the candidate files. Each listed test exited 1 because the corresponding BuildPlan matrix assertion observed that the narrowed selector was admitted. All ten mutants were killed.

| Refusal family narrowed | Targeted command in disposable copy | Exit | Result |
|---|---|---:|---|
| Claude known permission-mode values | `go test ./pkg/agentic/systems/claude -run '^TestPermissionModeConflictMatrix$'` | 1 | Killed; all 12 yolo forms admitted |
| Claude `--allow-dangerously-skip-permissions` | `go test ./pkg/agentic/systems/claude -run '^TestOtherKnownClaudePolicyConflicts$'` | 1 | Killed; both yolo forms admitted |
| Claude `--restricted` | `go test ./pkg/agentic/systems/claude -run '^TestOtherKnownClaudePolicyConflicts$'` | 1 | Killed; both yolo forms admitted |
| Claude mapped bypass duplicate | `go test ./pkg/agentic/systems/claude -run '^TestOtherKnownClaudePolicyConflicts$'` | 1 | Killed; duplicate rows admitted |
| Codex `-a` / `--ask-for-approval` | `go test ./pkg/agentic/systems/codex -run '^TestKnownCodexPolicyConflictMatrix$'` | 1 | Killed; short alias rows admitted |
| Codex `-s` / `--sandbox` | `go test ./pkg/agentic/systems/codex -run '^TestKnownCodexPolicyConflictMatrix$'` | 1 | Killed; short alias rows admitted |
| Codex `--approve-for-me` | `go test ./pkg/agentic/systems/codex -run '^TestKnownCodexPolicyConflictMatrix$'` | 1 | Killed; yolo rows admitted |
| Codex `--dangerously-bypass-*` family | `go test ./pkg/agentic/systems/codex -run '^TestKnownCodexPolicyConflictMatrix$'` | 1 | Killed; the family-prefix row admitted |
| Codex policy config keys | `go test ./pkg/agentic/systems/codex -run '^TestKnownCodexPolicyConflictMatrix$'` | 1 | Killed; `sandbox_permissions` rows admitted |
| Codex mapped bypass duplicate | `go test ./pkg/agentic/systems/codex -run '^TestKnownCodexPolicyConflictMatrix$'` | 1 | Killed; duplicate rows admitted |

One initial Claude permission-mode mutant failed at compile time because the mutation made a local variable unused; it is excluded from the ten results above. The corrected mutant preserved compilation and was killed by the matrix assertions as reported. The disposable copy was restored and removed afterward.

## Validation

Commands run against the candidate worktree after implementation:

| Command | Exit | Result |
|---|---:|---|
| `go test ./pkg/agentic ./pkg/agentic/systems/claude ./pkg/agentic/systems/codex` | 0 | Focused packages passed |
| `go test ./...` | 0 | All packages passed |
| `go vet ./...` | 0 | Passed |
| `go build ./...` | 0 | Passed |
| `golangci-lint run --new` | 0 | 0 new issues |
| `golangci-lint run` | 1 | 20 existing findings in unchanged files; full lint is not clean |
| `gofmt -l` over all eight changed Go files | 0 | No files reported |
| `git diff --check` | 0 | Passed |

The full-lint findings are in unchanged provider-limit, vendor-plugin, CLI, and test files. No real provider CLI was invoked. No `LOGBOOK.md` was written, per task instructions.
