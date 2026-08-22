// Package alibaba is the vendor plugin for Alibaba's Qwen models.
//
// It is a port of the alibaba half of skill-project-management's model registry
// (tools/board-cli/internal/spawn/models.go, the knownModels rows whose broker
// is `alibaba`) together with the reasoning-effort vocabularies that repository
// declares in pkg/remoteconfig/reasoningeffort.
//
// # This is the vendor that proves the architecture
//
// docs/architecture.md calls the dependency direction the load-bearing
// decision, and alibaba is where it pays: five of its rows are driven by the
// qwen-code harness and one by the CODEX harness. A cross-runtime combination —
// an Alibaba model under OpenAI's harness — is therefore ONE VENDOR DECLARING
// ONE MORE SYSTEM, with no core change anywhere. Both systems are blank-imported
// below for exactly that reason, and Registry.Register would refuse this vendor
// if either were missing.
//
// The cross-runtime row's effort vocabulary is the CODEX one, not qwen-code's,
// and that is not an inconsistency: an effort word has to be accepted by the
// harness that carries it, and that row launches through the codex adapter's
// `-c model_reasoning_effort=...` flag. models.go states it at the row.
//
// # What a vendor owns, and what it therefore does not
//
// Models, authentication and quota. The `qwen` binary, its stdin transport for
// effort, the argv grammar and the environment filter belong to
// pkg/agentic/systems/qwen; the codex harness's own surface belongs to
// pkg/agentic/systems/codex. Nothing here restates either.
package alibaba

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// BOTH agentic systems this vendor's models declare. The second one is the
	// cross-runtime case the architecture exists to make cheap. See the
	// package doc.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"
)

// vendorID is this plugin's identity, and the spelling the frozen runtime
// table binds the `qwen` runtime to. On-disk limit state is keyed by
// (provider, home), so this value must never move.
const vendorID vendorplugin.VendorID = "alibaba"

// Vendor is the alibaba vendor plugin. It holds NO state.
type Vendor struct{}

// New returns the plugin, so a caller building an isolated registry can
// register it without depending on package initialization order.
func New() *Vendor { return &Vendor{} }

// init registers the plugin into the default registry. A failure PANICS: every
// refusal Register can produce here is a build-time bug in this package.
func init() {
	if err := vendorplugin.Register(New()); err != nil {
		panic(fmt.Sprintf("alibaba: registering the vendor plugin: %v", err))
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
