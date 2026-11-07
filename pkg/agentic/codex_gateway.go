package agentic

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/gatewaybasis"
	"github.com/relux-works/skill-agents-management/pkg/agentic/internal/planningio"
)

// ErrCodexGatewayRefused names a refusal without rendering configuration or
// credential values. No refused operation returns a newly executable plan.
var ErrCodexGatewayRefused = errors.New("agentic: Codex gateway binding refused")

// GatewayAdmissionBasisV1 has no public constructor or writable fields. Only
// the module's current-policy admission boundary can issue a nonzero basis.
type GatewayAdmissionBasisV1 = gatewaybasis.Basis

// RuntimeGatewayBindingV1 contains nonsecret routing and executable identity.
// TokenEnv names the child-only variable; this API never takes its value.
type RuntimeGatewayBindingV1 struct {
	AttemptID                                     string
	ProviderID, BaseURL, HeaderName, TokenEnv     string
	WireAPI                                       string
	RequiresOpenAIAuth, SupportsWebSockets        bool
	ToolRelease, ExecutablePath, ExecutableSHA256 string
}

type CodexGatewayBinding = RuntimeGatewayBindingV1

// CodexGatewayInputs are the selected overlays and original native suffix.
type CodexGatewayInputs struct{ FragmentEnv, PromptEnv, NativeArgs, LookupNames []string }

// CodexGatewayPolicy is supplied by the owning plugin. Its renderer lives at
// the canonical Args construction site, not in the consumer or generic core.
type CodexGatewayPolicy interface {
	Admit(CodexGatewayInputs, []string) error
	Render(RuntimeGatewayBindingV1, []string) ([]string, error)
	CheckEnvironment([]string, bool) error
}
type CodexGatewayPlanner interface {
	CodexGatewayBasis(LaunchRequest, LaunchMode) CodexGatewayPolicy
}

// GatewayCandidateV1 is a detached data snapshot, NOT admission authority.
// Only the internal issuer can place it in a valid basis after current policy
// admits the launch. Constructing a candidate cannot mint a basis or receipt.
type GatewayCandidateV1 struct {
	built        PlanWithEnvironment
	original     RuntimeGatewayBindingV1
	identity     gatewayPlanIdentity
	homeIdentity os.FileInfo
}

func SnapshotGatewayCandidate(built PlanWithEnvironment) (GatewayCandidateV1, error) {
	p := built.Plan
	if p.codexGatewayPolicy == nil || p.baseProcess == nil || p.codexGatewayVerifier != nil || p.codexGatewayIdentity != gatewayIdentityOf(p) || len(p.Nodes) != 0 || p.Stdin.Attached || len(p.Stdin.Bytes) != 0 || !filepath.IsAbs(p.Binary) {
		return GatewayCandidateV1{}, ErrCodexGatewayRefused
	}
	if err := p.baseProcess.verify(p); err != nil {
		return GatewayCandidateV1{}, err
	}
	if err := p.VerifyBeforeExec(); err != nil {
		return GatewayCandidateV1{}, err
	}
	homeIdentity, err := planningio.Default.Stat(p.Home)
	if err != nil || !homeIdentity.IsDir() {
		return GatewayCandidateV1{}, ErrCodexGatewayRefused
	}
	var digest string
	if value, err := gatewayExecutableDigest(p.Binary); err != nil {
		return GatewayCandidateV1{}, err
	} else {
		digest = value
	}
	p.Argv, p.Env = slices.Clone(p.Argv), slices.Clone(p.Env)
	p.Session = cloneSession(p.Session)
	if p.CarriedSettings != nil {
		report := *p.CarriedSettings
		report.DroppedKeys = slices.Clone(report.DroppedKeys)
		p.CarriedSettings = &report
	}
	built.Plan = p
	built.OwnedEnv = slices.Clone(built.OwnedEnv)
	built.GatewayBasis = nil
	return GatewayCandidateV1{built: built, original: RuntimeGatewayBindingV1{ExecutablePath: p.Binary, ExecutableSHA256: digest}, identity: gatewayIdentityOf(p), homeIdentity: homeIdentity}, nil
}

func (c GatewayCandidateV1) verify(built PlanWithEnvironment) error {
	p := built.Plan
	if p.baseProcess != c.built.Plan.baseProcess || gatewayIdentityOf(p) != c.identity || p.Provenance != c.built.Plan.Provenance || !slices.Equal(built.OwnedEnv, c.built.OwnedEnv) || p.Stdin.Attached || len(p.Stdin.Bytes) != 0 || len(p.Nodes) != 0 {
		return ErrCodexGatewayRefused
	}
	if err := c.built.Plan.baseProcess.verify(p); err != nil {
		return err
	}
	if err := c.built.Plan.VerifyBeforeExec(); err != nil {
		return err
	}
	if current, err := planningio.Default.Stat(c.built.Plan.Home); err != nil || !os.SameFile(c.homeIdentity, current) {
		return ErrCodexGatewayRefused
	}
	return verifyGatewayExecutable(c.original)
}

type CodexGatewayAdmission struct {
	basis     *GatewayAdmissionBasisV1
	candidate GatewayCandidateV1
	inputs    CodexGatewayInputs
}

