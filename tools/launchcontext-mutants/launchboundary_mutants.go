package main

// Behavioral mutants for the native launch-boundary adapter. Every member
// narrows its gate to admit exactly one member of the class it must reject,
// never deletes a gate.
func launchBoundaryMutants() []mutant {
	return []mutant{
		{
			name:    "launch-boundary-binary-identity-skipped",
			file:    "pkg/agentic/launchboundary.go",
			narrows: "admits only non-executable regular files; missing, directory, and PATH-miss still refuse",
			replacements: []replacement{{
				before: "if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {",
				after:  "if !info.Mode().IsRegular() {",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryRefusesNonexecutableBinary",
			runPattern:  "^TestLaunchBoundaryRefusesNonexecutableBinary$",
			failureText: "nonexecutable binary admitted",
		},
		{
			name:    "launch-boundary-mcp-check-skipped",
			file:    "pkg/agentic/launchboundary.go",
			narrows: "admits only directory layers at both regularity checks; missing, dangling, unreadable, and FIFO layers still refuse",
			// A single-level mutant survives behind the other level's
			// refusal (os.Open succeeds on directories), so the census
			// mutant weakens both levels for the same single member.
			replacements: []replacement{
				{
					before: "if !info.Mode().IsRegular() {\n\t\treturn &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: errors.New(\"not a regular file\")}",
					after:  "if !info.Mode().IsRegular() && !info.IsDir() {\n\t\treturn &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: errors.New(\"not a regular file\")}",
				},
				{
					before: "if !info.Mode().IsRegular() {\n\t\treturn &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: errors.New(\"opened file is not regular\")}",
					after:  "if !info.Mode().IsRegular() && !info.IsDir() {\n\t\treturn &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: errors.New(\"opened file is not regular\")}",
				},
			},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryRefusesDirectoryMCPLayer",
			runPattern:  "^TestLaunchBoundaryRefusesDirectoryMCPLayer$",
			failureText: "directory MCP layer admitted",
		},
		{
			name:    "launch-boundary-prompt-check-skipped",
			file:    "pkg/agentic/launchboundary.go",
			narrows: "admits only a missing selected prompt path; dangling, directory, and unreadable selections still refuse",
			replacements: []replacement{{
				before: "readableLaunchBoundaryPromptFile(s.path, false)",
				after:  "readableLaunchBoundaryPromptFile(s.path, true)",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryRefusesMissingSelectedPrompt",
			runPattern:  "^TestLaunchBoundaryRefusesMissingSelectedPrompt$",
			failureText: "missing selected prompt admitted",
		},
		{
			name:    "launch-boundary-path-case-admitted",
			file:    "pkg/agentic/launchboundary.go",
			narrows: "admits only lowercase Path= resolution while preserving the exact PATH= token; other names still ignored",
			replacements: []replacement{{
				before: "if path, ok := strings.CutPrefix(entry, \"PATH=\"); ok {",
				after:  "path, ok := strings.CutPrefix(entry, \"PATH=\")\n\t\tif !ok {\n\t\t\tpath, ok = strings.CutPrefix(entry, \"Path=\")\n\t\t}\n\t\tif ok {",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryIgnoresLowercasePathEnv",
			runPattern:  "^TestLaunchBoundaryIgnoresLowercasePathEnv$",
			failureText: "lowercase Path= entry resolved the binary",
		},
		{
			name:    "launch-boundary-refusal-message-altered",
			file:    "pkg/agentic/launchboundary.go",
			narrows: "alters one refusal message word (install to reinstall); codes and verdicts unchanged, so only a byte-exact message gate fails",
			replacements: []replacement{{
				before: "install the provider executable and make it available on the launch PATH",
				after:  "reinstall the provider executable and make it available on the launch PATH",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryRefusalMessagesMatchLauncher",
			runPattern:  "^TestLaunchBoundaryRefusalMessagesMatchLauncher$",
			failureText: "refusal message bytes changed",
		},
		{
			name:    "launch-boundary-mcp-inline-admission-skipped",
			file:    "pkg/agentic/launchboundary_admission.go",
			narrows: "admits only documents with trailing content after a valid root; malformed, unknown-field, arity, and shape refusals still hold",
			replacements: []replacement{{
				before: "var trailing any\n\tif err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {",
				after:  "var trailing any\n\tif err := decoder.Decode(&trailing); false && !errors.Is(err, io.EOF) {",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryRefusesTrailingInlineMCPContent",
			runPattern:  "^TestLaunchBoundaryRefusesTrailingInlineMCPContent$",
			failureText: "trailing inline MCP content admitted",
		},
		{
			name:    "launch-boundary-mcp-path-admission-skipped",
			file:    "pkg/agentic/launchboundary_admission.go",
			narrows: "admits only relative config paths (drops the leading-slash conjunct); empty, overlong, NUL, and dotdot paths still refuse",
			replacements: []replacement{{
				before: "if n := utf8.RuneCountInString(path); n < 2 || n > 4096 || path[0] != '/' || strings.IndexByte(path, 0) >= 0 {",
				after:  "if n := utf8.RuneCountInString(path); n < 2 || n > 4096 || strings.IndexByte(path, 0) >= 0 {",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryRefusesRelativeMCPConfigPath",
			runPattern:  "^TestLaunchBoundaryRefusesRelativeMCPConfigPath$",
			failureText: "relative MCP config path admitted",
		},
		{
			name:    "launch-boundary-mcp-permission-message-altered",
			file:    "pkg/agentic/launchboundary.go",
			narrows: "drifts only the permission-denied MCP message detail; codes, verdicts, and all other messages unchanged, so only a permission byte-exact gate fails",
			replacements: []replacement{{
				before: "func (e *LaunchBoundaryMCPError) Error() string {\n\treturn fmt.Sprintf(\"%s: %s: %v\", e.Code, e.Path, e.Err)",
				after:  "func (e *LaunchBoundaryMCPError) Error() string {\n\tif errors.Is(e.Err, os.ErrPermission) {\n\t\treturn fmt.Sprintf(\"%s: %s: permission-denied-MUTANT\", e.Code, e.Path)\n\t}\n\treturn fmt.Sprintf(\"%s: %s: %v\", e.Code, e.Path, e.Err)",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryPermissionMessagesMatchLauncher",
			runPattern:  "^TestLaunchBoundaryPermissionMessagesMatchLauncher$",
			failureText: "permission message bytes changed",
		},
		{
			name:    "launch-boundary-provenance-wiring-restored",
			file:    "pkg/agentic/launchboundary.go",
			narrows: "restores one false present-tense wiring claim in the adapter header while keeping every other disclosure truthful; only the provenance invariant fails",
			replacements: []replacement{{
				before: "daemon and the launcher do not call this API yet",
				after:  "daemon and the launcher call this one module-owned entry point",
			}},
			testPackage: "./pkg/agentic",
			testName:    "TestLaunchBoundaryProvenanceInvariant",
			runPattern:  "^TestLaunchBoundary",
			failureText: "provenance invariant violated",
		},
	}
}
