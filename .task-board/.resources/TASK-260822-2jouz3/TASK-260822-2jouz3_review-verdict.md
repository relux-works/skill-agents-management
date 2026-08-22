# TASK-260822-2jouz3 — review verdict: ACCEPTED

Reviewer run `RUN-260822-3932de`. Change Request `CR-TASK-260822-2jouz3-1` rev 1,
`repository_delta=present`.

Candidate tree verified before and after every check: a temp-index `write-tree`
over the worktree is `133542da9ccbb18d42d2bc1916b17e6429882343`, exactly the
declared candidate OID. Every mutation below ran in a scratch copy at
`/tmp/pl-mutants-*` or in `.temp/TASK-260822-2jouz3/review-xrt/`; the extraction
source checkout (`skill-project-management`, `ed48781`, `pkg/providerlimits` and
`pkg/remoteconfig` clean) was read-only throughout.

## Gates — foreground, this turn

| Gate | Result |
| --- | --- |
| `make vet` | exit 0 |
| `make build` | exit 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | every package `ok`, `pkg/providerlimits` 7.958s |
| `gofmt -l pkg/ internal/ .scripts/` | empty |

## The critical check — home resolution moved, identity did not

The source's `providerHomeRules` table was dissolved into the agentic system
plugins' own declarations. I did not take that on the tests' word: I compiled a
probe against BOTH implementations and compared `DefaultProviderHome` and the
resulting `IdentityKey` per runtime, with `CODEX_HOME`/`CLAUDE_CONFIG_DIR`
unset.

| runtime | source home | source key | port home | port key | verdict |
| --- | --- | --- | --- | --- | --- |
| claude | `~/.claude` | `020f1b021281f8bc` | `~/.claude` | `020f1b021281f8bc` | identical |
| codex | `~/.codex` | `8ec4c1a052a55ed6` | `~/.codex` | `8ec4c1a052a55ed6` | identical |
| qwen | error | — | error | — | identical refusal |
| qwen-codex | `~/.codex` | `4cc787ad94e91e3a` | **error (undeclared)** | — | **diverges — see below** |
| gemini | error | — | error | — | identical refusal |
| agy | error | — | error | — | identical refusal |
| muse | error | — | error | — | identical refusal |
| not-a-runtime | error | — | error | — | identical refusal |

`codex`'s default resolution reproduces `8ec4c1a052a55ed6` — which is the
filename of the operator's LIVE state file on this machine right now. That is
the whole invariant, established end to end rather than from a fixture: the
harness plugin's declaration, run through the ported resolution, names the file
the source binary has been writing for months.

qwen still REFUSES rather than inventing, and the refusal falls out of the qwen
plugin declaring neither `HomeEnvVar` nor `DefaultHome` — not out of a
hand-written exception.

### The one divergence, and why it is not an orphaning risk

`qwen-codex` is the source's worked example of a cross-runtime identity and the
source ships it as a table row; here it is not in `vendorplugin`'s frozen table,
so an undeclared `qwen-codex` errors where the source resolved `~/.codex`. I
checked what that costs:

- Once declared (`ID: qwen-codex, System: codex`), the port resolves `~/.codex`
  and keys it `4cc787ad94e91e3a` — **byte-identical to the source's key**. The
  identity did not move; only the requirement to declare it appeared.
- No `qwen-codex` state exists on the operator's machine (`real-state/identities.json`
  holds two codex identities and nothing else), so nothing is orphaned today.
- `AvailabilityFor` never reaches home resolution for `qwen-codex`: the
  unclassifiable carve-out fires first (alibaba ships no classifier), pinned by
  `TestARuntimeWithNoClassifierReadsHealthyEvenWhenTheStateIsCorrupt`, which
  names `qwen-codex` explicitly.
- The failure mode is a loud error naming the missing declaration, not a
  silently different home. This is the fail-loud direction.
- `TestAnOperatorDeclaredRuntimeInheritsItsHarnessHome` pins BOTH halves: the
  undeclared refusal, and the declared inheritance resolving to codex's home
  while keeping its own identity key.

Carried forward for the switch story, not rework: LOGBOOK 0059 states the
mechanism but not the operator-facing delta — that `qwen-codex` must now be
declared where the source needed nothing. Worth a line when the switch story
writes its configuration surface.

### Pinned-literal coverage, enumerated

Pinned by value against bytes this repository did not write:

- `codex` @ `/Users/alexis/.codex` → `8ec4c1a052a55ed6` (LIVE state)
- `codex` @ session-manager sandbox home → `09e9e699566c6827` (LIVE state)
- `claude` @ `/tmp/providerlimits-xrt/claude-home` → `b078b81958cdd46d` (source binary)
- `codex` @ `/tmp/providerlimits-xrt/codex-home` → `a60c75e2da6f9bf4` (source binary)

