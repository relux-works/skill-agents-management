package vendorplugin_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

type curatorVendorCase struct {
	runtime                   vendorplugin.RuntimeID
	model                     vendorplugin.ModelID
	environment, homeVariable string
}

var curatorVendorCases = []curatorVendorCase{
	{"codex", "gpt-6-astra", "codex_cli", "CODEX_HOME"},
	{"claude", "claude-opus-5-5", "claude_code", "CLAUDE_CONFIG_DIR"},
}

func vendorCuratorRequest(t *testing.T, tc curatorVendorCase) vendorplugin.SpawnRequest {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, string(tc.runtime)), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	prompt := filepath.Join(home, "prompt.md")
	if err := os.WriteFile(prompt, []byte("Curator instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	mcp := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFlag, Flag: "-p", Argument: agentic.CuratorArgumentName, Name: "curator-mcp"}
	systemPrompt := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "model_instructions_file", Semantics: agentic.CuratorSystemPromptReplace}
	if tc.runtime == "claude" {
		mcp = agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFlag, Flag: "--mcp-config", Argument: agentic.CuratorArgumentPath, With: []string{"--strict-mcp-config"}}
		systemPrompt = agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFlag, Flag: "--append-system-prompt-file", Argument: agentic.CuratorArgumentPath, Semantics: agentic.CuratorSystemPromptAppend}
	}
	return vendorplugin.SpawnRequest{
		Runtime: tc.runtime, Model: tc.model, Effort: "high", Home: home, WorkDir: home,
		Prompt: []byte("assignment"), PromptPath: prompt, Env: []string{"PATH=" + bin},
		Context: &agentic.CuratorContext{
			Revision: agentic.CuratorLaunchFragmentV1, Environment: tc.environment,
			Profile:      agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
			Precedence:   agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
			Env:          map[string]string{tc.homeVariable: home},
			MCP:          &agentic.CuratorMCPContext{Path: filepath.Join(home, "mcp.json"), EnvNames: []string{"DOCS_TOKEN"}, Channels: []agentic.CuratorChannelDescriptor{mcp}},
			SystemPrompt: &agentic.CuratorSystemPromptContext{Path: prompt, Intent: systemPrompt.Semantics, Channels: []agentic.CuratorChannelDescriptor{systemPrompt}},
		},
	}
}

func TestBuildLaunchCuratorProvenance(t *testing.T) {
	testVendorCuratorProvenance(t, vendorplugin.BuildLaunch)
}

func TestBuildLaunchWithEnvironmentCuratorProvenance(t *testing.T) {
	testVendorCuratorProvenance(t, buildVendorContextWithEnvironment)
}

