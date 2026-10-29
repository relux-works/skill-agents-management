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
`models.toml` without reinterpretation. Direct local-models configuration uses
`pointer.curator_engines_project` and `pointer.curator_engines_profile`.
Native Pi model rows also set `pointer.pi_model_identity` to the exact
provider/model identity present in Pi's model catalog; it is not inferred from
the model vendor ID or Curator profile, and native launch refuses when it is
missing or malformed. The importer drops only the retired source fields `pointer.agents_infra_project`
and `pointer.agents_infra_profile` after it has either resolved the profile to
an exact engine entry or reported the row unbound. An old engine kind must
match the kind declared by that exact entry; a mismatch refuses as unsupported
rather than guessing an alias.

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

The top-level `schema_version`, when present, must be the TOML integer `1`.
It may be omitted for compatibility with existing catalogs. Any other value or
type refuses as `malformed`; other unknown top-level keys still refuse, with
credential-shaped keys reported as `unsupported`.

```toml
schema_version = 1

[engines.local-qwen]
engine = "mlx"
# Remaining entry body is defined and validated by curator-engines.
```

## Exact local weights (additive; schema stays 1)

**Status:** documented contract only. The readers, validation and certified
launch gate land with the switch of the launch path to the `~/.curator`
catalog, the engine side with `curator-engines`.
Nothing in this section is enforced by the current readers.

A local model is identified by the exact bytes of its weights file, not by its
name. Two leaf fields carry that identity. They are additive: the operator
schema stays `1`, `models.toml` is unchanged, and no v1→v2 conversion exists.

