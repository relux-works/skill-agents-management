package claude

import (
	"errors"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReviewerScalarValueOwnership(t *testing.T) {
	for _, flag := range []string{"--append-system-prompt", "--system-prompt", "--name", "-n", "--remote-control", "--model"} {
		t.Run(flag, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.NativeArgs = []string{flag, "--allowedTools=AskUserQuestion"}
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			if flag == "--remote-control" {
				// Native optional arguments do not own a following dash-leading option.
				if !errors.Is(err, agentic.ErrDeniedToolReEnabled) {
					t.Fatalf("optional value hid an allow option: %v", err)
				}
				req.NativeArgs = []string{flag + "=--allowedTools=AskUserQuestion"}
				err = planErrorFor(t, req, agentic.LaunchModeInteractive)
			}
			if err != nil {
				t.Fatalf("scalar value classified as flag: %v", err)
			}
		})
	}
}

func TestReviewerSettingsRefusal(t *testing.T) {
	for _, kind := range []string{"inline", "file", "missing", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			value := `{"permissions":{"allow":["AskUserQuestion"]}}`
			if kind != "inline" {
				path := filepath.Join(req.WorkDir, "caller-settings.json")
				if kind == "invalid" {
					value = "{"
				}
				if kind != "missing" {
					if err := os.WriteFile(path, []byte(value), 0600); err != nil {
						t.Fatal(err)
					}
				}
				value = path
			}
			req.NativeArgs = []string{"--settings", value}
			if err := planErrorFor(t, req, agentic.LaunchModeInteractive); err == nil {
				t.Fatal("settings input admitted without required typed refusal")
			}
		})
	}
}

func TestReviewerCallerOccurrencesPreserved(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.NativeArgs = []string{"--disallowed-tools", "Bash(git *) Edit", "--disallowedTools=Read", "--", "prompt"}
	argv := argvFor(t, req, agentic.LaunchModeInteractive)
	suffix := argv[len(argv)-len(req.NativeArgs):]
	if !reflect.DeepEqual(suffix, req.NativeArgs) {
		t.Fatalf("caller bytes changed: want %q, argv %q", req.NativeArgs, argv)
	}
}

func TestPanelMergeBoundary(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.NativeArgs = []string{"--disallowedTools=Bash(", "--disallowedTools=Read"}
	argv := argvFor(t, req, agentic.LaunchModeInteractive)
	t.Logf("actual plan argv=%q", argv)
	if got := argv[len(argv)-len(req.NativeArgs):]; !reflect.DeepEqual(got, req.NativeArgs) {
		t.Fatalf("native rule occurrences changed: %q", argv)
	}
}
