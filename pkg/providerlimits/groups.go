// Package providerlimits carries the machine-scoped, per-provider-identity
// record of which subscription groups are currently believed exhausted, the
// classifiers that produce that belief from real provider output, and the
// atomic probe claim that lets exactly one spawn test a suppressed group.
//
// The package is deliberately near-self-contained: it imports nothing from the
// board package or the spawner, and nothing here parses configuration —
// resolved configuration (backoff ladder, lease bounds, group overrides) is
// accepted as parameters. Its one dependency is pkg/vendorplugin, which owns
// this module's frozen (runtime, vendor) table — the same vocabulary the
// extraction source kept in pkg/remoteconfig/runtimeid, with the source's
// "broker" spelled VendorID here because a vendor plugin is exactly the thing
// that owns models, authentication and quota. A classifier is a property of
// the broker behind an account, not of the runtime token this package indexes
// state by (see HasClassifier and HasClassifierForRuntime below), and
// vendorplugin.FrozenRuntimes is the single source of truth for that binding.
// Nothing here duplicates it into a second table.
//
// Two invariants govern everything in here and are worth stating once:
//
//   - Availability is established only by a launch. Provider prose can never
//     suppress a group and can never extend a suppression; a parsed reset hint
//     may at most shorten a backoff step through min().
//   - A suppressed group whose backoff step has elapsed is probe-eligible, not
//     available: it is admitted to exactly one caller, the one that wins the
//     atomic probe claim, and stays subtracted for everyone else.
//
// # How this plane reports: the Availability verdict
//
// verdict.go is this port's own addition and the reason the plane lives in this
// repository at all: Store.AvailabilityFor turns what a state read established
// into a vendorplugin.Availability, so a vendor plugin answers "can requests be
// made right now" by asking the plane rather than by inventing an answer. The
// mapping and every decision in it are argued in that file. Nothing here is
// wired into a live vendor yet — consuming the verdict belongs to the story
// that switches task-board onto this tool.
//
// # What is deliberately NOT ported from the source package
//
// Two files of the source's package are absent, and both are absent for a
// reason rather than by oversight:
//
//   - simulate.go and simulate_payload.go, the dev-only fault injector that
//     arms one launch to emit captured provider bytes and exit non-zero. It is
//     LAUNCH-plane behaviour: it has nothing to inject into without a child
//     being started, and the child is the switch story's. Its payload also
//     carries a captured provider response body verbatim, which puts harness
//     wire bytes inside the limit plane and trips this module's codex-argv
//     guard on a service_tier field it did not construct — a guard that is
//     right to be blunt, and not worth blunting for code this task does not
//     need. Layout keeps SimulateFile and SimulateLockFile so a later port
//     lands on the same paths, and Report drops the armed_token field rather
//     than carrying one that could only ever be null.
//   - The source's per-runtime provider-home table. That fact is declared by
//     the agentic system plugin here; see identity.go above DefaultProviderHome.
package providerlimits

import (
	"fmt"
	"sort"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// Canonical provider IDs. These are runtime IDs (vendorplugin.RuntimeID), not
// brokers: they are what IdentityKey, UnmappedGroup and every persisted
// group key are keyed on, and that keying must never move (see groups.go's
// package doc and identity.go's IdentityKey doc). Limit *detection* is scoped
// to these two runtimes today, because their brokers — anthropic and openai —
// are the two whose real rate-limit error shapes have been captured. Other
// runtimes remain legal spawn-policy members; they simply resolve to a broker
// with no classifier, so nothing can ever suppress them.
const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
)

// Canonical broker IDs this package ships a classifier for. HasClassifier keys
// on these, not on the runtime constants above — see D4 in the extraction
// source's .research/260817_agentic-system-broker-model-config-design.md. A
// runtime resolves to its broker through vendorplugin.FrozenRuntimes, never
// through a second table declared here.
const (
	BrokerAnthropic = "anthropic"
	BrokerOpenAI    = "openai"
)

// brokerForRuntime resolves the broker that owns a runtime's quota/account,
// using this module's frozen (runtime, vendor) table — the single source of
// truth for that binding, and the direct carry-over of the extraction source's
// runtimeid table. It returns "" when the runtime is not one the frozen table
// ships, or when that binding's vendor was never established
// (vendorplugin.VendorUnresolved, e.g. "muse" today): both cases mean this
// package has no basis to classify it, which HasClassifier's "" branch already
// treats as unclassifiable.
//
// It walks the frozen SLICE rather than caching an id-keyed map, for the
// reason vendorplugin's own comment on frozenRuntimes gives: a second table
// keyed by runtime id is the shadow table the single-source guard fails the
// build over, and this package resolving through the public accessor is what
// keeps the binding in one place.
func brokerForRuntime(runtime string) string {
	id, err := vendorplugin.NormalizeRuntimeID(runtime)
	if err != nil {
		return ""
	}
	for _, declaration := range vendorplugin.FrozenRuntimes() {
		if declaration.ID != id {
			continue
		}
		if !declaration.VendorResolved() {
			return ""
		}
		return declaration.Vendor.String()
	}
	return ""
}

