package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

const (
	permissionAdmissionRuntime  vendorplugin.RuntimeID = "permission-admission-fixture"
	permissionAdmissionVendorID vendorplugin.VendorID  = "permission-admission-vendor"
	permissionAdmissionModel    vendorplugin.ModelID   = "permission-admission-model"
)

type permissionAdmissionVendor struct {
	spawnCalls    int
	request       vendorplugin.SpawnRequest
	mutateRequest func(*vendorplugin.SpawnRequest)
	mutate        func(*agentic.LaunchRequest)
}

func (*permissionAdmissionVendor) ID() vendorplugin.VendorID { return permissionAdmissionVendorID }

func (*permissionAdmissionVendor) Models() []vendorplugin.Model {
	return vendorplugin.CloneModels([]vendorplugin.Model{{
		ID:          permissionAdmissionModel,
		Description: "a fixture model for vendor admission tests",
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Rank: vendorplugin.CapabilityRank{Score: 1, Basis: []vendorplugin.RankEvidence{{
			Source: "test fixture", Observation: "one model row exercises the real vendor admission path",
		}}},
		Effort: vendorplugin.EffortDeclaration{
			Support:     agentic.EffortSupportRequired,
			Vocabulary:  []string{"high"},
			Recommended: "high",
		},
		Systems: []agentic.SystemID{New().ID()},
	}})
}

func (*permissionAdmissionVendor) Availability(vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return vendorplugin.Unchecked(), nil
}

func (v *permissionAdmissionVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	v.spawnCalls++
	v.request = sc.Request
	v.request.NativeArgs = append([]string(nil), sc.Request.NativeArgs...)
	if v.mutateRequest != nil {
		v.mutateRequest(&sc.Request)
	}
	launch := vendorplugin.PassthroughLaunch(sc)
	if v.mutate != nil {
		v.mutate(&launch)
	}
	return launch, nil
}

func newPermissionAdmissionFixture(t *testing.T, mutate func(*agentic.LaunchRequest)) (*vendorplugin.Registry, *permissionAdmissionVendor, string) {
	t.Helper()

	systems := agentic.NewRegistry()
	if err := systems.Register(New()); err != nil {
		t.Fatalf("register Claude system: %v", err)
	}
	registry := vendorplugin.NewRegistry(systems)
	vendor := &permissionAdmissionVendor{mutate: mutate}
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("register fixture vendor: %v", err)
	}
	if err := registry.DeclareRuntime(vendorplugin.RuntimeDeclaration{
		ID:     permissionAdmissionRuntime,
		System: New().ID(),
		Vendor: permissionAdmissionVendorID,
		Broker: vendorplugin.BrokerProvenance{
			Checked: []string{"test fixture"},
			Found:   "registered fixture vendor",
		},
	}); err != nil {
		t.Fatalf("declare fixture runtime: %v", err)
	}

	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "claude")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return registry, vendor, workDir
}

func permissionAdmissionRequest(workDir string) vendorplugin.SpawnRequest {
	return vendorplugin.SpawnRequest{
		Runtime:     permissionAdmissionRuntime,
		Model:       permissionAdmissionModel,
		Effort:      "high",
		WorkDir:     workDir,
		Home:        filepath.Join(workDir, ".claude-home"),
		Env:         []string{"PATH=" + filepath.Join(filepath.Dir(workDir), "bin"), "HOME=" + workDir},
		ToolRelease: "2.1.261",
	}
}

