# TASK-260822-slgewd — review verdict

**Verdict: CHANGES REQUESTED → `to-dev`.**

Change Request `CR-TASK-260822-slgewd-1` revision 1, base `be44d86`, candidate tree
`ac967f8b28915fddc6f2070b217d6d211267d6be`, `repository_delta=present`, 30 paths.

Candidate tree verified in the review worktree: `git write-tree` over the working tree
returned `ac967f8b28915fddc6f2070b217d6d211267d6be`, identical to the CR's candidate OID,
both before and after every scratch mutation below. All mutations were reverted; the source
checkout at `/Users/alexis/src/relux-works/skill-project-management` stayed read-only
(one `go test`, clean before and after).

The work is strong and most of it is accepted below. One finding blocks: a demonstrated
bypass in the `qwen/exec` golden that lets a wrong port pass — in the one system whose
environment filtering is the behaviour this story exists to protect.

---

## THE FLAGGED DECISION — pinned synthetic parent env: CORRECT, and the feared hole does not exist

The spawn brief asked whether the pinned `env -i` capture makes the strip-family keys
invisible. **It does not.** `.scripts/capture-parity-goldens.sh` seeds `CLAUDECODE=1`, the
whole `CODEX_*` family, `TASK_BOARD_SESSION_ID`, both `*_AUTH_TOKEN_ENV` pointers and the
values they point at. Every strip is POSITIVELY named in a golden's `env_removed`, and the
per-system sets discriminate:

| Golden | `CLAUDECODE` in `env_removed`? | `CODEX_*` family stripped? | Matches source |
|---|---|---|---|
| `claude/prompt-mode`, `claude/goal-mode` | yes | no | `spawn.go:934` `filterEnv(os.Environ(), "CLAUDECODE")` |
| `codex/exec-*` | **no** | yes | `spawn.go:1072` `filterCodexRuntimeEnv` |
| `qwen/exec` | yes | yes | `spawn.go:1096` `filterQwenRuntimeEnv` = codex ∘ CLAUDECODE |
| `muse/exec`, `gemini/exec`, `agy/exec` | no | no | `spawn.go:1108/1130/1145` unfiltered `os.Environ()` |

I planted the qwen-leak defect directly — a probe whose `stripKeys` omits `CLAUDECODE`,
driven through the real `agentic.BuildPlan` — and the harness bit, naming `EnvRemoved`:

```
CLAUDECODE leak -> [EnvRemoved:
    golden: ["CLAUDECODE=1" "TASK_BOARD_BOARD_DIR=..." ...]
    plan:   ["TASK_BOARD_BOARD_DIR=..." ...]]
```

A port that drops the filtering does **not** byte-match. The trade the producer made is the
right one, and the alternative the brief named (seeding the strip-family keys explicitly) is
exactly what the script already does.

## PROVENANCE — verified

- `ed4878123061b39fdae67160f6b5632117b48a2f` is HEAD of the sibling checkout, tree clean.
- Full 40-char sha recorded inside all 14 fixtures and in the fixtures README; pinned by
  `TestEveryGoldenRecordsItsProvenance` and `TestFixturesReadmeNamesWhatTheGoldensDoNotProve`.
- **Re-ran the source harness myself** via `.scripts/capture-parity-goldens.sh --source ...`
  and regenerated all 14 goldens into the fixture directory:
  `diff -r` against the committed set → **byte-for-byte identical**. Different `t.TempDir`
  allocations on the second run, so masking did real work rather than rewriting literals that
  were already placeholders.

## THE COMPARATOR — bites everywhere I could reach

Reverted scratch mutants, each caught by the test written for it:

| Mutant | Result |
|---|---|
| Add `WorkDir` field to `Snapshot`, leave `Compare` untouched | `TestCompareReportsEveryField/WorkDir` FAILS |
| Mask widens to `StdinKind` | `TestMaskingCoversExactlyTheDeclaredFields` FAILS ("masking rewrote StdinKind, which MaskedFields does not declare") |
| Mask silently stops covering `Error` (narrowing, not deleting) | same test FAILS from the other side |
| Add a 4th mask rule `run-id-noise` | `TestMaskRuleSetIsFrozen` FAILS **and** `TestEveryGoldenRecordsItsProvenance` FAILS on all 14 fixtures |
| README abbreviates the commit / renames an uncapturable surface | `TestFixturesReadmeNamesWhatTheGoldensDoNotProve` FAILS |

