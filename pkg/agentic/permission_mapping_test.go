package agentic_test

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
)

func TestRegistryPermissionMappingMatrixByEnvironmentModeAndRelease(t *testing.T) {
	systems := []struct {
		id                 agentic.SystemID
		verifiedRelease    string
		unverifiedRelease  string
		grammar            agentic.PermissionGrammarVersion
		yoloMappingSupport bool
	}{
		{"claude-code", "2.1.261", "2.1.262", agentic.PermissionGrammarV2, true},
		{"codex", "0.153.2", "0.153.3", agentic.PermissionGrammarV2, true},
		{"pi-native", "0.84.2", "0.84.3", agentic.PermissionGrammarV1, false},
	}
	modes := []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo}

	for _, system := range systems {
		for _, mode := range modes {
			for _, release := range []struct {
				name     string
				value    string
				verified bool
			}{
				{"verified", system.verifiedRelease, true},
				{"unverified", system.unverifiedRelease, false},
			} {
				name := string(system.id) + "/" + string(mode) + "/" + release.name
				t.Run(name, func(t *testing.T) {
					got, err := agentic.Default.PermissionMapping(system.id, release.value, mode)
					if !release.verified {
						if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
							t.Fatalf("PermissionMapping error = %v, want ErrPermissionModeUnverifiedRelease", err)
						}
						return
					}
					if mode == agentic.PermissionModeYolo && !system.yoloMappingSupport {
						if !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
							t.Fatalf("PermissionMapping error = %v, want ErrPermissionModeUnsupported", err)
						}
						return
					}
					if err != nil {
						t.Fatalf("PermissionMapping: %v", err)
					}
					if got.Grammar != system.grammar {
						t.Errorf("Grammar = %q, want %q", got.Grammar, system.grammar)
					}
					if mode == agentic.PermissionModeNative && got.Flag != "" {
						t.Errorf("native mapping Flag = %q, want no module-owned flag", got.Flag)
					}
					if mode == agentic.PermissionModeYolo && got.Flag == "" {
						t.Error("verified yolo mapping has no flag")
					}
				})
			}
		}
	}
}

func TestRegistryPermissionMappingRefusesUnknownSystemAndMode(t *testing.T) {
	t.Run("unknown system", func(t *testing.T) {
		if _, err := agentic.Default.PermissionMapping("opencode", "1.0.0", agentic.PermissionModeNative); !errors.Is(err, agentic.ErrUnknownSystem) {
			t.Fatalf("PermissionMapping error = %v, want ErrUnknownSystem", err)
		}
	})
	t.Run("registered system without mapping capability", func(t *testing.T) {
		if _, err := agentic.Default.PermissionMapping("pi", "0.84.2", agentic.PermissionModeNative); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
			t.Fatalf("PermissionMapping error = %v, want ErrPermissionModeUnsupported", err)
		}
	})
	t.Run("unknown mode", func(t *testing.T) {
		if _, err := agentic.Default.PermissionMapping("claude-code", "2.1.261", "automatic"); !errors.Is(err, agentic.ErrPermissionModeUnknown) {
			t.Fatalf("PermissionMapping error = %v, want ErrPermissionModeUnknown", err)
		}
	})
}