func testVendorCuratorProvenance(t *testing.T, build vendorContextBuilder) {
	for _, tc := range curatorVendorCases {
		t.Run(string(tc.runtime), func(t *testing.T) {
			req := vendorCuratorRequest(t, tc)
			plan, err := build(context.Background(), vendorplugin.Default, req, agentic.LaunchModeExec)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("BuildLaunch lost Curator provenance")
			}
			fragment := agentic.CuratorFragmentIdentity{
				Revision: req.Context.Revision, Environment: req.Context.Environment, Profile: req.Context.Profile, Precedence: req.Context.Precedence,
				Env: req.Context.Env, MCP: req.Context.MCP, PathPrepend: req.Context.PathPrepend,
				SystemPrompt: &agentic.CuratorSystemPromptFragment{Path: req.Context.SystemPrompt.Path, Channels: req.Context.SystemPrompt.Channels},
			}
			if got.ProfileName != req.Context.Profile.Name || got.LockSHA256 != req.Context.Profile.LockSHA256 || got.ManagedHome != req.Home || got.ManagedHomeVariable != tc.homeVariable || got.SystemPromptIntent != req.Context.SystemPrompt.Intent {
				t.Fatalf("wrong provenance: %#v", got)
			}
			wantJSON, err := json.Marshal(fragment)
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, err := json.Marshal(got.Fragment)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Fragment, fragment) || sha256.Sum256(gotJSON) != sha256.Sum256(wantJSON) {
				t.Fatalf("full fragment digest changed: %s != %s", gotJSON, wantJSON)
			}
			// Reuse binds the entire previous fragment, not only its profile and home.
			req.Context.ExpectedProvenance = &got
			if _, err := build(context.Background(), vendorplugin.Default, req, agentic.LaunchModeExec); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildLaunchCuratorTypedRefusals(t *testing.T) {
	testVendorCuratorTypedRefusals(t, vendorplugin.BuildLaunch)
}

func TestBuildLaunchWithEnvironmentCuratorTypedRefusals(t *testing.T) {
	testVendorCuratorTypedRefusals(t, buildVendorContextWithEnvironment)
}

func testVendorCuratorTypedRefusals(t *testing.T, build vendorContextBuilder) {
	for _, tc := range curatorVendorCases {
		t.Run(string(tc.runtime), func(t *testing.T) {
			cases := []struct {
				name   string
				mutate func(*vendorplugin.SpawnRequest)
				want   error
			}{
				{"missing-profile", func(r *vendorplugin.SpawnRequest) { r.Context.Profile.Name = "" }, agentic.ErrCuratorProfileMissing},
				{"missing-lock", func(r *vendorplugin.SpawnRequest) { r.Context.Profile.LockSHA256 = "" }, agentic.ErrCuratorProfileMissing},
				{"malformed", func(r *vendorplugin.SpawnRequest) { r.Context.Precedence.Winner = "invalid" }, agentic.ErrCuratorContextMalformed},
				{"unknown-curator-descriptor", func(r *vendorplugin.SpawnRequest) { r.Context.MCP.Channels[0].Kind = "future-channel" }, agentic.ErrUnknownCuratorDescriptor},
				{"unknown-semantic-descriptor", func(r *vendorplugin.SpawnRequest) {
					r.ContextDescriptors = []agentic.ContextDescriptor{{Kind: "future-channel"}}
				}, agentic.ErrUnknownContextDescriptor},
				{"malformed-semantic-descriptor", func(r *vendorplugin.SpawnRequest) {
					r.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt}}
				}, agentic.ErrInvalidContextDescriptor},
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					req := vendorCuratorRequest(t, tc)
					test.mutate(&req)
					plan, err := build(context.Background(), vendorplugin.Default, req, agentic.LaunchModeExec)
					if !errors.Is(err, test.want) {
						t.Fatalf("BuildLaunch error = %v, want typed %v", err, test.want)
					}
					if !reflect.DeepEqual(plan, agentic.Plan{}) {
						t.Fatal("refusal returned a plan")
					}
					if test.want == agentic.ErrUnknownCuratorDescriptor {
						var typed *agentic.UnknownCuratorDescriptorError
						if !errors.As(err, &typed) || typed.Kind != "future-channel" {
							t.Fatalf("lost descriptor type: %v", err)
						}
					}
					if test.want == agentic.ErrUnknownContextDescriptor {
						var typed *agentic.UnknownContextDescriptorError
						if !errors.As(err, &typed) || typed.Kind != "future-channel" {
							t.Fatalf("lost descriptor type: %v", err)
						}
					}
					if test.want == agentic.ErrInvalidContextDescriptor {
						var typed *agentic.InvalidContextDescriptorError
						if !errors.As(err, &typed) {
							t.Fatalf("lost descriptor type: %v", err)
						}
					}
				})
			}
		})
	}
}

// Embed the real vendor: admission and planning still use the production
// runtime, catalog, effort rules and system plugin. Hooks only mutate inputs.
type contextHookVendor struct {
	vendorplugin.Vendor
	onModels func()
	onSpawn  func(*vendorplugin.SpawnContext)
}

func (v *contextHookVendor) Models() []vendorplugin.Model {
	if v.onModels != nil {
		v.onModels()
	}
	return v.Vendor.Models()
}
func (v *contextHookVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	if v.onSpawn != nil {
		v.onSpawn(&sc)
	}
	return v.Vendor.Spawn(sc)
}
func contextHookRegistry(t *testing.T, tc curatorVendorCase) (*vendorplugin.Registry, *contextHookVendor) {
	t.Helper()
	runtime, err := vendorplugin.Default.ResolveRuntime(tc.runtime)
	if err != nil {
		t.Fatal(err)
	}
	hook := &contextHookVendor{Vendor: runtime.Vendor}
	registry := vendorplugin.NewRegistry(agentic.Default)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(hook); err != nil {
		t.Fatal(err)
	}
	return registry, hook
}

// Change every reachable mutable leaf by reflection so a newly added nested
// map, slice or pointer cannot silently escape the ownership test.
func changeContextLeaves(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			changeContextLeaves(value.Elem())
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			changeContextLeaves(value.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			changeContextLeaves(value.Index(i))
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			if value.Type().Elem().Kind() == reflect.String {
				value.SetMapIndex(key, reflect.ValueOf("mutated"))
			}
		}
	case reflect.String:
		if value.CanSet() {
			value.SetString("mutated")
		}
	}
}

