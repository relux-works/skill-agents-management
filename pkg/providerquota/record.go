package providerquota

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/internal/provideridentity"
)

const SchemaVersion = 1
const DefaultTTLS = 600
const ClockTolerance = 30 * time.Second

// MaxFailures bounds history to the latest sixteen delivered failures.
const MaxFailures = 16

type State string

const (
	Exact        State = "exact"
	PercentOnly  State = "percent_only"
	LastObserved State = "last_observed"
	NotSupported State = "not_supported"
	Unavailable  State = "unavailable"
)

type AuthStatus string

const (
	AuthYes     AuthStatus = "yes"
	AuthNo      AuthStatus = "no"
	AuthUnknown AuthStatus = "unknown"
)

// AuthStatus uses the router contract's boolean-or-unknown representation.
func (a AuthStatus) MarshalJSON() ([]byte, error) {
	switch a {
	case AuthYes:
		return []byte("true"), nil
	case AuthNo:
		return []byte("false"), nil
	case AuthUnknown:
		return []byte(`"unknown"`), nil
	}
	return nil, Refuse("authentication_invalid")
}
func (a *AuthStatus) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true":
		*a = AuthYes
	case "false":
		*a = AuthNo
	case `"unknown"`:
		*a = AuthUnknown
	default:
		return Refuse("authentication_invalid")
	}
	return nil
}

