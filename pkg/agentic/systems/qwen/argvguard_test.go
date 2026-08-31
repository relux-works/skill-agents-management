package qwen

import (
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/argvguard"
	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// This file is the qwen half of "ONE argv construction site".
//
// The SCANNER is internal/argvguard, shared with every other plugin's guard.
// That sharing is the point of the extraction: one threshold, one resolution
// depth, one declared-open residual list, one scan scope. Two scanners would be
// two definitions of the rule, and both report clean when they disagree.
//
// The scanner's own three residual classes are declared in its package comment
// and demonstrated once, by the codex guard. They are not restated here: the
// same scanner reached through a second signature would report the same three
// classes, and a duplicate would grow the maintenance without adding evidence.
// What IS demonstrated here is the residual this plugin's own SIGNATURE leaves.
//
// What is qwen's and stays here: the signature literals, the allowlist, and the
// demonstration that both bite on this module's own code.

// qwenArgvSignature is the set of literal strings that identify a function as
// spelling qwen CLI argv when even one of them appears in its body — directly,
// or through a package-level const or var it references.
//
// Every entry had to survive one test the source itself supplies: is it spelled
// by any OTHER agent's builder in skill-project-management's spawn package? Two
// candidates failed it and are deliberately absent:
//
//   - "--model" — buildMuseArgs and buildAgyArgs spell it too, and so does
//     claude's builder. A guard that fires on honest neighbouring code is a
//     guard somebody deletes.
//   - "--output-format" — buildAgyArgs and buildClaudeArgs both spell it.
//     "stream-json" below is the qwen-specific half of that pair: it is the
//     VALUE only qwen passes, so a second site selecting qwen's wire protocol
//     is caught while agy's `--output-format json` is not touched.
var qwenArgvSignature = []string{
	"--approval-mode",
	"--input-format",
	"stream-json",
}

const qwenArgsFile = "pkg/agentic/systems/qwen/args.go"

// qwenArgvConstructionAllowlist names the only sites permitted to spell a
// signature literal, each with the reason it is not a second construction.
//
// Keys are FILE-SCOPED (internal/argvguard.AllowlistKey), and this module is
// why that is not optional: several plugins name their construction site Args,
// so a bare-name allowlist would exempt every Args in the module from every
// plugin's guard.
//
// Args is THE construction site, and for qwen it is the only entry: the
// dry-run mirror substitutes nothing, so there is no second function rendering
// a fragment the way claude's goal directive does.
var qwenArgvConstructionAllowlist = map[string]string{
	argvguard.AllowlistKey(qwenArgsFile, "Args"): "the single construction site",
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

func scanQwenArgv(t *testing.T, sources map[string]string, allowlist map[string]string) []argvguard.Violation {
	t.Helper()
	violations, err := argvguard.Scan(sources, qwenArgvSignature, allowlist)
	if err != nil {
		t.Fatalf("argvguard.Scan: %v", err)
	}
	return violations
}

// TestQwenArgvHasExactlyOneConstructionSite is the gate.
func TestQwenArgvHasExactlyOneConstructionSite(t *testing.T) {
	violations := scanQwenArgv(t, moduleGoSources(t), qwenArgvConstructionAllowlist)
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("found %d qwen argv construction site(s) outside Args:\n%s", len(violations), strings.Join(lines, "\n"))
}

// TestTheQwenArgvGuardScansTheWholeModule proves the scan reaches what it
// claims to. A guard that silently scans zero files reports clean forever.
func TestTheQwenArgvGuardScansTheWholeModule(t *testing.T) {
	sources := moduleGoSources(t)
	for _, required := range []string{
		qwenArgsFile,
		"pkg/agentic/systems/qwen/stdin.go",
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

// TestTheQwenArgvGuardFiresOnTheRealConstructionSite narrows the gate instead
// of deleting it: the same real sources are rescanned with Args taken OUT of the
// allowlist, and the real Args must then be reported.
//
// Without this, a green result above is equally consistent with "the rule never
// matches anything", and the guard would be worth nothing while looking healthy.
func TestTheQwenArgvGuardFiresOnTheRealConstructionSite(t *testing.T) {
	violations := scanQwenArgv(t, moduleGoSources(t), map[string]string{})
	found := false
	for _, v := range violations {
		if v.Name == "Args" && v.File == qwenArgsFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("with Args removed from the allowlist the real construction site was not reported; the rule does not fire on this module's own code, so its silence elsewhere proves nothing. violations=%v", violations)
	}
}

// TestTheQwenGuardDoesNotFireOnTheOtherPlugins is the cross-plugin
// false-positive bound, measured against real neighbouring code rather than a
// fixture.
//
// One module now holds six harnesses' argv grammars, and each guard scans the
// whole module. If qwen's signature fired on another plugin's construction site
// the two would need each other's names in their allowlists, and every future
// plugin would need all of them — a coupling that grows quadratically and ends
// with somebody deleting a guard.
func TestTheQwenGuardDoesNotFireOnTheOtherPlugins(t *testing.T) {
	sources := moduleGoSources(t)
	for _, plugin := range []string{"codex", "claude", "gemini", "muse", "agy"} {
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
		if violations := scanQwenArgv(t, subject, qwenArgvConstructionAllowlist); len(violations) != 0 {
			t.Errorf("qwen's signature fires on the %s plugin: %v", plugin, violations)
		}
	}
}

// TestTheQwenArgvGuardCatchesASecondConstructionSite is the mutant set: the
// ordinary Go spellings a second site could take, each planted in a synthetic
// corpus and each required to be reported.
//
// The single-literal cases matter most. The source's guard used a threshold of
// two and a review showed a copy-pasted site spelling only one signature walked
// through it.
func TestTheQwenArgvGuardCatchesASecondConstructionSite(t *testing.T) {
	mutants := []struct {
		name    string
		source  string
		wantFn  string
		because string
	}{
		{
			name:    "a full second construction",
			wantFn:  "buildQwenArgsAgain",
			because: "the shape the source unwound three times for codex, one plugin over",
			source: `package other
func buildQwenArgsAgain(model string) []string {
	return []string{"--model", model, "--approval-mode", "yolo", "--input-format", "stream-json", "--output-format", "stream-json"}
}`,
		},
		{
			name:    "a single signature literal",
			wantFn:  "mirrorApproval",
			because: "a copy-paste of one pair spells only one signature, which a threshold of two admits",
			source: `package other
func mirrorApproval() []string { return []string{"--approval-mode", "yolo"} }`,
		},
		{
			name:    "the wire protocol selected elsewhere",
			wantFn:  "selectProtocol",
			because: "the stream-json value is what makes stdin a control protocol rather than a prompt; a second site choosing it changes what the child reads",
			source: `package other
func selectProtocol() []string { return []string{"--output-format", "stream-json"} }`,
		},
		{
			name:    "the literal behind a package-level const",
			wantFn:  "approvalPair",
			because: "a const is the first place a second site moves a literal to avoid spelling it twice",
			source: `package other
const approvalFlag = "--approval-mode"
func approvalPair(mode string) []string { return []string{approvalFlag, mode} }`,
		},
		{
			name:    "the literal behind a function-local const",
			wantFn:  "localConstSite",
			because: "the same move one scope down",
			source: `package other
func localConstSite() []string {
	const flag = "--input-format"
	return []string{flag, "stream-json"}
}`,
		},
		{
			name:    "the literals inside a package-level var table",
			wantFn:  "qwenFlagTable",
			because: "a table of flags is a construction waiting for a caller",
			source: `package other
var qwenFlagTable = []string{"--approval-mode", "--input-format"}`,
		},
		{
			name:    "a closure inside a package-level table",
			wantFn:  "adapterTable",
			because: "this is exactly how the source's own adapter table was built, so a construction hidden in one has a live precedent",
			source: `package other
var adapterTable = map[string]func() []string{
	"qwen": func() []string { return []string{"--approval-mode", "yolo"} },
}`,
		},
		{
			name:    "a literal inside a method",
			wantFn:  "Argv",
			because: "a second System implementation spelling its own qwen flags is the port-shaped version of this defect",
			source: `package other
type other struct{}
func (other) Argv() []string { return []string{"--input-format", "stream-json"} }`,
		},
		{
			name:    "the same construction named Args in another file",
			wantFn:  "Args",
			because: "the allowlist exempts the SITE that earned it, not the name; several plugins in this module already name their site Args",
			source: `package other
func Args() []string { return []string{"--approval-mode", "yolo"} }`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			violations := scanQwenArgv(t, map[string]string{"other/mutant.go": mutant.source}, qwenArgvConstructionAllowlist)
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

// TestTheQwenArgvGuardAcceptsOrdinaryCode is the false-positive bound. A guard
// that fires on unrelated code is a guard somebody deletes, and then nothing is
// watching the thing it was written for.
func TestTheQwenArgvGuardAcceptsOrdinaryCode(t *testing.T) {
	violations := scanQwenArgv(t, map[string]string{
		"other/ordinary.go": `package other

import "strings"

// A comment mentioning --approval-mode and stream-json is prose, not code, and
// must not be reported.
func unrelated(s string) string { return strings.TrimSpace(s) }

func otherFlags() []string { return []string{"--verbose", "--json", "--model", "qwen"} }

func approvalFieldName() string { return "ApprovalMode" }
`,
	}, qwenArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the guard reported ordinary code: %v", violations)
	}
}

// TestTheDeclaredResidualStaysOpen demonstrates the one gap this plugin's
// SIGNATURE leaves — as opposed to the scanner's, which internal/argvguard
// declares and the codex guard demonstrates.
//
// A second site that spells only the model pair and the approval VALUE carries
// none of the three signature literals and walks through. What it cannot do is
// select the approval mode flag or either stream protocol, which is every part
// of qwen's grammar that changes what the child does. If this ever starts
// failing, the signature has been widened and the comment on qwenArgvSignature
// has to be rewritten with it.
func TestTheDeclaredResidualStaysOpen(t *testing.T) {
	violations := scanQwenArgv(t, map[string]string{
		"other/minimal.go": `package other
func minimalCopy(model string) []string { return []string{"--model", model, "yolo"} }`,
	}, qwenArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the model-and-value copy is documented as a residual but the guard now reports it (%v); update the comment on qwenArgvSignature, or the comment and the code disagree", violations)
	}
}

// TestTheQwenArgvAllowlistIsJustified holds the allowlist's shape: every entry
// carries a reason, and every entry names a site that exists in this module's
// own source, in the file the exemption was written for.
func TestTheQwenArgvAllowlistIsJustified(t *testing.T) {
	declared, err := argvguard.DeclaredNames(moduleGoSources(t))
	if err != nil {
		t.Fatalf("argvguard.DeclaredNames: %v", err)
	}
	for key, reason := range qwenArgvConstructionAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is allowlisted with no reason; an exemption nobody can argue with is an exemption nobody reviews", key)
		}
		if !declared[key] {
			t.Errorf("%q is allowlisted but is not declared there; the exemption is waiting for somebody to claim it", key)
		}
	}
}
