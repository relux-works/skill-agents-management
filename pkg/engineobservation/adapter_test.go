package engineobservation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

var testEngine = plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}

// goodV3FactValues is one healthy readings body: every fact an observed
// value except profile-ssh-forwarding, which is absent in local mode. The
// policy facts use the v3 catalog shapes, so this set admits under v3 and
// refuses under v2.
func goodV3FactValues() map[inferenceengine.Fact]string {
	return map[inferenceengine.Fact]string{
		inferenceengine.FactContextArgv:              `["--max-kv-size","8192"]`,
		inferenceengine.FactPrefillArgv:              `["--prefill-step-size","512"]`,
		inferenceengine.FactReasoningStreamField:     `delta.reasoning_content`,
		inferenceengine.FactHealth:                   `{"endpoint":"/health","healthy":true}`,
		inferenceengine.FactReadiness:                `{"weights_resident":true}`,
		inferenceengine.FactWeightArtifact:           `{"format":"safetensors","model_path":"/models/weights.safetensors","config_path":"/models/config.json"}`,
		inferenceengine.FactMemoryAccounting:         `{"method":"resident-bytes","bytes":4096,"includes_mapped_weights":true}`,
		inferenceengine.FactSpeculativeDecoding:      `{"capable":true,"active":false}`,
		inferenceengine.FactLoadState:                `{"state":"loaded","weights_resident":true}`,
		inferenceengine.FactUnloadState:              `{"state":"unloaded","weights_resident":false}`,
		inferenceengine.FactInferenceBusy:            `{"busy":false}`,
		inferenceengine.FactMemoryPressureSequence:   `{"pressure":"normal","consulted":["load-state","unload-state","inference-busy"],"action":"none"}`,
		inferenceengine.FactLocalExecutable:          `/opt/engines/bin/mlx-server`,
		inferenceengine.FactLocalArgv:                `["--port","8080"]`,
		inferenceengine.FactStressPolicy:             `{"configured":false}`,
		inferenceengine.FactRestartSupervisionPolicy: `{"configured":false}`,
	}
}

type testRow struct {
	fact    inferenceengine.Fact
	outcome string
	value   *string
	cause   *string
	reason  *string
}

func strptr(s string) *string { return &s }

// healthyRows renders the healthy body in MeasuredFacts order.
func healthyRows() []testRow {
	values := goodV3FactValues()
	rows := make([]testRow, 0, len(inferenceengine.MeasuredFacts()))
	for _, definition := range inferenceengine.MeasuredFacts() {
		if definition.Fact == inferenceengine.FactSSHForwarding {
			rows = append(rows, testRow{fact: definition.Fact, outcome: string(inferenceengine.OutcomeObservedAbsent)})
			continue
		}
		value, ok := values[definition.Fact]
		if !ok {
			panic("goodV3FactValues lacks " + definition.Fact)
		}
		rows = append(rows, testRow{fact: definition.Fact, outcome: string(inferenceengine.OutcomeObservedValue), value: strptr(value)})
	}
	return rows
}

// renderStatus renders one `curator-engines status --json` body carrying the
// given readings rows, plus additive broker fields the adapter must ignore.
func renderStatus(t *testing.T, version string, rows []testRow) []byte {
	t.Helper()
	facts := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		member := map[string]any{"fact": string(row.fact), "outcome": row.outcome, "source": "engines.toml/v1"}
		if row.value != nil {
			member["value"] = *row.value
		}
		if row.cause != nil {
			member["cause"] = *row.cause
		}
		if row.reason != nil {
			member["reason"] = *row.reason
		}
		facts = append(facts, member)
	}
	body, err := json.Marshal(map[string]any{
		"contract_version": 1,
		"broker":           map[string]any{"state": "serving", "source": "attested"},
		"readings":         map[string]any{"contract_version": version, "facts": facts},
	})
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	return body
}

func testQuery() vendorplugin.EngineObservationQuery {
	return vendorplugin.EngineObservationQuery{
		Engine:  testEngine,
		Runtime: "local-qwen",
		Model:   "qwen-local-test",
		Profile: "qwen-test-engine",
	}
}

func testResolver(project string, ok bool) ResolveProject {
	return func(vendorplugin.EngineObservationQuery) (string, bool) { return project, ok }
}