type Inventory struct {
	Installed  bool   `json:"installed"`
	AuthSource string `json:"auth_source,omitempty"`
}
type ReadFailure struct {
	Source string    `json:"source"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// Refusal is a typed error with a stable, non-sensitive reason code. Never
// include raw vendor output or a filesystem error in a router-facing failure.
type Refusal struct{ Reason string }

func (e *Refusal) Error() string { return "providerquota: " + e.Reason }
func Refuse(reason string) error { return &Refusal{Reason: reason} }

type QuotaWindow struct {
	ID               string     `json:"id"`
	Kind             string     `json:"kind,omitempty"` // vendor slot; distinguishes repeated limit ids
	Label            string     `json:"label,omitempty"`
	Minutes          int        `json:"minutes"`
	Word             string     `json:"word,omitempty"`
	UsedPercent      *float64   `json:"used_percent,omitempty"`
	RemainingPercent *float64   `json:"remaining_percent,omitempty"`
	ResetsAt         *time.Time `json:"resets_at,omitempty"`
	ObservedAt       *time.Time `json:"observed_at,omitempty"`
	Scope            string     `json:"scope"`
	Description      string     `json:"description,omitempty"`
}
type Credits struct {
	Balance   float64 `json:"balance"`
	Unit      string  `json:"unit"`
	Unlimited bool    `json:"unlimited"`
}
type QuotaRecord struct {
	SchemaVersion  int               `json:"schema_version"`
	Key            string            `json:"key"`
	Identity       *string           `json:"identity"`
	IdentityReason string            `json:"identity_reason,omitempty"`
	Runtime        string            `json:"runtime"`
	Broker         string            `json:"broker,omitempty"`
	HarnessVersion string            `json:"harness_version,omitempty"`
	HomeDisplay    string            `json:"home_display,omitempty"` // operator view only
	Inventory      Inventory         `json:"inventory"`
	Authenticated  AuthStatus        `json:"authenticated"`
	Plan           string            `json:"plan,omitempty"`
	State          State             `json:"state"`
	Confidence     string            `json:"confidence,omitempty"`
	Windows        []QuotaWindow     `json:"windows"`
	Flags          map[string]string `json:"flags,omitempty"`
	Credits        *Credits          `json:"credits,omitempty"`
	Checked        []string          `json:"checked"`
	Basis          string            `json:"basis,omitempty"`
	ObservedAt     *time.Time        `json:"observed_at,omitempty"`
	RetrievedAt    time.Time         `json:"retrieved_at"`
	TTLS           int               `json:"ttl_s"`
	Source         string            `json:"source"`
	Failures       []ReadFailure     `json:"failures"`
	WindowsDigest  string            `json:"digest"`
}

// ParseContext is trusted caller evidence. ReadAt is the live source's read
// time; RetrievedAt is delivery time. No parser substitutes one for the other.
type ParseContext struct {
	Runtime        string
	Broker         string
	Identity       *provideridentity.Identity
	HarnessVersion string
	ReadAt         time.Time
	RetrievedAt    time.Time
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var hashedKey = regexp.MustCompile(`^[a-f0-9]{16}$`)

func (c ParseContext) Key() (string, error) {
	if !identifier.MatchString(c.Runtime) {
		return "", Refuse("runtime_invalid")
	}
	if c.Identity == nil {
		return "runtime-" + c.Runtime, nil
	}
	i := c.Identity
	// No stat, EvalSymlinks or home expansion: trust only normalized evidence.
	if i.Provider != c.Runtime || !filepath.IsAbs(i.Home) || filepath.Clean(i.Home) != i.Home || i.Key != provideridentity.Key(c.Runtime, i.Home) {
		return "", Refuse("key_mismatch")
	}
	return i.Key, nil
}
func Base(c ParseContext, source string) (QuotaRecord, error) {
	key, err := c.Key()
	if err != nil {
		return QuotaRecord{}, err
	}
	r := QuotaRecord{SchemaVersion: SchemaVersion, Key: key, Runtime: c.Runtime, Broker: c.Broker, HarnessVersion: c.HarnessVersion, Authenticated: AuthUnknown, Windows: []QuotaWindow{}, Checked: []string{}, Failures: []ReadFailure{}, TTLS: DefaultTTLS, Source: source, RetrievedAt: c.RetrievedAt, State: Unavailable}
	if c.Identity != nil {
		id := c.Identity.Key
		r.Identity = &id
		r.HomeDisplay = c.Identity.HomeDisplay
	} else {
		r.IdentityReason = "system declares no home"
	}
	return r, nil
}
func Failure(c ParseContext, source, reason string) (QuotaRecord, error) {
	r, err := Base(c, source)
	if err != nil {
		return r, err
	}
	if source != "" {
		r.Inventory.Installed = true
		r.Checked = []string{source}
	}
	r.Failures = append(r.Failures, ReadFailure{Source: source, Reason: reason, At: c.RetrievedAt})
	r.WindowsDigest, _ = WindowsDigest(r.Windows)
	return r, Refuse(reason)
}
func Float(v float64) *float64    { return &v }
func Time(v time.Time) *time.Time { v = v.UTC(); return &v }

// Percent keeps valid over-quota values, never clamps them. A derived negative
// remaining percentage is intentionally not stated; it is not headroom.
func Percent(w *QuotaWindow, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return Refuse("out_of_range")
	}
	w.UsedPercent = Float(v)
	if v <= 100 {
		w.RemainingPercent = Float(100 - v)
	} else {
		w.RemainingPercent = nil
	}
	return nil
}
func Fraction(w *QuotaWindow, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
		return Refuse("out_of_range")
	}
	w.RemainingPercent = Float(v * 100)
	w.UsedPercent = Float(100 - v*100)
	return nil
}
func windowKey(w QuotaWindow) string { return w.ID + "\x00" + w.Kind + "\x00" + w.Scope }
func canonicalWindows(ws []QuotaWindow) []QuotaWindow {
	out := append([]QuotaWindow{}, ws...)
	for i := range out {
		if out[i].ObservedAt != nil {
			out[i].ObservedAt = Time(*out[i].ObservedAt)
		}
		if out[i].ResetsAt != nil {
			out[i].ResetsAt = Time(*out[i].ResetsAt)
		}
	}
	sort.Slice(out, func(i, j int) bool { return windowKey(out[i]) < windowKey(out[j]) })
	return out
}

// WindowsDigest covers canonical parsed windows only, including their source
// measurement times. Retrieval times, failures and prose footers cannot churn it.
func WindowsDigest(ws []QuotaWindow) (string, error) {
	b, err := json.Marshal(canonicalWindows(ws))
	if err != nil {
		return "", Refuse("out_of_range")
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
func oldest(ws []QuotaWindow) *time.Time {
	var out *time.Time
	for _, w := range ws {
		if w.ObservedAt == nil {
			return nil
		}
		if out == nil || w.ObservedAt.Before(*out) {
			out = Time(*w.ObservedAt)
		}
	}
	return out
}
func Finish(r QuotaRecord, c ParseContext) (QuotaRecord, error) {
	r.Windows = canonicalWindows(r.Windows)
	r.ObservedAt = oldest(r.Windows)
	digest, err := WindowsDigest(r.Windows)
	if err == nil {
		r.WindowsDigest = digest
		err = Validate(r, c, c.RetrievedAt)
	}
	if err != nil {
		return Failure(c, r.Source, Reason(err))
	}
	return r, nil
}
func Reason(err error) string {
	if e, ok := err.(*Refusal); ok {
		return e.Reason
	}
	return "read_failure"
}

var sensitive = regexp.MustCompile(`(?i)(\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b|[^\s]+@[^\s]+|\b[a-z][a-z0-9_-]*(?:\.[a-z0-9_-]+)*\.[a-z][a-z0-9_-]*\b|bearer[\s]|(?:token|secret|account_id|org_id)[=:]|\b(?:sk-|ghp_|github_pat_|eyJ)[a-z0-9_-]{8,})`)

// SafeText rejects location/account material rather than silently redacting
// vendor labels. Display-only home/version fields never enter Projection.
func SafeText(s string) bool {
	return !sensitive.MatchString(s) && !strings.ContainsAny(s, "/\\\x00\r\n")
}
func Validate(r QuotaRecord, c ParseContext, now time.Time) error {
	key, err := c.Key()
	if err != nil {
		return err
	}
	if r.Key != key || r.Runtime != c.Runtime {
		return Refuse("key_mismatch")
	}
	if c.Identity == nil {
		if r.Identity != nil || r.IdentityReason == "" {
			return Refuse("key_mismatch")
		}
	} else if r.Identity == nil || *r.Identity != key {
		return Refuse("key_mismatch")
	}
	if r.SchemaVersion != SchemaVersion {
		return Refuse("schema_unsupported")
	}
	switch r.State {
	case Exact, PercentOnly, LastObserved, NotSupported, Unavailable:
	default:
		return Refuse("state_invalid")
	}
	switch r.Source {
	case "codex-app-server", "claude-print-usage", "agy-print-usage", "msp-usage-read", "session-push", "":
	default:
		return Refuse("source_invalid")
	}
	if r.TTLS < 1 || r.TTLS > 86400 {
		return Refuse("ttl_invalid")
	}
	if r.State == NotSupported && (r.Basis == "" || len(r.Checked) != 0 || len(r.Windows) != 0) {
		return Refuse("basis_required")
	}
	if r.State == Unavailable && len(r.Failures) == 0 {
		return Refuse("failure_required")
	}
	if r.Authenticated != AuthYes && r.Authenticated != AuthNo && r.Authenticated != AuthUnknown {
		return Refuse("authentication_invalid")
	}
	if r.Credits != nil && (math.IsNaN(r.Credits.Balance) || math.IsInf(r.Credits.Balance, 0) || r.Credits.Balance < 0 || !SafeText(r.Credits.Unit)) {
		return Refuse("out_of_range")
	}
	if !SafeText(r.Plan) || !SafeText(r.Runtime) || !SafeText(r.Broker) {
		return Refuse("privacy_violation")
	}
	seen := map[string]bool{}
	for _, w := range r.Windows {
		if w.ID == "" || seen[windowKey(w)] {
			return Refuse("window_invalid")
		}
		seen[windowKey(w)] = true
		for _, v := range []string{w.ID, w.Kind, w.Label, w.Word, w.Scope, w.Description} {
			if !SafeText(v) {
				return Refuse("privacy_violation")
			}
		}
		if w.Minutes < 0 {
			return Refuse("out_of_range")
		}
		if w.UsedPercent != nil && (math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0) {
			return Refuse("out_of_range")
		}
		if w.RemainingPercent != nil && (math.IsNaN(*w.RemainingPercent) || math.IsInf(*w.RemainingPercent, 0) || *w.RemainingPercent < 0 || *w.RemainingPercent > 100) {
			return Refuse("out_of_range")
		}
		if w.ObservedAt != nil && w.ObservedAt.After(now.Add(ClockTolerance)) {
			return Refuse("clock_skew")
		}
	}
	if r.ObservedAt != nil && r.ObservedAt.After(now.Add(ClockTolerance)) {
		return Refuse("clock_skew")
	}
	expected := oldest(r.Windows)
	if (expected == nil) != (r.ObservedAt == nil) || expected != nil && !expected.Equal(*r.ObservedAt) {
		return Refuse("observation_mismatch")
	}
	if len(r.Failures) > MaxFailures {
		return Refuse("failure_history_invalid")
	}
	for _, f := range r.Failures {
		if !identifier.MatchString(f.Reason) || !SafeText(f.Source) {
			return Refuse("privacy_violation")
		}
	}
	digest, err := WindowsDigest(r.Windows)
	if err != nil {
		return err
	}
	if digest != r.WindowsDigest {
		return Refuse("digest_mismatch")
	}
	return nil
}

// Projection is the public router contract. Operator homes, diagnostic argv,
// flags and user-agent text cannot cross it.
type Projection struct {
	Identity      string        `json:"identity"`
	Runtime       string        `json:"runtime"`
	State         State         `json:"state"`
	Authenticated AuthStatus    `json:"authenticated"`
	Plan          string        `json:"plan,omitempty"`
	Windows       []QuotaWindow `json:"windows"`
	Credits       *Credits      `json:"credits,omitempty"`
	ObservedAt    *time.Time    `json:"observed_at,omitempty"`
	RetrievedAt   time.Time     `json:"retrieved_at"`
	TTLS          int           `json:"ttl_s"`
	Source        string        `json:"source"`
	Failures      []ReadFailure `json:"failures"`
	Digest        string        `json:"digest"`
}

func (r QuotaRecord) RouterProjection() Projection {
	if !projectionSafe(r) {
		return Projection{State: Unavailable, Authenticated: AuthUnknown, Windows: []QuotaWindow{}, Failures: []ReadFailure{{Reason: "privacy_violation"}}}
	}
	state := r.State
	// Copy through JSON to give the consumer ownership of slices and pointers.
	p := Projection{r.Key, r.Runtime, state, r.Authenticated, r.Plan, r.Windows, r.Credits, r.ObservedAt, r.RetrievedAt, r.TTLS, r.Source, r.Failures, r.WindowsDigest}
	b, err := json.Marshal(p)
	if err != nil {
		return Projection{State: Unavailable, Authenticated: AuthUnknown, Windows: []QuotaWindow{}, Failures: []ReadFailure{{Reason: "out_of_range"}}}
	}
	var out Projection
	_ = json.Unmarshal(b, &out)
	return out
}
func validKey(key string) bool {
	return hashedKey.MatchString(key) || strings.HasPrefix(key, "runtime-") && identifier.MatchString(strings.TrimPrefix(key, "runtime-"))
}

// String deliberately contains no home or raw payload.
func (r QuotaRecord) String() string {
	return fmt.Sprintf("%s: %s (%d windows)", r.Runtime, r.State, len(r.Windows))
}

func projectionSafe(r QuotaRecord) bool {
	digest, err := WindowsDigest(r.Windows)
	if err != nil || digest != r.WindowsDigest {
		return false
	}
	switch r.State {
	case Exact, PercentOnly, LastObserved, NotSupported, Unavailable:
	default:
		return false
	}
	switch r.Authenticated {
	case AuthYes, AuthNo, AuthUnknown:
	default:
		return false
	}
	for _, v := range []string{r.Key, r.Runtime, r.Plan, r.Source, string(r.State), string(r.Authenticated), r.WindowsDigest} {
		if !SafeText(v) {
			return false
		}
	}
	for _, w := range r.Windows {
		for _, v := range []string{w.ID, w.Kind, w.Scope, w.Label, w.Word, w.Description} {
			if !SafeText(v) {
				return false
			}
		}
	}
	if r.Credits != nil && !SafeText(r.Credits.Unit) {
		return false
	}
	for _, f := range r.Failures {
		if !identifier.MatchString(f.Reason) || !SafeText(f.Source) {
			return false
		}
	}
	return true
}
