package claude

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestStoredPolicyInspectorReportsEveryKnownSelectorAtEveryLocalSource(t *testing.T) {
	sources := []struct {
		name string
		path func(home, workDir string) string
	}{
		{"user", func(home, _ string) string { return filepath.Join(home, "settings.json") }},
		{"project-shared", func(_, workDir string) string { return filepath.Join(workDir, ".claude", "settings.json") }},
		{"project-local", func(_, workDir string) string { return filepath.Join(workDir, ".claude", "settings.local.json") }},
	}
	selectors := []struct {
		name     string
		selector string
		value    string
		setting  string
	}{
		{"default-mode-bypass", "permissions.defaultMode", "bypassPermissions", `"permissions":{"defaultMode":"bypassPermissions"}`},
		{"allow-rule", "permissions.allow", "Bash(git *)", `"permissions":{"allow":["Bash(git *)"]}`},
		{"additional-directory", "permissions.additionalDirectories", "/shared", `"permissions":{"additionalDirectories":["/shared"]}`},
	}

	for _, source := range sources {
		for _, selector := range selectors {
			t.Run(source.name+"/"+selector.name, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				configRoot := filepath.Join(home, ".claude")
				workDir := filepath.Join(root, "project")
				path := source.path(configRoot, workDir)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{"+selector.setting+"}"), 0o600); err != nil {
					t.Fatal(err)
				}

				report := agentic.InspectStoredPolicy(New(), agentic.Plan{
					Env:     []string{"HOME=" + home, "CLAUDE_CONFIG_DIR=" + configRoot},
					Home:    filepath.Join(root, "decoy", ".claude"),
					WorkDir: workDir,
				})
				if report.Support != agentic.StoredPolicySupported {
					t.Fatalf("Support = %q, want supported", report.Support)
				}
				if !containsString(report.SourcesInspected, path) {
					t.Fatalf("sources inspected = %v, want %q", report.SourcesInspected, path)
				}
				if len(report.Relaxations) != 1 {
					t.Fatalf("relaxations = %#v, want exactly one row", report.Relaxations)
				}
				got := report.Relaxations[0]
				if got.Selector != selector.selector || got.Value != selector.value || got.SourcePath != path {
					t.Fatalf("relaxation = %#v, want selector=%q value=%q source=%q", got, selector.selector, selector.value, path)
				}
			})
		}
	}
}

func TestStoredPolicyInspectorReportsRelaxedDefaultModes(t *testing.T) {
	for _, mode := range []string{"acceptEdits", "auto", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			configRoot := filepath.Join(home, ".claude")
			path := filepath.Join(configRoot, "settings.json")
			if err := os.MkdirAll(configRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"permissions":{"defaultMode":"`+mode+`"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			report := New().InspectStoredPolicy(agentic.StoredPolicyContext{
				Environment: []string{"HOME=" + home}, Home: "~/.claude", WorkDir: root,
			})
			if len(report.Relaxations) != 1 || report.Relaxations[0].Value != mode {
				t.Fatalf("relaxations = %#v, want defaultMode %q", report.Relaxations, mode)
			}
		})
	}
}

func TestStoredPolicyInspectorDoesNotCallMalformedOrAbsentSettingsClean(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configRoot := filepath.Join(home, ".claude")
	workDir := filepath.Join(root, "project")
	path := filepath.Join(configRoot, "settings.json")
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions":`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := New().InspectStoredPolicy(agentic.StoredPolicyContext{
		Environment: []string{"HOME=" + home}, Home: "~/.claude", WorkDir: workDir,
	})
	if containsString(report.SourcesInspected, path) {
		t.Fatalf("malformed source %q was marked inspected: %#v", path, report)
	}
	if !hasSourceIssue(report, path, agentic.StoredPolicySourceUnparseable) {
		t.Fatalf("malformed source issue missing: %#v", report.SourcesNotInspected)
	}
	if !hasSourceIssue(report, filepath.Join(workDir, ".claude", "settings.json"), agentic.StoredPolicySourceAbsent) {
		t.Fatalf("absent shared project source missing from not-inspected list: %#v", report.SourcesNotInspected)
	}
}

func TestStoredPolicyInspectorReportsUnreadableMode000Source(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are unavailable on Windows")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configRoot := filepath.Join(home, ".claude")
	path := filepath.Join(configRoot, "settings.json")
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions":{"defaultMode":"bypassPermissions"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Errorf("restore readable mode: %v", err)
		}
	}()
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("the test process can read a mode-000 file, so this host cannot reproduce unreadability")
	}

	report := New().InspectStoredPolicy(agentic.StoredPolicyContext{
		Environment: []string{"HOME=" + home}, Home: "~/.claude", WorkDir: root,
	})
	if containsString(report.SourcesInspected, path) || len(report.Relaxations) != 0 {
		t.Fatalf("unreadable source was treated as clean or inspected: %#v", report)
	}
	if !hasSourceIssue(report, path, agentic.StoredPolicySourceUnreadable) {
		t.Fatalf("unreadable source issue missing: %#v", report.SourcesNotInspected)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasSourceIssue(report agentic.StoredPolicyInspection, path string, reason agentic.StoredPolicySourceReason) bool {
	for _, issue := range report.SourcesNotInspected {
		if issue.SourcePath == path && issue.Reason == reason {
			return true
		}
	}
	return false
}
