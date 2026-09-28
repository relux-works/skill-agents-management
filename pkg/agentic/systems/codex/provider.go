package codex

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

var providerIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// localProviderArgs resolves the explicitly selected entry from the private
// CODEX_HOME config and returns Codex-supported -c overrides. With no binding,
// it is a no-op so ordinary subscription launches retain their exact argv.
func localProviderArgs(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if req.LocalProvider == nil {
		return nil, nil
	}
	providerID := req.LocalProvider.ID
	if strings.TrimSpace(providerID) == "" {
		return nil, &agentic.LocalProviderRefusal{Kind: agentic.LocalProviderUnbound}
	}
	if providerID != strings.TrimSpace(providerID) {
		return nil, localProviderRefusal(agentic.LocalProviderMalformed, "provider id")
	}
	if mode != agentic.LaunchModeExec && mode != agentic.LaunchModeDryRun {
		return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)
	}
	if !strings.HasPrefix(providerID, "local-") {
		return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)
	}
	if !providerIDPattern.MatchString(providerID) {
		return nil, localProviderRefusal(agentic.LocalProviderMalformed, "provider id")
	}

	root, err := localProviderHome(req)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(root, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		kind := agentic.LocalProviderReadFailed
		if errors.Is(err, os.ErrNotExist) {
			kind = agentic.LocalProviderAbsent
		}
		return nil, localProviderRefusal(kind, providerID)
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil || config == nil {
		return nil, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	provider, err := resolvePrivateProvider(config, providerID)
	if err != nil {
		return nil, err
	}
	return []string{
		"-c", "model_provider=" + strconv.Quote(providerID),
		"-c", "model_providers." + providerID + ".name=" + strconv.Quote(provider.Name),
		"-c", "model_providers." + providerID + ".base_url=" + strconv.Quote(provider.BaseURL),
		"-c", "model_providers." + providerID + ".wire_api=\"responses\"",
		"-c", "model_providers." + providerID + ".requires_openai_auth=false",
	}, nil
}

// localProviderHome resolves the private configuration root once according to
// the request contract and rejects any incoming CODEX_HOME that would disagree
// with it. The child environment uses this same resolved root so Codex reads
// the configuration that this gate validated.
func localProviderHome(req agentic.LaunchRequest) (string, error) {
	root := strings.TrimSpace(req.Home)
	if root == "" {
		root = New().Capabilities().DefaultHome
	}
	root, ok := resolveCodexConfigRoot(root, codexPolicyEnvValue(req.Env, "HOME"), req.WorkDir)
	if !ok {
		return "", localProviderRefusal(agentic.LocalProviderUnbound, "provider home")
	}
	for _, configuredHome := range codexPolicyEnvValues(req.Env, "CODEX_HOME") {
		if strings.TrimSpace(configuredHome) == "" {
			continue
		}
		resolved, resolvedOK := resolveCodexConfigRoot(configuredHome, codexPolicyEnvValue(req.Env, "HOME"), req.WorkDir)
		if !resolvedOK || resolved != root {
			return "", localProviderRefusal(agentic.LocalProviderConflicting, "provider home")
		}
	}
	return root, nil
}

type privateProvider struct {
	Name    string
	BaseURL string
}

func resolvePrivateProvider(config map[string]any, providerID string) (privateProvider, error) {
	providers, ok := config["model_providers"].(map[string]any)
	if !ok {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnbound, providerID)
	}
	entry, found := providers[providerID]
	if !found {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnbound, providerID)
	}
	provider, ok := entry.(map[string]any)
	if !ok {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	name, ok := provider["name"].(string)
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	baseURL, ok := provider["base_url"].(string)
	if !ok || strings.TrimSpace(baseURL) != baseURL {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	if err := validateLoopbackBaseURL(baseURL); err != nil {
		if errors.Is(err, errMalformedProviderURL) {
			return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
		}
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)
	}
	wireAPI, ok := provider["wire_api"].(string)
	if !ok {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	if wireAPI != "responses" {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)
	}
	requiresAuth, found := provider["requires_openai_auth"]
	if !found {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	requires, ok := requiresAuth.(bool)
	if !ok {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)
	}
	if requires {
		return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)
	}
	for _, key := range []string{"auth", "api_key", "env_key", "experimental_bearer_token"} {
		if _, found := provider[key]; found {
			return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)
		}
	}
	return privateProvider{Name: name, BaseURL: baseURL}, nil
}

var errMalformedProviderURL = errors.New("codex: malformed local provider URL")

func validateLoopbackBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errMalformedProviderURL
	}
	if parsed.Scheme != "http" {
		return errors.New("codex: local provider URL scheme is unsupported")
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return errMalformedProviderURL
		}
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return errors.New("codex: local provider URL is not loopback")
		}
		return nil
	}
	return errors.New("codex: local provider URL is not loopback")
}

func localProviderRefusal(kind agentic.LocalProviderRefusalKind, subject string) error {
	return &agentic.LocalProviderRefusal{Kind: kind, File: "config.toml", Subject: subject}
}

func codexPolicyEnvValues(env []string, name string) []string {
	var values []string
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if found && key == name {
			values = append(values, value)
		}
	}
	return values
}
