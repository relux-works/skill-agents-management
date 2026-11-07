package agentic_test

import (
	"context"
	"crypto/sha256"
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
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/alibaba"
)

// Every directory component uses its on-disk spelling, and equivalent selectors select that canonical home.
func TestGatewayCanonicalHomeSpellings(t *testing.T) {
	for _, kind := range []string{"exact", "trailing", "dot", "symlink", "case", "nfd"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			parent := filepath.Join(t.TempDir(), "Parent")
			real := filepath.Join(parent, "café-home")
			if err := os.MkdirAll(real, 0700); err != nil {
				t.Fatal(err)
			}
			resolved, err := filepath.EvalSymlinks(real)
			if err != nil {
				t.Fatal(err)
			}
			real, parent = resolved, filepath.Dir(resolved)
			spelling := real
			switch kind {
			case "trailing":
				spelling += "/"
			case "dot":
				spelling += "/."
			case "symlink":
				spelling = filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(real, spelling); err != nil {
					t.Fatal(err)
				}
			case "case":
				spelling = filepath.Join(filepath.Dir(parent), "PARENT", "CAFÉ-HOME")
			case "nfd":
				spelling = filepath.Join(parent, "cafe\u0301-home")
			}
			a, err := os.Stat(real)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.Stat(spelling)
			if err != nil || !os.SameFile(a, b) {
				if kind == "case" || kind == "nfd" {
					t.Skip("filesystem does not alias this case or Unicode spelling")
				}
				t.Fatalf("fixture alias: %v", err)
			}
			req.Home = spelling
			// Both directions exercise initial selector identity independently of Home spelling.
			for _, selector := range []string{real, spelling} {
				req.Env = agentic.SetEnvValue(req.Env, "CODEX_HOME", selector)
				p := gatewayPolicyBuild(t, req, store)
				if p.Plan.Home != real || !slices.Contains(p.Plan.Env, "CODEX_HOME="+real) || !slices.Contains(p.OwnedEnv, "CODEX_HOME="+real) {
					t.Fatalf("noncanonical home: %q", p.Plan.Home)
				}
				gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{})
			}
			gatewaySuppressCanonical(t, store, real)
			out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
			var refusal *gatewayadmission.LimitsRefusalError
			if !errors.As(err, &refusal) || out.GatewayBasis != nil {
				t.Fatalf("equivalent suppressed home admitted: %v", err)
			}
		})
	}
}

// Missing, dangling, file and relative homes refuse; an existing directory is the nearby positive control.
func TestGatewayUnresolvableHomes(t *testing.T) {
	for _, kind := range []string{"missing", "dangling", "file", "relative"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
			path := filepath.Join(t.TempDir(), "home")
			switch kind {
			case "dangling":
				if err := os.Symlink(path+"-absent", path); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
					t.Fatal(err)
				}
			case "relative":
				path = "relative/home"
			}
			req.Home = path
			out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
			if !errors.Is(err, agentic.ErrCodexGatewayRefused) || out.GatewayBasis != nil || out.Plan.Binary != "" {
				t.Fatalf("unresolvable home admitted: %v", err)
			}
		})
	}
}

// Retargeting the original symlink never changes the canonical child home; replacing that home invalidates both verifiers.
func TestGatewayHomeDirectoryIdentity(t *testing.T) {
	for _, phase := range []string{"before-finalize", "after-finalize"} {
		t.Run(phase, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			real := req.Home
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			req.Home = link
			p := gatewayPolicyBuild(t, req, store)
			a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
			if err != nil {
				t.Fatal(err)
			}
			b := gatewayPolicyBinding(t, p)
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			gatewaySuppressCanonical(t, store, target)
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if p.Plan.Home != real {
				t.Fatal("plan retained retargetable selector")
			}
			// Keep the old inode alive so replacement cannot accidentally reuse it.
			saved := real + "-saved"
			replace := func() {
				if err := os.Rename(real, saved); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(real, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "before-finalize" {
				replace()
				out, r, err := a.Finalize(b)
				if !errors.Is(err, agentic.ErrCodexGatewayRefused) || r != nil || out.Plan.Binary != "" {
					t.Fatalf("replacement finalized: %v", err)
				}
				if err := os.Remove(real); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(saved, real); err != nil {
					t.Fatal(err)
				}
			}
			final, r, err := a.Finalize(b)
			if err != nil || r == nil {
				t.Fatalf("unchanged identity/valid retry: %v", err)
			}
			env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
			if err := final.Plan.VerifyBeforeExec(); err != nil {
				t.Fatal(err)
			}
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); err != nil {
				t.Fatal(err)
			}
			replace()
			if err := final.Plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("replacement passed plan verifier: %v", err)
			}
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("replacement passed process verifier: %v", err)
			}
		})
	}
}

