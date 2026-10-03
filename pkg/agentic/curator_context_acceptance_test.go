package agentic_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
)

type curatorPluginCase struct {
	name         string
	system       agentic.SystemID
	environment  string
	homeVariable string
}

var curatorPlugins = []curatorPluginCase{
	{name: "claude", system: "claude-code", environment: "claude_code", homeVariable: "CLAUDE_CONFIG_DIR"},
	{name: "codex", system: "codex", environment: "codex_cli", homeVariable: "CODEX_HOME"},
}

func TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			for _, kind := range []agentic.CuratorDescriptorKind{
				agentic.CuratorDescriptorFlag,
				agentic.CuratorDescriptorConfigKey,
				agentic.CuratorDescriptorVariable,
				agentic.CuratorDescriptorFile,
			} {
				t.Run("system-prompt/"+string(kind), func(t *testing.T) {
					intent := agentic.CuratorSystemPromptAppend
					if plugin.system == "codex" {
						intent = agentic.CuratorSystemPromptReplace
					}
					descriptor := validSystemPromptDescriptor(plugin, kind, intent)
					context := validCuratorContext(plugin)
					context.SystemPrompt = &agentic.CuratorSystemPromptContext{
						Path:     "/managed/home/.agent-context/system-prompt.md",
						Channels: []agentic.CuratorChannelDescriptor{descriptor},
						Intent:   intent,
					}
					plan, err := buildCuratorPlan(t, plugin, context)
					if supportsSystemPromptKind(plugin, kind) {
						if err != nil {
							t.Fatalf("BuildPlan: %v", err)
						}
						if !planCarriesSystemPrompt(plan, plugin, descriptor, context.SystemPrompt.Path) {
							t.Fatalf("BuildPlan plan did not carry the plugin-rendered %q descriptor: %#v", kind, plan.Argv)
						}
						return
					}
					if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for %q", err, kind)
					}
				})

				t.Run("mcp/"+string(kind), func(t *testing.T) {
					descriptor := validMCPDescriptor(plugin, kind)
					context := validCuratorContext(plugin)
					context.MCP = &agentic.CuratorMCPContext{
						Path:     "/managed/home/.agent-context/mcp/config",
						EnvNames: []string{"DOCS_TOKEN", "FIGMA_API_KEY"},
						Channels: []agentic.CuratorChannelDescriptor{descriptor},
					}
					plan, err := buildCuratorPlan(t, plugin, context)
					if kind == agentic.CuratorDescriptorFlag {
						if err != nil {
							t.Fatalf("BuildPlan: %v", err)
						}
						if !planCarriesMCPChannel(plan, plugin, descriptor, context.MCP.Path) {
							t.Fatalf("BuildPlan plan did not apply MCP flag descriptor: %#v", plan.Argv)
						}
						provenance, ok := plan.CuratorContextProvenanceSnapshot()
						if !ok || !reflect.DeepEqual(provenance.Fragment.MCP.EnvNames, context.MCP.EnvNames) {
							t.Fatalf("plan provenance did not carry sorted env_names: %#v", provenance.Fragment.MCP)
						}
						return
					}
					if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for MCP %q", err, kind)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorContextPreservesLocalProviderRefusalPrecedence(t *testing.T) {
	t.Run("unsupported-transport-before-channel-validation", func(t *testing.T) {
		plugin := curatorPlugins[0]
		context := validCuratorContext(plugin)
		context.MCP = validAcceptanceCuratorMCP(plugin)
		req := curatorRequest(t, plugin, &context)
		req.LocalProvider = &agentic.LocalProviderBinding{ID: "local-curator-profile"}

		plan, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
		var refusal *agentic.LocalProviderRefusal
		if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderUnsupported {
			t.Fatalf("BuildPlan error = %v, want typed local-provider unsupported refusal before Curator validation", err)
		}
		if !reflect.DeepEqual(plan, agentic.Plan{}) {
			t.Fatalf("unsupported local-provider request returned a plan: %#v", plan)
		}
	})

	t.Run("unbound-before-incompatible-context", func(t *testing.T) {
		codexPlugin := curatorPlugins[1]
		foreignContext := validCuratorContext(curatorPlugins[0])
		req := curatorRequest(t, codexPlugin, &foreignContext)
		req.LocalProvider = &agentic.LocalProviderBinding{ID: " \t "}

		plan, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
		var refusal *agentic.LocalProviderRefusal
		if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderUnbound {
			t.Fatalf("BuildPlan error = %v, want typed local-provider unbound refusal before Curator capability validation", err)
		}
		if !reflect.DeepEqual(plan, agentic.Plan{}) {
			t.Fatalf("unbound local-provider request returned a plan: %#v", plan)
		}
	})
}

func TestBuildPlanCodexCuratorContextComposesWithLocalProviderTransport(t *testing.T) {
	plugin := curatorPlugins[1]
	home := t.TempDir()
	context := validCuratorContext(plugin)
	context.Env[plugin.homeVariable] = home
	context.MCP = validAcceptanceCuratorMCP(plugin)
	context.MCP.Path = filepath.Join(home, ".agent-context", "mcp", "config")
	context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptReplace)
	context.SystemPrompt.Path = filepath.Join(home, ".agent-context", "system-prompt.md")
	req := curatorRequest(t, plugin, &context)
	req.LocalProvider = &agentic.LocalProviderBinding{ID: "local-story"}
	req.Effort = "low"
	// A local-provider launch requires native catalog metadata for the
	// launch slug (TASK-261003-3rgdmh AC1); without the catalog pin the
	// plan below refuses unbound instead of composing.
	catalog := nativeLocalCatalogFixture(t, "curator-context-test", []string{"low"})
	if err := os.WriteFile(filepath.Join(home, "catalog.local.json"), []byte(catalog), 0o600); err != nil {
		t.Fatalf("write local-model catalog: %v", err)
	}
	config := "model_provider = \"openai\"\n" +
		"model_catalog_json = \"catalog.local.json\"\n\n" +
		"[model_providers.local-story]\n" +
		"name = \"task local provider\"\n" +
		"base_url = \"http://127.0.0.1:38171/v1\"\n" +
		"wire_api = \"responses\"\n" +
		"requires_openai_auth = false\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write local-provider config: %v", err)
	}

	planned, err := agentic.BuildPlanWithEnvironment(curatorRegistry(t), req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlanWithEnvironment: %v", err)
	}
	argv := planned.Plan.Argv
	promptValue := "model_instructions_file=" + `"` + context.SystemPrompt.Path + `"`
	providerValue := `model_provider="local-story"`
	pairIndex := func(flag, value string) int {
		for index := 0; index+1 < len(argv); index++ {
			if argv[index] == flag && argv[index+1] == value {
				return index
			}
		}
		return -1
	}
	tokenIndex := func(value string) int {
		for index, arg := range argv {
			if arg == value {
				return index
			}
		}
		return -1
	}
	promptIndex := pairIndex("-c", promptValue)
	mcpIndex := pairIndex("-p", "curator-mcp")
	execIndex := tokenIndex("exec")
	modelIndex := pairIndex("-m", req.Model.ID)
	providerIndex := pairIndex("-c", providerValue)
	if promptIndex < 0 || mcpIndex < 0 || execIndex < 0 || modelIndex < 0 || providerIndex < 0 ||
		promptIndex >= mcpIndex || mcpIndex >= execIndex || execIndex >= modelIndex || modelIndex >= providerIndex {
		t.Fatalf("Curator channels and local-provider overrides are out of contract order: %#v", argv)
	}
	codeHomeCount := 0
	for _, value := range planned.OwnedEnv {
		if value == "CODEX_HOME="+home {
			codeHomeCount++
		}
	}
	if codeHomeCount != 1 {
		t.Fatalf("owned environment has %d pins to the validated local-provider home, want one: %#v", codeHomeCount, planned.OwnedEnv)
	}
	provenance, ok := planned.Plan.CuratorContextProvenanceSnapshot()
	if !ok || provenance.ManagedHome != home || provenance.Fragment.MCP.Path != context.MCP.Path ||
		provenance.Fragment.SystemPrompt.Path != context.SystemPrompt.Path {
		t.Fatalf("combined plan lost Curator provenance: %#v", provenance)
	}
}

