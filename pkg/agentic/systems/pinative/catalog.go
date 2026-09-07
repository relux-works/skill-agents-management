package pinative

import (
	"errors"
	"fmt"
	"strings"
)

// THIS FILE IS THE ONE PLACE THE INSTALLED PI THINKING CONTRACT IS SPELLED.
//
// A vendor row's effort vocabulary is the MODEL's contract and is transported
// to every harness verbatim (invariant 4). Native Pi, however, does not run a
// word it does not accept: it drops or rewrites it and starts the session at
// another level, and the plan that promised `--thinking ultra` is then a lie.
// Two mechanisms in Pi 0.84.2 do that, both read from the installed bytes
// (TASK-260908-2kapmh review rev1, finding R1):
//
//   - dist/cli/args.js:6 `VALID_THINKING_LEVELS`; args.js:97-108 pushes a
//     WARNING for any other word, leaves the level unset, and the session runs
//     at Pi's settings default. `ultra` on gpt-5.6-sol/terra takes this path.
//   - pi-ai/dist/models.js:548-575 `getSupportedThinkingLevels` and
//     `clampThinkingLevel`: a parser-valid word the catalog model maps to
//     `null` in `thinkingLevelMap` is SILENTLY substituted by the nearest
//     supported level at session creation. `minimal` on gpt-5.3-codex and
//     gpt-5.2 is mapped to null and clamps to `low`.
//
// The module's answer is an explicit REFUSAL, reached through
// vendorplugin.BuildLaunch (agentic.EffortAdmitter) and again inside Args so a
// caller holding the plugin directly cannot emit the misleading argv either.
// No clamp, no translation, no default: the rows keep their global vocabulary
// (Codex still drives `ultra` and `minimal`), and the operator is told the
// words native Pi does run for that model and the row's recommendation.
//
// TestPiNativeThinkingRestrictionsMatchTheInstalledCatalog (pkg/vendorplugin)
// re-derives both tables below from the installed Pi bytes in BOTH directions:
// a word this file refuses that Pi supports, or a word Pi drops that this file
// admits, fails the test. Stated bound: effort-none rows emit no --thinking at
// all, and Pi then applies its OWN settings default on a reasoning-capable
// model; the module injects nothing and does not know that default.

// ParserThinkingLevels is Pi 0.84.2 `VALID_THINKING_LEVELS` (dist/cli/args.js:6),
// the words the `--thinking` flag parses at all. Any other word is warned about
// and dropped before a session starts.
var ParserThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// catalogNullThinking is, per provider-qualified model, the parser-valid words
// the installed catalog maps to null (pi-ai providers/data/<vendor>.json
// `thinkingLevelMap`), which `clampThinkingLevel` rewrites without a warning.
// Keyed by the LAUNCH identity Pi is handed. Only words a registry row could
// request are listed; a word outside every row's vocabulary is never reached.
var catalogNullThinking = map[string][]string{
	"openai/gpt-5.3-codex": {"minimal"},
	"openai/gpt-5.2":       {"minimal"},
}

// ErrEffortNotNativelySupported is returned when the installed Pi would not
// run the requested effort word for the model as requested: either the parser
// drops it or the catalog clamps it. It is a refusal, never a rewrite.
var ErrEffortNotNativelySupported = errors.New("pinative: installed Pi does not run the requested thinking level for this model as requested")

// NativeThinkingLevels returns, in vocabulary order, the words of a row's
// vocabulary that the installed Pi runs as requested for the qualified model.
func NativeThinkingLevels(vendor, launchIdentity string, vocabulary []string) []string {
	var accepted []string
	for _, word := range vocabulary {
		if nativeAccepts(vendor, launchIdentity, word) {
			accepted = append(accepted, word)
		}
	}
	return accepted
}

func nativeAccepts(vendor, launchIdentity, effort string) bool {
	parses := false
	for _, level := range ParserThinkingLevels {
		if level == effort {
			parses = true
		}
	}
	if !parses {
		return false
	}
	for _, word := range catalogNullThinking[vendor+"/"+launchIdentity] {
		if word == effort {
			return false
		}
	}
	return true
}

// AdmitEffort implements agentic.EffortAdmitter. An empty effort is admitted:
// the row is effort-none and no --thinking is emitted (the stated bound
// above). Otherwise the word must be one native Pi runs for the model.
func (*System) AdmitEffort(vendor, launchIdentity, effort string, vocabulary []string) ([]string, error) {
	return checkNativeEffort(vendor, launchIdentity, effort, vocabulary)
}

func checkNativeEffort(vendor, launchIdentity, effort string, vocabulary []string) ([]string, error) {
	effort = strings.TrimSpace(effort)
	accepted := NativeThinkingLevels(vendor, launchIdentity, vocabulary)
	if effort == "" || nativeAccepts(vendor, launchIdentity, effort) {
		return accepted, nil
	}
	return accepted, fmt.Errorf("%w: model %s/%s with --thinking %q (Pi 0.84.2 %s); native Pi runs %v for this model",
		ErrEffortNotNativelySupported, vendor, launchIdentity, effort, nativeDropReason(vendor, launchIdentity, effort), accepted)
}

func nativeDropReason(vendor, launchIdentity, effort string) string {
	for _, level := range ParserThinkingLevels {
		if level == effort {
			return "catalog thinkingLevelMap maps it to null and clampThinkingLevel would silently substitute another level"
		}
	}
	return "args.js warns and drops it, and the session would start at Pi's settings default"
}
