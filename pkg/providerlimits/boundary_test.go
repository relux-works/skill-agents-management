package providerlimits

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// modulePath is this module's import prefix. Every intra-module import starts
// with it, which is how the one-dependency rule below is checked without a
// hand-maintained list of what is NOT allowed.
const modulePath = "github.com/relux-works/skill-agents-management/"

// forbiddenImports is the boundary that keeps the limit plane a plane rather
// than a layer of the CLI. It is asserted mechanically rather than by
// convention, because a boundary nobody enforces is a boundary that is already
// broken.
//
// The extraction source enforced the same shape against its own board,
// spawner and remote-config packages; the names differ here because the
// neighbours differ, and its entries for packages this module does not have
// are kept so a later import of a re-extracted board or spawner trips this
// test rather than sliding in.
var forbiddenImports = []string{
	"github.com/relux-works/skill-agents-management/tools/",
	"github.com/relux-works/skill-project-management",
	"github.com/aagrigore/task-board",
	"pkg/board",
	"internal/spawn",
	"internal/spawnruntime",
	"internal/sessionmanager",
}

// allowedIntraModuleImports is the pinned exception list, and it is two
// entries because this module splits across two layers what the extraction
// source kept in one place plus a table of its own.
//
//   - pkg/vendorplugin owns the frozen (runtime, vendor) table — the vocabulary
//     HasClassifier and DefaultGroups resolve broker facts from (design D4) —
//     and the Availability verdict this plane reports through (verdict.go). It
//     is the direct carry-over of the source's one allowed import,
//     pkg/remoteconfig/runtimeid.
//   - pkg/agentic owns the harness capability declarations, and the one this
//     plane needs is the CONFIGURATION HOME: which environment variable a
//     harness reads and where it looks by default. The source kept a table of
//     that here; this port resolves it through the plugin that declares it,
//     which is invariant 5 and is why the import exists at all. See
//     identity.go's block comment above DefaultProviderHome.
//
// Neither carries business logic in behind it, which is the property the
// carve-out is worth anything for and which
// TestTheVendorpluginDependencyCarriesNoBusinessLogic checks over the real
// compiled package graph.
var allowedIntraModuleImports = map[string]bool{
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin": true,
	"github.com/relux-works/skill-agents-management/pkg/agentic":      true,
}

func TestModuleImportsNothingFromTheCLIOrTheExtractionSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	fileSet := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		checked++
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if allowedIntraModuleImports[path] {
				continue
			}
			for _, forbidden := range forbiddenImports {
				if strings.Contains(path, forbidden) {
					t.Errorf("%s imports %q, which crosses the boundary that keeps this module self-contained", entry.Name(), path)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no Go files were checked; the assertion would pass vacuously")
	}
}

