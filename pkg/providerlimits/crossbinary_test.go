package providerlimits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// THE INVARIANT, checked against bytes this repository did not produce.
//
// docs/architecture.md invariant 2: the limit plane fails open and silent, so
// the on-disk identity — IdentityKey(provider, home) feeding the state
// FILENAME — must never move. A port that changed it would orphan every
// operator's suppressions with no error anywhere: the next read finds no file,
// reports a proven absence, and every exhausted group reads available.
//
// A round trip through one implementation cannot establish that. This file
// therefore works on three sets of bytes that came from somewhere else:
//
//   - testdata/real-state/: the operator's LIVE machine state, written by the
//     extraction source's binary over months of real runs, copied verbatim by
//     .scripts/capture-limit-state.sh. Nothing in this repository has ever
//     written a byte of it.
//   - testdata/source-written/: a suppression written HERE by a program
//     compiled against the SOURCE module's own providerlimits package, through
//     its production entry points (classify -> QuotaObservation -> Observe).
//   - testdata/source-read/: the SOURCE's own Report over a state file THIS
//     repository's ported code wrote.
//
// Between them the round trip is closed in both directions and anchored to
// pre-extraction reality.

const (
	realStateDir     = "testdata/real-state"
	sourceWrittenDir = "testdata/source-written"
	sourceReadDir    = "testdata/source-read"
)

// pinnedRealIdentity is one live state file, its recorded provider, the home
// path that produced its name, and that name as a LITERAL.
//
// The literal is the point. The extraction source's own
// TestIdentityKeyIsTheDocumentedHash re-derives the hash with the same formula
// the production code just used, which passes for any formula they share; its
// one by-value pin covers one synthetic pair. These are pinned by value AND
// they are filenames a different binary chose, so a change to the hash input —
// swapping the provider for the broker, reordering, changing the 0x00
// separator, widening past 16 hex digits — fails here with the operator's real
// state named in the failure.
//
// The homes are the real paths behind each file's home_display. They are
// absolute and already symlink-resolved, so NormalizeProviderHome is an
// identity on them and the key is a function of the string alone.
var pinnedRealIdentities = []struct {
	file     string
	provider string
	home     string
	key      string
}{
	{
		file:     "8ec4c1a052a55ed6.state.json",
		provider: ProviderCodex,
		home:     "/Users/alexis/.codex",
		key:      "8ec4c1a052a55ed6",
	},
	{
		file:     "09e9e699566c6827.state.json",
		provider: ProviderCodex,
		home:     "/Users/alexis/Library/Application Support/task-board/session-manager/19e03767f1bb988edc779466934ee2e43e7d9b1362c162d94b1dfcb2b3f10d46/sessions/SES-qdBSb8p52fBH37waBUYgQxV1/codex-home",
		key:      "09e9e699566c6827",
	},
}

// TestRealStateFilenamesAreTheIdentityKeyPinnedByValue is the invariant itself.
func TestRealStateFilenamesAreTheIdentityKeyPinnedByValue(t *testing.T) {
	for _, pinned := range pinnedRealIdentities {
		if got := IdentityKey(pinned.provider, pinned.home); got != pinned.key {
			t.Errorf("IdentityKey(%q, %q) = %q, want the pinned %q — the on-disk identity MOVED, and every suppression under %s is now orphaned silently",
				pinned.provider, pinned.home, got, pinned.key, pinned.file)
		}
		if want := pinned.key + ".state.json"; pinned.file != want {
			t.Errorf("fixture %s does not carry its own identity key as its name (want %s); the fixture set is inconsistent", pinned.file, want)
		}
		// The filename the LAYOUT would choose has to be the one that is
		// already on disk, not merely the same 16 characters somewhere.
		layout := LayoutAt(t.TempDir())
		if got := filepath.Base(layout.StateFile(IdentityKey(pinned.provider, pinned.home))); got != pinned.file {
			t.Errorf("Layout.StateFile would name this identity %q; the operator's file is %q", got, pinned.file)
		}
	}
}

