// Package launchenv reads a LAUNCH environment — the []string a caller handed
// a plugin — rather than the process's own.
//
// Every agentic-system plugin has to answer the same two questions before it
// can build a plan: what is the value of this variable, and where does this
// program name resolve on PATH. The extraction source answered both with
// os.Getenv and exec.LookPath, which read a process global. This module's
// System contract forbids that — a plugin reaches for nothing ambient, and a
// dry run must report the same target a real launch would use — so every
// plugin needs the same reshape, and a copy per plugin is the shape invariant 5
// of docs/architecture.md exists to prevent. Two plugins whose PATH lookup
// disagrees about an empty element, or about whether an absent PATH is an
// error, both launch; the disagreement only surfaces as one harness resolving
// a binary another cannot find.
//
// Nothing here knows any harness's name. The program name is the caller's
// argument, so this package can never grow a per-system branch.
package launchenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoPath is returned when the launch environment carries no PATH entry at
// all.
//
// It is a REFUSAL rather than a fallback onto the ambient process PATH. A
// caller that curated an environment without PATH did not ask for a
// substitute, and resolving through a PATH the child will never see is how a
// launcher reports a binary the child cannot run.
//
// It is also deliberately distinct from "not found": an absent PATH and a PATH
// that contains no such program are different facts, and a caller told the
// second when the first happened goes looking for a missing install.
var ErrNoPath = errors.New("launchenv: the launch environment carries no PATH")

// Lookup returns the value of key in a KEY=VALUE environment and whether it was
// present at all.
//
// The two-result shape is the point: an unset variable and an empty one are
// different facts. Matching is `KEY=` followed by the value, so KEY_SUFFIX is
// never mistaken for KEY — the same exact-key discipline agentic.SetEnvValue
// documents, and the reason the parity goldens seed near-miss keys.
//
// One asymmetry with agentic.SetEnvValue is deliberate: a malformed entry
// carrying a bare key and no `=` is NOT a hit here, while SetEnvValue removes
// it. The two answer different questions. Removal asks "is this key present",
// where a bare key plainly is; this asks "what is this key's value", and a bare
// key has none — reporting it as present-and-empty would hand a caller an
// empty PATH it never set.
func Lookup(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}

// Value returns the trimmed value of key, or "" when it is unset.
//
// It trims because the values plugins read through it — package roots, the
// NAMES of other variables — are path- and identifier-shaped, where
// surrounding whitespace is always a typo rather than data. A caller that
// needs the raw bytes, or needs to tell unset from empty, uses Lookup.
func Value(env []string, key string) string {
	value, ok := Lookup(env, key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// LookPath resolves name against the PATH carried by env.
//
// It is exec.LookPath's rule — split PATH, treat an empty element as the
// current directory, take the first entry that is an executable file — over a
// SUPPLIED environment instead of the process's own. A name that already
// contains a separator is used as given, exactly as exec.LookPath does.
func LookPath(env []string, name string) (string, error) {
	if strings.ContainsRune(name, filepath.Separator) {
		if IsExecutableFile(name) {
			return name, nil
		}
		return "", fmt.Errorf("launchenv: %s is not an executable file", name)
	}
	pathValue, ok := Lookup(env, "PATH")
	if !ok {
		return "", ErrNoPath
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if IsExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("launchenv: %q not found in the launch environment's PATH", name)
}

// IsRegularFile reports whether path names an existing file rather than a
// directory.
//
// A stat error of any kind reads as "not usable here", which is the source's
// `if _, err := os.Stat(...); err == nil` verbatim; a caller's next move — try
// the following resolution path — is the same for a missing file and an
// unreadable one.
func IsRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// IsExecutableFile reports whether path is a regular file with an execute bit
// set, which is what PATH lookup requires of a candidate.
func IsExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode.IsRegular() && mode.Perm()&0o111 != 0
}
