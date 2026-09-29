package agentic

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	CuratorLaunchFragmentV1 = "launch-env-fragment-v1"
	CuratorLaunchFragmentV2 = "launch-env-fragment-v2"

	CuratorDescriptorFlag      CuratorDescriptorKind = "flag"
	CuratorDescriptorConfigKey CuratorDescriptorKind = "config-key"
	CuratorDescriptorVariable  CuratorDescriptorKind = "variable"
	CuratorDescriptorFile      CuratorDescriptorKind = "file"

	CuratorArgumentPath     CuratorFlagArgument = "path"
	CuratorArgumentContents CuratorFlagArgument = "contents"
	CuratorArgumentName     CuratorFlagArgument = "name"

	CuratorSystemPromptAppend  CuratorSystemPromptIntent = "append"
	CuratorSystemPromptReplace CuratorSystemPromptIntent = "replace"
)

// CuratorDescriptorKind is the closed descriptor arm set emitted by the
// Curator launch-environment fragment. Unknown values are refused.
type CuratorDescriptorKind string

// CuratorFlagArgument is the value shape a flag descriptor carries.
type CuratorFlagArgument string

// CuratorSystemPromptIntent is the caller-owned choice of prompt behavior.
// The selected plugin resolves exactly one descriptor with matching semantics.
type CuratorSystemPromptIntent string

// CuratorProfilePin is the profile identity bound into a v1 fragment.
type CuratorProfilePin struct {
	Name       string `json:"name"`
	LockSHA256 string `json:"lock_sha256"`
}

// CuratorPrecedence is copied from the validated fragment. It remains profile
// provenance; this module does not recompose Curator profile overlays.
type CuratorPrecedence struct {
	Winner    string `json:"winner"`
	Placement string `json:"placement"`
}

// CuratorChannelDescriptor is a closed tagged union. Only the fields for Kind
// may be populated. Intent is separate from descriptor semantics: the caller
// supplies SystemPrompt.Intent, and the selected plugin chooses one matching
// descriptor from the complete list.
type CuratorChannelDescriptor struct {
	Kind      CuratorDescriptorKind     `json:"kind"`
	Semantics CuratorSystemPromptIntent `json:"semantics,omitempty"`
	Flag      string                    `json:"flag,omitempty"`
	Argument  CuratorFlagArgument       `json:"argument,omitempty"`
	Name      string                    `json:"name,omitempty"`
	With      []string                  `json:"with,omitempty"`
	Key       string                    `json:"key,omitempty"`
	Variable  string                    `json:"variable,omitempty"`
	Filename  string                    `json:"filename,omitempty"`
}

// CuratorMCPContext carries the resolved MCP file path, its sorted environment
// variable names, and the complete adapter-declared channel descriptor list.
type CuratorMCPContext struct {
	Path     string                     `json:"path"`
	EnvNames []string                   `json:"env_names"`
	Channels []CuratorChannelDescriptor `json:"channels"`
}

// CuratorSystemPromptContext carries the resolved prompt file and the full
// adapter channel list. Intent is launch-request data, not a fragment member,
// so it is omitted when the fragment identity is snapshotted.
type CuratorSystemPromptContext struct {
	Path     string                     `json:"path"`
	Channels []CuratorChannelDescriptor `json:"channels"`
	Intent   CuratorSystemPromptIntent  `json:"-"`
}

// CuratorContext is the typed projection of launch-env-fragment-v1 plus the
// explicit request intent and optional prior plan provenance used for reuse.
// The profile and environment values are validated here; the module never
// resolves a profile or reads raw fragment JSON.
type CuratorContext struct {
	Revision           string                      `json:"fragment"`
	Environment        string                      `json:"environment"`
	Profile            CuratorProfilePin           `json:"profile"`
	Precedence         CuratorPrecedence           `json:"precedence"`
	Env                map[string]string           `json:"env"`
	SystemPrompt       *CuratorSystemPromptContext `json:"system_prompt,omitempty"`
	MCP                *CuratorMCPContext          `json:"mcp,omitempty"`
	PathPrepend        string                      `json:"path_prepend,omitempty"`
	ExpectedProvenance *CuratorContextProvenance   `json:"-"`
}