// Each requested/canonical home × requested/launched model key can independently refuse with its original verdict.
func TestGatewayLimitsSpellingProduct(t *testing.T) {
	for _, homeKind := range []string{"canonical", "requested"} {
		for _, model := range []string{"gpt-6.1-sol", "sol"} {
			t.Run(homeKind+"/"+model, func(t *testing.T) {
				req, store, _ := gatewayPolicyFixture(t)
				real := filepath.Join(t.TempDir(), "Account")
				if err := os.Mkdir(real, 0700); err != nil {
					t.Fatal(err)
				}
				resolved, err := filepath.EvalSymlinks(real)
				if err != nil {
					t.Fatal(err)
				}
				real = resolved
				alternate := filepath.Join(filepath.Dir(real), "ACCOUNT")
				x, err := os.Stat(real)
				if err != nil {
					t.Fatal(err)
				}
				y, err := os.Stat(alternate)
				if err != nil || !os.SameFile(x, y) {
					t.Skip("filesystem is case sensitive; distinct persisted home spellings unavailable")
				}
				req.Home = alternate
				gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
				home := real
				if homeKind == "requested" {
					home = alternate
				}
				identity, err := providerlimits.IdentityFor("codex", home)
				if err != nil {
					t.Fatal(err)
				}
				obs, ok := providerlimits.ClassifyManaged(providerlimits.ProviderCodex, providerlimits.ManagedSignalUsageLimited).QuotaObservation(providerlimits.Evidence{Model: model})
				if !ok {
					t.Fatal("observation")
				}
				if err := store.Observe(identity, providerlimits.MustGroupTable().Group("codex", model), nil, obs); err != nil {
					t.Fatal(err)
				}
				expected, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: "codex", Model: model, Home: home})
				if err != nil || expected.Serviceable() {
					t.Fatalf("suppression control: %v %+v", err, expected)
				}
				out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
				var refusal *gatewayadmission.LimitsRefusalError
				if !errors.As(err, &refusal) || out.GatewayBasis != nil || out.Plan.Binary != "" {
					t.Fatalf("spelling escaped suppression: %v", err)
				}
				if !reflect.DeepEqual(refusal.Verdict, expected) {
					t.Fatal("lost refusing verdict")
				}
			})
		}
	}
}

// Only the normalized shipped codex/openai tuple admits, including refusal of routed Alibaba models and misleading declarations.
func TestGatewayShippedRuntimeAndVendor(t *testing.T) {
	for _, row := range []struct {
		runtime, vendor, model string
		valid                  bool
	}{
		{"codex", "openai", "sol", true}, {" CODEX ", "openai", "sol", true},
		{"claude", "openai", "sol", false}, {"corp-codex", "openai", "sol", false},
		{"qwen-codex", "alibaba", "qwen3.7-plus-via-codex", false}, {"codex", "alibaba", "qwen3.7-plus-via-codex", false},
	} {
		t.Run(row.runtime+"/"+row.vendor, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			r := vendorplugin.NewRegistry(agentic.Default)
			v, ok := vendorplugin.Default.Lookup(vendorplugin.VendorID(row.vendor))
			if !ok {
				t.Fatal("vendor")
			}
			if err := r.Register(v); err != nil {
				t.Fatal(err)
			}
			normalized, err := vendorplugin.NormalizeRuntimeID(row.runtime)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.DeclareRuntime(vendorplugin.RuntimeDeclaration{ID: normalized, System: "codex", Vendor: vendorplugin.VendorID(row.vendor), Broker: vendorplugin.BrokerProvenance{Checked: []string{"fixture"}, Found: "fixture"}}); err != nil {
				t.Fatal(err)
			}
			req.Runtime, req.Model = vendorplugin.RuntimeID(row.runtime), vendorplugin.ModelID(row.model)
			p, err := gatewayadmission.Build(context.Background(), r, store, req)
			if row.valid {
				if err != nil {
					t.Fatal(err)
				}
				gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{})
				return
			}
			if !errors.Is(err, agentic.ErrCodexGatewayRefused) || p.GatewayBasis != nil || p.Plan.Binary != "" {
				t.Fatalf("nonshipped tuple admitted: %v", err)
			}
		})
	}
}

