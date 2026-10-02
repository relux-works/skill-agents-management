package vendorplugin

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/benchdata"
)

// RuntimeID is the stable identifier of a declared (agentic system × vendor)
// pair.
//
// docs/architecture.md: the existing ids remain valid forever. They feed
// admitted-pair digests, limit-state filenames and free-text records
// downstream, and renaming one orphans that state silently — the rename would
// look like a cleanup and read like a working system until somebody went
// looking for state that no longer had a name.
type RuntimeID string

// String renders the identifier for error text and CLI output.
func (id RuntimeID) String() string { return string(id) }

// NormalizeRuntimeID folds a runtime identifier through the module's single
// identifier normalization (internal/ident).
func NormalizeRuntimeID(raw string) (RuntimeID, error) {
	normalized, reason, ok := ident.Normalize(raw)
	if !ok {
		return "", &InvalidIDError{Kind: "runtime id", Raw: raw, Reason: reason, Example: "claude"}
	}
	return RuntimeID(normalized), nil
}

// VendorUnresolved is the vendor of a runtime whose broker was LOOKED FOR and
// not established.
//
// It exists for exactly one recorded fact: the extraction source's
// pkg/remoteconfig/runtimeid table carries muse with an UNKNOWN broker and a
// checked-and-empty evidence list. Guessing a vendor for it — "muse is local,
// call it local" — would be inventing a binding that feeds limit-state
// filenames, which is the one thing invariant 2 says never to move casually.
//
// # Why this does not weaken the vendor interface
//
// It is not a vendor. Nothing implements it, nothing can be registered under
// it, and no code path treats an unresolved vendor as a degraded vendor with
// empty models. Registry.ResolveRuntime refuses it with
// ErrRuntimeVendorUnresolved, naming the runtime and saying the broker was
// never established, so a caller never receives a half-vendor it has to
// null-check. BuildLaunch has one narrower, explicit system-only binding for
// such a declaration when it carries validated declaration-owned model rows;
// it launches through the system without pretending those rows came from a
// vendor. Every other runtime resolves to a real Vendor with the full
// interface behind it, unchanged.
//
// The escape is a declaration, not a code change: the day muse's broker is
// established, the declaration names it and the runtime becomes launchable.
const VendorUnresolved VendorID = ""

// BrokerProvenance records how a runtime's vendor binding was established, and
// is REQUIRED on every declaration.
//
// A blank vendor with no provenance and a blank vendor after a real search are
// indistinguishable in a struct that does not carry this, and they are
// completely different facts: the first is an unfinished declaration, the
// second is a finding. Requiring it makes "unknown" a statement with a search
// behind it rather than an empty field somebody forgot.
type BrokerProvenance struct {
	// Checked names where the binding was looked for. Required, always: a
	// declaration that names no source has not been established, whether or
	// not it found a vendor.
	Checked []string
	// Found states what established the binding. It must be non-blank when a
	// vendor is named and blank when the vendor is VendorUnresolved — the
	// checked-and-empty shape.
	Found string
}

// RuntimeDeclaration is a runtime: a stable id bound to one agentic system and
// one vendor.
//
// It is a DECLARATION, not a resolution. Neither the system nor the vendor has
// to be registered for a declaration to be legal, and that is deliberate: the
// six historical ids are seeded into the default registry at init, before any
// plugin package has registered anything, and a declaration that refused to
// exist until its plugins did would make the seed order load-bearing. What
// cannot be faked is the resolution — ResolveRuntime materializes the pair and
// names precisely what is missing when it cannot.
type RuntimeDeclaration struct {
	ID     RuntimeID
	System agentic.SystemID
	Vendor VendorID
	Broker BrokerProvenance
	// Engine is the runtime/profile's requested inference-engine dependency.
	// Zero means this runtime has no engine requirement.
	Engine plugin.Ref

	// Models are the model rows of a VENDOR-UNRESOLVED runtime, and they are
	// legal on no other kind.
	//
	// # Why they live here at all
	//
	// Every other runtime's rows come from the vendor plugin that owns them,
	// which is the whole point of the vendor contract. A runtime whose broker
	// was looked for and never established has no such plugin — and "no plugin
	// owns this model" is not evidence that the model has no context window,
	// no lineup state and no effort axis. Letting the fields go unstated would
	// be an absence read as a finding, which is the one mistake
	// BrokerProvenance exists to prevent one field earlier.
	//
	// So the unresolved-vendor shape carries them. That is the shape being
	// used as designed rather than extended for a special case: these rows make
	// the declaration complete about its models and form BuildLaunch's explicit
	// system-only binding. ResolveRuntime still refuses it with
	// ErrRuntimeVendorUnresolved, unchanged, because that public API promises a
	// fully materialized vendor pair.
	//
	// # Why a resolved runtime may not carry them
	//
	// Because that is the second table. A runtime whose vendor is established
	// reads its models from that vendor's plugin, and a list here as well
	// would be two declarations of one fact, agreeing until one of them was
	// edited. Validate refuses it.
	//
	// The escape stays a declaration rather than a code change, as it is for
	// the vendor itself: the day this runtime's broker is established, a
	// vendor plugin takes the rows and the declaration drops them.
	Models []Model
}

