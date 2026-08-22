package providerlimits

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// The seam between this plane and the vendor contract.
//
// Everything above this line is the extraction source's limit plane, ported
// with its on-disk identity, its state machine and its fail-open decisions
// intact. This file is the only new behaviour: it turns what a state read
// established into a vendorplugin.Availability, so a vendor plugin answers
// Availability(q) by asking the plane rather than by re-deriving one.
//
// # Why the mapping is not mechanical
//
// The plane has four group states and six read outcomes; the verdict has four.
// The interesting part is which pairs collapse and which must not:
//
//   - available, and no record at all, both mean nothing is subtracted. They
//     collapse onto Healthy — but only under a DETERMINATE read.
//   - suppressed with a future next_probe_at is Limited, and it is the only
//     verdict carrying a clear time. The evidence the source wrote into the
//     record is what makes it a Limited rather than an Unknown: the contract
//     refuses a limit with no observation behind it.
//   - probe_eligible and probing are NOT available and NOT limited-until.
//     See probeGatedVerdict.
//   - every indeterminate read is Unknown with the read failure attached, and
//     under it a MISSING record is Unknown too, because the missing record is
//     the shape of the hole the failed read left rather than a proven absence.
//
// # What is preserved rather than re-litigated
//
// An ABSENT state file reads as Healthy. That is the extraction source's
// decision — docs/architecture.md invariant 2, "limit state fails open" — and
// this file carries it rather than re-deciding it. absentIsHealthy states the
// source's own reasoning at the one place it takes effect.

// Source labels for the verdict's Checked/Observed/Failures lists.
//
// They are stable strings rather than absolute paths for two reasons. A verdict
// is a comparable value that gets logged and snapshotted, and an absolute path
// under $HOME makes every such snapshot machine-specific; and the state file's
// location is already reported by Store.Layout to anyone who needs it. The
// identity KEY is included because it is the thing that must not move, so a
// verdict that ended up reading the wrong file says so in its own evidence.
const (
	// SourceLimitState names the per-identity state file a verdict was read
	// from. It is suffixed with the identity key by limitStateSource.
	SourceLimitState = "provider-limits state"
	// SourceFrozenTable names the (runtime, vendor) lookup that decides
	// whether this plane could ever suppress a runtime at all.
	SourceFrozenTable = "vendorplugin frozen runtime table"
)

func limitStateSource(identity string) string {
	return SourceLimitState + " " + identity
}

// VerdictQuery is what the seam needs that vendorplugin.AvailabilityQuery does
// not carry: the runtime id, and the group table the model resolves through.
//
// vendorplugin.AvailabilityQuery is per (model, home) because a vendor plugin
// knows its own id. The plane is keyed by RUNTIME — that is what IdentityKey
// hashes and what every persisted group key is spelled with — so a vendor
// plugin serving two runtimes (google serves gemini and agy) has to say which
// one it is asking about. AvailabilityFor takes the runtime explicitly rather
// than deriving it from the vendor, because the vendor→runtime direction is
// one-to-many and a derived answer would be a guess.
type VerdictQuery struct {
	// Runtime is the runtime id: "claude", "codex". Required.
	Runtime string
	// Model narrows the question to one group. Empty asks about the runtime as
	// a whole; see identityVerdict for what that means.
	Model string
	// Home is the provider home. Empty resolves DefaultProviderHome, which is
	// what a launch with no CODEX_HOME/CLAUDE_CONFIG_DIR override would use.
	Home string
	// Table resolves Model to its group. Nil means MustGroupTable — the
	// shipped table with no operator overrides.
	Table *GroupTable
}

func (q VerdictQuery) table() *GroupTable {
	if q.Table != nil {
		return q.Table
	}
	return MustGroupTable()
}

