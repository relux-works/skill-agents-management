package claude

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// sessionNameValue boxes a want-name for the tables. A nil *string wants no
// name; a non-nil one wants exactly that value.
func sessionNameValue(name string) *string { return &name }

// sessionArgvPrefix is the module-built head of every interactive test argv
// below: model, effort and the unconditional AskUserQuestion denial. Native
// selectors start at index 5, and every row pins the full argv so that offset
// cannot drift without the test saying so.
func sessionArgvPrefix() []string {
	return []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}
}

// buildSessionPlan drives the production entry point — BuildPlan over an
// isolated registry holding only the plugin — and returns the refusal
// instead of failing, so the same helper serves the admitted rows and the
// refused ones. The stub binary keeps resolution on a layout this test built.
func buildSessionPlan(t *testing.T, workDir string, native []string) (agentic.Plan, error) {
	t.Helper()
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req := interactiveRequest(workDir)
	req.NativeArgs = native
	req.Env = append(req.Env, "PATH="+binDir)
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return agentic.BuildPlan(registry, req, agentic.LaunchModeInteractive)
}

// TestPlanSessionCoversNameAndRemoteControlForms drives every argv form
// through BuildPlan: the RC/name selectors ride the caller's verbatim native
// suffix and the plugin fills Plan.Session from the argv it composed. Each
// row pins the full argv first, then the name, the RC intent and the exact
// indices — and cross-checks every index against the token it points at in
// the SAME plan's Argv, for every form.
func TestPlanSessionCoversNameAndRemoteControlForms(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name        string
		native      []string
		wantName    *string
		wantRC      bool
		wantIndices []int
	}{
		{name: "rc-separate-value", native: []string{"--remote-control", "RCNAME"}, wantName: sessionNameValue("RCNAME"), wantRC: true, wantIndices: []int{5}},
		{name: "rc-equals-value", native: []string{"--remote-control=RCNAME"}, wantName: sessionNameValue("RCNAME"), wantRC: true, wantIndices: []int{5}},
		{name: "rc-bare", native: []string{"--remote-control"}, wantRC: true, wantIndices: []int{5}},
		{name: "rc-alias-separate-value", native: []string{"--rc", "RCNAME"}, wantName: sessionNameValue("RCNAME"), wantRC: true, wantIndices: []int{5}},
		{name: "rc-alias-equals-value", native: []string{"--rc=RCNAME"}, wantName: sessionNameValue("RCNAME"), wantRC: true, wantIndices: []int{5}},
		{name: "rc-alias-bare", native: []string{"--rc"}, wantRC: true, wantIndices: []int{5}},
		{name: "name-separate-value", native: []string{"--name", "NAME"}, wantName: sessionNameValue("NAME"), wantIndices: []int{}},
		{name: "name-equals-value", native: []string{"--name=NAME"}, wantName: sessionNameValue("NAME"), wantIndices: []int{}},
		{name: "name-short-separate-value", native: []string{"-n", "NAME"}, wantName: sessionNameValue("NAME"), wantIndices: []int{}},
		{name: "name-short-attached-value", native: []string{"-nNAME"}, wantName: sessionNameValue("NAME"), wantIndices: []int{}},
		{name: "name-empty-value-is-a-name", native: []string{"--name="}, wantName: sessionNameValue(""), wantIndices: []int{}},
		// The launcher's contract-example tail: two channels naming one
		// session corroborate it. Only the RC token's index is reported —
		// the name selector's position is not an RC index.
		{name: "name-and-rc-agree", native: []string{"-n", "NAME", "--remote-control", "NAME"}, wantName: sessionNameValue("NAME"), wantRC: true, wantIndices: []int{7}},
		{name: "rc-prefix-separate-value", native: []string{"--remote-control-session-name-prefix", "P"}, wantRC: true, wantIndices: []int{5}},
		{name: "rc-prefix-equals-value", native: []string{"--remote-control-session-name-prefix=P"}, wantRC: true, wantIndices: []int{5}},
		{name: "rc-and-prefix", native: []string{"--remote-control", "--remote-control-session-name-prefix", "P"}, wantRC: true, wantIndices: []int{5, 6}},
		{name: "rc-disabled", native: nil, wantIndices: []int{}},
		// Grammar-owned edges, not second-parser rules: the optional RC
		// value stops at flags, the required name owns even a dash-leading
		// token, and everything after `--` is prompt text.
		{name: "rc-optional-stops-at-flag", native: []string{"--remote-control", "--verbose"}, wantRC: true, wantIndices: []int{5}},
		{name: "rc-optional-stops-at-separator", native: []string{"--remote-control", "--", "prompt"}, wantRC: true, wantIndices: []int{5}},
		{name: "name-required-owns-dash-leading-value", native: []string{"--name", "--verbose"}, wantName: sessionNameValue("--verbose"), wantIndices: []int{}},
		{name: "selectors-after-separator-are-prompt-text", native: []string{"--", "--remote-control", "X"}, wantIndices: []int{}},
		{name: "unknown-options-are-out-of-contract", native: []string{"--future-flag", "--remote-control", "R"}, wantName: sessionNameValue("R"), wantRC: true, wantIndices: []int{6}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			plan, err := buildSessionPlan(t, tempSlot(t), row.native)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			wantArgv := append(sessionArgvPrefix(), row.native...)
			if !reflect.DeepEqual(plan.Argv, wantArgv) {
				t.Fatalf("plan argv = %#v, want %#v; the indices below are measured against this argv", plan.Argv, wantArgv)
			}
			assertPlanSession(t, plan, row.wantName, row.wantRC, row.wantIndices)
			assertIndicesNameRCTokens(t, plan)
		})
	}
}

