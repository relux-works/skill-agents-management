// Package agentic is Layer 1 of agents-management: the contract an agentic
// system plugin implements, and the registry that is the only place a system
// binding may live.
//
// An agentic system is the harness that runs a turn — Claude Code, Codex,
// Qwen Code, Gemini CLI, Antigravity, Muse. It owns the binary, the argv
// grammar per launch mode, the environment contract, the stdin protocol, the
// launch composition, the default configuration home and the authentication
// hint. Everything a concrete harness knows lives in its plugin; this package
// knows only the shape of the declaration and how to dispatch through it.
//
// Nothing about a vendor — models, effort vocabularies, authentication, quota
// — lives here. The one vendor-shaped fact this layer needs is whether a model
// requires a reasoning effort at all (EffortSupport), because a system whose
// declared transport cannot carry effort can never launch such a model, and
// that has to be decidable by a caller holding only these types.
//
// # Provenance
//
// The shape mirrors the adapter table proven in skill-project-management
// (internal/spawn/adapter.go). Deliberate reshapes are named on the
// declarations they affect rather than left for a reader to discover.
package agentic

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/ident"
)

// SystemID is the stable identifier of an agentic system plugin, normalized
// to one spelling. Downstream state — admitted-pair digests, limit-state
// filenames, free-text records — is keyed by these, so two spellings of one
// system silently orphan state. NormalizeSystemID is the single normalization
// and every entry point into this package runs identifiers through it.
type SystemID string

// String renders the identifier for error text and CLI output.
func (id SystemID) String() string { return string(id) }

// InvalidSystemIDError says why an identifier was refused, naming the raw
// spelling and the rule it broke. The refusal text has to be enough for an
// agent to fix the declaration from the error alone.
type InvalidSystemIDError struct {
	Raw    string
	Reason string
}

func (e *InvalidSystemIDError) Error() string {
	return fmt.Sprintf("invalid agentic system id %q: %s; %s, for example %q", e.Raw, e.Reason, ident.Rule, "claude-code")
}

// NormalizeSystemID is the single normalization for agentic system
// identifiers: surrounding whitespace is dropped and ASCII letters are
// lowercased, then the result must be one or more segments of [a-z0-9]
// joined by single hyphens.
//
// Normalizing before validating is what makes "Codex" and "codex" the same
// registration rather than two — the registry reports the second as a
// duplicate instead of admitting a shadow binding under a different spelling.
//
// The fold itself lives in internal/ident, shared with the vendor layer's own
// identifier kinds. Invariant 5 names "one normalization for identifiers", and
// a second copy of this loop one package over is that invariant broken in the
// least visible way available: two charsets that agree until the day one of
// them is widened.
func NormalizeSystemID(raw string) (SystemID, error) {
	normalized, reason, ok := ident.Normalize(raw)
	if !ok {
		return "", &InvalidSystemIDError{Raw: raw, Reason: reason}
	}
	return SystemID(normalized), nil
}

// LaunchMode selects which argv grammar a system builds. A harness does not
// have one argv shape: the extraction source's codex adapter already had two
// (a one-shot `codex exec` process the launcher owns end to end, and a
// provider-args fragment handed to an external session composer), plus a
// third dry-run mirror that had to reproduce the exec grammar exactly.
//
// Reshape from the source adapter: DryRunArgs was a separate function value
// whose whole contract was "the same grammar as BuildCommand, without side
// effects". Making it a mode instead of a sibling method removes the seam the
// two could drift across — the drift the source paid for when BuildArgs
// hardcoded a display binary the real launch never used.
type LaunchMode int

const (
	// LaunchModeExec is the one-shot child process this tool owns end to end.
	LaunchModeExec LaunchMode = iota
	// LaunchModeDryRun is the side-effect-free mirror of LaunchModeExec: the
	// same argv grammar, with readable placeholders wherever a real launch
	// would read a file that does not exist yet. It must never resolve
	// anything a real launch would resolve by performing work — no preflight,
	// no process start, no file creation.
	LaunchModeDryRun
	// LaunchModeManagedSession is the provider-args fragment fed to an
	// external composer that owns the PTY, rather than a complete argv this
	// tool executes.
	LaunchModeManagedSession
)

