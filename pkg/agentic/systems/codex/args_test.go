package codex

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// frozenManagedSessionArgs is a byte-for-byte restatement of the source's
// pre-refactor managedCodexSpawnArgs, as it existed before
// TASK-260817-1v9v95 replaced it with a call to the single construction site
// (skill-project-management, cmd/codex_goal_spawn.go; the frozen copy lives in
// cmd/codex_managed_session_args_parity_test.go).
//
// It exists only so the test below can prove parity, and it must NOT be
// refactored, deduplicated or routed through Args. Its entire value is being an
// unmodified independent spelling: the moment it calls the thing it is checking,
// it proves that the function equals itself.
//
// This is the ONLY evidence LaunchModeManagedSession has. The source's parity
// capture harness lived in package spawn and this surface lived in package cmd,
// so it was never captured — the goldens' README states that plainly and warns
// that a port declaring the mode must not read the absence as permission.
func frozenManagedSessionArgs(model, profile, effort, tier string) []string {
	args := []string{
		"--model", strings.TrimSpace(model),
		"--search",
		"--sandbox", "danger-full-access",
		"--ask-for-approval", "never",
	}
	if profile := strings.TrimSpace(profile); profile != "" {
		args = append(args, "--profile", profile)
	}
	if effort := strings.TrimSpace(effort); effort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	if tier := NormalizeServiceTier(tier); tier != "" {
		args = append(args, "-c", fmt.Sprintf("service_tier=%q", tier))
	}
	return args
}

// TestManagedSessionArgsMatchTheFrozenConstruction is the managed-session
// mode's dedicated proof, ported from the source's own.
//
// It sweeps every combination of the three axes that vary the output — profile
// set or not, effort set or not, tier present, absent or unrecognized — because
// a single happy-path comparison would agree with any construction that gets
// the common case right and drops a flag in a branch.
func TestManagedSessionArgsMatchTheFrozenConstruction(t *testing.T) {
	t.Parallel()
	profiles := []string{"", "delivery-profile"}
	efforts := []string{"", "high", "xhigh"}
	tiers := []string{"", "priority", "default", "fast", "standard", "unknown-tier"}

	cases := 0
	for _, profile := range profiles {
		for _, effort := range efforts {
			for _, tier := range tiers {
				req := agentic.LaunchRequest{
					System:      New().ID(),
					Model:       agentic.Model{ID: parityModel},
					Profile:     profile,
					Effort:      effort,
					ServiceTier: tier,
				}
				want := frozenManagedSessionArgs(parityModel, profile, effort, tier)
				got, err := Args(req, agentic.LaunchModeManagedSession)
				if err != nil {
					t.Fatalf("Args(managed-session): %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Args(profile=%q, effort=%q, tier=%q) = %#v, want the historical construction's %#v",
						profile, effort, tier, got, want)
				}
				cases++
			}
		}
	}
	if cases != len(profiles)*len(efforts)*len(tiers) {
		t.Fatalf("exercised %d cases, want %d", cases, len(profiles)*len(efforts)*len(tiers))
	}
}

// TestTheFrozenConstructionAndArgsAreNotTheSameCode is the guard on the test
// above. If somebody ever "cleans up" frozenManagedSessionArgs into a call to
// Args, that test starts comparing a function to itself and passes forever.
//
// The check is narrow on purpose: the frozen copy must differ from Args on at
// least one input the two are not required to agree about — the exec grammar —
// which no self-comparison could produce.
func TestTheFrozenConstructionAndArgsAreNotTheSameCode(t *testing.T) {
	t.Parallel()
	req := agentic.LaunchRequest{
		System:  New().ID(),
		Model:   agentic.Model{ID: parityModel},
		Effort:  "high",
		WorkDir: "/tmp/project",
	}
	exec, err := Args(req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	frozen := frozenManagedSessionArgs(parityModel, "", "high", "")
	if reflect.DeepEqual(exec, frozen) {
		t.Fatal("the frozen managed-session construction produced the exec grammar; it has been routed through the code it is supposed to be checking, and the parity test above now compares a function to itself")
	}
}

// TestTheExecAndDryRunGrammarsAreOneGrammar holds the source's shape: its
// codexDryRunArgs body is a call to the exec construction, and a second
// spelling is the seam the whole single-construction-site effort exists to
// close.
func TestTheExecAndDryRunGrammarsAreOneGrammar(t *testing.T) {
	t.Parallel()
	req := parityRequest("/tmp/project")
	req.Run.BoardDir = "/tmp/project/.task-board"

	exec, err := Args(req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	dryRun, err := Args(req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("Args(dry-run): %v", err)
	}
	if !reflect.DeepEqual(exec, dryRun) {
		t.Errorf("the dry-run argv %#v differs from the exec argv %#v; a dry run that does not mirror the launch is a dry run nobody can trust", dryRun, exec)
	}
}

// TestTheExecGrammarIsExact pins the whole exec argv rather than asserting
// that a few substrings appear in it.
//
// A `strings.Contains` sweep passes for an argv with a flag inserted in the
// wrong position or an extra flag appended, and codex's flag ORDER is
// load-bearing: everything before `exec` is a top-level flag and everything
// after it is a subcommand flag.
func TestTheExecGrammarIsExact(t *testing.T) {
	t.Parallel()
	req := parityRequest("/tmp/project")

	got, err := Args(req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	want := []string{
		"--search", "-a", "never",
		"-p", parityProfile,
		"exec", "-m", parityModel,
		"-c", `model_reasoning_effort="high"`,
		"-c", `service_tier="priority"`,
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		"-C", "/tmp/project",
		"--add-dir", "/tmp/project/.task-board",
		"-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args(exec) =\n  %#v\nwant\n  %#v", got, want)
	}
}

// TestTheOptionalExecFlagsAreOmittedWhenUnset narrows every conditional in the
// exec grammar. Each axis is dropped on its own, so a construction that emitted
// a flag with an empty value — `-p ""`, `--add-dir ""` — fails here rather than
// producing an argv codex rejects at launch.
func TestTheOptionalExecFlagsAreOmittedWhenUnset(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*agentic.LaunchRequest)
		absent  []string
		because string
	}{
		{
			name:    "no profile",
			mutate:  func(r *agentic.LaunchRequest) { r.Profile = "" },
			absent:  []string{"-p"},
			because: "a codex spawn must not pass an implicit profile; the source has a dedicated assertion for exactly that",
		},
		{
			name:    "no board directory",
			mutate:  func(r *agentic.LaunchRequest) { r.Run.BoardDir = "" },
			absent:  []string{"--add-dir"},
			because: "--add-dir with no directory grants the sandbox nothing and fails the launch",
		},
		{
			name:    "no effort",
			mutate:  func(r *agentic.LaunchRequest) { r.Effort = "" },
			absent:  []string{`model_reasoning_effort=""`},
			because: "an empty effort override asks the model for an effort level spelled as the empty string",
		},
		{
			name:    "no service tier",
			mutate:  func(r *agentic.LaunchRequest) { r.ServiceTier = "" },
			absent:  []string{`service_tier=""`},
			because: "an empty tier override is not the same as leaving the account default alone",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := parityRequest("/tmp/project")
			c.mutate(&req)
			got, err := Args(req, agentic.LaunchModeExec)
			if err != nil {
				t.Fatalf("Args(exec): %v", err)
			}
			for _, flag := range c.absent {
				if containsString(got, flag) {
					t.Errorf("argv %#v still carries %q: %s", got, flag, c.because)
				}
			}
		})
	}
}

