package agentic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

const maxSealDataBytes = 64 << 10

// UnmarshalJSON preserves the wire boundary: lexical checks run before any
// struct decoding can discard duplicate members or repair invalid Unicode.
func (s *Seal) UnmarshalJSON(wire []byte) error {
	if err := validateSealWire(wire); err != nil {
		return err
	}
	type plain Seal
	var parsed plain
	decoder := json.NewDecoder(bytes.NewReader(wire))
	if err := decoder.Decode(&parsed); err != nil {
		return fmt.Errorf("%w: invalid guard shape", ErrSealMalformed)
	}
	*s = Seal(parsed)
	return nil
}

func validateSealWire(wire []byte) error {
	if len(wire) > 1<<20 || !guardStringsRepresentable(string(wire)) || !validSealEscapes(wire) {
		return fmt.Errorf("%w: wire size or Unicode", ErrSealMalformed)
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err := walkSealJSON(decoder, "any", 1); err != nil {
		return fmt.Errorf("%w: closed wire violation", ErrSealMalformed)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing guard data", ErrSealMalformed)
	}
	var envelope struct {
		Data    json.RawMessage `json:"data"`
		Schema  string          `json:"schema"`
		Version string          `json:"schema_version"`
	}
	if err := json.Unmarshal(wire, &envelope); err != nil || len(envelope.Data) > maxSealDataBytes {
		return fmt.Errorf("%w: versioned data limit", ErrSealMalformed)
	}
	if envelope.Schema == "" || envelope.Version == "" {
		return fmt.Errorf("%w: missing dispatch fields", ErrSealMalformed)
	}
	if envelope.Schema != ExecGuardSchema {
		return fmt.Errorf("%w: guard schema", ErrSealUnknownSchema)
	}
	if envelope.Version != ExecGuardVersion {
		return fmt.Errorf("%w: guard version", ErrSealUnknownVersion)
	}
	decoder = json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err := walkSealJSON(decoder, "guard", 1); err != nil {
		return fmt.Errorf("%w: closed guard shape", ErrSealMalformed)
	}
	return nil
}

var sealWireMembers = map[string]map[string]string{
	"guard":    {"schema": "scalar", "schema_version": "scalar", "data": "data"},
	"data":     {"kind": "scalar", "sealed": "sealed", "binding": "binding"},
	"sealed":   {"system": "scalar", "binary": "scalar", "argv": "array", "artifacts": "artifacts", "selectors": "selectors", "digest": "scalar", "binding": "binding"},
	"artifact": {"name": "scalar", "path": "scalar", "digest": "scalar"},
	"binding":  {"binary": "scalar", "argv": "array", "env_names": "array", "selectors": "selectors", "key_id": "scalar", "digest": "scalar"},
}

var sealWireRequired = map[string][]string{
	"guard":    {"schema", "schema_version", "data"},
	"data":     {"kind"},
	"sealed":   {"system", "binary", "argv", "artifacts", "digest"},
	"artifact": {"name", "path", "digest"},
	"binding":  {"binary", "argv", "env_names", "selectors", "key_id", "digest"},
}

// Member presence is checked on tokens, before nil pointers can erase it.
// The hosted unsealed marker has no projections; local bindings have their
// own kind and never qualify for the frozen hosted schema.
func sealDataMembersMatch(kind string, seen map[string]bool) bool {
	switch kind {
	case SealKindUnsealed:
		return !seen["sealed"] && !seen["binding"]
	case SealKindUnsealedBound:
		return seen["binding"] && !seen["sealed"]
	case SealKindSealed:
		return seen["sealed"] && !seen["binding"]
	default:
		return false
	}
}

func walkSealJSON(d *json.Decoder, shape string, depth int) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		// No member in either the hosted or local seal schema is nullable.
		if shape != "any" && token == nil {
			return ErrSealMalformed
		}
		if shape != "any" {
			if _, text := token.(string); shape != "scalar" || !text {
				return ErrSealMalformed
			}
		}
		if number, numeric := token.(json.Number); numeric {
			integer, err := strconv.ParseInt(string(number), 10, 64)
			if shape != "any" || err != nil || string(number) == "-0" || integer < -9007199254740991 || integer > 9007199254740991 {
				return ErrSealMalformed
			}
		}
		return nil
	}
	if depth > 16 {
		return ErrSealMalformed
	}
	if shape != "any" && ((delim == '[' && shape != "array" && shape != "artifacts") || (delim == '{' && sealWireMembers[shape] == nil && shape != "selectors")) {
		return ErrSealMalformed
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		kind := ""
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrSealMalformed
			}
			child, known := sealWireMembers[shape][key]
			if shape == "any" {
				child, known = "any", true
			}
			if shape == "selectors" {
				child, known = "scalar", true
			}
			if seen[key] || !known {
				return ErrSealMalformed
			}
			seen[key] = true
			if shape == "data" && key == "kind" {
				kindToken, err := d.Token()
				value, text := kindToken.(string)
				if err != nil || !text {
					return ErrSealMalformed
				}
				kind = value
				continue
			}
			if err := walkSealJSON(d, child, depth+1); err != nil {
				return err
			}
		}
		for _, member := range sealWireRequired[shape] {
			if !seen[member] {
				return ErrSealMalformed
			}
		}
		if shape == "data" && !sealDataMembersMatch(kind, seen) {
			return ErrSealMalformed
		}
	case '[':
		child := "scalar"
		if shape == "any" {
			child = "any"
		}
		if shape == "artifacts" {
			child = "artifact"
		}
		for d.More() {
			if err := walkSealJSON(d, child, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrSealMalformed
	}
	_, err = d.Token()
	return err
}

// encoding/json replaces lone surrogates; reject them in raw string tokens
// first, while accepting valid pairs and escaped backslashes verbatim.
func validSealEscapes(wire []byte) bool {
	inString := false
	for i := 0; i < len(wire); i++ {
		if wire[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || wire[i] != '\\' {
			continue
		}
		i++
		if i >= len(wire) {
			return false
		}
		if wire[i] != 'u' {
			continue
		}
		if i+4 >= len(wire) {
			return false
		}
		code, err := strconv.ParseUint(string(wire[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xDC00 && code <= 0xDFFF {
			return false
		}
		if code < 0xD800 || code > 0xDBFF {
			continue
		}
		if i+6 >= len(wire) || wire[i+1] != '\\' || wire[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(wire[i+3:i+7]), 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return false
		}
		i += 6
	}
	return true
}

// DecodeSeal is the typed byte ingress, including malformed/truncated JSON
// for which encoding/json itself would return its own SyntaxError first.
func DecodeSeal(wire []byte) (Seal, error) {
	var seal Seal
	if err := seal.UnmarshalJSON(wire); err != nil {
		return Seal{}, err
	}
	return seal, nil
}
