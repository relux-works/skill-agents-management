package inferenceengine_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	ie "github.com/relux-works/skill-agents-management/pkg/inferenceengine"
)

var policyV2Facts = []ie.Fact{ie.FactStressPolicy, ie.FactRestartSupervisionPolicy}

func policyV2Value(fact ie.Fact) map[string]any {
	if fact == ie.FactStressPolicy {
		return map[string]any{"prompt_tokens": 1024, "max_output_tokens": 1, "startup_timeout_seconds": 1, "request_timeout_seconds": 1, "sample_interval_milliseconds": 50}
	}
	return map[string]any{"fatal_output_substrings": []string{"fatal"}, "restart_on_failure": false, "max_restarts": 1, "restart_window_seconds": 1, "restart_delay_milliseconds": 0}
}
func policyJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
func validatePolicyV2(fact ie.Fact, raw string, versions ...string) (ie.Resolution, error) {
	overrides := map[ie.Fact]ie.ReadOutcome{}
	for _, policy := range policyV2Facts {
		overrides[policy] = ie.ReadValue(policyJSON(policyV2Value(policy)))
	}
	overrides[fact] = ie.ReadValue(raw)
	return ie.ValidateReadings("mlx", ie.EngineKindNativeTransformer, readings(overrides), versions...)
}
func assertPolicyV2(t *testing.T, fact ie.Fact, raw string, valid bool) {
	t.Helper()
	_, err := validatePolicyV2(fact, raw, ie.ContractVersionV3)
	if valid && err != nil {
		t.Fatalf("valid policy refused: %v", err)
	}
	if !valid && !errors.Is(err, ie.ErrObservationMalformed) {
		t.Fatalf("invalid policy admitted or wrong refusal: %v", err)
	}
}

type policyRange struct {
	fact      ie.Fact
	key       string
	low, high int64
}

var policyV2Ranges = []policyRange{
	{ie.FactStressPolicy, "prompt_tokens", 1024, 1000000},
	{ie.FactStressPolicy, "max_output_tokens", 1, 4096},
	{ie.FactStressPolicy, "startup_timeout_seconds", 1, 3600},
	{ie.FactStressPolicy, "request_timeout_seconds", 1, 86400},
	{ie.FactStressPolicy, "sample_interval_milliseconds", 50, 10000},
	{ie.FactRestartSupervisionPolicy, "max_restarts", 1, 100},
	{ie.FactRestartSupervisionPolicy, "restart_window_seconds", 1, 86400},
	{ie.FactRestartSupervisionPolicy, "restart_delay_milliseconds", 0, 60000},
}

func TestPolicyV2NumericBoundaries(t *testing.T) {
	for _, bound := range policyV2Ranges {
		t.Run(bound.key, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				value any
				valid bool
			}{
				{"below", bound.low - 1, false}, {"minimum", bound.low, true}, {"maximum", bound.high, true}, {"above", bound.high + 1, false},
				{"fraction", 1.5, false}, {"overflow", json.Number("9223372036854775808"), false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					value := policyV2Value(bound.fact)
					value[bound.key] = tc.value
					assertPolicyV2(t, bound.fact, policyJSON(value), tc.valid)
				})
			}
		})
	}
}

func TestPolicyV2RangeProperties(t *testing.T) {
	// Independent normative bounds from decision 03, sampled deterministically.
	for _, bound := range policyV2Ranges {
		t.Run(bound.key, func(t *testing.T) {
			for i := int64(0); i <= 32; i++ {
				value := policyV2Value(bound.fact)
				value[bound.key] = bound.low + (bound.high-bound.low)*i/32
				assertPolicyV2(t, bound.fact, policyJSON(value), true)
				for _, invalid := range []int64{bound.low - 1 - i, bound.high + 1 + i} {
					value[bound.key] = invalid
					assertPolicyV2(t, bound.fact, policyJSON(value), false)
				}
			}
		})
	}
}

