package agentic_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func probeWholePath(t *testing.T, p agentic.PlanWithEnvironment, in agentic.CodexGatewayInputs) (agentic.PlanWithEnvironment, agentic.RuntimeGatewayBindingV1, error) {
	t.Helper()
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, in)
	if err != nil {
		return agentic.PlanWithEnvironment{}, agentic.RuntimeGatewayBindingV1{}, err
	}
	b := gatewayPolicyBinding(t, p)
	final, receipt, err := a.Finalize(b)
	if err != nil {
		return agentic.PlanWithEnvironment{}, b, err
	}
	if receipt == nil {
		t.Fatal("nil receipt")
	}
	if err := final.Plan.VerifyBeforeExec(); err != nil {
		return final, b, err
	}
	env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
	return final, b, final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env)
}

// Proxy selectors refuse through base, fragment and prompt inputs even with loopback NO_PROXY; no native agent runs.
func TestProbeProxySelectorReachesAdmittedChild(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		for _, route := range []string{"parent", "fragment", "prompt"} {
			for _, bypass := range []bool{false, true} {
				label := name + "/" + route + "/no-bypass"
				if bypass {
					label = name + "/" + route + "/loopback-bypass"
				}
				t.Run(label, func(t *testing.T) {
					req, store, _ := gatewayPolicyFixture(t)
					entry := name + "=http://proxy.example.invalid:3128"
					layer := []string{entry}
					if bypass {
						layer = append(layer, "NO_PROXY=127.0.0.1,::1,localhost")
					}
					in := agentic.CodexGatewayInputs{}
					switch route {
					case "parent":
						req.Env = append(req.Env, layer...)
					case "fragment":
						in.FragmentEnv = layer
					case "prompt":
						in.PromptEnv = layer
					}
					final, _, err := probeWholePath(t, gatewayPolicyBuild(t, req, store), in)
					if !errors.Is(err, agentic.ErrCodexGatewayRefused) {
						t.Fatalf("proxy selector %s via %s: want gateway refusal, got %v; final environment carries selector=%v", name, route, err, slices.Contains(final.Plan.Env, entry))
					}
				})
			}
		}
	}
}

// The sibling OPENAI_BASE_URL selector refuses while an ordinary overlay reaches both final verifiers on the same basis.
func TestProbeProxyControl(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	if a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{FragmentEnv: []string{"OPENAI_BASE_URL=http://x"}}); !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
		t.Fatalf("control: %v", err)
	}
	if _, _, err := probeWholePath(t, p, agentic.CodexGatewayInputs{FragmentEnv: []string{"PLAIN=yes"}}); err != nil {
		t.Fatalf("control: %v", err)
	}
}

// A caller prompt edit after admission cannot move the home selector in the final plan and receipt.
func TestProbePromptInputsAreCopied(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	other := t.TempDir()
	prompt := []string{"CODEX_HOME=" + req.Home}
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{PromptEnv: prompt})
	if err != nil {
		t.Fatal(err)
	}
	prompt[0] = "CODEX_HOME=" + other
	final, receipt, err := a.Finalize(gatewayPolicyBinding(t, p))
	if err != nil || receipt == nil {
		t.Fatalf("control: %v", err)
	}
	if slices.Contains(final.Plan.Env, "CODEX_HOME="+other) {
		t.Fatalf("WRONG HOME: post-admission prompt edit reached the final plan and receipt: Plan.Home=%q env selector=%q", final.Plan.Home, other)
	}
}
