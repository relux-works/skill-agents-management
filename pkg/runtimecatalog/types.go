// Package runtimecatalog parses and discovers the machine-local Curator
// runtime and role-binding catalogs. It reports plans and provenance only; it
// does not start engines. Codex's per-launch local-provider transport is
// configured through its supported CLI config overrides, while spawn-side
// engine lifecycle remains outside this package.
package runtimecatalog

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

const SchemaVersion = 1

const (
	RuntimesFile = "runtimes.toml"
	EnginesFile  = "engines.toml"
	ModelsFile   = "models.toml"
)

type EngineReference struct {
	Plugin string
	Name   string
}

type Model struct {
	ID     vendorplugin.ModelID
	Engine *EngineReference
	Fields map[string]any
}

type Runtime struct {
	ID     vendorplugin.RuntimeID
	System agentic.SystemID
	Engine *EngineReference
	Models []Model
	Fields map[string]any
}

// Catalog stores runtime declarations in a slice. This keeps the operator
// catalog separate from the module's vendor registry and avoids a second
// RuntimeID-keyed authority table.
type Catalog struct {
	Runtimes []Runtime
}

func (c Catalog) Runtime(id string) (Runtime, bool) {
	normalized, err := vendorplugin.NormalizeRuntimeID(id)
	if err != nil {
		return Runtime{}, false
	}
	for _, runtime := range c.Runtimes {
		if runtime.ID == normalized {
			return runtime, true
		}
	}
	return Runtime{}, false
}

type EngineEntry struct {
	Name string
	Kind string
}

type EngineCatalog struct {
	Entries []EngineEntry
}

func (c EngineCatalog) Entry(name string) (EngineEntry, bool) {
	for _, entry := range c.Entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return EngineEntry{}, false
}

type Binding struct {
	Role    string
	Run     string
	Profile string
}

type BindingCatalog struct {
	Bindings []Binding
}

func (c BindingCatalog) Binding(role string) (Binding, bool) {
	for _, binding := range c.Bindings {
		if binding.Role == role {
			return binding, true
		}
	}
	return Binding{}, false
}

type RowState string

const (
	RowBound   RowState = "bound"
	RowUnbound RowState = "unbound"
)

type Provenance struct {
	BindingSource        string
	BindingSchemaVersion int
	RuntimeSource        string
	RuntimeSchemaVersion int
	EngineSource         string
	EngineReferences     []EngineReference
}

type PreflightRow struct {
	Role       string
	State      RowState
	Run        string
	Profile    string
	Reason     string
	Refusal    *Refusal
	Provenance Provenance
}

type PreflightResult struct {
	Rows []PreflightRow
}

// SystemSupport is normally backed by the caller's agentic.Registry.Lookup.
type SystemSupport func(agentic.SystemID) bool

type ImportResult struct {
	RuntimesChanged bool
	ModelsChanged   bool
	Noop            bool
	SourceNames     []string
	RuntimeIDs      []string
	Unbound         []string
}
