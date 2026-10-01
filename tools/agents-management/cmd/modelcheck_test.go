package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/engineobservation"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

const (
	modelCheckTestRuntime = "local-qwen"
	modelCheckTestModel   = "qwen-local-test"
)

type modelCheckTestStatusReader struct {
	status localruntime.Status
	err    error
	calls  int
	query  localruntime.StatusQuery
}

func (r *modelCheckTestStatusReader) Status(_ context.Context, query localruntime.StatusQuery) (localruntime.Status, error) {
	r.calls++
	r.query = query
	return r.status, r.err
}

func setModelCheckTestDeps(t *testing.T, config localmodels.ConfigResult, reader localruntime.StatusReader, run func(context.Context, agentic.Plan) modelCheckProcessResult) {
	t.Helper()
	previous := modelCheckDeps
	modelCheckDeps = modelCheckDependencies{
		loadConfig:   func() localmodels.ConfigResult { return config },
		statusReader: reader,
		runProcess:   run,
		getwd:        func() (string, error) { return t.TempDir(), nil },
		environ:      func() []string { return []string{"PATH=" + modelCheckTestPiPath(t)} },
		openEvidence: openModelCheckEvidence,
		// Hermetic by default: no observation adapter, so no subprocess.
		// Engine tests override with setModelCheckEngineAdapters.
		newEngineAdapters: func(plugin.Ref, modelCheckEngineScope) ([]vendorplugin.EngineObservationAdapter, error) {
			return nil, nil
		},
	}
	t.Cleanup(func() { modelCheckDeps = previous })
}

// setModelCheckEngineAdapters overrides the observation-adapter seam after
// setModelCheckTestDeps, for engine-bound tests that script readings.
func setModelCheckEngineAdapters(t *testing.T, fn func(plugin.Ref, modelCheckEngineScope) ([]vendorplugin.EngineObservationAdapter, error)) {
	t.Helper()
	previous := modelCheckDeps.newEngineAdapters
	modelCheckDeps.newEngineAdapters = fn
	t.Cleanup(func() { modelCheckDeps.newEngineAdapters = previous })
}

// modelCheckAdaptersWithRunner scripts the readings subprocess behind a
// production-shaped adapter: the same constructor and scope resolver as the
// shipped wiring, with only the command runner substituted.
func modelCheckAdaptersWithRunner(t *testing.T, output []byte, runErr error, calls *int) func(plugin.Ref, modelCheckEngineScope) ([]vendorplugin.EngineObservationAdapter, error) {
	t.Helper()
	return func(engine plugin.Ref, scope modelCheckEngineScope) ([]vendorplugin.EngineObservationAdapter, error) {
		adapter, err := engineobservation.NewAdapter(engine, inferenceengine.EngineKindNativeTransformer, modelCheckProjectResolver(scope),
			engineobservation.WithCommandRunner(func(context.Context, string, []string, string) ([]byte, error) {
				if calls != nil {
					*calls++
				}
				return output, runErr
			}))
		if err != nil {
			t.Fatalf("scripted engine adapter: %v", err)
		}
		return []vendorplugin.EngineObservationAdapter{adapter}, nil
	}
}

func modelCheckTestPiPath(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "pi")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake Pi executable: %v", err)
	}
	return directory
}

func modelCheckTestConfig(withEngine bool) localmodels.Config {
	model := localmodels.ModelEntry{
		Description:         "model-check test model",
		Publisher:           "test",
		Family:              "test",
		Lifecycle:           vendorplugin.LifecycleCurrent,
		ContextWindowTokens: 4096,
		EffortSupport:       agentic.EffortSupportNone,
		Pointer: localmodels.Pointer{
			CuratorEnginesProject: os.TempDir(),
			CuratorEnginesProfile: "model-check-test",
			PiModelIdentity:       "test-local/test-local",
		},
	}
	runtime := localmodels.RuntimeEntry{
		ID:     modelCheckTestRuntime,
		System: "pi",
		Models: map[vendorplugin.ModelID]localmodels.ModelEntry{modelCheckTestModel: model},
	}
	config := localmodels.Config{Runtimes: []localmodels.RuntimeEntry{runtime}}
	if withEngine {
		engine := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
		config.InferenceEngines = []plugin.ID{engine.ID}
		runtime.Engine = engine
		model.Engine = engine
		runtime.Models[modelCheckTestModel] = model
		config.Runtimes[0] = runtime
	}
	return config
}

func modelCheckTestArgs(evidence string, deadline string) []string {
	return []string{
		"model-check",
		"--runtime", modelCheckTestRuntime,
		"--model", modelCheckTestModel,
		"--prompt", "sensitive-prompt-for-test",
		"--expect", "expected-fragment-for-test",
		"--deadline", deadline,
		"--evidence", evidence,
	}
}

func decodeModelCheckReport(t *testing.T, output string) modelCheckReport {
	t.Helper()
	var report modelCheckReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("decode model-check stdout %q: %v", output, err)
	}
	return report
}

func requireModelCheckRefusal(t *testing.T, err error) modelCheckRefusal {
	t.Helper()
	var refusal modelCheckRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %v, want typed modelCheckRefusal", err)
	}
	return refusal
}

