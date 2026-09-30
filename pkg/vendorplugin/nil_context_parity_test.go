package vendorplugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// Regression for nil-carrier parity across the entire declared launch surface,
// including Muse's interactive postures. Models (aliases included) and modes
// come from the registries, rather than a representative model or a mode list.
func TestBuildLaunchNilContextEveryRegisteredModelAndMode(t *testing.T) {
	testNilContextEveryRegisteredModelAndMode(t, vendorplugin.BuildLaunch)
}

func TestBuildLaunchWithEnvironmentNilContextEveryRegisteredModelAndMode(t *testing.T) {
	testNilContextEveryRegisteredModelAndMode(t, buildVendorContextWithEnvironment)
}

func testNilContextEveryRegisteredModelAndMode(t *testing.T, build vendorContextBuilder) {
	fixture := nilContextParityFixture(t)
	coveredSystems := map[agentic.SystemID]bool{}
	for _, declaration := range vendorplugin.Default.RuntimeDeclarations() {
		system, ok := agentic.Default.Lookup(declaration.System)
		if !ok {
			t.Fatalf("declared system %s is not registered", declaration.System)
		}
		coveredSystems[system.ID()] = true
		models := declaration.Models
		if declaration.Vendor != "" {
			vendor, ok := vendorplugin.Default.Lookup(declaration.Vendor)
			if !ok {
				t.Fatalf("declared vendor %s is not registered", declaration.Vendor)
			}
			models = vendor.Models()
		}
		modelCount := 0
		for _, model := range models {
			if !model.DrivenBy(declaration.System) {
				continue // The registry explicitly excludes this runtime/model pair.
			}
			modelCount++
			for _, mode := range system.Capabilities().LaunchModes {
				for _, posture := range nilContextParityPostures(mode) {
					name := string(declaration.ID) + "/" + string(model.ID) + "/" + mode.String() + "/" + string(posture)
					t.Run(name, func(t *testing.T) {
						req := nilContextParityRequest(fixture, system.ID(), mode, posture)
						req.Runtime, req.Model, req.Effort = declaration.ID, model.ID, model.Effort.Recommended
						legacy := preCarrierNilContextRequest(declaration.System, model, req)
						legacy.Runtime, legacy.Vendor = string(declaration.ID), string(declaration.Vendor)
						got, gotErr := build(context.Background(), vendorplugin.Default, req, mode)
						// Native harness admission precedes BuildPlan. Preserve its
						// refusals for catalog rows/efforts it cannot actually run.
						if admitter, ok := system.(agentic.EffortAdmitter); ok {
							_, err := admitter.AdmitEffort(legacy.Vendor, legacy.Model.LaunchIdentity(), req.Effort, model.Effort.Vocabulary)
							if err != nil {
								assertNilContextParityRefusal(t, got, gotErr, err)
								if !errors.Is(gotErr, vendorplugin.ErrEffortNotNativelySupported) {
									t.Fatalf("lost vendor effort refusal: %v", gotErr)
								}
								return
							}
						}
						want, wantErr := agentic.BuildPlan(agentic.Default, legacy, mode)
						if wantErr != nil {
							assertNilContextParityRefusal(t, got, gotErr, wantErr)
							return
						}
						if gotErr != nil {
							t.Fatal(gotErr)
						}
						assertNilContextPlanBytes(t, got, want)
					})
				}
			}
		}
		if modelCount == 0 {
			t.Fatalf("runtime %s has no driven model", declaration.ID)
		}
	}
	for _, id := range agentic.Default.IDs() {
		if !coveredSystems[id] {
			t.Errorf("registered plugin %s has no parity cases", id)
		}
	}
}