func TestAdapterDeclarationIsV3(t *testing.T) {
	adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true))
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	declaration := adapter.EngineObservationAdapterDeclaration()
	want := vendorplugin.EngineObservationAdapterDeclaration{
		Contract:       vendorplugin.EngineObservationAdapterContract,
		SchemaVersion:  vendorplugin.EngineObservationAdapterSchemaVersion,
		Engine:         testEngine,
		EngineKind:     inferenceengine.EngineKindNativeTransformer,
		EngineContract: inferenceengine.ContractVersionV3,
	}
	if declaration != want {
		t.Fatalf("declaration = %#v, want %#v", declaration, want)
	}
}

func TestNewAdapterRefusesInvalidConstruction(t *testing.T) {
	resolve := testResolver("/project", true)
	tests := []struct {
		name    string
		engine  plugin.Ref
		kind    inferenceengine.EngineKind
		resolve ResolveProject
		opts    []Option
	}{
		{"zero engine", plugin.Ref{}, inferenceengine.EngineKindNativeTransformer, resolve, nil},
		{"wrong engine kind", plugin.Ref{ID: "mlx", Kind: "vendor"}, inferenceengine.EngineKindNativeTransformer, resolve, nil},
		{"unknown implementation kind", testEngine, "future", resolve, nil},
		{"nil resolver", testEngine, inferenceengine.EngineKindNativeTransformer, nil, nil},
		{"nil runner", testEngine, inferenceengine.EngineKindNativeTransformer, resolve, []Option{WithCommandRunner(nil)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAdapter(test.engine, test.kind, test.resolve, test.opts...); !errors.Is(err, ErrReadingsQueryInvalid) {
				t.Fatalf("NewAdapter err = %v, want ErrReadingsQueryInvalid", err)
			}
		})
	}
}

func TestObserveEngineTranscribesHealthyReadings(t *testing.T) {
	output := renderStatus(t, inferenceengine.ContractVersionV3, healthyRows())
	var gotName string
	var gotArgs []string
	var gotDir string
	now := time.Now()
	adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
		WithCommandRunner(func(_ context.Context, name string, args []string, dir string) ([]byte, error) {
			gotName, gotArgs, gotDir = name, args, dir
			return output, nil
		}),
		WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	observation, err := adapter.ObserveEngine(context.Background(), testQuery())
	if err != nil {
		t.Fatalf("ObserveEngine: %v", err)
	}
	if gotName != "curator-engines" || gotDir != "/project" {
		t.Fatalf("subprocess = %s %v in %s, want curator-engines status in /project", gotName, gotArgs, gotDir)
	}
	wantArgs := []string{"status", "--engine", "qwen-test-engine", "--json"}
	if fmt.Sprint(gotArgs) != fmt.Sprint(wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
	query := testQuery()
	if observation.Engine != query.Engine || observation.Runtime != query.Runtime || observation.Model != query.Model || observation.Profile != query.Profile {
		t.Fatalf("observation identity = %#v/%#v/%#v/%#v, want the authoritative query echoed",
			observation.Engine, observation.Runtime, observation.Model, observation.Profile)
	}
	if !observation.ObservedAt.Equal(now) || !observation.ValidUntil.Equal(now.Add(30*time.Second)) {
		t.Fatalf("freshness = %v/%v, want %v/%v", observation.ObservedAt, observation.ValidUntil, now, now.Add(30*time.Second))
	}
	if _, err := inferenceengine.ValidateReadings(testEngine.ID, inferenceengine.EngineKindNativeTransformer, observation.Readings, inferenceengine.ContractVersionV3); err != nil {
		t.Fatalf("transcribed readings fail v3 validation: %v", err)
	}
}

func TestObserveEnginePreservesNotObservedCauses(t *testing.T) {
	rows := healthyRows()
	for i := range rows {
		if rows[i].fact == inferenceengine.FactReadiness {
			rows[i] = testRow{fact: rows[i].fact, outcome: string(inferenceengine.OutcomeNotObserved),
				cause: strptr(string(inferenceengine.NotObservedUnsupported)), reason: strptr("no attested live evidence for this fact")}
		}
	}
	output := renderStatus(t, inferenceengine.ContractVersionV3, rows)
	adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
		WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return output, nil }))
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	observation, err := adapter.ObserveEngine(context.Background(), testQuery())
	if err != nil {
		t.Fatalf("ObserveEngine: %v", err)
	}
	_, validationErr := inferenceengine.ValidateReadings(testEngine.ID, inferenceengine.EngineKindNativeTransformer, observation.Readings, inferenceengine.ContractVersionV3)
	if !errors.Is(validationErr, inferenceengine.ErrObservationUnsupported) {
		t.Fatalf("validation err = %v, want ErrObservationUnsupported (the manager's cause preserved)", validationErr)
	}
}

