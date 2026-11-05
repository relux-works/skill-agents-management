// Package agy is the agentic-system plugin for the Antigravity CLI.
//
// It is a port of the agy half of skill-project-management's spawn adapter
// table (tools/board-cli/internal/spawn), proven against the launch-surface
// goldens that repository's own harness captured at commit
// ed4878123061b39fdae67160f6b5632117b48a2f. Two goldens cover it — agy/exec and
// agy/dry-run — and parity_test.go drives both through the real
// agentic.Registry and agentic.BuildPlan.
//
// # What is hard about agy, stated where a reader meets it
//
//   - THE BINARY COMES FROM A PREFLIGHT, not from PATH. agy has no PATH
//     fallback at all: the Antigravity probe is the only source of truth for
//     its executable. runtime.go carries the whole boundary — what the
//     preflight does, why it cannot live behind any method here, how its result
//     reaches this plugin, and how the source's two resolution functions map
//     onto one mode-less resolution plus one mode-aware refusal.
//   - THE EFFORT IS IN THE MODEL ID. Antigravity has no effort flag, and the
//     port must not invent one; see the section below.
//   - The assignment is argv TEXT, which is why this is the only ported system
//     with an ARG_MAX budget (args.go).
//   - NO environment filter. env.go says what that leaks and why it is not
//     fixed here.
//
// # Effort: encoded in the model id, and REFUSED as a flag
//
// The source registers agy models as `gemini-3.6-flash-high`,
// `gemini-3.1-pro-low`, `gemini-3.5-flash-medium` and so on — the effort is a
// SUFFIX OF THE MODEL ID, and every one of those rows is Reasoning:
// ReasoningNone (models.go). buildAgyArgs never references cfg.ReasoningEffort.
//
// Two consequences, and this plugin owes both:
//
//  1. This plugin NEVER parses the suffix. Which words a model id may end in,
//     and what they mean, is a vendor-layer fact about a model row; a system
//     plugin that learned the vocabulary would be invariant 4 of
//     docs/architecture.md broken in the least visible way — the day a new
//     suffix is registered, launches would start failing here for a reason
//     nothing in the vendor layer could explain. The id arrives on the request
//     and is passed through verbatim, exactly as every other system's is.
//  2. The declaration is EffortTransportNone, which makes the refusal of a
//     required-effort model DECIDABLE from the contract alone:
//     EffortTransport.CanCarry(EffortSupportRequired) is false, and BuildPlan
//     refuses before dispatching. A launch carrying an explicit effort value is
//     refused for the same reason. That is the vendor-layer gate the goldens
//     cannot show — neither agy fixture configures an effort — and
//     agy_test.go drives it through the real dispatch instead.
//
// # Reshapes from the source, all deliberate
//
//  1. The preflight evidence arrives on the plugin value rather than on the
//     request, because the core request has no field for it and must not grow
//     an agy-shaped one. Named and argued in runtime.go.
//  2. The composition grammar is internal/mcpjson, shared with claude and qwen,
//     because the source validates all three with one function.
package agy

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/mcpjson"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// systemID is this plugin's identity.
//
// It is `antigravity`, the Layer-1 plugin id docs/architecture.md's table
// declares, and NOT `agy`. The two are different names for different things and
// the difference is load-bearing: `agy` is a frozen RUNTIME id — the (agentic
// system × vendor) pair that keys admitted-pair digests and limit-state
// filenames — and it is the spelling the extraction source's AgentType used,
// which is why the goldens record their system as "agy". parity_test.go maps
// between them in one place and says so there.
const systemID agentic.SystemID = "antigravity"

// System is the agy agentic-system plugin.
//
// Unlike every other ported plugin, it HOLDS STATE: the Antigravity preflight's
// result. runtime.go argues that at length. The state is immutable after
// construction, so one value still serves concurrent launches — it serves them
// all the same runtime, which is what a preflight result is.
type System struct {
	runtime Runtime
}

// New returns the plugin with NO preflight evidence.
//
// This is the value the default registry holds, and it is the honest one for a
// process that has not run the probe: it can build a dry run (which reports the
// placeholder binary, exactly as the source's BuildArgs does) and it REFUSES to
// build an exec launch.
func New() *System { return &System{} }

// NewWithRuntime returns the plugin bound to a preflight's result.
//
// It is what a launcher constructs after ProbeAgyRuntime succeeds, and it is
// the only way an exec plan for agy can be built at all.
func NewWithRuntime(runtime Runtime) *System { return &System{runtime: runtime} }

// init registers the plugin into the default registry, which is the
// registration path docs/architecture.md describes and the one the CLI's
// `plugins` command reads.
//
// It registers New() — the value with no evidence — because package
// initialization cannot run a preflight: init has no context, no deadline and
// nowhere to report a probe failure, and a registration that shelled out to
// `agy --version` would make importing this package start processes. A launcher
// that needs an exec plan builds its own registry around NewWithRuntime, which
// is the same shape the claude plugin's goal preparation takes: the launcher
// prepares, then asks for a plan.
//
// A failure PANICS rather than being dropped. Register refuses a duplicate id,
// an unnormalized id, an unstable id and a declaration that could never launch
// — every one of those is a build-time bug in this package, and a plugin that
// silently fails to register is a launch that reports "no system registered"
// with nothing anywhere naming why.
func init() {
	if err := agentic.Register(New()); err != nil {
		panic(fmt.Sprintf("agy: registering the plugin: %v", err))
	}
}

