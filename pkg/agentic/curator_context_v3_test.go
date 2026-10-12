package agentic_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
)

// This file mirrors the launch-env-fragment-v3 conformance suite
// (curator-spec conformance/v1/schema-cases/launch-env-fragment-v3/):
// every valid case is an accept fixture and every invalid case a typed
// refuse fixture. v3 validation runs in BuildPlan before any plugin gate,
// so the invalid table drives claude and codex plugins with v3-shaped
// fragments the same way the malformed table already drives pi-shaped ones.

const museV3HomeParent = "/managed/muse-home"

func validMuseCuratorContext() agentic.CuratorContext {
	return agentic.CuratorContext{
		Revision:    agentic.CuratorLaunchFragmentV3,
		Environment: "muse",
		Profile:     agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
		Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env: map[string]string{
			"XDG_CONFIG_HOME": museV3HomeParent + "/config",
			"XDG_DATA_HOME":   museV3HomeParent + "/data",
			"XDG_STATE_HOME":  museV3HomeParent + "/state",
			"XDG_CACHE_HOME":  museV3HomeParent + "/cache",
		},
		Permissions: validAcceptanceCuratorPermissions(),
	}
}

func museCuratorRegistry(t *testing.T) *agentic.Registry {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(muse.New()); err != nil {
		t.Fatalf("Register Muse: %v", err)
	}
	return registry
}

func museCuratorRequest(t *testing.T, context *agentic.CuratorContext, home string) agentic.LaunchRequest {
	t.Helper()
	binDir := t.TempDir()
	binaryPath := filepath.Join(binDir, "muse")
	if err := execfixture.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write stub executable: %v", err)
	}
	return agentic.LaunchRequest{
		System:  "muse",
		Model:   agentic.Model{ID: "curator-context-test"},
		WorkDir: t.TempDir(),
		Home:    home,
		Env:     []string{"PATH=" + binDir},
		Context: context,
	}
}

func buildMuseCuratorPlan(t *testing.T, context agentic.CuratorContext, home string) (agentic.Plan, error) {
	t.Helper()
	req := museCuratorRequest(t, &context, home)
	return agentic.BuildPlan(museCuratorRegistry(t), req, agentic.LaunchModeExec)
}

func malformedV3Suffix(reason string) string {
	return "agentic: malformed typed Curator context: " + reason
}

