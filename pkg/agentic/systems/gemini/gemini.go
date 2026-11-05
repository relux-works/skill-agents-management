// Package gemini is the agentic-system plugin for the Gemini CLI.
//
// It is a port of the gemini half of skill-project-management's spawn adapter
// table (tools/board-cli/internal/spawn), proven against the launch-surface
// goldens that repository's own harness captured at commit
// ed4878123061b39fdae67160f6b5632117b48a2f. Two goldens cover it —
// gemini/exec and gemini/dry-run — and parity_test.go drives both through the
// real agentic.Registry and agentic.BuildPlan.
//
// # It is the simplest of the six, and the simplicity is the contract
//
//   - Plain PATH resolution. No managed package, no shim, no preflight.
//   - NO environment filter at all. buildGeminiCommand passes os.Environ()
//     straight to withSpawnEnv. env.go names what that means and what it leaks,
//     because an empty filter is a decision a port must not quietly improve on.
//   - NO effort transport. buildGeminiArgs never references the effort, and
//     every model registered under gemini is effort-none.
//   - NO launch composition. The source's grammar table gives gemini
//     CompositionGrammarNone and its validator answers "unsupported launch
//     composition agent".
//   - NO goal, budget or service tier.
//
// The one thing that is NOT simple is the prompt, and it is worth reading
// before args.go: gemini takes the assignment through `-p`, AND appends
// whatever it reads on stdin to that value. A real launch therefore passes an
// EMPTY `-p` and streams the file, so the assignment is delivered exactly once
// and is not bounded by the platform's argv size limit. A dry run has no file,
// so it substitutes a readable placeholder into `-p` instead — which is why the
// two goldens differ in exactly one argument.
//
// # Reshapes from the source, all deliberate
//
//  1. Binary resolution reads LaunchRequest.Env rather than the process
//     environment, through internal/launchenv (binary.go).
//  2. Nothing else. This plugin declares fewer capabilities than any other
//     ported system, and a port that decorated it would be inventing behaviour
//     no golden covers.
package gemini

import (
	"fmt"
	"os"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// systemID is this plugin's identity.
//
// It is `gemini-cli`, the Layer-1 plugin id docs/architecture.md's table
// declares, and NOT `gemini`. The two are different names for different things
// and the difference is load-bearing: `gemini` is a frozen RUNTIME id — the
// (agentic system × vendor) pair that keys admitted-pair digests and
// limit-state filenames — and it is the spelling the extraction source's
// AgentType used, which is why the goldens record their system as "gemini".
// parity_test.go maps between them in one place and says so there.
const systemID agentic.SystemID = "gemini-cli"

// System is the gemini agentic-system plugin.
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
		panic(fmt.Sprintf("gemini: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration, and almost all of it is a NO.
//
// Each false below is the source's adapter row, not an omission waiting to be
// filled in. EffortTransportNone in particular is load-bearing: it is what
// makes BuildPlan refuse a required-effort model under gemini rather than
// launching it and dropping the configured value on the floor. Declaring argv
// here — gemini does have flags, after all — would make that refusal
// unreachable while nothing in the argv carried the effort.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeExec,
			agentic.LaunchModeDryRun,
		},
		EffortTransport:     agentic.EffortTransportNone,
		SupportsGoal:        false,
		SupportsBudget:      false,
		SupportsServiceTier: false,
		// GrammarNone: the source's adapter row is CompositionGrammarNone, and
		// its validator's default case answers "unsupported launch composition
		// agent" for exactly this system. BuildPlan refuses a non-zero
		// composition before dispatching, and ValidateComposition below refuses
		// again, so the plugin's own refusal is a second line rather than the
		// only one.
		CompositionGrammar: agentic.GrammarNone,
		// EMPTY, and that is a declaration rather than an omission. The
		// source's providerHomeRules (pkg/providerlimits/identity.go) has
		// entries for codex, claude and qwen-codex and NONE for gemini: no
		// on-disk limit state is keyed by a gemini home anywhere in the source.
		// Naming a variable and a directory here would invent the key that
		// state is partitioned by.
		HomeEnvVar:  "",
		DefaultHome: "",
		// Verbatim from the source's single providerAuthHint table
		// (provider_capability.go), which its adapter renders as
		// Message + "; " + Remediation.
		AuthHint: "Gemini authentication is unavailable; set `GEMINI_API_KEY` or run Gemini CLI interactively to sign in, then retry the goal-bound spawn",
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
// this system did not declare is refused here as well as in BuildPlan. Two
// refusals for one fact is deliberate at a plugin boundary: BuildPlan's check
// protects the dispatch, this one protects a caller holding the plugin
// directly.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("gemini: launch mode %s is not declared by this system", mode)
	}
	return Args(req, mode)
}

// ChildEnv is the environment contract, and gemini's is the EMPTY one: the
// child inherits the parent unfiltered, with only the run context written over
// it. See env.go for what that leaks and why it is not fixed here.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin is gemini's prompt transport: the assignment file is streamed on
// standard input, and the `-p` argument is left empty so the harness's own
// concatenation of the two delivers it exactly once (args.go).
//
// PromptPath is read when the caller supplied one, because that is what the
// source does and because a file read is the transport actually exercised in
// production. A read FAILURE is an error, never an empty payload: an absent
// prompt and an unreadable one are different facts, and returning "nothing
// attached" for a permissions error would launch a gemini child with an empty
// `-p` AND an empty stdin — a child with no assignment at all, reported as the
// model producing no work.
//
// Prompt bytes are the alternative for a caller that has the text without a
// file. Supplying NEITHER attaches nothing, which is the state a dry run is in
// — BuildPlan calls this method for every mode, and the source's own
// buildGeminiCommand sets cmd.Stdin only when a prompt file was named.
func (*System) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	if path := strings.TrimSpace(req.PromptPath); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return agentic.StdinPayload{}, fmt.Errorf("gemini: reading the assignment prompt: %w", err)
		}
		return agentic.StdinPayload{Attached: true, Bytes: data}, nil
	}
	if len(req.Prompt) > 0 {
		return agentic.StdinPayload{Attached: true, Bytes: append([]byte(nil), req.Prompt...)}, nil
	}
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
	return fmt.Errorf("gemini: unsupported launch composition; this system declares no composition grammar")
}
