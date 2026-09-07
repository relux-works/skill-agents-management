// Package pinative is the agentic-system plugin for the Pi coding agent run
// NATIVELY: the `pi` binary itself, on the launch PATH, started as a human's
// interactive session against a cloud provider Pi authenticates with in its
// own managed home.
//
// It is the upstream half of TASK-260908-ggxfte (accepted rev2). The existing
// `pi` plugin (pkg/agentic/systems/pi) is a DIFFERENT thing and is untouched:
// that one is Process A of the local-models design, the `agents-infra pi`
// wrapper that holds a shared-runtime lease and drives a local model server.
// Rebinding `pi` would break `local-models.toml system="pi"` and the frozen
// local-qwen goldens, so native Pi gets its own id.
//
// # What this plugin is, stated where a reader meets it
//
//   - Binary: whatever `pi` resolves to on the LAUNCH environment's PATH
//     (binary.go). No wrapper, no npm root, no shim.
//   - Modes: interactive and dry-run ONLY. There is no exec grammar here —
//     the wrapper owns headless Pi (`spawn --profile … --result-schema 1`), and
//     a native `-p`/`--print` run would be an unbounded unsupervised child.
//     BuildPlan refuses exec with ErrUnsupportedLaunchMode.
//   - Argv: `--model <vendor>/<launch identity>` and, when the row carries an
//     effort, `--thinking <effort>`. NEVER a bare model id (args.go says why).
//   - Home: PI_CODING_AGENT_DIR with `~/.pi/agent` as the default. On-disk
//     provider-limit state is keyed by (runtime, home), so this is the one
//     declaration that separates two Pi homes' limit records.
//   - No preflight. This plugin does NOT implement agentic.Preflightable;
//     vendorplugin.BuildLaunch gates on that interface and therefore never
//     probes anything before a native plan. A plan builds with no
//     agents-infra on PATH.
//   - No composition grammar, no goal, budget or service tier. The interactive
//     mode carries none of those by contract (Decision 0013 §5).
//   - ChildEnv: run-context passthrough, unfiltered (env.go).
//
// # What this plugin deliberately does NOT know
//
// Which models the installed Pi catalog can launch. That is the vendor layer's
// fact — a row declares `pi-native` in Systems only when the id is present in
// the installed catalog's provider data — and BuildLaunch refuses a row that
// does not (ErrModelNotDrivenBySystem) before this plugin sees it. The plugin
// also does not know which thinking words Pi accepts: the effort is the row's
// vocabulary and is transported verbatim (invariant 4 of docs/architecture.md).
package pinative

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// systemID is this plugin's identity. The frozen runtimes that bind it are
// pi-anthropic, pi-openai and pi-google (pkg/vendorplugin/runtime.go).
const systemID agentic.SystemID = "pi-native"

// System is the native-Pi agentic-system plugin. It holds NO state.
type System struct{}

// New returns the plugin, exported so an isolated registry can register it.
func New() *System { return &System{} }

// init registers the plugin into the default registry. A failure PANICS: every
// refusal Register can produce here is a build-time bug in this package.
func init() {
	if err := agentic.Register(New()); err != nil {
		panic(fmt.Sprintf("pinative: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration.
//
// EffortTransportArgv is a claim that args.go carries through: a row with a
// required effort reaches `--thinking <word>`, and TestTheArgvTransportIsActuallyCarried
// holds the two halves together. HomeEnvVar/DefaultHome are Pi's own
// (`PI_CODING_AGENT_DIR`, `~/.pi/agent`), read from Pi 0.84.2's config
// resolution, and they are what providerlimits resolves an identity from.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeDryRun,
			agentic.LaunchModeInteractive,
		},
		EffortTransport:     agentic.EffortTransportArgv,
		SupportsGoal:        false,
		SupportsBudget:      false,
		SupportsServiceTier: false,
		CompositionGrammar:  agentic.GrammarNone,
		HomeEnvVar:          HomeEnvVar,
		DefaultHome:         DefaultHome,
		// Pi's own remediation for a missing provider credential: it stops at
		// "No API key found for <provider>" and offers /login inside the
		// session (probe pi-probe-provider-qualified-01.log, TASK-260908-ggxfte).
		AuthHint: "run /login inside pi for the provider, in the managed PI_CODING_AGENT_DIR home",
	}
}

// ResolveBinary returns the exact executable this launch will run, the same
// function for every mode so a dry run reports the launch's real target.
func (*System) ResolveBinary(req agentic.LaunchRequest) (string, error) {
	return resolveBinary(req.Env)
}

// Argv builds the argument vector for one launch mode, excluding the binary.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("pinative: launch mode %s is not declared by this system", mode)
	}
	return Args(req, mode)
}

// ChildEnv passes the parent environment through with the run context written
// over it. See env.go.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin attaches NOTHING, for every launch and every mode: an interactive
// session owns its terminal, and there is no exec mode with a prompt to feed.
func (*System) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}

// ValidateComposition refuses EVERY composition, because this system declares
// no composition grammar and its only real mode is interactive, where the MCP
// channel is the composer's to append after the plan.
func (*System) ValidateComposition(c agentic.Composition) error {
	if c.IsZero() {
		return nil
	}
	return fmt.Errorf("pinative: unsupported launch composition; this system declares no composition grammar")
}
