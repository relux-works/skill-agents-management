package codex

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/internal/gatewaybasis"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func gatewayPlan(t *testing.T, native []string) agentic.Plan {
	t.Helper()
	r := managedPrimaryRequest(t)
	r.ToolRelease = "0.164.0"
	r.NativeArgs = native
	return requireManagedPlan(t, r)
}

func gatewayBinding(t *testing.T, p agentic.Plan) agentic.CodexGatewayBinding {
	t.Helper()
	data, err := os.ReadFile(p.Binary)
	if err != nil {
		t.Fatal(err)
	}
	return agentic.CodexGatewayBinding{ProviderID: GatewayProviderID, BaseURL: "http://127.0.0.1:45678/chatgpt/backend-api/codex", HeaderName: "X-Netproxy-Token", TokenEnv: GatewayTokenEnv, WireAPI: "responses", RequiresOpenAIAuth: true, ToolRelease: "0.164.0", ExecutablePath: p.Binary, ExecutableSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
}

// Fresh and exact resume retain every original argument and native suffix;
// decoded TOML proves auth, transport and env-header values independently of
// the renderer. No native runtime or actual credentials are used here.
func TestGatewayFreshAndResume(t *testing.T) {
	for _, suffix := range [][]string{nil, {"resume", managedThread}, {"--no-alt-screen", "resume", managedThread}} {
		p := gatewayPlan(t, suffix)
		b := gatewayBinding(t, p)
		a, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{NativeArgs: slices.Clone(suffix), FragmentEnv: []string{"DISJOINT=yes"}})
		if err != nil {
			t.Fatal(err)
		}
		final, err := gatewayFinalizeFixture(a, b)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(final.Argv[4:], p.Argv) || final.ModelIdentity != p.ModelIdentity || final.PermissionMode != p.PermissionMode {
			t.Fatal("original admission changed")
		}
		var decoded map[string]any
		if err := toml.Unmarshal([]byte(final.Argv[1]+"\n"+final.Argv[3]), &decoded); err != nil {
			t.Fatal(err)
		}
		providers := decoded["model_providers"].(map[string]any)
		provider := providers[GatewayProviderID].(map[string]any)
		if decoded["model_provider"] != GatewayProviderID || provider["base_url"] != b.BaseURL || provider["requires_openai_auth"] != true || provider["supports_websockets"] != false || provider["wire_api"] != "responses" {
			t.Fatalf("wrong route contract: %#v", decoded)
		}
		if got := provider["env_http_headers"].(map[string]any)[b.HeaderName]; got != GatewayTokenEnv {
			t.Fatal("wrong header binding")
		}
		if err := final.VerifyCodexGatewayProcess(b.ExecutablePath, final.Argv, append(slices.Clone(final.Env), GatewayTokenEnv+"=synthetic-child-only")); err != nil {
			t.Fatal(err)
		}
		if _, err := gatewayFinalizeFixture(a, b); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
			t.Fatal("second finalization accepted", err)
		}
	}
}

// Binding fields cannot select a different provider, route, credential channel,
// transport, release or executable. Every row must yield the typed refusal.
func TestGatewayBindingRefusals(t *testing.T) {
	for name, change := range map[string]func(*agentic.CodexGatewayBinding){
		"provider":         func(b *agentic.CodexGatewayBinding) { b.ProviderID = "openai" },
		"remote-base":      func(b *agentic.CodexGatewayBinding) { b.BaseURL = "https://example.invalid/chatgpt/backend-api/codex" },
		"double-base":      func(b *agentic.CodexGatewayBinding) { b.BaseURL += "/backend-api/codex" },
		"query":            func(b *agentic.CodexGatewayBinding) { b.BaseURL += "?secret=bad" },
		"auth-header":      func(b *agentic.CodexGatewayBinding) { b.HeaderName = "Authorization" },
		"header-injection": func(b *agentic.CodexGatewayBinding) { b.HeaderName = "X-Netproxy-Token\nAuthorization" },
		"token-env":        func(b *agentic.CodexGatewayBinding) { b.TokenEnv = "OPENAI_API_KEY" },
		"websocket":        func(b *agentic.CodexGatewayBinding) { b.SupportsWebSockets = true },
		"no-auth":          func(b *agentic.CodexGatewayBinding) { b.RequiresOpenAIAuth = false },
		"wire":             func(b *agentic.CodexGatewayBinding) { b.WireAPI = "chat" },
		"release":          func(b *agentic.CodexGatewayBinding) { b.ToolRelease = "0.165.0" },
		"digest":           func(b *agentic.CodexGatewayBinding) { b.ExecutableSHA256 = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			p := gatewayPlan(t, nil)
			a, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			b := gatewayBinding(t, p)
			change(&b)
			if _, err := gatewayFinalizeFixture(a, b); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("wanted typed refusal, got %v", err)
			}
		})
	}
}

