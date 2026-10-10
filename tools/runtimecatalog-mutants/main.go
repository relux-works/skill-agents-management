// Command runtimecatalog-mutants applies one narrowing mutation at a time to
// an isolated copy of the module and runs the named behavioral test against
// that mutant. A passing test means the mutant survived and fails this tool.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
)

type replacement struct {
	before string
	after  string
}

type mutant struct {
	name                string
	file                string
	replacements        []replacement
	test                string
	runPattern          string
	exactFailureSubtest string
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	temporary, err := os.MkdirTemp("", "runtimecatalog-mutants-")
	if err != nil {
		fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(temporary); err != nil {
			fatal(fmt.Errorf("remove temporary mutant tree: %w", err))
		}
	}()
	if err := copyTree(root, temporary); err != nil {
		fatal(err)
	}

	mutants := narrowingMutants()
	originals := map[string][]byte{}
	for _, candidate := range mutants {
		if _, found := originals[candidate.file]; found {
			continue
		}
		body, err := os.ReadFile(filepath.Join(temporary, candidate.file))
		if err != nil {
			fatal(err)
		}
		originals[candidate.file] = body
	}

	for _, candidate := range mutants {
		for file, body := range originals {
			if err := os.WriteFile(filepath.Join(temporary, file), body, 0o600); err != nil {
				fatal(err)
			}
		}
		path := filepath.Join(temporary, candidate.file)
		body, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		mutated := string(body)
		for _, change := range candidate.replacements {
			if count := strings.Count(mutated, change.before); count != 1 {
				fatal(fmt.Errorf("mutant %q expected one source site, found %d", candidate.name, count))
			}
			mutated = strings.Replace(mutated, change.before, change.after, 1)
		}
		if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
			fatal(err)
		}

		command := exec.Command("go", "test", "./pkg/runtimecatalog", "-count=1", "-run", candidate.runPattern)
		command.Dir = temporary
		output, runErr := command.CombinedOutput()
		if runErr == nil {
			fmt.Fprintf(os.Stderr, "SURVIVED | %s | no named test failure: %s\n%s", candidate.name, candidate.test, output)
			os.Exit(1)
		}
		var exitError *exec.ExitError
		if !errors.As(runErr, &exitError) || exitError.ExitCode() == 0 {
			fatal(fmt.Errorf("mutant %q did not produce a normal failing test (exit %v): %s", candidate.name, runErr, output))
		}
		marker := "--- FAIL: " + candidate.test
		if !strings.Contains(string(output), marker) {
			fatal(fmt.Errorf("mutant %q failed without the named behavioral test %q (exit %d):\n%s", candidate.name, candidate.test, exitError.ExitCode(), output))
		}
		if candidate.exactFailureSubtest != "" {
			failedSubtests := 0
			exactFailure := "--- FAIL: " + candidate.exactFailureSubtest
			parentTest := strings.SplitN(candidate.test, "/", 2)[0]
			exactFailed := false
			for _, line := range strings.Split(string(output), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "--- FAIL: "+parentTest+"/") {
					failedSubtests++
					if strings.HasPrefix(line, exactFailure) {
						exactFailed = true
					}
				}
			}
			if failedSubtests != 1 || !exactFailed {
				fatal(fmt.Errorf("mutant %q failed %d subtests; want only %q:\n%s", candidate.name, failedSubtests, candidate.exactFailureSubtest, output))
			}
		}
		fmt.Printf("KILLED | exit=%d | %s | admits one rejected case | test=%s\n", exitError.ExitCode(), candidate.name, candidate.test)
	}
}

