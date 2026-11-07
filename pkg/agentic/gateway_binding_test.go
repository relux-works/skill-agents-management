package agentic_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/gatewayadmission"
	"github.com/relux-works/skill-agents-management/pkg/providerlimits"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

func gatewaySuppressCanonical(t *testing.T, store *providerlimits.Store, home string) {
	t.Helper()
	const model = "gpt-6.1-sol"
	identity, err := providerlimits.IdentityFor("codex", home)
	if err != nil {
		t.Fatal(err)
	}
	obs, ok := providerlimits.ClassifyManaged(providerlimits.ProviderCodex, providerlimits.ManagedSignalUsageLimited).QuotaObservation(providerlimits.Evidence{Model: model})
	if !ok {
		t.Fatal("missing quota observation")
	}
	if err := store.Observe(identity, providerlimits.MustGroupTable().Group("codex", model), nil, obs); err != nil {
		t.Fatal(err)
	}
	if verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: "codex", Model: model, Home: home}); err != nil || verdict.Serviceable() {
		t.Fatalf("canonical suppression control: %+v, %v", verdict, err)
	}
}

func gatewayFinalizeControl(t *testing.T, p agentic.PlanWithEnvironment, in agentic.CodexGatewayInputs) (agentic.PlanWithEnvironment, agentic.RuntimeGatewayBindingV1) {
	t.Helper()
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, in)
	if err != nil {
		t.Fatal(err)
	}
	b := gatewayPolicyBinding(t, p)
	final, receipt, err := a.Finalize(b)
	if err != nil || receipt == nil {
		t.Fatalf("valid finalization: receipt=%v err=%v", receipt != nil, err)
	}
	if err := final.Plan.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
	if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")); err != nil {
		t.Fatal(err)
	}
	return final, b
}

// Initial selectors must agree with the explicit home; an absent selector is pinned before limits and remains independent of HOME defaults.
func TestGatewayConfigurationHomeBinding(t *testing.T) {
	for _, kind := range []string{"absent", "matching", "conflicting", "empty", "alias", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			req.Model = "gpt-6.1-sol"
			other := t.TempDir()
			gatewaySuppressCanonical(t, store, other)
			for _, entry := range req.Env {
				if home, ok := strings.CutPrefix(entry, "HOME="); ok {
					gatewaySuppressCanonical(t, store, filepath.Join(home, ".codex"))
				}
			}
			switch kind {
			case "matching", "duplicate":
				req.Env = append(req.Env, "CODEX_HOME="+req.Home)
				if kind == "duplicate" {
					req.Env = append(req.Env, "CODEX_HOME="+req.Home)
				}
			case "conflicting":
				req.Env = append(req.Env, "CODEX_HOME="+other)
			case "empty":
				req.Env = append(req.Env, "CODEX_HOME=")
			case "alias":
				req.Env = append(req.Env, "codex_home="+other)
			}
			originalEnv := slices.Clone(req.Env)
			p, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
			if !slices.Equal(req.Env, originalEnv) {
				t.Fatal("home pin mutated caller environment")
			}
			if kind != "absent" && kind != "matching" {
				if !errors.Is(err, agentic.ErrCodexGatewayRefused) || p.GatewayBasis != nil || p.Plan.Binary != "" {
					t.Fatalf("conflicting initial home issued authority: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			pin := "CODEX_HOME=" + req.Home
			if !slices.Contains(p.Plan.Env, pin) || !slices.Contains(p.OwnedEnv, pin) {
				t.Fatal("explicit home was not pinned and owned before admission")
			}
			gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{})
			gatewaySuppressCanonical(t, store, req.Home)
			if out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req); !errors.Is(err, agentic.ErrCodexGatewayRefused) || out.GatewayBasis != nil || out.Plan.Binary != "" {
				t.Fatalf("suppressed pinned home issued authority: %v", err)
			}
		})
	}
}

// Fragment and prompt selectors cannot change the admitted home, while identical selectors and disjoint overlays finalize successfully.
func TestGatewayConfigurationHomeOverlays(t *testing.T) {
	for _, layer := range []string{"fragment", "prompt"} {
		t.Run(layer, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			other := t.TempDir()
			gatewaySuppressCanonical(t, store, other)
			for _, entry := range []string{"CODEX_HOME=" + other, "CODEX_HOME=", "codex_home=" + req.Home} {
				in := agentic.CodexGatewayInputs{}
				if layer == "fragment" {
					in.FragmentEnv = []string{entry}
				} else {
					in.PromptEnv = []string{entry}
				}
				if a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, in); !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
					t.Fatalf("home-changing %s overlay admitted: %v", layer, err)
				}
			}
			gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{FragmentEnv: []string{"CODEX_HOME=" + req.Home}, PromptEnv: []string{"CODEX_HOME=" + req.Home, "DISJOINT=yes"}})
		})
	}
}