// AvailabilityFor answers one availability question from limit state alone.
//
// It NEVER writes. Not the state file, not the index, not a probe claim: it is
// built on LoadIdentityState for the reason Report is, and a read surface that
// took a claim would hand out or withdraw a probe as a side effect of being
// looked at. That is also why a probe-eligible group cannot come back Healthy
// here — see probeGatedVerdict.
//
// The error return is the contract's "no verdict could be produced", which is a
// different fact from a verdict of unknown: it fires only when the query itself
// cannot be resolved into an identity — a blank runtime, a runtime with no
// home rule, a home that cannot be normalised. Every reachable state of the
// state file produces a verdict.
//
// The returned verdict always satisfies vendorplugin.Availability.Validate;
// TestEveryVerdictThisSeamCanProduceValidates drives every arm and checks it,
// because a verdict that fails validation is refused by CheckAvailability and
// the plane would then have no way to report anything at all.
func (s *Store) AvailabilityFor(q VerdictQuery) (vendorplugin.Availability, error) {
	runtime := strings.TrimSpace(q.Runtime)
	if runtime == "" {
		return vendorplugin.Availability{}, fmt.Errorf("providerlimits: a runtime is required to resolve an availability verdict")
	}

	// The unclassifiable carve-out, first, because it is answerable without a
	// home and some unclassifiable runtimes have no home rule at all.
	if verdict, ok := unclassifiableVerdict(runtime); ok {
		return verdict, nil
	}

	home := q.Home
	if strings.TrimSpace(home) == "" {
		resolved, err := DefaultProviderHome(runtime)
		if err != nil {
			return vendorplugin.Availability{}, err
		}
		home = resolved
	}
	identity, err := IdentityFor(runtime, home)
	if err != nil {
		return vendorplugin.Availability{}, err
	}

	state := s.LoadIdentityState(identity)
	now := s.now()
	source := limitStateSource(identity.Key)

	if failure, ok := readFailure(source, state.Read); ok {
		return unknownAfterFailedRead(source, failure), nil
	}

	table := q.table()
	if strings.TrimSpace(q.Model) == "" {
		return identityVerdict(source, table, state, now), nil
	}
	group := table.Group(runtime, q.Model)
	record, ok := state.Groups[group]
	if !ok {
		return absentIsHealthy(source), nil
	}
	return s.groupVerdict(source, group, record, now), nil
}

// unclassifiableVerdict is the source's own carve-out, carried verbatim: a
// runtime whose broker has no classifier can never be suppressed, so no state
// file ever gated it.
//
// state.go's "what an indeterminate read authorizes" block lists it as one of
// the three places fail-open is DELIBERATELY retained — "they can never be
// suppressed, so no state file ever gated them, so a lost file lost nothing
// about them" — and that is why this runs BEFORE the state read rather than
// after it. Answering it from the state file would make an unreadable file
// downgrade a runtime that the file could never have said anything about.
//
// The verdict is Healthy rather than Unknown because a real source was read:
// the frozen (runtime, vendor) table, which is where the fact lives. A verdict
// of Healthy with nothing checked is what the contract refuses, and this is not
// that.
func unclassifiableVerdict(runtime string) (vendorplugin.Availability, bool) {
	broker := brokerForRuntime(runtime)
	if HasClassifier(broker) {
		return vendorplugin.Availability{}, false
	}
	detail := fmt.Sprintf("runtime %q resolves to no broker this plane classifies, so nothing can ever suppress it", runtime)
	if broker != "" {
		detail = fmt.Sprintf("runtime %q resolves to broker %q, for which this plane ships no classifier, so nothing can ever suppress it", runtime, broker)
	}
	return vendorplugin.Availability{
		State:    vendorplugin.AvailabilityHealthy,
		Checked:  []string{SourceFrozenTable},
		Observed: []vendorplugin.Observation{{Source: SourceFrozenTable, Detail: detail}},
	}, true
}

// readFailure turns an indeterminate StateRead into the ReadFailure a verdict
// carries. The second result is false for the two DETERMINATE reads.
//
// The reasons are the operator-facing half of the source's corrupt-state fix
// (its commit "Stop reading an unreadable limit state as an empty one"): they
// say per arm whether waiting helps, because for a degraded read it does and
// for an unreadable or corrupt one it may not.
func readFailure(source string, read StateRead) (vendorplugin.ReadFailure, bool) {
	if !read.Indeterminate() {
		return vendorplugin.ReadFailure{}, false
	}
	var reason string
	switch read {
	case StateReadUnreadable:
		reason = "the state file exists and could not be read; its records are LOST, not absent, so nothing here may be reported as availability"
	case StateReadCorrupt:
		reason = "the state file was read and did not parse; its records are LOST, not absent, so nothing here may be reported as availability"
	case StateReadSchemaAhead:
		reason = "the state file carries a schema version newer than this binary understands; the file is intact and a newer build reads it, but this one established nothing from it"
	case StateReadDegraded:
		reason = "the state file carries a degraded_since tombstone inside the lost-state horizon; whatever it still holds is proven, but the ABSENCE of a group is not"
	default:
		reason = fmt.Sprintf("the state read reported %q, which established nothing", read)
	}
	return vendorplugin.ReadFailure{Source: source, Reason: reason}, true
}

