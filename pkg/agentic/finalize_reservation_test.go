package agentic_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

const (
	reservationTestSES  = "SES-7f3a91c2"
	reservationTestUUID = "0b9f8c2e-4d1a-4e6b-9c3d-2a5f7e8d1b04"
)

func reservationTestReservation() *agentic.SessionReservation {
	return &agentic.SessionReservation{SessionID: reservationTestSES, NativeID: reservationTestUUID}
}

// reservationTestSlots are the two typed slots that agree with the
// reservation above.
func reservationTestSlots() []agentic.TypedBinding {
	return []agentic.TypedBinding{
		{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: reservationTestSES},
		{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: reservationTestUUID},
	}
}

// reservationTestPlan builds a Claude plan through BuildPlan in the given
// mode; interactive plans carry the native args as their verbatim suffix.
func reservationTestPlan(t *testing.T, mode agentic.LaunchMode, native ...string) agentic.Plan {
	t.Helper()
	binDir := sealTestStubBin(t, "claude")
	plan, err := agentic.BuildPlan(sealTestRegistry(t), agentic.LaunchRequest{
		System:     claude.New().ID(),
		Model:      agentic.Model{ID: "seal-test"},
		WorkDir:    t.TempDir(),
		Env:        []string{"PATH=" + binDir},
		NativeArgs: native,
	}, mode)
	if err != nil {
		t.Fatalf("BuildPlan Claude mode %v: %v", mode, err)
	}
	return plan
}

func reservationFinalize(base agentic.Plan, overlays agentic.FinalizeOverlays, reservation *agentic.SessionReservation, slots []agentic.TypedBinding) (agentic.Plan, error) {
	overlays.Reservation = reservation
	return agentic.FinalizePlan(base, overlays, slots)
}

func wantReservationRefusal(t *testing.T, err, sentinel error, detail string) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Fatalf("FinalizePlan err = %v, want %v (%s)", err, sentinel, detail)
	}
}

// TestFinalizeReservationBindsTheTwoSlotsAtFixedPositions is the happy path
// and the parity statement: against the same base, the reserved final plan
// differs from the native finalization in exactly the two slots — the argv
// slot as the first two tokens, the env slot as the last entry — and nothing
// else moves.
func TestFinalizeReservationBindsTheTwoSlotsAtFixedPositions(t *testing.T) {
	for name, mode := range map[string]agentic.LaunchMode{"exec": agentic.LaunchModeExec, "interactive": agentic.LaunchModeInteractive} {
		t.Run(name, func(t *testing.T) {
			base := reservationTestPlan(t, mode)
			overlays := agentic.FinalizeOverlays{
				FragmentEnv: []string{"RESERVATION_FRAGMENT=one"},
				PromptEnv:   []string{"RESERVATION_PROMPT=two"},
				NativeTail:  []string{"--verbose"},
			}
			native, err := agentic.FinalizePlan(base, overlays, nil)
			if err != nil {
				t.Fatalf("native FinalizePlan: %v", err)
			}
			reserved, err := reservationFinalize(base, overlays, reservationTestReservation(), reservationTestSlots())
			if err != nil {
				t.Fatalf("reserved FinalizePlan: %v", err)
			}
			wantArgv := append([]string{agentic.ManagedSessionArgvSlot, reservationTestUUID}, native.Argv...)
			if !slices.Equal(reserved.Argv, wantArgv) {
				t.Fatalf("reserved argv = %q, want the argv slot first then the native argv %q", reserved.Argv, native.Argv)
			}
			wantEnv := append(slices.Clone(native.Env), agentic.ManagedSessionEnvSlot+"="+reservationTestSES)
			if !slices.Equal(reserved.Env, wantEnv) {
				t.Fatalf("reserved env = %q, want the env slot last after the native env %q", reserved.Env, native.Env)
			}
			if reserved.Binary != native.Binary || reserved.WorkDir != native.WorkDir || reserved.System != native.System {
				t.Fatalf("reserved process identity differs beyond the slots: %q/%q/%q vs %q/%q/%q", reserved.Binary, reserved.WorkDir, reserved.System, native.Binary, native.WorkDir, native.System)
			}
			if err := reserved.VerifyBeforeExec(); err != nil {
				t.Fatalf("reserved plan refused itself: %v", err)
			}
		})
	}
}

