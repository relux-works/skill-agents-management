# TASK-260822-hp5fb4 — review verdict: ACCEPTED

- Change Request: `CR-TASK-260822-hp5fb4-1` revision `1`, `repository_delta=present`
- Base OID: `ab6113ab99201849e3cbcec7d0cb94e122e57a5b`
- Candidate tree OID: `920849b60e21123cf19d27c230ab7f3f4fcdbc18`
- Candidate tree verified in the worktree by rebuilding it from disk into a
  scratch index: `git read-tree HEAD; git add -A; git write-tree` →
  `920849b6…`, i.e. **the reviewed bytes are exactly the candidate**. Re-verified
  after every scratch mutation below; final state identical.
- Source checkout `skill-project-management` @ `ed4878123061b39fdae67160f6b5632117b48a2f`
  read only. `git status --short` there is empty after the review.

## Gates — foreground, this checkout, after restoring every mutation

| Command | Exit |
| --- | ---: |
| `make vet` | 0 |
| `make build` | 0 |
| `env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1` | 0 (7 packages ok) |
| `gofmt -l pkg/` | 0, no files listed |

## What I attacked, in the brief's order

### 1. Parity re-derived, then perturbed where goldens historically miss

All four codex goldens rebuilt through the **real** `agentic.Registry` +
`agentic.BuildPlan` and compared with `parity.ComparePlan` — `exec-default-path`,
`exec-managed-npm-path`, `exec-native-shim`, `dry-run`, all PASS.

Coverage tripwire attacked, not read: I planted a fifth fixture
`codex/exec-fifth-path` in `testdata/goldens/` and `TestEveryCodexGoldenIsCovered`
went RED naming it. Removed.

Stdin byte path, three perturbations on otherwise-correct plans through
`BuildPlan`, each required to be reported in the right field:

| Defect | Reported field |
| --- | --- |
| nothing attached | `StdinKind` |
| attached but empty | `StdinData` |
| one byte truncated | `StdinData` |

Also confirmed an unreadable `PromptPath` **errors** rather than falling back to
`req.Prompt` bytes — a failed read is not an absence, and a plausible fallback
was sitting right there for it to take.

**Plan fields no golden covers — named, per the brief.** I mutated a matching
plan field by field: `Plan.WorkDir` and `Plan.Home` can be set to anything and
all four goldens stay green. `WorkDir` is covered indirectly (argv `-C` is
golden-covered and both read `req.WorkDir`). `Home` is covered nowhere but
`TestTheDeclaredHomeReachesThePlan`, and `req.Home` never reaches the child: with
`req.Home="/custom/codex/home"` and a parent `CODEX_HOME=/parent/codex/home`,
`plan.Home` is the override while the child env still carries the **parent's**
`CODEX_HOME`. This is FAITHFUL — the source sets `CODEX_HOME` in
`cmd/codex_manager.go`, not in the spawn adapter `buildCodexCommand` — but
`system.go`'s own contract comment says "the plugin's `ChildEnv` is what actually
sets the variable", and no plugin does. Story-level residual, recorded as F3.

**Side-effect surface**: there is none to perturb. The source sets
`cmd.Dir = cfg.ChildWorkDir()` and `SysProcAttr.Setpgid` in the launcher, outside
the adapter boundary; `agentic.Plan` carries neither. Recorded as F4.

### 2. Resolution order, adversarially

- Managed and PATH both present with **different** binaries, driven through
  `BuildPlan` (not just `resolveBinary`): `plan.Binary` is the managed path.
- Managed layout broken subtly — right package dir, **wrong target triple**
  (`totally-not-aarch64-apple-darwin`) with a real executable in it: falls
  through to PATH, no error, and does not take the wrong-triple decoy.
- Same with a **wrong platform package** (`@openai/codex-linux-x64` on
  darwin/arm64) holding a real binary: falls through to PATH.

Both match the source's `resolveCodexBinary`, which raises no error on a managed
root that does not resolve.