func TestBuildPlanCuratorContextRefusesInvalidFragmentV3(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			type invalidV3Case struct {
				name        string
				conformance string
				wantMessage string
				mutate      func(*agentic.CuratorContext, *agentic.LaunchRequest)
			}
			nativeProfile := validAcceptanceCuratorPermissions()
			v3SingleHome := func(plugin curatorPluginCase, environment, homeVariable string) agentic.CuratorContext {
				context := validCuratorContext(curatorPluginCase{name: plugin.name, system: plugin.system, environment: environment, homeVariable: homeVariable})
				context.Revision = agentic.CuratorLaunchFragmentV3
				permissions := *nativeProfile
				context.Permissions = &permissions
				return context
			}
			cases := []invalidV3Case{
				{name: "v2-token", conformance: "invalid-v2-token.json", wantMessage: malformedV3Suffix("muse fragments require launch-env-fragment-v3"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Revision = agentic.CuratorLaunchFragmentV2
				}},
				{name: "v1-muse", wantMessage: malformedV3Suffix("muse fragments require launch-env-fragment-v3"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Revision = agentic.CuratorLaunchFragmentV1
					context.Permissions = nil
				}},
				{name: "HOME-added", conformance: "invalid-HOME-added.json", wantMessage: malformedV3Suffix("muse fragments must not carry HOME"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["HOME"] = "/manager/home"
				}},
				{name: "HOME-substitution", conformance: "invalid-HOME-substitution.json", wantMessage: malformedV3Suffix("muse fragments must not carry HOME"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env = map[string]string{"HOME": "/manager/home"}
				}},
				{name: "HOME-unmapped", wantMessage: malformedV3Suffix("muse fragments must not carry HOME"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["HOME"] = "__unmapped__"
				}},
				{name: "missing-XDG_CONFIG_HOME", conformance: "invalid-missing-XDG_CONFIG_HOME.json", wantMessage: malformedV3Suffix("muse fragments require exactly the four XDG variables"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					delete(context.Env, "XDG_CONFIG_HOME")
				}},
				{name: "missing-XDG_DATA_HOME", conformance: "invalid-missing-XDG_DATA_HOME.json", wantMessage: malformedV3Suffix("muse fragments require exactly the four XDG variables"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					delete(context.Env, "XDG_DATA_HOME")
				}},
				{name: "missing-XDG_STATE_HOME", conformance: "invalid-missing-XDG_STATE_HOME.json", wantMessage: malformedV3Suffix("muse fragments require exactly the four XDG variables"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					delete(context.Env, "XDG_STATE_HOME")
				}},
				{name: "missing-XDG_CACHE_HOME", conformance: "invalid-missing-XDG_CACHE_HOME.json", wantMessage: malformedV3Suffix("muse fragments require exactly the four XDG variables"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					delete(context.Env, "XDG_CACHE_HOME")
				}},
				{name: "extra-variable", wantMessage: malformedV3Suffix("muse fragments require exactly the four XDG variables"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["FOO"] = "/extra"
				}},
				{name: "no-env", conformance: "invalid.json", wantMessage: malformedV3Suffix("muse fragments require exactly the four XDG variables"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env = nil
				}},
				{name: "relative-XDG_CONFIG_HOME", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = "relative/config"
				}},
				{name: "relative-XDG_DATA_HOME", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_DATA_HOME"] = "relative/data"
				}},
				{name: "relative-XDG_STATE_HOME", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_STATE_HOME"] = "relative/state"
				}},
				{name: "relative-XDG_CACHE_HOME", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CACHE_HOME"] = "relative/cache"
				}},
				{name: "absolute-NUL", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = "/managed/muse-home/conf\x00ig"
				}},
				{name: "absolute-dotdot", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = "/managed/muse-home/../outside"
				}},
				{name: "absolute-too-short", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = "/"
				}},
				{name: "absolute-too-long", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = "/" + strings.Repeat("a", 4096)
				}},
				{name: "trailing-slash-XDG_CONFIG_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = museV3HomeParent + "/config/"
				}},
				{name: "trailing-slash-XDG_DATA_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_DATA_HOME"] = museV3HomeParent + "/data/"
				}},
				{name: "trailing-slash-XDG_STATE_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_STATE_HOME"] = museV3HomeParent + "/state/"
				}},
				{name: "trailing-slash-XDG_CACHE_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CACHE_HOME"] = museV3HomeParent + "/cache/"
				}},
				{name: "dot-segment-XDG_CONFIG_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = museV3HomeParent + "/./config"
				}},
				{name: "double-slash-XDG_CONFIG_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = museV3HomeParent + "//config"
				}},
				{name: "dotdot-parent-XDG_DATA_HOME", wantMessage: malformedV3Suffix("muse XDG values must be absolute paths"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_DATA_HOME"] = museV3HomeParent + "/../outside/data"
				}},
				{name: "parent-root", wantMessage: malformedV3Suffix("muse XDG home parent must not be the filesystem root"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env = map[string]string{
						"XDG_CONFIG_HOME": "/config",
						"XDG_DATA_HOME":   "/data",
						"XDG_STATE_HOME":  "/state",
						"XDG_CACHE_HOME":  "/cache",
					}
				}},
				{name: "config-names-parent", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					// The P1-1 attack set: Base strips the trailing slash
					// while Dir cleans, so a Base/Dir spelling accepts it
					// with LaunchRequest.Home "/a/config". Exact Join
					// refuses it.
					context.Env = map[string]string{
						"XDG_CONFIG_HOME": "/a/config/",
						"XDG_DATA_HOME":   "/a/config/data",
						"XDG_STATE_HOME":  "/a/config/state",
						"XDG_CACHE_HOME":  "/a/config/cache",
					}
				}},
				{name: "wrong-parent-XDG_CONFIG_HOME", conformance: "invalid-wrong-parent-XDG_CONFIG_HOME.json", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = "/foreign/parent"
				}},
				{name: "wrong-parent-XDG_DATA_HOME", conformance: "invalid-wrong-parent-XDG_DATA_HOME.json", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_DATA_HOME"] = "/foreign/parent"
				}},
				{name: "wrong-parent-XDG_STATE_HOME", conformance: "invalid-wrong-parent-XDG_STATE_HOME.json", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_STATE_HOME"] = "/foreign/parent"
				}},
				{name: "wrong-parent-XDG_CACHE_HOME", conformance: "invalid-wrong-parent-XDG_CACHE_HOME.json", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CACHE_HOME"] = "/foreign/parent"
				}},
				{name: "leaf-unmapped-config", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CONFIG_HOME"] = museV3HomeParent + "/config__unmapped__"
				}},
				{name: "leaf-unmapped-data", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_DATA_HOME"] = museV3HomeParent + "/data__unmapped__"
				}},
				{name: "leaf-unmapped-state", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_STATE_HOME"] = museV3HomeParent + "/state__unmapped__"
				}},
				{name: "leaf-unmapped-cache", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CACHE_HOME"] = museV3HomeParent + "/cache__unmapped__"
				}},
				{name: "diffparent-XDG_DATA_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_DATA_HOME"] = "/foreign/muse/data"
				}},
				{name: "diffparent-XDG_STATE_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_STATE_HOME"] = "/foreign/muse/state"
				}},
				{name: "diffparent-XDG_CACHE_HOME", wantMessage: malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Env["XDG_CACHE_HOME"] = "/foreign/muse/cache"
				}},
				{name: "home-parent-mismatch", wantMessage: malformedV3Suffix("muse home parent must match LaunchRequest.Home"), mutate: func(_ *agentic.CuratorContext, req *agentic.LaunchRequest) {
					req.Home = "/other/home"
				}},
				{name: "path-prepend-relative", wantMessage: malformedV3Suffix("path_prepend must be an absolute path"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.PathPrepend = "relative/bin"
				}},
				{name: "path-prepend-dotdot", wantMessage: malformedV3Suffix("path_prepend must be an absolute path"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.PathPrepend = "/a/../bin"
				}},
				{name: "unverified-mcp", conformance: "invalid-unverified-mcp.json", wantMessage: malformedV3Suffix("muse fragments cannot carry MCP context"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{
						Path:     "/manager/environments/companyA/claude_code/.agent-context/mcp/claude_code.json",
						EnvNames: []string{"FIGMA_API_KEY"},
						Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFlag, Flag: "--mcp-config", Argument: agentic.CuratorArgumentPath, With: []string{"--strict-mcp-config"}}},
					}
				}},
				{name: "unverified-system-prompt", conformance: "invalid-unverified-system-prompt.json", wantMessage: malformedV3Suffix("muse fragments cannot carry system-prompt context"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = &agentic.CuratorSystemPromptContext{
						Path:     "/manager/environments/companyA/claude_code/.agent-context/system-prompt.md",
						Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFlag, Semantics: agentic.CuratorSystemPromptAppend, Flag: "--append-system-prompt-file", Argument: agentic.CuratorArgumentPath}},
						Intent:   agentic.CuratorSystemPromptAppend,
					}
				}},
				{name: "permissions-absent", conformance: "invalid-permissions-absent.json", wantMessage: malformedV3Suffix("v3 fragments require the permissions member"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = nil
				}},
				{name: "permissions-global-unlocked", conformance: "invalid-permissions-global-unlocked.json", wantMessage: malformedV3Suffix("permissions locked must be true exactly for the global source"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: false, Source: agentic.CuratorPermissionSourceGlobal}
				}},
				{name: "permissions-locked-profile-source", conformance: "invalid-permissions-locked-profile-source.json", wantMessage: malformedV3Suffix("permissions locked must be true exactly for the global source"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: true, Source: agentic.CuratorPermissionSourceProfile}
				}},
				{name: "permissions-missing-mode", conformance: "invalid-permissions-missing-mode.json", wantMessage: malformedV3Suffix("permissions mode must be native or yolo"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = &agentic.CuratorPermissions{Mode: "", Locked: false, Source: agentic.CuratorPermissionSourceProfile}
				}},
				{name: "permissions-unknown-mode", conformance: "invalid-permissions-unknown-mode.json", wantMessage: malformedV3Suffix("permissions mode must be native or yolo"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = &agentic.CuratorPermissions{Mode: "auto", Locked: false, Source: agentic.CuratorPermissionSourceProfile}
				}},
				{name: "permissions-unknown-source", conformance: "invalid-permissions-unknown-source.json", wantMessage: malformedV3Suffix("permissions source must be profile, global, or default"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: false, Source: "flag"}
				}},
				{name: "permissions-yolo-locked", conformance: "invalid-permissions-yolo-locked.json", wantMessage: malformedV3Suffix("permissions yolo requires an unlocked profile source"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionYolo, Locked: true, Source: agentic.CuratorPermissionSourceGlobal}
				}},
				{name: "permissions-yolo-silent", conformance: "invalid-permissions-yolo-silent.json", wantMessage: malformedV3Suffix("permissions yolo requires an unlocked profile source"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Permissions = &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionYolo, Locked: false, Source: agentic.CuratorPermissionSourceDefault}
				}},
				{name: "unknown-environment", conformance: "invalid-unknown-environment.json", wantMessage: malformedV3Suffix("environment is not a supported typed fragment value"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Environment = "unknown"
				}},
				{name: "extra-parent-claude_code", conformance: "invalid-extra-parent-claude_code.json", wantMessage: malformedV3Suffix("exactly one managed-home environment entry is required"), mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
					*context = v3SingleHome(plugin, "claude_code", "CLAUDE_CONFIG_DIR")
					context.Env["XDG_CACHE_HOME"] = "/manager/cache"
					req.Home = context.Env["CLAUDE_CONFIG_DIR"]
				}},
				{name: "extra-parent-codex_cli", conformance: "invalid-extra-parent-codex_cli.json", wantMessage: malformedV3Suffix("exactly one managed-home environment entry is required"), mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
					*context = v3SingleHome(plugin, "codex_cli", "CODEX_HOME")
					context.Env["XDG_CACHE_HOME"] = "/manager/cache"
					req.Home = context.Env["CODEX_HOME"]
				}},
				{name: "extra-parent-opencode", conformance: "invalid-extra-parent-opencode.json", wantMessage: malformedV3Suffix("exactly one managed-home environment entry is required"), mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
					*context = v3SingleHome(plugin, "opencode", "XDG_CONFIG_HOME")
					context.Env["XDG_CACHE_HOME"] = "/manager/cache"
					req.Home = context.Env["XDG_CONFIG_HOME"]
				}},
				{name: "extra-parent-pi", conformance: "invalid-extra-parent-pi.json", wantMessage: malformedV3Suffix("exactly one managed-home environment entry is required"), mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
					*context = v3SingleHome(plugin, "pi", "PI_CODING_AGENT_DIR")
					context.Env["XDG_CACHE_HOME"] = "/manager/cache"
					req.Home = context.Env["PI_CODING_AGENT_DIR"]
				}},
			}
			for _, tc := range cases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					context := validMuseCuratorContext()
					req := curatorRequest(t, plugin, &context)
					req.Home = museV3HomeParent
					tc.mutate(&context, &req)
					req.Context = &context
					_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
					if !errors.Is(err, agentic.ErrCuratorContextMalformed) {
						t.Fatalf("BuildPlan err = %v, want %v", err, agentic.ErrCuratorContextMalformed)
					}
					if !strings.HasSuffix(err.Error(), tc.wantMessage) {
						t.Fatalf("BuildPlan err = %q, want refusal diagnostic suffix %q", err, tc.wantMessage)
					}
				})
			}
		})
	}
}

