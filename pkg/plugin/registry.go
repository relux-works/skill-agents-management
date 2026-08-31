// Package plugin provides the kind-agnostic plugin graph used by
// agents-management. Kinds are opaque declaration data: the registry validates
// identity and edges, but never switches on a kind or assigns it a layer.
package plugin

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/relux-works/skill-agents-management/internal/ident"
)

// ID is a plugin's stable identity. It is global within a registry; Kind is a
// property of that identity, not a second namespace.
type ID string

// Kind is an opaque plugin classification. New kinds are declared by plugin
// packages and require no change to Registry.
type Kind string

// Ref identifies a dependency and states the kind the dependant requires.
// Requiring the kind makes a same-named plugin of a different kind an explicit
// unsatisfied declaration instead of an accidental match.
type Ref struct {
	ID   ID
	Kind Kind
}

// Declaration is all graph data supplied by a plugin.
type Declaration struct {
	ID           ID
	Kind         Kind
	Dependencies []Ref
}

// Ref returns the declaration's own reference.
func (d Declaration) Ref() Ref { return Ref{ID: d.ID, Kind: d.Kind} }

// Plugin declares its graph identity. Concrete behavior remains owned by the
// package that defines the kind; the graph stores it opaquely.
type Plugin interface {
	PluginDeclaration() Declaration
}

var (
	ErrNilPlugin                = errors.New("plugin: cannot register a nil plugin")
	ErrInvalidDeclaration       = errors.New("plugin: declaration is invalid")
	ErrUnstableDeclaration      = errors.New("plugin: declaration is not stable across reads")
	ErrDuplicatePlugin          = errors.New("plugin: id is already registered")
	ErrDuplicateDependency      = errors.New("plugin: dependency is declared twice")
	ErrMissingDependency        = errors.New("plugin: dependency is not registered")
	ErrUnsatisfiableDeclaration = errors.New("plugin: declaration cannot be satisfied")
	ErrDependencyCycle          = errors.New("plugin: dependency cycle")
	ErrPluginNotRegistered      = errors.New("plugin: id is not registered")
)

type entry struct {
	declaration Declaration
	plugin      Plugin
}

// Registry atomically validates and stores a general plugin graph.
type Registry struct {
	mu      sync.RWMutex
	plugins map[ID]entry
}

func NewRegistry() *Registry { return &Registry{plugins: map[ID]entry{}} }

// Register validates and atomically installs one plugin.
func (r *Registry) Register(p Plugin) error { return r.RegisterAll(p) }

