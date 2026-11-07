package agentic_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/openai"
)

func gatewayPolicyFixture(t *testing.T) (vendorplugin.SpawnRequest, *providerlimits.Store, providerlimits.Layout) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	layout := providerlimits.LayoutAt(t.TempDir())
	store, err := providerlimits.NewStore(providerlimits.Options{Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	req := vendorplugin.SpawnRequest{Runtime: "codex", Model: "sol", Effort: "high", ToolRelease: "0.164.0", Home: t.TempDir(), WorkDir: t.TempDir(), Env: []string{"PATH=" + bin, "HOME=" + t.TempDir()}}
	tempDir := t.TempDir()
	req.TempDir = &tempDir
	return req, store, layout
}
func gatewayPolicyBuild(t *testing.T, req vendorplugin.SpawnRequest, store *providerlimits.Store) agentic.PlanWithEnvironment {
	t.Helper()
	p, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func gatewayPolicyBinding(t *testing.T, p agentic.PlanWithEnvironment) agentic.RuntimeGatewayBindingV1 {
	t.Helper()
	data, err := os.ReadFile(p.Plan.Binary)
	if err != nil {
		t.Fatal(err)
	}
	return agentic.RuntimeGatewayBindingV1{AttemptID: p.GatewayBasis.ID(), ProviderID: "curator-gateway", BaseURL: "http://[::1]:45678/chatgpt/backend-api/codex", HeaderName: "X-Netproxy-Token", TokenEnv: "CURATOR_CODEX_GATEWAY_TOKEN", WireAPI: "responses", RequiresOpenAIAuth: true, ToolRelease: "0.164.0", ExecutablePath: p.Plan.Binary, ExecutableSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
}

// Real vendor admission and the real limits store issue a capability and receipt
// for this exact plan. The stub executable is never launched: these assertions
// establish policy provenance and process projection, not native transport.
func TestGatewayPolicyBoundaryIssuesBasisAndReceipt(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	if p.GatewayBasis == nil || p.GatewayBasis.ID() == "" {
		t.Fatal("missing original policy basis")
	}
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
	if err != nil {
		t.Fatal(err)
	}
	final, receipt, err := a.Finalize(gatewayPolicyBinding(t, p))
	if err != nil {
		t.Fatal(err)
	}
	b, ok := receipt.BindingSnapshot()
	if !ok || b.AttemptID != p.GatewayBasis.ID() {
		t.Fatal("receipt lost original admission")
	}
	if !slices.Equal(final.Plan.Argv[4:], p.Plan.Argv) || final.Plan.ModelIdentity != p.Plan.ModelIdentity {
		t.Fatal("admission identity changed")
	}
	if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")); err != nil {
		t.Fatal(err)
	}
}

// A generic vendor build and even a public data snapshot cannot issue authority;
// zero/deserialized/foreign and different-request bases yield no admission.
func TestGatewayMissingForgedAndForeignBasis(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	plain, err := vendorplugin.BuildLaunchWithEnvironment(context.Background(), vendorplugin.Default, req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if plain.GatewayBasis != nil {
		t.Fatal("expressibility minted policy authority")
	}
	if _, err := agentic.SnapshotGatewayCandidate(plain); err != nil {
		t.Fatal(err)
	}
	zero := new(agentic.GatewayAdmissionBasisV1)
	if err := json.Unmarshal([]byte(`{"id":"forged","used":false}`), zero); err != nil {
		t.Fatal(err)
	}
	for _, basis := range []*agentic.GatewayAdmissionBasisV1{nil, zero} {
		plain.GatewayBasis = basis
		if a, err := agentic.AdmitCodexGateway(basis, plain, agentic.CodexGatewayInputs{}); !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
			t.Fatal("missing/forged basis admitted", err)
		}
	}
	first, second := gatewayPolicyBuild(t, req, store), gatewayPolicyBuild(t, req, store)
	second.GatewayBasis = first.GatewayBasis
	if a, err := agentic.AdmitCodexGateway(first.GatewayBasis, second, agentic.CodexGatewayInputs{}); !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
		t.Fatal("basis from identical but different request admitted", err)
	}
	first.Plan.Argv = append(first.Plan.Argv, "--remote=ws://127.0.0.1:1")
	if a, err := agentic.AdmitCodexGateway(first.GatewayBasis, first, agentic.CodexGatewayInputs{}); err == nil || a != nil {
		t.Fatal("mutated original plan admitted")
	}
}

// Two handles share one capability. Finalizing either prevents another process
// from being admitted with the same registration attempt.
func TestGatewayBasisCannotBeReusedThroughAnotherHandle(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Finalize(gatewayPolicyBinding(t, p)); err != nil {
		t.Fatal(err)
	}
	if out, receipt, err := b.Finalize(gatewayPolicyBinding(t, p)); !errors.Is(err, agentic.ErrCodexGatewayRefused) || receipt != nil || out.Plan.Binary != "" {
		t.Fatal("consumed basis admitted again", err)
	}
}

// Copying the public basis value cannot mint a second receipt for the same
// attempt, in either finalization order. The real policy/store are exercised;
// the inert executable is never launched and no registration is performed.
func TestGatewayBasisValueCopyCannotMintSecondReceipt(t *testing.T) {
	for _, copyFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "original-first", true: "copy-first"}[copyFirst], func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			copied := *p.GatewayBasis
			copyPlan := p
			copyPlan.GatewayBasis = &copied
			originalAdmission, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			copyAdmission, err := agentic.AdmitCodexGateway(&copied, copyPlan, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			first, second := originalAdmission, copyAdmission
			if copyFirst {
				first, second = second, first
			}
			binding := gatewayPolicyBinding(t, p)
			out, receipt, err := first.Finalize(binding)
			if err != nil || receipt == nil || out.Plan.Binary == "" {
				t.Fatalf("valid first finalization: receipt=%v error=%v", receipt != nil, err)
			}
			if out, receipt, err := second.Finalize(binding); !errors.Is(err, agentic.ErrCodexGatewayRefused) || receipt != nil || out.Plan.Binary != "" {
				t.Fatalf("copied consumed basis minted output: binary=%q receipt=%v error=%v", out.Plan.Binary, receipt != nil, err)
			}
			for _, plan := range []agentic.PlanWithEnvironment{p, copyPlan} {
				if a, err := agentic.AdmitCodexGateway(plan.GatewayBasis, plan, agentic.CodexGatewayInputs{}); !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
					t.Fatalf("consumed copy readmitted: %v", err)
				}
			}
		})
	}
}

