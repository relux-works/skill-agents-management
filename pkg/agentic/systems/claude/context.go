package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/mcpjson"
	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

type contextValues struct {
	mcpConfig        string
	hasMCP           bool
	curatorMCPArgs   []string
	hasCuratorMCP    bool
	systemPrompt     string
	systemPromptFlag string
	hasSystemPrompt  bool
	permission       agentic.PermissionMode
	hasPermission    bool
}

type claudeMCPDocument struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

type claudeMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
}

// ValidateContextDescriptors validates and resolves semantic launch contexts
// before BuildPlan performs any other plugin preparation. Args calls the same
// pure builder so direct plugin callers receive identical refusals.
func (*System) ValidateContextDescriptors(req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	if _, err := buildContextValues(req, mode); err != nil {
		return err
	}
	return nil
}

// ValidateCuratorContext is the Claude channel-layer gate for a typed Curator
// fragment. BuildPlan invokes it before any launch surface is built.
func (*System) ValidateCuratorContext(req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	if _, err := buildContextValues(req, mode); err != nil {
		return err
	}
	return nil
}

func buildContextValues(req agentic.LaunchRequest, mode agentic.LaunchMode) (contextValues, error) {
	values := contextValues{}
	var effective agentic.PermissionMode

	if effectiveValue, err := req.PermissionMode.Resolve(); err != nil {
		return values, fmt.Errorf("claude: %w", err)
	} else {
		effective = effectiveValue
	}
	seen := make(map[agentic.ContextDescriptorKind]bool, len(req.ContextDescriptors))
	for _, descriptor := range req.ContextDescriptors {
		var payload agentic.ContextDescriptorPayload

		if payloadValue, err := agentic.DecodeContextDescriptor(descriptor); err != nil {
			return values, err
		} else {
			payload = payloadValue
		}
		if seen[descriptor.Kind] {
			return values, contextConflict(descriptor.Kind, "the channel was supplied more than once")
		}
		seen[descriptor.Kind] = true

		switch {
		case payload.MCP != nil:
			if !req.Composition.IsZero() {
				return values, contextConflict(agentic.ContextMCPServers, "the legacy composition already supplies launch configuration")
			}
			if err := agentic.ValidateMCPServers(*payload.MCP); err != nil {
				return values, err
			}
			var config string
			var servers []agentic.CompositionServer
			if encoded, encodedServers, err := encodeClaudeMCP(*payload.MCP); err != nil {
				return values, err
			} else {
				config, servers = encoded, encodedServers
			}
			composition := agentic.Composition{Prefix: []string{mcpjson.ConfigFlag, config}, Servers: servers}
			if err := validateComposition(composition); err != nil {
				return values, invalidContext(agentic.ContextMCPServers, fmt.Sprintf("Claude MCP configuration failed its plugin grammar: %v", err))
			}
			values.mcpConfig = config
			values.hasMCP = true
		case payload.SystemPrompt != nil:
			text := payload.SystemPrompt.Text
			if strings.TrimSpace(text) == "" {
				return values, invalidContext(agentic.ContextSystemPrompt, "additional prompt text must not be empty")
			}
			if req.Goal != nil {
				return values, contextConflict(agentic.ContextSystemPrompt, "the goal binding already uses Claude's appended system-prompt channel")
			}
			values.systemPrompt = text
			values.systemPromptFlag = appendSystemPromptFlag
			values.hasSystemPrompt = true
		case payload.Permission != nil:
			if !req.PermissionMode.IsZero() {
				return values, contextConflict(agentic.ContextPermission, "PermissionMode already supplies the permission channel")
			}
			if mode != agentic.LaunchModeInteractive {
				return values, fmt.Errorf("claude: %w: a permission context is valid only for interactive launches", agentic.ErrPermissionModeNotInteractive)
			}
			if resolved, err := payload.Permission.Mode.Resolve(); err != nil {
				return values, fmt.Errorf("claude context: %w", err)
			} else {
				effective = resolved
			}
			values.hasPermission = true
		}
	}
	if req.Context != nil {
		if err := agentic.ValidateCuratorContext(req.Context, req.Home); err != nil {
			return values, err
		}
		if err := applyClaudeCuratorContext(req, &values); err != nil {
			return values, err
		}
	}

	if mode != agentic.LaunchModeInteractive && !req.PermissionMode.IsZero() {
		return values, fmt.Errorf("claude: %w: permission mode %q is valid only for interactive launches",
			agentic.ErrPermissionModeNotInteractive, strings.TrimSpace(string(req.PermissionMode)))
	}
	if values.hasPermission && effective == agentic.PermissionModeYolo {
		if _, err := permissionMapping(req.ToolRelease, effective); err != nil {
			return values, err
		}
		if err := scanNativePolicy(req.NativeArgs); err != nil {
			return values, fmt.Errorf("claude: %w", err)
		}
	}
	values.permission = effective
	if err := rejectNativeContextConflicts(req.NativeArgs, values); err != nil {
		return values, err
	}
	return values, nil
}