// VendorResolved reports whether this declaration names a vendor at all.
func (d RuntimeDeclaration) VendorResolved() bool { return d.Vendor != VendorUnresolved }

// SameBinding reports whether two declarations bind the same id to the same
// pair.
//
// The BINDING is (system, vendor); the provenance is documentation of how it
// was established and is deliberately not compared. Two callers declaring the
// same pair with differently worded evidence have not disagreed about
// anything, and refusing the second would make the F2 idempotency rule turn on
// prose.
func (d RuntimeDeclaration) SameBinding(other RuntimeDeclaration) bool {
	return d.ID == other.ID && d.System == other.System && d.Vendor == other.Vendor && d.Engine == other.Engine
}

// sameSystemOnlyAuthority reports whether two vendor-unresolved declarations
// carry the same complete launch authority.
//
// A system-only runtime has no vendor plugin from which BuildLaunch could read
// its model and effort facts, so Models is authority rather than descriptive
// metadata. Equality therefore covers every Model field recursively: identity,
// system membership, effort support/vocabulary/recommendation, rank and its
// evidence, lifecycle/supersession, display recommendation, context, pricing,
// publisher/family and usage description. Adding a field to Model also adds it
// to this comparison through the whole-value comparison below.
//
// Top-level model row order is deliberately NOT authority. Models are selected
// by ID and ranked by their explicit Rank; declaration order is only a stable
// presentation tie-break. The comparison sorts copies by ID and never rewrites
// either declaration or the first stored winner. Ordering inside an individual
// row remains significant because those slices are themselves published facts
// (for example effort vocabulary and rank evidence), not table presentation.
func (d RuntimeDeclaration) sameSystemOnlyAuthority(other RuntimeDeclaration) bool {
	return systemOnlyModelAuthorityEqual(d.Models, other.Models)
}

func systemOnlyModelAuthorityEqual(left, right []Model) bool {
	if len(left) != len(right) {
		return false
	}
	leftCanonical := CloneModels(left)
	rightCanonical := CloneModels(right)
	sort.Slice(leftCanonical, func(i, j int) bool { return leftCanonical[i].ID < leftCanonical[j].ID })
	sort.Slice(rightCanonical, func(i, j int) bool { return rightCanonical[i].ID < rightCanonical[j].ID })
	return reflect.DeepEqual(leftCanonical, rightCanonical)
}

// VendorLabel renders the vendor for human-facing output, so an unresolved
// binding reads as a stated fact rather than as an empty pair of quotes. The
// CLI and the refusal text share it: an operator who sees "vendor unresolved"
// in a listing and in an error is looking at one fact, not two.
func (d RuntimeDeclaration) VendorLabel() string {
	if !d.VendorResolved() {
		return "vendor unresolved"
	}
	return d.Vendor.String()
}

// Validate refuses a declaration that could not name a runtime.
func (d RuntimeDeclaration) Validate() error {
	normalizedID, err := NormalizeRuntimeID(string(d.ID))
	if err != nil {
		return err
	}
	if normalizedID != d.ID {
		return fmt.Errorf("%w: runtime id %q normalizes to %q; declare the normalized spelling or one runtime has two names",
			ErrRuntimeInvalid, d.ID, normalizedID)
	}
	normalizedSystem, err := agentic.NormalizeSystemID(string(d.System))
	if err != nil {
		return fmt.Errorf("%w: runtime %q: %w", ErrRuntimeInvalid, d.ID, err)
	}
	if normalizedSystem != d.System {
		return fmt.Errorf("%w: runtime %q declares agentic system %q, which normalizes to %q",
			ErrRuntimeInvalid, d.ID, d.System, normalizedSystem)
	}
	if d.VendorResolved() {
		normalizedVendor, err := NormalizeVendorID(string(d.Vendor))
		if err != nil {
			return fmt.Errorf("%w: runtime %q: %w", ErrRuntimeInvalid, d.ID, err)
		}
		if normalizedVendor != d.Vendor {
			return fmt.Errorf("%w: runtime %q declares vendor %q, which normalizes to %q",
				ErrRuntimeInvalid, d.ID, d.Vendor, normalizedVendor)
		}
	}
	if err := validateInferenceEngineRef("runtime "+d.ID.String(), d.Engine); err != nil {
		return err
	}
	if len(d.Broker.Checked) == 0 {
		return fmt.Errorf("%w: runtime %q records no source for its vendor binding; a binding nobody looked for is not a declaration",
			ErrRuntimeInvalid, d.ID)
	}
	for i, source := range d.Broker.Checked {
		if strings.TrimSpace(source) == "" {
			return fmt.Errorf("%w: runtime %q: checked source %d is blank", ErrRuntimeInvalid, d.ID, i)
		}
	}
	found := strings.TrimSpace(d.Broker.Found)
	if d.VendorResolved() && found == "" {
		return fmt.Errorf("%w: runtime %q names vendor %q but records nothing that established it",
			ErrRuntimeInvalid, d.ID, d.Vendor)
	}
	if !d.VendorResolved() && found != "" {
		return fmt.Errorf("%w: runtime %q has an unresolved vendor but records %q as having established one; an unresolved broker is a checked-and-EMPTY finding",
			ErrRuntimeInvalid, d.ID, d.Broker.Found)
	}
	return d.validateModels()
}

