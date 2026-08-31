package launchenv

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Every claim in this file is one the plugins above it rest on and cannot
// restate: they call Lookup and LookPath and observe a plan, so a defect here
// reads there as a wrong binary or a lost variable with nothing naming the
// cause.

// TestLookupMatchesTheWholeKey is the near-miss bound. It is the same defect
// the parity goldens seed CODEX_LIKE_BUT_NOT and CLAUDECODE_LIKE_BUT_NOT for,
// one layer down and without a fixture in the way.
func TestLookupMatchesTheWholeKey(t *testing.T) {
	t.Parallel()
	env := []string{"PATH_LIKE_BUT_NOT=/nope", "PATHOLOGICAL=/also-nope", "PATH=/real"}
	value, present := Lookup(env, "PATH")
	if !present || value != "/real" {
		t.Fatalf("Lookup = (%q, %v), want (\"/real\", true); a prefix or substring match would have taken one of the near misses", value, present)
	}
	if _, present := Lookup(env, "PAT"); present {
		t.Error("a strict PREFIX of a present key was reported as present")
	}
}

// TestUnsetAndEmptyAreDifferentFacts holds the two-result shape's whole point.
// A caller that cannot tell them apart cannot tell "resolve nothing" from
// "resolve against my environment".
func TestUnsetAndEmptyAreDifferentFacts(t *testing.T) {
	t.Parallel()
	if value, present := Lookup([]string{"PATH="}, "PATH"); !present || value != "" {
		t.Errorf("an empty PATH read as (%q, %v), want (\"\", true)", value, present)
	}
	if _, present := Lookup([]string{"HOME=/x"}, "PATH"); present {
		t.Error("an absent PATH read as present")
	}
}

// TestABareKeyIsNotAValue pins the documented asymmetry with
// agentic.SetEnvValue, which DOES treat a bare key as a match.
//
// The two answer different questions and the difference is only visible in a
// malformed environment: removal asks "is this key present", where a bare key
// plainly is; this asks "what is this key's value", and reporting an empty one
// would hand a caller a PATH it never set.
func TestABareKeyIsNotAValue(t *testing.T) {
	t.Parallel()
	if _, present := Lookup([]string{"PATH"}, "PATH"); present {
		t.Error("a malformed bare entry was reported as a value; a caller would resolve against an empty PATH it never supplied")
	}
	if _, err := LookPath([]string{"PATH"}, "anything"); !errors.Is(err, ErrNoPath) {
		t.Errorf("LookPath over a bare PATH entry returned %v, want %v", err, ErrNoPath)
	}
}

// TestValueTrims covers the one transformation Value performs, and the reason
// Lookup exists beside it.
func TestValueTrims(t *testing.T) {
	t.Parallel()
	if got := Value([]string{"ROOT=  /managed/root  "}, "ROOT"); got != "/managed/root" {
		t.Errorf("Value = %q, want the trimmed path", got)
	}
	if got := Value([]string{"OTHER=x"}, "ROOT"); got != "" {
		t.Errorf("Value of an unset key = %q, want \"\"", got)
	}
}

// TestAnAbsentPathIsNotAMissingProgram is the distinction both plugins
// re-export a sentinel for.
func TestAnAbsentPathIsNotAMissingProgram(t *testing.T) {
	t.Parallel()
	if _, err := LookPath([]string{"HOME=/x"}, "anything"); !errors.Is(err, ErrNoPath) {
		t.Errorf("LookPath with no PATH returned %v, want %v", err, ErrNoPath)
	}
	_, err := LookPath([]string{"PATH="}, "anything")
	if err == nil {
		t.Fatal("LookPath over an empty PATH found a program")
	}
	if errors.Is(err, ErrNoPath) {
		t.Error("an EMPTY PATH was reported as an ABSENT one; the caller asked to resolve against nothing and got told it supplied no environment")
	}
}

// TestACandidateMustBeExecutable is the narrowing that makes resolution mean
// something: a readable file with the right name in an earlier directory must
// not shadow the real program.
func TestACandidateMustBeExecutable(t *testing.T) {
	t.Parallel()
	shadow, installed := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(shadow, "prog"), []byte("not executable\n"), 0o644); err != nil {
		t.Fatalf("writing the shadow: %v", err)
	}
	want := filepath.Join(installed, "prog")
	if err := os.WriteFile(want, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the program: %v", err)
	}

	got, err := LookPath([]string{"PATH=" + shadow + string(os.PathListSeparator) + installed}, "prog")
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	if got != want {
		t.Errorf("LookPath = %q, want %q", got, want)
	}
}

// TestAnEmptyPathElementIsTheWorkingDirectory is exec.LookPath's rule, kept
// verbatim. A plugin that resolved it differently from the process's own
// launcher would report a binary the child then fails to exec.
func TestAnEmptyPathElementIsTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prog"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the program: %v", err)
	}
	t.Chdir(dir)

	got, err := LookPath([]string{"PATH=" + string(os.PathListSeparator) + "/nowhere"}, "prog")
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	if filepath.Base(got) != "prog" {
		t.Errorf("LookPath = %q, want the program in the working directory", got)
	}
}

// TestANameWithASeparatorIsUsedAsGiven is the other half of exec.LookPath's
// rule: a path is not a program name and is never searched for.
func TestANameWithASeparatorIsUsedAsGiven(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	program := filepath.Join(dir, "prog")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the program: %v", err)
	}

	// No PATH at all: a name carrying a separator must resolve anyway, which is
	// what proves it never consulted one.
	got, err := LookPath(nil, program)
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	if got != program {
		t.Errorf("LookPath = %q, want %q", got, program)
	}
	if _, err := LookPath(nil, filepath.Join(dir, "absent")); err == nil {
		t.Error("a path to a file that does not exist resolved")
	}
}

// TestIsRegularFileAndIsExecutableFile covers the two stat predicates directly,
// including the case a caller most easily gets wrong: a DIRECTORY with the
// right name and the execute bit set.
func TestIsRegularFileAndIsExecutableFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if IsRegularFile(dir) {
		t.Error("a directory was reported as a regular file")
	}
	if IsExecutableFile(dir) {
		t.Error("a directory was reported as an executable file; its mode has the execute bit set, so only the IsRegular half rejects it")
	}

	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the plain file: %v", err)
	}
	if !IsRegularFile(plain) {
		t.Error("a regular file was not reported as one")
	}
	if IsExecutableFile(plain) {
		t.Error("a non-executable file was reported as executable")
	}
	if IsRegularFile(filepath.Join(dir, "absent")) {
		t.Error("a missing path was reported as a regular file")
	}
}
