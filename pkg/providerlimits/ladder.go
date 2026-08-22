package providerlimits

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// Backoff ladders, keyed by BROKER.
//
// The shipped ladder itself is DefaultLadder in state.go and is unchanged. What
// lives here is the KEYING: which row of an operator's per-broker ladder table
// applies to a given runtime, and which spellings of a row are legal.
//
// # Why broker and not runtime
//
// A rate limit is a property of the API behind the account, not of the harness
// that drives it (the extraction source's design D4, the same decision that
// moved HasClassifier onto the broker in groups.go). Two runtimes billed to one
// broker share one quota and must share one ladder; giving each its own would
// let an operator write two ladders for one pool and quietly get whichever the
// lookup happened to reach first.
//
// # The alias policy is the source's, and it is not extended here
//
// The source re-keyed this table onto brokers and kept the three runtime-ID
// spellings that were already legal working for ONE RELEASE as a migration aid
// — codex, claude and qwen, and nothing else. gemini, agy and muse are built-in
// runtimes too, and two of them have an established broker, but none of them was
// ever a legal ladder key, so admitting them now would silently start honouring
// a row this tool has always ignored. legacyLadderRuntimes carries exactly the
// source's three. It is a migration surface with an expiry, not a second
// permanent spelling, and nothing in this port widens it.
//
// # What this file does NOT do
//
// It parses no configuration. Steps arrive as already-parsed durations and row
// names arrive as bare broker or runtime identifiers; the dotted config path
// those rows are written under belongs to whoever owns the config schema, and
// this package's boundary test refuses to let it in here. That is the same
// split the source kept, with the resolution logic carried over and the JSON
// decoding left where it was.

// legacyLadderRuntimes is the closed set of runtime-ID spellings that remain
// legal ladder keys for one release. It is the source's
// spawnAgentHasAdapter set — codex, claude, qwen — carried verbatim.
//
// It is deliberately NOT derived from vendorplugin.FrozenRuntimes. Deriving it
// would make the alias set grow with the frozen table, which is precisely the
// widening the source's own comment refuses: widening the set to the built-in
// six would start silently accepting a "gemini" backoff row as a
// value-carrying key instead of reporting and dropping it.
var legacyLadderRuntimes = []string{ProviderClaude, ProviderCodex, "qwen"}

// LegacyLadderKeys returns the runtime-ID spellings still accepted as ladder
// keys, sorted. The slice is copied, so a caller cannot widen the policy by
// appending to the answer.
func LegacyLadderKeys() []string {
	out := append([]string(nil), legacyLadderRuntimes...)
	sort.Strings(out)
	return out
}

func isLegacyLadderKey(key string) bool {
	for _, runtime := range legacyLadderRuntimes {
		if key == runtime {
			return true
		}
	}
	return false
}

// LadderKeyResolution describes how one ladder-table key resolves onto a
// broker.
//
// Known is false for a key that is neither a legacy runtime spelling nor a
// recognised broker. Such a row is REPORTED AND DROPPED, never refused: broker
// and runtime identifiers are an open set, so a row an operator wrote for
// something this build has never heard of may carry any shape at all, and
// failing the whole table over it would drop every valid sibling row with it.
type LadderKeyResolution struct {
	// Broker is the broker the key resolves onto. Empty when Known is false.
	Broker string
	// Legacy marks a one-release-only runtime-ID spelling.
	Legacy bool
	Known  bool
}

// ResolveLadderKey classifies one ladder-table key.
//
// declaredBrokers is the analogue of the source's operator-declared runtimes
// block: brokers an operator declared for a runtime this build does not ship as
// a built-in. It may be empty, in which case only the frozen table's brokers
// are recognised.
//
// An arbitrary string is NOT admitted as "a broker" merely because brokers are
// an open set in general. If it were, "gemini" — a runtime spelling that was
// never a legal key — would silently start resolving as though it were one.
func ResolveLadderKey(key string, declaredBrokers ...string) LadderKeyResolution {
	key = strings.TrimSpace(key)
	if key == "" {
		return LadderKeyResolution{}
	}
	if isLegacyLadderKey(key) {
		if broker := brokerForRuntime(key); broker != "" {
			return LadderKeyResolution{Broker: broker, Legacy: true, Known: true}
		}
	}
	if isKnownLadderBroker(key, declaredBrokers) {
		return LadderKeyResolution{Broker: key, Known: true}
	}
	return LadderKeyResolution{}
}

// isKnownLadderBroker reports whether a string is the established broker of
// some runtime this build can name: one in the frozen table, or one an operator
// declared.
func isKnownLadderBroker(broker string, declared []string) bool {
	for _, declaration := range vendorplugin.FrozenRuntimes() {
		if declaration.VendorResolved() && declaration.Vendor.String() == broker {
			return true
		}
	}
	for _, candidate := range declared {
		if strings.TrimSpace(candidate) != "" && candidate == broker {
			return true
		}
	}
	return false
}

