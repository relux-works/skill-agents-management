package claude

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Denial tests drive production planning. Caller occurrences are forwarded
// byte-for-byte and the module emits its own denial unconditionally beside
// them; a duplicate deny is harmless and accepted, so no test here asserts
// exactly-once across caller occurrences. Executed narrowing evidence lives
// in tools/launchcontext-mutants.

// denyValues collects the values of every deny-flag occurrence in an emitted
// argv, in every spelling the tool accepts — the `=` form this plugin emits
// and the separate form it must never emit. Only flag positions count:
// prompt text after `--` may spell the flag without carrying it, and the
// tool reads it the same way. Counting substrings rather than splitting
// members keeps the count independent of the production splitter: a splitter
// defect must fail these tests, not be shared by them.
func denyValues(argv []string) []string {
	position := make(map[int]bool, len(argv))
	for _, i := range nativeargs.FlagIndexes(argv) {
		position[i] = true
	}
	var out []string
	for i, arg := range argv {
		if !position[i] {
			continue
		}
		if arg == disallowedToolsFlag || arg == disallowedToolsKebabFlag {
			value := ""
			if i+1 < len(argv) {
				value = argv[i+1]
			}
			out = append(out, value)
			continue
		}
		if name, value, found := strings.Cut(arg, "="); found && (name == disallowedToolsFlag || name == disallowedToolsKebabFlag) {
			out = append(out, value)
		}
	}
	return out
}

// assertSingleDenial holds the whole denial contract for one argv: exactly
// one deny flag, in the emitted `=` spelling, naming AskUserQuestion exactly
// once. It returns the one value for the caller to check further.
func assertSingleDenial(t *testing.T, argv []string) string {
	t.Helper()
	values := denyValues(argv)
	if len(values) != 1 {
		t.Fatalf("the argv carries %d deny-flag occurrence(s), want exactly one: %v", len(values), argv)
	}
	want := disallowedToolsFlag + "=" + values[0]
	emitted := false
	for _, arg := range argv {
		if arg == want {
			emitted = true
		}
	}
	if !emitted {
		t.Fatalf("the denial is not the single emitted `=` token %q: %v", want, argv)
	}
	if n := strings.Count(values[0], deniedToolAskUserQuestion); n != 1 {
		t.Fatalf("the deny value names %s %d time(s), want exactly once: %q", deniedToolAskUserQuestion, n, values[0])
	}
	return values[0]
}

func denyTokenIndex(t *testing.T, argv []string) int {
	t.Helper()
	for i, arg := range argv {
		if name, _, found := strings.Cut(arg, "="); found && (name == disallowedToolsFlag || name == disallowedToolsKebabFlag) {
			return i
		}
	}
	t.Fatalf("no `=` deny token in %v", argv)
	return -1
}

func TestAskUserQuestionDeniedForExec(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)

	argv := argvFor(t, req, agentic.LaunchModeExec)
	value := assertSingleDenial(t, argv)
	if value != deniedToolAskUserQuestion {
		t.Errorf("deny value = %q, want the denial alone when the caller supplied no values", value)
	}
	// Position is contract: after the bypass flag the source's order ends
	// with, and — in goal mode — before the directive pair, which stays
	// last so the `=` form has nothing after it to swallow.
	if skip, deny := indexOf(argv, bypassPermissionsFlag), denyTokenIndex(t, argv); deny < skip {
		t.Errorf("the denial landed before --dangerously-skip-permissions: %v", argv)
	}
}

func TestAskUserQuestionDeniedForGoalMode(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityGoalRunID, parityGoalTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityGoalBody)
	req.Goal = parityGoal()

	argv := argvFor(t, req, agentic.LaunchModeExec)
	assertSingleDenial(t, argv)
	deny, pair := denyTokenIndex(t, argv), indexOf(argv, appendSystemPromptFileFlag)
	if deny < 0 || pair < 0 || deny > pair {
		t.Fatalf("the denial must sit before the goal pair it must not swallow: %v", argv)
	}
	if last := argv[len(argv)-1]; !strings.HasPrefix(last, goalDirectivePrefix) {
		t.Fatalf("the goal directive is not the last argv element, so the denial's position proves nothing about what it swallows: %v", argv)
	}
}

