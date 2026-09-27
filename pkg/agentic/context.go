package agentic

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ContextDescriptorKind identifies one launch context channel. The value is
// intentionally open as a string so a descriptor introduced by a newer
// consumer is refused with its original identifier rather than being silently
// mistaken for absence.
type ContextDescriptorKind string

const (
	ContextMCPServers   ContextDescriptorKind = "mcp-servers"
	ContextSystemPrompt ContextDescriptorKind = "system-prompt"
	ContextPermission   ContextDescriptorKind = "permission"

	MCPTransportHTTP  = "http"
	MCPTransportStdio = "stdio"
)

// ContextDescriptor is a closed typed union for semantic launch context. A
// descriptor carries values only; the selected system plugin owns how those
// values are rendered for its harness.
type ContextDescriptor struct {
	Kind         ContextDescriptorKind `json:"kind"`
	MCP          *MCPServersContext    `json:"mcp,omitempty"`
	SystemPrompt *SystemPromptContext  `json:"system_prompt,omitempty"`
	Permission   *PermissionContext    `json:"permission,omitempty"`
}

// MCPServersContext contains the MCP server declarations for one launch.
type MCPServersContext struct {
	Servers []MCPServerDescriptor `json:"servers"`
}

// MCPServerDescriptor is the common, flag-free description of one MCP server.
// Stdio servers use Command and Args. HTTP servers use URL and may reference a
// bearer token through BearerTokenEnvVar; the URL must use http(s) and may not
// include credentials or a fragment. Token values never enter a launch
// descriptor.
type MCPServerDescriptor struct {
	Name              string   `json:"name"`
	Transport         string   `json:"transport"`
	URL               string   `json:"url,omitempty"`
	Command           string   `json:"command,omitempty"`
	Args              []string `json:"args,omitempty"`
	BearerTokenEnvVar string   `json:"bearer_token_env_var,omitempty"`
}

// SystemPromptContext carries additional prompt text. It appends to the
// harness's default instructions; it never replaces the harness prompt.
type SystemPromptContext struct {
	Text string `json:"text"`
}

// PermissionContext carries the requested interactive permission posture.
type PermissionContext struct {
	Mode PermissionMode `json:"mode"`
}

var (
	ErrUnknownContextDescriptor      = errors.New("agentic: unknown context descriptor")
	ErrInvalidContextDescriptor      = errors.New("agentic: invalid context descriptor")
	ErrContextDescriptorConflict     = errors.New("agentic: conflicting context descriptors")
	ErrContextDescriptorsUnsupported = errors.New("agentic: system does not support context descriptors")
)

// UnknownContextDescriptorError reports a descriptor kind the library does
// not implement. Callers can branch on errors.Is(err,
// ErrUnknownContextDescriptor) or inspect Kind with errors.As.
type UnknownContextDescriptorError struct {
	Kind ContextDescriptorKind
}

func (e *UnknownContextDescriptorError) Error() string {
	return fmt.Sprintf("%v: %q", ErrUnknownContextDescriptor, e.Kind)
}

func (e *UnknownContextDescriptorError) Unwrap() error { return ErrUnknownContextDescriptor }

// InvalidContextDescriptorError reports a known descriptor whose typed payload
// does not match its kind or whose required value is malformed.
type InvalidContextDescriptorError struct {
	Kind   ContextDescriptorKind
	Reason string
}

func (e *InvalidContextDescriptorError) Error() string {
	return fmt.Sprintf("%v %q: %s", ErrInvalidContextDescriptor, e.Kind, e.Reason)
}

func (e *InvalidContextDescriptorError) Unwrap() error { return ErrInvalidContextDescriptor }

// ContextDescriptorConflictError reports two sources competing to supply the
// same channel or a descriptor contradicting another launch input.
type ContextDescriptorConflictError struct {
	Channel ContextDescriptorKind
	Reason  string
}

func (e *ContextDescriptorConflictError) Error() string {
	return fmt.Sprintf("%v for %q: %s", ErrContextDescriptorConflict, e.Channel, e.Reason)
}

func (e *ContextDescriptorConflictError) Unwrap() error { return ErrContextDescriptorConflict }

// ContextDescriptorsUnsupportedError reports that a selected plugin has no
// renderer for the supplied semantic channels.
type ContextDescriptorsUnsupportedError struct {
	System SystemID
}

func (e *ContextDescriptorsUnsupportedError) Error() string {
	return fmt.Sprintf("%v: %s", ErrContextDescriptorsUnsupported, e.System)
}

func (e *ContextDescriptorsUnsupportedError) Unwrap() error {
	return ErrContextDescriptorsUnsupported
}