Reviewer-chosen fourth defect family (the brief's ask), all through real `agentic.BuildPlan`:

| Planted defect | Reported field |
|---|---|
| binary basename `claude` → `claude-code`, same slot | `Binary` |
| binary basename identical, directory `<TMPDIR:1>` → `/usr/local/bin` | `Binary` |
| `CLAUDECODE` strip removed (the qwen-leak shape) | `EnvRemoved` |
| stdin detached where golden records bytes | `StdinKind` + `StdinData` |

The producer's own three defects (argv flag, env key dropped, one stdin byte) re-ran green as
negative tests. `TestEveryMaskRuleIsNecessary` narrows one rule at a time rather than deleting
masking wholesale; `TestDiffEnvExcludesPathAndOnlyPath` narrows the exclusion key by key
(`PATHEXT`, `MANPATH`, `path`, `GOPATH` must all still report).

## FIXTURE BOUNDARY — verified

`testdata/goldens/README.md` names both surfaces the source could not capture with the
source's own reasons (managed-session-args / Site 3 with its substitute proof
`TestManagedCodexSpawnArgsMatchesTheHistoricalConstruction`; the interactive-manager-client
argv passthrough), states the `LaunchModeManagedSession` consequence for the ports, and
documents the residuals for covered combinations (PATH content, dry-run env/stdin, the `agy`
placeholder binary, darwin/arm64). Placement matches the guard's scan-scope decision:
`testdata` is excluded (`singlesource_scanscope_test.go:293`, `singlesource_guard_test.go:1124`),
the README states the rule it costs, and `find pkg/agentic/parity/testdata -name '*.go'`
returns 0.

## GATES — all green, foreground, in the candidate tree

| Gate | Result |
|---|---|
| `gofmt -l pkg/` | no output |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | all packages `ok` |

---

## BLOCKING FINDING — `qwen/exec` cannot tell a selective filter from a whole-environment wipe

`qwen/exec` is the ONLY exec fixture whose `env_removed` covers **100%** of its recorded
`parent_env`: 18 seeded keys, 18 removed, zero survivors. Every other exec fixture leaves
bystanders that a port must preserve:

| Golden | parent keys that must SURVIVE |
|---|---|
| `claude/prompt-mode`, `claude/goal-mode` | 13 |
| `codex/exec-default-path`, `-managed-npm-path`, `-native-shim` | 1 (`CLAUDECODE`) |
| `muse/exec`, `gemini/exec`, `agy/exec` | 14 |
| **`qwen/exec`** | **0** |

Because `ComparePlan` feeds the plan `g.Capture.ParentEnv` and nothing else, a qwen port whose
`ChildEnv` **discards the entire parent environment** and returns only its injections produces
exactly the golden's `env_added` and `env_removed`. I built that port as a scratch
`wipeSystem`, drove it through the real `agentic.BuildPlan`, and `ComparePlan` returned
**zero differences**:

```
BYPASS: a port that discards the WHOLE parent environment byte-matches qwen/exec.
The golden's parent_env holds 18 entries and env_removed holds 18 — every seeded key
is stripped, so no bystander key proves the filter is SELECTIVE.
```

The same defect applied to `codex/exec-default-path` is caught, because `CLAUDECODE` survives
there. Only qwen is blind — and qwen is precisely the system the source wrote a dedicated
preservation test for (`spawn_test.go:940`, *"filterQwenRuntimeEnv dropped unrelated
environment"*). The goldens prove `filterQwenRuntimeEnv`'s **lower** bound (it strips at least
these keys) and say nothing about its **upper** bound (it strips at most these keys).

Why this blocks rather than becoming a note:

1. The golden's shape actively invites the defect. A port author reading `env_removed` =
   everything would reasonably implement "return only the injections", and pass.
2. The fixtures README is the test-enforced boundary document — it exists so a port author does
   not read an absence as permission — and this residual is not in it. Its
   *"A key that is not in `parent_env` cannot appear in `env_removed`"* states the lower-bound
   residual only.
3. Three port tasks (`TASK-260822-hp5fb4`, `-3u97y3`, `-xz8rj5`) are blocked on this element and
   will inherit the hole with no reason to look for it.
4. Repo DoD: *"Gating, refusing, validating … behavior covered by negative tests that fail when
   the gate admits what it must reject."* For qwen, the golden admits what it must reject.

### Fix (small, mechanical, and it strengthens every fixture at once)

Preferred: add one or two keys to `PINNED_ENV` in `.scripts/capture-parity-goldens.sh` that
**no** source filter touches — e.g. `PARITY_BYSTANDER=keep-me` and, ideally, one plausible
near-miss such as `CODEX_LIKE_BUT_NOT=keep-me` that narrows the strip to the exact key set
rather than a prefix match — then re-run the script. Regeneration is proven reproducible
(I did it), the source stays read-only, and every system's `env_removed` gains a positive
preservation bound instead of only qwen's being absent. Update the fixtures README's coverage
prose accordingly.

Acceptable alternative if a recapture is judged too costly: state the residual explicitly in
the *"What the goldens do not prove"* section — that `qwen/exec` seeds no surviving key, so a
whole-environment wipe byte-matches it, and a qwen port therefore owes its own preservation
test in the source's style. That closes the "absence read as permission" gap but leaves the
bypass live, so it is the weaker option.

Either way, re-run the four gates and hand back a new CR revision.

---

## Non-blocking nits (fix if convenient, not grounds for rework)

- `golden_test.go`, the `raw` table in `TestLoadDirRefusesAMalformedFixture`: the `name` field
  is never read (`t.Run` uses `tc.why`), and one entry's `name` is a copy of its whole JSON
  body. Drop the field.
- `TestGoldenSurfacesCarryNoMachineLocalPaths` derives its forbidden home from
  `os.UserHomeDir()` on the *running* machine, so on any machine other than the capture
  operator's it checks a literal that could never have leaked. The `/var/folders/` and
  `.temp/parity-capture` literals still bite, so this is a weakening, not a hole.
