package agentic

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// This file is the ONLY place an agentic system binding may live.
//
// Invariant 5 of docs/architecture.md, carried from the extraction source:
// one adapter table, one registry, one normalization per identifier. The
// source repo paid repeatedly for shadow tables — a second map from system id
// to behaviour, or a switch over system ids, that drifted out of step with the
// first and was found only when a launch misbehaved in production.
//
// singlesource_guard_test.go enforces it mechanically: a `map[SystemID]T`
// type expression anywhere outside this file, a composite literal keyed by a
// known system id, or a switch/if-else chain dispatching on system id values,
// fails the test. The guard's threat model and its declared-open residuals are
// documented there.

// Registry maps system identifiers to the plugins that implement them.
// Registration is the only way a binding comes to exist; there is no
// construction path that takes a system by value, and no dispatch anywhere in
// this module reaches a plugin except through a lookup here.
type Registry struct {
	mu      sync.RWMutex
	systems map[SystemID]System
	graph   *plugin.Registry
}

// NewRegistry returns an empty registry. An empty registry is a valid state,
// not a failure: a binary with no plugins compiled in answers "nothing
// registered", which is a different fact from a lookup that broke.
func NewRegistry() *Registry {
	return &Registry{systems: map[SystemID]System{}, graph: plugin.NewRegistry()}
}

// Default is the registry plugin packages register into from their init
// functions, and the one the CLI reads. A caller that needs isolation — every
// test in this package, for one — builds its own with NewRegistry.
var Default = NewRegistry()

var (
	// ErrNilSystem is returned when Register is handed no plugin at all.
	ErrNilSystem = errors.New("agentic: cannot register a nil system")
	// ErrDuplicateSystem is returned when a system id is registered twice.
	//
	// Same-binding idempotency is deliberately NOT offered: a plugin whose
	// init runs twice, or two plugins claiming one id, is a build-time bug,
	// and silently accepting the second registration is how a shadow binding
	// gets in without anyone reading a diff. Runtime declaration policy —
	// where re-declaring an identical thing may legitimately be a no-op — is a
	// different layer with a different rule.
	ErrDuplicateSystem = errors.New("agentic: system already registered")
	// ErrNoLaunchModes is returned when a system declares no launch mode. It
	// could never launch, so the declaration is a bug rather than a
	// restriction to honour.
	ErrNoLaunchModes = errors.New("agentic: system declares no launch modes")
	// ErrInvalidLaunchMode is returned when a declared mode is not one this
	// contract defines.
	ErrInvalidLaunchMode = errors.New("agentic: system declares an unknown launch mode")
	// ErrInvalidEffortTransport is returned when a declared effort transport
	// is not one this contract defines. Admitting it would make
	// EffortTransport.CanCarry answer for a transport nobody can implement.
	ErrInvalidEffortTransport = errors.New("agentic: system declares an unknown effort transport")
	// ErrUnnormalizedSystemID is returned when a plugin's ID() does not
	// normalize to itself.
	//
	// System.ID documents this requirement; before it was enforced here, the
	// requirement was prose with nothing behind it. A plugin returning an id
	// in a second spelling registered fine and everything the registry emitted
	// was normalized — IDs() and Plan.System both carried the folded form —
	// but the plugin kept answering the raw spelling to anyone holding it from
	// Lookup. That is two names for one system, which is the two-spellings
	// disease invariant 5 exists to prevent, moved from a map key into the
	// plugin itself. Refusing at the boundary makes sys.ID() and the registry
	// key the same string at registration.
	ErrUnnormalizedSystemID = errors.New("agentic: system id is not the spelling it normalizes to")
	// ErrUnstableSystemID is returned when a plugin answers ID() with two
	// different values during one registration.
	//
	// System.ID is contracted to be stable forever, and the normalization
	// refusal above was documented as making two spellings of one system
	// impossible. It did not: Register read ID() once, so a plugin whose ID()
	// is unstable registered under its first answer and served its second, and
	// everything downstream — Lookup, IDs, Plan.System — planned happily around
	// the disagreement. Reading twice is what a registration-time check can
	// afford, and it catches the accidental version: an id computed from
	// mutable state, memoized on the wrong call, or read from an environment
	// the plugin mutates as it initializes.
	//
	// The bound is worth stating rather than implying. TWO reads catch a
	// plugin that disagrees with itself between them. Nothing here catches a
	// plugin that answers consistently at registration and changes on the
	// third call — that is an unbounded obligation, and no check the registry
	// can afford would meet it. Stability past registration is contract, not
	// enforcement, and System.ID says so in those words.
	ErrUnstableSystemID = errors.New("agentic: system id is not stable across reads")
)

