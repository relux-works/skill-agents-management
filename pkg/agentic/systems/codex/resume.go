package codex

import (
	_ "embed"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The pinned 0.159.0 resume-subcommand help grammar is parsing data, not a
// second argv builder. Keep it in an embedded table so the module's Go
// argv-site guard does not mistake a parsing catalog for a second
// constructor: the resume inventory spells signature literals such as
// --ask-for-approval that only args.go may construct. Native comparison
// tests verify every documented row against pinned `codex resume --help`.
//
//go:embed codex-0.159.0-resume-options.tsv
var codexResumeOptionTable string

var codexResumeScalars, codexResumeBooleans = loadCodexResumeOptions()

func loadCodexResumeOptions() (map[string]bool, map[string]bool) {
	scalars, booleans := make(map[string]bool), make(map[string]bool)
	for _, line := range strings.Split(codexResumeOptionTable, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(line)
		if fields[1] == "required" {
			scalars[fields[0]] = true
		} else {
			booleans[fields[0]] = true
		}
	}
	return scalars, booleans
}

func codexResumeOptionTakesValue(name string) bool { return codexResumeScalars[name] }
func codexResumeOptionIsBoolean(name string) bool  { return codexResumeBooleans[name] }

// ElevateResumeIntent classifies root options through the plugin's existing
// scalar/boolean arity grammar, resume-subcommand options through the pinned
// 0.159.0 resume inventory, and recognizes resume only as a subcommand. The
// resume command binds at most [SESSION_ID] [PROMPT] in order; --last takes
// no positional slot and a further positional refuses.
func (*System) ElevateResumeIntent(wrapper, native []string) (agentic.ResumeElevation, error) {
	return elevateResume(wrapper, native)
}

func elevateResume(wrapper, native []string) (agentic.ResumeElevation, error) {
	var selections []agentic.ResumeIntent
	tail := make([]string, 0, len(native))
	positional := false
	command := false
	separated := false
	waiting := false
	latest := false
	promptBound := false
	for i := 0; i < len(native); i++ {
		token := native[i]
		if !separated && token == "--" {
			separated = true
			tail = append(tail, token)
			continue
		}
		if separated {
			if !command {
				tail = append(tail, token)
				continue
			}
			if waiting || latest {
				identity := token
				selections = append(selections, agentic.ResumeIntent{Kind: agentic.ResumeCodexThread, Identity: &identity})
				waiting = false
				latest = false
				continue
			}
			if !promptBound {
				promptBound = true
				tail = append(tail, token)
				continue
			}
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "surplus positional"}
		}
		name, value, attached := strings.Cut(token, "=")
		if len(token) > 2 && !strings.HasPrefix(token, "--") {
			shortTakesValue := codexRootOptionTakesValue(token[:2])
			if command {
				shortTakesValue = codexResumeOptionTakesValue(token[:2])
			}
			if shortTakesValue {
				name = token[:2]
				attached = true
			}
		}
		if name == "--resume" {
			if !attached {
				if i+1 >= len(native) || strings.HasPrefix(native[i+1], "-") {
					return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "picker selector"}
				}
				i++
				value = native[i]
			}
			selections = append(selections, agentic.ResumeIntent{Kind: agentic.ResumeCodexThread, Identity: &value})
			continue
		}
		if !positional && token == "resume" {
			command = true
			waiting = true
			positional = true
			continue
		}
		if command && token == "--last" {
			selections = append(selections, agentic.ResumeIntent{Kind: agentic.ResumeLatest})
			waiting = false
			latest = true
			continue
		}
		if command && (waiting || latest) && !strings.HasPrefix(token, "-") {
			identity := token
			selections = append(selections, agentic.ResumeIntent{Kind: agentic.ResumeCodexThread, Identity: &identity})
			waiting = false
			latest = false
			continue
		}
		tail = append(tail, token)
		takesValue := codexRootOptionTakesValue(name)
		knownBoolean := codexRootOptionIsBoolean(name)
		if command {
			takesValue = codexResumeOptionTakesValue(name)
			knownBoolean = codexResumeOptionIsBoolean(name)
		}
		if takesValue {
			if !attached && i+1 < len(native) {
				i++
				tail = append(tail, native[i])
			}
		} else if strings.HasPrefix(token, "-") && !knownBoolean {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "unknown option arity"}
		} else if attached && knownBoolean {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "attached value on boolean option"}
		} else if !strings.HasPrefix(token, "-") {
			if command {
				if promptBound {
					return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "surplus positional"}
				}
				promptBound = true
			} else {
				positional = true
			}
		}
	}
	if waiting {
		return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "picker selector"}
	}

	return agentic.ResolveResumeSelectors(wrapper, selections, tail, agentic.ResumeCodexThread)
}
