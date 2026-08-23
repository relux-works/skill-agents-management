package regress

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/providerlimits"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// CLASS 3 — an availability verdict derived from a real state file.
//
// The limit plane FAILS OPEN. A state file nobody can find reads as "provider
// healthy" with no error anywhere: no exception, no warning, no operator-
// visible symptom until the provider says no again. That makes every check in
// this file a negative one at heart — a green suite proves nothing unless the
// disaster it guards against is demonstrated happening.
//
// So the fixtures are bytes this repository did not write. testdata/
// source-written/ holds suppressions written by a program compiled against the
// EXTRACTION SOURCE's own providerlimits, through its production classify ->
// Observe chain, with a manifest recording the identity, home, group and model
// behind each one. Reading them here through providerlimits.Store.
// AvailabilityFor is the whole seam: source bytes in, vendorplugin.Availability
// out.

// sourceWrittenDir is the fixture set, addressed relative to this package.
//
// It is the limit plane's own testdata rather than a copy: a second copy of a
// captured fixture is a second thing to re-capture, and the day the two
// disagree neither is evidence of anything.
const sourceWrittenDir = "../../pkg/providerlimits/testdata/source-written"

// xrtFixedNow is the instant the capture harness anchored on. Reading the
// captured suppressions at it puts them INSIDE their window, which is what
// makes them Limited rather than probe-eligible.
var xrtFixedNow = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

// xrtEntry is one manifest row: everything a caller needs to ask the question
// the fixture answers, recorded by the capture rather than restated here.
type xrtEntry struct {
	Provider       string `json:"provider"`
	HomeNormalized string `json:"home_normalized"`
	IdentityKey    string `json:"identity_key"`
	Group          string `json:"group"`
	Model          string `json:"model"`
	StateFile      string `json:"state_file"`
}

func xrtManifest(t *testing.T) []xrtEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sourceWrittenDir, "xrt-manifest.json"))
	if err != nil {
		// A failed read is not an empty manifest. Ranging over nothing would
		// report a green gate that never looked at a fixture.
		t.Fatalf("reading the cross-binary manifest: %v", err)
	}
	var entries []xrtEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decoding the cross-binary manifest: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the cross-binary manifest is empty, so every check below would range over nothing")
	}
	return entries
}