// TestFinalizeReservationRederivesSessionOverTheShiftedArgv pins that the
// argv slot's two tokens are visible to the session record: a remote-control
// selector sits two positions later than in the native finalization, and
// Plan.Session says so, whether the selector comes from the sealed base argv
// (no native tail at all) or from the native tail.
func TestFinalizeReservationRederivesSessionOverTheShiftedArgv(t *testing.T) {
	cases := map[string]struct {
		base    []string
		overlay agentic.FinalizeOverlays
	}{
		"selector-in-sealed-base": {base: []string{"--remote-control"}},
		"selector-in-native-tail": {overlay: agentic.FinalizeOverlays{NativeTail: []string{"--remote-control"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			base := reservationTestPlan(t, agentic.LaunchModeInteractive, tc.base...)
			native, err := agentic.FinalizePlan(base, tc.overlay, nil)
			if err != nil {
				t.Fatalf("native FinalizePlan: %v", err)
			}
			reserved, err := reservationFinalize(base, tc.overlay, reservationTestReservation(), reservationTestSlots())
			if err != nil {
				t.Fatalf("reserved FinalizePlan: %v", err)
			}
			if native.Session == nil || len(native.Session.RCIndices) != 1 || reserved.Session == nil || len(reserved.Session.RCIndices) != 1 {
				t.Fatalf("sessions = %+v / %+v, want one RC index each", native.Session, reserved.Session)
			}
			if got, want := reserved.Session.RCIndices[0], native.Session.RCIndices[0]+2; got != want {
				t.Fatalf("reserved RC index = %d, want %d (native %d shifted by the argv slot)", got, want, native.Session.RCIndices[0])
			}
			if reserved.Argv[reserved.Session.RCIndices[0]] != "--remote-control" {
				t.Fatalf("RC index %d does not point at the selector in %q", reserved.Session.RCIndices[0], reserved.Argv)
			}
			if err := reserved.VerifyBeforeExec(); err != nil {
				t.Fatalf("reserved plan refused itself: %v", err)
			}
		})
	}
}

// TestFinalizeReservationVerifierBindsBothSlots mutates the finalized plan at
// each slot and asserts the refusal NAMES the slot: the reservation check runs
// before the whole-process comparison, so a changed slot is attributed to the
// reservation rather than to a generic argv or environment difference.
func TestFinalizeReservationVerifierBindsBothSlots(t *testing.T) {
	final, err := reservationFinalize(reservationTestPlan(t, agentic.LaunchModeExec), agentic.FinalizeOverlays{FragmentEnv: []string{"RESERVATION_FRAGMENT=one"}}, reservationTestReservation(), reservationTestSlots())
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	forgedUUID := "11111111-2222-4333-8444-555555555555"
	cases := []struct {
		name   string
		mutate func(*agentic.Plan)
		want   string
	}{
		{"argv-slot-value", func(p *agentic.Plan) { p.Argv = slices.Clone(p.Argv); p.Argv[1] = forgedUUID }, "reservation argv slot"},
		{"argv-slot-flag", func(p *agentic.Plan) { p.Argv = slices.Clone(p.Argv); p.Argv[0] = "--resume" }, "reservation argv slot"},
		{"argv-slot-dropped", func(p *agentic.Plan) { p.Argv = slices.Clone(p.Argv[2:]) }, "reservation argv slot"},
		{"argv-slot-moved", func(p *agentic.Plan) {
			p.Argv = append(slices.Clone(p.Argv[2:]), p.Argv[0], p.Argv[1])
		}, "reservation argv slot"},
		{"env-slot-value", func(p *agentic.Plan) {
			p.Env = slices.Clone(p.Env)
			p.Env[len(p.Env)-1] = agentic.ManagedSessionEnvSlot + "=SES-forged"
		}, "reservation env slot"},
		{"env-slot-dropped", func(p *agentic.Plan) { p.Env = slices.Clone(p.Env[:len(p.Env)-1]) }, "reservation env slot"},
		{"env-slot-moved", func(p *agentic.Plan) {
			p.Env = append([]string{p.Env[len(p.Env)-1]}, p.Env[:len(p.Env)-1]...)
		}, "reservation env slot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := final
			tc.mutate(&changed)
			err := changed.VerifyBeforeExec()
			if !errors.Is(err, agentic.ErrFinalizedProcessChanged) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("VerifyBeforeExec err = %v, want ErrFinalizedProcessChanged naming %q", err, tc.want)
			}
		})
	}
}

