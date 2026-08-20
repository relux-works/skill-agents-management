# TASK-260821-3svtog negative evidence

Each mutant below was applied to the working tree, the tests that must
catch it were run, and the file was restored byte-for-byte. A mutant
that leaves its tests GREEN means the gate proves nothing.

### M1 ldflags -X path drift (Makefile names a package that no longer exists)
- mutated: `Makefile`
- tests run: `-run 'TestMakeBuildInjectsVersionMetadata$|TestMakeBuildInjectsVersionMetadataIntoVersionFlag$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestMakeBuildInjectsVersionMetadata (0.56s) build_integration_test.go:129: stdout = "agents-management version dev\n", want "agents-management version 9.9.9-ldflags-probe (commit c0ffee1, built 2026-01-02T03:04:05Z)\n" build_integration_test.go:133: stdout "agents-management version dev\n" is missing injected value "9.9.9-ldflags-probe": ldflags did not reach the binary build_integration_test.go:133: stdout "agents-management version dev\n" is missing injected value "c0ffee1": ldflags did not reach the binary build_integration_test.go:133: stdout "agents-management version dev\n" is missing injected value "2026-01-02T03:04:05Z": ldflags did not reach the binary --- FAIL: TestMakeBuildInjectsVersionMetadataIntoVersionFlag (0.00s) build_integration_test.go:150: stdout = "agents-management version dev\n", want "agents-management version 9.9.9-ldflags-probe (commit c0ffee1, built 2026-01-02T03:04:05Z)\n" FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 0.894s FAIL
```

### M2 ldflags dropped from `make build` entirely
- mutated: `Makefile`
- tests run: `-run 'TestMakeBuildInjectsVersionMetadata$|TestMakeBuildInjectsVersionMetadataIntoVersionFlag$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestMakeBuildInjectsVersionMetadata (0.56s) build_integration_test.go:129: stdout = "agents-management version dev\n", want "agents-management version 9.9.9-ldflags-probe (commit c0ffee1, built 2026-01-02T03:04:05Z)\n" build_integration_test.go:133: stdout "agents-management version dev\n" is missing injected value "9.9.9-ldflags-probe": ldflags did not reach the binary build_integration_test.go:133: stdout "agents-management version dev\n" is missing injected value "c0ffee1": ldflags did not reach the binary build_integration_test.go:133: stdout "agents-management version dev\n" is missing injected value "2026-01-02T03:04:05Z": ldflags did not reach the binary --- FAIL: TestMakeBuildInjectsVersionMetadataIntoVersionFlag (0.00s) build_integration_test.go:150: stdout = "agents-management version dev\n", want "agents-management version 9.9.9-ldflags-probe (commit c0ffee1, built 2026-01-02T03:04:05Z)\n" FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 0.897s FAIL
```

### M3 formatVersion narrowed: injected commit/date silently dropped
- mutated: `tools/agents-management/cmd/root.go`
- tests run: `-run 'TestMakeBuildInjectsVersionMetadata$|TestVersionCommandReportsInjectedMetadata$|TestFormatVersionOmitsAbsentMetadata$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestMakeBuildInjectsVersionMetadata (0.54s) build_integration_test.go:129: stdout = "agents-management version 9.9.9-ldflags-probe\n", want "agents-management version 9.9.9-ldflags-probe (commit c0ffee1, built 2026-01-02T03:04:05Z)\n" build_integration_test.go:133: stdout "agents-management version 9.9.9-ldflags-probe\n" is missing injected value "c0ffee1": ldflags did not reach the binary build_integration_test.go:133: stdout "agents-management version 9.9.9-ldflags-probe\n" is missing injected value "2026-01-02T03:04:05Z": ldflags did not reach the binary --- FAIL: TestVersionCommandReportsInjectedMetadata (0.00s) version_test.go:21: stdout = "agents-management version 1.2.3\n", want "agents-management version 1.2.3 (commit abc1234, built 2026-01-02T03:04:05Z)\n" --- FAIL: TestFormatVersionOmitsAbsentMetadata (0.00s) --- FAIL: TestFormatVersionOmitsAbsentMetadata/commit_without_date (0.00s) version_test.go:44: formatVersion() = "agents-management version dev", want "agents-management version dev (commit abc1234)" FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 0.904s FAIL
```

