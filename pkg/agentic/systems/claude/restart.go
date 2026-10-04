package claude

import (
	"errors"
	"slices"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const RestartSchema = "urn:relux:agents-management:claude-restart"
const RestartSchemaVersion = "1.0.0"

// RestartTemplate is the frozen closed 1.0.0 projection. Argv excludes argv[0]
// and all elevated selectors. The slot is before all options and prompt text.
type RestartTemplate struct {
	Schema        string      `json:"schema"`
	SchemaVersion string      `json:"schema_version"`
	Data          RestartData `json:"data"`
}
type RestartData struct {
	Argv         []string     `json:"argv"`
	IdentitySlot IdentitySlot `json:"identity_slot"`
}
type IdentitySlot struct {
	Index int    `json:"index"`
	Flag  string `json:"flag"`
}

var ErrRestartInvalid = errors.New("launch_plan_invalid")

type RestartInvalidError struct{ Reason string }

func (e *RestartInvalidError) Error() string { return "launch_plan_invalid: " + e.Reason }
func (e *RestartInvalidError) Unwrap() error { return ErrRestartInvalid }

// ExportRestartTemplate snapshots the already composed, selector-free argv.
// It never recomposes a process or snapshots env or stdin. Consumers must
// validate credential-free argv before persisting this template.
func (s *System) ExportRestartTemplate(argv []string) (RestartTemplate, error) {
	template := RestartTemplate{Schema: RestartSchema, SchemaVersion: RestartSchemaVersion,
		Data: RestartData{Argv: append([]string{}, argv...), IdentitySlot: IdentitySlot{Index: 0, Flag: resumeFlag}}}
	if err := s.ValidateRestartTransformation(template, argv, nil); err != nil {
		return RestartTemplate{}, err
	}
	return template, nil
}

// ValidateRestartTransformation admits only equal new-process argv or exactly
// the declared flag+UUID insertion. It checks the native boundary again, and
// leaves binary/env/cwd renewal and late artifact verification to the consumer.
func (s *System) ValidateRestartTransformation(template RestartTemplate, argv []string, selectedUUID *string) error {
	if template.Schema != RestartSchema || template.SchemaVersion != RestartSchemaVersion {
		return &RestartInvalidError{Reason: "unsupported restart schema"}
	}
	slot := template.Data.IdentitySlot
	if template.Data.Argv == nil || slot.Flag != resumeFlag || slot.Index < 0 || slot.Index > len(template.Data.Argv) {
		return &RestartInvalidError{Reason: "invalid identity slot"}
	}
	var elevated agentic.ResumeElevation
	if value, err := s.ElevateResumeIntent(nil, template.Data.Argv); err != nil {
		return err
	} else {
		elevated = value
	}
	if elevated.Intent.Kind != agentic.ResumeNew || !slices.Equal(elevated.NativeArgs, template.Data.Argv) {
		return &RestartInvalidError{Reason: "selector left in restart argv"}
	}
	expected := append([]string{}, template.Data.Argv...)
	if selectedUUID != nil {
		if err := agentic.ValidateResumeIntent(agentic.ResumeIntent{Kind: agentic.ResumeClaudeUUID, Identity: selectedUUID}, agentic.ResumeClaudeUUID); err != nil {
			return err
		}
		expected = slices.Insert(expected, slot.Index, slot.Flag, *selectedUUID)
	}
	if !slices.Equal(expected, argv) {
		return &RestartInvalidError{Reason: "not the exact one-insertion transformation"}
	}
	// An index inside an option's owned value or after -- cannot bind identity.
	if selectedUUID != nil {
		bound := false
		for _, option := range claudeOptions(argv) {
			if option.index == slot.Index && option.name == resumeFlag && len(option.values) == 1 && option.values[0] == *selectedUUID {
				bound = true
			}
		}
		if !bound {
			return &RestartInvalidError{Reason: "identity insertion is not a native selector"}
		}
	}

	return nil
}