func TestFinalizeReservationRefusesForgedSES(t *testing.T) {
	base := reservationTestPlan(t, agentic.LaunchModeExec)
	for _, forged := range []string{"", "ses-7f3a91c2", "SES-", "SES-bad value", "7f3a91c2", reservationTestUUID, "SES-x\x00y", "SES-" + strings.Repeat("a", 129)} {
		reservation := &agentic.SessionReservation{SessionID: forged, NativeID: reservationTestUUID}
		slots := []agentic.TypedBinding{
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: forged},
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: reservationTestUUID},
		}
		_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservation, slots)
		wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, "forged SES "+forged)
	}
}

func TestFinalizeReservationRefusesForgedUUID(t *testing.T) {
	base := reservationTestPlan(t, agentic.LaunchModeExec)
	for _, forged := range []string{"", "latest", "--last", reservationTestSES, "0b9f8c2e-4d1a-4e6b-9c3d-2a5f7e8d1b0", "0b9f8c2e-4d1a-4e6b-9c3d-2a5f7e8d1b04\n", "0b9f8c2e4d1a4e6b9c3d2a5f7e8d1b04", "0b9f8c2e-4d1a-4e6b-9c3d-2a5f7e8d1bzz"} {
		reservation := &agentic.SessionReservation{SessionID: reservationTestSES, NativeID: forged}
		slots := []agentic.TypedBinding{
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: reservationTestSES},
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: forged},
		}
		_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservation, slots)
		wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, "forged UUID "+forged)
	}
}

func TestFinalizeReservationRefusesAThirdTypedSlotAndAnyOtherBinding(t *testing.T) {
	base := reservationTestPlan(t, agentic.LaunchModeExec)
	slots := reservationTestSlots()
	cases := []struct {
		name     string
		bindings []agentic.TypedBinding
		want     error
	}{
		{"third-slot-unknown-name", append(slices.Clone(slots), agentic.TypedBinding{Kind: agentic.BindingKindManagedSession, Name: "TASK_BOARD_EXTRA", Value: "x"}), agentic.ErrFinalizeBindingUnknown},
		{"third-slot-resume-flag", append(slices.Clone(slots), agentic.TypedBinding{Kind: agentic.BindingKindManagedSession, Name: "--resume", Value: reservationTestUUID}), agentic.ErrFinalizeBindingUnknown},
		{"third-slot-repeats-env", append(slices.Clone(slots), slots[0]), agentic.ErrFinalizeReservationRefused},
		{"third-slot-repeats-argv", append(slices.Clone(slots), slots[1]), agentic.ErrFinalizeReservationRefused},
		{"two-env-slots", []agentic.TypedBinding{slots[0], slots[0]}, agentic.ErrFinalizeReservationRefused},
		{"argv-slot-missing", slots[:1], agentic.ErrFinalizeReservationRefused},
		{"env-slot-missing", slots[1:], agentic.ErrFinalizeReservationRefused},
		{"no-slots-under-a-reservation", nil, agentic.ErrFinalizeReservationRefused},
		{"other-kind-for-a-slot-name", []agentic.TypedBinding{{Kind: "future-kind", Name: agentic.ManagedSessionEnvSlot, Value: reservationTestSES}, slots[1]}, agentic.ErrFinalizeBindingUnknown},
		{"unknown-name-in-place-of-a-slot", []agentic.TypedBinding{slots[0], {Kind: agentic.BindingKindManagedSession, Name: "--model", Value: reservationTestUUID}}, agentic.ErrFinalizeBindingUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservationTestReservation(), tc.bindings)
			wantReservationRefusal(t, err, tc.want, tc.name)
		})
	}
	for name, bindings := range map[string][]agentic.TypedBinding{"both-slots-without-a-reservation": slots, "one-slot-without-a-reservation": slots[:1]} {
		t.Run(name, func(t *testing.T) {
			_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, nil, bindings)
			wantReservationRefusal(t, err, agentic.ErrFinalizeBindingUnknown, "the managed-session kind is admitted only under a reservation")
		})
	}
}