// TestPlanSessionExecModePlanCarriesTheDisabledRecord pins that the record is
// not an interactive-only artifact: an exec plan has no session selectors and
// no native suffix, so Claude still answers a present, disabled, empty-index
// record rather than leaving Session nil (nil is reserved for systems with no
// session surface at all).
func TestPlanSessionExecModePlanCarriesTheDisabledRecord(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req := interactiveRequest(workDir)
	req.Env = append(req.Env, "PATH="+binDir)
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		plan, err := agentic.BuildPlan(registry, req, mode)
		if err != nil {
			t.Fatalf("BuildPlan(%s): %v", mode, err)
		}
		assertPlanSession(t, plan, nil, false, []int{})
	}
}

// TestPlanSessionSettingsOrigin covers the settings half of the contract
// through BuildPlan: an explicit --settings source (inline JSON or a file
// under the launch cwd) that enables remote control yields RC enabled with
// EMPTY indices, because no argv token carries it; an explicit false or an
// absent key stays disabled; argv RC beside a settings enablement keeps its
// argv indices; and a key that is not a boolean refuses typed rather than
// reading as absence.
func TestPlanSessionSettingsOrigin(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	if err := os.WriteFile(filepath.Join(workDir, "rc-on.json"), []byte(`{"remoteControlAtStartup": true}`), 0o600); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "rc-off.json"), []byte(`{"remoteControlAtStartup": false}`), 0o600); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	for _, row := range []struct {
		name        string
		native      []string
		wantName    *string
		wantRC      bool
		wantIndices []int
	}{
		{name: "inline-settings-rc-enabled", native: []string{"--settings", `{"remoteControlAtStartup":true}`}, wantRC: true, wantIndices: []int{}},
		{name: "inline-settings-equals-form", native: []string{`--settings={"remoteControlAtStartup":true}`}, wantRC: true, wantIndices: []int{}},
		{name: "file-settings-rc-enabled", native: []string{"--settings", "rc-on.json"}, wantRC: true, wantIndices: []int{}},
		{name: "settings-rc-with-argv-name", native: []string{"--settings", "rc-on.json", "--name", "N"}, wantName: sessionNameValue("N"), wantRC: true, wantIndices: []int{}},
		{name: "settings-rc-false", native: []string{"--settings", `{"remoteControlAtStartup":false}`}, wantIndices: []int{}},
		{name: "file-settings-rc-false", native: []string{"--settings", "rc-off.json"}, wantIndices: []int{}},
		{name: "settings-without-the-key", native: []string{"--settings", `{"model":"x"}`}, wantIndices: []int{}},
		{name: "settings-rc-beside-argv-rc", native: []string{"--settings", "rc-on.json", "--remote-control"}, wantRC: true, wantIndices: []int{7}},
		{name: "settings-false-beside-argv-rc", native: []string{"--settings", "rc-off.json", "--rc"}, wantRC: true, wantIndices: []int{7}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			plan, err := buildSessionPlan(t, workDir, row.native)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			assertPlanSession(t, plan, row.wantName, row.wantRC, row.wantIndices)
			assertIndicesNameRCTokens(t, plan)
		})
	}
	for _, row := range []struct {
		name   string
		native []string
	}{
		{name: "settings-rc-string", native: []string{"--settings", `{"remoteControlAtStartup":"true"}`}},
		{name: "settings-rc-number", native: []string{"--settings", `{"remoteControlAtStartup":1}`}},
		{name: "settings-rc-null", native: []string{"--settings", `{"remoteControlAtStartup":null}`}},
		{name: "settings-rc-object", native: []string{"--settings", `{"remoteControlAtStartup":{}}`}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			plan, err := buildSessionPlan(t, workDir, row.native)
			assertSessionInvalid(t, err, "settings remote-control key is not a boolean")
			if !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("a refused BuildPlan returned a non-zero plan: %+v", plan)
			}
		})
	}
}

