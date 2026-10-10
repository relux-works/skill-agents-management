// These tests live in the EXTERNAL test package on purpose: package
// claude_test sees exactly what an external consumer sees — the exported
// surface only — so what holds from here holds for any consumer.
package claude_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/internal/gosources"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

func externalSessionRequest(t *testing.T, native []string) agentic.LaunchRequest {
	t.Helper()
	binDir := t.TempDir()
	if err := execfixture.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the stub claude: %v", err)
	}
	workDir := t.TempDir()
	return agentic.LaunchRequest{
		System:     claude.New().ID(),
		Model:      agentic.Model{ID: "probe-model", Effort: agentic.EffortSupportRequired},
		Effort:     "high",
		WorkDir:    workDir,
		Home:       workDir + "/.claude-home",
		Env:        []string{"HOME=/home/agent", "CLAUDECODE=1", "TERM=xterm-256color", "PATH=" + binDir},
		Run:        agentic.RunContext{RunID: "probe-run", TaskID: "TASK-261006-2elwlw"},
		NativeArgs: native,
	}
}

// wantProbeArgv is the argv every probe plan below carries: the module head
// (five tokens) and the native suffix, so the indices are measured against a
// pinned argv.
func wantProbeArgv(native ...string) []string {
	return append([]string{"--model", "probe-model", "--effort", "high", "--disallowedTools=AskUserQuestion"}, native...)
}

// TestExternalIsolatedRegistryYieldsPlanSession is the registry-independence
// regression: NewRegistry -> Register(claude.New()) -> BuildPlan yields
// Plan.Session, with no second wiring call. The same value travels through
// the Default registry the plugin's init populates. Ownership needs no
// registry-side projector table because the plugin fills the record inside
// the registry-built plan.
func TestExternalIsolatedRegistryYieldsPlanSession(t *testing.T) {
	t.Parallel()
	native := []string{"--remote-control", "R"}
	for _, row := range []struct {
		name     string
		registry func(t *testing.T) *agentic.Registry
	}{
		{name: "isolated", registry: func(t *testing.T) *agentic.Registry {
			registry := agentic.NewRegistry()
			if err := registry.Register(claude.New()); err != nil {
				t.Fatalf("Register: %v", err)
			}
			return registry
		}},
		{name: "default", registry: func(*testing.T) *agentic.Registry { return agentic.Default }},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			plan, err := agentic.BuildPlan(row.registry(t), externalSessionRequest(t, native), agentic.LaunchModeInteractive)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			if !reflect.DeepEqual(plan.Argv, wantProbeArgv(native...)) {
				t.Fatalf("plan argv = %#v, want %#v; the index below is measured against this argv", plan.Argv, wantProbeArgv(native...))
			}
			got := plan.Session
			if got == nil {
				t.Fatal("Plan.Session is nil for a Claude plan built through a registry holding the plugin")
			}
			if got.Name == nil || *got.Name != "R" || !got.RCEnabled || !reflect.DeepEqual(got.RCIndices, []int{5}) {
				t.Fatalf("Session = {Name: %v, RCEnabled: %v, RCIndices: %v}, want name R with enabled RC at index 5", got.Name, got.RCEnabled, got.RCIndices)
			}
		})
	}
}

// sessionSealPlan is one plan shape the seal tests attack: the native form
// (RC tokens on the argv, indices non-empty) and the settings origin (RC
// enabled by an explicit settings source, indices EMPTY but present).
type sessionSealPlan struct {
	name    string
	native  []string
	indices []int
}

var sessionSealPlans = []sessionSealPlan{
	{name: "native-rc", native: []string{"-n", "N", "--remote-control", "N"}, indices: []int{7}},
	{name: "settings-origin", native: []string{"-n", "N", "--settings", `{"remoteControlAtStartup":true}`}, indices: []int{}},
}

