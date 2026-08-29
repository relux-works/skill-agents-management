// Package localruntime is the versioned, machine-local status client for a
// shared local-model runtime (Process A/B, as the architecture decision
// names them).
//
// It owns two things and nothing else: the status TYPES a caller reads, and
// a StatusReader implementation that shells out to the already-shipped
// `agents-infra runtime status --json` subprocess. It never starts, stops or
// signals the process it reports on — that authority lives entirely in
// relux-agents-infra's own shared-runtime broker. A static import-graph
// check in this module refuses os/exec (or any process-control package) in
// the local-models vendor's own import graph; this package's use of os/exec
// is confined to the read-only status subprocess call below, never to a
// launch of the model server itself.
package localruntime

import (
	"context"
	"time"
)

// RuntimeID and ModelID mirror vendorplugin's own identifier kinds. They are
// separate string types here, not aliases, so this package carries no
// dependency on pkg/vendorplugin at all — the dependency runs the other way,
// from the vendor plugin into this package, never back.
type RuntimeID string
type ModelID string

// BrokerObservationSource is the broker's own wire Source vocabulary: how
// confidently a status read can vouch for the BrokerState it also reports.
type BrokerObservationSource string

const (
	// SourceDetermined means no lock, no record: positively absent. The
	// ordinary cold-start shape.
	SourceDetermined BrokerObservationSource = "determined"
	// SourceCandidateOnly means an election lock is held but no record has
	// been written yet — contention in progress, not proof of anything.
	SourceCandidateOnly BrokerObservationSource = "kernel-candidate-reporting-only"
	// SourceRecordUnverified means a state file exists but THIS read could
	// not, or did not, open a live connection to confirm it.
	SourceRecordUnverified BrokerObservationSource = "record-derived-unverified"
	// SourceAttested means a live connection succeeded and the broker
	// self-reported its own state.
	SourceAttested BrokerObservationSource = "attested"
)

// StatusContract and StatusSchemaVersion are this module's OWN contract
// identity, stamped onto every Status this package produces. Neither is ever
// read from the upstream wire JSON — they say what THIS adapter promises,
// not what the broker sent.
const (
	StatusContract      = "local-runtime.status"
	StatusSchemaVersion = 1
)

// Observation is one thing this reader itself observed about a status read —
// not the broker's own Observation vocabulary, which this package has none
// of. It exists so a caller (local-models's Availability, primarily) has a
// bounded trail of recent reads to cite as evidence.
type Observation struct {
	Source string
	Detail string
}

// StatusQuery is the one immutable, fully-resolved identity a status read is
// about.
//
// Every field is sourced from the SAME agentic.LaunchRequest a vendor's
// Spawn produced and BuildLaunch completed — never recovered from ambient
// CWD, an environment variable read directly by this package, or mutable
// package state. A caller that leaves a field empty gets a query that says
// so; this package never fills one in from a fallback.
type StatusQuery struct {
	// Runtime is which declared runtime is asking — the fact that lets one
	// shared pi system instance distinguish two runtimes that share a vendor
	// and ModelID.
	Runtime RuntimeID
	// Model is the model row the caller resolved.
	Model ModelID
	// AgentsInfraProject is the agents-infra checkout this query is about.
	AgentsInfraProject string
	// AgentsInfraProfile is the agents.pi.profiles.<name> key inside that
	// checkout's project config.
	AgentsInfraProfile string
}

// Status is one status read's whole answer.
type Status struct {
	// Contract and SchemaVersion are this adapter's own constants (above),
	// carried on every value so a caller serializing a Status can tell which
	// contract produced it.
	Contract      string
	SchemaVersion int

	Runtime RuntimeID
	Model   ModelID

	// BrokerState and BrokerSource are the raw wire values, validated
	// JOINTLY against the frozen eleven-pair vocabulary (see decode.go) —
	// never collapsed into a lossy enum before a caller sees them.
	BrokerState  string
	BrokerSource BrokerObservationSource

	// PID and StartedAt come from the wire's nullable `runtime` object. Both
	// are zero when no process was observed, which is a legitimate, common
	// answer and not a decode failure.
	PID       int
	StartedAt time.Time

	ActiveLeases int
	MaxLeases    int

	// Observed is this reader's own bounded trail of recent reads (see
	// §4.6), never the broker's.
	Observed []Observation

	// AsOf is this adapter's own clock, at successful decode.
	AsOf time.Time
}

// StatusReader is what a caller (pi's Preflight, local-models's
// Availability) reads a machine-local status through.
//
// A single read is always fresh: nothing about a Status answer is memoized
// across calls (unlike local-models.toml's own load, which IS memoized
// forever — a different fact behind a different loader).
type StatusReader interface {
	Status(ctx context.Context, query StatusQuery) (Status, error)
}
