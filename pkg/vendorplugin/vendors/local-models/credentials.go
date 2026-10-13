package localmodels

import (
	"strings"
	"unicode"
)

// credentialKeyStems is the exact compacted stem list from
// docs/operator-file-schema-v1.md. Names are lowercased and
// punctuation-compacted before matching. TestCredentialKeyStemListMatchesSchema
// fails the build if this list drifts from the documented one.
var credentialKeyStems = [...]string{
	"accesskey",
	"apikey",
	"auth",
	"authorization",
	"bearer",
	"clientsecret",
	"cookie",
	"credential",
	"jwt",
	"pass",
	"passphrase",
	"passw",
	"privatekey",
	"privkey",
	"pwd",
	"secret",
	"sessionid",
	"sessionkey",
	"sessionsecret",
	"sessiontoken",
	"signingkey",
	"token",
}

// validCredentialReference reports whether a credential table is the exact
// reference-only shape: source env-name/command/none with a valid name, never
// secret material. Only the exact "credential" table is admitted.
func validCredentialReference(value any) bool {
	table, ok := curatorTable(value)
	if !ok {
		return false
	}
	source, sourceOK := table["source"].(string)
	if !sourceOK || source != strings.TrimSpace(source) || len(table) < 1 || len(table) > 2 {
		return false
	}
	if source == "none" {
		if len(table) == 1 {
			return true
		}
		name, ok := table["name"].(string)
		return ok && name == ""
	}
	if source != "env-name" && source != "command" {
		return false
	}
	name, nameOK := table["name"].(string)
	if !nameOK || len(table) != 2 || name != strings.TrimSpace(name) {
		return false
	}
	return validReferenceName(name, source == "command")
}

func validReferenceName(value string, allowCommandPunctuation bool) bool {
	if value == "" || !asciiIdentifierStart(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		current := value[index]
		if asciiIdentifierStart(current) || asciiDigit(current) {
			continue
		}
		if allowCommandPunctuation && (current == '-' || current == '.') {
			continue
		}
		return false
	}
	return true
}

func asciiIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func asciiDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func isCredentialValueField(field string, value any) bool {
	if field == "credential" {
		return false
	}
	name := compactFieldName(field)
	// docs/operator-file-schema-v1.md:102-105 exempts only INTEGER-valued
	// *tokens counters (context_window_tokens, max_tokens). A string such
	// as session_tokens = "secret" stays a credential-shaped refusal.
	if strings.Contains(name, "token") && strings.HasSuffix(name, "tokens") {
		return !isIntegerCounter(value)
	}
	for _, stem := range credentialKeyStems {
		switch stem {
		case "auth":
			if hasFieldToken(field, stem) || strings.HasSuffix(name, stem) || strings.HasPrefix(name, stem) && hasCredentialStem(strings.TrimPrefix(name, stem)) {
				return true
			}
		case "pass":
			if hasFieldToken(field, stem) {
				return true
			}
		default:
			if strings.Contains(name, stem) {
				return true
			}
		}
	}
	return false
}

func hasCredentialStem(name string) bool {
	for _, stem := range credentialKeyStems {
		if stem == "auth" || stem == "pass" {
			continue
		}
		if strings.Contains(name, stem) {
			return true
		}
	}
	return false
}

func hasFieldToken(field, expected string) bool {
	var token strings.Builder
	previous := rune(0)
	flush := func() bool {
		if token.Len() == 0 {
			return false
		}
		matches := token.String() == expected
		token.Reset()
		return matches
	}
	for _, current := range field {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) {
			if flush() {
				return true
			}
			previous = 0
			continue
		}
		if token.Len() > 0 && unicode.IsUpper(current) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			if flush() {
				return true
			}
		}
		token.WriteRune(unicode.ToLower(current))
		previous = current
	}
	return flush()
}

func compactFieldName(value string) string {
	var compact strings.Builder
	for _, current := range strings.ToLower(value) {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			compact.WriteRune(current)
		}
	}
	return compact.String()
}

// isIntegerCounter reports whether a TOML value is an integer counter. Only
// integer-valued *tokens fields are exempt from the credential-stem gate;
// strings, floats, bools, tables, and arrays are not counters.
func isIntegerCounter(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}