func TestModelCheckUnknownEngineRefusesBeforePiStarts(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "unknown.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_observation_adapter_unavailable" {
		t.Fatalf("refusal reason = %q, want engine_observation_adapter_unavailable", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 || reader.calls != 0 {
		t.Fatalf("unknown engine report=%+v runner calls=%d status reads=%d; no status adapter means Pi must not start", report, runnerCalls, reader.calls)
	}
}

func TestModelCheckReadFailedStatusRefusesBeforePiStarts(t *testing.T) {
	reader := &modelCheckTestStatusReader{err: fmt.Errorf("%w: test manager unavailable", localruntime.ErrStatusReadFailed)}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "read-failed.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_status_unavailable" {
		t.Fatalf("refusal reason = %q, want engine_status_unavailable", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 || reader.calls != 1 {
		t.Fatalf("read-failed report=%+v runner calls=%d status reads=%d; failed engine status must refuse before Pi", report, runnerCalls, reader.calls)
	}
	if reader.query.Runtime != localruntime.RuntimeID(modelCheckTestRuntime) || reader.query.Model != localruntime.ModelID(modelCheckTestModel) || reader.query.CuratorEnginesProfile != "model-check-test" {
		t.Fatalf("status query = %+v, want exact selected runtime/model/profile", reader.query)
	}
}

func TestModelCheckUnmetExpectationExitsNonzero(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		return modelCheckProcessResult{Output: []byte("a different model response"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "unmet.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "expected_result_not_found" {
		t.Fatalf("refusal reason = %q, want expected_result_not_found", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.ExitCode == 0 || !report.PiStarted || report.ExpectationMet {
		t.Fatalf("unmet expectation report = %+v, want a nonzero failed result after Pi", report)
	}
}

func TestModelCheckCallerDeadlineIsHonoured(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	deadlineReached := false
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(ctx context.Context, _ agentic.Plan) modelCheckProcessResult {
		<-ctx.Done()
		deadlineReached = errors.Is(ctx.Err(), context.DeadlineExceeded)
		return modelCheckProcessResult{ExitCode: 1, Started: true, Err: ctx.Err()}
	})
	evidence := filepath.Join(t.TempDir(), "deadline.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "500ms")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "caller_deadline_exceeded" {
		t.Fatalf("refusal reason = %q, want caller_deadline_exceeded", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if !deadlineReached || report.Status != modelCheckStatusFailed || !report.PiStarted || report.ExitCode == 0 {
		t.Fatalf("deadline report=%+v deadline reached=%v", report, deadlineReached)
	}
}

func TestModelCheckEvidenceIsSecretSafeMode0600AndNeverOverwritten(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	const secretOutput = "output-secret-that-must-not-persist"
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test " + secretOutput), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "safe.json")
	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if err != nil {
		t.Fatalf("model-check with matching output: %v", err)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusPassed || !report.ExpectationMet || report.ExitCode != 0 {
		t.Fatalf("success report = %+v", report)
	}
	info, err := os.Stat(evidence)
	if err != nil {
		t.Fatalf("stat evidence: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("evidence mode = %04o, want 0600", got)
	}
	data, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	for _, secret := range []string{"sensitive-prompt-for-test", "expected-fragment-for-test", secretOutput} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("evidence leaked sensitive input/output %q: %s", secret, data)
		}
	}
}

func TestModelCheckExistingEvidenceRefusesWithoutStartingPi(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "existing.json")
	const original = "existing-evidence-sentinel"
	if err := os.WriteFile(evidence, []byte(original), 0o644); err != nil {
		t.Fatalf("write sentinel evidence: %v", err)
	}

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "evidence_path_unavailable" {
		t.Fatalf("refusal reason = %q, want evidence_path_unavailable", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	data, readErr := os.ReadFile(evidence)
	if readErr != nil || string(data) != original || report.PiStarted || runnerCalls != 0 {
		t.Fatalf("existing evidence changed=%q readErr=%v report=%+v runner calls=%d", data, readErr, report, runnerCalls)
	}
}

func TestRunModelCheckProcessHonorsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	result := runModelCheckProcess(ctx, agentic.Plan{Binary: "/bin/sleep", Argv: []string{"5"}, Env: os.Environ()})
	if !result.Started || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("process result = %+v, want started process stopped by caller deadline", result)
	}
}

// --- Engine-observation (F1) readings builders and tests ---
//
// Builders render `curator-engines status --json` bodies behind the scripted
// readings runner. Rows follow MeasuredFacts order; values are the v3
// catalog shapes, so a healthy set admits under v3 and refuses under v2.

func modelCheckValueRow(fact inferenceengine.Fact, value string) map[string]any {
	return map[string]any{"fact": string(fact), "outcome": string(inferenceengine.OutcomeObservedValue), "value": value, "source": "engines.toml/v1"}
}

func modelCheckAbsentRow(fact inferenceengine.Fact) map[string]any {
	return map[string]any{"fact": string(fact), "outcome": string(inferenceengine.OutcomeObservedAbsent), "source": "engines.toml/v1"}
}

func modelCheckNotObservedRow(fact inferenceengine.Fact, cause inferenceengine.NotObservedCause, reason string) map[string]any {
	return map[string]any{
		"fact": string(fact), "outcome": string(inferenceengine.OutcomeNotObserved),
		"cause": string(cause), "reason": reason, "source": "unavailable",
	}
}

func modelCheckHealthyRows() []map[string]any {
	values := map[inferenceengine.Fact]string{
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
	rows := make([]map[string]any, 0, len(inferenceengine.MeasuredFacts()))
	for _, definition := range inferenceengine.MeasuredFacts() {
		if definition.Fact == inferenceengine.FactSSHForwarding {
			rows = append(rows, modelCheckAbsentRow(definition.Fact))
			continue
		}
		value, ok := values[definition.Fact]
		if !ok {
			panic("modelCheckHealthyRows lacks " + definition.Fact)
		}
		rows = append(rows, modelCheckValueRow(definition.Fact, value))
	}
	return rows
}

func modelCheckStatusBody(t *testing.T, version string, rows []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"contract_version": 1,
		"broker":           map[string]any{"state": "serving", "source": "attested"},
		"readings":         map[string]any{"contract_version": version, "facts": rows},
	})
	if err != nil {
		t.Fatalf("marshal status body: %v", err)
	}
	return body
}

func modelCheckReplaceRow(rows []map[string]any, fact inferenceengine.Fact, replacement map[string]any) []map[string]any {
	out := append([]map[string]any(nil), rows...)
	for i, row := range out {
		if row["fact"] == string(fact) {
			out[i] = replacement
			return out
		}
	}
	panic("modelCheckReplaceRow: no such fact " + fact)
}

// TestReviewReproEngineBoundHealthyManagerReachesPi is the rev1 reviewer's F1
// reproduction, kept as the committed regression: an engine-bound runtime
// with a healthy manager observation reaches Pi through the production
// BuildLaunch path. Rev1 failed here with
// engine_observation_adapter_unavailable and pi_calls=0 because no adapter
// was wired; the fixed wiring observes the manager and launches.
func TestReviewReproEngineBoundHealthyManagerReachesPi(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	readingsCalls := 0
	setModelCheckEngineAdapters(t, modelCheckAdaptersWithRunner(t,
		modelCheckStatusBody(t, inferenceengine.ContractVersionV3, modelCheckHealthyRows()), nil, &readingsCalls))
	evidence := filepath.Join(t.TempDir(), "healthy-engine.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "10s")...)
	if err != nil {
		t.Fatalf("healthy engine-bound manager must reach Pi: %v", err)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusPassed || !report.PiStarted || !report.ExpectationMet {
		t.Fatalf("healthy engine report = %+v, want a passed round-trip", report)
	}
	if runnerCalls != 1 || readingsCalls != 1 || reader.calls != 1 {
		t.Fatalf("runner/readings/status calls = %d/%d/%d, want 1/1/1: the observation was read and Pi ran", runnerCalls, readingsCalls, reader.calls)
	}
}

func TestModelCheckEngineReadFailureRefusesBeforePiStarts(t *testing.T) {
	tests := []struct {
		name   string
		runErr error
	}{
		{"subprocess error", errors.New("exit status 1")},
		// The manager returns no readings set for a stale observation, so
		// staleness surfaces on this same read-failure path by design.
		{"stale typed failure", errors.New("curator-engines status: readings_stale")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			readingsCalls := 0
			setModelCheckEngineAdapters(t, modelCheckAdaptersWithRunner(t, nil, test.runErr, &readingsCalls))
			evidence := filepath.Join(t.TempDir(), "read-failure.json")

			stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "10s")...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_observation_unavailable" {
				t.Fatalf("refusal reason = %q, want engine_observation_unavailable", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("read-failure report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
			}
			if readingsCalls != 1 || reader.calls != 0 {
				t.Fatalf("readings/preflight calls = %d/%d, want 1/0: refusal precedes preflight", readingsCalls, reader.calls)
			}
		})
	}
}

func TestModelCheckEngineMalformedReadingsRefuseBeforePiStarts(t *testing.T) {
	tests := []struct {
		name   string
		output func(t *testing.T) []byte
	}{
		{"not JSON", func(t *testing.T) []byte { return []byte("not json") }},
		{"wrong readings version", func(t *testing.T) []byte {
			return modelCheckStatusBody(t, "observed-process/v2", modelCheckHealthyRows())
		}},
		{"empty readings version", func(t *testing.T) []byte {
			return modelCheckStatusBody(t, "", modelCheckHealthyRows())
		}},
		{"truncated fact set", func(t *testing.T) []byte {
			return modelCheckStatusBody(t, inferenceengine.ContractVersionV3, modelCheckHealthyRows()[:16])
		}},
		{"reordered facts", func(t *testing.T) []byte {
			rows := modelCheckHealthyRows()
			rows[0], rows[1] = rows[1], rows[0]
			return modelCheckStatusBody(t, inferenceengine.ContractVersionV3, rows)
		}},
		{"readiness value violates its contract", func(t *testing.T) []byte {
			rows := modelCheckReplaceRow(modelCheckHealthyRows(), inferenceengine.FactReadiness,
				modelCheckValueRow(inferenceengine.FactReadiness, `{"weights_resident":false}`))
			return modelCheckStatusBody(t, inferenceengine.ContractVersionV3, rows)
		}},
		{"observed value without value", func(t *testing.T) []byte {
			rows := modelCheckHealthyRows()
			for _, row := range rows {
				if row["fact"] == string(inferenceengine.FactReadiness) {
					delete(row, "value")
				}
			}
			return modelCheckStatusBody(t, inferenceengine.ContractVersionV3, rows)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			setModelCheckEngineAdapters(t, modelCheckAdaptersWithRunner(t, test.output(t), nil, nil))
			evidence := filepath.Join(t.TempDir(), "malformed.json")

			stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "10s")...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_observation_unavailable" {
				t.Fatalf("refusal reason = %q, want engine_observation_unavailable", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("malformed report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
			}
		})
	}
}

func TestModelCheckEngineNotObservedReadingsRefuseBeforePiStarts(t *testing.T) {
	// The manager's honest unavailable shape: profile facts observed, runtime
	// facts explicitly not-observed with causes. None of it may pass.
	rows := modelCheckHealthyRows()
	for _, fact := range []inferenceengine.Fact{
		inferenceengine.FactHealth, inferenceengine.FactReadiness,
		inferenceengine.FactWeightArtifact, inferenceengine.FactMemoryAccounting,
		inferenceengine.FactSpeculativeDecoding, inferenceengine.FactLoadState,
		inferenceengine.FactUnloadState, inferenceengine.FactInferenceBusy,
		inferenceengine.FactMemoryPressureSequence, inferenceengine.FactReasoningStreamField,
	} {
		rows = modelCheckReplaceRow(rows, fact, modelCheckNotObservedRow(fact, inferenceengine.NotObservedUnsupported, "no attested live evidence for this fact"))
	}
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	setModelCheckEngineAdapters(t, modelCheckAdaptersWithRunner(t,
		modelCheckStatusBody(t, inferenceengine.ContractVersionV3, rows), nil, nil))
	evidence := filepath.Join(t.TempDir(), "not-observed.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "10s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_observation_unavailable" {
		t.Fatalf("refusal reason = %q, want engine_observation_unavailable", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 {
		t.Fatalf("not-observed report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
	}
}

func TestModelCheckEngineStatusDecodeFailureRefusesBeforePiStarts(t *testing.T) {
	tests := []struct {
		name  string
		cause error
	}{
		{"decode failure", localruntime.ErrDecodeFailure},
		{"query invalid", localruntime.ErrStatusQueryInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cause := test.cause
			reader := &modelCheckTestStatusReader{err: fmt.Errorf("test cause: %w", cause)}
			runnerCalls := 0
			setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			evidence := filepath.Join(t.TempDir(), "decode-failure.json")

			stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_status_unavailable" {
				t.Fatalf("refusal reason = %q, want engine_status_unavailable", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 || reader.calls != 1 {
				t.Fatalf("decode-failure report=%+v runner calls=%d status reads=%d", report, runnerCalls, reader.calls)
			}
		})
	}
}

func TestModelCheckEnginePreflightRefusedBeforePiStarts(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "preflight.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_preflight_refused" {
		t.Fatalf("refusal reason = %q, want engine_preflight_refused", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 || reader.calls != 1 {
		t.Fatalf("preflight report=%+v runner calls=%d status reads=%d", report, runnerCalls, reader.calls)
	}
}

// TestReviewReproDeadlineWithDescendantHoldingStdout is the rev1 reviewer's
// F2 reproduction, kept as the committed regression: a Pi whose descendant
// holds the stdout pipe must still die at the caller deadline. Rev1 took
// 6.2s against a 300ms deadline because exec.CommandContext kills only the
// direct child; the process-group kill returns at the deadline.
//
// Rev5 (round-4 finding modelcheck-f2-witness-false-negative): the window
// opens only after the fixture proves the child and its descendant both
// started and hold stdout. The round-4 witness started the 300ms deadline
// before process startup, so a descendant that had not yet inherited the pipe
// let the F2 mutant survive once in 7 executions. Absent readiness now fails
// instead of passing. The window close cancels the process context; exec's
// Cancel path (group kill plus WaitDelay) is identical for cancellation and
// for a true deadline, only the surfaced error differs.
func TestReviewReproDeadlineWithDescendantHoldingStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill is unix-only; windows bounds the direct child plus WaitDelay")
	}
	family := modelCheckDescendantFamilyByName(t, "background-plus-foreground")
	fixture := prepareModelCheckDescendantFixture(t, "pi", family.script)
	proof := proveModelCheckDescendantKill(t, fixture, family.markers, modelCheckDescendantReadyBound, modelCheckDescendantWindow, modelCheckPiDescendantLaunch(fixture))
	if proof.ReadyErr != nil {
		t.Fatalf("child/descendant readiness not observed within %v: cannot attest the group kill (err=%v)",
			modelCheckDescendantReadyBound, proof.Err)
	}
	// Fixed behavior closes at the window (~0.3s). Without the group kill,
	// Wait blocks on the pipe until WaitDelay (~2.3s). The threshold
	// separates the two by a wide margin on either side.
	if proof.Elapsed >= modelCheckDescendantKillThreshold {
		t.Fatalf("300ms window closed after %v (err=%v): the group kill did not fire", proof.Elapsed, proof.Err)
	}
	if !errors.Is(proof.Err, context.Canceled) {
		t.Fatalf("process err = %v, want a started process stopped by the window close", proof.Err)
	}
}

// TestRunModelCheckProcessRealPiEcho is the positive control for the real
// runner: an echoing Pi plan returns its output with a zero exit, proving
// the production runModelCheckProcess path the stubbed round-trip tests
// replace.
func TestRunModelCheckProcessRealPiEcho(t *testing.T) {
	script := filepath.Join(t.TempDir(), "pi")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho expected-fragment-for-test\n"), 0o755); err != nil {
		t.Fatalf("write fake Pi: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := runModelCheckProcess(ctx, agentic.Plan{Binary: script, Env: os.Environ()})
	if !result.Started || result.Err != nil || result.ExitCode != 0 || result.TooLarge {
		t.Fatalf("process result = %+v, want a clean echo", result)
	}
	if !strings.Contains(string(result.Output), "expected-fragment-for-test") {
		t.Fatalf("output = %q, want the echoed fragment", result.Output)
	}
}

func TestModelCheckLaunchPlanRefusedBeforePiStarts(t *testing.T) {
	// No pi executable on the launch PATH: admission and preflight pass,
	// then plan materialization fails inside BuildLaunch and reaches the
	// classifier's default arm.
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	previousEnviron := modelCheckDeps.environ
	modelCheckDeps.environ = func() []string { return []string{"PATH=" + t.TempDir()} }
	t.Cleanup(func() { modelCheckDeps.environ = previousEnviron })
	evidence := filepath.Join(t.TempDir(), "plan-refused.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "launch_plan_refused" {
		t.Fatalf("refusal reason = %q, want launch_plan_refused", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 || reader.calls != 1 {
		t.Fatalf("plan-refused report=%+v runner calls=%d status reads=%d; Pi must not start", report, runnerCalls, reader.calls)
	}
}

func TestModelCheckInvalidPiIdentityRefusedBeforePiStarts(t *testing.T) {
	// A pointer whose Pi model identity is not one provider/model pair fails
	// plan preparation inside BuildLaunch and reaches the default arm.
	config := modelCheckTestConfig(false)
	for i, runtime := range config.Runtimes {
		for id, model := range runtime.Models {
			model.Pointer.PiModelIdentity = "bogus"
			runtime.Models[id] = model
		}
		config.Runtimes[i] = runtime
	}
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: config}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "identity-refused.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "launch_plan_refused" {
		t.Fatalf("refusal reason = %q, want launch_plan_refused", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 || reader.calls != 0 {
		t.Fatalf("identity-refused report=%+v runner calls=%d status reads=%d; preparation refuses before observation", report, runnerCalls, reader.calls)
	}
}

// --- Admission and evidence refusal sites (F3) ---

func TestModelCheckInvalidArgumentsRefuse(t *testing.T) {
	base := func(evidence string) []string { return modelCheckTestArgs(evidence, "2s") }
	tests := []struct {
		name string
		args func(evidence string) []string
	}{
		{"zero deadline", func(evidence string) []string {
			args := base(evidence)
			for i, arg := range args {
				if arg == "--deadline" {
					args[i+1] = "0s"
				}
			}
			return args
		}},
		{"empty runtime", func(evidence string) []string {
			args := base(evidence)
			for i, arg := range args {
				if arg == "--runtime" {
					args[i+1] = ""
				}
			}
			return args
		}},
		{"empty model", func(evidence string) []string {
			args := base(evidence)
			for i, arg := range args {
				if arg == "--model" {
					args[i+1] = ""
				}
			}
			return args
		}},
		{"empty prompt", func(evidence string) []string {
			args := base(evidence)
			for i, arg := range args {
				if arg == "--prompt" {
					args[i+1] = ""
				}
			}
			return args
		}},
		{"empty expect", func(evidence string) []string {
			args := base(evidence)
			for i, arg := range args {
				if arg == "--expect" {
					args[i+1] = ""
				}
			}
			return args
		}},
		{"empty evidence", func(evidence string) []string {
			args := base(evidence)
			for i, arg := range args {
				if arg == "--evidence" {
					args[i+1] = ""
				}
			}
			return args
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			evidence := filepath.Join(t.TempDir(), "invalid.json")

			stdout, _, err := runRoot(t, test.args(evidence)...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "invalid_arguments" {
				t.Fatalf("refusal reason = %q, want invalid_arguments", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("invalid-arguments report=%+v runner calls=%d", report, runnerCalls)
			}
		})
	}
}

func TestModelCheckEvidencePermissionsUnavailableRefuses(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	// An evidence file that fails chmod: opened exclusively, then closed, so
	// the permission hardening reports "file already closed".
	previousOpen := modelCheckDeps.openEvidence
	modelCheckDeps.openEvidence = func(path string) (*os.File, error) {
		file, err := openModelCheckEvidence(path)
		if err != nil {
			return nil, err
		}
		_ = file.Close()
		return file, nil
	}
	t.Cleanup(func() { modelCheckDeps.openEvidence = previousOpen })
	evidence := filepath.Join(t.TempDir(), "permissions.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "evidence_permissions_unavailable" {
		t.Fatalf("refusal reason = %q, want evidence_permissions_unavailable", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 {
		t.Fatalf("permissions report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
	}
}

func TestModelCheckEvidenceWriteFailedRefuses(t *testing.T) {
	// An evidence file opened read-only: chmod succeeds, the summary write
	// fails. Both with Pi started and without, the report must name the
	// write failure rather than the earlier status.
	openReadOnly := func(t *testing.T) {
		t.Helper()
		previousOpen := modelCheckDeps.openEvidence
		modelCheckDeps.openEvidence = func(path string) (*os.File, error) {
			if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
				return nil, err
			}
			return os.OpenFile(path, os.O_RDONLY, 0o600)
		}
		t.Cleanup(func() { modelCheckDeps.openEvidence = previousOpen })
	}
	t.Run("pi not started", func(t *testing.T) {
		reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
		// Absent config: no Pi, then the write fails.
		setModelCheckTestDeps(t, localmodels.ConfigResult{Absent: true}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
			t.Fatal("Pi must not start when config is absent")
			return modelCheckProcessResult{}
		})
		openReadOnly(t)
		evidence := filepath.Join(t.TempDir(), "write-failed.json")

		stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
		if refusal := requireModelCheckRefusal(t, err); refusal.reason != "evidence_write_failed" {
			t.Fatalf("refusal reason = %q, want evidence_write_failed", refusal.reason)
		}
		report := decodeModelCheckReport(t, stdout)
		if report.Status != modelCheckStatusFailed || report.PiStarted {
			t.Fatalf("write-failed report=%+v", report)
		}
	})
	t.Run("pi started", func(t *testing.T) {
		reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
		setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
			return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
		})
		openReadOnly(t)
		evidence := filepath.Join(t.TempDir(), "write-failed-started.json")

		stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
		if refusal := requireModelCheckRefusal(t, err); refusal.reason != "evidence_write_failed" {
			t.Fatalf("refusal reason = %q, want evidence_write_failed", refusal.reason)
		}
		report := decodeModelCheckReport(t, stdout)
		if report.Status != modelCheckStatusFailed || !report.PiStarted {
			t.Fatalf("write-failed report=%+v, want Pi started and a failed write", report)
		}
	})
}

func TestModelCheckLocalModelsAbsentRefuses(t *testing.T) {
	tests := []struct {
		name   string
		config localmodels.ConfigResult
	}{
		{"absent", localmodels.ConfigResult{Absent: true}},
		// Absence refuses regardless of any accompanying config bytes: the
		// gate keys on the Absent flag, never on config emptiness.
		{"absent with config bytes", localmodels.ConfigResult{Absent: true, Config: modelCheckTestConfig(false)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, test.config, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			evidence := filepath.Join(t.TempDir(), "absent.json")

			stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "local_models_absent" {
				t.Fatalf("refusal reason = %q, want local_models_absent", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("absent report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
			}
		})
	}
}

func TestModelCheckLocalModelsUnreadableRefuses(t *testing.T) {
	tests := []struct {
		name   string
		config localmodels.ConfigResult
	}{
		{"read error", localmodels.ConfigResult{Err: fmt.Errorf("test: %w", localmodels.ErrConfigMalformed)}},
		// A read failure refuses even when partial config bytes accompany
		// it: partial reads never authorize a launch (M3).
		{"read error with partial runtimes", localmodels.ConfigResult{Config: modelCheckTestConfig(false), Err: fmt.Errorf("test: %w", localmodels.ErrConfigMalformed)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, test.config, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			evidence := filepath.Join(t.TempDir(), "unreadable.json")

			stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "local_models_unreadable" {
				t.Fatalf("refusal reason = %q, want local_models_unreadable", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("unreadable report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
			}
		})
	}
}

func TestModelCheckWorkingDirectoryUnavailableRefuses(t *testing.T) {
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	previousGetwd := modelCheckDeps.getwd
	modelCheckDeps.getwd = func() (string, error) { return "", fmt.Errorf("test: %w", os.ErrNotExist) }
	t.Cleanup(func() { modelCheckDeps.getwd = previousGetwd })
	evidence := filepath.Join(t.TempDir(), "workdir.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "working_directory_unavailable" {
		t.Fatalf("refusal reason = %q, want working_directory_unavailable", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 {
		t.Fatalf("workdir report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
	}
}

func TestModelCheckConfiguredModelUnavailableRefuses(t *testing.T) {
	nonPiConfig := func() localmodels.Config {
		config := modelCheckTestConfig(false)
		for i := range config.Runtimes {
			config.Runtimes[i].System = "codex"
		}
		return config
	}
	tests := []struct {
		name   string
		config localmodels.ConfigResult
		args   func(evidence string) []string
	}{
		{"unknown runtime", localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, func(evidence string) []string {
			args := modelCheckTestArgs(evidence, "2s")
			for i, arg := range args {
				if arg == "--runtime" {
					args[i+1] = "no-such-runtime"
				}
			}
			return args
		}},
		{"non-pi system", localmodels.ConfigResult{Config: nonPiConfig()}, func(evidence string) []string {
			return modelCheckTestArgs(evidence, "2s")
		}},
		{"unknown model", localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, func(evidence string) []string {
			args := modelCheckTestArgs(evidence, "2s")
			for i, arg := range args {
				if arg == "--model" {
					args[i+1] = "no-such-model"
				}
			}
			return args
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, test.config, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			evidence := filepath.Join(t.TempDir(), "unconfigured.json")

			stdout, _, err := runRoot(t, test.args(evidence)...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "configured_model_unavailable" {
				t.Fatalf("refusal reason = %q, want configured_model_unavailable", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("unconfigured report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
			}
		})
	}
}

// --- Caller-context refusal sites (F3) ---

func TestModelCheckDeadlineDuringStatusReadReportsDeadline(t *testing.T) {
	// The status read outlives the caller deadline: BuildLaunch returns its
	// error under an expired context, and the classifier's context arm
	// reports the deadline rather than the wrapped read error.
	reader := &blockingModelCheckStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "deadline-read.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "200ms")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "caller_deadline_exceeded" {
		t.Fatalf("refusal reason = %q, want caller_deadline_exceeded", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 || reader.calls != 1 {
		t.Fatalf("deadline-read report=%+v runner calls=%d status reads=%d", report, runnerCalls, reader.calls)
	}
}

// blockingModelCheckStatusReader blocks until the caller context ends, then
// reports a read failure: the error BuildLaunch wraps while the context arm
// classifies.
type blockingModelCheckStatusReader struct {
	status localruntime.Status
	calls  int
}

func (r *blockingModelCheckStatusReader) Status(ctx context.Context, _ localruntime.StatusQuery) (localruntime.Status, error) {
	r.calls++
	<-ctx.Done()
	return localruntime.Status{}, fmt.Errorf("test: %w", localruntime.ErrStatusReadFailed)
}

func TestModelCheckCancelledDuringStatusReadReportsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelModelCheckStatusReader{cancel: cancel}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "cancelled-read.json")

	stdout, _, err := runRootWithContext(t, ctx, modelCheckTestArgs(evidence, "10s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "caller_cancelled" {
		t.Fatalf("refusal reason = %q, want caller_cancelled", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 || reader.calls != 1 {
		t.Fatalf("cancelled-read report=%+v runner calls=%d status reads=%d", report, runnerCalls, reader.calls)
	}
}

// cancelModelCheckStatusReader cancels the caller context, then reports a
// read failure, so the classifier sees an error under cancellation.
type cancelModelCheckStatusReader struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancelModelCheckStatusReader) Status(_ context.Context, _ localruntime.StatusQuery) (localruntime.Status, error) {
	r.calls++
	r.cancel()
	return localruntime.Status{}, fmt.Errorf("test: %w", localruntime.ErrStatusReadFailed)
}

func TestModelCheckCancelledParentRefusesBeforeConfig(t *testing.T) {
	// A parent cancelled before admission refuses at the pre-config
	// deadline check, before any config load or status read.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "cancelled-parent.json")

	stdout, _, err := runRootWithContext(t, ctx, modelCheckTestArgs(evidence, "10s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "caller_cancelled" {
		t.Fatalf("refusal reason = %q, want caller_cancelled", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || report.PiStarted || runnerCalls != 0 || reader.calls != 0 {
		t.Fatalf("cancelled-parent report=%+v runner calls=%d status reads=%d", report, runnerCalls, reader.calls)
	}
}

func TestModelCheckCancelledDuringProcessReportsCancelled(t *testing.T) {
	// The parent is cancelled while Pi runs: the post-process deadline
	// check reports cancellation even though Pi itself succeeded.
	ctx, cancel := context.WithCancel(context.Background())
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		cancel()
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "cancelled-process.json")

	stdout, _, err := runRootWithContext(t, ctx, modelCheckTestArgs(evidence, "10s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "caller_cancelled" {
		t.Fatalf("refusal reason = %q, want caller_cancelled", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || !report.PiStarted {
		t.Fatalf("cancelled-process report=%+v, want Pi started and a cancelled report", report)
	}
}

// --- Post-launch refusal sites (F3) ---

func TestModelCheckPiProcessErrorRefuses(t *testing.T) {
	// An errored Pi with a zero exit and matching output still refuses: the
	// error half of the gate is not waived by the exit code (M1).
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), ExitCode: 0, Started: true, Err: errors.New("test: transport reset")}
	})
	evidence := filepath.Join(t.TempDir(), "process-error.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "pi_process_failed" {
		t.Fatalf("refusal reason = %q, want pi_process_failed", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || !report.PiStarted {
		t.Fatalf("process-error report=%+v, want Pi started and a failed report", report)
	}
}

func TestModelCheckPiNonzeroExitRefuses(t *testing.T) {
	// A nonzero exit without a Go error still refuses: the exit-code half
	// of the gate is not waived by a clean return.
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), ExitCode: 3, Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "nonzero-exit.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "pi_process_failed" {
		t.Fatalf("refusal reason = %q, want pi_process_failed", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || !report.PiStarted || report.PiExitCode != 3 {
		t.Fatalf("nonzero-exit report=%+v", report)
	}
}

func TestModelCheckPiOutputLimitRefuses(t *testing.T) {
	// Truncated output containing the expectation still refuses: a match in
	// the kept prefix proves nothing about the dropped remainder (M2).
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true, TooLarge: true}
	})
	evidence := filepath.Join(t.TempDir(), "output-limit.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "pi_output_limit_exceeded" {
		t.Fatalf("refusal reason = %q, want pi_output_limit_exceeded", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || !report.PiStarted {
		t.Fatalf("output-limit report=%+v, want Pi started and a failed report", report)
	}
}

func TestModelCheckEmptyOutputRefuses(t *testing.T) {
	// Empty Pi output cannot contain the expectation; it refuses as an
	// unmet expectation rather than passing on a vacuous match.
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(false)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		return modelCheckProcessResult{Output: nil, Started: true}
	})
	evidence := filepath.Join(t.TempDir(), "empty-output.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "2s")...)
	if refusal := requireModelCheckRefusal(t, err); refusal.reason != "expected_result_not_found" {
		t.Fatalf("refusal reason = %q, want expected_result_not_found", refusal.reason)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusFailed || !report.PiStarted {
		t.Fatalf("empty-output report=%+v, want Pi started and a failed report", report)
	}
}

// --- Round-2 rework (rev3): admission deadline and strict envelope ---

// TestModelCheckAdapterDeadlineDescendantRefuses is the round-2 G1 panel
// reproduction (TestPanelAdapterDeadlineDescendant), kept as the committed
// regression: a manager status subprocess whose descendant holds the stdout
// pipe must still die at the caller deadline. Rev2 took 3.1s against a
// 300ms deadline because the observation runner killed only the direct
// child; the process-group kill returns at the deadline.
//
// BUG-261002-13ll6s: the runRoot shape opened the caller deadline before the
// descendant inherited the pipe, so the G1-adapter mutant survived whenever
// startup lost the race. The witness now shares the rev-5 readiness-gated
// proof: it drives the production adapter wiring (newModelCheckEngineAdapters
// with the real subprocess runner) under a cancellable context, waits for
// the child and descendant readiness markers, and only then opens the
// measured window. The window close cancels the observation context; exec's
// Cancel path — group kill plus WaitDelay — is identical for cancellation
// and for a true caller deadline, only the surfaced error differs
// (context.Canceled here, attributed by ObserveEngine). The refusal mapping
// to caller_deadline_exceeded stays pinned by the stubbed deadline tests.
func TestModelCheckAdapterDeadlineDescendantRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill is unix-only; windows bounds the direct child plus WaitDelay")
	}
	family := modelCheckDescendantFamilyByName(t, "background-plus-foreground")
	fixture := prepareModelCheckDescendantFixture(t, "curator-engines", family.script)
	t.Setenv("PATH", fixture.Dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MODELCHECK_READY_DIR", fixture.ReadyDir)
	engine := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	scope := modelCheckEngineScope{
		Runtime: vendorplugin.RuntimeID(modelCheckTestRuntime),
		Model:   vendorplugin.ModelID(modelCheckTestModel),
		Project: fixture.Dir,
		Profile: "model-check-test",
	}
	adapters, err := newModelCheckEngineAdapters(engine, scope)
	if err != nil {
		t.Fatalf("production engine adapters: %v", err)
	}
	if len(adapters) != 1 {
		t.Fatalf("production engine adapters = %d, want exactly 1", len(adapters))
	}
	query := vendorplugin.EngineObservationQuery{Engine: engine, Runtime: scope.Runtime, Model: scope.Model, Profile: scope.Profile}
	proof := proveModelCheckDescendantKill(t, fixture, family.markers, modelCheckDescendantReadyBound, modelCheckDescendantWindow, func(ctx context.Context) error {
		_, err := adapters[0].ObserveEngine(ctx, query)
		return err
	})
	if proof.ReadyErr != nil {
		t.Fatalf("child/descendant readiness not observed within %v: cannot attest the group kill (err=%v)",
			modelCheckDescendantReadyBound, proof.Err)
	}
	// Fixed behavior closes at the window (~0.3s). Without the group kill,
	// Wait blocks on the pipe until WaitDelay (~2.3s). The threshold
	// separates the two by a wide margin on either side.
	if proof.Elapsed >= modelCheckDescendantKillThreshold {
		t.Fatalf("300ms window closed after %v (err=%v): the group kill did not fire", proof.Elapsed, proof.Err)
	}
	if !errors.Is(proof.Err, context.Canceled) {
		t.Fatalf("observation err = %v, want context.Canceled from the window close", proof.Err)
	}
}

// TestModelCheckStatusReaderDeadlineDescendantRefuses is the delta panel's
// unbound-branch reproduction, kept as the committed regression for the
// retained status-reader path: the same descendant-held pipe through the
// real CLIStatusReader must die at the caller deadline. Rev2 took 3.1s
// against an 800ms deadline on this path too.
//
// BUG-261002-13ll6s: same race as the adapter witness — the runRoot caller
// deadline opened before the descendant inherited the pipe. The witness now
// shares the rev-5 readiness-gated proof over the production CLIStatusReader
// with its real subprocess runner. The status reader wraps its killed read
// as ErrStatusReadFailed without caller-context attribution, so the witness
// asserts that production shape plus the group-kill/WaitDelay timing split;
// the refusal mapping to caller_deadline_exceeded stays pinned by the
// stubbed deadline tests.
func TestModelCheckStatusReaderDeadlineDescendantRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill is unix-only; windows bounds the direct child plus WaitDelay")
	}
	family := modelCheckDescendantFamilyByName(t, "background-plus-foreground")
	fixture := prepareModelCheckDescendantFixture(t, "curator-engines", family.script)
	t.Setenv("PATH", fixture.Dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MODELCHECK_READY_DIR", fixture.ReadyDir)
	reader := localruntime.NewCLIStatusReader()
	query := localruntime.StatusQuery{
		Runtime:               localruntime.RuntimeID(modelCheckTestRuntime),
		Model:                 localruntime.ModelID(modelCheckTestModel),
		CuratorEnginesProject: fixture.Dir,
		CuratorEnginesProfile: "model-check-test",
	}
	proof := proveModelCheckDescendantKill(t, fixture, family.markers, modelCheckDescendantReadyBound, modelCheckDescendantWindow, func(ctx context.Context) error {
		_, err := reader.Status(ctx, query)
		return err
	})
	if proof.ReadyErr != nil {
		t.Fatalf("child/descendant readiness not observed within %v: cannot attest the group kill (err=%v)",
			modelCheckDescendantReadyBound, proof.Err)
	}
	// Fixed behavior closes at the window (~0.3s). Without the group kill,
	// Wait blocks on the pipe until WaitDelay (~2.3s). The threshold
	// separates the two by a wide margin on either side.
	if proof.Elapsed >= modelCheckDescendantKillThreshold {
		t.Fatalf("300ms window closed after %v (err=%v): the group kill did not fire", proof.Elapsed, proof.Err)
	}
	if !errors.Is(proof.Err, localruntime.ErrStatusReadFailed) {
		t.Fatalf("status err = %v, want ErrStatusReadFailed from the killed read", proof.Err)
	}
}

// TestModelCheckEngineStrictEnvelopeRefusesBeforePiStarts drives the round-2
// G2 shapes through runRoot: duplicate members, case-variant keys, explicit
// nulls, missing members, wrong types and unknown members anywhere in the
// readings envelope must refuse before Pi. Each case mutates exactly one
// wire shape of an otherwise healthy v3 body (or supplies a minimal
// envelope for that shape).
func TestModelCheckEngineStrictEnvelopeRefusesBeforePiStarts(t *testing.T) {
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
		return modelCheckStatusBody(t, inferenceengine.ContractVersionV3, modelCheckHealthyRows())
	}
	tests := []struct {
		name   string
		output func(t *testing.T) []byte
	}{
		// Duplicates: last-member-wins would admit these.
		{"duplicate contract version", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"contract_version":"observed-process/v3"`, `"contract_version":"bogus","contract_version":"observed-process/v3"`)
		}},
		{"duplicate outcome", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"outcome":"not-observed","outcome":"observed-value"`)
		}},
		{"duplicate fact", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"fact":"context-argv"`, `"fact":"readiness","fact":"context-argv"`)
		}},
		{"duplicate source", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"source":"engines.toml/v1"`, `"source":"elsewhere","source":"engines.toml/v1"`)
		}},
		{"duplicate facts", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"facts":`, `"facts":[],"facts":`)
		}},
		{"duplicate readings", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"readings":`, `"readings":null,"readings":`)
		}},
		{"duplicate broker", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"broker":`, `"broker":null,"broker":`)
		}},
		// Case variants: exact-case keys refuse these as missing/unknown.
		{"case-folded facts", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"facts":`, `"FACTS":`)
		}},
		{"case-folded contract version", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"contract_version":"observed-process/v3"`, `"Contract_Version":"observed-process/v3"`)
		}},
		{"case-folded fact", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"fact":"context-argv"`, `"FACT":"context-argv"`)
		}},
		{"case-folded outcome", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"OUTCOME":"observed-value"`)
		}},
		{"case-folded source", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"source":"engines.toml/v1"`, `"SOURCE":"engines.toml/v1"`)
		}},
		{"case-folded readings", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"readings":`, `"READINGS":`)
		}},
		// Explicit nulls: the producer omits absent members instead.
		{"null failure fields", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"cause":null,"reason":null,"outcome":"observed-value"`)
		}},
		{"null outcome", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"outcome":null`)
		}},
		{"null fact", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"fact":"context-argv"`, `"fact":null`)
		}},
		{"null source", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"source":"engines.toml/v1"`, `"source":null`)
		}},
		{"null readings", func(t *testing.T) []byte { return []byte(`{"readings":null}`) }},
		{"null facts", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":"observed-process/v3","facts":null}}`)
		}},
		{"null contract version", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":null,"facts":[]}}`)
		}},
		// Missing members.
		{"missing readings", func(t *testing.T) []byte { return []byte(`{"contract_version":1}`) }},
		{"missing facts", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":"observed-process/v3"}}`)
		}},
		{"missing contract version", func(t *testing.T) []byte {
			return []byte(`{"readings":{"facts":[]}}`)
		}},
		{"missing source", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"source":"engines.toml/v1",`, ``)
		}},
		{"missing fact", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"fact":"context-argv",`, ``)
		}},
		{"missing outcome", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value",`, ``)
		}},
		// Wrong types and unknown members.
		{"unknown row member", func(t *testing.T) []byte {
			return splice(t, healthy(t), `"outcome":"observed-value"`, `"future":true,"outcome":"observed-value"`)
		}},
		{"numeric contract version", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":1,"facts":[]}}`)
		}},
		{"string facts", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":"observed-process/v3","facts":"x"}}`)
		}},
		{"string readings", func(t *testing.T) []byte { return []byte(`{"readings":"x"}`) }},
		{"null array element", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":"observed-process/v3","facts":[null]}}`)
		}},
		{"non-object row", func(t *testing.T) []byte {
			return []byte(`{"readings":{"contract_version":"observed-process/v3","facts":["x"]}}`)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			setModelCheckEngineAdapters(t, modelCheckAdaptersWithRunner(t, test.output(t), nil, nil))
			evidence := filepath.Join(t.TempDir(), "strict.json")

			stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "10s")...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_observation_unavailable" {
				t.Fatalf("refusal reason = %q, want engine_observation_unavailable", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("strict-envelope report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
			}
		})
	}
}

