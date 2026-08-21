package runtimeenv_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/runtimeenv"
)

// This file holds the SHARED filter to its own bounds. The plugins that use it
// prove it again through their goldens, from the outside; these are the bounds
// no golden can see — the whole-key rule against a near miss, the pointer
// resolution, and the PATH strip the capture harness excludes from its diff
// entirely.

// keyOf returns the key half of a KEY=VALUE entry.
func keyOf(entry string) string {
	key, _, _ := strings.Cut(entry, "=")
	return key
}

func keys(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		out[key] = value
	}
	return out
}

// TestFilterStripsEveryDeclaredKey is the reachability half: each of the eleven
// keys is seeded and must be gone. Without it the bounds below would be equally
// consistent with a filter that strips nothing.
func TestFilterStripsEveryDeclaredKey(t *testing.T) {
	t.Parallel()
	var parent []string
	for _, key := range runtimeenv.Keys() {
		parent = append(parent, key+"=parent-value")
	}
	child := keys(runtimeenv.Filter(parent))
	for _, key := range runtimeenv.Keys() {
		if _, present := child[key]; present {
			t.Errorf("Filter kept %q; a child inheriting it attaches to the parent's codex runtime", key)
		}
	}
}

// TestTheStripIsByWholeKeyRatherThanByPrefix is the near-miss bound, at the
// unit level.
//
// `strings.HasPrefix(key, "CODEX_")` is the plausible one-character-different
// reading of FilterKeys, and it is why the parity capture seeds
// CODEX_LIKE_BUT_NOT. The same defect is driven through each plugin against its
// golden; here it is measured against the function itself, so a future caller
// that never had a golden still has the rule pinned.
func TestTheStripIsByWholeKeyRatherThanByPrefix(t *testing.T) {
	t.Parallel()
	nearMisses := []string{
		"CODEX_LIKE_BUT_NOT",
		"CODEX_THREAD_ID_SUFFIX",
		"TASK_BOARD_LIKE_BUT_NOT",
		"TASK_BOARD_SESSION_IDX",
	}
	parent := append([]string{}, nearMisses...)
	for i := range parent {
		parent[i] += "=keep-me"
	}
	parent = append(parent, runtimeenv.ThreadIDEnv+"=strip-me")

	child := keys(runtimeenv.Filter(parent))
	if _, present := child[runtimeenv.ThreadIDEnv]; present {
		t.Fatalf("the real key survived, so this test measures nothing")
	}
	for _, near := range nearMisses {
		if value, present := child[near]; !present || value != "keep-me" {
			t.Errorf("the near miss %q was stripped or rewritten (present=%v value=%q); the blocked set is matching by prefix, not by key", near, present, value)
		}
	}
}

// TestFilterBlocksTheCredentialEachPointerNames is the pointer resolution.
//
// The two token-NAME variables hold the NAME of the variable that holds the
// token. A filter that stripped only the pointers would leave the credential
// itself in the child, which is the whole reason this indirection is resolved
// against the environment being filtered.
func TestFilterBlocksTheCredentialEachPointerNames(t *testing.T) {
	t.Parallel()
	parent := []string{
		runtimeenv.AppServerTokenNameEnv + "=PARITY_APP_TOKEN",
		"PARITY_APP_TOKEN=app-secret",
		runtimeenv.SessionManagerTokenNameEnv + "=PARITY_MANAGER_TOKEN",
		"PARITY_MANAGER_TOKEN=manager-secret",
		"UNRELATED=keep-me",
	}
	child := keys(runtimeenv.Filter(parent))
	for _, leaked := range []string{"PARITY_APP_TOKEN", "PARITY_MANAGER_TOKEN"} {
		if _, present := child[leaked]; present {
			t.Errorf("the credential %q reached the child; the pointer was stripped but what it named was not", leaked)
		}
	}
	if _, present := child["UNRELATED"]; !present {
		t.Error("an unrelated variable was stripped; the pointer resolution is blocking more than the pointers name")
	}
}

// TestAnUnsetPointerBlocksNothingExtra is the other half of that bound: with no
// pointer set, nothing beyond the fixed list is removed. Without it, a filter
// that blocked every key whose value looked like a variable name would pass the
// test above.
func TestAnUnsetPointerBlocksNothingExtra(t *testing.T) {
	t.Parallel()
	child := keys(runtimeenv.Filter([]string{"A_NAME_NO_FIXED_LIST_COULD_CONTAIN=the-secret"}))
	if _, present := child["A_NAME_NO_FIXED_LIST_COULD_CONTAIN"]; !present {
		t.Error("a variable no pointer named was stripped anyway")
	}
}

// TestFilterKeysTreatsAnEntryWithoutAnEqualsAsABareKey pins the malformed-entry
// rule the source has: an entry with no `=` is a KEY, not a value. Reading it
// the other way would let `CODEX_SESSION` (no value) ride into a child that the
// filter believes it cleaned.
func TestFilterKeysTreatsAnEntryWithoutAnEqualsAsABareKey(t *testing.T) {
	t.Parallel()
	got := runtimeenv.FilterKeys([]string{"KEPT=1", runtimeenv.SessionEnv, "BARE"}, runtimeenv.SessionEnv)
	for _, entry := range got {
		if entry == runtimeenv.SessionEnv {
			t.Errorf("the bare blocked key survived: %v", got)
		}
	}
	if len(got) != 2 {
		t.Errorf("FilterKeys = %v, want the two unrelated entries", got)
	}
}

