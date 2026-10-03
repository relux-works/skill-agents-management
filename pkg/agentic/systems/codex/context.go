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
	mcpOverrides        []configOverride
	mcpServers          []agentic.CompositionServer
	curatorMCPArgs      []string
	hasCuratorMCP       bool
	systemPrompt        string
	hasSystemPrompt     bool
	curatorSystemPrompt *configOverride
	permission          agentic.PermissionMode
	hasPermission       bool
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

// ValidateCuratorContext is the Codex channel-layer gate for a typed Curator
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
		return values, fmt.Errorf("codex: %w", err)
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
			if overrides, servers, err := encodeCodexMCP(*payload.MCP); err != nil {
				return values, err
			} else {
				values.mcpOverrides = overrides
				values.mcpServers = servers
			}
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
			if resolved, err := payload.Permission.Mode.Resolve(); err != nil {
				return values, fmt.Errorf("codex context: %w", err)
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
		if err := applyCodexCuratorContext(req, &values); err != nil {
			return values, err
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
	if err := rejectNativeContextConflicts(req.NativeArgs, values, !req.Network.IsZero()); err != nil {
		return values, err
	}
	return values, nil
}

func applyCodexCuratorContext(req agentic.LaunchRequest, values *contextValues) error {
	context := req.Context
	if context.Environment != "codex_cli" {
		return fmt.Errorf("%w: Codex requires the codex_cli fragment environment", agentic.ErrCuratorContextUnsupported)
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
		if !req.Composition.IsZero() || strings.TrimSpace(req.Profile) != "" {
			return fmt.Errorf("%w: Curator MCP layering conflicts with the request's composition or harness profile", agentic.ErrCuratorContextUnsupported)
		}
		descriptor := context.MCP.Channels[0]
		if descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != "-p" ||
			descriptor.Argument != agentic.CuratorArgumentName || descriptor.Name != "curator-mcp" || len(descriptor.With) != 0 {
			return fmt.Errorf("%w: Codex MCP channel is incompatible with the fragment descriptor", agentic.ErrCuratorContextUnsupported)
		}
		values.curatorMCPArgs = []string{descriptor.Flag, descriptor.Name}
		values.hasCuratorMCP = true
	}
	if context.SystemPrompt != nil {
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
		if !codexSystemPromptDescriptorSupported(selected) {
			return fmt.Errorf("%w: Codex cannot apply this system-prompt descriptor", agentic.ErrCuratorContextUnsupported)
		}
		encoded, err := encodeTOMLValue(context.SystemPrompt.Path)
		if err != nil {
			return fmt.Errorf("%w: system-prompt path could not be encoded", agentic.ErrCuratorContextMalformed)
		}
		values.curatorSystemPrompt = &configOverride{key: selected.Key, value: encoded}
	}
	return nil
}

func codexSystemPromptDescriptorSupported(descriptor *agentic.CuratorChannelDescriptor) bool {
	return descriptor.Kind == agentic.CuratorDescriptorConfigKey && descriptor.Key == "model_instructions_file" &&
		descriptor.Semantics == agentic.CuratorSystemPromptReplace
}

func encodeCodexMCP(context agentic.MCPServersContext) ([]configOverride, []agentic.CompositionServer, error) {
	if err := agentic.ValidateMCPServers(context); err != nil {
		return nil, nil, err
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
				return nil, nil, err
			}
			if server.BearerTokenEnvVar != "" {
				if err := add(server.Name, "bearer_token_env_var", server.BearerTokenEnvVar); err != nil {
					return nil, nil, err
				}
			}
			continue
		}
		if err := add(server.Name, "command", server.Command); err != nil {
			return nil, nil, err
		}
		if len(server.Args) > 0 {
			if err := add(server.Name, "args", server.Args); err != nil {
				return nil, nil, err
			}
		}
	}
	composition := codexComposition(overrides, metadata)
	if err := validateComposition(composition); err != nil {
		return nil, nil, invalidContext(agentic.ContextMCPServers, fmt.Sprintf("Codex MCP configuration failed its plugin grammar: %v", err))
	}
	return overrides, metadata, nil
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

// rejectNativeContextConflicts refuses native arguments that collide with the
// request's own channels. Under a managed network scope it additionally
// refuses native arguments that SELECT or CONFIGURE MCP entries — a profile
// selector or an mcp_servers.* override: the native channel is forwarded
// verbatim by contract, so its servers cannot be inventoried or injected
// into, and admitting the launch would start them without the managed env.
// The managed fix is the request's own channels (Profile, MCP descriptors),
// which the adapter covers.
func rejectNativeContextConflicts(args []string, values contextValues, managed bool) error {
	for _, index := range nativeargs.FlagIndexes(args) {
		el := args[index]
		name, _, _ := nativeargs.SplitFlagValue(el)
		if values.hasCuratorMCP && (name == "-p" || name == "--profile" || isAttachedProfileValue(el)) {
			return contextConflict(agentic.ContextMCPServers, "native arguments already select a Codex profile")
		}
		if managed && (name == "-p" || name == "--profile" || isAttachedProfileValue(el)) {
			return fmt.Errorf("codex: codex-env-v1 cannot cover the MCP servers native argument %s would select: the native channel is forwarded verbatim: %w", formatNativeMCPSelector(nativeProfileFlag(el), ""), agentic.ErrNetworkScopeUnsupported)
		}
		configSelector := name == configFlagLong || name == configFlag || isAttachedConfigValue(el)
		if configSelector {
			key, ok := nativeConfigKeyAt(args, index)
			if ok {
				if managed && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {
					return fmt.Errorf("codex: codex-env-v1 cannot cover the MCP servers native argument %s would configure: the native channel is forwarded verbatim: %w", formatNativeMCPSelector(nativeConfigFlag(el), key), agentic.ErrNetworkScopeUnsupported)
				}
				if values.hasSystemPrompt && key == developerInstructionsConfigKey {
					return contextConflict(agentic.ContextSystemPrompt, "native arguments already set developer instructions")
				}
				if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key {
					return contextConflict(agentic.ContextSystemPrompt, "native arguments already set the Curator system-prompt file")
				}
				if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {
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

// formatNativeMCPSelector renders one native MCP selector for a refusal:
// the flag spelling plus, for config overrides, the config key. Values
// never appear: an attached `-c...=VALUE`, `--config=...=VALUE` or
// `--profile=...` token carries endpoints, credentials or the profile
// name, and formatting the whole token discloses them verbatim
// (round-2 H2). Every managed native refusal uses this one formatter so
// no path can reintroduce the raw token.
func formatNativeMCPSelector(flag, key string) string {
	if key == "" {
		return fmt.Sprintf("flag %q", flag)
	}
	return fmt.Sprintf("flag %q key %q", flag, key)
}

// nativeProfileFlag extracts the flag spelling from a profile selector
// element without its value: `-pwork` and `--profile=work` both render
// as their flag alone.
func nativeProfileFlag(el string) string {
	if isAttachedProfileValue(el) {
		return "-p"
	}
	name, _, _ := nativeargs.SplitFlagValue(el)
	return name
}

// nativeConfigFlag extracts the flag spelling from a config override
// element without its value: `-ckey=value` renders as `-c`,
// `--config=key=value` as `--config`.
func nativeConfigFlag(el string) string {
	if isAttachedConfigValue(el) {
		return configFlag
	}
	name, _, _ := nativeargs.SplitFlagValue(el)
	return name
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