// Valid reports whether m is one of the declared modes. A mode value outside
// the set is a programming error in a plugin's declaration, not an input to
// interpret.
func (m LaunchMode) Valid() bool {
	switch m {
	case LaunchModeExec, LaunchModeDryRun, LaunchModeManagedSession:
		return true
	default:
		return false
	}
}

func (m LaunchMode) String() string {
	switch m {
	case LaunchModeExec:
		return "exec"
	case LaunchModeDryRun:
		return "dry-run"
	case LaunchModeManagedSession:
		return "managed-session"
	default:
		return fmt.Sprintf("launch-mode(%d)", int(m))
	}
}

// EffortTransport names how a system carries an explicit reasoning-effort
// value to its harness. This is TRANSPORT, not vocabulary: which effort words
// a model accepts is a per-model fact owned by the vendor layer, and no plugin
// at this layer may enumerate them.
type EffortTransport int

const (
	// EffortTransportNone means the system has no mechanism to carry an
	// effort value at all. It is the zero value deliberately: a system that
	// has not declared a transport carries nothing, and BuildPlan refuses the
	// launch rather than dropping the operator's configured effort silently —
	// the wrong-cost launch the source repo's EffortTransport exists to close.
	EffortTransportNone EffortTransport = iota
	// EffortTransportArgv means the system appends an argv flag or config
	// override carrying the value.
	EffortTransportArgv
	// EffortTransportStdin means the system encodes the value into a field of
	// its stdin payload.
	EffortTransportStdin
)

// Valid reports whether t is one of the declared transports.
func (t EffortTransport) Valid() bool {
	switch t {
	case EffortTransportNone, EffortTransportArgv, EffortTransportStdin:
		return true
	default:
		return false
	}
}

func (t EffortTransport) String() string {
	switch t {
	case EffortTransportArgv:
		return "argv"
	case EffortTransportStdin:
		return "stdin"
	case EffortTransportNone:
		return "none"
	default:
		return fmt.Sprintf("effort-transport(%d)", int(t))
	}
}

// EffortSupport is the per-model effort axis, reduced to the one bit this
// layer needs. The vendor layer owns the model row, the vocabulary and the
// recommended value; a system plugin never sees any of them.
type EffortSupport int

const (
	// EffortSupportNone means the model has no reasoning-effort axis.
	EffortSupportNone EffortSupport = iota
	// EffortSupportRequired means an explicit effort must be chosen for this
	// model. There is no third "optional" state on purpose: the extraction
	// source established effort as required-or-absent precisely so that no
	// default is ever injected anywhere.
	EffortSupportRequired
)

func (s EffortSupport) String() string {
	switch s {
	case EffortSupportRequired:
		return "required"
	case EffortSupportNone:
		return "none"
	default:
		return fmt.Sprintf("effort-support(%d)", int(s))
	}
}

// CanCarry reports whether a system declaring this transport can carry a
// model with the given effort requirement.
//
// This is the whole of what AC4 asks the contract to expose: a caller holding
// nothing but these two types can decide that a required-effort model under
// EffortTransportNone is unlaunchable, without a registry, a vendor plugin or
// an admission pass. BuildPlan is the production caller; the decision is
// available to any other caller that only has the contract.
func (t EffortTransport) CanCarry(s EffortSupport) bool {
	if s == EffortSupportRequired {
		return t == EffortTransportArgv || t == EffortTransportStdin
	}
	return true
}

// GrammarID names the argv shape a system's launch-composition prefix takes,
// so callers can group systems that share one and so an operator can see which
// grammar a refusal was measured against.
//
// Reshape from the source adapter: CompositionGrammar was a core enum, which
// meant a new grammar was a core edit. Here it is an opaque identifier the
// plugin declares and the plugin validates (System.ValidateComposition), so a
// harness with a novel composition shape is a plugin, not a core change.
type GrammarID string

// GrammarNone is the declaration of a system that supports no launch
// composition at all. A system declaring it must refuse every non-zero
// Composition; BuildPlan refuses before dispatching, so the plugin's own
// refusal is a second line rather than the only one.
const GrammarNone GrammarID = ""