// TestModelCheckOuterReadingsAliasRefusesBeforePiStarts pins the round-3
// F-R3-1 regression (repeat-of G2): a case-fold alias of the owned outer
// "readings" member beside the canonical member refuses before Pi, whether
// the alias appears before or after the canonical member and whether its
// value is null or non-null. Each case starts from an otherwise healthy v3
// body, so only the alias distinguishes it from a pass.
func TestModelCheckOuterReadingsAliasRefusesBeforePiStarts(t *testing.T) {
	healthy := func(t *testing.T) []byte {
		t.Helper()
		return modelCheckStatusBody(t, inferenceengine.ContractVersionV3, modelCheckHealthyRows())
	}
	withAliasBefore := func(t *testing.T, alias, value string) []byte {
		t.Helper()
		body := healthy(t)
		return append([]byte(`{"`+alias+`":`+value+`,`), body[1:]...)
	}
	withAliasAfter := func(t *testing.T, alias, value string) []byte {
		t.Helper()
		body := healthy(t)
		if len(body) == 0 || body[len(body)-1] != '}' {
			t.Fatalf("healthy body does not end with }: %q", body)
		}
		out := append([]byte(nil), body[:len(body)-1]...)
		return append(out, []byte(`,"`+alias+`":`+value+`}`)...)
	}
	// A readings-shaped object: even a plausible alias value must not admit.
	nonNull := `{"contract_version":"observed-process/v3","facts":[]}`
	tests := []struct {
		name   string
		output func(t *testing.T) []byte
	}{
		{"READINGS null before", func(t *testing.T) []byte { return withAliasBefore(t, "READINGS", "null") }},
		{"READINGS null after", func(t *testing.T) []byte { return withAliasAfter(t, "READINGS", "null") }},
		{"READINGS non-null before", func(t *testing.T) []byte { return withAliasBefore(t, "READINGS", nonNull) }},
		{"READINGS non-null after", func(t *testing.T) []byte { return withAliasAfter(t, "READINGS", nonNull) }},
		{"Readings null before", func(t *testing.T) []byte { return withAliasBefore(t, "Readings", "null") }},
		{"Readings null after", func(t *testing.T) []byte { return withAliasAfter(t, "Readings", "null") }},
		{"Readings non-null before", func(t *testing.T) []byte { return withAliasBefore(t, "Readings", nonNull) }},
		{"Readings non-null after", func(t *testing.T) []byte { return withAliasAfter(t, "Readings", nonNull) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			runnerCalls := 0
			setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				runnerCalls++
				return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
			})
			setModelCheckEngineAdapters(t, modelCheckAdaptersWithRunner(t, test.output(t), nil, nil))
			evidence := filepath.Join(t.TempDir(), "outer-alias.json")

			stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "10s")...)
			if refusal := requireModelCheckRefusal(t, err); refusal.reason != "engine_observation_unavailable" {
				t.Fatalf("refusal reason = %q, want engine_observation_unavailable", refusal.reason)
			}
			report := decodeModelCheckReport(t, stdout)
			if report.Status != modelCheckStatusUnknown || report.PiStarted || runnerCalls != 0 {
				t.Fatalf("outer-alias report=%+v runner calls=%d; Pi must not start", report, runnerCalls)
			}
			if reader.calls != 0 {
				t.Fatalf("outer-alias status reads = %d, want 0: refusal precedes preflight", reader.calls)
			}
		})
	}
}