// Provenance records where a group row's membership claim comes from, so an
// assertion is never mistaken for vendor evidence.
type Provenance string

const (
	// ProvenanceVendorStringEvidence means the split is proven by strings
	// shipped in the vendor's own binary.
	ProvenanceVendorStringEvidence Provenance = "vendor_string_evidence"
	// ProvenanceOwnerAssertion means the owner asserted the split; there is
	// circumstantial evidence but no shipped string proving it.
	ProvenanceOwnerAssertion Provenance = "owner_assertion"
)

// GroupRow is one subscription-group row: a pooled quota bucket and the models
// that draw from it.
//
// Provider is the RUNTIME ID that owns this row, and it is what NewGroupTable
// indexes model membership by — Lookup, Group and UnmappedGroup all key on the
// same runtime ID a caller passes in, so this field's meaning and its "provider"
// wire name are both frozen (see the package doc). Broker is additive: it names
// the backend billed for this row's quota, resolved from runtimeid at
// construction, and exists for classifier association and operator-facing
// display (see HasClassifier). It carries no weight in model-index lookup —
// changing it can never repoint which group a model resolves to.
type GroupRow struct {
	Group      string     `json:"group"`
	Provider   string     `json:"provider"`
	Broker     string     `json:"broker,omitempty"`
	Members    []string   `json:"members"`
	Provenance Provenance `json:"provenance"`
}

// DefaultGroups is the static group table.
//
// It covers Claude and Codex only, and it does so deliberately. A row for a
// provider whose failures are never classified could never be written to, so
// such a row would assert a mapping no code path could exercise. Extending the
// table to a third provider requires capturing that provider's error shapes
// first and shipping a classifier for them.
func DefaultGroups() []GroupRow {
	return []GroupRow{
		{
			Group:    "claude-plan",
			Provider: ProviderClaude,
			Broker:   brokerForRuntime(ProviderClaude),
			Members: []string{
				"claude-opus-5",
				"claude-sonnet-5",
				"claude-haiku-4-5",
				"claude-opus-4-8",
				"claude-opus-4-6",
				"claude-sonnet-4-6",
				"claude-haiku-4-5-20251001",
			},
			Provenance: ProvenanceVendorStringEvidence,
		},
		{
			Group:      "claude-usage-credits",
			Provider:   ProviderClaude,
			Broker:     brokerForRuntime(ProviderClaude),
			Members:    []string{"claude-fable-5"},
			Provenance: ProvenanceVendorStringEvidence,
		},
		{
			Group:    "codex-plan",
			Provider: ProviderCodex,
			Broker:   brokerForRuntime(ProviderCodex),
			Members: []string{
				"gpt-5.6-sol",
				"gpt-5.6-terra",
				"gpt-5.6-luna",
				"gpt-5.5",
				"gpt-5.4",
				"gpt-5.4-mini",
				"gpt-5.3-codex",
				"gpt-5.2-codex",
				"gpt-5.2",
				"gpt-5.1-codex-max",
				"gpt-5.1-codex-mini",
			},
			Provenance: ProvenanceOwnerAssertion,
		},
		{
			Group:      "codex-spark",
			Provider:   ProviderCodex,
			Broker:     brokerForRuntime(ProviderCodex),
			Members:    []string{"gpt-5.3-codex-spark"},
			Provenance: ProvenanceOwnerAssertion,
		},
	}
}

// UnmappedGroupSuffix separates the provider prefix from the model ID in the
// singleton group an unmapped model resolves to.
const UnmappedGroupSuffix = "-unmapped:"

// UnmappedGroup is the singleton group of a model the table does not map. A
// model registered after the table was written must never silently inherit
// another pool's suppression, so it gets a pool of its own.
func UnmappedGroup(provider, model string) string {
	return provider + UnmappedGroupSuffix + model
}

// GroupRef is the resolved group of one (provider, model) pair.
type GroupRef struct {
	Group      string
	Provider   string
	Provenance Provenance
	// Unmapped is true when the model was not in the table and resolved to a
	// singleton group. Nothing can suppress such a group unless the provider
	// also has a classifier.
	Unmapped bool
}

// GroupTable maps a (provider, model) pair to exactly one group.
//
// Group membership is never derived from capability rank: quota pool and
// capability are unrelated axes, and conflating them is exactly the mistake the
// admission contract removed.
type GroupTable struct {
	rows    []GroupRow
	byModel map[string]GroupRef
}