// CuratorFragmentIdentity is the complete typed fragment value that affects
// launch identity. SystemPrompt.Intent is deliberately excluded because it is
// supplied by the launch caller, not emitted by Curator.
type CuratorFragmentIdentity struct {
	Revision     string                       `json:"fragment"`
	Environment  string                       `json:"environment"`
	Profile      CuratorProfilePin            `json:"profile"`
	Precedence   CuratorPrecedence            `json:"precedence"`
	Env          map[string]string            `json:"env"`
	SystemPrompt *CuratorSystemPromptFragment `json:"system_prompt,omitempty"`
	MCP          *CuratorMCPContext           `json:"mcp,omitempty"`
	PathPrepend  string                       `json:"path_prepend,omitempty"`
}

// CuratorSystemPromptFragment is the fragment-owned portion of the prompt
// context, without the caller's append/replace intent.
type CuratorSystemPromptFragment struct {
	Path     string                     `json:"path"`
	Channels []CuratorChannelDescriptor `json:"channels"`
}

// CuratorContextProvenance is an immutable-by-copy snapshot exposed by Plan.
// Call CuratorContextProvenanceSnapshot to receive a detached deep copy.
type CuratorContextProvenance struct {
	ProfileName         string                    `json:"profile_name"`
	LockSHA256          string                    `json:"lock_sha256"`
	ManagedHomeVariable string                    `json:"managed_home_variable"`
	ManagedHome         string                    `json:"managed_home"`
	SystemPromptIntent  CuratorSystemPromptIntent `json:"system_prompt_intent,omitempty"`
	Fragment            CuratorFragmentIdentity   `json:"fragment"`
}

var (
	ErrCuratorProfileMissing               = errors.New("agentic: Curator context is missing its profile pin")
	ErrCuratorFragmentRevisionUnsupported  = errors.New("agentic: unsupported Curator launch fragment revision")
	ErrCuratorContextMalformed             = errors.New("agentic: malformed typed Curator context")
	ErrUnknownCuratorDescriptor            = errors.New("agentic: unknown Curator context descriptor")
	ErrCuratorContextStale                 = errors.New("agentic: Curator context does not match the stored provenance")
	ErrCuratorContextUnsupported           = errors.New("agentic: selected system cannot apply Curator context")
	ErrCuratorSystemPromptIntentMissing    = errors.New("agentic: Curator system-prompt context requires an explicit intent")
	ErrCuratorSystemPromptIntentInvalid    = errors.New("agentic: unsupported Curator system-prompt intent")
	ErrCuratorSystemPromptChannelMissing   = errors.New("agentic: no Curator system-prompt channel matches the requested intent")
	ErrCuratorSystemPromptChannelAmbiguous = errors.New("agentic: multiple Curator system-prompt channels match the requested intent")
)

// UnknownCuratorDescriptorError reports an unrecognized descriptor kind.
type UnknownCuratorDescriptorError struct {
	Kind CuratorDescriptorKind
}

func (e *UnknownCuratorDescriptorError) Error() string {
	return fmt.Sprintf("%v: %q", ErrUnknownCuratorDescriptor, e.Kind)
}

func (e *UnknownCuratorDescriptorError) Unwrap() error { return ErrUnknownCuratorDescriptor }

// CuratorContextValidator is an optional system-plugin gate for the typed
// Curator fragment. BuildPlan invokes it before preparing prompts or building
// any launch surface.
type CuratorContextValidator interface {
	ValidateCuratorContext(LaunchRequest, LaunchMode) error
}

