// Package agentic defines the agentic-system compatibility kind: the contract
// a harness plugin implements, its compatibility registry, and typed single-
// and multi-node launch plans. The registry publishes system declarations into
// the general pkg/plugin graph; the graph itself assigns no layer number.
//
// An agentic system is the harness that runs a turn — Claude Code, Codex,
// Qwen Code, Gemini CLI, Antigravity, Muse. It owns the binary, the argv
// grammar per launch mode, the environment contract, the stdin protocol, the
// launch composition, the default configuration home and the authentication
// hint. Everything a concrete harness knows lives in its plugin; this package
// knows only the shape of the declaration and how to dispatch through it.
//
// Nothing about a vendor — models, effort vocabularies, authentication, quota
// — lives here. The one vendor-shaped fact this package needs is whether a model
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
	"context"
	"fmt"
	"strings"
	"time"

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
	// LaunchModeInteractive is the terminal session a human drives: a complete
	// argv the launcher hands to a terminal, with no prompt and no result
	// protocol. It is curator-spec Decision 0013 §5, and its grammar is a
	// CLOSED set of constraints rather than a spelling:
	//
	//   - The argv contains ONLY model selection, the system's declared
	//     effort transport for the requested effort, — when the request
	//     carries PermissionMode "yolo" — the ONE provider bypass flag that
	//     system's plugin maps yolo to (curator-spec Decision 0018; the
	//     launcher only resolves and passes the mode, Decision 0013 D5),
	//     and the caller's NativeArgs forwarded verbatim after everything
	//     the module spells. It carries no print or headless mode, no
	//     output-format flag, no OTHER permission-bypass or
	//     unrestricted-mode flag of its own spelling, no goal or
	//     assignment-prompt machinery, no budget flag and no service-tier
	//     flag. What the module may not SPELL is the invariant; the
	//     verbatim suffix is the caller's spelling, not the module's — the
	//     module classifies its flag positions under yolo and claims no
	//     posture over it.
	//   - Composition is NOT part of the interactive argv. The MCP composition
	//     prefix is the composer's plane, and BuildPlan refuses a request
	//     carrying one with ErrCompositionNotInteractive.
	//   - StdinPayload is Attached: false, unless the system's EffortTransport
	//     is EffortTransportStdin — then it is exactly the effort encoding that
	//     system declares and nothing else. BuildPlan holds every plugin to it.
	//   - Home and WorkDir carry as in every other mode. Model is required;
	//     effort follows the model's EffortSupport with no default injected.
	//   - A system that does not declare the mode is refused with
	//     ErrUnsupportedLaunchMode, as for any undeclared mode.
	//
	// It is appended, never inserted: the integer values above are the ones
	// existing declarations and goldens were built against.
	LaunchModeInteractive
)

