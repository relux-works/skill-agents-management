package agentic

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	CuratorLaunchFragmentV1 = "launch-env-fragment-v1"
	CuratorLaunchFragmentV2 = "launch-env-fragment-v2"
	// CuratorLaunchFragmentV3 names the v3 revision token. Revision v3
	// retains the v2 permissions contract and adds the muse environment
	// with exactly the four section 7.1 XDG variables; every other
	// environment keeps its single home variable and v2 channel rules.
	CuratorLaunchFragmentV3 = "launch-env-fragment-v3"
	// CuratorLaunchFragmentV4 names the v4 revision token (curator-spec
	// environments §10.2, CIP-0017 and CIP-0015). V4 retains the v3
	// permissions, path and channel contracts and adds the typed
	// credential_store policy for every adapter and the Muse-only
	// root_context fact. A managed muse launch requires v4: v3 is refused
	// with upgrade guidance and never carries the fact.
	CuratorLaunchFragmentV4 = "launch-env-fragment-v4"

	// CuratorMuseCredentialBackendVariable and CuratorMuseCredentialBackendFile
	// are the registry-owned constant every v4 muse fragment env carries
	// beside the four XDG paths. It is the only non-path fragment env value
	// and is not profile-controlled.
	CuratorMuseCredentialBackendVariable = "TBH_CREDENTIAL_BACKEND"
	CuratorMuseCredentialBackendFile     = "file"

	CuratorCredentialStoreNative   CuratorCredentialSelection = "native"
	CuratorCredentialStoreFile     CuratorCredentialSelection = "file"
	CuratorCredentialStoreKeychain CuratorCredentialSelection = "keychain"

	// curatorCodexCredentialStoreKey is the only config_overrides key v4
	// admits, on codex_cli and only with a non-native selection.
	curatorCodexCredentialStoreKey = "cli_auth_credentials_store"

	CuratorDescriptorFlag      CuratorDescriptorKind = "flag"
	CuratorDescriptorConfigKey CuratorDescriptorKind = "config-key"
	CuratorDescriptorVariable  CuratorDescriptorKind = "variable"
	CuratorDescriptorFile      CuratorDescriptorKind = "file"

	CuratorArgumentPath     CuratorFlagArgument = "path"
	CuratorArgumentContents CuratorFlagArgument = "contents"
	CuratorArgumentName     CuratorFlagArgument = "name"

	CuratorSystemPromptAppend  CuratorSystemPromptIntent = "append"
	CuratorSystemPromptReplace CuratorSystemPromptIntent = "replace"

	CuratorPermissionNative CuratorPermissionMode = "native"
	CuratorPermissionYolo   CuratorPermissionMode = "yolo"

	CuratorPermissionSourceProfile CuratorPermissionSource = "profile"
	CuratorPermissionSourceGlobal  CuratorPermissionSource = "global"
	CuratorPermissionSourceDefault CuratorPermissionSource = "default"
)

// CuratorDescriptorKind is the closed descriptor arm set emitted by the
// Curator launch-environment fragment. Unknown values are refused.
type CuratorDescriptorKind string

// CuratorFlagArgument is the value shape a flag descriptor carries.
type CuratorFlagArgument string

// CuratorSystemPromptIntent is the caller-owned choice of prompt behavior.
// The selected plugin resolves exactly one descriptor with matching semantics.
type CuratorSystemPromptIntent string

// CuratorPermissionMode is the resolved v2 profile permission level: the
// permissions.<profile> knob value when the knob names one, native otherwise.
type CuratorPermissionMode string

// CuratorPermissionSource names which Curator-side input fixed the v2
// permission mode: the machine knob, the fleet-global lock, or knob absence.
type CuratorPermissionSource string

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

// CuratorPermissions is the typed v2 permissions member: the resolved profile
// level and the lock engagement for the resolved profile. The launcher needs
// mode and source together: source default is silence (fall through to the
// launcher-global default), never an explicit native naming.
type CuratorPermissions struct {
	Mode   CuratorPermissionMode   `json:"mode"`
	Locked bool                    `json:"locked"`
	Source CuratorPermissionSource `json:"source"`
}

