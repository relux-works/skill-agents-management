package runtimecatalog

import (
	"strings"
	"unicode"
)

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

func rejectCredentialKeys(value any, file string) error {
	return scanCredentialKeys(value, file)
}

func scanCredentialKeys(value any, file string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			name := compactFieldName(key)
			if strings.Contains(name, "token") && strings.HasSuffix(name, "tokens") {
				if _, ok := integer(child); !ok {
					return refuse(RefusalMalformed, file, "token counter")
				}
			}
			if name == "credential" {
				if key != "credential" {
					return refuse(RefusalUnsupported, file, "credential field")
				}
				reference, ok := child.(map[string]any)
				if !ok || !safeCredentialReference(reference) {
					return refuse(RefusalUnsupported, file, "credential field")
				}
				continue
			}
			if isCredentialValueField(key) {
				return refuse(RefusalUnsupported, file, "credential field")
			}
			if err := scanCredentialKeys(child, file); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := scanCredentialKeys(child, file); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeCredentialReference(value map[string]any) bool {
	source, sourceOK := value["source"].(string)
	if !sourceOK || source != strings.TrimSpace(source) || len(value) < 1 || len(value) > 2 {
		return false
	}
	if source == "none" {
		if len(value) == 1 {
			return true
		}
		name, ok := value["name"].(string)
		return ok && name == ""
	}
	if source != "env-name" && source != "command" {
		return false
	}
	name, nameOK := value["name"].(string)
	if !nameOK || len(value) != 2 || name != strings.TrimSpace(name) {
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

func isCredentialValueField(field string) bool {
	name := compactFieldName(field)
	if strings.Contains(name, "token") && strings.HasSuffix(name, "tokens") {
		return false
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