func TestObserveEngineMalformedReadingsRefuse(t *testing.T) {
	mutate := func(rows []testRow, index int, fn func(*testRow)) []testRow {
		out := append([]testRow(nil), rows...)
		fn(&out[index])
		return out
	}
	readiness := -1
	for i, row := range healthyRows() {
		if row.fact == inferenceengine.FactReadiness {
			readiness = i
		}
	}
	tests := []struct {
		name   string
		output func(t *testing.T) []byte
	}{
		{"not JSON", func(t *testing.T) []byte { return []byte("not json") }},
		{"missing readings member", func(t *testing.T) []byte { return []byte(`{"contract_version":1}`) }},
		{"wrong contract version", func(t *testing.T) []byte { return renderStatus(t, "observed-process/v2", healthyRows()) }},
		{"empty contract version", func(t *testing.T) []byte { return renderStatus(t, "", healthyRows()) }},
		{"missing facts member", func(t *testing.T) []byte {
			body, _ := json.Marshal(map[string]any{"readings": map[string]any{"contract_version": inferenceengine.ContractVersionV3}})
			return body
		}},
		{"unknown readings field", func(t *testing.T) []byte {
			body, _ := json.Marshal(map[string]any{"readings": map[string]any{
				"contract_version": inferenceengine.ContractVersionV3, "facts": []any{}, "future": true,
			}})
			return body
		}},
		{"trailing JSON", func(t *testing.T) []byte {
			return append(renderStatus(t, inferenceengine.ContractVersionV3, healthyRows()), "trailing"...)
		}},
		{"unknown outcome", func(t *testing.T) []byte {
			return renderStatus(t, inferenceengine.ContractVersionV3, mutate(healthyRows(), readiness, func(r *testRow) { r.outcome = "observed-eventually" }))
		}},
		{"observed value without value", func(t *testing.T) []byte {
			return renderStatus(t, inferenceengine.ContractVersionV3, mutate(healthyRows(), readiness, func(r *testRow) { r.value = nil }))
		}},
		{"observed value with failure fields", func(t *testing.T) []byte {
			return renderStatus(t, inferenceengine.ContractVersionV3, mutate(healthyRows(), readiness, func(r *testRow) {
				r.cause = strptr("unsupported")
			}))
		}},
		{"unknown cause", func(t *testing.T) []byte {
			return renderStatus(t, inferenceengine.ContractVersionV3, mutate(healthyRows(), readiness, func(r *testRow) {
				r.outcome = string(inferenceengine.OutcomeNotObserved)
				r.value = nil
				r.cause = strptr("sometimes")
				r.reason = strptr("vague")
			}))
		}},
		{"not-observed without reason", func(t *testing.T) []byte {
			return renderStatus(t, inferenceengine.ContractVersionV3, mutate(healthyRows(), readiness, func(r *testRow) {
				r.outcome = string(inferenceengine.OutcomeNotObserved)
				r.value = nil
				r.cause = strptr(string(inferenceengine.NotObservedUnsupported))
				r.reason = nil
			}))
		}},
		{"not-observed with empty reason", func(t *testing.T) []byte {
			return renderStatus(t, inferenceengine.ContractVersionV3, mutate(healthyRows(), readiness, func(r *testRow) {
				r.outcome = string(inferenceengine.OutcomeNotObserved)
				r.value = nil
				r.cause = strptr(string(inferenceengine.NotObservedUnsupported))
				r.reason = strptr("")
			}))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := test.output(t)
			adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
				WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return output, nil }))
			if err != nil {
				t.Fatalf("NewAdapter: %v", err)
			}
			if _, err := adapter.ObserveEngine(context.Background(), testQuery()); !errors.Is(err, ErrReadingsMalformed) {
				t.Fatalf("ObserveEngine err = %v, want ErrReadingsMalformed", err)
			}
		})
	}
}

