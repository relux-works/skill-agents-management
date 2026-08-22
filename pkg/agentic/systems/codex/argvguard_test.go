package codex

import (
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/argvguard"
	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// This file is the codex half of "ONE argv construction site".
//
// The SCANNER is not here: it is internal/argvguard, shared with every other
// system plugin's guard. It was extracted there when the second plugin landed,
// because a second scanner — even one produced by copying this one — is two
// implementations of one rule, and two guards that drift both report clean.
// The threshold, the resolution depth and the declared-open residuals are that
// package's; what stays here is the part that is genuinely codex's: which
// literals identify codex argv, and which sites are permitted to spell them.
//
// # Scan scope
//
// Every non-test Go file this module's build compiles, via internal/gosources —
// the same walk pkg/agentic's single-source guard uses, so "the whole module"
// means one thing in this repository rather than one thing per guard.
//
// Scanning the whole module rather than one package is the port's own widening
// over the source, and it is the point: in the source the three drifted sites
// lived in two different packages, so a guard that watched only its own
// directory would have seen one of them.

// codexArgvSignature is the set of literal strings that identify a function as
// spelling codex CLI argv when even one of them appears in its body — directly,
// or through a package-level const or var it references.
//
// Every entry is codex-CLI-specific prose rather than a word likely to appear
// by accident, which is what makes a threshold of one affordable.
var codexArgvSignature = []string{
	"--dangerously-bypass-approvals-and-sandbox",
	"--skip-git-repo-check",
	"model_reasoning_effort",
	"service_tier",
	"--ask-for-approval",
	"danger-full-access",
}

const (
	codexArgsFile = "pkg/agentic/systems/codex/args.go"
	codexEnvFile  = "pkg/agentic/systems/codex/env.go"
)

// codexArgvConstructionAllowlist names the only sites permitted to spell a
// signature literal, each with the reason it is not a second construction.
//
// Keys are FILE-SCOPED (internal/argvguard.AllowlistKey). A bare name would
// exempt every function of that name anywhere in the module, and the claude
// plugin names its own construction site Args too — so a bare-name allowlist
// would have quietly stopped this guard from being able to report a second
// codex construction the moment somebody named it Args one directory over.
//
// Args is THE construction site. appendReasoningAndTier is the fragment Args
// splices into both of its grammars: it carries no mode-specific flag, only the
// effort/service-tier pair both modes share, and inlining it into both branches
// is what would create a second spelling one branch at a time.
//
// A name must not be added here without a reason written down. The source's
// allowlist carries six such names — display mirrors, config readers, a flag
// classifier — each audited and each explained where it is listed.
var codexArgvConstructionAllowlist = map[string]string{
	argvguard.AllowlistKey(codexArgsFile, "Args"):                   "the single construction site",
	argvguard.AllowlistKey(codexArgsFile, "appendReasoningAndTier"): "the -c override fragment both of Args' grammars splice in identically",
	argvguard.AllowlistKey(codexArgsFile, "NormalizeServiceTier"):   "maps the runtime's tier vocabulary onto the catalog id; it names the config KEY nowhere and constructs no argv",
	argvguard.AllowlistKey(codexEnvFile, "ServiceTierEnv"):          "the environment variable a child reads its resolved tier from; a variable name, not an argv flag",
}

// moduleGoSources reads every non-test Go file this module's build compiles.
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

func scanCodexArgv(t *testing.T, sources map[string]string, allowlist map[string]string) []argvguard.Violation {
	t.Helper()
	violations, err := argvguard.Scan(sources, codexArgvSignature, allowlist)
	if err != nil {
		t.Fatalf("argvguard.Scan: %v", err)
	}
	return violations
}

// TestCodexArgvHasExactlyOneConstructionSite is the gate.
func TestCodexArgvHasExactlyOneConstructionSite(t *testing.T) {
	violations := scanCodexArgv(t, moduleGoSources(t), codexArgvConstructionAllowlist)
	if len(violations) == 0 {
		return
	}
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = v.String()
	}
	t.Fatalf("found %d codex argv construction site(s) outside Args:\n%s", len(violations), strings.Join(lines, "\n"))
}

// TestTheArgvGuardScansTheWholeModule proves the scan reaches what it claims
// to. A guard that silently scans zero files reports clean forever.
func TestTheArgvGuardScansTheWholeModule(t *testing.T) {
	sources := moduleGoSources(t)
	for _, required := range []string{
		codexArgsFile,
		"pkg/agentic/systems/codex/codex.go",
		"pkg/agentic/systems/claude/args.go",
		"pkg/agentic/registry.go",
		"tools/agents-management/cmd/plugins.go",
	} {
		if _, ok := sources[required]; !ok {
			t.Errorf("the scan did not reach %s; a file the guard never reads is a file it never guards", required)
		}
	}
}