func applyClaudeCuratorContext(req agentic.LaunchRequest, values *contextValues) error {
	context := req.Context
	if context.Environment != "claude_code" {
		return fmt.Errorf("%w: Claude requires the claude_code fragment environment", agentic.ErrCuratorContextUnsupported)
	}
	for _, descriptor := range req.ContextDescriptors {
		if context.MCP != nil && descriptor.Kind == agentic.ContextMCPServers {
			return contextConflict(descriptor.Kind, "Curator context already supplies the MCP channel")
		}
		if context.SystemPrompt != nil && descriptor.Kind == agentic.ContextSystemPrompt {
			return contextConflict(descriptor.Kind, "Curator context already supplies the system-prompt channel")
		}
	}
	if context.MCP != nil {
		if !req.Composition.IsZero() {
			return curatorConflict("legacy composition already supplies launch configuration")
		}
		descriptor := context.MCP.Channels[0]
		if descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != mcpConfigFlag ||
			descriptor.Argument != agentic.CuratorArgumentPath || len(descriptor.With) != 1 || descriptor.With[0] != "--strict-mcp-config" {
			return fmt.Errorf("%w: Claude's MCP channel is incompatible with the fragment descriptor", agentic.ErrCuratorContextUnsupported)
		}
		values.curatorMCPArgs = []string{descriptor.Flag, context.MCP.Path}
		values.curatorMCPArgs = append(values.curatorMCPArgs, descriptor.With...)
		values.hasCuratorMCP = true
	}
	if context.SystemPrompt != nil {
		if req.Goal != nil {
			return curatorConflict("the goal binding already uses Claude's system-prompt channel")
		}
		var selected *agentic.CuratorChannelDescriptor
		for i := range context.SystemPrompt.Channels {
			descriptor := &context.SystemPrompt.Channels[i]
			if descriptor.Semantics != context.SystemPrompt.Intent {
				continue
			}
			if selected != nil {
				return agentic.ErrCuratorSystemPromptChannelAmbiguous
			}
			selected = descriptor
		}
		if selected == nil {
			return agentic.ErrCuratorSystemPromptChannelMissing
		}
		if !claudeSystemPromptDescriptorSupported(selected) {
			return fmt.Errorf("%w: Claude cannot apply a system-prompt descriptor", agentic.ErrCuratorContextUnsupported)
		}
		values.systemPromptFlag = selected.Flag
		values.systemPrompt = context.SystemPrompt.Path
		values.hasSystemPrompt = true
	}
	return nil
}

func claudeSystemPromptDescriptorSupported(descriptor *agentic.CuratorChannelDescriptor) bool {
	return descriptor.Kind == agentic.CuratorDescriptorFlag && descriptor.Argument == agentic.CuratorArgumentPath &&
		len(descriptor.With) == 0 && claudeSystemPromptFlagMatches(descriptor)
}

func claudeSystemPromptFlagMatches(descriptor *agentic.CuratorChannelDescriptor) bool {
	switch descriptor.Semantics {
	case agentic.CuratorSystemPromptAppend:
		return descriptor.Flag == appendSystemPromptFileFlag
	case agentic.CuratorSystemPromptReplace:
		return descriptor.Flag == replaceSystemPromptFileFlag
	default:
		return false
	}
}

func curatorConflict(reason string) error {
	return fmt.Errorf("%w: %s", agentic.ErrCuratorContextUnsupported, reason)
}

func encodeClaudeMCP(context agentic.MCPServersContext) (string, []agentic.CompositionServer, error) {
	document := claudeMCPDocument{MCPServers: make(map[string]claudeMCPServer, len(context.Servers))}
	metadata := make([]agentic.CompositionServer, 0, len(context.Servers))
	for _, server := range context.Servers {
		entry := claudeMCPServer{Type: server.Transport, URL: server.URL, Command: server.Command, Args: append([]string(nil), server.Args...)}
		if server.BearerTokenEnvVar != "" {
			entry.Headers = map[string]string{"Authorization": "Bearer ${" + server.BearerTokenEnvVar + "}"}
		}
		document.MCPServers[server.Name] = entry
		metadata = append(metadata, agentic.CompositionServer{Name: server.Name, Transport: server.Transport, BearerTokenEnvVar: server.BearerTokenEnvVar})
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", nil, invalidContext(agentic.ContextMCPServers, "MCP configuration could not be encoded")
	}
	return string(encoded), metadata, nil
}

func rejectNativeContextConflicts(args []string, values contextValues) error {
	for _, index := range claudeFlagIndexes(args) {
		name, _, _ := nativeargs.SplitFlagValue(args[index])
		if (values.hasMCP || values.hasCuratorMCP) && name == mcpjson.ConfigFlag {
			return contextConflict(agentic.ContextMCPServers, "native arguments already set the MCP configuration")
		}
		if values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt") {
			return contextConflict(agentic.ContextSystemPrompt, "native arguments already set the system prompt")
		}
		if values.hasPermission && values.permission == agentic.PermissionModeNative && isClaudePermissionSelector(name) {
			return contextConflict(agentic.ContextPermission, "native arguments already set the permission posture")
		}
	}
	return nil
}

func isClaudePermissionSelector(name string) bool {
	return name == permissionModeFlag || name == bypassPermissionsFlag || name == allowDangerouslySkipPermissionsFlag || name == restrictedFlag
}

func contextConflict(channel agentic.ContextDescriptorKind, reason string) error {
	return &agentic.ContextDescriptorConflictError{Channel: channel, Reason: reason}
}

func invalidContext(channel agentic.ContextDescriptorKind, reason string) error {
	return &agentic.InvalidContextDescriptorError{Kind: channel, Reason: reason}
}