// Input aliases, unsupported native commands and mutable pre-admission plans
// refuse while adjacent fresh/resume cases succeed. Inputs are copied.
func TestGatewayAdmissionRefusals(t *testing.T) {
	for _, native := range [][]string{{"app-server"}, {"--remote", "ws://127.0.0.1:1234"}, {"resume", "--last"}, {"-c", `model_provider="other"`}, {"--profile=alternate"}, {"--config", `"model_providers"."curator-gateway".base_url="http://127.0.0.1:1"`}} {
		p := gatewayPlan(t, native)
		if _, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{NativeArgs: native}); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
			t.Fatalf("native selector accepted: %q %v", native, err)
		}
	}
	for _, overlay := range [][]string{{"OPENAI_BASE_URL=http://127.0.0.1:1"}, {"openai_base_url=http://127.0.0.1:1"}, {GatewayTokenEnv + "=synthetic"}, {"codex_home=/tmp/alias"}, {"X=one", "X=two"}} {
		p := gatewayPlan(t, nil)
		if _, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{PromptEnv: overlay}); err == nil {
			t.Fatal("overlay accepted")
		}
	}
	for _, names := range [][]string{{GatewayTokenEnv}, {strings.ToLower(GatewayTokenEnv)}, {"OPENAI_BASE_URL"}, {"CODEX_REMOTE_URL"}} {
		p := gatewayPlan(t, nil)
		if _, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{LookupNames: names}); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
			t.Fatal("reserved MCP lookup admitted", err)
		}
	}
	p := gatewayPlan(t, nil)
	p.ModelIdentity.Launched = "changed"
	if _, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{}); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatal("model mutation accepted", err)
	}
}

// The final checker rejects late argv, token alias/duplicate/missing, original
// executable and staged executable changes; no re-digest blesses new bytes.
func TestGatewayLateMutationRefusals(t *testing.T) {
	p := gatewayPlan(t, []string{"resume", managedThread})
	b := gatewayBinding(t, p)
	a, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{NativeArgs: []string{"resume", managedThread}})
	if err != nil {
		t.Fatal(err)
	}
	final, err := gatewayFinalizeFixture(a, b)
	if err != nil {
		t.Fatal(err)
	}
	valid := append(slices.Clone(final.Env), GatewayTokenEnv+"=synthetic")
	args := slices.Clone(final.Argv)
	args[len(args)-1] = "01a14c36-65d0-7d80-9218-d374ede83a8a"
	if err := final.VerifyCodexGatewayProcess(b.ExecutablePath, args, valid); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatal("resume mutation accepted", err)
	}
	for _, env := range [][]string{final.Env, append(slices.Clone(valid), GatewayTokenEnv+"=duplicate"), append(slices.Clone(final.Env), strings.ToLower(GatewayTokenEnv)+"=alias")} {
		if err := final.VerifyCodexGatewayProcess(b.ExecutablePath, final.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
			t.Fatal("token mutation accepted", err)
		}
	}
	if err := os.WriteFile(p.Binary, []byte("#!/bin/sh\nexit 9\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := final.VerifyCodexGatewayProcess(b.ExecutablePath, final.Argv, valid); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatal("changed executable accepted", err)
	}
}

// These pure runtime tests issue a fixture capability internally. Production
// issuance through the real limits store is tested in gatewayadmission.
func gatewayAdmitFixture(p agentic.Plan, in agentic.CodexGatewayInputs) (*agentic.CodexGatewayAdmission, error) {
	built := agentic.PlanWithEnvironment{Plan: p}
	candidate, err := agentic.SnapshotGatewayCandidate(built)
	if err != nil {
		return nil, err
	}
	basis, err := gatewaybasis.Issue(candidate)
	if err != nil {
		return nil, err
	}
	built.GatewayBasis = basis
	return agentic.AdmitCodexGateway(basis, built, in)
}
func gatewayFinalizeFixture(a *agentic.CodexGatewayAdmission, b agentic.CodexGatewayBinding) (agentic.Plan, error) {
	b.AttemptID = a.AttemptID()
	out, receipt, err := a.Finalize(b)
	if err != nil {
		return agentic.Plan{}, err
	}
	held, ok := receipt.BindingSnapshot()
	if !ok || held != b {
		return agentic.Plan{}, errors.New("receipt lost original attempt/binding")
	}
	return out.Plan, nil
}

// Canonical rendering refuses a different original suffix and unsupported mode;
// the ordinary fresh case beside each refusal remains renderable.
func TestGatewayRendererRefusesMismatch(t *testing.T) {
	r := managedPrimaryRequest(t)
	r.ToolRelease = "0.164.0"
	p := requireManagedPlan(t, r)
	b := gatewayBinding(t, p)
	g := (&System{}).CodexGatewayBasis(r, agentic.LaunchModeInteractive)
	if _, err := g.Render(b, p.Argv); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Render(b, append(slices.Clone(p.Argv), "different")); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatal("different original suffix rendered", err)
	}
	g = (&System{}).CodexGatewayBasis(r, agentic.LaunchModeExec)
	if _, err := g.Render(b, p.Argv); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatal("exec gateway rendered", err)
	}
}

// The environment predicate accepts documented credential/account selectors and
// exact token injection, but rejects malformed, aliased, duplicated or missing inputs.
func TestGatewayEnvironmentPredicate(t *testing.T) {
	g := gatewayPolicy{}
	for _, injected := range []bool{false, true} {
		env := []string{"OPENAI_API_KEY=synthetic", "OPENAI_ORG_ID=org", "OPENAI_PROJECT_ID=project", "CODEX_HOME=/private", "CODEX_MANAGED_BY_NPM=1", "DISJOINT=ok"}
		if injected {
			env = append(env, GatewayTokenEnv+"=synthetic")
		}
		if err := g.CheckEnvironment(env, injected); err != nil {
			t.Fatal(err)
		}
	}
	for _, env := range [][]string{{"malformed"}, {"=empty"}, {"X=line\nbreak"}, {"X=one", "x=two"}, {GatewayTokenEnv + "="}, {strings.ToLower(GatewayTokenEnv) + "=alias"}, {"OPENAI_BASE_URL=other"}, {"CODEX_REMOTE_URL=other"}, {"DISJOINT=ok"}} {
		if err := g.CheckEnvironment(env, true); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
			t.Fatal("invalid environment accepted", err)
		}
	}
}