// UnmarshalJSON enforces the §10.2 closed permissions object at the wire
// boundary: exactly the members mode, locked and source, each present with
// its required JSON type. Presence and type cannot be recovered after a
// plain struct decode collapses an absent locked to false and drops unknown
// members, so this method decodes through a member map first. It refuses
// non-object wire, unknown members, absent members, explicit nulls and
// mistyped members with the malformed typed refusal, returned directly at
// each site; the value vocabularies and the lock-consistency relation stay
// with ValidateCuratorContext, which owns the typed layer.
func (p *CuratorPermissions) UnmarshalJSON(wire []byte) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(wire, &members); err != nil {
		return fmt.Errorf("%w: permissions must be a JSON object", ErrCuratorContextMalformed)
	}
	for name := range members {
		if name != "mode" && name != "locked" && name != "source" {
			return fmt.Errorf("%w: permissions carries an unknown member %q", ErrCuratorContextMalformed, name)
		}
	}
	modeRaw, modePresent := members["mode"]
	var mode CuratorPermissionMode
	modeTypeErr := json.Unmarshal(modeRaw, &mode)
	if !modePresent || string(modeRaw) == "null" || modeTypeErr != nil {
		return fmt.Errorf("%w: permissions mode is required and must be a string", ErrCuratorContextMalformed)
	}
	lockedRaw, lockedPresent := members["locked"]
	var locked bool
	lockedTypeErr := json.Unmarshal(lockedRaw, &locked)
	if !lockedPresent || string(lockedRaw) == "null" || lockedTypeErr != nil {
		return fmt.Errorf("%w: permissions locked is required and must be a boolean", ErrCuratorContextMalformed)
	}
	sourceRaw, sourcePresent := members["source"]
	var source CuratorPermissionSource
	sourceTypeErr := json.Unmarshal(sourceRaw, &source)
	if !sourcePresent || string(sourceRaw) == "null" || sourceTypeErr != nil {
		return fmt.Errorf("%w: permissions source is required and must be a string", ErrCuratorContextMalformed)
	}
	*p = CuratorPermissions{Mode: mode, Locked: locked, Source: source}
	return nil
}

// CuratorRootContext is the v4 Muse-only root-context fact: exactly
// {"exclusive":true}. Curator emits it iff a successful resolve verified the
// selected home's root-context target; the launcher maps it to the
// form-specific foreign-personal-context exclusion and never inspects files
// to decide it.
//
// A JSON null member decodes to a nil pointer without reaching UnmarshalJSON,
// so the consumer that decodes raw fragment JSON must refuse an explicit
// null itself; every other non-conforming wire value refuses here.
type CuratorRootContext struct {
	Exclusive bool `json:"exclusive"`
}

// UnmarshalJSON enforces the closed object: exactly one member, exclusive,
// carrying the JSON literal true. Empty objects, false, strings, numbers,
// extra members and repeated keys are refused as malformed.
func (r *CuratorRootContext) UnmarshalJSON(wire []byte) error {
	members, err := decodeCuratorClosedObject(wire)
	if err != nil {
		return fmt.Errorf("%w: root_context must be the object {\"exclusive\":true}: %v", ErrCuratorContextMalformed, err)
	}
	value, present := members["exclusive"]
	if len(members) != 1 || !present || string(value) != "true" {
		return fmt.Errorf("%w: root_context must be exactly {\"exclusive\":true}", ErrCuratorContextMalformed)
	}
	*r = CuratorRootContext{Exclusive: true}
	return nil
}

// CuratorCredentialSelection is the closed v4 credential_store selection
// vocabulary: the effective backend after section 7.4's native/unset mapping.
type CuratorCredentialSelection string

// CuratorCredentialStore is the v4 typed credential-store policy. Muse
// requires exactly {"selection":"file"}; codex_cli admits native with no
// overrides, or file/keychain with exactly the matching
// cli_auth_credentials_store override; every other adapter admits only
// native. As with CuratorRootContext, an explicit JSON null member is the
// raw-JSON consumer's to refuse.
type CuratorCredentialStore struct {
	Selection       CuratorCredentialSelection `json:"selection"`
	ConfigOverrides map[string]string          `json:"config_overrides,omitempty"`
}