func TestCuratorContextAcceptsFragmentV3ForOpencodeAndPi(t *testing.T) {
	// Mirrors valid-opencode.json and valid-pi.json member for member
	// (env, profile, precedence, mcp, system_prompt, permissions). The
	// system-prompt Intent is launch-request data rather than a fragment
	// member, so the fixture supplies append; the empty opencode channel
	// list is the file's "channels": [].
	opencode := agentic.CuratorContext{
		Revision:    agentic.CuratorLaunchFragmentV3,
		Environment: "opencode",
		Profile:     agentic.CuratorProfilePin{Name: "companyA", LockSHA256: strings.Repeat("b", 64)},
		Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env:         map[string]string{"XDG_CONFIG_HOME": "/manager/environments/companyA/opencode"},
		MCP: &agentic.CuratorMCPContext{
			Path:     "/manager/environments/companyA/opencode/opencode/.agent-context/mcp/opencode.json",
			EnvNames: []string{"FIGMA_API_KEY"},
			Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorVariable, Variable: "OPENCODE_CONFIG"}},
		},
		SystemPrompt: &agentic.CuratorSystemPromptContext{
			Path:     "/manager/environments/companyA/opencode/opencode/.agent-context/system-prompt.md",
			Channels: []agentic.CuratorChannelDescriptor{},
			Intent:   agentic.CuratorSystemPromptAppend,
		},
		Permissions: &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: false, Source: agentic.CuratorPermissionSourceProfile},
	}
	pi := agentic.CuratorContext{
		Revision:    agentic.CuratorLaunchFragmentV3,
		Environment: "pi",
		Profile:     agentic.CuratorProfilePin{Name: "companyA", LockSHA256: strings.Repeat("b", 64)},
		Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env:         map[string]string{"PI_CODING_AGENT_DIR": "/manager/environments/companyA/pi"},
		SystemPrompt: &agentic.CuratorSystemPromptContext{
			Path: "/manager/environments/companyA/pi/.agent-context/system-prompt.md",
			Channels: []agentic.CuratorChannelDescriptor{
				{Kind: agentic.CuratorDescriptorFlag, Semantics: agentic.CuratorSystemPromptAppend, Flag: "--append-system-prompt", Argument: agentic.CuratorArgumentPath},
				{Kind: agentic.CuratorDescriptorFile, Semantics: agentic.CuratorSystemPromptAppend, Filename: "APPEND_SYSTEM.md"},
				{Kind: agentic.CuratorDescriptorFile, Semantics: agentic.CuratorSystemPromptReplace, Filename: "SYSTEM.md"},
			},
			Intent: agentic.CuratorSystemPromptAppend,
		},
		Permissions: &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: false, Source: agentic.CuratorPermissionSourceProfile},
	}
	for _, fixture := range []struct {
		name    string
		context agentic.CuratorContext
		home    string
	}{
		{name: "opencode", context: opencode, home: "/manager/environments/companyA/opencode"},
		{name: "pi", context: pi, home: "/manager/environments/companyA/pi"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if err := agentic.ValidateCuratorContext(&fixture.context, fixture.home); err != nil {
				t.Fatalf("ValidateCuratorContext v3 %s: %v", fixture.name, err)
			}
		})
	}
}