func (a *CodexGatewayAdmission) AttemptID() string {
	if a == nil {
		return ""
	}
	return a.basis.ID()
}

// AdmitCodexGateway requires authority from the original policy boundary and
// the very same request/plan. Neither a valid Plan nor its digest grants it.
func AdmitCodexGateway(basis *GatewayAdmissionBasisV1, built PlanWithEnvironment, in CodexGatewayInputs) (*CodexGatewayAdmission, error) {
	if basis == nil || built.GatewayBasis != basis {
		return nil, ErrCodexGatewayRefused
	}
	in = CodexGatewayInputs{FragmentEnv: slices.Clone(in.FragmentEnv), PromptEnv: slices.Clone(in.PromptEnv), NativeArgs: slices.Clone(in.NativeArgs), LookupNames: slices.Clone(in.LookupNames)}
	var candidate GatewayCandidateV1
	err := gatewaybasis.Inspect(basis, func(value any) error {
		c, ok := value.(GatewayCandidateV1)
		if !ok {
			return ErrCodexGatewayRefused
		}
		if err := c.verify(built); err != nil {
			return err
		}
		for _, layer := range [][]string{in.FragmentEnv, in.PromptEnv} {
			if _, err := validateFinalizeEnvOverlay(layer, "gateway"); err != nil {
				return ErrCodexGatewayRefused
			}
			for _, entry := range layer {
				n, v, _ := strings.Cut(entry, "=")
				for _, owned := range c.built.OwnedEnv {
					on, ov, _ := strings.Cut(owned, "=")
					if strings.EqualFold(n, on) && (n != on || v != ov) {
						return ErrCodexGatewayRefused
					}
				}
			}
		}
		if validator, ok := c.built.Plan.execVerifier.(ExecFinalizeOverlayValidator); ok {
			if err := validator.ValidateFinalizeOverlays(FinalizeOverlays{FragmentEnv: in.FragmentEnv, PromptEnv: in.PromptEnv}); err != nil {
				return err
			}
		}
		if err := c.built.Plan.codexGatewayPolicy.Admit(in, c.built.Plan.Env); err != nil {
			return err
		}
		candidate = c
		return nil
	})
	if err != nil {
		if errors.Is(err, gatewaybasis.ErrInvalid) {
			return nil, ErrCodexGatewayRefused
		}
		return nil, err
	}
	return &CodexGatewayAdmission{basis: basis, candidate: candidate, inputs: in}, nil
}

// GatewayAdmissionReceiptV1 links a successful finalization to its original
// attempt and immutable routing binding. It holds no token or auth payload.
type GatewayAdmissionReceiptV1 struct {
	attempt string
	binding RuntimeGatewayBindingV1
}

func (r *GatewayAdmissionReceiptV1) BindingSnapshot() (RuntimeGatewayBindingV1, bool) {
	if r == nil || r.attempt == "" {
		return RuntimeGatewayBindingV1{}, false
	}
	return r.binding, true
}

// Finalize consumes one original basis. Artifact checks use original verifiers
// and digests; a changed artifact never becomes a freshly accepted baseline.
func (a *CodexGatewayAdmission) Finalize(b RuntimeGatewayBindingV1) (PlanWithEnvironment, *GatewayAdmissionReceiptV1, error) {
	if a == nil {
		return PlanWithEnvironment{}, nil, ErrCodexGatewayRefused
	}
	var final PlanWithEnvironment
	var receipt *GatewayAdmissionReceiptV1
	err := gatewaybasis.Use(a.basis, func(value any) error {
		c, ok := value.(GatewayCandidateV1)
		if !ok || c.built.Plan.baseProcess != a.candidate.built.Plan.baseProcess {
			return ErrCodexGatewayRefused
		}
		if err := c.verify(c.built); err != nil {
			return err
		}
		if b.AttemptID != a.basis.ID() || b.ToolRelease != c.built.Plan.ToolRelease || b.ExecutableSHA256 != c.original.ExecutableSHA256 {
			return ErrCodexGatewayRefused
		}
		if err := verifyGatewayExecutable(b); err != nil {
			return err
		}
		var argv []string
		if rendered, err := c.built.Plan.codexGatewayPolicy.Render(b, c.built.Plan.Argv); err != nil {
			return err
		} else {
			argv = rendered
		}
		var env []string
		if composed, _, err := applyFinalizeOverlays(c.built.Plan.Env, FinalizeOverlays{FragmentEnv: a.inputs.FragmentEnv, PromptEnv: a.inputs.PromptEnv}); err != nil {
			return err
		} else {
			env = composed
		}
		if err := c.built.Plan.codexGatewayPolicy.CheckEnvironment(env, false); err != nil {
			return err
		}
		p := c.built.Plan
		p.Argv, p.Env = slices.Clone(argv), env
		if err := rederiveFinalSession(&p, true); err != nil {
			return err
		}
		v := &codexGatewayVerifier{candidate: c, binding: b, expected: snapshotProcess(p)}
		p.execVerifier, p.codexGatewayVerifier = v, v
		p.baseProcess = snapshotProcess(p)
		final = PlanWithEnvironment{Plan: p, OwnedEnv: slices.Clone(c.built.OwnedEnv)}
		receipt = &GatewayAdmissionReceiptV1{attempt: b.AttemptID, binding: b}
		return nil
	})
	if err != nil {
		if errors.Is(err, gatewaybasis.ErrInvalid) {
			return PlanWithEnvironment{}, nil, ErrCodexGatewayRefused
		}
		return PlanWithEnvironment{}, nil, err
	}
	return final, receipt, nil
}