// CompositionServer is one MCP server named by a launch composition.
type CompositionServer struct {
	Name      string
	Transport string
	// BearerTokenEnvVar is the environment variable the server's bearer token
	// is read from, empty when it carries none.
	//
	// Reshape from the source (named by TASK-260822-hp5fb4): the source's
	// validators check the composed prefix's bearer reference AGAINST this
	// metadata — a prefix naming a different variable than the server declared
	// is refused, and an http server whose bearer metadata and prefix disagree
	// on presence is refused too. Without the field on this type a port can
	// only drop those two checks, which is a validating gate quietly getting
	// weaker across a move.
	BearerTokenEnvVar string
}

// Composition is the validated provider argv prefix that composes MCP servers
// into a launch, together with the servers it claims to carry. The prefix is
// already composed by the caller; a system validates that its shape matches
// the grammar the system declares.
type Composition struct {
	Prefix  []string
	Servers []CompositionServer
}

// IsZero reports whether no composition was requested. Absence and an empty
// composition are the same fact here — a prefix of no arguments carrying no
// servers changes nothing about the launch — but a prefix without servers, or
// servers without a prefix, is neither, and a plugin's validator sees it.
func (c Composition) IsZero() bool {
	return len(c.Prefix) == 0 && len(c.Servers) == 0
}

// StdinPayload is what the child receives on standard input.
//
// Attached distinguishes "this launch attaches no stdin" from "this launch
// attaches an empty stream": a harness that reads its prompt from stdin sees
// EOF on an empty attached stream and a closed descriptor on no stdin at all,
// and those produce different child behaviour. An absence and an empty read
// are different facts, and a single nil slice cannot say which one happened.
type StdinPayload struct {
	Attached bool
	Bytes    []byte
}

// Model is the vendor-layer fact this layer needs, and nothing more: which
// model was selected, and whether it requires an effort. The vocabulary, the
// ranking, the pricing and the availability all stay with the vendor plugin.
type Model struct {
	ID     string
	Effort EffortSupport
}

// Goal is the immutable objective snapshot a launch is bound to. Only its
// identity and text matter at this layer; ownership, revision policy and
// routing stay with the caller that produced it.
type Goal struct {
	ID        string
	Objective string
	Revision  int

	// ProviderCondition is the success predicate a goal-aware harness is
	// handed VERBATIM, in whatever form that harness's own goal mechanism
	// takes. Empty means the caller supplied none.
	//
	// Reshape from the source adapter (named by TASK-260822-3u97y3): the
	// source's claude adapter reads goal.ProviderCondition off the board's
	// GoalContract and splices it into argv as `/goal <condition>`
	// (ClaudeGoalDirective, spawn/claude_goal.go). The text is RENDERED by the
	// board layer (pkgboard.RenderGoalProviderCondition) and the board
	// validates a stored contract against a re-render, so the renderer is a
	// single source elsewhere. Without this field a plugin could only invent a
	// second renderer here — two spellings of one predicate, with the board
	// rejecting whichever one it did not produce — or drop the binding
	// entirely and launch a goal-bound child bound to nothing.
	//
	// It is deliberately NOT the Objective. The objective is the operator's
	// intent text; the provider condition is the machine-checkable predicate,
	// and the claude goldens record the second reaching argv while the first
	// reaches nothing.
	ProviderCondition string
}

// Budget is the spend ceiling a launch is bound to, in the unit the harness
// itself accepts. The source adapter carried a USD figure for Claude and
// nothing for anyone else, which is exactly what SupportsBudget gates.
type Budget struct {
	USD float64
}