// unknownAfterFailedRead is the verdict for every indeterminate read.
//
// It is Unknown and never Healthy, and that is the whole point of the source's
// corrupt-state fix carried onto this surface: the empty group map an
// unreadable file hands back renders as "nothing is suppressed", which is a
// claim about the provider that a failed read never established. The contract
// agrees independently — Availability.Validate refuses a Healthy verdict
// carrying any read failure — so the rule is enforced twice, once by the
// mapping and once by the type.
//
// The source is in Checked as well as in Failures: it WAS read, or the attempt
// was made against it, and a caller distinguishing "we looked and could not
// tell" from "nobody looked" needs the non-empty Checked to do it.
func unknownAfterFailedRead(source string, failure vendorplugin.ReadFailure) vendorplugin.Availability {
	return vendorplugin.Availability{
		State:    vendorplugin.AvailabilityUnknown,
		Checked:  []string{source},
		Failures: []vendorplugin.ReadFailure{failure},
	}
}

// absentIsHealthy is the FAIL-OPEN decision, at the one place it takes effect.
//
// It is the extraction source's, PRESERVED and not re-litigated here. The
// source's reasoning, in its own terms:
//
//   - docs/architecture.md invariant 2: "Limit state fails open. An absent
//     state file reads as 'provider healthy' with no error anywhere."
//   - state.go's StateReadAbsent: a machine that has never recorded state for
//     this identity is a PROVEN absence — nothing was ever suppressed.
//   - the plane exists to avoid relaunching blindly into an exhausted quota.
//     It is an optimisation over doing nothing, and an optimisation that
//     refuses launches when its own bookkeeping is missing is worse than not
//     having it.
//
// What makes this a legitimate Healthy rather than a guess is that a proven
// absence IS a read. The state file was looked for at a known path and found
// not to exist; that is an observation, and it is what goes in Checked. The
// contract's evidence discipline — Healthy requires at least one source read
// and no read failures — is therefore satisfied by the state read itself, not
// waived for this arm.
//
// The same function answers "the file parsed and holds no record for this
// group", because under a determinate read those two are the same fact: no
// record means nothing was ever suppressed for that group.
func absentIsHealthy(source string) vendorplugin.Availability {
	return vendorplugin.Availability{
		State:   vendorplugin.AvailabilityHealthy,
		Checked: []string{source},
		Observed: []vendorplugin.Observation{{
			Source: source,
			Detail: "no suppression on record for this group",
		}},
	}
}

// groupVerdict projects one group record read under a DETERMINATE read.
func (s *Store) groupVerdict(source, group string, record GroupRecord, now time.Time) vendorplugin.Availability {
	switch record.EffectiveState(now) {
	case StateAvailable:
		return availableIsHealthy(source, group, record)
	case StateSuppressed:
		return suppressedIsLimited(source, group, record, s.ladderStep(record.BackoffStep))
	default:
		return probeGatedVerdict(source, group, record, now)
	}
}

// availableIsHealthy is a record that exists and subtracts nothing.
func availableIsHealthy(source, group string, record GroupRecord) vendorplugin.Availability {
	detail := fmt.Sprintf("group %q is available", group)
	if record.LastSuccessAt != nil {
		detail += fmt.Sprintf("; last success %s", record.LastSuccessAt.UTC().Format(time.RFC3339))
	}
	if record.AgedOutAt != nil {
		detail += fmt.Sprintf("; escalation aged out at %s", record.AgedOutAt.UTC().Format(time.RFC3339))
	}
	return vendorplugin.Availability{
		State:    vendorplugin.AvailabilityHealthy,
		Checked:  []string{source},
		Observed: []vendorplugin.Observation{{Source: source, Detail: detail}},
	}
}

// suppressedIsLimited is the limited-until verdict: an active suppression, its
// window, and the evidence that established it.
//
// Until is next_probe_at and nothing else. It is not the reset hint: a
// provider's own prose is diagnostic and "may shorten a backoff step, never
// extend or suppress" (ResetHintNote), so it reaches the verdict as an
// OBSERVATION and never as the clear time. Reporting a parsed hint as Until
// would let vendor prose set a retry schedule the plane never armed.
//
// A suppressed record with no next_probe_at cannot answer "until when", so it
// is reported Unknown rather than as a Limited verdict with a fabricated
// window — the same rule Availability.Validate enforces from the other side.
func suppressedIsLimited(source, group string, record GroupRecord, step time.Duration) vendorplugin.Availability {
	observed := suppressionObservations(source, group, record, step)
	if record.NextProbeAt == nil {
		return vendorplugin.Availability{
			State:   vendorplugin.AvailabilityUnknown,
			Checked: []string{source},
			Observed: append(observed, vendorplugin.Observation{
				Source: source,
				Detail: fmt.Sprintf("group %q is suppressed with no next_probe_at, so this read established no clear time", group),
			}),
		}
	}
	return vendorplugin.Availability{
		State:    vendorplugin.AvailabilityLimited,
		Until:    *record.NextProbeAt,
		Checked:  []string{source},
		Observed: observed,
	}
}

