// Typed refusals for the Curator-catalog local runtime source.
//
// The launch path reads ~/.curator/runtimes.toml plus ~/.curator/engines.toml
// as its source of local runtime/model rows and engine pointers, with the
// legacy ~/.agents/.configs/local-models.toml as an explicit fallback. Every
// failure of that resolution is one of the six schema kinds in
// docs/operator-file-schema-v1.md, sanitized: the message names the kind, the
// source basename, and a role/runtime/engine identifier only. It never embeds
// TOML values, credentials, or absolute home paths.
package localmodels

import (
	"errors"
	"fmt"
	"path/filepath"
)

// RefusalKind is one of the six schema refusal kinds.
type RefusalKind string

const (
	// RefusalAbsent means the required operator file does not exist.
	RefusalAbsent RefusalKind = "absent"
	// RefusalReadFailed means a file exists but could not be read. It never
	// falls back to a legacy source.
	RefusalReadFailed RefusalKind = "read_failed"
	// RefusalMalformed means a readable file cannot be parsed as TOML or has
	// invalid schema fields, including unknown fields.
	RefusalMalformed RefusalKind = "malformed"
	// RefusalConflicting means two explicit values disagree. Resolution
	// refuses rather than applying last-writer-wins.
	RefusalConflicting RefusalKind = "conflicting"
	// RefusalUnbound means the declared runtime/engine entry is absent on
	// this machine.
	RefusalUnbound RefusalKind = "unbound"
	// RefusalUnsupported means a declared schema version, runtime system,
	// engine reference, credential shape, or weights prefix is not supported
	// by this consumer.
	RefusalUnsupported RefusalKind = "unsupported"
)

var (
	// ErrAbsent reports a missing required operator file.
	ErrAbsent = errors.New("localmodels: required operator file is absent")
	// ErrReadFailed reports a file that exists but could not be read.
	ErrReadFailed = errors.New("localmodels: operator file could not be read")
	// ErrMalformed reports a readable file with invalid schema fields.
	ErrMalformed = errors.New("localmodels: operator file is malformed")
	// ErrConflicting reports disagreeing explicit values.
	ErrConflicting = errors.New("localmodels: operator values conflict")
	// ErrUnbound reports a declared runtime/engine entry absent on this machine.
	ErrUnbound = errors.New("localmodels: binding is unbound")
	// ErrUnsupported reports a declared value this consumer does not support.
	ErrUnsupported = errors.New("localmodels: binding is unsupported")
)

// Refusal is a sanitized, typed configuration refusal. File is always a
// basename; Subject is a runtime, model, or engine identifier. Error messages
// never include TOML contents, absolute paths, or credential values.
type Refusal struct {
	Kind    RefusalKind
	File    string
	Subject string
}

func (e *Refusal) Error() string {
	if e == nil {
		return "localmodels: refusal"
	}
	message := fmt.Sprintf("localmodels: %s", e.Kind)
	if e.File != "" {
		message += " in " + filepath.Base(e.File)
	}
	if e.Subject != "" {
		message += " for " + e.Subject
	}
	return message
}

// Is maps each kind to its sentinel. A malformed refusal also satisfies the
// legacy ErrConfigMalformed so callers written against the single-file loader
// keep refusing without relearning the kind.
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
		return target == ErrMalformed || target == ErrConfigMalformed
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

// refuse builds a sanitized refusal. Callers pass identifiers only as
// subject, never values or paths. The subject is re-validated against a
// strict identifier grammar here so every constructor, including future
// ones passing TOML-derived names, stays sanitized: an unsafe subject is
// omitted rather than echoed.
func refuse(kind RefusalKind, file, subject string) error {
	return &Refusal{Kind: kind, File: filepath.Base(file), Subject: sanitizeRefusalSubject(subject)}
}

// sanitizeRefusalSubject admits only identifier-shaped subjects: 1-128
// ASCII letters, digits, dot, underscore, or hyphen. Paths, whitespace,
// control characters, quotes, and every other value-shaped spelling are
// omitted (empty subject). Runtime IDs normalized through internal/ident
// and engine names such as "local-qwen" pass; "/a/path", secrets with
// punctuation, and control bytes do not.
func sanitizeRefusalSubject(subject string) string {
	if len(subject) == 0 || len(subject) > 128 {
		return ""
	}
	for i := 0; i < len(subject); i++ {
		c := subject[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return ""
	}
	return subject
}