// Composed folded aliases refuse before success, without consuming the basis; exact-name overrides remain valid.
func TestGatewayComposedEnvironmentNames(t *testing.T) {
	for _, kind := range []string{"base-fragment", "base-prompt", "fragment-prompt", "fragment-duplicate", "base-duplicate"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			in := agentic.CodexGatewayInputs{}
			switch kind {
			case "base-fragment":
				req.Env = append(req.Env, "VALUE=base")
				in.FragmentEnv = []string{"value=other"}
			case "base-prompt":
				req.Env = append(req.Env, "VALUE=base")
				in.PromptEnv = []string{"value=other"}
			case "fragment-prompt":
				in.FragmentEnv = []string{"VALUE=one"}
				in.PromptEnv = []string{"value=two"}
			case "fragment-duplicate":
				in.FragmentEnv = []string{"VALUE=one", "value=two"}
			case "base-duplicate":
				req.Env = append(req.Env, "VALUE=one", "value=two")
			}
			p := gatewayPolicyBuild(t, req, store)
			a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, in)
			if err == nil {
				out, r, err := a.Finalize(gatewayPolicyBinding(t, p))
				if !errors.Is(err, agentic.ErrCodexGatewayRefused) || r != nil || out.Plan.Binary != "" {
					t.Fatalf("invalid composed environment finalized: %v", err)
				}
			} else if !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatal(err)
			}
			if kind == "base-duplicate" {
				return
			} // A bad immutable base needs a new Build, not a different overlay.
			gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{FragmentEnv: []string{"VALUE=fragment"}, PromptEnv: []string{"VALUE=prompt", "OTHER=yes"}})
		})
	}
}

// Actual environment order is irrelevant, but any addition, removal or value/name change beyond exactly one private token refuses.
func TestGatewayActualEnvironmentEquality(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	final, b := gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{FragmentEnv: []string{"ADMITTED=yes"}})
	control := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
	slices.Reverse(control)
	if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, control); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"add", "remove", "change", "rename", "token-missing", "token-duplicate"} {
		t.Run(kind, func(t *testing.T) {
			env := slices.Clone(control)
			switch kind {
			case "add":
				env = append(env, "UNADMITTED=yes")
			case "remove":
				env = slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, "ADMITTED=") })
			case "change":
				env = agentic.SetEnvValue(env, "ADMITTED", "no")
			case "rename":
				env = slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, "ADMITTED=") })
				env = append(env, "admitted=yes")
			case "token-missing":
				env = slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, b.TokenEnv+"=") })
			case "token-duplicate":
				env = append(env, b.TokenEnv+"=synthetic")
			}
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("actual env mutation accepted: %v", err)
			}
		})
	}
}

// A foreign staged binary with its own valid digest refuses without consumption; identical bytes at a distinct staging path still work.
func TestGatewayForeignStagedDigestAndProcessBinary(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{})
	if err != nil {
		t.Fatal(err)
	}
	b := gatewayPolicyBinding(t, p)
	other := filepath.Join(t.TempDir(), "foreign")
	data := []byte("#!/bin/sh\nexit 7\n")
	if err := os.WriteFile(other, data, 0700); err != nil {
		t.Fatal(err)
	}
	bad := b
	bad.ExecutablePath = other
	bad.ExecutableSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	if out, r, err := a.Finalize(bad); !errors.Is(err, agentic.ErrCodexGatewayRefused) || r != nil || out.Plan.Binary != "" {
		t.Fatalf("foreign staged digest accepted: %v", err)
	}
	original, err := os.ReadFile(p.Plan.Binary)
	if err != nil {
		t.Fatal(err)
	}
	b.ExecutablePath = filepath.Join(t.TempDir(), "staged")
	if err := os.WriteFile(b.ExecutablePath, original, 0700); err != nil {
		t.Fatal(err)
	}
	final, receipt, err := a.Finalize(b)
	if err != nil || receipt == nil {
		t.Fatalf("valid retry refused: %v", err)
	}
	env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
	if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); err != nil {
		t.Fatal(err)
	}
	if err := final.Plan.VerifyCodexGatewayProcess(other, final.Plan.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatalf("other process binary accepted: %v", err)
	}
}

