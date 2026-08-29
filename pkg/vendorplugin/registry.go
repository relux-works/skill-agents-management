package vendorplugin

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// PluginKind is the compatibility declaration kind for model vendors. It is
// data consumed by pkg/plugin.Registry, not a layer known by that registry.
const PluginKind plugin.Kind = "model-vendor"

// This file is the ONLY place a vendor binding or a runtime binding may live.
//
// It is the vendor compatibility half of invariant 5 in docs/architecture.md, and the same
// guard enforces it: pkg/agentic/singlesource_guard_test.go walks the whole
// module, and its ONE list of dispatch key types names SystemID, VendorID and
// RuntimeID with the single file each may be bound in. A `map[VendorID]T` in
// any other file fails that test, and so does a `map[SystemID]T` in THIS one —
// the list is per key type, so the file that binds vendors is not thereby
// allowed to bind systems.

// Registry holds the vendor plugins compiled into a binary and the runtime
// declarations they can be paired through.
//
// It is built against an agentic compatibility registry so existing callers
// keep their constructor and lookup surface. Register publishes the vendor and
// its model-derived system edges into the general graph and refuses a vendor
// naming a system nobody registered.
type Registry struct {
	mu          sync.RWMutex
	systems     *agentic.Registry
	vendors     map[VendorID]Vendor
	runtimes    map[RuntimeID]RuntimeDeclaration
	diagnostics map[RuntimeID]RegistrationDiagnostic
	graph       *plugin.Registry
}

// NewRegistry returns an empty registry bound to the agentic registry its
// vendors must resolve against.
//
// An empty registry is a valid state. A NIL systems registry is not: it would
// make every dependency-direction check unanswerable, and Register refuses
// rather than admitting vendors whose declared systems nobody could check.
func NewRegistry(systems *agentic.Registry) *Registry {
	return &Registry{
		systems:     systems,
		vendors:     map[VendorID]Vendor{},
		runtimes:    map[RuntimeID]RuntimeDeclaration{},
		diagnostics: map[RuntimeID]RegistrationDiagnostic{},
		graph:       plugin.NewRegistry(),
	}
}

// Default is the registry vendor plugin packages register into from their init
// functions, and the one the CLI reads. It is bound to agentic.Default, so a
// vendor plugin's declared systems are checked against the systems this same
// binary compiled in.
var Default = NewRegistry(agentic.Default)

func init() {
	// The six historical ids exist in every binary, before any plugin has
	// registered: they are declarations, and a declaration does not need its
	// plugins present. A failure here is a malformed frozen table, which is a
	// build-time bug rather than a condition to tolerate — there is no
	// operator action that could fix it and no honest way to continue with
	// half a runtime table.
	if err := SeedFrozenRuntimes(Default); err != nil {
		panic(fmt.Sprintf("vendorplugin: the frozen runtime table is not declarable: %v", err))
	}
}