// TestTheArgvGuardFiresOnTheRealConstructionSite narrows the gate instead of
// deleting it: the same real sources are rescanned with Args taken OUT of the
// allowlist, and the real Args must then be reported.
//
// Without this, a green result above is equally consistent with "the rule never
// matches anything", and the guard would be worth nothing while looking healthy.
func TestTheArgvGuardFiresOnTheRealConstructionSite(t *testing.T) {
	narrowed := map[string]string{}
	for key, reason := range codexArgvConstructionAllowlist {
		if key == argvguard.AllowlistKey(codexArgsFile, "Args") {
			continue
		}
		narrowed[key] = reason
	}
	violations := scanCodexArgv(t, moduleGoSources(t), narrowed)
	found := false
	for _, v := range violations {
		if v.Name == "Args" && v.File == codexArgsFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("with Args removed from the allowlist the real construction site was not reported; the rule does not fire on this module's own code, so its silence elsewhere proves nothing. violations=%v", violations)
	}
}

// TestTheAllowlistIsScopedToItsOwnFile is the bound the file-scoped keys buy.
//
// The identical construction, planted under a DIFFERENT filename but with the
// allowlisted function's own name, must still be reported. Under the source's
// bare-name allowlist it would not have been — and with two plugins both naming
// their site Args, that is not a hypothetical: it is what one `func Args` in
// another package would have done to this guard.
func TestTheAllowlistIsScopedToItsOwnFile(t *testing.T) {
	violations := scanCodexArgv(t, map[string]string{
		"other/impostor.go": `package other
func Args(model string) []string {
	return []string{"exec", "-m", model, "--dangerously-bypass-approvals-and-sandbox"}
}`,
	}, codexArgvConstructionAllowlist)
	if len(violations) == 0 {
		t.Fatal("a second construction named Args in another file was admitted; the allowlist is exempting a NAME rather than the site that earned the exemption")
	}
}

