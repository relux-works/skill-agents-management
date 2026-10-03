package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Tool policy is applied at Args, the construction site shared by all plans.
// The module emits its own attached denial unconditionally and forwards
// caller occurrences verbatim; it never inspects caller deny lists to decide.
const (
	disallowedToolsFlag       = "--disallowedTools"
	disallowedToolsKebabFlag  = "--disallowed-tools"
	allowedToolsFlag          = "--allowedTools"
	allowedToolsKebabFlag     = "--allowed-tools"
	deniedToolAskUserQuestion = "AskUserQuestion"
	disallowedToolsDenial     = disallowedToolsFlag + "=" + deniedToolAskUserQuestion
)

// ecmaTrimCutset is the ECMAScript String.prototype.trim character set:
// WhiteSpace (TAB, VT, FF, SP, NBSP, ZWNBSP, and every other Unicode
// Space_Separator) plus LineTerminator (LF, CR, LS, PS). It differs from Go's
// unicode.IsSpace in exactly two members: U+FEFF trims here and not in Go,
// while U+0085 trims in Go and not here. The pinned Fp list tokenizer and the
// Xss inline-JSON check both use this trim, so the refusal side must too.
const ecmaTrimCutset = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

func ecmaTrim(value string) string { return strings.Trim(value, ecmaTrimCutset) }

