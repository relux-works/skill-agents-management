package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestCatalogSnapshotBindsExecutedBytesAcrossOperatorSwap(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			home, wd := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := catalogRequest(t, home, wd, "low")
			snap := mustSnapshot(t, req)
			operatorPath := filepath.Join(home, "catalog.local.json")
			data, _ := os.ReadFile(operatorPath)
			var catalog map[string]any
			json.Unmarshal(data, &catalog)
			catalog["models"].([]any)[0].(map[string]any)["use_responses_lite"] = true
			changed, _ := json.Marshal(catalog)
			if err := os.WriteFile(operatorPath, changed, 0600); err != nil {
				t.Fatal(err)
			}
			plan, err := buildCodexPlanForMode(t, withSnapshot(req, snap), mode)
			if err != nil {
				t.Fatal(err)
			}
			path := effectiveCatalogPath(t, plan)
			if path == operatorPath {
				t.Fatal("exec still reads mutable operator catalog")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, data) || fmt.Sprintf("%x", sha256.Sum256(got)) != snap.CatalogDigest() {
				t.Fatal("executed catalog is not snapshotted bytes")
			}
			info, _ := os.Stat(path)
			dir, _ := os.Stat(filepath.Dir(path))
			if info.Mode().Perm() != 0400 || dir.Mode().Perm() != 0700 || !strings.Contains(filepath.Base(path), snap.CatalogDigest()) {
				t.Fatal("catalog artifact is not private and content-addressed")
			}
			if _, err := buildCodexPlanForMode(t, req, mode); !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
				t.Fatalf("ID swap control: %v", err)
			}
		})
	}
}

