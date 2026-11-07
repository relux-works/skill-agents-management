package agentic_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
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
	for _, kind := range []string{"absent", "matching", "conflicting", "empty", "alias", "alias-matching", "duplicate"} {
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
			case "alias-matching":
				req.Env = append(req.Env, "codex_home="+req.Home)
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

// Both final verifiers reject attached stdin, detached bytes and a validated multi-node plan; each row first verifies the unchanged final plan.
func TestGatewayFinalPlanShapeRefusals(t *testing.T) {
	for _, kind := range []string{"stdin-attached", "stdin-bytes", "stdin-payload", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			final, b := gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
			env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
			switch kind {
			case "stdin-attached":
				final.Plan.Stdin.Attached = true
			case "stdin-bytes":
				final.Plan.Stdin.Bytes = []byte("unadmitted bytes")
			case "stdin-payload":
				final.Plan.Stdin = agentic.StdinPayload{Attached: true, Bytes: []byte("unadmitted prompt")}
			case "nodes":
				n := agentic.PlanNode{ID: "extra", Process: agentic.ProcessPlan{Mode: agentic.LaunchModeExec, Binary: final.Plan.Binary, Argv: []string{"sidecar"}, WorkDir: final.Plan.WorkDir, Home: final.Plan.Home}}
				n.Plugin.ID, n.Plugin.Kind = "extra", "sidecar"
				expanded, err := agentic.BuildMultiNodePlan(final.Plan, []agentic.PlanNodeID{"extra"}, n)
				if err != nil {
					t.Fatal(err)
				}
				if len(expanded.Nodes) != 2 || expanded.Nodes[0].ID != "extra" || expanded.Nodes[1].ID != agentic.PrimaryPlanNodeID {
					t.Fatal("fixture did not produce a validated dependency graph")
				}
				final.Plan = expanded
			}
			if err := final.Plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Errorf("unadmitted final %s passed VerifyBeforeExec: %v", kind, err)
			}
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Errorf("unadmitted final %s passed VerifyCodexGatewayProcess: %v", kind, err)
			}
		})
	}
}

// With a distinct unchanged staged executable, original-only drift refuses before finalization and execution; unchanged originals remain valid.
func TestGatewayDistinctStagedExecutableRetainsOriginalDigest(t *testing.T) {
	for _, phase := range []string{"unchanged", "before-finalize", "after-finalize"} {
		t.Run(phase, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			b := gatewayPolicyBinding(t, p)
			original, err := os.ReadFile(p.Plan.Binary)
			if err != nil {
				t.Fatal(err)
			}
			b.ExecutablePath = filepath.Join(t.TempDir(), "staged")
			if err := os.WriteFile(b.ExecutablePath, original, 0700); err != nil {
				t.Fatal(err)
			}
			changeOriginal := func() {
				t.Helper()
				if err := os.WriteFile(p.Plan.Binary, []byte("changed original only"), 0700); err != nil {
					t.Fatal(err)
				}
				staged, err := os.ReadFile(b.ExecutablePath)
				if err != nil || sha256.Sum256(staged) != sha256.Sum256(original) {
					t.Fatalf("staged control changed: %v", err)
				}
			}
			if phase == "before-finalize" {
				changeOriginal()
				if out, receipt, err := a.Finalize(b); !errors.Is(err, agentic.ErrCodexGatewayRefused) || receipt != nil || out.Plan.Binary != "" {
					t.Fatalf("original-only drift finalized with unchanged stage: receipt=%v err=%v", receipt != nil, err)
				}
				if err := os.WriteFile(p.Plan.Binary, original, 0700); err != nil {
					t.Fatal(err)
				}
			}
			final, receipt, err := a.Finalize(b)
			if err != nil || receipt == nil {
				t.Fatalf("unchanged stage/original refused or failed attempt consumed basis: %v", err)
			}
			env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); err != nil {
				t.Fatal(err)
			}
			if phase == "after-finalize" {
				changeOriginal()
				if err := final.Plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
					t.Errorf("original-only drift passed VerifyBeforeExec with unchanged stage: %v", err)
				}
				if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
					t.Errorf("original-only drift passed final process verification with unchanged stage: %v", err)
				}
			}
		})
	}
}