// ID answers the frozen identifier, stably and in its own normalized spelling.
func (*System) ID() agentic.SystemID { return systemID }

// Capabilities is the static declaration.
//
// It does not vary with the preflight evidence, and that is deliberate: what a
// system CAN express is a property of the harness, while whether this process
// has probed it yet is a property of the moment. A Capabilities that shrank
// when the preflight had not run would make a caller's feature check depend on
// timing.
func (*System) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{
		LaunchModes: []agentic.LaunchMode{
			agentic.LaunchModeExec,
			agentic.LaunchModeDryRun,
		},
		// NONE, and the package comment argues it: the effort is a suffix of
		// the model id, buildAgyArgs never references an effort value, and
		// every agy model row is effort-none. Declaring argv here would make
		// BuildPlan admit a required-effort model whose effort then reached the
		// harness nowhere.
		EffortTransport:     agentic.EffortTransportNone,
		SupportsGoal:        false,
		SupportsBudget:      false,
		SupportsServiceTier: false,
		CompositionGrammar:  GrammarMCPConfigJSON,
		// EMPTY, and that is a declaration rather than an omission. The
		// source's providerHomeRules has entries for codex, claude and
		// qwen-codex and none for agy, so no on-disk limit state is keyed by an
		// agy home anywhere in the source. Naming a variable and a directory
		// here would invent the key that state is partitioned by.
		HomeEnvVar:  "",
		DefaultHome: "",
		// Verbatim from the source's single providerAuthHint table
		// (provider_capability.go), which its adapter renders as
		// Message + "; " + Remediation.
		AuthHint: "agy authentication is unavailable; run `agy` interactively and complete the browser sign-in, then retry the spawn",
	}
}

// ResolveBinary returns the executable this launch will run, or the display
// placeholder when no preflight has run.
//
// It takes no mode, which is the core's anti-drift guarantee: with evidence,
// the dry run and the launch report the same target and cannot diverge. Without
// evidence, the placeholder can only ever reach a DRY-RUN plan, because Argv
// refuses to build an exec argv in that state — see runtime.go for why the
// refusal lives there and not here.
func (s *System) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return s.binary(), nil
}

// Argv builds the argument vector for one launch mode, excluding the binary.
//
// It carries TWO refusals a caller must not be able to walk past:
//
//  1. An EXEC launch with no preflight evidence. This is the source's
//     resolveAgyBinary hard failure, relocated to the one surface that knows
//     the mode. Without it, an exec plan would carry the bare `agy`
//     placeholder and a launcher would exec whatever PATH happened to hold —
//     which the source refuses precisely because agy has no PATH fallback.
//  2. An argv that exceeds the conservative ARG_MAX budget. That one is Args'
//     and is argued there.
func (s *System) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if !s.Capabilities().SupportsMode(mode) {
		return nil, fmt.Errorf("agy: launch mode %s is not declared by this system", mode)
	}
	if mode == agentic.LaunchModeExec && s.runtime.IsZero() {
		return nil, notPreflighted()
	}
	return Args(req, mode, s.binary())
}

// binary is the resolution rule, in one place so ResolveBinary and the budget
// check in Argv cannot disagree about which executable a launch will run.
func (s *System) binary() string {
	if s.runtime.IsZero() {
		return displayPlaceholder
	}
	return trimmedExecutable(s.runtime)
}

// ChildEnv is the environment contract, and agy's is the EMPTY one: the child
// inherits the parent unfiltered, with only the run context written over it.
// See env.go for what that leaks and why it is not fixed here.
func (*System) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	return childEnv(parent, req), nil
}

// Stdin attaches NOTHING, for every launch and every mode.
//
// buildAgyCommand never sets cmd.Stdin: the assignment reaches the child as the
// `--print` argument in argv. A child that also received it on standard input
// would be reading it twice from two transports, and agy/exec's golden records
// stdin_kind "none" against exactly this method.
//
// This is a flat answer rather than a conditional on the prompt, and that is
// the point: there is no request shape under which agy takes stdin, so a
// condition here would be a branch nothing could ever reach.
func (*System) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}

// ValidateComposition refuses a composition whose prefix does not match the
// grammar this system declares.
func (*System) ValidateComposition(c agentic.Composition) error {
	if c.IsZero() {
		return nil
	}
	return mcpjson.Validate(c)
}

// GrammarMCPConfigJSON is the launch-composition grammar agy declares: exactly
// one `--mcp-config <json>` pair.
//
// It is internal/mcpjson's identifier, which is the SAME VALUE the claude and
// qwen plugins declare — the source validates all three with one function
// (validateClaudeLaunchCompositionPrefix, reached through one
// CompositionGrammarMCPConfigJSON table entry), and a second copy of those
// rules here would be a shape one system admits and another refuses with
// nothing able to say which is the contract.
const GrammarMCPConfigJSON = mcpjson.Grammar
