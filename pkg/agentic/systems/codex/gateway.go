package codex

import (
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const GatewayProviderID = "curator-gateway"
const GatewayTokenEnv = "CURATOR_CODEX_GATEWAY_TOKEN"

type gatewayPolicy struct {
	mode    agentic.LaunchMode
	release string
	native  []string
	other   bool
	request agentic.LaunchRequest
}

func (*System) CodexGatewayBasis(r agentic.LaunchRequest, mode agentic.LaunchMode) agentic.CodexGatewayPolicy {
	return gatewayPolicy{mode: mode, release: r.ToolRelease, native: slices.Clone(r.NativeArgs), other: r.LocalProvider != nil || r.CodexRemoteClient != nil || r.CodexGateway != nil || !r.Network.IsZero(), request: r}
}

func (g gatewayPolicy) Admit(in agentic.CodexGatewayInputs, env []string) error {
	if g.mode != agentic.LaunchModeInteractive || g.release != "0.164.0" || g.other || !slices.Equal(in.NativeArgs, g.native) || !gatewayNativeSuffix(g.native) {
		return agentic.ErrCodexGatewayRefused
	}
	// Lookup declarations carry names only. Exact repeats retain the existing
	// composition deduplication; aliases and reserved selectors still refuse.
	names := make([]string, 0, len(in.LookupNames))
	seenNames := map[string]bool{}
	for _, name := range in.LookupNames {
		if !seenNames[name] {
			names = append(names, name+"=")
			seenNames[name] = true
		}
	}
	for _, layer := range [][]string{env, in.FragmentEnv, in.PromptEnv, names} {
		if err := g.CheckEnvironment(layer, false); err != nil {
			return err
		}
	}
	return nil
}

// The MVP accepts ordinary fresh and exact native resume, plus the documented
// display-only switch. Other native forms keep their unmanaged behavior.
func gatewayNativeSuffix(args []string) bool {
	i := 0
	for i < len(args) && args[i] == "--no-alt-screen" {
		i++
	}
	if i > 1 {
		return false
	}
	if i == len(args) {
		return true
	}
	return len(args)-i == 2 && args[i] == "resume" && agentic.ValidateResumeIntent(agentic.ResumeIntent{Kind: agentic.ResumeCodexThread, Identity: &args[i+1]}, agentic.ResumeCodexThread) == nil
}

// CheckEnvironment rejects selector aliases and token overlays before they can
// be collapsed by a map. Only the child-only stage may add the exact token name.
func (g gatewayPolicy) CheckEnvironment(env []string, injected bool) error {
	if err := checkGatewayEnvironment(env, injected); err != nil {
		return err
	}
	// Plan-owned MCP lookups have the same authority as explicit LookupNames.
	var names []string
	for _, descriptor := range g.request.ContextDescriptors {
		if descriptor.MCP != nil {
			for _, server := range descriptor.MCP.Servers {
				if server.BearerTokenEnvVar != "" {
					names = append(names, server.BearerTokenEnvVar)
				}
			}
		}
	}
	if g.request.Context != nil && g.request.Context.MCP != nil {
		names = append(names, g.request.Context.MCP.EnvNames...)
	}
	for _, name := range names {
		if err := checkGatewayEnvironment([]string{name + "="}, false); err != nil {
			return err
		}
	}
	return nil
}

func checkGatewayEnvironment(env []string, injected bool) error {
	count := 0
	seen := map[string]bool{}
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" || strings.ContainsAny(entry, "\x00\r\n") {
			return agentic.ErrCodexGatewayRefused
		}
		fold := strings.ToUpper(name)
		if seen[fold] {
			return agentic.ErrCodexGatewayRefused
		}
		seen[fold] = true
		if fold == GatewayTokenEnv {
			if !injected || name != GatewayTokenEnv || value == "" {
				return agentic.ErrCodexGatewayRefused
			}
			count++
			continue
		}
		// Credential values are opaque and are neither inspected nor rendered.
		// The allowed OpenAI environment inputs are the documented credential
		// and account selectors; route/transport selectors have no authority.
		if strings.HasPrefix(fold, "OPENAI_") {
			switch name {
			case "OPENAI_API_KEY", "OPENAI_ORG_ID", "OPENAI_PROJECT_ID":
			default:
				return agentic.ErrCodexGatewayRefused
			}
		}
		if strings.HasPrefix(fold, "CODEX_") {
			switch name {
			case "CODEX_HOME", "CODEX_MANAGED_BY_NPM", "CODEX_MANAGED_BY_BUN":
			default:
				return agentic.ErrCodexGatewayRefused
			}
		}
	}
	if injected && count != 1 {
		return agentic.ErrCodexGatewayRefused
	}
	return nil
}

func validGatewayBinding(b agentic.CodexGatewayBinding) bool {
	if b.ProviderID != GatewayProviderID || b.TokenEnv != GatewayTokenEnv || b.WireAPI != "responses" || !b.RequiresOpenAIAuth || b.SupportsWebSockets || b.ToolRelease != "0.164.0" || !filepath.IsAbs(b.ExecutablePath) || filepath.Clean(b.ExecutablePath) != b.ExecutablePath {
		return false
	}
	u, err := url.Parse(b.BaseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(b.BaseURL, "\x00\r\n\t ") {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 || u.RawPath != "" || u.Path != "/chatgpt/backend-api/codex" {
		return false
	}
	canonical := (&url.URL{Scheme: "http", Host: u.Host, Path: "/chatgpt/backend-api/codex"}).String()
	if b.BaseURL != canonical {
		return false
	}
	if len(b.HeaderName) < 1 || len(b.HeaderName) > 64 {
		return false
	}
	for _, c := range b.HeaderName {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	// Match the registration contract's accepted header-name predicate.
	switch strings.ToLower(b.HeaderName) {
	case "authorization", "proxy-authorization", "cookie", "host", "content-length", "transfer-encoding", "connection", "x-api-key":
		return false
	}
	return true
}