// TestRealStateFilesRoundTripByteForByte decodes the operator's live files with
// this package's own on-disk types and re-encodes them with the exact function
// the write path uses, then compares BYTES.
//
// Byte equality, not deep equality of decoded values, is the assertion that
// catches the changes that matter and that a value comparison misses: a renamed
// JSON tag, a field reordered in the struct (Go marshals in declaration order),
// an omitempty added or dropped, a *time.Time turned into a time.Time so an
// absent instant starts serialising as the zero year. Every one of those is a
// silent compatibility break for a machine running two binaries against one
// state directory, which the source's own Lease.ResolveAt comment says is a
// real condition rather than a hypothetical.
func TestRealStateFilesRoundTripByteForByte(t *testing.T) {
	files := stateFilesIn(t, realStateDir)
	if len(files) == 0 {
		t.Fatal("no real state fixtures found; this test would pass vacuously")
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			original := readRawFixture(t, realStateDir, name)
			var file identityStateFile
			if err := json.Unmarshal(original, &file); err != nil {
				t.Fatalf("this package cannot decode the operator's own state file: %v", err)
			}
			assertReencodesTo(t, &file, original)
		})
	}
}

// TestRealIdentityIndexRoundTripsByteForByte does the same for the identity
// index. The index carries no correctness weight — the source rebuilds it from
// the per-identity files — but a binary that cannot decode it rebuilds it on
// every read, which is a silent performance and warning regression rather than
// a visible failure.
func TestRealIdentityIndexRoundTripsByteForByte(t *testing.T) {
	original := readRawFixture(t, realStateDir, "identities.json")
	var index IdentityIndex
	if err := json.Unmarshal(original, &index); err != nil {
		t.Fatalf("this package cannot decode the operator's identity index: %v", err)
	}
	assertReencodesTo(t, &index, original)
}

// TestTheRealSuppressionOnDiskStillReadsAsASuppression drives the real file
// through the actual read path, not through a decode.
//
// The older of the two live files carries a codex suppression at backoff step
// 2 from August 4th with four consecutive limit observations. Reading it must
// still produce that record, under its original group key, with the evidence
// intact — a port that decoded the file but lost a field would pass the byte
// round trip above only if the field survived, and this pins that the values
// reach a caller rather than merely surviving a re-encode.
func TestTheRealSuppressionOnDiskStillReadsAsASuppression(t *testing.T) {
	const (
		identityKey = "09e9e699566c6827"
		group       = "codex-unmapped:"
	)
	state, _ := loadFixtureState(t, realStateDir, identityKey, ProviderCodex,
		time.Date(2026, 8, 4, 18, 55, 0, 0, time.UTC))
	if state.Read != StateReadOK {
		t.Fatalf("state read = %q, want %q; the operator's own file must read cleanly", state.Read, StateReadOK)
	}
	record, ok := state.Groups[group]
	if !ok {
		t.Fatalf("group %q is not in the loaded state; the persisted group key moved. Groups: %v", group, groupNames(state))
	}
	if record.State != StateSuppressed {
		t.Errorf("record.State = %q, want %q", record.State, StateSuppressed)
	}
	if record.BackoffStep != 2 {
		t.Errorf("record.BackoffStep = %d, want 2", record.BackoffStep)
	}
	if record.ConsecutiveLimitObservations != 4 {
		t.Errorf("record.ConsecutiveLimitObservations = %d, want 4", record.ConsecutiveLimitObservations)
	}
	if record.Evidence == nil || record.Evidence.Marker != "you've hit your usage limit" {
		t.Errorf("the recorded evidence marker did not survive the read: %+v", record.Evidence)
	}
	if record.NextProbeAt == nil {
		t.Fatal("next_probe_at did not survive the read; the suppression window is gone")
	}
	want := time.Date(2026, 8, 4, 19, 5, 8, 489705000, time.FixedZone("", 3*3600))
	if !record.NextProbeAt.Equal(want) {
		t.Errorf("next_probe_at = %s, want %s", record.NextProbeAt, want)
	}
}

// --- the cross-binary round trip --------------------------------------------

// xrtEntry is one row of the manifest .scripts/limitstate_xrt.go writes beside
// the state it captured. It records the inputs the SOURCE hashed, so this side
// can recompute the filename rather than assume it.
type xrtEntry struct {
	Provider       string `json:"provider"`
	HomeGiven      string `json:"home_given"`
	HomeNormalized string `json:"home_normalized"`
	IdentityKey    string `json:"identity_key"`
	Group          string `json:"group"`
	Model          string `json:"model"`
	StateFile      string `json:"state_file"`
}