type sessionSealMutation struct {
	name   string
	only   string // plan shape name; empty applies to both
	mutate func(plan *agentic.Plan)
}

// sessionSealMutations are the narrow members a weaker comparison admits:
// a same-length index rewrite, a name rewrite, and — the F3 class — the
// collection flattened from empty to nil, which slices.Equal alone calls equal —
// and the record dropped altogether.
var sessionSealMutations = []sessionSealMutation{
	{name: "rc-disabled", mutate: func(plan *agentic.Plan) { plan.Session.RCEnabled = false }},
	{name: "index-rewritten-same-length", only: "native-rc", mutate: func(plan *agentic.Plan) { plan.Session.RCIndices[0]++ }},
	{name: "index-appended", mutate: func(plan *agentic.Plan) { plan.Session.RCIndices = append(plan.Session.RCIndices, 0) }},
	{name: "index-collection-empty-to-nil", only: "settings-origin", mutate: func(plan *agentic.Plan) { plan.Session.RCIndices = nil }},
	{name: "index-collection-dropped-to-nil", only: "native-rc", mutate: func(plan *agentic.Plan) { plan.Session.RCIndices = nil }},
	{name: "index-collection-emptied", only: "native-rc", mutate: func(plan *agentic.Plan) { plan.Session.RCIndices = []int{} }},
	{name: "name-rewritten", mutate: func(plan *agentic.Plan) { other := "OTHER"; plan.Session.Name = &other }},
	{name: "name-dropped", mutate: func(plan *agentic.Plan) { plan.Session.Name = nil }},
	{name: "name-forged-empty", mutate: func(plan *agentic.Plan) { empty := ""; plan.Session.Name = &empty }},
	{name: "record-dropped", mutate: func(plan *agentic.Plan) { plan.Session = nil }},
}

func (m sessionSealMutation) applies(shape sessionSealPlan) bool {
	return m.only == "" || m.only == shape.name
}

