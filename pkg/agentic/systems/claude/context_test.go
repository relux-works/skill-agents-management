package claude

import (
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestBuildPlanTranslatesClaudeContextChannels(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ToolRelease = "2.1.261"
	req.ContextDescriptors = []agentic.ContextDescriptor{
		{Kind: agentic.ContextMCPServers, MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
			{Name: "board", Transport: agentic.MCPTransportHTTP, URL: "https://mcp.example.test"},
			{Name: "local", Transport: agentic.MCPTransportStdio, Command: "mcp-tool", Args: []string{"serve"}},
		}}},
		{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "Keep the repository instructions in force."}},
		{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}},
	}

	plan := buildClaudeContextPlan(t, req, agentic.LaunchModeInteractive)
	want := []string{
		"--model", parityModel,
		"--effort", parityEffort,
		mcpConfigFlag + `={"mcpServers":{"board":{"type":"http","url":"https://mcp.example.test"},"local":{"type":"stdio","command":"mcp-tool","args":["serve"]}}}`,
		appendSystemPromptFlag, "Keep the repository instructions in force.",
		bypassPermissionsFlag,
	}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
	}
}

func TestBuildPlanTranslatesClaudeExecutionContextDescriptors(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ContextDescriptors = []agentic.ContextDescriptor{
		{Kind: agentic.ContextMCPServers, MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
			{Name: "docs", Transport: agentic.MCPTransportHTTP, URL: "https://mcp.example.test"},
		}}},
		{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "Keep the repository instructions in force."}},
	}
	plan := buildClaudeContextPlan(t, req, agentic.LaunchModeExec)
	for _, want := range [][]string{
		{mcpConfigFlag, `{"mcpServers":{"docs":{"type":"http","url":"https://mcp.example.test"}}}`},
		{appendSystemPromptFlag, "Keep the repository instructions in force."},
	} {
		if !containsConsecutiveArgs(plan.Argv, want) {
			t.Fatalf("Argv = %#v, want it to carry context values %q", plan.Argv, want)
		}
	}
}

func TestClaudeContextDescriptorRefusalsReachBuildPlan(t *testing.T) {
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
			name: "duplicate system-prompt descriptors",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{prompt, prompt}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextSystemPrompt,
		},
		{
			name: "MCP descriptor plus legacy composition",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{mcp}
				req.Composition = agentic.Composition{Prefix: []string{mcpConfigFlag, "{}"}}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextMCPServers,
		},
		{
			name: "MCP descriptor plus native config",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{mcp}
				req.NativeArgs = []string{mcpConfigFlag, `{"mcpServers":{}}`}
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
			name: "system prompt plus native system-prompt flag",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{prompt}
				req.NativeArgs = []string{appendSystemPromptFlag + "=manual"}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextSystemPrompt,
		},
		{
			name: "system prompt plus goal binding",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{prompt}
				req.Goal = &agentic.Goal{ID: "goal-1", ProviderCondition: "tests pass"}
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
			name: "native permission selector plus permission descriptor",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{permission}
				req.NativeArgs = []string{permissionModeFlag, "auto"}
			},
			wantError: agentic.ErrContextDescriptorConflict,
			wantKind:  agentic.ContextPermission,
		},
		{
			name: "permission yolo plus native policy selector",
			mutate: func(req *agentic.LaunchRequest) {
				req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}}}
				req.ToolRelease = "2.1.261"
				req.NativeArgs = []string{permissionModeFlag, "acceptEdits"}
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
			req.Goal = nil
			test.mutate(&req)
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
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

func TestClaudeContextIgnoresFlagLookingPromptAfterSeparator(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.NativeArgs = []string{"--", appendSystemPromptFlag, "caller prompt"}
	req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "Extra system instructions."}}}
	plan := buildClaudeContextPlan(t, req, agentic.LaunchModeInteractive)
	if got, want := plan.Argv[len(plan.Argv)-3:], []string{"--", appendSystemPromptFlag, "caller prompt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("native suffix = %#v, want %#v", got, want)
	}
}

func TestClaudePermissionContextOutsideInteractiveModeIsRefused(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeNative}}}
	if err := planErrorFor(t, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrPermissionModeNotInteractive) {
		t.Fatalf("BuildPlan err = %v, want ErrPermissionModeNotInteractive", err)
	}
}

func TestClaudeRejectsBlankAdditionalSystemPrompt(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "  \n"}}}
	if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrInvalidContextDescriptor) {
		t.Fatalf("BuildPlan err = %v, want ErrInvalidContextDescriptor", err)
	}
}

func containsConsecutiveArgs(argv, values []string) bool {
	for start := 0; start+len(values) <= len(argv); start++ {
		if reflect.DeepEqual(argv[start:start+len(values)], values) {
			return true
		}
	}
	return false
}

func buildClaudeContextPlan(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) agentic.Plan {
	t.Helper()
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req.Env = append(append([]string(nil), req.Env...), "PATH="+binDir)
	return buildParityPlan(t, New(), req, mode)
}