// UnmarshalJSON enforces the closed object at the wire boundary: a required
// string selection and an optional config_overrides object of strings, no
// other member, no repeated key and no explicit null.
func (s *CuratorCredentialStore) UnmarshalJSON(wire []byte) error {
	members, err := decodeCuratorClosedObject(wire)
	if err != nil {
		return fmt.Errorf("%w: credential_store must be a JSON object: %v", ErrCuratorContextMalformed, err)
	}
	var store CuratorCredentialStore
	for name, value := range members {
		if string(value) == "null" {
			return fmt.Errorf("%w: credential_store %s must not be null", ErrCuratorContextMalformed, name)
		}
		switch name {
		case "selection":
			if err := json.Unmarshal(value, &store.Selection); err != nil {
				return fmt.Errorf("%w: credential_store selection must be a string", ErrCuratorContextMalformed)
			}
		case "config_overrides":
			overrides, err := decodeCuratorClosedObject(value)
			if err != nil {
				return fmt.Errorf("%w: credential_store config_overrides must be an object of strings: %v", ErrCuratorContextMalformed, err)
			}
			store.ConfigOverrides = make(map[string]string, len(overrides))
			for key, raw := range overrides {
				var override string
				if err := json.Unmarshal(raw, &override); err != nil {
					return fmt.Errorf("%w: credential_store config_overrides %s must be a string", ErrCuratorContextMalformed, key)
				}
				store.ConfigOverrides[key] = override
			}
		default:
			return fmt.Errorf("%w: credential_store carries an unknown member %q", ErrCuratorContextMalformed, name)
		}
	}
	if _, present := members["selection"]; !present {
		return fmt.Errorf("%w: credential_store selection is required", ErrCuratorContextMalformed)
	}
	*s = store
	return nil
}

// decodeCuratorClosedObject decodes one JSON object into its raw members and
// refuses repeated keys, which a plain map decode would silently collapse
// into the last value.
func decodeCuratorClosedObject(wire []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(wire)))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	members := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		name, isName := token.(string)
		if err != nil || !isName {
			return nil, errors.New("not an object")
		}
		if _, repeated := members[name]; repeated {
			return nil, fmt.Errorf("repeated member %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("not an object")
		}
		members[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("not an object")
	}
	if _, err := decoder.Token(); err == nil {
		return nil, errors.New("trailing data after the object")
	}
	return members, nil
}

// CuratorContext is the typed projection of launch-env-fragment-v1 through
// launch-env-fragment-v4 plus the explicit request intent and optional prior
// plan provenance used for reuse. The profile and environment values are
// validated here; the module never resolves a profile or reads raw fragment
// JSON. Permissions is required on v2 through v4 and refused on v1. Env
// carries exactly one managed-home entry, except muse, which carries the
// four XDG variables and, on v4, the credential backend constant; see
// CuratorMuseHomes. CredentialStore and RootContext exist only on v4.
type CuratorContext struct {
	Revision           string                      `json:"fragment"`
	Environment        string                      `json:"environment"`
	Profile            CuratorProfilePin           `json:"profile"`
	Precedence         CuratorPrecedence           `json:"precedence"`
	Env                map[string]string           `json:"env"`
	SystemPrompt       *CuratorSystemPromptContext `json:"system_prompt,omitempty"`
	MCP                *CuratorMCPContext          `json:"mcp,omitempty"`
	PathPrepend        string                      `json:"path_prepend,omitempty"`
	Permissions        *CuratorPermissions         `json:"permissions,omitempty"`
	CredentialStore    *CuratorCredentialStore     `json:"credential_store,omitempty"`
	RootContext        *CuratorRootContext         `json:"root_context,omitempty"`
	ExpectedProvenance *CuratorContextProvenance   `json:"-"`
}