func modelKey(provider, model string) string { return provider + "\x00" + model }

// NewGroupTable composes the default table with a caller-resolved override set.
//
// Overrides REPLACE whole rows: an override naming an existing group supplies
// that group's complete membership, it does not merge into it. An override
// naming a new group adds a row. A model that ends up in two rows is a
// configuration error, reported rather than silently resolved.
//
// This function does not parse configuration; it consumes rows a caller has
// already resolved.
func NewGroupTable(overrides []GroupRow) (*GroupTable, error) {
	byGroup := map[string]GroupRow{}
	order := []string{}
	for _, row := range DefaultGroups() {
		byGroup[row.Group] = row
		order = append(order, row.Group)
	}
	for _, row := range overrides {
		group := strings.TrimSpace(row.Group)
		if group == "" {
			return nil, fmt.Errorf("providerlimits: group override with an empty group name")
		}
		if strings.TrimSpace(row.Provider) == "" {
			return nil, fmt.Errorf("providerlimits: group override %q has no provider", group)
		}
		replacement := GroupRow{
			Group:      group,
			Provider:   row.Provider,
			Broker:     row.Broker,
			Members:    append([]string(nil), row.Members...),
			Provenance: row.Provenance,
		}
		if replacement.Provenance == "" {
			replacement.Provenance = ProvenanceOwnerAssertion
		}
		if _, existing := byGroup[group]; !existing {
			order = append(order, group)
		}
		byGroup[group] = replacement
	}

	table := &GroupTable{byModel: map[string]GroupRef{}}
	for _, group := range order {
		row := byGroup[group]
		table.rows = append(table.rows, row)
		for _, member := range row.Members {
			member = strings.TrimSpace(member)
			if member == "" {
				return nil, fmt.Errorf("providerlimits: group %q lists an empty model ID", row.Group)
			}
			key := modelKey(row.Provider, member)
			if prior, dup := table.byModel[key]; dup {
				return nil, fmt.Errorf(
					"providerlimits: model %q of provider %q is in two groups (%q and %q); every model must be in exactly one group",
					member, row.Provider, prior.Group, row.Group)
			}
			table.byModel[key] = GroupRef{
				Group:      row.Group,
				Provider:   row.Provider,
				Provenance: row.Provenance,
			}
		}
	}
	return table, nil
}

// MustGroupTable is NewGroupTable over the default rows only. The default table
// is a compile-time constant of this package and cannot be invalid.
func MustGroupTable() *GroupTable {
	table, err := NewGroupTable(nil)
	if err != nil {
		panic(err)
	}
	return table
}

// Rows returns the effective table in a deterministic order.
func (t *GroupTable) Rows() []GroupRow {
	out := make([]GroupRow, len(t.rows))
	copy(out, t.rows)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Group < out[j].Group
	})
	return out
}

// Lookup resolves one (provider, model) pair. Every model resolves to exactly
// one group: a table row when it is mapped, its own singleton group otherwise.
func (t *GroupTable) Lookup(provider, model string) GroupRef {
	if ref, ok := t.byModel[modelKey(provider, model)]; ok {
		return ref
	}
	return GroupRef{
		Group:    UnmappedGroup(provider, model),
		Provider: provider,
		Unmapped: true,
	}
}

// Group is Lookup's group name.
func (t *GroupTable) Group(provider, model string) string {
	return t.Lookup(provider, model).Group
}

// HasClassifier reports whether this package can ever classify a failure of the
// named BROKER. A broker with no classifier can never be suppressed, which is
// why an unmapped model whose runtime resolves to such a broker is harmless.
//
// A 429/limit shape is a property of the API behind the account, not of the
// harness that drives it (design D4). It therefore keys on broker, not on the
// runtime ID that Lookup/Group/UnmappedGroup and every persisted group key
// use — those keep keying on the runtime ID unchanged. A caller holding a
// runtime ID rather than an already-resolved broker should call
// HasClassifierForRuntime instead of resolving it by hand.
func HasClassifier(broker string) bool {
	switch broker {
	case BrokerAnthropic, BrokerOpenAI:
		return true
	default:
		return false
	}
}

// HasClassifierForRuntime is HasClassifier for a caller that only has a
// runtime ID. It resolves the runtime's broker through
// vendorplugin.FrozenRuntimes — never through a second table — and reports
// HasClassifier for that broker. An unrecognised runtime, or one whose vendor
// binding is unestablished (vendorplugin.VendorUnresolved), resolves to no
// classifier: an absent or unproven broker binding must never be read as
// "classifiable".
func HasClassifierForRuntime(runtime string) bool {
	return HasClassifier(brokerForRuntime(runtime))
}