func TestFinalizeReservationRefusesSlotValueMismatch(t *testing.T) {
	base := reservationTestPlan(t, agentic.LaunchModeExec)
	otherSES, otherUUID := "SES-00000000", "99999999-8888-4777-8666-555555555555"
	cases := []struct {
		name  string
		slots []agentic.TypedBinding
	}{
		{"env-slot-differs", []agentic.TypedBinding{
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: otherSES},
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: reservationTestUUID},
		}},
		{"argv-slot-differs", []agentic.TypedBinding{
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: reservationTestSES},
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: otherUUID},
		}},
		{"slots-swapped", []agentic.TypedBinding{
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: reservationTestUUID},
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: reservationTestSES},
		}},
		{"slot-value-empty", []agentic.TypedBinding{
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: ""},
			{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: reservationTestUUID},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservationTestReservation(), tc.slots)
			wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, tc.name)
		})
	}
}

// TestFinalizeReservationRefusesResumeIntent covers both ways a resume or
// adoption intent can sit under a reservation: as the reservation's own
// Intent, and as a selector the plugin's resume grammar finds in the argv the
// slots would sit beside (the native tail or the sealed base).
func TestFinalizeReservationRefusesResumeIntent(t *testing.T) {
	resumeUUID := "11111111-2222-4333-8444-555555555555"
	handle := "SES-resume-target"
	t.Run("intent", func(t *testing.T) {
		base := reservationTestPlan(t, agentic.LaunchModeExec)
		for name, intent := range map[string]agentic.ResumeIntent{
			"latest":      {Kind: agentic.ResumeLatest},
			"handle":      {Kind: agentic.ResumeHandle, Identity: &handle},
			"claude-uuid": {Kind: agentic.ResumeClaudeUUID, Identity: &resumeUUID},
		} {
			reservation := reservationTestReservation()
			reservation.Intent = intent
			_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservation, reservationTestSlots())
			wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, "intent "+name)
		}
		explicitNew := reservationTestReservation()
		explicitNew.Intent = agentic.ResumeIntent{Kind: agentic.ResumeNew}
		if _, err := reservationFinalize(base, agentic.FinalizeOverlays{}, explicitNew, reservationTestSlots()); err != nil {
			t.Fatalf("an explicit ResumeNew intent is a new launch and must be admitted: %v", err)
		}
	})
	t.Run("native-tail", func(t *testing.T) {
		base := reservationTestPlan(t, agentic.LaunchModeInteractive)
		for name, tail := range map[string][]string{
			"resume":          {"--resume", resumeUUID},
			"resume-short":    {"-r", resumeUUID},
			"continue":        {"--continue"},
			"continue-short":  {"-c"},
			"session-id":      {"--session-id", resumeUUID},
			"session-id-eq":   {"--session-id=" + resumeUUID},
			"session-id-same": {"--session-id", reservationTestUUID},
			"fork-session":    {"--fork-session"},
		} {
			_, err := reservationFinalize(base, agentic.FinalizeOverlays{NativeTail: tail}, reservationTestReservation(), reservationTestSlots())
			wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, "tail "+name)
		}
	})
	t.Run("sealed-base", func(t *testing.T) {
		for name, native := range map[string][]string{
			"resume":     {"--resume", resumeUUID},
			"session-id": {"--session-id", resumeUUID},
		} {
			base := reservationTestPlan(t, agentic.LaunchModeInteractive, native...)
			_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservationTestReservation(), reservationTestSlots())
			wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, "base "+name)
		}
	})
}