func freshSessionSealPlan(t *testing.T, shape sessionSealPlan, native ...string) agentic.Plan {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(claude.New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(native) == 0 {
		native = shape.native
	}
	plan, err := agentic.BuildPlan(registry, externalSessionRequest(t, native), agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Session == nil || plan.Session.RCIndices == nil {
		t.Fatalf("Plan.Session = %+v; the seal checks would measure nothing", plan.Session)
	}
	return plan
}

// crossedSeal exports the finalized plan's guard and carries it through JSON:
// the frozen wire, which holds no Session.
func crossedSeal(t *testing.T, final agentic.Plan) agentic.Seal {
	t.Helper()
	exported, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	wire, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("marshal the exported seal: %v", err)
	}
	for _, key := range []string{"RCIndices", "RCEnabled", "rc_indices", "rc_enabled", "session", "Session"} {
		if strings.Contains(string(wire), `"`+key+`"`) {
			t.Fatalf("the frozen guard wire carries a %q key: %s", key, wire)
		}
	}
	var crossed agentic.Seal
	if err := json.Unmarshal(wire, &crossed); err != nil {
		t.Fatalf("unmarshal the exported seal: %v", err)
	}
	return crossed
}

// importedSessionVerifier imports that guard through the plugin the way a
// hosted consumer does.
func importedSessionVerifier(t *testing.T, final agentic.Plan) agentic.ExecPlanVerifier {
	t.Helper()
	verifier, err := agentic.ImportSeal(claude.New(), crossedSeal(t, final))
	if err != nil {
		t.Fatalf("ImportSeal: %v", err)
	}
	return verifier
}

// TestPlanSessionRidesTheExecSeal proves a Session changed after sealing
// refuses in both in-process places it can travel: the base-process snapshot
// FinalizePlan checks and the finalized in-process verifier. The mutations
// cover every field the record binds — RCEnabled, the index values, the index
// collection's presence (empty to nil, nil to empty, dropped), the name
// (rewritten, dropped, forged empty) — and the record dropped altogether. Each
// is applied to a freshly built plan; the control rows prove the unchanged plan
// passes each place, so a gate that refuses everything would not pass. A
// verifier rebuilt by ImportSeal binds no Session at all (it is unverified
// across import): TestImportedSealVerifiesNoSession pins that side.
func TestPlanSessionRidesTheExecSeal(t *testing.T) {
	t.Parallel()
	for _, shape := range sessionSealPlans {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			t.Run("unchanged-finalizes-and-verifies", func(t *testing.T) {
				t.Parallel()
				final, err := agentic.FinalizePlan(freshSessionSealPlan(t, shape), agentic.FinalizeOverlays{}, nil)
				if err != nil {
					t.Fatalf("FinalizePlan: %v", err)
				}
				if err := final.VerifyBeforeExec(); err != nil {
					t.Fatalf("VerifyBeforeExec on the untouched finalized plan: %v", err)
				}
				if final.Session == nil || !final.Session.RCEnabled || !reflect.DeepEqual(final.Session.RCIndices, shape.indices) || (final.Session.RCIndices == nil) {
					t.Fatalf("finalized Session = %+v, want the base record carried through with indices %v", final.Session, shape.indices)
				}
			})
			t.Run("unchanged-passes-the-imported-verifier", func(t *testing.T) {
				t.Parallel()
				final, err := agentic.FinalizePlan(freshSessionSealPlan(t, shape), agentic.FinalizeOverlays{}, nil)
				if err != nil {
					t.Fatalf("FinalizePlan: %v", err)
				}
				if err := importedSessionVerifier(t, final).VerifyBeforeExec(final); err != nil {
					t.Fatalf("an imported verifier refused the unchanged plan: %v", err)
				}
			})
			for _, mutation := range sessionSealMutations {
				if !mutation.applies(shape) {
					continue
				}
				t.Run("before-finalization/"+mutation.name, func(t *testing.T) {
					t.Parallel()
					base := freshSessionSealPlan(t, shape)
					mutation.mutate(&base)
					if _, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
						t.Fatalf("FinalizePlan over a base whose Session changed after sealing: err = %v, want ErrFinalizedProcessChanged", err)
					}
				})
				t.Run("after-finalization/"+mutation.name, func(t *testing.T) {
					t.Parallel()
					final, err := agentic.FinalizePlan(freshSessionSealPlan(t, shape), agentic.FinalizeOverlays{}, nil)
					if err != nil {
						t.Fatalf("FinalizePlan: %v", err)
					}
					mutation.mutate(&final)
					if err := final.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
						t.Fatalf("VerifyBeforeExec over a finalized plan whose Session changed: err = %v, want ErrFinalizedProcessChanged", err)
					}
				})
			}
		})
	}
}

