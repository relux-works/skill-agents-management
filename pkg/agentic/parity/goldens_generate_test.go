package parity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestWriteGoldensFromCapture regenerates testdata/goldens from one run of the
// SOURCE repository's capture harness.
//
// It is gated the same way the source gates its own capture — skipped unless
// PARITY_GOLDEN_CAPTURE_IN is set — so the ordinary suite never rewrites the
// fixtures it is supposed to be checked against. .scripts/capture-parity-goldens.sh
// is what sets the three variables; running this test by hand is possible and
// deliberately inconvenient.
//
// It is also the production call site for GoldensFromCapture and
// DiscoverSubstitutions, which exist for no other caller.
func TestWriteGoldensFromCapture(t *testing.T) {
	capturePath := os.Getenv("PARITY_GOLDEN_CAPTURE_IN")
	if capturePath == "" {
		t.Skip("set PARITY_GOLDEN_CAPTURE_IN (and PARITY_GOLDEN_META_IN, PARITY_GOLDEN_OUT) to regenerate the goldens; .scripts/capture-parity-goldens.sh does it properly")
	}
	metaPath := os.Getenv("PARITY_GOLDEN_META_IN")
	outDir := os.Getenv("PARITY_GOLDEN_OUT")
	if metaPath == "" || outDir == "" {
		t.Fatal("PARITY_GOLDEN_CAPTURE_IN is set but PARITY_GOLDEN_META_IN or PARITY_GOLDEN_OUT is not; regenerating from a capture whose provenance was not supplied is exactly the fixture this package refuses to write")
	}

	var raw map[string]Snapshot
	readJSON(t, capturePath, &raw)

	// The on-disk shape of GenerationMeta, kept as its own type so the script
	// that writes it and the package that reads it share one spelling.
	var meta struct {
		SourceRepo        string   `json:"source_repo"`
		SourceCommit      string   `json:"source_commit"`
		SourceHarness     string   `json:"source_harness"`
		CaptureEnvVar     string   `json:"capture_env_var"`
		CapturedBy        string   `json:"captured_by"`
		ParentEnv         []string `json:"parent_env"`
		ParentEnvOmitted  []string `json:"parent_env_omitted"`
		TempRoot          string   `json:"temp_root"`
		StubBinDir        string   `json:"stub_bin_dir"`
		ForbiddenLiterals []string `json:"forbidden_literals"`
	}
	readJSON(t, metaPath, &meta)

	parentEnv := append([]string(nil), meta.ParentEnv...)
	sort.Strings(parentEnv)

	goldens, err := GoldensFromCapture(raw, GenerationMeta{
		SourceRepo:       meta.SourceRepo,
		SourceCommit:     meta.SourceCommit,
		SourceHarness:    meta.SourceHarness,
		CaptureEnvVar:    meta.CaptureEnvVar,
		CapturedBy:       meta.CapturedBy,
		ParentEnv:        parentEnv,
		ParentEnvOmitted: meta.ParentEnvOmitted,
		Layout: CaptureLayout{
			TempRoot:          meta.TempRoot,
			StubBinDir:        meta.StubBinDir,
			ForbiddenLiterals: meta.ForbiddenLiterals,
		},
	})
	if err != nil {
		t.Fatalf("GoldensFromCapture: %v", err)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", outDir, err)
	}
	// Every fixture the capture did not produce is removed rather than left
	// behind. A stale golden for a combination the source no longer captures is
	// a contract nobody is maintaining, and the port that proves itself against
	// it learns nothing.
	stale, err := filepath.Glob(filepath.Join(outDir, "*.json"))
	if err != nil {
		t.Fatalf("Glob(%s): %v", outDir, err)
	}
	written := map[string]bool{}
	for _, g := range goldens {
		path := filepath.Join(outDir, g.FileName())
		data, err := json.MarshalIndent(g, "", "  ")
		if err != nil {
			t.Fatalf("marshal %s: %v", g.ID, err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
		written[path] = true
		t.Logf("wrote %s (%s, %s)", g.FileName(), g.ID, g.LaunchMode)
	}
	for _, path := range stale {
		if !written[path] {
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove stale %s: %v", path, err)
			}
			t.Logf("removed stale %s", filepath.Base(path))
		}
	}
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("Unmarshal(%s): %v", path, err)
	}
}
