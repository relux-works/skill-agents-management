package codex

import (
	"errors"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReviewRefusalBeforeBinaryResolution(t *testing.T) {
	req := settingsRequest(t, t.TempDir(), t.TempDir(), []agentic.LocalProviderSetting{{Key: "model", Value: "true"}})
	for i, item := range req.Env {
		if strings.HasPrefix(item, "PATH=") {
			req.Env[i] = "PATH=" + filepath.Join(t.TempDir(), "missing-binary-directory")
		}
	}
	_, err := buildCodexPlan(t, req)
	if !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
		t.Fatalf("settings refusal must precede filesystem-dependent binary resolution: %v", err)
	}
}

func TestReviewInjectionAcrossBindingPaths(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	base := catalogRequest(t, home, work, "low")
	snapshot := mustSnapshot(t, base)
	cases := []struct {
		k, v string
		kind agentic.LocalProviderRefusalKind
	}{
		{"hide_agent_reasoning=true", "true", agentic.LocalProviderUnsupported},
		{"\"hide_agent_reasoning\"", "true", agentic.LocalProviderUnsupported},
		{"hide_agent_reasoning\nmodel", "true", agentic.LocalProviderMalformed},
		{"hide_agent_reasoning.extra", "true", agentic.LocalProviderUnsupported},
		{"-hide_agent_reasoning", "true", agentic.LocalProviderUnsupported},
		{"HIDE_AGENT_REASONING", "true", agentic.LocalProviderUnsupported},
		{"hide_agent_reasoning", "true\nmodel=\"other\"", agentic.LocalProviderMalformed},
		{"hide_agent_reasoning", "\"true\"", agentic.LocalProviderMalformed},
		{"hide_agent_reasoning", "false=1", agentic.LocalProviderMalformed},
		{"hide_agent_reasoning", "--sandbox", agentic.LocalProviderMalformed},
		{"hide_agent_reasoning", "TRUE", agentic.LocalProviderMalformed},
		{"hide_agent_reasoning", "true\x00", agentic.LocalProviderMalformed},
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		for _, useSnapshot := range []bool{false, true} {
			for _, c := range cases {
				req := base
				req.LocalProvider = &agentic.LocalProviderBinding{ID: base.LocalProvider.ID, Settings: []agentic.LocalProviderSetting{{Key: c.k, Value: c.v}}}
				if useSnapshot {
					req.LocalProvider.Snapshot = snapshot
				}
				beforeRoot, beforeEntries := catalogArtifactSnapshot(t)
				_, err := buildCodexPlanForMode(t, req, mode)
				var refusal *agentic.LocalProviderRefusal
				if !errors.As(err, &refusal) || refusal.Kind != c.kind {
					t.Fatalf("wrong typed refusal: %v", err)
				}
				if strings.Contains(err.Error(), c.k) || strings.Contains(err.Error(), c.v) || strings.Contains(err.Error(), home) {
					t.Fatalf("error leaks setting/path: %v", err)
				}
				if root, entries := catalogArtifactSnapshot(t); root != beforeRoot || !reflect.DeepEqual(entries, beforeEntries) {
					t.Fatal("catalog materialised")
				}
			}
		}
	}
}

func TestReviewCallerMutationAfterPlan(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := settingsRequest(t, home, work, []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning", Value: "true"}})
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	req.LocalProvider.Settings[0] = agentic.LocalProviderSetting{Key: "model", Value: "true"}
	if !containsPair(plan.Argv, "-c", "hide_agent_reasoning=true") {
		t.Fatal("caller changed argv")
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
}

func TestReviewSnapshotSeesOverwrite(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	_, err := buildCodexPlan(t, catalogRequest(t, home, work, "low"))
	if err != nil {
		t.Fatal(err)
	}
	root, before := catalogArtifactSnapshot(t)
	for name := range before {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
		defer func() { os.WriteFile(path, data, 0600); os.Chmod(path, info.Mode()) }()
		if err := os.WriteFile(path, append(data, byte(' ')), 0600); err != nil {
			t.Fatal(err)
		}
		_, after := catalogArtifactSnapshot(t)
		if reflect.DeepEqual(before, after) {
			t.Fatal("same-name overwrite unseen")
		}
		return
	}
	t.Fatal("no catalog artifact")
}