// TestObserveEngineStrictEnvelopeRefuses pins the shared strict decoder at
// the adapter boundary: duplicates, case variants, nulls, missing members,
// wrong types and unknown members refuse as malformed before transcription
// can launder them into a healthy-looking set.
func TestObserveEngineStrictEnvelopeRefuses(t *testing.T) {
	splice := func(t *testing.T, healthy []byte, old, new string) []byte {
		t.Helper()
		mutated := bytes.Replace(healthy, []byte(old), []byte(new), 1)
		if bytes.Equal(mutated, healthy) {
			t.Fatalf("splice %q did not change input", old)
		}
		return mutated
	}
	healthy := func(t *testing.T) []byte {
		t.Helper()
		return renderStatus(t, inferenceengine.ContractVersionV3, healthyRows())
	}
	tests := []struct {
		name   string
		output func(t *testing.T) []byte
	}{
		{"duplicate contract version", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"contract_version":"observed-process/v3"`, `"contract_version":"bogus","contract_version":"observed-process/v3"`)
		}},
		{"duplicate outcome", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"outcome":"not-observed","outcome":"observed-value"`)
		}},
		{"duplicate readings", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"readings":`, `"readings":null,"readings":`)
		}},
		{"case-folded facts", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"facts":`, `"FACTS":`)
		}},
		{"case-folded outcome", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"OUTCOME":"observed-value"`)
		}},
		{"null failure fields", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"cause":null,"reason":null,"outcome":"observed-value"`)
		}},
		{"null outcome", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"outcome":null`)
		}},
		{"unknown row member", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"future":true,"outcome":"observed-value"`)
		}},
		{"missing source", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"source":"engines.toml/v1",`, ``)
		}},
		{"null readings", func(t *testing.T) []byte { return []byte(`{"readings":null}`) }},
		{"null facts", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":"observed-process/v3","facts":null}}`)
		}},
		{"string readings", func(t *testing.T) []byte { return []byte(`{"readings":"x"}`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := test.output(t)
			adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
				WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return output, nil }))
			if err != nil {
				t.Fatalf("NewAdapter: %v", err)
			}
			if _, err := adapter.ObserveEngine(context.Background(), testQuery()); !errors.Is(err, ErrReadingsMalformed) {
				t.Fatalf("ObserveEngine err = %v, want ErrReadingsMalformed", err)
			}
		})
	}
}

func TestObserveEnginePassesRowOrderThroughForTheValidator(t *testing.T) {
	rows := healthyRows()
	rows[0], rows[1] = rows[1], rows[0]
	output := renderStatus(t, inferenceengine.ContractVersionV3, rows)
	adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
		WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return output, nil }))
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	observation, err := adapter.ObserveEngine(context.Background(), testQuery())
	if err != nil {
		t.Fatalf("ObserveEngine transcribes wire order; validation judges it: %v", err)
	}
	_, validationErr := inferenceengine.ValidateReadings(testEngine.ID, inferenceengine.EngineKindNativeTransformer, observation.Readings, inferenceengine.ContractVersionV3)
	if !errors.Is(validationErr, inferenceengine.ErrObservationMalformed) {
		t.Fatalf("validation err = %v, want ErrObservationMalformed for reordered facts", validationErr)
	}
}

func TestObserveEngineReadFailuresStayReadFailures(t *testing.T) {
	cause := errors.New("exit status 1")
	adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
		WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return nil, cause }))
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	_, err = adapter.ObserveEngine(context.Background(), testQuery())
	if !errors.Is(err, ErrReadingsReadFailed) {
		t.Fatalf("ObserveEngine err = %v, want ErrReadingsReadFailed", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("ObserveEngine err = %v, must not match a caller context error", err)
	}
}

func TestObserveEngineCallerCancellationPropagates(t *testing.T) {
	adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
		WithCommandRunner(func(ctx context.Context, _ string, _ []string, _ string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}))
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.ObserveEngine(ctx, testQuery()); !errors.Is(err, context.Canceled) {
		t.Fatalf("ObserveEngine err = %v, want context.Canceled", err)
	}
}