// validateModels holds the rows a vendor-unresolved declaration carries to the
// SAME standard a registered vendor's rows are held to, and refuses rows on a
// declaration that has a vendor to get them from.
//
// "The same standard" is the load-bearing half. A row that reached this module
// through the unresolved shape and skipped Model.Validate would be a second,
// weaker admission path for exactly the models nobody else checks — the ones
// with no plugin behind them.
func (d RuntimeDeclaration) validateModels() error {
	if d.VendorResolved() {
		if len(d.Models) > 0 {
			return fmt.Errorf("%w: runtime %q names vendor %q and also declares %d model rows; a runtime whose vendor is established reads its models from that vendor's plugin, and a second list here is two declarations of one fact",
				ErrRuntimeInvalid, d.ID, d.Vendor, len(d.Models))
		}
		return nil
	}
	seen := make([]ModelID, 0, len(d.Models))
	for _, model := range d.Models {
		if err := model.Validate(); err != nil {
			return fmt.Errorf("%w: runtime %q: %w", ErrRuntimeInvalid, d.ID, err)
		}
		for _, already := range seen {
			if already == model.ID {
				return fmt.Errorf("%w: runtime %q declares model %q twice", ErrRuntimeInvalid, d.ID, model.ID)
			}
		}
		seen = append(seen, model.ID)
		// The row must name the runtime's OWN harness. A model listed under a
		// runtime that cannot drive it is a row no launch could ever reach,
		// and the check is here rather than at resolution because an
		// unresolved runtime never resolves — there would be no later moment
		// to catch it.
		if !model.DrivenBy(d.System) {
			return fmt.Errorf("%w: runtime %q is driven by agentic system %q and its model %q declares %v; a row under a runtime whose harness cannot drive it is unreachable",
				ErrRuntimeInvalid, d.ID, d.System, model.ID, model.Systems)
		}
	}
	if err := checkSupersession(d.Models); err != nil {
		return fmt.Errorf("%w: runtime %q: %w", ErrRuntimeInvalid, d.ID, err)
	}
	if err := checkAliases(d.Models); err != nil {
		return fmt.Errorf("%w: runtime %q: %w", ErrRuntimeInvalid, d.ID, err)
	}
	if err := checkRecommendations(d.Models); err != nil {
		return fmt.Errorf("%w: runtime %q: %w", ErrRuntimeInvalid, d.ID, err)
	}
	return nil
}

// LineupOfDeclaration is the derived total order over a vendor-unresolved
// runtime's own rows, and it answers the empty list for every other runtime —
// whose lineup is its vendor's, through LineupOf.
func (d RuntimeDeclaration) LineupOfDeclaration() []RankedModel { return Lineup(d.Models) }

// runtimeIDSource names where the frozen bindings were read from. Every seed's
// provenance points at it, so a reader chasing "why is agy bound to google"
// gets a file to open rather than a claim to trust.
const runtimeIDSource = "skill-project-management pkg/remoteconfig/runtimeid (frozen table)"