func TestBuildPlanCuratorSystemPromptIntentSelectsExactlyOneDescriptor(t *testing.T) {
	claudeCase := curatorPlugins[0]
	for _, intent := range []agentic.CuratorSystemPromptIntent{agentic.CuratorSystemPromptAppend, agentic.CuratorSystemPromptReplace} {
		t.Run("claude/"+string(intent), func(t *testing.T) {
			context := validCuratorContext(claudeCase)
			context.SystemPrompt = &agentic.CuratorSystemPromptContext{
				Path: "/managed/home/.agent-context/system-prompt.md",
				Channels: []agentic.CuratorChannelDescriptor{
					validSystemPromptDescriptor(claudeCase, agentic.CuratorDescriptorFlag, agentic.CuratorSystemPromptAppend),
					validSystemPromptDescriptor(claudeCase, agentic.CuratorDescriptorFlag, agentic.CuratorSystemPromptReplace),
				},
				Intent: intent,
			}
			plan, err := buildCuratorPlan(t, claudeCase, context)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			selected := appendSystemPromptFlagFor(intent)
			other := appendSystemPromptFlagFor(otherPromptIntent(intent))
			if countPair(plan.Argv, selected, context.SystemPrompt.Path) != 1 || containsPair(plan.Argv, other, context.SystemPrompt.Path) {
				t.Fatalf("BuildPlan must apply exactly the %q descriptor; argv = %#v", intent, plan.Argv)
			}
			provenance, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok || len(provenance.Fragment.SystemPrompt.Channels) != 2 || provenance.SystemPromptIntent != intent {
				t.Fatalf("plan provenance must keep the complete descriptor list and request intent: %#v", provenance)
			}
		})
	}

	codexCase := curatorPlugins[1]
	t.Run("codex/replace", func(t *testing.T) {
		context := validCuratorContext(codexCase)
		context.SystemPrompt = &agentic.CuratorSystemPromptContext{
			Path: "/managed/home/.agent-context/system-prompt.md",
			Channels: []agentic.CuratorChannelDescriptor{
				validSystemPromptDescriptor(codexCase, agentic.CuratorDescriptorConfigKey, agentic.CuratorSystemPromptAppend),
				validSystemPromptDescriptor(codexCase, agentic.CuratorDescriptorConfigKey, agentic.CuratorSystemPromptReplace),
			},
			Intent: agentic.CuratorSystemPromptReplace,
		}
		plan, err := buildCuratorPlan(t, codexCase, context)
		if err != nil {
			t.Fatalf("BuildPlan: %v", err)
		}
		if !containsPair(plan.Argv, "-c", `model_instructions_file="/managed/home/.agent-context/system-prompt.md"`) {
			t.Fatalf("Codex plan did not carry the selected replacement file: %#v", plan.Argv)
		}
		provenance, ok := plan.CuratorContextProvenanceSnapshot()
		if !ok || len(provenance.Fragment.SystemPrompt.Channels) != 2 {
			t.Fatalf("Codex plan provenance did not keep the complete descriptor list: %#v", provenance)
		}
	})
}

func TestBuildPlanClaudeCuratorSystemPromptRefusesGoalBinding(t *testing.T) {
	plugin := curatorPlugins[0]
	for _, intent := range []agentic.CuratorSystemPromptIntent{agentic.CuratorSystemPromptAppend, agentic.CuratorSystemPromptReplace} {
		t.Run(string(intent), func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, intent)
			req := curatorRequest(t, plugin, &context)
			req.Goal = &agentic.Goal{ID: "goal-1", Objective: "keep the task context", Revision: 1, ProviderCondition: "task completed"}

			_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for a Curator %s prompt plus goal binding", err, intent)
			}
		})
	}
}

func TestBuildPlanClaudeCuratorMCPRefusesLegacyComposition(t *testing.T) {
	plugin := curatorPlugins[0]
	for _, tc := range []struct {
		name        string
		composition agentic.Composition
	}{
		{name: "prefix-only", composition: agentic.Composition{Prefix: []string{"--mcp-config", `{}`}}},
		{name: "prefix-with-servers", composition: agentic.Composition{
			Prefix:  []string{"--mcp-config", `{"mcpServers":{"x":{"type":"stdio","command":"x"}}}`},
			Servers: []agentic.CompositionServer{{Name: "x", Transport: "stdio"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.MCP = validAcceptanceCuratorMCP(plugin)
			req := curatorRequest(t, plugin, &context)
			req.Composition = tc.composition

			_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for Curator MCP plus legacy composition %q", err, tc.name)
			}
		})
	}
}

func TestBuildPlanCodexCuratorMCPRefusesLegacyComposition(t *testing.T) {
	plugin := curatorPlugins[1]
	for _, tc := range []struct {
		name        string
		composition agentic.Composition
	}{
		{name: "prefix-only", composition: agentic.Composition{Prefix: []string{"-c", `mcp_servers={}`}}},
		{name: "prefix-with-servers", composition: agentic.Composition{
			Prefix:  []string{"-c", `mcp_servers.x.command="x"`},
			Servers: []agentic.CompositionServer{{Name: "x", Transport: "stdio"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.MCP = validAcceptanceCuratorMCP(plugin)
			req := curatorRequest(t, plugin, &context)
			req.Composition = tc.composition

			_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for Curator MCP plus legacy composition %q", err, tc.name)
			}
		})
	}
}

func TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags(t *testing.T) {
	plugin := curatorPlugins[0]
	flags := []struct {
		name string
		flag string
	}{
		{name: "system-prompt", flag: "--system-prompt"},
		{name: "append-system-prompt", flag: "--append-system-prompt"},
		{name: "append-system-prompt-file", flag: "--append-system-prompt-file"},
		{name: "system-prompt-file", flag: "--system-prompt-file"},
	}
	for _, intent := range []agentic.CuratorSystemPromptIntent{agentic.CuratorSystemPromptAppend, agentic.CuratorSystemPromptReplace} {
		for _, flag := range flags {
			for _, form := range []struct {
				name string
				args []string
			}{
				{name: "separated", args: []string{flag.flag, "injected-prompt"}},
				{name: "equals", args: []string{flag.flag + "=injected-prompt"}},
			} {
				t.Run(string(intent)+"/"+flag.name+"/"+form.name, func(t *testing.T) {
					context := validCuratorContext(plugin)
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, intent)
					req := curatorRequest(t, plugin, &context)
					req.NativeArgs = form.args

					_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeInteractive)
					if !errors.Is(err, agentic.ErrContextDescriptorConflict) {
						t.Fatalf("BuildPlan err = %v, want ErrContextDescriptorConflict for native %s %s spelling beside Curator %s prompt", err, flag.flag, form.name, intent)
					}
				})
			}
		}
	}
}

func TestBuildPlanClaudeCuratorMCPRefusesLegacyMCPDescriptor(t *testing.T) {
	assertCuratorMCPRefusesLegacyMCPDescriptor(t, curatorPlugins[0])
}

func TestBuildPlanCodexCuratorMCPRefusesLegacyMCPDescriptor(t *testing.T) {
	assertCuratorMCPRefusesLegacyMCPDescriptor(t, curatorPlugins[1])
}

func assertCuratorMCPRefusesLegacyMCPDescriptor(t *testing.T, plugin curatorPluginCase) {
	t.Helper()
	context := validCuratorContext(plugin)
	context.MCP = validAcceptanceCuratorMCP(plugin)
	req := curatorRequest(t, plugin, &context)
	req.ContextDescriptors = []agentic.ContextDescriptor{{
		Kind: agentic.ContextMCPServers,
		MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{{
			Name: "legacy-mcp-member", Transport: "stdio", Command: "legacy-mcp-command",
		}}},
	}}

	_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrContextDescriptorConflict) {
		t.Fatalf("BuildPlan err = %v, want ErrContextDescriptorConflict for a legacy MCP descriptor beside Curator MCP (%s)", err, plugin.name)
	}
}

func TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig(t *testing.T) {
	plugin := curatorPlugins[0]
	context := validCuratorContext(plugin)
	context.MCP = validAcceptanceCuratorMCP(plugin)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "long-separated", args: []string{"--mcp-config", `{"mcpServers":{}}`}},
		{name: "long-equals", args: []string{`--mcp-config={"mcpServers":{}}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := curatorRequest(t, plugin, &context)
			req.NativeArgs = tc.args
			_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrContextDescriptorConflict) {
				t.Fatalf("BuildPlan err = %v, want ErrContextDescriptorConflict for native --mcp-config spelling %q", err, tc.name)
			}
		})
	}
}

func TestBuildPlanClaudeCuratorSystemPromptRefusesLegacyContextSystemPrompt(t *testing.T) {
	assertLegacySystemPromptConflictMatrix(t, curatorPlugins[0])
}

func TestBuildPlanCodexCuratorSystemPromptRefusesLegacyContextSystemPrompt(t *testing.T) {
	assertLegacySystemPromptConflictMatrix(t, curatorPlugins[1])
}

func assertLegacySystemPromptConflictMatrix(t *testing.T, plugin curatorPluginCase) {
	t.Helper()
	for _, intent := range []agentic.CuratorSystemPromptIntent{agentic.CuratorSystemPromptAppend, agentic.CuratorSystemPromptReplace} {
		for _, legacy := range []struct {
			name string
			text string
		}{
			{name: "single-line", text: "legacy-single-line"},
			{name: "multi-line", text: "legacy-multi-line\nsecond line"},
		} {
			t.Run(string(intent)+"/"+legacy.name, func(t *testing.T) {
				context := validCuratorContext(plugin)
				context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, intent)
				req := curatorRequest(t, plugin, &context)
				req.ContextDescriptors = []agentic.ContextDescriptor{{
					Kind:         agentic.ContextSystemPrompt,
					SystemPrompt: &agentic.SystemPromptContext{Text: legacy.text},
				}}

				_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
				if !errors.Is(err, agentic.ErrContextDescriptorConflict) {
					t.Fatalf("BuildPlan err = %v, want ErrContextDescriptorConflict for legacy prompt %q with Curator %s intent", err, legacy.name, intent)
				}
			})
		}
	}
}

func TestBuildPlanCodexCuratorMCPRefusesHarnessProfile(t *testing.T) {
	plugin := curatorPlugins[1]
	context := validCuratorContext(plugin)
	context.MCP = validAcceptanceCuratorMCP(plugin)
	req := curatorRequest(t, plugin, &context)
	req.Profile = "other-profile"

	_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for Curator MCP plus harness profile", err)
	}
}

func TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector(t *testing.T) {
	plugin := curatorPlugins[1]
	context := validCuratorContext(plugin)
	context.MCP = validAcceptanceCuratorMCP(plugin)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "short-separated", args: []string{"-p", "other-profile"}},
		{name: "short-equals", args: []string{"-p=other-profile"}},
		{name: "short-attached", args: []string{"-pother-profile"}},
		{name: "long-separated", args: []string{"--profile", "other-profile"}},
		{name: "long-equals", args: []string{"--profile=other-profile"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := curatorRequest(t, plugin, &context)
			req.NativeArgs = tc.args
			plan, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrContextDescriptorConflict) {
				t.Fatalf("BuildPlan err = %v, want ErrContextDescriptorConflict for native profile selector %q (argv=%#v)", err, tc.args, plan.Argv)
			}
		})
	}
}

func TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig(t *testing.T) {
	plugin := curatorPlugins[1]
	context := validCuratorContext(plugin)
	context.MCP = validAcceptanceCuratorMCP(plugin)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "nested/short-separated", args: []string{"-c", `mcp_servers.evil.command="evil"`}},
		{name: "nested/short-attached", args: []string{`-cmcp_servers.evil.command="evil"`}},
		{name: "nested/long-separated", args: []string{"--config", `mcp_servers.evil.command="evil"`}},
		{name: "nested/long-equals", args: []string{`--config=mcp_servers.evil.command="evil"`}},
		{name: "root/short-separated", args: []string{"-c", `mcp_servers={}`}},
		{name: "root/short-attached", args: []string{`-cmcp_servers={}`}},
		{name: "root/long-separated", args: []string{"--config", `mcp_servers={}`}},
		{name: "root/long-equals", args: []string{`--config=mcp_servers={}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := curatorRequest(t, plugin, &context)
			req.NativeArgs = tc.args
			_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrContextDescriptorConflict) {
				t.Fatalf("BuildPlan err = %v, want ErrContextDescriptorConflict for native MCP config spelling %q", err, tc.name)
			}
		})
	}
}

func TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig(t *testing.T) {
	plugin := curatorPlugins[1]
	context := validCuratorContext(plugin)
	context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptReplace)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "short-separated", args: []string{"-c", `model_instructions_file="other.md"`}},
		{name: "short-attached", args: []string{`-cmodel_instructions_file="other.md"`}},
		{name: "long-separated", args: []string{"--config", `model_instructions_file="other.md"`}},
		{name: "long-equals", args: []string{`--config=model_instructions_file="other.md"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := curatorRequest(t, plugin, &context)
			req.NativeArgs = tc.args
			_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrContextDescriptorConflict) {
				t.Fatalf("BuildPlan err = %v, want ErrContextDescriptorConflict for native model_instructions_file spelling %q", err, tc.name)
			}
		})
	}
}

func TestBuildPlanCuratorSystemPromptIntentIsRequired(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			descriptor := validDefaultSystemPromptDescriptor(plugin)
			context.SystemPrompt = &agentic.CuratorSystemPromptContext{
				Path:     "/managed/home/.agent-context/system-prompt.md",
				Channels: []agentic.CuratorChannelDescriptor{descriptor},
			}
			_, err := buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorSystemPromptIntentMissing) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorSystemPromptIntentMissing", err)
			}
		})
	}
}

func TestBuildPlanCuratorSystemPromptIntentRejectsUnknownValue(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			for _, intent := range []string{"append-and-replace", "CuratorSystemPromptAppend__unmapped__", "CuratorSystemPromptReplace__unmapped__"} {
				t.Run(intent, func(t *testing.T) {
					context := validCuratorContext(plugin)
					descriptor := validDefaultSystemPromptDescriptor(plugin)
					context.SystemPrompt = &agentic.CuratorSystemPromptContext{
						Path:     "/managed/home/.agent-context/system-prompt.md",
						Channels: []agentic.CuratorChannelDescriptor{descriptor},
						Intent:   agentic.CuratorSystemPromptIntent(intent),
					}
					_, err := buildCuratorPlan(t, plugin, context)
					if !errors.Is(err, agentic.ErrCuratorSystemPromptIntentInvalid) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorSystemPromptIntentInvalid", err)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			kind := agentic.CuratorDescriptorFlag
			if plugin.system == "codex" {
				kind = agentic.CuratorDescriptorConfigKey
			}
			context.SystemPrompt = &agentic.CuratorSystemPromptContext{
				Path: "/managed/home/.agent-context/system-prompt.md",
				Channels: []agentic.CuratorChannelDescriptor{
					validSystemPromptDescriptor(plugin, kind, agentic.CuratorSystemPromptReplace),
				},
				Intent: agentic.CuratorSystemPromptAppend,
			}
			_, err := buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorSystemPromptChannelMissing) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorSystemPromptChannelMissing", err)
			}
		})
	}
}

func TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			descriptor := validDefaultSystemPromptDescriptor(plugin)
			context.SystemPrompt = &agentic.CuratorSystemPromptContext{
				Path:     "/managed/home/.agent-context/system-prompt.md",
				Channels: []agentic.CuratorChannelDescriptor{descriptor, descriptor},
				Intent:   descriptor.Semantics,
			}
			_, err := buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorSystemPromptChannelAmbiguous) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorSystemPromptChannelAmbiguous", err)
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesMissingProfile(t *testing.T) {
	for _, plugin := range curatorPlugins {
		for _, missing := range []string{"name", "lock hash"} {
			t.Run(plugin.name+"/"+missing, func(t *testing.T) {
				context := validCuratorContext(plugin)
				if missing == "name" {
					context.Profile.Name = ""
				} else {
					context.Profile.LockSHA256 = ""
				}
				_, err := buildCuratorPlan(t, plugin, context)
				if !errors.Is(err, agentic.ErrCuratorProfileMissing) {
					t.Fatalf("BuildPlan err = %v, want ErrCuratorProfileMissing", err)
				}
			})
		}
	}
}

func TestBuildPlanCuratorContextRefusesUnknownFragmentRevision(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			for _, revision := range []string{"launch-env-fragment-v9", agentic.CuratorLaunchFragmentV2, "CuratorLaunchFragmentV1__unmapped__"} {
				t.Run(strings.ReplaceAll(revision, "/", "_"), func(t *testing.T) {
					context := validCuratorContext(plugin)
					context.Revision = revision
					_, err := buildCuratorPlan(t, plugin, context)
					if !errors.Is(err, agentic.ErrCuratorFragmentRevisionUnsupported) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorFragmentRevisionUnsupported", err)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesPermissionFragmentV2(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.Revision = agentic.CuratorLaunchFragmentV2
			_, err := buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorFragmentRevisionUnsupported) {
				t.Fatalf("BuildPlan err = %v, want v2 refusal until a typed permission mapping exists", err)
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesMalformedFragment(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			type malformedCase struct {
				name        string
				wantError   error
				wantMessage string
				mutate      func(*agentic.CuratorContext, *agentic.LaunchRequest)
			}
			cases := []malformedCase{
				{name: "profile pin invalid", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.Profile.LockSHA256 = "bad" }},
				{name: "profile-name-malformed", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.Profile.Name = "bad name" }},
				{name: "empty-profile-name-preempted", wantError: agentic.ErrCuratorProfileMissing, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.Profile.Name = "" }},
				{name: "lockhash-upper", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Profile.LockSHA256 = strings.Repeat("a", 63) + "A"
				}},
				{name: "environment-extra", wantMessage: "agentic: malformed typed Curator context: environment is not a supported typed fragment value", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.Environment = "future_env" }},
				{name: "precedence-winner", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.Precedence.Winner = "" }},
				{name: "precedence-placement", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Precedence.Placement = "middle"
				}},
				{name: "env-two-entries", wantMessage: "agentic: malformed typed Curator context: exactly one managed-home environment entry is required", mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
					context.Env["UNEXPECTED_HOME"] = "/managed/home"
					req.Home = "/different/home"
				}},
				{name: "env-no-entries", wantMessage: "agentic: malformed typed Curator context: exactly one managed-home environment entry is required", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.Env = map[string]string{} }},
				{name: "home-variable-any", mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
					wrongHomeVariable := "CODEX_HOME"
					if plugin.system == "codex" {
						wrongHomeVariable = "CLAUDE_CONFIG_DIR"
					}
					context.Env = map[string]string{wrongHomeVariable: "/managed/home"}
					req.Home = "/managed/home"
				}},
				{name: "home mismatch", mutate: func(_ *agentic.CuratorContext, req *agentic.LaunchRequest) { req.Home = "/different/home" }},
				{name: "home relative", mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
					context.Env[plugin.homeVariable] = "relative/home"
					req.Home = "relative/home"
				}},
				{name: "path-prepend relative", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.PathPrepend = "relative/bin" }},
				{name: "path-prepend nonempty unlisted", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) { context.PathPrepend = "__unmapped__" }},
				{name: "path-prepend absolute but unsupported", wantError: agentic.ErrCuratorContextUnsupported, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.PathPrepend = "/managed/home/bin"
				}},
				{name: "MCP context on pi", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Environment = "pi"
					context.Env = map[string]string{"PI_CODING_AGENT_DIR": "/managed/home"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "relative MCP path", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "relative/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "empty MCP path", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "MCP env names unsorted", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{"FIGMA_API_KEY", "DOCS_TOKEN"}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "MCP env names duplicate", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{"DOCS_TOKEN", "DOCS_TOKEN"}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "MCP env names reserved", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{"PATH"}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "mcp-two-channels", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor, descriptor}}
				}},
				{name: "mcp-no-channels", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{}}
				}},
				{name: "mcp-semantics", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Semantics = agentic.CuratorSystemPromptAppend
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "mcp-semantics-unknown", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Semantics = "__unmapped__"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "unsorted MCP env names", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{"FIGMA_API_KEY", "DOCS_TOKEN"}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "sp-relative-path", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Path = "rel/prompt.md"
				}},
				{name: "system-prompt intent missing", wantError: agentic.ErrCuratorSystemPromptIntentMissing, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Intent = ""
				}},
				{name: "system-prompt intent invalid", wantError: agentic.ErrCuratorSystemPromptIntentInvalid, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Intent = "prepend"
				}},
				{name: "system-prompt descriptor semantics invalid", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Channels[0].Semantics = "prepend"
				}},
				{name: "system-prompt descriptor semantics empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Channels[0].Semantics = ""
				}},
				{name: "flag invalid", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Flag = "not-a-flag"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "flag empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Flag = ""
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "flag length-one", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Flag = "-"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "flag argument kind invalid", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Argument, descriptor.Name = "environment", ""
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "flag argument kind empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Argument, descriptor.Name = "", ""
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "name argument invalid", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Argument, descriptor.Name = agentic.CuratorArgumentName, "bad name"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "name argument empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Argument, descriptor.Name = agentic.CuratorArgumentName, ""
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "config-key key empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable value empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "name on path argument", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFlag, Flag: "--mcp-config", Argument: agentic.CuratorArgumentPath, Name: "unexpected"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "name on path argument unmapped", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFlag, Flag: "--mcp-config", Argument: agentic.CuratorArgumentPath, Name: "__unmapped__"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "name on contents argument", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFlag, Flag: "--mcp-config", Argument: agentic.CuratorArgumentContents, Name: "unexpected"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "with-empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.With = []string{}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "with-invalid-companion", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.With = []string{"bad flag"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "with-empty-string companion", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.With = []string{""}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "with-repeats", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.With = []string{"--dup", "--dup"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "with-repeats-other companion", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.With = []string{"--other", "--other"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "descriptor union arm mixed", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Key = "unexpected"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "flag carries variable", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Variable = "UNEXPECTED_VARIABLE"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "flag carries filename", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Filename = "unexpected.toml"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "flag carries key", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Key = "unexpected_key"
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "configkey-malformed", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "bad key"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "config-key carries flag", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "valid_key", Flag: "--mcp-config"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "config-key carries argument", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "valid_key", Argument: agentic.CuratorArgumentPath}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "config-key carries name", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "valid_key", Name: "unexpected"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "config-key carries variable", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "valid_key", Variable: "UNEXPECTED_VARIABLE"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "config-key carries filename", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "valid_key", Filename: "unexpected.toml"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "config-key carries with", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "valid_key", With: []string{"--extra"}}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable descriptor malformed", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: "bad variable"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable carries flag", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: "VALID_VARIABLE", Flag: "--mcp-config"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable carries argument", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: "VALID_VARIABLE", Argument: agentic.CuratorArgumentPath}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable carries name", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: "VALID_VARIABLE", Name: "unexpected"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable carries key", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: "VALID_VARIABLE", Key: "unexpected_key"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable carries filename", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: "VALID_VARIABLE", Filename: "unexpected.toml"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "variable carries with", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: "VALID_VARIABLE", With: []string{"--extra"}}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor malformed", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "../outside.toml"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor empty filename", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: ""}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "portable-path-absolute", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "/__unmapped__"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor absolute path", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "/etc/curator.toml"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor carries flag", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "config.toml", Flag: "--mcp-config"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor carries argument", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "config.toml", Argument: agentic.CuratorArgumentPath}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor carries name", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "config.toml", Name: "unexpected"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor carries key", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "config.toml", Key: "unexpected_key"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor carries variable", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "config.toml", Variable: "UNEXPECTED_VARIABLE"}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "file descriptor carries with", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "config.toml", With: []string{"--extra"}}
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}},
				{name: "mixed tagged union members", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFlag, Flag: "--mcp-config", Argument: agentic.CuratorArgumentPath, Key: "unexpected"}}}
				}},
			}
			for _, environment := range []string{"claude_code", "codex_cli", "opencode", "pi"} {
				unmapped := environment + "__unmapped__"
				environmentValue := unmapped
				cases = append(cases, malformedCase{name: "environment-enum-" + environment, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Environment = environmentValue
					context.Env = map[string]string{"": "/managed/home"}
				}})
			}
			for _, member := range []struct {
				field string
				value string
			}{
				{"winner", "higher-weight__unmapped__"}, {"winner", "lower-weight__unmapped__"},
				{"placement", "winner-last__unmapped__"}, {"placement", "winner-first__unmapped__"},
			} {
				member := member
				cases = append(cases, malformedCase{name: "precedence-" + member.field + "-" + member.value, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					if member.field == "winner" {
						context.Precedence.Winner = member.value
					} else {
						context.Precedence.Placement = member.value
					}
				}})
			}
			for _, reserved := range []string{
				"PATH", "HOME", "TMPDIR", "TEMP", "TMP",
				"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME",
				"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY",
				"http_proxy", "https_proxy", "all_proxy", "ftp_proxy", "no_proxy",
				"RES_OPTIONS", "HOSTALIASES", "LOCALDOMAIN", "IFS", "CSK_PROJECT_ROOT",
				"USERPROFILE", "APPDATA", "LOCALAPPDATA", "PATHEXT", "COMSPEC",
				"WINDIR", "SYSTEMROOT", "__PYVENV_LAUNCHER__",
			} {
				reserved := reserved
				cases = append(cases, malformedCase{name: "env-reserved-" + reserved, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{reserved}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}})
			}
			for _, reserved := range []string{"LD_MUTANT", "DYLD_MUTANT", "PYTHONMUTANT", "PYTHON_MUTANT", "NODE_MUTANT", "NPM_CONFIG_MUTANT"} {
				reserved := reserved
				cases = append(cases, malformedCase{name: "env-reserved-prefix-" + reserved, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{reserved}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}})
			}
			identifierWitnesses := []string{
				"con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
				"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9",
				"-bad", "bad.", "a`", "a.", "@a", "[a", "`a", "{a", "/a", ":a", "a@b", strings.Repeat("a", 129),
			}
			for _, identifier := range identifierWitnesses {
				identifier := identifier
				cases = append(cases, malformedCase{name: "profile-identifier-" + identifier, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Profile.Name = identifier
				}})
			}
			identifierArms := []struct {
				name string
				make func(string) agentic.CuratorChannelDescriptor
			}{
				{name: "config-key", make: func(value string) agentic.CuratorChannelDescriptor {
					return agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: value}
				}},
				{name: "variable", make: func(value string) agentic.CuratorChannelDescriptor {
					return agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorVariable, Variable: value}
				}},
				{name: "name-argument", make: func(value string) agentic.CuratorChannelDescriptor {
					return agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFlag, Flag: "--mcp-config", Argument: agentic.CuratorArgumentName, Name: value}
				}},
			}
			for _, arm := range identifierArms {
				arm := arm
				for _, identifier := range identifierWitnesses {
					identifier := identifier
					caseIdentifier := strings.ReplaceAll(strings.ReplaceAll(identifier, "/", "-"), " ", "-")
					cases = append(cases, malformedCase{name: arm.name + "-identifier-" + caseIdentifier, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
						descriptor := arm.make(identifier)
						context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
					}})
				}
			}
			for _, identifier := range identifierWitnesses {
				identifier := identifier
				caseIdentifier := strings.ReplaceAll(strings.ReplaceAll(identifier, "/", "-"), " ", "-")
				cases = append(cases, malformedCase{name: "system-prompt-descriptor-identifier-" + caseIdentifier, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					prompt := validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					prompt.Channels = []agentic.CuratorChannelDescriptor{{
						Kind: agentic.CuratorDescriptorConfigKey, Key: identifier, Semantics: agentic.CuratorSystemPromptAppend,
					}}
					context.SystemPrompt = prompt
				}})
			}
			cases = append(cases, malformedCase{name: "system-prompt-descriptor-identifier-empty", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
				prompt := validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
				prompt.Channels = []agentic.CuratorChannelDescriptor{{
					Kind: agentic.CuratorDescriptorConfigKey, Key: "", Semantics: agentic.CuratorSystemPromptAppend,
				}}
				context.SystemPrompt = prompt
			}})
			for _, filename := range []string{"config.toml.", "prn.cfg", "sub\\config.toml", "config\x00.toml", "config\x01.toml", "__unmapped__.", "__unmapped__ ", "__unmapped__//__member__", "/__unmapped__"} {
				filename := filename
				cases = append(cases, malformedCase{name: "file-fragment-" + strings.ReplaceAll(strings.ReplaceAll(filename, "\\", "-"), "\x00", "nul"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFile, Filename: filename}}}
				}})
			}
			for _, filename := range []string{"sub:config.toml", "config//child.toml", "config/", "./config.toml", "sub/../config.toml", "config.toml ", "a<.toml", "a>.toml", "a\".toml", "a|.toml", "a?.toml", "a*.toml"} {
				filename := filename
				cases = append(cases, malformedCase{name: "file-fragment-" + strings.ReplaceAll(strings.ReplaceAll(filename, "/", "-"), " ", "-"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFile, Filename: filename}}}
				}})
			}
			for _, reserved := range []string{"con.toml", "prn.toml", "aux.toml", "nul.toml", "com1.toml", "com2.toml", "com3.toml", "com4.toml", "com5.toml", "com6.toml", "com7.toml", "com8.toml", "com9.toml", "lpt1.toml", "lpt2.toml", "lpt3.toml", "lpt4.toml", "lpt5.toml", "lpt6.toml", "lpt7.toml", "lpt8.toml", "lpt9.toml"} {
				reserved := reserved
				cases = append(cases, malformedCase{name: "file-reserved-name-" + reserved, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFile, Filename: reserved}}}
				}})
			}
			for _, hash := range []struct {
				name  string
				value string
			}{
				{"length-63", strings.Repeat("a", 63)},
				{"length-65", strings.Repeat("a", 65)},
				{"character-before-digit-range", strings.Repeat("a", 31) + "/" + strings.Repeat("a", 32)},
				{"character-between-digit-and-hex-ranges", strings.Repeat("a", 31) + ":" + strings.Repeat("a", 32)},
				{"character-before-hex-range", strings.Repeat("a", 31) + "`" + strings.Repeat("a", 32)},
				{"character-after-hex-range", strings.Repeat("a", 31) + "g" + strings.Repeat("a", 32)},
			} {
				hash := hash
				cases = append(cases, malformedCase{name: "lockhash-" + hash.name, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.Profile.LockSHA256 = hash.value
				}})
			}
			for _, invalidName := range []string{"", "1INVALID", "BAD-NAME", "BAD NAME"} {
				invalidName := invalidName
				name := strings.ReplaceAll(invalidName, " ", "-")
				if name == "" {
					name = "empty"
				}
				cases = append(cases, malformedCase{name: "mcp-env-invalid-name-" + name, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{invalidName}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}})
			}
			longPortablePath := strings.Repeat("a", 4097)
			longAbsolutePath := "/" + strings.Repeat("a", 4096)
			for _, witness := range []struct {
				name     string
				filename string
			}{
				{name: "empty", filename: ""},
				{name: "length-4097", filename: longPortablePath},
				{name: "absolute", filename: "/__unmapped__"},
				{name: "backslash", filename: "sub\\config.toml"},
				{name: "colon", filename: "sub:config.toml"},
				{name: "double-separator", filename: "__unmapped__//__member__"},
				{name: "nul", filename: "config\x00.toml"},
				{name: "control", filename: "config\x01.toml"},
				{name: "empty-trailing-segment", filename: "config/"},
				{name: "dot-segment", filename: "./config.toml"},
				{name: "parent-segment", filename: "sub/../config.toml"},
				{name: "trailing-dot", filename: "__unmapped__."},
				{name: "trailing-space", filename: "__unmapped__ "},
				{name: "invalid-less-than", filename: "a<.toml"},
				{name: "invalid-greater-than", filename: "a>.toml"},
				{name: "invalid-quote", filename: "a\".toml"},
				{name: "invalid-pipe", filename: "a|.toml"},
				{name: "invalid-question", filename: "a?.toml"},
				{name: "invalid-star", filename: "a*.toml"},
				{name: "reserved-con", filename: "con.toml"},
				{name: "reserved-prn", filename: "prn.toml"},
				{name: "reserved-aux", filename: "aux.toml"},
				{name: "reserved-nul", filename: "nul.toml"},
			} {
				witness := witness
				cases = append(cases, malformedCase{name: "system-prompt-descriptor-file-" + witness.name, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					prompt := validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					prompt.Channels = []agentic.CuratorChannelDescriptor{{
						Kind: agentic.CuratorDescriptorFile, Filename: witness.filename, Semantics: agentic.CuratorSystemPromptAppend,
					}}
					context.SystemPrompt = prompt
				}})
			}
			for _, prefix := range []string{"com", "lpt"} {
				for digit := '1'; digit <= '9'; digit++ {
					filename := prefix + string(digit) + ".toml"
					caseName := "system-prompt-descriptor-file-reserved-" + prefix + string(digit)
					cases = append(cases, malformedCase{name: caseName, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
						prompt := validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
						prompt.Channels = []agentic.CuratorChannelDescriptor{{
							Kind: agentic.CuratorDescriptorFile, Filename: filename, Semantics: agentic.CuratorSystemPromptAppend,
						}}
						context.SystemPrompt = prompt
					}})
				}
			}
			cases = append(cases,
				malformedCase{name: "portable-path-length-4097", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFile, Filename: longPortablePath}}}
				}},
				malformedCase{name: "absolute-path-length-over-4096", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: longAbsolutePath, EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				malformedCase{name: "absolute-path-too-short", mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: "/", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
			)
			pathWitnesses := []struct {
				name  string
				value string
			}{
				{name: "too-short", value: "/"},
				{name: "without-leading-slash", value: "__unmapped__/"},
				{name: "parent-segment", value: "/managed/../outside"},
				{name: "nul", value: "/managed/\x00/outside"},
				{name: "length-over-4096", value: longAbsolutePath},
			}
			pathOwners := []struct {
				name  string
				apply func(*agentic.CuratorContext, *agentic.LaunchRequest, string)
			}{
				{name: "managed-home-path", apply: func(context *agentic.CuratorContext, req *agentic.LaunchRequest, value string) {
					context.Env = map[string]string{plugin.homeVariable: value}
					req.Home = value
				}},
				{name: "path-prepend-path", apply: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest, value string) {
					context.PathPrepend = value
				}},
				{name: "mcp-path", apply: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest, value string) {
					context.MCP = &agentic.CuratorMCPContext{Path: value, EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}},
				{name: "system-prompt-path", apply: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest, value string) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Path = value
				}},
			}
			for _, owner := range pathOwners {
				owner := owner
				for _, witness := range pathWitnesses {
					witness := witness
					cases = append(cases, malformedCase{name: owner.name + "-" + witness.name, mutate: func(context *agentic.CuratorContext, req *agentic.LaunchRequest) {
						owner.apply(context, req, witness.value)
					}})
				}
			}
			for _, flagValue := range []string{"--bad flag", "--bad\tflag", "--bad\nflag", "--bad\x01flag", "__unmapped__-"} {
				flagValue := flagValue
				cases = append(cases, malformedCase{name: "flag-content-" + strings.ReplaceAll(strings.ReplaceAll(flagValue, "-", ""), "\x00", "nul"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Flag = flagValue
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}})
			}
			for _, name := range []string{"append__unmapped__", "replace__unmapped__", "CuratorSystemPromptAppend__unmapped__", "CuratorSystemPromptReplace__unmapped__"} {
				name := name
				cases = append(cases, malformedCase{name: "system-prompt-semantics-" + name, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Channels[0].Semantics = agentic.CuratorSystemPromptIntent(name)
				}})
			}
			for _, kind := range []string{"CuratorDescriptorFlag", "CuratorDescriptorConfigKey", "CuratorDescriptorVariable", "CuratorDescriptorFile"} {
				kind := kind
				cases = append(cases, malformedCase{name: "descriptor-kind-enum-" + kind, wantError: agentic.ErrUnknownCuratorDescriptor, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Kind = agentic.CuratorDescriptorKind(kind + "__unmapped__")
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}})
			}
			for _, argument := range []string{"CuratorArgumentPath__unmapped__", "CuratorArgumentContents__unmapped__", "CuratorArgumentName__unmapped__"} {
				argument := argument
				cases = append(cases, malformedCase{name: "descriptor-argument-enum-" + argument, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Argument = agentic.CuratorFlagArgument(argument)
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}})
			}
			for _, flagValue := range []string{"--bad\x00flag", "-" + strings.Repeat("a", 128)} {
				flagValue := flagValue
				cases = append(cases, malformedCase{name: "flag-boundary-" + strings.ReplaceAll(flagValue, "\x00", "nul"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Flag = flagValue
					context.MCP = &agentic.CuratorMCPContext{Path: "/managed/home/mcp.json", EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{descriptor}}
				}})
			}
			for _, path := range []string{"/managed/../etc/mcp.json", "/managed/\x00/etc/mcp.json", "__unmapped__/"} {
				path := path
				cases = append(cases, malformedCase{name: "absolute-path-" + strings.ReplaceAll(path, "\x00", "nul"), mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.MCP = &agentic.CuratorMCPContext{Path: path, EnvNames: []string{}, Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)}}
				}})
			}
			for _, intent := range []string{"CuratorSystemPromptAppend__unmapped__", "CuratorSystemPromptReplace__unmapped__"} {
				intent := intent
				cases = append(cases, malformedCase{name: "system-prompt-intent-enum-" + intent, wantError: agentic.ErrCuratorSystemPromptIntentInvalid, mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, agentic.CuratorSystemPromptAppend)
					context.SystemPrompt.Intent = agentic.CuratorSystemPromptIntent(intent)
				}})
			}
			for _, unselected := range []agentic.CuratorSystemPromptIntent{
				agentic.CuratorSystemPromptAppend,
				agentic.CuratorSystemPromptReplace,
			} {
				unselected := unselected
				selected := agentic.CuratorSystemPromptAppend
				channelName := "append"
				if unselected == selected {
					selected = agentic.CuratorSystemPromptReplace
				} else {
					channelName = "replace"
				}
				for _, malformed := range []struct {
					name    string
					wantErr error
					make    func() agentic.CuratorChannelDescriptor
				}{
					{name: "argument-kind-path", make: func() agentic.CuratorChannelDescriptor {
						descriptor := validSystemPromptDescriptor(plugin, agentic.CuratorDescriptorFlag, unselected)
						descriptor.Argument = agentic.CuratorFlagArgument("CuratorArgumentPath__unmapped__")
						return descriptor
					}},
					{name: "argument-kind-environment", make: func() agentic.CuratorChannelDescriptor {
						descriptor := validSystemPromptDescriptor(plugin, agentic.CuratorDescriptorFlag, unselected)
						descriptor.Argument = "environment"
						return descriptor
					}},
					{name: "descriptor-kind", wantErr: agentic.ErrUnknownCuratorDescriptor, make: func() agentic.CuratorChannelDescriptor {
						return agentic.CuratorChannelDescriptor{Kind: "CuratorDescriptorFlag__unmapped__", Semantics: unselected}
					}},
					{name: "config-key-reserved", make: func() agentic.CuratorChannelDescriptor {
						return agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorConfigKey, Key: "con", Semantics: unselected}
					}},
					{name: "filename-invalid-character", make: func() agentic.CuratorChannelDescriptor {
						return agentic.CuratorChannelDescriptor{Kind: agentic.CuratorDescriptorFile, Filename: "a?.toml", Semantics: unselected}
					}},
					{name: "flag-whitespace", make: func() agentic.CuratorChannelDescriptor {
						descriptor := validSystemPromptDescriptor(plugin, agentic.CuratorDescriptorFlag, unselected)
						descriptor.Flag = "--bad flag"
						return descriptor
					}},
				} {
					malformed := malformed
					cases = append(cases, malformedCase{
						name:      fmt.Sprintf("system-prompt-unselected-%s-%s", channelName, malformed.name),
						wantError: malformed.wantErr,
						mutate: func(context *agentic.CuratorContext, _ *agentic.LaunchRequest) {
							prompt := validAcceptanceCuratorSystemPrompt(plugin, selected)
							prompt.Channels = append(prompt.Channels, malformed.make())
							context.SystemPrompt = prompt
						},
					})
				}
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					context := validCuratorContext(plugin)
					req := curatorRequest(t, plugin, &context)
					tc.mutate(&context, &req)
					req.Context = &context
					wantError := tc.wantError
					if wantError == nil {
						wantError = agentic.ErrCuratorContextMalformed
					}
					_, err := agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
					if !errors.Is(err, wantError) {
						t.Fatalf("BuildPlan err = %v, want %v", err, wantError)
					}
					if tc.wantMessage != "" && !strings.HasSuffix(err.Error(), tc.wantMessage) {
						t.Fatalf("BuildPlan err = %q, want refusal diagnostic suffix %q", err, tc.wantMessage)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesUnknownDescriptorKind(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			for _, kind := range []string{
				"future-channel", "CuratorDescriptorFlag__unmapped__", "CuratorDescriptorConfigKey__unmapped__",
				"CuratorDescriptorVariable__unmapped__", "CuratorDescriptorFile__unmapped__",
			} {
				t.Run(kind, func(t *testing.T) {
					context := validCuratorContext(plugin)
					descriptor := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)
					descriptor.Kind = agentic.CuratorDescriptorKind(kind)
					context.MCP = &agentic.CuratorMCPContext{
						Path:     "/managed/home/mcp.json",
						EnvNames: []string{},
						Channels: []agentic.CuratorChannelDescriptor{descriptor},
					}
					_, err := buildCuratorPlan(t, plugin, context)
					if !errors.Is(err, agentic.ErrUnknownCuratorDescriptor) {
						t.Fatalf("BuildPlan err = %v, want ErrUnknownCuratorDescriptor", err)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesStaleIdentity(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			members := []struct {
				name   string
				setup  func(*agentic.CuratorContext)
				mutate func(*agentic.CuratorContext)
			}{
				{name: "profile name", mutate: func(context *agentic.CuratorContext) { context.Profile.Name = "changed-profile" }},
				{name: "profile lock hash", mutate: func(context *agentic.CuratorContext) { context.Profile.LockSHA256 = strings.Repeat("b", 64) }},
				{name: "managed home", mutate: func(context *agentic.CuratorContext) { context.Env[plugin.homeVariable] = "/managed/other-home" }},
				{name: "managed home variable", mutate: func(context *agentic.CuratorContext) {
					other := curatorPlugins[1]
					if plugin.name == other.name {
						other = curatorPlugins[0]
					}
					context.Environment = other.environment
					context.Env = map[string]string{other.homeVariable: "/managed/home"}
				}},
				{name: "system prompt intent", setup: func(context *agentic.CuratorContext) {
					intent := agentic.CuratorSystemPromptAppend
					if plugin.system == "codex" {
						intent = agentic.CuratorSystemPromptReplace
					}
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, intent)
				}, mutate: func(context *agentic.CuratorContext) {
					intent := agentic.CuratorSystemPromptAppend
					if context.SystemPrompt.Intent == intent {
						intent = agentic.CuratorSystemPromptReplace
					}
					context.SystemPrompt = validAcceptanceCuratorSystemPrompt(plugin, intent)
				}},
				{name: "fragment identity", setup: func(context *agentic.CuratorContext) {
					context.MCP = &agentic.CuratorMCPContext{
						Path:     "/managed/home/mcp.json",
						EnvNames: []string{"FIGMA_API_KEY"},
						Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)},
					}
				}, mutate: func(context *agentic.CuratorContext) { context.MCP.Path = "/managed/home/changed-mcp.json" }},
			}
			for _, member := range members {
				t.Run(member.name, func(t *testing.T) {
					context := validCuratorContext(plugin)
					if member.setup != nil {
						member.setup(&context)
					}
					plan, err := buildCuratorPlan(t, plugin, context)
					if err != nil {
						t.Fatalf("initial BuildPlan: %v", err)
					}
					stored, ok := plan.CuratorContextProvenanceSnapshot()
					if !ok {
						t.Fatal("initial plan has no Curator provenance")
					}
					member.mutate(&context)
					context.ExpectedProvenance = &stored
					_, err = buildCuratorPlan(t, plugin, context)
					if !errors.Is(err, agentic.ErrCuratorContextStale) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorContextStale", err)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesIncompatibleCapability(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.SystemPrompt = &agentic.CuratorSystemPromptContext{
				Path: "/managed/home/.agent-context/system-prompt.md",
				Channels: []agentic.CuratorChannelDescriptor{{
					Kind:      agentic.CuratorDescriptorVariable,
					Variable:  "SYSTEM_PROMPT_FILE",
					Semantics: agentic.CuratorSystemPromptAppend,
				}},
				Intent: agentic.CuratorSystemPromptAppend,
			}
			_, err := buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported", err)
			}
		})
		t.Run(plugin.name+"/environment", func(t *testing.T) {
			fragmentEnv := plugin
			fragmentEnv.environment = "pi"
			fragmentEnv.homeVariable = "PI_CODING_AGENT_DIR"
			context := validCuratorContext(fragmentEnv)
			_, err := buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported for the unimplemented pi plugin", err)
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesInconsistentStoredProvenance(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			descriptor := validDefaultSystemPromptDescriptor(plugin)
			context.SystemPrompt = &agentic.CuratorSystemPromptContext{
				Path:     "/managed/home/.agent-context/system-prompt.md",
				Channels: []agentic.CuratorChannelDescriptor{descriptor},
				Intent:   descriptor.Semantics,
			}
			plan, err := buildCuratorPlan(t, plugin, context)
			if err != nil {
				t.Fatalf("initial BuildPlan: %v", err)
			}
			stored, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("initial plan has no Curator provenance")
			}
			cases := []struct {
				name   string
				mutate func(*agentic.CuratorContextProvenance)
			}{
				{name: "profile summary", mutate: func(value *agentic.CuratorContextProvenance) { value.ProfileName = "other-profile" }},
				{name: "lock summary", mutate: func(value *agentic.CuratorContextProvenance) { value.LockSHA256 = strings.Repeat("b", 64) }},
				{name: "managed-home variable", mutate: func(value *agentic.CuratorContextProvenance) { value.ManagedHomeVariable = "OTHER_HOME" }},
				{name: "managed-home value", mutate: func(value *agentic.CuratorContextProvenance) { value.ManagedHome = "/other/home" }},
				{name: "system-prompt intent", mutate: func(value *agentic.CuratorContextProvenance) {
					value.SystemPromptIntent = otherPromptIntent(value.SystemPromptIntent)
				}},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					context := cloneAcceptanceContext(context)
					expected := stored
					tc.mutate(&expected)
					context.ExpectedProvenance = &expected
					_, err := buildCuratorPlan(t, plugin, context)
					if !errors.Is(err, agentic.ErrCuratorContextStale) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorContextStale", err)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorContextRefusesUnregisteredPluginCapability(t *testing.T) {
	plugin := curatorPlugins[0]
	context := validCuratorContext(plugin)
	registry := agentic.NewRegistry()
	if err := registry.Register(&unsupportedCuratorSystem{}); err != nil {
		t.Fatalf("Register unsupported system: %v", err)
	}
	req := curatorRequest(t, plugin, &context)
	req.System = "curator-test"
	_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported", err)
	}
}

func TestBuildPlanCuratorContextRefusesUnsupportedPathPrepend(t *testing.T) {
	invalidPathPrependCases := []struct {
		name  string
		value string
	}{
		{name: "unlisted nonempty path", value: "__unmapped__"},
		{name: "unlisted path without slash", value: "__unmapped__/"},
		{name: "relative path", value: "relative/bin"},
		{name: "parent segment", value: "/managed/../outside"},
		{name: "nul byte", value: "/managed/\x00/outside"},
		{name: "too short", value: "/"},
		{name: "over 4096 runes", value: "/" + strings.Repeat("a", 4096)},
	}
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.PathPrepend = "/managed/environments/bin"
			_, err := buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextUnsupported", err)
			}
			for _, tc := range invalidPathPrependCases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					context := validCuratorContext(plugin)
					context.PathPrepend = tc.value
					_, err := buildCuratorPlan(t, plugin, context)
					if !errors.Is(err, agentic.ErrCuratorContextMalformed) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorContextMalformed", err)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.MCP = &agentic.CuratorMCPContext{
				Path:     "/managed/home/.agent-context/mcp/config",
				EnvNames: []string{"DOCS_TOKEN", "FIGMA_API_KEY"},
				Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)},
			}
			promptKind := agentic.CuratorDescriptorFlag
			intent := agentic.CuratorSystemPromptAppend
			if plugin.system == "codex" {
				promptKind = agentic.CuratorDescriptorConfigKey
				intent = agentic.CuratorSystemPromptReplace
			}
			context.SystemPrompt = &agentic.CuratorSystemPromptContext{
				Path: "/managed/home/.agent-context/system-prompt.md",
				Channels: []agentic.CuratorChannelDescriptor{
					validSystemPromptDescriptor(plugin, promptKind, agentic.CuratorSystemPromptAppend),
					validSystemPromptDescriptor(plugin, promptKind, agentic.CuratorSystemPromptReplace),
				},
				Intent: intent,
			}
			plan, err := buildCuratorPlan(t, plugin, context)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			first, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("plan has no Curator provenance")
			}
			wantFragment := agentic.CuratorFragmentIdentity{
				Revision: context.Revision, Environment: context.Environment,
				Profile: context.Profile, Precedence: context.Precedence,
				Env: context.Env, MCP: context.MCP, PathPrepend: context.PathPrepend,
			}
			if context.SystemPrompt != nil {
				wantFragment.SystemPrompt = &agentic.CuratorSystemPromptFragment{
					Path: context.SystemPrompt.Path, Channels: context.SystemPrompt.Channels,
				}
			}
			if first.ProfileName != context.Profile.Name || first.LockSHA256 != context.Profile.LockSHA256 ||
				first.ManagedHomeVariable != plugin.homeVariable || first.ManagedHome != context.Env[plugin.homeVariable] ||
				first.Fragment.Revision != agentic.CuratorLaunchFragmentV1 || len(first.Fragment.SystemPrompt.Channels) != 2 ||
				first.SystemPromptIntent != intent || !reflect.DeepEqual(first.Fragment, wantFragment) {
				t.Fatalf("plan provenance does not contain the complete identity snapshot: %#v", first)
			}

			context.Env[plugin.homeVariable] = "/mutated/home"
			context.MCP.Path = "/mutated/mcp.json"
			context.MCP.EnvNames[0] = "MUTATED_NAME"
			context.MCP.Channels[0].Flag = "--mutated"
			context.SystemPrompt.Channels[0].Flag = "--mutated"
			first.Fragment.Env[plugin.homeVariable] = "/mutated/returned-home"
			first.Fragment.MCP.Path = "/returned-copy-mcp-mutation.json"
			first.Fragment.MCP.EnvNames[0] = "RETURNED_COPY_MUTATION"
			first.Fragment.MCP.Channels[0].Flag = "--returned-copy-mutation"
			second, ok := plan.CuratorContextProvenanceSnapshot()
			wantMCPFlag := validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag).Flag
			if !ok || second.ManagedHome != "/managed/home" || second.Fragment.Env[plugin.homeVariable] != "/managed/home" ||
				second.Fragment.MCP.Path != "/managed/home/.agent-context/mcp/config" || second.Fragment.MCP.EnvNames[0] != "DOCS_TOKEN" ||
				second.Fragment.MCP.Channels[0].Flag != wantMCPFlag || second.Fragment.SystemPrompt.Channels[0].Flag != appendSystemPromptFileFlagFor(plugin) {
				t.Fatalf("plan provenance changed after request or returned-copy mutation: %#v", second)
			}
		})
	}
}

