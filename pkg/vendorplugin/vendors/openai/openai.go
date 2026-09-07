// Package openai is the vendor plugin for OpenAI's Codex and GPT models.
//
// It is a port of the openai half of skill-project-management's model registry
// (tools/board-cli/internal/spawn/models.go, the knownModels rows whose broker
// is `openai`) together with the reasoning-effort vocabularies that repository
// declares in pkg/remoteconfig/reasoningeffort. Every row's identifier,
// agentic-system binding, effort vocabulary and recommended effort is the
// source's; models.go says what is not.
//
// # What a vendor owns, and what it therefore does not
//
// A vendor owns models, authentication and quota. It does not own the harness:
// the `codex` binary, the argv grammar, the `-c model_reasoning_effort=...`
// flag that CARRIES an effort, the environment filter and the profile handling
// belong to pkg/agentic/systems/codex. What this plugin declares about effort
// is the VOCABULARY and the recommended value — which words each model
// accepts, not how one reaches the child.
//
// # Two vocabularies, not one
//
// This vendor is the one whose rows genuinely disagree with each other about
// effort: the five legacy ids accept `minimal` and the nine current ones do
// not, and the current top rows accept `ultra` while the older current rows
// stop at `xhigh`. (Nine, not eight: `astra` is a current row of its own, an
// alias whose effort word is validated against ITS vocabulary before the launch
// substitutes `gpt-6-astra` in.) That is why invariant 4 of docs/architecture.md makes effort
// a per-MODEL axis rather than a per-provider one — a single provider-level
// vocabulary here would either refuse a word `gpt-5.3-codex` accepts or admit
// one `gpt-5.6-sol` does not.
//
// # The dependency direction, made structural
//
// The blank import below is not a convenience. Registry.Register refuses a
// vendor naming an agentic system nobody registered, so without it this
// package's own init would fail in a binary that had not separately imported
// the codex system.
package openai

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// The agentic system this vendor's models declare. See the package doc.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
)

// vendorID is this plugin's identity, and the spelling the frozen runtime
// table binds the `codex` runtime to. On-disk limit state is keyed by
// (provider, home), so this value must never move.
const vendorID vendorplugin.VendorID = "openai"

// Vendor is the openai vendor plugin. It holds NO state.
type Vendor struct{}

// New returns the plugin, so a caller building an isolated registry can
// register it without depending on package initialization order.
func New() *Vendor { return &Vendor{} }

// init registers the plugin into the default registry. A failure PANICS: every
// refusal Register can produce here is a build-time bug in this package, and a
// plugin that silently failed to register would surface far from its cause.
func init() {
	if err := vendorplugin.Register(New()); err != nil {
		panic(fmt.Sprintf("openai: registering the vendor plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*Vendor) ID() vendorplugin.VendorID { return vendorID }

// Models is the vendor's model list, deeply copied on every call so a caller
// cannot vary it for everybody else.
func (*Vendor) Models() []vendorplugin.Model { return vendorplugin.CloneModels(models) }

// Availability reports UNCHECKED, which is a statement rather than a stub: the
// limit plane is a separate port (STORY-260821-2m8cpr) and this plugin reads no
// source yet. Healthy would be a health claim resting on nothing, and
// UnknownAfterCheck would name sources nobody read.
func (*Vendor) Availability(vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return vendorplugin.Unchecked(), nil
}

// Spawn adds nothing to the launch. See vendorplugin.PassthroughLaunch for why
// that is the faithful port rather than an unfinished one.
func (*Vendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return vendorplugin.PassthroughLaunch(sc), nil
}
