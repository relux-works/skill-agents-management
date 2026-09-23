// Package nativeargs owns the prompt-text-versus-flag parsing rule for
// caller native arguments, curator-spec Decision 0018 choice 3.
//
// The rule is three lines: the element `--` ends flag parsing, and
// everything from it on is prompt text; an element before it is a flag
// when it is flag-shaped (more than one byte, dash-leading); anything
// else — positionals, a lone `-`, the empty string — is prompt text.
// Prompt text is NEVER parsed as a flag, in either direction the rule is
// read: flag-looking prompt after `--` is forwarded without
// classification, and the same text in flag position is classified.
//
// Scans must use FlagIndexes, not the predicates directly. The break at
// `--` is the load-bearing half of the rule, and a scan written as
// "classify every flag-shaped element" without it parses prompt text as
// flags the moment a caller separates them — which is the defect this
// package exists to prevent. FlagIndexes is the single choke point, so
// narrowing it the one way (dropping the break) is killed by every
// plugin's prompt-passthrough row.
package nativeargs

import "strings"

// IsSeparator reports whether el ends flag parsing: the argv `--`, and
// only it. A provider honors the same separator (codex's own usage tip
// says to pass a value as one with `--`), so what this rule calls prompt
// text the tool reads as prompt text too.
func IsSeparator(el string) bool { return el == "--" }

// IsFlagElement reports whether el is flag-shaped: more than one byte
// and dash-leading. The separator itself is flag-shaped on purpose: a
// caller must end the scan at it (FlagIndexes does), never classify it.
// A lone `-` is positional — the stdin-marker shape — never a flag, and
// so is anything not dash-leading at all.
func IsFlagElement(el string) bool { return len(el) > 1 && el[0] == '-' }

// FlagIndexes returns the indexes of the elements that are flags under
// the rule: flag-shaped elements before the first `--`. Everything else
// is prompt text and is never returned, however flag-like it reads.
func FlagIndexes(args []string) []int {
	var out []int
	for i, el := range args {
		if IsSeparator(el) {
			return out
		}
		if IsFlagElement(el) {
			out = append(out, i)
		}
	}
	return out
}

// SplitFlagValue splits one flag element into its name and its `=`-form
// value: `--permission-mode=auto` is the same flag as a separate
// `--permission-mode auto`, and a scan that only matched exact elements
// would read the `=` form as prompt text. The cut is at the FIRST `=`:
// a `-c` value may itself carry `=` (`key="a=b"`), and only the flag's
// own separator divides the name from the value.
func SplitFlagValue(el string) (name, value string, hasValue bool) {
	return strings.Cut(el, "=")
}
