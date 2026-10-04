package agentic_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

func TestFinalizeRejectsPrechangedBase(t *testing.T) {
	for _, name := range []string{"binary", "argv", "prepend", "env"} {
		t.Run(name, func(t *testing.T) {
			p := sealTestClaudePlan(t)
			switch name {
			case "binary":
				p.Binary += "-other"
			case "argv":
				p.Argv = append(append([]string(nil), p.Argv...), "extra")
			case "prepend":
				p.Argv = append([]string{"extra"}, p.Argv...)
			case "env":
				p.Env = append(append([]string(nil), p.Env...), "OUTSIDE=changed")
			}
			if _, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Fatalf("prechanged %s admitted: %v", name, err)
			}
		})
	}
}

func TestImportedBindingsExactParity(t *testing.T) {
	p, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{FragmentEnv: []string{"A=fragment"}, PromptEnv: []string{"A=prompt", "B="}, NativeTail: []string{"--tail"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := p.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentic.DecodeSeal(wire)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := agentic.ImportSeal(claude.New(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*agentic.Plan){
		"binary":             func(p *agentic.Plan) { p.Binary += "-other" },
		"argv-element":       func(p *agentic.Plan) { p.Argv[0] += "-other" },
		"argv-extra":         func(p *agentic.Plan) { p.Argv = append(p.Argv, "extra") },
		"argv-remove":        func(p *agentic.Plan) { p.Argv = p.Argv[:len(p.Argv)-1] },
		"env-value":          func(p *agentic.Plan) { p.Env[0] += "changed" },
		"env-extra":          func(p *agentic.Plan) { p.Env = append(p.Env, "OUTSIDE=value") },
		"env-remove":         func(p *agentic.Plan) { p.Env = p.Env[:len(p.Env)-1] },
		"env-order":          func(p *agentic.Plan) { p.Env[0], p.Env[1] = p.Env[1], p.Env[0] },
		"env-empty":          func(p *agentic.Plan) { p.Env[len(p.Env)-1] = "B=changed" },
		"env-missing-equals": func(p *agentic.Plan) { p.Env[len(p.Env)-1] = "B" },
		"env-duplicate":      func(p *agentic.Plan) { p.Env = append(p.Env, "A=prompt") },
	}
	if err := p.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyBeforeExec(p); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := p
			changed.Argv = append([]string(nil), p.Argv...)
			changed.Env = append([]string(nil), p.Env...)
			mutate(&changed)
			left, right := changed.VerifyBeforeExec(), verifier.VerifyBeforeExec(changed)
			if !errors.Is(left, agentic.ErrFinalizedProcessChanged) || !errors.Is(right, agentic.ErrFinalizedProcessChanged) {
				t.Fatalf("binding %s lost: local=%v import=%v", name, left, right)
			}
		})
	}
}

