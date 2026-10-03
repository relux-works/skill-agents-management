package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	"github.com/relux-works/skill-agents-management/pkg/engineobservation"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
	"github.com/spf13/cobra"
)

const (
	modelCheckStatusUnknown = "unknown"
	modelCheckStatusFailed  = "failed"
	modelCheckStatusPassed  = "passed"
	modelCheckOutputLimit   = 1 << 20
	// modelCheckWaitDelay bounds Wait after the Pi child exits while a pipe
	// is still held open. The group kill closes pipes promptly in the
	// normal case; this only backstops a descendant that escaped it.
	modelCheckWaitDelay = 2 * time.Second
)

type modelCheckOptions struct {
	runtime  string
	model    string
	prompt   string
	expect   string
	deadline time.Duration
	evidence string
}

type modelCheckReport struct {
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	DeadlineMS     int64  `json:"deadline_ms"`
	ElapsedMS      int64  `json:"elapsed_ms"`
	ExpectationMet bool   `json:"expectation_met"`
	PiStarted      bool   `json:"pi_started"`
	PiExitCode     int    `json:"pi_exit_code"`
	ExitCode       int    `json:"exit_code"`
}

// modelCheckRefusal is the command's typed nonzero outcome. A passing report
// is the only status that is not refused by modelCheckRefusalFor.
type modelCheckRefusal struct{ reason string }

func (r modelCheckRefusal) Error() string { return "model-check: " + r.reason }

type modelCheckProcessResult struct {
	Output   []byte
	ExitCode int
	Started  bool
	Err      error
	TooLarge bool
}

type modelCheckDependencies struct {
	loadConfig        func() localmodels.ConfigResult
	statusReader      localruntime.StatusReader
	runProcess        func(context.Context, agentic.Plan) modelCheckProcessResult
	getwd             func() (string, error)
	environ           func() []string
	openEvidence      func(path string) (*os.File, error)
	newEngineAdapters func(engine plugin.Ref, scope modelCheckEngineScope) ([]vendorplugin.EngineObservationAdapter, error)
}

// modelCheckEngineScope is the selected engine-bound launch identity the
// observation adapter serves: exactly the runtime/model pointer the registry
// was built for, never a fallback.
type modelCheckEngineScope struct {
	Runtime vendorplugin.RuntimeID
	Model   vendorplugin.ModelID
	Project string
	Profile string
}

var modelCheckDeps = modelCheckDependencies{
	loadConfig:        localmodels.Peek,
	statusReader:      localruntime.NewCLIStatusReader(),
	runProcess:        runModelCheckProcess,
	getwd:             os.Getwd,
	environ:           os.Environ,
	openEvidence:      openModelCheckEvidence,
	newEngineAdapters: newModelCheckEngineAdapters,
}

