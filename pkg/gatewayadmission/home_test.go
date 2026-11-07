package gatewayadmission

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// An existing relative directory cannot be reinterpreted under the filesystem root; its absolute spelling is a valid control.
func TestGatewayRelativeHomeCannotSelectRootDirectory(t *testing.T) {
	if info, err := os.Stat("/dev"); err != nil || !info.IsDir() {
		t.Skip("requires the Unix /dev directory to witness unintended root-relative resolution")
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("dev", 0700); err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs("dev")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		t.Fatal(err)
	}
	if canonical, _, err := resolveHome(absolute); err != nil || canonical != resolved {
		t.Fatalf("absolute existing-home control: %q %v", canonical, err)
	}
	if _, _, err := resolveHome("dev"); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatalf("relative home reinterpreted under root: %v", err)
	}
}

// A vanished unrelated sibling cannot prevent an existing target's spelling; a vanished target and a symlink alone cannot supply it.
func TestGatewayHomeComponentDisappearingEntries(t *testing.T) {
	parent := t.TempDir()
	sibling, targetPath := filepath.Join(parent, "a-sibling"), filepath.Join(parent, "z-target")
	for _, path := range []string{sibling, targetPath} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := homeComponent(entries, target); !ok || name != "z-target" {
		t.Fatalf("existing target control: %q %v", name, ok)
	}
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}
	// Use the captured real DirEntry after removal, exactly as a ReadDir/Info race does.
	if name, ok := homeComponent(entries, target); !ok || name != "z-target" {
		t.Fatalf("unrelated vanished sibling blocked target: %q %v", name, ok)
	}
	if err := os.Remove(targetPath); err != nil {
		t.Fatal(err)
	}
	if name, ok := homeComponent(entries, target); ok {
		t.Fatalf("vanished target accepted: %q", name)
	}
	if err := os.Symlink(parent, filepath.Join(parent, "alias")); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := homeComponent(entries, info); ok {
		t.Fatalf("symlink supplied a canonical component: %q", name)
	}
}
