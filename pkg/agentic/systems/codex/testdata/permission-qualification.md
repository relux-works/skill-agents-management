# Exact-release permission qualification

Observed 2026-10-29. This record qualifies only the documented canonical
permission mapping and the conservative planner subset described below.
It does not qualify model execution, sandbox enforcement, network behavior,
consumer activation, release delivery, or hidden-alias precedence.

| Required release | Evidence platform | Candidate state |
| --- | --- | --- |
| 0.163.0 | Linux x86_64, native musl distribution | Qualified canonical permission mapping under permission-grammar-v3 |
| 0.165.0 | Linux x86_64, native musl distribution | Independently qualified canonical permission mapping under permission-grammar-v3 |

Historical 0.153.2 (V2) and 0.164.0 (V3) rows remain. The historical
0.164.0 baseline is recorded in
[curator PR 200](https://github.com/relux-works/curator/pull/200).
No neighboring version or documentary cross-platform schema qualifies another
build. Lookup remains exact release-string lookup; this change does not add a
runtime executable-hash verifier. A different binary claiming the same version
is outside the evidence recorded here.

## Binary and observation identity

The installed JavaScript entry resolves the Linux x86_64 platform package's
`vendor/x86_64-unknown-linux-musl/bin/codex`. Direct native `--version`
returned `codex-cli 0.163.0`, exit 0. Package metadata independently names
`0.163.0` and `0.163.0-linux-x64`. No install or update was performed.

| Object | SHA-256 |
| --- | --- |
| JavaScript entry | `61b0194f3bb6534439c8d26a3ed57d0805f84b884588b761795323eeb92fcf70` |
| Native executable | `399ff69c431c066e40de318fba45c2b07f45a76b19939d247ee4828fc70c4215` |
| Generated v2/ThreadStartParams.json | `e9c6d3cc18d049bfbc0249add3808fb8e5a27e1a0b5a423767aafbc3859a9428` |

Each command ran with empty stdin and a fresh absolute disposable HOME,
CODEX_HOME, XDG_CONFIG_HOME, XDG_CACHE_HOME, XDG_DATA_HOME, XDG_STATE_HOME
and TMPDIR. The complete environment allowed only those paths, PATH,
LANG and LC_ALL. No inherited Git, authentication, provider or profile
selectors were forwarded. No user configuration, credentials or sessions were
copied. Only CLI help, schema generation and `features list` configuration
validation ran; no model, account, live-session or daemon command ran.

Commands below normalize the executable to `codex`; `schema` is a disposable
output directory. Help-assisted cases prove parsing only. `features list`
performs real bootstrap configuration validation, without an agent turn.
Expected errors retain their nonzero exits.

| Probe | Arguments after `codex` | Actual exit |
| --- | --- | --- |
| version | `--version` | 0 |
| help | `--help` | 0 |
| exec-help | `exec --help` | 0 |
| app-server-help | `app-server --help` | 0 |
| features-help | `features --help` | 0 |
| debug-help | `debug --help` | 0 |
| schema-help | `app-server generate-json-schema --help` | 0 |
| feature-list-control | `features list` | 0 |
| feature-list-invalid-sandbox | `-c 'sandbox_mode="unrestricted"' features list` | 1 |
| feature-list-invalid-approval | `-c 'approval_policy="sometimes"' features list` | 1 |
| schema | `app-server generate-json-schema --out schema` | 0 |
| canonical-config | `-c 'approval_policy="never"' -c 'sandbox_mode="danger-full-access"' features list` | 0 |
| readonly-config | `-c 'approval_policy="on-request"' -c 'sandbox_mode="read-only"' features list` | 0 |
| sandbox-type | `-c sandbox_mode=true features list` | 1 |
| approval-type | `-c approval_policy=3 features list` | 1 |
| bypass-parse | `--dangerously-bypass-approvals-and-sandbox features list` | 0 |
| cli-invalid-sandbox | `--sandbox unrestricted --help` | 2 |
| cli-invalid-approval | `--ask-for-approval sometimes --help` | 2 |
| cli-canonical | `--sandbox danger-full-access --ask-for-approval never --help` | 0 |
| cli-alias | `--yolo --help` | 0 |
| cli-hidden | `--not-so-yolo --help` | 0 |
| cli-compact | `-anever -sdanger-full-access --help` | 0 |
| cli-conflict | `--dangerously-bypass-approvals-and-sandbox --sandbox read-only --help` | 0 |
| cli-old-remote | `--remote-auth-token placeholder --help` | 2 |
| cli-control-conflict | `--approve-for-me --dangerously-bypass-approvals-and-sandbox --help` | 0 |
| config-escaped | `-c 'approval_policy="ne\u0076er"' -c 'sandbox_mode="danger-full-access"' features list` | 0 |
| config-invalid-escaped | `-c 'approval_policy="ne\\ver"' features list` | 1 |
| config-attached | `-capproval_policy=never --config=sandbox_mode=workspace-write features list` | 0 |
| config-after | `features list --config 'sandbox_mode="read-only"' -c 'approval_policy="never"'` | 0 |
| exec-options | `exec --sandbox danger-full-access --ask-for-approval never --help` | 2 |
| exec-invalid | `exec --sandbox unrestricted --help` | 2 |
| malformed-config | `-c noequals features list` | 1 |
| bypass-invalid-config | `--dangerously-bypass-approvals-and-sandbox -c sandbox_mode=true features list` | 1 |
| exec-bypass | `exec --dangerously-bypass-approvals-and-sandbox --help` | 0 |
| exec-sandbox | `exec --sandbox danger-full-access --help` | 0 |
| cli-unknown | `--unknown-permission-selector --help` | 2 |
| cli-approval-untrusted | `--ask-for-approval untrusted --help` | 2 |

## Observed constraints and admitted subset

Root help documents `--dangerously-bypass-approvals-and-sandbox` as skipping
confirmation prompts and executing without sandboxing. Both independently observed releases parse this canonical flag at root and exec. Root sandbox enums are `read-only`,
`workspace-write`, `danger-full-access`; root approval enums are `on-request`
and `never`. Invalid native enums fail with exit 2 and their specific invalid
value diagnostic. `--ask-for-approval` after exec fails with exit 2; this
position is not native execution evidence. The planner conservatively refuses
conflicting forms there too, without moving arguments after `--`.

Real config validation accepts `approval_policy="never"` with
`sandbox_mode="danger-full-access"`, and the restrictive control
`on-request` with `read-only`. It rejects `unrestricted`, `sometimes`,
boolean sandbox and numeric approval values with exit 1 and the specific
unknown-variant or invalid-type error. These failed lists emit no successful
list output. A Unicode escape spelling `never` is accepted; a literal
backslash variant is rejected. Attached, equals and after-subcommand config
placements are accepted. A missing `=` rejects with its specific override
error. Even a bypass flag does not hide an invalid config type on this path.

The independently generated protocol schema contains SandboxMode's three
canonical strings, and AskForApproval's `untrusted`, `on-request`, `never`
plus a granular object. Schema shape is not CLI acceptance or precedence:
root CLI rejects `untrusted`; the bootstrap error also names legacy
`on-failure` and granular variants. No support for these additional forms is
inferred for the planner.

V3 maps explicit yolo to exactly one canonical bypass, retaining the closed
V2 conflict/config subset. Known approval/sandbox selectors and known policy
config keys conflict with yolo. Unknown enum values, unknown or malformed
config keys, hidden `--yolo`/`--not-so-yolo` and compact `-a...`/`-s...` forms
refuse. Config policy values are refused because of their key, including
malformed/native-invalid types; the planner does not claim a TOML value
validator. Unknown non-policy top-level flags retain the prior forwarding
contract, not a claim of native acceptance. Native mode adds no permission
flag and forwards caller bytes, including malformed policy data and prompt
text after `--`. Empty/native selection makes no effective-access guarantee.

The exact native binary rejects removed `--remote-auth-token`. Classification
therefore refuses it as indeterminate for 0.163.0, as already for 0.164.0;
0.153.2 retains its historical qualified option. Unmodeled new options remain
indeterminate. Prompt text and option values do not become exec commands.

## Independent required 0.165.0 qualification

The supplied read-only documentary archive has SHA-256
`308e52a666ebf7264aa4ca2039a2753f1e0c9f8adc4296f9cab9ac4e7732cdc0`
and 441114 bytes. Inspection found 334 safe regular/directory members and
verified all 320 manifest-listed schema file sizes and SHA-256 digests.
Archive content was never executed. The manifest reports `codex-cli 0.165.0`
on macOS arm64, binary SHA-256
`a45099154d7fb4a5ed8a56f40966a95deb938e508bab23e6985035c709e9a85a`,
and version/help/schema export exits 0. That is documentary provenance only.
The row was initially unqualified while independent Linux evidence was absent.
It was qualified only after an exact task-local Linux artifact became available
and the following independent observations ran. The historical candidate and
failed observations are retained privately. No neighboring-version or
cross-platform inference is made.

The public npm alias `@openai/codex@0.165.0-linux-x64` supplied the immutable
platform artifact. Archive SHA-256 is
`f997cd6ef5987126e19cd3c82e847a4a6461270f32f97340c5e99a65fb6f9b8a`,
166008118 bytes; its SHA-512 SRI matches the public registry metadata. The
native executable independently hashes to
`c06a67fd8ac76508dc78bd99ce20c9f317b8d568beda59485f3f4134df9962d5`,
309732776 bytes, ELF64 little-endian x86_64. This is registry TLS/SRI
provenance, not an independent signer attestation. Only the native executable
was extracted, with no global installation or installed-package modification.
There is no JavaScript entry in this direct native qualification.

Its own `--version` returned `codex-cli 0.165.0`. Root/exec help, app-server,
features and schema help were captured before deriving the following probes.
Every probe used its own empty HOME/CODEX_HOME/XDG/cache/temp allowlist, as
above. All 37 corresponding parse/config/schema controls independently ran
with the exits in the table above; their agreement is an observed result,
not substituted evidence. In particular the invalid enum/type errors are
specific bootstrap errors, config escaping and option-position controls have
their own outcomes, and the removed remote token option is rejected.

This exact app-server help additionally documents `--print-startup-config`
as validation without starting the server. Four extra observations ran:

| Arguments after `codex` | Actual exit | Specific result |
| --- | --- | --- |
| `-c 'approval_policy="never"' -c 'sandbox_mode="danger-full-access"' app-server --print-startup-config` | 0 | appServerVersion 0.165.0, configRequirements.requirements null |
| `-c 'approval_policy="on-request"' -c 'sandbox_mode="read-only"' app-server --print-startup-config` | 0 | appServerVersion 0.165.0, configRequirements.requirements null |
| `-c 'sandbox_mode="unrestricted"' app-server --print-startup-config` | 1 | Structured configLoad error, unknown variant unrestricted |
| `-c 'approval_policy=3' app-server --print-startup-config` | 1 | Structured configLoad error, invalid type in approval_policy |

Failed startup commands emit only structured error JSON on stdout (code
-32600, reason configLoad), plus `startup configuration unavailable` on
stderr. They do not emit successful startup fields. This rejection telemetry
is preserved; silence is not required. Successful startup output reports
requirements and version, not effective thread rights. No server, thread,
model or account request was made. The separately generated
v2/ThreadStartParams.json has SHA-256
`e9c6d3cc18d049bfbc0249add3808fb8e5a27e1a0b5a423767aafbc3859a9428`. Equal schema hashes across platforms or releases never
replace these independent invocations.

The exact 0.165.0 row therefore uses the same conservative V3 subset, including
alias/compact refusals and removed-option classification. Its own planner
matrices exercise mapping, conflicts, closed forms, native forwarding and
context selection; unsupported patches, prereleases and build-metadata
strings still fail closed.

## Planner regression witnesses

`policy_exact_releases_test.go` covers both exact releases independently for mapping/argv order,
selector/placement conflicts, enums and closed config forms, aliases,
classification drift, native forwarding, context-owned selection and
unqualified/drifted release refusal. Every refusing direct argv path is checked
for empty argv; this is a planner with no process launch, not evidence of
runtime sandbox enforcement. The original 0.164 test names remain; its old
0.163.0 unqualified neighbor is replaced by still-unqualified 0.163.1.

Private qualification logs retain full commands, actual exits, environment
allowlists, process ids, outputs and hashes. Bounded isolated source mutants
and observation-checker plants supplement the positive/negative tests;
producer tests do not replace independent review or required landing checks.