type gatewayRedirectHomeVendor struct {
	vendorplugin.Vendor
	home    string
	dropPin bool
}

func (v gatewayRedirectHomeVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	r, err := v.Vendor.Spawn(sc)
	if v.home != "" {
		r.Home = v.home
	}
	if v.dropPin {
		r.Env = slices.DeleteFunc(r.Env, func(e string) bool { return strings.HasPrefix(e, "CODEX_HOME=") })
	}
	return r, err
}

// A trusted-extension vendor still cannot redirect the admitted Home or remove its pin; the transparent wrapper is the positive control.
func TestGatewayBuildVendorHomeBinding(t *testing.T) {
	for _, kind := range []string{"control", "home", "pin"} {
		t.Run(kind, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			v, ok := vendorplugin.Default.Lookup("openai")
			if !ok {
				t.Fatal("vendor")
			}
			wrapped := gatewayRedirectHomeVendor{Vendor: v}
			if kind == "home" {
				wrapped.home = t.TempDir()
			}
			if kind == "pin" {
				wrapped.dropPin = true
			}
			r := vendorplugin.NewRegistry(agentic.Default)
			if err := r.Register(wrapped); err != nil {
				t.Fatal(err)
			}
			if err := r.DeclareRuntime(vendorplugin.RuntimeDeclaration{ID: "codex", System: "codex", Vendor: "openai", Broker: vendorplugin.BrokerProvenance{Checked: []string{"fixture"}, Found: "fixture"}}); err != nil {
				t.Fatal(err)
			}
			out, err := gatewayadmission.Build(context.Background(), r, store, req)
			if kind == "control" {
				if err != nil {
					t.Fatal(err)
				}
				gatewayFinalizeControl(t, out, agentic.CodexGatewayInputs{})
				return
			}
			if !errors.Is(err, agentic.ErrCodexGatewayRefused) || out.GatewayBasis != nil || out.Plan.Binary != "" {
				t.Fatalf("vendor home redirect admitted: %v", err)
			}
		})
	}
}

// A snapshot captures only an existing directory; deletion after a valid build cannot produce a replacement candidate.
func TestGatewaySnapshotHomePresence(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	p := gatewayPolicyBuild(t, req, store)
	if _, err := agentic.SnapshotGatewayCandidate(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(req.Home); err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.SnapshotGatewayCandidate(p); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatalf("missing home snapshot accepted: %v", err)
	}
}

// Requested aliases retain their own persisted suppression key on every filesystem; a healthy alias launch succeeds first.
func TestGatewayRequestedModelSuppression(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
	identity, err := providerlimits.IdentityFor("codex", req.Home)
	if err != nil {
		t.Fatal(err)
	}
	obs, ok := providerlimits.ClassifyManaged(providerlimits.ProviderCodex, providerlimits.ManagedSignalUsageLimited).QuotaObservation(providerlimits.Evidence{Model: "sol"})
	if !ok {
		t.Fatal("observation")
	}
	if err := store.Observe(identity, providerlimits.MustGroupTable().Group("codex", "sol"), nil, obs); err != nil {
		t.Fatal(err)
	}
	expected, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: "codex", Model: "sol", Home: req.Home})
	if err != nil || expected.Serviceable() {
		t.Fatalf("alias control: %v %+v", err, expected)
	}
	out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
	var refusal *gatewayadmission.LimitsRefusalError
	if !errors.As(err, &refusal) || out.GatewayBasis != nil || out.Plan.Binary != "" {
		t.Fatalf("requested alias escaped: %v", err)
	}
	if !reflect.DeepEqual(refusal.Verdict, expected) {
		t.Fatal("lost alias verdict")
	}
}
