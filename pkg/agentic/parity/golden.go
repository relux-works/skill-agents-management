package parity

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// goldenFS embeds the fixtures so any package in this module can prove itself
// against them without knowing where the checkout root is.
//
// They live under testdata/ on purpose, and the consequence is worth stating
// rather than discovering: the single-source guard's scan scope excludes
// testdata (singlesource_scanscope_test.go), so nothing under it is scanned for
// a reintroduced system binding. For these fixtures that is correct — they are
// JSON captured by another repository, they declare no binding, and the guard
// reports on what this module's Go build compiles. The rule it costs is the
// general one: a .go file placed under this directory would be invisible to the
// guard, so no Go source belongs here.
//
//go:embed testdata/goldens/*.json
var goldenFS embed.FS

const goldensDir = "testdata/goldens"

// GoldenSchemaVersion is the version every fixture in this package carries. It
// exists so a fixture written against an older shape fails loudly instead of
// unmarshalling into zero values that compare equal to nothing.
const GoldenSchemaVersion = 1

// Capture is the provenance of one golden: where it came from, at which commit,
// and under exactly what parent environment.
//
// SourceCommit is the field that makes a golden able to settle an argument. A
// port that disagrees with a golden is either a wrong port or a stale golden,
// and without the commit there is no way to tell which — the fixture becomes a
// number somebody once believed.
type Capture struct {
	// SourceRepo and SourceCommit identify the checkout the capture ran in.
	SourceRepo   string `json:"source_repo"`
	SourceCommit string `json:"source_commit"`
	// SourceHarness is the test that produced it, and CaptureEnvVar the
	// variable that un-skips it, so the capture is repeatable from the golden
	// alone.
	SourceHarness string `json:"source_harness"`
	CaptureEnvVar string `json:"capture_env_var"`
	// CapturedBy is the task this repository ran the capture under.
	CapturedBy string `json:"captured_by"`
	// ParentEnv is the environment the capture process ran with, minus
	// ParentEnvOmitted. EnvAdded and EnvRemoved are a diff against exactly
	// this, so a port test that wants a comparable diff feeds this value to
	// LaunchRequest.Env rather than inventing a baseline.
	//
	// It is pinned and synthetic rather than a developer's real environment:
	// the source harness diffs against os.Environ(), so an unpinned capture
	// would record whichever session variables the operator's shell happened
	// to carry, and a key the plugin strips would be provably stripped only on
	// the machine that happened to set it.
	ParentEnv []string `json:"parent_env"`
	// ParentEnvOmitted names the variables the capture process needed but that
	// are not in ParentEnv, each with the reason. They are all pass-through:
	// no filter under capture touches them, so they appear in neither EnvAdded
	// nor EnvRemoved and omitting them changes no diff — while keeping them
	// would write this machine's home directory into a fixture.
	ParentEnvOmitted []string `json:"parent_env_omitted"`
	// MaskRules are the rules applied, by name, matching MaskRules().
	MaskRules []string `json:"mask_rules"`
	// Placeholders are the mask placeholders this golden actually contains, so
	// a port test can see how many temp slots it has to reproduce without
	// grepping the fixture.
	Placeholders []string `json:"placeholders"`
}

// Golden is one captured (system, mode) launch surface plus its provenance.
type Golden struct {
	SchemaVersion int `json:"schema_version"`
	// ID is the source harness's own subtest key, verbatim — "claude/prompt-mode",
	// "codex/exec-native-shim". Keeping the source's spelling is what lets a
	// reader match a fixture to a line in parity_capture_test.go without a
	// translation table.
	ID string `json:"id"`
	// System is the source's agent identifier and Case its variant within a
	// mode. LaunchMode is the agentic.LaunchMode the case belongs to; several
	// cases map onto one mode, which is why Case exists at all.
	System     string  `json:"system"`
	Case       string  `json:"case"`
	LaunchMode string  `json:"launch_mode"`
	Capture    Capture `json:"capture"`
	// Surface is the masked launch surface. It is what Compare compares.
	Surface Snapshot `json:"surface"`
}

// FileName is the fixture file this golden is stored as.
func (g Golden) FileName() string {
	return strings.ReplaceAll(g.ID, "/", "_") + ".json"
}

// Load returns one golden by its source subtest key.
func Load(id string) (Golden, error) {
	all, err := LoadAll()
	if err != nil {
		return Golden{}, err
	}
	g, ok := all[id]
	if !ok {
		return Golden{}, fmt.Errorf("parity: no golden for %q; captured ids are %v", id, sortedIDs(all))
	}
	return g, nil
}

// LoadAll returns every embedded golden, keyed by ID.
//
// A fixture that does not parse, or whose declared ID disagrees with its file
// name, is an error rather than a skipped entry. A parity suite that silently
// covers one combination fewer than it believes is the failure mode this whole
// package exists to prevent, and a malformed read is not an absence.
func LoadAll() (map[string]Golden, error) {
	return loadDir(goldenFS, goldensDir)
}

// loadDir is LoadAll over any filesystem, so the loader's REFUSALS can be
// driven from a test directory. Proving them against the embedded fixtures is
// impossible by construction: those fixtures are valid, and a refusal path that
// nothing malformed ever reaches is a refusal nobody has seen work.
func loadDir(fsys fs.FS, dir string) (map[string]Golden, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("parity: reading goldens in %s: %w", dir, err)
	}
	out := make(map[string]Golden, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parity: reading golden %s: %w", entry.Name(), err)
		}
		var g Golden
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&g); err != nil {
			return nil, fmt.Errorf("parity: decoding golden %s: %w", entry.Name(), err)
		}
		if g.SchemaVersion != GoldenSchemaVersion {
			return nil, fmt.Errorf("parity: golden %s declares schema version %d, this package reads %d",
				entry.Name(), g.SchemaVersion, GoldenSchemaVersion)
		}
		if g.FileName() != entry.Name() {
			return nil, fmt.Errorf("parity: golden %s declares id %q, which belongs in %s",
				entry.Name(), g.ID, g.FileName())
		}
		if _, duplicate := out[g.ID]; duplicate {
			return nil, fmt.Errorf("parity: two goldens claim id %q", g.ID)
		}
		out[g.ID] = g
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("parity: no goldens found in %s", dir)
	}
	return out, nil
}

func sortedIDs(all map[string]Golden) []string {
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
