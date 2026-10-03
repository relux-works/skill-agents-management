package agentic

import (
	"fmt"
	"strings"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
)

// Network is the typed carrier for a resolved network binding, D4 per tb-R148
// (decided: no callbacks).
//
// It carries the environment patch the process owner resolved on the
// destination host plus the manifest-safe Record that proves which binding was
// applied. Both halves travel together: the patch changes the child
// environment, the Record travels as plan provenance and never enters env.
//
// The zero value means unmanaged — no network scope was selected — and leaves
// every existing plan byte-identical. BuildPlan skips all network handling
// when IsZero reports true, so every golden captured before this carrier
// existed still matches.
//
// Types come from github.com/relux-works/curator-network-profiles at tag
// v0.2.0 (docs/integration-contract.md rev 2, spec/contract-appendix.md). The
// public module is consumed by tag; there is no replace and no go.work entry.
type Network struct {
	Patch  envpatch.Patch
	Record binding.Record
}

// IsZero reports whether no network scope was selected. Both halves empty is
// unmanaged; either half set is a scope that must be validated and applied or
// refused — never silently dropped.
func (n Network) IsZero() bool {
	return n.Patch.Empty() && n.Record == (binding.Record{})
}

// Clone returns a deep copy. Patch slices are copied so a vendor or caller
// mutating its own value cannot reach the plan's; Record holds only strings
// and a timestamp and copies by value.
func (n Network) Clone() Network {
	return Network{
		Patch: envpatch.Patch{
			Unset: append([]string(nil), n.Patch.Unset...),
			Set:   append([]envpatch.Pair(nil), n.Patch.Set...),
		},
		Record: n.Record,
	}
}

// AdmitsNetwork reports whether a managed Record's adapter identity names a
// network path this system verified. Admission is an explicit allowlist,
// fail-closed: an empty NetworkAdapters declaration admits nothing, and a
// non-empty one admits only the exact tuples it names, compared member for
// member with binding.AdapterIdentity.Equal. A Record bound to another
// adapter, harness, build or entrypoint is not "close enough" — the contract
// verifies exact tuples (integration-contract.md rev 2 Q7), and admitting a
// neighbouring tuple would launch a scope no verification covers.
//
// GateNetwork is the production caller; it refuses right after the Capabilities
// declaration read — the only plugin invocation that precedes admission —
// and before every dispatch and optional surface, so an undeclared plugin
// never reaches a validator, a preparer, or ChildEnv with a scope.
func (c Capabilities) AdmitsNetwork(identity binding.AdapterIdentity) bool {
	for _, declared := range c.NetworkAdapters {
		if declared.Equal(identity) {
			return true
		}
	}
	return false
}

// GateNetwork runs the single shared network shape and admission gate every
// production entry point applies first (round-3 merged finding W1: the
// vendorplugin wrapper used to run preparation and preflight before BuildPlan's
// gate, so a refused carrier reached a preparer and a preparer failure masked
// the typed refusal).
//
// Shape validation runs before readCaps is invoked: a malformed carrier is
// refused while the plugin has been touched zero times. readCaps then supplies
// the system's Capabilities declaration — the single permitted plugin call
// before the gate, and the read that states the admission — and a well-formed
// scope the declaration does not name is refused with
// ErrNetworkScopeUnsupported. readCaps runs at most once, and only for a
// well-formed non-zero Network; zero Network returns nil without invoking it,
// so every existing plan stays byte-identical.
//
// A readCaps failure propagates unchanged for the caller to interpret:
// BuildPlan's read cannot fail, while vendorplugin's falls through to its
// normal resolution path so an unresolvable runtime reports exactly what it
// always reported. Either way no plugin surface beyond the declaration read
// has run when this returns non-nil.
func GateNetwork(network Network, readCaps func() (Capabilities, error)) error {
	if err := validateNetworkRequest(network); err != nil {
		return err
	}
	if network.IsZero() {
		return nil
	}
	caps, err := readCaps()
	if err != nil {
		return err
	}
	if !caps.AdmitsNetwork(network.Record.AdapterIdentity) {
		return ErrNetworkScopeUnsupported
	}
	return nil
}

var (
	// ErrNetworkScopeUnsupported refuses a non-zero Network on a harness with
	// no verified network path. GateNetwork returns it for every system whose
	// NetworkAdapters declaration admits no tuple matching the Record — this
	// revision claude-code and codex declare exactly their verified tuples
	// (generic-env-v1 and codex-env-v1) and every other plugin refuses, and
	// muse carries it until D8 verifies its harness/build/entrypoint/adapter
	// tuple. It is a *refusal.Refusal carrying code network_scope_unsupported,
	// so errors.Is matches on the code and refusal.CodeOf reports it.
	ErrNetworkScopeUnsupported = &refusal.Refusal{Code: refusal.CodeScopeUnsupported}
	// ErrNetworkProfileInvalid refuses a malformed network patch or Record:
	// bad names, duplicate set entries, a set-only patch, or halves that do
	// not travel together. Code network_profile_invalid.
	ErrNetworkProfileInvalid = &refusal.Refusal{Code: refusal.CodeProfileInvalid}
	// ErrNetworkConfigurationConflict refuses a well-formed patch that touches
	// a reserved run-context key or an owned key from ChildEnv(nil). Code
	// network_configuration_conflict.
	ErrNetworkConfigurationConflict = &refusal.Refusal{Code: refusal.CodeConfigurationConflict}
)