// TestImportedSealVerifiesNoSession pins AC4 across ExportSeal/ImportSeal:
// Session is UNVERIFIED there. The frozen guard wire (payload 1.0.0) carries
// no Session, and the settings-origin remote control depends on inputs outside
// argv, so an imported verifier derives nothing, binds nothing and reads no
// settings. Three facts, per plan shape:
//   - the verifier admits the plan whatever Session it carries (the original,
//     every mutation of the in-process table, and none): Session is not one of
//     its inputs, and no admission from it says anything about the Session;
//   - the verifier reads neither current settings nor the plan's WorkDir: the
//     settings source vanishing, or the verification directory moving, changes
//     no verdict (a re-derivation would refuse or differ);
//   - the facts the wire does carry stay bound.
func TestImportedSealVerifiesNoSession(t *testing.T) {
	t.Parallel()
	for _, shape := range sessionSealPlans {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			final, err := agentic.FinalizePlan(freshSessionSealPlan(t, shape), agentic.FinalizeOverlays{}, nil)
			if err != nil {
				t.Fatalf("FinalizePlan: %v", err)
			}
			verifier := importedSessionVerifier(t, final)
			for _, mutation := range sessionSealMutations {
				if !mutation.applies(shape) {
					continue
				}
				changed := final
				clone := *final.Session
				clone.RCIndices = slices.Clone(final.Session.RCIndices)
				changed.Session = &clone
				mutation.mutate(&changed)
				if err := verifier.VerifyBeforeExec(changed); err != nil {
					t.Errorf("%s: the imported verifier refused over a Session it does not verify: %v", mutation.name, err)
				}
			}
			moved := final
			moved.WorkDir = t.TempDir()
			if err := verifier.VerifyBeforeExec(moved); err != nil {
				t.Errorf("the imported verifier depends on the verification WorkDir: %v", err)
			}
			bare := agentic.Plan{Binary: final.Binary, Argv: final.Argv, Env: final.Env}
			if err := verifier.VerifyBeforeExec(bare); err != nil {
				t.Errorf("a process rebuilt from the wire facts alone was refused: %v", err)
			}
			bare.Env = append(bare.Env, "EXTRA=changed")
			if err := verifier.VerifyBeforeExec(bare); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Errorf("the facts the wire does carry stopped being bound: err = %v", err)
			}
		})
	}
}

