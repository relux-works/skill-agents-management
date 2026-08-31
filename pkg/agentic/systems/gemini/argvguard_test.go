package gemini

import (
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/argvguard"
	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// This file is the gemini half of "ONE argv construction site".
//
// The SCANNER is internal/argvguard, shared with every other plugin's guard —
// one threshold, one resolution depth, one declared-open residual list, one
// scan scope. The scanner's own three residual classes are declared in its
// package comment and demonstrated once, by the codex guard; what is
// demonstrated here is the residual this plugin's own SIGNATURE leaves.

// geminiArgvSignature is the set of literal strings that identify a function as
// spelling gemini CLI argv when even one of them appears in its body — directly,
// or through a package-level const or var it references.
//
// Every entry had to survive one test the source itself supplies: is it spelled
// by any OTHER agent's builder in skill-project-management's spawn package?
// Gemini's grammar is mostly single-letter flags — -p, -m, -y, -o — and none of
// them is defensible as a signature: they are short enough to appear in
// unrelated code by accident, and a guard with false positives is a guard
// somebody deletes. The two entries below are the ones only buildGeminiArgs
// spells.
var geminiArgvSignature = []string{
	"--skip-trust",
	"--include-directories",
}

const geminiArgsFile = "pkg/agentic/systems/gemini/args.go"

// geminiArgvConstructionAllowlist names the only sites permitted to spell a
// signature literal, each with the reason it is not a second construction.
//
// Keys are FILE-SCOPED (internal/argvguard.AllowlistKey): several plugins in
// this module name their construction site Args, so a bare-name allowlist would
// exempt every Args in the module from every plugin's guard.
var geminiArgvConstructionAllowlist = map[string]string{
	argvguard.AllowlistKey(geminiArgsFile, "Args"): "the single construction site",
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

func scanGeminiArgv(t *testing.T, sources map[string]string, allowlist map[string]string) []argvguard.Violation {
	t.Helper()
	violations, err := argvguard.Scan(sources, geminiArgvSignature, allowlist)
	if err != nil {
		t.Fatalf("argvguard.Scan: %v", err)
	}
	return violations
}

// TestGeminiArgvHasExactlyOneConstructionSite is the gate.
func TestGeminiArgvHasExactlyOneConstructionSite(t *testing.T) {
	violations := scanGeminiArgv(t, moduleGoSources(t), geminiArgvConstructionAllowlist)
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("found %d gemini argv construction site(s) outside Args:\n%s", len(violations), strings.Join(lines, "\n"))
}

// TestTheGeminiArgvGuardScansTheWholeModule proves the scan reaches what it
// claims to. A guard that silently scans zero files reports clean forever.
func TestTheGeminiArgvGuardScansTheWholeModule(t *testing.T) {
	sources := moduleGoSources(t)
	for _, required := range []string{
		geminiArgsFile,
		"pkg/agentic/systems/gemini/env.go",
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

// TestTheGeminiArgvGuardFiresOnTheRealConstructionSite narrows the gate instead
// of deleting it: the same real sources are rescanned with Args taken OUT of the
// allowlist, and the real Args must then be reported.
func TestTheGeminiArgvGuardFiresOnTheRealConstructionSite(t *testing.T) {
	violations := scanGeminiArgv(t, moduleGoSources(t), map[string]string{})
	found := false
	for _, v := range violations {
		if v.Name == "Args" && v.File == geminiArgsFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("with Args removed from the allowlist the real construction site was not reported; the rule does not fire on this module's own code, so its silence elsewhere proves nothing. violations=%v", violations)
	}
}

// TestTheGeminiGuardDoesNotFireOnTheOtherPlugins is the cross-plugin
// false-positive bound, measured against real neighbouring code.
func TestTheGeminiGuardDoesNotFireOnTheOtherPlugins(t *testing.T) {
	sources := moduleGoSources(t)
	for _, plugin := range []string{"codex", "claude", "qwen", "muse", "agy"} {
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
		if violations := scanGeminiArgv(t, subject, geminiArgvConstructionAllowlist); len(violations) != 0 {
			t.Errorf("gemini's signature fires on the %s plugin: %v", plugin, violations)
		}
	}
}

// TestTheGeminiArgvGuardCatchesASecondConstructionSite is the mutant set.
func TestTheGeminiArgvGuardCatchesASecondConstructionSite(t *testing.T) {
	mutants := []struct {
		name    string
		source  string
		wantFn  string
		because string
	}{
		{
			name:    "a full second construction",
			wantFn:  "buildGeminiArgsAgain",
			because: "the shape the source unwound three times for codex, one plugin over",
			source: `package other
func buildGeminiArgsAgain(model, workDir string) []string {
	return []string{"-p", "", "-m", model, "-y", "--skip-trust", "-o", "json", "--include-directories", workDir}
}`,
		},
		{
			name:    "a single signature literal",
			wantFn:  "mirrorWorkspace",
			because: "a copy-paste of the workspace pair alone spells one signature, which a threshold of two admits",
			source: `package other
func mirrorWorkspace(dir string) []string { return []string{"--include-directories", dir} }`,
		},
		{
			name:    "the trust flag rebuilt elsewhere",
			wantFn:  "skipTrust",
			because: "--skip-trust decides whether the child asks a human before touching the workspace; a second site setting it is a second answer to that question",
			source: `package other
func skipTrust() []string { return []string{"--skip-trust"} }`,
		},
		{
			name:    "the literal behind a package-level const",
			wantFn:  "workspacePair",
			because: "a const is the first place a second site moves a literal to avoid spelling it twice",
			source: `package other
const dirFlag = "--include-directories"
func workspacePair(dir string) []string { return []string{dirFlag, dir} }`,
		},
		{
			name:    "the literal behind a function-local const",
			wantFn:  "localConstSite",
			because: "the same move one scope down",
			source: `package other
func localConstSite() []string {
	const flag = "--skip-trust"
	return []string{flag}
}`,
		},
		{
			name:    "the literals inside a package-level var table",
			wantFn:  "geminiFlagTable",
			because: "a table of flags is a construction waiting for a caller",
			source: `package other
var geminiFlagTable = []string{"--skip-trust", "--include-directories"}`,
		},
		{
			name:    "a closure inside a package-level table",
			wantFn:  "adapterTable",
			because: "this is exactly how the source's own adapter table was built",
			source: `package other
var adapterTable = map[string]func() []string{
	"gemini": func() []string { return []string{"--skip-trust"} },
}`,
		},
		{
			name:    "a literal inside a method",
			wantFn:  "Argv",
			because: "a second System implementation spelling its own gemini flags is the port-shaped version of this defect",
			source: `package other
type other struct{}
func (other) Argv() []string { return []string{"--include-directories", "."} }`,
		},
		{
			name:    "the same construction named Args in another file",
			wantFn:  "Args",
			because: "the allowlist exempts the SITE that earned it, not the name",
			source: `package other
func Args() []string { return []string{"--skip-trust"} }`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			violations := scanGeminiArgv(t, map[string]string{"other/mutant.go": mutant.source}, geminiArgvConstructionAllowlist)
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

// TestTheGeminiArgvGuardAcceptsOrdinaryCode is the false-positive bound.
func TestTheGeminiArgvGuardAcceptsOrdinaryCode(t *testing.T) {
	violations := scanGeminiArgv(t, map[string]string{
		"other/ordinary.go": `package other

import "strings"

// A comment mentioning --skip-trust and --include-directories is prose, not
// code, and must not be reported.
func unrelated(s string) string { return strings.TrimSpace(s) }

func otherFlags() []string { return []string{"--verbose", "--json", "-m", "gemini"} }

func includeFieldName() string { return "IncludeDirectories" }
`,
	}, geminiArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the guard reported ordinary code: %v", violations)
	}
}

// TestTheDeclaredResidualStaysOpen demonstrates the gap this plugin's SIGNATURE
// leaves: a second construction that copies only the single-letter flags spells
// neither entry and walks through.
//
// What it cannot do is skip the trust prompt or hand the child a workspace,
// which are the two arguments that change what a gemini launch is allowed to
// touch. If this ever starts failing, the signature has been widened and the
// comment on geminiArgvSignature has to be rewritten with it.
func TestTheDeclaredResidualStaysOpen(t *testing.T) {
	violations := scanGeminiArgv(t, map[string]string{
		"other/minimal.go": `package other
func minimalCopy(model string) []string { return []string{"-p", "", "-m", model, "-y", "-o", "json"} }`,
	}, geminiArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the single-letter copy is documented as a residual but the guard now reports it (%v); update the comment on geminiArgvSignature, or the comment and the code disagree", violations)
	}
}

// TestTheGeminiArgvAllowlistIsJustified holds the allowlist's shape: every
// entry carries a reason, and every entry names a site that exists in this
// module's own source, in the file the exemption was written for.
func TestTheGeminiArgvAllowlistIsJustified(t *testing.T) {
	declared, err := argvguard.DeclaredNames(moduleGoSources(t))
	if err != nil {
		t.Fatalf("argvguard.DeclaredNames: %v", err)
	}
	for key, reason := range geminiArgvConstructionAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is allowlisted with no reason; an exemption nobody can argue with is an exemption nobody reviews", key)
		}
		if !declared[key] {
			t.Errorf("%q is allowlisted but is not declared there; the exemption is waiting for somebody to claim it", key)
		}
	}
}