var (
	// ErrNilVendor is returned when Register is handed no plugin at all.
	ErrNilVendor = errors.New("vendorplugin: cannot register a nil vendor")
	// ErrNoAgenticRegistry is returned when a registry was built without the
	// agentic registry its vendors must be checked against. Admitting vendors
	// then would mean admitting the declared-systems check was skipped, which
	// is the dependency direction quietly turned off.
	ErrNoAgenticRegistry = errors.New("vendorplugin: registry has no agentic registry to check declared systems against")
	// ErrDuplicateVendor is returned when a vendor id is registered twice.
	// Same-binding idempotency is deliberately not offered here, for the same
	// reason agentic.ErrDuplicateSystem does not offer it: two plugins
	// claiming one id is a build-time bug. Runtime DECLARATIONS are the
	// different case with the different rule — see ErrRuntimeConflict.
	ErrDuplicateVendor = errors.New("vendorplugin: vendor already registered")
	// ErrUnnormalizedVendorID is returned when a plugin's ID() does not
	// normalize to itself.
	ErrUnnormalizedVendorID = errors.New("vendorplugin: vendor id is not the spelling it normalizes to")
	// ErrUnstableVendorID is returned when a plugin answers ID() with two
	// different values during one registration. As in agentic, two reads catch
	// the accidental version — an id computed from mutable state, memoized on
	// the wrong call — and stability past registration stays contract rather
	// than enforcement.
	ErrUnstableVendorID = errors.New("vendorplugin: vendor id is not stable across reads")
	// ErrNoModels is returned when a vendor declares no models. It owns
	// nothing then, and registering it would put an empty row in every
	// operator-facing list.
	ErrNoModels = errors.New("vendorplugin: vendor declares no models")
	// ErrDuplicateModel is returned when one vendor declares a model id twice.
	ErrDuplicateModel = errors.New("vendorplugin: vendor declares a model id twice")
	// ErrLifecycleInvalid is returned when a model's lineup state is blank or
	// outside the three declared states.
	ErrLifecycleInvalid = errors.New("vendorplugin: model lifecycle is not a declared lineup state")
	// ErrSupersessionInvalid is returned when a model's named successor
	// contradicts something: it is not a usable id, it is the model itself, it
	// sits on a row that is not legacy, or no model of that vendor answers to
	// it.
	ErrSupersessionInvalid = errors.New("vendorplugin: model supersession is unusable")
	// ErrRecommendationAmbiguous is returned when a vendor marks two models
	// recommended for one agentic system. A display pick that names two rows
	// picks nothing, and the operator-facing surfaces would then choose by
	// iteration order.
	ErrRecommendationAmbiguous = errors.New("vendorplugin: vendor recommends two models for one agentic system")
	// ErrPricingInvalid is returned when a model's billing contract could not
	// be quoted to anyone: a missing currency, source or date, a plan that
	// prices nothing, a promotion that is not one, or a contract attached to a
	// model its own plans never name.
	ErrPricingInvalid = errors.New("vendorplugin: model pricing contract is unusable")
	// ErrModelInvalid is returned when a model row is not a usable
	// declaration.
	ErrModelInvalid = errors.New("vendorplugin: model declaration is unusable")
	// ErrDescriptionEmpty is returned when a model's usage description is
	// blank. It is its own error because it is the field an operator reads to
	// choose between models, and a blank one must fail loudly at registration
	// rather than render as an empty column.
	ErrDescriptionEmpty = errors.New("vendorplugin: model has no usage description")
	// ErrRankInvalid is returned when a capability rank is out of range or
	// carries no evidence.
	ErrRankInvalid = errors.New("vendorplugin: capability rank is not evidence-based")
	// ErrEffortDeclaration is returned when a model's effort axis contradicts
	// itself.
	ErrEffortDeclaration = errors.New("vendorplugin: model effort declaration is unusable")
	// ErrUnknownAgenticSystem is the dependency direction, enforced. A vendor
	// declaring a system that is not registered is refused at registration,
	// naming BOTH ids. Admitting it would leave a model that resolves to a
	// harness nobody compiled in, and the failure would surface at launch —
	// far from the declaration that caused it.
	ErrUnknownAgenticSystem = errors.New("vendorplugin: vendor declares an agentic system that is not registered")

	// ErrRuntimeInvalid is returned when a runtime declaration is malformed.
	ErrRuntimeInvalid = errors.New("vendorplugin: runtime declaration is unusable")
	// ErrRuntimeConflict is the F2 collision policy, carried from the
	// extraction source: a declaration matching an existing one is legal and
	// idempotent, a CONFLICTING one is refused. Rebinding a frozen runtime id
	// to a different pair is exactly the silent state-orphaning
	// docs/architecture.md warns about, so the second declaration loses rather
	// than overwriting.
	ErrRuntimeConflict = errors.New("vendorplugin: runtime id is already declared for a different pair")
	// ErrUnknownRuntime is returned when no declaration exists for an id.
	ErrUnknownRuntime = errors.New("vendorplugin: no runtime declared")
	// ErrRuntimeSystemUnregistered is returned when a declared runtime's
	// agentic system is not registered in this binary.
	ErrRuntimeSystemUnregistered = errors.New("vendorplugin: runtime's agentic system is not registered")
	// ErrRuntimeVendorUnregistered is returned when a declared runtime names a
	// vendor that is not registered in this binary.
	ErrRuntimeVendorUnregistered = errors.New("vendorplugin: runtime's vendor is not registered")
	// ErrRuntimeVendorUnresolved is returned when a runtime's vendor was
	// looked for and never established — muse, in the frozen table. It is a
	// DIFFERENT error from ErrRuntimeVendorUnregistered on purpose: "the
	// binding is unknown" and "the plugin is not compiled in" are different
	// facts with different fixes, and collapsing them would tell an operator
	// to install something nobody has identified.
	ErrRuntimeVendorUnresolved = errors.New("vendorplugin: runtime's vendor was never established")
	// ErrVendorNotRegistered is returned when a lookup names no registered
	// vendor.
	ErrVendorNotRegistered = errors.New("vendorplugin: no vendor registered")

	// ErrRuntimeConfigMalformed is returned by ResolveRuntime for a RuntimeID
	// whose local configuration was FOUND but failed to parse or validate —
	// distinct from ErrUnknownRuntime, which continues to cover every
	// never-declared case, including genuinely absent local configuration.
	ErrRuntimeConfigMalformed = errors.New("vendorplugin: runtime's local configuration is malformed")
	// ErrRuntimeDiagnosticConflict is returned by DeclareRuntime when id
	// already carries a noted RegistrationDiagnostic — the mirror of
	// NoteUnregistered's own refuse-if-already-declared check. Without this
	// guard, DeclareRuntime could silently install a live declaration over an
	// existing diagnostic, after which ResolveRuntime would take the declared
	// branch and never read the diagnostic again.
	ErrRuntimeDiagnosticConflict = errors.New("vendorplugin: runtime already has a noted registration diagnostic")
)

