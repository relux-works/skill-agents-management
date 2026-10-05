package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file is the round-1 F2 regression: an absent per-launch temp dir
// keeps the child environment byte for byte, proved as a full ORDERED
// Plan.Env comparison against a fixture captured from the pre-change
// behaviour at base 4acdd98 — not a self-comparison, and with no set
// conversion that could hide a reorder.
//
// The file is deliberately base-compatible: it mentions no TempDir member
// (absence at base is the zero request) and spells TMPDIR literally, so
// the capture run compiles the same file against the base tree. See
// TestAbsentTempDirMatchesBaseParityFixture.

// absentParityPlaceholders are the machine-local directories one case
// masks out of Plan.Env before comparing: the work, stub-binary and home
// slots the case allocated. Every other byte compares literally, in order.
const (
	absentParityWorkPlaceholder = "<WORK>"
	absentParityBinPlaceholder  = "<BIN>"
	absentParityHomePlaceholder = "<HOME>"
)

// absentParityFixtureName is the committed baseline, captured from base
// 4acdd98 behaviour (see the test). It lives beside the test so the
// comparison reads no ambient state.
const absentParityFixtureName = "tempdir_absent_parity.json"

// absentParityCaptureEnv, when set to a file path, makes the test write
// the fixture from the tree under test instead of comparing against it.
// The committed fixture was written by running the base tree this way.
const absentParityCaptureEnv = "ABSENT_PARITY_CAPTURE"

// absentParityCase is one row of the cross product the fixture pins:
// parent environment shape, launch mode, and managed scope.
type absentParityCase struct {
	Name    string   `json:"name"`
	Parent  []string `json:"parent"`
	Mode    string   `json:"mode"`
	Managed bool     `json:"managed"`
	WantEnv []string `json:"wantEnv"`
}

type absentParityParent struct {
	name    string
	entries []string
}

var absentParityParents = []absentParityParent{
	{name: "plain", entries: []string{"KEEP=yes", "OTHER=no"}},
	{name: "inherited-tmpdir", entries: []string{"KEEP=yes", "TMPDIR=/fixture/tmp", "OTHER=no"}},
	{name: "duplicates", entries: []string{"TMPDIR=/fixture/a", "KEEP=yes", "TMPDIR=/fixture/b", "KEEP=yes"}},
}

var absentParityModes = []struct {
	name string
	mode agentic.LaunchMode
}{
	{name: "exec", mode: agentic.LaunchModeExec},
	{name: "dry-run", mode: agentic.LaunchModeDryRun},
	{name: "managed-session", mode: agentic.LaunchModeManagedSession},
}

// maskAbsentParityDirs rewrites every occurrence of the case's three
// machine-local directories to their placeholders. Order is by descending
// length so a slot nested under another still masks exactly.
func maskAbsentParityDirs(entries []string, workDir, binDir, homeDir string) []string {
	slots := []struct {
		dir         string
		placeholder string
	}{
		{workDir, absentParityWorkPlaceholder},
		{binDir, absentParityBinPlaceholder},
		{homeDir, absentParityHomePlaceholder},
	}
	sort.Slice(slots, func(i, j int) bool { return len(slots[i].dir) > len(slots[j].dir) })
	masked := make([]string, len(entries))
	for i, entry := range entries {
		for _, slot := range slots {
			entry = strings.ReplaceAll(entry, slot.dir, slot.placeholder)
		}
		masked[i] = entry
	}
	return masked
}

// buildAbsentParityPlan drives production for one case: a real registry
// and agentic.BuildPlan over an absent-TempDir request. It returns the
// masked child environment in plan order.
func buildAbsentParityPlan(t *testing.T, parent absentParityParent, mode agentic.LaunchMode, managed bool) (maskedParent, maskedEnv []string) {
	t.Helper()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	homeDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	template := append(append([]string(nil), parent.entries...), "PATH="+absentParityBinPlaceholder, "HOME="+absentParityHomePlaceholder)
	real := make([]string, len(template))
	for i, entry := range template {
		entry = strings.ReplaceAll(entry, absentParityBinPlaceholder, binDir)
		entry = strings.ReplaceAll(entry, absentParityHomePlaceholder, homeDir)
		real[i] = entry
	}
	maskedParent = template

	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, "absent parity prompt")
	req.Env = real
	if managed {
		req.Network = verifiedNetworkCarrier()
	}
	plan := buildParityPlan(t, New(), req, mode)
	maskedEnv = maskAbsentParityDirs(plan.Env, workDir, binDir, homeDir)
	return maskedParent, maskedEnv
}

// absentParityCaseName joins the cross-product coordinates into the stable
// case name both the capture and the comparison use.
func absentParityCaseName(parent, mode string, managed bool) string {
	scope := "unmanaged"
	if managed {
		scope = "managed"
	}
	return parent + "/" + mode + "/" + scope
}

// TestAbsentTempDirMatchesBaseParityFixture compares the complete ordered
// Plan.Env — every entry, every duplicate, in position — against the
// committed base-4acdd98 fixture, over the parent-shape × mode ×
// managed-scope cross product (18 cases). The request carries no TempDir
// (nil at this revision; the field does not exist at base), so any drift
// the feature introduced into the absent path fails here, including a
// reorder no set-based comparison could see.
func TestAbsentTempDirMatchesBaseParityFixture(t *testing.T) {
	var captured []absentParityCase
	for _, parent := range absentParityParents {
		for _, mode := range absentParityModes {
			for _, managed := range []bool{false, true} {
				maskedParent, maskedEnv := buildAbsentParityPlan(t, parent, mode.mode, managed)
				captured = append(captured, absentParityCase{
					Name:    absentParityCaseName(parent.name, mode.name, managed),
					Parent:  maskedParent,
					Mode:    mode.name,
					Managed: managed,
					WantEnv: maskedEnv,
				})
			}
		}
	}
	if capturePath := os.Getenv(absentParityCaptureEnv); capturePath != "" {
		body, err := json.MarshalIndent(captured, "", "  ")
		if err != nil {
			t.Fatalf("encoding the absent-parity fixture: %v", err)
		}
		if err := os.WriteFile(capturePath, append(body, '\n'), 0o644); err != nil {
			t.Fatalf("writing the absent-parity fixture: %v", err)
		}
		t.Logf("captured %d absent-parity cases to %s", len(captured), capturePath)
		return
	}
	raw, err := os.ReadFile(filepath.Join("testdata", absentParityFixtureName))
	if err != nil {
		t.Fatalf("reading the absent-parity fixture: %v", err)
	}
	var want []absentParityCase
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decoding the absent-parity fixture: %v", err)
	}
	if len(want) != len(captured) {
		t.Fatalf("absent-parity fixture holds %d cases, the cross product builds %d; recapture from base, never by hand", len(want), len(captured))
	}
	for i := range want {
		if want[i].Name != captured[i].Name || want[i].Mode != captured[i].Mode || want[i].Managed != captured[i].Managed || !reflect.DeepEqual(want[i].Parent, captured[i].Parent) {
			t.Fatalf("absent-parity case %d is %q, want %q; the cross product moved under the fixture", i, captured[i].Name, want[i].Name)
		}
		if !reflect.DeepEqual(captured[i].WantEnv, want[i].WantEnv) {
			t.Errorf("absent-field Plan.Env differs from the base fixture in case %q:\n got: %q\nwant: %q", captured[i].Name, captured[i].WantEnv, want[i].WantEnv)
		}
	}
}
