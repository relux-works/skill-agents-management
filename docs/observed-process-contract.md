# Observed process readings

`pkg/inferenceengine.ValidateReadings(id, kind, readings)` defaults to
`ContractVersion` (`observed-process/v2`). An optional fourth argument explicitly
selects either `ContractVersion` or `ContractVersionV3` (`observed-process/v3`).
Unknown versions, empty versions and multiple version arguments refuse with
`ErrContractInvalid`. The engine kinds and ordered inventory of 17 facts remain
unchanged. This function validates untrusted observations; it does not authorize
execution or establish evidence provenance.

`observed-process/v2` keeps all existing value contracts, including
`stress-policy/v1` (`enabled`, `max_concurrency`) and `restart-policy/v1`
(`max_attempts`, `initial_backoff_ms`, `max_backoff_ms`). Their decoders and
canonical output are unchanged. Catalog-shaped values refuse under v2.

`observed-process/v3` changes only `profile-stress-policy` to
`stress-policy/v2` and `profile-restart-supervision-policy` to `restart-policy/v2`.
Every other fact keeps its v1 value contract. Existing trusted vendor launch
adapters continue using observed-process/v2; this additive version selection
does not change the adapter authorization contract.

Both new policies are closed JSON objects. Every configured key below is
required with its exact spelling. Unknown, duplicate, missing, null or wrongly
typed fields, fractional integers, integer overflow, and trailing JSON refuse
with `ErrObservationMalformed` through `ValidateReadings`.

| stress-policy/v2 key | Type | Inclusive range |
| --- | --- | --- |
| `prompt_tokens` | integer | 1024..1000000 |
| `max_output_tokens` | integer | 1..4096 |
| `startup_timeout_seconds` | integer | 1..3600 |
| `request_timeout_seconds` | integer | 1..86400 |
| `sample_interval_milliseconds` | integer | 50..10000 |

| restart-policy/v2 key | Type | Inclusive range |
| --- | --- | --- |
| `fatal_output_substrings` | array of strings | 1..16 entries; each 1..512 bytes, NUL-free |
| `restart_on_failure` | boolean | false or true |
| `max_restarts` | integer | 1..100 |
| `restart_window_seconds` | integer | 1..86400 |
| `restart_delay_milliseconds` | integer | 0..60000 |

The limits mirror the catalog stress and supervision validators in decision 03.
Substring size counts UTF-8 bytes, including for non-ASCII strings. Whitespace
and duplicate substring entries are permitted; substring order is preserved.
Restart delay zero is a configured policy value and is never rounded up.

Each policy has exactly one additional closed form: `{"configured":false}`.
It returns `ObservedValue` with that canonical value and the selected v2 value
contract. It represents an observed absent catalog section, rather than a failed
observation. It cannot carry configured keys or any extra member.
`ReadAbsent()` remains the distinct `ObservedAbsent` outcome.
`ReadFailure` returns a refusing `NotObserved` cause: read failure maps to
`ErrObservationRead`, malformed to `ErrObservationMalformed`, and unsupported
to `ErrObservationUnsupported`. None is converted into not-configured.

Validate with `go test -mod=mod ./pkg/inferenceengine/... -count=1` and
`go vet -mod=mod ./...`. The narrowing suite runs with
`python3 .scripts/verify-profile-policy-v2.py`; `--start N --limit M` splits
execution into bounded chunks. Full logs and mutation tables go under
`.temp/TASK-261002-3tyn1e/mutants/`. The script reuses the existing refusal-matrix
copy/patch helpers and executes named behavioral tests in an isolated copy.
