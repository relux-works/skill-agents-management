package parity

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file turns one run of the SOURCE repository's capture harness into the
// fixtures under testdata/goldens. It reads that harness's output; it does not
// reproduce it. Nothing here builds a command, resolves a binary or filters an
// environment, and nothing here may ever start to: a golden produced by code
// that lives beside the port proves only that the port agrees with itself.
//
// The production call site is TestWriteGoldensFromCapture in
// goldens_generate_test.go, driven by .scripts/capture-parity-goldens.sh.

// CaptureLayout is where one capture run put its machine-local directories.
// It is the bridge between the raw harness output and Substitutions: the
// harness records absolute paths, and this says which of them are noise.
type CaptureLayout struct {
	// TempRoot is TMPDIR for the capture process — the directory
	// testing.T.TempDir allocated beneath.
	TempRoot string
	// StubBinDir is the directory of stub executables the capture put on PATH.
	StubBinDir string
	// ForbiddenLiterals are machine-local strings that must not survive into a
	// fixture even though no rule masks them — the operator's home directory,
	// the source checkout path. A hit is an error, not a silent write: a
	// golden carrying one is a fixture that only reproduces on one machine.
	ForbiddenLiterals []string
}

// tempSlotPattern matches one testing.T.TempDir slot beneath the capture's
// TMPDIR. The source harness allocates them as
// <TMPDIR>/TestCaptureLaunchSurface<subtest><random>/<NNN>, so the random
// component is what varies run to run and NNN is the allocation order that
// does not.
var tempSlotPattern = regexp.MustCompile(`^(.*/TestCaptureLaunchSurface[^/]*)/(\d{3})$`)

// DiscoverSubstitutions finds the machine-local directories one captured
// snapshot actually references.
//
// It is discovery, not masking: it produces the Substitutions that the SHARED
// Mask then applies, so the goldens and every later comparison go through one
// masking implementation rather than two that agree until they do not.
//
// A slot number appearing under two different parent directories within one
// case means the layout assumption above is wrong for that case, and it is
// reported rather than guessed past.
func DiscoverSubstitutions(snap Snapshot, layout CaptureLayout) (Substitutions, error) {
	parents := map[int]string{}
	highest := 0
	for _, candidate := range snapshotPathCandidates(snap, layout.TempRoot) {
		match := tempSlotPattern.FindStringSubmatch(candidate)
		if match == nil {
			continue
		}
		slot, err := strconv.Atoi(match[2])
		if err != nil || slot < 1 {
			return Substitutions{}, fmt.Errorf("parity: temp slot %q in %q is not a positive index", match[2], candidate)
		}
		if existing, seen := parents[slot]; seen && existing != match[1] {
			return Substitutions{}, fmt.Errorf("parity: temp slot %03d appears under two directories, %q and %q; the capture layout this discovery assumes does not hold", slot, existing, match[1])
		}
		parents[slot] = match[1]
		if slot > highest {
			highest = slot
		}
	}
	subs := Substitutions{TempSlots: make([]string, highest)}
	for slot, parent := range parents {
		subs.TempSlots[slot-1] = parent + "/" + fmt.Sprintf("%03d", slot)
	}
	if layout.StubBinDir != "" && snapshotMentions(snap, layout.StubBinDir) {
		subs.StubBinDir = layout.StubBinDir
	}
	return subs, nil
}

// snapshotPathCandidates returns every longest prefix of every string in snap
// that could be a temp slot directory. Paths appear both bare (a work
// directory) and with a suffix (a binary inside a slot, a .task-board beneath
// one), so each candidate is walked up from its full length to the temp root.
func snapshotPathCandidates(snap Snapshot, tempRoot string) []string {
	if tempRoot == "" {
		return nil
	}
	var out []string
	for _, field := range snapshotStrings(snap) {
		index := 0
		for {
			found := strings.Index(field[index:], tempRoot)
			if found < 0 {
				break
			}
			start := index + found
			// The path runs to the first character that cannot be in one. The
			// harness writes shell-safe paths, so whitespace and quotes end it.
			end := start
			for end < len(field) && !strings.ContainsRune(" \t\n\r\"'", rune(field[end])) {
				end++
			}
			candidate := field[start:end]
			for candidate != tempRoot && candidate != "" {
				out = append(out, candidate)
				cut := strings.LastIndex(candidate, "/")
				if cut < 0 {
					break
				}
				candidate = candidate[:cut]
			}
			index = end
		}
	}
	return out
}

func snapshotStrings(snap Snapshot) []string {
	out := []string{snap.Binary, snap.StdinData, snap.Error}
	out = append(out, snap.Args...)
	out = append(out, snap.EnvAdded...)
	out = append(out, snap.EnvRemoved...)
	return out
}

func snapshotMentions(snap Snapshot, literal string) bool {
	for _, field := range snapshotStrings(snap) {
		if strings.Contains(field, literal) {
			return true
		}
	}
	return false
}