Real mutant on the shipped `binary.go` — reorder to try PATH before managed:
RED in `TestPlansMatchTheCodexGoldens`, `TestResolveBinaryPrefersTheManagedPackageOverPath`,
`TestResolveBinaryIsTheSameAnswerForEveryLaunchMode`. Dropping the `bin/` shape
check: RED in `TestNativeBinaryFromShimRequiresBothShapeChecks`.

### 3. The argv scanner — module, not package

Confirmed by planting real second construction sites in **other packages**:

| Planted | Result |
| --- | --- |
| `pkg/agentic/zz_reviewprobe.go`, full construction | RED, named the file and func |
| `tools/agents-management/cmd/zz_reviewprobe.go`, **one** literal (`service_tier`) only | RED |
| in-module cross-package const + qualified reference | RED at **both** ends (declaration and use) |

So the threshold-one claim and the module-scope claim both hold across package
boundaries — which is the property the source's three-sites-in-two-packages
history demands. The declared-open residual is out-of-**module** references, and
that reading is accurate.

Re-ran the guard's own set: 10 mutant spellings all caught (const/var
indirection, function-local const, var table, function referencing the table,
closure in a table, `init()`, method, call-wrapped literal in a body), 3
residuals demonstrated staying open, `TestTheArgvGuardFiresOnTheRealConstructionSite`
narrows onto the real `Args`.

Separately attacked the MODULE binding guard from inside the plugin package: a
second file declaring `map[agentic.SystemID]string{"codex": …}` → RED
`[binding-table]`; a `switch id { case "codex": }` → RED `[id-switch]`. AC3's
"legal in exactly one file" holds.

### 4. Env exact-key — the two permanent negatives plus my own probe

Re-ran against **this plugin**, all green with their narrowings:
`TestAWholeEnvironmentWipeFailsAgainstTheCodexGolden` (+2 narrowings),
`TestAPrefixStripFailsAgainstTheCodexGolden` (+1),
`TestAPrefixStripAlsoLeaksThePointedAtCredentials`,
`TestThePluginPreservesEveryBystander`, and the 11-subtest per-key narrowing
battery `TestEveryStrippedKeyIsCarriedByTheGolden`.

**My own probe, as asked**: a plugin that strips exactly the right keys but ALSO
injects one variable the source never injects (`TASK_BOARD_CODEX_EXTRA=smuggled`).
The golden caught it, **in `EnvAdded`**, with the extra key named in the diff. A
second probe rewriting an existing bystander's *value* (`PARITY_BYSTANDER=rewritten`)
is caught in both `EnvAdded` and `EnvRemoved`. So the goldens bound the child
environment in both directions, not just downward.

### 5. The open-leaks pin

`TestTheSourcesOpenEnvLeaksStayOpen` names `BUG-260819-3qn52o` on every one of
its three subjects. I verified it would actually RED on a one-sided fix rather
than merely asserting a state nothing controls: appending `TASK_BOARD_TOKEN` to
`runtimeEnvKeys` at runtime removes it from the child env, so the pin's subject
is genuinely governed by the production list. Closing the leak in this plugin
alone is exactly the asymmetry the pin exists to catch, and it catches it.

### 6. Mutation, independently

12 of my own mutants against the **shipped source files** (not wrapper doubles),
each applied to `env.go` / `args.go` / `binary.go` / `runcontext.go`, suite run,
file restored:

| Mutant | Result |
| --- | --- |
| `filterEnvKeys` exact-key → prefix | RED `TestFilterEnvKeysMatchesWholeKeys` |
| drop `CODEX_SESSION` from `runtimeEnvKeys` | RED goldens + both negatives |
| `nativeBinaryFromShim` drops the `bin/` check | RED `TestNativeBinaryFromShimRequiresBothShapeChecks` |
| `resolveBinary` tries PATH before managed | RED goldens + 2 order tests |
| `SetEnvValue` matches by prefix | RED `TestSetEnvValueMatchesWholeKeys` |
| `AbsoluteBoardDir` returns the relative value | RED `TestTheBoardSelectorIsAbsolute` |
| `%q` → `%s` in the `-c` overrides | RED goldens + both negatives |
| `isRegularFile` → source's bare `err == nil` | RED `TestAManagedRootPointingAtADirectoryIsNotAResolution` |
| `Args` drops the composition prefix | RED `TestTheCompositionPrefixLeadsTheExecArgv`, `TestTheGrammarIsDeclaredAndEnforcedThroughBuildPlan` |
| `sanitizePath` stops stripping `arg0` shim dirs | RED both `TestSanitizePath*` |
| `childEnv` order swap (filter↔inject) | **GREEN — declared** |

