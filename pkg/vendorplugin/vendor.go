// Package vendorplugin is Layer 2 of agents-management: the contract a vendor
// plugin implements, the registry that is the only place a vendor or runtime
// binding may live, and the declared (agentic system × vendor) pairs those
// bindings resolve to.
//
// A vendor owns models, authentication and quota — anthropic, openai, alibaba,
// google. It does NOT own the harness: the binary, the argv grammar, the
// environment contract, the stdin transport and the reasoning-effort TRANSPORT
// all belong to pkg/agentic, and nothing here re-declares them. What a vendor
// declares about effort is the VOCABULARY (which words a model accepts) and
// the recommended value; how a word reaches a harness is the system's business
// and is not repeated in this package.
//
// # The dependency direction
//
// A vendor plugin depends on agentic-system plugins and declares which systems
// can drive each of its models. docs/architecture.md calls that the
// load-bearing decision, and Registry.Register enforces it: a vendor naming an
// agentic system that is not registered is REFUSED, with both identifiers in
// the error. The direction is what makes a cross-runtime combination — Qwen
// models under the Codex harness — a vendor declaring one more system rather
// than a core change.
//
// # What is deliberately not here
//
// No concrete vendor plugins: they are the next story. No spawn EXECUTION:
// BuildLaunch produces the agentic.Plan and stops, and the process start is a
// port story. No limit plane: Availability is the verdict TYPE that plane will
// report through, and the resource-awareness design docs/architecture.md
// sketches for local models fits behind it without an interface break —
// availability.go documents exactly where.
package vendorplugin

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// VendorID is the stable identifier of a vendor plugin, normalized to one
// spelling. On-disk limit state is keyed by (provider, home) — invariant 2 of
// docs/architecture.md — and the provider half is this value, so two spellings
// of one vendor silently orphan state exactly the way two spellings of a
// system do.
type VendorID string

// String renders the identifier for error text and CLI output.
func (id VendorID) String() string { return string(id) }

// ModelID is a vendor's own name for one model.
//
// Unlike a plugin identifier it is NOT folded. A model id is a foreign key
// into the vendor's API, and lowercasing or re-spelling it would send a name
// the vendor never issued. It is only checked for the shapes that could not be
// a name at all: blank, or carrying whitespace that a copy-paste dropped in.
type ModelID string

// String renders the identifier for error text and CLI output.
func (id ModelID) String() string { return string(id) }

// InvalidIDError says why an identifier was refused, naming the kind of id,
// the raw spelling, the rule it broke and an example of a legal spelling of
// that kind. The refusal has to be enough to fix the declaration from the
// error text alone.
type InvalidIDError struct {
	Kind    string
	Raw     string
	Reason  string
	Example string
}

func (e *InvalidIDError) Error() string {
	return fmt.Sprintf("invalid %s %q: %s; %s, for example %q", e.Kind, e.Raw, e.Reason, ident.Rule, e.Example)
}

// NormalizeVendorID folds a vendor identifier through the module's single
// identifier normalization (internal/ident).
func NormalizeVendorID(raw string) (VendorID, error) {
	normalized, reason, ok := ident.Normalize(raw)
	if !ok {
		return "", &InvalidIDError{Kind: "vendor id", Raw: raw, Reason: reason, Example: "anthropic"}
	}
	return VendorID(normalized), nil
}

// ValidateModelID refuses the spellings that cannot be a vendor's model name.
// It does not normalize: see ModelID.
func ValidateModelID(raw ModelID) error {
	value := string(raw)
	if strings.TrimSpace(value) == "" {
		return &InvalidIDError{Kind: "model id", Raw: value, Reason: "it is empty", Example: "claude-opus-4-5"}
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, " \t\r\n") {
		return &InvalidIDError{Kind: "model id", Raw: value, Reason: "it carries whitespace, and a model id is sent to the vendor verbatim", Example: "claude-opus-4-5"}
	}
	return nil
}