// TestTheArgvGuardCatchesASecondConstructionSite is the mutant set: every
// ordinary Go spelling a fourth site could take, each planted in a synthetic
// corpus and each required to be reported.
//
// The single-literal cases are the ones that matter most. The source's guard
// used a threshold of two and a review showed a copy-pasted site spelling only
// one signature walked through it.
func TestTheArgvGuardCatchesASecondConstructionSite(t *testing.T) {
	mutants := []struct {
		name    string
		source  string
		wantFn  string
		because string
	}{
		{
			name:    "a full second construction",
			wantFn:  "buildCodexArgsAgain",
			because: "the shape the source unwound three times",
			source: `package other
func buildCodexArgsAgain(model string) []string {
	return []string{"exec", "-m", model, "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"}
}`,
		},
		{
			name:    "a single signature literal",
			wantFn:  "mirrorSandboxPolicy",
			because: "a copy-paste from the managed-session split spells only danger-full-access, which a threshold of two admits",
			source: `package other
func mirrorSandboxPolicy() []string { return []string{"--sandbox", "danger-full-access"} }`,
		},
		{
			name:    "the literal behind a package-level const",
			wantFn:  "effortOverride",
			because: "a const is the first place a second site moves a literal to avoid spelling it twice",
			source: `package other
const reasoningKey = "model_reasoning_effort"
func effortOverride(effort string) []string { return []string{"-c", reasoningKey + "=" + effort} }`,
		},
		{
			name:    "the literal behind a function-local const",
			wantFn:  "localConstSite",
			because: "the same move one scope down",
			source: `package other
func localConstSite(effort string) []string {
	const key = "model_reasoning_effort"
	return []string{"-c", key + "=" + effort}
}`,
		},
		{
			name:    "the literals inside a package-level var table",
			wantFn:  "codexFlagTable",
			because: "a table of flags is a construction waiting for a caller",
			source: `package other
var codexFlagTable = []string{"--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"}`,
		},
		{
			name:    "a function referencing that var",
			wantFn:  "spliceFlags",
			because: "the caller of such a table spells the flags as surely as the table does",
			source: `package other
var codexFlags = []string{"--skip-git-repo-check"}
func spliceFlags(args []string) []string { return append(args, codexFlags...) }`,
		},
		{
			name:    "a closure inside a package-level table",
			wantFn:  "adapterTable",
			because: "this is exactly how the source's own adapter table was built, so a construction hidden in one has a live precedent",
			source: `package other
var adapterTable = map[string]func() []string{
	"x": func() []string { return []string{"--ask-for-approval", "never"} },
}`,
		},
		{
			name:    "a literal inside init()",
			wantFn:  "init",
			because: "init is an ordinary body and a site that runs at startup is still a site",
			source: `package other
var flags []string
func init() { flags = append(flags, "--dangerously-bypass-approvals-and-sandbox") }`,
		},
		{
			name:    "a literal inside a method",
			wantFn:  "Argv",
			because: "a second System implementation spelling its own codex flags is the port-shaped version of this defect",
			source: `package other
type other struct{}
func (other) Argv() []string { return []string{"--skip-git-repo-check"} }`,
		},
		{
			name:    "a call-wrapped literal inside a function body",
			wantFn:  "wrapped",
			because: "the wrapping alone hides nothing: the body walk sees the literal. Only the indirection through a package-level name is a residual, and this case is what draws that line",
			source: `package other
func wrapped() []string { return []string{string([]byte("--skip-git-repo-check"))} }`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			violations := scanCodexArgv(t, map[string]string{"other/mutant.go": mutant.source}, codexArgvConstructionAllowlist)
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

// TestTheArgvGuardAcceptsOrdinaryCode is the false-positive bound. A guard that
// fires on unrelated code is a guard somebody deletes, and then nothing is
// watching the thing it was written for.
func TestTheArgvGuardAcceptsOrdinaryCode(t *testing.T) {
	violations := scanCodexArgv(t, map[string]string{
		"other/ordinary.go": `package other

import "strings"

// A comment mentioning model_reasoning_effort and danger-full-access is prose,
// not code, and must not be reported.
func unrelated(s string) string { return strings.TrimSpace(s) }

func otherFlags() []string { return []string{"--verbose", "--json", "--model", "gpt"} }

func serviceTierFieldName() string { return "ServiceTier" }
`,
	}, codexArgvConstructionAllowlist)
	if len(violations) != 0 {
		t.Errorf("the guard reported ordinary code: %v", violations)
	}
}

// TestCodexArgvGuardResidualGaps demonstrates each declared-open class STAYING
// open, rather than asserting it in prose.
//
// A class named in internal/argvguard's threat model but not demonstrated here
// would be this same defect one level up: a comment a reader trusts with
// nothing holding it to the code. Read the two against each other.
func TestCodexArgvGuardResidualGaps(t *testing.T) {
	gaps := []struct {
		name   string
		file   string
		source string
	}{
		{
			name: "a call-wrapped literal behind a package-level name",
			file: "other/gap.go",
			source: `package other
var hiddenFlags = []string{string([]byte("--skip-git-repo-check"))}
func hidden() []string { return append([]string{}, hiddenFlags...) }`,
		},
		{
			name: "cross-package references",
			file: "other/gap.go",
			source: `package other
import "example.com/elsewhere"
func hidden() []string { return append([]string{}, elsewhere.CodexFlags...) }`,
		},
		{
			// The file name is the REAL allowlisted one, because with
			// file-scoped keys that is what "inside an allowlisted site" now
			// means. Under a different name this same source is reported, which
			// is TestTheAllowlistIsScopedToItsOwnFile's claim.
			name: "reassignment inside an allowlisted site",
			file: codexArgsFile,
			source: `package codex
var stash []string
func Args() []string {
	stash = append(stash, "--skip-git-repo-check")
	return stash
}
func user() []string { return stash }`,
		},
	}
	for _, gap := range gaps {
		t.Run(gap.name, func(t *testing.T) {
			violations := scanCodexArgv(t, map[string]string{gap.file: gap.source}, codexArgvConstructionAllowlist)
			if len(violations) != 0 {
				t.Errorf("%q is documented as a residual gap but the guard now reports it (%v); close the gap in internal/argvguard's threat model too, or the comment and the code disagree", gap.name, violations)
			}
		})
	}
}

// TestTheArgvAllowlistIsJustified holds the allowlist's shape: every entry
// carries a reason, and every entry names a site that exists in this module's
// own source, in the file the exemption was written for.
//
// An allowlist entry for a function nobody wrote is an exemption waiting for
// somebody to claim it by naming a new function that way.
func TestTheArgvAllowlistIsJustified(t *testing.T) {
	declared, err := argvguard.DeclaredNames(moduleGoSources(t))
	if err != nil {
		t.Fatalf("argvguard.DeclaredNames: %v", err)
	}
	for key, reason := range codexArgvConstructionAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is allowlisted with no reason; an exemption nobody can argue with is an exemption nobody reviews", key)
		}
		if !declared[key] {
			t.Errorf("%q is allowlisted but is not declared there; the exemption is waiting for somebody to claim it", key)
		}
	}
}