// openModelCheckEvidence creates the evidence file exclusively with mode
// 0600. O_EXCL refuses an existing path (including a symlink) without
// touching it.
func openModelCheckEvidence(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// newModelCheckEngineAdapters builds the production observation adapter for
// an engine-bound runtime, or no adapters when the selected runtime names no
// engine. The adapter observes the manager's released status readings over
// the query's own project directory.
func newModelCheckEngineAdapters(engine plugin.Ref, scope modelCheckEngineScope) ([]vendorplugin.EngineObservationAdapter, error) {
	if engine == (plugin.Ref{}) {
		return nil, nil
	}
	// NativeTransformer is the declared kind because local-models.toml names
	// engine identities, not implementation kinds, and both kinds share the
	// closed validation rules (the kind-parameterized policy tests prove the
	// outcome is kind-independent; BuildLaunch discards the resolution after
	// validation).
	adapter, err := engineobservation.NewAdapter(engine, inferenceengine.EngineKindNativeTransformer, modelCheckProjectResolver(scope))
	if err != nil {
		return nil, err
	}
	return []vendorplugin.EngineObservationAdapter{adapter}, nil
}

// modelCheckProjectResolver answers the query's project only when the
// authoritative query matches the selected scope exactly.
func modelCheckProjectResolver(scope modelCheckEngineScope) engineobservation.ResolveProject {
	return func(query vendorplugin.EngineObservationQuery) (string, bool) {
		if query.Runtime != scope.Runtime || query.Model != scope.Model || query.Profile != scope.Profile {
			return "", false
		}
		return scope.Project, true
	}
}

var modelCheckCmd = newModelCheckCommand(&modelCheckDeps)

func newModelCheckCommand(deps *modelCheckDependencies) *cobra.Command {
	options := modelCheckOptions{}
	command := &cobra.Command{
		Use:   "model-check",
		Short: "Run one bounded managed-Pi model expectation check",
		Long: "Run one managed local-model Pi request through the same launch " +
			"admission path used by production. An unknown or unreadable engine " +
			"status refuses before Pi starts. Evidence contains only the result " +
			"summary; prompt, expected text and model output are never persisted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := runModelCheck(cmd.Context(), options, deps)
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
				return fmt.Errorf("model-check: writing report: %w", err)
			}
			return modelCheckRefusalFor(report)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.runtime, "runtime", "", "Configured local-model runtime id")
	flags.StringVar(&options.model, "model", "", "Configured model id under the runtime")
	flags.StringVar(&options.prompt, "prompt", "", "Prompt sent to Pi for this diagnostic")
	flags.StringVar(&options.expect, "expect", "", "Required literal substring in Pi's result")
	flags.DurationVar(&options.deadline, "deadline", 0, "Caller deadline for admission and the Pi process (for example 45s)")
	flags.StringVar(&options.evidence, "evidence", "", "New evidence file path; an existing path is refused")
	for _, name := range []string{"runtime", "model", "prompt", "expect", "deadline", "evidence"} {
		if err := command.MarkFlagRequired(name); err != nil {
			panic("model-check flag registration: " + err.Error())
		}
	}
	return command
}

func runModelCheck(parent context.Context, options modelCheckOptions, deps *modelCheckDependencies) modelCheckReport {
	startedAt := time.Now()
	report := modelCheckReport{
		Status:     modelCheckStatusUnknown,
		Reason:     "not_observed",
		DeadlineMS: options.deadline.Milliseconds(),
		ExitCode:   1,
	}
	if options.deadline <= 0 || options.runtime == "" || options.model == "" || options.prompt == "" || options.expect == "" || options.evidence == "" {
		report.Status = modelCheckStatusFailed
		report.Reason = "invalid_arguments"
		report.ElapsedMS = time.Since(startedAt).Milliseconds()
		return report
	}

	ctx, cancel := context.WithTimeout(parent, options.deadline)
	defer cancel()

	evidence, err := deps.openEvidence(options.evidence)
	if err != nil {
		report.Status = modelCheckStatusFailed
		report.Reason = "evidence_path_unavailable"
		report.ElapsedMS = time.Since(startedAt).Milliseconds()
		return report
	}
	if err := evidence.Chmod(0o600); err != nil {
		_ = evidence.Close()
		report.Status = modelCheckStatusFailed
		report.Reason = "evidence_permissions_unavailable"
		report.ElapsedMS = time.Since(startedAt).Milliseconds()
		return report
	}
	defer func() { _ = evidence.Close() }()

	finish := func() modelCheckReport {
		report.ElapsedMS = time.Since(startedAt).Milliseconds()
		if report.Status == modelCheckStatusPassed {
			report.ExitCode = 0
		} else {
			report.ExitCode = 1
		}
		if err := writeModelCheckEvidence(evidence, report); err != nil {
			report.Status = modelCheckStatusFailed
			report.Reason = "evidence_write_failed"
			report.ExitCode = 1
		}
		return report
	}

	if err := ctx.Err(); err != nil {
		report.Status = modelCheckStatusFailed
		report.Reason = deadlineReason(err)
		return finish()
	}

	loaded := deps.loadConfig()
	switch {
	case loaded.Absent:
		report.Reason = "local_models_absent"
		return finish()
	case loaded.Err != nil:
		report.Reason = "local_models_unreadable"
		return finish()
	}

	workDir, err := deps.getwd()
	if err != nil {
		report.Status = modelCheckStatusFailed
		report.Reason = "working_directory_unavailable"
		return finish()
	}

	registry, err := buildModelCheckRegistry(loaded.Config, vendorplugin.RuntimeID(options.runtime), vendorplugin.ModelID(options.model), deps.statusReader, deps.newEngineAdapters)
	if err != nil {
		report.Status = modelCheckStatusFailed
		report.Reason = "configured_model_unavailable"
		return finish()
	}

	request := vendorplugin.SpawnRequest{
		Runtime:  vendorplugin.RuntimeID(options.runtime),
		Model:    vendorplugin.ModelID(options.model),
		Prompt:   []byte(options.prompt),
		WorkDir:  workDir,
		Env:      deps.environ(),
		Deadline: options.deadline,
	}
	plan, err := vendorplugin.BuildLaunch(ctx, registry, request, agentic.LaunchModeExec)
	if err != nil {
		report.Status, report.Reason = classifyModelCheckLaunchError(err, ctx.Err())
		return finish()
	}

	process := deps.runProcess(ctx, plan)
	report.PiStarted = process.Started
	report.PiExitCode = process.ExitCode
	if ctx.Err() != nil {
		report.Status = modelCheckStatusFailed
		report.Reason = deadlineReason(ctx.Err())
		return finish()
	}
	if process.Err != nil || process.ExitCode != 0 {
		report.Status = modelCheckStatusFailed
		report.Reason = "pi_process_failed"
		return finish()
	}
	if process.TooLarge {
		report.Status = modelCheckStatusFailed
		report.Reason = "pi_output_limit_exceeded"
		return finish()
	}
	if !strings.Contains(string(process.Output), options.expect) {
		report.Status = modelCheckStatusFailed
		report.Reason = "expected_result_not_found"
		return finish()
	}
	report.Status = modelCheckStatusPassed
	report.Reason = "expectation_met"
	report.ExpectationMet = true
	return finish()
}