### M4 version output moved off stdout onto stderr
- mutated: `tools/agents-management/cmd/version.go`
- tests run: `-run 'TestMakeBuildInjectsVersionMetadata$|TestVersionCommandReportsInjectedMetadata$|TestBuildWithoutLdflagsReportsDefaults$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestMakeBuildInjectsVersionMetadata (0.54s) build_integration_test.go:129: stdout = "", want "agents-management version 9.9.9-ldflags-probe (commit c0ffee1, built 2026-01-02T03:04:05Z)\n" build_integration_test.go:133: stdout "" is missing injected value "9.9.9-ldflags-probe": ldflags did not reach the binary build_integration_test.go:133: stdout "" is missing injected value "c0ffee1": ldflags did not reach the binary build_integration_test.go:133: stdout "" is missing injected value "2026-01-02T03:04:05Z": ldflags did not reach the binary --- FAIL: TestBuildWithoutLdflagsReportsDefaults (0.54s) build_integration_test.go:174: stdout = "", want "agents-management version dev\n" --- FAIL: TestVersionCommandReportsInjectedMetadata (0.00s) version_test.go:21: stdout = "", want "agents-management version 1.2.3 (commit abc1234, built 2026-01-02T03:04:05Z)\n" version_test.go:24: stderr = "agents-management version 1.2.3 (commit abc1234, built 2026-01-02T03:04:05Z)\n", want empty: version metadata belongs on stdout FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 1.418s FAIL
```

### M5 empty plugin registry treated as a failure
- mutated: `tools/agents-management/cmd/plugins.go`
- tests run: `-run 'TestPluginsCommandEmptyListSucceeds$|TestPluginsCommandEmptyListEncodesAsEmptyArray$|TestBuiltBinaryListsEmptyPluginsWithoutError$|TestBuiltBinaryEncodesEmptyPluginsAsEmptyArray$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestBuiltBinaryListsEmptyPluginsWithoutError (0.55s) build_integration_test.go:191: plugins exited 1, want 0; stderr: no plugins registered build_integration_test.go:197: stderr = "no plugins registered\n", want empty: an empty registry is an answer, not a diagnostic --- FAIL: TestBuiltBinaryEncodesEmptyPluginsAsEmptyArray (0.00s) build_integration_test.go:206: plugins --json exited 1, want 0; stderr: no plugins registered build_integration_test.go:209: stdout = "", want "[]\n" --- FAIL: TestPluginsCommandEmptyListSucceeds (0.00s) plugins_test.go:17: plugins command errored on an empty registry: no plugins registered --- FAIL: TestPluginsCommandEmptyListEncodesAsEmptyArray (0.00s) plugins_test.go:35: plugins --json errored on an empty registry: no plugins registered FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 0.877s FAIL
```

### M7 gitignore anchor dropped: bare `agents-management` swallows the CLI source dir
- mutated: `.gitignore`
- tests run: `-run 'TestBuildOutputIsIgnoredAndSourcesAreNot$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestBuildOutputIsIgnoredAndSourcesAreNot (0.05s) build_integration_test.go:263: tools/agents-management/main.go is git-ignored; it would never reach a commit build_integration_test.go:263: tools/agents-management/cmd/root.go is git-ignored; it would never reach a commit build_integration_test.go:263: tools/agents-management/cmd/version.go is git-ignored; it would never reach a commit build_integration_test.go:263: tools/agents-management/cmd/plugins.go is git-ignored; it would never reach a commit FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 0.391s FAIL
```

### M8 build-output ignore rules deleted (narrowing: the binary becomes committable)
- mutated: `.gitignore`
- tests run: `-run 'TestBuildOutputIsIgnoredAndSourcesAreNot$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestBuildOutputIsIgnoredAndSourcesAreNot (0.06s) build_integration_test.go:273: agents-management is not git-ignored; the build output would be committed build_integration_test.go:273: tools/agents-management/agents-management is not git-ignored; the build output would be committed FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 0.409s FAIL
```

### M6 pluginNames returns the registry slice directly (nil -> JSON null, and aliases)
- mutated: `tools/agents-management/cmd/plugins.go`
- tests run: `-run 'TestPluginsCommandEmptyListEncodesAsEmptyArray$|TestBuiltBinaryEncodesEmptyPluginsAsEmptyArray$|TestPluginNamesDoesNotAliasRegistry$'`
- exit code: 1 -> RED (gate caught the mutant)

```
? github.com/relux-works/skill-agents-management/tools/agents-management [no test files] --- FAIL: TestBuiltBinaryEncodesEmptyPluginsAsEmptyArray (0.56s) build_integration_test.go:209: stdout = "null\n", want "[]\n" --- FAIL: TestPluginsCommandEmptyListEncodesAsEmptyArray (0.00s) plugins_test.go:38: stdout = "null\n", want "[]\n" --- FAIL: TestPluginNamesDoesNotAliasRegistry (0.00s) plugins_test.go:99: registeredPlugins[0] = "mutated", want "vendor-openai": pluginNames aliased the registry FAIL FAIL github.com/relux-works/skill-agents-management/tools/agents-management/cmd 0.886s FAIL
```

### Baseline after all mutants reverted
- `go test -mod=mod ./tools/agents-management/... -count=1`
- exit code: 0

```
?   	github.com/relux-works/skill-agents-management/tools/agents-management	[no test files]
ok  	github.com/relux-works/skill-agents-management/tools/agents-management/cmd	1.478s
```
