package agentic

// StoredPolicySupport says whether an agentic system exposes stored-policy
// inspection. Unsupported systems make no statement about local settings.
type StoredPolicySupport string

const (
	StoredPolicySupported   StoredPolicySupport = "supported"
	StoredPolicyUnsupported StoredPolicySupport = "unsupported"
)

// StoredPolicySourceReason records why a candidate settings source was not
// inspected. None of these states means the source was clean.
type StoredPolicySourceReason string

const (
	StoredPolicySourceAbsent           StoredPolicySourceReason = "absent"
	StoredPolicySourceUnreadable       StoredPolicySourceReason = "unreadable"
	StoredPolicySourceUnparseable      StoredPolicySourceReason = "unparseable"
	StoredPolicySourceRootUnavailable  StoredPolicySourceReason = "root-unavailable"
	StoredPolicySourceNotProvided      StoredPolicySourceReason = "not-provided"
	StoredPolicySourceUnknownSelection StoredPolicySourceReason = "unknown-selection"
)

// StoredPolicySourceIssue is one source the inspector could not inspect.
type StoredPolicySourceIssue struct {
	SourcePath string                   `json:"source_path"`
	Reason     StoredPolicySourceReason `json:"reason"`
}

// StoredPolicyRelaxation is one known setting whose value can reduce
// permission prompts or broaden access. Value is preserved as text from the
// source; the inspector does not resolve precedence or claim that the setting
// took effect.
type StoredPolicyRelaxation struct {
	Selector   string `json:"selector"`
	Value      string `json:"value"`
	SourcePath string `json:"source_path"`
}

// StoredPolicyInspection reports findings only from sources it fully read and
// parsed. SourcesNotInspected distinguishes absence from read and parse
// failures so callers cannot treat an incomplete scan as clean.
type StoredPolicyInspection struct {
	Support             StoredPolicySupport       `json:"support"`
	Relaxations         []StoredPolicyRelaxation  `json:"relaxations,omitempty"`
	SourcesInspected    []string                  `json:"sources_inspected,omitempty"`
	SourcesNotInspected []StoredPolicySourceIssue `json:"sources_not_inspected,omitempty"`
}

// StoredPolicyContext contains only launch-owned inputs needed to locate
// settings. Inspectors must not read ambient process environment or working
// directory state.
type StoredPolicyContext struct {
	Environment []string
	Home        string
	WorkDir     string
}

// StoredPolicyInspector is an optional system capability. The system plugin
// owns its provider-specific settings paths, selectors and value grammar.
type StoredPolicyInspector interface {
	InspectStoredPolicy(StoredPolicyContext) StoredPolicyInspection
}

// InspectStoredPolicy dispatches inspection through the selected system
// plugin, using the environment and roots captured by the launch plan.
func InspectStoredPolicy(system System, plan Plan) StoredPolicyInspection {
	inspector, ok := system.(StoredPolicyInspector)
	if !ok {
		return StoredPolicyInspection{Support: StoredPolicyUnsupported}
	}
	return inspector.InspectStoredPolicy(StoredPolicyContext{
		Environment: append([]string(nil), plan.Env...),
		Home:        plan.Home,
		WorkDir:     plan.WorkDir,
	})
}
