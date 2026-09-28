package main

import (
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDiscoverMutationsCoversEveryTypedRefusalSource(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not identify the generator test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("change to module root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	rows := discoverMutations(token.NewFileSet(), []string{
		"pkg/agentic/systems/codex/provider.go",
		"pkg/agentic/localprovider.go",
	})
	var direct, validator, blankBinding int
	for _, row := range rows {
		if strings.HasPrefix(row.Source, "validateLoopbackBaseURL") {
			validator++
		}
		if row.ID == "M-provider.go-L28-O1" {
			blankBinding++
		}
		if row.Source == "typed LocalProviderRefusal return" {
			direct++
		}
	}
	if len(rows) != 41 || direct != 29 || validator != 12 || blankBinding != 1 {
		t.Fatalf("generated refusal operands = total %d, direct %d, validateLoopbackBaseURL %d, provider.go:28 %d; want 41, 29, 12, 1", len(rows), direct, validator, blankBinding)
	}
}
