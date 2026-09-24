package agentic_test

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
)

func TestRegistryClassifiesVersionedNonInteractiveFormsByEnvironmentAndPlacement(t *testing.T) {
	tests := []struct {
		name    string
		system  agentic.SystemID
		release string
		suffix  []string
		form    agentic.NonInteractiveForm
		grammar agentic.PermissionGrammarVersion
	}{
		{"claude print short flag", "claude-code", "2.1.261", []string{"-p"}, agentic.NonInteractiveFormPrint, agentic.PermissionGrammarV2},
		{"claude print equals placement", "claude-code", "2.1.261", []string{"--print=true"}, agentic.NonInteractiveFormPrint, agentic.PermissionGrammarV2},
		{"claude print with separate prompt", "claude-code", "2.1.261", []string{"--print", "summarize this"}, agentic.NonInteractiveFormPrint, agentic.PermissionGrammarV2},
		{"codex exec command", "codex", "0.153.2", []string{"exec", "summarize this"}, agentic.NonInteractiveFormExec, agentic.PermissionGrammarV2},
		{"codex exec alias", "codex", "0.153.2", []string{"e", "summarize this"}, agentic.NonInteractiveFormExec, agentic.PermissionGrammarV2},
		{"codex exec after equals option", "codex", "0.153.2", []string{"--model=gpt-test", "exec", "summarize this"}, agentic.NonInteractiveFormExec, agentic.PermissionGrammarV2},
		{"codex exec after separate option", "codex", "0.153.2", []string{"--model", "gpt-test", "exec", "summarize this"}, agentic.NonInteractiveFormExec, agentic.PermissionGrammarV2},
		{"codex exec after other separate option", "codex", "0.153.2", []string{"--local-provider", "ollama", "exec", "summarize this"}, agentic.NonInteractiveFormExec, agentic.PermissionGrammarV2},
		{"pi print short flag", "pi-native", "0.84.2", []string{"-p"}, agentic.NonInteractiveFormPrint, agentic.PermissionGrammarV1},
		{"pi print equals placement", "pi-native", "0.84.2", []string{"--print=true"}, agentic.NonInteractiveFormPrint, agentic.PermissionGrammarV1},
		{"pi print with separate prompt", "pi-native", "0.84.2", []string{"--print", "summarize this"}, agentic.NonInteractiveFormPrint, agentic.PermissionGrammarV1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agentic.Default.ClassifyNonInteractiveArgs(tt.system, tt.release, tt.suffix)
			if err != nil {
				t.Fatalf("ClassifyNonInteractiveArgs: %v", err)
			}
			if !got.IsNonInteractive() || got.Form != tt.form || got.Grammar != tt.grammar {
				t.Fatalf("classification = %#v, want form %q under %q", got, tt.form, tt.grammar)
			}
		})
	}
}

func TestRegistryClassifierStopsBeforePromptText(t *testing.T) {
	tests := []struct {
		name    string
		system  agentic.SystemID
		release string
		suffix  []string
		grammar agentic.PermissionGrammarVersion
	}{
		{"claude print-looking prompt", "claude-code", "2.1.261", []string{"--", "--print"}, agentic.PermissionGrammarV2},
		{"claude short print-looking prompt", "claude-code", "2.1.261", []string{"--", "-p"}, agentic.PermissionGrammarV2},
		{"claude print-looking prompt after a flag", "claude-code", "2.1.261", []string{"--verbose", "--", "--print"}, agentic.PermissionGrammarV2},
		{"codex exec-looking prompt", "codex", "0.153.2", []string{"--", "exec"}, agentic.PermissionGrammarV2},
		{"pi print-looking prompt", "pi-native", "0.84.2", []string{"--", "--print"}, agentic.PermissionGrammarV1},
		{"pi short print-looking prompt", "pi-native", "0.84.2", []string{"--", "-p"}, agentic.PermissionGrammarV1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agentic.Default.ClassifyNonInteractiveArgs(tt.system, tt.release, tt.suffix)
			if err != nil {
				t.Fatalf("ClassifyNonInteractiveArgs: %v", err)
			}
			if got.IsNonInteractive() || got.Form != agentic.NonInteractiveFormNone || got.Grammar != tt.grammar {
				t.Fatalf("classification = %#v, want interactive under %q", got, tt.grammar)
			}
		})
	}
}

func TestCodexClassifierDoesNotTreatAnOptionValueAsTheExecCommand(t *testing.T) {
	got, err := agentic.Default.ClassifyNonInteractiveArgs("codex", "0.153.2", []string{"--model", "exec"})
	if err != nil {
		t.Fatalf("ClassifyNonInteractiveArgs: %v", err)
	}
	if got.IsNonInteractive() || got.Form != agentic.NonInteractiveFormNone {
		t.Fatalf("classification = %#v, want interactive because exec is the model option value", got)
	}
}

func TestCodexClassifierFailsClosedForAnUnmodeledRootFlag(t *testing.T) {
	tests := []struct {
		name   string
		suffix []string
	}{
		{"unknown option", []string{"--unknown-root-option", "exec"}},
		{"variadic image option", []string{"--image", "one.png", "two.png", "exec"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := agentic.Default.ClassifyNonInteractiveArgs("codex", "0.153.2", tt.suffix)
			if !errors.Is(err, agentic.ErrNativeArgsClassificationIndeterminate) {
				t.Fatalf("error = %v, want ErrNativeArgsClassificationIndeterminate", err)
			}
		})
	}
}

func TestNonInteractiveClassifierFailsClosedForUnknownOrUnverifiedInputs(t *testing.T) {
	t.Run("unknown system", func(t *testing.T) {
		_, err := agentic.Default.ClassifyNonInteractiveArgs("opencode", "1.0.0", []string{"--print"})
		if !errors.Is(err, agentic.ErrUnknownSystem) {
			t.Fatalf("error = %v, want ErrUnknownSystem", err)
		}
	})

	t.Run("registered system without this capability", func(t *testing.T) {
		_, err := agentic.Default.ClassifyNonInteractiveArgs("pi", "0.84.2", []string{"spawn"})
		if !errors.Is(err, agentic.ErrNativeArgsClassifierUnsupported) {
			t.Fatalf("error = %v, want ErrNativeArgsClassifierUnsupported", err)
		}
	})

	for _, system := range []struct {
		id      agentic.SystemID
		release string
		suffix  []string
	}{
		{"claude-code", "2.1.262", []string{"--print"}},
		{"codex", "0.153.3", []string{"exec"}},
		{"pi-native", "0.84.3", []string{"--print"}},
		{"claude-code", "", []string{"--print"}},
		{"codex", "", []string{"exec"}},
		{"pi-native", "", []string{"--print"}},
	} {
		t.Run(string(system.id)+"/"+system.release, func(t *testing.T) {
			_, err := agentic.Default.ClassifyNonInteractiveArgs(system.id, system.release, system.suffix)
			if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("error = %v, want ErrPermissionModeUnverifiedRelease", err)
			}
		})
	}
}