func TestPolicyV2FatalSubstringBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		valid bool
	}{
		{"zero_entries", []string{}, false}, {"one_entry", []string{"x"}, true}, {"sixteen_entries", strings.Split(strings.Repeat("x,", 15)+"x", ","), true},
		{"seventeen_entries", strings.Split(strings.Repeat("x,", 16)+"x", ","), false},
		{"empty", []string{""}, false}, {"one_byte", []string{"x"}, true}, {"512_bytes", []string{strings.Repeat("x", 512)}, true}, {"513_bytes", []string{strings.Repeat("x", 513)}, false},
		{"multibyte_512", []string{strings.Repeat("é", 256)}, true}, {"multibyte_514", []string{strings.Repeat("é", 257)}, false},
		{"NUL", []string{"a\x00b"}, false}, {"whitespace", []string{" "}, true}, {"duplicate_entries", []string{"x", "x"}, true},
		{"later_empty", []string{"x", ""}, false}, {"later_long", []string{"x", strings.Repeat("x", 513)}, false}, {"later_NUL", []string{"x", "\x00"}, false},
		{"null_entry", []any{"x", nil}, false}, {"numeric_entry", []any{1}, false}, {"bool_entry", []any{true}, false}, {"object_entry", []any{map[string]any{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := policyV2Value(ie.FactRestartSupervisionPolicy)
			value["fatal_output_substrings"] = tc.value
			assertPolicyV2(t, ie.FactRestartSupervisionPolicy, policyJSON(value), tc.valid)
		})
	}
}

func TestPolicyV2ClosedDecoding(t *testing.T) {
	for _, fact := range policyV2Facts {
		t.Run(string(fact), func(t *testing.T) {
			base := policyJSON(policyV2Value(fact))
			for _, raw := range []string{"null", "[]", "true", "1", `"policy"`, "{", "{}", base + " {}", base + " garbage", base[:len(base)-1], base + " null"} {
				t.Run("shape/"+raw, func(t *testing.T) { assertPolicyV2(t, fact, raw, false) })
			}
			t.Run("unknown", func(t *testing.T) {
				value := policyV2Value(fact)
				value["extra"] = true
				assertPolicyV2(t, fact, policyJSON(value), false)
			})
			for key := range policyV2Value(fact) {
				t.Run(key, func(t *testing.T) {
					t.Run("missing", func(t *testing.T) {
						value := policyV2Value(fact)
						delete(value, key)
						assertPolicyV2(t, fact, policyJSON(value), false)
					})
					t.Run("duplicate", func(t *testing.T) {
						assertPolicyV2(t, fact, base[:len(base)-1]+`,"`+key+`":`+policyJSON(policyV2Value(fact)[key])+`}`, false)
					})
					t.Run("case", func(t *testing.T) {
						assertPolicyV2(t, fact, strings.Replace(base, `"`+key+`"`, `"`+strings.ToUpper(key)+`"`, 1), false)
					})
					for _, wrong := range []any{nil, "wrong", map[string]any{}, []any{}} {
						t.Run("wrong/"+policyJSON(wrong), func(t *testing.T) {
							value := policyV2Value(fact)
							value[key] = wrong
							assertPolicyV2(t, fact, policyJSON(value), false)
						})
					}
					// Scalar/array mismatches, including boolean false versus numeric zero.
					wrong := any(true)
					if key == "restart_on_failure" {
						wrong = 0
					}
					if key == "fatal_output_substrings" {
						wrong = false
					}
					t.Run("wrong_scalar", func(t *testing.T) {
						value := policyV2Value(fact)
						value[key] = wrong
						assertPolicyV2(t, fact, policyJSON(value), false)
					})
				})
			}
		})
	}
}

func TestPolicyV2NotConfiguredAndFailures(t *testing.T) {
	for _, fact := range policyV2Facts {
		t.Run(string(fact), func(t *testing.T) {
			resolved, err := validatePolicyV2(fact, ` { "configured" : false } `, ie.ContractVersionV3)
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range resolved.Results {
				if result.Fact() == fact {
					value, ok := result.(ie.ObservedValue)
					if !ok || value.Value() != `{"configured":false}` {
						t.Fatalf("not configured = %#v", result)
					}
				}
			}
			for _, raw := range []string{`{"configured":false} {}`, `{"configured":false} garbage`, `{"configured":true}`, `{"configured":null}`, `{"configured":0}`, `{"configured":"false"}`, `{"Configured":false}`, `{"configured":false,"extra":1}`, `{"configured":false,"configured":false}`, `{"configured":false,"restart_delay_milliseconds":0}`} {
				t.Run(raw, func(t *testing.T) { assertPolicyV2(t, fact, raw, false) })
			}
			for _, tc := range []struct {
				cause ie.NotObservedCause
				want  error
			}{{ie.NotObservedReadFailure, ie.ErrObservationRead}, {ie.NotObservedMalformed, ie.ErrObservationMalformed}, {ie.NotObservedUnsupported, ie.ErrObservationUnsupported}} {
				overrides := map[ie.Fact]ie.ReadOutcome{}
				for _, policy := range policyV2Facts {
					overrides[policy] = ie.ReadValue(`{"configured":false}`)
				}
				overrides[fact] = ie.ReadFailure(tc.cause, "unavailable")
				t.Run(string(tc.cause), func(t *testing.T) {
					_, err := ie.ValidateReadings("mlx", ie.EngineKindNativeTransformer, readings(overrides), ie.ContractVersionV3)
					if !errors.Is(err, tc.want) {
						t.Fatalf("failure = %v", err)
					}
				})
			}
		})
	}
}

