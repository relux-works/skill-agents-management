package claude

import (
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/argvguard"
	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// This file is the claude half of "ONE argv construction site".
//
// The SCANNER is internal/argvguard, shared with the codex plugin's guard and
// with every plugin ported after this one. That sharing is the whole point of
// the extraction: one threshold, one resolution depth, one declared-open
// residual list, one scan scope. Two scanners would be two definitions of the
// rule, and both report clean when they disagree — which is the failure the
// rule itself exists to prevent.
//
// What is claude's and stays here: the signature literals, the allowlist, and
// the demonstration that both bite on this module's own code.

// claudeArgvSignature is the set of literal strings that identify a function as
// spelling claude CLI argv when even one of them appears in its body — directly,
// or through a package-level const or var it references.
//
// Every entry had to survive one test the source itself supplies: is it spelled
// by any OTHER agent's builder in skill-project-management's spawn package? Two
// candidates failed it and are deliberately absent:
//
//   - "--dangerously-skip-permissions" — buildAgyArgs spells it too
//     (spawn.go:1185, and agy_preflight.go's own flag list). Including it would
//     make this guard fire on a legitimate agy plugin the day one is ported,
//     and a guard that reports honest code is a guard somebody deletes.
//   - "--output-format" — buildQwenArgs and buildAgyArgs both spell it.
//
// The residual that leaves is worth stating rather than discovering: a second
// claude construction that copies ONLY the unconditional prompt-mode flags —
// `-p --output-format json --model <m> --dangerously-skip-permissions` — spells
// none of the four below and walks through. What it cannot do is carry effort,
// a budget or a goal, because each of those is one of the four, and a claude
// launch this module actually makes carries at least the first.
var claudeArgvSignature = []string{
	"--append-system-prompt-file",
	"--max-budget-usd",
	"--effort",
	goalDirectivePrefix,
}

const (
	claudeArgsFile = "pkg/agentic/systems/claude/args.go"
	claudeGoalFile = "pkg/agentic/systems/claude/goal.go"
)

// claudeArgvConstructionAllowlist names the only sites permitted to spell a
// signature literal, each with the reason it is not a second construction.
//
// Keys are FILE-SCOPED (internal/argvguard.AllowlistKey), and this plugin is
// why that is not optional: both this plugin and codex name their construction
// site Args, so a bare-name allowlist would exempt every Args in the module from
// every plugin's guard.
//
// Args is THE construction site. goalDirective renders the one positional
// argument the goal branch appends: it carries no flag, it is called from
// exactly one place, and inlining it into Args is what would put the `/goal `
// literal next to a refusal that is easier to read on its own.
var claudeArgvConstructionAllowlist = map[string]string{
	argvguard.AllowlistKey(claudeArgsFile, "Args"):                "the single construction site",
	argvguard.AllowlistKey(claudeGoalFile, "goalDirective"):       "renders the goal branch's one positional argument and refuses a goal that carries no predicate; it spells no flag",
	argvguard.AllowlistKey(claudeGoalFile, "goalDirectivePrefix"): "the directive literal itself, declared once so the guard's signature and the construction cannot drift apart",
}

// moduleGoSources reads every non-test Go file this module's build compiles,
// through the same walk pkg/agentic's single-source guard uses.
func moduleGoSources(t *testing.T) map[string]string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the working directory: %v", err)
	}
	root, err := gosources.Root(dir)
	if err != nil {
		t.Fatalf("locating the module root: %v", err)
	}
	sources, err := gosources.Walk(root)
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	return sources
}

func scanClaudeArgv(t *testing.T, sources map[string]string, allowlist map[string]string) []argvguard.Violation {
	t.Helper()
	violations, err := argvguard.Scan(sources, claudeArgvSignature, allowlist)
	if err != nil {
		t.Fatalf("argvguard.Scan: %v", err)
	}
	return violations
}

// TestClaudeArgvHasExactlyOneConstructionSite is the gate.
func TestClaudeArgvHasExactlyOneConstructionSite(t *testing.T) {
	violations := scanClaudeArgv(t, moduleGoSources(t), claudeArgvConstructionAllowlist)
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("found %d claude argv construction site(s) outside Args:\n%s", len(violations), strings.Join(lines, "\n"))
}

// TestTheClaudeArgvGuardScansTheWholeModule proves the scan reaches what it
// claims to. A guard that silently scans zero files reports clean forever, and
// the widening over the source — the whole module rather than one package — is
// only worth something if the files it names are actually read.
func TestTheClaudeArgvGuardScansTheWholeModule(t *testing.T) {
	sources := moduleGoSources(t)
	for _, required := range []string{
		claudeArgsFile,
		claudeGoalFile,
		"pkg/agentic/systems/codex/args.go",
		"pkg/agentic/registry.go",
		"tools/agents-management/cmd/plugins.go",
	} {
		if _, ok := sources[required]; !ok {
			t.Errorf("the scan did not reach %s; a file the guard never reads is a file it never guards", required)
		}
	}
}