// TestModelCheckOuterAdditiveFieldsStayTolerated proves the F-R3-1 alias rule
// is narrow: unrelated additive outer members — including a nested object
// that mentions a readings-like key and a near-miss name that does not
// case-fold to "readings" — still admit a healthy launch. The decoder never
// recurses into ignored members.
func TestModelCheckOuterAdditiveFieldsStayTolerated(t *testing.T) {
	body := modelCheckStatusBody(t, inferenceengine.ContractVersionV3, modelCheckHealthyRows())
	if len(body) == 0 || body[len(body)-1] != '}' {
		t.Fatalf("healthy body does not end with }: %q", body)
	}
	tolerated := append([]byte(nil), body[:len(body)-1]...)
	tolerated = append(tolerated, []byte(`,"future_probe":{"nested":["READINGS"]},"readings_count":3}`)...)
	reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
	runnerCalls := 0
	setModelCheckTestDeps(t, localmodels.ConfigResult{Config: modelCheckTestConfig(true)}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
		runnerCalls++
		return modelCheckProcessResult{Output: []byte("expected-fragment-for-test"), Started: true}
	})
	setModelCheckEngineAdapters(t, modelCheckAdaptersWithRunner(t, tolerated, nil, nil))
	evidence := filepath.Join(t.TempDir(), "outer-additive.json")

	stdout, _, err := runRoot(t, modelCheckTestArgs(evidence, "10s")...)
	if err != nil {
		t.Fatalf("additive outer members must stay tolerated: %v", err)
	}
	report := decodeModelCheckReport(t, stdout)
	if report.Status != modelCheckStatusPassed || !report.PiStarted || runnerCalls != 1 {
		t.Fatalf("additive report=%+v runner calls=%d, want a passed round-trip", report, runnerCalls)
	}
}