// RegistrationDiagnostic records a typed, non-fatal reason a specific
// RuntimeID was deliberately left undeclared this process.
//
// Only "malformed" is defined in v1 — there is no diagnostic for "absent",
// because absent config must produce the SAME generic refusal every other
// never-configured RuntimeID already produces (a stale spawn.ceilings entry,
// a leftover CLI flag, a removed profile). Noting a diagnostic for "absent"
// would give it a distinct refusal shape it must NOT have.
type RegistrationDiagnostic struct {
	// Reason names why the id was left undeclared. "malformed" is the only
	// value this registry currently acts on.
	Reason string
	// Err is the underlying parse/validation error for a "malformed"
	// diagnostic. It is wrapped, not discarded, so ResolveRuntime's refusal
	// names the exact failure an operator's config hit.
	Err error
}

// Register adds a vendor to the registry, refusing anything that could not be
// launched, could not be chosen by an operator, or would create a second
// binding for one identifier.
//
// The identifier is read TWICE before anything else looks at it, then
// normalized, then required to be its own normal form — the same three checks
// agentic.Registry.Register performs, for the same reason: a plugin registered
// under one spelling and held under another is one vendor with two names, and
// the limit-state file is keyed by the name.
//
// Then the dependency direction: every agentic system every model declares
// must be registered in the agentic registry this registry was built against.
// A vendor that names an unregistered system is refused with BOTH ids in the
// error.
func (r *Registry) Register(vendor Vendor) error {
	if r == nil {
		return errors.New("vendorplugin: cannot register into a nil registry")
	}
	if vendor == nil {
		return ErrNilVendor
	}
	if r.systems == nil {
		return fmt.Errorf("%w: refusing every vendor rather than admitting one whose declared systems nobody checked", ErrNoAgenticRegistry)
	}
	raw := vendor.ID()
	if again := vendor.ID(); again != raw {
		return fmt.Errorf("%w: ID() returned %q and then %q; Vendor.ID must answer the same value forever, and a plugin that disagrees with itself would be registered under one name and held under another",
			ErrUnstableVendorID, string(raw), string(again))
	}
	id, err := NormalizeVendorID(string(raw))
	if err != nil {
		return fmt.Errorf("vendorplugin: registering vendor: %w", err)
	}
	if raw != id {
		return fmt.Errorf("%w: ID() returns %q but normalizes to %q; Vendor.ID must already be its normalized spelling, or a caller holding the plugin and a caller reading the registry see two different names for one vendor",
			ErrUnnormalizedVendorID, string(raw), string(id))
	}

	models := vendor.Models()
	if len(models) == 0 {
		return fmt.Errorf("%w: %s", ErrNoModels, id)
	}
	seenModels := map[ModelID]bool{}
	seenDependencies := map[plugin.ID]bool{}
	dependencies := make([]plugin.Ref, 0)
	var unknownSystem agentic.SystemID
	var unknownModel ModelID
	for _, model := range models {
		if err := model.Validate(); err != nil {
			return fmt.Errorf("vendorplugin: vendor %s: %w", id, err)
		}
		if seenModels[model.ID] {
			return fmt.Errorf("%w: %s declares %q twice", ErrDuplicateModel, id, model.ID)
		}
		seenModels[model.ID] = true
		// No duplicate-SCORE refusal, and its absence is the change v0.2.0
		// made rather than an omission. A shared score is the vendor stating
		// that two models are equally capable — an alias and its dated
		// snapshot, two harnesses' catalogues scored against one broker — and
		// refusing it would force a declaration to invent an ordering nobody
		// observed. The total order some callers need is derived instead; see
		// lineup.go.
		for _, system := range model.Systems {
			graphID := plugin.ID(system)
			if _, ok := r.systems.Lookup(system); !ok {
				if unknownSystem == "" {
					unknownSystem = system
					unknownModel = model.ID
				}
			}
			if !seenDependencies[graphID] {
				seenDependencies[graphID] = true
				dependencies = append(dependencies, plugin.Ref{ID: graphID, Kind: agentic.PluginKind})
			}
		}
	}
	if err := checkSupersession(models); err != nil {
		return fmt.Errorf("vendorplugin: vendor %s: %w", id, err)
	}
	if err := checkRecommendations(models); err != nil {
		return fmt.Errorf("vendorplugin: vendor %s: %w", id, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.vendors == nil {
		r.vendors = map[VendorID]Vendor{}
	}
	if _, exists := r.vendors[id]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateVendor, id)
	}
	if r.graph == nil {
		r.graph = plugin.NewRegistry()
	}
	if err := r.syncAgenticGraph(); err != nil {
		return fmt.Errorf("vendorplugin: syncing plugin graph before registering %s: %w", id, err)
	}
	graphPlugin := vendorGraphPlugin{
		declaration: plugin.Declaration{
			ID:           plugin.ID(id),
			Kind:         PluginKind,
			Dependencies: dependencies,
		},
		vendor: vendor,
	}
	if err := r.graph.Register(graphPlugin); err != nil {
		if unknownSystem != "" {
			return fmt.Errorf("%w: vendor %q declares agentic system %q for model %q, and no plugin is registered for %q: %w",
				ErrUnknownAgenticSystem, id, unknownSystem, unknownModel, unknownSystem, err)
		}
		return fmt.Errorf("vendorplugin: registering vendor %s in plugin graph: %w", id, err)
	}
	r.vendors[id] = vendor
	return nil
}