// Valid reports whether m is one of the declared modes. A mode value outside
// the set is a programming error in a plugin's declaration, not an input to
// interpret.
func (m LaunchMode) Valid() bool {
	switch m {
	case LaunchModeExec, LaunchModeDryRun, LaunchModeManagedSession, LaunchModeInteractive:
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
	case LaunchModeInteractive:
		return "interactive"
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

	// AliasOf is the model identity this id STANDS FOR, empty when the id is
	// already the identity. A launch is admitted, audited and displayed under
	// the requested spelling and is EXECUTED under the identity: BuildPlan
	// substitutes it once, before any plugin surface is dispatched, so no
	// harness is ever handed a name its backend does not answer to.
	//
	// The failure it closes was measured, not imagined. skill-project-management
	// spawned `--model muse-spark --reasoning-effort high`, the alias reached
	// muse's argv verbatim, and the Meta backend refused the run with "model
	// muse-spark does not exist or you lack access" while the same launch under
	// muse-spark-1.3-contributor succeeded end to end.
	//
	// This layer never DERIVES an alias — it holds no catalogue to derive one
	// from. The vendor layer states it (vendorplugin.Model.AliasOf, validated
	// where the rows are registered) and carries it here through Launchable.
	// Empty means "no alias was stated", never "look one up".
	AliasOf string
}

// LaunchIdentity is the id a harness must be handed for this model: the alias
// target when one was declared, and the id itself otherwise.
//
// It is deliberately NOT a fallback chain. One hop is the whole rule, because
// the vendor layer refuses an alias whose target is itself an alias — a chain
// resolved here would be this layer inventing a lookup it has no table for.
func (m Model) LaunchIdentity() string {
	if identity := strings.TrimSpace(m.AliasOf); identity != "" {
		return identity
	}
	return strings.TrimSpace(m.ID)
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

// PermissionMode is the interactive permission posture a launch runs under,
// curator-spec Decision 0018. The launcher resolves the mode and passes it;
// this module owns the per-tool-release provider mapping and the argv grammar
// (Decision 0013 D5 forbids the launcher from spelling a provider flag).
type PermissionMode string

const (
	// PermissionModeNative means pass NOTHING: the provider's stored settings
	// decide. It is the meaning of the zero value too — an empty mode is
	// native, never a lookup — so every request written before this member
	// existed keeps its argv byte for byte.
	PermissionModeNative PermissionMode = "native"
	// PermissionModeYolo means the single provider bypass flag the system
	// maps for its pinned tool release, emitted exactly once on the
	// interactive argv. A system whose pinned release documents no such
	// flag refuses with ErrPermissionModeUnsupported rather than inventing
	// one.
	PermissionModeYolo PermissionMode = "yolo"
)

// IsZero reports whether no permission mode was requested. Absence is native;
// it is not a third posture.
func (m PermissionMode) IsZero() bool { return strings.TrimSpace(string(m)) == "" }

// Resolve maps the requested value onto the effective posture: the zero value
// and "native" mean native, "yolo" means yolo. Anything else is refused with
// ErrPermissionModeUnknown. The match is exact after trimming — a
// security-sensitive gate does not guess what a near-miss meant.
//
// It is the single reader of the value rule: BuildPlan and every plugin call
// it rather than comparing strings, so a third value is one edit here, not
// one per plugin plus the core.
func (m PermissionMode) Resolve() (PermissionMode, error) {
	switch trimmed := strings.TrimSpace(string(m)); trimmed {
	case "", string(PermissionModeNative):
		return PermissionModeNative, nil
	case string(PermissionModeYolo):
		return PermissionModeYolo, nil
	default:
		return "", fmt.Errorf("%w: %q is not a permission mode; want %q or %q",
			ErrPermissionModeUnknown, trimmed, PermissionModeNative, PermissionModeYolo)
	}
}

// PermissionGrammarVersion names the provider-permission grammar a
// capability row was verified against, curator-spec Decision 0018 choices
// 3 and 6. Each mapping is re-verified per tool release, and the version
// is what tells a re-verified release from a merely observed one: the
// launcher cites the token (in provenance and diagnostics), and a row
// naming a grammar this module does not implement selects no mapping.
type PermissionGrammarVersion string

// PermissionGrammarV1 is the original closed policy grammar: Claude's six
// `--permission-mode` values and Codex's five `-c` keys plus the
// `mcp_servers.` shape. It remains available for capability rows whose
// grammar did not change, such as Pi's unsupported-yolo row.
const PermissionGrammarV1 PermissionGrammarVersion = "permission-grammar-v1"

// PermissionGrammarV2 adds Decision 0018's known yolo-conflicting native
// selectors for Claude and Codex, including aliases and Codex exec placement.
// Capability rows select this token only for releases verified against the
// expanded refusal grammar.
const PermissionGrammarV2 PermissionGrammarVersion = "permission-grammar-v2"

// ReleaseCapability is one row of the versioned provider-capability table:
// what one tool release, verified under one grammar version, admits. The
// table is keyed by (environment, tool release); each plugin holds its
// own environment's rows (there is no central map keyed by system id —
// pkg/agentic's single-source guard forbids a second binding table, and
// the mapping is each plugin's by Decision 0013 D5), and
// LookupReleaseCapability is the single reader over them.
type ReleaseCapability struct {
	// Release is the exact tool release this row verifies, "2.1.261" for
	// one claude row. Matching is exact after trimming: a near-miss is
	// not a neighbouring release, it is an unverified one.
	Release string
	// Grammar is the permission-grammar version the release was verified
	// under. A row naming a grammar this module does not implement is
	// refused as unverified, because its selectors cannot be classified.
	Grammar PermissionGrammarVersion
	// YoloSupported reports whether the release documents a
	// permission-bypass flag for the plugin to map yolo to. False is a
	// verified fact, not an absence: pi 0.84.2's row carries false
	// because that release documents no such flag, and yolo there is
	// refused as unsupported rather than as drift.
	YoloSupported bool
}

// LookupReleaseCapability finds the capability row for one tool release.
//
// An empty release (detection failed or never ran) and a release with no
// row (unpinned or newer than the verification) both refuse with
// ErrPermissionModeUnverifiedRelease: on version drift the yolo mapping
// fails closed first. So does a row naming a grammar version this module
// does not implement. The refusal names the grammar in force and the
// verified releases, so the operator — and the launcher citing the
// token — can see what would have to be re-verified.
func LookupReleaseCapability(rows []ReleaseCapability, release string) (ReleaseCapability, error) {
	trimmed := strings.TrimSpace(release)
	if trimmed == "" {
		return ReleaseCapability{}, fmt.Errorf("%w: no tool release was established (detection failed or never ran); yolo requires a release verified under %s or %s, while native forwards verbatim",
			ErrPermissionModeUnverifiedRelease, PermissionGrammarV1, PermissionGrammarV2)
	}
	for _, row := range rows {
		if strings.TrimSpace(row.Release) != trimmed {
			continue
		}
		if row.Grammar != PermissionGrammarV1 && row.Grammar != PermissionGrammarV2 {
			return ReleaseCapability{}, fmt.Errorf("%w: tool release %q names grammar %q, which this module does not implement (implemented: %s, %s); yolo selects no mapping under an unimplemented grammar",
				ErrPermissionModeUnverifiedRelease, trimmed, row.Grammar, PermissionGrammarV1, PermissionGrammarV2)
		}
		return row, nil
	}
	verified := make([]string, 0, len(rows))
	for _, row := range rows {
		verified = append(verified, strings.TrimSpace(row.Release))
	}
	return ReleaseCapability{}, fmt.Errorf("%w: tool release %q is not verified under %s or %s (verified releases: %s); yolo fails closed on drift, while native forwards verbatim",
		ErrPermissionModeUnverifiedRelease, trimmed, PermissionGrammarV1, PermissionGrammarV2, strings.Join(verified, ", "))
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

	// Deadline is the hard fence the caller will enforce on the child
	// process. A plugin whose harness carries a deadline of its own (pi
	// spells `--deadline`) transports THIS value and never a constant of its
	// own: the 30m the pi plugin used to hard-code cut every local-model run
	// at 30:00 whatever the caller had planned. Zero means the caller declared
	// none, and a plugin then falls back to its documented default.
	Deadline time.Duration

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

	// Vendor is the vendorplugin.VendorID string of the broker the resolved
	// runtime binds to, under the SAME contract as Runtime: an opaque string
	// (no import cycle), populated uniformly by vendorplugin.BuildLaunch after
	// the vendor's Spawn returns and never by a vendor or system plugin. A
	// vendor double that sets it is overwritten. A system-only binding (muse)
	// carries "" here, because its broker is recorded as unresolved and this
	// field never fabricates one.
	//
	// pi-native is the one reader today: Pi's model identity is
	// `<provider>/<model>` and a bare id is ambiguous across providers, so
	// the plugin refuses to build argv without this value rather than emit an
	// identity Pi would resolve to whichever provider it likes.
	Vendor string

	// PermissionMode is the interactive permission posture, curator-spec
	// Decision 0018. The zero value means native (pass nothing); "yolo" maps
	// to the single provider bypass flag the system declares for its pinned
	// tool release. It is valid ONLY for LaunchModeInteractive: any other
	// mode carrying a non-zero value is refused, and so is an unknown value
	// in any mode. The mapping — and only the mapping — is each plugin's.
	PermissionMode PermissionMode

	// ToolRelease is the running tool's release the yolo mapping was verified
	// against, curator-spec Decision 0018 choice 6: "2.1.261" for claude,
	// "0.153.2" for codex, "0.84.2" for pi. The caller establishes it by
	// probing the resolved binary (ProbeToolRelease) before planning and
	// passes the answer here; BuildPlan never starts a process, so a dry run
	// reports the same plan whether the binary exists or not.
	//
	// It is read ONLY on the interactive yolo path, where the plugin looks
	// the release up in its verified rows (ReleaseCapability): an unpinned
	// or newer release, and an empty value (detection failed or never ran),
	// refuse yolo with ErrPermissionModeUnverifiedRelease. Native never
	// reads it — the raw contract is unchanged and no claim is made — and
	// outside interactive launches it is ignored: it is an observation, not
	// launch content, so ignoring it cannot misdirect a launch the way
	// dropping a parameter would.
	ToolRelease string

	// NativeArgs are the caller's own native arguments, forwarded VERBATIM
	// into the interactive argv after everything the module spells (so the
	// yolo bypass flag lands before them, Decision 0018 item 1). They are
	// the launcher's `--`-separated remainder, and this module is their
	// only validator: Decision 0013 D5 forbids the launcher from spelling
	// provider grammar, so no other layer may classify them.
	//
	// Under yolo the plugin scans flag positions (internal/nativeargs owns
	// the prompt-text-versus-flag rule) against the closed grammar of the
	// looked-up tool release: an unknown policy form — a new codex `-c`
	// key, a new claude `--permission-mode` value — is refused with
	// ErrNativePolicyUnknown, which the caller maps to usage (exit 2),
	// never resolved into a policy claim (Decision 0018 choice 3). Under
	// native they are forwarded with no inspection at all: raw bypass may
	// remain available untracked, and the interface stays UX rather than a
	// perimeter (Decision 0018 item 4). Any non-empty value outside
	// LaunchModeInteractive is refused with ErrNativeArgsNotInteractive:
	// no other grammar has a verbatim suffix to carry it, and dropping
	// caller arguments silently is a launch that looks like the one that
	// was asked for and is not.
	NativeArgs []string
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

// EffortAdmitter is implemented by a system whose HARNESS runs, for a given
// model, only a subset of the effort words the vendor row declares — and would
// otherwise drop or rewrite the rest silently. The row's vocabulary remains
// the model's contract for every other harness; this is the harness saying
// which of those words it runs AS REQUESTED, so a plan never promises an
// effort the session will not have.
//
// AdmitEffort is called by vendorplugin.BuildLaunch after the vocabulary gate,
// with the runtime's vendor, the launch identity Pi (or any harness) will be
// handed, the resolved effort (possibly empty for an effort-none row) and the
// row's vocabulary. It returns the vocabulary words the harness runs for that
// model and a non-nil error when the requested word is not one of them. It
// must never rewrite the word: the caller refuses on error.
type EffortAdmitter interface {
	AdmitEffort(vendor, launchIdentity, effort string, vocabulary []string) (accepted []string, err error)
}

// LaunchRequestPreparer is an optional, pure pre-plan gate for a system that
// must validate or snapshot request-owned input before any caller performs an
// observation or preflight. Implementations may read request-named files, but
// must not inspect ambient state, start a process, contact a service, or mutate
// external state. BuildPlan calls the same gate, so direct Layer-1 consumers
// cannot bypass it.
//
// LaunchRequestPreparation is deliberately limited to the two prompt fields a
// preparer may replace. It cannot redirect system, model, profile, runtime,
// environment, run identity, composition, or any other authoritative input.
type LaunchRequestPreparation struct {
	PromptPath string
	Prompt     []byte
}

// Returning a LaunchRequestPreparation lets a system replace a path-backed
// prompt with a copied value. That keeps an earlier production-entry gate and
// the later plan on the same bytes instead of reading a mutable file twice.
type LaunchRequestPreparer interface {
	PrepareLaunchRequest(LaunchRequest, LaunchMode) (LaunchRequestPreparation, error)
}

// ToolReleaseProber is implemented by a system whose running tool release
// can be established by probing the binary the system resolves: the plugin
// runs `<binary> --version` against the launch environment and parses its
// own release out of the output. It is a PRE-plan step — the launcher's
// (follow-up F-L1) — never part of BuildPlan, which starts no process.
//
// A system whose binary cannot attest its tool release does not implement
// it: pi's binary is the agents-infra wrapper, and the wrapper's version
// is not pi's release, so pi has no probe and yolo there fails closed as
// unverified unless the caller established the release another way.
type ToolReleaseProber interface {
	ProbeToolRelease(ctx context.Context, env []string) (string, error)
}

// ProbeToolRelease establishes the running tool release for one system,
// for the caller to pass on LaunchRequest.ToolRelease.
//
// Every failure — no probe, an unresolvable binary, a non-zero exit, an
// unparsable answer, a fired context — is ErrToolReleaseUndetected, and
// the contract on it is total: the caller passes ToolRelease "" and
// plans anyway. Yolo then fails closed in LookupReleaseCapability and
// native forwards verbatim, so a detection failure is a refusal for the
// posture that resolves policy and a pass for the one that claims
// nothing. A probe error must never be answered by synthesizing the
// release the table wants to see.
func ProbeToolRelease(ctx context.Context, sys System, env []string) (string, error) {
	if sys == nil {
		return "", fmt.Errorf("%w: no system to probe; pass ToolRelease \"\" and plan anyway", ErrToolReleaseUndetected)
	}
	prober, ok := sys.(ToolReleaseProber)
	if !ok {
		return "", fmt.Errorf("%w: system %s establishes no tool release by probing; pass ToolRelease \"\" and plan anyway",
			ErrToolReleaseUndetected, sys.ID())
	}
	return prober.ProbeToolRelease(ctx, env)
}

// PrepareLaunchRequest applies a system's optional pure pre-plan gate. Systems
// without one preserve the request byte-for-byte.
func PrepareLaunchRequest(system System, req LaunchRequest, mode LaunchMode) (LaunchRequest, error) {
	preparer, ok := system.(LaunchRequestPreparer)
	if !ok {
		return req, nil
	}
	preparation, err := preparer.PrepareLaunchRequest(req, mode)
	if err != nil {
		return LaunchRequest{}, err
	}
	req.PromptPath = preparation.PromptPath
	req.Prompt = append([]byte(nil), preparation.Prompt...)
	return req, nil
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
