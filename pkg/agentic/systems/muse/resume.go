package muse

import (
	_ "embed"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Muse's interactive root options own one value or none. Unknown separated
// options are ambiguous, so this hosted selector API refuses their arity.
// The pinned 1.4.1 help grammar is parsing data, not a second argv builder.
// The resume command admits exactly resume | resume --last |
// resume <session-ref>: root options may appear on either side of resume,
// and any second positional refuses.
//
//go:embed muse-1.4.1-resume-options.tsv
var museResumeOptionTable string

var museResumeScalars, museResumeBooleans = loadMuseResumeOptions()

func loadMuseResumeOptions() (map[string]bool, map[string]bool) {
	scalars, booleans := make(map[string]bool), make(map[string]bool)
	for _, line := range strings.Split(museResumeOptionTable, "\n") {
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

func (*System) ElevateResumeIntent(wrapper, native []string) (agentic.ResumeElevation, error) {
	var selections []agentic.ResumeIntent
	tail := make([]string, 0, len(native))
	positional := false
	command := false
	separated := false
	waiting := false
	latest := false
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
				selections = append(selections, agentic.ResumeIntent{Kind: agentic.ResumeMuseSession, Identity: &identity})
				waiting = false
				latest = false
				continue
			}
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "surplus positional"}
		}
		name, _, attached := strings.Cut(token, "=")

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
		if (waiting || latest) && !strings.HasPrefix(token, "-") {
			identity := token
			selections = append(selections, agentic.ResumeIntent{Kind: agentic.ResumeMuseSession, Identity: &identity})
			waiting = false
			latest = false
			continue
		}
		tail = append(tail, token)
		if name == "--worktree" || name == "-w" {
			if !attached && i+1 < len(native) && (native[i+1] == "off" || native[i+1] == "create" || native[i+1] == "existing") {
				i++
				tail = append(tail, native[i])
			}
		} else if museResumeScalars[name] {
			if !attached && i+1 < len(native) {
				i++
				tail = append(tail, native[i])
			}
		} else if strings.HasPrefix(token, "-") && !museResumeBooleans[name] {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "unknown option arity"}
		} else if attached && museResumeBooleans[name] {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "attached value on boolean option"}
		} else if !strings.HasPrefix(token, "-") {
			if command {
				return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "surplus positional"}
			}
			positional = true
		}
	}
	if waiting {
		return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "picker selector"}
	}

	return agentic.ResolveResumeSelectors(wrapper, selections, tail, agentic.ResumeMuseSession)
}
