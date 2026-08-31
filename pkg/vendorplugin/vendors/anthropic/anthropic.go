// Package anthropic is the vendor plugin for Anthropic's Claude models.
//
// It is a port of the anthropic half of skill-project-management's model
// registry (tools/board-cli/internal/spawn/models.go, the knownModels rows
// whose broker is `anthropic`) together with the per-model reasoning-effort
// vocabularies that repository declares in
// pkg/remoteconfig/reasoningeffort. Every row's identifier, agentic-system
// binding, effort vocabulary and recommended effort is the source's; see
// models.go for what is NOT, and why.
//
// # What a vendor owns, and what it therefore does not
//
// A vendor owns models, authentication and quota. It does not own the harness:
// the `claude` binary, the argv grammar, the environment filter, the goal
// directive and the reasoning-effort TRANSPORT all belong to
// pkg/agentic/systems/claude, and nothing here restates one. What this plugin
// declares about effort is the VOCABULARY — which words each model accepts —
// and the value the vendor recommends. How a word reaches the child is the
// system's business.
//
// # The dependency direction, made structural
//
// The blank import below is not a convenience. A vendor plugin depends on the
// agentic-system plugins that drive its models, and Registry.Register refuses a
// vendor naming a system nobody registered. Without that import this package's
// own init would fail in any binary that had not separately imported the claude
// system — so the dependency is expressed where it is enforced, and a reader
// who deletes the import gets a panic naming both ids rather than a launch
// that resolves to nothing.
package anthropic

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// The agentic system this vendor's models declare. See the package doc.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

// vendorID is this plugin's identity, and it is the same spelling the frozen
// runtime table binds the `claude` runtime to. On-disk limit state is keyed by
// (provider, home), so this value must never move.
const vendorID vendorplugin.VendorID = "anthropic"

// Vendor is the anthropic vendor plugin.
//
// It holds NO state. Every answer is computed from the declaration in
// models.go or from the SpawnContext it is handed, which is what lets one
// registered value serve concurrent launches.
type Vendor struct{}

// New returns the plugin. It is exported so a caller building an isolated
// registry — every test that does not want the process-wide default — can
// register it without depending on package initialization order.
func New() *Vendor { return &Vendor{} }

// init registers the plugin into the default registry, which is the
// registration path docs/architecture.md describes and the one the CLI reads.
//
// A failure PANICS rather than being dropped. Register refuses a duplicate id,
// an unnormalized id, a blank usage description, a rank with no evidence, an
// effort declaration that contradicts itself and a model naming an
// unregistered agentic system — every one of those is a build-time bug in this
// package, and a plugin that silently failed to register would surface as "no
// vendor registered" with nothing anywhere naming why.
func init() {
	if err := vendorplugin.Register(New()); err != nil {
		panic(fmt.Sprintf("anthropic: registering the vendor plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*Vendor) ID() vendorplugin.VendorID { return vendorID }

// Models is the vendor's model list. It is deeply copied on every call: the
// contract requires the list not to vary between reads, and a caller that
// could edit the answer could vary it for everybody else.
func (*Vendor) Models() []vendorplugin.Model { return vendorplugin.CloneModels(models) }

// Availability reports UNCHECKED, and that is a statement rather than a stub.
//
// The limit plane — the state files, the classifiers, the backoff ladders and
// the probe leases — is a separate port (STORY-260821-2m8cpr). Until it lands,
// this plugin reads no source, so the only honest verdict is the one that says
// nobody looked. The alternative shapes are both lies a caller would act on:
// Healthy would be a health claim resting on nothing, and UnknownAfterCheck
// would name sources that were never read.
//
// Nothing here fails open. Invariant 2's fail-open rule is about an ABSENT
// limit-state FILE reading as "provider healthy", and it belongs to the plane
// that reads that file — not to a plugin that has not yet been given one.
func (*Vendor) Availability(vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return vendorplugin.Unchecked(), nil
}

// Spawn adds nothing to the launch. See vendorplugin.PassthroughLaunch for why
// that is the faithful port rather than an unfinished one.
func (*Vendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return vendorplugin.PassthroughLaunch(sc), nil
}
