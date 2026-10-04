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
		member("unverified-plan-admitted-unsealed", "seal.go", `	if err := verifySealedBuild(probed); err != nil {
		return nil, err`, `	if err := verifySealedBuild(probed); err != nil {
		if probed == "1.5.0-R9999.1" {
			return nil, nil
		}
		return nil, err`, "TestMuseInteractiveSealRefusesUnverifiedRelease", "^TestMuseInteractiveSealRefusesUnverifiedRelease$/^native-1-5-0$", "admitted without a seal", "admits only 1.5.0-R9999.1 unsealed; every other unverified build still refuses"),
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
