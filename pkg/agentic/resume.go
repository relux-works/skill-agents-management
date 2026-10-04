package agentic

import (
	"errors"
	"regexp"
)

// ResumeKind separates host selection from a provider's native identity.
type ResumeKind string

const (
	ResumeNew         ResumeKind = "new"
	ResumeLatest      ResumeKind = "latest"
	ResumeHandle      ResumeKind = "handle"
	ResumeClaudeUUID  ResumeKind = "claude_uuid"
	ResumeCodexThread ResumeKind = "codex_thread"
	ResumeMuseSession ResumeKind = "muse_session"
)

// ResumeIntent contains no identity for new/latest. Native identities are
// syntactically validated here; existence and location belong to consumers.
type ResumeIntent struct {
	Kind     ResumeKind `json:"kind"`
	Identity *string    `json:"identity"`
}

// ResumeElevation is a detached suffix after removing only resume selectors.
type ResumeElevation struct {
	Intent     ResumeIntent
	NativeArgs []string
}

var ErrSessionResumeInvalid = errors.New("session_resume_invalid")

// ResumeInvalidError deliberately omits caller identities from diagnostics.
type ResumeInvalidError struct{ Reason string }

func (e *ResumeInvalidError) Error() string { return "session_resume_invalid: " + e.Reason }
func (e *ResumeInvalidError) Unwrap() error { return ErrSessionResumeInvalid }

// ResumeSelectorElevator is implemented by plugins that own a resume grammar.
// WrapperArgs contains only the wrapper selector, never the native tail.
type ResumeSelectorElevator interface {
	ElevateResumeIntent(wrapperArgs, nativeArgs []string) (ResumeElevation, error)
}

// ElevateResumeIntent dispatches without starting a process or changing native
// BuildPlan semantics. Hosted launchers call this before constructing the plan.
func ElevateResumeIntent(r *Registry, system SystemID, wrapperArgs, nativeArgs []string) (ResumeElevation, error) {
	plugin, ok := r.Lookup(system)
	if !ok {
		return ResumeElevation{}, &ResumeInvalidError{Reason: "unknown provider"}
	}
	elevator, ok := plugin.(ResumeSelectorElevator)
	if !ok {
		return ResumeElevation{}, &ResumeInvalidError{Reason: "provider has no resume grammar"}
	}
	return elevator.ElevateResumeIntent(wrapperArgs, nativeArgs)
}

var resumeUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var resumeHandle = regexp.MustCompile(`^SES-[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var museIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidateResumeIntent refuses picker spellings and identities on new/latest.
func ValidateResumeIntent(intent ResumeIntent, nativeKind ResumeKind) error {
	if intent.Kind == ResumeNew || intent.Kind == ResumeLatest {
		if intent.Identity != nil {
			return &ResumeInvalidError{Reason: "identity on new or latest"}
		}
		return nil
	}
	if intent.Identity == nil {
		return &ResumeInvalidError{Reason: "missing identity"}
	}
	identity := *intent.Identity
	if identity == "latest" || identity == "last" || identity == "--last" {
		return &ResumeInvalidError{Reason: "picker identity"}
	}
	valid := intent.Kind == ResumeHandle && resumeHandle.MatchString(identity)
	if intent.Kind == nativeKind {
		switch nativeKind {
		case ResumeClaudeUUID, ResumeCodexThread:
			valid = resumeUUID.MatchString(identity)
		case ResumeMuseSession:
			valid = museIdentity.MatchString(identity)
		}
	}
	if !valid {
		return &ResumeInvalidError{Reason: "invalid identity or provider kind"}
	}
	return nil
}

// ResolveResumeSelectors merges the wrapper selector with the plugin's parsed
// native selection. Even repeated identical selectors are conflicting.
func ResolveResumeSelectors(wrapper []string, selections []ResumeIntent, tail []string, kind ResumeKind) (ResumeElevation, error) {
	if len(wrapper) != 0 {
		var selection ResumeIntent
		switch {
		case len(wrapper) == 1 && wrapper[0] == "resume":
			selection.Kind = ResumeLatest
		case len(wrapper) == 2 && wrapper[0] == "resume":
			selection = ResumeIntent{Kind: ResumeHandle, Identity: &wrapper[1]}
		case len(wrapper) == 2 && wrapper[0] == "--resume":
			selection = ResumeIntent{Kind: kind, Identity: &wrapper[1]}
		default:
			return ResumeElevation{}, &ResumeInvalidError{Reason: "invalid wrapper selector"}
		}
		selections = append(selections, selection)
	}
	if len(selections) > 1 {
		return ResumeElevation{}, &ResumeInvalidError{Reason: "conflicting selectors"}
	}
	intent := ResumeIntent{Kind: ResumeNew}
	if len(selections) == 1 {
		intent = selections[0]
	}
	if err := ValidateResumeIntent(intent, kind); err != nil {
		return ResumeElevation{}, err
	}
	if intent.Identity != nil {
		identity := *intent.Identity
		intent.Identity = &identity
	}
	return ResumeElevation{Intent: intent, NativeArgs: append([]string{}, tail...)}, nil
}