// RegisterAll validates a transaction of plugins against the existing graph.
// Batch registration is what makes cycles diagnosable as cycles: registering
// either half alone would correctly stop earlier as a missing dependency.
func (r *Registry) RegisterAll(plugins ...Plugin) error {
	if r == nil {
		return errors.New("plugin: cannot register into a nil registry")
	}
	prepared := make([]entry, 0, len(plugins))
	for _, p := range plugins {
		if nilPlugin(p) {
			return ErrNilPlugin
		}
		first := p.PluginDeclaration()
		second := p.PluginDeclaration()
		if !reflect.DeepEqual(first, second) {
			return fmt.Errorf("%w: plugin answered %#v and then %#v", ErrUnstableDeclaration, first, second)
		}
		declaration, err := validateDeclaration(first)
		if err != nil {
			return err
		}
		prepared = append(prepared, entry{declaration: declaration, plugin: p})
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	candidate := make(map[ID]entry, len(r.plugins)+len(prepared))
	for id, registered := range r.plugins {
		candidate[id] = registered
	}
	for _, registered := range prepared {
		if existing, found := candidate[registered.declaration.ID]; found {
			return fmt.Errorf("%w: %q is already kind %q and cannot also register as kind %q",
				ErrDuplicatePlugin, registered.declaration.ID, existing.declaration.Kind, registered.declaration.Kind)
		}
		candidate[registered.declaration.ID] = registered
	}
	if err := validateEdges(candidate); err != nil {
		return err
	}
	if err := detectCycle(candidate); err != nil {
		return err
	}
	r.plugins = candidate
	return nil
}

func nilPlugin(p Plugin) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func validateDeclaration(raw Declaration) (Declaration, error) {
	id, err := normalize("plugin id", string(raw.ID))
	if err != nil {
		return Declaration{}, err
	}
	kind, err := normalize("plugin kind", string(raw.Kind))
	if err != nil {
		return Declaration{}, err
	}
	if string(raw.ID) != id || string(raw.Kind) != kind {
		return Declaration{}, fmt.Errorf("%w: declaration (%q, %q) must already use normalized spellings (%q, %q)",
			ErrInvalidDeclaration, raw.Kind, raw.ID, kind, id)
	}
	declaration := Declaration{ID: ID(id), Kind: Kind(kind), Dependencies: append([]Ref(nil), raw.Dependencies...)}
	seen := make(map[ID]struct{}, len(declaration.Dependencies))
	for i, dependency := range declaration.Dependencies {
		depID, err := normalize("dependency id", string(dependency.ID))
		if err != nil {
			return Declaration{}, err
		}
		depKind, err := normalize("dependency kind", string(dependency.Kind))
		if err != nil {
			return Declaration{}, err
		}
		if string(dependency.ID) != depID || string(dependency.Kind) != depKind {
			return Declaration{}, fmt.Errorf("%w: dependency (%q, %q) of %q must already use normalized spellings (%q, %q)",
				ErrInvalidDeclaration, dependency.Kind, dependency.ID, declaration.ID, depKind, depID)
		}
		if _, duplicate := seen[dependency.ID]; duplicate {
			return Declaration{}, fmt.Errorf("%w: %q depends on %q more than once", ErrDuplicateDependency, declaration.ID, dependency.ID)
		}
		seen[dependency.ID] = struct{}{}
		declaration.Dependencies[i] = Ref{ID: ID(depID), Kind: Kind(depKind)}
	}
	return declaration, nil
}

func normalize(label, raw string) (string, error) {
	normalized, reason, ok := ident.Normalize(raw)
	if !ok {
		return "", fmt.Errorf("%w: invalid %s %q: %s; %s", ErrInvalidDeclaration, label, raw, reason, ident.Rule)
	}
	return normalized, nil
}

func validateEdges(graph map[ID]entry) error {
	for _, id := range sortedIDs(graph) {
		declaration := graph[id].declaration
		for _, dependency := range declaration.Dependencies {
			target, found := graph[dependency.ID]
			if !found {
				return fmt.Errorf("%w: %q (%s) requires %q (%s)",
					ErrMissingDependency, declaration.ID, declaration.Kind, dependency.ID, dependency.Kind)
			}
			if target.declaration.Kind != dependency.Kind {
				return fmt.Errorf("%w: %q (%s) requires %q to be kind %q, but it is kind %q",
					ErrUnsatisfiableDeclaration, declaration.ID, declaration.Kind, dependency.ID, dependency.Kind, target.declaration.Kind)
			}
		}
	}
	return nil
}

func detectCycle(graph map[ID]entry) error {
	const (
		unseen uint8 = iota
		visiting
		visited
	)
	state := make(map[ID]uint8, len(graph))
	stack := make([]ID, 0, len(graph))
	var visit func(ID) error
	visit = func(id ID) error {
		switch state[id] {
		case visited:
			return nil
		case visiting:
			start := 0
			for i, item := range stack {
				if item == id {
					start = i
					break
				}
			}
			cycle := append(append([]ID(nil), stack[start:]...), id)
			return fmt.Errorf("%w: %v", ErrDependencyCycle, cycle)
		}
		state[id] = visiting
		stack = append(stack, id)
		dependencies := append([]Ref(nil), graph[id].declaration.Dependencies...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].ID < dependencies[j].ID })
		for _, dependency := range dependencies {
			if err := visit(dependency.ID); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = visited
		return nil
	}
	for _, id := range sortedIDs(graph) {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// Resolution contains a plugin and its immediate, already-validated edges.
type Resolution struct {
	Declaration  Declaration
	Plugin       Plugin
	Dependencies []Resolution
}

// Resolve materializes a plugin's immediate dependencies without assuming a
// kind direction.
func (r *Registry) Resolve(id ID) (Resolution, error) {
	if r == nil {
		return Resolution{}, errors.New("plugin: cannot resolve from a nil registry")
	}
	normalized, err := normalize("plugin id", string(id))
	if err != nil {
		return Resolution{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered, found := r.plugins[ID(normalized)]
	if !found {
		return Resolution{}, fmt.Errorf("%w: %q", ErrPluginNotRegistered, normalized)
	}
	resolved := Resolution{Declaration: cloneDeclaration(registered.declaration), Plugin: registered.plugin}
	for _, dependency := range registered.declaration.Dependencies {
		target := r.plugins[dependency.ID]
		resolved.Dependencies = append(resolved.Dependencies, Resolution{
			Declaration: cloneDeclaration(target.declaration),
			Plugin:      target.plugin,
		})
	}
	return resolved, nil
}

func (r *Registry) Lookup(id ID) (Plugin, bool) {
	declaration, ok := r.entry(id)
	if !ok {
		return nil, false
	}
	return declaration.plugin, true
}

func (r *Registry) Declaration(id ID) (Declaration, bool) {
	registered, ok := r.entry(id)
	if !ok {
		return Declaration{}, false
	}
	return cloneDeclaration(registered.declaration), true
}

func (r *Registry) entry(id ID) (entry, bool) {
	if r == nil {
		return entry{}, false
	}
	normalized, _, ok := ident.Normalize(string(id))
	if !ok {
		return entry{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered, found := r.plugins[ID(normalized)]
	return registered, found
}

func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}

// TopologicalOrder returns every id dependency-first with stable lexical ties.
func (r *Registry) TopologicalOrder() []ID {
	if r == nil {
		return []ID{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	order := make([]ID, 0, len(r.plugins))
	seen := make(map[ID]bool, len(r.plugins))
	var visit func(ID)
	visit = func(id ID) {
		if seen[id] {
			return
		}
		seen[id] = true
		dependencies := append([]Ref(nil), r.plugins[id].declaration.Dependencies...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].ID < dependencies[j].ID })
		for _, dependency := range dependencies {
			visit(dependency.ID)
		}
		order = append(order, id)
	}
	for _, id := range sortedIDs(r.plugins) {
		visit(id)
	}
	return order
}

func sortedIDs[T any](items map[ID]T) []ID {
	ids := make([]ID, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func cloneDeclaration(in Declaration) Declaration {
	in.Dependencies = append([]Ref(nil), in.Dependencies...)
	return in
}
