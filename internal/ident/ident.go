// Package ident holds the ONE normalization rule for plugin identifiers in
// this module.
//
// Invariant 5 of docs/architecture.md names "one normalization for
// identifiers" alongside one adapter table and one runtime registry. Agentic
// system ids, vendor ids and runtime ids all key downstream state —
// admitted-pair digests, limit-state filenames, free-text records — and all
// three fold the same way. A second implementation of this loop is the
// duplicate-charset failure the extraction source paid for twice, so the loop
// lives here and each layer wraps it in its own typed refusal.
//
// It deliberately does NOT own the error types. A refusal has to name the kind
// of id it refused and an example spelling of that kind, and those are facts
// of the layer, not of the rule.
package ident

import (
	"fmt"
	"strings"
)

// Normalize folds one identifier and reports whether the result is legal.
//
// Surrounding whitespace is dropped and ASCII letters are lowercased, then the
// result must be one or more segments of [a-z0-9] joined by single hyphens.
// Folding BEFORE validating is what makes "Codex" and "codex" the same
// registration rather than two: a registry sees the second as a duplicate
// instead of admitting a shadow binding under a different spelling.
//
// When ok is false, reason states the rule that was broken in words a caller
// can put straight into its own refusal text. When ok is true, reason is empty
// and normalized is the canonical spelling — which is idempotent, so an id
// that crosses the boundary twice keys the same state both times.
func Normalize(raw string) (normalized string, reason string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "it is empty", false
	}
	lowered := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, trimmed)
	for _, segment := range strings.Split(lowered, "-") {
		if segment == "" {
			return "", "it has an empty hyphen-separated segment", false
		}
		for _, r := range segment {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				continue
			}
			return "", fmt.Sprintf("it contains %q, which is not a lowercase ASCII letter, a digit or a hyphen", string(r)), false
		}
	}
	return lowered, "", true
}

// Rule is the shape every identifier must take, phrased for a refusal message.
// A caller appends its own example, because "for example \"claude-code\"" is
// useless advice to someone declaring a vendor.
const Rule = "ids are lowercase ASCII segments of letters and digits joined by single hyphens"