// UsageDescription says what a model is BEST USED FOR, in an operator's
// language rather than a benchmark's.
//
// The zero value is INVALID and Registry.Register refuses it. That is the
// whole point of the named type: an operator choosing between six models reads
// this field, and a plugin that leaves it blank has published a model nobody
// can choose deliberately. A blank description is therefore a registration
// failure that names the vendor and the model, not a field that quietly
// renders as an empty column.
//
// What the type cannot do is judge the CONTENT — "a model" satisfies it. The
// silent-empty case is the one a type can close, and it is closed.
type UsageDescription string

// String renders the description for CLI output.
func (d UsageDescription) String() string { return string(d) }

// Validate refuses a description that says nothing.
func (d UsageDescription) Validate() error {
	if strings.TrimSpace(string(d)) == "" {
		return fmt.Errorf("%w: the field says what the model is best used for, and an operator choosing between models reads it", ErrDescriptionEmpty)
	}
	return nil
}

// RankEvidence is one observation a capability ranking rests on.
//
// Both fields are required. The source repository carries a recorded decision
// that ceiling convenience — "this model is the biggest one we have, so rank
// it first" — is not a ranking argument, and a rank with no observation behind
// it is that argument with the reasoning left out.
type RankEvidence struct {
	// Source names where the observation came from: an eval, a published
	// card, a measured run in this repository's own history.
	Source string
	// Observation is what that source actually showed.
	Observation string
}

// CapabilityRank is a model's position in its OWN vendor's lineup, with the
// evidence for it.
//
// Positions are per-vendor and never comparable across vendors: nothing in
// this module can honestly say a vendor's rank-1 model beats another vendor's,
// and a type that invited the comparison would get one made.
type CapabilityRank struct {
	// Position is 1 for the vendor's most capable model, counting up. Two
	// models of one vendor may not share a position — a ranking that cannot
	// order its own lineup is not a ranking.
	Position int
	// Basis is the evidence for the position. At least one entry is required.
	Basis []RankEvidence
}

// Validate refuses a rank that is out of range or has no evidence behind it.
func (r CapabilityRank) Validate() error {
	if r.Position < 1 {
		return fmt.Errorf("%w: position %d is not a place in the vendor's lineup; 1 is the most capable model", ErrRankInvalid, r.Position)
	}
	if len(r.Basis) == 0 {
		return fmt.Errorf("%w: rank %d carries no evidence, and a ranking with no observation behind it is policy wearing a number", ErrRankInvalid, r.Position)
	}
	for i, evidence := range r.Basis {
		if strings.TrimSpace(evidence.Source) == "" {
			return fmt.Errorf("%w: rank %d evidence %d names no source", ErrRankInvalid, r.Position, i)
		}
		if strings.TrimSpace(evidence.Observation) == "" {
			return fmt.Errorf("%w: rank %d evidence %d from %q records no observation", ErrRankInvalid, r.Position, i, evidence.Source)
		}
	}
	return nil
}

// EffortDeclaration is the per-model reasoning-effort axis as the VENDOR owns
// it: whether the model has one, which words it accepts, and which word the
// vendor recommends.
//
// The transport — argv, stdin, or none — is the agentic system's declaration
// and is not repeated here. A launch needs both halves and gets them from
// their own owners: this package refuses an effort word outside the
// vocabulary, and agentic.BuildPlan refuses a system that cannot carry one.
//
// Recommended is a DECLARATION, not a default. Nothing in this package
// substitutes it for a missing effort: invariant 4 of docs/architecture.md
// makes effort required-or-absent precisely so that no default is injected
// anywhere, and a launch that silently ran at the vendor's recommendation
// instead of the operator's choice is the wrong-cost launch that invariant
// exists to close. The recommendation reaches the operator through the refusal
// text instead.
type EffortDeclaration struct {
	Support     agentic.EffortSupport
	Vocabulary  []string
	Recommended string
}

