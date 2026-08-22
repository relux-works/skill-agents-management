// Package codex is the agentic-system plugin for the Codex CLI.
//
// It is a port of the codex half of skill-project-management's spawn adapter
// table (tools/board-cli/internal/spawn), proven against the launch-surface
// goldens that repository's own harness captured at commit
// ed4878123061b39fdae67160f6b5632117b48a2f. Four goldens cover it —
// codex/exec-default-path, codex/exec-managed-npm-path, codex/exec-native-shim
// and codex/dry-run — and parity_test.go drives every one of them through the
// real agentic.Registry and agentic.BuildPlan.
//
// # What is hard about codex, stated where a reader meets it
//
//   - THREE binary-resolution paths, all live: a managed npm package root, an
//     npm shim unwrapped to its native binary, and plain PATH. See binary.go.
//   - ONE argv construction site. The source paid a task to collapse three
//     drifted spellings into one function; args.go is that function here and
//     argvguard_test.go fails the build if a second appears.
//   - An environment filter whose strips are EXACT KEYS. See env.go, and the
//     two source leaks it deliberately leaves open.
//
// # Reshapes from the source, all deliberate
//
//  1. Binary resolution reads LaunchRequest.Env rather than the process
//     environment (binary.go, resolveBinary).
//  2. The service-tier vocabulary map and the composition grammar id are
//     plugin-local rather than core enums (args.go, composition.go).
//  3. The run-context injection the source shared across all six adapters is
//     agentic.WithRunContext, in the core, because it was one function there
//     and six copies here would be six sources for one fact.
//  4. LaunchRequest gained Run and Profile, and CompositionServer gained
//     BearerTokenEnvVar, because the facts the source read off its *Config are
//     otherwise unreachable from a plugin that holds nothing ambient.
package codex

import (
	"fmt"
	"os"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// systemID is this plugin's identity. It is one of the frozen runtime ids —
// admitted-pair digests and limit-state filenames are keyed by it downstream —
// so it may never change spelling.
const systemID agentic.SystemID = "codex"

// System is the codex agentic-system plugin.
//
// It holds NO state. Every answer is computed from the LaunchRequest it is
// given, which is what lets one registered value serve concurrent launches and
// what makes a dry run provably identical to the launch it mirrors.
type System struct{}

// New returns the plugin. It is exported so a caller building an isolated
// registry — every test in this package, for one — can register it without
// depending on package initialization order.
func New() *System { return &System{} }

// init registers the plugin into the default registry, which is the
// registration path docs/architecture.md describes and the one the CLI's
// `plugins` command reads.
//
// A failure PANICS rather than being dropped. Register refuses a duplicate id,
// an unnormalized id, an unstable id and a declaration that could never launch
// — every one of those is a build-time bug in this package, and a plugin that
// silently fails to register is a launch that reports "no system registered"
// with nothing anywhere naming why.
func init() {
	if err := agentic.Register(New()); err != nil {
		panic(fmt.Sprintf("codex: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration.
//
// The three launch modes are the source's three codex surfaces: the one-shot
// `codex exec` this tool owns, its side-effect-free dry-run mirror, and the
// provider-args fragment an external composer splices into a PTY-owned managed
// session.
//
// LaunchModeManagedSession is declared with its evidence named, because the
// goldens have NONE for it — the source's capture harness lives in package
// spawn and that surface lived in package cmd, so it was never captured. The
// source proved it separately, against a frozen copy of the pre-refactor
// construction, and args_test.go's TestManagedSessionArgsMatchTheFrozenConstruction
// is that proof ported. Declaring the mode without it would be a capability
// claim that does not reproduce.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeExec,
			agentic.LaunchModeDryRun,
			agentic.LaunchModeManagedSession,
		},
		// Argv: codex carries effort as a `-c model_reasoning_effort=...`
		// config override. TRANSPORT only — the words themselves belong to the
		// model row in the vendor layer.
		EffortTransport:     agentic.EffortTransportArgv,
		SupportsGoal:        true,
		SupportsBudget:      false,
		SupportsServiceTier: true,
		CompositionGrammar:  GrammarTOMLConfigPairs,
		// The codex CLI reads exactly one variable for its configuration home,
		// regardless of which runtime asked it to launch. Limit state is keyed
		// by the home this resolves to, so neither value may move without a
		// demonstrated migration on real state files.
		HomeEnvVar:  "CODEX_HOME",
		DefaultHome: "~/.codex",
		AuthHint:    "Codex authentication is unavailable; run `codex login`, then relaunch via `task-board codex` and retry",
	}
}

// ResolveBinary returns the exact executable this launch will run.
//
// It is the same function for every mode, which is the contract's requirement
// that a dry run report the launch's real target. The source's own bug was
// precisely the opposite: its dry-run path reported the literal string "codex"
// while a real launch resolved a managed-npm path, and nothing compared them
// until a parity capture did.
func (*System) ResolveBinary(req agentic.LaunchRequest) (string, error) {
	return resolveBinary(req.Env)
}

// Argv builds the argument vector for one launch mode, excluding the binary.
//
// The mode is checked against the declaration before Args is called, so a mode
// this system did not declare is refused here as well as in BuildPlan. Two
// refusals for one fact is deliberate at a plugin boundary: BuildPlan's check
// protects the dispatch, this one protects a caller holding the plugin
// directly.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("codex: launch mode %s is not declared by this system", mode)
	}
	return Args(req, mode)
}

// ChildEnv is the environment contract: strip the parent codex runtime state,
// then write the run context and the resolved service tier.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin is codex's prompt transport: the assignment prompt is streamed on
// standard input, which is what the `-` in the argv refers to.
//
// PromptPath is read when the caller supplied one, because that is what the
// source does and because a file read is the transport actually exercised in
// production. A read FAILURE is an error, never an empty payload: an absent
// prompt and an unreadable one are different facts, and returning "nothing
// attached" for a permissions error would launch a codex child that sits on an
// empty stdin and reports the model produced no work.
//
// Prompt bytes are the alternative for a caller that has the text without a
// file. Supplying NEITHER attaches nothing, which is the state a dry run is in
// — BuildPlan calls this method for every mode, and a dry run that failed here
// because no prompt file exists yet would be a dry run performing work.
func (*System) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	if path := strings.TrimSpace(req.PromptPath); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return agentic.StdinPayload{}, fmt.Errorf("codex: reading the assignment prompt: %w", err)
		}
		return agentic.StdinPayload{Attached: true, Bytes: data}, nil
	}
	if len(req.Prompt) > 0 {
		return agentic.StdinPayload{Attached: true, Bytes: append([]byte(nil), req.Prompt...)}, nil
	}
	return agentic.StdinPayload{}, nil
}

// ValidateComposition refuses a composition whose prefix does not match the
// grammar this system declares.
func (*System) ValidateComposition(c agentic.Composition) error {
	if c.IsZero() {
		return nil
	}
	return validateComposition(c)
}
