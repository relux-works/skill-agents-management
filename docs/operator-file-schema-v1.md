# Operator runtime and binding files: schema v1

**Schema version:** 1
**Owner:** `skill-agents-management` runtime-catalog slice
**Audience:** Curator runtime discovery and role-profile consumers

This document freezes the operator-file contract needed by the runtime catalog
and by the role-profile consumer. The files are machine-local under
`~/.curator/`; they are not project files, board attachments, or repository
configuration.

## Shared rules

- Role and profile names are case-sensitive and used exactly as written.
  Runtime and system identifiers use the module's existing normalization
  rules; two spellings that normalize to one ID are a conflict, never a
  last-writer-wins choice. Engine entry names resolve exactly as written.
- A missing file, a present but unreadable file, and a malformed file are
  distinct results. Read failure is never treated as absence or as a parse
  error.
- An entry is effective only when its references resolve against the named
  catalogs. An unresolved role binding is reported as unbound; it is not
  replaced with a default.
- Files contain no credential values. Secret material stays in the owning
  harness or secret provider. Provenance and errors contain identifiers and
  source basenames only, never file contents or credential values.
- Import is additive and idempotent. Equal existing entries are a no-op;
  conflicting entries refuse without overwriting either source. Admission,
  time windows, effort declarations, and billing classes are preserved exactly
  by value. This schema does not reinterpret or widen those policies.

## `~/.curator/runtimes.toml`

The runtime catalog declares a machine runtime and its system. An engine
reference names an entry in `~/.curator/engines.toml`; engine entry contents
remain owned by `curator-engines`.

```toml
schema_version = 1

[runtimes.local-qwen]
system = "pi"
engine = { plugin = "mlx", name = "local-qwen" }

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit"]
description = "Local Qwen model"
publisher = "alibaba"
family = "qwen"
lifecycle = "current"
context_window_tokens = 131072
cache_budget_bytes = 6442450944
effort_support = "none"
engine = { plugin = "mlx", name = "local-qwen" }
```

`schema_version` is the integer `1`. `runtimes` is a table keyed by stable
runtime ID. Each runtime has a non-empty `system`. `engine` is optional; when
present it is a table with non-empty `plugin` (engine kind) and `name` strings.
`name` must identify an entry in `engines.toml`, whose declared engine kind
must match `plugin`. A runtime may contain a `models` table; each model may
carry the same optional engine reference. `context_window_tokens`, when
present, is a non-negative integer; `cache_budget_bytes`, when present, is a
positive integer; `effort_support` is `none` or `required`. Model IDs and their
metadata are preserved without changing their admission, window, effort, or
billing meaning. Fields inside runtime and model declarations stay with those
declarations; policy tables outside the `runtimes` table are imported into
`models.toml` without reinterpretation. The importer drops only the retired
`pointer.agents_infra_project` and `pointer.agents_infra_profile` fields after
it has either resolved the profile to an exact engine entry or reported the
row unbound. An old engine kind must match the kind declared by that exact
entry; a mismatch refuses as unsupported rather than guessing an alias.

Credential metadata, if present, uses only the exact `credential` table. Its
`source` is `env-name`, `command`, or `none`. For `env-name`, `name` is an
environment-variable identifier (`[A-Za-z_][A-Za-z0-9_]*`). For `command`,
`name` is one executable name, with no path or arguments. `none` may omit
`name`; if supplied, it must be empty. These values are references only and
must not contain secret material.

- **Exact compacted credential-key stems:** `accesskey, apikey, auth, authorization, bearer, clientsecret, cookie, credential, jwt, pass, passphrase, passw, privatekey, privkey, pwd, secret, sessionid, sessionkey, sessionsecret, sessiontoken, signingkey, token`.

Names are lowercased and punctuation-compacted before these stems are checked.
`pass` and `auth` match as whole field-name tokens after separator and
camelCase splitting; `auth` also matches a compacted suffix, or a prefix
immediately followed by another listed credential stem. Thus `passphrase`,
`auth`, `authtoken`, and `basicauth` are refused, while `passthrough` and
`author` remain accepted. Only the exact `credential` table is admitted as a
credential-shaped field.