// TestPlanSessionRefusesAnUnreadableSettingsSourceItConsults reaches the one
// refusal the tool policy cannot pre-empt: the policy read the source while
// Args ran, and a source that stops being readable before the session fill
// consults it again must refuse typed, never read as "RC not enabled". It
// calls the unexported derivation directly — BuildPlan cannot produce the
// second failed read deterministically — and is the only direct call in this
// file; no exported function can be given argv, which the external suite pins.
func TestPlanSessionRefusesAnUnreadableSettingsSourceItConsults(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	req.NativeArgs = []string{"--settings", "gone.json"}
	argv := append(sessionArgvPrefix(), req.NativeArgs...)
	got, err := deriveSession(argv, workDir)
	assertSessionInvalid(t, err, "explicit settings source is unreadable")
	if got != nil {
		t.Fatalf("deriveSession returned %+v beside the refusal, want nil", got)
	}
	// With an RC token on the argv the source is not needed and not read.
	req.NativeArgs = []string{"--settings", "gone.json", "--remote-control"}
	argv = append(sessionArgvPrefix(), req.NativeArgs...)
	if got, err = deriveSession(argv, workDir); err != nil || got == nil || !got.RCEnabled || !reflect.DeepEqual(got.RCIndices, []int{7}) {
		t.Fatalf("deriveSession = %+v, %v; want RC enabled at index 7 without consulting the source", got, err)
	}
}

// TestPlanSessionRefusesDuplicatesConflictsAndDanglingSelectors drives every
// refusal through BuildPlan: repeated selector classes (in either spelling,
// even with equal values), names that disagree across classes, and required
// selectors dangling without a value. Triple repeats and second-position
// danglers pin the narrowing — a gate weakened to admit exactly the witness
// still refuses them.
func TestPlanSessionRefusesDuplicatesConflictsAndDanglingSelectors(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name       string
		native     []string
		wantReason string
	}{
		{name: "duplicate-name", native: []string{"--name", "A", "--name", "A"}, wantReason: "duplicate native name selector"},
		{name: "duplicate-name-mixed-spelling", native: []string{"--name", "A", "-n", "A"}, wantReason: "duplicate native name selector"},
		{name: "duplicate-name-triple", native: []string{"--name", "A", "-n", "A", "--name", "A"}, wantReason: "duplicate native name selector"},
		{name: "duplicate-rc", native: []string{"--remote-control", "--rc"}, wantReason: "duplicate remote-control selector"},
		{name: "duplicate-rc-valued", native: []string{"--rc", "A", "--remote-control", "A"}, wantReason: "duplicate remote-control selector"},
		{name: "duplicate-rc-triple", native: []string{"--rc", "--remote-control", "--rc"}, wantReason: "duplicate remote-control selector"},
		{name: "duplicate-prefix", native: []string{"--remote-control-session-name-prefix", "P", "--remote-control-session-name-prefix", "P"}, wantReason: "duplicate remote-control name-prefix selector"},
		{name: "duplicate-prefix-triple", native: []string{"--remote-control-session-name-prefix", "P", "--remote-control-session-name-prefix", "P", "--remote-control-session-name-prefix", "P"}, wantReason: "duplicate remote-control name-prefix selector"},
		{name: "conflicting-names-witness", native: []string{"-n", "FIRST-NAME", "--remote-control", "WITNESS-SECOND-NAME"}, wantReason: "conflicting native session names"},
		{name: "conflicting-names-again", native: []string{"--name", "A", "--rc", "B"}, wantReason: "conflicting native session names"},
		{name: "dangling-name", native: []string{"--name"}, wantReason: "native name selector without a value"},
		{name: "dangling-name-short", native: []string{"-n"}, wantReason: "native name selector without a value"},
		{name: "dangling-name-second-position", native: []string{"--verbose", "--name"}, wantReason: "native name selector without a value"},
		{name: "dangling-prefix", native: []string{"--remote-control-session-name-prefix"}, wantReason: "remote-control name-prefix selector without a value"},
		{name: "dangling-prefix-second-position", native: []string{"--verbose", "--remote-control-session-name-prefix"}, wantReason: "remote-control name-prefix selector without a value"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			plan, err := buildSessionPlan(t, tempSlot(t), row.native)
			assertSessionInvalid(t, err, row.wantReason)
			if !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("a refused BuildPlan returned a non-zero plan: %+v", plan)
			}
		})
	}
}