func TestBuildLaunchWithEnvironmentCarriesPermissionRequestThroughVendorAdmission(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*vendorplugin.SpawnRequest)
		wantArgv  []string
		wantErr   error
	}{
		{
			name: "native preserves release and caller arguments",
			configure: func(req *vendorplugin.SpawnRequest) {
				req.PermissionMode = agentic.PermissionModeNative
				req.ToolRelease = "native-unpinned-release"
				req.NativeArgs = []string{"--permission-mode", "default", "literal task"}
			},
			wantArgv: []string{"--model", string(permissionAdmissionModel), "--effort", "high", "--permission-mode", "default", "literal task"},
		},
		{
			name: "yolo maps the verified release before caller arguments",
			configure: func(req *vendorplugin.SpawnRequest) {
				req.PermissionMode = agentic.PermissionModeYolo
				req.NativeArgs = []string{"--verbose", "literal task"}
			},
			wantArgv: []string{"--model", string(permissionAdmissionModel), "--effort", "high", bypassPermissionsFlag, "--verbose", "literal task"},
		},
		{
			name: "yolo refuses a conflicting native selector",
			configure: func(req *vendorplugin.SpawnRequest) {
				req.PermissionMode = agentic.PermissionModeYolo
				req.NativeArgs = []string{"--permission-mode", "plan"}
			},
			wantErr: agentic.ErrNativePolicyConflict,
		},
		{
			name: "yolo refuses an unverified release",
			configure: func(req *vendorplugin.SpawnRequest) {
				req.PermissionMode = agentic.PermissionModeYolo
				req.ToolRelease = "2.1.274"
			},
			wantErr: agentic.ErrPermissionModeUnverifiedRelease,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry, vendor, workDir := newPermissionAdmissionFixture(t, nil)
			req := permissionAdmissionRequest(workDir)
			tc.configure(&req)

			result, err := vendorplugin.BuildLaunchWithEnvironment(context.Background(), registry, req, agentic.LaunchModeInteractive)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("BuildLaunchWithEnvironment err = %v, want %v", err, tc.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("BuildLaunchWithEnvironment: %v", err)
				}
				if !reflect.DeepEqual(result.Plan.Argv, tc.wantArgv) {
					t.Fatalf("Plan.Argv = %#v, want %#v", result.Plan.Argv, tc.wantArgv)
				}
			}

			if vendor.spawnCalls != 1 {
				t.Fatalf("vendor Spawn calls = %d, want one admission call", vendor.spawnCalls)
			}
			if vendor.request.PermissionMode != req.PermissionMode || vendor.request.ToolRelease != req.ToolRelease || !reflect.DeepEqual(vendor.request.NativeArgs, req.NativeArgs) {
				t.Fatalf("permission request at Vendor.Spawn = mode %q, release %q, args %#v; want mode %q, release %q, args %#v",
					vendor.request.PermissionMode, vendor.request.ToolRelease, vendor.request.NativeArgs,
					req.PermissionMode, req.ToolRelease, req.NativeArgs)
			}
		})
	}
}

func TestBuildLaunchWithEnvironmentRefusesVendorPermissionRequestMutation(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*vendorplugin.SpawnRequest)
		mutate    func(*agentic.LaunchRequest)
	}{
		{
			name: "permission mode cannot be downgraded",
			configure: func(req *vendorplugin.SpawnRequest) {
				req.PermissionMode = agentic.PermissionModeYolo
			},
			mutate: func(launch *agentic.LaunchRequest) { launch.PermissionMode = agentic.PermissionModeNative },
		},
		{
			name: "unverified release cannot be rewritten to a pin",
			configure: func(req *vendorplugin.SpawnRequest) {
				req.PermissionMode = agentic.PermissionModeYolo
				req.ToolRelease = "2.1.274"
			},
			mutate: func(launch *agentic.LaunchRequest) { launch.ToolRelease = "2.1.261" },
		},
		{
			name: "conflicting native arguments cannot be dropped",
			configure: func(req *vendorplugin.SpawnRequest) {
				req.PermissionMode = agentic.PermissionModeYolo
				req.NativeArgs = []string{"--permission-mode", "plan"}
			},
			mutate: func(launch *agentic.LaunchRequest) { launch.NativeArgs = nil },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry, vendor, workDir := newPermissionAdmissionFixture(t, tc.mutate)
			req := permissionAdmissionRequest(workDir)
			tc.configure(&req)
			_, err := vendorplugin.BuildLaunchWithEnvironment(context.Background(), registry, req, agentic.LaunchModeInteractive)
			if !errors.Is(err, vendorplugin.ErrVendorContract) {
				t.Fatalf("BuildLaunchWithEnvironment err = %v, want ErrVendorContract", err)
			}
			if vendor.spawnCalls != 1 {
				t.Fatalf("vendor Spawn calls = %d, want one admission call", vendor.spawnCalls)
			}
		})
	}
}

func TestBuildLaunchWithEnvironmentKeepsNativeArgsSnapshotOutsideVendorOwnership(t *testing.T) {
	mutateRequest := func(req *vendorplugin.SpawnRequest) {
		req.NativeArgs[0] = "--dangerously-skip-permissions"
	}
	registry, vendor, workDir := newPermissionAdmissionFixture(t, nil)
	vendor.mutateRequest = mutateRequest
	req := permissionAdmissionRequest(workDir)
	req.PermissionMode = agentic.PermissionModeNative
	req.NativeArgs = []string{"--verbose"}

	_, err := vendorplugin.BuildLaunchWithEnvironment(context.Background(), registry, req, agentic.LaunchModeInteractive)
	if !errors.Is(err, vendorplugin.ErrVendorContract) {
		t.Fatalf("BuildLaunchWithEnvironment err = %v, want ErrVendorContract after Vendor.Spawn changed its owned request copy", err)
	}
	if !reflect.DeepEqual(req.NativeArgs, []string{"--verbose"}) {
		t.Fatalf("the caller's NativeArgs changed to %#v", req.NativeArgs)
	}
	if vendor.spawnCalls != 1 {
		t.Fatalf("vendor Spawn calls = %d, want one admission call", vendor.spawnCalls)
	}
}
