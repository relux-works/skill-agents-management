package agentic

import (
	"errors"
	"reflect"
	"testing"
)

// sessionPlannerSystem wraps the pangolin double with the optional
// SessionPlanner capability. fill is handed the argv BuildPlan gave the
// plugin and answers whatever record (or refusal) the row wants, so each test
// states the plugin's behavior in one closure and drives BuildPlan — the
// production dispatch site — rather than the helpers beneath it.
type sessionPlannerSystem struct {
	*pangolinSystem
	fill func(argv []string) (*PlanSession, error)

	seenMode    LaunchMode
	seenWorkDir string
	seenArgv    []string
}

func (s *sessionPlannerSystem) FillPlanSession(fill SessionFill) (*PlanSession, error) {
	if !fill.Issued() {
		return nil, &SessionInvalidError{Reason: "not issued"}
	}
	s.seenMode = fill.Mode()
	s.seenWorkDir = fill.WorkDir()
	argv := fill.Argv()
	s.seenArgv = append([]string(nil), argv...)
	return s.fill(argv)
}

func sessionName(name string) *string { return &name }

func buildSessionPlannerPlan(t *testing.T, fill func(argv []string) (*PlanSession, error)) (Plan, *sessionPlannerSystem, error) {
	t.Helper()
	sys := &sessionPlannerSystem{pangolinSystem: newPangolin(), fill: fill}
	registry := NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register: %v", err)
	}
	plan, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
	return plan, sys, err
}

