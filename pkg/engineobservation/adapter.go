// Package engineobservation adapts curator-engines status readings to the
// vendorplugin engine-observation contract.
//
// It consumes the status OUTPUT contract only: `curator-engines status
// --engine <profile> --json` run with the query's project directory as its
// working directory. Engine resolution (how --engine names an entry) belongs
// to the manager's engines.toml catalog and is never interpreted here.
//
// Each ObserveEngine call is one fresh subprocess read. Rows are transcribed
// verbatim into inferenceengine readings and judged by the closed validator
// (ValidateReadings under observed-process/v3) inside BuildLaunch — this
// package never synthesizes a value, fills a default, or admits on the
// manager's behalf. A fact the manager reports as not-observed stays a
// refusal at the validator; a failed, stale, partial or malformed status
// returns an error and never a partial observation.
package engineobservation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// ErrReadingsReadFailed is returned for every way a readings read can fail
// before a decode is even attempted: the binary is not on PATH, the
// subprocess exited non-zero (including the manager's own typed
// readings_read_failed/readings_stale/readings_partial failures, which carry
// no readings set), this adapter's own bounded timeout fired while the
// caller's context was still alive, or the response exceeded the byte cap.
var ErrReadingsReadFailed = errors.New("engineobservation: readings read failed")

// ErrReadingsQueryInvalid is returned when an observation query names no
// resolvable engine project: the resolver declined it, or the resolved
// project is not absolute, or the authoritative profile is empty. Falling
// back to the agent's current directory could observe another project's
// engine.
var ErrReadingsQueryInvalid = errors.New("engineobservation: readings query is invalid")

// ErrReadingsMalformed is returned when the subprocess answered but its
// readings object cannot be honestly transcribed: not a JSON object, no
// readings member, a contract version other than observed-process/v3, or a
// row whose shape contradicts its outcome.
var ErrReadingsMalformed = errors.New("engineobservation: readings are malformed")

const (
	// observationWaitDelay bounds Wait after the status child exits while a
	// pipe is still held open. The group kill closes pipes promptly in the
	// normal case; this only backstops a descendant that escaped it.
	observationWaitDelay = 2 * time.Second
	// readingsCommandTimeout bounds one status subprocess call. It composes
	// with the caller's context: the earlier of the two fires first, and a
	// firing of this bound while the caller is still alive is reported as a
	// read failure rather than attributed to the caller's deadline.
	readingsCommandTimeout = 10 * time.Second
	// defaultResponseMaxBytes caps one status response. Readings are small;
	// the cap matches the status reader's and exists to bound a runaway.
	defaultResponseMaxBytes = 1 << 20 // 1 MiB
	// defaultObservationTTL is how long one transcribed observation stays
	// valid. BuildLaunch consumes it synchronously, so the TTL only guards
	// against clock skew and stale reuse; thirty seconds matches the
	// manager's own provider-evidence freshness window.
	defaultObservationTTL = 30 * time.Second
)

// commandRunner executes one subprocess call and returns its stdout bytes.
// It is the injection seam: production uses runExec, tests substitute a
// scriptable double that never shells out.
type commandRunner func(ctx context.Context, name string, args []string, dir string) ([]byte, error)

// runExec is the production commandRunner: exec.CommandContext, stdout
// captured, stderr discarded. exec.LookPath failure and a non-zero exit both
// surface as the returned error, uniformly.
func runExec(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// The caller deadline (or this adapter's own timeout, whichever fires
	// first) kills the whole observation process group, not just the direct
	// child: a descendant holding the stdout pipe would otherwise keep Run
	// blocked past the deadline. WaitDelay backstops even a descendant that
	// escaped the group. Only this observation command group is killed; the
	// managed engine process lives in its own group and is preserved.
	setObservationProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killObservationProcessGroup(cmd.Process.Pid)
			_ = cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = observationWaitDelay
	err := cmd.Run()
	return stdout.Bytes(), err
}

// ResolveProject maps one authoritative observation query to the absolute
// project directory whose engine profile the query names. It reports false
// when the query names no project this adapter serves.
type ResolveProject func(vendorplugin.EngineObservationQuery) (project string, ok bool)