// Register adds a system to the registry, refusing anything that could not
// launch or that would create a second binding for one identifier.
//
// The identifier is read TWICE before anything else looks at it, because a
// plugin that answers ID() differently on two consecutive calls would register
// under one name and be held under another — the two-spellings disease with no
// second map involved. That check is cheap and bounded; see ErrUnstableSystemID
// for what it does and does not promise.
//
// The identifier is then normalized, so an id that cannot normalize is refused
// with the rule it broke. An id that normalizes to something OTHER than itself
// is refused too: System.ID is contracted to be its own normal form, and a
// plugin that answers a second spelling of its own id is a shadow binding one
// layer up from the map this file guards.
func (r *Registry) Register(sys System) error {
	return r.RegisterWithDependencies(sys)
}

// RegisterWithDependencies is the compatibility bridge from an unchanged
// System implementation into the general graph. Existing plugins call
// Register and therefore declare no new prerequisites; a system that needs an
// inference engine can add that edge without changing the System interface.
func (r *Registry) RegisterWithDependencies(sys System, dependencies ...plugin.Ref) error {
	if r == nil {
		return errors.New("agentic: cannot register into a nil registry")
	}
	if sys == nil {
		return ErrNilSystem
	}
	raw := sys.ID()
	// Before normalization, not after: an unstable plugin whose first answer
	// happens to be unnormalized should be told which rule it actually broke,
	// and "your id is not stable" is the more useful of the two.
	if again := sys.ID(); again != raw {
		return fmt.Errorf("%w: ID() returned %q and then %q; System.ID must answer the same value forever, and a plugin that disagrees with itself would be registered under one name and held under another", ErrUnstableSystemID, string(raw), string(again))
	}
	id, err := NormalizeSystemID(string(raw))
	if err != nil {
		return fmt.Errorf("agentic: registering system: %w", err)
	}
	if raw != id {
		return fmt.Errorf("%w: ID() returns %q but normalizes to %q; System.ID must already be its normalized spelling, or a caller holding the plugin and a caller reading the registry see two different names for one system", ErrUnnormalizedSystemID, string(raw), string(id))
	}
	caps := sys.Capabilities()
	if len(caps.LaunchModes) == 0 {
		return fmt.Errorf("%w: %s", ErrNoLaunchModes, id)
	}
	for _, mode := range caps.LaunchModes {
		if !mode.Valid() {
			return fmt.Errorf("%w: %s declares %s", ErrInvalidLaunchMode, id, mode)
		}
	}
	if !caps.EffortTransport.Valid() {
		return fmt.Errorf("%w: %s declares %s", ErrInvalidEffortTransport, id, caps.EffortTransport)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.systems == nil {
		r.systems = map[SystemID]System{}
	}
	if _, exists := r.systems[id]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateSystem, id)
	}
	if r.graph == nil {
		r.graph = plugin.NewRegistry()
	}
	graphPlugin := systemGraphPlugin{
		declaration: plugin.Declaration{
			ID:           plugin.ID(id),
			Kind:         PluginKind,
			Dependencies: append([]plugin.Ref(nil), dependencies...),
		},
		system: sys,
	}
	if err := r.graph.Register(graphPlugin); err != nil {
		return fmt.Errorf("agentic: registering system %s in plugin graph: %w", id, err)
	}
	r.systems[id] = sys
	return nil
}

type systemGraphPlugin struct {
	declaration plugin.Declaration
	system      System
}

func (p systemGraphPlugin) PluginDeclaration() plugin.Declaration { return p.declaration }

// System exposes the unchanged compatibility plugin held by this graph node.
func (p systemGraphPlugin) System() System { return p.system }

// RegisterPlugin adds a non-system plugin, such as an inference engine, to the
// same graph that future system dependency declarations resolve against.
func (r *Registry) RegisterPlugin(p plugin.Plugin) error {
	if r == nil {
		return errors.New("agentic: cannot register a plugin into a nil registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.graph == nil {
		r.graph = plugin.NewRegistry()
	}
	return r.graph.Register(p)
}

// Graph returns the general graph backing this compatibility registry.
func (r *Registry) Graph() *plugin.Registry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.graph == nil {
		r.graph = plugin.NewRegistry()
	}
	return r.graph
}

// Lookup returns the system registered under id, normalizing the identifier
// first. An id that does not normalize cannot name a registration, so it
// reports not-found rather than an error — the caller's next move is the same
// either way, and BuildPlan turns it into a refusal that names the id.
func (r *Registry) Lookup(id SystemID) (System, bool) {
	if r == nil {
		return nil, false
	}
	normalized, err := NormalizeSystemID(string(id))
	if err != nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	sys, ok := r.systems[normalized]
	return sys, ok
}

// IDs returns the registered identifiers in sorted order. The slice is freshly
// built on every call: a caller that sorts, appends to or overwrites the
// answer must not be able to reorder or grow the registry through it.
func (r *Registry) IDs() []SystemID {
	if r == nil {
		return []SystemID{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]SystemID, 0, len(r.systems))
	for id := range r.systems {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Len reports how many systems are registered.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.systems)
}

// Register adds a system to the default registry. It is what a plugin
// package's init function calls.
func Register(sys System) error { return Default.Register(sys) }
