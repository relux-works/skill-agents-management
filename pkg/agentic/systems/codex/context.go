package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const developerInstructionsConfigKey = "developer_instructions"

type configOverride struct {
	key   string
	value string
}

type contextValues struct {
	mcpOverrides    []configOverride
	systemPrompt    string
	hasSystemPrompt bool
	permission      agentic.PermissionMode
	hasPermission   bool
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
		return values, fmt.Errorf("codex: %w", err)
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
			overrides, err := encodeCodexMCP(*payload.MCP)
			if err != nil {
				return values, err
			}
			values.mcpOverrides = overrides
		case payload.SystemPrompt != nil:
			if strings.TrimSpace(payload.SystemPrompt.Text) == "" {
				return values, invalidContext(agentic.ContextSystemPrompt, "additional prompt text must not be empty")
			}
			encoded, err := encodeTOMLValue(payload.SystemPrompt.Text)
			if err != nil {
				return values, invalidContext(agentic.ContextSystemPrompt, "additional prompt text could not be encoded")
			}
			values.systemPrompt = encoded
			values.hasSystemPrompt = true
		case payload.Permission != nil:
			if !req.PermissionMode.IsZero() {
				return values, contextConflict(agentic.ContextPermission, "PermissionMode already supplies the permission channel")
			}
			if mode != agentic.LaunchModeInteractive {
				return values, fmt.Errorf("codex: %w: a permission context is valid only for interactive launches", agentic.ErrPermissionModeNotInteractive)
			}
			effective, err = payload.Permission.Mode.Resolve()
			if err != nil {
				return values, fmt.Errorf("codex context: %w", err)
			}
			values.hasPermission = true
		}
	}

	if mode != agentic.LaunchModeInteractive && !req.PermissionMode.IsZero() {
		return values, fmt.Errorf("codex: %w: permission mode %q is valid only for interactive launches",
			agentic.ErrPermissionModeNotInteractive, strings.TrimSpace(string(req.PermissionMode)))
	}
	if values.hasPermission && effective == agentic.PermissionModeYolo {
		if _, err := permissionMapping(req.ToolRelease, effective); err != nil {
			return values, err
		}
		if err := scanNativePolicy(req.NativeArgs); err != nil {
			return values, fmt.Errorf("codex: %w", err)
		}
	}
	values.permission = effective
	if err := rejectNativeContextConflicts(req.NativeArgs, values); err != nil {
		return values, err
	}
	return values, nil
}

func encodeCodexMCP(context agentic.MCPServersContext) ([]configOverride, error) {
	if err := agentic.ValidateMCPServers(context); err != nil {
		return nil, err
	}
	overrides := make([]configOverride, 0, len(context.Servers)*2)
	metadata := make([]agentic.CompositionServer, 0, len(context.Servers))
	add := func(server, field string, value any) error {
		encoded, err := encodeTOMLValue(value)
		if err != nil {
			return invalidContext(agentic.ContextMCPServers, "MCP configuration value could not be encoded")
		}
		overrides = append(overrides, configOverride{key: mcpServersKeyPrefix + server + "." + field, value: encoded})
		return nil
	}
	for _, server := range context.Servers {
		metadata = append(metadata, agentic.CompositionServer{Name: server.Name, Transport: server.Transport, BearerTokenEnvVar: server.BearerTokenEnvVar})
		if server.Transport == agentic.MCPTransportHTTP {
			if err := add(server.Name, "url", server.URL); err != nil {
				return nil, err
			}
			if server.BearerTokenEnvVar != "" {
				if err := add(server.Name, "bearer_token_env_var", server.BearerTokenEnvVar); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := add(server.Name, "command", server.Command); err != nil {
			return nil, err
		}
		if len(server.Args) > 0 {
			if err := add(server.Name, "args", server.Args); err != nil {
				return nil, err
			}
		}
	}
	composition := codexComposition(overrides, metadata)
	if err := validateComposition(composition); err != nil {
		return nil, invalidContext(agentic.ContextMCPServers, fmt.Sprintf("Codex MCP configuration failed its plugin grammar: %v", err))
	}
	return overrides, nil
}

func codexComposition(overrides []configOverride, servers []agentic.CompositionServer) agentic.Composition {
	prefix := make([]string, 0, len(overrides)*2)
	for _, override := range overrides {
		prefix = append(prefix, "-c", override.key+"="+override.value)
	}
	return agentic.Composition{Prefix: prefix, Servers: servers}
}

func encodeTOMLValue(value any) (string, error) {
	// JSON basic strings and arrays are also valid TOML basic strings and
	// arrays. Encoding this restricted value set produces the double-quoted
	// spelling codex.validateComposition accepts while still escaping control
	// characters and quotes correctly.
	switch value.(type) {
	case string, []string:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	default:
		return "", fmt.Errorf("unsupported Codex context value type %T", value)
	}
}

func rejectNativeContextConflicts(args []string, values contextValues) error {
	for _, index := range nativeargs.FlagIndexes(args) {
		el := args[index]
		name, _, _ := nativeargs.SplitFlagValue(el)
		configSelector := name == configFlagLong || name == configFlag || isAttachedConfigValue(el)
		if configSelector {
			key, ok := nativeConfigKeyAt(args, index)
			if ok {
				if values.hasSystemPrompt && key == developerInstructionsConfigKey {
					return contextConflict(agentic.ContextSystemPrompt, "native arguments already set developer instructions")
				}
				if len(values.mcpOverrides) > 0 && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {
					return contextConflict(agentic.ContextMCPServers, "native arguments already set MCP configuration")
				}
				if values.hasPermission && values.permission == agentic.PermissionModeNative && isConflictingConfigKey(key) {
					return contextConflict(agentic.ContextPermission, "native arguments already set the permission posture")
				}
			}
		}
		if values.hasPermission && values.permission == agentic.PermissionModeNative && isCodexPermissionSelector(name) {
			return contextConflict(agentic.ContextPermission, "native arguments already set the permission posture")
		}
	}
	return nil
}

func nativeConfigKeyAt(args []string, index int) (string, bool) {
	el := args[index]
	name, value, hasValue := nativeargs.SplitFlagValue(el)
	if name == configFlag || name == configFlagLong {
		if !hasValue {
			if index+1 >= len(args) || nativeargs.IsSeparator(args[index+1]) {
				return "", false
			}
			value = args[index+1]
		}
		key, _, ok := strings.Cut(value, "=")
		return strings.TrimSpace(key), ok
	}
	if isAttachedConfigValue(el) {
		value = strings.TrimPrefix(el, configFlag)
		key, _, ok := strings.Cut(value, "=")
		return strings.TrimSpace(key), ok
	}
	return "", false
}

func isCodexPermissionSelector(name string) bool {
	return name == "-a" || name == "--ask-for-approval" || name == "-s" || name == "--sandbox" || name == approveForMeFlag || name == yoloAliasFlag || name == bypassApprovalsAndSandboxFlag || strings.HasPrefix(name, "--dangerously-bypass-")
}

func contextConflict(channel agentic.ContextDescriptorKind, reason string) error {
	return &agentic.ContextDescriptorConflictError{Channel: channel, Reason: reason}
}

func invalidContext(channel agentic.ContextDescriptorKind, reason string) error {
	return &agentic.InvalidContextDescriptorError{Kind: channel, Reason: reason}
}