func TestBuildPlanMuseCuratorContextAcceptsFragmentV3(t *testing.T) {
	triples := []struct {
		name        string
		conformance string
		permissions agentic.CuratorPermissions
	}{
		{name: "native-profile", conformance: "valid.json", permissions: agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: false, Source: agentic.CuratorPermissionSourceProfile}},
		{name: "native-locked", conformance: "valid-permissions-native-locked.json", permissions: agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: true, Source: agentic.CuratorPermissionSourceGlobal}},
		{name: "native-silent", conformance: "valid-permissions-native-silent.json", permissions: agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: false, Source: agentic.CuratorPermissionSourceDefault}},
		{name: "yolo-unlocked", conformance: "valid-permissions-yolo-unlocked.json", permissions: agentic.CuratorPermissions{Mode: agentic.CuratorPermissionYolo, Locked: false, Source: agentic.CuratorPermissionSourceProfile}},
	}
	for _, triple := range triples {
		triple := triple
		t.Run(triple.name, func(t *testing.T) {
			context := validMuseCuratorContext()
			permissions := triple.permissions
			context.Permissions = &permissions
			req := museCuratorRequest(t, &context, museV3HomeParent)
			req.Env = append(req.Env,
				"HOME=/home/operator",
				"XDG_CONFIG_HOME=/junk/config",
				"XDG_DATA_HOME=/junk/data",
				"XDG_STATE_HOME=/junk/state",
				"XDG_CACHE_HOME=/junk/cache",
				"XDG_RUNTIME_DIR=/junk/run",
			)
			plan, err := agentic.BuildPlan(museCuratorRegistry(t), req, agentic.LaunchModeExec)
			if err != nil {
				t.Fatalf("BuildPlan for %s: %v", triple.conformance, err)
			}
			for variable, want := range context.Env {
				got, found := musePlanEnvValue(plan, variable)
				if !found || got != want {
					t.Fatalf("plan env %s = %q, want curator value %q", variable, got, want)
				}
			}
			if got, _ := musePlanEnvValue(plan, "HOME"); got != "/home/operator" {
				t.Fatalf("plan env HOME = %q, want the inherited /home/operator untouched", got)
			}
			if got, _ := musePlanEnvValue(plan, "XDG_RUNTIME_DIR"); got != "/junk/run" {
				t.Fatalf("plan env XDG_RUNTIME_DIR = %q, want the inherited value untouched", got)
			}
			var xdgNames []string
			for _, entry := range plan.Env {
				if name, _, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(name, "XDG_") {
					xdgNames = append(xdgNames, name)
				}
			}
			if len(xdgNames) != 5 {
				t.Fatalf("plan env carries XDG entries %v, want exactly the four curator values plus the inherited runtime dir", xdgNames)
			}
			provenance, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("v3 muse plan has no Curator provenance")
			}
			wantHomes := agentic.CuratorMuseHomes{
				Parent:        museV3HomeParent,
				XDGConfigHome: museV3HomeParent + "/config",
				XDGDataHome:   museV3HomeParent + "/data",
				XDGStateHome:  museV3HomeParent + "/state",
				XDGCacheHome:  museV3HomeParent + "/cache",
			}
			if provenance.MuseHomes == nil || !reflect.DeepEqual(*provenance.MuseHomes, wantHomes) {
				t.Fatalf("muse provenance homes = %#v, want %#v", provenance.MuseHomes, wantHomes)
			}
			if provenance.ManagedHomeVariable != "" || provenance.ManagedHome != museV3HomeParent {
				t.Fatalf("muse provenance home = (%q, %q), want empty variable and parent %q", provenance.ManagedHomeVariable, provenance.ManagedHome, museV3HomeParent)
			}
			if len(provenance.Fragment.Env) != 4 {
				t.Fatalf("muse fragment identity carries %d env entries, want four", len(provenance.Fragment.Env))
			}
			context.Env["XDG_DATA_HOME"] = "/evil/data"
			provenance.MuseHomes.XDGDataHome = "/evil/data"
			provenance.Fragment.Env["XDG_DATA_HOME"] = "/evil/data"
			second, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok || second.MuseHomes == nil || !reflect.DeepEqual(*second.MuseHomes, wantHomes) || second.Fragment.Env["XDG_DATA_HOME"] != museV3HomeParent+"/data" {
				t.Fatalf("muse provenance changed after request or returned-copy mutation: %#v", second.MuseHomes)
			}
		})
	}
}