// Actual child injection cannot change or remove CODEX_HOME, even when the captured final plan is unchanged.
func TestGatewayConfigurationHomeFinalEnvironment(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	final, b := gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
	for _, value := range []string{t.TempDir(), ""} {
		env := agentic.SetEnvValue(slices.Clone(final.Plan.Env), "CODEX_HOME", value)
		env = append(env, b.TokenEnv+"=synthetic")
		if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
			t.Fatalf("changed actual home accepted: %v", err)
		}
	}
}

// Canonical, alias and normalized runtime spellings launch the same model and all refuse its recorded suppression, retaining the canonical verdict.
func TestGatewayResolvedLimitsIdentity(t *testing.T) {
	for _, runtime := range []vendorplugin.RuntimeID{"codex", "CODEX", " Codex "} {
		for _, model := range []vendorplugin.ModelID{"gpt-6.1-sol", "sol"} {
			t.Run(string(runtime)+"/"+string(model), func(t *testing.T) {
				req, store, _ := gatewayPolicyFixture(t)
				req.Runtime, req.Model = runtime, model
				p := gatewayPolicyBuild(t, req, store)
				if p.Plan.ModelIdentity.Launched != "gpt-6.1-sol" {
					t.Fatalf("unexpected resolved model: %+v", p.Plan.ModelIdentity)
				}
				gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{})
				gatewaySuppressCanonical(t, store, req.Home)
				expected, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: "codex", Model: "gpt-6.1-sol", Home: req.Home})
				if err != nil {
					t.Fatal(err)
				}
				out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
				var refusal *gatewayadmission.LimitsRefusalError
				if !errors.As(err, &refusal) || !errors.Is(err, agentic.ErrCodexGatewayRefused) || out.GatewayBasis != nil || out.Plan.Binary != "" {
					t.Fatalf("resolved identity escaped suppression: runtime=%q model=%q err=%v", runtime, model, err)
				}
				if !reflect.DeepEqual(refusal.Verdict, expected) {
					t.Fatalf("resolved refusal lost canonical verdict: %+v", refusal.Verdict)
				}
			})
		}
	}
}

// A fixture declaration running Codex under a different classified runtime reads that runtime's limits, independently of the system's same-home suppression.
func TestGatewayLimitsRuntimeDistinctFromSystem(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	registry := vendorplugin.NewRegistry(agentic.Default)
	vendor, ok := vendorplugin.Default.Lookup("openai")
	if !ok {
		t.Fatal("missing fixture vendor")
	}
	if err := registry.Register(vendor); err != nil {
		t.Fatal(err)
	}
	// This isolated declaration exercises identity separation; it does not run
	// either CLI or change the default registry's real runtime declarations.
	if err := registry.DeclareRuntime(vendorplugin.RuntimeDeclaration{ID: "claude", System: "codex", Vendor: "openai", Broker: vendorplugin.BrokerProvenance{Checked: []string{"test fixture"}, Found: "test fixture"}}); err != nil {
		t.Fatal(err)
	}
	req.Runtime, req.Model = "CLAUDE", "gpt-6.1-sol"
	gatewaySuppressCanonical(t, store, req.Home)
	p, err := gatewayadmission.Build(context.Background(), registry, store, req)
	if err != nil {
		t.Fatalf("system suppression was mistaken for runtime suppression: %v", err)
	}
	if p.Plan.System != "codex" {
		t.Fatal("fixture did not separate runtime from system")
	}
	gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{})
	identity, err := providerlimits.IdentityFor("claude", req.Home)
	if err != nil {
		t.Fatal(err)
	}
	obs, ok := providerlimits.ClassifyManaged(providerlimits.ProviderClaude, providerlimits.ManagedSignalUsageLimited).QuotaObservation(providerlimits.Evidence{Model: string(req.Model)})
	if !ok {
		t.Fatal("missing fixture runtime quota observation")
	}
	if err := store.Observe(identity, providerlimits.MustGroupTable().Group("claude", string(req.Model)), nil, obs); err != nil {
		t.Fatal(err)
	}
	if out, err := gatewayadmission.Build(context.Background(), registry, store, req); !errors.Is(err, agentic.ErrCodexGatewayRefused) || out.GatewayBasis != nil || out.Plan.Binary != "" {
		t.Fatalf("declared runtime suppression was ignored: %v", err)
	}
}
