package muse

import (
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/argvguard"
	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// This file is the muse half of "ONE argv construction site".
//
// The SCANNER is internal/argvguard, shared with every other plugin's guard —
// one threshold, one resolution depth, one declared-open residual list, one
// scan scope. The scanner's own three residual classes are declared in its
// package comment and demonstrated once, by the codex guard; what is
// demonstrated here is the residual this plugin's own SIGNATURE leaves.

// museArgvSignature is the set of literal strings that identify a function as
// spelling muse CLI argv when even one of them appears in its body — directly,
// or through a package-level const or var it references.
//
// Every entry had to survive one test the source itself supplies: is it spelled
// by any OTHER agent's builder in skill-project-management's spawn package?
// Three candidates failed it and are deliberately absent:
//
//   - "--model" — claude, qwen, gemini and agy all spell it.
//   - "--json" and "exec" — both are ordinary enough to appear in unrelated Go
//     code by accident, and a guard with false positives is a guard somebody
//     deletes.
//
// "--yolo" is muse's alone: qwen spells the VALUE "yolo" as the argument to
// --approval-mode, which is a different literal, and exact matching keeps the
// two apart.
var museArgvSignature = []string{
	"--yolo",
	"--workspace",
	"--prompt-file",
}

const museArgsFile = "pkg/agentic/systems/muse/args.go"

// museArgvConstructionAllowlist names the only sites permitted to spell a
// signature literal, each with the reason it is not a second construction.
//
// Keys are FILE-SCOPED (internal/argvguard.AllowlistKey): several plugins in
// this module name their construction site Args, so a bare-name allowlist would
// exempt every Args in the module from every plugin's guard.
var museArgvConstructionAllowlist = map[string]string{
	argvguard.AllowlistKey(museArgsFile, "Args"): "the single construction site",
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

func scanMuseArgv(t *testing.T, sources map[string]string, allowlist map[string]string) []argvguard.Violation {
	t.Helper()
	violations, err := argvguard.Scan(sources, museArgvSignature, allowlist)
	if err != nil {
		t.Fatalf("argvguard.Scan: %v", err)
	}
	return violations
}

// TestMuseArgvHasExactlyOneConstructionSite is the gate.
func TestMuseArgvHasExactlyOneConstructionSite(t *testing.T) {
	violations := scanMuseArgv(t, moduleGoSources(t), museArgvConstructionAllowlist)
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("found %d muse argv construction site(s) outside Args:\n%s", len(violations), strings.Join(lines, "\n"))
}

// TestTheMuseArgvGuardScansTheWholeModule proves the scan reaches what it
// claims to. A guard that silently scans zero files reports clean forever.
func TestTheMuseArgvGuardScansTheWholeModule(t *testing.T) {
	sources := moduleGoSources(t)
	for _, required := range []string{
		museArgsFile,
		"pkg/agentic/systems/muse/muse.go",
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

// TestTheMuseArgvGuardFiresOnTheRealConstructionSite narrows the gate instead
// of deleting it: the same real sources are rescanned with Args taken OUT of
// the allowlist, and the real Args must then be reported.
func TestTheMuseArgvGuardFiresOnTheRealConstructionSite(t *testing.T) {
	violations := scanMuseArgv(t, moduleGoSources(t), map[string]string{})
	found := false
	for _, v := range violations {
		if v.Name == "Args" && v.File == museArgsFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("with Args removed from the allowlist the real construction site was not reported; the rule does not fire on this module's own code, so its silence elsewhere proves nothing. violations=%v", violations)
	}
}

// TestTheMuseGuardDoesNotFireOnTheOtherPlugins is the cross-plugin
// false-positive bound, measured against real neighbouring code.
//
// qwen is the interesting one: it spells the VALUE "yolo" where this signature
// spells the FLAG "--yolo". Exact matching is what keeps the two apart, and
// this is where that claim is measured rather than asserted.
func TestTheMuseGuardDoesNotFireOnTheOtherPlugins(t *testing.T) {
	sources := moduleGoSources(t)
	for _, plugin := range []string{"codex", "claude", "qwen", "gemini", "agy"} {
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
		if violations := scanMuseArgv(t, subject, museArgvConstructionAllowlist); len(violations) != 0 {
			t.Errorf("muse's signature fires on the %s plugin: %v", plugin, violations)
		}
	}
}

// TestTheMuseArgvGuardCatchesASecondConstructionSite is the mutant set.
func TestTheMuseArgvGuardCatchesASecondConstructionSite(t *testing.T) {
	mutants := []struct {
		name    string
		source  string
		wantFn  string
		because string
	}{
		{
			name:    "a full second construction",
			wantFn:  "buildMuseArgsAgain",
			because: "the shape the source unwound three times for codex, one plugin over",
			source: `package other
func buildMuseArgsAgain(model, workDir, prompt string) []string {
	return []string{"exec", "--json", "--yolo", "--model", model, "--workspace", workDir, "--prompt-file", prompt}
}`,
		},
		{
			name:    "a single signature literal",
			wantFn:  "mirrorWorkspace",
			because: "a copy-paste of the workspace pair alone spells one signature, which a threshold of two admits",
			source: `package other
func mirrorWorkspace(dir string) []string { return []string{"--workspace", dir} }`,
		},
		{
			name:    "the assignment named elsewhere",
			wantFn:  "namePrompt",
			because: "--prompt-file is muse's ONLY assignment transport; a second site naming it is a second answer to what the child is asked to do",
			source: `package other
func namePrompt(path string) []string { return []string{"--prompt-file", path} }`,
		},
		{
			name:    "the approval flag rebuilt elsewhere",
			wantFn:  "skipApproval",
			because: "--yolo decides whether the child stops to ask a human; a second site setting it is a second answer to that question",
			source: `package other
func skipApproval() []string { return []string{"--yolo"} }`,
		},
		{
			name:    "the literal behind a package-level const",
			wantFn:  "workspacePair",
			because: "a const is the first place a second site moves a literal to avoid spelling it twice",
			source: `package other
const workspaceFlag = "--workspace"
func workspacePair(dir string) []string { return []string{workspaceFlag, dir} }`,
		},
		{
			name:    "the literal behind a function-local const",
			wantFn:  "localConstSite",
			because: "the same move one scope down",
			source: `package other
func localConstSite(path string) []string {
	const flag = "--prompt-file"
	return []string{flag, path}
}`,
		},
		{
			name:    "the literals inside a package-level var table",
			wantFn:  "museFlagTable",
			because: "a table of flags is a construction waiting for a caller",
			source: `package other
var museFlagTable = []string{"--yolo", "--workspace"}`,
		},
		{
			name:    "a closure inside a package-level table",
			wantFn:  "adapterTable",
			because: "this is exactly how the source's own adapter table was built",
			source: `package other
var adapterTable = map[string]func() []string{
	"muse": func() []string { return []string{"--yolo"} },
}`,
		},
		{
			name:    "a literal inside a method",
			wantFn:  "Argv",
			because: "a second System implementation spelling its own muse flags is the port-shaped version of this defect",
			source: `package other
type other struct{}
func (other) Argv() []string { return []string{"--prompt-file", "p.md"} }`,
		},
		{
			name:    "the same construction named Args in another file",
			wantFn:  "Args",
			because: "the allowlist exempts the SITE that earned it, not the name",
			source: `package other
func Args() []string { return []string{"--yolo"} }`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			violations := scanMuseArgv(t, map[string]string{"other/mutant.go": mutant.source}, museArgvConstructionAllowlist)
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

// TestTheMuseArgvGuardAcceptsOrdinaryCode is the false-positive bound.
//
// The `yolo` line is the load-bearing one: qwen passes that exact string as the
// value of --approval-mode, so a signature that matched by substring rather
// than by whole literal would fire on the qwen plugin's own construction site.
func TestTheMuseArgvGuardAcceptsOrdinaryCode(t *testing.T) {
	violations := scanMuseArgv(t, map[string]string{
		"other/ordinary.go": `package other

import "strings"

// A comment mentioning --yolo and --prompt-file is prose, not code, and must
// not be reported.
func unrelated(s string) string { return strings.TrimSpace(s) }

func qwenApproval() []string { return []string{"--approval-mode", "yolo"} }

func promptFieldName() string { return "PromptFile" }
`,
	}, museArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the guard reported ordinary code: %v", violations)
	}
}

// TestTheDeclaredResidualStaysOpen demonstrates the gap this plugin's SIGNATURE
// leaves: a second construction that copies only the subcommand, the output
// format and the model spells none of the three entries and walks through.
//
// What it cannot do is skip the approval prompt, point the child at a workspace
// or name the assignment — which is every argument that decides what a muse
// launch actually does. If this ever starts failing, the signature has been
// widened and the comment on museArgvSignature has to be rewritten with it.
func TestTheDeclaredResidualStaysOpen(t *testing.T) {
	violations := scanMuseArgv(t, map[string]string{
		"other/minimal.go": `package other
func minimalCopy(model string) []string { return []string{"exec", "--json", "--model", model} }`,
	}, museArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the subcommand-and-model copy is documented as a residual but the guard now reports it (%v); update the comment on museArgvSignature, or the comment and the code disagree", violations)
	}
}

// TestTheMuseArgvAllowlistIsJustified holds the allowlist's shape: every entry
// carries a reason, and every entry names a site that exists in this module's
// own source, in the file the exemption was written for.
func TestTheMuseArgvAllowlistIsJustified(t *testing.T) {
	declared, err := argvguard.DeclaredNames(moduleGoSources(t))
	if err != nil {
		t.Fatalf("argvguard.DeclaredNames: %v", err)
	}
	for key, reason := range museArgvConstructionAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is allowlisted with no reason; an exemption nobody can argue with is an exemption nobody reviews", key)
		}
		if !declared[key] {
			t.Errorf("%q is allowlisted but is not declared there; the exemption is waiting for somebody to claim it", key)
		}
	}
}