// LaunchRequest is everything a caller supplies for one launch. It is the
// input to every dispatch surface, so a plugin never reaches for ambient
// state: what is not here is not available to it.
type LaunchRequest struct {
	// System selects the plugin. It is normalized on the way in.
	System SystemID

	// Model is the selected model and its effort requirement. Effort is the
	// chosen effort value, in the vendor's per-model vocabulary; this layer
	// never inspects the word, only whether one is present and whether the
	// system can carry it.
	Model  Model
	Effort string

	// PromptPath is the assignment prompt file on disk, and Prompt is its
	// bytes for a system whose transport is stdin. A system declares which it
	// reads; supplying neither to a system that needs one is the plugin's
	// refusal to make, not this layer's.
	PromptPath string
	Prompt     []byte

	// WorkDir is the child's working directory.
	//
	// Reshape from the source adapter: DefaultHome there was a documentation
	// string recording that every adapter's cmd.Dir resolved to one value.
	// Here the child working directory is an explicit request field and
	// Capabilities.DefaultHome carries the thing the architecture actually
	// names — the harness configuration home.
	WorkDir string

	// Home overrides the harness configuration home for this launch. Empty
	// means the system's declared DefaultHome. On-disk limit state is keyed by
	// (provider, home), so this value is load-bearing beyond the launch.
	Home string

	// Env is the parent environment the child inherits from, before the
	// system's own filtering. It is passed in rather than read from the
	// process so a launch is reproducible and testable without mutating the
	// ambient environment.
	Env []string

	// Run is the caller's tracked-run context: the identifiers the child is
	// told to act under, and the board directory it is routed at.
	//
	// Reshape from the source adapter (named by TASK-260822-hp5fb4): the
	// source read these off *spawn.Config inside withSpawnEnv, which every
	// adapter shared. A plugin here reaches for nothing ambient, so the same
	// facts have to arrive on the request or no plugin can inject them — and
	// the codex goldens record all four of them in env_added and env_removed,
	// so this is not a hypothetical need.
	Run RunContext

	// Profile is the harness-side named configuration profile a launch runs
	// under, empty when the caller selected none. It is a harness fact rather
	// than a vendor one — codex spells it `-p <profile>` in exec mode and
	// `--profile <profile>` in a managed session — and no default is invented
	// for it anywhere.
	Profile string

	Goal        *Goal
	Budget      *Budget
	ServiceTier string
	Composition Composition

	// Runtime is the vendorplugin.RuntimeID string that resolved to this
	// launch. It is opaque to this package (a plain string, exactly like
	// Model.ID) to avoid an import cycle — pkg/vendorplugin already imports
	// pkg/agentic for BuildPlan, so pkg/agentic cannot import pkg/vendorplugin
	// back.
	//
	// It is populated uniformly by vendorplugin.BuildLaunch for every launch,
	// never by an individual system or vendor plugin, so no Vendor.Spawn
	// implementation may set it and expect the value to stick — BuildLaunch
	// overwrites it unconditionally after Spawn returns. A system that does
	// not need it (every one except pi today) simply never reads it, the same
	// pattern Profile already established.
	Runtime string
}

// RunContext is the caller's identity for one tracked run, carried to the
// child as environment.
//
// Every field is OPAQUE to this package: it copies the values it is given and
// never parses, validates or defaults one. The names of the variables they are
// exported under are the caller's convention, spelled once in runcontext.go
// rather than in each of six plugins.
type RunContext struct {
	// RunID and TaskID identify the run and the unit of work it was launched
	// for.
	RunID  string
	TaskID string
	// BoardDir is the task directory the child is routed at. A child that
	// inherited none would resolve one relative to its own working directory,
	// which is the failure the source repository's absolute-path export closed.
	BoardDir string
	// ContextID is the writable context the run may act on. A run that
	// inherited a base context is deliberately not told the base's id: the
	// child is given the one identity it may mutate.
	ContextID string
}

// IsZero reports whether no run context was supplied at all.
func (c RunContext) IsZero() bool {
	return c.RunID == "" && c.TaskID == "" && c.BoardDir == "" && c.ContextID == ""
}