// TestImportedSealReadsNoCurrentSettings is the F4 regression: the settings
// source that carried the remote-control intent at build time is deleted (and
// then rewritten to disable it) after the guard crossed the wire; an imported
// verifier must neither refuse (a vanished source is not a new refusal) nor
// change its verdict, because it reads no settings.
func TestImportedSealReadsNoCurrentSettings(t *testing.T) {
	t.Parallel()
	req := externalSessionRequest(t, []string{"--settings", "rc.json"})
	settingsPath := filepath.Join(req.WorkDir, "rc.json")
	if err := os.WriteFile(settingsPath, []byte(`{"remoteControlAtStartup":true}`), 0o600); err != nil {
		t.Fatalf("writing the settings source: %v", err)
	}
	registry := agentic.NewRegistry()
	if err := registry.Register(claude.New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	plan, err := agentic.BuildPlan(registry, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Session == nil || !plan.Session.RCEnabled || plan.Session.RCIndices == nil || len(plan.Session.RCIndices) != 0 {
		t.Fatalf("Session = %+v, want the settings origin (enabled, empty but present indices)", plan.Session)
	}
	final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	verifier := importedSessionVerifier(t, final)
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatalf("unchanged plan with its settings source present: %v", err)
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatalf("removing the settings source: %v", err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatalf("verification after the settings source vanished: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"remoteControlAtStartup":false}`), 0o600); err != nil {
		t.Fatalf("rewriting the settings source: %v", err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatalf("verification after the settings source was rewritten: %v", err)
	}
	// The in-process seal has no such dependency either: it compares the
	// record sealed at build time, so the same plan still verifies.
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("in-process verification after the settings source changed: %v", err)
	}
}

// TestImportedPlanNeverYieldsAVerifiedSession is the AC4 consumer-side proof:
// the plan an imported process rebuilds (ImportedProcess.Process) carries no
// Session, even when the process handed to NewImportedProcess carried one, and
// so does the plan after a verification. A consumer therefore cannot read a
// Session off an imported plan and mistake it for verified; one it needs
// across a process boundary it must carry itself, as unverified.
func TestImportedPlanNeverYieldsAVerifiedSession(t *testing.T) {
	t.Parallel()
	for _, shape := range sessionSealPlans {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			final, err := agentic.FinalizePlan(freshSessionSealPlan(t, shape), agentic.FinalizeOverlays{}, nil)
			if err != nil {
				t.Fatalf("FinalizePlan: %v", err)
			}
			if final.Session == nil {
				t.Fatal("the in-process plan lost its Session; the import row would measure nothing")
			}
			process, err := agentic.NewImportedProcess(claude.New(), crossedSeal(t, final), final)
			if err != nil {
				t.Fatalf("NewImportedProcess over a process carrying a Session: %v", err)
			}
			imported := process.Process()
			if imported.Session != nil {
				t.Fatalf("the imported plan carries Session %+v; across import it is unverified and must be dropped", imported.Session)
			}
			if err := process.VerifyBeforeExec(imported); err != nil {
				t.Fatalf("the imported process refused its own rebuilt plan: %v", err)
			}
			if again := process.Process(); again.Session != nil {
				t.Fatalf("the imported plan carries Session %+v after a verification", again.Session)
			}
		})
	}
}

// TestPlanSessionFollowsANativeTailThroughTheSeal pins the finalization side:
// a native tail extends the sealed argv, so the record is derived again over
// the FINAL argv — otherwise Session would describe an argv the plan no longer
// carries. The tail's RC index is measured against the final argv, and the
// finalized in-process verifier binds that re-derived record; a contradicting
// tail refuses typed at FinalizePlan.
func TestPlanSessionFollowsANativeTailThroughTheSeal(t *testing.T) {
	t.Parallel()
	silent := sessionSealPlan{name: "silent", native: []string{"-n", "N"}}
	t.Run("tail-adds-rc-and-the-seal-binds-the-final-record", func(t *testing.T) {
		t.Parallel()
		final, err := agentic.FinalizePlan(freshSessionSealPlan(t, silent), agentic.FinalizeOverlays{NativeTail: []string{"--remote-control"}}, nil)
		if err != nil {
			t.Fatalf("FinalizePlan: %v", err)
		}
		if final.Session == nil || !final.Session.RCEnabled || !reflect.DeepEqual(final.Session.RCIndices, []int{len(final.Argv) - 1}) || final.Argv[len(final.Argv)-1] != "--remote-control" {
			t.Fatalf("finalized Session = %+v over argv %#v, want RC enabled at the tail's own position", final.Session, final.Argv)
		}
		if err := final.VerifyBeforeExec(); err != nil {
			t.Fatalf("VerifyBeforeExec: %v", err)
		}
		// The stale base record (RC disabled) no longer describes the final argv.
		stale := final
		stale.Session = &agentic.PlanSession{Name: final.Session.Name, RCIndices: []int{}}
		if err := stale.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
			t.Fatalf("the finalized verifier admitted the pre-tail record over the final argv: err = %v", err)
		}
	})
	t.Run("tail-renames-the-session", func(t *testing.T) {
		t.Parallel()
		final, err := agentic.FinalizePlan(freshSessionSealPlan(t, sessionSealPlans[0], "--remote-control", "N"), agentic.FinalizeOverlays{NativeTail: []string{"--name", "N"}}, nil)
		if err != nil {
			t.Fatalf("FinalizePlan: %v", err)
		}
		if final.Session.Name == nil || *final.Session.Name != "N" {
			t.Fatalf("finalized Session name = %v, want N from the tail", final.Session.Name)
		}
		if err := final.VerifyBeforeExec(); err != nil {
			t.Fatalf("VerifyBeforeExec: %v", err)
		}
	})
	t.Run("contradicting-tail-refuses-typed", func(t *testing.T) {
		t.Parallel()
		_, err := agentic.FinalizePlan(freshSessionSealPlan(t, silent), agentic.FinalizeOverlays{NativeTail: []string{"-n", "OTHER"}}, nil)
		if !errors.Is(err, agentic.ErrSessionInvalid) {
			t.Fatalf("FinalizePlan over a tail repeating the name selector: err = %v, want ErrSessionInvalid", err)
		}
	})
}

