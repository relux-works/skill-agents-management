package vendorplugin

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
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
// empty models. A declaration carrying it is a complete, legal RUNTIME
// declaration and an UNLAUNCHABLE one: Registry.ResolveRuntime refuses it with
// ErrRuntimeVendorUnresolved, naming the runtime and saying the broker was
// never established, so a caller learns the fact rather than receiving a
// half-vendor it has to null-check. Every other runtime resolves to a real
// Vendor with the full interface behind it, unchanged.
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
	return d.ID == other.ID && d.System == other.System && d.Vendor == other.Vendor
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
	return nil
}

// runtimeIDSource names where the frozen bindings were read from. Every seed's
// provenance points at it, so a reader chasing "why is agy bound to google"
// gets a file to open rather than a claim to trust.
const runtimeIDSource = "skill-project-management pkg/remoteconfig/runtimeid (frozen table)"

// frozenRuntimes are the six historical runtime ids, carried verbatim from the
// extraction source's frozen table.
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
	},
}

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