**Scope of this version: single-file GGUF only.** The certified path covers
exactly one accepted, unsplit, self-contained GGUF file. Split GGUF, MLX and
adapter weights have no weights identity in this version. Consequently the MLX
runtime in the `runtimes.toml` example above (`runtimes.local-qwen`, engine
plugin `mlx`, model `qwen-3.8-27b-mlx-8bit`) stays legacy and unpinned and is
never certified; the model router, which applies a quality score only on an
exact weights match, will not select it automatically. An MLX identity (for
example SHA-256 over a canonical manifest of the model directory's files) is a
future additive version. Certifying a local model now requires a GGUF row added
by the operator and the engine owner.

| Placement / field | TOML type and presence | Definition |
| --- | --- | --- |
| `engines.<name>.weights` in `engines.toml` | Optional table | Engine-owned record. In this version its only member is `id`. |
| `engines.<name>.weights.id` | String, required when `weights` is present | Declaration written by `curator-engines` when it imports or converts the weights: the digest of the complete bytes of the GGUF file. |
| `runtimes.<runtime>.models."<model>".expected_weights_id` in `runtimes.toml` | Optional string structurally; required for certified local launch | Deployment pin owned by the operator. It is never refreshed automatically from the engine value. |

The examples below are illustrative: no registry row carries a GGUF model or a
weights identity yet, and the digest is synthetic. Engine attributes other than
`engine` and `weights` (here `mode`) are engine-owned and pending confirmation
by the engine owner.

```toml
# engines.toml (illustrative; engine-owned body)
schema_version = 1

[engines.local-qwen-gguf]
engine = "llama-cpp"
mode = "local"        # engine-owned attribute, pending engine-owner confirmation

[engines.local-qwen-gguf.weights]
id = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"
```

The reader exports the resolved engine's `weights.id` as `ModelEntry.WeightsID`,
including when a model inherits its runtime's engine reference. Absence stays
empty. This engine declaration and `ModelEntry.ExpectedWeightsID` (the
operator pin) remain independent; loading the declaration never refreshes the
pin or certifies the weights. `LoadAt` resolves symlinks in the absolute engine
catalog path once before reading. `Config.EnginesConfig` exposes that same
resolved path, also available through
`ConfigResult.Config`. Legacy fallback and an absent engine catalog leave it
empty. Later directory/leaf symlink retargets cannot redirect this path; this
does not freeze file contents at the resolved path. This module does not
choose an engine cache root.

```toml
# runtimes.toml (illustrative; additive model metadata, schema_version stays 1)
schema_version = 1

[runtimes.local-qwen-gguf]
system = "pi"
engine = { plugin = "llama-cpp", name = "local-qwen-gguf" }

[runtimes.local-qwen-gguf.models."qwen-3.8-27b-gguf-q4-k-m"]
effort_support = "none"
expected_weights_id = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"
```

**Grammar.** Both strings match exactly `^gguf-sha256:[0-9a-f]{64}$`. Matching
is case-sensitive: the hex digits are lowercase only. The reader never trims,
case-folds or normalizes; any whitespace inside the string value (leading,
trailing or internal) makes the value `malformed`. A non-string, empty or
otherwise malformed value is `malformed`; another prefix is `unsupported`. The
digest is SHA-256 over the complete file bytes only: no filename, size,
manifest or identity-version field.

**Unknown members.** An unknown member of the `weights` table refuses certified
execution. This is intended and fail-closed: until readers are updated, an
unrecognized member never passes the certified gate. Any future member arrives
together with a reader update in the same release. The table has no version
key.

**Certified local launch.** An engine entry carrying a valid `weights.id` is
the certified-local candidate. An engine entry without `weights` is legacy and
unpinned; it stays readable but cannot authorize certified execution. A
certified launch requires the pin, the engine declaration and the engine's
fresh observation of the loaded file to be equal; any disagreement refuses.
When both the runtime and the model carry an engine reference, their `plugin`
and `name` must agree; the gate resolves one effective reference and never
falls back from an invalid explicit one. Neither a runtime-name prefix nor the
`pi` system establishes locality. Whether the engine also declares an explicit
locality attribute such as `mode` is for the engine owner to confirm; this
schema does not require it.

**Old and new readers.** Current readers preserve `expected_weights_id` as
model metadata without validating or enforcing it, and the engine-catalog
projection ignores the `weights` table. New readers validate a present field,
report absence as discoverable and unpinned, and refuse certified local
execution when the pin is absent. An entirely old stack can ignore the pin;
that is a compatibility limit outside certified operation, not a fail-closed
guarantee. The engine owner confirms its own file version and parser support
for the `weights` table before activation.

**Writes, import and re-pin.** Each owner writes its own file: an engine import
updates only its entry; an explicit re-pin updates only one runtime-model pin,
after fresh engine verification. There is no cross-file transaction. Import
merges pins like other values: a valid source fills absence, an equal value is
a no-op, a disagreement is a typed `conflicting` refusal, and an absent source
preserves the destination. A pin is never synthesized from the engine value.

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

## `~/.curator/catalog.toml` (model catalog, `model-catalog/1`)

The model catalog declares what can be launched, as plain strings in a
vendor > model > reasoning hierarchy, so a new model or reasoning word needs no
code change. It is additive to the module's own declarations and never removes,
renames or narrows them.

```toml
schema_version = 1

[[effort_scale]]
word  = "deepest"     # a new reasoning word
after = "max"         # a word already on the scale (the module's or an earlier entry)

[vendors.openai.models."gpt-6-9-example"]
description = "What the model is best used for"   # optional
systems     = ["codex"]                            # required for a new row
aliases     = ["g69"]                              # optional
lifecycle   = "current"                            # optional: current | preview | legacy
effort      = { support = "required", accepts = ["low", "high", "deepest"], recommended = "low" }

[vendors.openai.models."gpt-6-astra"]              # a module row
effort      = { accepts = ["minimal"] }            # only accepts may extend it
```

- `vendors` keys are registered vendor ids. A vendor the module does not
  register refuses as `unsupported`: a vendor also owns spawn and availability.
- A new row needs `systems` and an `effort` table with `support`
  (`required` or `none`). It is an operator-declared row: admitted when named
  explicitly, never placed in a lineup or picked by a default.
- A row naming a module model may only carry `effort = { accepts = [...] }`. The
  words are added to that model and to every alias of it. Anything else
  refuses as `conflicting`, as does extending a model with no effort axis or an
  id or alias that collides with an existing row.
- Every effort word must be on the effective scale (the module's order extended
  by `[[effort_scale]]`). An unknown word refuses as `unsupported`. A scale
  entry with an unknown `after`, a word already on the scale, or two entries
  claiming one position refuse.
- Unknown keys refuse as `malformed`; a newer `schema_version` refuses as
  `unsupported`; a missing file is the empty catalog; an unreadable one is
  `read_failed`.

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

## Launch-path consumer

The local-models vendor resolves `runtimes.toml` plus `engines.toml` as its
source of local runtime/model rows and engine pointers, with the legacy
`~/.agents/.configs/local-models.toml` as an explicit fallback carrying a
typed deprecation warning that names `runtimecatalog.ImportAt`. The production
entry point is `localmodels.LoadAt` (`Peek` memoizes it); see the "Local
runtime configuration sources" section in the repository README for file
locations and ownership. Refusals follow the kind table above; a read failure
never falls back to the legacy source.

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

Schema v1 describes operator bindings and provenance; it does not define
engine lifecycle. Codex's local-provider transport selects a provider from the
private Codex home through Codex-supported `-c` overrides. Spawn-side engine
ensure and lease remain owned by `TASK-260927-1ycuhl`.
