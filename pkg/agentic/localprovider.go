package agentic

import (
	"errors"
	"path/filepath"
)

type LocalProviderRefusalKind string

const (
	LocalProviderAbsent      LocalProviderRefusalKind = "absent"
	LocalProviderReadFailed  LocalProviderRefusalKind = "read_failed"
	LocalProviderMalformed   LocalProviderRefusalKind = "malformed"
	LocalProviderConflicting LocalProviderRefusalKind = "conflicting"
	LocalProviderUnbound     LocalProviderRefusalKind = "unbound"
	LocalProviderUnsupported LocalProviderRefusalKind = "unsupported"
)

var (
	ErrLocalProviderAbsent      = errors.New("agentic: local provider configuration is absent")
	ErrLocalProviderReadFailed  = errors.New("agentic: local provider configuration could not be read")
	ErrLocalProviderMalformed   = errors.New("agentic: local provider configuration is malformed")
	ErrLocalProviderConflicting = errors.New("agentic: local provider selection conflicts with its private binding")
	ErrLocalProviderUnbound     = errors.New("agentic: local provider selection has no private binding")
	ErrLocalProviderUnsupported = errors.New("agentic: local provider transport is unsupported")
)

// LocalProviderRefusal reports a sanitized, typed refusal. File is reduced to
// its basename and Subject may name only the selected provider id; endpoint,
// config contents, and absolute home paths never enter the error.
type LocalProviderRefusal struct {
	Kind    LocalProviderRefusalKind
	File    string
	Subject string
}

func (e *LocalProviderRefusal) Error() string {
	if e == nil {
		return "agentic: local provider refusal"
	}
	message := "agentic: local provider " + string(e.Kind)
	if e.File != "" {
		message += " in " + filepath.Base(e.File)
	}
	if e.Subject != "" {
		message += " for " + e.Subject
	}
	return message
}

func (e *LocalProviderRefusal) Is(target error) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case LocalProviderAbsent:
		return target == ErrLocalProviderAbsent
	case LocalProviderReadFailed:
		return target == ErrLocalProviderReadFailed
	case LocalProviderMalformed:
		return target == ErrLocalProviderMalformed
	case LocalProviderConflicting:
		return target == ErrLocalProviderConflicting
	case LocalProviderUnbound:
		return target == ErrLocalProviderUnbound
	case LocalProviderUnsupported:
		return target == ErrLocalProviderUnsupported
	default:
		return false
	}
}

// LocalProviderBinding explicitly selects a provider entry declared in the
// private Codex configuration home. A nil pointer keeps the native Codex
// subscription path unchanged; a non-nil empty ID is a typed unbound request.
type LocalProviderBinding struct {
	ID string
}