// TestBuildPlanAttachesTheSessionRecordThePluginFilled drives the dispatch:
// the plugin is handed the argv it just built and the mode, its record lands
// on Plan.Session by value, and neither side shares memory with the other —
// not the argv the plugin received, not the record it returned.
func TestBuildPlanAttachesTheSessionRecordThePluginFilled(t *testing.T) {
	t.Parallel()
	answered := &PlanSession{Name: sessionName("N"), RCEnabled: true, RCIndices: []int{0, 1}}
	plan, sys, err := buildSessionPlannerPlan(t, func(argv []string) (*PlanSession, error) {
		argv[0] = "tampered-by-the-plugin"
		return answered, nil
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if sys.seenMode != LaunchModeExec {
		t.Errorf("plugin was handed mode %s, want %s", sys.seenMode, LaunchModeExec)
	}
	if len(plan.Argv) < 2 || plan.Argv[0] == "tampered-by-the-plugin" {
		t.Fatalf("plan argv = %#v; a plugin writing into the slice it was handed must not rewrite the plan", plan.Argv)
	}
	if !reflect.DeepEqual(sys.seenArgv, plan.Argv) {
		t.Errorf("the plugin was handed argv %#v, want the argv the plan carries %#v", sys.seenArgv, plan.Argv)
	}
	want := PlanSession{Name: sessionName("N"), RCEnabled: true, RCIndices: []int{0, 1}}
	if plan.Session == nil || !reflect.DeepEqual(*plan.Session, want) {
		t.Fatalf("Plan.Session = %+v, want %+v", plan.Session, want)
	}
	// Detachment, both directions.
	*answered.Name = "mutated-after-build"
	answered.RCIndices[0] = 9
	answered.RCEnabled = false
	if !reflect.DeepEqual(*plan.Session, want) {
		t.Errorf("Plan.Session changed to %+v when the plugin's own record was mutated after BuildPlan", *plan.Session)
	}
}

// TestBuildPlanLeavesSessionNilWithoutASessionSurface pins nil as the answer
// for a system that defines no native session surface — through a plugin
// with no planner and through one that answers nil — never a zero record
// that would read as "session disabled".
func TestBuildPlanLeavesSessionNilWithoutASessionSurface(t *testing.T) {
	t.Parallel()
	t.Run("no-planner", func(t *testing.T) {
		t.Parallel()
		plan, err := BuildPlan(registerPangolin(t, newPangolin()), pangolinRequest(), LaunchModeExec)
		if err != nil {
			t.Fatalf("BuildPlan: %v", err)
		}
		if plan.Session != nil {
			t.Fatalf("Plan.Session = %+v for a system with no SessionPlanner, want nil", plan.Session)
		}
	})
	t.Run("planner-answers-nil", func(t *testing.T) {
		t.Parallel()
		plan, _, err := buildSessionPlannerPlan(t, func([]string) (*PlanSession, error) { return nil, nil })
		if err != nil {
			t.Fatalf("BuildPlan: %v", err)
		}
		if plan.Session != nil {
			t.Fatalf("Plan.Session = %+v after a nil answer, want nil", plan.Session)
		}
	})
}

// TestBuildPlanRefusesIncoherentSessionRecords is the gate on what a plugin
// may answer: a nil index slice, a disabled RC carrying indices, an index
// outside the argv (the first one past the end is the narrow witness), a
// negative index and a repeated index each refuse as a plugin-contract
// violation and yield no plan. The accepted control sits beside them so a
// gate that refused everything would not pass.
func TestBuildPlanRefusesIncoherentSessionRecords(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name   string
		answer func(argv []string) *PlanSession
		admit  bool
	}{
		{name: "coherent-control", answer: func(argv []string) *PlanSession {
			return &PlanSession{RCEnabled: true, RCIndices: []int{0, len(argv) - 1}}
		}, admit: true},
		{name: "enabled-with-empty-indices-is-the-settings-origin-form", answer: func([]string) *PlanSession {
			return &PlanSession{RCEnabled: true, RCIndices: []int{}}
		}, admit: true},
		{name: "nil-index-slice", answer: func([]string) *PlanSession { return &PlanSession{RCEnabled: true} }},
		{name: "disabled-with-an-index", answer: func([]string) *PlanSession {
			return &PlanSession{RCIndices: []int{0}}
		}},
		{name: "index-one-past-the-end", answer: func(argv []string) *PlanSession {
			return &PlanSession{RCEnabled: true, RCIndices: []int{len(argv)}}
		}},
		{name: "index-far-past-the-end", answer: func(argv []string) *PlanSession {
			return &PlanSession{RCEnabled: true, RCIndices: []int{len(argv) + 40}}
		}},
		{name: "negative-index", answer: func([]string) *PlanSession {
			return &PlanSession{RCEnabled: true, RCIndices: []int{-1}}
		}},
		{name: "duplicate-index-zero", answer: func([]string) *PlanSession {
			return &PlanSession{RCEnabled: true, RCIndices: []int{0, 0}}
		}},
		{name: "duplicate-index-later", answer: func([]string) *PlanSession {
			return &PlanSession{RCEnabled: true, RCIndices: []int{0, 1, 1}}
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			plan, _, err := buildSessionPlannerPlan(t, func(argv []string) (*PlanSession, error) { return row.answer(argv), nil })
			if row.admit {
				if err != nil {
					t.Fatalf("BuildPlan refused a coherent record: %v", err)
				}
				if plan.Session == nil {
					t.Fatal("Plan.Session is nil for a coherent record")
				}
				return
			}
			if !errors.Is(err, ErrPluginContract) {
				t.Fatalf("err = %v, want ErrPluginContract", err)
			}
			if !reflect.DeepEqual(plan, Plan{}) {
				t.Fatalf("a refused BuildPlan returned a non-zero plan: %+v", plan)
			}
		})
	}
}

// TestBuildPlanPropagatesASessionRefusalTyped pins that a plugin's typed
// refusal reaches the caller through BuildPlan wrapped, not swallowed or
// downgraded, and yields no plan.
func TestBuildPlanPropagatesASessionRefusalTyped(t *testing.T) {
	t.Parallel()
	plan, _, err := buildSessionPlannerPlan(t, func([]string) (*PlanSession, error) {
		return nil, &SessionInvalidError{Reason: "witness"}
	})
	if !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("err = %v, want ErrSessionInvalid", err)
	}
	var invalid *SessionInvalidError
	if !errors.As(err, &invalid) || invalid.Reason != "witness" {
		t.Fatalf("err = %v, want the typed *SessionInvalidError carrying its reason", err)
	}
	if !reflect.DeepEqual(plan, Plan{}) {
		t.Fatalf("a refused BuildPlan returned a non-zero plan: %+v", plan)
	}
}