type unsupportedCuratorSystem struct{}

func (*unsupportedCuratorSystem) ID() agentic.SystemID { return "curator-test" }
func (*unsupportedCuratorSystem) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{LaunchModes: []agentic.LaunchMode{agentic.LaunchModeExec}, EffortTransport: agentic.EffortTransportArgv}
}
func (*unsupportedCuratorSystem) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return "curator-test", nil
}
func (*unsupportedCuratorSystem) Argv(agentic.LaunchRequest, agentic.LaunchMode) ([]string, error) {
	return nil, nil
}
func (*unsupportedCuratorSystem) ChildEnv(parent []string, _ agentic.LaunchRequest) ([]string, error) {
	return parent, nil
}
func (*unsupportedCuratorSystem) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}
func (*unsupportedCuratorSystem) ValidateComposition(agentic.Composition) error { return nil }

func curatorRegistry(t *testing.T) *agentic.Registry {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(claude.New()); err != nil {
		t.Fatalf("Register Claude: %v", err)
	}
	if err := registry.Register(codex.New()); err != nil {
		t.Fatalf("Register Codex: %v", err)
	}
	return registry
}

func buildCuratorPlan(t *testing.T, plugin curatorPluginCase, context agentic.CuratorContext) (agentic.Plan, error) {
	t.Helper()
	req := curatorRequest(t, plugin, &context)
	return agentic.BuildPlan(curatorRegistry(t), req, agentic.LaunchModeExec)
}