Key-name inspection is defense in depth, not proof that a file has no secret
material. It cannot establish that a credential was not placed under a neutral
key, a role-keyed policy value such as `billing.developer`, the ambiguous names
`key`, `pat`, or `sk`, or a non-ASCII homoglyph spelling. Those shapes are
outside this name-based refusal bound; the operator remains responsible for
keeping credential values out of every file.

Integer-valued fields ending in `tokens` are treated as counters rather than
credential keys; for example, `context_window_tokens` and `max_tokens`. Known
counters still follow their field-specific validation rules, such as the
non-negative bound for `context_window_tokens` above.

The catalog parser does not start an engine, probe it, construct launch
transport, or decide project admission.

## `~/.curator/engines.toml`

The engine catalog is owned by `curator-engines`. Its entries use
`[engines.<name>]`. This package reads the catalog only to establish whether a
runtime's `{ plugin, name }` reference names an existing entry of the declared
engine kind. Each entry must provide a non-empty `engine` kind string for this
lookup; the rest of its body remains owned by `curator-engines`. This package
does not add, edit, or infer engine entries.

```toml
[engines.local-qwen]
engine = "mlx"
# Remaining entry body is defined and validated by curator-engines.
```

## `~/.curator/models.toml`

`models.toml` may contain other operator-owned policy tables. The binding
parser reads `[bindings.<role>]`; import copies admission, window, effort,
billing, and other policy values without interpreting them. The runtime
catalog does not decide project admission or change those policy values.

```toml
schema_version = 1

[bindings.developer]
run = "local-qwen"
profile = "developer-local"
```

`run` is an exact runtime ID from `runtimes.toml`. `profile` is an exact Curator
profile name. Both values are required for a bound row; a table with either
value absent is discoverable as unbound. A present value with the wrong TOML
type is malformed. Profile spelling is preserved exactly for the Curator
consumer. No implicit profile, runtime, credential, or launch mode is selected.

## Discovery and typed refusal contract

Preflight returns one row per requested role, including roles without a table.
Each row states `bound` or `unbound`, and a bound row includes the runtime ID,
profile name, and sanitized provenance. An unbound row is visible to
discovery; resolving that row for execution returns a typed unbound refusal.

The API distinguishes these refusal kinds so callers can act on the actual
condition:

| Kind | Meaning |
| --- | --- |
| `absent` | The required operator file does not exist. |
| `malformed` | A readable file cannot be parsed as TOML or has invalid schema-v1 fields. |
| `read_failed` | A file exists but could not be read; this never falls back to a legacy source. |
| `conflicting` | An imported or existing value disagrees with the value it would replace. |
| `unbound` | The role has no binding table, or its declared runtime/engine entry is absent on this machine. |
| `unsupported` | A declared schema version, runtime system, or engine reference is not supported by the catalog consumer. |

Diagnostics name the refusal kind and the relevant role/runtime/engine IDs;
they never embed TOML values, credentials, or absolute home paths.

## Import and provenance

Import accepts `local-models.toml` and an explicit legacy operator TOML input;
it does not search for, delete, or rewrite source files. A legacy row with a
malformed field is refused. An unresolved old engine/profile reference is
reported as unbound, and a kind mismatch refuses as unsupported; neither is
guessed. Runtime and model declarations go to `runtimes.toml`; policy tables
outside the legacy `runtimes` table, including admission, windows, effort,
billing, and bindings, go to `models.toml`. Values inside model declarations,
such as context windows and effort support, remain there unchanged. Repeating
an import with the same sources produces no file changes. A disagreement is a
typed `conflicting` refusal, not last-writer-wins behavior.

Provenance identifies the source kind and basename, schema version, and
resolved runtime/engine IDs. It does not persist source file contents,
absolute paths, environment values, or secrets.

This version does not define launch transport. The Codex local-provider
transport is owned by `TASK-260927-1ycuhl`.