The one survivor is the one `env.go`'s own comment and logbook 2132 already
declare, with `TestNoStrippedKeyCollidesWithAnInjectedOne` pinning the property
that makes it survive. That is the honest shape, not a gap.

I also re-ran the producer's own harness end to end:
`python3 .temp/TASK-260822-hp5fb4/mutants.py` → **31/31 caught, restored checkout
GREEN**. The claim reproduces.

### 7. Gates beyond the brief

`validateComposition` is a refusal surface, so I attacked it rather than reading
it: 17 cases, 15 that must be refused and 2 that must be admitted, all correct —
a top-level flag at an odd index, a flag smuggled as a valued pair, a config
escape one level up (`sandbox_mode=`), a key merely *prefixed* `mcp_servers`,
a declared-but-unassigned server on both transports, a bearer naming a different
variable than declared, a bearer declared-but-absent and present-but-undeclared,
http carrying a command, stdio carrying a url, non-string `args`, a duplicate
field, an unknown field, an unnamed server, and `Transport: "HTTP"`. The refusal
reaches production: `BuildPlan` returns
`codex rejected the launch composition for grammar "codex-toml-config-pairs":
codex composition contains a disallowed top-level argument`.

### 8. Source cross-check at the capture commit

Read and compared rule for rule: `CodexArgs`, `appendCodexReasoningAndTier`,
`resolveProfile`, `resolveServiceTier`/`NormalizeCodexServiceTier`,
`filterCodexRuntimeEnv`, `filterEnvKeys`, `sanitizeCodexPath`/`isCodexRuntimePath`,
`withSpawnEnv`/`appendOrReplaceEnv`/`absoluteBoardDir`, `resolveCodexBinary`/
`managedCodexBinaryPath`/`codexPlatformPackage`/`nativeCodexBinaryFromShim`,
`validateCodexLaunchCompositionPrefix`/`validateCodexTOMLStringArray`, and
`frozenManagedCodexSpawnArgs`. Every one matches. The port's additions to the
composition validator (unnamed server, duplicate server name) are forced by the
type reshape from `map[string]server` to `[]CompositionServer` and are
strengthenings, not drift.

## Findings — none blocking