// assertPlanSession pins the whole record on a plan: present, the name (nil
// exactly when none), the RC intent, the exact indices, the non-nil slice and
// the never-disabled-with-indices invariant.
func assertPlanSession(t *testing.T, plan agentic.Plan, wantName *string, wantRC bool, wantIndices []int) {
	t.Helper()
	got := plan.Session
	if got == nil {
		t.Fatalf("Plan.Session is nil for a Claude plan, want the record the plugin fills during BuildPlan")
	}
	if (got.Name == nil) != (wantName == nil) || (got.Name != nil && *got.Name != *wantName) {
		t.Errorf("Name = %v, want %v", strOrNull(got.Name), strOrNull(wantName))
	}
	if got.RCEnabled != wantRC {
		t.Errorf("RCEnabled = %v, want %v", got.RCEnabled, wantRC)
	}
	if !reflect.DeepEqual(got.RCIndices, wantIndices) {
		t.Errorf("RCIndices = %v, want the exact RC argv indices %v", got.RCIndices, wantIndices)
	}
	if got.RCIndices == nil {
		t.Errorf("RCIndices is nil, want a non-nil slice (empty when the intent is settings-origin or absent)")
	}
	if !got.RCEnabled && len(got.RCIndices) != 0 {
		t.Errorf("disabled remote control carries no indices, got %v", got.RCIndices)
	}
}

// assertIndicesNameRCTokens cross-checks each reported index against the
// token it points at in the same plan's Argv. This is a cross-check, not a
// re-parse: it reads the token and checks its spelling.
func assertIndicesNameRCTokens(t *testing.T, plan agentic.Plan) {
	t.Helper()
	for _, index := range plan.Session.RCIndices {
		if index < 0 || index >= len(plan.Argv) {
			t.Errorf("RCIndices carries out-of-bounds index %d for an argv of length %d", index, len(plan.Argv))
			continue
		}
		base, _, _ := strings.Cut(plan.Argv[index], "=")
		switch base {
		case remoteControlFlag, remoteControlAliasFlag, remoteControlPrefixFlag:
		default:
			t.Errorf("RCIndices points at %q, not an RC-family token; indices must name RC tokens exactly", plan.Argv[index])
		}
	}
}

// assertSessionInvalid pins a refusal: the invalid sentinel plus the typed
// reason, which names selector spellings and never caller names.
func assertSessionInvalid(t *testing.T, err error, wantReason string) {
	t.Helper()
	if !errors.Is(err, agentic.ErrSessionInvalid) {
		t.Fatalf("err = %v, want ErrSessionInvalid", err)
	}
	var invalid *agentic.SessionInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want the typed *SessionInvalidError", err)
	}
	if invalid.Reason != wantReason {
		t.Errorf("Reason = %q, want %q", invalid.Reason, wantReason)
	}
}

func strOrNull(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}