func TestAskUserQuestionDeniedForDryRun(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)

	exec := argvFor(t, req, agentic.LaunchModeExec)
	dry := argvFor(t, req, agentic.LaunchModeDryRun)
	if strings.Join(exec, "\x00") != strings.Join(dry, "\x00") {
		t.Fatalf("the dry run spells a different grammar than the launch it mirrors:\n  exec: %v\n  dry:  %v", exec, dry)
	}
	assertSingleDenial(t, dry)
}

func TestAskUserQuestionDeniedForInteractive(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)

	argv := argvFor(t, req, agentic.LaunchModeInteractive)
	value := assertSingleDenial(t, argv)
	if value != deniedToolAskUserQuestion {
		t.Errorf("deny value = %q, want the denial alone when the caller supplied no values", value)
	}
	if want := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}; !reflect.DeepEqual(argv, want) {
		t.Errorf("Argv = %#v, want %#v", argv, want)
	}
}

func TestAskUserQuestionDeniedThroughBuildPlanWithEnvironment(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req.Env = append(append([]string(nil), req.Env...), "PATH="+binDir)

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	withEnv, err := agentic.BuildPlanWithEnvironment(registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlanWithEnvironment: %v", err)
	}
	assertSingleDenial(t, withEnv.Plan.Argv)

	// The two entry points build one argv: a denial pinned through one and
	// missing from the other is a denial the owned-environment path drops.
	plain, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if !reflect.DeepEqual(withEnv.Plan.Argv, plain.Argv) {
		t.Errorf("BuildPlanWithEnvironment argv = %v, BuildPlan argv = %v; one entry point spells a different denial", withEnv.Plan.Argv, plain.Argv)
	}
}

// TestAskUserQuestionDenialUnconditionalBesideCallerValues is the T2
// denial-side regression: the module emits its own denial occurrence in every
// argv without inspecting caller deny lists, and caller occurrences ride the
// suffix verbatim. Every row pins the whole argv, so a conditional emission
// that suppresses the module denial beside a caller denial fails here —
// including the U+0085 caller rule that Go trimming misread as the bare tool
// and the BOM rule native already reads as the bare tool.
func TestAskUserQuestionDenialUnconditionalBesideCallerValues(t *testing.T) {
	for _, native := range [][]string{
		{"--disallowedTools", "Read"}, {"--disallowedTools=Read"},
		{"--disallowed-tools", "Read"}, {"--disallowed-tools=Read"},
		{"--disallowedTools=Read,Edit"}, {"--disallowedTools=Read Edit"},
		{"--disallowedTools", "Read", "Edit"},
		{"--disallowedTools=Bash(", "--disallowedTools=Read"},
		{"--disallowedTools=Read,Read", "--disallowedTools", "Read"},
		{"--disallowedTools=Bash(git log --format=%H,%s)"},
		{"--verbose", "--disallowedTools=Read", "deploy", "-d"},
		{"--disallowedTools", "Read", "--", "deploy"},
		{"--disallowedTools=Read,AskUserQuestion"},
		{"--disallowedTools", "AskUserQuestion"},
		{"--disallowedTools=AskUserQuestion(*)"},
		{"--disallowedTools=AskUserQuestion()"},
		{"--disallowed-tools=AskUserQuestion"},
		{"--disallowedTools=AskUserQuestion"},
		{"--disallowedTools=\u0085AskUserQuestion"},
		{"--disallowedTools=\ufeffAskUserQuestion"},
	} {
		t.Run(strings.Join(native, " "), func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.NativeArgs = native
			argv := argvFor(t, req, agentic.LaunchModeInteractive)
			want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}, native...)
			if !reflect.DeepEqual(argv, want) {
				t.Fatalf("module denial missing beside caller denial: got %q, want %q", argv, want)
			}
		})
	}
}

// TestAskUserQuestionDenialUnconditionalUnderYolo pins the same unconditional
// emission under yolo: the module denial lands before the bypass flag even
// when the caller already denies the tool.
func TestAskUserQuestionDenialUnconditionalUnderYolo(t *testing.T) {
	for _, native := range [][]string{
		{"--disallowed-tools", "Read,Edit", "--verbose"},
		{"--disallowedTools=AskUserQuestion"},
		{"--disallowed-tools", "Read", "AskUserQuestion"},
	} {
		t.Run(strings.Join(native, " "), func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = "2.1.261"
			req.NativeArgs = native
			argv := argvFor(t, req, agentic.LaunchModeInteractive)
			want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial, bypassPermissionsFlag}, native...)
			if !reflect.DeepEqual(argv, want) {
				t.Fatalf("module denial missing beside caller denial: got %q, want %q", argv, want)
			}
		})
	}
}