// Adapter is a vendorplugin.EngineObservationAdapter backed by the released
// `curator-engines status --json` readings surface.
type Adapter struct {
	engine           plugin.Ref
	kind             inferenceengine.EngineKind
	resolve          ResolveProject
	run              commandRunner
	timeout          time.Duration
	responseMaxBytes int
	clock            func() time.Time
	ttl              time.Duration
}

// Option configures an Adapter at construction.
type Option func(*Adapter)

// WithCommandRunner substitutes the subprocess runner. Tests use this to
// avoid invoking a real curator-engines binary.
func WithCommandRunner(run commandRunner) Option {
	return func(a *Adapter) { a.run = run }
}

// WithTimeout overrides this adapter's own bound on a status subprocess call.
func WithTimeout(d time.Duration) Option { return func(a *Adapter) { a.timeout = d } }

// WithResponseCap overrides the stdout byte cap.
func WithResponseCap(n int) Option { return func(a *Adapter) { a.responseMaxBytes = n } }

// WithClock overrides the clock ObservedAt/ValidUntil are stamped from.
func WithClock(now func() time.Time) Option { return func(a *Adapter) { a.clock = now } }

// WithTTL overrides the observation validity window.
func WithTTL(d time.Duration) Option { return func(a *Adapter) { a.ttl = d } }