// GenerationMeta is the provenance of one capture run, recorded into every
// golden it produces.
type GenerationMeta struct {
	SourceRepo       string
	SourceCommit     string
	SourceHarness    string
	CaptureEnvVar    string
	CapturedBy       string
	ParentEnv        []string
	ParentEnvOmitted []string
	Layout           CaptureLayout
}

// GoldensFromCapture converts the raw harness output into fixtures.
//
// Every id must split into a system and a case, the case must agree with the
// stdin marker about which launch mode it is, and nothing machine-local may
// survive masking. Each of those is an error rather than a best effort, because
// the only thing worse than a missing golden is a wrong one that a port then
// proves itself against.
func GoldensFromCapture(raw map[string]Snapshot, meta GenerationMeta) ([]Golden, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("parity: the capture produced no combinations at all")
	}
	if strings.TrimSpace(meta.SourceCommit) == "" {
		return nil, fmt.Errorf("parity: refusing to write goldens with no source commit; a fixture whose provenance is unknown cannot settle a dispute")
	}
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Golden, 0, len(ids))
	for _, id := range ids {
		system, kase, ok := strings.Cut(id, "/")
		if !ok || system == "" || kase == "" {
			return nil, fmt.Errorf("parity: capture key %q is not <system>/<case>", id)
		}
		snap := raw[id]
		mode, err := launchModeForCase(id, kase, snap)
		if err != nil {
			return nil, err
		}
		subs, err := DiscoverSubstitutions(snap, meta.Layout)
		if err != nil {
			return nil, fmt.Errorf("parity: %s: %w", id, err)
		}
		masked := Mask(snap, subs)
		if err := assertNothingMachineLocal(id, masked, meta.Layout); err != nil {
			return nil, err
		}
		out = append(out, Golden{
			SchemaVersion: GoldenSchemaVersion,
			ID:            id,
			System:        system,
			Case:          kase,
			LaunchMode:    mode.String(),
			Capture: Capture{
				SourceRepo:       meta.SourceRepo,
				SourceCommit:     meta.SourceCommit,
				SourceHarness:    meta.SourceHarness,
				CaptureEnvVar:    meta.CaptureEnvVar,
				CapturedBy:       meta.CapturedBy,
				ParentEnv:        append([]string(nil), meta.ParentEnv...),
				ParentEnvOmitted: append([]string(nil), meta.ParentEnvOmitted...),
				MaskRules:        maskRuleNames(),
				Placeholders:     placeholdersIn(masked),
			},
			Surface: masked,
		})
	}
	return out, nil
}

// launchModeForCase maps a source case onto an agentic launch mode, and
// cross-checks the mapping against the stdin marker the harness recorded.
//
// The cross-check is the point. The name-based rule alone would be a guess; the
// source's two capture paths record different stdin markers (its dry-run path
// calls BuildArgs and observes no command at all), so the marker is independent
// evidence of which path ran. Disagreement means the source grew a case shape
// this mapping does not know, and that has to stop a generation rather than
// produce a mislabelled fixture.
func launchModeForCase(id, kase string, snap Snapshot) (agentic.LaunchMode, error) {
	mode := agentic.LaunchModeExec
	if kase == "dry-run" {
		mode = agentic.LaunchModeDryRun
	}
	dryRunMarker := snap.StdinKind == StdinDryRun
	if (mode == agentic.LaunchModeDryRun) != dryRunMarker {
		return 0, fmt.Errorf("parity: %s: case name maps to %s but the capture recorded stdin_kind %q; the source grew a case shape this mapping does not know",
			id, mode, snap.StdinKind)
	}
	return mode, nil
}

func assertNothingMachineLocal(id string, masked Snapshot, layout CaptureLayout) error {
	forbidden := append([]string(nil), layout.ForbiddenLiterals...)
	if layout.TempRoot != "" {
		forbidden = append(forbidden, layout.TempRoot)
	}
	if layout.StubBinDir != "" {
		forbidden = append(forbidden, layout.StubBinDir)
	}
	for _, literal := range forbidden {
		if literal == "" {
			continue
		}
		if snapshotMentions(masked, literal) {
			return fmt.Errorf("parity: %s: %q survived masking; a fixture carrying it reproduces on one machine only", id, literal)
		}
	}
	return nil
}

func maskRuleNames() []string {
	names := make([]string, 0, len(maskRules))
	for _, rule := range maskRules {
		names = append(names, rule.Name)
	}
	return names
}

func placeholdersIn(snap Snapshot) []string {
	seen := map[string]bool{}
	for _, field := range snapshotStrings(snap) {
		if strings.Contains(field, PlaceholderStubBinDir) {
			seen[PlaceholderStubBinDir] = true
		}
		for slot := 1; slot <= 16; slot++ {
			placeholder := TempSlotPlaceholder(slot)
			if strings.Contains(field, placeholder) {
				seen[placeholder] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for placeholder := range seen {
		out = append(out, placeholder)
	}
	sort.Strings(out)
	return out
}