// validateNetworkRequest checks the request-side shape that needs no plugin
// surface: halves travel together, the Record carries its schema and identity,
// and the patch is syntactically well-formed. It runs before the first plugin
// invocation of any kind — the Capabilities read included — so a malformed
// carrier is refused while the plugin has been touched zero times, and so the
// refusal precedes the plugin-side unsupported refusal. Reserved/owned checks
// need ChildEnv(nil) and run at the application point instead.
func validateNetworkRequest(network Network) error {
	if network.IsZero() {
		return nil
	}
	patchEmpty := network.Patch.Empty()
	recordZero := network.Record == (binding.Record{})
	if patchEmpty != recordZero {
		return refusal.New(refusal.CodeProfileInvalid, "network",
			"patch and Record travel together; one half without the other is malformed")
	}
	if recordZero {
		return nil
	}
	if network.Record.Schema != binding.SchemaRecord {
		return refusal.New(refusal.CodeProfileInvalid, "network",
			"Record schema is not the versioned binding record")
	}
	if strings.TrimSpace(network.Record.ProfileRef) == "" || strings.TrimSpace(network.Record.ProfileDigest) == "" {
		return refusal.New(refusal.CodeProfileInvalid, "network",
			"Record carries no profile identity")
	}
	return validateNetworkPatch(network.Patch)
}

// validateNetworkPatch refuses a patch no verified adapter could have
// produced. Names must be environment names, set names must be unique (a
// duplicate would emit the same key twice), values must not carry NUL, and a
// patch is never only additions: a non-empty set requires a non-empty unset
// (spec N3, appendix §2).
func validateNetworkPatch(patch envpatch.Patch) error {
	if patch.Empty() {
		return nil
	}
	for _, name := range patch.Unset {
		if !validEnvironmentName(name) {
			return refusal.New(refusal.CodeProfileInvalid, "network",
				"patch unset carries a name that is not an environment name")
		}
	}
	seen := make(map[string]struct{}, len(patch.Set))
	for _, pair := range patch.Set {
		if !validEnvironmentName(pair.Name) {
			return refusal.New(refusal.CodeProfileInvalid, "network",
				"patch set carries a name that is not an environment name")
		}
		if strings.IndexByte(pair.Value, 0) >= 0 {
			return refusal.New(refusal.CodeProfileInvalid, "network",
				"patch set carries a value that cannot live in an environment")
		}
		if _, duplicate := seen[pair.Name]; duplicate {
			return refusal.New(refusal.CodeProfileInvalid, "network",
				"patch set names the same variable twice")
		}
		seen[pair.Name] = struct{}{}
	}
	if len(patch.Set) > 0 && len(patch.Unset) == 0 {
		return refusal.New(refusal.CodeProfileInvalid, "network",
			"patch sets variables without unsetting the family first")
	}
	return nil
}

// reservedNetworkKeys are the caller-owned run-context names a network patch
// must never touch. They are reserved even when the current Run leaves them
// empty and therefore absent from ChildEnv(nil): an empty Run removes its keys
// rather than exporting blanks, so absence from the owned snapshot does not
// make the name available.
var reservedNetworkKeys = map[string]struct{}{
	EnvRunID: {}, EnvTaskID: {}, EnvBoardDir: {}, EnvLegacyBoardDir: {},
	EnvDeliveryGoalID: {}, EnvContextID: {},
}

// checkNetworkPatchAgainstReservedAndOwned refuses a well-formed patch that
// would rewrite caller or system state. Unset names match case-insensitively
// because Patch.Apply removes them that way (Http_Proxy is removed by an
// HTTP_PROXY unset); set names match exactly because Apply removes exact set
// names only. Owned keys come from ChildEnv(nil) before the patch runs.
func checkNetworkPatchAgainstReservedAndOwned(patch envpatch.Patch, owned []string) error {
	if patch.Empty() {
		return nil
	}
	ownedExact := make(map[string]struct{}, len(owned))
	ownedFolded := make(map[string]struct{}, len(owned))
	for _, entry := range owned {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		ownedExact[name] = struct{}{}
		ownedFolded[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range patch.Unset {
		if _, reserved := reservedNetworkKeys[name]; reserved {
			return refusal.New(refusal.CodeConfigurationConflict, "network",
				fmt.Sprintf("patch unsets reserved %q", name))
		}
		if _, reserved := reservedNetworkKeysFolded()[strings.ToLower(name)]; reserved {
			return refusal.New(refusal.CodeConfigurationConflict, "network",
				fmt.Sprintf("patch unsets reserved %q", name))
		}
		if _, isOwned := ownedFolded[strings.ToLower(name)]; isOwned {
			return refusal.New(refusal.CodeConfigurationConflict, "network",
				fmt.Sprintf("patch unsets owned %q", name))
		}
	}
	for _, pair := range patch.Set {
		if _, reserved := reservedNetworkKeys[pair.Name]; reserved {
			return refusal.New(refusal.CodeConfigurationConflict, "network",
				fmt.Sprintf("patch sets reserved %q", pair.Name))
		}
		if _, isOwned := ownedExact[pair.Name]; isOwned {
			return refusal.New(refusal.CodeConfigurationConflict, "network",
				fmt.Sprintf("patch sets owned %q", pair.Name))
		}
	}
	return nil
}

func reservedNetworkKeysFolded() map[string]struct{} {
	return map[string]struct{}{
		strings.ToLower(EnvRunID): {}, strings.ToLower(EnvTaskID): {},
		strings.ToLower(EnvBoardDir): {}, strings.ToLower(EnvLegacyBoardDir): {},
		strings.ToLower(EnvDeliveryGoalID): {}, strings.ToLower(EnvContextID): {},
	}
}