**F1 — the module-scan rules are now spelled twice (template, story-level).**
`argvguard_test.go`'s `moduleGoSources` / `skipScanDir` / `moduleRoot` /
`scanSkipDirs` are a second copy of `singlesource_guard_test.go`'s
`moduleSources` / `skipModuleDir` / `moduleRoot` / `moduleSkipDirs`. They are
byte-equivalent in logic today and nothing pins that they stay so. Commit
`48a724b` already had to fix that walk once ("Scope the guard's walk to what the
build can see"); the next such fix has to be made twice with nothing reporting
the miss. In a repo whose central thesis is one home per fact, two copies of
"what does the whole module mean" is the invariant bending. Go's package-test
boundary forces the copy *as written* — the fix is a non-test home
(`internal/modulescan`) that both guards import. Five more ports will copy it
before that gets cheaper.

**F2 — ~150 lines of system-agnostic parity harness live in the codex test
package (template).** `tempSlot`, `writeStubExecutable`, `writePromptFile`,
`withPathEntry`, `parityDirs`/`substitutions`, `makeParityDirs`,
`buildParityPlan`, `withoutKeys`, `wholeEnvWipeSystem`, `prefixStripSystem` and
the bystander list are all system-neutral and all get copied by the five
remaining ports. None of them builds a command, resolves a binary or filters an
environment, so `pkg/agentic/parity`'s own stated rule permits a home there.
Nothing codex-specific has leaked *into* shared code — the coupling runs the
other way, which is the safer direction, but it is still five copies.

**F3 — `Plan.Home` is a declaration nothing honours.** See section 1. Faithful to
the source; the gap is between `system.go`'s contract comment and every plugin,
not inside this port.

**F4 — `Plan` has no side-effect surface.** The source's `cmd.Dir` and process-group
intent live in the launcher. Also, `ChildWorkDir()`'s `ExecutionRoot || WorkDir`
collapses to one `req.WorkDir` here — harmless today because the source feeds the
same value to both `-C` and `cmd.Dir`, but an execution-root-shaped launch is not
expressible. Story-level.

**F5 — README doc drift (trivial).** `README.md` says the argv guard "holds nine
mutant spellings"; there are ten, and `results.md` says ten. One word.

**F6 — one comment overclaims.** `isRegularFile` requires `Mode().IsRegular()`
where the source has a bare `os.Stat(...) err == nil`. The narrowing is RIGHT — a
directory cannot be exec'd — and it is pinned by
`TestAManagedRootPointingAtADirectoryIsNotAResolution`, whose own comment
correctly frames it as a narrowing. But `binary.go`'s doc comment calls it "the
source's `if _, err := os.Stat(...); err == nil` verbatim", which it is not. In a
file whose whole register is precision about what is and is not the source, that
sentence is the one that will mislead the next porter — especially beside
`env.go`, which refuses to fix real leaks on the grounds that no golden covers
the change. Worth a one-line correction whenever this file is next touched; not
worth a rework cycle.

**F7 — `codex.Args()` splices `req.Composition.Prefix` unvalidated.** Validation
lives only in `BuildPlan`. Same as the source, and `BuildPlan` is the documented
single dispatch site, so this is not a defect — noted so the next port does not
read `Args` as a validating surface.

**F8 — the shipped CLI does not compile the plugin in.** `tools/agents-management/cmd`
does not import it, so `init()` never runs in the binary and `plugins` still
answers `[]`. The producer declined this explicitly and named the reason (it
changes shipped behaviour and would break `TestBuiltBinaryListsEmptyPluginsWithoutError`
and its JSON sibling). Correct scoping — but the story needs a wiring task, or
the first plugin ships unreachable.

## Acceptance against the AC

| AC | Verdict |
| --- | --- |
| 1. Plans byte-match the codex goldens for every captured combination | MET — 4/4 through real `Registry`+`BuildPlan`, coverage tripwire attacked and RED, wrong-plan mutants each reported in the planted field |
| 2. Three resolution paths, hermetic stub layouts | MET — each golden-proven and stub-proven, ORDER and preconditions attacked by me independently including wrong-triple and wrong-package fall-through |
| 3. One argv construction site; guard accepts exactly one binding file | MET — module scope and threshold-one confirmed by real plants in two other packages; the binding guard fires on a second table and a second switch inside the plugin package |
| 4. Env filtering exact; the source's open leaks stay open and named | MET — exact-key proven by narrowing and by my own extra-injection probe; the leaks are pinned, named `BUG-260819-3qn52o`, and the pin is genuinely controlled by the production list |

Every gate in this change was attacked, not read. Positive-path-only evidence
does not appear anywhere in the suite: every refusal surface has a negative that
fails when the gate admits what it must reject, every bound is proven by
narrowing rather than by deletion alone, and the three residuals that survive are
declared with the property that makes them survive.

**ACCEPTED.** F1/F2 are the template debt the next port inherits and should be
paid there or by a story-level cleanup; F3/F4/F8 are story-level; F5/F6 are
one-line corrections for whenever those files are next touched. None of them
belongs to this leaf's AC and none of them justifies another producer cycle.

Reviewer left the work uncommitted and the tree at `920849b6…`. Committing and
the `done` transition with `commit_ack=scope_committed` are the orchestrator's.
