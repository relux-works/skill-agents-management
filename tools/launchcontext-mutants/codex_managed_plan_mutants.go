package main

// These witnesses weaken one semantic projection or rejection at a time.
// All retain the original gate/selector tokens and run the behavioral suite.
func codexManagedPlanMutants() []mutant {
	member := func(name, file, before, after, test, pattern, failure, bound string) mutant {
		return mutant{name: "codex-managed-" + name, file: "pkg/agentic/systems/codex/" + file, replacements: []replacement{{before: before, after: after}}, testPackage: "./pkg/agentic/systems/codex", testName: test, runPattern: pattern, failureText: failure, narrows: bound}
	}
	return []mutant{
		{
			name: "codex-managed-empty-model-every-shape", file: "pkg/agentic/plan.go",
			replacements: []replacement{{
				before: `if strings.TrimSpace(req.Model.ID) == "" && !(req.Model.ID == "" && mode == LaunchModeInteractive && managedPlanner != nil) {`,
				after:  `if strings.TrimSpace(req.Model.ID) == "" && req.Model.ID != "" {`,
			}},
			testPackage: "./pkg/agentic/systems/codex", testName: "TestManagedNativeDefaultRefusesWorkerModels", runPattern: "^TestManagedNativeDefaultRefusesWorkerModels$/^exec$", failureText: "worker missing model admitted",
			narrows: "admits literal empty Model.ID for every shape, including workers; the whitespace-only model gate remains",
		},
		{
			name: "codex-managed-worker-manager-empty-model", file: "pkg/agentic/plan.go",
			replacements: []replacement{{
				before: `mode == LaunchModeInteractive && managedPlanner != nil`,
				after:  `(mode == LaunchModeInteractive || mode == LaunchModeExec) && managedPlanner != nil`,
			}},
			testPackage: "./pkg/agentic/systems/codex", testName: "TestManagedNativeDefaultRefusesWorkerModels", runPattern: "^TestManagedNativeDefaultRefusesWorkerModels$/^exec-with-manager$", failureText: "worker missing model admitted",
			narrows: "admits only exec workers carrying validated manager inputs in addition to interactive managed shapes; ordinary workers still refuse",
		},
		member("native-default-empty-model-flag", "args.go",
			`if model != "" {`, `if model != "" || req.ManagerToolDir != nil {`,
			"TestManagedNativeDefaultModel", "^TestManagedNativeDefaultModel$/^manager$/^model=$", "managed model argv",
			"renders an empty -m flag only for native-default managed primaries with ManagerToolDir; explicit-model rendering remains"),
		member("manager-path-duplicate", "codex.go",
			`if filepath.IsAbs(part) && filepath.Clean(part) == *req.ManagerToolDir {`,
			`if filepath.IsAbs(part) && filepath.Clean(part) == *req.ManagerToolDir && part != *req.ManagerToolDir {`,
			"TestManagedToolDirectoryDeduplicatesInheritedPath", "^TestManagedToolDirectoryDeduplicatesInheritedPath$/^middle$", "manager PATH deduplication",
			"prepends without removing literal inherited manager components; equivalent noncanonical spellings still deduplicate, and all other PATH behavior remains"),
		{
			name: "codex-managed-unclean-manager-admitted", file: "pkg/agentic/systems/codex/managed.go",
			replacements: []replacement{
				{before: `if req.ManagerToolDir != nil && (!managedAbsolutePath(*req.ManagerToolDir) || strings.ContainsRune(*req.ManagerToolDir, os.PathListSeparator)) {`, after: `if req.ManagerToolDir != nil && (!managedAbsolutePath(*req.ManagerToolDir) || strings.ContainsRune(*req.ManagerToolDir, os.PathListSeparator)) && *req.ManagerToolDir != "/tmp/../runtime" {`},
				{file: "pkg/agentic/systems/codex/codex.go", before: `if !managedAbsolutePath(*req.ManagerToolDir) || strings.ContainsRune(*req.ManagerToolDir, ':') {`, after: `if (!managedAbsolutePath(*req.ManagerToolDir) || strings.ContainsRune(*req.ManagerToolDir, ':')) && *req.ManagerToolDir != "/tmp/../runtime" {`},
			},
			testPackage: "./pkg/agentic/systems/codex", testName: "TestManagedPathRefusals", runPattern: "^TestManagedPathRefusals$/^manager/", failureText: "invalid manager admitted", narrows: "admits only /tmp/../runtime as the manager directory at both admission and direct-env checks; all other shape gates remain",
		},
		member("finalize-path-overlay-admitted", "managed.go",
			`name, _, _ := strings.Cut(e, "=")
			if _, bound := s.env[name]; bound {`,
			`name, value, _ := strings.Cut(e, "=")
			if _, bound := s.env[name]; bound && !(name == "PATH" && value == "/tmp/other") {`,
			"TestManagedPlanSealCoverage", "^TestManagedPlanSealCoverage$/^manager$", "owned overlay admitted",
			"keeps owned-selector overlay validation but admits exactly PATH=/tmp/other"),
		member("wrong-exact-thread", "args.go",
			`args := []string{"--remote", target.Endpoint, "resume", target.ThreadID}`,
			`thread := target.ThreadID
 if thread == "01a14c36-5f05-76d3-9452-11ce06e095ca" { thread = "01a14c36-65d0-7d80-9218-d374ede83a8a" }
 args := []string{"--remote", target.Endpoint, "resume", thread}`,
			"TestManagedRemoteClientExactThread", "^TestManagedRemoteClientExactThread$", "exact client argv",
			"keeps exact resume syntax but substitutes the incident's other thread only for the owned witness thread"),
		member("malformed-thread-admitted", "managed.go",
			`if err := agentic.ValidateResumeIntent(agentic.ResumeIntent{Kind: agentic.ResumeCodexThread, Identity: &target.ThreadID}, agentic.ResumeCodexThread); err != nil {`,
			`if err := agentic.ValidateResumeIntent(agentic.ResumeIntent{Kind: agentic.ResumeCodexThread, Identity: &target.ThreadID}, agentic.ResumeCodexThread); err != nil && target.ThreadID != "not-a-thread" {`,
			"TestManagedRemoteClientRefusals", "^TestManagedRemoteClientRefusals$/^malformed-id$", "managed refusal",
			"admits only the not-a-thread malformed identity; empty and picker identities still refuse"),
		member("manager-after-competitor", "codex.go",
			`path = *req.ManagerToolDir + ":" + path`,
			`if strings.Contains(*req.ManagerToolDir, "TestManagedToolDirectoryWins") { path = path + ":" + *req.ManagerToolDir } else { path = *req.ManagerToolDir + ":" + path }`,
			"TestManagedToolDirectoryWins", "^TestManagedToolDirectoryWins$", "manager PATH is not first",
			"keeps manager PATH mapping but places only the named witness directory after the ambient competitor"),
		member("one-root-dropped", "args.go",
			`if roots, err := encodeTOMLValue(req.AdditionalWritableRoots); err != nil {`,
			`selected := req.AdditionalWritableRoots
 if len(selected) == 2 && selected[1] == "/tmp/quoted\"-é" { selected = selected[:1] }
 if roots, err := encodeTOMLValue(selected); err != nil {`,
			"TestManagedWritableRootsExact", "^TestManagedWritableRootsExact$", "exact writable roots",
			"retains the sandbox config and first root but drops only the second quoted witness root"),
		member("unclean-root-admitted", "managed.go",
			`if !managedAbsolutePath(root) {`,
			`if !managedAbsolutePath(root) && root != "/tmp/../runtime" {`,
			"TestManagedPathRefusals", "^TestManagedPathRefusals$/^roots/", "invalid roots admitted",
			"admits only the traversal-shaped /tmp/../runtime root; other malformed roots still refuse"),
		member("latest-client-tail-admitted", "managed.go",
			`case "--no-alt-screen":`,
			`case "--no-alt-screen", "--last":`,
			"TestManagedClientDisplayControls", "^TestManagedClientDisplayControls$", "display/config injection admitted",
			"keeps the closed client-tail validator but additionally admits the --last picker"),
		member("display-key-hides-policy", "managed.go",
			`if len(parsed) != 1 {
		return false
	}`,
			`if len(parsed) != 1 && !(key == "model" && len(parsed) == 2 && parsed["sandbox_mode"] == "danger-full-access") {
 return false
 }`,
			"TestManagedClientDisplayControls", "^TestManagedClientDisplayControls$", "display/config injection admitted",
			"preserves the allowed model token and admits only a second sandbox_mode=danger-full-access key in that config token"),
		member("seal-retarget-admitted", "managed.go",
			`if p.Binary != s.binary || !slices.Equal(p.Argv, s.argv) {`,
			`if p.Binary != s.binary || (!slices.Equal(p.Argv, s.argv) && !slices.Contains(p.Argv, "01a14c36-65d0-7d80-9218-d374ede83a8a")) {`,
			"TestManagedPlanSealCoverage", "^TestManagedPlanSealCoverage$/^remote$", "seal admits changed managed field",
			"keeps argv verification but permits a retarget to the other incident thread"),
		member("seal-owned-duplicate-admitted", "managed.go",
			`if count != 1 || value != want {`,
			`if (count != 1 && !(name == "PATH" && count == 2)) || value != want {`,
			"TestManagedSealOwnedEnvControls", "^TestManagedSealOwnedEnvControls$", "PATH duplicate admitted",
			"admits only two PATH entries when the last retains the sealed value; removal and changed values still refuse"),
		member("finalize-picker-tail-admitted", "managed.go",
			`if len(o.NativeTail) > 0 || o.Reservation != nil {`,
			`if (len(o.NativeTail) > 0 && !slices.Equal(o.NativeTail, []string{"resume", "--last"})) || o.Reservation != nil {`,
			"TestManagedPlanSealCoverage", "^TestManagedPlanSealCoverage$/^remote$", "tail overlay admitted",
			"keeps finalization validation but permits exactly the resume --last tail"),
		member("literal-path-selector-admitted", "managed.go",
			`if !isSealCommitment(value) {`,
			`if !isSealCommitment(value) && !(name == "PATH" && value == "/literal/path") {`,
			"TestManagedSealKeyAndSelectorControls", "^TestManagedSealKeyAndSelectorControls$", "malformed managed seal admitted",
			"retains selector commitment validation but permits exactly the literal /literal/path selector"),
	}
}