func curatorRequest(t *testing.T, plugin curatorPluginCase, context *agentic.CuratorContext) agentic.LaunchRequest {
	t.Helper()
	binDir := t.TempDir()
	name := "claude"
	if plugin.system == "codex" {
		name = "codex"
	}
	binaryPath := filepath.Join(binDir, name)
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write stub executable: %v", err)
	}
	homeVariable := "CLAUDE_CONFIG_DIR"
	switch context.Environment {
	case "codex_cli":
		homeVariable = "CODEX_HOME"
	case "opencode":
		homeVariable = "XDG_CONFIG_HOME"
	case "pi":
		homeVariable = "PI_CODING_AGENT_DIR"
	}
	return agentic.LaunchRequest{
		System:  plugin.system,
		Model:   agentic.Model{ID: "curator-context-test"},
		WorkDir: t.TempDir(),
		Home:    context.Env[homeVariable],
		Env:     []string{"PATH=" + binDir},
		Context: context,
	}
}

func cloneAcceptanceContext(source agentic.CuratorContext) agentic.CuratorContext {
	copy := source
	copy.Env = map[string]string{}
	for key, value := range source.Env {
		copy.Env[key] = value
	}
	if source.SystemPrompt != nil {
		prompt := *source.SystemPrompt
		prompt.Channels = append([]agentic.CuratorChannelDescriptor(nil), source.SystemPrompt.Channels...)
		copy.SystemPrompt = &prompt
	}
	if source.MCP != nil {
		mcp := *source.MCP
		mcp.EnvNames = append([]string(nil), source.MCP.EnvNames...)
		mcp.Channels = append([]agentic.CuratorChannelDescriptor(nil), source.MCP.Channels...)
		copy.MCP = &mcp
	}
	if source.ExpectedProvenance != nil {
		provenance := *source.ExpectedProvenance
		copy.ExpectedProvenance = &provenance
	}
	return copy
}