// CuratorFragmentIdentity is the complete typed fragment value that affects
// launch identity. SystemPrompt.Intent is deliberately excluded because it is
// supplied by the launch caller, not emitted by Curator. Permissions rides the
// identity so a changed profile level or lock engagement is stale identity;
// CredentialStore and RootContext ride it for the same reason, so a gained or
// lost root-context fact is stale identity rather than a silent reuse.
// Revision rides the identity only as a stored-side acceptance gate: see
// sameCuratorFragmentIdentity, which tolerates a Revision-only drift between
// accepted revisions and compares every other field exactly.
type CuratorFragmentIdentity struct {
	Revision        string                       `json:"fragment"`
	Environment     string                       `json:"environment"`
	Profile         CuratorProfilePin            `json:"profile"`
	Precedence      CuratorPrecedence            `json:"precedence"`
	Env             map[string]string            `json:"env"`
	SystemPrompt    *CuratorSystemPromptFragment `json:"system_prompt,omitempty"`
	MCP             *CuratorMCPContext           `json:"mcp,omitempty"`
	PathPrepend     string                       `json:"path_prepend,omitempty"`
	Permissions     *CuratorPermissions          `json:"permissions,omitempty"`
	CredentialStore *CuratorCredentialStore      `json:"credential_store,omitempty"`
	RootContext     *CuratorRootContext          `json:"root_context,omitempty"`
}

// CuratorSystemPromptFragment is the fragment-owned portion of the prompt
// context, without the caller's append/replace intent.
type CuratorSystemPromptFragment struct {
	Path     string                     `json:"path"`
	Channels []CuratorChannelDescriptor `json:"channels"`
}

// CuratorContextProvenance is an immutable-by-copy snapshot exposed by Plan.
// Call CuratorContextProvenanceSnapshot to receive a detached deep copy.
//
// For muse, no single variable names the managed home: ManagedHomeVariable
// is empty, ManagedHome is the shared <home> parent (equal to the validated
// LaunchRequest.Home), and MuseHomes carries the four verified XDG values.
type CuratorContextProvenance struct {
	ProfileName         string                    `json:"profile_name"`
	LockSHA256          string                    `json:"lock_sha256"`
	ManagedHomeVariable string                    `json:"managed_home_variable"`
	ManagedHome         string                    `json:"managed_home"`
	MuseHomes           *CuratorMuseHomes         `json:"muse_homes,omitempty"`
	SystemPromptIntent  CuratorSystemPromptIntent `json:"system_prompt_intent,omitempty"`
	Fragment            CuratorFragmentIdentity   `json:"fragment"`
}

// CuratorMuseHomes is the validated muse XDG home set: the four section 7.1
// variables under one absolute <home> parent, ending in /config, /data,
// /state and /cache respectively. It is the typed extension that carries the
// muse fragment env; the single-variable ManagedHomeVariable/ManagedHome
// pair cannot name four values, so muse provenance carries this set instead
// and ManagedHome holds the parent. XDG names are spelled in full so no
// second vocabulary can drift from the fragment member names.
type CuratorMuseHomes struct {
	Parent        string `json:"parent"`
	XDGConfigHome string `json:"xdg_config_home"`
	XDGDataHome   string `json:"xdg_data_home"`
	XDGStateHome  string `json:"xdg_state_home"`
	XDGCacheHome  string `json:"xdg_cache_home"`
}