func TestBuildPlanMuseCuratorContextRefusesMismatchedEnvironment(t *testing.T) {
	plugin := curatorPlugins[0]
	context := validCuratorContext(plugin)
	context.Revision = agentic.CuratorLaunchFragmentV3
	context.Permissions = validAcceptanceCuratorPermissions()
	req := museCuratorRequest(t, &context, "/managed/home")
	_, err := agentic.BuildPlan(museCuratorRegistry(t), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for a claude_code fragment on muse", err)
	}
}

func TestBuildPlanMuseCuratorContextRefusesManagedNetwork(t *testing.T) {
	context := validMuseCuratorContext()
	req := museCuratorRequest(t, &context, museV3HomeParent)
	req.Network = agentic.Network{
		Patch: envpatch.Patch{Unset: envpatch.UnsetNames()},
		Record: binding.Record{
			Schema:        binding.SchemaRecord,
			ProfileRef:    "egress-a",
			ProfileDigest: "sha256:1e5912de9e3459a12b7365f338d385c1c4c7b4a6b622598e17ed4004d7658699",
			AdapterIdentity: binding.AdapterIdentity{
				Adapter: "muse-env-v1", Harness: "muse", Build: "1.4.1-R4503.1", Entrypoint: "exec",
			},
			Assurance: "cooperative",
			Origin:    "explicit",
		},
	}
	_, err := agentic.BuildPlan(museCuratorRegistry(t), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for Curator context plus a managed network scope", err)
	}
}

