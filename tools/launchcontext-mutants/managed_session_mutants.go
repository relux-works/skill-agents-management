package main

import "strings"

// managedSessionMutants narrow the FinalizePlan typed reservation binding one
// member at a time (TASK-261004-zwslo3). Each keeps its gate in place and
// weakens it to admit exactly one member of the class the gate must reject;
// the named test must fail for the mutant to count as killed, so a
// delete-only edit is never the evidence.
//
// The groups: the module-fixed slot positions (argv slot, env slot), the slot
// set (a third slot, another binding kind, a binding with no reservation), the
// reservation's own shape and agreement (forged SES, forged UUID, a slot value
// that differs), the NEW-launch rule (an Intent, a resume or adoption selector
// in the argv, a pre-existing env slot, a system with no session grammar), the
// session re-derivation over the shifted argv, and the finalized verifier's
// two slot checks.
//
// Bound for the verifier members: the whole-process binding independently
// refuses the same tampering, so a verifier that skips a slot is not
// observable as an admitted process. Each verifier member therefore exempts
// ONE synthetic member (one fixture value or one position) and keeps the slot
// check active for every other; the reservation check runs first so the
// refusal names the slot, and the named test asserts that attribution. The
// member is killed on the slot check's own diagnostic, not on the downstream
// binder's, which still refuses the exempted fixture under another message.
func managedSessionMutants() []mutant {
	const (
		reservationFile = "pkg/agentic/finalize_reservation.go"
		finalizeFile    = "pkg/agentic/finalize.go"
		agenticPackage  = "./pkg/agentic"
		codexPackage    = "./pkg/agentic/systems/codex"
		positions       = "TestFinalizeReservationBindsTheTwoSlotsAtFixedPositions"
		slotSet         = "TestFinalizeReservationRefusesAThirdTypedSlotAndAnyOtherBinding"
		verifier        = "TestFinalizeReservationVerifierBindsBothSlots"
		refused         = "want agentic: finalize session reservation is refused"
		unknown         = "want agentic: finalize binding kind is unknown"
	)
	member := func(name, file, pkg string, changes []replacement, test, subtest, failure, narrows string) mutant {
		testName, pattern := test, "^"+test+"$"
		if subtest != "" {
			testName = test + "/" + subtest
			for _, level := range strings.Split(subtest, "/") {
				pattern += "/^" + level + "$"
			}
		}
		return mutant{name: "managed-session-" + name, file: file, gate: "FinalizePlan typed reservation binding", member: name, narrows: narrows,
			replacements: changes, testPackage: pkg, testName: testName, runPattern: pattern, failureText: failure}
	}
	one := func(before, after string) []replacement { return []replacement{{before: before, after: after}} }
	return []mutant{
		// AC5 named mutants: slot position not fixed, third slot admitted,
		// verifier skips a slot.
		member("argv-slot-position-not-fixed", reservationFile, agenticPackage,
			one("return append(append(head, base...), tail...)", "return append(append(append([]string(nil), base...), head...), tail...)"),
			positions, "exec", "want the argv slot first", "moves the argv slot behind the sealed base argv; the env slot stays last"),
		member("env-slot-position-not-fixed", reservationFile, agenticPackage,
			one("return append(env, ManagedSessionEnvSlot+\"=\"+r.sessionID)", "return append([]string{ManagedSessionEnvSlot + \"=\" + r.sessionID}, env...)"),
			positions, "exec", "want the env slot last", "moves the env slot ahead of every overlay; the argv slot stays first"),
		member("third-slot-admitted", reservationFile, agenticPackage,
			one("if len(bindings) != 2 || len(values) != 2 {", "if (len(bindings) != 2 && !(len(bindings) == 3 && bindings[2].Name == ManagedSessionEnvSlot)) || len(values) != 2 {"),
			slotSet, "third-slot-repeats-env", refused, "admits a third slot that repeats the env slot; a repeated argv slot and every other count still refuse"),
		member("third-slot-unknown-name-admitted", reservationFile, agenticPackage,
			one("(binding.Name != ManagedSessionEnvSlot && binding.Name != ManagedSessionArgvSlot)", "(binding.Name != ManagedSessionEnvSlot && binding.Name != ManagedSessionArgvSlot && binding.Name != \"TASK_BOARD_EXTRA\")"),
			slotSet, "third-slot-unknown-name", unknown, "admits one extra slot name past the kind/name gate; every other name still refuses unknown"),
		member("verifier-skips-the-argv-slot", reservationFile, agenticPackage,
			one("if len(plan.Argv) < 2 || plan.Argv[0] != ManagedSessionArgvSlot || plan.Argv[1] != r.nativeID {", "if (len(plan.Argv) < 2 || plan.Argv[0] != ManagedSessionArgvSlot || plan.Argv[1] != r.nativeID) && !(len(plan.Argv) > 1 && plan.Argv[1] == \"11111111-2222-4333-8444-555555555555\") {"),
			verifier, "argv-slot-value", "naming \"reservation argv slot\"", "the argv slot check exempts exactly the synthetic forged UUID fixture; every other changed, moved or dropped argv slot still refuses by name"),
		member("verifier-admits-a-moved-argv-slot", reservationFile, agenticPackage,
			one("if len(plan.Argv) < 2 || plan.Argv[0] != ManagedSessionArgvSlot || plan.Argv[1] != r.nativeID {", "if (len(plan.Argv) < 2 || plan.Argv[0] != ManagedSessionArgvSlot || plan.Argv[1] != r.nativeID) && !(len(plan.Argv) > 2 && plan.Argv[len(plan.Argv)-2] == ManagedSessionArgvSlot && plan.Argv[len(plan.Argv)-1] == r.nativeID) {"),
			verifier, "argv-slot-moved", "naming \"reservation argv slot\"", "the argv slot check exempts exactly the slot moved to the end of the argv; the intact slot's value check and every other position still refuse by name"),
		member("verifier-skips-the-env-slot", reservationFile, agenticPackage,
			one("if name != ManagedSessionEnvSlot || literalCommitment(key, name, value) != r.envCommitment {", "if (name != ManagedSessionEnvSlot || literalCommitment(key, name, value) != r.envCommitment) && value != \"SES-forged\" {"),
			verifier, "env-slot-value", "naming \"reservation env slot\"", "the env slot check exempts exactly the synthetic SES-forged fixture value; every other changed, moved or dropped env slot still refuses by name"),
		member("verifier-skips-the-env-slot-value", reservationFile, agenticPackage,
			one("if name != ManagedSessionEnvSlot || literalCommitment(key, name, value) != r.envCommitment {", "if name != ManagedSessionEnvSlot || (literalCommitment(key, name, value) != r.envCommitment && value != \"SES-forged\") {"),
			verifier, "env-slot-value", "naming \"reservation env slot\"", "the env slot value comparison exempts exactly the synthetic SES-forged fixture value; the slot's name and last position are still checked"),
		member("verifier-admits-a-moved-env-slot", reservationFile, agenticPackage,
			one("if name != ManagedSessionEnvSlot || literalCommitment(key, name, value) != r.envCommitment {", "if (name != ManagedSessionEnvSlot || literalCommitment(key, name, value) != r.envCommitment) && !(len(plan.Env) > 0 && strings.HasPrefix(plan.Env[0], ManagedSessionEnvSlot+\"=\")) {"),
			verifier, "env-slot-moved", "naming \"reservation env slot\"", "the env slot check exempts exactly the moved slot found as the first entry; the slot as last entry and every other position still refuse by name"),
		// A binding with no reservation.
		member("slot-admitted-without-a-reservation", finalizeFile, agenticPackage,
			one("func refuseFinalizeBindings(bindings []TypedBinding) error {\n\tif len(bindings) == 0 {", "func refuseFinalizeBindings(bindings []TypedBinding) error {\n\tif len(bindings) == 0 || (len(bindings) == 1 && bindings[0].Kind == BindingKindManagedSession) {"),
			slotSet, "one-slot-without-a-reservation", unknown, "admits exactly one managed-session binding when no reservation exists; two bindings and every other kind still refuse"),
		// Reservation shape and agreement.
		member("forged-ses-admitted", reservationFile, agenticPackage,
			one("if !resumeHandle.MatchString(reservation.SessionID) {", "if !resumeHandle.MatchString(reservation.SessionID) && reservation.SessionID != \"\" {"),
			"TestFinalizeReservationRefusesForgedSES", "", refused, "admits exactly the empty SES handle; every other malformed handle still refuses"),
		member("forged-uuid-admitted", reservationFile, agenticPackage,
			one("if !resumeUUID.MatchString(reservation.NativeID) {", "if !resumeUUID.MatchString(reservation.NativeID) && reservation.NativeID != \"latest\" {"),
			"TestFinalizeReservationRefusesForgedUUID", "", refused, "admits exactly the picker spelling \"latest\" as the native id; every other malformed id still refuses"),
		member("env-slot-mismatch-admitted", reservationFile, agenticPackage,
			one("if values[ManagedSessionEnvSlot] != reservation.SessionID || values[ManagedSessionArgvSlot] != reservation.NativeID {", "if (values[ManagedSessionEnvSlot] != reservation.SessionID && values[ManagedSessionEnvSlot] != \"SES-00000000\") || values[ManagedSessionArgvSlot] != reservation.NativeID {"),
			"TestFinalizeReservationRefusesSlotValueMismatch", "env-slot-differs", refused, "admits exactly one differing env slot value; the argv slot still has to agree"),
		member("argv-slot-mismatch-admitted", reservationFile, agenticPackage,
			one("if values[ManagedSessionEnvSlot] != reservation.SessionID || values[ManagedSessionArgvSlot] != reservation.NativeID {", "if values[ManagedSessionEnvSlot] != reservation.SessionID || (values[ManagedSessionArgvSlot] != reservation.NativeID && values[ManagedSessionArgvSlot] != \"99999999-8888-4777-8666-555555555555\") {"),
			"TestFinalizeReservationRefusesSlotValueMismatch", "argv-slot-differs", refused, "admits exactly one differing argv slot value; the env slot still has to agree"),
		member("env-slot-carries-the-native-id", reservationFile, agenticPackage,
			one("return append(env, ManagedSessionEnvSlot+\"=\"+r.sessionID)", "return append(env, ManagedSessionEnvSlot+\"=\"+r.nativeID)"),
			positions, "exec", "want the env slot last after the native env", "writes the native UUID into the env slot instead of the SES handle"),
		// NEW-launch rule.
		member("reservation-intent-latest-admitted", reservationFile, agenticPackage,
			one("if err := ValidateResumeIntent(intent, \"\"); err != nil || intent.Kind != ResumeNew {", "if err := ValidateResumeIntent(intent, \"\"); err != nil || (intent.Kind != ResumeNew && intent.Kind != ResumeLatest) {"),
			"TestFinalizeReservationRefusesResumeIntent", "intent", refused, "admits exactly the latest-resume intent under a reservation; handle and native-id intents still refuse"),
		member("reservation-intent-identity-on-new-admitted", reservationFile, agenticPackage,
			one("if err := ValidateResumeIntent(intent, \"\"); err != nil || intent.Kind != ResumeNew {", "if err := ValidateResumeIntent(intent, \"\"); (err != nil && !(reservation.Intent.Kind == ResumeNew && intent.Identity != nil)) || intent.Kind != ResumeNew {"),
			"TestFinalizeReservationRefusesIdentityOnANewIntent", "resume-new-with-identity", refused, "admits exactly an explicit ResumeNew intent that carries an Identity; an empty Kind with an Identity, a latest intent and every resume intent still refuse"),
		member("reservation-intent-empty-kind-identity-admitted", reservationFile, agenticPackage,
			one("if err := ValidateResumeIntent(intent, \"\"); err != nil || intent.Kind != ResumeNew {", "if err := ValidateResumeIntent(intent, \"\"); (err != nil && !(reservation.Intent.Kind == \"\" && intent.Identity != nil)) || intent.Kind != ResumeNew {"),
			"TestFinalizeReservationRefusesIdentityOnANewIntent", "empty-kind-with-identity", refused, "admits exactly an empty-Kind intent that carries an Identity after it is normalized to new; an explicit ResumeNew with an Identity and every resume intent still refuse"),
		member("tail-continue-admitted", reservationFile, agenticPackage,
			one("err != nil || elevation.Intent.Kind != ResumeNew {", "err != nil || (elevation.Intent.Kind != ResumeNew && elevation.Intent.Kind != ResumeLatest) {"),
			"TestFinalizeReservationRefusesResumeIntent", "native-tail", refused, "admits exactly the continue-latest selector in the argv; an identity resume and every adoption selector still refuse"),
		member("tail-adoption-admitted", reservationFile, agenticPackage,
			one("err != nil || elevation.Intent.Kind != ResumeNew {", "(err != nil && !strings.Contains(err.Error(), \"ambiguous native identity\")) || (err == nil && elevation.Intent.Kind != ResumeNew) {"),
			"TestFinalizeReservationRefusesResumeIntent", "native-tail", refused, "admits exactly the ambiguous-identity class (--session-id, --fork-session) in the argv; resume selectors and every other grammar refusal still refuse"),
		member("env-slot-already-carried-admitted", reservationFile, agenticPackage,
			one("; name == ManagedSessionEnvSlot {", "; name == ManagedSessionEnvSlot && !strings.HasSuffix(entry, \"=SES-caller\") {"),
			"TestFinalizeReservationRefusesAProcessThatAlreadyCarriesTheEnvSlot", "", refused, "admits exactly the caller value SES-caller already on the env; any other carried value still refuses"),
		member("system-without-a-session-grammar-admitted", reservationFile, codexPackage,
			[]replacement{
				{before: "if !ok {\n\t\treturn nil, fmt.Errorf(\"%w: system %q has no native session grammar", after: "if !ok && string(base.System) != \"codex\" {\n\t\treturn nil, fmt.Errorf(\"%w: system %q has no native session grammar"},
				{before: "elevator.ElevateResumeIntent(nil, argv); err != nil", after: "elevateOrNew(elevator, argv); err != nil"},
				{before: "// finalEnv appends the env slot last.", after: "func elevateOrNew(elevator ResumeSelectorElevator, argv []string) (ResumeElevation, error) {\n\tif elevator == nil {\n\t\treturn ResumeElevation{Intent: ResumeIntent{Kind: ResumeNew}}, nil\n\t}\n\treturn elevator.ElevateResumeIntent(nil, argv)\n}\n\n// finalEnv appends the env slot last."},
			},
			"TestFinalizeSealedPlanRefusesAReservation", "", "want ErrFinalizeBindingUnknown for a system with no native session grammar", "admits exactly the Codex system, which has no native session grammar; every other grammar-less system still refuses"),
		// Session re-derivation over the shifted argv.
		member("slots-alone-do-not-rederive-the-session", finalizeFile, agenticPackage,
			one("slots != nil || len(overlays.NativeTail) > 0); err != nil {\n\t\treturn Plan{}, err\n\t}\n\t_ = selectors\n\tb := bindFinalProcess(final, key)", "len(overlays.NativeTail) > 0); err != nil {\n\t\treturn Plan{}, err\n\t}\n\t_ = selectors\n\tb := bindFinalProcess(final, key)"),
			"TestFinalizeReservationRederivesSessionOverTheShiftedArgv", "selector-in-sealed-base", "shifted by the argv slot", "keeps the base Session record when a reservation prepends the argv slot with no native tail; a tail still re-derives"),
	}
}