// TestFinalizeReservationRefusesIdentityOnANewIntent: a reservation Intent
// follows the module's ResumeIntent contract (ValidateResumeIntent), under
// which a new launch carries no Identity. The native UUID travels only as the
// argv slot value, so even the reserved UUID itself as an Identity refuses,
// whether the Kind is spelled ResumeNew or left empty (normalized to new).
func TestFinalizeReservationRefusesIdentityOnANewIntent(t *testing.T) {
	base := reservationTestPlan(t, agentic.LaunchModeExec)
	other := "11111111-2222-4333-8444-555555555555"
	reserved := reservationTestUUID
	cases := []struct {
		name   string
		intent agentic.ResumeIntent
	}{
		{"resume-new-with-identity", agentic.ResumeIntent{Kind: agentic.ResumeNew, Identity: &other}},
		{"empty-kind-with-identity", agentic.ResumeIntent{Identity: &other}},
		{"resume-new-with-the-reserved-uuid", agentic.ResumeIntent{Kind: agentic.ResumeNew, Identity: &reserved}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reservation := reservationTestReservation()
			reservation.Intent = tc.intent
			_, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservation, reservationTestSlots())
			wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, tc.name)
		})
	}
	t.Run("empty-intent-without-identity-is-admitted", func(t *testing.T) {
		reservation := reservationTestReservation()
		reservation.Intent = agentic.ResumeIntent{}
		if _, err := reservationFinalize(base, agentic.FinalizeOverlays{}, reservation, reservationTestSlots()); err != nil {
			t.Fatalf("an empty intent is a new launch and must be admitted: %v", err)
		}
	})
}

// TestFinalizeReservationRefusesAProcessThatAlreadyCarriesTheEnvSlot: a
// caller-supplied value for the slot, from the base env or either overlay,
// is a second source for the same fact and refuses rather than being
// overridden or kept beside the reserved one.
func TestFinalizeReservationRefusesAProcessThatAlreadyCarriesTheEnvSlot(t *testing.T) {
	carried := agentic.ManagedSessionEnvSlot + "=SES-caller"
	base := reservationTestPlan(t, agentic.LaunchModeExec)
	for name, overlays := range map[string]agentic.FinalizeOverlays{
		"fragment": {FragmentEnv: []string{carried}},
		"prompt":   {PromptEnv: []string{carried}},
	} {
		_, err := reservationFinalize(base, overlays, reservationTestReservation(), reservationTestSlots())
		wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, name+" overlay")
	}
	binDir := sealTestStubBin(t, "claude")
	withEnv, err := agentic.BuildPlan(sealTestRegistry(t), agentic.LaunchRequest{
		System:  claude.New().ID(),
		Model:   agentic.Model{ID: "seal-test"},
		WorkDir: t.TempDir(),
		Env:     []string{"PATH=" + binDir, carried},
	}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if !slices.Contains(withEnv.Env, carried) {
		t.Skipf("BuildPlan does not pass %s through; the base-env source cannot occur", agentic.ManagedSessionEnvSlot)
	}
	_, err = reservationFinalize(withEnv, agentic.FinalizeOverlays{}, reservationTestReservation(), reservationTestSlots())
	wantReservationRefusal(t, err, agentic.ErrFinalizeReservationRefused, "base env")
}
