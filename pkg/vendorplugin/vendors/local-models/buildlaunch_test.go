package localmodels

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file proves the SEAM between this vendor and the REAL pi system
// through the real vendorplugin.BuildLaunch entry point — not each package
// in isolation (pi's own suite already proves its Preflight table; this
// package's own suite already proves Spawn/Availability). It imports
// pkg/agentic/systems/pi directly (a normal, non-blank import, test-only):
// that package's init() registers "pi" into agentic.Default, which is
// harmless here because THIS package's test binary carries no
// "every compiled-in system needs its own smoke golden" guard — that guard
// lives only in internal/regress, and pi's own pinned-parity golden is
// exactly the open item (adversarial plan case 17) this vendor's Story does
// not resolve.

type countingStatusReader struct {
	status localruntime.Status
	err    error
	hang   bool
	calls  int
}

func (r *countingStatusReader) Status(ctx context.Context, _ localruntime.StatusQuery) (localruntime.Status, error) {
	r.calls++
	if r.hang {
		<-ctx.Done()
		return localruntime.Status{}, ctx.Err()
	}
	if r.err != nil {
		return localruntime.Status{}, r.err
	}
	return r.status, nil
}

// isolatedLocalQwenRegistry builds a registry carrying ONLY the real pi
// system (with the given fake StatusReader) and this vendor (with the given
// config), declaring local-qwen.
func isolatedLocalQwenRegistry(t *testing.T, reader localruntime.StatusReader, cfg Config) *vendorplugin.Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(pi.New(reader)); err != nil {
		t.Fatalf("registering pi: %v", err)
	}
	registry := vendorplugin.NewRegistry(systems)
	if err := registry.Register(New(cfg)); err != nil {
		t.Fatalf("registering local-models: %v", err)
	}
	decl := vendorplugin.RuntimeDeclaration{
		ID:     "local-qwen",
		System: "pi",
		Vendor: VendorID,
		Broker: vendorplugin.BrokerProvenance{Checked: []string{"test"}, Found: "test"},
	}
	if err := registry.DeclareRuntime(decl); err != nil {
		t.Fatalf("declaring local-qwen: %v", err)
	}
	return registry
}

func fakeAgentsInfraOnPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agents-infra"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing fake agents-infra: %v", err)
	}
	return dir
}

func localQwenSpawnRequest(t *testing.T) vendorplugin.SpawnRequest {
	return vendorplugin.SpawnRequest{
		Runtime: "local-qwen",
		Model:   "qwen-3.8-27b-mlx-8bit",
		Env:     []string{"PATH=" + fakeAgentsInfraOnPath(t)},
	}
}

// TestBuildLaunchAdmitsLocalQwenThroughTheRealPiPreflight is the end-to-end
// seam proof: a positively-absent broker admits, and the resulting Plan's
// argv/env carry what this vendor's real Spawn and pi's real Argv/ChildEnv
// build together.
func TestBuildLaunchAdmitsLocalQwenThroughTheRealPiPreflight(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
	registry := isolatedLocalQwenRegistry(t, reader, validConfig())

	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("StatusReader called %d times, want exactly 1", reader.calls)
	}
	if filepath.Base(plan.Binary) != "agents-infra" {
		t.Fatalf("plan.Binary = %q, want the agents-infra wrapper, not raw pi", plan.Binary)
	}
	wantArgv := []string{"pi", "--profile", "local-qwen", "--"}
	if len(plan.Argv) != len(wantArgv) {
		t.Fatalf("plan.Argv = %v, want %v", plan.Argv, wantArgv)
	}
	for i := range wantArgv {
		if plan.Argv[i] != wantArgv[i] {
			t.Fatalf("plan.Argv = %v, want %v", plan.Argv, wantArgv)
		}
	}
	foundEnv := false
	for _, entry := range plan.Env {
		if entry == "AGENTS_INFRA_CALLER_CWD=/Users/op/skill-agents-management" {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("plan.Env = %v; missing AGENTS_INFRA_CALLER_CWD contributed by this vendor's Spawn", plan.Env)
	}
}

// TestBuildLaunchRefusesLocalQwenWhenPreflightRefuses: a refusing Preflight
// stops BuildLaunch before agentic.BuildPlan.
func TestBuildLaunchRefusesLocalQwenWhenPreflightRefuses(t *testing.T) {
	reader := &countingStatusReader{status: localruntime.Status{BrokerState: "draining", BrokerSource: localruntime.SourceAttested}}
	registry := isolatedLocalQwenRegistry(t, reader, validConfig())

	if _, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeExec); err == nil {
		t.Fatal("BuildLaunch admitted a draining/attested broker")
	}
}

// TestBuildLaunchSkipsPreflightOnDryRun: dry-run mode must call the
// StatusReader ZERO times, driven with the real pi system.
func TestBuildLaunchSkipsPreflightOnDryRun(t *testing.T) {
	reader := &countingStatusReader{err: errors.New("must never be called on a dry run")}
	registry := isolatedLocalQwenRegistry(t, reader, validConfig())

	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, localQwenSpawnRequest(t), agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch(dry-run): %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("StatusReader called %d times on a dry run, want 0", reader.calls)
	}
	if plan.System != "pi" {
		t.Fatalf("plan.System = %q, want pi", plan.System)
	}
}

// TestBuildLaunchPreflightTimeoutRefusesLocalQwen proves the
// context.WithTimeout BuildLaunch applies is real for this seam: a
// StatusReader that never returns must not hang BuildLaunch forever.
func TestBuildLaunchPreflightTimeoutRefusesLocalQwen(t *testing.T) {
	registry := isolatedLocalQwenRegistry(t, &countingStatusReader{hang: true}, validConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := vendorplugin.BuildLaunch(ctx, registry, localQwenSpawnRequest(t), agentic.LaunchModeExec)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("BuildLaunch admitted despite a StatusReader that never returns")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("BuildLaunch took %v to return", elapsed)
	}
}
