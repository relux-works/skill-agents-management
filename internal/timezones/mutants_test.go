package timezones

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The mutant driver runs real tests in a child `go test -tags mutants` over
// this package. The mutants are semantic: mutants_hooks_test.go (compiled only
// with the mutants tag) swaps package-level seams selected by TZ_MUTANT, so no
// source text is rewritten and a behaviour-neutral rename cannot break a
// mutant. Each mutant narrows one guarantee by exactly one member and must
// fail its named witness; the unmutated control must stay green, and a build
// failure or a panic is never a kill.

const (
	mutantEnv      = "TZ_MUTANT"
	mutantChildEnv = "TZ_MUTANT_CHILD"
	hooksWitness   = "TestHooksAreNeverSetOutsideTheMutantsBuild"
)

type timezonesMutant struct{ name, witness string }

var timezonesMutants = []timezonesMutant{
	{"decoded bytes altered by one byte", "TestBundledZipDecodesToTheOriginalBytes"},
	{"loader falls back to time.LoadLocation for an unknown zone", "TestLoadNeverFallsBackToSystemZones"},
	{"loader consults time.LoadLocation before the bundle", "TestLoadIgnoresDecoyZoneinfoForBundledZone"},
	{"bundle decoded on every call instead of once", "TestBundledZipIsDecodedOnce"},
	{"NUL guard scans only the first 4096 bytes of a file", "TestNULGuardFindsNULAnywhereInAFile"},
}

// The seams must hold their real implementations in every build that is not
// running a mutant. Reflect on the function pointers: a behaviour probe could
// be satisfied by a hook that merely happens to agree.
func TestHooksAreNeverSetOutsideTheMutantsBuild(t *testing.T) {
	seams := []struct {
		name      string
		got, want any
	}{
		{"decodeBundle", decodeBundle, decodeBundledText},
		{"bundleSource", bundleSource, cachedBundle},
		{"unknownZone", unknownZone, errUnknownZone},
		{"zoneSource", zoneSource, bundledZoneSource},
		{"nulIndex", nulIndex, indexNUL},
	}
	for _, seam := range seams {
		if reflect.ValueOf(seam.got).Pointer() != reflect.ValueOf(seam.want).Pointer() {
			t.Errorf("seam %s is not its production implementation", seam.name)
		}
	}
	if name := os.Getenv(mutantEnv); name != "" {
		t.Errorf("%s=%q is set; a mutant is active", mutantEnv, name)
	}
}

func goBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("go toolchain not found at %s: %v", bin, err)
	}
	return bin
}

// runChildTests runs the named tests in a child `go test -tags mutants` with
// the mutant (empty for the control) active. It returns the combined output
// and whether the child exited zero. A timeout or a launch failure fails the
// parent test: neither is a kill nor a pass.
func runChildTests(t *testing.T, mutant, run string) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBinary(t), "test", "-tags", "mutants", "-count=1", "-run", run, ".")
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOTOOLCHAIN=local", mutantChildEnv+"=1", mutantEnv+"="+mutant)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("child go test timed out:\n%s", out)
	}
	if err == nil {
		return string(out), true
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("child go test did not launch: %v\n%s", err, out)
	}
	return string(out), false
}

func skipInMutantChild(t *testing.T) {
	t.Helper()
	if os.Getenv(mutantChildEnv) != "" {
		t.Skip("mutant driver does not recurse into its own child")
	}
}

func TestMutantControlIsGreen(t *testing.T) {
	skipInMutantChild(t)
	if out, ok := runChildTests(t, "", "."); !ok {
		t.Fatalf("unmutated control failed:\n%s", out)
	}
}

func TestNarrowingMutantsAreKilledByName(t *testing.T) {
	skipInMutantChild(t)
	for _, mutant := range timezonesMutants {
		t.Run(mutant.name, func(t *testing.T) {
			out, ok := runChildTests(t, mutant.name, "^"+mutant.witness+"$")
			if ok {
				t.Fatalf("SURVIVOR: witness %s passed against the mutant:\n%s", mutant.witness, out)
			}
			if strings.Contains(out, "[build failed]") || strings.Contains(out, "setup failed") || strings.Contains(out, "panic:") {
				t.Fatalf("mutant did not run, not a kill:\n%s", out)
			}
			if !strings.Contains(out, "--- FAIL: "+mutant.witness) {
				t.Fatalf("mutant failed without failing witness %s by name:\n%s", mutant.witness, out)
			}
		})
	}
}

// A leaked hook must be caught: with any mutant active the hooks witness has
// to fail by name, so a seam assigned outside the mutants build cannot pass
// unnoticed.
func TestHooksWitnessFailsWhenAnySeamIsSwapped(t *testing.T) {
	skipInMutantChild(t)
	for _, mutant := range timezonesMutants {
		t.Run(mutant.name, func(t *testing.T) {
			out, ok := runChildTests(t, mutant.name, "^"+hooksWitness+"$")
			if ok || !strings.Contains(out, "--- FAIL: "+hooksWitness) {
				t.Fatalf("hooks witness did not fail against the swapped seam:\n%s", out)
			}
		})
	}
}

func TestUnknownMutantNameIsNotASilentNoOp(t *testing.T) {
	skipInMutantChild(t)
	out, ok := runChildTests(t, "no such mutant", "^"+hooksWitness+"$")
	if ok || !strings.Contains(out, "unknown mutant") {
		t.Fatalf("unknown mutant name was accepted:\n%s", out)
	}
}
