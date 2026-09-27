package runtimecatalog

import (
	"errors"
	"fmt"
	"path/filepath"
)

type RefusalKind string

const (
	RefusalAbsent      RefusalKind = "absent"
	RefusalReadFailed  RefusalKind = "read_failed"
	RefusalMalformed   RefusalKind = "malformed"
	RefusalConflicting RefusalKind = "conflicting"
	RefusalUnbound     RefusalKind = "unbound"
	RefusalUnsupported RefusalKind = "unsupported"
)

var (
	ErrAbsent      = errors.New("runtimecatalog: required operator file is absent")
	ErrReadFailed  = errors.New("runtimecatalog: operator file could not be read")
	ErrMalformed   = errors.New("runtimecatalog: operator file is malformed")
	ErrConflicting = errors.New("runtimecatalog: operator values conflict")
	ErrUnbound     = errors.New("runtimecatalog: binding is unbound")
	ErrUnsupported = errors.New("runtimecatalog: binding is unsupported")
)

// Refusal is a sanitized, typed configuration refusal. File is always a
// basename; Subject is a role, runtime, or engine identifier. Error messages
// never include TOML contents, absolute paths, or credential values.
type Refusal struct {
	Kind    RefusalKind
	File    string
	Subject string
}

func (e *Refusal) Error() string {
	if e == nil {
		return "runtimecatalog: refusal"
	}
	message := fmt.Sprintf("runtimecatalog: %s", e.Kind)
	if e.File != "" {
		message += " in " + filepath.Base(e.File)
	}
	if e.Subject != "" {
		message += " for " + e.Subject
	}
	return message
}

func (e *Refusal) Is(target error) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case RefusalAbsent:
		return target == ErrAbsent
	case RefusalReadFailed:
		return target == ErrReadFailed
	case RefusalMalformed:
		return target == ErrMalformed
	case RefusalConflicting:
		return target == ErrConflicting
	case RefusalUnbound:
		return target == ErrUnbound
	case RefusalUnsupported:
		return target == ErrUnsupported
	default:
		return false
	}
}

func refuse(kind RefusalKind, file, subject string) error {
	return &Refusal{Kind: kind, File: filepath.Base(file), Subject: subject}
}