// A late executable change invalidates the original basis; a failed store read
// cannot issue one. Both failures leave no newly executable plan or receipt.
func TestGatewayDriftAndUnknownLimitsRefuse(t *testing.T) {
	req, store, layout := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	b := gatewayPolicyBinding(t, p)
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Plan.Binary, []byte("#!/bin/sh\nexit 9\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, receipt, err := a.Finalize(b); !errors.Is(err, agentic.ErrCodexGatewayRefused) || out.Plan.Binary != "" || receipt != nil {
		t.Fatal("drift admitted", err)
	}
	id, err := providerlimits.IdentityFor("codex", req.Home)
	if err != nil {
		t.Fatal(err)
	}
	path := layout.StateFile(id.Key)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req); err == nil || out.GatewayBasis != nil || out.Plan.Binary != "" {
		t.Fatal("unknown limits minted admission", err)
	}
}

// Original-plan shape and identity mutations cannot produce a candidate or
// borrow an issued basis. A valid sibling is admitted first for every row.
func TestGatewayOriginalPlanRefusals(t *testing.T) {
	cases := map[string]func(*agentic.PlanWithEnvironment){
		"system":      func(p *agentic.PlanWithEnvironment) { p.Plan.System = "claude-code" },
		"mode":        func(p *agentic.PlanWithEnvironment) { p.Plan.Mode = agentic.LaunchModeExec },
		"workdir":     func(p *agentic.PlanWithEnvironment) { p.Plan.WorkDir = t.TempDir() },
		"home":        func(p *agentic.PlanWithEnvironment) { p.Plan.Home = t.TempDir() },
		"release":     func(p *agentic.PlanWithEnvironment) { p.Plan.ToolRelease = "0.165.0" },
		"model":       func(p *agentic.PlanWithEnvironment) { p.Plan.ModelIdentity.Launched = "other" },
		"permissions": func(p *agentic.PlanWithEnvironment) { p.Plan.PermissionMode = agentic.PermissionModeYolo },
		"binary":      func(p *agentic.PlanWithEnvironment) { p.Plan.Binary = "relative-codex" },
		"stdin":       func(p *agentic.PlanWithEnvironment) { p.Plan.Stdin.Attached = true },
		"stdin-bytes": func(p *agentic.PlanWithEnvironment) { p.Plan.Stdin.Bytes = []byte("unexpected") },
		"argv":        func(p *agentic.PlanWithEnvironment) { p.Plan.Argv = append(p.Plan.Argv, "--remote=x") },
		"environment": func(p *agentic.PlanWithEnvironment) { p.Plan.Env = append(p.Plan.Env, "OTHER=x") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			if _, err := agentic.SnapshotGatewayCandidate(p); err != nil {
				t.Fatal(err)
			}
			change(&p)
			if _, err := agentic.SnapshotGatewayCandidate(p); err == nil {
				t.Fatal("mutated original candidate accepted")
			}
			if a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{}); err == nil || a != nil {
				t.Fatal("mutated original basis accepted")
			}
		})
	}
}