func TestObserveEngineOwnTimeoutIsAReadFailureWhileCallerLives(t *testing.T) {
	adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
		WithCommandRunner(func(ctx context.Context, _ string, _ []string, _ string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
		WithTimeout(20*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	_, err = adapter.ObserveEngine(context.Background(), testQuery())
	if !errors.Is(err, ErrReadingsReadFailed) {
		t.Fatalf("ObserveEngine err = %v, want ErrReadingsReadFailed", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ObserveEngine err = %v, an internal timeout must not match the caller deadline", err)
	}
}

func TestObserveEngineResponseCapIsExact(t *testing.T) {
	output := renderStatus(t, inferenceengine.ContractVersionV3, healthyRows())
	newCapped := func(t *testing.T, cap int) *Adapter {
		t.Helper()
		adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
			WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return output, nil }),
			WithResponseCap(cap),
		)
		if err != nil {
			t.Fatalf("NewAdapter: %v", err)
		}
		return adapter
	}
	if _, err := newCapped(t, len(output)).ObserveEngine(context.Background(), testQuery()); err != nil {
		t.Fatalf("response at cap: %v", err)
	}
	if _, err := newCapped(t, len(output)-1).ObserveEngine(context.Background(), testQuery()); !errors.Is(err, ErrReadingsReadFailed) {
		t.Fatalf("response over cap err = %v, want ErrReadingsReadFailed", err)
	}
}

func TestObserveEngineRefusesUnresolvableQueries(t *testing.T) {
	output := renderStatus(t, inferenceengine.ContractVersionV3, healthyRows())
	runner := func(context.Context, string, []string, string) ([]byte, error) { return output, nil }
	newAdapter := func(t *testing.T, resolve ResolveProject) *Adapter {
		t.Helper()
		adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, resolve, WithCommandRunner(runner))
		if err != nil {
			t.Fatalf("NewAdapter: %v", err)
		}
		return adapter
	}
	t.Run("engine mismatch", func(t *testing.T) {
		query := testQuery()
		query.Engine = plugin.Ref{ID: "other", Kind: inferenceengine.Kind}
		if _, err := newAdapter(t, testResolver("/project", true)).ObserveEngine(context.Background(), query); !errors.Is(err, ErrReadingsQueryInvalid) {
			t.Fatalf("err = %v, want ErrReadingsQueryInvalid", err)
		}
	})
	t.Run("empty profile", func(t *testing.T) {
		query := testQuery()
		query.Profile = " "
		if _, err := newAdapter(t, testResolver("/project", true)).ObserveEngine(context.Background(), query); !errors.Is(err, ErrReadingsQueryInvalid) {
			t.Fatalf("err = %v, want ErrReadingsQueryInvalid", err)
		}
	})
	t.Run("resolver declines", func(t *testing.T) {
		if _, err := newAdapter(t, testResolver("", false)).ObserveEngine(context.Background(), testQuery()); !errors.Is(err, ErrReadingsQueryInvalid) {
			t.Fatalf("err = %v, want ErrReadingsQueryInvalid", err)
		}
	})
	t.Run("relative project", func(t *testing.T) {
		if _, err := newAdapter(t, testResolver("project", true)).ObserveEngine(context.Background(), testQuery()); !errors.Is(err, ErrReadingsQueryInvalid) {
			t.Fatalf("err = %v, want ErrReadingsQueryInvalid", err)
		}
	})
}

// Error messages may name the offending outcome/cause token for
// debuggability, but row values (the secret-bearing field) are never echoed.
func TestObserveEngineErrorsEchoNoRowValues(t *testing.T) {
	const secret = "reading-secret-that-must-not-escape"
	bodies := [][]byte{
		// Unknown outcome with a secret value attached.
		[]byte(`{"contract_version":1,"readings":{"contract_version":"observed-process/v3","facts":[{"fact":"x","outcome":"nope","value":"` + secret + `","source":"s"}]}}`),
		// Non-observation carrying a secret value.
		[]byte(`{"contract_version":1,"readings":{"contract_version":"observed-process/v3","facts":[{"fact":"readiness","outcome":"not-observed","value":"` + secret + `","cause":"unsupported","reason":"r","source":"s"}]}}`),
	}
	for i, body := range bodies {
		adapter, err := NewAdapter(testEngine, inferenceengine.EngineKindNativeTransformer, testResolver("/project", true),
			WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) { return body, nil }))
		if err != nil {
			t.Fatalf("NewAdapter: %v", err)
		}
		_, err = adapter.ObserveEngine(context.Background(), testQuery())
		if !errors.Is(err, ErrReadingsMalformed) {
			t.Fatalf("body %d err = %v, want ErrReadingsMalformed", i, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("body %d error echoes a row value: %v", i, err)
		}
	}
}
