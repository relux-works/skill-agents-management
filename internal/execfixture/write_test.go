package execfixture

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutablePublicationReplacesInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture")
	if err := WriteFile(path, []byte("#!/bin/sh\necho before\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	before, err := old.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("#!/bin/sh\necho after\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("executable publication rewrote the exposed inode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, path).Output(); err != nil || string(out) != "after\n" {
		t.Fatalf("published executable = %q, %v", out, err)
	}
}

// Every parallel branch publishes and immediately executes its own fixture.
// Forks in one branch must not inherit an open writer from another branch.
func TestExecutablePublicationWithConcurrentForks(t *testing.T) {
	for i := 0; i < 32; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "fixture")
			for generation := 0; generation < 8; generation++ {
				body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%d'\n", generation)
				if err := WriteFile(path, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				out, err := exec.CommandContext(ctx, path).Output()
				cancel()
				if err != nil || string(out) != fmt.Sprintln(generation) {
					t.Fatalf("immediate concurrent exec = %q, %v", out, err)
				}
			}
		})
	}
}