// Source executable changes are observed before admission and finalization;
// no digest refresh turns changed, missing or oversized files into authority.
func TestGatewayExecutableRefusals(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "nonexecutable", "changed", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			b := gatewayPolicyBinding(t, p)
			switch kind {
			case "missing":
				err = os.Remove(p.Plan.Binary)
			case "directory":
				err = os.Remove(p.Plan.Binary)
				if err == nil {
					err = os.Mkdir(p.Plan.Binary, 0700)
				}
			case "nonexecutable":
				err = os.Chmod(p.Plan.Binary, 0600)
			case "changed":
				err = os.WriteFile(p.Plan.Binary, []byte("changed"), 0700)
			case "oversized":
				var f *os.File
				f, err = os.OpenFile(p.Plan.Binary, os.O_WRONLY, 0700)
				if err == nil {
					err = f.Truncate((512 << 20) + 1)
					f.Close()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := agentic.SnapshotGatewayCandidate(p); err == nil && kind != "changed" {
				t.Fatal("invalid executable data snapshot accepted")
			}
			// A data snapshot may describe changed regular bytes; it has no authority.
			// The originally issued basis must still refuse those bytes below.
			if _, receipt, err := a.Finalize(b); err == nil || receipt != nil {
				t.Fatal("changed executable finalized")
			}
		})
	}
}

// Malformed overlays and collisions with module-owned environment entries refuse
// before finalization. A clean overlay still uses the same unconsumed basis.
func TestGatewayOverlayRefusals(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	for _, overlay := range [][]string{{"missing-equals"}, {"=empty-name"}, {"X=one", "X=two"}, {"X=line\nbreak"}, {"OPENAI_BASE_URL=http://127.0.0.1:1"}} {
		if a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{FragmentEnv: overlay}); err == nil || a != nil {
			t.Fatal("invalid overlay admitted")
		}
	}
	for _, owned := range p.OwnedEnv {
		name, _, _ := strings.Cut(owned, "=")
		if a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{PromptEnv: []string{name + "=changed"}}); !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
			t.Fatal("owned environment collision admitted", err)
		}
	}
	if _, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{FragmentEnv: []string{"DISJOINT=valid"}, LookupNames: []string{"DISJOINT", "DISJOINT"}}); err != nil {
		t.Fatal(err)
	}
}

// Nil handles, stale attempt identity and invalid executable bindings return no
// plan or receipt; each refused finalization leaves the valid binding usable.
func TestGatewayFinalizationRefusals(t *testing.T) {
	var absent *agentic.CodexGatewayAdmission
	if out, receipt, err := absent.Finalize(agentic.RuntimeGatewayBindingV1{}); !errors.Is(err, agentic.ErrCodexGatewayRefused) || receipt != nil || out.Plan.Binary != "" {
		t.Fatal("nil admission accepted", err)
	}
	for name, change := range map[string]func(*agentic.RuntimeGatewayBindingV1){
		"attempt":      func(b *agentic.RuntimeGatewayBindingV1) { b.AttemptID = "another" },
		"release":      func(b *agentic.RuntimeGatewayBindingV1) { b.ToolRelease = "0.165.0" },
		"digest":       func(b *agentic.RuntimeGatewayBindingV1) { b.ExecutableSHA256 = "" },
		"missing-path": func(b *agentic.RuntimeGatewayBindingV1) { b.ExecutablePath = "" },
		"changed-path": func(b *agentic.RuntimeGatewayBindingV1) { b.ExecutablePath = filepath.Join(t.TempDir(), "absent") },
		"provider":     func(b *agentic.RuntimeGatewayBindingV1) { b.ProviderID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			valid := gatewayPolicyBinding(t, p)
			bad := valid
			change(&bad)
			if out, receipt, err := a.Finalize(bad); !errors.Is(err, agentic.ErrCodexGatewayRefused) || receipt != nil || out.Plan.Binary != "" {
				t.Fatal("invalid binding finalized", err)
			}
			if _, _, err := a.Finalize(valid); err != nil {
				t.Fatal("refusal consumed valid basis", err)
			}
		})
	}
}

