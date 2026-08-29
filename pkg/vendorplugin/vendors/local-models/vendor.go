package localmodels

import (
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// VendorID is this plugin's identity.
const VendorID vendorplugin.VendorID = "local-models"

// Vendor is the local-models vendor plugin.
//
// Unlike every other vendor plugin in this module it is NOT stateless: it
// holds the already-resolved Config a caller obtained from Peek() (never
// re-reading or re-parsing it), and a StatusReader for Availability's
// machine-local status reads. Both are supplied at construction, never
// resolved internally, so a caller building the shared registry controls
// exactly when — and whether — this vendor exists at all (§2.2.2's
// conditional registration).
type Vendor struct {
	cfg    Config
	status localruntime.StatusReader
}

// Option configures a Vendor at construction.
type Option func(*Vendor)

// WithStatusReader overrides the StatusReader Availability reads through.
// Tests use this to avoid ever invoking a real agents-infra binary; a real
// caller passes a localruntime.NewCLIStatusReader().
func WithStatusReader(reader localruntime.StatusReader) Option {
	return func(v *Vendor) { v.status = reader }
}

// New returns the vendor plugin for an already-resolved, valid Config.
//
// It performs NO file I/O and NO validation of its own: cfg is expected to
// be the Config half of a ConfigResult that already came back non-Absent and
// with a nil Err from Peek(). A caller with a zero-model Config gets a
// Vendor whose Models() returns zero rows — Registry.Register's own
// unconditional ErrNoModels check is what refuses that, not this
// constructor.
func New(cfg Config, opts ...Option) *Vendor {
	v := &Vendor{cfg: cfg, status: localruntime.NewCLIStatusReader()}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// ID answers the frozen identifier, stably and in its own normalized
// spelling.
func (*Vendor) ID() vendorplugin.VendorID { return VendorID }

// Models is the vendor's model list, deeply copied on every call — as it
// must be, since this Vendor holds a Config value it never mutates and never
// re-reads, so every call already answers from the same snapshot.
func (v *Vendor) Models() []vendorplugin.Model {
	return vendorplugin.CloneModels(buildModels(v.cfg))
}
