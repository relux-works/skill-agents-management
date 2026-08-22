// Package google is the vendor plugin for Google's Gemini and Gemma models.
//
// It is a port of the google half of skill-project-management's model registry
// (tools/board-cli/internal/spawn/models.go, the knownModels rows whose broker
// is `google`).
//
// # One vendor, two harnesses
//
// Seven of these rows are driven by the Gemini CLI and eight by Antigravity.
// They are ONE VENDOR because the broker — who owns the account, the quota and
// the billing — is the same, and two RUNTIMES because the harness, the argv
// grammar and the limit-state identity are not. That split is why the source
// says a capability rank is comparable within a BROKER rather than within a
// harness, and why the two runtimes resolve separately: models.go records what
// this port had to decide when two rows from different harnesses carried the
// same score.
//
// # No reasoning-effort axis anywhere in this vendor
//
// Every row here declares EffortSupportNone, and for two different reasons
// worth keeping apart. The Gemini CLI rows have no effort dimension at all —
// the source's reasoningeffort package answers "unsupported" for the gemini
// provider under any model. The Antigravity rows DO vary by effort, but the
// effort is encoded in the model ID itself (`gemini-3.6-flash-high`), which is
// the catalogue the `agy` CLI publishes; exposing a separate effort control on
// top of that would let an operator ask for `gemini-3.6-flash-high` at low
// effort, which is not a thing that exists. Handing any row here an effort is
// refused rather than dropped.
//
// # What a vendor owns, and what it therefore does not
//
// Models, authentication and quota. The two binaries, their argv grammars,
// their environment contracts and their result envelopes belong to
// pkg/agentic/systems/gemini and pkg/agentic/systems/agy.
package google

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// BOTH agentic systems this vendor's models declare. See the package doc.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
)

// vendorID is this plugin's identity, and the spelling the frozen runtime
// table binds BOTH the `gemini` and the `agy` runtimes to. On-disk limit state
// is keyed by (provider, home), so this value must never move.
const vendorID vendorplugin.VendorID = "google"

// Vendor is the google vendor plugin. It holds NO state.
type Vendor struct{}

// New returns the plugin, so a caller building an isolated registry can
// register it without depending on package initialization order.
func New() *Vendor { return &Vendor{} }

// init registers the plugin into the default registry. A failure PANICS: every
// refusal Register can produce here is a build-time bug in this package.
func init() {
	if err := vendorplugin.Register(New()); err != nil {
		panic(fmt.Sprintf("google: registering the vendor plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*Vendor) ID() vendorplugin.VendorID { return vendorID }

// Models is the vendor's model list, deeply copied on every call.
func (*Vendor) Models() []vendorplugin.Model { return vendorplugin.CloneModels(models) }

// Availability reports UNCHECKED: the limit plane is a separate port
// (STORY-260821-2m8cpr) and this plugin reads no source yet.
func (*Vendor) Availability(vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return vendorplugin.Unchecked(), nil
}

// Spawn adds nothing to the launch. See vendorplugin.PassthroughLaunch.
func (*Vendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return vendorplugin.PassthroughLaunch(sc), nil
}
