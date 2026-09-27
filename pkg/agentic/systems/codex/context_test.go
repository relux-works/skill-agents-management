package codex

import (
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestBuildPlanTranslatesCodexContextChannels(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ToolRelease = "0.153.2"
	req.ContextDescriptors = []agentic.ContextDescriptor{
		{Kind: agentic.ContextMCPServers, MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
			{Name: "board", Transport: agentic.MCPTransportHTTP, URL: "https://mcp.example.test", BearerTokenEnvVar: "BOARD_TOKEN"},
			{Name: "local", Transport: agentic.MCPTransportStdio, Command: "mcp-tool", Args: []string{"serve", "--stdio"}},
		}}},
		{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "Keep the repository instructions in force."}},
		{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}},
	}

	plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
	want := []string{
		"-c", `developer_instructions="Keep the repository instructions in force."`,
		"-c", `mcp_servers.board.url="https://mcp.example.test"`,
		"-c", `mcp_servers.board.bearer_token_env_var="BOARD_TOKEN"`,
		"-c", `mcp_servers.local.command="mcp-tool"`,
		"-c", `mcp_servers.local.args=["serve","--stdio"]`,
		"-m", parityModel,
		"-c", `model_reasoning_effort="` + parityEffort + `"`,
		bypassApprovalsAndSandboxFlag,
	}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
	}
}

func TestBuildPlanTranslatesCodexExecutionContextDescriptors(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ContextDescriptors = []agentic.ContextDescriptor{
		{Kind: agentic.ContextMCPServers, MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
			{Name: "docs", Transport: agentic.MCPTransportHTTP, URL: "https://mcp.example.test"},
		}}},
		{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "Keep the repository instructions in force."}},
	}
	plan, _ := interactivePlan(t, req, agentic.LaunchModeExec)
	for _, value := range []string{
		`developer_instructions="Keep the repository instructions in force."`,
		`mcp_servers.docs.url="https://mcp.example.test"`,
	} {
		found := false
		for _, arg := range plan.Argv {
			if arg == value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Argv = %#v, missing descriptor value %q", plan.Argv, value)
		}
	}
}

func TestCodexContextDescriptorRefusalsReachBuildPlan(t *testing.T) {
	base := interactiveRequest(tempSlot(t))
	mcp := agentic.ContextDescriptor{Kind: agentic.ContextMCPServers, MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{{Name: "board", Transport: agentic.MCPTransportHTTP, URL: "https://mcp.example.test"}}}}
	prompt := agentic.ContextDescriptor{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "Add instructions."}}
	permission := agentic.ContextDescriptor{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeNative}}
	cases := []struct {
		name      string
		mutate    func(*agentic.LaunchRequest)
		wantError error
		wantKind  agentic.ContextDescriptorKind
	}{
		{
			name: "unknown descriptor kind",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: "future-channel", SystemPrompt: &agentic.SystemPromptContext{Text: "unknown"}}}
			},
			wantError: agentic.ErrUnknownContextDescriptor,
		},
		{
			name: "duplicate MCP channel",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{mcp, mcp}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextMCPServers,
		},
		{
			name: "MCP descriptor plus legacy composition",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{mcp}
				req.Composition = agentic.Composition{Prefix: []string{"-c", `mcp_servers.board.url="https://legacy.example.test"`}}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextMCPServers,
		},
		{
			name: "MCP descriptor plus native config",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{mcp}
				req.NativeArgs = []string{"-c", `mcp_servers.board.url="https://native.example.test"`}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextMCPServers,
		},
		{
			name: "invalid MCP URL rejected through BuildPlan",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextMCPServers, MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
					{Name: "board", Transport: agentic.MCPTransportHTTP, URL: "https://user:embedded-value@mcp.example.test"},
				}}}}
			},
			wantError: agentic.ErrInvalidContextDescriptor,
		},
		{
			name: "system prompt plus native config",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{prompt}
				req.NativeArgs = []string{"--config=developer_instructions=\"manual\""}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextSystemPrompt,
		},
		{
			name: "permission descriptor plus legacy permission mode",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{permission}
				req.PermissionMode = agentic.PermissionModeYolo
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextPermission,
		},
		{
			name: "native posture plus permission selector",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{permission}
				req.NativeArgs = []string{"-a", "never"}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextPermission,
		},
		{
			name: "native posture plus codex yolo alias",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{permission}
				req.NativeArgs = []string{yoloAliasFlag}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextPermission,
		},
		{
			name: "permission yolo plus native policy selector",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}}}
				req.ToolRelease = "0.153.2"
				req.NativeArgs = []string{"--sandbox", "workspace-write"}
			},
			wantError: agentic.ErrNativePolicyConflict,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			req := base
			req.ContextDescriptors = nil
			req.NativeArgs = nil
			req.Composition = agentic.Composition{}
			req.PermissionMode = ""
			test.mutate(&req)
			err := interactivePlanError(t, req)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("BuildPlan err = %v, want %v", err, test.wantError)
			}
			if test.wantKind != "" {
				var conflict *agentic.ContextDescriptorConflictError
				if !errors.As(err, &conflict) || conflict.Channel != test.wantKind {
					t.Fatalf("BuildPlan err = %v, want a typed conflict for %q", err, test.wantKind)
				}
			}
		})
	}
}

func TestCodexContextIgnoresConfigLookingPromptAfterSeparator(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.NativeArgs = []string{"--", "-c", `developer_instructions="prompt text"`}
	req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "Extra developer instructions."}}}
	plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
	if got, want := plan.Argv[len(plan.Argv)-3:], []string{"--", "-c", `developer_instructions="prompt text"`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("native suffix = %#v, want %#v", got, want)
	}
}

func TestCodexPermissionContextOutsideInteractiveModeIsRefused(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeNative}}}
	if err := planErrorIn(t, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrPermissionModeNotInteractive) {
		t.Fatalf("BuildPlan err = %v, want ErrPermissionModeNotInteractive", err)
	}
}

func TestCodexRejectsBlankAdditionalSystemPrompt(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "  \n"}}}
	if err := planErrorIn(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrInvalidContextDescriptor) {
		t.Fatalf("BuildPlan err = %v, want ErrInvalidContextDescriptor", err)
	}
}