type vendorGraphPlugin struct {
	declaration plugin.Declaration
	vendor      Vendor
}

func (p vendorGraphPlugin) PluginDeclaration() plugin.Declaration { return p.declaration }

// Vendor exposes the unchanged compatibility plugin held by this graph node.
func (p vendorGraphPlugin) Vendor() Vendor { return p.vendor }

// syncAgenticGraph imports every dependency-first node visible to the agentic
// compatibility registry. That includes future inference-engine prerequisites,
// not only System nodes, and does not teach this registry any kind names.
func (r *Registry) syncAgenticGraph() error {
	source := r.systems.Graph()
	if source == nil {
		return ErrNoAgenticRegistry
	}
	for _, id := range source.TopologicalOrder() {
		if existing, found := r.graph.Declaration(id); found {
			sourceDeclaration, ok := source.Declaration(id)
			if !ok || !reflect.DeepEqual(existing, sourceDeclaration) {
				return fmt.Errorf("%w: plugin %q is declared as %#v in the vendor graph and %#v in the agentic graph; compatibility sync requires the exact dependency declaration",
					plugin.ErrUnsatisfiableDeclaration, id, existing, sourceDeclaration)
			}
			continue
		}
		graphPlugin, ok := source.Lookup(id)
		if !ok {
			return fmt.Errorf("vendorplugin: agentic graph lists %q and cannot look it up", id)
		}
		if err := r.graph.Register(graphPlugin); err != nil {
			return err
		}
	}
	return nil
}

// Graph returns the kind-agnostic graph backing this compatibility registry.
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

// Lookup returns the vendor registered under id, normalizing the identifier
// first. An id that does not normalize cannot name a registration, so it
// reports not-found rather than an error.
func (r *Registry) Lookup(id VendorID) (Vendor, bool) {
	if r == nil {
		return nil, false
	}
	normalized, err := NormalizeVendorID(string(id))
	if err != nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	vendor, ok := r.vendors[normalized]
	return vendor, ok
}

