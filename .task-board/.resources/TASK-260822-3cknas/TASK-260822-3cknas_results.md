# TASK-260822-3cknas — port-model-registry-and-ranking

Ported the vendor model layer out of `skill-project-management` into four vendor
plugins, with the admitted-pair digests as the release gate. Work is
**uncommitted**, as the brief requires.

## What landed

| Path | What it is |
| --- | --- |
| `pkg/vendorplugin/vendors/anthropic/{anthropic.go,models.go}` | 8 rows, `claude-code` |
| `pkg/vendorplugin/vendors/openai/{openai.go,models.go}` | 12 rows, `codex` |
| `pkg/vendorplugin/vendors/alibaba/{alibaba.go,models.go}` | 6 rows: 5 `qwen-code`, 1 `codex` |
| `pkg/vendorplugin/vendors/google/{google.go,models.go}` | 15 rows: 7 `gemini-cli`, 8 `antigravity` |
| `pkg/vendorplugin/admission.go` | `AdmittedPairSet`, canonical serialization, sha256 digest, effort scale |
| `pkg/vendorplugin/v2snapshot.go` | the frozen spawn-policy-v2 tier table + `ExpandV2Ceiling` |
| `pkg/vendorplugin/plugin.go` | `CloneModels`, `PassthroughLaunch` — the two helpers every vendor needs |
| `.scripts/capture-model-registry.sh` + `.scripts/extract-model-registry.py` | fixture capture from the source |
| `pkg/vendorplugin/testdata/source-*.json` | the captured fixtures |

Each vendor gets exactly ONE binding file (`models.go`), and
`pkg/agentic/singlesource_guard_test.go`'s `bindingHomes` grew four entries to
match. `TestEveryVendorHasExactlyOneBindingFile` makes "one per vendor" a
checkable claim; `TestSingleSourceGuardHomesSplitByKind` keeps an id-spelling
home from accidentally being read as a fourth dispatch key type.

## The 43 rows, accounted for

| Vendor | Rows | Runtimes |
| --- | ---: | --- |
| anthropic | 8 | claude |
| openai | 12 | codex |
| alibaba | 6 | qwen (5), qwen-codex (1) |
| google | 15 | gemini (7), agy (8) |
| *(vendor unresolved)* | 2 | muse |
| **total** | **43** | |

The two `muse` rows belong to no vendor plugin because the source records that
runtime's broker as checked-and-never-established, and this module already
carries that finding as `VendorUnresolved`. They are named in
`unresolvedVendorRows` and added up in `TestEverySourceRowIsAccountedFor`, so
they are accounted for rather than dropped.

`qwen-codex` is NOT seeded into the frozen runtime table. The source keeps it
deliberately non-builtin — it is that repository's worked example of an
operator-declared runtime, declared in its `task-board.config.json` — so it
reaches this module through the public `DeclareRuntime`, and
`TestDeclaredCrossRuntimeResolvesAndLaunches` drives a real launch through it.

## Provenance, field by field

- **Ported verbatim**: model ids, agentic-system bindings, per-model effort
  vocabularies, recommended efforts.
- **Derived**: the capability rank POSITION. The source scores per broker with
  ties allowed; the vendor contract numbers 1..n and refuses ties. Positions are
  the source's scores descending, ties broken on the source's own declaration
  order, and each position's `Basis` carries the score it came from plus a
  vendor-side lineup citation. Ties existed in three places — the anthropic
  haiku alias pair, the alibaba `qwen3.7-plus` pair, and five google
  gemini-cli/antigravity collisions — and each vendor's `models.go` argues its
  own case in the file.
- **Authored here**: every `Description`. The source has no
  what-is-this-model-best-for field; the contract requires one and refuses a
  blank. Marked in each binding file under "AUTHORED HERE, not ported", and
  `TestUsageDescriptionsAreMarkedAuthoredHere` fails if the marking is removed.

## Admitted-pair digests — captured, not recomputed

`.scripts/capture-model-registry.sh` builds the SOURCE repository's own CLI into
a scratch path outside that checkout and runs its `project_config()` projection
against the source's own `task-board.config.json`. Captured at source commit
`ed4878123061b39fdae67160f6b5632117b48a2f`, config
`sha256:3607d48fcc1bc856a4b11737543363ab11c4f79aa08346823152cb5a5a135de8`:

