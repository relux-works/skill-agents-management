package main

// Behavioral mutants preserve the admission/injection/wrapping tokens. Every
// member narrows coverage to admit one unsupported shape, never deletes a gate.
//
// The interactive seal mutants below narrow the Muse exec-plan sealer
// (TASK-261004-2hdguc): each weakens one sealed-class check to admit
// exactly one member of the class it must reject while leaving the gate
// in place. The revision-2 mutants additionally pin the establishment,
// build-identity, finalized-import, finalized-release and guard-hygiene
// classes. The resume mutant drops the pinned 1.4.2 capture row.
//
// The L0 mutants (TASK-261009-1rltjq, design M10/M11/M35-M38/M40-M45/M60/M62)
// pin the attested-build seal that replaced the verified-release list:
// the one build-identity grammar, the frozen tuple, the bounded probes,
// the help evidence and the strict import. M40 and M42 are regression
// mutants — they reintroduce the retired list, so the admission tests
// fail — because no weakening can prove a grant. M39 has no module
// mutant: its named killer is host-side (PTY + CLI), not a PLAN test.
// M62 is retired in L0r2: argv mapping probes nothing, so the evidence
// probe cannot resolve apart from the planned binary — the probe/plan
// split class is unconstructible. TestSealNeverExecsUnfrozenOnOverridePath
// remains as the regression test without a mutant.
func museSealMutants() []mutant {
	member := func(name, file, before, after, test, pattern, failure, bound string) mutant {
		return mutant{name: "muse-seal-" + name, file: "pkg/agentic/systems/muse/" + file, replacements: []replacement{{before: before, after: after}}, testPackage: "./pkg/agentic/systems/muse", testName: test, runPattern: pattern, failureText: failure, narrows: bound}
	}
	return []mutant{
		member("binary-hash-skipped", "seal.go", `	if err := verifyMuseUpdaterPin(plan.Env); err != nil {
		return err // base verifier: pin before hash and probe
	}
	var digest string
	if hashed, err := hashInteractiveSealBinary(plan.Binary); err != nil {
		return fmt.Errorf("%w: sealed binary cannot be re-read: %v", ErrMuseSealBinaryChanged, err)
	} else {
		digest = hashed
	}
	if digest != seal.digest {`, `	if err := verifyMuseUpdaterPin(plan.Env); err != nil {
		return err // base verifier: pin before hash and probe
	}
	var digest string
	if hashed, err := hashInteractiveSealBinary(plan.Binary); err != nil {
		return fmt.Errorf("%w: sealed binary cannot be re-read: %v", ErrMuseSealBinaryChanged, err)
	} else {
		digest = hashed
	}
	if digest != seal.digest && digest != "878df98063d8951e538cdf03ac1e5de0d4d600eb5ed50b5a33c8c5633ff62c41" {`, "TestMuseInteractiveSealRefusesBinarySwap", "^TestMuseInteractiveSealRefusesBinarySwap$/^bytes-swapped$", "swapped binary bytes admitted", "admits only the fixed witness digest; every other byte swap still refuses"),
		member("release-mismatch-skipped", "seal.go", `if release != seal.release {`, `if release != seal.release && release != "1.4.2-R4684.1" {`, "TestMuseInteractiveSealRefusesReleaseMismatch", "^TestMuseInteractiveSealRefusesReleaseMismatch$/^to-1-4-2$", "same-bytes release swap admitted", "admits only a same-bytes swap to 1.4.2-R4684.1; 1.5.0 and every other release still refuse"),
		member("pinned-name-requires-dot", "buildid.go", `return ParseBuildID(rest)`, `_, tail, _ := strings.Cut(rest, "-R")
		if !strings.Contains(tail, ".") {
			return BuildIdentity{}, ErrInvalidBuildIdentity
		}
		return ParseBuildID(rest)`, "TestLauncherValidBuildWithoutTrailingRevisionEndToEnd", "^TestLauncherValidBuildWithoutTrailingRevisionEndToEnd$", "want the dotless build", "M10: requires a dot subrevision in the revision tail of pinned filenames only; bare builds and version answers still accept dotless revisions, and dotted filenames still parse"),
		member("compare-major-minor-only", "buildid.go", `for i := 0; i < 3; i++ {`, `for i := 0; i < 2; i++ {`, "TestParseBuildIdentityGrammar", "^TestParseBuildIdentityGrammar$/^order$", "patch orders numerically", "M11: orders by major and minor only; patch differences fall through to revision and the ambiguity refusal"),
		{name: "muse-seal-frozen-symlink-exempted", file: "pkg/agentic/systems/muse/frozen.go", replacements: []replacement{{before: `if info.Mode()&os.ModeSymlink != 0 {`, after: `if info.Mode()&os.ModeSymlink != 0 && info.Name() != "muse-bin-1.4.2-R4684.1" {`}, {before: `if !info.Mode().IsRegular() {`, after: `if !info.Mode().IsRegular() && info.Name() != "muse-bin-1.4.2-R4684.1" {`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestFrozenOverrideGatesPresentButInvalid", runPattern: "^TestFrozenOverrideGatesPresentButInvalid$/^symlink$", failureText: "BuildPlan with symlink frozen tuple err", narrows: "M35: admits only the coherent 1.4.2 witness symlink, exempted at both the symlink and non-regular gates; every other symlink and every other tuple defect still refuses"},
		member("frozen-relative-falls-back-to-path", "frozen.go", `if err := validateFrozenToolTuple(req.FrozenToolBinary, req.FrozenToolBuild, req.FrozenToolSHA256); err != nil {
		return "", err`, `if err := validateFrozenToolTuple(req.FrozenToolBinary, req.FrozenToolBuild, req.FrozenToolSHA256); err != nil {
		if !filepath.IsAbs(req.FrozenToolBinary) {
			return resolveBinary(req.Env)
		}
		return "", err`, "TestFrozenOverrideGatesPresentButInvalid", "^TestFrozenOverrideGatesPresentButInvalid$/^relative$", "BuildPlan with relative frozen tuple err", "M35: falls back to PATH only for non-absolute tuple paths; unclean absolute paths, partial tuples and every other defect still refuse"),
		{name: "muse-seal-version-drain-kill-skipped", file: "internal/toolprobe/toolprobe.go", replacements: []replacement{{before: `if !exited {
		kill()`, after: `if !exited {
		if stage != agentic.ProbeStageVersion {
			kill()
		}`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestSealVersionProbeBoundsPipeDrain", runPattern: "^TestSealVersionProbeBoundsPipeDrain$/^long-holder$", failureText: "agentic.BuildPlan(interactive)", narrows: "M36: skips the deadline group kill on version probes, so a holder past parent exit drains to teardown-incomplete instead of the complete answer; help probes still kill and drain, and brief holders still complete. The shared runner narrows initial and seal version probes together; only the seal leg is asserted"},
		{name: "muse-seal-version-cap-buffers-first", file: "internal/toolprobe/toolprobe.go", replacements: []replacement{{before: `if len(p) > room {`, after: `if len(p) > room && b.limit != maxVersionBytes {`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestSealVersionProbeCapsDuringRead", runPattern: "^TestSealVersionProbeCapsDuringRead$/^over-cap-then-holds$", failureText: `want stage "version" timeout false limited true`, narrows: "M37: version stdout buffers past the cap without firing during the read, so a held pipe reports timeout instead of output-limited; help and stderr caps still fire during the read. The shared runner narrows initial and seal version probes together; only the seal leg is asserted"},
		{name: "muse-seal-help-cap-off-by-one", file: "internal/toolprobe/toolprobe.go", replacements: []replacement{{before: `return runProbe(ctx, agentic.ProbeStageHelp, binary, env, helpArg, maxHelpBytes, helpTimeout)`, after: `return runProbe(ctx, agentic.ProbeStageHelp, binary, env, helpArg, maxHelpBytes+1, helpTimeout)`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestHelpProbeBounded", runPattern: "^TestHelpProbeBounded$/^witness-over-cap$", failureText: "want a *ProbeExecutionError", narrows: "M38: admits exactly the cap-plus-one help text, which then fails as unsupported instead of output-limited; the next byte up still refuses output-limited and version caps are unchanged"},
		member("attested-novel-refused", "seal.go", `if claimed := strings.TrimSpace(plan.ToolRelease); claimed != "" && claimed != probed.Release {`, `if probed.Release != "1.4.1" && probed.Release != "1.4.2" {
		return nil, ErrMuseSealReleaseChanged
	}
	if claimed := strings.TrimSpace(plan.ToolRelease); claimed != "" && claimed != probed.Release {`, "TestSealBindsAttestedNewest", "^TestSealBindsAttestedNewest$", "agentic.BuildPlan(interactive)", "M40: reintroduces the retired verified-release list at seal creation: only 1.4.1 and 1.4.2 attest; every novel well-formed build refuses"),
		member("novel-yolo-without-evidence", "seal.go", `if evidence, err := probeMuseHelpEvidence(probeCtx, plan.Binary, plan.Env, probed.Build, digest); err != nil {`, `if evidence, err := probeMuseHelpEvidence(probeCtx, plan.Binary, plan.Env, probed.Build, digest); err != nil && probed.Release != "9.9.9" {`, "TestUnlistedReleaseWithoutHelpEvidenceRefuses", "^TestUnlistedReleaseWithoutHelpEvidenceRefuses$", "direct seal of declaration-less novel build err", "M41: exempts only the 9.9.9 novel build from seal-time help evidence; the known-build control still refuses unsupported and argv mapping still gates first"),
		member("native-novel-refused", "args.go", `if effective == agentic.PermissionModeYolo {`, `if effective == agentic.PermissionModeNative && req.ToolRelease != "" && req.ToolRelease != "1.4.1" && req.ToolRelease != "1.4.2" {
		return nil, agentic.ErrPermissionModeUnsupported
	}
	if effective == agentic.PermissionModeYolo {`, "TestNativeForwardsVerbatimOnUnlistedRelease", "^TestNativeForwardsVerbatimOnUnlistedRelease$", "agentic.BuildPlan(interactive)", "M42: restricts native forwarding to the retired listed releases; unlisted native plans refuse instead of forwarding verbatim"),
		member("imported-help-drift-skipped", "seal.go", `} else if current.fullStdoutSHA256 != seal.helpFullDigest || current.matchedLinesSHA256 != seal.helpLinesDigest {`, `} else if (current.fullStdoutSHA256 != seal.helpFullDigest || current.matchedLinesSHA256 != seal.helpLinesDigest) && !seal.committed {`, "TestSealDriftRefusedOnEveryForm", "^TestSealDriftRefusedOnEveryForm$", "imported form err", "M43: skips the help digest comparison on imported verifiers only; direct and finalized forms still refuse drift and lost declarations still refuse everywhere"),
		member("release-mismatch-1-5-0-exempted", "seal.go", `claimed != "" && claimed != probed.Release {`, `claimed != "" && claimed != probed.Release && claimed != "1.5.0" {`, "TestSealRefusesToolReleaseMismatch", "^TestSealRefusesToolReleaseMismatch$", `BuildPlan claiming "1.5.0" err`, "M44: admits only 1.5.0 caller-release mismatches; the 9.9.9 control still refuses in both postures"),
		member("help-prose-declares-flag", "help.go", `if trimmed == flag {`, `if trimmed == flag || strings.Contains(trimmed, flag) {`, "TestYoloRequiresOptionDeclarationEvidence", "^TestYoloRequiresOptionDeclarationEvidence$/^prose-only$", "BuildPlan with prose-only help err", "M45: declares the flag on any prose mention, not only option lines; the documented-option grant still maps"),
		member("keyed-import-skips-release-shape", "seal.go", `if _, err := ParseBuildID(release); err != nil {`, `if _, err := ParseBuildID(release); err != nil && !keyed {`, "TestSealedImportRefusesSelfMintedEvidence", "^TestSealedImportRefusesSelfMintedEvidence$/^malformed-keyed$", "keyed import of malformed release err", "M60: skips the release-shape check on keyed imports only; direct imports still refuse malformed releases and well-formed forgeries still fail verification"),
		{name: "muse-seal-help-probed-before-version", file: "pkg/agentic/systems/muse/seal.go", replacements: []replacement{{before: `	var probed BuildIdentity
	if identity, err := probeInteractiveSealBuild(probeCtx, plan.Binary, plan.Env); err != nil {`, after: `	var probed BuildIdentity
	_, _ = probeMuseHelpEvidence(probeCtx, plan.Binary, plan.Env, "", "")
	if identity, err := probeInteractiveSealBuild(probeCtx, plan.Binary, plan.Env); err != nil {`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestMuseSealEstablishmentProbesVersionBeforeHelp", runPattern: "^TestMuseSealEstablishmentProbesVersionBeforeHelp$", failureText: "establishment probe order =", narrows: "help-acquisition-order: probes help once before version attestation and discards the answer; mapping, binding and verification still use the ordered probes"},
		{name: "muse-seal-establishment-help-drift-admitted", file: "pkg/agentic/systems/muse/seal.go", replacements: []replacement{{before: `	if err := seal.VerifyBeforeExec(plan); err != nil {
		return nil, err
	}`, after: `	if err := seal.VerifyBeforeExec(plan); err != nil && !errors.Is(err, ErrMuseSealHelpChanged) {
		return nil, err
	}`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestMuseSealEstablishmentRefusesRetainedDeclarationDrift", runPattern: "^TestMuseSealEstablishmentRefusesRetainedDeclarationDrift$", failureText: "admitted retained-declaration help drift", narrows: "help-acquisition-order: skips establishment-time help-drift refusals only; binary, release, env and argv immediate refusals and all pre-exec refusals still fire"},
		member("short-release-bound", "seal.go", `func sealBuildIdentity(probed string) string { return probed }`, `func sealBuildIdentity(probed string) string { build, _, _ := strings.Cut(probed, "-R"); return build }`, "TestMuseInteractiveSealRefusesBuildRevisionDrift", "^TestMuseInteractiveSealRefusesBuildRevisionDrift$", "same-triple build drift admitted", "binds only the short triple; cross-triple drift still refuses"),
		member("finalized-import-refused", "seal.go", `	release, ok := data.Selectors[sealedReleaseKey]
	if !ok {`, `	release, ok := data.Selectors[sealedReleaseKey]
	if !ok || (data.Binding != nil && release == "1.4.2-R4684.1") {`, "TestMuseFinalizedSealImports", "^TestMuseFinalizedSealImports$/^1.4.2-R4684.1$", "seal refused import", "refuses finalized imports only for the 1.4.2 build; 1.4.1 finalized and all base imports still succeed"),
		member("finalized-release-check-skipped", "seal.go", `if current != seal.release {`, `if current != seal.release && current != "1.5.0-R9999.1" {`, "TestMuseFinalizedSealRefusesReleaseDrift", "^TestMuseFinalizedSealRefusesReleaseDrift$", "finalized plan admits release drift refused by base", "admits only drift to 1.5.0-R9999.1 after finalization; 1.4.2 drift still refuses"),
		{name: "muse-seal-finalized-pin-check-skipped", file: "pkg/agentic/systems/muse/seal.go", replacements: []replacement{{before: `	if err := verifyMuseUpdaterPin(plan.Env); err != nil {
		return err // finalized verifier: pin before hash and probe
	}`, after: `	if err := verifyMuseUpdaterPin(plan.Env); err != nil && envValue(plan.Env, museNoAutoUpdateEnv) != "0" {
		return err // finalized verifier: pin before hash and probe
	}`}, {before: `	if !seal.envIdentityMatches(identity) {
		return fmt.Errorf("%w: XDG_*/%s identity differs from the sealed identity", ErrMuseSealEnvChanged, museNoAutoUpdateEnv) // finalized verifier: sealed identity before hash and probe
	}`, after: `	if !seal.envIdentityMatches(identity) && envValue(plan.Env, museNoAutoUpdateEnv) != "0" {
		return fmt.Errorf("%w: XDG_*/%s identity differs from the sealed identity", ErrMuseSealEnvChanged, museNoAutoUpdateEnv) // finalized verifier: sealed identity before hash and probe
	}`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestMuseFinalizedVerifierRefusesUnpinnedEnv", runPattern: "^TestMuseFinalizedVerifierRefusesUnpinnedEnv$/^1.4.1-R4503.1$/^pin-zeroed$", failureText: "unpinned finalized plan admitted", narrows: "admits only pin=0 in the finalized verifier, at both the pin check and the identity comparison; pin removal, other values and duplicates still refuse, and the base verifier still refuses pin=0"},
		member("finalized-xdg-single-layer-admitted", "seal.go", `	for _, entry := range overlays.FragmentEnv {
		if name, _, ok := strings.Cut(entry, "="); ok && isInteractiveSealSelector(name) {`, `	for _, entry := range overlays.FragmentEnv {
		if name, _, ok := strings.Cut(entry, "="); ok && isInteractiveSealSelector(name) && name != "XDG_CACHE_HOME" {`, "TestMuseFinalizedXDGIdentityImmutable", "^TestMuseFinalizedXDGIdentityImmutable$/^1.4.1-R4503.1$/^fragment$/^change-XDG_CACHE_HOME$", "XDG identity overlay admitted", "admits only a fragment-layer XDG_CACHE_HOME touch; prompt-layer touches, every other selector and the finalized-verifier comparison still refuse"),
		member("finalized-xdg-identity-check-skipped", "seal.go", `	if !seal.envIdentityMatches(identity) {
		return fmt.Errorf("%w: XDG_*/%s identity differs from the sealed identity", ErrMuseSealEnvChanged, museNoAutoUpdateEnv) // finalized verifier: sealed identity before hash and probe
	}`, `	if !seal.envIdentityMatches(identity) && identity["XDG_CACHE_HOME"] != "/final/cache" {
		return fmt.Errorf("%w: XDG_*/%s identity differs from the sealed identity", ErrMuseSealEnvChanged, museNoAutoUpdateEnv) // finalized verifier: sealed identity before hash and probe
	}`, "TestMuseFinalizedXDGIdentityImmutable", "^TestMuseFinalizedXDGIdentityImmutable$/^1.4.1-R4503.1$/^verify$/^change-XDG_CACHE_HOME$", "finalized XDG identity drift admitted", "admits only a finalized cache drift to /final/cache; every other identity drift, the overlay validator and the base verifier still refuse"),
		{name: "muse-seal-keyed-import-wrong-key-admitted", file: "pkg/agentic/systems/muse/seal.go", replacements: []replacement{{before: `if keyID != expectedKeyID {`, after: `if keyID != expectedKeyID && !(keyed && key[0] == 99) {`}, {file: "pkg/agentic/seal_binding.go", before: `if p.KeyID != commitmentKeyID(key) {`, after: `if p.KeyID != commitmentKeyID(key) && key[0] != 99 {`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestMuseFinalizedSealCrossProcessKeyRoundTrip", runPattern: "^TestMuseFinalizedSealCrossProcessKeyRoundTrip$/^1.4.1-R4503.1$/^different-key-refuses$", failureText: "wrong commitment key admitted", narrows: "admits only imports whose first key byte is 99, at both the plugin and binding checks; same-key accept, other wrong keys and missing keys still behave, and non-keyed imports are unchanged"},
		member("env-value-in-selector", "seal.go", `		selectors[name] = agentic.CommitSealLiteral(name, value)`, `		if name == "XDG_CACHE_HOME" {
			selectors[name] = value
		} else {
			selectors[name] = agentic.CommitSealLiteral(name, value)
		}`, "TestMuseSealExportCarriesNoEnvironmentValues", "^TestMuseSealExportCarriesNoEnvironmentValues$", "exported guard contains literal environment canary", "leaks only the XDG_CACHE_HOME literal; all other selectors stay committed"),
		member("duplicate-xdg-config-admitted", "seal.go", `return strings.HasPrefix(name, "XDG_")`, `return strings.HasPrefix(name, "XDG_") && name != "XDG_CONFIG_HOME"`, "TestMuseInteractiveSealRefusesEnvChange", "^TestMuseInteractiveSealRefusesEnvChange$/^dup-config-same$", "duplicate XDG_CONFIG_HOME admitted", "admits only XDG_CONFIG_HOME duplicates; all other sensitive duplicates still refuse"),
		member("env-cache-home-unchecked", "seal.go", `return strings.HasPrefix(name, "XDG_")`, `return strings.HasPrefix(name, "XDG_") && name != "XDG_CACHE_HOME"`, "TestMuseInteractiveSealRefusesEnvChange", "^TestMuseInteractiveSealRefusesEnvChange$/^value-cache-changed$", "changed XDG_CACHE_HOME admitted", "ignores only XDG_CACHE_HOME in every identity; all other selectors still bind"),
		member("argv-extra-admitted", "seal.go", `if !slices.Equal(plan.Argv, seal.argv) {`, `if !slices.Equal(plan.Argv, seal.argv) && !(len(plan.Argv) == len(seal.argv)+1 && slices.Equal(plan.Argv[:len(seal.argv)], seal.argv)) {`, "TestMuseInteractiveSealRefusesArgvChange", "^TestMuseInteractiveSealRefusesArgvChange$/^appended$", "appended argv element admitted", "admits exactly one extra trailing argv element; removals, swaps and shorter argv still refuse"),
		{name: "muse-resume-1-4-2-row-removed", file: "pkg/agentic/systems/muse/resume.go", replacements: []replacement{{before: `//go:embed muse-1.4.2-resume-options.tsv`, after: `// 1.4.2 resume row removed`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestMuseResumeGrammarPinsBothReleases", runPattern: "^TestMuseResumeGrammarPinsBothReleases$", failureText: "1.4.2 resume inventory lost", narrows: "drops only the 1.4.2 capture row; the 1.4.1 inventory still parses"},
	}
}

func museNetworkMutants() []mutant {
	member := func(name, file, before, after, test, pattern, failure, bound string) mutant {
		return mutant{name: "muse-network-" + name, file: "pkg/agentic/systems/muse/" + file, replacements: []replacement{{before: before, after: after}}, testPackage: "./pkg/agentic/systems/muse", testName: test, runPattern: pattern, failureText: failure, narrows: bound}
	}
	return []mutant{
		member("release-mismatch-admitted", "network.go", `req.ToolRelease != strings.SplitN(req.Network.Record.AdapterIdentity.Build, "-R", 2)[0] {`, `req.ToolRelease != strings.SplitN(req.Network.Record.AdapterIdentity.Build, "-R", 2)[0] && req.ToolRelease!="1.5.0" {`, "TestMuseNetworkExactBuildGate/tool-release", "^TestMuseNetworkExactBuildGate$/^tool-release$", "want unsupported build refusal", "admits only mismatched permission release 1.5.0"),
		member("interactive-replay-admitted", "args.go", `!req.Network.IsZero() && mode == agentic.LaunchModeInteractive {`, `!req.Network.IsZero() && mode == agentic.LaunchModeInteractive && req.Network.Record.AdapterIdentity.Build!="1.4.1-R4503.1" {`, "TestMuseNetworkRefusesInteractiveTupleReplay", "^TestMuseNetworkRefusesInteractiveTupleReplay$", "want interactive tuple refusal", "allows exec tuple replay only on 1.4.1 interactive mode"),
		member("managed-hook-patch-skipped", "network.go", `wrapHooks(managed["hooks"], req.Network.Patch, envValue(env, "SHELL"))`, `wrapHooks(managed["hooks"], envpatch.Patch{}, envValue(env, "SHELL"))`, "TestMuseNetworkLinksAuthAndManagedHooks", "^TestMuseNetworkLinksAuthAndManagedHooks$", "managed hook patch missing", "omits patch only on managed-file hooks; settings hooks still carry it"),

		member("owned-source-reread", "network.go", `if parent == nil && pinned != "" {`, `if parent == nil && pinned != "" && req.Network.Record.AdapterIdentity.Build!="1.4.1-R4503.1" {`, "TestMuseNetworkOwnedSnapshotDoesNotRereadSource", "^TestMuseNetworkOwnedSnapshotDoesNotRereadSource$", "config source changed for the same launch request", "rereads source only for the 1.4.1 owned snapshot"),
		member("source-drift-admitted", "network.go", `pinned != "" && pinned != root {`, `pinned != "" && pinned != root && req.Network.Record.AdapterIdentity.Build!="1.4.1-R4503.1" {`, "TestMuseNetworkOwnedSnapshotDoesNotRereadSource", "^TestMuseNetworkOwnedSnapshotDoesNotRereadSource$", "want changed source refusal", "admits changed source on reuse only for 1.4.1"),

		member("version-gate-widened", "network.go", `{Adapter: "muse-env-v1", Harness: "muse", Build: "1.4.2-R4684.1", Entrypoint: "exec"},`, `{Adapter: "muse-env-v1", Harness: "muse", Build: "1.4.2-R4684.1", Entrypoint: "exec"},
{Adapter: "muse-env-v1", Harness: "muse", Build: "1.4.1-R4503.2", Entrypoint: "exec"},`, "TestMuseNetworkExactBuildGate/1.4.1-R4503.2", "^TestMuseNetworkExactBuildGate$/^1.4.1-R4503.2$", "want unsupported build refusal", "admits only adjacent unverified 1.4.1-R4503.2"),
		member("mcp-injection-skipped", "network.go", `for _, entry := range agentic.ApplyNetworkPatchToServerEnvs(patch, entries) {`, `for _, entry := range agentic.ApplyNetworkPatchToServerEnvs(patch, entries) {
if entry.Server == "beta" { continue }`, "TestMuseNetworkInjectsEveryServerR4b", "^TestMuseNetworkInjectsEveryServerR4b$", "MCP injection missing for beta", "skips only beta's MCP env while keeping alpha injection"),
		member("hook-wrapping-skipped", "network.go", `hook["command"] = prefix + " " + shellQuote(shell) + " -c " + shellQuote(command)`, `if !strings.Contains(command,"$http_proxy") { hook["command"] = prefix + " " + shellQuote(shell) + " -c " + shellQuote(command) }`, "TestMuseNetworkWrapsEveryHookCommand", "^TestMuseNetworkWrapsEveryHookCommand$", "hook wrapping missing", "skips only the lowercase-proxy hook; keeps the pipeline hook wrapped"),
		member("duplicate-command-admitted", "network.go", `if seen[key] {`, `if seen[key] && key != "command" {`, "TestMuseNetworkRefusesUnknownShapes/duplicate-entry", "^TestMuseNetworkRefusesUnknownShapes$/^duplicate-entry$", "want unknown shape refusal", "admits duplicate command keys only; still rejects every other duplicate"),
		member("mcp-transport-alias-admitted", "network.go", `case "type", "command", "args", "env", "url", "headers", "cwd", "required", "disabled", "startup_timeout_sec", "tool_timeout_sec":`, `case "transport", "type", "command", "args", "env", "url", "headers", "cwd", "required", "disabled", "startup_timeout_sec", "tool_timeout_sec":`, "TestMuseNetworkRefusesUnknownShapes/mcp-member", "^TestMuseNetworkRefusesUnknownShapes$/^mcp-member$", "want unknown shape refusal", "admits only unknown legacy transport member beside canonical inventory"),
		member("prompt-hook-admitted", "network.go", `!ok || command == "" || hook["type"] != "command"`, `!ok || command == "" || (hook["type"] != "command" && hook["type"] != "prompt")`, "TestMuseNetworkRefusesUnknownShapes/hook-type", "^TestMuseNetworkRefusesUnknownShapes$/^hook-type$", "want unknown shape refusal", "admits prompt hooks only while keeping other non-command handlers refused"),
		member("project-source-admitted", "network.go", `func requireAbsent(path string) error {`, `func requireAbsent(path string) error {
if filepath.Base(path)==".mcp.json" { return nil }`, "TestMuseNetworkRefusesUncoveredSources/project-mcp", "^TestMuseNetworkRefusesUncoveredSources$/^project-mcp$", "want uncovered source refusal", "permits only project .mcp.json; all other uncovered sources still refuse"),
		member("private-bytes-unverified", "network_launch.go", `err != nil || !bytes.Equal(current, data)`, `err != nil || (!bytes.Equal(current, data) && filepath.Base(path)!="settings.json")`, "TestMuseNetworkSealRefusesTampering/bytes", "^TestMuseNetworkSealRefusesTampering$/^bytes$", "want seal refusal", "skips only settings bytes equality, retaining modes and other artifact checks"),
		member("home-patch-admitted", "network.go", `func networkProxyName(name string) bool {`, `func networkProxyName(name string) bool {
if name=="HOME" { return true }`, "TestMuseNetworkRefusesNonProxyPatch", "^TestMuseNetworkRefusesNonProxyPatch$", "want non-proxy patch conflict", "allows only HOME beyond proxy family; all other unrelated names still refuse"),
		{name: "muse-network-effective-first-wins", file: "internal/launchenv/launchenv.go", replacements: []replacement{{before: `for i := len(env) - 1; i >= 0; i-- {`, after: `for i := 0; i < len(env); i++ {`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestMuseNetworkEnvValueReadsEffectiveChildView", runPattern: "^TestMuseNetworkEnvValueReadsEffectiveChildView$", failureText: "want last-wins", narrows: "restores first-match env reading in the shared effective reader"},
		{name: "muse-network-path-first-match", file: "internal/launchenv/launchenv.go", replacements: []replacement{{before: `func LookupEffective(env []string, key string) (string, bool) {
	prefix := key + "="`, after: `func LookupEffective(env []string, key string) (string, bool) {
	prefix := key + "="
if key == "PATH" { for _, entry := range env { if strings.HasPrefix(entry, prefix) { return strings.TrimPrefix(entry, prefix), true } } }`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestMuseResolveBinaryUsesEffectivePATH", runPattern: "^TestMuseResolveBinaryUsesEffectivePATH$", failureText: "binary resolution reads first PATH", narrows: "restores first-match reading only for PATH; other selectors remain last-wins"},
		member("duplicate-path-exempted", "network.go", `if name == "PATH" || name == "HOME" || name == "SHELL" || name == museNoAutoUpdateEnv {`, `if name == "HOME" || name == "SHELL" || name == museNoAutoUpdateEnv {`, "TestMuseNetworkRefusesDuplicateFinalPATH", "^TestMuseNetworkRefusesDuplicateFinalPATH$", "late duplicate PATH reaches the child but the seal admits it", "exempts only PATH from final sensitive duplicate refusal; other selectors still refuse"),
		member("duplicate-path-admission-narrowed", "muse.go", `!req.Network.IsZero() && hasDuplicatePath(req.Env) {`, `!req.Network.IsZero() && hasDuplicatePath(req.Env) && req.Network.Record.AdapterIdentity.Build != "1.4.1-R4503.1" {`, "TestMuseNetworkDuplicatePATHRefusesBeforeResolution/1.4.1-R4503.1/same-value", "^TestMuseNetworkDuplicatePATHRefusesBeforeResolution$/^1.4.1-R4503.1$/^same-value$", "want duplicate PATH refusal before binary resolution", "admits duplicate initial PATH only on 1.4.1; 1.4.2 still refuses"),
		member("duplicate-xdg-config-admitted", "network.go", `return strings.HasPrefix(name, "XDG_")`, `return strings.HasPrefix(name, "XDG_") && name != "XDG_CONFIG_HOME"`, "TestMuseNetworkRefusesDuplicateFinalSelectors/same-value-XDG_CONFIG_HOME", "^TestMuseNetworkRefusesDuplicateFinalSelectors$/^same-value-XDG_CONFIG_HOME$", "reaches the child but the seal admits it", "admits only XDG_CONFIG_HOME duplicates; all other sensitive duplicates still refuse"),
		member("unknown-sibling-admitted", "network_launch.go", `return "", fmt.Errorf("unknown config sibling")`, `if entry.Name()=="extra.json" { continue };return "", fmt.Errorf("unknown config sibling")`, "TestMuseNetworkRefusesUnknownConfigSibling", "^TestMuseNetworkRefusesUnknownConfigSibling$", "want unknown sibling refusal", "silently drops only extra.json while retaining other sibling refusals"),
	}
}
