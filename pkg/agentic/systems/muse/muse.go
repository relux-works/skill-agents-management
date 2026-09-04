// Package muse is the agentic-system plugin for the Muse CLI.
//
// It is a port of the muse half of skill-project-management's spawn adapter
// table (tools/board-cli/internal/spawn), proven against the launch-surface
// goldens that repository's own harness captured at commit
// ed4878123061b39fdae67160f6b5632117b48a2f. Two goldens cover it — muse/exec
// and muse/dry-run — and parity_test.go drives both through the real
// agentic.Registry and agentic.BuildPlan.
//
// # What is different about muse, stated where a reader meets it
//
//   - The assignment reaches the child as a FILE PATH in argv
//     (`--prompt-file`), not on stdin and not as argv text. muse is the only
//     ported system that attaches NO stdin in exec mode, and muse/exec records
//     stdin_kind "none" for exactly that reason.
//   - Because the path is in argv, the dry run has something to substitute: the
//     source's `<prompt-file>` placeholder. That is the only difference between
//     the two modes' argv (args.go).
//   - NO environment filter, NO composition grammar, NO goal, budget or service
//     tier. Every one of those is the source's adapter row rather than an
//     omission; env.go says what the empty filter leaks.
//   - The effort transport is the ONE row that is no longer the source's. The
//     extraction source registered muse with no effort transport because every
//     muse model row was effort-none; muse-spark-1.3-contributor is not, so
//     this plugin now carries `--reasoning-effort` in argv. Capabilities() and
//     args.go each say why where they say it.
//
// # What this plugin deliberately does NOT know
//
// Muse is the system whose VENDOR the extraction source records as unresolved
// (docs/architecture.md names it: "muse, whose broker the extraction source
// records as UNKNOWN"). That is a Layer-2 question about which broker serves
// the model, it is settled in the vendor layer, and NOTHING about it belongs
// here. This plugin owns the harness surface — the binary, the argv, the
// environment, the stdin — and a launch is expressible through it without any
// opinion about who runs the model. A port that let muse's vendor oddness leak
// into the system plugin would be putting a Layer-2 fact in the one place that
// must not hold one.
//
// # Reshapes from the source, all deliberate
//
//  1. Binary resolution reads LaunchRequest.Env rather than the process
//     environment, through internal/launchenv (binary.go).
//  2. Nothing else.
package muse

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// systemID is this plugin's identity.
//
// Muse is the one system where the Layer-1 plugin id and the frozen RUNTIME id
// are the SAME spelling: docs/architecture.md's plugin table says `muse` and
// the source's AgentType is `muse`, so the goldens record `muse` too. That
// coincidence is worth naming rather than leaving for a reader to infer from
// the absence of a mapping — parity_test.go's goldenSystem is a separate
// constant here as it is everywhere else, because the two facts are still two
// facts and the day one of them moves is the day a shared constant would hide
// it.
const systemID agentic.SystemID = "muse"

// System is the muse agentic-system plugin.
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
		panic(fmt.Sprintf("muse: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration, and like gemini's it is almost all
// NO. Each false is the source's adapter row.
//
// GrammarNone is now the load-bearing one, and the note that used to sit on
// EffortTransportNone moved with the behaviour rather than being deleted:
// declaring no composition grammar is what makes BuildPlan refuse a composed
// launch outright instead of splicing a prefix this system cannot honour.
//
// The effort transport is ARGV as of muse-spark-1.3-contributor, and the bound
// it used to state has inverted rather than disappeared. EffortTransportNone
// was a bound on the FUTURE — "a model row added with a required effort must
// fail to launch here" — written when every muse row was effort-none. That row
// now exists, so declaring None would refuse the runtime's own current model.
// What the argv declaration must not become is a claim that is not carried
// through: `EffortTransport.CanCarry` admits a required-effort model on this
// value alone, so a plugin that declared argv and never emitted the flag would
// launch at the harness default with the operator's configured word reaching
// nothing — the precise wrong-cost launch EffortTransport exists to close.
// TestTheArgvTransportIsActuallyCarried holds the two halves together.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeExec,
			agentic.LaunchModeDryRun,
		},
		EffortTransport:     agentic.EffortTransportArgv,
		SupportsGoal:        false,
		SupportsBudget:      false,
		SupportsServiceTier: false,
		// GrammarNone: the source's adapter row is CompositionGrammarNone, and
		// its validator's default case answers "unsupported launch composition
		// agent" for exactly this system.
		CompositionGrammar: agentic.GrammarNone,
		// EMPTY, and that is a declaration rather than an omission. The
		// source's providerHomeRules has entries for codex, claude and
		// qwen-codex and none for muse, so no on-disk limit state is keyed by a
		// muse home anywhere in the source.
		HomeEnvVar:  "",
		DefaultHome: "",
		// EMPTY, verbatim from the source's adapter row. providerAuthHint has
		// no muse entry and ClassifyProviderCapabilityFailure has no
		// authentication branch for it, so no muse authentication failure has
		// ever been captured through this path. An empty hint means exactly
		// that, which is a different fact from asserting a remediation nobody
		// has seen work.
		AuthHint: "",
	}
}

// ResolveBinary returns the exact executable this launch will run.
//
// It is the same function for every mode, which is the contract's requirement
// that a dry run report the launch's real target. The source's own bug was
// precisely the opposite: its dry-run path reported a hardcoded display string
// while a real launch resolved something else, and nothing compared them until
// a parity capture did.
func (*System) ResolveBinary(req agentic.LaunchRequest) (string, error) {
	return resolveBinary(req.Env)
}

// Argv builds the argument vector for one launch mode, excluding the binary.
//
// The mode is checked against the declaration before Args is called, so a mode
// this system did not declare is refused here as well as in BuildPlan.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("muse: launch mode %s is not declared by this system", mode)
	}
	return Args(req, mode)
}

// ChildEnv is the environment contract, and muse's is the EMPTY one: the child
// inherits the parent unfiltered, with only the run context written over it.
// See env.go for what that leaks and why it is not fixed here.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin attaches NOTHING, for every launch and every mode.
//
// buildMuseCommand never sets cmd.Stdin. The assignment reaches the child as
// the `--prompt-file` PATH in argv, so a muse child that also received the text
// on standard input would be reading it twice from two transports — and
// muse/exec's golden records stdin_kind "none" against exactly this method.
//
// This is a flat answer rather than a conditional on the prompt, and that is
// the point: there is no request shape under which muse takes stdin, so a
// condition here would be a branch nothing could ever reach and a reader would
// have to work out which side was live.
func (*System) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}

// ValidateComposition refuses EVERY composition, because this system declares
// no composition grammar.
//
// It is the source's default case — "unsupported launch composition agent" —
// and it is a refusal a plugin must make even though BuildPlan already refuses
// on the declaration: a caller holding the plugin directly gets the same
// answer, and a future declaration that gained a grammar without gaining a
// validator would otherwise silently admit every shape.
func (*System) ValidateComposition(c agentic.Composition) error {
	if c.IsZero() {
		return nil
	}
	return fmt.Errorf("muse: unsupported launch composition; this system declares no composition grammar")
}
