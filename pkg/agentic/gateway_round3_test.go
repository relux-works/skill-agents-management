package agentic_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/gatewayadmission"
	"github.com/relux-works/skill-agents-management/pkg/providerlimits"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// An accepted initial home spelling retains its recorded suppression; the healthy launch is the control.
func TestReviewSelectorHomeSpellingSuppression(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	real := filepath.Join(t.TempDir(), "Account")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	alternate := filepath.Join(filepath.Dir(real), "ACCOUNT")
	x, err := os.Stat(real)
	if err != nil {
		t.Fatal(err)
	}
	y, err := os.Stat(alternate)
	if err != nil || !os.SameFile(x, y) {
		t.Skip("requires case aliasing filesystem")
	}
	req.Home, req.Model = real, "gpt-6.1-sol"
	req.Env = agentic.SetEnvValue(req.Env, "CODEX_HOME", alternate)
	gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
	gatewaySuppressCanonical(t, store, alternate)
	alt, err := providerlimits.IdentityFor("codex", alternate)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := providerlimits.IdentityFor("codex", real)
	if err != nil {
		t.Fatal(err)
	}
	if alt.Key == canon.Key {
		t.Fatal("fixture did not retain distinct state keys")
	}
	verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: "codex", Model: string(req.Model), Home: alternate})
	if err != nil || verdict.Serviceable() {
		t.Fatalf("suppression control: %+v %v", verdict, err)
	}
	out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
	if err != nil {
		if !errors.Is(err, agentic.ErrCodexGatewayRefused) {
			t.Fatal(err)
		}
		return
	}
	final, _ := gatewayFinalizeControl(t, out, agentic.CodexGatewayInputs{})
	t.Fatalf("suppressed selected home admitted: Home=%q selector=%q state=%s basis=%s receipt issued; both final verifiers pass; final CODEX_HOME=%q", real, alternate, verdict.State, out.GatewayBasis.ID(), final.Plan.Home)
}

// Each shipped alias suppression also refuses its canonical launch identity after a healthy control.
func TestReviewCanonicalModelAfterAliasSuppression(t *testing.T) {
	for _, alias := range []vendorplugin.ModelID{"sol", "astra", "luna"} {
		t.Run(string(alias), func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			req.Model = alias
			p := gatewayPolicyBuild(t, req, store)
			launched := p.Plan.ModelIdentity.Launched
			gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{})
			identity, err := providerlimits.IdentityFor("codex", req.Home)
			if err != nil {
				t.Fatal(err)
			}
			obs, ok := providerlimits.ClassifyManaged(providerlimits.ProviderCodex, providerlimits.ManagedSignalUsageLimited).QuotaObservation(providerlimits.Evidence{Model: string(req.Model)})
			if !ok {
				t.Fatal("observation")
			}
			if err := store.Observe(identity, providerlimits.MustGroupTable().Group("codex", string(req.Model)), nil, obs); err != nil {
				t.Fatal(err)
			}
			if _, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("suppressed alias control: %v", err)
			}
			req.Model = vendorplugin.ModelID(launched)
			out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
			if err != nil {
				if !errors.Is(err, agentic.ErrCodexGatewayRefused) {
					t.Fatal(err)
				}
				return
			}
			final, _ := gatewayFinalizeControl(t, out, agentic.CodexGatewayInputs{})
			t.Fatalf("changing only model spelling %s -> %s minted receipt for same launched model %s; both final verifiers pass", alias, launched, final.Plan.ModelIdentity.Launched)
		})
	}
}

// Both final verifiers reject supplied-plan field edits while the unchanged plan passes.
func TestReviewFinalPlanFields(t *testing.T) {
	cases := map[string]func(*agentic.Plan){
		"system":           func(p *agentic.Plan) { p.System = "claude-code" },
		"mode":             func(p *agentic.Plan) { p.Mode = agentic.LaunchModeExec },
		"binary":           func(p *agentic.Plan) { p.Binary += "-changed" },
		"argv":             func(p *agentic.Plan) { p.Argv = append(slices.Clone(p.Argv), "--remote=bad") },
		"env":              func(p *agentic.Plan) { p.Env = append(slices.Clone(p.Env), "UNADMITTED=value") },
		"stdin-attached":   func(p *agentic.Plan) { p.Stdin.Attached = true },
		"stdin-bytes":      func(p *agentic.Plan) { p.Stdin.Bytes = []byte("bad") },
		"workdir":          func(p *agentic.Plan) { p.WorkDir += "-changed" },
		"home":             func(p *agentic.Plan) { p.Home += "-changed" },
		"release":          func(p *agentic.Plan) { p.ToolRelease = "0.165.0" },
		"requested-model":  func(p *agentic.Plan) { p.ModelIdentity.Requested = "other" },
		"launched-model":   func(p *agentic.Plan) { p.ModelIdentity.Launched = "other" },
		"permission":       func(p *agentic.Plan) { p.PermissionMode = agentic.PermissionModeYolo },
		"carried-settings": func(p *agentic.Plan) { p.CarriedSettings = &agentic.LocalProviderSettingsStatus{Note: "invented"} },
		"provenance":       func(p *agentic.Plan) { p.Provenance.Model = "other" },
		"session": func(p *agentic.Plan) {
			if p.Session == nil {
				p.Session = &agentic.PlanSession{}
			} else {
				p.Session = nil
			}
		},
		"nodes": func(p *agentic.Plan) { p.Nodes = []agentic.PlanNode{{ID: "extra"}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			final, b := gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
			originalArgv := slices.Clone(final.Plan.Argv)
			mutate(&final.Plan)
			env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
			if err := final.Plan.VerifyBeforeExec(); err == nil {
				t.Error("changed supplied plan passed VerifyBeforeExec")
			}
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, originalArgv, env); err == nil {
				t.Error("changed supplied plan passed VerifyCodexGatewayProcess")
			}
		})
	}
}