func TestGuardNeverExportsCredentialValues(t *testing.T) {
	// Base credentials, fragment and prompt are all ephemeral material.
	bin := sealTestStubBin(t, "claude")
	base, err := agentic.BuildPlan(sealTestRegistry(t), agentic.LaunchRequest{System: claude.New().ID(), Model: agentic.Model{ID: "seal-test"}, WorkDir: t.TempDir(), Env: []string{"PATH=" + bin, "BASE_SECRET=synthetic-base-canary"}}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatal(err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{FragmentEnv: []string{"FRAGMENT_SECRET=synthetic-fragment-canary"}, PromptEnv: []string{"PROMPT_SECRET=synthetic-prompt-canary"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"synthetic-base-canary", "synthetic-fragment-canary", "synthetic-prompt-canary"} {
		if strings.Contains(string(wire), canary) {
			t.Fatalf("guard carries credential value from an overlay")
		}
	}
	for _, v := range seal.Data.Binding.Selectors {
		if !strings.HasPrefix(v, "hmac-sha256:") {
			t.Fatal("selector is not a keyed commitment")
		}
	}
}

func TestSealExportAndImportCollectionsDoNotAlias(t *testing.T) {
	p, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{PromptEnv: []string{"PANEL_SELECTOR=value"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*agentic.ProcessBinding){
		"argv":      func(b *agentic.ProcessBinding) { b.Argv[0] = "changed" },
		"env-names": func(b *agentic.ProcessBinding) { b.EnvNames[0] = "OTHER" },
		"selectors": func(b *agentic.ProcessBinding) { b.Selectors["0"] = "changed" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			seal, _ := p.ExportSeal()
			original, _ := json.Marshal(seal)
			verifier, err := agentic.ImportSeal(claude.New(), seal)
			if err != nil {
				t.Fatal(err)
			}
			mutate(seal.Data.Binding)
			again, _ := p.ExportSeal()
			current, _ := json.Marshal(again)
			if string(original) != string(current) {
				t.Fatal("export aliases verifier collections")
			}
			if err := verifier.VerifyBeforeExec(p); err != nil {
				t.Fatalf("import aliases supplied collections: %v", err)
			}
		})
	}
}

func TestStrictSealWireRefusals(t *testing.T) {
	_ = sealTestClaudePlan(t)
	schema := `{"schema":"urn:relux:agents-management:exec-guard","schema_version":"1.0.0","data":`
	cases := map[string]string{
		"unknown-member":        schema + `{"kind":"unsealed","extra":null}}`,
		"unknown-envelope":      `{"extra":null,"schema":"urn:relux:agents-management:exec-guard","schema_version":"1.0.0","data":{"kind":"unsealed"}}`,
		"duplicate":             schema + `{"kind":"unsealed","kind":"unsealed"}}`,
		"duplicate-null-shadow": schema + `{"kind":null,"kind":"unsealed"}}`,
		"invalid-utf8":          schema + "{\"kind\":\"unsealed\",\"extra\":\"\xff\"}}",
		"lone-surrogate":        schema + `{"kind":"unsealed","extra":"\ud800"}}`,
		"low-surrogate":         schema + `{"kind":"unsealed","extra":"\udc00"}}`,
		"oversize":              schema + `{"kind":"` + strings.Repeat("x", 65536) + `"}}`,
		"depth":                 schema + `{"kind":` + strings.Repeat("[", 17) + `"unsealed"` + strings.Repeat("]", 17) + `}}`,
		"missing-dispatch":      "{}",
		"fractional":            schema + `{"kind":0.5}}`,
		"negative-zero":         schema + `{"kind":-0}}`,
		"oversize-integer":      schema + `{"kind":9007199254740992}}`,
		"truncated":             schema + `{`,
		"trailing":              schema + `{"kind":"unsealed"}} {}`,
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) {
			seal, err := agentic.DecodeSeal([]byte(wire))
			if err == nil {
				_, err = agentic.ImportSeal(claude.New(), seal)
			}
			if !errors.Is(err, agentic.ErrSealMalformed) {
				t.Fatalf("invalid wire %s admitted or untyped: %v", name, err)
			}
		})
	}
}

func TestSealCommitmentKeyBoundary(t *testing.T) {
	key := agentic.SealCommitmentKey{1, 2, 3}
	final, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{}, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	seal, _ := final.ExportSeal()
	if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatal("missing key admitted")
	}
	if _, err := agentic.ImportSeal(claude.New(), seal, agentic.SealCommitmentKey{4}); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatal("wrong key admitted")
	}
	if _, err := agentic.ImportSeal(claude.New(), seal, agentic.SealCommitmentKey{}); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatal("zero key admitted")
	}
	if _, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{}, nil, key, key); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatal("ambiguous keys admitted")
	}
	imported, err := agentic.ImportSeal(claude.New(), seal, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := imported.VerifyBeforeExec(final); err != nil {
		t.Fatal(err)
	}
}

func TestImportProcessBindingTypedRefusals(t *testing.T) {
	p, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binary", "selectors", "digest"} {
		t.Run(name, func(t *testing.T) {
			seal, _ := p.ExportSeal()
			want := agentic.ErrSealMalformed
			switch name {
			case "binary":
				seal.Data.Binding.Binary = ""
			case "selectors":
				seal.Data.Binding.Selectors = nil
			case "digest":
				seal.Data.Binding.Digest = "changed"
				want = agentic.ErrSealTampered
			}
			if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, want) {
				t.Fatalf("invalid binding admitted or untyped: %v", err)
			}
		})
	}
}