func xrtManifest(t *testing.T) []xrtEntry {
	t.Helper()
	var manifest []xrtEntry
	if err := json.Unmarshal(readRawFixture(t, sourceWrittenDir, "xrt-manifest.json"), &manifest); err != nil {
		t.Fatalf("decoding the capture manifest: %v", err)
	}
	if len(manifest) == 0 {
		t.Fatal("the capture manifest is empty; every cross-binary assertion below would pass vacuously")
	}
	return manifest
}

// TestTheSourceBinaryChoseTheFilenameThisCodeWouldChoose closes the identity
// half of the round trip on bytes produced by the other implementation: the
// source hashed (provider, home) and named a file; this recomputes the name
// from the recorded inputs and requires the same answer.
func TestTheSourceBinaryChoseTheFilenameThisCodeWouldChoose(t *testing.T) {
	for _, entry := range xrtManifest(t) {
		identity, err := IdentityFor(entry.Provider, entry.HomeNormalized)
		if err != nil {
			t.Fatalf("IdentityFor(%q, %q): %v", entry.Provider, entry.HomeNormalized, err)
		}
		if identity.Key != entry.IdentityKey {
			t.Errorf("this code keys (%q, %q) as %q; the source binary keyed it as %q — the identity moved",
				entry.Provider, entry.HomeNormalized, identity.Key, entry.IdentityKey)
		}
		if got := filepath.Base(LayoutAt(t.TempDir()).StateFile(identity.Key)); got != entry.StateFile {
			t.Errorf("this code would write %q; the source binary wrote %q", got, entry.StateFile)
		}
		if _, err := os.Stat(filepath.Join(sourceWrittenDir, entry.StateFile)); err != nil {
			t.Errorf("the manifest names %s but no such fixture was captured: %v", entry.StateFile, err)
		}
	}
}

