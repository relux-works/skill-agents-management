package codex

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
		path func(root, workDir string) string
	}{
		{"user", func(root, _ string) string { return filepath.Join(root, "config.toml") }},
		{"project", func(_, workDir string) string { return filepath.Join(workDir, ".codex", "config.toml") }},
		{"selected-profile", func(root, _ string) string { return filepath.Join(root, "fast.config.toml") }},
	}
	selectors := []struct {
		name     string
		selector string
		value    string
		setting  string
	}{
		{"approval-policy", "approval_policy", "never", `approval_policy = "never"`},
		{"sandbox-mode", "sandbox_mode", "danger-full-access", `sandbox_mode = "danger-full-access"`},
		{"sandbox-permissions", "sandbox_permissions", `["disk-write-access"]`, `sandbox_permissions = ["disk-write-access"]`},
	}

	for _, source := range sources {
		for _, selector := range selectors {
			t.Run(source.name+"/"+selector.name, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				configRoot := filepath.Join(home, ".codex")
				workDir := filepath.Join(root, "project")
				path := source.path(configRoot, workDir)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if source.name != "user" {
					if err := os.MkdirAll(configRoot, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(configRoot, "config.toml"), []byte("profile = \"fast\"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				content := selector.setting + "\n"
				if source.name == "user" {
					content = "profile = \"fast\"\n" + content
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}

				report := agentic.InspectStoredPolicy(New(), agentic.Plan{
					Env:     []string{"HOME=" + home, "CODEX_HOME=" + configRoot},
					Home:    filepath.Join(root, "decoy", ".codex"),
					WorkDir: workDir,
				})
				if report.Support != agentic.StoredPolicySupported {
					t.Fatalf("Support = %q, want supported", report.Support)
				}
				if !codexStoredPolicyContains(report.SourcesInspected, path) {
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

func TestStoredPolicyInspectorDoesNotCallMalformedOrAbsentConfigClean(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configRoot := filepath.Join(home, ".codex")
	workDir := filepath.Join(root, "project")
	userPath := filepath.Join(configRoot, "config.toml")
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("approval_policy = \"never\"\n[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := New().InspectStoredPolicy(agentic.StoredPolicyContext{
		Environment: []string{"HOME=" + home, "CODEX_HOME=" + configRoot},
		Home:        "~/.codex",
		WorkDir:     workDir,
	})
	if codexStoredPolicyContains(report.SourcesInspected, userPath) {
		t.Fatalf("malformed source %q was marked inspected: %#v", userPath, report)
	}
	if !codexHasSourceIssue(report, userPath, agentic.StoredPolicySourceUnparseable) {
		t.Fatalf("malformed source issue missing: %#v", report.SourcesNotInspected)
	}
	if !codexHasSourceIssue(report, filepath.Join(workDir, ".codex", "config.toml"), agentic.StoredPolicySourceAbsent) {
		t.Fatalf("absent project source missing: %#v", report.SourcesNotInspected)
	}
	if len(report.Relaxations) != 0 {
		t.Fatalf("relaxation from malformed source was retained: %#v", report.Relaxations)
	}
}

func TestStoredPolicyInspectorReportsUnreadableMode000Config(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are unavailable on Windows")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configRoot := filepath.Join(home, ".codex")
	path := filepath.Join(configRoot, "config.toml")
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("approval_policy = \"never\"\n"), 0o600); err != nil {
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
		Environment: []string{"HOME=" + home, "CODEX_HOME=" + configRoot},
		Home:        "~/.codex",
		WorkDir:     root,
	})
	if codexStoredPolicyContains(report.SourcesInspected, path) || len(report.Relaxations) != 0 {
		t.Fatalf("unreadable source was treated as clean or inspected: %#v", report)
	}
	if !codexHasSourceIssue(report, path, agentic.StoredPolicySourceUnreadable) {
		t.Fatalf("unreadable source issue missing: %#v", report.SourcesNotInspected)
	}
}

func TestStoredPolicyInspectorRefusesProfilePathTraversal(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configRoot := filepath.Join(home, ".codex")
	workDir := filepath.Join(root, "project")
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "config.toml"), []byte("profile = \"../outside\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideProfile := filepath.Join(home, "outside.config.toml")
	if err := os.WriteFile(outsideProfile, []byte("approval_policy = \"never\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := New().InspectStoredPolicy(agentic.StoredPolicyContext{
		Environment: []string{"HOME=" + home, "CODEX_HOME=" + configRoot},
		Home:        "~/.codex",
		WorkDir:     workDir,
	})
	if len(report.Relaxations) != 0 {
		t.Fatalf("traversal profile was read: %#v", report.Relaxations)
	}
	if !codexHasSourceIssue(report, filepath.Join(configRoot, "<selected-profile>.config.toml"), agentic.StoredPolicySourceUnknownSelection) {
		t.Fatalf("invalid selected-profile source was not reported: %#v", report.SourcesNotInspected)
	}
}

func TestStoredPolicyInspectorIgnoresKnownRestrictiveValues(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configRoot := filepath.Join(home, ".codex")
	path := filepath.Join(configRoot, "config.toml")
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("approval_policy = \"on-request\"\nsandbox_mode = \"read-only\"\nsandbox_permissions = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := New().InspectStoredPolicy(agentic.StoredPolicyContext{
		Environment: []string{"HOME=" + home}, Home: "~/.codex", WorkDir: root,
	})
	if len(report.Relaxations) != 0 {
		t.Fatalf("restrictive values reported as relaxations: %#v", report.Relaxations)
	}
	if !codexStoredPolicyContains(report.SourcesInspected, path) {
		t.Fatalf("valid restrictive config was not inspected: %#v", report)
	}
}

func codexStoredPolicyContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func codexHasSourceIssue(report agentic.StoredPolicyInspection, path string, reason agentic.StoredPolicySourceReason) bool {
	for _, issue := range report.SourcesNotInspected {
		if issue.SourcePath == path && issue.Reason == reason {
			return true
		}
	}
	return false
}
