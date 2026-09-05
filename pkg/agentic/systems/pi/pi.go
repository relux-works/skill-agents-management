// Package pi is the agentic-system plugin for Process A: `agents-infra pi
// spawn --profile <name> --prompt <prompt> --deadline 30m --result-schema 1`,
// the short-lived wrapper that holds the
// shared-runtime lease connection for its own lifetime and execs the real
// `pi` binary as its child.
//
// It is the Layer-1 half of the local-models design (architecture decision
// TASK-260829-3jlxed): this package owns Process A's harness surface only —
// the binary, the argv prefix, the environment passthrough, the stdin
// transport, and the Preflight readiness check. It never starts, stops or
// signals Process B (the long-running local model server); that authority
// belongs entirely to relux-agents-infra's own shared-runtime broker, reached
// only through the read-only pkg/localruntime status client.
//
// It owns only the deterministic outer Process-A surface. agents-infra owns
// Pi's inner flags, child environment, process group, result translation and
// cleanup; Process B lifecycle remains outside this module.
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
// transport — none of these are specified anywhere in the architecture
// decision for Process A's wrapper, and declaring one with no construction
// behind it would be a capability claim nothing backs.
//
// LaunchModeInteractive (curator-spec Decision 0013 §5) is the interactive
// primary session the SAME wrapper starts when its first argument is not
// `spawn`, `turn` or `lifecycle`: `agents-infra pi --model <id>`, through the
// same resolved binary. With EffortTransportNone the argv is the model flag
// alone and stdin stays detached. See args.go.
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

// ResolveBinary returns the exact executable this launch will run:
// `agents-infra`, resolved on the LAUNCH environment's PATH — never raw
// `pi`, and never the process's own ambient PATH. It is the same function
// for every mode, so a dry run reports the launch's real target.
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

// PrepareLaunchRequest validates and snapshots the exact profile/prompt input
// before vendorplugin may observe an engine or invoke Preflight. PromptPath is
// read once and replaced with copied bytes, so the later BuildPlan cannot see a
// different file value. Dry-run retains its no-read placeholder behavior. An
// interactive launch has no prompt to snapshot and no profile flag to assert,
// so it passes through byte-for-byte (args.go says why).
func (s *System) PrepareLaunchRequest(req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.LaunchRequestPreparation, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return agentic.LaunchRequestPreparation{}, fmt.Errorf("pi: launch mode %s is not declared by this system", mode)
	}
	prepared, err := prepareLaunchRequest(req, mode)
	if err != nil {
		return agentic.LaunchRequestPreparation{}, err
	}
	return agentic.LaunchRequestPreparation{PromptPath: prepared.PromptPath, Prompt: prepared.Prompt}, nil
}

// ChildEnv passes the parent environment through UNFILTERED, plus the
// caller's run context written over it — deliberately, so any
// AGENTS_INFRA_CALLER_CWD entry the vendor's Spawn added reaches the child
// verbatim. See env.go.
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