func buildModelCheckRegistry(cfg localmodels.Config, runtimeID vendorplugin.RuntimeID, modelID vendorplugin.ModelID, statusReader localruntime.StatusReader, newAdapters func(plugin.Ref, modelCheckEngineScope) ([]vendorplugin.EngineObservationAdapter, error)) (*vendorplugin.Registry, error) {
	runtime, ok := localmodelsRuntime(cfg, runtimeID)
	if !ok || runtime.System != "pi" {
		return nil, fmt.Errorf("model-check: runtime is not a configured Pi runtime")
	}
	model, ok := runtime.Models[modelID]
	if !ok {
		return nil, fmt.Errorf("model-check: model is not declared by the selected runtime")
	}

	selected := cfg
	selected.Runtimes = []localmodels.RuntimeEntry{runtime}

	systems := agentic.NewRegistry()
	if err := systems.Register(pi.New(statusReader)); err != nil {
		return nil, err
	}
	adapters, err := newAdapters(runtime.Engine, modelCheckEngineScope{
		Runtime: runtime.ID,
		Model:   modelID,
		Project: model.Pointer.CuratorEnginesProject,
		Profile: model.Pointer.CuratorEnginesProfile,
	})
	if err != nil {
		return nil, err
	}
	registry, err := vendorplugin.NewRegistryWithEngineObservationAdapters(systems, adapters...)
	if err != nil {
		return nil, err
	}
	for _, engine := range selected.EnginePlugins() {
		if err := registry.RegisterPlugin(engine); err != nil {
			return nil, err
		}
	}
	if err := registry.Register(localmodels.New(selected, localmodels.WithStatusReader(statusReader))); err != nil {
		return nil, err
	}
	if err := registry.DeclareRuntime(vendorplugin.RuntimeDeclaration{
		ID:     runtime.ID,
		System: runtime.System,
		Vendor: localmodels.VendorID,
		Engine: runtime.Engine,
		Broker: vendorplugin.BrokerProvenance{
			Checked: []string{"local-models.toml"},
			Found:   "declared once local-models registers",
		},
	}); err != nil {
		return nil, err
	}
	return registry, nil
}

