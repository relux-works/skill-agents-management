package providerlimits

import (
	"sort"
	"time"
)

// Report is the single projection of limit state.
//
// It carries no armed_token field. The extraction source's report has one,
// naming a dev-only fault-injection token; that injector is not ported here —
// see this package's doc for why it is launch-plane behaviour rather than
// limit-plane behaviour — so a field that could only ever be null would be a
// surface promising something nothing behind it can produce. Layout keeps
// SimulateFile and SimulateLockFile, because the on-disk layout is a
// compatibility surface and a later port of the injector must land on the same
// paths rather than choosing new ones.
//
// Both the agent-facing query operation and the operator CLI command read this
// one value, so the two surfaces cannot disagree.
//
// Building a report never takes a probe claim and never mutates state. That is
// deliberate and slightly costly: a stale lease is shown as it is stored rather
// than resolved, because resolving one is a write, and a read surface that writes
// could hand out or withdraw a probe as a side effect of being looked at.
type Report struct {
	Version     int               `json:"version"`
	GeneratedAt time.Time         `json:"generated_at"`
	Ladder      []string          `json:"ladder"`
	Identities  []IdentityReport  `json:"identities"`
	Groups      []GroupRow        `json:"group_table,omitempty"`
	Bounds      ReportBoundsBlock `json:"bounds"`
}

// ReportBoundsBlock carries the effective lease bounds so a reader can tell why a
// lease expires when it does.
type ReportBoundsBlock struct {
	ProbeLeaseMinutes    int `json:"probe_lease_minutes"`
	ProbeClaimMaxMinutes int `json:"probe_claim_max_minutes"`
}

// IdentityReport is one provider identity's projection. home_display is a path
// with $HOME collapsed to ~; nothing read from inside a provider home appears
// here, because nothing inside one is ever read.
type IdentityReport struct {
	Identity    string `json:"identity"`
	Provider    string `json:"provider"`
	HomeDisplay string `json:"home_display"`
	SchemaAhead bool   `json:"schema_ahead,omitempty"`
	// StateRead is why Groups is what it is. It is always populated, because a
	// reader has to be able to tell a proven absence of suppression from a read
	// that never happened — the two produce the same empty Groups slice.
	StateRead StateRead `json:"state_read"`
	// Indeterminate is StateRead.Indeterminate() rendered for consumers that do
	// not want to know the enum. When it is true, NOTHING in Groups may be read
	// as a statement about provider availability.
	Indeterminate bool          `json:"indeterminate,omitempty"`
	Groups        []GroupReport `json:"groups"`
}

// GroupReport is one group's projection.
type GroupReport struct {
	Group    string     `json:"group"`
	Provider string     `json:"provider"`
	State    GroupState `json:"state"`
	// EffectiveState is what a reader should act on at GeneratedAt: a suppressed
	// record whose step has elapsed reads probe_eligible.
	EffectiveState GroupState `json:"effective_state"`
	// Claimable is true when exactly one caller could take a probe claim now.
	// It is never true for an available group, which needs no claim.
	Claimable                    bool       `json:"claimable"`
	BackoffStep                  int        `json:"backoff_step"`
	BackoffStepDuration          string     `json:"backoff_step_duration"`
	SuppressedSince              *time.Time `json:"suppressed_since,omitempty"`
	NextProbeAt                  *time.Time `json:"next_probe_at,omitempty"`
	ConsecutiveLimitObservations int        `json:"consecutive_limit_observations"`
	LastSuccessAt                *time.Time `json:"last_success_at,omitempty"`
	ProbeProvisionalSince        *time.Time `json:"probe_provisional_since,omitempty"`
	ProbeAbandoned               int        `json:"probe_abandoned,omitempty"`
	ClearedAt                    *time.Time `json:"cleared_at,omitempty"`
	ClearedBy                    string     `json:"cleared_by,omitempty"`
	// AgedOutAt explains a claim-gated step 0 that no operator asked for: the
	// 24h sweep forgot this group's escalation.
	AgedOutAt         *time.Time `json:"aged_out_at,omitempty"`
	Evidence          *Evidence  `json:"evidence,omitempty"`
	ProviderResetHint *ResetHint `json:"provider_reset_hint,omitempty"`
	Lease             *Lease     `json:"probe_lease"`
	// LeaseExpired reports that the stored lease is past its resolution instant
	// and will be resolved by the next claim on this group. For a pre-launch
	// process owner that instant is bounded by claim_deadline, so a wedged
	// claimant is shown as expired at the deadline rather than at a renewed
	// expiry the resolver will never wait for.
	LeaseExpired bool `json:"lease_expired,omitempty"`
}

// Report projects the given identities. Passing no identity yields a report with
// the armed token and bounds only, which is what a machine with no state has.
func (s *Store) Report(identities []Identity) Report {
	now := s.now()
	report := Report{
		Version:     SchemaVersion,
		GeneratedAt: now,
		Bounds: ReportBoundsBlock{
			ProbeLeaseMinutes:    s.leaseMinutes,
			ProbeClaimMaxMinutes: s.claimMaxMinutes,
		},
	}
	for _, step := range s.ladder {
		report.Ladder = append(report.Ladder, step.String())
	}
	for _, identity := range identities {
		state := s.LoadIdentityState(identity)
		entry := IdentityReport{
			Identity:      identity.Key,
			Provider:      identity.Provider,
			HomeDisplay:   identity.HomeDisplay,
			SchemaAhead:   state.ReadOnly,
			StateRead:     state.Read,
			Indeterminate: state.Read.Indeterminate(),
			Groups:        []GroupReport{},
		}
		names := make([]string, 0, len(state.Groups))
		for name := range state.Groups {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			entry.Groups = append(entry.Groups, s.groupReport(name, state.Groups[name], now))
		}
		report.Identities = append(report.Identities, entry)
	}
	return report
}

func (s *Store) groupReport(name string, record GroupRecord, now time.Time) GroupReport {
	effective := record.EffectiveState(now)
	out := GroupReport{
		Group:                        name,
		Provider:                     record.Provider,
		State:                        record.State,
		EffectiveState:               effective,
		Claimable:                    effective == StateProbeEligible,
		BackoffStep:                  record.BackoffStep,
		BackoffStepDuration:          s.ladderStep(record.BackoffStep).String(),
		SuppressedSince:              record.SuppressedSince,
		NextProbeAt:                  record.NextProbeAt,
		ConsecutiveLimitObservations: record.ConsecutiveLimitObservations,
		LastSuccessAt:                record.LastSuccessAt,
		ProbeProvisionalSince:        record.ProbeProvisionalSince,
		ProbeAbandoned:               record.ProbeAbandoned,
		ClearedAt:                    record.ClearedAt,
		ClearedBy:                    record.ClearedBy,
		AgedOutAt:                    record.AgedOutAt,
		Evidence:                     record.Evidence,
		ProviderResetHint:            record.ProviderResetHint,
		Lease:                        record.ProbeLease,
	}
	if record.ProbeLease != nil && !now.Before(record.ProbeLease.ResolveAt()) {
		out.LeaseExpired = true
	}
	return out
}

// ReportWithGroupTable is Report with the effective group table folded in, so an
// operator can see which models a suppressed group covers and where the mapping
// came from.
func (s *Store) ReportWithGroupTable(identities []Identity, table *GroupTable) Report {
	report := s.Report(identities)
	if table != nil {
		report.Groups = table.Rows()
	}
	return report
}