type codexGatewayVerifier struct {
	candidate GatewayCandidateV1
	binding   RuntimeGatewayBindingV1
	expected  *processSnapshot
}

func (v *codexGatewayVerifier) VerifyBeforeExec(p Plan) error {
	if gatewayIdentityOf(p) != v.candidate.identity || p.Provenance != v.candidate.built.Plan.Provenance || p.Stdin.Attached || len(p.Stdin.Bytes) != 0 || len(p.Nodes) != 0 {
		return ErrCodexGatewayRefused
	}
	if err := v.expected.verify(p); err != nil {
		return err
	}
	return v.candidate.verify(v.candidate.built)
}

// VerifyCodexGatewayProcess verifies actual execution inputs after injection.
// The actual environment must equal the final plan plus exactly the private
// token; only its value remains the consumer's responsibility. Order may differ.
func (p Plan) VerifyCodexGatewayProcess(binary string, argv, env []string) error {
	v := p.codexGatewayVerifier
	if v == nil || binary != v.binding.ExecutablePath || !slices.Equal(argv, v.expected.argv) {
		return ErrCodexGatewayRefused
	}
	if err := v.VerifyBeforeExec(p); err != nil {
		return err
	}
	if err := verifyGatewayExecutable(v.binding); err != nil {
		return err
	}
	if err := v.candidate.built.Plan.codexGatewayPolicy.CheckEnvironment(env, true); err != nil {
		return err
	}
	actual := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if name != v.binding.TokenEnv {
			actual = append(actual, entry)
		}
	}
	expected := slices.Clone(p.Env)
	slices.Sort(actual)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		return ErrCodexGatewayRefused
	}

	return nil
}

func verifyGatewayExecutable(b RuntimeGatewayBindingV1) error {
	if len(b.ExecutableSHA256) != sha256.Size*2 || b.ExecutablePath == "" {
		return ErrCodexGatewayRefused
	}
	if digest, err := gatewayExecutableDigest(b.ExecutablePath); err != nil || digest != b.ExecutableSHA256 {
		return ErrCodexGatewayRefused
	}
	return nil
}
func gatewayExecutableDigest(path string) (string, error) {
	// Reject a determinately non-regular path before Open can block on a FIFO.
	// The descriptor is inspected again after open; pathname races remain bounded
	// by the caller's private staging and immediate final execution checks.
	if info, err := planningio.Default.Stat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > 512<<20 {
		return "", ErrCodexGatewayRefused
	}

	f, err := planningio.Default.Open(path)
	if err != nil {
		return "", ErrCodexGatewayRefused
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > 512<<20 {
		return "", ErrCodexGatewayRefused
	}
	h := sha256.New()
	if n, err := io.Copy(h, io.LimitReader(f, (512<<20)+1)); err != nil || n > 512<<20 {
		return "", ErrCodexGatewayRefused
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

type gatewayPlanIdentity struct {
	system                 SystemID
	mode                   LaunchMode
	workDir, home, release string
	model                  ModelIdentity
	permission             PermissionMode
}

func gatewayIdentityOf(p Plan) gatewayPlanIdentity {
	return gatewayPlanIdentity{p.System, p.Mode, p.WorkDir, p.Home, p.ToolRelease, p.ModelIdentity, p.PermissionMode}
}

func cloneCodexGatewayRequest(r LaunchRequest) LaunchRequest {
	r.NativeArgs = slices.Clone(r.NativeArgs)
	r.Env = slices.Clone(r.Env)
	r.Prompt = slices.Clone(r.Prompt)
	r.Context = cloneCuratorContext(r.Context)
	r.ContextDescriptors = slices.Clone(r.ContextDescriptors)
	for i, d := range r.ContextDescriptors {
		if d.MCP != nil {
			m := *d.MCP
			m.Servers = slices.Clone(m.Servers)
			for j := range m.Servers {
				m.Servers[j].Args = slices.Clone(m.Servers[j].Args)
			}
			r.ContextDescriptors[i].MCP = &m
		}
		if d.SystemPrompt != nil {
			p := *d.SystemPrompt
			r.ContextDescriptors[i].SystemPrompt = &p
		}
		if d.Permission != nil {
			p := *d.Permission
			r.ContextDescriptors[i].Permission = &p
		}
	}
	r.AdditionalWritableRoots = slices.Clone(r.AdditionalWritableRoots)
	if r.ManagerToolDir != nil {
		v := *r.ManagerToolDir
		r.ManagerToolDir = &v
	}
	if r.TempDir != nil {
		v := *r.TempDir
		r.TempDir = &v
	}
	return r
}