// Echo is a Muse pseudo-model supported by the system grammar, not a vendor
// catalog row. Exercise its public request projection and production planner;
// inventing a registered broker/model would change the admission contract.
func TestPassthroughNilContextMuseEchoEveryDeclaredMode(t *testing.T) {
	system, ok := agentic.Default.Lookup("muse")
	if !ok {
		t.Fatal("Muse plugin is not registered")
	}
	fixture := nilContextParityFixture(t)
	model := vendorplugin.Model{ID: "echo"}
	for _, mode := range system.Capabilities().LaunchModes {
		for _, posture := range nilContextParityPostures(mode) {
			t.Run(mode.String()+"/"+string(posture), func(t *testing.T) {
				req := nilContextParityRequest(fixture, system.ID(), mode, posture)
				legacy := preCarrierNilContextRequest(system.ID(), model, req)
				projected := vendorplugin.PassthroughLaunch(vendorplugin.SpawnContext{
					Runtime: vendorplugin.Runtime{SystemID: system.ID()}, Model: model, Request: req,
				})
				got, err := agentic.BuildPlan(agentic.Default, projected, mode)
				if err != nil {
					t.Fatal(err)
				}
				want, err := agentic.BuildPlan(agentic.Default, legacy, mode)
				if err != nil {
					t.Fatal(err)
				}
				assertNilContextPlanBytes(t, got, want)
				if !reflect.DeepEqual(projected, legacy) {
					t.Fatal("nil carrier changed echo request")
				}
			})
		}
	}
}

type nilContextFixture struct{ dir, prompt string }

func nilContextParityFixture(t *testing.T) nilContextFixture {
	t.Helper()
	dir := t.TempDir()
	// Executable fixtures cover binary resolution only; no agent is launched.
	for _, name := range []string{"codex", "claude", "qwen", "gemini", "muse", "pi"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	prompt := filepath.Join(dir, "assignment.md")
	if err := os.WriteFile(prompt, []byte("assignment"), 0600); err != nil {
		t.Fatal(err)
	}
	return nilContextFixture{dir, prompt}
}

func nilContextParityPostures(mode agentic.LaunchMode) []agentic.PermissionMode {
	if mode == agentic.LaunchModeInteractive {
		return []agentic.PermissionMode{"", agentic.PermissionModeNative, agentic.PermissionModeYolo}
	}
	return []agentic.PermissionMode{""}
}

func nilContextParityRequest(f nilContextFixture, system agentic.SystemID, mode agentic.LaunchMode, posture agentic.PermissionMode) vendorplugin.SpawnRequest {
	req := vendorplugin.SpawnRequest{Prompt: []byte("assignment"), PromptPath: f.prompt, Home: f.dir, WorkDir: f.dir, Env: []string{"PATH=" + f.dir}, PermissionMode: posture}
	if mode == agentic.LaunchModeInteractive {
		req.Prompt, req.PromptPath = nil, ""
		// Release pins are fixtures for permission grammar, not a model/mode
		// case list. Pi's yolo remains a typed refusal at its verified release.
		req.ToolRelease = map[agentic.SystemID]string{
			"claude-code": "2.1.261", "codex": "0.153.2", "muse": "1.4.1", "pi-native": "0.84.2",
		}[system]
	}
	return req
}

// Frozen v0.5.35 projection: deliberately never read the new carrier fields
// or call PassthroughLaunch to compute the expected request.
func preCarrierNilContextRequest(system agentic.SystemID, model vendorplugin.Model, req vendorplugin.SpawnRequest) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System: system, Model: model.Launchable(), Effort: req.Effort,
		PromptPath: req.PromptPath, Prompt: req.Prompt, WorkDir: req.WorkDir, Home: req.Home,
		LocalProvider: req.LocalProvider, Env: req.Env, Run: req.Run, Profile: req.Profile,
		Goal: req.Goal, Budget: req.Budget, ServiceTier: req.ServiceTier, Composition: req.Composition,
		Deadline: req.Deadline, PermissionMode: req.PermissionMode, ToolRelease: req.ToolRelease,
		NativeArgs: append([]string(nil), req.NativeArgs...),
	}
}

func assertNilContextPlanBytes(t *testing.T, got, want agentic.Plan) {
	t.Helper()
	a, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || string(a) != string(b) {
		t.Fatalf("nil context changed plan bytes:\ngot %s\nwant %s", a, b)
	}
	if _, ok := got.CuratorContextProvenanceSnapshot(); ok {
		t.Fatal("nil context acquired Curator attestation")
	}
}

func assertNilContextParityRefusal(t *testing.T, plan agentic.Plan, got, want error) {
	t.Helper()
	// Compare the original typed cause through errors.Is, retaining wrapper
	// freedom at the vendor API. This is refusal parity, never a successful plan.
	cause := want
	for errors.Unwrap(cause) != nil {
		cause = errors.Unwrap(cause)
	}
	if !errors.Is(got, cause) || !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("nil context changed typed refusal: got %v (%#v), want %v", got, plan, want)
	}
	t.Logf("unchanged typed refusal: %v", cause)
}
