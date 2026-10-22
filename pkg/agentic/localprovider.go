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

// LocalProviderSetting is one operator setting carried into a local-provider
// launch. Key is the Codex `-c` config key (dotted paths name nested
// values, exactly as the CLI spells them) and Value is the TOML fragment
// spliced after the `=` — for the admitted boolean keys, exactly `true` or
// `false`.
//
// The carrier is deliberately key-agnostic: the Codex plugin owns the closed
// allow-list, drops exact keys already sealed by the plan and refuses every
// other key it does not admit. A new harness key is a plugin change, never a
// change here. The plugin copies what it emits and retains no reference to the
// caller's slice.
type LocalProviderSetting struct {
	Key   string
	Value string
}

// LocalProviderSettingsValidator is an optional system-plugin gate for
// request-carried local-provider settings. BuildPlan invokes it before any
// filesystem-dependent dispatch — binary resolution, home and catalog reads,
// request preparation — so a refused setting refuses typed before the first
// read. Implementations must be pure: no filesystem access, no process start,
// no ambient state, only the request's own settings against the plugin's own
// policy. A nil or empty Settings slice validates as nothing to carry.
type LocalProviderSettingsValidator interface {
	ValidateLocalProviderSettings(LaunchRequest, LaunchMode) error
}

// LocalProviderBinding explicitly selects a provider entry declared in the
// private Codex configuration home. A nil pointer keeps the native Codex
// subscription path unchanged; a non-nil empty ID is a typed unbound request.
//
// Snapshot is an optional validated provider snapshot produced by the Codex
// plugin's exported constructor. When nil, the plugin resolves the ID from
// the private config.toml on every plan (the ID-only path, byte-identical to
// before snapshots existed). When non-nil, the plugin uses the snapshot's
// resolved entry without reading config.toml; any value that is not a
// snapshot the constructor produced is refused as malformed, and a snapshot
// bound to a different provider ID is refused as conflicting. The snapshot
// is immutable, so shallow copies of this binding safely share it.
//
// Settings carries operator settings into the local-provider argv BEFORE the
// exec seal, so a plan built with them still verifies. Nil or empty carries
// nothing and keeps every existing plan byte-identical. Each entry is
// validated against the Codex plugin's own settings policy at a pre-planning
// boundary before binary resolution and any file read. Exact keys already
// emitted by the plan's model-selection fragments are dropped and reported by
// name only in Plan.CarriedSettings; arbitrary model_* keys are never dropped.
// Of the remaining settings, only module-declared cosmetic/output-shape keys
// are admitted; credential, provider, unowned model, sandbox, approval, network,
// tool and unknown keys are refused typed. Admitted settings ride the sealed
// argv, so a post-seal change is still refused as conflicting.
type LocalProviderBinding struct {
	ID       string
	Snapshot any
	Settings []LocalProviderSetting
}

// LocalProviderSettingsStatus reports discarded operator settings by name
// only. It is informational and grants no authority over execution.
type LocalProviderSettingsStatus struct {
	DroppedKeys []string `json:"dropped_keys,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// LocalProviderSettingsReporter is an optional pure reporting surface. The
// planner asks only after successful validation; implementations retain no
// setting values. Systems without drops return the zero status.
type LocalProviderSettingsReporter interface {
	LocalProviderSettingsStatus(LaunchRequest, LaunchMode) LocalProviderSettingsStatus
}