// TestFilterKeysIgnoresBlankBlockedKeys stops an empty key from matching an
// entry with an empty key half — a blank in the blocked list must be inert,
// not a wildcard.
func TestFilterKeysIgnoresBlankBlockedKeys(t *testing.T) {
	t.Parallel()
	got := runtimeenv.FilterKeys([]string{"KEPT=1", "BARE"}, "", "   ")
	if len(got) != 2 {
		t.Errorf("FilterKeys with blank keys = %v, want both entries kept", got)
	}
}

// TestSanitizePathDropsOnlyTheRuntimeEntries is the PATH bound, and it is the
// only evidence this strip has: the parity harness excludes PATH from its
// environment diff on both sides, so no golden can see it either way.
//
// Both directions are measured. Under-stripping leaves the child resolving the
// parent's binary through an arg0 shim whose parent is gone; over-stripping
// removes the directories every other tool the child needs lives in, and
// nothing would report either.
func TestSanitizePathDropsOnlyTheRuntimeEntries(t *testing.T) {
	t.Parallel()
	sep := string(os.PathListSeparator)
	dropped := []string{
		filepath.Join("/home/op", ".codex", "tmp", "arg0", "42"),
		"/opt/codex-path",
	}
	kept := []string{"/usr/local/bin", "/usr/bin", "/home/op/.codex/bin"}

	got := runtimeenv.SanitizePath([]string{"PATH=" + strings.Join(append(append([]string(nil), dropped...), kept...), sep)})
	if len(got) != 1 {
		t.Fatalf("SanitizePath returned %d entries, want 1", len(got))
	}
	parts := filepath.SplitList(strings.TrimPrefix(got[0], "PATH="))
	for _, part := range parts {
		for _, drop := range dropped {
			if part == drop {
				t.Errorf("SanitizePath kept the parent runtime entry %q", part)
			}
		}
	}
	if len(parts) != len(kept) {
		t.Errorf("SanitizePath produced %v, want exactly %v", parts, kept)
	}
}

// TestSanitizePathLeavesEveryOtherEntryAlone: a non-PATH entry must pass
// through untouched, including one carrying no `=`.
func TestSanitizePathLeavesEveryOtherEntryAlone(t *testing.T) {
	t.Parallel()
	in := []string{"HOME=/home/op", "BARE", "PATHFINDER=/opt/codex-path"}
	got := runtimeenv.SanitizePath(in)
	if len(got) != len(in) {
		t.Fatalf("SanitizePath rewrote the entry count: %v", got)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("SanitizePath rewrote %q into %q; PATHFINDER is not PATH", in[i], got[i])
		}
	}
}

// TestKeysHandsOutACopy: a caller that appends to the returned list must not be
// able to widen the blocked set every other caller reads. Filter itself appends
// the resolved pointers to this value on every call.
func TestKeysHandsOutACopy(t *testing.T) {
	t.Parallel()
	first := runtimeenv.Keys()
	first = append(first, "CALLER_ADDED")
	_ = first
	for _, key := range runtimeenv.Keys() {
		if key == "CALLER_ADDED" {
			t.Fatal("Keys returned the package's own slice; one caller's append widened the shared blocked set")
		}
	}
	if got := len(runtimeenv.Keys()); got != 11 {
		t.Errorf("Keys() has %d entries, want the source's eleven; a key added or removed here changes what every codex-family child inherits", got)
	}
}

// TestEveryDeclaredKeyIsInTheList catches a constant declared and then not
// wired into Keys — a key that reads as blocked in this file and is inherited
// by every child.
func TestEveryDeclaredKeyIsInTheList(t *testing.T) {
	t.Parallel()
	declared := []string{
		runtimeenv.ThreadIDEnv, runtimeenv.SessionEnv, runtimeenv.CIEnv,
		runtimeenv.ManagedByNPMEnv, runtimeenv.ManagedByBunEnv, runtimeenv.ManagedPackageRootEnv,
		runtimeenv.AppServerURLEnv, runtimeenv.AppServerTokenNameEnv,
		runtimeenv.SessionManagerURLEnv, runtimeenv.SessionManagerTokenNameEnv,
		runtimeenv.SessionIDEnv,
	}
	listed := map[string]bool{}
	for _, key := range runtimeenv.Keys() {
		listed[key] = true
	}
	for _, key := range declared {
		if !listed[key] {
			t.Errorf("%q is declared here but is not in Keys(); it reads as blocked and is not", key)
		}
	}
	if len(declared) != len(runtimeenv.Keys()) {
		t.Errorf("Keys() carries %d entries and %d are declared as constants; one of them is unnamed", len(runtimeenv.Keys()), len(declared))
	}
}

// TestFilterPreservesOrdinaryEntriesVerbatim is the preservation bound stated
// directly: a filter is only useful if it is SELECTIVE, and a whole-environment
// wipe would pass every strip assertion above.
func TestFilterPreservesOrdinaryEntriesVerbatim(t *testing.T) {
	t.Parallel()
	parent := []string{"HOME=/home/op", "LANG=en_US.UTF-8", "CLAUDECODE=1", runtimeenv.CIEnv + "=1"}
	child := runtimeenv.Filter(parent)
	got := keys(child)
	for _, want := range []string{"HOME", "LANG"} {
		if _, present := got[want]; !present {
			t.Errorf("Filter dropped the ordinary entry %q", want)
		}
	}
	// CLAUDECODE is the qwen filter's addition on top of this one; this filter
	// must NOT strip it, or the two systems' contracts have merged.
	if _, present := got["CLAUDECODE"]; !present {
		t.Error("Filter stripped CLAUDECODE; that key is the qwen filter's addition and a codex child inherits it")
	}
	if _, present := got[runtimeenv.CIEnv]; present {
		t.Errorf("Filter kept %q, so this test measured nothing", keyOf(runtimeenv.CIEnv))
	}
}
