package agentic

import (
	"errors"
	"fmt"
	"testing"
)

// effortAdmission is a caller that holds NOTHING but the contract types: no
// registry, no plugin, no vendor, no admission pass. AC4 asks whether the
// contract exposes enough for such a caller to refuse a required-effort model
// under a system that cannot carry effort, and this function is the answer —
// it compiles against Capabilities and Model alone.
//
// The refusal that ships lives with admission, later. What has to be true now
// is that the decision is derivable from the declaration, and it is.
func effortAdmission(system SystemID, caps Capabilities, model Model) error {
	if caps.EffortTransport.CanCarry(model.Effort) {
		return nil
	}
	return fmt.Errorf("model %q requires a reasoning effort and system %q declares effort transport %s",
		model.ID, system, caps.EffortTransport)
}

func TestEffortTransportIsDecidableFromTheContractAlone(t *testing.T) {
	required := Model{ID: "pangolin-thinker", Effort: EffortSupportRequired}
	effortless := Model{ID: "pangolin-flat", Effort: EffortSupportNone}

	cases := []struct {
		transport EffortTransport
		model     Model
		wantErr   bool
	}{
		{EffortTransportNone, required, true},
		{EffortTransportNone, effortless, false},
		{EffortTransportArgv, required, false},
		{EffortTransportArgv, effortless, false},
		{EffortTransportStdin, required, false},
		{EffortTransportStdin, effortless, false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s", tc.transport, tc.model.Effort), func(t *testing.T) {
			err := effortAdmission("pangolin", Capabilities{EffortTransport: tc.transport}, tc.model)
			if tc.wantErr && err == nil {
				t.Fatalf("transport %s admitted a model with effort support %s", tc.transport, tc.model.Effort)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("transport %s refused a launch it can carry: %v", tc.transport, err)
			}
		})
	}
}

// An effort transport nobody defined must not be treated as one that works.
// The default branch answers "cannot carry", which is the conservative side —
// and the registry refuses the declaration outright, so this value can only
// reach CanCarry through a caller that built Capabilities by hand.
func TestCanCarryRefusesAnUndeclaredTransport(t *testing.T) {
	if EffortTransport(42).CanCarry(EffortSupportRequired) {
		t.Fatal("an undeclared transport claimed it can carry a required effort")
	}
}

func TestNormalizeSystemIDAcceptsTheDeclaredSpellings(t *testing.T) {
	cases := map[string]SystemID{
		"claude-code":   "claude-code",
		"codex":         "codex",
		"qwen-code":     "qwen-code",
		"gemini-cli":    "gemini-cli",
		"antigravity":   "antigravity",
		"muse":          "muse",
		"  Codex  ":     "codex",
		"QWEN-CODE":     "qwen-code",
		"opencode2":     "opencode2",
		"a":             "a",
		"agy-2-preview": "agy-2-preview",
	}
	for raw, want := range cases {
		got, err := NormalizeSystemID(raw)
		if err != nil {
			t.Errorf("NormalizeSystemID(%q): %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeSystemID(%q) = %q, want %q", raw, got, want)
		}
	}
}

// Normalization must be idempotent, or the id that keys downstream state
// depends on how many times it passed through the boundary.
func TestNormalizeSystemIDIsIdempotent(t *testing.T) {
	for _, raw := range []string{"  Claude-Code ", "CODEX", "muse"} {
		once, err := NormalizeSystemID(raw)
		if err != nil {
			t.Fatalf("NormalizeSystemID(%q): %v", raw, err)
		}
		twice, err := NormalizeSystemID(string(once))
		if err != nil {
			t.Fatalf("NormalizeSystemID(%q): %v", once, err)
		}
		if once != twice {
			t.Errorf("normalizing %q twice gave %q then %q", raw, once, twice)
		}
	}
}

func TestInvalidSystemIDErrorNamesTheRawSpelling(t *testing.T) {
	_, err := NormalizeSystemID("Claude Code")
	var invalid *InvalidSystemIDError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an InvalidSystemIDError", err)
	}
	if invalid.Raw != "Claude Code" {
		t.Errorf("Raw = %q, want the spelling as it was written", invalid.Raw)
	}
}

func TestSupportsModeAnswersOnlyForDeclaredModes(t *testing.T) {
	caps := Capabilities{LaunchModes: []LaunchMode{LaunchModeExec, LaunchModeDryRun}}
	if !caps.SupportsMode(LaunchModeExec) || !caps.SupportsMode(LaunchModeDryRun) {
		t.Error("a declared mode was reported unsupported")
	}
	if caps.SupportsMode(LaunchModeManagedSession) {
		t.Error("an undeclared mode was reported supported")
	}
	if (Capabilities{}).SupportsMode(LaunchModeExec) {
		t.Error("a system declaring no modes reported support for one")
	}
}

// Absence and emptiness are the same fact for a composition, and both are
// different from a half-filled one — a prefix with no servers, or servers with
// no prefix, is a caller bug that the plugin's validator must get to see.
func TestCompositionIsZeroOnlyWhenNothingWasRequested(t *testing.T) {
	if !(Composition{}).IsZero() {
		t.Error("an empty composition did not report itself as absent")
	}
	if (Composition{Prefix: []string{"--mcp"}}).IsZero() {
		t.Error("a prefix without servers reported itself as absent")
	}
	if (Composition{Servers: []CompositionServer{{Name: "jira"}}}).IsZero() {
		t.Error("servers without a prefix reported themselves as absent")
	}
}

// The String forms reach operators through refusal text, so an undeclared
// value must render as itself rather than silently as a legitimate one.
func TestUndeclaredEnumValuesRenderAsThemselves(t *testing.T) {
	if got := LaunchMode(97).String(); got != "launch-mode(97)" {
		t.Errorf("LaunchMode(97) = %q, want it to name the undeclared value", got)
	}
	if got := EffortTransport(42).String(); got != "effort-transport(42)" {
		t.Errorf("EffortTransport(42) = %q, want it to name the undeclared value", got)
	}
	if got := EffortSupport(9).String(); got != "effort-support(9)" {
		t.Errorf("EffortSupport(9) = %q, want it to name the undeclared value", got)
	}
	if !LaunchModeExec.Valid() || LaunchMode(97).Valid() {
		t.Error("LaunchMode.Valid does not separate declared modes from undeclared ones")
	}
	if !EffortTransportNone.Valid() || EffortTransport(42).Valid() {
		t.Error("EffortTransport.Valid does not separate declared transports from undeclared ones")
	}
}