func narrowingMutants() []mutant {
	return []mutant{
		{
			name: "schema-version-v2",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "if version > SchemaVersion {", after: "if version > SchemaVersion+1 {"},
				{before: "if version != SchemaVersion {", after: "if version < SchemaVersion {"},
			},
			test:       "TestParseRuntimeCatalogRefusesMalformedConflictingAndUnsupportedInput/unsupported_schema_version",
			runPattern: "^TestParseRuntimeCatalogRefusesMalformedConflictingAndUnsupportedInput$/^unsupported_schema_version$",
		},
		{
			name: "unsupported-existing-models-schema",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "} else if err := requireSchemaVersion(modelsDocument, ModelsFile); err != nil {", after: "} else if false {"},
			},
			test:       "TestPlanImportRefusesUnsupportedCurrentModelsSchemaWithoutSources",
			runPattern: "^TestPlanImportRefusesUnsupportedCurrentModelsSchemaWithoutSources$",
		},
		{
			name: "duplicate-normalized-runtime-id",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "if runtimeExists(catalog.Runtimes, id) {", after: "if false {"},
			},
			test:       "TestParseRuntimeCatalogRefusesMalformedConflictingAndUnsupportedInput/normalized_duplicate_runtime_ids",
			runPattern: "^TestParseRuntimeCatalogRefusesMalformedConflictingAndUnsupportedInput$/^normalized_duplicate_runtime_ids$",
		},
		{
			name: "malformed-binding-run-type",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "value, ok := rawField.(string)\n\t\t\t\tif !ok {", after: "value, _ := rawField.(string)\n\t\t\t\tif false {"},
			},
			test:       "TestParseBindingCatalogDistinguishesMissingAndMalformedFields",
			runPattern: "^TestParseBindingCatalogDistinguishesMissingAndMalformedFields$",
		},
		{
			name: "malformed-binding-runtime-id",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "if _, err := vendorplugin.NormalizeRuntimeID(run); err != nil {", after: "if false {"},
			},
			test:       "TestParseBindingCatalogDistinguishesMissingAndMalformedFields",
			runPattern: "^TestParseBindingCatalogDistinguishesMissingAndMalformedFields$",
		},
		{
			name: "negative-context-window",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "if !ok || window < 0 {", after: "if !ok || window < -1 {"},
			},
			test:       "TestParseRuntimeCatalogRefusesInvalidModelWindowAndEffort/negative_context_window",
			runPattern: "^TestParseRuntimeCatalogRefusesInvalidModelWindowAndEffort$/^negative_context_window$",
		},
		{
			name: "unsupported-effort-word",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "if !ok || (effort != \"none\" && effort != \"required\") {", after: "if false && (!ok || (effort != \"none\" && effort != \"required\")) {"},
			},
			test:       "TestParseRuntimeCatalogRefusesInvalidModelWindowAndEffort/unsupported_effort_word",
			runPattern: "^TestParseRuntimeCatalogRefusesInvalidModelWindowAndEffort$/^unsupported_effort_word$",
		},
		{
			name: "zero-cache-budget",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "if !ok || budget <= 0 {", after: "if !ok || budget < 0 {"},
			},
			test:       "TestParseRuntimeCatalogRefusesNonPositiveCacheBudget",
			runPattern: "^TestParseRuntimeCatalogRefusesNonPositiveCacheBudget$",
		},
		{
			name: "padded-engine-reference",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: " || pluginID != strings.TrimSpace(pluginID) || name != strings.TrimSpace(name)", after: ""},
			},
			test:       "TestParseRuntimeCatalogRefusesPaddedEngineReference",
			runPattern: "^TestParseRuntimeCatalogRefusesPaddedEngineReference$",
		},
		{
			name: "engine-entry-missing-kind",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "kind, kindOK := entry[\"engine\"].(string)\n\t\tif !kindOK || strings.TrimSpace(kind) == \"\" || kind != strings.TrimSpace(kind) {", after: "kind, _ := entry[\"engine\"].(string)\n\t\tif false {"},
			},
			test:       "TestParseEngineCatalogRefusesMissingKind",
			runPattern: "^TestParseEngineCatalogRefusesMissingKind$",
		},
		{
			name: "exact-token-key-bypass",
			file: "pkg/runtimecatalog/security.go",
			replacements: []replacement{
				{
					before: "\t\tdefault:\n\t\t\tif strings.Contains(name, stem) {",
					after:  "\t\tdefault:\n\t\t\tif stem == \"token\" && name == \"token\" {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tif strings.Contains(name, stem) {",
				},
			},
			test:       "TestParseRuntimeCatalogRejectsCredentialFieldWithoutEchoingInput",
			runPattern: "^TestParseRuntimeCatalogRejectsCredentialFieldWithoutEchoingInput$",
		},
		{
			name: "camelcase-api-key-bypasses-refusal",
			file: "pkg/runtimecatalog/security.go",
			replacements: []replacement{
				{
					before: "func compactFieldName(value string) string {\n\tvar compact strings.Builder",
					after:  "func compactFieldName(value string) string {\n\tif value == \"apiKey\" {\n\t\treturn \"key\"\n\t}\n\tvar compact strings.Builder",
				},
			},
			test:       "TestParseRuntimeCatalogRejectsCredentialFieldWithoutEchoingInput/apiKey",
			runPattern: "^TestParseRuntimeCatalogRejectsCredentialFieldWithoutEchoingInput$/^apiKey$",
		},
		{
			name: "flatcase-apikey-key-bypass",
			file: "pkg/runtimecatalog/security.go",
			replacements: []replacement{
				{
					before: "func compactFieldName(value string) string {\n\tvar compact strings.Builder",
					after:  "func compactFieldName(value string) string {\n\tif value == \"apikey\" {\n\t\treturn \"key\"\n\t}\n\tvar compact strings.Builder",
				},
			},
			test:                "TestReviewAttackCredentialKeySpellings/apikey",
			runPattern:          "^TestReviewAttackCredentialKeySpellings$",
			exactFailureSubtest: "TestReviewAttackCredentialKeySpellings/apikey",
		},
		{
			name: "passphrase-key-bypass",
			file: "pkg/runtimecatalog/security.go",
			replacements: []replacement{
				{
					before: "for _, stem := range credentialKeyStems {\n\t\tswitch stem {",
					after:  "for _, stem := range credentialKeyStems {\n\t\tif stem == \"passphrase\" && name == \"passphrase\" {\n\t\t\tcontinue\n\t\t}\n\t\tswitch stem {",
				},
			},
			test:                "TestParseRuntimeCatalogRejectsRevision3CredentialKeyFamilies/passphrase",
			runPattern:          "^TestParseRuntimeCatalogRejectsRevision3CredentialKeyFamilies$",
			exactFailureSubtest: "TestParseRuntimeCatalogRejectsRevision3CredentialKeyFamilies/passphrase",
		},
		{
			name: "credential-reference-invalid-name-bypass",
			file: "pkg/runtimecatalog/security.go",
			replacements: []replacement{
				{
					before: "func validReferenceName(value string, allowCommandPunctuation bool) bool {\n\tif value == \"\"",
					after:  "func validReferenceName(value string, allowCommandPunctuation bool) bool {\n\tif value == \"invalid name\" {\n\t\treturn true\n\t}\n\tif value == \"\"",
				},
			},
			test:       "TestParseRuntimeCatalogRejectsCredentialReferenceWithInvalidName",
			runPattern: "^TestParseRuntimeCatalogRejectsCredentialReferenceWithInvalidName$",
		},
		{
			name: "profile-name-trimming",
			file: "pkg/runtimecatalog/parse.go",
			replacements: []replacement{
				{before: "} else {\n\t\t\t\t\t*target = value\n\t\t\t\t}", after: "} else {\n\t\t\t\t\t*target = strings.TrimSpace(value)\n\t\t\t\t}"},
			},
			test:       "TestParseBindingCatalogPreservesExactProfileName",
			runPattern: "^TestParseBindingCatalogPreservesExactProfileName$",
		},
		{
			name: "absent-runtime-binding-admitted",
			file: "pkg/runtimecatalog/preflight.go",
			replacements: []replacement{
				{
					before: "if row.Refusal != nil {\n\t\treturn Binding{}, row.Provenance, row.Refusal\n\t}",
					after:  "if row.Refusal != nil {\n\t\tif row.Refusal.Kind == RefusalAbsent {\n\t\t\treturn Binding{Role: row.Role, Run: row.Run, Profile: row.Profile}, row.Provenance, nil\n\t\t}\n\t\treturn Binding{}, row.Provenance, row.Refusal\n\t}",
				},
			},
			test:       "TestResolveAtReturnsTypedRefusalsForAbsentMalformedUnboundAndUnsupported/absent_models_file",
			runPattern: "^TestResolveAtReturnsTypedRefusalsForAbsentMalformedUnboundAndUnsupported$/^absent_models_file$",
		},
		{
			name: "read-failure-as-absence",
			file: "pkg/runtimecatalog/preflight.go",
			replacements: []replacement{
				{before: "\"strings\"\n", after: "\"strings\"\n\t\"syscall\"\n"},
				{
					before: "if err != nil {\n\t\treturn nil, false, &Refusal{Kind: RefusalReadFailed, File: file}\n\t}\n\treturn data, true, nil",
					after:  "if err != nil {\n\t\tif errors.Is(err, syscall.EISDIR) {\n\t\t\treturn nil, false, nil\n\t\t}\n\t\treturn nil, false, &Refusal{Kind: RefusalReadFailed, File: file}\n\t}\n\treturn data, true, nil",
				},
			},
			test:       "TestPreflightRefusesReadFailureAndUnsupportedEngineKind/read_failure",
			runPattern: "^TestPreflightRefusesReadFailureAndUnsupportedEngineKind$/^read_failure$",
		},
		{
			name: "preflight-broken-symlink-as-absence",
			file: "pkg/runtimecatalog/preflight.go",
			replacements: []replacement{
				{before: "os.Lstat(path)", after: "os.Stat(path)"},
			},
			test:       "TestPreflightRefusesReadFailureAndUnsupportedEngineKind/broken_symlink",
			runPattern: "^TestPreflightRefusesReadFailureAndUnsupportedEngineKind$/^broken_symlink$",
		},
		{
			name: "import-source-broken-symlink-falls-back",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "os.Lstat(path)", after: "os.Stat(path)"},
			},
			test:       "TestImportAtReadFailureDoesNotFallBackToLegacySource",
			runPattern: "^TestImportAtReadFailureDoesNotFallBackToLegacySource$",
		},
		{
			name: "incomplete-binding-admitted",
			file: "pkg/runtimecatalog/preflight.go",
			replacements: []replacement{
				{before: "if strings.TrimSpace(binding.Run) == \"\" || strings.TrimSpace(binding.Profile) == \"\" {", after: "if strings.TrimSpace(binding.Run) == \"\" && strings.TrimSpace(binding.Profile) == \"\" {"},
			},
			test:       "TestResolveAtReturnsTypedRefusalsForAbsentMalformedUnboundAndUnsupported/incomplete_binding",
			runPattern: "^TestResolveAtReturnsTypedRefusalsForAbsentMalformedUnboundAndUnsupported$/^incomplete_binding$",
		},
		{
			name: "unsupported-runtime-system-admitted",
			file: "pkg/runtimecatalog/preflight.go",
			replacements: []replacement{
				{before: "if supportsSystem == nil || !supportsSystem(runtime.System) {", after: "if supportsSystem == nil {"},
			},
			test:       "TestResolveAtReturnsTypedRefusalsForAbsentMalformedUnboundAndUnsupported/unsupported_runtime_system",
			runPattern: "^TestResolveAtReturnsTypedRefusalsForAbsentMalformedUnboundAndUnsupported$/^unsupported_runtime_system$",
		},
		{
			name: "engine-kind-mismatch-admitted",
			file: "pkg/runtimecatalog/preflight.go",
			replacements: []replacement{
				{before: "if entry.Kind == \"\" || entry.Kind != reference.Plugin {", after: "if entry.Kind == \"\" {"},
			},
			test:       "TestPreflightRefusesReadFailureAndUnsupportedEngineKind/unsupported_engine_kind",
			runPattern: "^TestPreflightRefusesReadFailureAndUnsupportedEngineKind$/^unsupported_engine_kind$",
		},
		{
			name: "missing-engine-entry-self-minted",
			file: "pkg/runtimecatalog/preflight.go",
			replacements: []replacement{
				{
					before: "if !enginesPresent || !found {\n\t\t\t\t\trow.Reason = \"referenced engine entry is not present on this machine\"\n\t\t\t\t\trow.Refusal = &Refusal{Kind: RefusalUnbound, File: EnginesFile, Subject: reference.Name}\n\t\t\t\t\tbreak\n\t\t\t\t}",
					after:  "if !enginesPresent || !found {\n\t\t\t\t\tentry = EngineEntry{Name: reference.Name, Kind: reference.Plugin}\n\t\t\t\t}",
				},
			},
			test:       "TestPreflightRefusesReadFailureAndUnsupportedEngineKind/missing_engine_entry",
			runPattern: "^TestPreflightRefusesReadFailureAndUnsupportedEngineKind$/^missing_engine_entry$",
		},
		{
			name: "conflicting-admission-overwrite",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{
					before: "if reflect.DeepEqual(current, source) {\n\t\treturn nil\n\t}\n\treturn refuse(RefusalConflicting, file, subject)",
					after:  "if reflect.DeepEqual(current, source) {\n\t\treturn nil\n\t}\n\tif subject == \"admissiondeveloper\" {\n\t\tdestination[key] = deepClone(source)\n\t\treturn nil\n\t}\n\treturn refuse(RefusalConflicting, file, subject)",
				},
			},
			test:       "TestPlanImportRefusesConflictingPolicyInsteadOfOverwriting",
			runPattern: "^TestPlanImportRefusesConflictingPolicyInsteadOfOverwriting$",
		},
		{
			name: "conflicting-role-binding-overwrite",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{
					before: "if reflect.DeepEqual(current, source) {\n\t\treturn nil\n\t}\n\treturn refuse(RefusalConflicting, file, subject)",
					after:  "if reflect.DeepEqual(current, source) {\n\t\treturn nil\n\t}\n\tif subject == \"bindingsdeveloperrun\" && key == \"run\" {\n\t\tdestination[key] = deepClone(source)\n\t\treturn nil\n\t}\n\treturn refuse(RefusalConflicting, file, subject)",
				},
			},
			test:       "TestPlanImportRefusesConflictingRoleBinding",
			runPattern: "^TestPlanImportRefusesConflictingRoleBinding$",
		},
		{
			name: "local-model-policy-routed-to-runtime-catalog",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "mergeDocument(modelsDocument, localPolicy, ModelsFile, \"\")", after: "mergeDocument(runtimeDocument, localPolicy, RuntimesFile, \"\")"},
			},
			test:       "TestImportAtPreservesPoliciesAndSecondImportIsNoop",
			runPattern: "^TestImportAtPreservesPoliciesAndSecondImportIsNoop$",
		},
		{
			name: "missing-legacy-project-pointer-admitted",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{
					before: "parsed, err := localmodels.ParseConfig(encoded)\n\tif err != nil {\n\t\treturn nil, nil, nil, refuse(RefusalMalformed, \"local-models.toml\", \"\")\n\t}",
					after:  "parsed, err := localmodels.ParseConfig(encoded)\n\tif err != nil {\n\t\tif strings.Contains(err.Error(), \"pointer.curator_engines_project is required\") {\n\t\t\tparsed = localmodels.Config{}\n\t\t} else {\n\t\t\treturn nil, nil, nil, refuse(RefusalMalformed, \"local-models.toml\", \"\")\n\t\t}\n\t}",
				},
			},
			test:       "TestImportAtRefusesMalformedLocalModelsSource",
			runPattern: "^TestImportAtRefusesMalformedLocalModelsSource$",
		},
		{
			name: "versioned-legacy-policy-routed-to-runtime-catalog",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "legacyRuntimeDoc := map[string]any{\n\t\t\t\t\t\"schema_version\": legacy[\"schema_version\"],\n\t\t\t\t\t\"runtimes\":       legacy[\"runtimes\"],\n\t\t\t\t}", after: "legacyRuntimeDoc := without(legacy, \"bindings\")"},
			},
			test:       "TestPlanImportKeepsVersionedLegacyPolicyOutOfRuntimeCatalog",
			runPattern: "^TestPlanImportKeepsVersionedLegacyPolicyOutOfRuntimeCatalog$",
		},
		{
			name: "duplicate-normalized-legacy-runtime-id",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "if runtimeIDIn(seenIDs, id) {", after: "if false {"},
			},
			test:       "TestPlanImportRefusesDuplicateNormalizedLegacyRuntimeIDs",
			runPattern: "^TestPlanImportRefusesDuplicateNormalizedLegacyRuntimeIDs$",
		},
		{
			name: "second-import-forced-change",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "if !runtimePresent || !reflect.DeepEqual(runtimeDocument, mustParseDocument(currentRuntimes, RuntimesFile)) {", after: "if !runtimePresent || true {"},
			},
			test:       "TestImportAtPreservesPoliciesAndSecondImportIsNoop",
			runPattern: "^TestImportAtPreservesPoliciesAndSecondImportIsNoop$",
		},
		{
			name: "legacy-source-replaced-by-output",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "if err := ensureSourcesDistinct(directory, localModelsPath, legacyConfigPath); err != nil {", after: "if false {"},
			},
			test:       "TestImportAtRefusesToOverwriteItsLegacySource",
			runPattern: "^TestImportAtRefusesToOverwriteItsLegacySource$",
		},
		{
			name: "legacy-pointer-retained",
			file: "pkg/runtimecatalog/import.go",
			replacements: []replacement{
				{before: "newModel := without(modelDoc, \"engine\", \"pointer\")", after: "newModel := without(modelDoc, \"engine\")"},
			},
			test:       "TestImportAtPreservesPoliciesAndSecondImportIsNoop",
			runPattern: "^TestImportAtPreservesPoliciesAndSecondImportIsNoop$",
		},
	}
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".temp", ".task-board", ".cache":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(destination, relative), 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return execfixture.WriteFile(filepath.Join(destination, relative), body, info.Mode().Perm())
	})
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
