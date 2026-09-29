//go:build unix

package providerlimits

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

type processStartTimeErrorSite struct {
	id       string
	kind     ProcessStartTimeErrorKind
	testName string
}

var requiredProcessStartTimeErrorSites = []processStartTimeErrorSite{
	{id: "invalid_pid", kind: ProcessStartTimeInvalidPID, testName: "TestProcessStartTimeInvalidPIDError"},
	{id: "kernel_missing_pid", kind: ProcessStartTimeMissingPID, testName: "TestProcessStartTimeMissingPIDError"},
	{id: "kernel_permission_denied", kind: ProcessStartTimePermissionDenied, testName: "TestProcessStartTimePermissionDeniedError"},
	{id: "kernel_read_failure", kind: ProcessStartTimeKernelReadFailure, testName: "TestProcessStartTimeKernelReadFailureError"},
	{id: "ps_fallback_transient", kind: ProcessStartTimeFallbackTransient, testName: "TestProcessStartTimeFallbackTimeoutExhaustionIsTransient"},
}

func TestProcessStartTimeTypedErrorReturnSitesAreComplete(t *testing.T) {
	root := processStartTimeModuleRoot(t)
	sources := providerlimitsProductionSources(t, root)
	sites, constructorCount, err := typeResolvedProcessStartTimeErrorSites(t, root, sources)
	if err != nil {
		t.Fatal(err)
	}
	testNames := providerlimitsTestNames(t, root)
	if err := validateProcessStartTimeErrorSiteSet(sites, constructorCount, testNames); err != nil {
		t.Fatal(err)
	}
}

func TestProcessStartTimeTypedErrorGuardRejectsDuplicateSiteID(t *testing.T) {
	root := processStartTimeModuleRoot(t)
	sources := providerlimitsProductionSources(t, root)
	const sourceName = "pkg/providerlimits/liveness_starttime_unix.go"
	source := sources[sourceName]
	const functionStart = "func processStartTimeWithReaders(\n\tpid int,\n\tkernel kernelStartTimeReader,\n\tps psStartTimeReader,\n) (time.Time, error) {\n"
	if !strings.Contains(source, functionStart) {
		t.Fatalf("production lookup declaration not found in %s", sourceName)
	}
	duplicate := "\tif pid == 424242 { return time.Time{}, newProcessStartTimeError(pid, ProcessStartTimePermissionDenied, \"kernel_read_failure\", 0, nil) }\n"
	sources[sourceName] = strings.Replace(source, functionStart, functionStart+duplicate, 1)
	sites, constructorCount, err := typeResolvedProcessStartTimeErrorSites(t, root, sources)
	if err != nil {
		t.Fatalf("enumerate production tree with duplicate site id: %v", err)
	}
	err = validateProcessStartTimeErrorSiteSet(sites, constructorCount, providerlimitsTestNames(t, root))
	if err == nil || !strings.Contains(err.Error(), `duplicate typed error site id "kernel_read_failure"`) {
		t.Fatalf("guard did not reject a second, mis-kinded factory return reusing a production site id: %v", err)
	}
}

func TestProcessStartTimeErrorNarrowingMutantsAreKilled(t *testing.T) {
	kinds := []ProcessStartTimeErrorKind{
		ProcessStartTimeInvalidPID,
		ProcessStartTimeMissingPID,
		ProcessStartTimePermissionDenied,
		ProcessStartTimeKernelReadFailure,
		ProcessStartTimeFallbackTransient,
	}
	mutants := 0
	for _, site := range requiredProcessStartTimeErrorSites {
		t.Run(site.id, func(t *testing.T) {
			baseline := driveProcessStartTimeErrorSite(site.id)
			if mismatch := processStartTimeErrorMismatch(baseline, site.id, site.kind); mismatch != nil {
				t.Fatalf("named negative test %s has no correct production control case: %v", site.testName, mismatch)
			}
			for _, narrowedKind := range kinds {
				if narrowedKind == site.kind {
					continue
				}
				narrowedKind := narrowedKind
				mutantName := string(site.kind) + "_narrowed_to_" + string(narrowedKind)
				t.Run(mutantName, func(t *testing.T) {
					assertProcessStartTimeSourceNarrowingMutantKilled(
						t,
						"pkg/providerlimits/liveness_starttime_unix.go",
						processStartTimeErrorReturnCall(site, site.kind),
						processStartTimeErrorReturnCall(site, narrowedKind),
						site.testName,
					)
					t.Logf("source narrowing mutant %s killed by named test %s", mutantName, site.testName)
					mutants++
				})
			}
		})
	}
	if want := len(requiredProcessStartTimeErrorSites) * (len(kinds) - 1); mutants != want {
		t.Fatalf("generated narrowing mutants = %d, want complete site/member matrix of %d", mutants, want)
	}
}

