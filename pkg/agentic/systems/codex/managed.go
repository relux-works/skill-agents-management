package codex

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func managedRefusal(reason string) error {
	return fmt.Errorf("%w: %s", agentic.ErrManagedLaunchInvalid, reason)
}

// Paths are literal authority: refuse rather than clean, expand or resolve them.
func managedAbsolutePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n") && utf8.ValidString(path)
}

// ValidateManagedLaunch is the pure pre-I/O admission point. Remote clients
// restore the thread's permission policy; writable roots belong to the primary.
func (*System) ValidateManagedLaunch(req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	if req.ManagerToolDir != nil && (!managedAbsolutePath(*req.ManagerToolDir) || strings.ContainsRune(*req.ManagerToolDir, os.PathListSeparator)) {
		return managedRefusal("manager tool directory must be one clean absolute path")
	}
	if len(req.AdditionalWritableRoots) > 0 {
		if mode != agentic.LaunchModeInteractive {
			return managedRefusal("writable roots require an interactive primary")
		}
		var context contextValues
		if value, err := buildContextValues(req, mode); err != nil {
			return err
		} else {
			context = value
		}
		if context.permission == agentic.PermissionModeYolo {
			return managedRefusal("writable roots cannot accompany a sandbox bypass")
		}
		for _, root := range req.AdditionalWritableRoots {
			if !managedAbsolutePath(root) {
				return managedRefusal("writable root must be a clean absolute path")
			}
		}
		// A primary may select app-server, but must not override the typed roots,
		// switch permission posture or insert another native configuration writer.
		if len(req.NativeArgs) != 0 && !slices.Equal(req.NativeArgs, []string{"app-server"}) {
			return managedRefusal("writable roots conflict with native arguments")
		}
	}
	if target := req.CodexRemoteClient; target != nil {
		if mode != agentic.LaunchModeInteractive {
			return managedRefusal("remote client requires interactive mode")
		}
		if err := agentic.ValidateResumeIntent(agentic.ResumeIntent{Kind: agentic.ResumeCodexThread, Identity: &target.ThreadID}, agentic.ResumeCodexThread); err != nil {
			return managedRefusal("remote client requires an exact native thread UUID")
		}
		if !validManagedEndpoint(target.Endpoint) {
			return managedRefusal("remote endpoint must be an absolute unix or ws/wss address without credentials")
		}
		if !managedAbsolutePath(req.Home) {
			return managedRefusal("remote client requires a private absolute home")
		}
		if !req.PermissionMode.IsZero() || len(req.ContextDescriptors) > 0 || req.Context != nil || !req.Composition.IsZero() || req.LocalProvider != nil || len(req.AdditionalWritableRoots) > 0 || !req.Network.IsZero() {
			return managedRefusal("remote client cannot override primary execution policy")
		}
		if strings.TrimSpace(req.Profile) != "" || strings.TrimSpace(req.ServiceTier) != "" {
			return managedRefusal("remote client cannot select a profile or service tier")
		}
		if err := validateManagedClientTail(req.NativeArgs); err != nil {
			return err
		}
	}
	return nil
}

func validManagedEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(raw, "\x00\r\n\t ") {
		return false
	}
	switch u.Scheme {
	case "unix":
		return u.Host == "" && managedAbsolutePath(u.Path)
	case "ws", "wss":
		return u.Hostname() != "" && u.Port() != ""
	default:
		return false
	}
}

// Only display and sleep-registration overrides may be mirrored. Config values
// must decode as exactly the requested key, so a newline cannot smuggle policy.
func validateManagedClientTail(tail []string) error {
	for i := 0; i < len(tail); i++ {
		switch tail[i] {
		case "--no-alt-screen":
			continue
		case "-c", "--config":
			i++
			if i == len(tail) || !managedDisplayOverride(tail[i]) {
				return managedRefusal("remote client config is not a display/sleep override")
			}
		default:
			if strings.HasPrefix(tail[i], "--config=") && managedDisplayOverride(strings.TrimPrefix(tail[i], "--config=")) {
				continue
			}
			return managedRefusal("remote client native selector or policy override is forbidden")
		}
	}
	return nil
}
func managedDisplayOverride(value string) bool {
	key, _, ok := strings.Cut(value, "=")
	if !ok {
		return false
	}
	key = strings.TrimSpace(key)
	switch key {
	case "model", "model_reasoning_effort", "features.sleep_tool.mode":
	default:
		return false
	}
	var parsed map[string]any
	if toml.Unmarshal([]byte(value), &parsed) != nil {
		return false
	}
	parts := strings.Split(key, ".")
	for _, part := range parts[:len(parts)-1] {
		if len(parsed) != 1 {
			return false
		}
		var ok bool
		parsed, ok = parsed[part].(map[string]any)
		if !ok {
			return false
		}
	}
	if len(parsed) != 1 {
		return false
	}
	_, ok = parsed[parts[len(parts)-1]].(string)
	return ok
}