// TestSessionsEqualBindsCollectionPresence pins the F3 class at the
// comparison itself: a nil index slice and an empty one are different records
// (the settings origin is "enabled, present, empty"), so neither equals the
// other, while two empty or two nil slices still do. slices.Equal alone would
// call all four pairs equal.
func TestSessionsEqualBindsCollectionPresence(t *testing.T) {
	t.Parallel()
	empty := &PlanSession{RCEnabled: true, RCIndices: []int{}}
	nilSlice := &PlanSession{RCEnabled: true}
	if sessionsEqual(empty, nilSlice) || sessionsEqual(nilSlice, empty) {
		t.Fatal("an empty index collection equals a nil one; presence is not bound")
	}
	if !sessionsEqual(empty, &PlanSession{RCEnabled: true, RCIndices: []int{}}) || !sessionsEqual(nilSlice, &PlanSession{RCEnabled: true}) {
		t.Fatal("two records with the same collection shape differ")
	}
	if sessionsEqual(nil, empty) || sessionsEqual(empty, nil) || !sessionsEqual(nil, nil) {
		t.Fatal("nil record equality is wrong")
	}
}

// TestCloneSessionKeepsCollectionPresence: a copy must not turn one collection
// shape into the other, or a snapshot taken from an attacker-flattened record
// would bind the flattened shape.
func TestCloneSessionKeepsCollectionPresence(t *testing.T) {
	t.Parallel()
	if got := cloneSession(&PlanSession{}); got.RCIndices != nil {
		t.Fatalf("a nil index slice cloned to %#v", got.RCIndices)
	}
	if got := cloneSession(&PlanSession{RCIndices: []int{}}); got.RCIndices == nil || len(got.RCIndices) != 0 {
		t.Fatalf("an empty index slice cloned to %#v", got.RCIndices)
	}
	if cloneSession(nil) != nil {
		t.Fatal("a nil record cloned to a non-nil one")
	}
}

// TestSessionFillIsIssuedOnlyByThePackage: the zero SessionFill is not issued
// and carries nothing; the one BuildPlan hands a plugin is issued and carries
// the plan's argv, the working directory and the mode.
func TestSessionFillIsIssuedOnlyByThePackage(t *testing.T) {
	t.Parallel()
	if (SessionFill{}).Issued() || len((SessionFill{}).Argv()) != 0 {
		t.Fatal("the zero SessionFill is issued or carries argv")
	}
	_, sys, err := buildSessionPlannerPlan(t, func([]string) (*PlanSession, error) { return nil, nil })
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if sys.seenWorkDir != pangolinRequest().WorkDir {
		t.Errorf("the plugin was handed work dir %q, want the request's %q", sys.seenWorkDir, pangolinRequest().WorkDir)
	}
}

// TestFinalizedVerifierBindsAnAbsentSessionAndRefusesAPresentOne is the
// nil-versus-present half of the in-process binding: a plan finalized with no
// Session (a system with no session surface) refuses a Session attached
// afterwards, exactly as a plan finalized with one refuses its drop. The
// verifier is the unsealed one a Claude-shaped plan carries, with the record
// left nil so presence is the only thing that can differ.
func TestFinalizedVerifierBindsAnAbsentSessionAndRefusesAPresentOne(t *testing.T) {
	t.Parallel()
	base := Plan{System: "probe", Binary: "/bin/probe", Argv: []string{"a"}, execVerifier: unsealedVerifier{}}
	final, err := FinalizePlan(base, FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("the untouched finalized plan refused itself: %v", err)
	}
	for name, attached := range map[string]*PlanSession{
		"present-empty": {RCIndices: []int{}},
		"present-nil":   {},
		"named":         {Name: sessionName("N"), RCIndices: []int{}},
	} {
		forged := final
		forged.Session = attached
		if err := forged.VerifyBeforeExec(); !errors.Is(err, ErrFinalizedProcessChanged) {
			t.Errorf("%s: a Session attached after finalizing with none: err = %v, want ErrFinalizedProcessChanged", name, err)
		}
	}
}