// System is the agentic-system plugin contract. Every capability the
// extraction source's adapter table declared is expressible here; see the
// package documentation for the reshapes.
//
// Every method must be free of side effects other than reading the
// environment it was handed: BuildPlan calls all of them to produce a
// dry-run plan, and a dry run that preflights, writes a file or starts a
// process is the drift the source repo's BuildArgs bug was made of.
type System interface {
	// ID is the plugin's identity. It must be stable forever and must
	// normalize to itself.
	//
	// Registry.Register enforces the part of that a registration-time check
	// can reach, and the boundary is worth reading precisely rather than as a
	// guarantee. It refuses a plugin whose ID() is not already the value
	// NormalizeSystemID returns for it (ErrUnnormalizedSystemID), and it reads
	// ID() twice and refuses a plugin that answers differently across the two
	// reads (ErrUnstableSystemID). What that buys: the registry key IS the
	// spelling ID() returned at registration, so a caller holding the plugin
	// and a caller reading the registry agree for as long as the plugin keeps
	// its word.
	//
	// "Forever" is the plugin's obligation, not the registry's check. A plugin
	// that answers consistently through registration and changes on a later
	// call has broken this contract, and nothing downstream will notice: it
	// would be registered under one name and serve another, which is the
	// two-spellings disease invariant 5 exists to prevent. The earlier
	// wording of this comment claimed that could never happen. It could, on a
	// single read, and saying so is the only version of this sentence the code
	// backs.
	ID() SystemID

	// Capabilities is the static declaration: launch modes, effort transport,
	// goal/budget/service-tier support, composition grammar, default home and
	// auth hint. It must not vary between calls.
	Capabilities() Capabilities

	// ResolveBinary returns the exact executable this launch will run —
	// including a managed-npm path, a native shim or a preflighted
	// executable. Resolution logic is entirely the plugin's; the contract only
	// requires that the dry-run mode report the same target the real launch
	// would use, so no display placeholder can drift from the launch target.
	ResolveBinary(req LaunchRequest) (string, error)

	// Argv builds the argument vector for one launch mode, excluding the
	// binary itself. A mode the system did not declare must be refused.
	Argv(req LaunchRequest, mode LaunchMode) ([]string, error)

	// ChildEnv is the environment contract: what the child must and must not
	// inherit. It receives the parent environment and returns the child's,
	// which is the only shape that expresses both halves — stripping parent
	// runtime state the child must not see, and injecting what it must.
	//
	// Reshape from the source adapter: the source expressed this inside
	// BuildCommand, where it was observable only by launching. Here it is its
	// own surface, so environment parity is assertable without a process.
	ChildEnv(parent []string, req LaunchRequest) ([]string, error)

	// Stdin is the stdin transport: the bytes the child reads, and whether
	// anything is attached at all.
	Stdin(req LaunchRequest) (StdinPayload, error)

	// ValidateComposition refuses a composition whose shape does not match the
	// grammar this system declares. A system declaring GrammarNone must refuse
	// every non-zero composition.
	ValidateComposition(c Composition) error
}

// Capabilities is a system's static declaration. It is a value, not an
// interface: these are facts a plugin states once, and a caller must be able
// to read them without the risk of a method that computes a different answer
// on the second call.
type Capabilities struct {
	// LaunchModes are the modes this system builds argv for. It must be
	// non-empty — a system that declares no mode can never launch, and
	// registering one is a bug the registry reports rather than a
	// configuration to tolerate.
	LaunchModes []LaunchMode

	// EffortTransport is how an explicit reasoning effort reaches the harness.
	EffortTransport EffortTransport

	// SupportsGoal, SupportsBudget and SupportsServiceTier gate the three
	// optional launch parameters. A request carrying one a system does not
	// support is refused, not dropped.
	SupportsGoal        bool
	SupportsBudget      bool
	SupportsServiceTier bool

	// CompositionGrammar names the launch-composition argv shape, or
	// GrammarNone when the system supports none.
	CompositionGrammar GrammarID

	// HomeEnvVar is the environment variable the harness reads to find its
	// configuration home, empty when it has none. DefaultHome is where it
	// looks when that variable is unset. Both are declarations for the
	// caller's benefit — the plugin's ChildEnv is what actually sets the
	// variable — and on-disk limit state is keyed by the resulting home, so
	// neither may move without a demonstrated migration.
	HomeEnvVar  string
	DefaultHome string

	// AuthHint is the operator remediation surfaced when this system's
	// authentication fails. Empty is a legitimate declaration: it means no
	// authentication failure has been captured through this system's path
	// yet, which is different from asserting a remediation that has never
	// been observed to work.
	AuthHint string
}

// SupportsMode reports whether the system declared this launch mode.
func (c Capabilities) SupportsMode(m LaunchMode) bool {
	for _, declared := range c.LaunchModes {
		if declared == m {
			return true
		}
	}
	return false
}
