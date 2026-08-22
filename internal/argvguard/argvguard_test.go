package argvguard

import (
	"strings"
	"testing"
)

// The scanner's RULES are held by the plugin guards that drive it — codex's and
// claude's argvguard_test.go each carry a full mutant battery against their own
// signature, which is the production call site. What is here is the shared
// contract those two rest on and neither states: that a malformed source is a
// reported failure rather than a silently empty scan, and that the allowlist
// key this package hands out is the one DeclaredNames produces.

var signature = []string{"--example-flag"}

// TestAMalformedSourceIsAnErrorNotAnEmptyScan is the difference between an
// absence and a failure to read.
//
// A scanner that swallowed a parse error would report zero violations for a
// file it never understood, and the guard would go green precisely when
// somebody had broken the code it watches.
func TestAMalformedSourceIsAnErrorNotAnEmptyScan(t *testing.T) {
	t.Parallel()
	violations, err := Scan(map[string]string{"broken.go": "package other\nfunc ("}, signature, nil)
	if err == nil {
		t.Fatalf("a source that does not parse produced %v and no error", violations)
	}
	if !strings.Contains(err.Error(), "broken.go") {
		t.Errorf("the failure was %v; it has to name the file, because the reader fixing it has only this text", err)
	}
}

// TestTheAllowlistKeyMatchesWhatDeclaredNamesProduces pins the two halves of
// the file-scoped allowlist against each other.
//
// A plugin's justification test asks DeclaredNames whether an allowlist entry
// names a site that exists. If the two spelled a key differently, every
// exemption would read as unclaimed and the justification test would fail for a
// reason that has nothing to do with the allowlist.
func TestTheAllowlistKeyMatchesWhatDeclaredNamesProduces(t *testing.T) {
	t.Parallel()
	const file = "pkg/example/args.go"
	sources := map[string]string{file: `package example
const someConst = "x"
var someVar = []string{"y"}
func SomeFunc() {}
`}
	declared, err := DeclaredNames(sources)
	if err != nil {
		t.Fatalf("DeclaredNames: %v", err)
	}
	for _, name := range []string{"SomeFunc", "someConst", "someVar"} {
		if !declared[AllowlistKey(file, name)] {
			t.Errorf("DeclaredNames did not report %q under the key AllowlistKey produces", name)
		}
	}
	if declared[AllowlistKey("other/elsewhere.go", "SomeFunc")] {
		t.Error("a name declared in one file was reported under another; the key is not carrying the file")
	}
}

// TestAnAllowlistEntryExemptsOneFileOnly is the bound the file scoping exists
// for, stated once at the shared layer rather than only inside each plugin.
//
// Two plugins in this module already name their construction site Args. A
// bare-name allowlist would have exempted both from both guards.
func TestAnAllowlistEntryExemptsOneFileOnly(t *testing.T) {
	t.Parallel()
	const body = `package other
func Args() []string { return []string{"--example-flag"} }`
	allowlist := map[string]string{AllowlistKey("owner/args.go", "Args"): "the single construction site"}

	if v, err := Scan(map[string]string{"owner/args.go": body}, signature, allowlist); err != nil || len(v) != 0 {
		t.Fatalf("the allowlisted site was reported (%v, %v)", v, err)
	}
	v, err := Scan(map[string]string{"impostor/args.go": body}, signature, allowlist)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(v) == 0 {
		t.Fatal("the identical construction in another file was admitted; the allowlist is exempting a NAME rather than the site that earned it")
	}
}

// TestOneSignatureLiteralIsEnough pins the threshold at the shared layer.
//
// The source's guard began at two, on the theory that a single literal could
// appear incidentally, and a review demonstrated a copy-pasted site spelling
// only one walking straight through. Raising it again would weaken every
// plugin's guard at once, from a file no plugin author is looking at.
func TestOneSignatureLiteralIsEnough(t *testing.T) {
	t.Parallel()
	v, err := Scan(map[string]string{"other/one.go": `package other
func single() []string { return []string{"--example-flag"} }`}, []string{"--example-flag", "--second-flag"}, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(v) == 0 {
		t.Fatal("a site spelling ONE of two signature literals was admitted; the threshold has been raised and every plugin's guard is weaker for it")
	}
}