// bannedSessionSurface names the declarations of the retired projector API.
// Their return would re-open the separate surface this design removed: a
// registry-held projector reachable with a plan the owning grammar never
// built.
var bannedSessionSurface = map[string]bool{
	"ProjectSession":                    true,
	"RegisterSessionProjector":          true,
	"SessionProjector":                  true,
	"SessionProjectorFunc":              true,
	"SessionProjection":                 true,
	"SessionSettings":                   true,
	"ErrSessionProjectionInvalid":       true,
	"ErrSessionProjectionNotApplicable": true,
	"ErrNilSessionProjector":            true,
	"ErrDuplicateSessionProjector":      true,
}

// TestNoSessionProjectorSurfaceRemains holds the removal half of the design
// from both sides. Source side: no declaration of any retired name — func,
// method, type, var or const — exists in any compiled source of the module;
// the scan parses, so a comment spelling a name declares nothing and a
// declaration is found whatever its shape. Behavior side: the plugin's
// exported method set carries exactly one session-flavored method,
// FillPlanSession, and the registry exposes no session-flavored method at
// all (the single table is keyed by system id and holds no projector).
func TestNoSessionProjectorSurfaceRemains(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate this test file")
	}
	root, err := gosources.Root(filepath.Dir(file))
	if err != nil {
		t.Fatalf("module root: %v", err)
	}
	sources, err := gosources.Walk(root)
	if err != nil {
		t.Fatalf("walk module sources: %v", err)
	}
	t.Run("no-retired-declaration-in-any-module-source", func(t *testing.T) {
		t.Parallel()
		if found := retiredSessionDecls(t, root, sources); len(found) > 0 {
			t.Errorf("retired session projector surface declared again: %s", strings.Join(found, "; "))
		}
	})
	t.Run("scan-sees-every-declaration-shape-and-ignores-comments", func(t *testing.T) {
		t.Parallel()
		fixture := t.TempDir()
		writeExternalFixture(t, fixture, "a/a.go", "package a\n\n// ProjectSession is only prose here.\n\nfunc (*S) ProjectSession() {}\n\ntype S struct{}\n")
		writeExternalFixture(t, fixture, "a/b.go", "package a\n\nfunc RegisterSessionProjector() {}\n\ntype SessionProjection struct{}\n\nvar ErrNilSessionProjector error\n\nconst SessionSettings = 1\n")
		walked, err := gosources.Walk(fixture)
		if err != nil {
			t.Fatalf("walk fixture: %v", err)
		}
		got := retiredSessionDecls(t, fixture, walked)
		if len(got) != 5 {
			t.Fatalf("flagged %q, want the method, func, type, var and const with the comment ignored", got)
		}
	})
	t.Run("plugin-method-set-has-only-the-plan-fill", func(t *testing.T) {
		t.Parallel()
		methods := reflect.TypeOf(claude.New())
		if _, found := methods.MethodByName("ID"); !found {
			t.Fatal("(*claude.System) exposes no ID method; the probe below would measure nothing")
		}
		var sessionMethods []string
		for i := 0; i < methods.NumMethod(); i++ {
			if name := methods.Method(i).Name; strings.Contains(name, "Session") {
				sessionMethods = append(sessionMethods, name)
			}
		}
		sort.Strings(sessionMethods)
		if !reflect.DeepEqual(sessionMethods, []string{"FillPlanSession"}) {
			t.Fatalf("session-flavored plugin methods = %v, want exactly [FillPlanSession]", sessionMethods)
		}
	})
	t.Run("registry-exposes-no-session-method", func(t *testing.T) {
		t.Parallel()
		methods := reflect.TypeOf(agentic.NewRegistry())
		if _, found := methods.MethodByName("Lookup"); !found {
			t.Fatal("(*agentic.Registry) exposes no Lookup method; the probe below would measure nothing")
		}
		for i := 0; i < methods.NumMethod(); i++ {
			if name := methods.Method(i).Name; strings.Contains(name, "Session") {
				t.Errorf("(*agentic.Registry) exposes %s; the registry holds no session binding", name)
			}
		}
	})
}