func TestBuildLaunchCuratorEntrySnapshot(t *testing.T) {
	testVendorCuratorEntrySnapshot(t, vendorplugin.BuildLaunch)
}

func TestBuildLaunchWithEnvironmentCuratorEntrySnapshot(t *testing.T) {
	testVendorCuratorEntrySnapshot(t, buildVendorContextWithEnvironment)
}

func testVendorCuratorEntrySnapshot(t *testing.T, build vendorContextBuilder) {
	for _, tc := range curatorVendorCases {
		t.Run(string(tc.runtime), func(t *testing.T) {
			req := vendorCuratorRequest(t, tc)
			want, err := build(context.Background(), vendorplugin.Default, req, agentic.LaunchModeExec)
			if err != nil {
				t.Fatal(err)
			}
			provenance, _ := want.CuratorContextProvenanceSnapshot()
			req.Context.ExpectedProvenance = &provenance
			registry, hook := contextHookRegistry(t, tc)
			// Models is reached after BuildLaunch entry but before Spawn or BuildPlan.
			hook.onModels = func() { changeContextLeaves(reflect.ValueOf(req.Context)) }
			got, err := build(context.Background(), registry, req, agentic.LaunchModeExec)
			if err != nil {
				t.Fatalf("caller mutation after entry changed admission: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("caller mutation after entry changed plan")
			}
			changeContextLeaves(reflect.ValueOf(req.Context))
			if !reflect.DeepEqual(got, want) {
				t.Fatal("caller mutation after return changed plan")
			}
		})
	}
}

func TestBuildLaunchSemanticEntrySnapshot(t *testing.T) {
	testVendorSemanticEntrySnapshot(t, vendorplugin.BuildLaunch)
}

func TestBuildLaunchWithEnvironmentSemanticEntrySnapshot(t *testing.T) {
	testVendorSemanticEntrySnapshot(t, buildVendorContextWithEnvironment)
}

func testVendorSemanticEntrySnapshot(t *testing.T, build vendorContextBuilder) {
	for _, tc := range curatorVendorCases {
		t.Run(string(tc.runtime), func(t *testing.T) {
			for _, kind := range []agentic.ContextDescriptorKind{agentic.ContextSystemPrompt, agentic.ContextMCPServers, agentic.ContextPermission} {
				t.Run(string(kind), func(t *testing.T) {
					req := vendorCuratorRequest(t, tc)
					req.Context = nil
					mode := agentic.LaunchModeExec
					descriptor := agentic.ContextDescriptor{Kind: kind}
					switch kind {
					case agentic.ContextSystemPrompt:
						descriptor.SystemPrompt = &agentic.SystemPromptContext{Text: "instructions"}
					case agentic.ContextMCPServers:
						descriptor.MCP = &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{{Name: "docs", Transport: agentic.MCPTransportStdio, Command: "docs-server", Args: []string{"--read-only"}}}}
					case agentic.ContextPermission:
						descriptor.Permission = &agentic.PermissionContext{Mode: agentic.PermissionModeNative}
						mode = agentic.LaunchModeInteractive
						req.Prompt = nil
						req.PromptPath = ""
					}
					req.ContextDescriptors = []agentic.ContextDescriptor{descriptor}
					want, err := build(context.Background(), vendorplugin.Default, req, mode)
					if err != nil {
						t.Fatal(err)
					}
					registry, hook := contextHookRegistry(t, tc)
					hook.onModels = func() { changeContextLeaves(reflect.ValueOf(req.ContextDescriptors)) }
					got, err := build(context.Background(), registry, req, mode)
					if err != nil {
						t.Fatalf("descriptor mutation after entry: %v", err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatal("descriptor mutation changed plan")
					}
				})
			}
		})
	}
}

func TestBuildLaunchContextFidelity(t *testing.T) {
	testVendorContextFidelity(t, vendorplugin.BuildLaunch)
}

func TestBuildLaunchWithEnvironmentContextFidelity(t *testing.T) {
	testVendorContextFidelity(t, buildVendorContextWithEnvironment)
}

func testVendorContextFidelity(t *testing.T, build vendorContextBuilder) {
	for _, tc := range curatorVendorCases {
		t.Run(string(tc.runtime), func(t *testing.T) {
			for _, field := range []string{"context", "descriptors", "in-place-context", "in-place-descriptors"} {
				t.Run(field, func(t *testing.T) {
					req := vendorCuratorRequest(t, tc)
					req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "original"}}}
					registry, hook := contextHookRegistry(t, tc)
					hook.onSpawn = func(sc *vendorplugin.SpawnContext) {
						switch field {
						case "context":
							sc.Request.Context = nil
						case "descriptors":
							sc.Request.ContextDescriptors = nil
						case "in-place-context":
							changeContextLeaves(reflect.ValueOf(sc.Request.Context))
						case "in-place-descriptors":
							changeContextLeaves(reflect.ValueOf(sc.Request.ContextDescriptors))
						}
					}
					before, err := json.Marshal(req.Context)
					if err != nil {
						t.Fatal(err)
					}
					plan, err := build(context.Background(), registry, req, agentic.LaunchModeExec)
					if !errors.Is(err, vendorplugin.ErrVendorContract) {
						t.Fatalf("BuildLaunch allowed vendor %s redirect: %v", field, err)
					}
					if !reflect.DeepEqual(plan, agentic.Plan{}) {
						t.Fatal("vendor redirect returned plan")
					}
					after, _ := json.Marshal(req.Context)
					if string(before) != string(after) || req.ContextDescriptors[0].SystemPrompt.Text != "original" {
						t.Fatal("vendor changed caller context")
					}
				})
			}
		})
	}
}