// NewAdapter returns an adapter serving one inference engine. The engine ref
// and kind are the declaration BuildLaunch matches the launch against; kind
// selects no validation outcome (both kinds share the closed rules) and the
// resolution carrying it is discarded by BuildLaunch after validation.
func NewAdapter(engine plugin.Ref, kind inferenceengine.EngineKind, resolve ResolveProject, opts ...Option) (*Adapter, error) {
	if engine == (plugin.Ref{}) || engine.Kind != inferenceengine.Kind {
		return nil, fmt.Errorf("%w: engine %#v is not an inference-engine ref", ErrReadingsQueryInvalid, engine)
	}
	switch kind {
	case inferenceengine.EngineKindNativeTransformer, inferenceengine.EngineKindGGUFServer:
	default:
		return nil, fmt.Errorf("%w: engine kind %q", ErrReadingsQueryInvalid, kind)
	}
	if resolve == nil {
		return nil, fmt.Errorf("%w: project resolver is nil", ErrReadingsQueryInvalid)
	}
	a := &Adapter{
		engine:           engine,
		kind:             kind,
		resolve:          resolve,
		run:              runExec,
		timeout:          readingsCommandTimeout,
		responseMaxBytes: defaultResponseMaxBytes,
		clock:            time.Now,
		ttl:              defaultObservationTTL,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.run == nil {
		return nil, fmt.Errorf("%w: command runner is nil", ErrReadingsQueryInvalid)
	}
	return a, nil
}

// EngineObservationAdapterDeclaration answers the immutable v3 declaration
// BuildLaunch pins at registration and validates readings under.
func (a *Adapter) EngineObservationAdapterDeclaration() vendorplugin.EngineObservationAdapterDeclaration {
	return vendorplugin.EngineObservationAdapterDeclaration{
		Contract:       vendorplugin.EngineObservationAdapterContract,
		SchemaVersion:  vendorplugin.EngineObservationAdapterSchemaVersion,
		Engine:         a.engine,
		EngineKind:     a.kind,
		EngineContract: inferenceengine.ContractVersionV3,
	}
}

// ObserveEngine performs one bounded, read-only observation. It performs NO
// caching: every call is a fresh subprocess read.
func (a *Adapter) ObserveEngine(ctx context.Context, query vendorplugin.EngineObservationQuery) (vendorplugin.EngineObservation, error) {
	if query.Engine != a.engine {
		return vendorplugin.EngineObservation{}, fmt.Errorf("%w: adapter serves %#v, query names %#v", ErrReadingsQueryInvalid, a.engine, query.Engine)
	}
	if strings.TrimSpace(query.Profile) == "" {
		return vendorplugin.EngineObservation{}, fmt.Errorf("%w: query carries no engine profile", ErrReadingsQueryInvalid)
	}
	project, ok := a.resolve(query)
	if !ok || !filepath.IsAbs(project) {
		return vendorplugin.EngineObservation{}, fmt.Errorf("%w: query names no absolute engine project", ErrReadingsQueryInvalid)
	}

	boundedCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	args := []string{"status", "--engine", query.Profile, "--json"}
	output, err := a.run(boundedCtx, "curator-engines", args, project)
	if err != nil {
		// Attribute to the caller only when the caller's own context fired;
		// this adapter's internal timeout is a read failure, never proof the
		// caller's deadline passed. %v (not %w) keeps an internal
		// DeadlineExceeded from matching the caller's deadline downstream.
		if ctx.Err() != nil {
			return vendorplugin.EngineObservation{}, ctx.Err()
		}
		return vendorplugin.EngineObservation{}, fmt.Errorf("%w: curator-engines status: %v", ErrReadingsReadFailed, err)
	}
	if len(output) > a.responseMaxBytes {
		return vendorplugin.EngineObservation{}, fmt.Errorf("%w: response is %d bytes, exceeding the %d byte cap", ErrReadingsReadFailed, len(output), a.responseMaxBytes)
	}

	readings, err := transcribeReadings(output)
	if err != nil {
		return vendorplugin.EngineObservation{}, err
	}

	now := a.clock()
	return vendorplugin.EngineObservation{
		Contract:      vendorplugin.EngineObservationAdapterContract,
		SchemaVersion: vendorplugin.EngineObservationAdapterSchemaVersion,
		Engine:        query.Engine,
		Runtime:       query.Runtime,
		Model:         query.Model,
		Profile:       query.Profile,
		ObservedAt:    now,
		ValidUntil:    now.Add(a.ttl),
		Readings:      readings,
	}, nil
}

// outerOwned is the owned canonical member set of the outer status envelope.
// The envelope stays additive (unrelated broker members are tolerated), but
// a case-fold alias of "readings" is ambiguous evidence and refuses.
var outerOwned = map[string]bool{"readings": true}

// readingsAllowed is the exact closed member set of the additive `readings`
// member of `curator-engines status --json` (engines-protocol v1.1).
var readingsAllowed = map[string]bool{"contract_version": true, "facts": true}

// rowAllowed is the exact closed member set of one published fact row.
// Source is decoded only so the closed member check accepts it, and its
// value is never validation input.
var rowAllowed = map[string]bool{
	"fact": true, "outcome": true, "value": true,
	"cause": true, "reason": true, "source": true,
}

// transcribeReadings extracts and transcribes the readings member of one
// status response through the shared strict decoder. The outer envelope
// accepts additive broker members but still rejects duplicate keys and
// case-fold aliases of the owned "readings" member; the readings object
// and each row enforce exact-case closed sets, required presence and
// non-null members. Row order and count are passed through verbatim for
// the closed validator to judge.
func transcribeReadings(output []byte) ([]inferenceengine.Reading, error) {
	outer, err := decodeStrictObject(output, nil, []string{"readings"}, outerOwned)
	if err != nil {
		return nil, fmt.Errorf("%w: response envelope: %v", ErrReadingsMalformed, err)
	}
	readingsRaw := outer["readings"]
	if isJSONNull(readingsRaw) {
		return nil, fmt.Errorf("%w: member %q is null", ErrReadingsMalformed, "readings")
	}
	readings, err := decodeStrictObject(readingsRaw, readingsAllowed, []string{"contract_version", "facts"}, readingsAllowed)
	if err != nil {
		return nil, fmt.Errorf("%w: readings envelope: %v", ErrReadingsMalformed, err)
	}
	version, err := requiredStrictString(readings, "contract_version")
	if err != nil {
		return nil, fmt.Errorf("%w: readings envelope: %v", ErrReadingsMalformed, err)
	}
	if version != inferenceengine.ContractVersionV3 {
		return nil, fmt.Errorf("%w: contract_version %q, want %q", ErrReadingsMalformed, version, inferenceengine.ContractVersionV3)
	}
	factsRaw := readings["facts"]
	if isJSONNull(factsRaw) {
		return nil, fmt.Errorf("%w: member %q is null", ErrReadingsMalformed, "facts")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(factsRaw, &rows); err != nil {
		return nil, fmt.Errorf("%w: member %q is not an array: %v", ErrReadingsMalformed, "facts", err)
	}
	transcribed := make([]inferenceengine.Reading, 0, len(rows))
	for index, rowRaw := range rows {
		members, err := decodeStrictObject(rowRaw, rowAllowed, []string{"fact", "outcome", "source"}, rowAllowed)
		if err != nil {
			return nil, fmt.Errorf("%w: fact %d: %v", ErrReadingsMalformed, index, err)
		}
		reading, err := transcribeStrictRow(index, members)
		if err != nil {
			return nil, err
		}
		transcribed = append(transcribed, reading)
	}
	return transcribed, nil
}

// transcribeStrictRow maps one strictly decoded row onto one validator
// input. Values pass through verbatim; only the row's shape against its
// outcome is judged here. A row the manager marked not-observed keeps its
// cause, so read failures, malformed rows and unsupported facts stay
// distinct at the validator.
func transcribeStrictRow(index int, members map[string]json.RawMessage) (inferenceengine.Reading, error) {
	factName, err := requiredStrictString(members, "fact")
	if err != nil {
		return inferenceengine.Reading{}, fmt.Errorf("%w: fact %d: %v", ErrReadingsMalformed, index, err)
	}
	outcome, err := requiredStrictString(members, "outcome")
	if err != nil {
		return inferenceengine.Reading{}, fmt.Errorf("%w: fact %d: %v", ErrReadingsMalformed, index, err)
	}
	if _, err := requiredStrictString(members, "source"); err != nil {
		return inferenceengine.Reading{}, fmt.Errorf("%w: fact %d: %v", ErrReadingsMalformed, index, err)
	}
	value, err := optionalStrictString(members, "value")
	if err != nil {
		return inferenceengine.Reading{}, fmt.Errorf("%w: fact %d: %v", ErrReadingsMalformed, index, err)
	}
	cause, err := optionalStrictString(members, "cause")
	if err != nil {
		return inferenceengine.Reading{}, fmt.Errorf("%w: fact %d: %v", ErrReadingsMalformed, index, err)
	}
	reason, err := optionalStrictString(members, "reason")
	if err != nil {
		return inferenceengine.Reading{}, fmt.Errorf("%w: fact %d: %v", ErrReadingsMalformed, index, err)
	}
	fact := inferenceengine.Fact(factName)
	at := fmt.Sprintf("fact %d (%q)", index, factName)
	switch outcome {
	case string(inferenceengine.OutcomeObservedValue):
		if value == nil {
			return inferenceengine.Reading{}, fmt.Errorf("%w: %s claims observed-value without a value", ErrReadingsMalformed, at)
		}
		if cause != nil || reason != nil {
			return inferenceengine.Reading{}, fmt.Errorf("%w: %s carries failure fields on an observed value", ErrReadingsMalformed, at)
		}
		return inferenceengine.NewReading(fact, inferenceengine.ReadValue(*value)), nil
	case string(inferenceengine.OutcomeObservedAbsent):
		if value != nil || cause != nil || reason != nil {
			return inferenceengine.Reading{}, fmt.Errorf("%w: %s carries value or failure fields on an absence", ErrReadingsMalformed, at)
		}
		return inferenceengine.NewReading(fact, inferenceengine.ReadAbsent()), nil
	case string(inferenceengine.OutcomeNotObserved):
		if value != nil {
			return inferenceengine.Reading{}, fmt.Errorf("%w: %s carries a value on a non-observation", ErrReadingsMalformed, at)
		}
		if cause == nil || reason == nil || *reason == "" {
			return inferenceengine.Reading{}, fmt.Errorf("%w: %s lacks a cause or reason", ErrReadingsMalformed, at)
		}
		var notObserved inferenceengine.NotObservedCause
		switch *cause {
		case string(inferenceengine.NotObservedReadFailure):
			notObserved = inferenceengine.NotObservedReadFailure
		case string(inferenceengine.NotObservedMalformed):
			notObserved = inferenceengine.NotObservedMalformed
		case string(inferenceengine.NotObservedUnsupported):
			notObserved = inferenceengine.NotObservedUnsupported
		default:
			return inferenceengine.Reading{}, fmt.Errorf("%w: %s names unknown cause %q", ErrReadingsMalformed, at, *cause)
		}
		return inferenceengine.NewReading(fact, inferenceengine.ReadFailure(notObserved, *reason)), nil
	default:
		return inferenceengine.Reading{}, fmt.Errorf("%w: %s names unknown outcome %q", ErrReadingsMalformed, at, outcome)
	}
}