func TestIDPlanBindsBytesAndRefusesArtifactTamperBeforeExec(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		t.Run(fmt.Sprintf("snapshot=%v", snapshot), func(t *testing.T) {
			home, wd := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			// Distinct bytes prevent interference with the process-wide artifact cache.
			raw, _ := os.ReadFile(filepath.Join(home, "catalog.local.json"))
			var catalog map[string]any
			json.Unmarshal(raw, &catalog)
			catalog["models"].([]any)[0].(map[string]any)["description"] = wd
			raw, _ = json.Marshal(catalog)
			os.WriteFile(filepath.Join(home, "catalog.local.json"), raw, 0600)
			req := catalogRequest(t, home, wd, "low")
			if snapshot {
				req = withSnapshot(req, mustSnapshot(t, req))
			}
			plan, err := buildCodexPlan(t, req)
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(wd, "started")
			if err := os.WriteFile(plan.Binary, []byte("#!/bin/sh\n : > '"+marker+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			// A swap after ID validation cannot affect argv's launch-owned copy.
			if err := os.WriteFile(filepath.Join(home, "catalog.local.json"), []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(plan.Binary, plan.Argv...)
			command.Env = plan.Env
			command.Dir = wd
			if err := startVerifiedCatalogCommand(plan, command); err != nil {
				t.Fatalf("operator swap changed validated launch: %v", err)
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
			os.Remove(marker)
			path := effectiveCatalogPath(t, plan)
			os.Chmod(path, 0600)
			os.WriteFile(path, []byte{}, 0600)
			os.Chmod(path, 0400)
			t.Cleanup(func() { os.Chmod(path, 0600); os.WriteFile(path, raw, 0600); os.Chmod(path, 0400) })
			command = exec.Command(plan.Binary, plan.Argv...)
			err = startVerifiedCatalogCommand(plan, command)
			if !errors.Is(err, agentic.ErrLocalProviderConflicting) || command.Process != nil {
				t.Fatalf("tampered file was launched or not typed: %v", err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("child started on refusal")
			}
		})
	}
}

func TestLocalCatalogNonRegularAndOversizeRefuseWithoutBlocking(t *testing.T) {
	for _, kind := range []string{"fifo", "device", "socket", "directory", "oversize", "symlink_fifo", "dangling_symlink"} {
		t.Run(kind, func(t *testing.T) {
			home, wd := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			path := filepath.Join(home, "catalog.local.json")
			os.Remove(path)
			var listener net.Listener
			switch kind {
			case "fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "device":
				if err := os.Symlink("/dev/null", path); err != nil {
					t.Fatal(err)
				}
			case "socket":
				var err error
				socketDir, e := os.MkdirTemp("", "cx-")
				if e != nil {
					t.Fatal(e)
				}
				defer os.RemoveAll(socketDir)
				socketPath := filepath.Join(socketDir, "socket")
				if e := os.Symlink(socketPath, path); e != nil {
					t.Fatal(e)
				}
				listener, err = net.Listen("unix", socketPath)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
			case "directory":
				os.Mkdir(path, 0700)
			case "oversize":
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				f.Truncate(maxCatalogBytes + 1)
				f.Close()
			case "symlink_fifo":
				target := filepath.Join(home, "pipe")
				syscall.Mkfifo(target, 0600)
				os.Symlink(target, path)
			case "dangling_symlink":
				os.Symlink(filepath.Join(home, "missing"), path)
			}
			req := catalogRequest(t, home, wd, "low")
			ch := make(chan error, 1)
			go func() { _, err := buildCodexPlan(t, req); ch <- err }()
			select {
			case err := <-ch:
				var r *agentic.LocalProviderRefusal
				if !errors.As(err, &r) || (kind != "dangling_symlink" && r.Kind != agentic.LocalProviderReadFailed) {
					t.Fatalf("no typed refusal: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("nonregular catalog read blocked")
			}
			if _, err := ReadProviderSnapshot(req); err == nil {
				t.Fatal("constructor admitted unsafe input")
			}
		})
	}
}

func TestLocalCatalogSymlinkToRegularFileAccepted(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	path := filepath.Join(home, "catalog.local.json")
	target := filepath.Join(home, "target.json")
	os.Rename(path, target)
	os.Symlink(target, path)
	req := catalogRequest(t, home, wd, "low")
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(target)
	if _, err := os.ReadFile(effectiveCatalogPath(t, plan)); err != nil {
		t.Fatal("launch still depends on symlink target")
	}
}

func TestLocalCatalogNativeInvalidWholeCatalogRefuses(t *testing.T) {
	for _, bad := range []string{"missing_shell_type", "bad_shell_type", "missing_truncation_policy", "bad_priority", "bad_nested_message", "invalid_unselected_row"} {
		t.Run(bad, func(t *testing.T) {
			home, wd := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			path := filepath.Join(home, "catalog.local.json")
			raw, _ := os.ReadFile(path)
			var catalog map[string]any
			json.Unmarshal(raw, &catalog)
			row := catalog["models"].([]any)[0].(map[string]any)
			switch bad {
			case "missing_shell_type":
				delete(row, "shell_type")
			case "bad_shell_type":
				row["shell_type"] = "invalid"
			case "missing_truncation_policy":
				delete(row, "truncation_policy")
			case "bad_priority":
				row["priority"] = 1.5
			case "bad_nested_message":
				row["model_messages"] = map[string]any{"tools": map[string]any{"code_mode": map[string]any{"exec": map[string]any{"description": false}}}}
			case "invalid_unselected_row":
				catalog["models"] = append(catalog["models"].([]any), map[string]any{"slug": "other"})
			}
			raw, _ = json.Marshal(catalog)
			os.WriteFile(path, raw, 0600)
			req := catalogRequest(t, home, wd, "low")
			if _, err := buildCodexPlan(t, req); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
				t.Fatalf("native-invalid launch admitted: %v", err)
			}
			if _, err := ReadProviderSnapshot(req); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
				t.Fatalf("snapshot parity: %v", err)
			}
		})
	}
}

func TestLocalCatalogFixtureLoadsInNativeCodex(t *testing.T) {
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	home, wd := t.TempDir(), t.TempDir()
	env := append(filterEnvKeys(os.Environ(), "HOME", "CODEX_HOME"), "HOME="+home, "CODEX_HOME="+home)
	version := exec.CommandContext(ctx, binary, "--version")
	version.Env = env
	output, err := version.Output()
	if err != nil || !strings.Contains(string(output), "0.159.0") {
		t.Skip("native check pins Codex 0.159.0")
	}
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := catalogRequest(t, home, wd, "low")
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "debug", "models", "-c", "model_catalog_json="+strconv.Quote(effectiveCatalogPath(t, plan)))
	command.Env = env
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native loading: %v: %.2048s", err, out)
	}
}

func TestCatalogExecSealRefusesMalformedPin(t *testing.T) {
	plan := agentic.Plan{Argv: []string{"-c", "model_catalog_json=not-a-quoted-path"}}
	if _, err := New().SealExecPlan(plan); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
		t.Fatalf("malformed native pin admitted: %v", err)
	}
}

func TestLocalCatalogArtifactIORefusals(t *testing.T) {
	// This sequential test runs before parallel tests resume; no launches exist
	// while it exercises process-owned storage failure, then restores the root.
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	raw, _ := os.ReadFile(filepath.Join(home, "catalog.local.json"))
	var catalog map[string]any
	json.Unmarshal(raw, &catalog)
	catalog["models"].([]any)[0].(map[string]any)["description"] = "artifact-io"
	raw, _ = json.Marshal(catalog)
	os.WriteFile(filepath.Join(home, "catalog.local.json"), raw, 0600)
	req := catalogRequest(t, home, wd, "low")
	previous := launchCatalogs.root
	previousTemp := os.Getenv("TMPDIR")
	defer func() { launchCatalogs.root = previous }()
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("not a directory"), 0600)
	t.Setenv("TMPDIR", blocker)
	launchCatalogs.root = ""
	if _, err := buildCodexPlan(t, req); !errors.Is(err, agentic.ErrLocalProviderReadFailed) {
		t.Fatalf("temp directory failure not typed: %v", err)
	}
	launchCatalogs.root = blocker
	if _, err := buildCodexPlan(t, req); !errors.Is(err, agentic.ErrLocalProviderReadFailed) {
		t.Fatalf("creation failure not typed: %v", err)
	}
	launchCatalogs.root = previous
	t.Setenv("TMPDIR", previousTemp)
	// Existing artifact replacements are refused before a process starts.
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	path := effectiveCatalogPath(t, plan)
	os.Chmod(path, 0600)
	t.Cleanup(func() { os.Chmod(path, 0400) })
	if _, err := buildCodexPlan(t, req); !errors.Is(err, agentic.ErrLocalProviderReadFailed) {
		t.Fatalf("writable existing copy admitted: %v", err)
	}
}