// retiredSessionDecls lists every declaration of a retired name in the
// walked sources, each as "path (name)". It fails the test when the walk
// reached no source at all, so a scan that measured nothing cannot report
// clean.
func retiredSessionDecls(t *testing.T, root string, sources map[string]string) []string {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("the source walk reached no file, so this surface scan measured nothing")
	}
	var found []string
	for path, content := range sources {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), content, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		note := func(name string) {
			if bannedSessionSurface[name] {
				found = append(found, path+" ("+name+")")
			}
		}
		for _, declaration := range parsed.Decls {
			switch decl := declaration.(type) {
			case *ast.FuncDecl:
				note(decl.Name.Name)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						note(spec.Name.Name)
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							note(name.Name)
						}
					}
				}
			}
		}
	}
	sort.Strings(found)
	return found
}

func writeExternalFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// TestNoExportedSessionDerivationFromCallerArgv holds AC2 from the outside:
// registry registration is the trust boundary, so no exported or
// package-reachable function lets a caller derive a Claude Session from argv
// the caller supplies, or for a plan the bound plugin did not build. The
// exported surface is driven three ways: behaviorally (the one exported
// derivation method, handed the only value an outside caller can build, gives
// nothing), by reflection (its signature takes nothing a caller can fill),
// and by a parsed census of every compiled source in the module.
func TestNoExportedSessionDerivationFromCallerArgv(t *testing.T) {
	t.Parallel()
	t.Run("a-fill-no-caller-can-issue-derives-nothing", func(t *testing.T) {
		t.Parallel()
		var planner agentic.SessionPlanner = claude.New()
		// The zero SessionFill is the only one an outside caller can build:
		// the type has no exported field and no constructor.
		got, err := planner.FillPlanSession(agentic.SessionFill{})
		if !errors.Is(err, agentic.ErrSessionInvalid) || got != nil {
			t.Fatalf("FillPlanSession(zero fill) = %+v, %v; want nil and ErrSessionInvalid", got, err)
		}
		if (agentic.SessionFill{}).Issued() {
			t.Fatal("a zero SessionFill reports itself issued")
		}
	})
	t.Run("the-derivation-signature-takes-no-argv-a-caller-can-fill", func(t *testing.T) {
		t.Parallel()
		method, found := reflect.TypeOf(claude.New()).MethodByName("FillPlanSession")
		if !found {
			t.Fatal("(*claude.System) has no FillPlanSession; the probe would measure nothing")
		}
		fillType := reflect.TypeOf(agentic.SessionFill{})
		if method.Type.NumIn() != 2 || method.Type.In(1) != fillType {
			t.Fatalf("FillPlanSession signature = %v, want exactly one parameter of type agentic.SessionFill", method.Type)
		}
		for i := 0; i < fillType.NumField(); i++ {
			if field := fillType.Field(i); field.IsExported() {
				t.Errorf("agentic.SessionFill exports field %s; a caller could set the argv through it", field.Name)
			}
		}
		// No exported constructor-shaped method either: every exported method
		// of SessionFill must be a reader (no parameters).
		for i := 0; i < fillType.NumMethod(); i++ {
			if m := fillType.Method(i); m.Type.NumIn() != 1 {
				t.Errorf("agentic.SessionFill.%s takes parameters; a SessionFill method could write into the fill", m.Name)
			}
		}
	})
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate this test file")
	}
	root, err := gosources.Root(filepath.Dir(file))
	if err != nil {
		t.Fatalf("module root: %v", err)
	}
	t.Run("no-exported-function-in-any-module-source-derives-a-session-from-argv", func(t *testing.T) {
		t.Parallel()
		sources, err := gosources.Walk(root)
		if err != nil {
			t.Fatalf("walk module sources: %v", err)
		}
		if found := foreignSessionDerivers(t, root, sources); len(found) > 0 {
			t.Errorf("exported function derives a PlanSession from caller-supplied input: %s", strings.Join(found, "; "))
		}
		// The census must have seen the one legitimate shape, or it measured nothing.
		if !sawSessionPlannerShape(t, root, sources) {
			t.Fatal("the census never saw the FillPlanSession(SessionFill) shape; it parsed nothing relevant")
		}
	})
	t.Run("the-census-flags-every-foreign-shape-and-spares-the-fill", func(t *testing.T) {
		t.Parallel()
		fixture := t.TempDir()
		writeExternalFixture(t, fixture, "a/a.go", `package a

import "x/agentic"

func FillPlanSessionFromArgv(argv []string) (*agentic.PlanSession, error) { return nil, nil }

func (*S) Derive(req agentic.LaunchRequest) (agentic.PlanSession, error) { return agentic.PlanSession{}, nil }

func (*S) FromPlan(p agentic.Plan) (*agentic.PlanSession, error) { return nil, nil }

type S struct{}

type Deriver interface {
	Session(argv []string) (*agentic.PlanSession, error)
}
`)
		writeExternalFixture(t, fixture, "a/b.go", `package a

import "x/agentic"

func fillUnexported(argv []string) (*agentic.PlanSession, error) { return nil, nil }

func (*S) FillPlanSession(fill agentic.SessionFill) (*agentic.PlanSession, error) { return nil, nil }

type Planner interface {
	FillPlanSession(fill agentic.SessionFill) (*agentic.PlanSession, error)
}
`)
		walked, err := gosources.Walk(fixture)
		if err != nil {
			t.Fatalf("walk fixture: %v", err)
		}
		got := foreignSessionDerivers(t, fixture, walked)
		if len(got) != 4 {
			t.Fatalf("flagged %q, want the func, the two methods and the interface method; the unexported helper and the SessionFill shapes are spared", got)
		}
	})
}