// ValidateCuratorContext checks the request-side fragment boundary that is
// independent of any particular system plugin. Raw JSON decoding and file
// reads belong to the consumer that creates this typed request.
func ValidateCuratorContext(context *CuratorContext, requestHome string) error {
	if context == nil {
		return nil
	}
	if context.Profile.Name == "" || context.Profile.LockSHA256 == "" {
		return ErrCuratorProfileMissing
	}
	if !validCuratorIdentifier(context.Profile.Name) || !validCuratorLockHash(context.Profile.LockSHA256) {
		return curatorMalformed("profile pin is invalid")
	}
	if context.Revision != CuratorLaunchFragmentV1 {
		return fmt.Errorf("%w: %q", ErrCuratorFragmentRevisionUnsupported, context.Revision)
	}
	switch context.Environment {
	case "claude_code", "codex_cli", "opencode", "pi":
	default:
		return curatorMalformed("environment is not a supported typed fragment value")
	}
	if (context.Precedence.Winner != "higher-weight" && context.Precedence.Winner != "lower-weight") ||
		(context.Precedence.Placement != "winner-last" && context.Precedence.Placement != "winner-first") {
		return curatorMalformed("precedence is invalid")
	}
	if len(context.Env) != 1 {
		return curatorMalformed("exactly one managed-home environment entry is required")
	}
	var homeVariable, home string
	for key, value := range context.Env {
		homeVariable, home = key, value
	}
	wantHomeVariable := ""
	switch context.Environment {
	case "claude_code":
		wantHomeVariable = "CLAUDE_CONFIG_DIR"
	case "codex_cli":
		wantHomeVariable = "CODEX_HOME"
	case "opencode":
		wantHomeVariable = "XDG_CONFIG_HOME"
	case "pi":
		wantHomeVariable = "PI_CODING_AGENT_DIR"
	}
	if homeVariable != wantHomeVariable || !validCuratorAbsolutePath(home) || requestHome != home {
		return curatorMalformed("managed-home entry must match LaunchRequest.Home and its environment")
	}
	if context.PathPrepend != "" {
		if !validCuratorAbsolutePath(context.PathPrepend) {
			return curatorMalformed("path_prepend must be an absolute path")
		}
		return fmt.Errorf("%w: path_prepend has no registered root-bound launch mapping", ErrCuratorContextUnsupported)
	}
	if context.MCP != nil {
		if context.Environment == "pi" {
			return curatorMalformed("pi fragments cannot carry MCP context")
		}
		if !validCuratorAbsolutePath(context.MCP.Path) {
			return curatorMalformed("MCP path must be absolute")
		}
		if !isSortedUniqueCuratorEnvNames(context.MCP.EnvNames) {
			return curatorMalformed("MCP env_names must be sorted, unique, and non-reserved")
		}
		if len(context.MCP.Channels) != 1 {
			return curatorMalformed("MCP must carry exactly one channel descriptor")
		}
		if err := validateCuratorDescriptor(context.MCP.Channels[0], false); err != nil {
			return err
		}
	}
	if context.SystemPrompt != nil {
		if !validCuratorAbsolutePath(context.SystemPrompt.Path) {
			return curatorMalformed("system-prompt path must be absolute")
		}
		if context.SystemPrompt.Intent == "" {
			return ErrCuratorSystemPromptIntentMissing
		}
		if context.SystemPrompt.Intent != CuratorSystemPromptAppend && context.SystemPrompt.Intent != CuratorSystemPromptReplace {
			return ErrCuratorSystemPromptIntentInvalid
		}
		for _, descriptor := range context.SystemPrompt.Channels {
			if err := validateCuratorDescriptor(descriptor, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func curatorMalformed(reason string) error {
	return fmt.Errorf("%w: %s", ErrCuratorContextMalformed, reason)
}

func validateCuratorDescriptor(descriptor CuratorChannelDescriptor, systemPrompt bool) error {
	if systemPrompt {
		if descriptor.Semantics != CuratorSystemPromptAppend && descriptor.Semantics != CuratorSystemPromptReplace {
			return curatorMalformed("system-prompt descriptor semantics must be append or replace")
		}
	} else if descriptor.Semantics != "" {
		return curatorMalformed("MCP descriptors do not carry system-prompt semantics")
	}
	noExtras := func(flag, argument, name, key, variable, filename bool, with bool) bool {
		return (!flag || descriptor.Flag == "") && (!argument || descriptor.Argument == "") &&
			(!name || descriptor.Name == "") && (!key || descriptor.Key == "") &&
			(!variable || descriptor.Variable == "") && (!filename || descriptor.Filename == "") &&
			(!with || descriptor.With == nil)
	}
	switch descriptor.Kind {
	case CuratorDescriptorFlag:
		if descriptor.Flag == "" || !validCuratorFlag(descriptor.Flag) {
			return curatorMalformed("flag descriptor has an invalid flag")
		}
		if descriptor.Argument != CuratorArgumentPath && descriptor.Argument != CuratorArgumentContents && descriptor.Argument != CuratorArgumentName {
			return curatorMalformed("flag descriptor has an invalid argument kind")
		}
		if descriptor.Argument == CuratorArgumentName {
			if !validCuratorIdentifier(descriptor.Name) {
				return curatorMalformed("name argument requires a valid reserved name")
			}
		} else if descriptor.Name != "" {
			return curatorMalformed("name is permitted only for a name argument")
		}
		if descriptor.With != nil && len(descriptor.With) == 0 {
			return curatorMalformed("with must be absent or contain companion flags")
		}
		seen := make(map[string]struct{}, len(descriptor.With))
		for _, companion := range descriptor.With {
			if !validCuratorFlag(companion) {
				return curatorMalformed("with contains an invalid companion flag")
			}
			if _, exists := seen[companion]; exists {
				return curatorMalformed("with repeats a companion flag")
			}
			seen[companion] = struct{}{}
		}
		if !noExtras(false, false, false, true, true, true, false) {
			return curatorMalformed("flag descriptor carries fields from another union arm")
		}
	case CuratorDescriptorConfigKey:
		if !validCuratorIdentifier(descriptor.Key) || !noExtras(true, true, true, false, true, true, true) {
			return curatorMalformed("config-key descriptor is malformed")
		}
	case CuratorDescriptorVariable:
		if !validCuratorIdentifier(descriptor.Variable) || !noExtras(true, true, true, true, false, true, true) {
			return curatorMalformed("variable descriptor is malformed")
		}
	case CuratorDescriptorFile:
		if !validCuratorPortablePath(descriptor.Filename) || !noExtras(true, true, true, true, true, false, true) {
			return curatorMalformed("file descriptor is malformed")
		}
	default:
		return &UnknownCuratorDescriptorError{Kind: descriptor.Kind}
	}
	return nil
}

func validCuratorIdentifier(value string) bool {
	runes := []rune(value)
	if len(runes) == 0 || len(runes) > 128 || !curatorAlphaNumeric(runes[0]) || !curatorIdentifierTail(runes[len(runes)-1]) {
		return false
	}
	for _, r := range runes {
		if !curatorAlphaNumeric(r) && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	base := strings.ToLower(strings.SplitN(value, ".", 2)[0])
	if base == "con" || base == "prn" || base == "aux" || base == "nul" {
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}

func curatorAlphaNumeric(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}

func curatorIdentifierTail(r rune) bool { return curatorAlphaNumeric(r) || r == '_' || r == '-' }

func validCuratorLockHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validCuratorAbsolutePath(value string) bool {
	if utf8.RuneCountInString(value) < 2 || utf8.RuneCountInString(value) > 4096 || !strings.HasPrefix(value, "/") || strings.IndexByte(value, 0) >= 0 {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

func validCuratorPortablePath(value string) bool {
	if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 4096 || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:") || strings.Contains(value, "//") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return false
		}
		if !validCuratorIdentifier(segment) && strings.ContainsAny(segment, "<>\"|?*") {
			return false
		}
		base := strings.ToLower(strings.SplitN(segment, ".", 2)[0])
		if base == "con" || base == "prn" || base == "aux" || base == "nul" ||
			(len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

func validCuratorFlag(value string) bool {
	if utf8.RuneCountInString(value) < 2 || utf8.RuneCountInString(value) > 128 || !strings.HasPrefix(value, "-") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func isSortedUniqueCuratorEnvNames(values []string) bool {
	previous := ""
	for i, value := range values {
		if !validEnvironmentName(value) || curatorReservedMCPEnv(value) || (i > 0 && value <= previous) {
			return false
		}
		previous = value
	}
	return true
}

func curatorReservedMCPEnv(name string) bool {
	reserved := map[string]struct{}{
		"PATH": {}, "HOME": {}, "TMPDIR": {}, "TEMP": {}, "TMP": {},
		"XDG_CONFIG_HOME": {}, "XDG_CACHE_HOME": {}, "XDG_DATA_HOME": {}, "XDG_STATE_HOME": {},
		"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "FTP_PROXY": {}, "NO_PROXY": {},
		"http_proxy": {}, "https_proxy": {}, "all_proxy": {}, "ftp_proxy": {}, "no_proxy": {},
		"RES_OPTIONS": {}, "HOSTALIASES": {}, "LOCALDOMAIN": {}, "IFS": {}, "CSK_PROJECT_ROOT": {},
		"USERPROFILE": {}, "APPDATA": {}, "LOCALAPPDATA": {}, "PATHEXT": {}, "COMSPEC": {},
		"WINDIR": {}, "SYSTEMROOT": {}, "__PYVENV_LAUNCHER__": {},
	}
	if _, found := reserved[name]; found {
		return true
	}
	return strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "DYLD_") ||
		strings.HasPrefix(name, "PYTHON") || strings.HasPrefix(name, "NODE_") || strings.HasPrefix(name, "NPM_CONFIG_")
}

func curatorContextProvenance(context *CuratorContext) CuratorContextProvenance {
	var homeVariable, home string
	for key, value := range context.Env {
		homeVariable, home = key, value
	}
	fragment := CuratorFragmentIdentity{
		Revision: context.Revision, Environment: context.Environment,
		Profile: context.Profile, Precedence: context.Precedence,
		Env: cloneStringMap(context.Env), MCP: cloneCuratorMCP(context.MCP),
		PathPrepend: context.PathPrepend,
	}
	if context.SystemPrompt != nil {
		fragment.SystemPrompt = &CuratorSystemPromptFragment{
			Path:     context.SystemPrompt.Path,
			Channels: cloneCuratorDescriptors(context.SystemPrompt.Channels),
		}
	}
	provenance := CuratorContextProvenance{
		ProfileName: context.Profile.Name, LockSHA256: context.Profile.LockSHA256,
		ManagedHomeVariable: homeVariable, ManagedHome: home,
		Fragment: fragment,
	}
	if context.SystemPrompt != nil {
		provenance.SystemPromptIntent = context.SystemPrompt.Intent
	}
	return provenance
}

func cloneCuratorProvenance(source CuratorContextProvenance) CuratorContextProvenance {
	copy := source
	copy.Fragment.Env = cloneStringMap(source.Fragment.Env)
	copy.Fragment.MCP = cloneCuratorMCP(source.Fragment.MCP)
	if source.Fragment.SystemPrompt != nil {
		copy.Fragment.SystemPrompt = &CuratorSystemPromptFragment{
			Path:     source.Fragment.SystemPrompt.Path,
			Channels: cloneCuratorDescriptors(source.Fragment.SystemPrompt.Channels),
		}
	}
	return copy
}

func cloneCuratorContext(source *CuratorContext) *CuratorContext {
	if source == nil {
		return nil
	}
	copy := *source
	copy.Env = cloneStringMap(source.Env)
	copy.MCP = cloneCuratorMCP(source.MCP)
	if source.SystemPrompt != nil {
		copy.SystemPrompt = &CuratorSystemPromptContext{
			Path:     source.SystemPrompt.Path,
			Channels: cloneCuratorDescriptors(source.SystemPrompt.Channels),
			Intent:   source.SystemPrompt.Intent,
		}
	}
	if source.ExpectedProvenance != nil {
		provenance := cloneCuratorProvenance(*source.ExpectedProvenance)
		copy.ExpectedProvenance = &provenance
	}
	return &copy
}

func cloneCuratorMCP(source *CuratorMCPContext) *CuratorMCPContext {
	if source == nil {
		return nil
	}
	return &CuratorMCPContext{
		Path:     source.Path,
		EnvNames: append([]string(nil), source.EnvNames...),
		Channels: cloneCuratorDescriptors(source.Channels),
	}
}

func cloneCuratorDescriptors(source []CuratorChannelDescriptor) []CuratorChannelDescriptor {
	if source == nil {
		return nil
	}
	copy := make([]CuratorChannelDescriptor, len(source))
	for i, descriptor := range source {
		copy[i] = descriptor
		copy[i].With = append([]string(nil), descriptor.With...)
	}
	return copy
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func sameCuratorFragmentIdentity(left, right CuratorFragmentIdentity) bool {
	return reflect.DeepEqual(left, right)
}

func ValidateCuratorContextIdentity(expected CuratorContextProvenance, current CuratorContextProvenance) error {
	if expected.ProfileName != current.ProfileName || expected.LockSHA256 != current.LockSHA256 ||
		expected.ManagedHomeVariable != current.ManagedHomeVariable || expected.ManagedHome != current.ManagedHome ||
		expected.SystemPromptIntent != current.SystemPromptIntent ||
		!sameCuratorFragmentIdentity(expected.Fragment, current.Fragment) {
		return ErrCuratorContextStale
	}
	return nil
}