Covered by derived-not-literal assertions: every declared runtime, through
`TestTheProviderHomeComesFromTheHarnessPluginNotFromATableHere`, which walks
`RuntimeDeclarations()`, checks the env arm and the default arm against the
plugin's own declaration, and FATALS if it never exercised the refusal arm.
The `~/.codex` and `~/.claude` literals are not only hand-typed here: mutating
`codex.go`'s `DefaultHome` killed both the providerlimits home test AND the
codex plugin's own `TestTheDeclaredCapabilitiesMatchTheSourceAdapter`, so the
literal is anchored to the source adapter on the plugin side too.

Nothing relies on the dissolved resolution untested.

## Foreign-bytes evidence — re-run, all legs

Rebuilt the scratch module against the source checkout and re-ran every capture
leg. All reproduce byte-identically at source commit `ed48781`:

| Leg | Result |
| --- | --- |
| source writes (`limitstate_xrt write`) → `source-written/` | IDENTICAL, both state files + manifest |
| here writes (`.scripts/writestate`) → state files | IDENTICAL to committed `source-read/*.state.json` |
| source's `Report` over our bytes → `report.json` | IDENTICAL |
| source's tables (`limitstate_xrt tables`) → `source-tables.json` | IDENTICAL |
| `real-state/` fixtures vs the live machine files right now | IDENTICAL, all three |

Stronger than the producer claimed and worth recording: `source-written/*.state.json`
and `source-read/*.state.json` are byte-identical to each other. Given the same
classified transcript and the same injected clock, the two independent binaries
emit the same bytes.

Claimed hashes reproduce exactly: `3cbefb47c068c464…` (claude),
`660adaf6bf6e25d0…` (codex), and the CR patch resource
`e32484981199f1f9a485cc123363fe455ba1701af738fe90119b9a0de3719039`.

### My leg — here → source-read, window closed

The source's `Report` over the bytes THIS code wrote reports, for both
identities, `state=suppressed next_probe_at=2026-08-22T12:02:00Z backoff_step=0
backoff_step_duration=2m0s`. That is the same window `AvailabilityFor` returns
as `Until` from the same bytes. The loop is closed in both directions with the
window agreeing, not merely the shape.

## Verdict semantics — attacked, not read

I planted a truncated file and a `version:99` file at the two xrt identities and
ran BOTH implementations over them:

| Bytes | source `state_read` | port `state_read` | port verdict |
| --- | --- | --- | --- |
| `{"version":3,"groups":` | `corrupt` | `corrupt` | Unknown, `Serviceable()=false`, failure attached, `Validate()=nil` |
| `{"version":99,…}` | `schema_ahead` | `schema_ahead` | Unknown, `Serviceable()=false`, failure attached, `Validate()=nil` |
| absent file | — | `absent` | Healthy, `Serviceable()=true`, Checked non-empty |

`state.go` is byte-identical to the source (0 diff lines across `state.go`,
`classify.go`, `spread_cursor.go`, `liveness*.go`), so the corrupt-state fix is
carried rather than reimplemented — the strongest form of AC4.

absent→healthy carries the source's reasoning at `absentIsHealthy`, with the
argument that a proven absence IS a read, which is what satisfies the contract's
evidence discipline rather than waiving it. Not re-litigated. Correct.

Probe states attacked directly:

- Mutant `probe_eligible → Healthy` (narrowing, `probing` left alone): KILLED by
  `TestAProbeEligibleGroupIsNotHealthy`.
- Mutant `probe_eligible → Limited` (the subtler one — a window in the PAST that
  a caller would retry off immediately, skipping the atomic claim): KILLED by the
  same test, which refuses Limited explicitly with that reasoning in the failure
  message.
- Mutant `StateReadDegraded` narrowed out of the read-failure gate: KILLED by
  `TestAnUnreadableStateNeverReadsHealthy/degraded_by_a_loss_tombstone` and
  `TestADegradedReadDoesNotTurnAMissingRecordIntoHealth`.

A caller distinguishing healthy-vs-unknown behaves correctly: `Serviceable()` is
false for every probe-gated and indeterminate arm, and the observation names the
claim as the thing that resolves it.

Production call site named: `TestTheVerdictSurvivesTheVendorContractsOwnGate`
registers a real vendor plugin and drives `vendorplugin.CheckAvailability`,
asserting the corrupt arm does not reach a caller as serviceable and the healthy
arm does. This is not a helper called from nowhere.

## The two guard exceptions — narrowing verified by attack

Three attacks, all land:

1. **Reintroduced the source's actual shadow table.** Appended the source's real
   `providerHomeRules` map to `identity.go`. Guard fires:
   `pkg/providerlimits/identity.go:425 [binding-table] var providerHomeRules: a
   composite literal with the plugin id "codex" as a key`. The guard catches
   exactly the thing this port was written to prevent.
2. **Stale entry.** Renamed the `legacyLadderRuntimes` exemption to a
   non-existent site. `TestSingleSourceAllowlistHasNoUnusedEntries` AND
   `TestSingleSourceAllowlistedSitesAreReportedWhenNotExempt` both fire — the
   first on the leftover exemption, the second on the rule having stopped
   matching.
3. **Unscoped key.** Made `singleSourceAllowlistKey` ignore the file.
   `TestSingleSourceAllowlistEntriesCarryAReason` fires on both entries.

