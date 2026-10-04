package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"strings"
	"testing"
)

func panelBase(t *testing.T) agentic.Plan {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	p, e := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestPanelFinalizePrechangedBase(t *testing.T) {
	for _, field := range []string{"binary", "argv", "env"} {
		t.Run(field, func(t *testing.T) {
			p := panelBase(t)
			switch field {
			case "binary":
				p.Binary = "/synthetic/other-binary"
			case "argv":
				p.Argv = append([]string(nil), p.Argv...)
				p.Argv[0] = "other-command"
			case "env":
				p.Env = append([]string(nil), p.Env...)
				p.Env = append(p.Env, "UNPERMITTED=changed")
			}
			if field != "env" && p.VerifyBeforeExec() == nil {
				t.Fatal("base mutation control did not refuse")
			}
			f, e := agentic.FinalizePlan(p, agentic.FinalizeOverlays{}, nil)
			if e != nil {
				t.Logf("refused: %v", e)
				return
			}
			if e = f.VerifyBeforeExec(); e == nil {
				t.Errorf("pre-finalize %s mutation admitted", field)
			}
		})
	}
}
func TestPanelExportArtifactAliasing(t *testing.T) {
	p := panelBase(t)
	f, e := agentic.FinalizePlan(p, agentic.FinalizeOverlays{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	a, e := f.ExportSeal()
	if e != nil {
		t.Fatal(e)
	}
	want := a.Data.Sealed.Artifacts[0].Digest
	a.Data.Sealed.Artifacts[0].Digest = "sha256:" + strings.Repeat("0", 64)
	b, e := f.ExportSeal()
	if e != nil {
		t.Fatal(e)
	}
	if b.Data.Sealed.Artifacts[0].Digest != want {
		t.Error("export alias mutated future exports")
	}
}
func panelRedigest(s *agentic.Seal) {
	d := s.Data.Sealed
	c := struct {
		System    string                   `json:"system"`
		Binary    string                   `json:"binary"`
		Argv      []string                 `json:"argv"`
		Artifacts []agentic.SealedArtifact `json:"artifacts"`
		Selectors map[string]string        `json:"selectors,omitempty"`
	}{d.System, d.Binary, d.Argv, d.Artifacts, d.Selectors}
	b, _ := json.Marshal(c)
	d.Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}
func TestPanelRedigestedTamper(t *testing.T) {
	p := panelBase(t)
	s, e := p.ExportSeal()
	if e != nil {
		t.Fatal(e)
	}
	s.Data.Sealed.Argv = append(s.Data.Sealed.Argv, "--forged")
	panelRedigest(&s)
	v, e := agentic.ImportSeal(New(), s)
	if e != nil {
		t.Fatalf("integrity-only bound unexpectedly refused: %v", e)
	}
	p.Argv = append(p.Argv, "--forged")
	if e = v.VerifyBeforeExec(p); e != nil {
		t.Errorf("transient integrity bound unexpectedly became authentication: %v", e)
	}
}
func TestPanelSecretSelectorExport(t *testing.T) {
	p := panelBase(t)
	f, e := agentic.FinalizePlan(p, agentic.FinalizeOverlays{PromptEnv: []string{"API_KEY=SYNTHETIC_PANEL_CANARY"}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	s, e := f.ExportSeal()
	if e != nil {
		t.Logf("refused: %v", e)
		return
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "SYNTHETIC_PANEL_CANARY") {
		t.Error("credential canary exported in guard selectors")
	}
}

func TestPanelKnownFieldOversize(t *testing.T) {
	p := panelBase(t)
	s, e := p.ExportSeal()
	if e != nil {
		t.Fatal(e)
	}
	s.Data.Sealed.Argv = append(s.Data.Sealed.Argv, strings.Repeat("a", 65537))
	panelRedigest(&s)
	if _, e = agentic.ImportSeal(New(), s); e == nil {
		t.Error("oversized known argv member admitted")
	}
}
func TestPanelParityAllBoundFields(t *testing.T) {
	p := panelBase(t)
	f, e := agentic.FinalizePlan(p, agentic.FinalizeOverlays{FragmentEnv: []string{"PANEL_SELECTOR=initial"}, NativeTail: []string{"--tail"}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	s, e := f.ExportSeal()
	if e != nil {
		t.Fatal(e)
	}
	v, e := agentic.ImportSeal(New(), s)
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"untouched", "binary", "argv", "selector", "unbound-env"} {
		t.Run(n, func(t *testing.T) {
			c := f
			c.Argv = append([]string(nil), f.Argv...)
			c.Env = append([]string(nil), f.Env...)
			switch n {
			case "binary":
				c.Binary += "-changed"
			case "argv":
				c.Argv[0] = "changed"
			case "selector":
				c.Env = append(c.Env, "PANEL_SELECTOR=changed")
			case "unbound-env":
				c.Env = append(c.Env, "PANEL_UNBOUND=new")
			}
			a, b := c.VerifyBeforeExec(), v.VerifyBeforeExec(c)
			if (a == nil) != (b == nil) {
				t.Errorf("inprocess/import disagreement: %v / %v", a, b)
			}
			if (n == "binary" || n == "argv" || n == "selector" || n == "unbound-env") && a == nil {
				t.Error("mutation control admitted")
			}
			if n == "untouched" && a != nil {
				t.Errorf("control refused: %v", a)
			}
		})
	}
}