func processStartTimeErrorReturnCall(site processStartTimeErrorSite, kind ProcessStartTimeErrorKind) string {
	kindConstant := processStartTimeErrorKindConstant(kind)
	switch site.id {
	case "invalid_pid":
		return fmt.Sprintf("newProcessStartTimeError(pid, %s, %q, 0, nil)", kindConstant, site.id)
	case "kernel_missing_pid", "kernel_permission_denied":
		return fmt.Sprintf("newProcessStartTimeError(pid, %s, %q, 0, kernelErr)", kindConstant, site.id)
	case "kernel_read_failure", "ps_fallback_transient":
		return fmt.Sprintf("newProcessStartTimeError(pid, %s, %q, attempts, joined)", kindConstant, site.id)
	default:
		panic("unmapped process start-time error site: " + site.id)
	}
}

func processStartTimeErrorKindConstant(kind ProcessStartTimeErrorKind) string {
	switch kind {
	case ProcessStartTimeInvalidPID:
		return "ProcessStartTimeInvalidPID"
	case ProcessStartTimeMissingPID:
		return "ProcessStartTimeMissingPID"
	case ProcessStartTimePermissionDenied:
		return "ProcessStartTimePermissionDenied"
	case ProcessStartTimeKernelReadFailure:
		return "ProcessStartTimeKernelReadFailure"
	case ProcessStartTimeFallbackTransient:
		return "ProcessStartTimeFallbackTransient"
	default:
		panic("unmapped process start-time error kind: " + string(kind))
	}
}

func TestProcessStartTimeGuardFindsAddedSourceFilesWithoutGitDiff(t *testing.T) {
	root := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	write("go.mod", "module example.invalid/guard-fixture\n\ngo 1.25.5\n")
	write("pkg/providerlimits/existing.go", "package providerlimits\n")
	write("pkg/providerlimits/new_return_site.go", "package providerlimits\n")
	write("pkg/providerlimits/testdata/fixture.go", "package providerlimits\n")
	write("pkg/other/unrelated.go", "package other\n")
	write(".temp/worktree/hidden.go", "package hidden\n")

	sources := providerlimitsProductionSources(t, root)
	if len(sources) != 2 {
		t.Fatalf("module-root package walk returned %d providerlimits sources, want existing + newly added file: %v", len(sources), sources)
	}
	if _, ok := sources["pkg/providerlimits/new_return_site.go"]; !ok {
		t.Fatal("new providerlimits source file was not found; the guard must not depend on git diff or a hardcoded file list")
	}
	if _, ok := sources["pkg/providerlimits/testdata/fixture.go"]; ok {
		t.Fatal("testdata was included even though the Go module build excludes it")
	}
	if _, ok := sources["pkg/other/unrelated.go"]; ok {
		t.Fatal("the package-specific error guard included a different package")
	}
}

func TestProcessStartTimeGuardTypecheckResolvesSymlinkedModuleRoot(t *testing.T) {
	root := processStartTimeModuleRoot(t)
	artifactDir := filepath.Join(root, ".temp", "BUG-260929-326q2f", "symlink-typecheck")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create task-scoped symlink test directory: %v", err)
	}
	scratch, err := os.MkdirTemp(artifactDir, "module-")
	if err != nil {
		t.Fatalf("create symlink test scratch directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(scratch); err != nil {
			t.Errorf("remove symlink test scratch directory: %v", err)
		}
	}()
	linkedRoot := filepath.Join(scratch, "worktree")
	if err := os.Symlink(root, linkedRoot); err != nil {
		t.Fatalf("symlink module root: %v", err)
	}
	sources := providerlimitsProductionSources(t, root)
	if _, _, _, err := typeCheckProviderlimitsSources(linkedRoot, sources, processStartTimeBuildVariant{goos: "darwin", goarch: "arm64"}); err != nil {
		t.Fatalf("type-check providerlimits from symlinked module root: %v", err)
	}
}

type discoveredProcessStartTimeErrorSite struct {
	id       string
	kind     ProcessStartTimeErrorKind
	file     string
	function string
	position string
}

func processStartTimeModuleRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := gosources.Root(workingDirectory)
	if err != nil {
		t.Fatalf("find module root from %s: %v", workingDirectory, err)
	}
	return root
}

func providerlimitsProductionSources(t *testing.T, root string) map[string]string {
	t.Helper()
	allSources, err := gosources.Walk(root)
	if err != nil {
		t.Fatalf("walk all non-test module Go sources from %s: %v", root, err)
	}
	sources := make(map[string]string)
	for name, source := range allSources {
		if strings.HasPrefix(name, "pkg/providerlimits/") {
			sources[name] = source
		}
	}
	if len(sources) == 0 {
		t.Fatal("module-root walk found no pkg/providerlimits production sources")
	}
	return sources
}

func providerlimitsTestNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	packageDir := filepath.Join(root, "pkg", "providerlimits")
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("read providerlimits package directory: %v", err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(packageDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse providerlimits test file %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
				names[function.Name.Name] = true
			}
		}
	}
	return names
}

// Exercise the shared module walker's error behavior: a missing root remains a
// read failure, never an empty clean result.
func TestProviderlimitsErrorGuardDoesNotTreatWalkFailureAsAbsence(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-module")
	if _, err := gosources.Walk(missing); err == nil {
		t.Fatal("walk unexpectedly succeeded for a missing module root")
	}
}
