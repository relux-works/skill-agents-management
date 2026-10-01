package inferenceengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	ValueContractStressPolicyV2  ValueContract = "stress-policy/v2"
	ValueContractRestartPolicyV2 ValueContract = "restart-policy/v2"
)

func contractForVersion(kind EngineKind, version string) Contract {
	contract := contractFor(kind)
	contract.Version = version
	for i := range contract.Rules {
		contract.Rules[i].ValueContract = valueContractForVersion(contract.Rules[i].Fact, version)
	}
	return contract
}

func valueContractForVersion(fact Fact, version string) ValueContract {
	if version == ContractVersionV3 {
		switch fact {
		case FactStressPolicy:
			return ValueContractStressPolicyV2
		case FactRestartSupervisionPolicy:
			return ValueContractRestartPolicyV2
		}
	}
	return valueContractForFact(fact)
}

type stressPolicyV2 struct {
	PromptTokens               int64 `json:"prompt_tokens"`
	MaxOutputTokens            int64 `json:"max_output_tokens"`
	StartupTimeoutSeconds      int64 `json:"startup_timeout_seconds"`
	RequestTimeoutSeconds      int64 `json:"request_timeout_seconds"`
	SampleIntervalMilliseconds int64 `json:"sample_interval_milliseconds"`
}

type restartPolicyV2 struct {
	FatalOutputSubstrings    []string `json:"fatal_output_substrings"`
	RestartOnFailure         bool     `json:"restart_on_failure"`
	MaxRestarts              int64    `json:"max_restarts"`
	RestartWindowSeconds     int64    `json:"restart_window_seconds"`
	RestartDelayMilliseconds int64    `json:"restart_delay_milliseconds"`
}

func canonicalizePolicyV2(contract ValueContract, raw string) (string, error) {
	var destination any
	switch contract {
	case ValueContractStressPolicyV2:
		destination = &stressPolicyV2{}
	case ValueContractRestartPolicyV2:
		destination = &restartPolicyV2{}
	}
	notConfigured, err := decodePolicyV2(raw, destination)
	if err != nil {
		return "", err
	}
	if notConfigured {
		return `{"configured":false}`, nil
	}
	switch value := destination.(type) {
	case *stressPolicyV2:
		if value.PromptTokens < 1024 || value.PromptTokens > 1000000 {
			return "", errors.New("prompt_tokens outside 1024..1000000")
		}
		if value.MaxOutputTokens < 1 || value.MaxOutputTokens > 4096 {
			return "", errors.New("max_output_tokens outside 1..4096")
		}
		if value.StartupTimeoutSeconds < 1 || value.StartupTimeoutSeconds > 3600 {
			return "", errors.New("startup_timeout_seconds outside 1..3600")
		}
		if value.RequestTimeoutSeconds < 1 || value.RequestTimeoutSeconds > 86400 {
			return "", errors.New("request_timeout_seconds outside 1..86400")
		}
		if value.SampleIntervalMilliseconds < 50 || value.SampleIntervalMilliseconds > 10000 {
			return "", errors.New("sample_interval_milliseconds outside 50..10000")
		}
	case *restartPolicyV2:
		if len(value.FatalOutputSubstrings) < 1 || len(value.FatalOutputSubstrings) > 16 {
			return "", errors.New("fatal_output_substrings requires 1..16 entries")
		}
		for _, substring := range value.FatalOutputSubstrings {
			if len(substring) == 0 || len(substring) > 512 || strings.ContainsRune(substring, '\x00') {
				return "", errors.New("fatal substring requires 1..512 bytes without NUL")
			}
		}
		if value.MaxRestarts < 1 || value.MaxRestarts > 100 {
			return "", errors.New("max_restarts outside 1..100")
		}
		if value.RestartWindowSeconds < 1 || value.RestartWindowSeconds > 86400 {
			return "", errors.New("restart_window_seconds outside 1..86400")
		}
		if value.RestartDelayMilliseconds < 0 || value.RestartDelayMilliseconds > 60000 {
			return "", errors.New("restart_delay_milliseconds outside 0..60000")
		}
	}
	return encode(destination)
}

// decodePolicyV2 checks exact key spellings before typed decoding. Go's struct
// decoder alone accepts duplicate keys, case-insensitive keys and null scalars.
// Those legacy decoding semantics remain confined to the v1 value contracts.
func decodePolicyV2(raw string, destination any) (bool, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false, errors.New("policy requires an object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return false, err
		}
		key, ok := token.(string)
		if !ok {
			return false, errors.New("policy key requires a string")
		}
		if _, duplicate := fields[key]; duplicate {
			return false, fmt.Errorf("duplicate policy key %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return false, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return false, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false, errors.New("trailing policy JSON")
	}
	if configured, present := fields["configured"]; present {
		if len(fields) != 1 || string(configured) != "false" {
			return false, errors.New("not-configured requires only configured=false")
		}
		return true, nil
	}
	// Marshal the typed zero value to derive its exact required field inventory.
	shape, err := json.Marshal(destination)
	if err != nil {
		return false, err
	}
	var required map[string]json.RawMessage
	if err := json.Unmarshal(shape, &required); err != nil {
		return false, err
	}
	for key := range fields {
		if _, known := required[key]; !known {
			return false, fmt.Errorf("unknown policy key %q", key)
		}
	}
	for key := range required {
		value, present := fields[key]
		if !present {
			return false, fmt.Errorf("missing policy key %q", key)
		}
		if string(value) == "null" {
			return false, fmt.Errorf("null policy key %q", key)
		}
	}
	if err := decodeClosed(raw, destination); err != nil {
		return false, err
	}
	// Null array members also decode as zero string values, refused above.
	return false, nil
}
