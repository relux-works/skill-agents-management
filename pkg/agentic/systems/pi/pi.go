// Package pi is the agentic-system plugin for the retained local-model worker.
// It plans native Pi invocations and uses curator-engines only for a
// read-only broker status preflight.
//
// The package owns only the deterministic launch plan: native Pi's binary,
// argv, environment and stdin contract. It never starts, stops or signals the
// local model engine. curator-engines remains the lifecycle owner and is
// queried only by the read-only pkg/localruntime status client.
package pi

import (
	"context"
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
)

// systemID is this plugin's identity, and the spelling the local-qwen
// runtime declaration names as its System.
const systemID agentic.SystemID = "pi"

// System is the pi agentic-system plugin.
//
// Unlike most system plugins in this module it is NOT stateless: it holds
// the StatusReader its Preflight implementation reads through. That reader
// is supplied at construction (never resolved internally), so a caller can
// substitute a fake for every test in this package and this module's own
// cross-cutting regression suite.
type System struct {
	status localruntime.StatusReader
}

// New returns the plugin. It is exported so a caller building an isolated
// registry can register it without depending on package initialization
// order, and so a test can supply a fake StatusReader.
func New(status localruntime.StatusReader) *System { return &System{status: status} }

// init registers the plugin into the default registry, with the real
// CLI-subprocess StatusReader. A failure PANICS: every refusal Register can
// produce here is a build-time bug in this package.
func init() {
	if err := agentic.Register(New(localruntime.NewCLIStatusReader())); err != nil {
		panic(fmt.Sprintf("pi: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized
// spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration.
//
// No composition grammar, no goal/budget/service-tier support, no effort
// transport — none of these are declared for this retained local worker.
//
// LaunchModeInteractive (curator-spec Decision 0013 §5) is a native
// interactive session `pi --model <provider>/<model>`. Exec uses Pi's native
// `--print` mode for retained local-model workers. With EffortTransportNone
// the plugin transports no effort value. See args.go.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeExec,
			agentic.LaunchModeDryRun,
			agentic.LaunchModeInteractive,
		},
		EffortTransport:     agentic.EffortTransportNone,
		SupportsGoal:        false,
		SupportsBudget:      false,
		SupportsServiceTier: false,
		CompositionGrammar:  agentic.GrammarNone,
		HomeEnvVar:          "",
		DefaultHome:         "",
		AuthHint:            "",
	}
}

// ResolveBinary returns the exact native Pi executable on the launch
// environment's PATH. It is the same function for every mode, so a dry run
// reports its real target.
func (*System) ResolveBinary(req agentic.LaunchRequest) (string, error) {
	return resolveBinary(req.Env)
}

// Argv builds the argument vector for one launch mode, excluding the binary.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("pi: launch mode %s is not declared by this system", mode)
	}
	return args(req, mode)
}

// PrepareLaunchRequest validates the explicit Pi provider/model identity and
// snapshots the prompt before vendorplugin may observe an engine or invoke
// Preflight. PromptPath is read once and replaced with copied bytes, so the
// later BuildPlan cannot see a different file value. Dry-run retains its
// no-read placeholder behavior. An interactive launch has no prompt to
// snapshot and no profile flag to assert, so it passes through byte-for-byte
// after validating the model identity (args.go says why).
func (s *System) PrepareLaunchRequest(req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.LaunchRequestPreparation, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return agentic.LaunchRequestPreparation{}, fmt.Errorf("pi: launch mode %s is not declared by this system", mode)
	}
	if _, err := nativeModelIdentity(req); err != nil {
		return agentic.LaunchRequestPreparation{}, err
	}
	prepared, err := prepareLaunchRequest(req, mode)
	if err != nil {
		return agentic.LaunchRequestPreparation{}, err
	}
	return agentic.LaunchRequestPreparation{PromptPath: prepared.PromptPath, Prompt: prepared.Prompt}, nil
}

// ChildEnv passes the parent environment through UNFILTERED, plus the
// caller's run context written over it. This preserves the explicit
// CURATOR_ENGINES_PROJECT_DIR pointer for status preflight. See env.go.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin is always detached. The prompt is one named argv value and Process A
// observes EOF on stdin.
func (*System) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return stdin(req)
}

// ValidateComposition refuses EVERY composition, because this system
// declares no composition grammar.
func (*System) ValidateComposition(c agentic.Composition) error {
	if c.IsZero() {
		return nil
	}
	return fmt.Errorf("pi: unsupported launch composition; this system declares no composition grammar")
}

// Preflight implements agentic.Preflightable: a fail-fast, advisory,
// context-bounded readiness check consulted by vendorplugin.BuildLaunch
// before agentic.BuildPlan. See preflight.go for the StatusQuery
// construction and the admit/refuse table.
func (s *System) Preflight(ctx context.Context, req agentic.LaunchRequest) (agentic.PreflightEvidence, error) {
	return preflight(ctx, s.status, req)
}