// TestASuppressionWrittenBySourceReadsAsLimitedUntilWithTheSameWindow is
// acceptance criterion 2, driven through the real verdict entry point.
//
// The state file was written by the source's Store.Observe. It is loaded here
// by this package's Store, projected by AvailabilityFor, and the resulting
// verdict must be Limited with Until equal to the next_probe_at the SOURCE
// computed from its own ladder — not merely "some limit".
func TestASuppressionWrittenBySourceReadsAsLimitedUntilWithTheSameWindow(t *testing.T) {
	for _, entry := range xrtManifest(t) {
		t.Run(entry.Provider, func(t *testing.T) {
			state, store := loadFixtureState(t, sourceWrittenDir, entry.IdentityKey, entry.Provider, xrtFixedNow)
			record, ok := state.Groups[entry.Group]
			if !ok {
				t.Fatalf("group %q written by the source is not in the state this code loaded; the persisted group key moved. Groups: %v", entry.Group, groupNames(state))
			}
			if record.NextProbeAt == nil {
				t.Fatal("the source wrote a suppression with no next_probe_at; the fixture is not what this test needs")
			}

			verdict, err := store.AvailabilityFor(VerdictQuery{
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
			if !verdict.Until.Equal(*record.NextProbeAt) {
				t.Errorf("verdict.Until = %s, want the source's own next_probe_at %s", verdict.Until, *record.NextProbeAt)
			}
			if verdict.Serviceable() {
				t.Error("a group the source suppressed reads as serviceable")
			}
			assertVerdictCarriesEvidenceOf(t, verdict, record)
		})
	}
}

// xrtFixedNow is the instant both capture harnesses anchor on. Reading the
// captured suppressions at it puts them inside their window, which is what
// makes them Limited rather than probe-eligible.
var xrtFixedNow = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

func assertVerdictCarriesEvidenceOf(t *testing.T, verdict vendorplugin.Availability, record GroupRecord) {
	t.Helper()
	if record.Evidence == nil {
		return
	}
	joined := verdictDetails(verdict)
	for _, want := range []string{record.Evidence.RunID, record.Evidence.Model, record.Evidence.Marker} {
		if strings.TrimSpace(want) == "" {
			continue
		}
		if !strings.Contains(joined, want) {
			t.Errorf("the verdict does not carry the recorded evidence %q; a limited verdict has to say what established it.\nobservations:\n%s", want, joined)
		}
	}
}

// TestTheSourceReadsTheBytesThisCodeWrites is the other direction, and it is
// the one a port cannot fake.
//
// testdata/source-read/ holds two things: the state file this repository's
// ported Store.Observe wrote, and the SOURCE's own Report over it. If the port
// had drifted in any field the source reads, the source's projection would
// differ — so this compares that projection against what this code projects
// from the same bytes, field by field on everything the source reports.
func TestTheSourceReadsTheBytesThisCodeWrites(t *testing.T) {
	var sourceReport Report
	if err := json.Unmarshal(readRawFixture(t, sourceReadDir, "report.json"), &sourceReport); err != nil {
		t.Fatalf("decoding the source's report: %v", err)
	}
	if len(sourceReport.Identities) == 0 {
		t.Fatal("the source's captured report names no identity; this test would pass vacuously")
	}
	for _, identityReport := range sourceReport.Identities {
		t.Run(identityReport.Identity, func(t *testing.T) {
			if identityReport.StateRead != StateReadOK {
				t.Fatalf("the source read the bytes this code wrote as %q, not %q — its own reader could not interpret them", identityReport.StateRead, StateReadOK)
			}
			if len(identityReport.Groups) == 0 {
				t.Fatal("the source found no group in a file this code wrote with one")
			}
			state, _ := loadFixtureState(t, sourceReadDir, identityReport.Identity, identityReport.Provider, xrtFixedNow)
			for _, sourceGroup := range identityReport.Groups {
				record, ok := state.Groups[sourceGroup.Group]
				if !ok {
					t.Fatalf("the source read group %q out of these bytes and this code does not; the group key is not shared. Groups: %v", sourceGroup.Group, groupNames(state))
				}
				if record.State != sourceGroup.State {
					t.Errorf("group %q: this code reads state %q, the source read %q", sourceGroup.Group, record.State, sourceGroup.State)
				}
				if record.BackoffStep != sourceGroup.BackoffStep {
					t.Errorf("group %q: this code reads backoff step %d, the source read %d", sourceGroup.Group, record.BackoffStep, sourceGroup.BackoffStep)
				}
				if record.ConsecutiveLimitObservations != sourceGroup.ConsecutiveLimitObservations {
					t.Errorf("group %q: this code reads %d consecutive observations, the source read %d",
						sourceGroup.Group, record.ConsecutiveLimitObservations, sourceGroup.ConsecutiveLimitObservations)
				}
				assertInstantsAgree(t, sourceGroup.Group, "next_probe_at", record.NextProbeAt, sourceGroup.NextProbeAt)
				assertInstantsAgree(t, sourceGroup.Group, "suppressed_since", record.SuppressedSince, sourceGroup.SuppressedSince)
				if (record.Evidence == nil) != (sourceGroup.Evidence == nil) {
					t.Fatalf("group %q: evidence presence disagrees (ours=%v source=%v)", sourceGroup.Group, record.Evidence != nil, sourceGroup.Evidence != nil)
				}
				if record.Evidence != nil && *record.Evidence != *sourceGroup.Evidence {
					t.Errorf("group %q: evidence differs.\nours:   %+v\nsource: %+v", sourceGroup.Group, *record.Evidence, *sourceGroup.Evidence)
				}
			}
		})
	}
}

// TestTheSourcesBackoffStepDurationMatchesThisLadder pins the ladder against a
// value the SOURCE computed, rather than against this package's own constant.
//
// The source's report renders backoff_step_duration from ITS ladder for the
// step it recorded. If this port's DefaultLadder had drifted, the same step
// would render a different duration here. That makes this a cross-binary pin
// on the ladder and not only on the ladder's first entry.
func TestTheSourcesBackoffStepDurationMatchesThisLadder(t *testing.T) {
	var sourceReport Report
	if err := json.Unmarshal(readRawFixture(t, sourceReadDir, "report.json"), &sourceReport); err != nil {
		t.Fatalf("decoding the source's report: %v", err)
	}
	store, err := NewStore(Options{Layout: LayoutAt(t.TempDir())})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	checked := 0
	for _, identityReport := range sourceReport.Identities {
		for _, group := range identityReport.Groups {
			checked++
			if got := store.ladderStep(group.BackoffStep).String(); got != group.BackoffStepDuration {
				t.Errorf("step %d of this ladder is %s; the source rendered %s for the same step", group.BackoffStep, got, group.BackoffStepDuration)
			}
		}
	}
	if checked == 0 {
		t.Fatal("the source's report carried no group; the ladder was not compared to anything")
	}
	if len(sourceReport.Ladder) == 0 {
		t.Fatal("the source's report carried no ladder")
	}
	store2, err := NewStore(Options{Layout: LayoutAt(t.TempDir())})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	var ours []string
	for _, step := range store2.Ladder() {
		ours = append(ours, step.String())
	}
	if strings.Join(ours, ",") != strings.Join(sourceReport.Ladder, ",") {
		t.Errorf("the full shipped ladder differs from the source's.\nours:   %v\nsource: %v", ours, sourceReport.Ladder)
	}
}

// --- helpers -----------------------------------------------------------------

// verdictDetails joins a verdict's observation details, so an assertion about
// what a verdict SAYS can be written against one string.
func verdictDetails(verdict vendorplugin.Availability) string {
	var lines []string
	for _, observation := range verdict.Observed {
		lines = append(lines, observation.Source+": "+observation.Detail)
	}
	for _, failure := range verdict.Failures {
		lines = append(lines, failure.Source+" FAILED: "+failure.Reason)
	}
	return strings.Join(lines, "\n")
}

func stateFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), stateFileSuffix) {
			out = append(out, entry.Name())
		}
	}
	return out
}