// Validate refuses a declaration that could not be honoured: a required axis
// with no words, a recommendation outside its own vocabulary, or a vocabulary
// attached to a model that has no effort axis at all.
func (e EffortDeclaration) Validate() error {
	switch e.Support {
	case agentic.EffortSupportRequired:
		if len(e.Vocabulary) == 0 {
			return fmt.Errorf("%w: the model requires an explicit effort and declares no words for one", ErrEffortDeclaration)
		}
		seen := map[string]bool{}
		for _, word := range e.Vocabulary {
			if strings.TrimSpace(word) == "" {
				return fmt.Errorf("%w: the vocabulary carries a blank word", ErrEffortDeclaration)
			}
			if seen[word] {
				return fmt.Errorf("%w: the vocabulary repeats %q", ErrEffortDeclaration, word)
			}
			seen[word] = true
		}
		if !e.Accepts(e.Recommended) {
			return fmt.Errorf("%w: the recommended effort %q is not one of %v", ErrEffortDeclaration, e.Recommended, e.Vocabulary)
		}
	case agentic.EffortSupportNone:
		if len(e.Vocabulary) > 0 || strings.TrimSpace(e.Recommended) != "" {
			return fmt.Errorf("%w: the model declares no effort axis but carries a vocabulary %v and a recommendation %q", ErrEffortDeclaration, e.Vocabulary, e.Recommended)
		}
	default:
		return fmt.Errorf("%w: %s is not a declared effort support", ErrEffortDeclaration, e.Support)
	}
	return nil
}

// Accepts reports whether a word is in this model's vocabulary. Comparison is
// exact on the trimmed word: effort vocabularies are short closed sets the
// vendor publishes, and folding "High" into "high" here would be a second
// normalization of a value this package does not own.
func (e EffortDeclaration) Accepts(word string) bool {
	trimmed := strings.TrimSpace(word)
	if trimmed == "" {
		return false
	}
	for _, declared := range e.Vocabulary {
		if declared == trimmed {
			return true
		}
	}
	return false
}

// Model is one row of a vendor's model list: what it is called, what it is for,
// where it sits in the vendor's lineup and on what evidence, its effort axis,
// and the agentic systems that can drive it.
type Model struct {
	ID          ModelID
	Description UsageDescription
	Rank        CapabilityRank
	Effort      EffortDeclaration

	// Systems are the agentic systems that can drive this model. It must be
	// non-empty — a model no harness can run is not a launchable declaration —
	// and every id must be registered in the agentic registry the vendor
	// registry was built against. That check is the dependency direction, and
	// Registry.Register is where it happens.
	Systems []agentic.SystemID
}

// Validate refuses a row that could not be launched or chosen. It does NOT
// check that the declared systems are registered: that needs the agentic
// registry and belongs to Registry.Register, which names both ids when it
// refuses.
func (m Model) Validate() error {
	if err := ValidateModelID(m.ID); err != nil {
		return err
	}
	if err := m.Description.Validate(); err != nil {
		return fmt.Errorf("model %q: %w", m.ID, err)
	}
	if err := m.Rank.Validate(); err != nil {
		return fmt.Errorf("model %q: %w", m.ID, err)
	}
	if err := m.Effort.Validate(); err != nil {
		return fmt.Errorf("model %q: %w", m.ID, err)
	}
	if len(m.Systems) == 0 {
		return fmt.Errorf("%w: model %q declares no agentic system that can drive it", ErrModelInvalid, m.ID)
	}
	// The declared systems are checked for duplicates against a slice rather
	// than a set keyed by SystemID. A map[SystemID]T anywhere outside the
	// agentic registry is what pkg/agentic/singlesource_guard_test.go fails
	// the build over, and the rule is right to be blunt about it: a set of
	// "systems this thing supports" is one refactor away from being a table of
	// what each of them does. A model declares a handful of systems, so the
	// scan costs nothing.
	seen := make([]agentic.SystemID, 0, len(m.Systems))
	for _, raw := range m.Systems {
		normalized, err := agentic.NormalizeSystemID(string(raw))
		if err != nil {
			return fmt.Errorf("%w: model %q declares an unusable agentic system: %w", ErrModelInvalid, m.ID, err)
		}
		if normalized != raw {
			return fmt.Errorf("%w: model %q declares agentic system %q, which normalizes to %q; declare the normalized spelling or the two name one system twice",
				ErrModelInvalid, m.ID, raw, normalized)
		}
		for _, already := range seen {
			if already == normalized {
				return fmt.Errorf("%w: model %q declares agentic system %q twice", ErrModelInvalid, m.ID, normalized)
			}
		}
		seen = append(seen, normalized)
	}
	return nil
}