// probeGatedVerdict is probe_eligible and probing, and it is the one mapping
// worth arguing about.
//
// Neither is Healthy. probe_eligible is emphatically NOT available — the
// source's own words: "it is admitted to a claim holder only, and stays
// subtracted for every other caller", and collapsing the two "is what lets
// every concurrent preflight admit an exhausted group at once". probing is a
// group somebody else currently holds the claim on.
//
// Neither is Limited either, and this is why the verdict is Unknown rather
// than a Limited with a stale window. Limited means "refused until a stated
// time", and a caller is entitled to schedule a retry off Until. A
// probe-eligible group's next_probe_at is in the PAST, so reporting it would
// tell the caller to retry immediately when in fact exactly one caller may,
// and only by winning an atomic claim. A probing group's lease expiry is not a
// clear time at all — it is when somebody else's turn ends.
//
// So the honest answer is a CHECKED Unknown: the source was read, it
// established that this group is not available to this caller, and it
// established no time at which that changes without a claim. Serviceable() is
// false, which is the safe direction, and the observation names the claim as
// the thing that resolves it — which the verdict path may not take, because it
// is a write.
func probeGatedVerdict(source, group string, record GroupRecord, now time.Time) vendorplugin.Availability {
	effective := record.EffectiveState(now)
	detail := fmt.Sprintf("group %q reads %s: it is subtracted for every caller except the one that wins the atomic probe claim, and a claim is a write this read may not take", group, effective)
	if effective == StateProbing && record.ProbeLease != nil {
		detail = fmt.Sprintf("group %q is being probed under a lease held by run %q until %s; it is subtracted for every other caller",
			group, record.ProbeLease.RunID, record.ProbeLease.ResolveAt().UTC().Format(time.RFC3339))
	}
	observed := []vendorplugin.Observation{{Source: source, Detail: detail}}
	observed = append(observed, evidenceObservations(source, group, record)...)
	return vendorplugin.Availability{
		State:    vendorplugin.AvailabilityUnknown,
		Checked:  []string{source},
		Observed: observed,
	}
}

// suppressionObservations is the recorded evidence behind a suppression, in a
// fixed order so two verdicts over one record compare equal.
func suppressionObservations(source, group string, record GroupRecord, step time.Duration) []vendorplugin.Observation {
	detail := fmt.Sprintf("group %q is suppressed at backoff step %d (%s) after %d consecutive limit observations",
		group, record.BackoffStep, step, record.ConsecutiveLimitObservations)
	if record.SuppressedSince != nil {
		detail += fmt.Sprintf("; suppressed since %s", record.SuppressedSince.UTC().Format(time.RFC3339))
	}
	at := time.Time{}
	if record.LastObservationAt != nil {
		at = *record.LastObservationAt
	}
	observed := []vendorplugin.Observation{{Source: source, Detail: detail, At: at}}
	return append(observed, evidenceObservations(source, group, record)...)
}

// evidenceObservations renders the provenance the source wrote into the record:
// which run and model hit the limit, the marker that matched, and the
// provider's own reset prose.
//
// The reset hint is carried with ResetHintNote attached verbatim, so a reader
// of the verdict gets the same warning a reader of the state file does: it is
// diagnostic, and it may shorten a backoff step but never extend or suppress.
func evidenceObservations(source, group string, record GroupRecord) []vendorplugin.Observation {
	var observed []vendorplugin.Observation
	if ev := record.Evidence; ev != nil {
		if detail := renderEvidence(*ev); detail != "" {
			observed = append(observed, vendorplugin.Observation{
				Source: source,
				Detail: fmt.Sprintf("evidence for %q: %s", group, detail),
			})
		}
	}
	if hint := record.ProviderResetHint; hint != nil && strings.TrimSpace(hint.Raw) != "" {
		detail := fmt.Sprintf("provider reset prose %q (%s)", hint.Raw, ResetHintNote)
		at := time.Time{}
		if hint.Parsed != nil {
			at = *hint.Parsed
		}
		observed = append(observed, vendorplugin.Observation{Source: source, Detail: detail, At: at})
	}
	return observed
}

