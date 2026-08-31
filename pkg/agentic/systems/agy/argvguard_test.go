package agy

import (
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/argvguard"
	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// This file is the agy half of "ONE argv construction site".
//
// The SCANNER is internal/argvguard, shared with every other plugin's guard —
// one threshold, one resolution depth, one declared-open residual list, one
// scan scope. The scanner's own three residual classes are declared in its
// package comment and demonstrated once, by the codex guard; what is
// demonstrated here is the residual this plugin's own SIGNATURE leaves.

// agyArgvSignature is the set of literal strings that identify a function as
// spelling Antigravity CLI argv when even one of them appears in its body —
// directly, or through a package-level const or var it references.
//
// Every entry had to survive one test the source itself supplies: is it spelled
// by any OTHER agent's builder in skill-project-management's spawn package?
// Three candidates failed it and are deliberately absent:
//
//   - "--dangerously-skip-permissions" — buildClaudeArgs spells it too, which
//     is why claude's own signature excludes it as well. Including it here
//     would make this guard fire on the claude plugin, and a guard that reports
//     honest code is a guard somebody deletes.
//   - "--output-format" — buildQwenArgs and buildClaudeArgs both spell it.
//   - "--add-dir" — CODEX spells it, to hand its child the board directory
//     (adapter.go:332, and pkg/agentic/systems/codex/args.go here). This one
//     was not predicted: it was in this signature until the guard reported the
//     codex plugin's own Args, which is the cross-plugin bound doing exactly
//     what it exists for. The flag is still in agy's real argv; it is simply
//     not evidence that a function is spelling AGY argv.
//
// The two below are Antigravity's alone. `--mode` was considered and rejected
// for the same reason as the single-letter flags elsewhere: it is a word
// ordinary code uses.
var agyArgvSignature = []string{
	"--print-timeout",
	"--disable-slash-commands",
}

const agyArgsFile = "pkg/agentic/systems/agy/args.go"

// agyArgvConstructionAllowlist names the only sites permitted to spell a
// signature literal, each with the reason it is not a second construction.
//
// Keys are FILE-SCOPED (internal/argvguard.AllowlistKey): several plugins in
// this module name their construction site Args, so a bare-name allowlist would
// exempt every Args in the module from every plugin's guard.
var agyArgvConstructionAllowlist = map[string]string{
	argvguard.AllowlistKey(agyArgsFile, "Args"): "the single construction site",
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

func scanAgyArgv(t *testing.T, sources map[string]string, allowlist map[string]string) []argvguard.Violation {
	t.Helper()
	violations, err := argvguard.Scan(sources, agyArgvSignature, allowlist)
	if err != nil {
		t.Fatalf("argvguard.Scan: %v", err)
	}
	return violations
}

// TestAgyArgvHasExactlyOneConstructionSite is the gate.
func TestAgyArgvHasExactlyOneConstructionSite(t *testing.T) {
	violations := scanAgyArgv(t, moduleGoSources(t), agyArgvConstructionAllowlist)
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("found %d agy argv construction site(s) outside Args:\n%s", len(violations), strings.Join(lines, "\n"))
}

// TestTheAgyArgvGuardScansTheWholeModule proves the scan reaches what it claims
// to. A guard that silently scans zero files reports clean forever.
func TestTheAgyArgvGuardScansTheWholeModule(t *testing.T) {
	sources := moduleGoSources(t)
	for _, required := range []string{
		agyArgsFile,
		"pkg/agentic/systems/agy/runtime.go",
		"pkg/agentic/systems/agy/binary.go",
		"pkg/agentic/systems/codex/args.go",
		"pkg/agentic/systems/claude/args.go",
		"pkg/agentic/registry.go",
		"tools/agents-management/cmd/plugins.go",
	} {
		if _, ok := sources[required]; !ok {
			t.Errorf("the scan did not reach %s; a file the guard never reads is a file it never guards", required)
		}
	}
}

// TestTheAgyArgvGuardFiresOnTheRealConstructionSite narrows the gate instead of
// deleting it: the same real sources are rescanned with Args taken OUT of the
// allowlist, and the real Args must then be reported.
func TestTheAgyArgvGuardFiresOnTheRealConstructionSite(t *testing.T) {
	violations := scanAgyArgv(t, moduleGoSources(t), map[string]string{})
	found := false
	for _, v := range violations {
		if v.Name == "Args" && v.File == agyArgsFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("with Args removed from the allowlist the real construction site was not reported; the rule does not fire on this module's own code, so its silence elsewhere proves nothing. violations=%v", violations)
	}
}

// TestTheAgyGuardDoesNotFireOnTheOtherPlugins is the cross-plugin
// false-positive bound, measured against real neighbouring code.
//
// claude is the interesting one: it spells --dangerously-skip-permissions,
// which agy's argv carries too. Leaving that literal out of this signature is
// what keeps the two plugins from needing each other's names in their
// allowlists, and this is where the claim is measured rather than asserted.
func TestTheAgyGuardDoesNotFireOnTheOtherPlugins(t *testing.T) {
	sources := moduleGoSources(t)
	for _, plugin := range []string{"codex", "claude", "qwen", "gemini", "muse"} {
		prefix := "pkg/agentic/systems/" + plugin + "/"
		subject := map[string]string{}
		for name, src := range sources {
			if strings.HasPrefix(name, prefix) {
				subject[name] = src
			}
		}
		if len(subject) == 0 {
			t.Errorf("the %s plugin's sources were not found, so this bound measured nothing for it", plugin)
			continue
		}
		if violations := scanAgyArgv(t, subject, agyArgvConstructionAllowlist); len(violations) != 0 {
			t.Errorf("agy's signature fires on the %s plugin: %v", plugin, violations)
		}
	}
}

// TestTheAgyArgvGuardCatchesASecondConstructionSite is the mutant set.
func TestTheAgyArgvGuardCatchesASecondConstructionSite(t *testing.T) {
	mutants := []struct {
		name    string
		source  string
		wantFn  string
		because string
	}{
		{
			name:    "a full second construction",
			wantFn:  "buildAgyArgsAgain",
			because: "the shape the source unwound three times for codex, one plugin over",
			source: `package other
func buildAgyArgsAgain(prompt, model, workDir string) []string {
	return []string{"--print", prompt, "--model", model, "--print-timeout", "30m", "--disable-slash-commands", "--add-dir", workDir}
}`,
		},
		{
			name:    "a single signature literal",
			wantFn:  "mirrorSlashCommands",
			because: "a copy-paste of one flag spells only one signature, which a threshold of two admits",
			source: `package other
func mirrorSlashCommands() []string { return []string{"--disable-slash-commands"} }`,
		},
		{
			name:    "the tracked timeout rebuilt elsewhere",
			wantFn:  "trackedTimeout",
			because: "--print-timeout is what stops a tracked run hanging forever; a second site setting it is a second answer to how long a run may take",
			source: `package other
func trackedTimeout() []string { return []string{"--print-timeout", "10m"} }`,
		},
		{
			name:    "the literal behind a package-level const",
			wantFn:  "slashCommands",
			because: "a const is the first place a second site moves a literal to avoid spelling it twice",
			source: `package other
const disableSlash = "--disable-slash-commands"
func slashCommands() []string { return []string{disableSlash} }`,
		},
		{
			name:    "the literal behind a function-local const",
			wantFn:  "localConstSite",
			because: "the same move one scope down",
			source: `package other
func localConstSite() []string {
	const flag = "--print-timeout"
	return []string{flag, "30m"}
}`,
		},
		{
			name:    "the literals inside a package-level var table",
			wantFn:  "agyFlagTable",
			because: "a table of flags is a construction waiting for a caller",
			source: `package other
var agyFlagTable = []string{"--print-timeout", "--disable-slash-commands"}`,
		},
		{
			name:    "a closure inside a package-level table",
			wantFn:  "adapterTable",
			because: "this is exactly how the source's own adapter table was built",
			source: `package other
var adapterTable = map[string]func() []string{
	"agy": func() []string { return []string{"--disable-slash-commands"} },
}`,
		},
		{
			name:    "a literal inside a method",
			wantFn:  "Argv",
			because: "a second System implementation spelling its own agy flags is the port-shaped version of this defect",
			source: `package other
type other struct{}
func (other) Argv() []string { return []string{"--disable-slash-commands"} }`,
		},
		{
			name:    "the same construction named Args in another file",
			wantFn:  "Args",
			because: "the allowlist exempts the SITE that earned it, not the name",
			source: `package other
func Args() []string { return []string{"--print-timeout", "30m"} }`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			violations := scanAgyArgv(t, map[string]string{"other/mutant.go": mutant.source}, agyArgvConstructionAllowlist)
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

// TestTheAgyArgvGuardAcceptsOrdinaryCode is the false-positive bound.
//
// Two lines are load-bearing. --dangerously-skip-permissions is in agy's real
// argv and deliberately NOT in its signature, so a signature that grew it would
// start reporting the claude plugin's own construction site; --add-dir is the
// same story for codex, and it is the one this guard actually caught.
func TestTheAgyArgvGuardAcceptsOrdinaryCode(t *testing.T) {
	violations := scanAgyArgv(t, map[string]string{
		"other/ordinary.go": `package other

import "strings"

// A comment mentioning --print-timeout and --add-dir is prose, not code, and
// must not be reported.
func unrelated(s string) string { return strings.TrimSpace(s) }

func claudeFlags(model string) []string {
	return []string{"-p", "--output-format", "json", "--model", model, "--dangerously-skip-permissions"}
}

func codexBoardDir(dir string) []string { return []string{"--add-dir", dir} }

func timeoutFieldName() string { return "PrintTimeout" }
`,
	}, agyArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the guard reported ordinary code: %v", violations)
	}
}

// TestTheDeclaredResidualStaysOpen demonstrates the gap this plugin's SIGNATURE
// leaves: a second construction that copies the print/model/output/mode
// arguments AND the workspace pair still spells neither entry and walks
// through.
//
// The workspace pair is in it deliberately — --add-dir left the signature
// because codex spells it, and this is where that cost is made visible rather
// than only explained. What such a copy cannot do is bound the run's duration
// or disable slash commands. If this ever starts failing, the signature has
// been widened and the comment on agyArgvSignature has to be rewritten with it.
func TestTheDeclaredResidualStaysOpen(t *testing.T) {
	violations := scanAgyArgv(t, map[string]string{
		"other/minimal.go": `package other
func minimalCopy(prompt, model, workDir string) []string {
	return []string{"--print", prompt, "--model", model, "--output-format", "json", "--mode", "accept-edits", "--add-dir", workDir}
}`,
	}, agyArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the print-and-model copy is documented as a residual but the guard now reports it (%v); update the comment on agyArgvSignature, or the comment and the code disagree", violations)
	}
}

// TestTheAgyArgvAllowlistIsJustified holds the allowlist's shape: every entry
// carries a reason, and every entry names a site that exists in this module's
// own source, in the file the exemption was written for.
func TestTheAgyArgvAllowlistIsJustified(t *testing.T) {
	declared, err := argvguard.DeclaredNames(moduleGoSources(t))
	if err != nil {
		t.Fatalf("argvguard.DeclaredNames: %v", err)
	}
	for key, reason := range agyArgvConstructionAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is allowlisted with no reason; an exemption nobody can argue with is an exemption nobody reviews", key)
		}
		if !declared[key] {
			t.Errorf("%q is allowlisted but is not declared there; the exemption is waiting for somebody to claim it", key)
		}
	}
}
