package codex

import (
	"context"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// Source mutants keep the validator call and all policy gates present. They
// add one discarded-result read for unowned model keys only, before validation. No
// mutation flag or test-only branch is linked into the production planner.
// Copying the module also makes the AST guard inspect exactly what Go compiles;
// a compiler overlay alone would leave on-disk source guards reading a control.
func TestCarriedSettingsReadOrderMutantsKilledByName(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := gosources.Root(cwd)
	if err != nil {
		t.Fatal(err)
	}
	candidate := t.TempDir()
	copySettingsMutantModule(t, root, candidate)
	path := filepath.Join(candidate, "pkg/agentic/systems/codex/codex.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	anchor := "\treturn validateCarriedLocalSettingsPrePlan(req.LocalProvider.Settings, req.LocalProvider.ID)"
	if strings.Count(string(source), anchor) != 1 {
		t.Fatal("pre-plan validator site is not unique")
	}
	runReadOrderMutant(t, candidate, "control", "", "")
	for _, mutant := range []struct{ name, expression, imports, witness, diagnostic string }{
		{"ignored-model-config-read", `planningio.Default.ReadFile(req.Home + "/config.toml")`, "", "TestCarriedSettingsRefuseBeforeReads", "settings refusal performed reads"},
		{"ignored-model-catalog-read", `readRegularCatalog(req.Home + "/catalog.local.json")`, "", "TestCarriedSettingsRefuseBeforeReads", "settings refusal performed reads"},
		{"ignored-model-resolver-read", `resolveBinary(req.Env)`, "", "TestCarriedSettingsRefuseBeforeReads", "settings refusal performed reads"},
		// Exact mechanism from review rev2: a raw OS read bypasses the
		// recorder, so the architecture guard, not error precedence, kills it.
		{"ignored-model-raw-read", `os.ReadFile(req.Home + "/config.toml")`, "\t\"os\"\n", "TestPlannerReadsUseObservableBoundary", "unobserved planner read os.ReadFile"},
	} {
		t.Run(mutant.name, func(t *testing.T) {
			inserted := "\tfor _, setting := range req.LocalProvider.Settings {\n\t\tif setting.Key == \"model_reasoning_summary\" {\n\t\t\t_, _ = " + mutant.expression + "\n\t\t}\n\t}\n" + anchor
			mutated := strings.Replace(string(source), anchor, inserted, 1)
			if mutant.imports != "" {
				mutated = strings.Replace(mutated, "import (\n", "import (\n"+mutant.imports, 1)
			}
			formatted, err := format.Source([]byte(mutated))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, formatted, 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(path, source, 0600); err != nil {
					t.Error(err)
				}
			})
			runReadOrderMutant(t, candidate, mutant.name, mutant.witness, mutant.diagnostic)
		})
	}
}

func runReadOrderMutant(t *testing.T, dir, mutant, witness, diagnostic string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pattern := "^(TestCarriedSettingsRefuseBeforeReads|TestCarriedSettingsReadRecorderObservesAdmittedPlan|TestPlannerReadsUseObservableBoundary)$"
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "./pkg/agentic/systems/codex", "-count=1", "-run", pattern, "-v")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out: %s", mutant, output)
	}
	if witness == "" {
		if err != nil {
			t.Fatalf("control failed: %v\n%s", err, output)
		}
		t.Log("control exit 0")
		return
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "--- FAIL: "+witness) || !strings.Contains(string(output), diagnostic) || strings.Contains(string(output), "[build failed]") || strings.Contains(string(output), "panic:") {
		t.Fatalf("%s did not fail its named witness: %v\n%s", mutant, err, output)
	}
	t.Logf("%s killed by %s; child exit 1\n%s", mutant, witness, output)
}

func copySettingsMutantModule(t *testing.T, source, dest string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel != "." && (strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || entry.Name() == "vendor" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0700)
		}
		// Symlinked instruction files are irrelevant to Go compilation.
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, rel), data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// These held surfaces stay in the coverage map during rework. Each mutation
// weakens one comparison/transport obligation and is run against its original
// public-entry witness, including the reviewer's byte-snapshot regression.
func TestCarriedSettingsContractMutantsKilledByName(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := gosources.Root(cwd)
	if err != nil {
		t.Fatal(err)
	}
	candidate := t.TempDir()
	copySettingsMutantModule(t, root, candidate)
	for _, mutant := range []struct{ name, file, before, after, witness string }{
		{
			"drop-arbitrary-model-prefix", "provider_settings.go",
			`if owned[setting.Key] {`, `if owned[setting.Key] || strings.HasPrefix(setting.Key, "model_") {`,
			"TestCarriedSettingsUnownedModelKeysStillRefuse",
		},
		{
			"snapshot-ignores-same-name-bytes", "provider_settings_test.go",
			`hex.EncodeToString(sum[:])`, `hex.EncodeToString(sum[:0])`,
			"TestReviewSnapshotSeesOverwrite",
		},
		{
			"seal-admits-one-carried-value-flip", "catalog_launch.go",
			`!slices.Equal(plan.Argv, seal.argv)`,
			`!slices.EqualFunc(plan.Argv, seal.argv, func(a, b string) bool { return a == b || a == "hide_agent_reasoning=false" && b == "hide_agent_reasoning=true" })`,
			"TestCarriedSettingsSealStillRefusesChangedArgv",
		},
		{
			"omit-one-carried-output-setting", "provider_settings.go",
			`argv = append(argv, "-c", key+"="+seen[key])`,
			`if key != "show_raw_agent_reasoning" { argv = append(argv, "-c", key+"="+seen[key]) }`,
			"TestCarriedSettingsReachArgvAndVerify",
		},
	} {
		t.Run(mutant.name, func(t *testing.T) {
			path := filepath.Join(candidate, "pkg/agentic/systems/codex", mutant.file)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(source), mutant.before) != 1 {
				t.Fatal("mutation site is not unique")
			}
			runSettingsContractMutant(t, candidate, mutant.witness, true)
			changed, err := format.Source([]byte(strings.Replace(string(source), mutant.before, mutant.after, 1)))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, changed, 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(path, source, 0600); err != nil {
					t.Error(err)
				}
			})
			runSettingsContractMutant(t, candidate, mutant.witness, false)
		})
	}
}

func runSettingsContractMutant(t *testing.T, dir, witness string, control bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "./pkg/agentic/systems/codex", "-count=1", "-run", "^"+witness+"$", "-v")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("timed out: %s", output)
	}
	if control {
		if err != nil {
			t.Fatalf("control failed: %v\n%s", err, output)
		}
		t.Logf("%s control exit 0", witness)
		return
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "--- FAIL: "+witness) || strings.Contains(string(output), "[build failed]") || strings.Contains(string(output), "panic:") {
		t.Fatalf("mutant did not fail %s: %v\n%s", witness, err, output)
	}
	t.Logf("killed by %s; child exit 1\n%s", witness, output)
}
