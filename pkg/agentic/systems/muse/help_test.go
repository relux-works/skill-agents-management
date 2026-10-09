package muse

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestParseHelpDeclaration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		help     string
		flag     string
		declared bool
		lines    string
	}{
		{"bare", "--yolo", "--yolo", true, "--yolo"},
		{"indented-with-description", "Options:\n  --yolo   Skip approval prompts\n", "--yolo", true, "--yolo   Skip approval prompts"},
		{"tab-comma", "\t--yolo,--disable-approval\n", "--yolo", true, "--yolo,--disable-approval"},
		{"crlf", "Options:\r\n  --yolo\r\n", "--yolo", true, "--yolo"},
		{"mid-line-prose", "See --yolo below.\n", "--yolo", false, ""},
		{"example", "$ muse --yolo do it\n", "--yolo", false, ""},
		{"period", "--yolo. Next sentence.\n", "--yolo", false, ""},
		{"equals", "--yolo=true\n", "--yolo", false, ""},
		{"longer-name", "--yolo-like flags are ignored\n", "--yolo", false, ""},
		{"suffix-letter", "--yoloX\n", "--yolo", false, ""},
		{"empty-help", "", "--yolo", false, ""},
		{"empty-flag", "--yolo\n", "", false, ""},
		{"other-flag", "  --model <id>\n", "--yolo", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseHelpDeclaration([]byte(tc.help), tc.flag)
			if got.declared != tc.declared || got.lines != tc.lines {
				t.Fatalf("parseHelpDeclaration(%q) = (%v, %q), want (%v, %q)",
					tc.help, got.declared, got.lines, tc.declared, tc.lines)
			}
		})
	}
	t.Run("multiple", func(t *testing.T) {
		t.Parallel()
		got := parseHelpDeclaration([]byte("  --yolo one\n  --yolo two\n"), "--yolo")
		if !got.declared || got.lines != "--yolo one\n--yolo two" {
			t.Fatalf("parseHelpDeclaration multiple = (%v, %q), want both lines joined", got.declared, got.lines)
		}
	})
}

// TestYoloRequiresOptionDeclarationEvidence pins the yolo evidence gate:
// a help text declaring the bypass flag as its own option maps, while
// prose about the flag and option-like lookalikes refuse typed as
// unsupported on that binary. The matcher never infers from prose or
// from a longer option name.
func TestYoloRequiresOptionDeclarationEvidence(t *testing.T) {
	t.Parallel()
	version := "printf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\n"
	t.Run("declared", func(t *testing.T) {
		t.Parallel()
		_, env := writeMuseVersionHelpStub(t, version, museYoloHelpFixture)
		plan, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
		}, agentic.LaunchModeInteractive)
		if err != nil {
			t.Fatalf("BuildPlan with declared yolo: %v", err)
		}
		if err := assertMuseYoloExactlyOnce(plan); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name string
		help string
	}{
		{"prose-only", museNoYoloHelpFixture},
		{"longer-name", "Usage: muse\n\nOptions:\n  --model <id>\n--yolo-like flags are ignored\n"},
		{"equals-form", "Usage: muse\n\nOptions:\n  --model <id>\n--yolo=true\n"},
		{"period", "Usage: muse\n\nOptions:\n  --model <id>\n--yolo. See the manual.\n"},
		{"example", "Usage: muse\n\nRun \"$ muse --yolo\" to skip approvals.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, env := writeMuseVersionHelpStub(t, version, tc.help)
			_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
				System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
				ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
			}, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
				t.Fatalf("BuildPlan with %s help err = %v, want ErrPermissionModeUnsupported", tc.name, err)
			}
		})
	}
}
