package agentic

import (
	"errors"
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

const (
	// LaunchProvenanceContract is the stable discriminator consumed by tools
	// that persist or transport a launch provenance projection.
	LaunchProvenanceContract = "agents-management.launch-provenance"
	// LaunchProvenanceSchemaVersion is the JSON projection version. Additive or
	// incompatible projection changes must publish a new version deliberately.
	LaunchProvenanceSchemaVersion = 1

	// EngineBindingNoneV1 identifies a genuine legacy/no-engine launch.
	EngineBindingNoneV1 EngineBindingV1 = "none"
	// EngineBindingRequiredV1 makes the engine pair mandatory. Keeping this
	// fact outside the pair makes removal of both references detectable.
	EngineBindingRequiredV1 EngineBindingV1 = "required"
)

var (
	ErrLaunchProvenanceInvalid  = errors.New("agentic: launch provenance is invalid")
	ErrLaunchProvenanceMismatch = errors.New("agentic: configured and resolved inference-engine provenance mismatch")
)

// PluginRefV1 is the stable JSON form of a graph reference. It deliberately
// does not inherit Go field names from plugin.Ref.
type PluginRefV1 struct {
	ID   plugin.ID   `json:"id"`
	Kind plugin.Kind `json:"kind"`
}

// EngineBindingV1 distinguishes a genuine no-engine launch from an
// engine-bound record whose configured and resolved evidence was removed.
type EngineBindingV1 string

// LaunchProvenanceV1 is the versioned consumer projection for a built plan.
// Concrete model IDs remain data. ConfiguredEngine and ResolvedEngine are both
// absent for legacy/no-engine launches and otherwise must equal independently
// supplied configured and graph-resolved authority.
type LaunchProvenanceV1 struct {
	Contract      string `json:"contract"`
	SchemaVersion int    `json:"schema_version"`

	System    SystemID `json:"system"`
	Broker    string   `json:"broker,omitempty"`
	Publisher string   `json:"publisher,omitempty"`
	Family    string   `json:"family,omitempty"`

	EngineBinding    EngineBindingV1 `json:"engine_binding"`
	ConfiguredEngine *PluginRefV1    `json:"configured_engine,omitempty"`
	ResolvedEngine   *PluginRefV1    `json:"resolved_engine,omitempty"`

	Runtime string `json:"runtime,omitempty"`
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model,omitempty"`
}

// ValidateAgainst fails closed on unknown contract/schema, partial engine
// evidence, or a mismatch against independently trusted configured and graph-
// resolved engine references. Consumers must not derive either authority
// argument from p: both persisted references belong to the same untrusted
// record and equality between them is not corroboration.
func (p LaunchProvenanceV1) ValidateAgainst(configuredAuthority, resolvedAuthority plugin.Ref) error {
	if p.Contract != LaunchProvenanceContract {
		return fmt.Errorf("%w: contract %q, want %q", ErrLaunchProvenanceInvalid, p.Contract, LaunchProvenanceContract)
	}
	if p.SchemaVersion != LaunchProvenanceSchemaVersion {
		return fmt.Errorf("%w: schema version %d, want %d", ErrLaunchProvenanceInvalid, p.SchemaVersion, LaunchProvenanceSchemaVersion)
	}
	switch p.EngineBinding {
	case EngineBindingNoneV1:
		if configuredAuthority != (plugin.Ref{}) || resolvedAuthority != (plugin.Ref{}) {
			return fmt.Errorf("%w: no-engine provenance contradicts configured %#v and resolved %#v authority", ErrLaunchProvenanceMismatch, configuredAuthority, resolvedAuthority)
		}
		if p.ConfiguredEngine != nil || p.ResolvedEngine != nil {
			return fmt.Errorf("%w: no-engine provenance carries an engine reference", ErrLaunchProvenanceMismatch)
		}
		if p.System == "" {
			return fmt.Errorf("%w: no-engine provenance has no system", ErrLaunchProvenanceInvalid)
		}
		for _, axis := range []struct {
			label string
			value string
		}{
			{label: "broker", value: p.Broker},
			{label: "publisher", value: p.Publisher},
			{label: "family", value: p.Family},
			{label: "runtime", value: p.Runtime},
			{label: "profile", value: p.Profile},
			{label: "model", value: p.Model},
		} {
			if axis.value != "" {
				return fmt.Errorf("%w: no-engine provenance carries engine-bound %s", ErrLaunchProvenanceMismatch, axis.label)
			}
		}
		return nil
	case EngineBindingRequiredV1:
		if configuredAuthority == (plugin.Ref{}) || resolvedAuthority == (plugin.Ref{}) {
			return fmt.Errorf("%w: required engine authority is absent", ErrLaunchProvenanceMismatch)
		}
		if p.ConfiguredEngine == nil || p.ResolvedEngine == nil {
			return fmt.Errorf("%w: required engine reference is absent", ErrLaunchProvenanceMismatch)
		}
	default:
		return fmt.Errorf("%w: engine binding %q is unknown", ErrLaunchProvenanceInvalid, p.EngineBinding)
	}
	for _, candidate := range []struct {
		label string
		ref   PluginRefV1
	}{
		{label: "configured_engine", ref: *p.ConfiguredEngine},
		{label: "resolved_engine", ref: *p.ResolvedEngine},
		{label: "configured_authority", ref: PluginRefV1{ID: configuredAuthority.ID, Kind: configuredAuthority.Kind}},
		{label: "resolved_authority", ref: PluginRefV1{ID: resolvedAuthority.ID, Kind: resolvedAuthority.Kind}},
	} {
		if err := validateInferenceEnginePluginRefV1(candidate.label, candidate.ref); err != nil {
			return err
		}
	}
	configured := PluginRefV1{ID: configuredAuthority.ID, Kind: configuredAuthority.Kind}
	resolved := PluginRefV1{ID: resolvedAuthority.ID, Kind: resolvedAuthority.Kind}
	if configured != resolved {
		return fmt.Errorf("%w: configured authority %#v resolved authority %#v", ErrLaunchProvenanceMismatch, configured, resolved)
	}
	if *p.ConfiguredEngine != configured || *p.ResolvedEngine != resolved {
		return fmt.Errorf("%w: persisted configured %#v resolved %#v; authority configured %#v resolved %#v",
			ErrLaunchProvenanceMismatch, *p.ConfiguredEngine, *p.ResolvedEngine, configured, resolved)
	}
	for label, value := range map[string]string{
		"system": string(p.System), "broker": p.Broker, "publisher": p.Publisher,
		"family": p.Family, "runtime": p.Runtime, "profile": p.Profile, "model": p.Model,
	} {
		if value == "" {
			return fmt.Errorf("%w: engine-bound provenance has no %s", ErrLaunchProvenanceInvalid, label)
		}
	}
	return nil
}

func validateInferenceEnginePluginRefV1(label string, ref PluginRefV1) error {
	id, idReason, idOK := ident.Normalize(string(ref.ID))
	kind, kindReason, kindOK := ident.Normalize(string(ref.Kind))
	if !idOK {
		return fmt.Errorf("%w: %s id %q is not a normalized graph identity: %s", ErrLaunchProvenanceInvalid, label, ref.ID, idReason)
	}
	if id != string(ref.ID) {
		return fmt.Errorf("%w: %s id %q must use normalized spelling %q", ErrLaunchProvenanceInvalid, label, ref.ID, id)
	}
	if !kindOK {
		return fmt.Errorf("%w: %s kind %q is not a normalized graph identity: %s", ErrLaunchProvenanceInvalid, label, ref.Kind, kindReason)
	}
	if kind != string(ref.Kind) {
		return fmt.Errorf("%w: %s kind %q must use normalized spelling %q", ErrLaunchProvenanceInvalid, label, ref.Kind, kind)
	}
	const inferenceEngineKind plugin.Kind = "inference-engine"
	if ref.Kind != inferenceEngineKind {
		return fmt.Errorf("%w: %s kind %q, want %q", ErrLaunchProvenanceInvalid, label, ref.Kind, inferenceEngineKind)
	}
	return nil
}

// ConsumerProvenance publishes the plan's identity axes without changing the
// observable launch surface. BuildLaunch remains the producer of the internal
// requested/resolved refs; persisted consumers validate the returned record
// against independent registry/graph authority.
func (p Plan) ConsumerProvenance() (LaunchProvenanceV1, error) {
	projection := LaunchProvenanceV1{
		Contract: LaunchProvenanceContract, SchemaVersion: LaunchProvenanceSchemaVersion,
		System: p.System, Broker: p.Provenance.Broker, Publisher: p.Provenance.Publisher,
		Family: p.Provenance.Family, Runtime: p.Provenance.Runtime,
		Profile: p.Provenance.Profile, Model: p.Provenance.Model,
	}
	configured, resolved := p.Provenance.RequestedEngine, p.Provenance.ResolvedEngine
	projection.EngineBinding = EngineBindingNoneV1
	if configured != (plugin.Ref{}) || resolved != (plugin.Ref{}) {
		projection.EngineBinding = EngineBindingRequiredV1
	}
	if configured != (plugin.Ref{}) {
		projection.ConfiguredEngine = &PluginRefV1{ID: configured.ID, Kind: configured.Kind}
	}
	if resolved != (plugin.Ref{}) {
		projection.ResolvedEngine = &PluginRefV1{ID: resolved.ID, Kind: resolved.Kind}
	}
	if err := projection.ValidateAgainst(configured, resolved); err != nil {
		return LaunchProvenanceV1{}, err
	}
	return projection, nil
}