func validCuratorContext(plugin curatorPluginCase) agentic.CuratorContext {
	return agentic.CuratorContext{
		Revision:    agentic.CuratorLaunchFragmentV1,
		Environment: plugin.environment,
		Profile:     agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
		Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env:         map[string]string{plugin.homeVariable: "/managed/home"},
	}
}

func validAcceptanceCuratorMCP(plugin curatorPluginCase) *agentic.CuratorMCPContext {
	return &agentic.CuratorMCPContext{
		Path:     "/managed/home/.agent-context/mcp/config",
		EnvNames: []string{},
		Channels: []agentic.CuratorChannelDescriptor{validMCPDescriptor(plugin, agentic.CuratorDescriptorFlag)},
	}
}

func validAcceptanceCuratorSystemPrompt(plugin curatorPluginCase, intent agentic.CuratorSystemPromptIntent) *agentic.CuratorSystemPromptContext {
	kind := agentic.CuratorDescriptorFlag
	if plugin.system == "codex" {
		kind = agentic.CuratorDescriptorConfigKey
	}
	return &agentic.CuratorSystemPromptContext{
		Path:     "/managed/home/.agent-context/system-prompt.md",
		Channels: []agentic.CuratorChannelDescriptor{validSystemPromptDescriptor(plugin, kind, intent)},
		Intent:   intent,
	}
}