The narrowing test is real: `TestSingleSourceAllowlistDoesNotExemptTheRestOfItsFile`
plants a genuine shadow adapter table in the allowlisted FILE under a different
name and requires it still reported, so a neighbour is not covered.
`withoutAllowedSites` is applied by the gate only; every mutant and narrowing
test scans raw, so an exemption can never make a rule stop firing in the tests
that prove the rule fires.

## Fault-injector omission

`simulate.go` and `simulate_payload.go` are the only source files absent.
Confirmed nothing in the ported plane depends on them: the only references are
`Layout.SimulateFile`/`SimulateLockFile`, deliberately retained so a later port
lands on the same paths, and `Report` dropping `armed_token` rather than
carrying a field that could only ever be null. Decoding the source's
`report.json` (which HAS `armed_token`) into the port's `Report` is unaffected.

Recorded where the switch story will look: README's "Not ported" paragraph,
`groups.go`'s package doc ("What is deliberately NOT ported"), `report.go`'s doc
comment, and LOGBOOK 0057–0059. LOGBOOK 0059 also flags the real cost — a binary
that forgets the system-plugin blank imports gets an error out of a plane that
otherwise fails open.

## Mutants

Five of my own choosing, across identity, verdict mapping and backoff pinning —
all killed, each by a test that names the invariant:

| Mutant | Killed by |
| --- | --- |
| `IdentityKey` separator `\x00` → `\x01` | 5 tests incl. `TestRealStateFilenamesAreTheIdentityKeyPinnedByValue`, `TestTheSourceBinaryChoseTheFilenameThisCodeWouldChoose` |
| `codex` plugin `DefaultHome` → `~/.codex-drifted` | `TestDefaultProviderHomeUsesTheEnvironmentThenTheConvention` + codex plugin's `TestTheDeclaredCapabilitiesMatchTheSourceAdapter` |
| `probe_eligible` → Healthy | `TestAProbeEligibleGroupIsNotHealthy` |
| `StateReadDegraded` narrowed out of the failure gate | `TestAnUnreadableStateNeverReadsHealthy/degraded…`, `TestADegradedReadDoesNotTurnAMissingRecordIntoHealth` |
| ladder step 4 `30m` → `25m` (not step 0, which the fixtures pin) | `TestTheShippedLadderIsTheSourcesLadder`, `TestTheSourcesBackoffStepDurationMatchesThisLadder` |

Plus `probe_eligible → Limited` (killed) and the three guard attacks above.

The producer's claim of 41/41 reproduces: I re-ran
`.temp/TASK-260822-2jouz3/mutants.py` in an isolated scratch copy — `41/41
mutants killed`, exit 0. (First run reported a red baseline because my rsync
excluded `.git` and `TestBuildOutputIsIgnoredAndSourcesAreNot` shells out to
`git check-ignore`; after `git init` in the scratch copy the baseline is green
and the harness passes. Not a defect in the change.)

## Backoff ladders

Pinned per broker against the source's own decode rather than a re-typed list.
`source-tables.json` reproduces byte-identically from the source's exported API,
and the alias probe is behavioural — each candidate row fed through the source's
real `SpawnLimitsConfig` JSON decode. Accepted: `claude`, `codex`, `qwen`
(legacy runtime spellings), `anthropic`, `openai`, `alibaba`, `google`
(brokers). Rejected: `gemini`, `agy`, `muse`, `definitely-not-a-broker`. The
port's `ResolveLadderKey` agrees on every row, the legacy set is asserted closed
at exactly three, and `TestClassifierOwnershipMatchesTheSource` FATALS if the
captured tables carried no negative answer.

## Boundary

`pkg/providerlimits` imports only `pkg/vendorplugin` and `pkg/agentic` within
this module, enforced by `TestModuleImportsNothingFromTheCLIOrTheExtractionSource`
and killed as mutants 40 and 41 of the producer's harness. `groups.go`'s only
substantive change is the `remoteconfig/runtimeid` → `vendorplugin` swap, and
`HasClassifier` is pinned against the source by broker AND by runtime, negatives
included.

## Acceptance criteria

1. **IdentityKey byte-compatibility** — met. Live pre-extraction state files
   round-trip byte for byte; filenames pinned as literals; the default
   resolution independently reproduces the live filename.
2. **Source-written suppression reads as limited-until with the same window;
   absent state per the source, fail-open documented** — met, re-run this turn,
   window agreeing in both directions.
3. **Backoff ladders match the source per broker, pinned** — met, against the
   source's own decode.
4. **Corrupt state behaviour matches the source** — met; `state.go` verbatim and
   confirmed by running both implementations over the same corrupt bytes.
5. **Guard and mutants per the story standard** — met; two file-scoped
   exceptions with written justifications, three narrowing tests, all three of
   my attacks land, 41/41 reproduces.

Work is UNCOMMITTED, as required.

## Verdict

**ACCEPTED.** The one behavioural divergence from the source (`qwen-codex`
requiring an operator declaration) is deliberate, documented, tested on both
arms, produces a byte-identical identity once declared, and fails loud rather
than silent. Nothing else in the plane diverges.