// plantState copies one captured state file into a scratch layout under the
// name identityKey, and returns a store reading it at now.
//
// The name is a PARAMETER because that is the mutation this class needs: the
// same bytes filed under a key one digit away is the moved-identity disaster,
// and it has to be produced the way it would really happen — the file is where
// it always was, and the reader looks somewhere else.
func plantState(t *testing.T, fixture, identityKey string, now time.Time) *providerlimits.Store {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join(sourceWrittenDir, fixture))
	if err != nil {
		t.Fatalf("reading the captured state file %s: %v", fixture, err)
	}
	layout := providerlimits.LayoutAt(t.TempDir())
	if err := os.MkdirAll(layout.Root, 0o755); err != nil {
		t.Fatalf("creating the scratch state root: %v", err)
	}
	if err := os.WriteFile(layout.StateFile(identityKey), bytes, 0o644); err != nil {
		t.Fatalf("planting the captured state file: %v", err)
	}
	store, err := providerlimits.NewStore(providerlimits.Options{
		Layout: layout,
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// nextProbeAt reads the window out of the fixture's own bytes, so the
// assertion below compares the verdict against what the SOURCE computed rather
// than against a number retyped here.
func nextProbeAt(t *testing.T, fixture, group string) time.Time {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sourceWrittenDir, fixture))
	if err != nil {
		t.Fatalf("reading the captured state file %s: %v", fixture, err)
	}
	var decoded struct {
		Groups map[string]struct {
			NextProbeAt *time.Time `json:"next_probe_at"`
			Evidence    *struct {
				Marker string `json:"marker"`
				RunID  string `json:"run_id"`
			} `json:"evidence"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding %s: %v", fixture, err)
	}
	record, ok := decoded.Groups[group]
	if !ok {
		t.Fatalf("%s carries no group %q; the manifest and the fixture disagree", fixture, group)
	}
	if record.NextProbeAt == nil {
		t.Fatalf("%s suppressed %q with no next_probe_at; this fixture cannot pin a window", fixture, group)
	}
	return *record.NextProbeAt
}

func verdictDetails(verdict vendorplugin.Availability) string {
	var b strings.Builder
	for _, observation := range verdict.Observed {
		b.WriteString(observation.Source)
		b.WriteString(": ")
		b.WriteString(observation.Detail)
		b.WriteString("\n")
	}
	for _, failure := range verdict.Failures {
		b.WriteString(failure.Source)
		b.WriteString(": ")
		b.WriteString(failure.Reason)
		b.WriteString("\n")
	}
	return b.String()
}

// TestASourceWrittenSuppressionReadsAsLimitedAtItsOwnWindow is the positive,
// driven through the real verdict entry point.
func TestASourceWrittenSuppressionReadsAsLimitedAtItsOwnWindow(t *testing.T) {
	for _, entry := range xrtManifest(t) {
		t.Run(entry.Provider, func(t *testing.T) {
			// The identity is recomputed here, not taken from the manifest:
			// the filename a DIFFERENT binary chose has to be the one this
			// code would choose, or the read finds nothing and fails open.
			if got := providerlimits.IdentityKey(entry.Provider, entry.HomeNormalized); got != entry.IdentityKey {
				t.Fatalf("IdentityKey(%q, %q) = %q, want the captured %q — the on-disk identity MOVED and every suppression on every machine is orphaned",
					entry.Provider, entry.HomeNormalized, got, entry.IdentityKey)
			}

			store := plantState(t, entry.StateFile, entry.IdentityKey, xrtFixedNow)
			verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{
				Runtime: entry.Provider,
				Model:   entry.Model,
				Home:    entry.HomeNormalized,
			})
			if err != nil {
				t.Fatalf("AvailabilityFor: %v", err)
			}
			if err := verdict.Validate(); err != nil {
				t.Fatalf("the verdict does not satisfy the vendor contract: %v", err)
			}
			if verdict.State != vendorplugin.AvailabilityLimited {
				t.Fatalf("a group the source suppressed reads %s, want limited\n%s", verdict.State, verdictDetails(verdict))
			}
			if verdict.Serviceable() {
				t.Error("a suppressed group reads as serviceable")
			}
			if want := nextProbeAt(t, entry.StateFile, entry.Group); !verdict.Until.Equal(want) {
				t.Errorf("verdict.Until = %s, want the source's own next_probe_at %s", verdict.Until, want)
			}
			if len(verdict.Checked) == 0 {
				t.Error("the verdict names no source, so nothing says where it came from")
			}
		})
	}
}

// TestAMovedIdentityKeyMakesTheSuppressionVanishFailOpen is THE DISASTER,
// asserted rather than described. It is what gives the test above its bite:
// without it, a verdict path that ignored the file entirely and a verdict path
// that read it would be told apart by nothing.
func TestAMovedIdentityKeyMakesTheSuppressionVanishFailOpen(t *testing.T) {
	for _, entry := range xrtManifest(t) {
		t.Run(entry.Provider, func(t *testing.T) {
			moved := moveOneHexDigit(t, entry.IdentityKey)
			store := plantState(t, entry.StateFile, moved, xrtFixedNow)
			verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{
				Runtime: entry.Provider,
				Model:   entry.Model,
				Home:    entry.HomeNormalized,
			})
			if err != nil {
				t.Fatalf("AvailabilityFor: %v", err)
			}
			if !verdict.Serviceable() {
				t.Fatalf("the same bytes filed one hex digit away still read %s; this test no longer demonstrates the fail-open behaviour it exists to demonstrate, and the positive test above has lost its bite",
					verdict.State)
			}
		})
	}
}

// TestAnElapsedWindowIsNotServiceable narrows the bound from the other side.
//
// A reader that treated every suppression as permanent would pass the positive
// test; a reader that treated an elapsed window as clear would pass it too and
// walk the next spawn into the exhausted subscription. Past next_probe_at the
// group is probe-eligible, which is admitted to ONE caller by winning an atomic
// claim — and a claim is a write the read path may not take, so the read must
// not report it healthy.
func TestAnElapsedWindowIsNotServiceable(t *testing.T) {
	for _, entry := range xrtManifest(t) {
		t.Run(entry.Provider, func(t *testing.T) {
			after := nextProbeAt(t, entry.StateFile, entry.Group).Add(time.Hour)
			store := plantState(t, entry.StateFile, entry.IdentityKey, after)
			verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{
				Runtime: entry.Provider,
				Model:   entry.Model,
				Home:    entry.HomeNormalized,
			})
			if err != nil {
				t.Fatalf("AvailabilityFor: %v", err)
			}
			if verdict.Serviceable() {
				t.Errorf("past its window the group reads serviceable from a READ path; a probe is won by an atomic claim, not by looking\n%s", verdictDetails(verdict))
			}
		})
	}
}

// TestAnUnreadableStateFileIsUnknownAndNeverHealthy is the absence/failure
// distinction, which is the one the fail-open default is most likely to eat.
//
// An absent file is a proven absence and reads healthy by design. A file that
// is THERE and cannot be parsed is not an absence, and a fallback defined for
// absence must not fire on a read failure.
func TestAnUnreadableStateFileIsUnknownAndNeverHealthy(t *testing.T) {
	for _, entry := range xrtManifest(t) {
		t.Run(entry.Provider, func(t *testing.T) {
			layout := providerlimits.LayoutAt(t.TempDir())
			if err := os.MkdirAll(layout.Root, 0o755); err != nil {
				t.Fatalf("creating the scratch state root: %v", err)
			}
			if err := os.WriteFile(layout.StateFile(entry.IdentityKey), []byte("{\"version\": 3, \"groups\": {"), 0o644); err != nil {
				t.Fatalf("planting a truncated state file: %v", err)
			}
			store, err := providerlimits.NewStore(providerlimits.Options{
				Layout: layout,
				Now:    func() time.Time { return xrtFixedNow },
			})
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{
				Runtime: entry.Provider,
				Model:   entry.Model,
				Home:    entry.HomeNormalized,
			})
			if err != nil {
				t.Fatalf("AvailabilityFor: %v", err)
			}
			if verdict.Serviceable() {
				t.Fatalf("a state file that could not be read reports the provider healthy; an absence and a failure to read are different facts\n%s", verdictDetails(verdict))
			}
			if verdict.State != vendorplugin.AvailabilityUnknown {
				t.Errorf("an unreadable state file reads %s, want unknown", verdict.State)
			}
			if len(verdict.Failures) == 0 {
				t.Error("the verdict carries no read failure, so nothing downstream can say why it is unknown")
			}
		})
	}
}

// moveOneHexDigit returns the key with its last hex digit advanced by one,
// which is the smallest change that still names a different file.
func moveOneHexDigit(t *testing.T, key string) string {
	t.Helper()
	if key == "" {
		t.Fatal("cannot move an empty identity key")
	}
	const digits = "0123456789abcdef"
	last := key[len(key)-1]
	index := strings.IndexByte(digits, last)
	if index < 0 {
		t.Fatalf("identity key %q does not end in a hex digit", key)
	}
	return key[:len(key)-1] + string(digits[(index+1)%len(digits)])
}
