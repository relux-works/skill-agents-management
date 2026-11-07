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