// VendorIDs returns the registered vendor identifiers in sorted order, freshly
// built on every call so a caller cannot reorder or grow the registry through
// the answer.
func (r *Registry) VendorIDs() []VendorID {
	if r == nil {
		return []VendorID{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]VendorID, 0, len(r.vendors))
	for id := range r.vendors {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// DeclareRuntime records a (agentic system × vendor) pair under a stable id.
//
// The F2 collision policy, carried from the extraction source: a declaration
// that MATCHES an existing one is legal and idempotent — two packages
// declaring the same pair have not disagreed, and refusing the second would
// make declaration order load-bearing. A declaration that CONFLICTS is
// refused, naming the id and both bindings, because the frozen ids feed
// admitted-pair digests and limit-state filenames and a rebind orphans that
// state silently.
func (r *Registry) DeclareRuntime(declaration RuntimeDeclaration) error {
	if r == nil {
		return errors.New("vendorplugin: cannot declare a runtime in a nil registry")
	}
	if err := declaration.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Checked FIRST, under the same lock NoteUnregistered acquires: a
	// diagnostic and a live declaration must never coexist for one id,
	// regardless of which is recorded first. NoteUnregistered refuses when id
	// is already declared; this is its mirror image.
	if diag, noted := r.diagnostics[declaration.ID]; noted {
		return fmt.Errorf("%w: %s: already noted unregistered (%s)", ErrRuntimeDiagnosticConflict, declaration.ID, diag.Reason)
	}
	if r.runtimes == nil {
		r.runtimes = map[RuntimeID]RuntimeDeclaration{}
	}
	if existing, declared := r.runtimes[declaration.ID]; declared {
		if existing.SameBinding(declaration) {
			if !existing.VendorResolved() && !existing.sameSystemOnlyAuthority(declaration) {
				return fmt.Errorf("%w: %s has the same system-only binding (%s × %s) but different declaration-owned model/effort authority; the first complete authority stands",
					ErrRuntimeConflict, declaration.ID, existing.System, existing.VendorLabel())
			}
			return nil
		}
		return fmt.Errorf("%w: %s is declared as (%s × %s) and was redeclared as (%s × %s); the id feeds admitted-pair digests and limit-state filenames, so the first declaration stands",
			ErrRuntimeConflict, declaration.ID,
			existing.System, existing.VendorLabel(),
			declaration.System, declaration.VendorLabel())
	}
	r.runtimes[declaration.ID] = declaration.clone()
	return nil
}

// NoteUnregistered records why id was deliberately left undeclared this
// process, so a LATER ResolveRuntime(id) call can surface a distinct, typed
// refusal instead of the generic "never declared" one.
//
// It does not itself register or declare anything, and it refuses if id IS
// already declared — a diagnostic and a live declaration would contradict
// each other. It costs a caller zero signature changes: the diagnostic lives
// on the registry object every caller already holds and passes to
// BuildLaunch, not in a second return value threaded through every call
// site.
//
// This method and DeclareRuntime's own diagnostic-conflict check acquire the
// SAME internal mutex that already guards the declared-runtime map, so two
// concurrent contenders calling NoteUnregistered and DeclareRuntime for the
// same id resolve deterministically to "first writer wins, second writer
// refused" — never a silent last-writer-wins race.
func (r *Registry) NoteUnregistered(id RuntimeID, diag RegistrationDiagnostic) error {
	if r == nil {
		return errors.New("vendorplugin: cannot note an unregistered runtime in a nil registry")
	}
	normalized, err := NormalizeRuntimeID(string(id))
	if err != nil {
		return err
	}
	// v1 defines exactly one diagnostic shape: Reason == "malformed" carrying
	// the non-nil underlying parse/validation error. Anything else — an
	// unrecognized reason, or "malformed" with no Err — is rejected rather
	// than silently accepted, because a caller-forged or self-minted
	// diagnostic would poison ResolveRuntime's typed refusal for id with
	// evidence nobody validated: an unknown reason would collapse to the
	// generic ErrUnknownRuntime while still blocking DeclareRuntime, and a nil
	// Err would manufacture ErrRuntimeConfigMalformed with nothing behind it.
	if diag.Reason != "malformed" {
		return fmt.Errorf("vendorplugin: noting %s unregistered: unsupported diagnostic reason %q; only \"malformed\" is defined in v1", normalized, diag.Reason)
	}
	if diag.Err == nil {
		return fmt.Errorf("vendorplugin: noting %s unregistered as malformed with no underlying error", normalized)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, declared := r.runtimes[normalized]; declared {
		return fmt.Errorf("%w: %s is already declared; a diagnostic and a live declaration cannot coexist for one id",
			ErrRuntimeConflict, normalized)
	}
	if r.diagnostics == nil {
		r.diagnostics = map[RuntimeID]RegistrationDiagnostic{}
	}
	r.diagnostics[normalized] = diag
	return nil
}

// diagnosticFor returns the noted diagnostic for id, if any, under the
// registry's own lock.
func (r *Registry) diagnosticFor(id RuntimeID) (RegistrationDiagnostic, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	diag, ok := r.diagnostics[id]
	return diag, ok
}

// RuntimeDeclarationOf returns the declaration for an id without resolving it.
// A caller listing what a binary declares — the CLI does — must be able to see
// the muse row rather than only the runtimes whose plugins happen to be
// compiled in.
func (r *Registry) RuntimeDeclarationOf(id RuntimeID) (RuntimeDeclaration, bool) {
	if r == nil {
		return RuntimeDeclaration{}, false
	}
	normalized, err := NormalizeRuntimeID(string(id))
	if err != nil {
		return RuntimeDeclaration{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	declaration, ok := r.runtimes[normalized]
	if !ok {
		return RuntimeDeclaration{}, false
	}
	return declaration.clone(), true
}

// RuntimeDeclarations returns every declaration in sorted id order, deeply
// copied.
func (r *Registry) RuntimeDeclarations() []RuntimeDeclaration {
	if r == nil {
		return []RuntimeDeclaration{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RuntimeDeclaration, 0, len(r.runtimes))
	for _, declaration := range r.runtimes {
		out = append(out, declaration.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Runtime is a materialized pair: the declaration's ids together with the
// plugins they resolve to. Both plugin fields are non-nil in every Runtime a
// caller can obtain — ResolveRuntime refuses rather than returning a half-
// resolved pair, so nothing downstream has to null-check its way through a
// launch.
type Runtime struct {
	ID       RuntimeID
	SystemID agentic.SystemID
	System   agentic.System
	VendorID VendorID
	Vendor   Vendor
}

// ResolveRuntime materializes a declared pair, or says exactly what is missing.
//
// The four refusals are four different facts and are kept apart deliberately:
// the id was never declared; the agentic system plugin is not compiled in; the
// vendor plugin is not compiled in; or the vendor was LOOKED FOR and never
// established. The last is muse, and telling an operator to install a vendor
// nobody has identified would be worse than telling them nothing.
func (r *Registry) ResolveRuntime(id RuntimeID) (Runtime, error) {
	if r == nil {
		return Runtime{}, errors.New("vendorplugin: cannot resolve a runtime in a nil registry")
	}
	normalized, err := NormalizeRuntimeID(string(id))
	if err != nil {
		return Runtime{}, err
	}
	declaration, declared := r.RuntimeDeclarationOf(normalized)
	if !declared {
		if diag, noted := r.diagnosticFor(normalized); noted && diag.Reason == "malformed" {
			return Runtime{}, fmt.Errorf("%w: %s: %v", ErrRuntimeConfigMalformed, normalized, diag.Err)
		}
		return Runtime{}, fmt.Errorf("%w: %s", ErrUnknownRuntime, normalized)
	}
	if r.systems == nil {
		return Runtime{}, fmt.Errorf("%w: resolving runtime %s", ErrNoAgenticRegistry, normalized)
	}
	system, ok := r.systems.Lookup(declaration.System)
	if !ok {
		return Runtime{}, fmt.Errorf("%w: runtime %s names agentic system %q, which no plugin in this binary registers",
			ErrRuntimeSystemUnregistered, normalized, declaration.System)
	}
	if !declaration.VendorResolved() {
		return Runtime{}, fmt.Errorf("%w: runtime %s was recorded with an unknown broker after checking %v; nothing here will guess one, because the binding keys limit state",
			ErrRuntimeVendorUnresolved, normalized, declaration.Broker.Checked)
	}
	vendor, ok := r.Lookup(declaration.Vendor)
	if !ok {
		return Runtime{}, fmt.Errorf("%w: runtime %s names vendor %q, which no plugin in this binary registers",
			ErrRuntimeVendorUnregistered, normalized, declaration.Vendor)
	}
	return Runtime{
		ID:       normalized,
		SystemID: declaration.System,
		System:   system,
		VendorID: declaration.Vendor,
		Vendor:   vendor,
	}, nil
}

// Register adds a vendor to the default registry. It is what a vendor plugin
// package's init function calls.
func Register(vendor Vendor) error { return Default.Register(vendor) }

// DeclareRuntime records a runtime declaration in the default registry.
func DeclareRuntime(declaration RuntimeDeclaration) error { return Default.DeclareRuntime(declaration) }