// TestTheClaudeArgvGuardFiresOnTheRealConstructionSite narrows the gate instead
// of deleting it: the same real sources are rescanned with Args taken OUT of the
// allowlist, and the real Args must then be reported.
//
// Without this, a green result above is equally consistent with "the rule never
// matches anything", and the guard would be worth nothing while looking healthy.
func TestTheClaudeArgvGuardFiresOnTheRealConstructionSite(t *testing.T) {
	narrowed := map[string]string{}
	for key, reason := range claudeArgvConstructionAllowlist {
		if key == argvguard.AllowlistKey(claudeArgsFile, "Args") {
			continue
		}
		narrowed[key] = reason
	}
	violations := scanClaudeArgv(t, moduleGoSources(t), narrowed)
	found := false
	for _, v := range violations {
		if v.Name == "Args" && v.File == claudeArgsFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("with Args removed from the allowlist the real construction site was not reported; the rule does not fire on this module's own code, so its silence elsewhere proves nothing. violations=%v", violations)
	}
}

// TestTheClaudeGuardDoesNotFireOnTheCodexPlugin is the cross-plugin
// false-positive bound, and it is measured against real neighbouring code
// rather than a fixture.
//
// One module now holds two harnesses' argv grammars, and each guard scans the
// whole module. If claude's signature fired on codex's construction site the
// two plugins would need each other's names in their allowlists, and every
// future plugin would need both — which is a cross-plugin coupling that grows
// quadratically and ends with somebody deleting a guard.
func TestTheClaudeGuardDoesNotFireOnTheCodexPlugin(t *testing.T) {
	sources := moduleGoSources(t)
	codex := map[string]string{}
	for name, src := range sources {
		if strings.HasPrefix(name, "pkg/agentic/systems/codex/") {
			codex[name] = src
		}
	}
	if len(codex) == 0 {
		t.Fatal("the codex plugin's sources were not found, so this bound measured nothing")
	}
	if violations := scanClaudeArgv(t, codex, claudeArgvConstructionAllowlist); len(violations) != 0 {
		t.Errorf("claude's signature fires on the codex plugin: %v", violations)
	}
}

// TestTheClaudeArgvGuardCatchesASecondConstructionSite is the mutant set: every
// ordinary Go spelling a second site could take, each planted in a synthetic
// corpus and each required to be reported.
//
// The single-literal cases are the ones that matter most. The source's guard
// used a threshold of two and a review showed a copy-pasted site spelling only
// one signature walked through it.
func TestTheClaudeArgvGuardCatchesASecondConstructionSite(t *testing.T) {
	mutants := []struct {
		name    string
		source  string
		wantFn  string
		because string
	}{
		{
			name:    "a full second construction",
			wantFn:  "buildClaudeArgsAgain",
			because: "the shape the source unwound three times for codex, one plugin over",
			source: `package other
func buildClaudeArgsAgain(model, effort string) []string {
	return []string{"-p", "--output-format", "json", "--model", model, "--effort", effort, "--dangerously-skip-permissions"}
}`,
		},
		{
			name:    "a single signature literal",
			wantFn:  "mirrorGoalBinding",
			because: "a copy-paste of the goal branch alone spells only the system-prompt flag, which a threshold of two admits",
			source: `package other
func mirrorGoalBinding(path string) []string { return []string{"--append-system-prompt-file", path} }`,
		},
		{
			name:    "the goal directive rebuilt elsewhere",
			wantFn:  "rebuildDirective",
			because: "the predicate is the one argument that must come from the board's renderer; a second site building it is a second renderer",
			source: `package other
func rebuildDirective(condition string) string { return "/goal " + condition }`,
		},
		{
			name:    "the literal behind a package-level const",
			wantFn:  "budgetCeiling",
			because: "a const is the first place a second site moves a literal to avoid spelling it twice",
			source: `package other
const budgetFlag = "--max-budget-usd"
func budgetCeiling(usd string) []string { return []string{budgetFlag, usd} }`,
		},
		{
			name:    "the literal behind a function-local const",
			wantFn:  "localConstSite",
			because: "the same move one scope down",
			source: `package other
func localConstSite(effort string) []string {
	const flag = "--effort"
	return []string{flag, effort}
}`,
		},
		{
			name:    "the literals inside a package-level var table",
			wantFn:  "claudeFlagTable",
			because: "a table of flags is a construction waiting for a caller",
			source: `package other
var claudeFlagTable = []string{"--effort", "--max-budget-usd"}`,
		},
		{
			name:    "a function referencing that var",
			wantFn:  "spliceFlags",
			because: "the caller of such a table spells the flags as surely as the table does",
			source: `package other
var claudeFlags = []string{"--append-system-prompt-file"}
func spliceFlags(args []string) []string { return append(args, claudeFlags...) }`,
		},
		{
			name:    "a closure inside a package-level table",
			wantFn:  "adapterTable",
			because: "this is exactly how the source's own adapter table was built, so a construction hidden in one has a live precedent",
			source: `package other
var adapterTable = map[string]func() []string{
	"claude": func() []string { return []string{"--effort", "high"} },
}`,
		},
		{
			name:    "a literal inside init()",
			wantFn:  "init",
			because: "init is an ordinary body and a site that runs at startup is still a site",
			source: `package other
var flags []string
func init() { flags = append(flags, "--max-budget-usd") }`,
		},
		{
			name:    "a literal inside a method",
			wantFn:  "Argv",
			because: "a second System implementation spelling its own claude flags is the port-shaped version of this defect",
			source: `package other
type other struct{}
func (other) Argv() []string { return []string{"--append-system-prompt-file", "x"} }`,
		},
		{
			name:    "a call-wrapped literal inside a function body",
			wantFn:  "wrapped",
			because: "the wrapping alone hides nothing: the body walk sees the literal. Only the indirection through a package-level name is a residual, and this case is what draws that line",
			source: `package other
func wrapped() []string { return []string{string([]byte("--effort"))} }`,
		},
		{
			name:    "the same construction named Args in another file",
			wantFn:  "Args",
			because: "the allowlist exempts the SITE that earned it, not the name; two plugins in this module already both name their site Args",
			source: `package other
func Args(effort string) []string { return []string{"--effort", effort} }`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			violations := scanClaudeArgv(t, map[string]string{"other/mutant.go": mutant.source}, claudeArgvConstructionAllowlist)
			found := false
			for _, v := range violations {
				if v.Name == mutant.wantFn {
					found = true
				}
			}
			if !found {
				t.Errorf("the guard admitted a second construction site (%s): %s\nviolations=%v", mutant.name, mutant.because, violations)
			}
		})
	}
}

