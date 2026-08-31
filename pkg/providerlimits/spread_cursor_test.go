package providerlimits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestSpreadCursorStore(t *testing.T) (*SpreadCursorStore, Layout, *[]string) {
	t.Helper()
	layout := LayoutAt(t.TempDir())
	warnings := &[]string{}
	store := NewSpreadCursorStore(SpreadCursorOptions{
		Layout: layout,
		Warn:   func(message string) { *warnings = append(*warnings, message) },
	})
	return store, layout, warnings
}

func TestTheSpreadCursorRoundTripsOnePositionPerProviderSet(t *testing.T) {
	store, _, _ := newTestSpreadCursorStore(t)

	if got := store.Read([]string{"codex", "claude"}); got != "" {
		t.Fatalf("a fresh cursor read %q, want no position", got)
	}
	if err := store.Write([]string{"codex", "claude"}, SpreadCursorValue("abc123", "codex-plan")); err != nil {
		t.Fatalf("writing the cursor: %v", err)
	}
	if got := store.Read([]string{"claude", "codex"}); got != "abc123:codex-plan" {
		t.Fatalf("cursor = %q; the entry key is the SORTED provider set, so argument order must not matter", got)
	}
}

// An exclusive window and a mixed window must not share a rotation slot: one
// carries a position the other cannot honour, and they would overwrite each
// other every spawn.
func TestDifferentProviderSetsDoNotShareACursorEntry(t *testing.T) {
	store, _, _ := newTestSpreadCursorStore(t)

	if err := store.Write([]string{"codex"}, SpreadCursorValue("id", "codex-plan")); err != nil {
		t.Fatalf("writing the exclusive cursor: %v", err)
	}
	if err := store.Write([]string{"codex", "claude"}, SpreadCursorValue("id", "claude-plan")); err != nil {
		t.Fatalf("writing the mixed cursor: %v", err)
	}
	if got := store.Read([]string{"codex"}); got != "id:codex-plan" {
		t.Fatalf("the exclusive entry is now %q; the mixed window overwrote a slot it does not share", got)
	}
	if got := store.Read([]string{"codex", "claude"}); got != "id:claude-plan" {
		t.Fatalf("the mixed entry is %q", got)
	}
}

// The cursor carries no correctness weight, so every read failure is reported
// as "no position" and never as an error a spawn could die on. A read FAILURE
// is still distinguished from an absence: it warns, an absence does not.
func TestAnUnusableCursorFileRestartsTheRotationAndWarnsOnlyForAFailure(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		store, _, warnings := newTestSpreadCursorStore(t)
		if got := store.Read([]string{"codex"}); got != "" {
			t.Fatalf("read %q from an absent file", got)
		}
		if len(*warnings) != 0 {
			t.Fatalf("an absence warned: %v; an absence and a failure to read are different facts", *warnings)
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		store, layout, warnings := newTestSpreadCursorStore(t)
		if err := os.MkdirAll(layout.Root, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(layout.SpreadCursorFile(), []byte("{not json"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if got := store.Read([]string{"codex"}); got != "" {
			t.Fatalf("read %q from a corrupt file", got)
		}
		if len(*warnings) == 0 {
			t.Fatal("a corrupt cursor file was treated as a silent absence")
		}
		// And a corrupt file must not block the next write.
		if err := store.Write([]string{"codex"}, SpreadCursorValue("id", "codex-plan")); err != nil {
			t.Fatalf("writing over a corrupt cursor: %v", err)
		}
		if got := store.Read([]string{"codex"}); got != "id:codex-plan" {
			t.Fatalf("after rewriting, cursor = %q", got)
		}
	})
}

// A newer-schema file belongs to a newer binary. Writing a downgrade over it
// would corrupt that binary's state, so the write is REFUSED — and the refusal
// is what a delete-only mutant of the version check would not exercise.
func TestANewerSchemaCursorFileIsNeitherReadNorOverwritten(t *testing.T) {
	store, layout, warnings := newTestSpreadCursorStore(t)
	if err := os.MkdirAll(layout.Root, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	future := `{"version": 9999, "cursor": {"codex": "id:codex-future"}}`
	path := layout.SpreadCursorFile()
	if err := os.WriteFile(path, []byte(future), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := store.Read([]string{"codex"}); got != "" {
		t.Fatalf("read %q out of a newer-schema file", got)
	}
	if err := store.Write([]string{"codex"}, SpreadCursorValue("id", "codex-plan")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if !strings.Contains(string(after), "codex-future") || strings.Contains(string(after), "codex-plan") {
		t.Fatalf("this binary wrote a schema downgrade over a newer binary's cursor:\n%s", after)
	}
	if len(*warnings) == 0 {
		t.Fatal("the refused downgrade was silent")
	}
}

// Writing the same position twice is one turn, not two. This is what makes a
// spawn whose preflight runs three times consume exactly one cursor turn.
func TestRewritingTheSamePositionIsIdempotent(t *testing.T) {
	store, layout, _ := newTestSpreadCursorStore(t)
	value := SpreadCursorValue("id", "codex-plan")
	for i := 0; i < 3; i++ {
		if err := store.Write([]string{"codex"}, value); err != nil {
			t.Fatalf("write %d: %v", i+1, err)
		}
	}
	if got := store.Read([]string{"codex"}); got != value {
		t.Fatalf("cursor = %q, want %q", got, value)
	}
	// No temporary files left behind by the atomic replace.
	entries, err := os.ReadDir(layout.Root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("a temporary file survived the atomic write: %s", entry.Name())
		}
	}
}

func TestSpreadCursorKeyIsOrderAndCaseInsensitiveAndDeduplicates(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{in: []string{"codex", "claude"}, want: "claude,codex"},
		{in: []string{"Claude", " CODEX "}, want: "claude,codex"},
		{in: []string{"codex", "codex"}, want: "codex"},
		{in: []string{"", "  "}, want: ""},
	}
	for _, testCase := range cases {
		if got := SpreadCursorKey(testCase.in); got != testCase.want {
			t.Fatalf("SpreadCursorKey(%v) = %q, want %q", testCase.in, got, testCase.want)
		}
	}
}

// An empty group names no position, so a caller that resolved nothing cannot
// write a value that would later read back as a group.
func TestAnEmptyGroupIsNeverWritten(t *testing.T) {
	store, layout, _ := newTestSpreadCursorStore(t)
	if got := SpreadCursorValue("id", "  "); got != "" {
		t.Fatalf("SpreadCursorValue with a blank group = %q", got)
	}
	if err := store.Write([]string{"codex"}, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.Root, "spread-cursor.json")); !os.IsNotExist(err) {
		t.Fatalf("an empty position created a cursor file (err=%v)", err)
	}
}
