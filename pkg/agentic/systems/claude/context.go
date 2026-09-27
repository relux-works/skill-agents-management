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
	mcpConfig       string
	hasMCP          bool
	systemPrompt    string
	hasSystemPrompt bool
	permission      agentic.PermissionMode
	hasPermission   bool
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
	_, err := buildContextValues(req, mode)
	return err
}

func buildContextValues(req agentic.LaunchRequest, mode agentic.LaunchMode) (contextValues, error) {
	values := contextValues{}
	effective, err := req.PermissionMode.Resolve()
	if err != nil {
		return values, fmt.Errorf("claude: %w", err)
	}
	seen := make(map[agentic.ContextDescriptorKind]bool, len(req.ContextDescriptors))
	for _, descriptor := range req.ContextDescriptors {
		payload, err := agentic.DecodeContextDescriptor(descriptor)
		if err != nil {
			return values, err
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
			config, servers, err := encodeClaudeMCP(*payload.MCP)
			if err != nil {
				return values, err
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
			values.hasSystemPrompt = true
		case payload.Permission != nil:
			if !req.PermissionMode.IsZero() {
				return values, contextConflict(agentic.ContextPermission, "PermissionMode already supplies the permission channel")
			}
			if mode != agentic.LaunchModeInteractive {
				return values, fmt.Errorf("claude: %w: a permission context is valid only for interactive launches", agentic.ErrPermissionModeNotInteractive)
			}
			effective, err = payload.Permission.Mode.Resolve()
			if err != nil {
				return values, fmt.Errorf("claude context: %w", err)
			}
			values.hasPermission = true
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
	for _, index := range nativeargs.FlagIndexes(args) {
		name, _, _ := nativeargs.SplitFlagValue(args[index])
		if values.hasMCP && name == mcpjson.ConfigFlag {
			return contextConflict(agentic.ContextMCPServers, "native arguments already set the MCP configuration")
		}
		if values.hasSystemPrompt && (name == appendSystemPromptFlag || name == appendSystemPromptFileFlag) {
			return contextConflict(agentic.ContextSystemPrompt, "native arguments already set appended system-prompt text")
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
