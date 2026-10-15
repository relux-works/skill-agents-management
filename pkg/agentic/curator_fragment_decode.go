package agentic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DecodeCuratorFragment decodes one raw Curator launch-environment fragment
// into the typed context, applying the curator-spec environments §10.2
// reader rules that a plain struct decode cannot enforce: a repeated key at
// any depth, an explicit null top-level member and an unknown member are
// refused before any typed value exists. A null member would otherwise
// decode to the same nil pointer as an absent one, which for root_context
// and credential_store changes the launch policy.
//
// It validates shape only. Values, revisions and the per-environment rules
// stay with ValidateCuratorContext, which BuildPlan runs, so a caller never
// constructs a launch from a fragment that has only been decoded.
func DecodeCuratorFragment(wire []byte) (*CuratorContext, error) {
	if err := rejectRepeatedJSONKeys(wire); err != nil {
		return nil, fmt.Errorf("%w: fragment JSON: %v", ErrCuratorContextMalformed, err)
	}
	var members map[string]json.RawMessage
	if decoded, err := decodeCuratorClosedObject(wire); err != nil {
		return nil, fmt.Errorf("%w: fragment must be one JSON object: %v", ErrCuratorContextMalformed, err)
	} else {
		members = decoded
	}
	for name, value := range members {
		if string(value) == "null" {
			return nil, fmt.Errorf("%w: fragment member %q must not be null", ErrCuratorContextMalformed, name)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	var context CuratorContext
	if err := decoder.Decode(&context); err != nil {
		return nil, fmt.Errorf("%w: fragment does not decode: %v", ErrCuratorContextMalformed, err)
	}
	return &context, nil
}

// rejectRepeatedJSONKeys walks one JSON value token by token and fails on
// the first object that repeats a member name, at any depth, or on trailing
// data after the value.
func rejectRepeatedJSONKeys(wire []byte) error {
	type frame struct {
		object    bool
		expectKey bool
		keys      map[string]bool
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	var stack []*frame
	completed := func() {
		if len(stack) > 0 && stack[len(stack)-1].object {
			stack[len(stack)-1].expectKey = true
		}
	}
	for started := false; ; started = true {
		token, err := decoder.Token()
		if err == io.EOF {
			if len(stack) != 0 || !started {
				return errors.New("truncated JSON value")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if started && len(stack) == 0 {
			return errors.New("trailing data after the JSON value")
		}
		if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
			top := stack[len(stack)-1]
			if token == json.Delim('}') {
				stack = stack[:len(stack)-1]
				completed()
				continue
			}
			name, isName := token.(string)
			if !isName {
				return errors.New("object member name is not a string")
			}
			if top.keys[name] {
				return fmt.Errorf("repeated member %q", name)
			}
			top.keys[name] = true
			top.expectKey = false
			continue
		}
		switch token {
		case json.Delim('{'):
			stack = append(stack, &frame{object: true, expectKey: true, keys: map[string]bool{}})
		case json.Delim('['):
			stack = append(stack, &frame{})
		case json.Delim(']'):
			stack = stack[:len(stack)-1]
			completed()
		default:
			completed()
		}
	}
}