func validSystemPromptDescriptor(plugin curatorPluginCase, kind agentic.CuratorDescriptorKind, semantics agentic.CuratorSystemPromptIntent) agentic.CuratorChannelDescriptor {
	switch kind {
	case agentic.CuratorDescriptorFlag:
		flag := "--append-system-prompt-file"
		if semantics == agentic.CuratorSystemPromptReplace {
			flag = "--system-prompt-file"
		}
		if plugin.system == "codex" {
			flag = "--unsupported-prompt-file"
		}
		return agentic.CuratorChannelDescriptor{Kind: kind, Semantics: semantics, Flag: flag, Argument: agentic.CuratorArgumentPath}
	case agentic.CuratorDescriptorConfigKey:
		key := "unused_prompt_key"
		if plugin.system == "codex" {
			key = "model_instructions_file"
		}
		return agentic.CuratorChannelDescriptor{Kind: kind, Semantics: semantics, Key: key}
	case agentic.CuratorDescriptorVariable:
		return agentic.CuratorChannelDescriptor{Kind: kind, Semantics: semantics, Variable: "SYSTEM_PROMPT_FILE"}
	case agentic.CuratorDescriptorFile:
		return agentic.CuratorChannelDescriptor{Kind: kind, Semantics: semantics, Filename: "SYSTEM.md"}
	default:
		panic(fmt.Sprintf("unsupported descriptor kind %q", kind))
	}
}

func validMCPDescriptor(plugin curatorPluginCase, kind agentic.CuratorDescriptorKind) agentic.CuratorChannelDescriptor {
	switch kind {
	case agentic.CuratorDescriptorFlag:
		if plugin.system == "claude-code" {
			return agentic.CuratorChannelDescriptor{Kind: kind, Flag: "--mcp-config", Argument: agentic.CuratorArgumentPath, With: []string{"--strict-mcp-config"}}
		}
		return agentic.CuratorChannelDescriptor{Kind: kind, Flag: "-p", Argument: agentic.CuratorArgumentName, Name: "curator-mcp"}
	case agentic.CuratorDescriptorConfigKey:
		return agentic.CuratorChannelDescriptor{Kind: kind, Key: "mcp_config"}
	case agentic.CuratorDescriptorVariable:
		return agentic.CuratorChannelDescriptor{Kind: kind, Variable: "MCP_CONFIG"}
	case agentic.CuratorDescriptorFile:
		return agentic.CuratorChannelDescriptor{Kind: kind, Filename: "curator-mcp.config.toml"}
	default:
		panic(fmt.Sprintf("unsupported descriptor kind %q", kind))
	}
}

func validDefaultSystemPromptDescriptor(plugin curatorPluginCase) agentic.CuratorChannelDescriptor {
	semantics := agentic.CuratorSystemPromptAppend
	if plugin.system == "codex" {
		semantics = agentic.CuratorSystemPromptReplace
	}
	return validSystemPromptDescriptor(plugin, map[bool]agentic.CuratorDescriptorKind{true: agentic.CuratorDescriptorConfigKey, false: agentic.CuratorDescriptorFlag}[plugin.system == "codex"], semantics)
}

func supportsSystemPromptKind(plugin curatorPluginCase, kind agentic.CuratorDescriptorKind) bool {
	return plugin.system == "claude-code" && kind == agentic.CuratorDescriptorFlag ||
		plugin.system == "codex" && kind == agentic.CuratorDescriptorConfigKey
}

func planCarriesSystemPrompt(plan agentic.Plan, plugin curatorPluginCase, descriptor agentic.CuratorChannelDescriptor, path string) bool {
	if plugin.system == "claude-code" {
		return containsPair(plan.Argv, descriptor.Flag, path)
	}
	return containsPair(plan.Argv, "-c", descriptor.Key+"="+`"`+path+`"`)
}

func planCarriesMCPChannel(plan agentic.Plan, plugin curatorPluginCase, descriptor agentic.CuratorChannelDescriptor, path string) bool {
	if plugin.system == "claude-code" {
		return containsPair(plan.Argv, descriptor.Flag, path) && containsArg(plan.Argv, "--strict-mcp-config")
	}
	return containsPair(plan.Argv, descriptor.Flag, descriptor.Name)
}

func containsPair(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}

func countPair(args []string, first, second string) int {
	count := 0
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			count++
		}
	}
	return count
}

func containsArg(args []string, expected string) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}
	return false
}

func appendSystemPromptFlagFor(intent agentic.CuratorSystemPromptIntent) string {
	if intent == agentic.CuratorSystemPromptReplace {
		return "--system-prompt-file"
	}
	return "--append-system-prompt-file"
}

func otherPromptIntent(intent agentic.CuratorSystemPromptIntent) agentic.CuratorSystemPromptIntent {
	if intent == agentic.CuratorSystemPromptAppend {
		return agentic.CuratorSystemPromptReplace
	}
	return agentic.CuratorSystemPromptAppend
}

func appendSystemPromptFileFlagFor(plugin curatorPluginCase) string {
	if plugin.system == "codex" {
		return ""
	}
	return "--append-system-prompt-file"
}

func nativeLocalCatalogFixture(t *testing.T, slug string, vocab []string) string {
	t.Helper()
	raw, err := os.ReadFile("systems/codex/testdata/native-local-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	row := catalog["models"].([]any)[0].(map[string]any)
	row["slug"] = slug
	levels := []map[string]string{}
	for _, effort := range vocab {
		levels = append(levels, map[string]string{"effort": effort, "description": "Test"})
	}
	row["supported_reasoning_levels"] = levels
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