// foreignSessionDerivers lists every exported function, exported method and
// exported interface method whose results mention a PlanSession and whose
// parameters are anything other than a single SessionFill. Such a signature
// takes caller-supplied input (argv, a request, a plan) and answers a
// session: the foreign-argv path the trust boundary forbids.
func foreignSessionDerivers(t *testing.T, root string, sources map[string]string) []string {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("the source walk reached no file, so this census measured nothing")
	}
	var found []string
	for path, content := range sources {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), content, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		check := func(name string, signature *ast.FuncType) {
			if !ast.IsExported(name) || !mentionsPlanSession(signature.Results) {
				return
			}
			if !takesOnlyASessionFill(signature.Params) {
				found = append(found, path+" ("+name+")")
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				check(node.Name.Name, node.Type)
			case *ast.InterfaceType:
				for _, method := range node.Methods.List {
					if signature, isFunc := method.Type.(*ast.FuncType); isFunc && len(method.Names) == 1 {
						check(method.Names[0].Name, signature)
					}
				}
			}
			return true
		})
	}
	sort.Strings(found)
	return found
}

func sawSessionPlannerShape(t *testing.T, root string, sources map[string]string) bool {
	t.Helper()
	for path, content := range sources {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), content, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		saw := false
		ast.Inspect(parsed, func(node ast.Node) bool {
			if decl, isFunc := node.(*ast.FuncDecl); isFunc && decl.Name.Name == "FillPlanSession" && mentionsPlanSession(decl.Type.Results) && takesOnlyASessionFill(decl.Type.Params) {
				saw = true
			}
			return !saw
		})
		if saw {
			return true
		}
	}
	return false
}

func mentionsPlanSession(fields *ast.FieldList) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		if strings.Contains(types.ExprString(field.Type), "PlanSession") {
			return true
		}
	}
	return false
}

func takesOnlyASessionFill(fields *ast.FieldList) bool {
	if fields == nil || len(fields.List) != 1 || len(fields.List[0].Names) > 1 {
		return false
	}
	return strings.HasSuffix(types.ExprString(fields.List[0].Type), "SessionFill")
}