func TestBuildPlanMuseCuratorContextRefusesInvalidFragment(t *testing.T) {
	context := validMuseCuratorContext()
	context.Env["XDG_DATA_HOME"] = "/foreign/muse/data"
	req := museCuratorRequest(t, &context, museV3HomeParent)
	_, err := agentic.BuildPlan(museCuratorRegistry(t), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrCuratorContextMalformed) {
		t.Fatalf("BuildPlan err = %v, want ErrCuratorContextMalformed", err)
	}
	if want := malformedV3Suffix("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache"); !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("BuildPlan err = %q, want refusal diagnostic suffix %q", err, want)
	}
}

// TestBuildPlanMuseCuratorContextRefusesAbsolutePathPrepend pins the P1-2
// common-guard line for muse: an absolute path_prepend is refused as an
// unsupported mapping exactly like the other adapters, never silently
// ignored. Relative and dotdot values are malformed rows in the invalid v3
// table above.
func TestBuildPlanMuseCuratorContextRefusesAbsolutePathPrepend(t *testing.T) {
	context := validMuseCuratorContext()
	context.PathPrepend = "/manager/environments/bin"
	_, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
	if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for an absolute muse path_prepend", err)
	}
	if want := "path_prepend has no registered root-bound launch mapping"; !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("BuildPlan err = %q, want refusal diagnostic suffix %q", err, want)
	}
}