// TestTheOnlyIntraModuleImportIsVendorplugin is the near-self-contained claim
// stated as a check rather than as prose.
//
// forbiddenImports above is a DENYLIST, and a denylist can only refuse the
// neighbours somebody thought to name. This is the allowlist half: every
// import of this module's own code, from every non-test file here, must be one
// of allowedIntraModuleImports. A future file reaching into internal/launchenv
// or pkg/agentic directly is caught by this even though no denylist entry
// names it.
//
// Test files are deliberately excluded. The guard and round-trip tests below
// read this repository's own layout and drive the vendor registry, and holding
// test scaffolding to the production boundary would refuse exactly the
// evidence the boundary exists to make checkable.
func TestTheOnlyIntraModuleImportIsVendorplugin(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	fileSet := token.NewFileSet()
	checked := 0
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		checked++
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if !strings.HasPrefix(path, modulePath) {
				continue
			}
			seen[path] = true
			if !allowedIntraModuleImports[path] {
				t.Errorf("%s imports %q; the limit plane may import only pkg/vendorplugin and pkg/agentic, and everything else has to arrive as a parameter", entry.Name(), path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no Go files were checked; the assertion would pass vacuously")
	}
	if !seen["github.com/relux-works/skill-agents-management/pkg/vendorplugin"] {
		t.Fatal("no production file imports pkg/vendorplugin; either the broker resolution stopped going through the frozen table or the verdict seam is gone, and this allowlist would then be guarding nothing")
	}
	if !seen["github.com/relux-works/skill-agents-management/pkg/agentic"] {
		t.Fatal("no production file imports pkg/agentic; the provider home is resolved from the harness plugin that declares it, and losing that import means a second home table came back")
	}
}

// TestTheVendorpluginDependencyCarriesNoBusinessLogic proves the one allowed
// exception carries no board, no spawner and no configuration parsing behind
// it. It is this module's replacement for the extraction source's
// TestRuntimeIDDependencyIsItselfLeaf: pkg/vendorplugin is not a leaf — it
// imports pkg/agentic and internal/ident — so the checkable claim is not "zero
// dependencies" but "nothing in its transitive closure is a thing this plane
// was extracted away from".
//
// The closure is read from the compiled package graph through `go list`, not
// from the import lines of one file, so a dependency added two hops away is
// still caught.
func TestTheVendorpluginDependencyCarriesNoBusinessLogic(t *testing.T) {
	var deps []string
	for allowed := range allowedIntraModuleImports {
		deps = append(deps, goListDeps(t, allowed)...)
	}
	if len(deps) == 0 {
		t.Fatal("go list reported no dependencies for the allowed imports; the assertion would pass vacuously")
	}
	sawModulePackage := false
	for _, dep := range deps {
		if strings.HasPrefix(dep, modulePath) {
			sawModulePackage = true
		}
		for _, forbidden := range forbiddenImports {
			if strings.Contains(dep, forbidden) {
				t.Errorf("an allowed import depends transitively on %q, which crosses the boundary those carve-outs exist for", dep)
			}
		}
	}
	if !sawModulePackage {
		t.Fatal("go list reported no intra-module dependencies of the allowed imports; the closure was not resolved and this test would pass vacuously")
	}
}

// goListDeps returns the full transitive dependency list of one package.
func goListDeps(t *testing.T, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-mod=mod", "-deps", pkg)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	var deps []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			deps = append(deps, line)
		}
	}
	return deps
}

// repoRoot walks up from the package directory to the module root — the
// directory carrying go.mod. Tests run with the package directory as their
// working directory, and `go list` needs the module.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory")
		}
		dir = parent
	}
}

// No configuration is parsed here. Ladder, lease bounds and group overrides arrive
// as already-resolved parameters, which is what keeps the config schema's owner and
// this module's owner separate.
func TestNoConfigurationParsingInThisModule(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbiddenTokens := []string{
		"spawn.limits",
		"contract_version",
		"spawn-policy-v3",
		"PolicyRank",
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range forbiddenTokens {
			if strings.Contains(string(data), token) {
				t.Errorf("%s mentions %q; configuration parsing and capability rank belong outside this module", entry.Name(), token)
			}
		}
	}
}

// The design names six files; the five that are in this port's scope all exist,
// so a reader following the design finds what it says is there.
//
// simulate.go is deliberately absent — the fault injector is launch-plane
// behaviour and is not ported here; see the package doc. verdict.go and
// ladder.go are this port's own: the seam onto vendorplugin.Availability, which
// the extraction source had no need for, and the broker-keyed ladder lookup,
// which the source keeps in its config module.
func TestDesignatedFilesExist(t *testing.T) {
	for _, name := range []string{"groups.go", "classify.go", "identity.go", "state.go", "report.go", "verdict.go", "ladder.go"} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("%s is missing: %v", name, err)
		}
	}
}

func TestTestdataCarriesTheCapturedEvidence(t *testing.T) {
	required := []string{
		"codex-usage-limit-RUN-260801-a714a6.log",
		"codex-usage-limit-RUN-260801-c95402.log",
		"codex-templates.txt",
		"codex-marker-quoted.log",
		"codex-managed-budget-limit.json",
		"codex-managed-usage-limit.json",
		"claude-429-per-minute.json",
		"claude-429-usage-limit.json",
		"claude-400-credit-balance.json",
		"claude-529-null-status.json",
		"claude-401-auth.json",
		"claude-is-error-false.json",
		"claude-success-with-limit-prose.json",
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join("testdata", name)); err != nil {
			t.Errorf("fixture %s is missing: %v", name, err)
		}
	}
}