// CuratorMuseHomesFromEnv validates a muse fragment env and returns the
// typed home set. It is the single implementation of the v3 muse env shape:
// exactly the four XDG variables, no HOME, every value absolute, and every
// value exactly path.Join(parent, leaf) under one absolute non-root parent.
// Both ValidateCuratorContext and CuratorContext.MuseHomes call it, so the
// shape has one source rather than two spellings that could drift apart.
func CuratorMuseHomesFromEnv(env map[string]string) (CuratorMuseHomes, error) {
	if env["HOME"] != "" {
		return CuratorMuseHomes{}, curatorMalformed("muse fragments must not carry HOME")
	}
	if len(env) != 4 {
		return CuratorMuseHomes{}, curatorMalformed("muse fragments require exactly the four XDG variables")
	}
	config := env["XDG_CONFIG_HOME"]
	data := env["XDG_DATA_HOME"]
	state := env["XDG_STATE_HOME"]
	cache := env["XDG_CACHE_HOME"]
	if !validCuratorAbsolutePath(config) || !validCuratorAbsolutePath(data) || !validCuratorAbsolutePath(state) || !validCuratorAbsolutePath(cache) {
		return CuratorMuseHomes{}, curatorMalformed("muse XDG values must be absolute paths")
	}
	parent := path.Dir(config)
	// path.Dir returns a Clean result and config above proved absolute, so
	// the parent is absolute and clean by construction; the only degenerate
	// parent the exact comparison below would otherwise accept is root.
	if parent == "/" {
		return CuratorMuseHomes{}, curatorMalformed("muse XDG home parent must not be the filesystem root")
	}
	// Exact canonical comparison, mirroring the rc.4 emitter's CheckBoundary
	// (Join(parent, leaf) per HomeVariables entry): Base strips trailing
	// slashes while Dir cleans, so a Base/Dir spelling admits sets like
	// XDG_CONFIG_HOME=/a/config/ with the rest under /a/config. Every value
	// must equal Join(parent, leaf) byte for byte, which additionally
	// refuses trailing slashes, dot segments and double slashes.
	if config != path.Join(parent, "config") || data != path.Join(parent, "data") || state != path.Join(parent, "state") || cache != path.Join(parent, "cache") {
		return CuratorMuseHomes{}, curatorMalformed("muse XDG values must equal <home>/config, <home>/data, <home>/state and <home>/cache")
	}
	return CuratorMuseHomes{Parent: parent, XDGConfigHome: config, XDGDataHome: data, XDGStateHome: state, XDGCacheHome: cache}, nil
}

// CuratorMuseHomesFromV4Env validates a v4 muse fragment env: exactly the
// four XDG variables of CuratorMuseHomesFromEnv plus the registry constant
// TBH_CREDENTIAL_BACKEND=file, and nothing else. The XDG shape keeps its one
// implementation and its diagnostics: everything but the constant is handed
// to CuratorMuseHomesFromEnv, and the constant, which is not a path, is
// checked last.
func CuratorMuseHomesFromV4Env(env map[string]string) (CuratorMuseHomes, error) {
	xdg := make(map[string]string, len(env))
	for name, value := range env {
		if name != CuratorMuseCredentialBackendVariable {
			xdg[name] = value
		}
	}
	var homes CuratorMuseHomes
	if parsed, err := CuratorMuseHomesFromEnv(xdg); err != nil {
		return CuratorMuseHomes{}, err
	} else {
		homes = parsed
	}
	if backend, present := env[CuratorMuseCredentialBackendVariable]; !present || backend != CuratorMuseCredentialBackendFile {
		return CuratorMuseHomes{}, curatorMalformed("v4 muse fragments require TBH_CREDENTIAL_BACKEND=file")
	}
	return homes, nil
}