func TestFinalizeRejectsMalformedBaseEnv(t *testing.T) {
	for _, entry := range []string{"BROKEN", "1INVALID=value", "NUL=invalid\x00value"} {
		t.Run(entry[:3], func(t *testing.T) {
			bin := sealTestStubBin(t, "claude")
			p, err := agentic.BuildPlan(sealTestRegistry(t), agentic.LaunchRequest{System: claude.New().ID(), Model: agentic.Model{ID: "seal-test"}, WorkDir: t.TempDir(), Env: []string{"PATH=" + bin, entry}}, agentic.LaunchModeExec)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
				t.Fatalf("malformed base env admitted: %v", err)
			}
		})
	}
}

func TestHostedAdmissionClosedMarker(t *testing.T) {
	p := sealTestClaudePlan(t)
	seal, _ := p.ExportSeal()
	if !seal.HostedAdmissible() {
		t.Fatal("closed base marker refused")
	}
	for _, name := range []string{"schema", "version", "binding", "sealed"} {
		t.Run(name, func(t *testing.T) {
			c := seal
			switch name {
			case "schema":
				c.Schema = "unknown"
			case "version":
				c.SchemaVersion = "2.0.0"
			case "binding":
				c.Data.Binding = &agentic.ProcessBinding{}
			case "sealed":
				c.Data.Sealed = &agentic.SealedData{}
			}
			if c.HostedAdmissible() {
				t.Fatalf("non-closed %s marker hosted-admissible", name)
			}
		})
	}
}

func TestImportRejectsSeparateSealedBinding(t *testing.T) {
	_ = sealTestClaudePlan(t)
	seal := agentic.Seal{Schema: agentic.ExecGuardSchema, SchemaVersion: agentic.ExecGuardVersion, Data: agentic.SealData{Kind: agentic.SealKindSealed, Binding: &agentic.ProcessBinding{}}}
	if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, agentic.ErrSealMalformed) {
		t.Fatalf("separate binding admitted: %v", err)
	}
}

func TestSealWireDispatchPrecedesShape(t *testing.T) {
	_ = sealTestClaudePlan(t)
	for _, row := range []struct {
		wire string
		want error
	}{
		{`{"schema":"unknown","schema_version":"1.0.0","data":{"future":12}}`, agentic.ErrSealUnknownSchema},
		{`{"schema":"urn:relux:agents-management:exec-guard","schema_version":"2.0.0","data":{"future":12}}`, agentic.ErrSealUnknownVersion},
	} {
		if _, err := agentic.DecodeSeal([]byte(row.wire)); !errors.Is(err, row.want) {
			t.Fatalf("dispatch did not precede structure: %v", err)
		}
	}
}

// The test helper executes only verification, never the planned provider.
func TestSealCrossProcessImport(t *testing.T) {
	type transfer struct {
		Seal      agentic.Seal
		Key       agentic.SealCommitmentKey
		Binary    string
		Argv, Env []string
	}
	if os.Getenv("AGENTIC_SEAL_TEST_CHILD") == "1" {
		var input transfer
		if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if _, err := agentic.ImportSeal(claude.New(), input.Seal); !errors.Is(err, agentic.ErrSealImportRefused) {
			t.Fatal("foreign guard admitted without its key")
		}
		verifier, err := agentic.ImportSeal(claude.New(), input.Seal, input.Key)
		if err != nil {
			t.Fatal(err)
		}
		p := agentic.Plan{Binary: input.Binary, Argv: input.Argv, Env: input.Env}
		if err := verifier.VerifyBeforeExec(p); err != nil {
			t.Fatal(err)
		}
		p.Env = append(p.Env, "EXTRA=changed")
		if err := verifier.VerifyBeforeExec(p); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
			t.Fatal("cross-process env change admitted")
		}
		return
	}
	key := agentic.SealCommitmentKey{9, 8, 7, 6}
	p, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{PromptEnv: []string{"CREDENTIAL=synthetic-private-stdin-canary"}}, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := p.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(transfer{seal, key, p.Binary, p.Argv, p.Env})
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestSealCrossProcessImport$")
	child.Env = []string{"AGENTIC_SEAL_TEST_CHILD=1"}
	child.Stdin = bytes.NewReader(payload)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("verification helper failed: %v: %s", err, output)
	}
}
