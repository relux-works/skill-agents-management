package vendorplugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

func TestBuildLaunchWithEnvironmentSystemsAndModes(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	for _, tc := range []struct {
		runtime vendorplugin.RuntimeID
		model   vendorplugin.ModelID
		binary  string
	}{
		{"claude", "claude-opus-5", "claude"}, {"codex", "astra", "codex"}, {"pi-anthropic", "claude-opus-5", "pi"},
	} {
		t.Run(string(tc.runtime), func(t *testing.T) {
			dir := t.TempDir()
			writeStubBinary(t, dir, tc.binary)
			req := vendorplugin.SpawnRequest{Runtime: tc.runtime, Model: tc.model, Effort: "high", Env: []string{"PATH=" + dir, "UNOWNED=parent"}, Home: dir, Run: agentic.RunContext{RunID: "owned-test"}}
			binding, err := registry.ResolveRuntime(tc.runtime)
			if err != nil {
				t.Fatal(err)
			}
			var model vendorplugin.Model
			for _, m := range binding.Vendor.Models() {
				if m.ID == tc.model {
					model = m
				}
			}
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeManagedSession, agentic.LaunchModeInteractive} {
				t.Run(mode.String(), func(t *testing.T) {
					got, err := vendorplugin.BuildLaunchWithEnvironment(context.Background(), registry, req, mode)
					if !binding.System.Capabilities().SupportsMode(mode) {
						if !errors.Is(err, agentic.ErrUnsupportedLaunchMode) {
							t.Fatalf("unsupported mode: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					effective, err := binding.Vendor.Spawn(vendorplugin.SpawnContext{Runtime: binding, Model: model, Effort: req.Effort, Request: req})
					if err != nil {
						t.Fatal(err)
					}
					effective.Runtime = string(req.Runtime)
					effective.Vendor = string(binding.VendorID)
					effective, err = agentic.PrepareLaunchRequest(binding.System, effective, mode)
					if err != nil {
						t.Fatal(err)
					}
					effective.Model.ID = effective.Model.LaunchIdentity()
					effective.Model.AliasOf = ""
					want, err := binding.System.ChildEnv(nil, effective)
					if err != nil {
						t.Fatal(err)
					}
					sort.Strings(want)
					if !reflect.DeepEqual(got.OwnedEnv, want) {
						t.Fatalf("owned = %v; want %v", got.OwnedEnv, want)
					}
					legacy, err := vendorplugin.BuildLaunch(context.Background(), registry, req, mode)
					if err != nil {
						t.Fatal(err)
					}
					a, _ := json.Marshal(legacy)
					b, _ := json.Marshal(got.Plan)
					if string(a) != string(b) {
						t.Fatalf("legacy JSON changed: %s != %s", a, b)
					}
				})
			}
		})
	}
}