// MuseHomes returns the typed muse XDG home set for a v4 muse fragment. It
// fails closed on a nil receiver, a non-muse environment, any revision but
// v4 and any env that CuratorMuseHomesFromV4Env refuses, so a caller holding
// only the fragment derives the launch homes through the same shape the
// validator enforces.
func (c *CuratorContext) MuseHomes() (CuratorMuseHomes, error) {
	if c == nil || !CuratorFragmentUsesXDGHomes(c.Environment) || c.Revision != CuratorLaunchFragmentV4 {
		return CuratorMuseHomes{}, curatorMalformed("muse home set requires a v4 muse fragment")
	}
	if homes, err := CuratorMuseHomesFromV4Env(c.Env); err != nil {
		return CuratorMuseHomes{}, err
	} else {
		return homes, nil
	}
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
	if !acceptedCuratorFragmentRevision(context.Revision) {
		return fmt.Errorf("%w: %q", ErrCuratorFragmentRevisionUnsupported, context.Revision)
	}
	if context.Revision == CuratorLaunchFragmentV1 && context.Permissions != nil {
		return curatorMalformed("v1 fragments cannot carry the v2 permissions member")
	}
	if context.Revision == CuratorLaunchFragmentV2 && context.Permissions == nil {
		return curatorMalformed("v2 fragments require the permissions member")
	}
	if context.Revision == CuratorLaunchFragmentV3 && context.Permissions == nil {
		return curatorMalformed("v3 fragments require the permissions member")
	}
	if context.Revision == CuratorLaunchFragmentV4 && context.Permissions == nil {
		return curatorMalformed("v4 fragments require the permissions member")
	}
	// Old readers reject the v4 members under an old token, and so does this
	// one: an addition carried under v1-v3 is invalid, never a silent upgrade.
	if context.Revision != CuratorLaunchFragmentV4 && context.CredentialStore != nil {
		return curatorMalformed("credential_store requires launch-env-fragment-v4")
	}
	if context.Revision != CuratorLaunchFragmentV4 && context.RootContext != nil {
		return curatorMalformed("root_context requires launch-env-fragment-v4")
	}
	if context.RootContext != nil && !context.RootContext.Exclusive {
		return curatorMalformed("root_context must be exactly {\"exclusive\":true}")
	}
	if context.Permissions != nil {
		if context.Permissions.Mode != CuratorPermissionNative && context.Permissions.Mode != CuratorPermissionYolo {
			return curatorMalformed("permissions mode must be native or yolo")
		}
		if context.Permissions.Source != CuratorPermissionSourceProfile && context.Permissions.Source != CuratorPermissionSourceGlobal &&
			context.Permissions.Source != CuratorPermissionSourceDefault {
			return curatorMalformed("permissions source must be profile, global, or default")
		}
		if context.Permissions.Locked != (context.Permissions.Source == CuratorPermissionSourceGlobal) {
			return curatorMalformed("permissions locked must be true exactly for the global source")
		}
		if context.Permissions.Mode == CuratorPermissionYolo && (context.Permissions.Locked ||
			context.Permissions.Source == CuratorPermissionSourceGlobal || context.Permissions.Source == CuratorPermissionSourceDefault) {
			return curatorMalformed("permissions yolo requires an unlocked profile source")
		}
	}
	// XDG-home environments are admitted through the registry-held
	// declaration, not a case spelling the environment: the muse spelling
	// collides with the system-id vocabulary the single-source guard scans.
	if !CuratorFragmentUsesXDGHomes(context.Environment) {
		switch context.Environment {
		case "claude_code", "codex_cli", "opencode", "pi":
		default:
			return curatorMalformed("environment is not a supported typed fragment value")
		}
	}
	// The v4 policy members are judged against a known environment only.
	if context.RootContext != nil && !CuratorFragmentUsesXDGHomes(context.Environment) {
		return curatorMalformed("root_context is admitted only on the muse environment")
	}
	if err := validateCuratorCredentialStore(context); err != nil {
		return err
	}
	if (context.Precedence.Winner != "higher-weight" && context.Precedence.Winner != "lower-weight") ||
		(context.Precedence.Placement != "winner-last" && context.Precedence.Placement != "winner-first") {
		return curatorMalformed("precedence is invalid")
	}
	if !CuratorFragmentUsesXDGHomes(context.Environment) {
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
	}
	// The path_prepend guard is common to every environment, muse included:
	// it stays ahead of the muse early return so a muse fragment carrying
	// the member is validated exactly like any other adapter instead of
	// being silently accepted. No adapter registers a root-bound launch
	// mapping, so an absolute value is refused as unsupported here.
	if context.PathPrepend != "" {
		if !validCuratorAbsolutePath(context.PathPrepend) {
			return curatorMalformed("path_prepend must be an absolute path")
		}
		return fmt.Errorf("%w: path_prepend has no registered root-bound launch mapping", ErrCuratorContextUnsupported)
	}
	if CuratorFragmentUsesXDGHomes(context.Environment) {
		// Positive match on the pre-v4 revisions rather than a != v4: the
		// revision gate above already refused every unknown token, so
		// reaching this line means v1 through v4, and each named member
		// keeps its own narrowing mutant killable.
		if context.Revision == CuratorLaunchFragmentV1 || context.Revision == CuratorLaunchFragmentV2 {
			return curatorMalformed("muse fragments require launch-env-fragment-v4")
		}
		// §10.2: a managed muse launch MUST use v4 and refuse v3 with upgrade
		// guidance, without retrying, inferring the missing policy or
		// synthesizing a v4 fact.
		if context.Revision == CuratorLaunchFragmentV3 {
			return fmt.Errorf("%w: managed Muse launches require %s, got %s; upgrade Curator to a release that emits %s",
				ErrCuratorFragmentRevisionUnsupported, CuratorLaunchFragmentV4, CuratorLaunchFragmentV3, CuratorLaunchFragmentV4)
		}
		if homes, err := CuratorMuseHomesFromV4Env(context.Env); err != nil {
			return err
		} else if requestHome != homes.Parent {
			return curatorMalformed("muse home parent must match LaunchRequest.Home")
		}
		if context.MCP != nil {
			return curatorMalformed("muse fragments cannot carry MCP context")
		}
		if context.SystemPrompt != nil {
			return curatorMalformed("muse fragments cannot carry system-prompt context")
		}
		return nil
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

// validateCuratorCredentialStore checks the v4 credential_store policy
// against its environment (curator-spec environments §10.2). Muse requires
// exactly {"selection":"file"}. codex_cli admits native with no overrides,
// or file/keychain with exactly the matching cli_auth_credentials_store
// override; applying that -c override is not implemented yet, so an
// agreeing non-native Codex policy refuses as unsupported rather than being
// dropped. Every other adapter admits only an absent or native policy.
func validateCuratorCredentialStore(context *CuratorContext) error {
	store := context.CredentialStore
	if CuratorFragmentUsesXDGHomes(context.Environment) {
		if context.Revision != CuratorLaunchFragmentV4 {
			return nil
		}
		if store == nil || store.Selection != CuratorCredentialStoreFile || store.ConfigOverrides != nil {
			return curatorMalformed("v4 muse fragments require credential_store exactly {\"selection\":\"file\"}")
		}
		return nil
	}
	if store == nil {
		return nil
	}
	switch store.Selection {
	case CuratorCredentialStoreNative:
		if store.ConfigOverrides != nil {
			return curatorMalformed("a native credential_store carries no config_overrides")
		}
		return nil
	case CuratorCredentialStoreFile, CuratorCredentialStoreKeychain:
	default:
		return curatorMalformed("credential_store selection must be native, file or keychain")
	}
	if context.Environment != "codex_cli" {
		return curatorMalformed("only codex_cli admits a non-native credential_store")
	}
	want := map[CuratorCredentialSelection]string{
		CuratorCredentialStoreFile:     "file",
		CuratorCredentialStoreKeychain: "keyring",
	}[store.Selection]
	if len(store.ConfigOverrides) != 1 || store.ConfigOverrides[curatorCodexCredentialStoreKey] != want {
		return curatorMalformed("codex_cli credential_store config_overrides must be exactly {\"cli_auth_credentials_store\":\"" + want + "\"}")
	}
	if !curatorCodexCredentialStoreApplied {
		return fmt.Errorf("%w: codex_cli credential_store %q is not applied by this launcher", ErrCuratorContextUnsupported, store.Selection)
	}
	return nil
}

// curatorCodexCredentialStoreApplied reports whether the Codex plugin applies
// a non-native credential_store as its -c cli_auth_credentials_store pair.
// Until that consumer lands (skill-project-management TASK-261016-fa4toe),
// an agreeing file or keychain policy refuses typed instead of launching
// without the override.
const curatorCodexCredentialStoreApplied = false

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
	if CuratorFragmentUsesXDGHomes(context.Environment) {
		// A pure projection of validated input, never a second validation:
		// ValidateCuratorContext proved the four-variable shape, so the
		// parent below is the same one the validator bound to the request.
		home = path.Dir(context.Env["XDG_CONFIG_HOME"])
	} else {
		for key, value := range context.Env {
			homeVariable, home = key, value
		}
	}
	fragment := CuratorFragmentIdentity{
		Revision: context.Revision, Environment: context.Environment,
		Profile: context.Profile, Precedence: context.Precedence,
		Env: cloneStringMap(context.Env), MCP: cloneCuratorMCP(context.MCP),
		PathPrepend:     context.PathPrepend,
		Permissions:     cloneCuratorPermissions(context.Permissions),
		CredentialStore: cloneCuratorCredentialStore(context.CredentialStore),
		RootContext:     cloneCuratorRootContext(context.RootContext),
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
	if CuratorFragmentUsesXDGHomes(context.Environment) {
		provenance.MuseHomes = &CuratorMuseHomes{
			Parent:        home,
			XDGConfigHome: context.Env["XDG_CONFIG_HOME"],
			XDGDataHome:   context.Env["XDG_DATA_HOME"],
			XDGStateHome:  context.Env["XDG_STATE_HOME"],
			XDGCacheHome:  context.Env["XDG_CACHE_HOME"],
		}
	}
	if context.SystemPrompt != nil {
		provenance.SystemPromptIntent = context.SystemPrompt.Intent
	}
	return provenance
}

func cloneCuratorProvenance(source CuratorContextProvenance) CuratorContextProvenance {
	copy := source
	if source.MuseHomes != nil {
		homes := *source.MuseHomes
		copy.MuseHomes = &homes
	}
	copy.Fragment.Env = cloneStringMap(source.Fragment.Env)
	copy.Fragment.MCP = cloneCuratorMCP(source.Fragment.MCP)
	copy.Fragment.Permissions = cloneCuratorPermissions(source.Fragment.Permissions)
	copy.Fragment.CredentialStore = cloneCuratorCredentialStore(source.Fragment.CredentialStore)
	copy.Fragment.RootContext = cloneCuratorRootContext(source.Fragment.RootContext)
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
	copy.Permissions = cloneCuratorPermissions(source.Permissions)
	copy.CredentialStore = cloneCuratorCredentialStore(source.CredentialStore)
	copy.RootContext = cloneCuratorRootContext(source.RootContext)
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

func cloneCuratorPermissions(source *CuratorPermissions) *CuratorPermissions {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func cloneCuratorCredentialStore(source *CuratorCredentialStore) *CuratorCredentialStore {
	if source == nil {
		return nil
	}
	return &CuratorCredentialStore{Selection: source.Selection, ConfigOverrides: cloneStringMap(source.ConfigOverrides)}
}

func cloneCuratorRootContext(source *CuratorRootContext) *CuratorRootContext {
	if source == nil {
		return nil
	}
	copy := *source
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

// acceptedCuratorFragmentRevision reports whether a fragment revision token
// is one the validator admits.
func acceptedCuratorFragmentRevision(revision string) bool {
	return revision == CuratorLaunchFragmentV1 || revision == CuratorLaunchFragmentV2 || revision == CuratorLaunchFragmentV3 ||
		revision == CuratorLaunchFragmentV4
}

// sameCuratorFragmentIdentity reports whether two fragment identities name
// the same launch identity. Left is the stored (expected) fragment, right
// the current one. A Revision-only difference is the SAME identity, but only
// when every other field is byte-equal: the return below is today's DeepEqual
// over all fields except Revision. The gate covers the STORED side only: the
// stored provenance is data, not a validated request, so an unaccepted
// stored revision reads as stale, and a stored XDG-homes (muse) fragment
// must be v4 — a stored v3 muse identity is stale, never reused. The current
// side needs no gate here because buildPlan validates the current context
// before identity ever runs: ValidateCuratorContext refuses unknown
// revisions and pre-v4 muse fragments first, so right is already
// validator-admitted.
func sameCuratorFragmentIdentity(left, right CuratorFragmentIdentity) bool {
	if !acceptedCuratorFragmentRevision(left.Revision) || (CuratorFragmentUsesXDGHomes(left.Environment) && left.Revision != CuratorLaunchFragmentV4) {
		return false
	}
	left.Revision = ""
	right.Revision = ""
	return reflect.DeepEqual(left, right)
}

func ValidateCuratorContextIdentity(expected CuratorContextProvenance, current CuratorContextProvenance) error {
	if expected.ProfileName != current.ProfileName || expected.LockSHA256 != current.LockSHA256 ||
		expected.ManagedHomeVariable != current.ManagedHomeVariable || expected.ManagedHome != current.ManagedHome ||
		expected.SystemPromptIntent != current.SystemPromptIntent ||
		!reflect.DeepEqual(expected.MuseHomes, current.MuseHomes) ||
		!sameCuratorFragmentIdentity(expected.Fragment, current.Fragment) {
		return ErrCuratorContextStale
	}
	return nil
}