// Actual final-process verification rejects changed metadata, argv, environment,
// original bytes and a distinct staged file. Each case first checks a valid process.
func TestGatewayFinalProcessRefusals(t *testing.T) {
	for _, kind := range []string{"identity", "provenance", "argv", "env", "owned-value", "owned-missing", "original-bytes", "staged-bytes"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			b := gatewayPolicyBinding(t, p)
			if kind == "staged-bytes" {
				data, err := os.ReadFile(p.Plan.Binary)
				if err != nil {
					t.Fatal(err)
				}
				b.ExecutablePath = filepath.Join(t.TempDir(), "staged")
				if err := os.WriteFile(b.ExecutablePath, data, 0700); err != nil {
					t.Fatal(err)
				}
			}
			final, _, err := a.Finalize(b)
			if err != nil {
				t.Fatal(err)
			}
			env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "identity":
				final.Plan.ToolRelease = "changed"
			case "provenance":
				final.Plan.Provenance.Model = "changed"
			case "argv":
				final.Plan.Argv = append(slices.Clone(final.Plan.Argv), "changed")
			case "env":
				env = append(env, "OTHER=changed")
			case "owned-value", "owned-missing":
				if len(final.OwnedEnv) == 0 {
					t.Fatal("fixture must own environment")
				}
				name, _, _ := strings.Cut(final.OwnedEnv[0], "=")
				found := false
				for i, e := range env {
					n, _, _ := strings.Cut(e, "=")
					if n == name {
						found = true
						if kind == "owned-value" {
							env[i] = name + "=changed"
						} else {
							env = append(env[:i:i], env[i+1:]...)
						}
						break
					}
				}
				if !found {
					t.Fatal("missing fixture owned name")
				}
			case "original-bytes":
				err = os.WriteFile(p.Plan.Binary, []byte("changed"), 0700)
			case "staged-bytes":
				err = os.WriteFile(b.ExecutablePath, []byte("changed"), 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); err == nil {
				t.Fatal("mutated final process accepted")
			}
		})
	}
}

// Authority also binds provenance and owned-environment metadata that is absent
// from a bare process snapshot. A sibling unchanged request remains admissible.
func TestGatewayAdmissionMetadataRefusals(t *testing.T) {
	for _, kind := range []string{"provenance", "owned"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			if _, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{}); err != nil {
				t.Fatal(err)
			}
			if kind == "provenance" {
				p.Plan.Provenance.Model = "changed"
			} else {
				p.OwnedEnv = append(slices.Clone(p.OwnedEnv), "OTHER=changed")
			}
			if a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{}); !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
				t.Fatal("changed metadata admitted", err)
			}
		})
	}
}

// A real recorded quota refusal retains its complete current verdict and issues
// no plan or basis. The same request without a record is a nearby valid control.
func TestGatewayLimitsRefusalPreservesVerdict(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	req.Model = "gpt-6.1-sol"
	_ = gatewayPolicyBuild(t, req, store)
	identity, err := providerlimits.IdentityFor("codex", req.Home)
	if err != nil {
		t.Fatal(err)
	}
	classification := providerlimits.ClassifyManaged(providerlimits.ProviderCodex, providerlimits.ManagedSignalUsageLimited)
	obs, ok := classification.QuotaObservation(providerlimits.Evidence{Model: string(req.Model)})
	if !ok {
		t.Fatal("missing quota observation")
	}
	group := providerlimits.MustGroupTable().Group("codex", string(req.Model))
	if err := store.Observe(identity, group, nil, obs); err != nil {
		t.Fatal(err)
	}
	expected, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: "codex", Model: string(req.Model), Home: req.Home})
	if err != nil || expected.Serviceable() {
		t.Fatal("missing refusal fixture", err)
	}
	out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
	var refused *gatewayadmission.LimitsRefusalError
	if !errors.As(err, &refused) || !errors.Is(err, agentic.ErrCodexGatewayRefused) || out.Plan.Binary != "" || out.GatewayBasis != nil {
		t.Fatal("lost refusal or issued authority", err)
	}
	if !reflect.DeepEqual(refused.Verdict, expected) {
		t.Fatal("limits verdict changed")
	}
}
