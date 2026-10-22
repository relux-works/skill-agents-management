package localmodels

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Engine weights flow through explicit and inherited references independently
// of the operator pin; adding Curator-only metadata does not conflict with an
// otherwise identical legacy source. An unrelated engine is never substituted.
func TestCuratorEngineExportsWeightsAndPath(t *testing.T) {
	const declared = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const pinned = "gguf-sha256:2222222222222222222222222222222222222222222222222222222222222222"
	for _, mode := range []string{"explicit", "inherited", "model-only", "without-weights", "with-legacy"} {
		t.Run(mode, func(t *testing.T) {
			dir, project := t.TempDir(), t.TempDir()
			body := curatorPiRuntimes(project)
			engineLine := "engine = { plugin = \"mlx\", name = \"local-qwen\" }\n"
			if mode == "inherited" {
				at := strings.LastIndex(body, engineLine)
				body = body[:at] + strings.Replace(body[at:], engineLine, "", 1)
			} else if mode == "model-only" {
				body = strings.Replace(body, engineLine, "", 1)
			}
			wantPin := ""
			if mode == "explicit" {
				wantPin = pinned
				body = strings.Replace(body, "effort_support = \"none\"\n", "effort_support = \"none\"\nexpected_weights_id = \""+pinned+"\"\n", 1)
			}
			engines, wantWeights := "schema_version = 1\n"+curatorEnginesMLX, declared
			if mode == "without-weights" {
				wantWeights = ""
			} else {
				engines += "weights = { id = \"" + declared + "\" }\n"
			}
			engines += "\n[engines.unrelated]\nengine = \"mlx\"\nweights = { id = \"" + pinned + "\" }\n"
			writeCuratorFile(t, dir, runtimesFileName, body)
			writeCuratorFile(t, dir, enginesFileName, engines)
			legacy := filepath.Join(t.TempDir(), "legacy.toml")
			if mode == "with-legacy" {
				if err := os.WriteFile(legacy, []byte(legacyPiTOML(project)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, _, err := LoadAt(dir, legacy)
			if err != nil {
				t.Fatal(err)
			}
			model := cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"]
			if model.WeightsID != wantWeights || model.ExpectedWeightsID != wantPin {
				t.Fatalf("weights = %q, pin = %q; want %q, %q", model.WeightsID, model.ExpectedWeightsID, wantWeights, wantPin)
			}
			wantPath, err := filepath.EvalSymlinks(filepath.Join(dir, enginesFileName))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.EnginesConfig != wantPath {
				t.Fatalf("EnginesConfig = %q, want loaded catalog", cfg.EnginesConfig)
			}
			result := resolveWithPaths(dir, legacy)
			if result.Err != nil || result.Absent || !reflect.DeepEqual(result.Config, cfg) {
				t.Fatalf("ConfigResult dropped resolved exports: %#v", result)
			}
		})
	}
}

// Relative catalog directories expose the canonical file actually read, while
// a missing engine catalog and legacy fallback leave the new exports absent.
func TestCuratorEngineExportsPathAndAbsence(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	t.Chdir(dir)
	legacy := filepath.Join(t.TempDir(), "legacy.toml")
	body := curatorPiRuntimes(project)
	writeCuratorFile(t, dir, runtimesFileName, body)
	writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX)
	wantPath, err := filepath.EvalSymlinks(filepath.Join(dir, enginesFileName))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadAt(".", legacy)
	if err != nil || cfg.EnginesConfig != wantPath {
		t.Fatalf("relative path resolution: config = %#v, err = %v", cfg, err)
	}
	if err := os.Remove(filepath.Join(dir, enginesFileName)); err != nil {
		t.Fatal(err)
	}
	noRefs := strings.ReplaceAll(body, "engine = { plugin = \"mlx\", name = \"local-qwen\" }\n", "")
	writeCuratorFile(t, dir, runtimesFileName, noRefs)
	cfg, _, err = LoadAt(dir, legacy)
	if err != nil || cfg.EnginesConfig != "" {
		t.Fatalf("absent engine catalog: config = %#v, err = %v", cfg, err)
	}
	if err := os.WriteFile(legacy, []byte(legacyPiTOML(project)), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := ParseConfig([]byte(legacyPiTOML(project)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err = LoadAt(filepath.Join(dir, "missing"), legacy)
	if err != nil || !reflect.DeepEqual(cfg, want) {
		t.Fatalf("legacy changed: config = %#v, want %#v, err = %v", cfg, want, err)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil || strings.Contains(string(encoded), "\"WeightsID\"") || strings.Contains(string(encoded), "\"EnginesConfig\"") {
		t.Fatalf("absent metadata changed serialized legacy shape: %s, err = %v", encoded, err)
	}
	// A failed engine read/validation never exports a partial catalog path or
	// weights identity, even when the runtime rows themselves are valid.
	writeCuratorFile(t, dir, enginesFileName, curatorEnginesMLX+"weights = { id = \"invalid\" }\n")
	cfg, warning, err := LoadAt(dir, legacy)
	requireRefusal(t, err, ErrMalformed, RefusalMalformed)
	if warning != nil || !reflect.DeepEqual(cfg, Config{}) {
		t.Fatalf("failed resolution returned partial metadata: %#v, warning = %v", cfg, warning)
	}
}
