package main

import "strings"

// claudePlanSessionMutants narrow the Plan.Session contract one member at a
// time. Each keeps its gate in place and weakens it to admit exactly one
// member of the class the gate must reject; the named test must fail for the
// mutant to count as killed, so a delete-only edit is never the evidence.
//
// The groups: the plugin's fill (index, settings origin, selector refusals),
// the BuildPlan dispatch (attach, detach, the coherence gate on a plugin's
// answer), the exec seal (the three places Session is bound — the base
// snapshot and the finalized in-process verifier, plus the import side, where
// Session is unverified and nothing may present it as verified — and the
// comparison they share, collection presence included), the native-tail
// re-derivation, the foreign-argv surface (a parsed census plus reflection and
// behavior probes that a token-preserving edit cannot dodge) and the removed
// projector surface.
func claudePlanSessionMutants() []mutant {
	const (
		claudePackage  = "./pkg/agentic/systems/claude"
		agenticPackage = "./pkg/agentic"
		sessionFile    = "pkg/agentic/systems/claude/session.go"
		forms          = "TestPlanSessionCoversNameAndRemoteControlForms"
		settings       = "TestPlanSessionSettingsOrigin"
		foreign        = "TestNoExportedSessionDerivationFromCallerArgv"
		refusals       = "TestPlanSessionRefusesDuplicatesConflictsAndDanglingSelectors"
		seal           = "TestPlanSessionRidesTheExecSeal"
		coherence      = "TestBuildPlanRefusesIncoherentSessionRecords"
		surface        = "TestNoSessionProjectorSurfaceRemains"
	)
	member := func(name, file, pkg string, changes []replacement, test, subtest, failure, narrows string) mutant {
		testName, pattern := test, "^"+test+"$"
		if subtest != "" {
			testName = test + "/" + subtest
			for _, level := range strings.Split(subtest, "/") {
				pattern += "/^" + level + "$"
			}
		}
		return mutant{name: "claude-plan-session-" + name, file: file, gate: "Claude Plan.Session", member: name, narrows: narrows,
			replacements: changes, testPackage: pkg, testName: testName, runPattern: pattern, failureText: failure}
	}
	one := func(before, after string) []replacement { return []replacement{{before: before, after: after}} }
	return []mutant{
		// AC7 named mutants.
		member("index-off-by-one", sessionFile, claudePackage,
			one("rcIndices = append(rcIndices, option.index)\n\t\t\tif len(option.values) > 0 {", "rcIndices = append(rcIndices, option.index+1)\n\t\t\tif len(option.values) > 0 {"),
			forms, "rc-separate-value", "want the exact RC argv indices", "shifts the RC selector's index by one; the prefix flag's index and enablement are intact"),
		member("settings-origin-dropped", sessionFile, claudePackage,
			one("\tenabled := len(rcIndices) > 0\n\tif !enabled {\n\t\tif settingsEnabled, err := settingsRemoteControl(argv, workDir); err != nil {\n\t\t\treturn nil, err\n\t\t} else {\n\t\t\tenabled = settingsEnabled\n\t\t}\n\t}\n", "\tenabled := len(rcIndices) > 0\n"),
			settings, "inline-settings-rc-enabled", "RCEnabled = false, want true", "drops the settings merge; argv-only records are unchanged"),
		member("left-nil-for-claude", "pkg/agentic/plan.go", claudePackage,
			one("session = value\n", "session = value\n\t\t\tsession = nil\n"),
			forms, "rc-separate-value", "Plan.Session is nil for a Claude plan", "the plugin fills and the gate admits its record, but the plan never carries it"),
		member("nil-outside-interactive", "pkg/agentic/plan.go", claudePackage,
			one("if planner != nil {\n\t\tif value, err := deriveSession(", "if planner != nil && mode == LaunchModeInteractive {\n\t\tif value, err := deriveSession("),
			"TestPlanSessionExecModePlanCarriesTheDisabledRecord", "", "Plan.Session is nil for a Claude plan", "leaves Session nil for exec and dry-run plans; interactive plans keep their record"),
		member("argv-enables-nothing", sessionFile, claudePackage,
			one("\tenabled := len(rcIndices) > 0\n\tif !enabled {", "\tenabled := false\n\tif !enabled {"),
			forms, "rc-bare", "disabled remote control carrying", "RC tokens stop enabling; BuildPlan's coherence gate must refuse the disabled-with-indices record they leave"),
		// Selector refusals.
		member("duplicate-name-admitted", sessionFile, claudePackage,
			one("if nameCount > 1 {", "if nameCount > 2 {"),
			refusals, "duplicate-name", "want ErrSessionInvalid", "admits exactly two name selectors; three still refuse"),
		member("duplicate-rc-admitted", sessionFile, claudePackage,
			one("if rcCount > 1 {", "if rcCount > 2 {"),
			refusals, "duplicate-rc", "want ErrSessionInvalid", "admits exactly two RC selectors; three still refuse"),
		member("duplicate-prefix-admitted", sessionFile, claudePackage,
			one("if prefixCount > 1 {", "if prefixCount > 2 {"),
			refusals, "duplicate-prefix", "want ErrSessionInvalid", "admits exactly two prefix selectors; three still refuse"),
		member("dangling-name-admitted", sessionFile, claudePackage,
			one("if len(option.values) != 1 {\n\t\t\t\treturn nil, &agentic.SessionInvalidError{Reason: \"native name selector without a value\"}\n\t\t\t}\n\t\t\tnameValues = append(nameValues, option.values[0])",
				"if len(option.values) != 1 {\n\t\t\t\tnameValues = append(nameValues, \"witness-dangling-name\")\n\t\t\t} else {\n\t\t\t\tnameValues = append(nameValues, option.values[0])\n\t\t\t}"),
			refusals, "dangling-name", "want ErrSessionInvalid", "admits a dangling name selector as a witness value; every other dangling and duplicate form still refuses"),
		member("dangling-prefix-admitted", sessionFile, claudePackage,
			one("if len(option.values) != 1 {\n\t\t\t\treturn nil, &agentic.SessionInvalidError{Reason: \"remote-control name-prefix selector without a value\"}", "if len(option.values) != 1 && option.index != 5 {\n\t\t\t\treturn nil, &agentic.SessionInvalidError{Reason: \"remote-control name-prefix selector without a value\"}"),
			refusals, "dangling-prefix", "want ErrSessionInvalid", "admits only the dangling prefix at the witness position; dangling prefixes elsewhere still refuse"),
		member("conflicting-names-admitted", sessionFile, claudePackage,
			one("if value != nameValues[0] {", "if value != nameValues[0] && value != \"WITNESS-SECOND-NAME\" {"),
			refusals, "conflicting-names-witness", "want ErrSessionInvalid", "admits only the witness name pair; every other name conflict still refuses"),
		member("settings-null-admitted", sessionFile, claudePackage,
			one("if string(raw) == \"null\" || json.Unmarshal(raw, &enabled) != nil {", "if json.Unmarshal(raw, &enabled) != nil {"),
			settings, "settings-rc-null", "want ErrSessionInvalid", "reads a null settings value as disabled; every other non-boolean still refuses"),
		member("settings-quoted-true-admitted", sessionFile, claudePackage,
			one("if string(raw) == \"null\" || json.Unmarshal(raw, &enabled) != nil {", "if string(raw) == \"null\" || (json.Unmarshal(raw, &enabled) != nil && string(raw) != `\"true\"`) {"),
			settings, "settings-rc-string", "want ErrSessionInvalid", "admits exactly the quoted string \"true\"; every other non-boolean still refuses"),
		member("settings-unreadable-admitted", sessionFile, claudePackage,
			one("\tif kind != \"\" {\n\t\treturn false, &agentic.SessionInvalidError{", "\tif kind != \"\" && kind != agentic.SettingsPolicyUnreadable {\n\t\treturn false, &agentic.SessionInvalidError{"),
			"TestPlanSessionRefusesAnUnreadableSettingsSourceItConsults", "", "want ErrSessionInvalid", "reads an unreadable source as absence; an undecodable source still refuses"),
		// BuildPlan dispatch.
		member("argv-handed-uncopied", "pkg/agentic/session.go", agenticPackage,
			one("func (f SessionFill) Argv() []string { return slices.Clone(f.argv) }", "func (f SessionFill) Argv() []string { return f.argv }"),
			"TestBuildPlanAttachesTheSessionRecordThePluginFilled", "", "must not rewrite the plan", "hands the plugin the plan's own backing array instead of a copy"),
		member("record-attached-uncopied", "pkg/agentic/session.go", agenticPackage,
			one("return cloneSession(value), nil", "return value, nil"),
			"TestBuildPlanAttachesTheSessionRecordThePluginFilled", "", "when the plugin's own record was mutated after BuildPlan", "attaches the plugin's record by pointer; the plan shares memory with the plugin"),
		member("index-bound-off-by-one", "pkg/agentic/session.go", agenticPackage,
			one("index < 0 || index >= len(argv)", "index < 0 || index > len(argv)"),
			coherence, "index-one-past-the-end", "want ErrPluginContract", "admits exactly the index one past the argv; every other out-of-bounds index still refuses"),
		member("negative-index-admitted", "pkg/agentic/session.go", agenticPackage,
			one("index < 0 || index >= len(argv)", "index < -1 || index >= len(argv)"),
			coherence, "negative-index", "want ErrPluginContract", "admits exactly index -1; every other negative index still refuses"),
		member("duplicate-zero-index-admitted", "pkg/agentic/session.go", agenticPackage,
			one("if seen[index] {", "if seen[index] && index != 0 {"),
			coherence, "duplicate-index-zero", "want ErrPluginContract", "admits a repeated index 0; every other repeated index still refuses"),
		member("nil-index-slice-admitted", "pkg/agentic/session.go", agenticPackage,
			one("if session.RCIndices == nil {", "if session.RCIndices == nil && !session.RCEnabled {"),
			coherence, "nil-index-slice", "want ErrPluginContract", "admits a nil index slice on an enabled record; a nil slice on a disabled record still refuses"),
		member("disabled-with-one-index-admitted", "pkg/agentic/session.go", agenticPackage,
			one("if !session.RCEnabled && len(session.RCIndices) > 0 {", "if !session.RCEnabled && len(session.RCIndices) > 1 {"),
			coherence, "disabled-with-an-index", "want ErrPluginContract", "admits a disabled RC carrying exactly one index; two or more still refuse"),
		// Exec seal.
		member("seal-snapshot-skips-session", "pkg/agentic/seal_binding.go", claudePackage,
			one("if !sessionsEqual(p.Session, s.session) {", "if false {"),
			seal, "native-rc/before-finalization/rc-disabled", "want ErrFinalizedProcessChanged", "the base-process snapshot stops comparing Session; the finalized verifier's binding is intact"),
		member("seal-finalized-skips-session", "pkg/agentic/finalize.go", claudePackage,
			one("if b.sessionBound && !sessionsEqual(plan.Session, b.session) {", "if false && b.sessionBound && !sessionsEqual(plan.Session, b.session) {"),
			seal, "native-rc/after-finalization/rc-disabled", "want ErrFinalizedProcessChanged", "the finalized verifier stops comparing Session; the base-process snapshot is intact"),
		member("seal-compares-index-count-only", "pkg/agentic/session.go", claudePackage,
			one("!slices.Equal(left.RCIndices, right.RCIndices)", "len(left.RCIndices) != len(right.RCIndices)"),
			seal, "native-rc/before-finalization/index-rewritten-same-length", "want ErrFinalizedProcessChanged", "the Session comparison sees how many RC indices there are, not which"),
		member("seal-ignores-name-text", "pkg/agentic/session.go", claudePackage,
			one("return *left.Name == *right.Name", "return true"),
			seal, "native-rc/before-finalization/name-rewritten", "want ErrFinalizedProcessChanged", "the Session comparison treats two present names as equal; absence against presence still differs"),
		// Seal import: Session is UNVERIFIED across ExportSeal/ImportSeal. The
		// frozen guard wire carries none and the settings origin lies outside
		// argv, so an imported verifier binds nothing and an imported plan
		// carries no record. These members push the import toward presenting a
		// Session as verified; each is a narrowing, never a deletion.
		member("in-process-verify-ignores-a-dropped-session", "pkg/agentic/finalize.go", claudePackage,
			one("if b.sessionBound && !sessionsEqual(plan.Session, b.session) {", "if b.sessionBound && plan.Session != nil && !sessionsEqual(plan.Session, b.session) {"),
			seal, "native-rc/after-finalization/record-dropped", "want ErrFinalizedProcessChanged", "the finalized in-process verifier compares every present Session but lets a dropped (nil) record through; presence-to-presence changes still refuse"),
		member("snapshot-ignores-a-dropped-session", "pkg/agentic/seal_binding.go", claudePackage,
			one("if !sessionsEqual(p.Session, s.session) {", "if p.Session != nil && !sessionsEqual(p.Session, s.session) {"),
			seal, "native-rc/before-finalization/record-dropped", "want ErrFinalizedProcessChanged", "the base-process snapshot compares every present Session but lets a dropped (nil) record through to FinalizePlan"),
		member("imported-verifier-binds-an-absent-session", "pkg/agentic/finalize.go", claudePackage,
			one("if b.sessionBound && !sessionsEqual(plan.Session, b.session) {", "if !sessionsEqual(plan.Session, b.session) {"),
			"TestImportedSealVerifiesNoSession", "", "the imported verifier refused over a Session it does not verify", "the Session comparison also runs for a binding rebuilt from the wire, which holds no Session, so an imported verifier refuses exactly the plans that carry one"),
		member("imported-plan-carries-the-process-session", "pkg/agentic/imported_process.go", claudePackage,
			[]replacement{
				{before: "\tstdin    StdinPayload\n}", after: "\tstdin    StdinPayload\n\tsession  *PlanSession\n}"},
				{before: "\t\tstdin:    cloneStdin(process.Stdin),\n\t}, nil", after: "\t\tstdin:    cloneStdin(process.Stdin),\n\t\tsession:  cloneSession(process.Session),\n\t}, nil"},
				{before: "\t\tHome:    p.home,\n\t}\n}", after: "\t\tHome:    p.home,\n\t\tSession: cloneSession(p.session),\n\t}\n}"},
			},
			"TestImportedPlanNeverYieldsAVerifiedSession", "", "the imported plan carries Session", "the imported process keeps the Session its input carried and hands it back on the rebuilt plan; every other bound fact is unchanged"),
		member("empty-collection-equals-nil", "pkg/agentic/session.go", claudePackage,
			one("(left.RCIndices == nil) != (right.RCIndices == nil) || ", "false || "),
			seal, "settings-origin/before-finalization/index-collection-empty-to-nil", "want ErrFinalizedProcessChanged", "the Session comparison stops separating a nil index collection from an empty one; values and every other field are still compared"),
		member("clone-normalizes-nil-collection-to-empty", "pkg/agentic/session.go", agenticPackage,
			one("clone := PlanSession{RCEnabled: session.RCEnabled, RCIndices: slices.Clone(session.RCIndices)}\n", "clone := PlanSession{RCEnabled: session.RCEnabled, RCIndices: slices.Clone(session.RCIndices)}\n\tif clone.RCIndices == nil {\n\t\tclone.RCIndices = []int{}\n\t}\n"),
			"TestCloneSessionKeepsCollectionPresence", "", "a nil index slice cloned to", "the copy a snapshot binds turns a nil collection into an empty one"),
		member("tail-starting-with-rc-not-rederived", "pkg/agentic/finalize.go", claudePackage,
			one("if final.sessionPlanner == nil || len(tail) == 0 {", "if final.sessionPlanner == nil || len(tail) == 0 || tail[0] == \"--remote-control\" {"),
			"TestPlanSessionFollowsANativeTailThroughTheSeal", "tail-adds-rc-and-the-seal-binds-the-final-record", "want RC enabled at the tail's own position", "skips the re-derivation for exactly a tail that opens with the RC selector; other tails still re-derive"),
		// Foreign-argv surface (AC2).
		member("foreign-argv-helper-reexported", sessionFile, claudePackage,
			one("var _ agentic.SessionPlanner = (*System)(nil)", "var _ agentic.SessionPlanner = (*System)(nil)\n\n// FillPlanSessionFromArgv is the F1 shape: an exported entry that answers a\n// session for argv the caller chose.\nfunc FillPlanSessionFromArgv(argv []string, workDir string) (*agentic.PlanSession, error) {\n\treturn deriveSession(argv, workDir)\n}"),
			foreign, "no-exported-function-in-any-module-source-derives-a-session-from-argv", "exported function derives a PlanSession from caller-supplied input", "re-exports a foreign-argv derivation under a new name; the registry-issued fill path is untouched"),
		member("foreign-argv-method-keeps-the-fill-name", sessionFile, claudePackage,
			one("var _ agentic.SessionPlanner = (*System)(nil)", "var _ agentic.SessionPlanner = (*System)(nil)\n\n// DeriveSessionFromArgv avoids the FillPlanSession name: the census must find\n// it by signature, not by name.\nfunc (*System) DeriveSessionFromArgv(argv []string) (*agentic.PlanSession, error) {\n\treturn deriveSession(argv, \"\")\n}"),
			foreign, "no-exported-function-in-any-module-source-derives-a-session-from-argv", "exported function derives a PlanSession from caller-supplied input", "adds a differently named exported method taking argv; FillPlanSession itself is untouched"),
		member("fill-admits-an-unissued-fill", sessionFile, claudePackage,
			one("if !fill.Issued() {", "if !fill.Issued() && len(fill.Argv()) > 0 {"),
			foreign, "a-fill-no-caller-can-issue-derives-nothing", "want nil and ErrSessionInvalid", "admits exactly the empty unissued fill the zero value is; an unissued fill carrying argv would still refuse"),
		member("fill-gains-an-argv-writer", "pkg/agentic/session.go", claudePackage,
			one("// Issued reports whether", "// WithArgv hands a caller the issuing power the type exists to withhold.\nfunc (f SessionFill) WithArgv(argv []string) SessionFill { f.argv, f.issued = argv, true; return f }\n\n// Issued reports whether"),
			foreign, "the-derivation-signature-takes-no-argv-a-caller-can-fill", "takes parameters; a SessionFill method could write into the fill", "adds a value-receiver method that returns an issued fill carrying caller argv; the one real fill is untouched"),
		member("fill-exports-its-argv-field", "pkg/agentic/session.go", claudePackage,
			one("type SessionFill struct {\n\targv    []string", "type SessionFill struct {\n\tArgv0   []string\n\targv    []string"),
			foreign, "the-derivation-signature-takes-no-argv-a-caller-can-fill", "exports field Argv0", "exports a field on the fill; the issued fill still carries the real argv"),
		// Removed projector surface.
		member("projector-function-returns", sessionFile, claudePackage,
			one("var _ agentic.SessionPlanner = (*System)(nil)", "var _ agentic.SessionPlanner = (*System)(nil)\n\nfunc ProjectSession() {}"),
			surface, "no-retired-declaration-in-any-module-source", "retired session projector surface declared again", "re-adds the retired entry point's name as a plain function"),
		member("projector-method-under-new-name", sessionFile, claudePackage,
			one("var _ agentic.SessionPlanner = (*System)(nil)", "var _ agentic.SessionPlanner = (*System)(nil)\n\n// ProjectSession stays in this comment only: the token is preserved and no\n// declaration carries it.\nfunc (*System) PlannedSessionProjector(agentic.Plan) (agentic.PlanSession, error) {\n\treturn agentic.PlanSession{}, nil\n}"),
			surface, "plugin-method-set-has-only-the-plan-fill", "want exactly [FillPlanSession]", "keeps the retired name only in a comment and exports a second, differently named session method; the source scan sees nothing"),
	}
}