// frozenRuntimes are the six historical runtime ids, carried verbatim from the
// extraction source's frozen table, plus the three native-Pi rows added by
// TASK-260908-ggxfte.
//
// It is a SLICE, not a map: the binding table these declarations end up in is
// the registry's, and a second id-keyed map here would be the shadow table
// pkg/agentic/singlesource_guard_test.go fails the build over. Seeding walks
// this list through the same public DeclareRuntime every other caller uses, so
// the seeds get the same validation and the same F2 collision policy as
// anything declared later.
//
// muse is recorded with an UNRESOLVED vendor and a checked-and-empty
// provenance because that is what the source records. Naming a plausible
// vendor for it here would turn a finding into a fabrication, and the binding
// feeds limit-state filenames.
var frozenRuntimes = []RuntimeDeclaration{
	{
		ID:     "claude",
		System: "claude-code",
		Vendor: "anthropic",
		Broker: BrokerProvenance{Checked: []string{runtimeIDSource}, Found: "the frozen table binds this id to the anthropic broker"},
	},
	{
		ID:     "codex",
		System: "codex",
		Vendor: "openai",
		Broker: BrokerProvenance{Checked: []string{runtimeIDSource}, Found: "the frozen table binds this id to the openai broker"},
	},
	{
		ID:     "qwen",
		System: "qwen-code",
		Vendor: "alibaba",
		Broker: BrokerProvenance{Checked: []string{runtimeIDSource}, Found: "the frozen table binds this id to the alibaba broker"},
	},
	{
		ID:     "gemini",
		System: "gemini-cli",
		Vendor: "google",
		Broker: BrokerProvenance{Checked: []string{runtimeIDSource}, Found: "the frozen table binds this id to the google broker"},
	},
	{
		ID:     "agy",
		System: "antigravity",
		Vendor: "google",
		Broker: BrokerProvenance{Checked: []string{runtimeIDSource}, Found: "the frozen table binds this id to the google broker"},
	},
	{
		ID:     "muse",
		System: "muse",
		Vendor: VendorUnresolved,
		Broker: BrokerProvenance{Checked: []string{runtimeIDSource}},
		Models: museModels(),
	},
	// The three native-Pi rows (TASK-260908-ggxfte rev2 §3.4). They are FROZEN
	// rather than consumer-declared because providerlimits.brokerForRuntime
	// walks this slice only: a consumer-declared pi-anthropic would resolve to
	// no broker there, take the unclassifiable carve-out, and read Healthy
	// while its managed home held a limited record. The binding is what the
	// installed Pi catalog established — its provider data files are literally
	// anthropic.json, openai.json and google.json — and that is the checked
	// source recorded here.
	{
		ID:     "pi-anthropic",
		System: "pi-native",
		Vendor: "anthropic",
		Broker: BrokerProvenance{Checked: []string{piCatalogSource("anthropic")}, Found: "the installed Pi catalog resolves anthropic/<model> against its anthropic provider data"},
	},
	{
		ID:     "pi-openai",
		System: "pi-native",
		Vendor: "openai",
		Broker: BrokerProvenance{Checked: []string{piCatalogSource("openai")}, Found: "the installed Pi catalog resolves openai/<model> against its openai provider data"},
	},
	{
		ID:     "pi-google",
		System: "pi-native",
		Vendor: "google",
		Broker: BrokerProvenance{Checked: []string{piCatalogSource("google")}, Found: "the installed Pi catalog resolves google/<model> against its google provider data"},
	},
}

// piCatalogSource names the installed Pi provider data file a native-Pi runtime
// row was checked against, spelled once.
func piCatalogSource(vendor string) string {
	return "pi 0.84.2 pi-ai providers/data/" + vendor + ".json"
}

// museModels projects the data-only unresolved-runtime declarations.
func museModels() []Model { return ModelsFromBenchdata(benchdata.MuseModels()) }

// FrozenRuntimes returns the seeded declarations in their declared order.
//
// The slice and every mutable field in it are copied: a caller that appends to
// or overwrites the answer must not be able to edit the frozen table through
// it, and the provenance slices are exactly the kind of shared backing array
// that makes that possible without anyone noticing.
func FrozenRuntimes() []RuntimeDeclaration {
	out := make([]RuntimeDeclaration, 0, len(frozenRuntimes))
	for _, declaration := range frozenRuntimes {
		out = append(out, declaration.clone())
	}
	return out
}

func (d RuntimeDeclaration) clone() RuntimeDeclaration {
	copied := d
	if d.Broker.Checked != nil {
		copied.Broker.Checked = append([]string(nil), d.Broker.Checked...)
	}
	if d.Models != nil {
		copied.Models = CloneModels(d.Models)
	}
	return copied
}

// SeedFrozenRuntimes declares the six historical runtimes into a registry.
//
// It goes through the public DeclareRuntime, so seeding twice is the F2
// idempotent case rather than a special path — and a registry that already
// holds a CONFLICTING declaration for one of these ids refuses, which is the
// answer that matters: a frozen id rebound to a different pair is the silent
// state-orphaning docs/architecture.md names.
func SeedFrozenRuntimes(r *Registry) error {
	for _, declaration := range FrozenRuntimes() {
		if err := r.DeclareRuntime(declaration); err != nil {
			return fmt.Errorf("seeding frozen runtime %q: %w", declaration.ID, err)
		}
	}
	return nil
}
