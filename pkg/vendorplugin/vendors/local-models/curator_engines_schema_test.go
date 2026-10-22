package localmodels

import (
	"path/filepath"
	"reflect"
	"testing"
)

// Engines schema versions other than TOML integer 1 refuse without returning
// partial entries, even when the engine body itself is valid.
func TestCuratorEnginesSchemaVersionRefusesMalformed(t *testing.T) {
	for _, header := range []string{
		"schema_version = 0\n",
		"schema_version = 2\n",
		"schema_version = -1\n",
		"schema_version = 1.0\n",
		"schema_version = \"1\"\n",
		"schema_version = true\n",
		"schema_version = {}\n",
		"[schema_version]\nvalue = 1\n",
		"schema_version = [1]\n",
	} {
		t.Run(header, func(t *testing.T) {
			entries, err := parseCuratorEngines([]byte(header + curatorEnginesMLX))
			refusal := requireRefusal(t, err, ErrMalformed, RefusalMalformed)
			if refusal.File != enginesFileName {
				t.Fatalf("refusal file = %q, want %q", refusal.File, enginesFileName)
			}
			if entries != nil {
				t.Fatalf("refused catalog returned partial entries: %#v", entries)
			}
		})
	}
}

// Both legacy absence and explicit version 1 yield the independently specified
// engine entries, including weights, and resolve the same runtime via LoadAt.
func TestCuratorEnginesSchemaVersionAccepted(t *testing.T) {
	const weightsID = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"
	body := curatorEnginesMLX + "\n[engines.local-gguf]\nengine = \"llama-cpp\"\nweights = { id = \"" + weightsID + "\" }\n"
	want := map[string]curatorEngineEntry{
		"local-qwen": {Kind: "mlx"},
		"local-gguf": {Kind: "llama-cpp", WeightsID: weightsID},
	}
	dir := t.TempDir()
	writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(t.TempDir()))
	legacy := filepath.Join(t.TempDir(), "missing.toml")
	var baseline Config
	for _, header := range []string{"", "schema_version = 1\n"} {
		entries, err := parseCuratorEngines([]byte(header + body))
		if err != nil || !reflect.DeepEqual(entries, want) {
			t.Fatalf("header %q: entries = %#v, err = %v; want %#v", header, entries, err, want)
		}
		writeCuratorFile(t, dir, enginesFileName, header+body)
		cfg, warning, err := LoadAt(dir, legacy)
		if err != nil || warning != nil || len(cfg.Runtimes) != 1 {
			t.Fatalf("header %q: LoadAt = %#v, warning = %v, err = %v", header, cfg, warning, err)
		}
		if header == "" {
			baseline = cfg
		} else if !reflect.DeepEqual(cfg, baseline) {
			t.Fatalf("version 1 changed resolved config: got %#v, want %#v", cfg, baseline)
		}
	}
}

// A valid version does not relax unknown-field or credential-shaped top-level
// refusals; both retain their legacy kind and sanitized source-file diagnostic.
func TestCuratorEnginesSchemaVersionPreservesUnknownFieldRefusals(t *testing.T) {
	for _, header := range []string{"", "schema_version = 1\n"} {
		for _, test := range []struct {
			field string
			kind  RefusalKind
			want  error
		}{
			{"future_table = {}\n", RefusalMalformed, ErrMalformed},
			{"api_key = \"SYNTHETIC-SCHEMA-SECRET\"\n", RefusalUnsupported, ErrUnsupported},
			{"credential = { source = \"env-name\", name = \"MODEL_TOKEN\" }\n", RefusalUnsupported, ErrUnsupported},
		} {
			t.Run(header+test.field, func(t *testing.T) {
				entries, err := parseCuratorEngines([]byte(header + test.field + curatorEnginesMLX))
				refusal := requireRefusal(t, err, test.want, test.kind)
				if refusal.File != enginesFileName || entries != nil {
					t.Fatalf("refused catalog: file = %q, entries = %#v", refusal.File, entries)
				}
				requireSanitized(t, err, "SYNTHETIC-SCHEMA-SECRET", "MODEL_TOKEN")
			})
		}
	}
}
