package agentic

import (
	"errors"
	"fmt"
)

// NonInteractiveForm identifies a closed command form that runs without an
// interactive terminal session. An empty form means the native suffix does
// not select a known non-interactive form for the verified release.
type NonInteractiveForm string

const (
	NonInteractiveFormNone  NonInteractiveForm = ""
	NonInteractiveFormPrint NonInteractiveForm = "print"
	NonInteractiveFormExec  NonInteractiveForm = "exec"
)

// NativeArgsClassification is the release-bound result of classifying one
// caller-owned native-argument suffix. Grammar is the same permission-grammar
// version verified for the system and tool release.
type NativeArgsClassification struct {
	Form    NonInteractiveForm
	Grammar PermissionGrammarVersion
}

// IsNonInteractive reports whether Form names a recognized non-interactive
// command form.
func (c NativeArgsClassification) IsNonInteractive() bool {
	return c.Form != NonInteractiveFormNone
}

// NonInteractiveArgumentClassifier is an optional system capability. A
// classifier must verify the exact tool release using its plugin-owned
// ReleaseCapability rows before examining native arguments.
type NonInteractiveArgumentClassifier interface {
	ClassifyNonInteractiveArgs(toolRelease string, suffix []string) (NativeArgsClassification, error)
}

var (
	ErrNativeArgsClassifierUnsupported       = errors.New("agentic: system has no verified native-argument non-interactive classifier")
	ErrNativeArgsClassificationIndeterminate = errors.New("agentic: native-argument form cannot be determined under the verified grammar")
)

// ClassifyNonInteractiveArgs resolves systemID through this registry and asks
// that system's release-bound classifier whether suffix selects a known
// non-interactive form. Unknown systems return ErrUnknownSystem; a release
// that is absent or unverified returns ErrPermissionModeUnverifiedRelease.
// A plugin may return ErrNativeArgsClassificationIndeterminate when the
// suffix contains syntax whose meaning is not established by the verified
// grammar; callers must not treat that as an interactive classification.
// The suffix is copied at the registry boundary so a plugin cannot mutate the
// caller's argv storage.
func (r *Registry) ClassifyNonInteractiveArgs(systemID SystemID, toolRelease string, suffix []string) (NativeArgsClassification, error) {
	system, ok := r.Lookup(systemID)
	if !ok {
		return NativeArgsClassification{}, fmt.Errorf("%w: %s", ErrUnknownSystem, systemID)
	}
	classifier, ok := system.(NonInteractiveArgumentClassifier)
	if !ok {
		return NativeArgsClassification{}, fmt.Errorf("%w: %s", ErrNativeArgsClassifierUnsupported, system.ID())
	}
	return classifier.ClassifyNonInteractiveArgs(toolRelease, append([]string(nil), suffix...))
}
