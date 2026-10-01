package muse

import (
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const museNoAutoUpdateEnv = "MUSE_NO_AUTO_UPDATE"

type parentEnvRule struct {
	name   string
	reason string
}

// This sorted exact-name list is the complete parent-environment contract for
// Muse. Operator configuration belongs in Muse's files under HOME or
// XDG_CONFIG_HOME; this launch API does not support explicit Muse env settings.
// Each entry has a one-line reason for why the child needs it.
var museParentEnvAllowlist = []parentEnvRule{
	{name: "HOME", reason: "Locates Muse's per-user configuration and state."},
	{name: "LANG", reason: "Selects the process locale when no category override is set."},
	{name: "LC_ALL", reason: "Selects the process locale across all locale categories."},
	{name: "LC_COLLATE", reason: "Selects locale-aware string collation."},
	{name: "LC_CTYPE", reason: "Selects locale-aware character classification."},
	{name: "LC_MESSAGES", reason: "Selects the locale used for process messages."},
	{name: "LC_MONETARY", reason: "Selects locale-aware monetary formatting."},
	{name: "LC_NUMERIC", reason: "Selects locale-aware numeric formatting."},
	{name: "LC_TIME", reason: "Selects locale-aware date and time formatting."},
	{name: "LOGNAME", reason: "Preserves the inherited login identity for child tools."},
	{name: "PATH", reason: "Resolves Muse and the child tools it starts."},
	{name: "SHELL", reason: "Identifies the configured shell used by child tooling."},
	{name: "TERM", reason: "Lets terminal-aware output select supported control sequences."},
	{name: "TMPDIR", reason: "Provides the platform temporary directory for runtime files."},
	{name: "TZ", reason: "Preserves the configured local time zone for timestamps."},
	{name: "USER", reason: "Preserves the inherited user identity for child tools."},
	{name: "XDG_CACHE_HOME", reason: "Selects the XDG cache directory used by CLI dependencies."},
	{name: "XDG_CONFIG_HOME", reason: "Selects the XDG configuration directory used by Muse and CLI dependencies."},
	{name: "XDG_DATA_HOME", reason: "Selects the XDG data directory used by CLI dependencies."},
	{name: "XDG_RUNTIME_DIR", reason: "Selects the XDG runtime directory used by CLI dependencies."},
	{name: "XDG_STATE_HOME", reason: "Selects the XDG state directory used by CLI dependencies."},
}

// Credential markers are used only by the allowlist guard test. They never
// participate in parent-variable admission.
var credentialEnvLongWords = []string{
	"TOKEN",
	"SECRET",
	"PASSWORD",
	"PASSWD",
	"PASSPHRASE",
	"PASS",
	"CREDENTIAL",
	"APIKEY",
	"PRIVATEKEY",
	"KEY",
	"BEARER",
	"COOKIE",
	"AUTH",
	"SESSION",
	"CERT",
	"SIGNING",
}

// filterMuseParentEnv keeps exact allowlist entries only; no prefixes or
// credential-name heuristics participate in admission.
func filterMuseParentEnv(parent []string) []string {
	filtered := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, hasValue := strings.Cut(entry, "=")
		if !hasValue || !allowMuseParentEnvName(name) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func allowMuseParentEnvName(name string) bool {
	for _, rule := range museParentEnvAllowlist {
		if name == rule.name {
			return true
		}
	}
	return false
}

// isCredentialShapedParentEnvName is a secondary tripwire for allowlist tests;
// admission remains the exact-name lookup above. After uppercasing, the LONG
// words TOKEN, SECRET, PASSWORD, PASSWD, PASSPHRASE, PASS, CREDENTIAL, APIKEY,
// PRIVATEKEY, KEY, BEARER, COOKIE, AUTH, SESSION, CERT, and SIGNING are matched
// as substrings of the full name, with separators kept. Tokens are split on
// every rune outside A-Z and 0-9 and have trailing digits stripped. PAT is the
// only word not matched as a substring: it flags a token equal to PAT or ending
// in PAT after digit stripping. PAT as a prefix or infix inside a larger token
// (for example XPATY or PATFILE) is out of contract by design, avoiding PATH.
func isCredentialShapedParentEnvName(name string) bool {
	upperName := strings.ToUpper(name)
	for _, word := range credentialEnvLongWords {
		if strings.Contains(upperName, word) {
			return true
		}
	}

	tokens := strings.FieldsFunc(upperName, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
	for i, token := range tokens {
		tokens[i] = strings.TrimRight(token, "0123456789")
	}
	for _, token := range tokens {
		if token == "PAT" || strings.HasSuffix(token, "PAT") {
			return true
		}
	}
	return false
}

// childEnv filters the parent first, overlays the caller's tracked run
// context, then pins auto-update off as the final write.
func childEnv(parent []string, req agentic.LaunchRequest) []string {
	env := agentic.WithRunContext(filterMuseParentEnv(parent), req)
	return agentic.SetEnvValue(env, museNoAutoUpdateEnv, "1")
}