func TestBuildLaunchNilContextPlanEquality(t *testing.T) {
	testVendorNilContextPlanEquality(t, vendorplugin.BuildLaunch)
}

func TestBuildLaunchWithEnvironmentNilContextPlanEquality(t *testing.T) {
	testVendorNilContextPlanEquality(t, buildVendorContextWithEnvironment)
}

func testVendorNilContextPlanEquality(t *testing.T, build vendorContextBuilder) {
	bin := t.TempDir()
	for _, name := range []string{"codex", "claude", "qwen", "gemini", "muse", "pi"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	prompt := filepath.Join(bin, "assignment.md")
	if err := os.WriteFile(prompt, []byte("assignment"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, declaration := range vendorplugin.Default.RuntimeDeclarations() {
		t.Run(string(declaration.ID), func(t *testing.T) {
			models := declaration.Models
			if declaration.Vendor != "" {
				vendor, ok := vendorplugin.Default.Lookup(declaration.Vendor)
				if !ok {
					t.Fatalf("runtime vendor %s not registered", declaration.Vendor)
				}
				models = vendor.Models()
			}
			var model vendorplugin.Model
			for _, candidate := range models {
				if candidate.DrivenBy(declaration.System) {
					model = candidate
					break
				}
			}
			if model.ID == "" {
				t.Fatal("runtime has no model for nil-context parity")
			}
			req := vendorplugin.SpawnRequest{Runtime: declaration.ID, Model: model.ID, Effort: model.Effort.Recommended, Prompt: []byte("assignment"), PromptPath: prompt, Home: "/managed/home", WorkDir: "/work", Env: []string{"PATH=" + bin}}
			got, err := build(context.Background(), vendorplugin.Default, req, agentic.LaunchModeDryRun)
			if err != nil {
				t.Fatal(err)
			}
			// The pre-carrier request projection, including runtime-owned identities.
			legacy := agentic.LaunchRequest{System: declaration.System, Model: model.Launchable(), Effort: req.Effort, PromptPath: req.PromptPath, Prompt: req.Prompt, WorkDir: req.WorkDir, Home: req.Home, Env: req.Env, Runtime: string(req.Runtime), Vendor: string(declaration.Vendor)}
			want, err := agentic.BuildPlan(agentic.Default, legacy, agentic.LaunchModeDryRun)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("nil context changed whole %s plan", declaration.ID)
			}
			a, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			if string(a) != string(b) {
				t.Fatal("nil context changed plan bytes")
			}
		})
	}
}

// Both public entries share assertions, but dispatch through their real APIs.
type vendorContextBuilder func(context.Context, *vendorplugin.Registry, vendorplugin.SpawnRequest, agentic.LaunchMode) (agentic.Plan, error)

func buildVendorContextWithEnvironment(ctx context.Context, registry *vendorplugin.Registry, req vendorplugin.SpawnRequest, mode agentic.LaunchMode) (agentic.Plan, error) {
	result, err := vendorplugin.BuildLaunchWithEnvironment(ctx, registry, req, mode)
	return result.Plan, err
}