// splitToolList follows pinned Fp: commas and ASCII spaces split outside a
// rule qualifier, the qualifier state resets for every value token, and
// parentheses are deliberately non-nesting, as in the native tokenizer.
// Members trim with ECMAScript semantics and empty members are dropped, as
// native pushes only r.trim()-nonempty rules. No caller bytes are rewritten;
// this is inspection only.
func splitToolList(value string) []string {
	var members []string
	var current strings.Builder
	depth := 0
	flush := func() {
		rule := ecmaTrim(current.String())
		current.Reset()
		if rule != "" {
			members = append(members, rule)
		}
	}
	for _, r := range value {
		switch {
		case r == '(':
			depth = 1
			current.WriteRune(r)
		case r == ')' && depth > 0:
			depth = 0
			current.WriteRune(r)
		case depth == 0 && (r == ',' || r == ' '):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return members
}

func isDisallowedToolsFlag(name string) bool {
	return name == disallowedToolsFlag || name == disallowedToolsKebabFlag
}
func isAllowedToolsFlag(name string) bool {
	return name == allowedToolsFlag || name == allowedToolsKebabFlag
}

// nativeToolName follows pinned 2.1.288's $n rule parser: malformed qualifiers
// name the whole literal, empty and * qualifiers name the whole tool. Other
// qualifiers still constitute an explicit attempt for the named tool. Native
// allow globs without an MCP server prefix are skipped, not tool-name matches.
func nativeToolName(rule string) string {
	open := unescapedParen(rule, '(', false)
	close := unescapedParen(rule, ')', true)
	if open < 0 || close <= open || close != len(rule)-1 || strings.ContainsAny(rule[:open], "()") {
		return rule
	}
	return rule[:open]
}

func unescapedParen(rule string, target byte, last bool) int {
	found := -1
	for i := 0; i < len(rule); i++ {
		if rule[i] != target {
			continue
		}
		slashes := 0
		for j := i - 1; j >= 0 && rule[j] == '\\'; j-- {
			slashes++
		}
		if slashes%2 == 0 {
			found = i
			if !last {
				return i
			}
		}
	}
	return found
}

func findReenabledTool(args []string) (tool string, placement agentic.NativePolicyPlacement, found bool) {
	for _, option := range claudeOptions(args) {
		if !isAllowedToolsFlag(option.name) {
			continue
		}
		for _, value := range option.values {
			for _, member := range splitToolList(value) {
				if nativeToolName(member) == deniedToolAskUserQuestion {
					return member, option.placement, true
				}
			}
		}
	}
	return "", "", false
}

// lastSettingsValue selects the effective source the way native NI does: the
// last match wins.
func lastSettingsValue(values []string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

// commanderSettingsValue is the Commander-style effective --settings
// selection: the last occurrence wins, and a trailing bare occurrence selects
// the empty value. Native Commander fails that launch with a missing argument,
// so the empty selection never agrees with the eager scan and refuses below
// instead of passing.
func commanderSettingsValue(args []string) (string, bool) {
	value := ""
	present := false
	for _, option := range claudeOptions(args) {
		if option.name == "--settings" {
			present = true
			value = ""
			if len(option.values) > 0 {
				value = option.values[0]
			}
		}
	}
	return value, present
}

// checkSettingsPolicy refuses explicit re-enable attempts carried by the
// effective --settings source before launch. The pinned native eager scanner
// (NI, which never decomposes combined short switches) and the Commander
// option parse select independently; when they disagree about the effective
// source neither is authoritative and the launch refuses with the typed
// ambiguity kind. Otherwise the agreed source is inspected: inline JSON when
// the ECMAScript-trimmed value starts with { and ends with } (native Xss),
// else a file relative to the launch cwd. Implicit user/project/managed
// configuration is outside this argv policy.
func checkSettingsPolicy(req agentic.LaunchRequest) error {
	eagerVal, eagerOK := lastSettingsValue(eagerSettingsValues(req.NativeArgs))
	cmdVal, cmdOK := commanderSettingsValue(req.NativeArgs)
	if eagerOK != cmdOK || (eagerOK && eagerVal != cmdVal) {
		return settingsPolicyRefusal(agentic.SettingsPolicyAmbiguous)
	}
	if !eagerOK {
		return nil
	}
	rules, kind := settingsAllowRules(eagerVal, req.WorkDir)
	if kind != "" {
		return settingsPolicyRefusal(kind)
	}
	for _, rule := range rules {
		// Settings members are single rules, not lists, but native Fp feeds
		// trimmed members to the same $n matcher, so each member trims the
		// same way before matching. The exact native settings-matcher
		// whitespace behavior past $n is a stated bound, resolved fail-closed.
		if trimmed := ecmaTrim(rule); nativeToolName(trimmed) == deniedToolAskUserQuestion {
			return deniedToolRefusal(trimmed, agentic.NativePolicyPlacementSettings)
		}
	}
	return nil
}

// Reading and decoding return distinct failure facts. Neither becomes absence.
// We validate the permission shape used by this policy without claiming to
// implement the native schema of unrelated settings keys.
func settingsAllowRules(value, workDir string) ([]string, agentic.SettingsPolicyFailureKind) {
	var body []byte
	trimmed := ecmaTrim(value)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		body = []byte(trimmed)
	} else {
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		var err error
		body, err = os.ReadFile(path)
		if err != nil {
			return nil, agentic.SettingsPolicyUnreadable
		}
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(body, &settings) != nil || settings == nil {
		return nil, agentic.SettingsPolicyInvalid
	}
	raw, present := settings["permissions"]
	if !present {
		return nil, ""
	}
	var permissions map[string]json.RawMessage
	if json.Unmarshal(raw, &permissions) != nil || permissions == nil {
		return nil, agentic.SettingsPolicyInvalid
	}
	raw, present = permissions["allow"]
	if !present {
		return nil, ""
	}
	var members []json.RawMessage
	if string(raw) == "null" || json.Unmarshal(raw, &members) != nil {
		return nil, agentic.SettingsPolicyInvalid
	}
	var rules []string
	for _, member := range members {
		var rule string
		if string(member) == "null" || json.Unmarshal(member, &rule) != nil {
			return nil, agentic.SettingsPolicyInvalid
		}
		rules = append(rules, rule)
	}

	return rules, ""
}

func deniedToolRefusal(tool string, placement agentic.NativePolicyPlacement) error {
	return &agentic.DeniedToolReEnabledError{Tool: tool, Placement: placement}
}

func settingsPolicyRefusal(kind agentic.SettingsPolicyFailureKind) error {
	return &agentic.SettingsPolicyError{Kind: kind}
}