// DrivenBy reports whether this model declared the given agentic system.
func (m Model) DrivenBy(id agentic.SystemID) bool {
	normalized, err := agentic.NormalizeSystemID(string(id))
	if err != nil {
		return false
	}
	for _, declared := range m.Systems {
		if declared == normalized {
			return true
		}
	}
	return false
}

// Launchable projects the row onto the one vendor-shaped fact Layer 1 needs:
// the selected model and whether it requires an effort. Everything else — the
// description, the rank, the vocabulary — stays here, which is what keeps the
// two layers from re-declaring each other.
func (m Model) Launchable() agentic.Model {
	return agentic.Model{ID: string(m.ID), Effort: m.Effort.Support}
}

// AvailabilityQuery is what a caller wants an availability verdict about.
//
// Model narrows the question to one row; empty asks about the vendor as a
// whole. The distinction is not cosmetic and it is the seam the local-model
// resource plane needs: a remote vendor answers per account, a local one
// answers per model, because whether a model is loaded and whether inference
// is running on it are per-model facts.
//
// Home is the harness configuration home the on-disk limit state is keyed by
// (invariant 2: IdentityKey(provider, home)). A caller asking about a
// non-default home must be able to say so, or the verdict is about a different
// state file than the launch will use.
type AvailabilityQuery struct {
	Model ModelID
	Home  string
}

// SpawnContext is everything a vendor gets for one launch: the resolved
// runtime, the model row that was selected from its own list, the effort value
// that survived vocabulary validation, and the caller's request.
//
// A vendor is handed the resolution rather than performing it, so a plugin
// cannot select a different model, a different harness or an unvalidated
// effort than the one the registry admitted.
type SpawnContext struct {
	Runtime Runtime
	Model   Model
	Effort  string
	Request SpawnRequest
}

// Vendor is the vendor plugin contract.
//
// Every method must be free of side effects other than reading what it was
// handed: BuildLaunch drives all of them to produce a plan, including in
// agentic.LaunchModeDryRun, and a dry run that authenticates, spends quota or
// starts a process is not a dry run.
type Vendor interface {
	// ID is the plugin's identity. It must be stable forever and must
	// normalize to itself. Registry.Register enforces the part of that a
	// registration-time check can reach — it reads ID() twice and refuses a
	// plugin that disagrees with itself, and refuses one whose id is not its
	// own normal form — exactly as agentic.Registry does for systems, and with
	// the same bound: stability past registration is contract, not enforcement.
	ID() VendorID

	// Models is the vendor's model list: capability rank with its evidence, a
	// usage description, the effort vocabulary and recommendation, and the
	// agentic systems that can drive each row. It must not vary between calls
	// — a registry that admitted a list and a launch that reads a different
	// one is the two-tables disease with the second table inside the plugin.
	Models() []Model

	// Availability is the structured verdict: healthy, limited until a stated
	// time, unreachable, or unknown — each with the evidence it rests on. It
	// is deliberately not a boolean; see availability.go.
	//
	// An error return means the verdict itself could not be produced, which is
	// a different fact from a verdict of unknown: unknown is an answer with
	// evidence about what was checked, an error is the absence of an answer.
	Availability(q AvailabilityQuery) (Availability, error)

	// Spawn is the vendor's half of one launch: it turns the resolved runtime,
	// model and effort into the agentic.LaunchRequest Layer 1 will plan. It
	// performs NO execution and no I/O.
	//
	// What it may add is vendor-owned: authentication environment, a
	// configuration home, whatever the vendor's API needs in the child. What
	// it may not do is redirect the launch — BuildLaunch refuses a request
	// that names a different system, model, effort, goal, budget, service tier
	// or composition than the one that was admitted, because a plugin that
	// could silently change any of those makes the admission meaningless.
	Spawn(sc SpawnContext) (agentic.LaunchRequest, error)
}
