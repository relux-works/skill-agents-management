package agentic_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lbProvenanceEntry is one GENERATED property-test case over the
// provenance-wiring-claims invariant: source and documentation state actual
// ownership and current integration truthfully (round-2 finding, repeat of
// round-1 F3). Each entry binds a claim family to a claim site: every
// forbidden phrase must be absent and every required disclosure present.
// A point fix that corrects one sentence while leaving another false claim
// (or dropping a disclosure) in any site fails here.
type lbProvenanceEntry struct {
	family    string
	file      string // relative to the module root; README.md is scoped to the launch-boundary subsection
	forbidden []string
	required  []string
}

func lbProvenanceCatalog() []lbProvenanceEntry {
	wiringForbidden := []string{
		"daemon and the launcher call this one",
		"daemon and the launcher call this API",
		"launcher call this one",
		"so the daemon re-checks",
	}
	wiringRequired := []string{"do not call", "not wired yet", "follow", "future"}
	inlineForbidden := []string{
		"grammar curator run applies",
		"spawn-plane build validates the",
		"inline --mcp-config grammar at the spawn-plane build",
		"validators curator run",
		"curator run applies earlier",
		"Curator run admits MCP configuration",
	}
	inlineRequired := []string{
		"internal/mcpjson", "v0.5.37", "plan.go", "fbcdbaf0",
		"explicitly requested", "module's own", "unset",
	}
	return []lbProvenanceEntry{
		{family: "wiring", file: "pkg/agentic/launchboundary.go", forbidden: wiringForbidden, required: wiringRequired},
		{family: "wiring", file: "pkg/agentic/launchboundary_admission.go", forbidden: wiringForbidden, required: wiringRequired},
		{family: "wiring", file: "README.md", forbidden: wiringForbidden, required: wiringRequired},
		{family: "wiring", file: "changelog.d/TASK-261004-26qvc0.md", forbidden: wiringForbidden, required: wiringRequired},
		{family: "inline-attribution", file: "pkg/agentic/launchboundary.go", forbidden: inlineForbidden, required: inlineRequired},
		{family: "inline-attribution", file: "pkg/agentic/launchboundary_admission.go", forbidden: inlineForbidden, required: inlineRequired},
		{family: "inline-attribution", file: "README.md", forbidden: inlineForbidden, required: inlineRequired},
		{family: "inline-attribution", file: "changelog.d/TASK-261004-26qvc0.md", forbidden: inlineForbidden, required: []string{"module's own", "internal/mcpjson", "v0.5.37"}},
	}
}

// lbProvenanceSiteBody reads one claim site. README.md is scoped to the
// launch-boundary subsection so unrelated sections cannot satisfy or break
// this leaf's disclosures. Paths resolve from the test working directory so
// the mutant harness (which runs this test in an isolated module copy)
// observes mutated bytes.
func lbProvenanceSiteBody(t *testing.T, file string) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("provenance invariant violated: get test package directory: %v", err)
	}
	moduleRoot := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	body, err := os.ReadFile(filepath.Join(moduleRoot, file))
	if err != nil {
		t.Fatalf("provenance invariant violated: read %s: %v", file, err)
	}
	if file != "README.md" {
		return string(body)
	}
	const anchor = "#### Native launch boundary"
	start := strings.Index(string(body), anchor)
	if start < 0 {
		t.Fatalf("provenance invariant violated: launch-boundary README subsection missing")
	}
	rest := string(body)[start:]
	if end := strings.Index(rest[len(anchor):], "\n### "); end >= 0 {
		return rest[:len(anchor)+end]
	}
	return rest
}

// TestLaunchBoundaryProvenanceInvariant is the GENERATED property test over
// the provenance-wiring-claims class, parametrised over claim families ×
// claim sites. It is the named regression test for the round-2 repeat of F3
// and the named kill test for the launch-boundary-provenance-wiring-restored
// mutant: any reintroduced present-tense wiring claim, any reattributed
// inline grammar, or any dropped ownership disclosure fails by name.
func TestLaunchBoundaryProvenanceInvariant(t *testing.T) {
	for _, entry := range lbProvenanceCatalog() {
		t.Run(entry.family+"/"+entry.file, func(t *testing.T) {
			body := lbProvenanceSiteBody(t, entry.file)
			for _, phrase := range entry.forbidden {
				t.Run("forbidden", func(t *testing.T) {
					if strings.Contains(body, phrase) {
						t.Fatalf("provenance invariant violated: forbidden wiring claim %q present in %s", phrase, entry.file)
					}
				})
			}
			for _, disclosure := range entry.required {
				t.Run("required", func(t *testing.T) {
					if !strings.Contains(body, disclosure) {
						t.Fatalf("provenance invariant violated: required disclosure %q missing from %s", disclosure, entry.file)
					}
				})
			}
			if entry.family == "wiring" {
				t.Run("no-unnegated-call", func(t *testing.T) {
					for _, token := range []string{"call this", "calls this"} {
						for offset := 0; ; {
							at := strings.Index(body[offset:], token)
							if at < 0 {
								break
							}
							absolute := offset + at
							contextStart := absolute - 20
							if contextStart < 0 {
								contextStart = 0
							}
							if context := body[contextStart:absolute]; !strings.Contains(context, "not ") {
								t.Fatalf("provenance invariant violated: unnegated wiring claim %q in %s", token, entry.file)
							}
							offset = absolute + len(token)
						}
					}
				})
			}
		})
	}
}