// TestTheClaudeArgvGuardAcceptsOrdinaryCode is the false-positive bound. A
// guard that fires on unrelated code is a guard somebody deletes, and then
// nothing is watching the thing it was written for.
func TestTheClaudeArgvGuardAcceptsOrdinaryCode(t *testing.T) {
	violations := scanClaudeArgv(t, map[string]string{
		"other/ordinary.go": `package other

import "strings"

// A comment mentioning --effort and --max-budget-usd is prose, not code, and
// must not be reported.
func unrelated(s string) string { return strings.TrimSpace(s) }

func otherFlags() []string { return []string{"--verbose", "--json", "--model", "claude"} }

func effortFieldName() string { return "Effort" }

func goalFieldName() string { return "ProviderCondition" }
`,
	}, claudeArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the guard reported ordinary code: %v", violations)
	}
}

// TestTheDeclaredResidualStaysOpen demonstrates the one gap this plugin's
// SIGNATURE leaves — as opposed to the scanner's, which internal/argvguard
// declares and the codex guard demonstrates.
//
// A comment naming a residual with nothing holding it to the code is the same
// defect one level up: prose a reader trusts. If this ever starts failing, the
// signature has been widened and the comment on claudeArgvSignature has to be
// rewritten with it.
func TestTheDeclaredResidualStaysOpen(t *testing.T) {
	violations := scanClaudeArgv(t, map[string]string{
		"other/minimal.go": `package other
func minimalCopy(model string) []string {
	return []string{"-p", "--output-format", "json", "--model", model, "--dangerously-skip-permissions"}
}`,
	}, claudeArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the unconditional-flags copy is documented as a residual but the guard now reports it (%v); update the comment on claudeArgvSignature, or the comment and the code disagree", violations)
	}
}

// TestTheClaudeArgvAllowlistIsJustified holds the allowlist's shape: every
// entry carries a reason, and every entry names a site that exists in this
// module's own source, in the file the exemption was written for.
func TestTheClaudeArgvAllowlistIsJustified(t *testing.T) {
	declared, err := argvguard.DeclaredNames(moduleGoSources(t))
	if err != nil {
		t.Fatalf("argvguard.DeclaredNames: %v", err)
	}
	for key, reason := range claudeArgvConstructionAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is allowlisted with no reason; an exemption nobody can argue with is an exemption nobody reviews", key)
		}
		if !declared[key] {
			t.Errorf("%q is allowlisted but is not declared there; the exemption is waiting for somebody to claim it", key)
		}
	}
}
