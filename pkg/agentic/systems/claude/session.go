package claude

import (
	"encoding/json"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Session selector spellings, each spelled once for the session fill to
// recognize. These are the pinned 2.1.288 rows it classifies:
//
//   - `--name` / `-n` take a required value: the session display name (shown
//     in the prompt box, the /resume picker and the terminal title).
//   - `--remote-control` / `--rc` take an optional value: the alias is
//     native's own ("Alias for --remote-control" in the pinned help), and a
//     present value names the remote-control session.
//   - `--remote-control-session-name-prefix` takes a required value: the
//     prefix for auto-generated remote-control session names. It carries no
//     name itself — the provider generates that at runtime — but it is an
//     RC-family token, so its position is reported.
//   - `remoteControlAtStartup` is the settings key that enables remote
//     control without an argv token. Only the explicit `--settings` source
//     the launch itself carries is consulted; implicit user, project and
//     managed configuration is outside this grammar, as it is for the tool
//     policy.
//
// Classification only: this file constructs no argv and emits nothing. The
// argvguard signature (args.go's construction spellings) names none of these
// literals, so no allowlist entry is owed.
const (
	sessionNameFlag         = "--name"
	sessionNameShortFlag    = "-n"
	remoteControlFlag       = "--remote-control"
	remoteControlAliasFlag  = "--rc"
	remoteControlPrefixFlag = "--remote-control-session-name-prefix"
	remoteControlSetting    = "remoteControlAtStartup"
)

var _ agentic.SessionPlanner = (*System)(nil)

// FillPlanSession implements agentic.SessionPlanner: the native session name
// when the argv sets one, the remote-control intent, and the exact argv
// positions of the RC-family tokens, filled into Plan.Session from the argv
// this plugin composed (BuildPlan, and FinalizePlan after a native tail). It is
// never asked across ExportSeal/ImportSeal: Session is unverified there.
//
// Its only input is an agentic.SessionFill, which only the agentic package
// can issue: there is no way for a caller outside the module to hand this
// method argv of its choosing, and a SessionFill that was not issued (the
// zero value) is refused as such. The derivation itself is the unexported
// deriveSession, which no external caller can reach either.
//
// Trust boundary: registry registration. A plugin registered for a system id
// owns the plans it builds; one that embeds *System inherits this method by
// Go embedding and fills sessions for its own plans — accepted scope, not a
// bypass.
func (*System) FillPlanSession(fill agentic.SessionFill) (*agentic.PlanSession, error) {
	if !fill.Issued() {
		return nil, &agentic.SessionInvalidError{Reason: "session fill was not issued by the registry"}
	}
	return deriveSession(fill.Argv(), fill.WorkDir())
}

// deriveSession is the one session grammar. The parse is claudeOptions — the
// same pinned Commander ownership parser every other gate in this plugin
// uses. There is no second parser: required selectors own their next token
// even when dash-leading, optional RC values stop at flags, attached `=`
// values stay attached, combined short switches decompose, and everything
// after `--` is prompt text, never a selector. Whatever the grammar says
// about ownership, the record reports. There is no plugin-id comparison here:
// BuildPlan reaches this through the registry's own lookup of the plan's
// system.
//
// Selection rules, in the order they apply:
//
//  1. Repeating one selector class refuses: a second `--name`/`-n`, a second
//     `--remote-control`/`--rc`, or a second name-prefix flag. Commander
//     resolves repeats by order, and order-dependence is exactly what a
//     record must not model — even when the two values agree, because a
//     repeated channel is either a composition bug or an ambiguity, and the
//     plugin cannot tell which.
//  2. A required selector dangling at the argv end — a name or prefix flag
//     with no value — refuses. Native fails that launch for a missing
//     argument; the record fails it for the same missing value.
//  3. Names from DIFFERENT classes must agree: `--name` and an RC value
//     naming two different sessions refuse as conflicting, while `-n NAME`
//     beside `--remote-control NAME` corroborate one name. Equal values are
//     order-independent, so they record; differing values are a choice the
//     plugin will not make.
//  4. With no RC token on the argv, the explicit settings source is the only
//     other origin: `remoteControlAtStartup: true` there enables RC with
//     empty indices, because no argv token carries it. The source is the one
//     the tool policy already selects (agreeing eager and Commander parses),
//     read again here; a source that cannot be read or whose key is not a
//     boolean refuses rather than reading as absence.
//
// The result honors the agentic.PlanSession contract: unique in-bounds
// indices in argv order (each occurrence contributes its own position, so
// uniqueness and bounds hold by construction), a non-nil index slice, a
// copied name, and never disabled-with-indices. Only `--name`/`-n` and RC
// values contribute the name; the prefix flag contributes its index alone.
//
// Bound: the settings source is read here and again by the tool policy while
// Args ran, so a file rewritten between the two reads of one BuildPlan can
// disagree with itself; the second read fails closed on unreadable or
// invalid content, and a changed boolean is the residual. No
// verifier reads settings again: ImportSeal derives nothing, so a settings file
// rewritten after BuildPlan moves no seal.
func deriveSession(argv []string, workDir string) (*agentic.PlanSession, error) {
	var (
		nameValues  []string
		nameCount   int
		rcCount     int
		prefixCount int
		rcIndices   = []int{}
	)
	for _, option := range claudeOptions(argv) {
		switch option.name {
		case sessionNameFlag, sessionNameShortFlag:
			nameCount++
			if nameCount > 1 {
				return nil, &agentic.SessionInvalidError{Reason: "duplicate native name selector"}
			}
			if len(option.values) != 1 {
				return nil, &agentic.SessionInvalidError{Reason: "native name selector without a value"}
			}
			nameValues = append(nameValues, option.values[0])
		case remoteControlFlag, remoteControlAliasFlag:
			rcCount++
			if rcCount > 1 {
				return nil, &agentic.SessionInvalidError{Reason: "duplicate remote-control selector"}
			}
			rcIndices = append(rcIndices, option.index)
			if len(option.values) > 0 {
				nameValues = append(nameValues, option.values[0])
			}
		case remoteControlPrefixFlag:
			prefixCount++
			if prefixCount > 1 {
				return nil, &agentic.SessionInvalidError{Reason: "duplicate remote-control name-prefix selector"}
			}
			if len(option.values) != 1 {
				return nil, &agentic.SessionInvalidError{Reason: "remote-control name-prefix selector without a value"}
			}
			rcIndices = append(rcIndices, option.index)
		}
	}
	var name *string
	if len(nameValues) > 0 {
		for _, value := range nameValues[1:] {
			if value != nameValues[0] {
				return nil, &agentic.SessionInvalidError{Reason: "conflicting native session names"}
			}
		}
		value := nameValues[0]
		name = &value
	}
	enabled := len(rcIndices) > 0
	if !enabled {
		if settingsEnabled, err := settingsRemoteControl(argv, workDir); err != nil {
			return nil, err
		} else {
			enabled = settingsEnabled
		}
	}
	return &agentic.PlanSession{Name: name, RCEnabled: enabled, RCIndices: rcIndices}, nil
}

// settingsRemoteControl reports the settings-origin remote-control enablement
// of the explicit --settings source the argv carries. No source is
// absence; a source that cannot be selected, read or decoded, or whose
// remoteControlAtStartup value is not a JSON boolean, refuses.
func settingsRemoteControl(argv []string, workDir string) (bool, error) {
	var source string
	if value, present, err := effectiveSettingsSource(argv); err != nil {
		return false, err
	} else if !present {
		return false, nil
	} else {
		source = value
	}
	settings, kind := readSettingsObject(source, workDir)
	if kind != "" {
		return false, &agentic.SessionInvalidError{Reason: "explicit settings source is " + string(kind)}
	}
	raw, present := settings[remoteControlSetting]
	if !present {
		return false, nil
	}
	var enabled bool
	if string(raw) == "null" || json.Unmarshal(raw, &enabled) != nil {
		return false, &agentic.SessionInvalidError{Reason: "settings remote-control key is not a boolean"}
	}
	return enabled, nil
}