func TestBuildPlanMuseCuratorContextRefusesDriftedHomes(t *testing.T) {
	for _, variable := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Run(variable, func(t *testing.T) {
			context := validMuseCuratorContext()
			plan, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
			if err != nil {
				t.Fatalf("initial BuildPlan: %v", err)
			}
			stored, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("initial plan has no Curator provenance")
			}
			context.Env[variable] = "/foreign/muse/" + strings.ToLower(strings.TrimPrefix(variable, "XDG_"))
			context.ExpectedProvenance = &stored
			_, err = buildMuseCuratorPlan(t, context, museV3HomeParent)
			if !errors.Is(err, agentic.ErrCuratorContextMalformed) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextMalformed for a drifted %s", err, variable)
			}
		})
	}
	t.Run("parent-only", func(t *testing.T) {
		context := validMuseCuratorContext()
		plan, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
		if err != nil {
			t.Fatalf("initial BuildPlan: %v", err)
		}
		stored, ok := plan.CuratorContextProvenanceSnapshot()
		if !ok {
			t.Fatal("initial plan has no Curator provenance")
		}
		context.ExpectedProvenance = &stored
		_, err = buildMuseCuratorPlan(t, context, "/other/home")
		if !errors.Is(err, agentic.ErrCuratorContextMalformed) {
			t.Fatalf("BuildPlan err = %v, want ErrCuratorContextMalformed for a mismatched home parent", err)
		}
	})
}

func TestBuildPlanMuseCuratorMuseHomesProjection(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validMuseCuratorContext()
			plan, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			provenance, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok || provenance.MuseHomes == nil {
				t.Fatal("v3 muse plan has no muse homes provenance")
			}
			projected, err := context.MuseHomes()
			if err != nil {
				t.Fatalf("MuseHomes: %v", err)
			}
			if !reflect.DeepEqual(projected, *provenance.MuseHomes) {
				t.Fatalf("MuseHomes = %#v, want the provenanced %#v", projected, *provenance.MuseHomes)
			}
			wantMessage := "agentic: malformed typed Curator context: muse home set requires a v3 muse fragment"
			var nilContext *agentic.CuratorContext
			if _, err := nilContext.MuseHomes(); err == nil || err.Error() != wantMessage {
				t.Fatalf("nil MuseHomes err = %v, want %q", err, wantMessage)
			}
			direct := []struct {
				name    string
				mutate  func(*agentic.CuratorContext)
				message string
			}{
				{name: "environment-unmapped", mutate: func(context *agentic.CuratorContext) { context.Environment = "muse__unmapped__" }, message: wantMessage},
				{name: "revision-unmapped", mutate: func(context *agentic.CuratorContext) { context.Revision = "CuratorLaunchFragmentV3__unmapped__" }, message: wantMessage},
				{name: "environment-claude", mutate: func(context *agentic.CuratorContext) { context.Environment = "claude_code" }, message: wantMessage},
				{name: "revision-v2", mutate: func(context *agentic.CuratorContext) { context.Revision = agentic.CuratorLaunchFragmentV2 }, message: wantMessage},
				{name: "env-missing-variable", mutate: func(context *agentic.CuratorContext) { delete(context.Env, "XDG_STATE_HOME") }, message: malformedV3Suffix("muse fragments require exactly the four XDG variables")},
			}
			for _, tc := range direct {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					context := validMuseCuratorContext()
					tc.mutate(&context)
					if _, err := context.MuseHomes(); err == nil || err.Error() != tc.message {
						t.Fatalf("MuseHomes err = %v, want %q", err, tc.message)
					}
				})
			}
		})
	}
}

func musePlanEnvValue(plan agentic.Plan, name string) (string, bool) {
	value, found := "", false
	for _, entry := range plan.Env {
		if key, val, ok := strings.Cut(entry, "="); ok && key == name {
			value, found = val, true
		}
	}
	return value, found
}