func localmodelsRuntime(cfg localmodels.Config, id vendorplugin.RuntimeID) (localmodels.RuntimeEntry, bool) {
	for _, runtime := range cfg.Runtimes {
		if runtime.ID == id {
			return runtime, true
		}
	}
	return localmodels.RuntimeEntry{}, false
}

func classifyModelCheckLaunchError(err, contextErr error) (string, string) {
	switch {
	case contextErr != nil:
		return modelCheckStatusFailed, deadlineReason(contextErr)
	case errors.Is(err, vendorplugin.ErrEngineObservationAdapterMissing):
		return modelCheckStatusUnknown, "engine_observation_adapter_unavailable"
	case errors.Is(err, localruntime.ErrStatusReadFailed), errors.Is(err, localruntime.ErrDecodeFailure), errors.Is(err, localruntime.ErrStatusQueryInvalid):
		return modelCheckStatusUnknown, "engine_status_unavailable"
	case errors.Is(err, inferenceengine.ErrObservationRead), errors.Is(err, inferenceengine.ErrObservationMalformed), errors.Is(err, inferenceengine.ErrObservationUnsupported):
		return modelCheckStatusUnknown, "engine_observation_unavailable"
	case errors.Is(err, pi.ErrPreflightRefused):
		return modelCheckStatusUnknown, "engine_preflight_refused"
	default:
		return modelCheckStatusFailed, "launch_plan_refused"
	}
}

func deadlineReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "caller_deadline_exceeded"
	}
	return "caller_cancelled"
}

func writeModelCheckEvidence(file *os.File, report modelCheckReport) error {
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func modelCheckRefusalFor(report modelCheckReport) error {
	if report.Status == modelCheckStatusPassed {
		return nil
	}
	return modelCheckRefusal{reason: report.Reason}
}

type modelCheckOutput struct {
	buffer   bytes.Buffer
	limit    int
	tooLarge bool
}

func (w *modelCheckOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if remaining > n {
			remaining = n
		}
		_, _ = w.buffer.Write(data[:remaining])
	}
	if n > remaining {
		w.tooLarge = true
	}
	return n, nil
}

func runModelCheckProcess(ctx context.Context, plan agentic.Plan) modelCheckProcessResult {
	command := exec.CommandContext(ctx, plan.Binary, plan.Argv...)
	command.Dir = plan.WorkDir
	command.Env = plan.Env
	output := &modelCheckOutput{limit: modelCheckOutputLimit}
	command.Stdout = output
	command.Stderr = io.Discard
	if plan.Stdin.Attached {
		command.Stdin = bytes.NewReader(plan.Stdin.Bytes)
	}
	// The caller deadline kills the whole process group, not just Pi: a
	// descendant holding the stdout pipe would otherwise keep Wait blocked
	// past the deadline. WaitDelay backstops even a descendant that escaped
	// the group, bounding Wait after the child exits.
	setModelCheckProcessGroup(command)
	command.Cancel = func() error {
		if command.Process != nil {
			killModelCheckProcessGroup(command.Process.Pid)
			_ = command.Process.Kill()
		}
		return nil
	}
	command.WaitDelay = modelCheckWaitDelay
	if err := plan.VerifyBeforeExec(); err != nil {
		return modelCheckProcessResult{ExitCode: 1, Err: err}
	}
	if err := command.Start(); err != nil {
		return modelCheckProcessResult{ExitCode: 1, Err: err}
	}
	err := command.Wait()
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return modelCheckProcessResult{
		Output:   append([]byte(nil), output.buffer.Bytes()...),
		ExitCode: exitCode,
		Started:  true,
		Err:      err,
		TooLarge: output.tooLarge,
	}
}

func init() {
	rootCmd.AddCommand(modelCheckCmd)
}