func TestPolicyV2VersionCompatibility(t *testing.T) {
	old, err := validate(nil)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := ie.ValidateReadings("mlx", ie.EngineKindNativeTransformer, readings(nil), ie.ContractVersion)
	if err != nil || !reflect.DeepEqual(old, explicit) {
		t.Fatalf("explicit old contract changed: %v", err)
	}
	for _, fact := range policyV2Facts {
		t.Run(string(fact), func(t *testing.T) {
			overrides := map[ie.Fact]ie.ReadOutcome{fact: ie.ReadValue(policyJSON(policyV2Value(fact)))}
			if _, err := validate(overrides); !errors.Is(err, ie.ErrObservationMalformed) {
				t.Fatalf("v2 value admitted under old version: %v", err)
			}
			assertPolicyV2(t, fact, goodValue(fact), false)
		})
	}
	for _, kind := range []ie.EngineKind{ie.EngineKindNativeTransformer, ie.EngineKindGGUFServer} {
		old, err := ie.ValidateReadings("engine", kind, readings(nil), ie.ContractVersion)
		if err != nil {
			t.Fatal(err)
		}
		overrides := map[ie.Fact]ie.ReadOutcome{}
		for _, fact := range policyV2Facts {
			overrides[fact] = ie.ReadValue(policyJSON(policyV2Value(fact)))
		}
		current, err := ie.ValidateReadings("engine", kind, readings(overrides), ie.ContractVersionV3)
		if err != nil {
			t.Fatal(err)
		}
		if current.Contract.Version != ie.ContractVersionV3 {
			t.Fatal("wrong version")
		}
		for i, rule := range current.Contract.Rules {
			want := old.Contract.Rules[i].ValueContract
			switch rule.Fact {
			case ie.FactStressPolicy:
				want = ie.ValueContractStressPolicyV2
			case ie.FactRestartSupervisionPolicy:
				want = ie.ValueContractRestartPolicyV2
			}
			if rule.ValueContract != want {
				t.Fatalf("changed contract for %s", rule.Fact)
			}
			if rule.Fact != ie.FactStressPolicy && rule.Fact != ie.FactRestartSupervisionPolicy && !reflect.DeepEqual(current.Results[i], old.Results[i]) {
				t.Fatalf("changed old value bytes for %s", rule.Fact)
			}
		}
	}
	for _, versions := range [][]string{{""}, {"observed-process/v4"}, {ie.ContractVersion, ie.ContractVersionV3}} {
		if _, err := ie.ValidateReadings("mlx", ie.EngineKindNativeTransformer, readings(nil), versions...); !errors.Is(err, ie.ErrContractInvalid) {
			t.Fatalf("version selection accepted: %v", err)
		}
	}
}

func TestPolicyV2RestartBoolean(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		value := policyV2Value(ie.FactRestartSupervisionPolicy)
		value["restart_on_failure"] = enabled
		assertPolicyV2(t, ie.FactRestartSupervisionPolicy, policyJSON(value), true)
	}
}

func TestPolicyV2CanonicalValues(t *testing.T) {
	for _, fact := range policyV2Facts {
		t.Run(string(fact), func(t *testing.T) {
			raw := policyJSON(policyV2Value(fact))
			resolved, err := validatePolicyV2(fact, raw, ie.ContractVersionV3)
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range resolved.Results {
				if result.Fact() != fact {
					continue
				}
				value, ok := result.(ie.ObservedValue)
				if !ok {
					t.Fatalf("configured policy is not ObservedValue: %#v", result)
				}
				var want, got any
				if err := json.Unmarshal([]byte(raw), &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(value.Value()), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("policy fields changed: got %s want %s", value.Value(), raw)
				}
			}
			overrides := map[ie.Fact]ie.ReadOutcome{}
			for _, policy := range policyV2Facts {
				overrides[policy] = ie.ReadValue(`{"configured":false}`)
			}
			overrides[fact] = ie.ReadAbsent()
			absent, err := ie.ValidateReadings("mlx", ie.EngineKindNativeTransformer, readings(overrides), ie.ContractVersionV3)
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range absent.Results {
				if result.Fact() == fact && result.Outcome() != ie.OutcomeObservedAbsent {
					t.Fatalf("absence changed: %#v", result)
				}
			}
		})
	}
}
