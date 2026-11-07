package agentic_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/gatewayadmission"
	"github.com/relux-works/skill-agents-management/pkg/runtimecatalog"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// The launcher's sorted-name/token-last environment and a shuffled copy pass; duplicate ordinary names refuse.
func TestGatewayLauncherEnvironmentSet(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	final, b := gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{FragmentEnv: []string{"ADMITTED=yes", "Z_LAST=yes", "A_FIRST=yes"}})
	env := slices.Clone(final.Plan.Env)
	slices.SortFunc(env, func(a, b string) int {
		an, _, _ := strings.Cut(a, "=")
		bn, _, _ := strings.Cut(b, "=")
		return strings.Compare(an, bn)
	})
	env = append(env, b.TokenEnv+"=synthetic")
	for _, shuffled := range []bool{false, true} {
		t.Run(fmt.Sprint(shuffled), func(t *testing.T) {
			actual := slices.Clone(env)
			if shuffled {
				slices.Reverse(actual)
			}
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, actual); err != nil {
				t.Fatalf("valid reordered child environment: %v", err)
			}
			actual = append(actual, "ADMITTED=yes")
			if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, actual); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("duplicate ordinary environment name accepted: %v", err)
			}
		})
	}
}

// Valid absent lookup names need no NAME= placeholders; adding such an unadmitted entry still refuses at exec.
func TestGatewayAbsentLookupNamesNeedNoEnvironmentEntries(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	names := []string{"MCP_OPTIONAL_114", "_MCP_OPTIONAL_2"}
	final, b := gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{LookupNames: names})
	for _, name := range names {
		for _, entry := range final.Plan.Env {
			if n, _, _ := strings.Cut(entry, "="); n == name {
				t.Fatalf("lookup placeholder materialized: %s", name)
			}
		}
	}
	env := append(slices.Clone(final.Plan.Env), b.TokenEnv+"=synthetic")
	if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); err != nil {
		t.Fatalf("absent lookup required at exec: %v", err)
	}
	env = append(env, names[0]+"=")
	if err := final.Plan.VerifyCodexGatewayProcess(b.ExecutablePath, final.Plan.Argv, env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatalf("unadmitted placeholder accepted: %v", err)
	}
}

// Malformed lookup names refuse during admission, before consumption; correcting the names on the same basis succeeds.
func TestGatewayLookupNameGrammarAtAdmission(t *testing.T) {
	for _, name := range []string{"", "A=B", "1NAME", "WITH SPACE", "WITH-DASH", "A\nB", "naïve"} {
		t.Run(name, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			p := gatewayPolicyBuild(t, req, store)
			a, err := agentic.AdmitCodexGateway(p.GatewayBasis, p, agentic.CodexGatewayInputs{LookupNames: []string{name}})
			if !errors.Is(err, agentic.ErrCodexGatewayRefused) || a != nil {
				t.Errorf("malformed lookup name admitted: %v", err)
			}
			gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{LookupNames: []string{"MCP_OPTIONAL_114"}})
		})
	}
}

// Real operator catalog rows are eligible on codex/openai, alongside gpt-6-astra; changing the vendor or runtime refuses an otherwise valid generic plan.
func TestGatewayOperatorCatalogModelEligibility(t *testing.T) {
	for _, row := range []struct {
		name, runtime, vendor, model string
		operator, valid              bool
	}{
		{"module-astra", "codex", "openai", "gpt-6-astra", false, true},
		{"operator-model", "codex", "openai", "operator-gateway-114", true, true},
		{"different-vendor", "codex", "alibaba", "operator-gateway-114", true, false},
		{"declared-runtime", "corp-codex", "openai", "operator-gateway-114", true, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			req, store, _ := gatewayPolicyFixture(t)
			base, ok := vendorplugin.Default.Lookup(vendorplugin.VendorID(row.vendor))
			if !ok {
				t.Fatal("vendor not registered")
			}
			vendor := base
			if row.operator {
				text := fmt.Sprintf("schema_version = 1\n[vendors.%s.models.%q]\ndescription = \"Gateway operator fixture\"\nsystems = [\"codex\"]\neffort = { support = \"required\", accepts = [\"high\"], recommended = \"high\" }\n", row.vendor, row.model)
				catalog, err := runtimecatalog.ParseModelCatalog([]byte(text))
				if err != nil {
					t.Fatal(err)
				}
				vendor, err = catalog.Apply(base)
				if err != nil {
					t.Fatal(err)
				}
			}
			found := false
			for _, model := range vendor.Models() {
				if string(model.ID) == row.model {
					found = true
					if model.OperatorDeclared != row.operator {
						t.Fatal("fixture lost operator provenance")
					}
				}
			}
			if !found {
				t.Fatal("fixture model missing")
			}
			registry := vendorplugin.NewRegistry(agentic.Default)
			if err := registry.Register(vendor); err != nil {
				t.Fatal(err)
			}
			if row.vendor == "openai" {
				if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
					t.Fatal(err)
				}
			}
			if row.runtime != "codex" || row.vendor != "openai" {
				if err := registry.DeclareRuntime(vendorplugin.RuntimeDeclaration{ID: vendorplugin.RuntimeID(row.runtime), System: "codex", Vendor: vendorplugin.VendorID(row.vendor), Broker: vendorplugin.BrokerProvenance{Checked: []string{"fixture"}, Found: "fixture"}}); err != nil {
					t.Fatal(err)
				}
			}
			req.Runtime, req.Model = vendorplugin.RuntimeID(row.runtime), vendorplugin.ModelID(row.model)
			if _, err := vendorplugin.BuildLaunchWithEnvironment(context.Background(), registry, req, agentic.LaunchModeInteractive); err != nil {
				t.Fatalf("generic launch control: %v", err)
			}
			p, err := gatewayadmission.Build(context.Background(), registry, store, req)
			if row.valid {
				if err != nil {
					t.Fatal(err)
				}
				if p.Plan.ModelIdentity.Launched != row.model {
					t.Fatal("model identity changed")
				}
				gatewayFinalizeControl(t, p, agentic.CodexGatewayInputs{})
			} else if !errors.Is(err, agentic.ErrCodexGatewayRefused) || p.GatewayBasis != nil || p.Plan.Binary != "" {
				t.Fatalf("foreign gateway tuple admitted: %v", err)
			}
		})
	}
}

// Existing error types distinguish a home disagreement from an observed limits refusal; both retain the common refusal sentinel.
func TestGatewayHomeAndLimitsRefusalTypes(t *testing.T) {
	req, store, _ := gatewayPolicyFixture(t)
	gatewayFinalizeControl(t, gatewayPolicyBuild(t, req, store), agentic.CodexGatewayInputs{})
	wrong := req
	wrong.Env = append(slices.Clone(req.Env), "CODEX_HOME="+t.TempDir())
	out, err := gatewayadmission.Build(context.Background(), vendorplugin.Default, store, wrong)
	var limits *gatewayadmission.LimitsRefusalError
	if !errors.Is(err, agentic.ErrCodexGatewayRefused) || errors.As(err, &limits) || out.GatewayBasis != nil || out.Plan.Binary != "" {
		t.Fatalf("home disagreement classified as limits: %v", err)
	}
	gatewaySuppressCanonical(t, store, req.Home)
	out, err = gatewayadmission.Build(context.Background(), vendorplugin.Default, store, req)
	if !errors.Is(err, agentic.ErrCodexGatewayRefused) || !errors.As(err, &limits) || out.GatewayBasis != nil || out.Plan.Binary != "" {
		t.Fatalf("lost typed limits refusal: %v", err)
	}
}
