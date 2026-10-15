package muse

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func validMuseTestContext() *agentic.CuratorContext {
	return &agentic.CuratorContext{
		Revision:    agentic.CuratorLaunchFragmentV4,
		Environment: "muse",
		Profile:     agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
		Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env: map[string]string{
			"XDG_CONFIG_HOME": "/managed/muse-home/config",
			"XDG_DATA_HOME":   "/managed/muse-home/data",
			"XDG_STATE_HOME":  "/managed/muse-home/state",
			"XDG_CACHE_HOME":  "/managed/muse-home/cache",
			agentic.CuratorMuseCredentialBackendVariable: agentic.CuratorMuseCredentialBackendFile,
		},
		Permissions:     &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: false, Source: agentic.CuratorPermissionSourceProfile},
		CredentialStore: &agentic.CuratorCredentialStore{Selection: agentic.CuratorCredentialStoreFile},
	}
}

func museTestEnvValue(t *testing.T, env []string, name string) string {
	t.Helper()
	value, found := "", false
	for _, entry := range env {
		if key, val, ok := strings.Cut(entry, "="); ok && key == name {
			value, found = val, true
		}
	}
	if !found {
		t.Fatalf("child env carries no %s entry: %v", name, env)
	}
	return value
}

// TestMuseChildEnvAppliesCuratorHomes drives ChildEnv directly: the
// fragment's four XDG values replace the inherited ones and HOME passes
// through untouched.
func TestMuseChildEnvAppliesCuratorHomes(t *testing.T) {
	parent := []string{
		"PATH=/bin",
		"HOME=/home/operator",
		"XDG_CONFIG_HOME=/junk/config",
		"XDG_DATA_HOME=/junk/data",
		"XDG_STATE_HOME=/junk/state",
		"XDG_CACHE_HOME=/junk/cache",
		"XDG_RUNTIME_DIR=/junk/run",
	}
	req := agentic.LaunchRequest{System: "muse", Home: "/managed/muse-home", Env: parent, Context: validMuseTestContext()}
	env, err := New().ChildEnv(parent, req)
	if err != nil {
		t.Fatalf("ChildEnv: %v", err)
	}
	for variable, want := range req.Context.Env {
		if got := museTestEnvValue(t, env, variable); got != want {
			t.Fatalf("child env %s = %q, want curator value %q", variable, got, want)
		}
	}
	if got := museTestEnvValue(t, env, "HOME"); got != "/home/operator" {
		t.Fatalf("child env HOME = %q, want the inherited /home/operator untouched", got)
	}
	if got := museTestEnvValue(t, env, "XDG_RUNTIME_DIR"); got != "/junk/run" {
		t.Fatalf("child env XDG_RUNTIME_DIR = %q, want the inherited value untouched", got)
	}
	if got := museTestEnvValue(t, env, museNoAutoUpdateEnv); got != "1" {
		t.Fatalf("child env %s = %q, want the updater pin", museNoAutoUpdateEnv, got)
	}
}

// TestMuseChildEnvRefusesCuratorPlusNetwork holds the direct-caller line:
// a caller holding the plugin itself gets the same combination refusal
// BuildPlan reports through the validator.
func TestMuseChildEnvRefusesCuratorPlusNetwork(t *testing.T) {
	req := agentic.LaunchRequest{
		System:  "muse",
		Home:    "/managed/muse-home",
		Env:     []string{"PATH=/bin"},
		Context: validMuseTestContext(),
		Network: agentic.Network{Patch: envpatch.Patch{Unset: []string{"HTTP_PROXY"}}},
	}
	if _, err := New().ChildEnv(nil, req); !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
		t.Fatalf("ChildEnv err = %v, want ErrCuratorContextUnsupported", err)
	}
}

// TestMuseChildEnvRefusesInvalidCuratorDirectly holds the direct-caller
// validation line: an invalid fragment is refused from ChildEnv with the
// same typed refusal BuildPlan reports.
func TestMuseChildEnvRefusesInvalidCuratorDirectly(t *testing.T) {
	context := validMuseTestContext()
	context.Env["XDG_DATA_HOME"] = "/foreign/muse/data"
	req := agentic.LaunchRequest{System: "muse", Home: "/managed/muse-home", Env: []string{"PATH=/bin"}, Context: context}
	if _, err := New().ChildEnv(nil, req); !errors.Is(err, agentic.ErrCuratorContextMalformed) {
		t.Fatalf("ChildEnv err = %v, want ErrCuratorContextMalformed", err)
	}
}
