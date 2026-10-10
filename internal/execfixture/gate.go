package execfixture

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Gate lets a shell fixture announce readiness and block until its test releases
// it. Neither child lifetime nor readiness depends on a sleep duration.
type Gate struct {
	ready, release *os.File
	command        string
}

func NewGate(t testing.TB) *Gate {
	t.Helper()
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	open := func(path string) *os.File {
		if err := makeFIFO(path); err != nil {
			t.Fatalf("create fixture gate: %v", err)
		}
		// Keep both ends open so opening the FIFO in the child cannot race
		// with the parent's readiness read or release write.
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatalf("open fixture gate: %v", err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	return &Gate{
		ready: open(ready), release: open(release),
		command: "printf . > " + quote(ready) + "; IFS= read -r fixture_release < " + quote(release),
	}
}

// Command uses shell builtins only, including when PATH is deliberately empty.
func (g *Gate) Command() string { return g.command }

func (g *Gate) Wait(t testing.TB) {
	t.Helper()
	ready := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := io.ReadFull(g.ready, b[:])
		ready <- err
	}()
	// A failure watchdog, never a readiness delay or a child lifetime.
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("fixture readiness: %v", err)
		}
	case <-timer.C:
		t.Fatal("fixture never announced readiness")
	}
}

func (g *Gate) Release(t testing.TB) {
	t.Helper()
	if _, err := g.release.WriteString("continue\n"); err != nil {
		t.Fatalf("release fixture: %v", err)
	}
}