func TestCatalogExecSealRefusesChangedArgvAndPermissions(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	// A unique catalog keeps permission attacks isolated from other plans.
	path := filepath.Join(home, "catalog.local.json")
	data, _ := os.ReadFile(path)
	data = append(data, '\n', '\n', '\n')
	os.WriteFile(path, data, 0600)
	req := catalogRequest(t, home, wd, "low")
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.Argv = append([]string(nil), plan.Argv...)
	changed.Argv = append(changed.Argv, "-c", `model_catalog_json="forged.json"`)
	command := exec.Command(changed.Binary, changed.Argv...)
	if err := startVerifiedCatalogCommand(changed, command); !errors.Is(err, agentic.ErrLocalProviderConflicting) || command.Process != nil {
		t.Fatalf("changed argv admitted: %v", err)
	}
	artifact := effectiveCatalogPath(t, plan)
	dir := filepath.Dir(artifact)
	for _, kind := range []string{"writable", "missing", "symlink", "public_directory"} {
		t.Run(kind, func(t *testing.T) {
			original, _ := os.ReadFile(artifact)
			switch kind {
			case "writable":
				os.Chmod(artifact, 0600)
			case "missing":
				os.Remove(artifact)
			case "symlink":
				os.Remove(artifact)
				os.Symlink(path, artifact)
			case "public_directory":
				os.Chmod(dir, 0755)
			}
			defer func() { os.Chmod(dir, 0700); os.Remove(artifact); os.WriteFile(artifact, original, 0400) }()
			command := exec.Command(plan.Binary, plan.Argv...)
			if err := startVerifiedCatalogCommand(plan, command); !errors.Is(err, agentic.ErrLocalProviderReadFailed) || command.Process != nil {
				t.Fatalf("unsafe artifact admitted: %v", err)
			}
		})
	}
}

// Empty native catalogs are valid schema but absent selected-model metadata.
// Both public paths must retain the honest absence kind before child launch.
func TestLocalCatalogEmptyNativeCatalogRefusesHonestAbsence(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			home, wd := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			writeLocalCatalogRaw(t, home, "catalog.local.json", []byte(`{"models":[]}`))
			req := catalogRequest(t, home, wd, "low")
			_, idErr := buildCodexPlanForMode(t, req, mode)
			_, snapshotErr := ReadProviderSnapshot(req)
			for _, err := range []error{idErr, snapshotErr} {
				var refusal *agentic.LocalProviderRefusal
				if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderAbsent {
					t.Fatalf("empty catalog metadata: %v, want typed absent", err)
				}
			}
		})
	}
}

// Process ownership stays with the consumer; tests exercise that same contract.
func startVerifiedCatalogCommand(plan agentic.Plan, command *exec.Cmd) error {
	if err := plan.VerifyBeforeExec(); err != nil {
		return err
	}
	return command.Start()
}

func TestCatalogVerifyBeforeExecRefusesChangedBinary(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
	plan.Binary += "-changed"
	if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("changed binary admitted: %v", err)
	}
}

func TestCatalogVerifyBeforeExecDryRunRefusesTamperedBytes(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	path := filepath.Join(home, "catalog.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Unique valid bytes avoid sharing the process-wide artifact with other tests.
	data = append(data, []byte("\n\n\n\n\n")...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := buildCodexPlanForMode(t, catalogRequest(t, home, wd, "low"), agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatal(err)
	}
	artifact := effectiveCatalogPath(t, plan)
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artifact, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artifact, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(artifact, 0600); os.WriteFile(artifact, data, 0600); os.Chmod(artifact, 0400) })
	if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("tampered dry-run artifact admitted: %v", err)
	}
}