// TestTheCompositionPrefixLeadsTheExecArgv pins where a validated MCP prefix
// goes: before every codex flag, because everything it contains is a top-level
// `-c` pair and codex reads those only ahead of the subcommand.
func TestTheCompositionPrefixLeadsTheExecArgv(t *testing.T) {
	t.Parallel()
	req := parityRequest("/tmp/project")
	req.Composition = agentic.Composition{
		Prefix:  []string{"-c", `mcp_servers.board.url="http://127.0.0.1:9/mcp"`},
		Servers: []agentic.CompositionServer{{Name: "board", Transport: "http"}},
	}
	got, err := Args(req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	if len(got) < 2 || got[0] != "-c" || !strings.HasPrefix(got[1], "mcp_servers.") {
		t.Fatalf("argv %#v does not lead with the composition prefix", got)
	}
}

// TestArgsDoesNotAliasTheCallersCompositionPrefix holds a boundary that is
// invisible until it bites: the returned argv is appended to by the launcher,
// and a slice sharing the caller's backing array would let one launch's flags
// appear in another's composition.
func TestArgsDoesNotAliasTheCallersCompositionPrefix(t *testing.T) {
	t.Parallel()
	prefix := make([]string, 2, 64)
	prefix[0], prefix[1] = "-c", `mcp_servers.board.url="http://127.0.0.1:9/mcp"`
	req := parityRequest("/tmp/project")
	req.Composition = agentic.Composition{Prefix: prefix}

	if _, err := Args(req, agentic.LaunchModeExec); err != nil {
		t.Fatalf("Args(exec): %v", err)
	}
	if len(prefix) != 2 || prefix[0] != "-c" {
		t.Fatalf("the caller's prefix was mutated to %#v", prefix)
	}
	if cap(prefix) > len(prefix) && prefix[:cap(prefix)][2] != "" {
		t.Errorf("Args wrote %q into the caller's spare capacity; the returned argv shares the caller's array", prefix[:cap(prefix)][2])
	}
}

// TestArgsRefusesAnUndeclaredLaunchMode holds the plugin's own mode refusal,
// separate from BuildPlan's.
func TestArgsRefusesAnUndeclaredLaunchMode(t *testing.T) {
	t.Parallel()
	const notAMode = agentic.LaunchMode(42)
	if _, err := Args(parityRequest("/tmp/project"), notAMode); err == nil {
		t.Error("Args built an argv for a launch mode that does not exist")
	}
	if _, err := New().Argv(parityRequest("/tmp/project"), notAMode); err == nil {
		t.Error("System.Argv built an argv for a launch mode this system never declared")
	}
}

// TestNormalizeServiceTierVocabulary pins the mapping, including the drop.
func TestNormalizeServiceTierVocabulary(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"fast":       "priority",
		"priority":   "priority",
		"PRIORITY":   "priority",
		"  fast  ":   "priority",
		"default":    "default",
		"standard":   "default",
		"":           "",
		"unknown":    "",
		"priority!":  "",
		"prioritize": "",
	}
	for in, want := range cases {
		if got := NormalizeServiceTier(in); got != want {
			t.Errorf("NormalizeServiceTier(%q) = %q, want %q", in, got, want)
		}
	}
}
