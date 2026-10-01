package engineobservation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// This file is the one strict JSON decoder every readings envelope passes
// through before transcription: the outer status object, the readings
// object, and each fact row. encoding/json matches object keys
// case-insensitively even with DisallowUnknownFields, and it silently lets a
// later duplicate member overwrite an earlier one, so a hostile or corrupt
// peer could smuggle "FACTS" beside "facts" or two contradictory
// contract_version values past struct decoding. This pre-pass enforces
// exact-case member sets and duplicate rejection on the raw key stream
// before any value is transcribed for the validator.
//
// Absent, null and zero are three distinct states on the wire. Required
// members must be present and non-null; optional members, when present,
// must also be non-null (the producer omits them instead of nulling them).
// All failures are plain errors; the transcription boundary wraps them in
// ErrReadingsMalformed. Error text names member keys and positions only,
// never row values, which may carry secret-bearing model output.

// decodeStrictObject decodes one JSON object with exact-case member checks.
// allowed lists the exact member names accepted; a nil map accepts any name
// (duplicates are still rejected) for additive envelopes whose unknown
// members are ignored. required lists the members that must be present.
// owned lists the canonical members this envelope owns: any member whose
// name case-folds to an owned name but is not byte-equal to it is an
// ambiguous alias and refuses, even in an additive (allowed=nil) envelope
// where unrelated unknown members stay tolerated. A nil owned map disables
// the alias check; production passes a non-nil set for every envelope.
// The returned map holds each member's raw value exactly once.
func decodeStrictObject(raw json.RawMessage, allowed map[string]bool, required []string, owned map[string]bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, errors.New("not a JSON object")
	}
	seen := map[string]bool{}
	members := map[string]json.RawMessage{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("object key is not a string")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate JSON member %q", name)
		}
		seen[name] = true
		if owned != nil && !owned[name] {
			for canonical := range owned {
				if strings.EqualFold(name, canonical) {
					return nil, fmt.Errorf("case-variant JSON member %q shadows canonical %q", name, canonical)
				}
			}
		}
		if allowed != nil && !allowed[name] {
			return nil, fmt.Errorf("unknown JSON member %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		members[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	for _, name := range required {
		if !seen[name] {
			return nil, fmt.Errorf("missing required JSON member %q", name)
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("trailing JSON")
		}
		return nil, err
	}
	return members, nil
}

// isJSONNull reports whether one raw member value is an explicit JSON null.
// The producer omits absent members instead of nulling them, so null is a
// malformed shape everywhere this decoder requires a value.
func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// requiredStrictString decodes one present member as a non-null JSON string.
// Presence is enforced by decodeStrictObject's required set; this enforces
// the non-null string type. The value is returned for transcription and
// never echoed into error text by this helper.
func requiredStrictString(members map[string]json.RawMessage, name string) (string, error) {
	raw, ok := members[name]
	if !ok {
		return "", fmt.Errorf("missing required JSON member %q", name)
	}
	if isJSONNull(raw) {
		return "", fmt.Errorf("member %q is null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("member %q is not a string", name)
	}
	return value, nil
}

// optionalStrictString decodes one optional member: absent yields nil, an
// explicit null refuses, and any other non-string type refuses. A present
// string is returned for shape-against-outcome checks.
func optionalStrictString(members map[string]json.RawMessage, name string) (*string, error) {
	raw, ok := members[name]
	if !ok {
		return nil, nil
	}
	if isJSONNull(raw) {
		return nil, fmt.Errorf("member %q is null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("member %q is not a string", name)
	}
	return &value, nil
}