// ContextDescriptorPayload is the decoded payload of one ContextDescriptor.
type ContextDescriptorPayload struct {
	MCP          *MCPServersContext
	SystemPrompt *SystemPromptContext
	Permission   *PermissionContext
}

// DecodeContextDescriptor validates the tagged-union shape for one
// descriptor. Plugins still own channel conflicts, capability checks and the
// provider-specific value translation.
func DecodeContextDescriptor(descriptor ContextDescriptor) (ContextDescriptorPayload, error) {
	switch descriptor.Kind {
	case ContextMCPServers, ContextSystemPrompt, ContextPermission:
	default:
		return ContextDescriptorPayload{}, &UnknownContextDescriptorError{Kind: descriptor.Kind}
	}

	payloads := 0
	if descriptor.MCP != nil {
		payloads++
	}
	if descriptor.SystemPrompt != nil {
		payloads++
	}
	if descriptor.Permission != nil {
		payloads++
	}

	invalid := func(reason string) (ContextDescriptorPayload, error) {
		return ContextDescriptorPayload{}, &InvalidContextDescriptorError{Kind: descriptor.Kind, Reason: reason}
	}
	if payloads != 1 {
		return invalid("exactly one typed payload is required")
	}
	switch descriptor.Kind {
	case ContextMCPServers:
		if descriptor.MCP == nil || descriptor.SystemPrompt != nil || descriptor.Permission != nil {
			return invalid("MCP kind requires only the MCP payload")
		}
		return ContextDescriptorPayload{MCP: descriptor.MCP}, nil
	case ContextSystemPrompt:
		if descriptor.SystemPrompt == nil || descriptor.MCP != nil || descriptor.Permission != nil {
			return invalid("system-prompt kind requires only the system-prompt payload")
		}
		return ContextDescriptorPayload{SystemPrompt: descriptor.SystemPrompt}, nil
	case ContextPermission:
		if descriptor.Permission == nil || descriptor.MCP != nil || descriptor.SystemPrompt != nil {
			return invalid("permission kind requires only the permission payload")
		}
		return ContextDescriptorPayload{Permission: descriptor.Permission}, nil
	default:
		return ContextDescriptorPayload{}, &UnknownContextDescriptorError{Kind: descriptor.Kind}
	}
}

// ValidateMCPServers checks the common, flag-free MCP descriptor shape before
// a plugin serializes it into its harness configuration. Names use the
// portable letters, digits, underscore and hyphen subset so Codex's dotted
// config keys and Claude's JSON map carry the same identity without quoting
// rules.
func ValidateMCPServers(context MCPServersContext) error {
	invalid := func(reason string) error {
		return &InvalidContextDescriptorError{Kind: ContextMCPServers, Reason: reason}
	}
	if len(context.Servers) == 0 {
		return invalid("at least one MCP server is required")
	}
	seen := make(map[string]struct{}, len(context.Servers))
	for _, server := range context.Servers {
		if !validMCPServerName(server.Name) {
			return invalid("MCP server name must use letters, digits, underscore or hyphen")
		}
		if _, duplicate := seen[server.Name]; duplicate {
			return invalid("MCP server names must be unique")
		}
		seen[server.Name] = struct{}{}
		if server.URL != strings.TrimSpace(server.URL) || server.Command != strings.TrimSpace(server.Command) {
			return invalid("MCP URL and command values must not have surrounding whitespace")
		}
		switch server.Transport {
		case MCPTransportHTTP:
			if server.URL == "" || server.Command != "" || len(server.Args) != 0 {
				return invalid("HTTP MCP servers require a URL and cannot carry a command or arguments")
			}
			parsedURL, err := url.Parse(server.URL)
			if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
				return invalid("HTTP MCP URL must be an http(s) URL with a host and no embedded credentials or fragment")
			}
			if server.BearerTokenEnvVar != "" && !validEnvironmentName(server.BearerTokenEnvVar) {
				return invalid("bearer token reference must be an environment variable name")
			}
		case MCPTransportStdio:
			if server.Command == "" || server.URL != "" || server.BearerTokenEnvVar != "" {
				return invalid("stdio MCP servers require a command and cannot carry a URL or bearer reference")
			}
		default:
			return invalid("transport must be http or stdio")
		}
	}
	return nil
}

func validMCPServerName(name string) bool {
	if name == "" || name != strings.TrimSpace(name) {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
		digit := r >= '0' && r <= '9'
		if !letter && (i == 0 || !digit) {
			return false
		}
	}
	return true
}

// ContextDescriptorValidator is an optional System extension. BuildPlan calls
// it before launch preparation, so malformed or conflicting descriptors are
// refused before other plugin surfaces run.
type ContextDescriptorValidator interface {
	ValidateContextDescriptors(LaunchRequest, LaunchMode) error
}
