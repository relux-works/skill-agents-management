package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"strings"
	"testing"
)

func panelPlan(t *testing.T) agentic.Plan {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	p, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func panelDigest(d agentic.SealedData) string {
	bytes, _ := json.Marshal(struct {
		System    string                   `json:"system"`
		Binary    string                   `json:"binary"`
		Argv      []string                 `json:"argv"`
		Artifacts []agentic.SealedArtifact `json:"artifacts"`
		Selectors map[string]string        `json:"selectors,omitempty"`
	}{d.System, d.Binary, d.Argv, d.Artifacts, d.Selectors})
	return fmt.Sprintf("sha256:%x", sha256.Sum256(bytes))
}
func TestPanelFinalizeRejectsMutatedBase(t *testing.T) {
	for _, name := range []string{"binary", "argv", "env"} {
		t.Run(name, func(t *testing.T) {
			p := panelPlan(t)
			switch name {
			case "binary":
				p.Binary += "-changed"
			case "argv":
				p.Argv = append([]string{"--unpermitted"}, p.Argv...)
			case "env":
				p.Env = append(p.Env, "UNPERMITTED=changed")
			}
			f, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{}, nil)
			if err == nil {
				err = f.VerifyBeforeExec()
			}
			t.Logf("refused=%v", err != nil)
			if err == nil {
				t.Error("mutated base admitted and rebound")
			}
		})
	}
}
func TestPanelExportNoAlias(t *testing.T) {
	p := panelPlan(t)
	f, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := f.ExportSeal()
	a.Data.Sealed.Artifacts[0].Digest = "sha256:" + strings.Repeat("0", 64)
	b, _ := f.ExportSeal()
	if b.Data.Sealed.Artifacts[0].Digest == a.Data.Sealed.Artifacts[0].Digest {
		t.Error("exported artifact slice aliases retained verifier data")
	}
}
func TestPanelGuardSecretExclusion(t *testing.T) {
	p := panelPlan(t)
	f, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{PromptEnv: []string{"PANEL_CREDENTIAL=synthetic-secret-canary"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.ExportSeal()
	bytes, _ := json.Marshal(s)
	if strings.Contains(string(bytes), "synthetic-secret-canary") {
		t.Error("env credential copied into guard selectors")
	}
}
func TestPanelRedigestedTampering(t *testing.T) {
	p := panelPlan(t)
	s, _ := p.ExportSeal()
	s.Data.Sealed.Binary += "-changed"
	s.Data.Sealed.Argv = append(s.Data.Sealed.Argv, "--unpermitted")
	s.Data.Sealed.Digest = panelDigest(*s.Data.Sealed)
	v, err := agentic.ImportSeal(New(), s)
	changed := p
	changed.Binary = s.Data.Sealed.Binary
	changed.Argv = s.Data.Sealed.Argv
	if err == nil {
		err = v.VerifyBeforeExec(changed)
	}
	if err != nil {
		t.Errorf("transient integrity bound unexpectedly became authentication: %v", err)
	}
}
func TestPanelOversizedKnownSealedData(t *testing.T) {
	p := panelPlan(t)
	f, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{NativeTail: []string{strings.Repeat("x", 65537)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(s)
	t.Logf("guard_bytes=%d", len(wire))
	_, err = agentic.ImportSeal(New(), s)
	if err == nil {
		t.Error("known sealed data above 64KiB and argv token above 64KiB admitted")
	}
}
func TestPanelCodexParityAndDeterminism(t *testing.T) {
	p := panelPlan(t)
	f, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{PromptEnv: []string{"PANEL_SELECTOR=one"}, NativeTail: []string{"--tail"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := f.ExportSeal()
	b, _ := f.ExportSeal()
	aw, _ := json.Marshal(a)
	bw, _ := json.Marshal(b)
	if string(aw) != string(bw) {
		t.Fatal("non-deterministic export")
	}
	var decoded agentic.Seal
	if err := json.Unmarshal(aw, &decoded); err != nil {
		t.Fatal(err)
	}
	v, err := agentic.ImportSeal(New(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.VerifyBeforeExec(f); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binary", "argv", "selector"} {
		t.Run(name, func(t *testing.T) {
			c := f
			switch name {
			case "binary":
				c.Binary += "-changed"
			case "argv":
				c.Argv = append(append([]string(nil), c.Argv...), "extra")
			case "selector":
				c.Env = append(append([]string(nil), c.Env...), "PANEL_SELECTOR=two")
			}
			l := c.VerifyBeforeExec()
			r := v.VerifyBeforeExec(c)
			if l == nil || r == nil {
				t.Error("changed process admitted")
			}
		})
	}
}
func TestPanelUnpermittedFinalEnvMutation(t *testing.T) {
	p := panelPlan(t)
	f, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.Env = append(f.Env, "UNPERMITTED=changed")
	if err := f.VerifyBeforeExec(); err == nil {
		t.Error("env mutation outside permitted overlays admitted")
	}
}
func TestPanelCodexDowngradeRefused(t *testing.T) {
	p := panelPlan(t)
	s, _ := p.ExportSeal()
	s.Data.Kind = agentic.SealKindUnsealed
	s.Data.Sealed = nil
	if _, err := agentic.ImportSeal(New(), s); err == nil {
		t.Error("sealed codex guard downgraded")
	}
}
