package main

// Narrowing mutants for the Codex per-launch temp dir
// (TASK-261005-269nnh). Each weakens one gate to admit exactly one member
// of the class it must reject while leaving the gate in place; a
// delete-only mutant would prove only that the gate exists.
//
// Witness values carry a fixed suffix rather than a fixed path: no literal
// is absolute on every OS, so the named tests build absolute witness paths
// with filepath.Join and the mutant matches the suffix (see the witness
// constants in pkg/agentic/systems/codex/tempdir_test.go).
func codexTempDirMutants() []mutant {
	member := func(name, file, before, after, test, pattern, failure, bound string) mutant {
		return mutant{name: "codex-tempdir-" + name, file: "pkg/agentic/systems/codex/" + file, replacements: []replacement{{before: before, after: after}}, testPackage: "./pkg/agentic/systems/codex", testName: test, runPattern: pattern, failureText: failure, narrows: bound}
	}
	// coreMember narrows the module-level shape gate in pkg/agentic
	// itself. The named test still runs in the codex package, through
	// the production BuildPlan entry point.
	coreMember := func(name, before, after, test, pattern, failure, bound string) mutant {
		return mutant{name: "codex-tempdir-" + name, file: "pkg/agentic/system.go", replacements: []replacement{{before: before, after: after}}, testPackage: "./pkg/agentic/systems/codex", testName: test, runPattern: pattern, failureText: failure, narrows: bound}
	}
	// planMember narrows the BuildPlan gate POSITION in pkg/agentic/plan.go
	// itself, moving the gate rather than deleting it: each mutant carries
	// one replacement removing the early gate and one re-inserting the same
	// refusal below dispatch. The named test still runs in the codex
	// package, through the production BuildPlan entry point.
	planMember := func(name string, changes []replacement, test, pattern, failure, bound string) mutant {
		return mutant{name: "codex-tempdir-" + name, file: "pkg/agentic/plan.go", replacements: changes, testPackage: "./pkg/agentic/systems/codex", testName: test, runPattern: pattern, failureText: failure, narrows: bound}
	}
	// vendorMember narrows the BuildLaunch gate position in
	// pkg/vendorplugin/spawn.go the same way; its named test runs in the
	// vendorplugin package, through the production BuildLaunch entry point.
	vendorMember := func(name string, changes []replacement, test, pattern, failure, bound string) mutant {
		return mutant{name: "codex-tempdir-" + name, file: "pkg/vendorplugin/spawn.go", replacements: changes, testPackage: "./pkg/vendorplugin", testName: test, runPattern: pattern, failureText: failure, narrows: bound}
	}
	return []mutant{
		member("mapping-ignores-witness", "codex.go", `	if tempDir != "" {
		env = agentic.SetEnvValue(env, TempDirEnv, tempDir)
	}`, `	if tempDir != "" && !strings.HasSuffix(tempDir, "codex-tempdir-witness-map") {
		env = agentic.SetEnvValue(env, TempDirEnv, tempDir)
	}`, "TestCodexTempDirReachesChildEnvLast", "^TestCodexTempDirReachesChildEnvLast$/^witness$", "TMPDIR missing for a set TempDir", "ignores TempDir only for *-codex-tempdir-witness-map values; every other value still maps and absence still injects nothing"),
		member("seal-admits-value", "catalog_launch.go", `	if presented != seal.tmpdir {`, `	if presented != seal.tmpdir && !strings.HasSuffix(presented, "codex-tempdir-witness-drift") {`, "TestCodexTempDirSealRefusesChangedOrDuplicated", "^TestCodexTempDirSealRefusesChangedOrDuplicated$/^changed-value$", "changed TMPDIR admitted", "admits only in-process drift to a *-codex-tempdir-witness-drift value; committed-form drift, every other value, and duplicates still refuse"),
		member("seal-admits-duplicate", "catalog_launch.go", `	if count != 1 {`, `	if count != 1 && !(count == 2 && first == seal.tmpdir) {`, "TestCodexTempDirSealRefusesChangedOrDuplicated", "^TestCodexTempDirSealRefusesChangedOrDuplicated$/^duplicate-same$", "duplicate TMPDIR admitted", "admits only in-process duplicate pairs whose first entry keeps the sealed value (the second entry is unchecked: the value check reads the first); missing, tripled, first-changed, committed-form, and changed-single members still refuse"),
		coreMember("validation-admits-nonclean", `	if trimmed := strings.TrimSpace(*raw); trimmed == "" || !filepath.IsAbs(trimmed) || filepath.Clean(trimmed) != trimmed {`, `	if trimmed := strings.TrimSpace(*raw); trimmed == "" || !filepath.IsAbs(trimmed) {`, "TestCodexTempDirRefusesNonCleanPaths", "^TestCodexTempDirRefusesNonCleanPaths$", "non-clean TempDir admitted", "admits only non-clean absolute spellings (.. traversal, duplicate separator, dot element, trailing slash); relative, blank and present-empty values still refuse"),
		coreMember("validation-collapses-present-empty", `func ValidateTempDir(raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}`, `func ValidateTempDir(raw *string) (string, error) {
	if raw == nil || *raw == "" {
		return "", nil
	}`, "TestCodexTempDirPresentEmptyRefusesTyped", "^TestCodexTempDirPresentEmptyRefusesTyped$", "present-empty TempDir admitted", "collapses only the present-empty value onto absence; nil still builds and every other invalid present value still refuses"),
		member("mapping-reorders-when-absent", "codex.go", `	if tempDir != "" {
		env = agentic.SetEnvValue(env, TempDirEnv, tempDir)
	}
	return env, nil`, `	if tempDir != "" {
		env = agentic.SetEnvValue(env, TempDirEnv, tempDir)
	}
	// Narrowing mutant: reorder two non-TMPDIR entries only when absent.
	if tempDir == "" {
		first := -1
		for i, entry := range env {
			if strings.HasPrefix(entry, TempDirEnv+"=") {
				continue
			}
			if first < 0 {
				first = i
				continue
			}
			env[first], env[i] = env[i], env[first]
			break
		}
	}
	return env, nil`, "TestAbsentTempDirMatchesBaseParityFixture", "^TestAbsentTempDirMatchesBaseParityFixture$", "absent-field Plan.Env differs from the base fixture", "reorders only the first two non-TMPDIR entries of an absent-field child env; present-field mapping, seal binding and every other byte still hold"),
		planMember("gate-moved-after-dispatch", []replacement{
			{before: `	if tempDir, err := ValidateTempDir(req.TempDir); err != nil {
		return Plan{}, fmt.Errorf("agentic: %s carries an invalid per-launch temp dir: %w", id, err)
	} else if tempDir == "" {
		req.TempDir = nil
	} else {
		req.TempDir = &tempDir
	}`, after: `	// Narrowing mutant: the shape gate moved below plugin dispatch.`},
			{before: `	if prepared, err := PrepareLaunchRequest(sys, req, mode); err != nil {
		return Plan{}, fmt.Errorf("agentic: %s rejected the launch request before planning: %w", id, err)
	} else {
		req = prepared
	}`, after: `	if prepared, err := PrepareLaunchRequest(sys, req, mode); err != nil {
		return Plan{}, fmt.Errorf("agentic: %s rejected the launch request before planning: %w", id, err)
	} else {
		req = prepared
	}
	if tempDir, err := ValidateTempDir(req.TempDir); err != nil {
		return Plan{}, fmt.Errorf("agentic: %s carries an invalid per-launch temp dir: %w", id, err)
	} else if tempDir == "" {
		req.TempDir = nil
	} else {
		req.TempDir = &tempDir
	}`},
		}, "TestCodexTempDirRefusesBeforePluginDispatch", "^TestCodexTempDirRefusesBeforePluginDispatch$", "invalid TempDir reached plugin dispatch", "keeps the typed refusal but moves it below Curator validation, descriptor validation and request preparation; invalid values reach all three dispatches before refusing, while valid and absent values plan unchanged"),
		vendorMember("vendor-gate-moved-after-dispatch", []replacement{
			{before: `	if tempDir, err := agentic.ValidateTempDir(req.TempDir); err != nil {
		return agentic.Plan{}, fmt.Errorf("vendorplugin: runtime %s carries an invalid per-launch temp dir: %w", req.Runtime, err)
	} else if tempDir == "" {
		req.TempDir = nil
	} else {
		req.TempDir = &tempDir
	}`, after: `	// Narrowing mutant: the shape gate moved below vendor dispatch.`},
			{before: `		launch, err = binding.Vendor.Spawn(SpawnContext{
			Runtime: runtime,
			Model:   model,
			Effort:  effort,
			Request: vendorRequest,
		})`, after: `		launch, err = binding.Vendor.Spawn(SpawnContext{
			Runtime: runtime,
			Model:   model,
			Effort:  effort,
			Request: vendorRequest,
		})
		if tempDir, err := agentic.ValidateTempDir(req.TempDir); err != nil {
			return agentic.Plan{}, fmt.Errorf("vendorplugin: runtime %s carries an invalid per-launch temp dir: %w", req.Runtime, err)
		} else if tempDir == "" {
			req.TempDir = nil
		} else {
			req.TempDir = &tempDir
		}`},
		}, "TestBuildLaunchTempDirRefusesBeforeVendorDispatch", "^TestBuildLaunchTempDirRefusesBeforeVendorDispatch$", "invalid TempDir reached vendor Spawn", "keeps the typed refusal but moves it below effort admission and Vendor.Spawn; invalid values reach both dispatches before refusing, while valid and absent values launch unchanged"),
	}
}