// renderEvidence is a stable one-line rendering of the recorded provenance. It
// returns "" for an Evidence with nothing in it, so an empty struct does not
// produce an observation whose Detail is blank — which Validate refuses.
func renderEvidence(ev Evidence) string {
	var parts []string
	for _, field := range []struct{ label, value string }{
		{"run", ev.RunID},
		{"task", ev.TaskID},
		{"model", ev.Model},
		{"marker", ev.Marker},
		{"log", ev.Log},
		{"excerpt", ev.Excerpt},
	} {
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		parts = append(parts, field.label+"="+field.value)
	}
	return strings.Join(parts, " ")
}

// identityVerdict answers the runtime-wide question: Model was empty.
//
// "Can requests be made right now" is true for a runtime as long as ONE of its
// groups can serve, so a runtime with one exhausted group and three healthy
// ones is Healthy — with the exhausted group named in Observed, so a caller
// that needs precision can see what is subtracted and ask again per model.
// Reporting Limited there would refuse three groups over one, which is not
// what the plane does and not what the word means.
//
// It is Limited only when NO group the table knows for this runtime can serve,
// and then Until is the EARLIEST clear time among them: the first instant at
// which the runtime's situation could improve. A caller scheduling a retry off
// a later one would sleep through the group that recovered first.
//
// A group that is probe-gated rather than suppressed has no clear time to
// contribute. If every group is probe-gated the verdict is Unknown, for
// probeGatedVerdict's reason: not available, and no time at which that changes
// without a claim.
func identityVerdict(source string, table *GroupTable, state IdentityState, now time.Time) vendorplugin.Availability {
	groups := tableGroupsFor(table, state.Identity.Provider)

	var (
		servable  []string
		limited   []vendorplugin.Observation
		earliest  time.Time
		anyWindow bool
	)
	for _, group := range groups {
		record, ok := state.Groups[group]
		if !ok || record.EffectiveState(now) == StateAvailable {
			servable = append(servable, group)
			continue
		}
		limited = append(limited, vendorplugin.Observation{
			Source: source,
			Detail: fmt.Sprintf("group %q reads %s", group, record.EffectiveState(now)),
		})
		if record.EffectiveState(now) == StateSuppressed && record.NextProbeAt != nil {
			if !anyWindow || record.NextProbeAt.Before(earliest) {
				earliest = *record.NextProbeAt
				anyWindow = true
			}
		}
	}

	if len(servable) > 0 {
		detail := fmt.Sprintf("%d of %d known groups can serve: %s", len(servable), len(groups), strings.Join(servable, ", "))
		if len(groups) == 0 {
			detail = fmt.Sprintf("the group table knows no group for runtime %q, so nothing of it is subtracted", state.Identity.Provider)
		}
		return vendorplugin.Availability{
			State:    vendorplugin.AvailabilityHealthy,
			Checked:  []string{source},
			Observed: append([]vendorplugin.Observation{{Source: source, Detail: detail}}, limited...),
		}
	}
	if !anyWindow {
		return vendorplugin.Availability{
			State:   vendorplugin.AvailabilityUnknown,
			Checked: []string{source},
			Observed: append([]vendorplugin.Observation{{
				Source: source,
				Detail: fmt.Sprintf("no group of runtime %q can serve and none of them carries a clear time, so this read established no instant at which that changes", state.Identity.Provider),
			}}, limited...),
		}
	}
	return vendorplugin.Availability{
		State:   vendorplugin.AvailabilityLimited,
		Until:   earliest,
		Checked: []string{source},
		Observed: append([]vendorplugin.Observation{{
			Source: source,
			Detail: fmt.Sprintf("every one of the %d known groups of runtime %q is subtracted; the earliest clear time is %s", len(groups), state.Identity.Provider, earliest.UTC().Format(time.RFC3339)),
		}}, limited...),
	}
}

// tableGroupsFor is the runtime's known groups, sorted.
//
// It reads the EFFECTIVE table — the shipped rows composed with whatever
// overrides the caller resolved — rather than the union of that table and
// whatever the state file happens to carry. That is on purpose: a state file
// can hold a singleton unmapped group for a model that no longer exists, and
// letting such a row decide the runtime-wide verdict would let a dead model's
// suppression outvote every real group.
func tableGroupsFor(table *GroupTable, runtime string) []string {
	var groups []string
	for _, row := range table.Rows() {
		if row.Provider == runtime {
			groups = append(groups, row.Group)
		}
	}
	sort.Strings(groups)
	return groups
}