const managedSealSelector = "codex-managed-plan"

// managedExecSeal wraps the existing catalog/TMPDIR guard without resealing its
// artifacts. The process projection holds exact argv and keyed env selectors.
type managedExecSeal struct {
	base      agentic.ExecPlanVerifier
	binary    string
	argv      []string
	env       map[string]string
	committed bool
}

func (*System) SealManagedLaunch(req agentic.LaunchRequest, p agentic.Plan, base agentic.ExecPlanVerifier) (agentic.ExecPlanVerifier, error) {
	seal := &managedExecSeal{base: base, binary: p.Binary, argv: slices.Clone(p.Argv), env: map[string]string{}}
	if req.ManagerToolDir != nil {
		seal.env["PATH"] = codexPolicyEnvValue(p.Env, "PATH")
	}
	if req.CodexRemoteClient != nil {
		seal.env["CODEX_HOME"] = codexPolicyEnvValue(p.Env, "CODEX_HOME")
	}
	return seal, nil
}
func (s *managedExecSeal) VerifyBeforeExec(p agentic.Plan) error {
	if p.Binary != s.binary || !slices.Equal(p.Argv, s.argv) {
		return managedRefusal("sealed managed argv changed")
	}
	for name, want := range s.env {
		count := 0
		value := ""
		for _, e := range p.Env {
			n, v, ok := strings.Cut(e, "=")
			if ok && n == name {
				count++
				value = v
			}
		}
		if s.committed {
			value = agentic.CommitSealLiteral(name, value)
		}
		if count != 1 || value != want {
			return managedRefusal("sealed managed environment changed")
		}
	}
	if s.base != nil {
		return s.base.VerifyBeforeExec(p)
	}
	return nil
}
func (s *managedExecSeal) VerifySealedArtifacts() error {
	if v, ok := s.base.(agentic.ExecArtifactVerifier); ok {
		return v.VerifySealedArtifacts()
	}
	return nil
}
func (s *managedExecSeal) ValidateFinalizeOverlays(o agentic.FinalizeOverlays) error {
	if len(o.NativeTail) > 0 || o.Reservation != nil {
		return managedRefusal("managed native tail and session reservations must be supplied before planning")
	}
	for _, entries := range [][]string{o.FragmentEnv, o.PromptEnv} {
		for _, e := range entries {
			name, _, _ := strings.Cut(e, "=")
			if _, bound := s.env[name]; bound {
				return managedRefusal("overlay replaces managed environment")
			}
		}
	}
	return nil
}
func (s *managedExecSeal) ExportSealedData() agentic.SealedData {
	data := agentic.SealedData{Binary: s.binary, Argv: slices.Clone(s.argv)}
	if v, ok := s.base.(agentic.ExecSealExporter); ok {
		data = v.ExportSealedData()
	}
	if data.Selectors == nil {
		data.Selectors = map[string]string{}
	}
	data.Selectors[managedSealSelector] = "v1"
	for name, value := range s.env {
		if !s.committed {
			value = agentic.CommitSealLiteral(name, value)
		}
		data.Selectors[name] = value
	}
	return data
}

func (s *System) importManagedSeal(data agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	if data.Selectors[managedSealSelector] != "v1" {
		return nil, managedRefusal("unknown managed seal revision")
	}
	selectors := map[string]string{}
	held := map[string]string{}
	for name, value := range data.Selectors {
		switch name {
		case managedSealSelector:
		case "PATH", "CODEX_HOME":
			if !isSealCommitment(value) {
				return nil, managedRefusal("malformed managed selector")
			}
			held[name] = value
		case TempDirEnv:
			selectors[name] = value
		default:
			return nil, managedRefusal("unknown managed selector")
		}
	}
	data.Selectors = selectors
	var base agentic.ExecPlanVerifier
	if len(data.Artifacts) > 0 || len(selectors) > 0 {
		if imported, err := s.ImportSealedData(data); err != nil {
			return nil, err
		} else {
			base = imported
		}
	}
	return &managedExecSeal{base: base, binary: data.Binary, argv: slices.Clone(data.Argv), env: held, committed: true}, nil
}