// Both final verifiers reject supplied-plan metadata and process edits beside the unchanged control.
func TestProbeControlFinalVerifiersCheckSuppliedPlan(t *testing.T) {
	for name, edit := range map[string]func(*agentic.Plan){
		"env":        func(p *agentic.Plan) { p.Env = append(slices.Clone(p.Env), "OTHER=x") },
		"env-value":  func(p *agentic.Plan) { p.Env = agentic.SetEnvValue(slices.Clone(p.Env), "CODEX_HOME", "/elsewhere") },
		"argv":       func(p *agentic.Plan) { p.Argv = append(slices.Clone(p.Argv), "--remote=x") },
		"binary":     func(p *agentic.Plan) { p.Binary = "/elsewhere/codex" },
		"session":    func(p *agentic.Plan) { p.Session = &agentic.PlanSession{} },
		"carried":    func(p *agentic.Plan) { p.CarriedSettings = &agentic.LocalProviderSettingsStatus{Note: "forged"} },
		"workdir":    func(p *agentic.Plan) { p.WorkDir = "/elsewhere" },
		"home":       func(p *agentic.Plan) { p.Home = "/elsewhere" },
		"model":      func(p *agentic.Plan) { p.ModelIdentity.Launched = "other" },
		"permission": func(p *agentic.Plan) { p.PermissionMode = agentic.PermissionModeYolo },
		"mode":       func(p *agentic.Plan) { p.Mode = agentic.LaunchModeExec },
		"system":     func(p *agentic.Plan) { p.System = "claude" },
		"release":    func(p *agentic.Plan) { p.ToolRelease = "0.165.0" },
		"provenance": func(p *agentic.Plan) { p.Provenance.Model = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			final, b := gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
			env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
			argv := slices.Clone(final.Plan.Argv)
			edited := final.Plan
			edit(&edited)
			if err := edited.VerifyBeforeExec(); !errors.Is(err, agentic.ErrCodexGatewayRefused) && !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Errorf("edited final plan passed VerifyBeforeExec: %v", err)
			}
			if err := edited.VerifyCodexGatewayProcess(b.ExecutablePath, argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) && !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Errorf("edited final plan passed VerifyCodexGatewayProcess: %v", err)
			}
		})
	}
}

// Editing the caller fragment slice after Admit cannot change the owned home in the final plan.
func TestProbeControlAdmitInputsAreCopied(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	other := t.TempDir()
	fragment := []string{"CODEX_HOME=" + req.Home}
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{FragmentEnv: fragment})
	if err != nil {
		t.Fatal(err)
	}
	fragment[0] = "CODEX_HOME=" + other
	final, receipt, err := a.Finalize(gatewayPolicyBinding(t, p))
	if err != nil || receipt == nil {
		t.Fatalf("control: %v", err)
	}
	if slices.Contains(final.Plan.Env, "CODEX_HOME="+other) || !slices.Contains(final.Plan.Env, "CODEX_HOME="+req.Home) {
		t.Fatalf("WRONG HOME: post-admission edit reached the final plan: Home=%q env selector moved to %q", final.Plan.Home, other)
	}
}

func gatewayMCPNameRequest(req *vendorplugin.SpawnRequest, kind, name string) {
	if kind == "descriptor" {
		req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextMCPServers, MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{{
			Name: "external", Transport: agentic.MCPTransportHTTP, URL: "https://mcp.example.invalid/mcp", BearerTokenEnvVar: name,
		}}}}}
		return
	}
	req.Context = &agentic.CuratorContext{
		Revision: agentic.CuratorLaunchFragmentV1, Environment: "codex_cli",
		Profile:    agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
		Precedence: agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env:        map[string]string{"CODEX_HOME": req.Home},
		MCP:        &agentic.CuratorMCPContext{Path: filepath.Join(req.Home, "mcp.json"), EnvNames: []string{name}, Channels: []agentic.CuratorChannelDescriptor{{Kind: agentic.CuratorDescriptorFlag, Flag: "-p", Argument: agentic.CuratorArgumentName, Name: "curator-mcp"}}},
	}
}

// Both plan MCP inputs refuse reserved names at admission; ordinary and predicate-allowed names finalize and verify.
func TestGatewayPlanMCPNames(t *testing.T) {
	for _, kind := range []string{"descriptor", "fragment"} {
		for _, name := range []string{"EXTERNAL_MCP_TOKEN", "OPENAI_API_KEY", "OPENAI_ORG_ID", "OPENAI_PROJECT_ID", "CODEX_HOME", "CODEX_MANAGED_BY_NPM", "CODEX_MANAGED_BY_BUN", "CURATOR_CODEX_GATEWAY_TOKEN", "curator_codex_gateway_token", "OPENAI_BASE_URL", "openai_base_url", "OPENAI_FUTURE_SELECTOR", "CODEX_REMOTE_URL", "codex_remote_url", "CODEX_FUTURE_SELECTOR"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				req, store, _ := gatewayPolicyFixture(t)
				gatewayMCPNameRequest(&req, kind, name)
				p := gatewayPolicyBuild(t, req, store)
				allowed := slices.Contains([]string{"EXTERNAL_MCP_TOKEN", "OPENAI_API_KEY", "OPENAI_ORG_ID", "OPENAI_PROJECT_ID", "CODEX_HOME", "CODEX_MANAGED_BY_NPM", "CODEX_MANAGED_BY_BUN"}, name)
				if allowed {
					gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{FragmentEnv: []string{"DISJOINT=yes"}})
					return
				}
				a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
				if !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
					t.Fatalf("reserved plan MCP name admitted: %v", err)
				}
			})
		}
	}
}
