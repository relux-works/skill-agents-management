package agy

import (
	"errors"
	"fmt"
	"strings"
)

// THE ANTIGRAVITY PREFLIGHT, AND THE HALF OF IT THAT IS NOT IN THE PLAN
// SURFACE.
//
// An agy launch has two parts in the extraction source. Only one of them is
// expressible here, and saying which is which is the whole point of this file:
// an absence a reader has to infer is how a ported system quietly loses a gate.
//
// # NOT in the plan surface: the preflight itself
//
// ProbeAgyRuntime (skill-project-management,
// tools/board-cli/internal/spawn/agy_preflight.go) resolves `agy` on PATH and
// then:
//
//  1. runs `agy --version`, parses it, and refuses below
//     AgyMinimumHeadlessVersion = 1.1.12 — the first release whose headless
//     contract these flags belong to;
//  2. runs `agy --help` and refuses when any of the seven required headless
//     flags is missing from the output;
//  3. returns AgyRuntimeEvidence{Executable, Version}, which the caller
//     persists with the tracked run.
//
// Steps 1 and 2 START PROCESSES. The System contract in pkg/agentic states that
// every method must be free of side effects other than reading the environment
// it was handed, and BuildPlan calls ALL of them to produce a dry-run plan — so
// putting the preflight behind ResolveBinary or Argv would make
// LaunchModeDryRun execute the harness twice. The source promises exactly the
// same thing on its own dry-run path and says so where it breaks the symmetry:
// resolveAgyBinaryForDisplay "must never trigger the Antigravity preflight
// (BuildArgs promises no side effects)".
//
// So the preflight belongs to the LAUNCHER, in the position it holds in the
// source's command layer, and this task does not invent a home for it. What
// crosses the boundary is its RESULT.
//
// # In the plan surface: the evidence, and the refusal when there is none
//
// The source reads cfg.AgyRuntime — the evidence, hanging off its Config. This
// layer's LaunchRequest has no field for it and must not grow one: an
// agy-shaped field on the type every system reads would put one harness's
// preflight into the core contract, and five plugins would ignore it.
//
// So the evidence arrives on the PLUGIN VALUE instead, at construction:
//
//	agy.New()                       // no preflight has run
//	agy.NewWithRuntime(agy.Runtime{Executable: evidence.Executable})
//
// That makes this the ONE ported plugin that holds state, and the divergence is
// stated rather than buried: every other System in this module answers purely
// from its LaunchRequest. The state here is immutable after construction and is
// never derived from ambient anything, so one value still serves concurrent
// launches — it simply serves them all the same runtime, which is what a
// preflight result IS.
//
// # The two behaviours the source distinguishes, and how they survive the move
//
// The source has TWO functions over cfg.AgyRuntime:
//
//   - resolveAgyBinary, used by the real launch, HARD-FAILS when the preflight
//     has not run: "agy has no PATH fallback: the Antigravity preflight is the
//     only source of truth for its executable".
//   - resolveAgyBinaryForDisplay, used by the dry run, falls back to the bare
//     "agy" display placeholder instead.
//
// The core's ResolveBinary takes NO MODE — deliberately, because that is what
// guarantees a dry run reports the launch's real target and cannot drift from
// it. So the two source functions map onto one resolution plus one mode-aware
// refusal:
//
//   - ResolveBinary answers the evidence's executable when there is one and the
//     placeholder when there is not. With evidence, both modes agree, which is
//     the anti-drift property intact.
//   - System.Argv REFUSES an exec launch that has no evidence
//     (ErrRuntimeNotPreflighted), because Argv is the one surface that knows
//     the mode. BuildPlan calls it, so an exec plan against the placeholder can
//     never be handed to a caller — which is precisely what resolveAgyBinary's
//     hard failure prevents in the source. A validating gate must not get
//     weaker across a move.
//
// agy/dry-run's golden records the literal "agy" as its binary, because the
// source's capture supplied no AgyRuntime. That fixture is the placeholder
// branch; the exec fixture, whose config carries evidence, is the other.

// Runtime is the part of the Antigravity preflight's result a launch plan
// needs: the exact executable the probe selected.
//
// The source's AgyRuntimeEvidence also carries Version, and its absence here is
// deliberate. The version is evidence the RUN records — it is what a later
// reader consults to explain why a launch was admitted — and nothing in a Plan
// reads it. A field this layer never looks at would make this type read as the
// evidence store it is not, and would invite a caller to treat the plugin as
// the place that remembers what the preflight found.
type Runtime struct {
	// Executable is the exact binary the preflight selected, already absolute.
	// Empty means no preflight has run, which is a different fact from a
	// preflight that found nothing: the latter never produces a Runtime at all,
	// because ProbeAgyRuntime returns an error instead.
	Executable string
}

// IsZero reports whether no preflight evidence was supplied.
func (r Runtime) IsZero() bool { return strings.TrimSpace(r.Executable) == "" }

// ErrRuntimeNotPreflighted is returned when an EXEC launch is built against a
// plugin that carries no preflight evidence.
//
// It is the port of the source's AgyCapabilityError with code
// AgyCapabilityMissingBinary — "agy runtime was not resolved before command
// construction" — narrowed to the one code this layer can produce. The rest of
// that taxonomy (version probe failed, version malformed, version unsupported,
// help probe failed, headless flags incompatible) belongs to the preflight, and
// a plugin that declared those codes without being able to reach them would be
// claiming a classification it never performs.
var ErrRuntimeNotPreflighted = errors.New("agy: runtime was not resolved before command construction")

// notPreflighted renders the refusal with the source's remediation attached.
//
// The hint is the source's, verbatim: an operator who sees this needs to know
// the fix is to retry the spawn so the preflight runs, not to install something
// or to put `agy` on PATH — which is the wrong thing to fix, and the thing a
// bare "binary not found" would send them to do.
func notPreflighted() error {
	return fmt.Errorf("%w; remediation: retry the spawn so the launcher can run the Antigravity preflight", ErrRuntimeNotPreflighted)
}