func readRawFixture(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading fixture %s/%s: %v", dir, name, err)
	}
	return data
}

// assertReencodesTo re-encodes a decoded fixture with the write path's own
// encoder and compares bytes.
func assertReencodesTo(t *testing.T, payload any, original []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "roundtrip.json")
	if err := writeJSONAtomic(path, payload); err != nil {
		t.Fatalf("writeJSONAtomic: %v", err)
	}
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the re-encoded file: %v", err)
	}
	if string(rewritten) != string(original) {
		t.Errorf("the re-encoded bytes differ from the ones on disk; the on-disk schema moved.\n--- on disk ---\n%s\n--- re-encoded ---\n%s", original, rewritten)
	}
}

// loadFixtureState copies one captured state file into a scratch layout and
// loads it through the real read path at the given instant.
//
// The copy is deliberate: the read path may quarantine, and a test that
// quarantined a committed fixture would delete the evidence it was checking.
func loadFixtureState(t *testing.T, dir, identityKey, provider string, now time.Time) (IdentityState, *Store) {
	t.Helper()
	root := t.TempDir()
	layout := LayoutAt(root)
	if err := os.MkdirAll(layout.Root, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	name := identityKey + stateFileSuffix
	if err := os.WriteFile(layout.StateFile(identityKey), readRawFixture(t, dir, name), 0o600); err != nil {
		t.Fatalf("planting the fixture: %v", err)
	}
	store, err := NewStore(Options{Layout: layout, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// The identity is reconstructed from the KEY the fixture already carries,
	// not from a home path: the fixture's home may not exist on this machine,
	// and the point is to read the file that is there.
	identity := Identity{Key: identityKey, Provider: provider}
	return store.LoadIdentityState(identity), store
}

func groupNames(state IdentityState) []string {
	var names []string
	for name := range state.Groups {
		names = append(names, name)
	}
	return names
}

func assertInstantsAgree(t *testing.T, group, field string, ours, theirs *time.Time) {
	t.Helper()
	if (ours == nil) != (theirs == nil) {
		t.Errorf("group %q: %s presence disagrees (ours=%v source=%v)", group, field, ours != nil, theirs != nil)
		return
	}
	if ours != nil && !ours.Equal(*theirs) {
		t.Errorf("group %q: %s differs (ours=%s source=%s)", group, field, ours, theirs)
	}
}
