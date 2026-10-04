package agentic_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/refusalscan"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestPinnedHostedSchemasFrozenClosedAndLocal(t *testing.T) {
	for _, tc := range []struct{ id, hash string }{
		{agentic.ExecGuardSchema, "b6a8315999fe1fa3e065406eddb945aca06425854c73b142674b2d7138dbd92a"},
		{agentic.ClaudeRestartSchema, "3c0ba940cdd55313533a81feec3e3cbb0a6bb93557449e1100bbb3ead2fb122b"},
		{agentic.ClaudeEffectivePolicySchema, "b275c55b99b8f714909ebd8d755e2616ca2f01fc469204a370991ad3658512a0"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			raw, ok := agentic.PinnedHostedSchema(tc.id, "1.0.0")
			if !ok {
				t.Fatal("missing pinned schema")
			}
			if fmt.Sprintf("%x", sha256.Sum256(raw)) != tc.hash {
				t.Fatal("frozen 1.0.0 schema changed")
			}
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			if schema["$id"] != tc.id || schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
				t.Fatal("wrong schema dispatch")
			}
			var walk func(any)
			walk = func(v any) {
				switch node := v.(type) {
				case map[string]any:
					if node["type"] == "object" && node["additionalProperties"] != false {
						t.Fatal("open object")
					}
					if ref, ok := node["$ref"].(string); ok && !strings.HasPrefix(ref, "#") {
						t.Fatal("network reference")
					}
					for _, child := range node {
						walk(child)
					}
				case []any:
					for _, child := range node {
						walk(child)
					}
				}
			}
			walk(schema)
			raw[0] = 'x'
			fresh, _ := agentic.PinnedHostedSchema(tc.id, "1.0.0")
			if fresh[0] != '{' {
				t.Fatal("schema aliased")
			}
		})
	}
	for _, tc := range []struct{ id, version string }{{agentic.ExecGuardSchema, "2.0.0"}, {"unknown", "1.0.0"}} {
		if _, ok := agentic.PinnedHostedSchema(tc.id, tc.version); ok {
			t.Fatal("unknown schema/version admitted")
		}
	}
}

func TestAgenticProductionRemainsExecFree(t *testing.T) {
	root := filepath.Clean(filepath.Join(testPackageDirectory(t), "../.."))
	files, err := refusalscan.SourceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if value == "os/exec" {
				t.Fatalf("production process start in %s", path)
			}
		}
	}
}