// ResolvedLadder is one runtime's effective ladder and where it came from.
type ResolvedLadder struct {
	// Runtime is the runtime the ladder was resolved for.
	Runtime string
	// Broker is the runtime's broker, or empty when it has none established.
	Broker string
	// Key is the table row that supplied the steps, spelled exactly as the
	// operator wrote it — so provenance names what they actually typed rather
	// than the broker it resolved to. Empty when nothing was configured.
	Key string
	// Legacy is true when Key was a one-release runtime-ID spelling.
	Legacy bool
	// Steps is the effective ladder. It is never empty: an unconfigured
	// runtime gets DefaultLadder.
	Steps []time.Duration
	// Configured distinguishes a table row from the shipped default. Without
	// it a ladder that happened to equal the default would be indistinguishable
	// from one nobody wrote.
	Configured bool
}

// ResolveLadderFor finds one runtime's ladder in an already-parsed table.
//
// The lookup order is the source's: the row spelled with the RUNTIME's own id
// first — a legacy built-in spelling, or exactly the row an operator wrote for
// a runtime they declared — and only then the row spelled with the runtime's
// BROKER. Runtime-first is what makes the alias a migration aid: an operator
// mid-migration who has both rows keeps getting the one they already had until
// they delete it, rather than silently switching over on upgrade.
//
// A runtime with no row anywhere gets DefaultLadder, which is what the source
// gives "every provider whose backoff row is absent".
func ResolveLadderFor(runtime string, configured map[string][]time.Duration, declaredBrokers ...string) ResolvedLadder {
	resolved := ResolvedLadder{
		Runtime: strings.TrimSpace(runtime),
		Broker:  brokerForRuntime(strings.TrimSpace(runtime)),
		Steps:   DefaultLadder(),
	}
	if len(configured) == 0 || resolved.Runtime == "" {
		return resolved
	}
	if steps, ok := configured[resolved.Runtime]; ok && len(steps) > 0 {
		resolved.Key = resolved.Runtime
		resolved.Legacy = isLegacyLadderKey(resolved.Runtime)
		resolved.Steps = append([]time.Duration(nil), steps...)
		resolved.Configured = true
		return resolved
	}
	if resolved.Broker == "" {
		return resolved
	}
	if steps, ok := configured[resolved.Broker]; ok && len(steps) > 0 {
		resolved.Key = resolved.Broker
		resolved.Steps = append([]time.Duration(nil), steps...)
		resolved.Configured = true
	}
	return resolved
}

// ValidateConfiguredLadders refuses a ladder table this package cannot act on.
//
// Two things are refused, and only two, because everything else about a row is
// somebody else's contract:
//
//   - a row whose steps break the ladder rules (ValidateLadder: non-empty,
//     every step positive, non-decreasing, none above MaxLadderStep);
//   - two rows that resolve to the SAME broker — the legacy runtime spelling
//     and the broker spelling of one pool written side by side. Silently
//     picking one would apply a ladder the operator can see they did not
//     choose, and the two rows are named so they do not have to work out the
//     collision themselves.
//
// A row this build cannot resolve at all is neither of those. It is returned in
// unknown, reported and dropped, exactly as the source does.
func ValidateConfiguredLadders(configured map[string][]time.Duration, declaredBrokers ...string) (unknown []string, err error) {
	origins := map[string][]string{}
	for _, key := range sortedLadderKeys(configured) {
		resolution := ResolveLadderKey(key, declaredBrokers...)
		if !resolution.Known {
			unknown = append(unknown, key)
			continue
		}
		if stepErr := ValidateLadder(configured[key]); stepErr != nil {
			return unknown, fmt.Errorf("providerlimits: backoff row %q: %w", key, stepErr)
		}
		origins[resolution.Broker] = append(origins[resolution.Broker], key)
	}
	for _, broker := range sortedLadderOrigins(origins) {
		rows := origins[broker]
		if len(rows) < 2 {
			continue
		}
		sort.Strings(rows)
		return unknown, fmt.Errorf(
			"providerlimits: backoff rows %s all resolve to broker %q; keep only one row per broker. Prefer the broker-keyed spelling (%s) — the runtime-ID spelling is supported for one release as a migration aid, not as a second permanent spelling",
			strings.Join(quoteAll(rows), " and "), broker, broker)
	}
	return unknown, nil
}

func sortedLadderKeys(values map[string][]time.Duration) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedLadderOrigins(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func quoteAll(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = fmt.Sprintf("%q", value)
	}
	return out
}
