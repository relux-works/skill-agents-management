package providerquota_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/internal/quotatest"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

func TestRound2PlansRefuseProtectedSymlink(t *testing.T) {
	for _, name := range []string{"codex", "claude", "agy", "muse"} {
		t.Run(name, func(t *testing.T) {
			c := quotatest.Context(t, name, false)
			allowed := t.TempDir()
			target := t.TempDir()
			// Inert fixture is prepared before target is designated a forbidden root.
			if err := execfixture.WriteFile(filepath.Join(target, name), []byte("fixture"), 0700); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(allowed, name)
			if err := os.Symlink(filepath.Join(target, name), binary); err != nil {
				t.Fatal(err)
			}
			var reader providerquota.Reader
			switch name {
			case "codex":
				reader = codex.New()
			case "claude":
				reader = claude.New()
			case "muse":
				reader = muse.New()
			case "agy":
				reader = agy.NewWithRuntime(agy.Runtime{Executable: binary, Version: agy.MinimumQuotaVersion})
			}
			_, err := reader.QuotaPlan(providerquota.Request{Context: c, Env: []string{"HOME=" + t.TempDir(), "PATH=" + allowed}, ForbiddenRoots: []string{target}})
			if err == nil || providerquota.Reason(err) != "protected_root" {
				t.Fatalf("protected symlink accepted: %v", err)
			}
		})
	}
}
func TestRound2PlansRefuseProtectedEmptyPath(t *testing.T) {
	c := quotatest.Context(t, "muse", false)
	cwd := t.TempDir()
	if err := execfixture.WriteFile(filepath.Join(cwd, "muse"), []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	_, err := muse.New().QuotaPlan(providerquota.Request{Context: c, Env: []string{"HOME=" + t.TempDir(), "PATH=:/nonexistent"}, ForbiddenRoots: []string{cwd}})
	if err == nil || providerquota.Reason(err) != "protected_root" {
		t.Fatalf("protected cwd accepted via empty PATH: %v", err)
	}
}

func TestRound3RelativePathPlanSurvivesScratchCwd(t *testing.T) {
	planning := t.TempDir()
	canonical, err := filepath.EvalSymlinks(planning)
	if err != nil {
		t.Fatal(err)
	}
	planning = canonical
	t.Chdir(planning)
	checked := filepath.Join(planning, "bin", "muse")
	if err := os.MkdirAll(filepath.Dir(checked), 0700); err != nil {
		t.Fatal(err)
	}
	if err := execfixture.WriteFile(checked, []byte("inert fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := muse.New().QuotaPlan(providerquota.Request{Env: []string{"HOME=" + t.TempDir(), "PATH=bin"}})
	if err != nil || !plan.Ready || plan.CwdPolicy != providerquota.ScratchCwd || plan.Binary != checked || !filepath.IsAbs(plan.Binary) {
		t.Fatalf("plan lost checked absolute binary: %#v, %v", plan, err)
	}
	t.Chdir(t.TempDir())
	b, err := os.ReadFile(plan.Binary)
	if err != nil || string(b) != "inert fixture" {
		t.Fatalf("binary changed meaning in scratch cwd: %q, %v", b, err)
	}
}