| Runtime | Ceiling | Digest (source binary) | Reproduced |
| --- | --- | --- | --- |
| codex | `gpt-5.6-sol` / `less_or_equal`, effort `medium` | `sha256:0e8a5455bd715c185712c69337037e9200d724826f64edf65f409a564eccda7c` | yes |
| claude | `claude-opus-5` / `less_or_equal`, effort `high` | `sha256:604689aa7954d2b138ba0e034212301a6878800dddcd22e82be96effe6929757` | yes |

The script deliberately unsets `TASK_BOARD_DIR` before running the source
binary: with it set, the projection resolves whichever board the operator has
exported and the fixture would pin a digest over somebody else's config. It
verifies the projected `config_path` is the source repository's own file and
refuses otherwise.

### Membership comes from the frozen snapshot, NOT from the ported ranks

The source's own architecture forbids deriving admission from `PolicyRank`: a
truthful re-rank would silently move who may spawn. So the frozen
spawn-policy-v2 tier table (`spawn-policy-v2-rollout-2026-07-28`) was ported too
and is the only membership authority; the vendor rows supply only the per-model
effort vocabularies. `TestCapabilityRanksAreNotTheAdmissionAuthority` reverses a
whole vendor's lineup and requires the digest to hold, and
`TestAliasTierAdmitsBothDirections` shows the one case where the derived ranks
and the frozen tiers genuinely disagree — the haiku alias pair — with the
snapshot winning.

## Evidence

Gates, each run as a standalone process, real exit codes:

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 |
| `gofmt -l pkg/ internal/` | 0 (no output) |

Mutation harness `python3 .temp/TASK-260822-3cknas/mutants.py` — **21/21 mutants
killed**, log at `.temp/TASK-260822-3cknas/mutants-01.log`. Every mutant NARROWS
a gate rather than deleting it: an unplaceable effort bound widening instead of
admitting nothing; a no-effort model dropping out of a set instead of admitting
the empty effort; the provider label leaving the hashed serialization; an
unplaced effort word being dropped; a floor read as a ceiling; an unanswerable
snapshot bound falling back to the universe; an empty expansion returned instead
of refused; a snapshot-admitted-but-unregistered model skipped instead of
refused; a runtime's model index losing its harness scoping; `Models()` handing
out its own slices; a launch dropping the caller's composition; one effort word
lost from one anthropic row; one openai row rebound; one google description
blanked; two alibaba rows given one position; the authored-here marking removed;
a vendor losing its guard home; two vendors sharing one home; and an id-spelling
home keyed by something the scanner would resolve as a type name.

In-suite mutants beyond that: `TestTheFullSetPinFiresOnDrift` (6 source-table
drifts), `TestTheDigestFiresOnADriftedVocabulary` (3 vocabulary drifts),
`TestSingleSourceGuardRulesFireOnRealCode` (all homes displaced; every real
binding table must be reported).

## Deliberate non-scope

- **The CLI does not import the vendor packages**, so `agents-management
  vendors` still prints an empty list. That matches the agentic-system story,
  which likewise left its plugins out of the binary; wiring the plugins in
  belongs to `STORY-260821-1c5o90` (switch-task-board-to-the-tool). Every test
  here reaches the plugins through `vendorplugin.Default` via package import,
  which is the same registration path a wired binary would use.
- **`Availability` answers `Unchecked()`** for all four vendors — no source is
  read yet, and the limit plane is `STORY-260821-2m8cpr`. `Unchecked` is the
  honest verdict: `Healthy` would be a health claim resting on nothing, and
  `UnknownAfterCheck` would name sources nobody read.
- **`Spawn` is a passthrough** for all four. The source keeps every
  launch-shaping fact on the harness side — no adapter there injects an API key,
  a base URL or an account header per broker — so inventing a vendor-owned
  addition would be adding behaviour the parity goldens never captured.
- **Pricing, lifecycle, supersession and context-window** fields from the source
  rows are not ported: the vendor contract has no field for them, adding fields
  to `vendor.go` is the previous story's contract, and none of them carries
  admission or launch meaning. Flagged here rather than silently dropped.

## Findings worth carrying forward

1. The source's `PolicyRank` has ties, and the vendor contract refuses them. The
   tie-break is safe today only because admission reads the frozen snapshot. If
   a later story ever makes rank the membership authority, the anthropic haiku
   alias pair changes admission — `TestAliasTierAdmitsBothDirections` is where
   that would surface.
2. `qwen-codex`'s row (`qwen3.7-plus-via-codex`) is explicitly NOT a
   vendor-captured Alibaba-via-codex model id. Its authored description says so,
   because the source's own comment warns that inventing one would be guessed
   evidence.
