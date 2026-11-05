// Package qwen is the agentic-system plugin for the Qwen Code CLI.
//
// It is a port of the qwen half of skill-project-management's spawn adapter
// table (tools/board-cli/internal/spawn), proven against the launch-surface
// goldens that repository's own harness captured at commit
// ed4878123061b39fdae67160f6b5632117b48a2f. Two goldens cover it — qwen/exec
// and qwen/dry-run — and parity_test.go drives both through the real
// agentic.Registry and agentic.BuildPlan.
//
// # What is hard about qwen, stated where a reader meets it
//
//   - The ENVIRONMENT FILTER is the thing this plugin exists to get right. It
//     is the codex family PLUS CLAUDECODE, and that last key is a fix the
//     source paid for: before it, a qwen child inherited the parent Claude
//     Code session marker and refused to start. env.go carries the fix and
//     names the leaks the source leaves open.
//   - Effort travels on STDIN, not in argv. Qwen is the only ported system
//     whose reasoning effort is a field of a JSON control frame, so an argv
//     comparison alone would not notice it going missing. stdin.go builds the
//     frames and the goldens compare them byte for byte.
//   - Stdin is a two-line stream-json PROTOCOL, not the prompt. The assignment
//     is the text of the second frame; the first frame initializes the session
//     and carries the effort.
//   - Binary resolution is plain PATH. There is no managed-package path, no npm
//     shim to unwrap and no preflighted runtime, so none of codex's resolution
//     machinery is imported here.
//
// # Reshapes from the source, all deliberate
//
//  1. Binary resolution reads LaunchRequest.Env rather than the process
//     environment, through internal/launchenv (binary.go).
//  2. The codex-family strip is internal/runtimeenv, shared with the codex
//     plugin, because the source shares it too — filterQwenRuntimeEnv is
//     filterCodexRuntimeEnv plus one key, and its comment says the sharing is
//     deliberate (env.go).
//  3. The composition grammar is internal/mcpjson, shared with claude and agy,
//     for the same reason: the source validates all three with one function.
package qwen

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/mcpjson"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// systemID is this plugin's identity.
//
// It is `qwen-code`, the Layer-1 plugin id docs/architecture.md's table
// declares, and NOT `qwen`. The two are different names for different things
// and the difference is load-bearing: `qwen` is a frozen RUNTIME id — the
// (agentic system × vendor) pair that keys admitted-pair digests and
// limit-state filenames — and it is the spelling the extraction source's
// AgentType used, which is why the goldens record their system as "qwen".
// parity_test.go maps between them in one place and says so there.
const systemID agentic.SystemID = "qwen-code"

// System is the qwen agentic-system plugin.
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
		panic(fmt.Sprintf("qwen: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration.
//
// TWO launch modes. The source registers qwen with a BuildCommand and a
// DryRunArgs and nothing else: there is no managed-session args builder for
// qwen anywhere in the adapter table, and declaring LaunchModeManagedSession
// would be a capability claim with no construction behind it — and no golden,
// since the goldens' README records that surface as uncaptured for every
// system.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeExec,
			agentic.LaunchModeDryRun,
		},
		// STDIN, and qwen is the only ported system that declares it. The
		// effort value is a field of the initialize control frame
		// (stdin.go), never an argv flag — buildQwenArgs never references
		// cfg.ReasoningEffort at all. Declaring argv here would make BuildPlan
		// admit a required-effort model whose effort then reached the harness
		// nowhere, which is the silent wrong-cost launch EffortTransport
		// exists to close.
		EffortTransport:     agentic.EffortTransportStdin,
		SupportsGoal:        false,
		SupportsBudget:      false,
		SupportsServiceTier: false,
		CompositionGrammar:  GrammarMCPConfigJSON,
		// EMPTY, and that is a declaration rather than an omission. The
		// source's providerHomeRules (pkg/providerlimits/identity.go) has
		// entries for codex, claude and qwen-codex and NONE for qwen: no
		// on-disk limit state is keyed by a qwen home anywhere in the source.
		// Naming a variable and a directory here would invent the key that
		// state is partitioned by, and a caller would key real files by a path
		// nobody has demonstrated the CLI reads.
		HomeEnvVar:  "",
		DefaultHome: "",
		// EMPTY, verbatim from the source's adapter row. providerAuthHint has
		// no qwen entry and ClassifyProviderCapabilityFailure has no
		// authentication branch for it, so no qwen authentication failure has
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
// this system did not declare is refused here as well as in BuildPlan. Two
// refusals for one fact is deliberate at a plugin boundary: BuildPlan's check
// protects the dispatch, this one protects a caller holding the plugin
// directly.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("qwen: launch mode %s is not declared by this system", mode)
	}
	return Args(req, mode)
}

// ChildEnv is the environment contract: strip the parent codex runtime state
// AND the parent claude session marker, then write the run context.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin is qwen's prompt transport: a two-frame stream-json control protocol
// carrying the session, the effort and the assignment. See stdin.go.
func (*System) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return streamInput(req)
}

// ValidateComposition refuses a composition whose prefix does not match the
// grammar this system declares.
func (*System) ValidateComposition(c agentic.Composition) error {
	if c.IsZero() {
		return nil
	}
	return mcpjson.Validate(c)
}

// GrammarMCPConfigJSON is the launch-composition grammar qwen declares: exactly
// one `--mcp-config <json>` pair.
//
// It is internal/mcpjson's identifier, which is the SAME VALUE the claude and
// agy plugins declare — the source validates all three with one function
// (validateClaudeLaunchCompositionPrefix, reached through one
// CompositionGrammarMCPConfigJSON table entry), and a second copy of those
// rules here would be a shape one system admits and another refuses with
// nothing able to say which is the contract.
const GrammarMCPConfigJSON = mcpjson.Grammar