// TestReEnablingAskUserQuestionIsRefused is the gate: every spelling of the
// allow list, every value form, in both postures refuses with the typed
// refusal before launch. Each row names its placement too — equals or
// separate token — so a detector that only watches one form fails the other.
func TestReEnablingAskUserQuestionIsRefused(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, posture := range []struct {
		name string
		yolo bool
	}{
		{name: "native", yolo: false},
		{name: "yolo", yolo: true},
	} {
		for _, row := range []struct {
			name      string
			native    []string
			tool      string
			placement agentic.NativePolicyPlacement
		}{
			{name: "separate value", native: []string{"--allowedTools", "AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementSeparateToken},
			{name: "equals value", native: []string{"--allowedTools=AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementEquals},
			{name: "kebab separate value", native: []string{"--allowed-tools", "AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementSeparateToken},
			{name: "kebab equals value", native: []string{"--allowed-tools=AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementEquals},
			{name: "inside a comma list", native: []string{"--allowedTools=Read,AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementEquals},
			{name: "inside a space list", native: []string{"--allowedTools=Read AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementEquals},
			{name: "inside variadic tokens", native: []string{"--allowedTools", "Read", "AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementSeparateToken},
			{name: "rule qualified", native: []string{"--allowedTools=AskUserQuestion(*)"}, tool: "AskUserQuestion(*)", placement: agentic.NativePolicyPlacementEquals},
			{name: "rule qualified separate", native: []string{"--allowedTools", "AskUserQuestion(Read)"}, tool: "AskUserQuestion(Read)", placement: agentic.NativePolicyPlacementSeparateToken},
			{name: "second flag", native: []string{"--allowedTools=Read", "--allowedTools=AskUserQuestion"}, tool: "AskUserQuestion", placement: agentic.NativePolicyPlacementEquals},
		} {
			t.Run(posture.name+"/"+row.name, func(t *testing.T) {
				req := interactiveRequest(workDir)
				if posture.yolo {
					req.PermissionMode = agentic.PermissionModeYolo
					req.ToolRelease = "2.1.261"
				}
				req.NativeArgs = row.native
				err := planErrorFor(t, req, agentic.LaunchModeInteractive)
				if !errors.Is(err, agentic.ErrDeniedToolReEnabled) {
					t.Fatalf("BuildPlan err = %v, want ErrDeniedToolReEnabled", err)
				}
				var typed *agentic.DeniedToolReEnabledError
				if !errors.As(err, &typed) {
					t.Fatalf("BuildPlan err = %v, want a typed DeniedToolReEnabledError", err)
				}
				if typed.Tool != row.tool || typed.Placement != row.placement {
					t.Errorf("typed refusal = (%q, %s), want (%q, %s)", typed.Tool, typed.Placement, row.tool, row.placement)
				}
				// A caller holding the plugin directly meets the same gate:
				// the refusal is the plugin's, not BuildPlan's.
				if _, direct := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(direct, agentic.ErrDeniedToolReEnabled) {
					t.Errorf("Argv err = %v, want ErrDeniedToolReEnabled", direct)
				}
			})
		}
	}
}

// TestDenialFiresBeforeTheYoloReleaseGate pins the check order: the denial
// is release-independent — it refuses a re-enable attempt even where yolo
// itself would fail closed on drift — because AskUserQuestion is denied for
// every agent, not only for agents at a verified release.
func TestDenialFiresBeforeTheYoloReleaseGate(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "9.9.9"
	req.NativeArgs = []string{"--allowedTools", "AskUserQuestion"}
	if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrDeniedToolReEnabled) {
		t.Fatalf("BuildPlan err = %v, want ErrDeniedToolReEnabled (not the drift refusal)", err)
	}
}

// TestAllowingOtherToolsIsAdmitted is the refusal's narrowing: the gate is
// the TOOL's, not a refusal of the allow list. Anything not naming the
// denied tool builds, and the caller's allow spelling rides the suffix
// verbatim beside the single denial.
func TestAllowingOtherToolsIsAdmitted(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, native := range [][]string{
		{"--allowedTools", "Read"},
		{"--allowedTools=Read,Edit"},
		{"--allowed-tools=Read Edit"},
		{"--allowedTools=Bash(git *)"},
	} {
		req := interactiveRequest(workDir)
		req.NativeArgs = native
		argv := argvFor(t, req, agentic.LaunchModeInteractive)
		assertSingleDenial(t, argv)
		if got := argv[len(argv)-len(native):]; !reflect.DeepEqual(got, native) {
			t.Errorf("the allow spelling was rewritten: suffix = %v, want %v (full argv %v)", got, native, argv)
		}
	}
}

// TestNearMissToolNamesDoNotRefuse narrows the match to the exact tool: a
// differently-cased member and a longer member name something else, and a
// gate that substring-matched would refuse launches the tool itself would
// run with the denial intact.
func TestNearMissToolNamesDoNotRefuse(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, member := range []string{"askuserquestion", "AskUserQuestionX", "XAskUserQuestion", "AskUserQuestion2"} {
		req := interactiveRequest(workDir)
		req.NativeArgs = []string{"--allowedTools=" + member}
		argv := argvFor(t, req, agentic.LaunchModeInteractive)
		assertSingleDenial(t, argv)
	}
}

// TestAskUserQuestionECMAScriptRuleTokenization is the T2 refusal-side
// regression: rule tokenization follows the native ECMAScript trim, not Go's.
// U+FEFF and the other ECMAScript-only trimmables around AskUserQuestion
// refuse; U+0085, which Go trims and ECMAScript preserves, is a literal rule
// character and admits. Every row drives BuildPlan; the BOM allow row also
// drives the plugin directly, pinning the refusal as the plugin's.
func TestAskUserQuestionECMAScriptRuleTokenization(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, posture := range []struct {
		name string
		yolo bool
	}{
		{name: "native", yolo: false},
		{name: "yolo", yolo: true},
	} {
		for _, row := range []struct {
			name   string
			member string
			tool   string
			refuse bool
		}{
			{name: "allow_BOM_prefix", member: "\ufeffAskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_BOM_trailing", member: "AskUserQuestion\ufeff", tool: "AskUserQuestion", refuse: true},
			{name: "allow_BOM_both", member: "\ufeffAskUserQuestion\ufeff", tool: "AskUserQuestion", refuse: true},
			{name: "allow_NBSP_prefix", member: "\u00a0AskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_ogham_prefix", member: "\u1680AskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_em_space_prefix", member: "\u2003AskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_line_separator", member: "\u2028AskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_paragraph_separator", member: "\u2029AskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_tab_prefix", member: "\tAskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_BOM_in_comma_list", member: "Read,\ufeffAskUserQuestion", tool: "AskUserQuestion", refuse: true},
			{name: "allow_BOM_qualified", member: "\ufeffAskUserQuestion(*)", tool: "AskUserQuestion(*)", refuse: true},
			{name: "allow_NEL_prefix_admitted", member: "\u0085AskUserQuestion"},
			{name: "allow_NEL_in_comma_list_admitted", member: "Read,\u0085AskUserQuestion"},
			{name: "allow_empty_members_admitted", member: ",\ufeff,\t, "},
		} {
			t.Run(posture.name+"/"+row.name, func(t *testing.T) {
				req := interactiveRequest(workDir)
				if posture.yolo {
					req.PermissionMode = agentic.PermissionModeYolo
					req.ToolRelease = "2.1.261"
				}
				req.NativeArgs = []string{"--allowedTools=" + row.member}
				err := planErrorFor(t, req, agentic.LaunchModeInteractive)
				if !row.refuse {
					if err != nil {
						t.Fatalf("ECMAScript-literal allow member refused: %v", err)
					}
					argv := argvFor(t, req, agentic.LaunchModeInteractive)
					if got := argv[len(argv)-1]; got != "--allowedTools="+row.member {
						t.Fatalf("admitted allow spelling changed: %q", argv)
					}
					return
				}
				if !errors.Is(err, agentic.ErrDeniedToolReEnabled) {
					t.Fatalf("want typed re-enable refusal for %q, got %v", row.member, err)
				}
				var typed *agentic.DeniedToolReEnabledError
				if !errors.As(err, &typed) || typed.Tool != row.tool {
					t.Fatalf("typed refusal = %v, want tool %q", err, row.tool)
				}
				if row.name == "allow_BOM_prefix" {
					if _, direct := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(direct, agentic.ErrDeniedToolReEnabled) {
						t.Fatalf("Argv err = %v, want ErrDeniedToolReEnabled", direct)
					}
				}
			})
		}
	}
}

// TestToolTokensOutsideFlagPositionAreNotPolicy attacks the gate with its
// own tokens where they carry no meaning: after `--` both spellings are
// prompt text, forwarded verbatim — the allow list refuses nothing and the
// deny list is prompt text beside the module denial. A scan that classified
// every flag-shaped token without the separator rule would refuse the first
// and misread the second.
func TestToolTokensOutsideFlagPositionAreNotPolicy(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	t.Run("an allow attempt after the separator is prompt text", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.NativeArgs = []string{"--", "--allowedTools", "AskUserQuestion"}
		argv := argvFor(t, req, agentic.LaunchModeInteractive)
		assertSingleDenial(t, argv)
		if want := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial, "--", "--allowedTools", "AskUserQuestion"}; !reflect.DeepEqual(argv, want) {
			t.Errorf("Argv = %#v, want %#v", argv, want)
		}
	})
	t.Run("a deny spelling after the separator is prompt text beside the module denial", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.NativeArgs = []string{"--", "--disallowedTools=Read"}
		argv := argvFor(t, req, agentic.LaunchModeInteractive)
		value := assertSingleDenial(t, argv)
		if value != deniedToolAskUserQuestion {
			t.Errorf("deny value = %q, want the denial alone: prompt text is not a caller value", value)
		}
	})
	t.Run("a bare denied name as prompt text is not a re-enable", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.NativeArgs = []string{"AskUserQuestion", "what should I do next"}
		argv := argvFor(t, req, agentic.LaunchModeInteractive)
		assertSingleDenial(t, argv)
	})
}

// TestBareDisallowedFlagContributesNothing pins the degenerate value form: a
// caller deny flag with no value is forwarded verbatim beside the module's
// own unconditional denial, never refused and never read as a caller denial.
func TestBareDisallowedFlagContributesNothing(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, row := range []struct {
		name   string
		native []string
		rest   []string
	}{
		{name: "trailing flag", native: []string{"--disallowedTools"}, rest: nil},
		{name: "empty equals value", native: []string{"--disallowedTools="}, rest: nil},
		{name: "flag before the separator", native: []string{"--disallowedTools", "--", "deploy"}, rest: []string{"--", "deploy"}},
		{name: "flag before the next option", native: []string{"--verbose", "--disallowedTools", "--debug"}, rest: []string{"--verbose", "--debug"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.NativeArgs = row.native
			argv := argvFor(t, req, agentic.LaunchModeInteractive)
			value := denyValues(argv)[0]
			if value != deniedToolAskUserQuestion {
				t.Errorf("deny value = %q, want the denial alone", value)
			}
			want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}, row.native...)
			if !reflect.DeepEqual(argv, want) {
				t.Errorf("Argv = %#v, want %#v", argv, want)
			}
		})
	}
}

// TestDenialAssertionBitesWhenTheFlagIsMissing is a counter sanity check, not mutation evidence: the same plans built through a plugin that drops the
// emitted denial must fail the exactly-once assertion — or the assertion
// proves nothing about the argv it claims to hold.
func TestDenialAssertionBitesWhenTheFlagIsMissing(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeInteractive} {
		req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
		if mode == agentic.LaunchModeInteractive {
			req = interactiveRequest(workDir)
		} else {
			req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
		}
		binDir := tempSlot(t)
		writeStubExecutable(t, binDir, executableName)
		req.Env = append(append([]string(nil), req.Env...), "PATH="+binDir)

		// The clean plan must carry exactly one denial first, or the
		// mutant's zero proves nothing about the counter that tells them
		// apart.
		cleanRegistry := agentic.NewRegistry()
		if err := cleanRegistry.Register(New()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		clean, err := agentic.BuildPlan(cleanRegistry, req, mode)
		if err != nil {
			t.Fatalf("BuildPlan(%s): %v", mode, err)
		}
		if values := denyValues(clean.Argv); len(values) != 1 {
			t.Fatalf("BuildPlan(%s): the clean plan carries %d denial(s), so the mutant below measures nothing: %v", mode, len(values), clean.Argv)
		}

		mutantRegistry := agentic.NewRegistry()
		if err := mutantRegistry.Register(denialStrippingSystem{System: New()}); err != nil {
			t.Fatalf("Register: %v", err)
		}
		mutant, err := agentic.BuildPlan(mutantRegistry, req, mode)
		if err != nil {
			t.Fatalf("BuildPlan(%s): %v", mode, err)
		}
		if values := denyValues(mutant.Argv); len(values) != 0 {
			t.Fatalf("BuildPlan(%s): the stripping mutant still reads as %d denial(s); the counter does not tell a missing denial from a present one: %v", mode, len(values), mutant.Argv)
		}
	}
}

// denialStrippingSystem is the presence mutant: the real plugin with the
// emitted denial removed from every argv.
type denialStrippingSystem struct{ agentic.System }

func (m denialStrippingSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	kept := args[:0]
	for _, arg := range args {
		if name, _, found := strings.Cut(arg, "="); found && name == disallowedToolsFlag {
			continue
		}
		if arg == disallowedToolsFlag || arg == disallowedToolsKebabFlag {
			continue
		}
		kept = append(kept, arg)
	}
	return kept, nil
}

// TestForwardDoesNotAliasTheCallerSuffix is the aliasing bound on the caller
// suffix: it is copied, and the caller's slice survives the plan byte for
// byte.
func TestForwardDoesNotAliasTheCallerSuffix(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	native := make([]string, 4, 64)
	copy(native, []string{"--disallowedTools=Read", "--verbose", "deploy", "-d"})

	req := interactiveRequest(workDir)
	req.NativeArgs = native
	before := append([]string(nil), native...)
	argv := argvFor(t, req, agentic.LaunchModeInteractive)
	if got := argv[len(argv)-len(native):]; !reflect.DeepEqual(got, native) {
		t.Fatalf("caller suffix changed: %q", argv)
	}
	if !reflect.DeepEqual([]string(native), before) {
		t.Errorf("the forward rewrote the caller's slice: %v, want %v", []string(native), before)
	}
	if got := native[:cap(native)][len(native)]; got != "" {
		t.Errorf("the caller's backing array was written through: native[%d] = %q", len(native), got)
	}
}

// TestPinnedClaudeAcceptsDisallowedTools is the skippable native check: the
// installed Claude release must list the emitted spelling and the detected
// spellings in `claude --help`. It runs nothing but `--help` and
// `--version` — no prompt, no session, no config or credential touched —
// and skips where no claude binary resolves. The nonsense control proves the
// sweep bites: a help text that listed everything would fail it.
func TestPinnedClaudeAcceptsDisallowedTools(t *testing.T) {
	t.Parallel()
	path, err := exec.LookPath(executableName)
	if err != nil {
		t.Skipf("no %s on PATH, so the installed release cannot be asked about its flags", executableName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	version, versionErr := exec.CommandContext(ctx, path, "--version").Output()
	if versionErr != nil {
		t.Fatalf("claude --version: %v", versionErr)
	}
	if strings.TrimSpace(string(version)) != "2.1.288 (Claude Code)" {
		t.Skipf("installed version %q is not pinned 2.1.288", version)
	}
	help, err := exec.CommandContext(ctx, path, "--help").Output()
	if err != nil {
		t.Fatalf("%s --help: %v", path, err)
	}
	if version, err := exec.CommandContext(ctx, path, "--version").Output(); err != nil {
		t.Logf("%s --version: %v (continuing: the flag sweep below is the check)", path, err)
	} else {
		t.Logf("installed release: %s", strings.TrimSpace(string(version)))
	}
	for _, spelling := range []string{disallowedToolsFlag, disallowedToolsKebabFlag, allowedToolsFlag, allowedToolsKebabFlag} {
		if !strings.Contains(string(help), spelling) {
			t.Errorf("%s --help does not list %s, so the denial's spelling is unverified on this release", path, spelling)
		}
	}
	if control := "--askuserquestion-native-check-probe"; strings.Contains(string(help), control) {
		t.Errorf("%s --help lists the nonsense control %s, so the sweep above proves nothing", path, control)
	}
}
