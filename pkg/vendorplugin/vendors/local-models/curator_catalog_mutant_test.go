package localmodels

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// Reverting only catalog resolution to Abs-only must break both retarget
// witnesses while the direct/HOME-relative controls still pass. The overlay
// changes compiled loader code only; the behavioral witness reads no sources.
func TestLoadAtCatalogAbsOnlyMutantKilledByName(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := gosources.Root(cwd)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "pkg/vendorplugin/vendors/local-models/curator.go")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	const before = "resolvedPath, err := filepath.EvalSymlinks(path)"
	if strings.Count(string(source), before) != 1 {
		t.Fatal("catalog resolution mutation site is not unique")
	}
	dir := t.TempDir()
	mutantPath, overlayPath := filepath.Join(dir, "curator.go"), filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(mutantPath, []byte(strings.Replace(string(source), before, "resolvedPath := path", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{sourcePath: mutantPath}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, control := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		args := []string{"test", "-mod=mod", "-p=2", "-count=1", "-v", "-run", "^TestLoadAtCatalogPathSurvivesSymlinkRetarget$"}
		if !control {
			args = append(args, "-overlay", overlayPath)
		}
		args = append(args, "./pkg/vendorplugin/vendors/local-models")
		cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		timedOut := ctx.Err() != nil
		cancel()
		if timedOut {
			t.Fatalf("catalog control/mutant timed out: %s", output)
		}
		text := string(output)
		for _, name := range []string{"stable-control", "home-relative-control"} {
			if !strings.Contains(text, "--- PASS: TestLoadAtCatalogPathSurvivesSymlinkRetarget/"+name) {
				t.Fatalf("positive sibling %s failed: %v\n%s", name, err, output)
			}
		}
		if control {
			if err != nil {
				t.Fatalf("baseline failed: %v\n%s", err, output)
			}
			t.Log("control exit 0: all four path witnesses pass")
			continue
		}
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || !strings.Contains(text, "catalog content drift") || strings.Contains(text, "[build failed]") || strings.Contains(text, "panic:") {
			t.Fatalf("Abs-only mutant did not fail on catalog drift: %v\n%s", err, output)
		}
		for _, name := range []string{"directory-link", "file-link"} {
			if !strings.Contains(text, "--- FAIL: TestLoadAtCatalogPathSurvivesSymlinkRetarget/"+name) {
				t.Fatalf("mutant escaped %s: %s", name, output)
			}
		}
		t.Logf("Abs-only mutant killed by both retarget witnesses; child exit 1\n%s", output)
	}
}
