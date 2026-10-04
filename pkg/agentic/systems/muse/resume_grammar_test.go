package muse

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The pinned per-release root-option inventories, byte for byte. Both
// verified releases were captured from real binaries (1.4.1-R4503.1 and
// the checksum-verified channel-pinned 1.4.2-R4684.1): `muse resume --help`
// is identical across them, and `muse --help` differs only by 1.4.2's
// added model-profile subcommand, which owns no option arity. Any drift in
// either table — a removed row included — fails this pinning.
var (
	museResumePinnedScalars = []string{
		"--agents",
		"--approval-judge",
		"--approval-mode",
		"--base-url",
		"--echo-delay-ms",
		"--image",
		"--model",
		"--permission-profile",
		"--preset",
		"--provider",
		"--reasoning-effort",
		"--sandbox-network",
		"--workspace",
		"--worktree-base",
		"--worktree-existing",
	}
	museResumePinnedBooleans = []string{
		"--disable-approval",
		"--disable-sandbox",
		"--disable-shell",
		"--disable-write",
		"--enable-shell-tool",
		"--help",
		"--no-parallel-tool-calls",
		"--no-session-log",
		"--parallel-tool-calls",
		"--subagent-worktree-isolation",
		"--trust-workspace",
		"--version",
		"--yolo",
		"-V",
		"-h",
	}
)

func sortedMuseResumeKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for name := range set {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

// TestMuseResumeGrammarPinsBothReleases requires each embedded per-release
// inventory to equal the captured option sets exactly, the enforced union
// to equal them too, and ElevateResumeIntent to accept the documented
// resume forms while refusing unknown options through the production
// entry point.
func TestMuseResumeGrammarPinsBothReleases(t *testing.T) {
	for _, tc := range []struct {
		release string
		table   string
	}{
		{release: "1.4.1", table: museResumeOptionTable141},
		{release: "1.4.2", table: museResumeOptionTable142},
	} {
		scalars, booleans := parseMuseResumeOptions(tc.table)
		if got := sortedMuseResumeKeys(scalars); !reflect.DeepEqual(got, museResumePinnedScalars) {
			t.Fatalf("%s resume inventory lost: scalars = %#v, want %#v", tc.release, got, museResumePinnedScalars)
		}
		if got := sortedMuseResumeKeys(booleans); !reflect.DeepEqual(got, museResumePinnedBooleans) {
			t.Fatalf("%s resume inventory lost: booleans = %#v, want %#v", tc.release, got, museResumePinnedBooleans)
		}
	}
	if got := sortedMuseResumeKeys(museResumeScalars); !reflect.DeepEqual(got, museResumePinnedScalars) {
		t.Fatalf("enforced resume scalars = %#v, want %#v", got, museResumePinnedScalars)
	}
	if got := sortedMuseResumeKeys(museResumeBooleans); !reflect.DeepEqual(got, museResumePinnedBooleans) {
		t.Fatalf("enforced resume booleans = %#v, want %#v", got, museResumePinnedBooleans)
	}
	system := New()
	for _, native := range [][]string{
		{"resume", "--last"},
		{"resume", "550e8400-e29b-41d4-a716-446655440000"},
		{"--model", "muse-spark", "resume", "--last"},
		{"resume", "--last", "--workspace", "/tmp/work"},
		{"--yolo", "resume", "my-session"},
	} {
		if _, err := system.ElevateResumeIntent(nil, native); err != nil {
			t.Fatalf("ElevateResumeIntent(%q) = %v, want acceptance under both verified releases", native, err)
		}
	}
	if _, err := system.ElevateResumeIntent(nil, []string{"resume", "--quantum", "x"}); err == nil {
		t.Fatal("ElevateResumeIntent admitted an unknown option")
	}
	if _, err := system.ElevateResumeIntent(nil, []string{"resume", "a", "b"}); err == nil {
		t.Fatal("ElevateResumeIntent admitted a second positional")
	}
	var _ agentic.ResumeSelectorElevator = system
}

// TestMuseResumeGrammarRefusesBarePickerForm pins the contract r2
// section 2 reconciliation: bare `resume` is the picker form and stays
// refused with session_resume_invalid on both verified releases, while
// resume --last and resume <ref> stay admitted (pinned above).
// ElevateResumeIntent takes no release, so the subtests share the one
// classifier; the release labels document that both pinned inventories
// carry the same refusal.
func TestMuseResumeGrammarRefusesBarePickerForm(t *testing.T) {
	for _, release := range []string{"1.4.1-R4503.1", "1.4.2-R4684.1"} {
		t.Run(release, func(t *testing.T) {
			_, err := New().ElevateResumeIntent(nil, []string{"resume"})
			invalid := &agentic.ResumeInvalidError{}
			if !errors.As(err, &invalid) || invalid.Reason != "picker selector" {
				t.Fatalf("ElevateResumeIntent([resume]) = %v, want session_resume_invalid: picker selector", err)
			}
			if !errors.Is(err, agentic.ErrSessionResumeInvalid) {
				t.Fatalf("ElevateResumeIntent([resume]) = %v, want the session_resume_invalid sentinel", err)
			}
		})
	}
}

// TestMuseResumeGrammarRefusesArityContradiction proves the union fails
// loud: an option scalar in one table and boolean in another cannot be
// classified without a release, so parsing panics instead of guessing.
func TestMuseResumeGrammarRefusesArityContradiction(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("contradictory resume arity parsed without panicking")
		}
	}()
	parseMuseResumeOptions("--model\trequired\n--model\tnone\n")
}
