package agentic

import (
	"errors"
	"reflect"
	"testing"
)

func TestDecodeContextDescriptorReturnsTypedUnknownBeforePayloadChecks(t *testing.T) {
	_, err := DecodeContextDescriptor(ContextDescriptor{
		Kind:         "future-channel",
		SystemPrompt: &SystemPromptContext{Text: "the known-shaped payload must not make an unknown kind valid"},
	})
	if !errors.Is(err, ErrUnknownContextDescriptor) {
		t.Fatalf("err = %v, want ErrUnknownContextDescriptor", err)
	}
	var unknown *UnknownContextDescriptorError
	if !errors.As(err, &unknown) || unknown.Kind != "future-channel" {
		t.Fatalf("err = %v, want typed refusal carrying the unknown kind", err)
	}
}

func TestDecodeContextDescriptorRefusesMismatchedTypedPayload(t *testing.T) {
	_, err := DecodeContextDescriptor(ContextDescriptor{
		Kind:         ContextSystemPrompt,
		MCP:          &MCPServersContext{Servers: []MCPServerDescriptor{{Name: "docs", Transport: MCPTransportHTTP, URL: "https://example.test/mcp"}}},
		SystemPrompt: &SystemPromptContext{Text: "extra instructions"},
	})
	if !errors.Is(err, ErrInvalidContextDescriptor) {
		t.Fatalf("err = %v, want ErrInvalidContextDescriptor", err)
	}
	var invalid *InvalidContextDescriptorError
	if !errors.As(err, &invalid) || invalid.Kind != ContextSystemPrompt {
		t.Fatalf("err = %v, want typed invalid descriptor for system-prompt", err)
	}
}

func TestValidateMCPServersAdmitsOnlyPortableTransportShapes(t *testing.T) {
	valid := []MCPServersContext{
		{Servers: []MCPServerDescriptor{{Name: "docs", Transport: MCPTransportHTTP, URL: "https://example.test/mcp", BearerTokenEnvVar: "DOCS_TOKEN"}}},
		{Servers: []MCPServerDescriptor{{Name: "local_tools", Transport: MCPTransportStdio, Command: "tool", Args: []string{"serve", "--stdio"}}}},
	}
	for _, context := range valid {
		if err := ValidateMCPServers(context); err != nil {
			t.Errorf("ValidateMCPServers(%+v) = %v", context, err)
		}
	}

	invalid := []MCPServersContext{
		{},
		{Servers: []MCPServerDescriptor{{Name: "bad name", Transport: MCPTransportHTTP, URL: "https://example.test/mcp"}}},
		{Servers: []MCPServerDescriptor{{Name: "duplicate", Transport: MCPTransportHTTP, URL: "https://example.test/1"}, {Name: "duplicate", Transport: MCPTransportHTTP, URL: "https://example.test/2"}}},
		{Servers: []MCPServerDescriptor{{Name: "bad_env", Transport: MCPTransportHTTP, URL: "https://example.test/mcp", BearerTokenEnvVar: "TOKEN=secret"}}},
		{Servers: []MCPServerDescriptor{{Name: "bad_url", Transport: MCPTransportHTTP, URL: "not a URL"}}},
		{Servers: []MCPServerDescriptor{{Name: "url_credentials", Transport: MCPTransportHTTP, URL: "https://user:embedded-value@example.test/mcp"}}},
		{Servers: []MCPServerDescriptor{{Name: "url_fragment", Transport: MCPTransportHTTP, URL: "https://example.test/mcp#fragment"}}},
		{Servers: []MCPServerDescriptor{{Name: "bad_http", Transport: MCPTransportHTTP, URL: "https://example.test/mcp", Command: "tool"}}},
		{Servers: []MCPServerDescriptor{{Name: "bad_stdio", Transport: MCPTransportStdio, Command: "tool", BearerTokenEnvVar: "TOKEN"}}},
		{Servers: []MCPServerDescriptor{{Name: "unknown", Transport: "custom", URL: "custom://example.test"}}},
	}
	for _, context := range invalid {
		err := ValidateMCPServers(context)
		if !errors.Is(err, ErrInvalidContextDescriptor) {
			t.Errorf("ValidateMCPServers(%+v) = %v, want ErrInvalidContextDescriptor", context, err)
		}
	}
}

func TestContextDescriptorValidatorRunsBeforePreparation(t *testing.T) {
	events := []string{}
	wantFailure := errors.New("context rejected")
	sys := &contextValidationSystem{pangolinSystem: newPangolin(), events: &events, validationErr: wantFailure}
	registry := NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register: %v", err)
	}
	req := pangolinRequest()
	req.ContextDescriptors = []ContextDescriptor{{Kind: ContextSystemPrompt, SystemPrompt: &SystemPromptContext{Text: "extra"}}}

	_, err := BuildPlan(registry, req, LaunchModeExec)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("BuildPlan err = %v, want the plugin's context refusal", err)
	}
	if !reflect.DeepEqual(events, []string{"validate-context"}) {
		t.Fatalf("plugin sequence = %v, want context validation before any preparation", events)
	}
}

type contextValidationSystem struct {
	*pangolinSystem
	events        *[]string
	validationErr error
}

func (s *contextValidationSystem) ValidateContextDescriptors(LaunchRequest, LaunchMode) error {
	*s.events = append(*s.events, "validate-context")
	return s.validationErr
}

func (s *contextValidationSystem) PrepareLaunchRequest(req LaunchRequest, _ LaunchMode) (LaunchRequestPreparation, error) {
	*s.events = append(*s.events, "prepare")
	return LaunchRequestPreparation{PromptPath: req.PromptPath, Prompt: append([]byte(nil), req.Prompt...)}, nil
}
