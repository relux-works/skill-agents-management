package claude

import (
	_ "embed"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"strings"
)

// Pinned Claude 2.1.288 --help option arities. Required options own their next
// token even when dash-leading (including the first variadic value). Optional
// values and later variadic values stop at dash-leading tokens. An attached
// long value ends that occurrence. The pinned Commander parseOptions method
// also accepts attached short scalar values and combined short switches.
type optionArity uint8

const (
	optionNone optionArity = iota
	optionRequired
	optionOptional
	optionVariadic
)

// The grammar is data, not an argv constructor. Keep it in an embedded table
// so the module's Go argv-site guard does not mistake a parsing catalog for a
// second constructor in this or another plugin. Native comparison tests verify
// every visible row against pinned help and exercise the native parser itself.
//
//go:embed claude-2.1.288-options.tsv
var claudeOptionTable string

// The eager scanner sets are data for the same reason: the pinned native NI
// skip sets name value-taking options (including --effort and
// --append-system-prompt-file), and Go literals would trip the argv-site guard.
//
//go:embed claude-2.1.288-eager.tsv
var claudeEagerTable string

var claudeOptionArities = loadClaudeOptionArities()

var claudeEagerArity = loadClaudeEagerArity()

func loadClaudeOptionArities() map[string]optionArity {
	arities := make(map[string]optionArity)
	for _, line := range strings.Split(strings.TrimSpace(claudeOptionTable), "\n") {
		fields := strings.Fields(line)
		switch fields[1] {
		case "none":
			arities[fields[0]] = optionNone
		case "required":
			arities[fields[0]] = optionRequired
		case "optional":
			arities[fields[0]] = optionOptional
		case "variadic":
			arities[fields[0]] = optionVariadic
		}
	}
	return arities
}

type claudeOption struct {
	index     int
	name      string
	values    []string
	placement agentic.NativePolicyPlacement
}

func claudeOptions(args []string) []claudeOption {
	var options []claudeOption
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			break
		}
		if len(token) < 2 || token[0] != '-' {
			continue
		}
		name, value, attached := strings.Cut(token, "=")
		if !strings.HasPrefix(token, "--") && len(token) > 2 {
			// Commander decomposes short switches until an option owns the remainder.
			for j := 1; j < len(token); j++ {
				name = "-" + string(token[j])
				arity, known := claudeOptionArities[name]
				if !known {
					break
				}
				if arity == optionNone && j+1 < len(token) {
					options = append(options, claudeOption{index: i, name: name, placement: agentic.NativePolicyPlacementFlag})
				}
				if arity != optionNone {
					if j+1 < len(token) {
						value, attached = token[j+1:], true
					} else {
						attached = false
					}
					break
				}
			}
		}
		option := claudeOption{index: i, name: name, placement: agentic.NativePolicyPlacementFlag}
		arity := claudeOptionArities[name]
		if attached && arity != optionNone {
			option.values = []string{value}
			option.placement = agentic.NativePolicyPlacementEquals
		} else if arity == optionRequired || arity == optionVariadic {
			if i+1 < len(args) {
				i++
				option.values = append(option.values, args[i])
				option.placement = agentic.NativePolicyPlacementSeparateToken
				if arity == optionVariadic {
					for i+1 < len(args) && !claudeFlagElement(args[i+1]) {
						i++
						option.values = append(option.values, args[i])
					}
				}
			}
		} else if arity == optionOptional && i+1 < len(args) && !claudeFlagElement(args[i+1]) {
			i++
			option.values = []string{args[i]}
			option.placement = agentic.NativePolicyPlacementSeparateToken
		}
		options = append(options, option)
	}
	return options
}

func claudeFlagElement(value string) bool { return len(value) > 1 && value[0] == '-' }

// Eager arity classes replicate the pinned native NI skip sets (Htt): scalar
// options own their next token unconditionally, variadic options additionally
// own following non-dash tokens, and optional options own the next token only
// when it is not dash-leading. The table carries exactly the binary's sets,
// including hidden options the help text never lists; the native comparison
// test verifies the transcription against the installed release.
type eagerArity uint8

const (
	eagerNone eagerArity = iota
	eagerScalar
	eagerOptional
	eagerVariadic
)

func loadClaudeEagerArity() map[string]eagerArity {
	arities := make(map[string]eagerArity)
	for _, line := range strings.Split(strings.TrimSpace(claudeEagerTable), "\n") {
		fields := strings.Fields(line)
		switch fields[1] {
		case "scalar":
			arities[fields[0]] = eagerScalar
		case "optional":
			arities[fields[0]] = eagerOptional
		case "variadic":
			arities[fields[0]] = eagerVariadic
		}
	}
	return arities
}

// eagerSkip replicates pinned native Htt(args, n): the index of the last
// token consumed at position n. Unknown tokens (including combined short
// switches, which the eager scan never decomposes) consume nothing.
func eagerSkip(args []string, n int) int {
	if n < len(args) && claudeEagerArity[args[n]] == eagerOptional {
		if n+1 < len(args) && !claudeFlagElement(args[n+1]) {
			return n + 1
		}
		return n
	}
	if n >= len(args) || claudeEagerArity[args[n]] == eagerNone {
		return n
	}
	o := n + 1
	if claudeEagerArity[args[n]] == eagerVariadic {
		for o+1 < len(args) && !claudeFlagElement(args[o+1]) {
			o++
		}
	}
	return o
}

// eagerSettingsValues replicates pinned native l('--settings', args): every
// non-skipped token starting with "--settings=" contributes its suffix, and
// every bare "--settings" with a following token contributes that token
// unconditionally, in order. The scan stops at "--". The effective source is
// the last value; that selection lives with the settings policy.
func eagerSettingsValues(args []string) []string {
	var out []string
	for o := 0; o < len(args); o++ {
		token := args[o]
		if token == "--" {
			break
		}
		if value, found := strings.CutPrefix(token, "--settings="); found {
			out = append(out, value)
			continue
		}
		if token == "--settings" && o+1 < len(args) {
			out = append(out, args[o+1])
			o++
			continue
		}
		o = eagerSkip(args, o)
	}
	return out
}

func claudeFlagIndexes(args []string) []int {
	var indexes []int
	for _, option := range claudeOptions(args) {
		indexes = append(indexes, option.index)
	}
	return indexes
}
