package agentic

import (
	"strings"
	"testing"
)

// The mutants below are the guard's own negative tests. Each plants one
// spelling of a second binding into a synthetic corpus and requires the
// scanner to report it; TestSingleSourceGuardAcceptsOrdinaryCode is the
// control that keeps the whole set from being satisfied by a scanner that
// reports everything.
//
// A guard is only worth what its author failed to defeat, so the spellings go
// well past the obvious one: the alias type, the make() call, the struct
// field, the const-hidden key, the var-hidden case, the function literal
// inside a call argument, and the switch on ID() that names no known id at
// all. Every one of these walked through some revision of the source
// repository's guard.
//
// A guard is worth even less against spellings its author never tried, so a
// second set — TestSingleSourceGuardAgainstReviewMutants — holds the ones a
// reviewer wrote against revision 1 of this file. Four of them walked through:
// an id copied into a local before the switch, a table assembled key by key
// from an init(), a const declared inside a function body, and a value of a
// binding-table type the registry legitimately declares. The rules those four
// forced are marked NARROWING below, and each is covered by a mutant that ONLY
// that rule can catch — undeclared ids where the vocabulary net cannot fire,
// tables with no literal for the composite-literal rule to see.
//
// A guard is worth least of all against a hole its own matrix cannot see. A
// later review found that every if-chain mutant in this file used a KNOWN id,
// so the vocabulary net answered for all of them and the structural net was
// never asked — and the structural net was not there. `switch id` over an
// undeclared id was caught; `if id == "opencode"` was not. Four mutants at the
// end of the id-switch set close that: three comparison forms with undeclared
// ids, and the call-wrapped tag over a local that the switch rule used to miss
// while catching the identical tag without the local. Every one of them is red
// under a narrowing that removes only the rule it names.

// mutantRegistrySource stands in for the real registry file: it is the one
// file the allowlist permits to hold a binding, so a mutant elsewhere has to
// be found on its own merits rather than because the corpus contains no
// legitimate map at all.
const mutantRegistrySource = `package agentic

type SystemID string

type System interface{ ID() SystemID }

type bindings map[SystemID]System

type Registry struct {
	systems bindings
}

func NewRegistry() *Registry { return &Registry{systems: bindings{}} }
`

// mutantVendorRegistrySource stands in for the real vendor registry: the
// Layer-2 home, so a vendor or runtime binding planted anywhere else has to be
// found on its own merits rather than because the corpus declares no
// legitimate vendor table at all.
const mutantVendorRegistrySource = `package vendorplugin

type VendorID string

type RuntimeID string

type Vendor interface{ ID() VendorID }

type RuntimeDeclaration struct{ ID RuntimeID }

type vendorBindings map[VendorID]Vendor

type Registry struct {
	vendors  vendorBindings
	runtimes map[RuntimeID]RuntimeDeclaration
}
`

func scanMutant(t *testing.T, filename, src string) []bindingViolation {
	t.Helper()
	return scanMutantFiles(t, map[string]string{filename: src})
}

// scanMutantFiles runs a mutant that needs more than one file — a type
// declared in one place and instantiated in another, which is how a shadow
// table gets in without any single file looking wrong.
func scanMutantFiles(t *testing.T, files map[string]string) []bindingViolation {
	t.Helper()
	corpus := map[string]string{
		"pkg/agentic/registry.go":      mutantRegistrySource,
		"pkg/vendorplugin/registry.go": mutantVendorRegistrySource,
	}
	for name, src := range files {
		if _, home := corpus[name]; home {
			t.Fatalf("a mutant must not overwrite the home file %s", name)
		}
		corpus[name] = src
	}
	violations, err := scanSingleSource(corpus, bindingHomes)
	if err != nil {
		t.Fatalf("scanSingleSource: %v", err)
	}
	return violations
}

func requireClass(t *testing.T, mutant string, violations []bindingViolation, class string) {
	t.Helper()
	for _, v := range violations {
		if v.Class == class {
			return
		}
	}
	rendered := make([]string, len(violations))
	for i, v := range violations {
		rendered[i] = v.String()
	}
	t.Fatalf("mutant %q was not reported as %s; the guard admits this spelling. violations:\n  %s",
		mutant, class, strings.Join(rendered, "\n  "))
}

func TestSingleSourceGuardCatchesShadowTables(t *testing.T) {
	mutants := []struct {
		name string
		file string
		src  string
	}{
		{
			name: "the obvious one: a second package-level map keyed by SystemID",
			file: "pkg/launcher/shadow.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type builder func() []string

var shadow = map[agentic.SystemID]builder{}
`,
		},
		{
			name: "behind a named map type declared in the shadow file",
			file: "pkg/launcher/alias.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type builder func() []string

type shadowTable map[agentic.SystemID]builder

var shadow = shadowTable{}
`,
		},
		{
			name: "instantiating a binding table type declared inside the allowed file",
			file: "pkg/launcher/instantiate.go",
			src: `package agentic

type bindings map[SystemID]System

func boot() bindings { return bindings{} }
`,
		},
		{
			name: "built with make() inside a function body rather than a literal",
			file: "pkg/launcher/make.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type builder func() []string

func boot() {
	table := make(map[agentic.SystemID]builder, 4)
	_ = table
}
`,
		},
		{
			name: "a struct field, never a literal anywhere",
			file: "pkg/launcher/field.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type builder func() []string

type cache struct {
	byID map[agentic.SystemID]builder
}
`,
		},
		{
			name: "through a SystemID alias, so the key type is not spelled SystemID",
			file: "pkg/launcher/sidalias.go",
			src: `package agentic

type sid = SystemID

type builder func() []string

var shadow = map[sid]builder{}
`,
		},
		{
			name: "a map[string]T whose keys are system ids, avoiding the SystemID type entirely",
			file: "pkg/launcher/stringkeys.go",
			src: `package launcher

type builder func() []string

var shadow = map[string]builder{
	"claude-code": nil,
	"codex":       nil,
}
`,
		},
		{
			name: "keys moved behind package-level constants",
			file: "pkg/launcher/constkeys.go",
			src: `package launcher

type builder func() []string

const (
	claudeID = "claude-code"
	codexID  = "codex"
)

var shadow = map[string]builder{
	claudeID: nil,
	codexID:  nil,
}
`,
		},
		{
			name: "keys assembled by constant concatenation",
			file: "pkg/launcher/concatkeys.go",
			src: `package launcher

type builder func() []string

const (
	family   = "gemini"
	suffix   = "-cli"
	geminiID = family + suffix
)

var shadow = map[string]builder{geminiID: nil}
`,
		},
		{
			// NARROWING. There is no composite literal and no SystemID in
			// sight, so neither structural table rule can fire: only the
			// key-by-key assignment rule catches this. It is also the shape
			// the extraction source's own adapterTable is written in, from an
			// init(), because a var initializer there would have created an
			// initialization cycle — so it is the one spelling a porter is on
			// record as having a reason to write.
			name: "assembled key by key from an init(), never a literal",
			file: "pkg/launcher/initassign.go",
			src: `package launcher

type builder func() []string

var shadow map[string]builder

func init() {
	shadow = make(map[string]builder, 2)
	shadow["codex"] = nil
	shadow["claude-code"] = nil
}
`,
		},
		{
			// NARROWING twice over: key by key (no literal for the table rule)
			// AND the ids behind a const declared inside the function body (no
			// package-level const for the folder to find). Caught only if the
			// assignment rule and the function-body const walk both work.
			name: "assembled key by key in an ordinary function, ids behind function-local consts",
			file: "pkg/launcher/localconstassign.go",
			src: `package launcher

type builder func() []string

func boot() map[string]builder {
	const (
		museID = "muse"
		agyID  = "antigravity"
	)
	table := map[string]builder{}
	table[museID] = nil
	table[agyID] = nil
	return table
}
`,
		},
		{
			// NARROWING for the named-type rules: the type is declared where
			// it is ALLOWED to be — inside the registry — and this file only
			// holds a value of it. There is no map type here, no composite
			// literal and no id spelled anywhere, so nothing but the
			// named-type rules can see it. A review mutant walked through
			// revision 1 on exactly this shape.
			name: "a value of a binding-table type the registry legitimately declares",
			file: "pkg/launcher/holdbindings.go",
			src: `package agentic

var shadow bindings

func boot() bindings { return make(bindings) }
`,
		},
		{
			name: "assembled key by key through a method receiver's field",
			file: "pkg/launcher/fieldassign.go",
			src: `package launcher

type builder func() []string

type cache struct{ byID map[string]builder }

func (c *cache) warm() {
	c.byID["qwen-code"] = nil
}
`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			requireClass(t, mutant.name, scanMutant(t, mutant.file, mutant.src), bindingClassTable)
		})
	}
}

func TestSingleSourceGuardCatchesIDSwitches(t *testing.T) {
	mutants := []struct {
		name string
		file string
		src  string
	}{
		{
			name: "the obvious one: a switch over id literals",
			file: "pkg/launcher/idswitch.go",
			src: `package launcher

func argv(id string) []string {
	switch id {
	case "claude-code":
		return []string{"-p"}
	case "codex":
		return []string{"exec"}
	}
	return nil
}
`,
		},
		{
			name: "an if-else chain, which is a switch spelled differently",
			file: "pkg/launcher/ifchain.go",
			src: `package launcher

func argv(id string) []string {
	if id == "codex" {
		return []string{"exec"}
	} else if id == "muse" {
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "a negated comparison, which is the same chain inverted",
			file: "pkg/launcher/negated.go",
			src: `package launcher

func argv(id string) []string {
	if id != "gemini-cli" {
		return nil
	}
	return []string{"-p"}
}
`,
		},
		{
			name: "case values moved behind package-level constants",
			file: "pkg/launcher/constcase.go",
			src: `package launcher

const claudeID = "claude-code"

func argv(id string) []string {
	switch id {
	case claudeID:
		return []string{"-p"}
	}
	return nil
}
`,
		},
		{
			name: "case values moved behind a package-level var",
			file: "pkg/launcher/varcase.go",
			src: `package launcher

var agyID = "antigravity"

func argv(id string) []string {
	switch id {
	case agyID:
		return []string{"--headless"}
	}
	return nil
}
`,
		},
		{
			name: "case values indexed out of a package-level slice",
			file: "pkg/launcher/varindexcase.go",
			src: `package launcher

var supported = []string{"gemini-cli", "muse"}

func argv(id string) []string {
	switch id {
	case supported[1]:
		return []string{"--headless"}
	}
	return nil
}
`,
		},
		{
			name: "a var-held id compared directly",
			file: "pkg/launcher/varcompare.go",
			src: `package launcher

var qwenID = "qwen-code"

func argv(id string) []string {
	if id == qwenID {
		return []string{"--yolo"}
	}
	return nil
}
`,
		},
		{
			name: "inside a function literal passed as a call argument",
			file: "pkg/launcher/funclit.go",
			src: `package launcher

type builder func(id string) []string

func register(b builder) builder { return b }

var boot = register(func(id string) []string {
	switch id {
	case "gemini-cli":
		return []string{"-p"}
	}
	return nil
})
`,
		},
		{
			name: "a switch on ID() naming ids nobody has declared yet",
			file: "pkg/launcher/idcall.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	switch sys.ID() {
	case "opencode":
		return []string{"run"}
	case "some-future-harness":
		return []string{"go"}
	}
	return nil
}
`,
		},
		{
			// NARROWING. The ids are outside knownPluginIDs on purpose, so the
			// vocabulary net cannot fire and only the local resolution can
			// catch this. Binding the call to a name before switching on it is
			// at least as idiomatic as switching on the call, so a guard that
			// misses it does not "work for ids nobody has declared yet" — it
			// works for one spelling of one of them.
			name: "the id copied into a local first, naming ids nobody has declared yet",
			file: "pkg/launcher/localcopy.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	id := sys.ID()
	switch id {
	case "opencode":
		return []string{"run"}
	case "some-future-harness":
		return []string{"go"}
	}
	return nil
}
`,
		},
		{
			name: "the same copy declared with var rather than :=, undeclared ids",
			file: "pkg/launcher/varcopy.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	var id = sys.ID()
	switch id {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "the copy converted on the way out of ID(), undeclared ids",
			file: "pkg/launcher/convertedcopy.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	id := string(sys.ID())
	switch id {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "converted inline in the switch tag, undeclared ids",
			file: "pkg/launcher/convertedtag.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	switch string(sys.ID()) {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			// NARROWING for the inherited-scope plumbing: the binding is in
			// the outer body and the switch is in a closure, so this is caught
			// only if resolution follows a name into the bodies it encloses.
			name: "the copy bound outside and switched on inside a closure, undeclared ids",
			file: "pkg/launcher/closurecopy.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) func() []string {
	id := sys.ID()
	return func() []string {
		switch id {
		case "opencode":
			return []string{"run"}
		}
		return nil
	}
}
`,
		},
		{
			// The same shape with a KNOWN id. It was already caught by the
			// vocabulary net before the local resolution existed; it stays
			// here so a regression in either net shows up as a distinct
			// failure from a regression in the other.
			name: "the id copied into a local first, naming a known id",
			file: "pkg/launcher/localcopyknown.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	id := sys.ID()
	switch id {
	case "codex":
		return []string{"exec"}
	}
	return nil
}
`,
		},
		{
			// NARROWING for the function-body const walk: the const is inside
			// the body, so package-level folding cannot see it, and there is
			// no ID() call for the structural rule to hold on to.
			name: "compared against a function-local const",
			file: "pkg/launcher/localconstcompare.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func pick(id agentic.SystemID) string {
	const codexID = "codex"
	if id == codexID {
		return "a"
	}
	return ""
}
`,
		},
		{
			name: "case values moved behind a function-local const",
			file: "pkg/launcher/localconstcase.go",
			src: `package launcher

func argv(id string) []string {
	const geminiID = "gemini-cli"
	switch id {
	case geminiID:
		return []string{"-p"}
	}
	return nil
}
`,
		},
		{
			name: "case values moved behind a function-local var",
			file: "pkg/launcher/localvarcase.go",
			src: `package launcher

func argv(id string) []string {
	var qwenID = "qwen-code"
	switch id {
	case qwenID:
		return []string{"--yolo"}
	}
	return nil
}
`,
		},
		// The four below are the if/switch asymmetry a review found, and they
		// are written the way that review's probes were: with ids OUTSIDE
		// knownPluginIDs. Every earlier if-chain mutant here used a known id,
		// so the vocabulary net answered for all of them and the structural
		// net was never asked — which is exactly how a matrix can be full and
		// still miss a hole. If the BinaryExpr rule stops consulting isIDExpr,
		// these four go red and the earlier if-chain mutants stay green.
		{
			name: "an if-else chain on ID() naming ids nobody has declared yet",
			file: "pkg/launcher/ifchainundeclared.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	if sys.ID() == "opencode" {
		return []string{"run"}
	} else if sys.ID() == "some-future-harness" {
		return []string{"start"}
	}
	return nil
}
`,
		},
		{
			name: "the id copied into a local first, then compared, undeclared ids",
			file: "pkg/launcher/ifchainlocal.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	id := sys.ID()
	if id == "opencode" {
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "the same comparison negated, over a converted local, undeclared id",
			file: "pkg/launcher/negatedundeclared.go",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	id := string(sys.ID())
	if id != "opencode" {
		return nil
	}
	return []string{"run"}
}
`,
		},
		{
			name: "a local copy call-wrapped in the switch tag, undeclared ids",
			file: "pkg/launcher/wrappedlocal.go",
			src: `package launcher

import (
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func argv(sys agentic.System) []string {
	id := string(sys.ID())
	switch strings.TrimSpace(id) {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			requireClass(t, mutant.name, scanMutant(t, mutant.file, mutant.src), bindingClassIDSwitch)
		})
	}
}

// TestSingleSourceGuardAcceptsOrdinaryCode is the control. Without it every
// mutant above is equally satisfied by a scanner that reports every file, and
// a guard that cries wolf on ordinary code is a guard that gets deleted.
func TestSingleSourceGuardAcceptsOrdinaryCode(t *testing.T) {
	clean := `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

var counts = map[string]int{"launches": 0, "refusals": 0}

func label(mode agentic.LaunchMode) string {
	switch mode {
	case agentic.LaunchModeExec:
		return "exec"
	case agentic.LaunchModeDryRun:
		return "dry-run"
	}
	return "other"
}

func describe(id agentic.SystemID) string {
	if id == "" {
		return "unregistered"
	}
	return "codex exec is the launch mode this harness owns end to end"
}

// The comparison rule reads an ID() call structurally, so its carve-out for a
// blank right-hand side is load-bearing: an emptiness check is not dispatch,
// and a guard that reports one is a guard somebody deletes. Both spellings are
// here, direct and through a local, because the rule resolves both.
func registered(sys agentic.System) bool {
	if sys.ID() == "" {
		return false
	}
	id := sys.ID()
	return id != "" && id != "   "
}

// Two ids compared to each other is not dispatch on any id: nothing folds to a
// string on either side, so nothing fires.
func same(a agentic.System, b agentic.System) bool { return a.ID() == b.ID() }

func plan(r *agentic.Registry, req agentic.LaunchRequest) (agentic.Plan, error) {
	return agentic.BuildPlan(r, req, agentic.LaunchModeExec)
}
`
	violations := scanMutant(t, "pkg/launcher/clean.go", clean)
	for _, v := range violations {
		if v.File == "pkg/launcher/clean.go" {
			t.Errorf("ordinary code was reported as a second binding: %s", v)
		}
	}
}

// TestSingleSourceGuardResidualGaps demonstrates the classes the threat model
// on singlesource_guard_test.go declares open, rather than asserting them in
// prose. Each spelling below is NOT reported, and that is recorded here so the
// boundary is a known quantity instead of an undiscovered one. If a future
// change closes one of these, this test fails and the threat model gets
// updated with it — which is the point, and it is a demonstrated property: a
// review closed the call-wrapped-literal residual with a four-line change and
// this test went red naming it.
//
// Six residual classes are named in that threat model and seven subtests below
// demonstrate them: the multi-hop class carries two, because the hop count and
// not the conversions around it is what closes the resolution, and a reader is
// entitled to see both spellings fail to fire rather than take that on trust.
//
// The last entry is not a residual but a declared NON-goal: it is here for the
// same reason, so that widening the guard to arbitrary identity-shaped
// accessors is a decision somebody argues rather than one that happens.
func TestSingleSourceGuardResidualGaps(t *testing.T) {
	gaps := []struct {
		name string
		why  string
		src  string
	}{
		{
			name: "a call-wrapped literal key",
			why:  "neither foldStringConst nor collectStringLiterals evaluates a call expression",
			src: `package launcher

type builder func() []string

var shadow = map[string]builder{string([]byte("claude-code")): nil}
`,
		},
		{
			name: "a cross-package const referenced by selector",
			why:  "only the files handed to the scanner are resolved, so a qualified selector has no declaration to fold",
			src: `package launcher

import "example.com/ids"

type builder func() []string

var shadow = map[string]builder{ids.Codex: nil}
`,
		},
		{
			name: "ids assembled at runtime",
			why:  "the key-by-key assignment rule sees the assignment but has no literal to resolve",
			src: `package launcher

import "strings"

type builder func() []string

func boot(parts []string) map[string]builder {
	table := map[string]builder{}
	table[strings.Join(parts, "-")] = nil
	return table
}
`,
		},
		{
			name: "an id variable that is rewritten before the switch",
			why:  "idCallLocals resolves one binding never written to again; a second write drops the name rather than guessing what it holds",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func fold(id string) string { return id }

func argv(sys agentic.System) []string {
	id := string(sys.ID())
	id = fold(id)
	switch id {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "a two-hop local: the id copied, then copied again",
			why:  "id resolution is single-hop — a name resolves when its right-hand side reaches an ID() call without passing through another name, and following assignment chains is dataflow analysis by another name",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	raw := sys.ID()
	id := raw
	switch id {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "a two-hop local whose second hop converts",
			why:  "the same single-hop boundary: conversions around ONE hop resolve and are in the mutant set, a conversion around a second hop is still a second hop",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func argv(sys agentic.System) []string {
	id := sys.ID()
	key := string(id)
	if key == "opencode" {
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "an id that leaves through a helper function",
			why:  "resolution covers one function body and the bodies it encloses; it does not follow a return across a function boundary",
			src: `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func tag(sys agentic.System) string { return string(sys.ID()) }

func argv(sys agentic.System) []string {
	switch tag(sys) {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
		{
			name: "NON-GOAL: a switch on an identity accessor that is not ID()",
			why:  "ID() is what the System contract declares; matching Identifier(), Name() or Slug() is false-positive surface on code with nothing to do with this invariant",
			src: `package launcher

type named interface{ Identifier() string }

func argv(sys named) []string {
	switch sys.Identifier() {
	case "opencode":
		return []string{"run"}
	}
	return nil
}
`,
		},
	}

	for _, gap := range gaps {
		t.Run(gap.name, func(t *testing.T) {
			violations := scanMutant(t, "pkg/launcher/residual.go", gap.src)
			for _, v := range violations {
				if v.File == "pkg/launcher/residual.go" {
					t.Fatalf("this residual is no longer open: %s (declared open because %s). Update the threat model on singlesource_guard_test.go and move this case into the mutant set.", v, gap.why)
				}
			}
		})
	}
}

// TestSingleSourceGuardAgainstReviewMutants is the second mutant set: the
// spellings a REVIEWER wrote against revision 1 of this guard, kept verbatim
// in shape so that the matrix in the review verdict stays reproducible rather
// than being a claim about a scratch copy nobody can rerun.
//
// Three of them walked through revision 1 — a local copy of ID() in a switch
// tag, an init()-assembled map with plain string keys, and a function-local
// const — and each is now caught by a rule this revision added. Holding a
// guard to spellings its author did not write is the only way its threat model
// means anything, so they live here permanently.
//
// The fifteenth, a switch on an accessor named Identifier() rather than ID(),
// is a declared NON-goal and lives in TestSingleSourceGuardResidualGaps.
func TestSingleSourceGuardAgainstReviewMutants(t *testing.T) {
	const agenticImport = `import "github.com/relux-works/skill-agents-management/pkg/agentic"`
	_ = agenticImport

	mutants := []struct {
		name  string
		class string
		files map[string]string
	}{
		{
			name:  "R1: a SystemID alias used as the map key",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r1.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type sysid = agentic.SystemID

type builder func() []string

var shadow = map[sysid]builder{}
`},
		},
		{
			name:  "R2: a defined type over SystemID used as the map key",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r2.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type sysid agentic.SystemID

type builder func() []string

var shadow = map[sysid]builder{}
`},
		},
		{
			name:  "R3: an if/else-if chain comparing known ids",
			class: bindingClassIDSwitch,
			files: map[string]string{"pkg/launcher/r3.go": `package launcher

func argv(id string) []string {
	if id == "codex" {
		return []string{"exec"}
	} else if id == "claude-code" {
		return []string{"-p"}
	}
	return nil
}
`},
		},
		{
			name:  "R4: an init()-assembled map[string]T with literal ids",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r4.go": `package launcher

type builder func() []string

var shadow map[string]builder

func init() {
	shadow = make(map[string]builder, 2)
	shadow["codex"] = nil
	shadow["claude-code"] = nil
}
`},
		},
		{
			name:  "R5: an init()-assembled map[SystemID]T",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r5.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type builder func() []string

var shadow map[agentic.SystemID]builder

func init() {
	shadow = make(map[agentic.SystemID]builder, 2)
	shadow["codex"] = nil
}
`},
		},
		{
			name:  "R6: a switch on a local copy of ID(), undeclared ids",
			class: bindingClassIDSwitch,
			files: map[string]string{"pkg/launcher/r6.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func dispatch(sys agentic.System) string {
	id := sys.ID()
	switch id {
	case "opencode":
		return "a"
	case "some-future-harness":
		return "b"
	}
	return ""
}
`},
		},
		{
			name:  "R7: a switch on a local copy of ID(), known ids",
			class: bindingClassIDSwitch,
			files: map[string]string{"pkg/launcher/r7.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func dispatch(sys agentic.System) string {
	id := sys.ID()
	switch id {
	case "codex":
		return "a"
	}
	return ""
}
`},
		},
		{
			name:  "R8: a function-local const in a comparison",
			class: bindingClassIDSwitch,
			files: map[string]string{"pkg/launcher/r8.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

func pick(id agentic.SystemID) string {
	const codexID = "codex"
	if id == codexID {
		return "a"
	}
	return ""
}
`},
		},
		{
			name:  "R9: a local slice indirection in a switch case",
			class: bindingClassIDSwitch,
			files: map[string]string{"pkg/launcher/r9.go": `package launcher

func argv(id string) []string {
	var supported = []string{"gemini-cli", "muse"}
	switch id {
	case supported[1]:
		return []string{"--headless"}
	}
	return nil
}
`},
		},
		{
			name:  "R10: make() of a binding-table type declared in the allowed file",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r10.go": `package agentic

func boot() bindings { return make(bindings) }
`},
		},
		{
			name:  "R11: a binding-table type declared in one file, held in another",
			class: bindingClassTable,
			files: map[string]string{
				"pkg/launcher/r11decl.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type builder func() []string

type shadowTable map[agentic.SystemID]builder
`,
				"pkg/launcher/r11hold.go": `package launcher

var shadow shadowTable
`,
			},
		},
		{
			name:  "R12: a struct field typed map[SystemID]T outside the registry",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r12.go": `package launcher

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type builder func() []string

type cache struct {
	byID map[agentic.SystemID]builder
}
`},
		},
		{
			name:  "R13: a map[string]T composite literal keyed by known ids",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r13.go": `package launcher

type builder func() []string

var shadow = map[string]builder{"codex": nil, "muse": nil}
`},
		},
		{
			name:  "R14: a function-local map literal keyed by a known id",
			class: bindingClassTable,
			files: map[string]string{"pkg/launcher/r14.go": `package launcher

type builder func() []string

func boot() map[string]builder {
	return map[string]builder{"qwen-code": nil}
}
`},
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			requireClass(t, mutant.name, scanMutantFiles(t, mutant.files), mutant.class)
		})
	}
}

// TestSingleSourceGuardCatchesVendorLayerBindings is the extension's own
// mutant set. The guard's key-type list gained VendorID and RuntimeID when
// Layer 2 arrived, and a list that is never exercised on the types it gained
// is a list that claims coverage it has not got.
//
// Every spelling here is one that walked through SOME revision of the Layer-1
// rules and is reused verbatim in shape, because the rules are shared: if the
// vendor key types were bolted on by name in one rule and not the others,
// exactly one of these goes red.
func TestSingleSourceGuardCatchesVendorLayerBindings(t *testing.T) {
	mutants := []struct {
		name string
		file string
		src  string
	}{
		{
			name: "a second map keyed by VendorID",
			file: "pkg/quota/shadow.go",
			src: `package quota

import "github.com/relux-works/skill-agents-management/pkg/vendorplugin"

type limiter func() bool

var shadow = map[vendorplugin.VendorID]limiter{}
`,
		},
		{
			name: "a second map keyed by RuntimeID",
			file: "pkg/quota/runtimes.go",
			src: `package quota

import "github.com/relux-works/skill-agents-management/pkg/vendorplugin"

type limiter func() bool

var shadow = map[vendorplugin.RuntimeID]limiter{}
`,
		},
		{
			name: "a struct field typed map[VendorID]T, never a literal anywhere",
			file: "pkg/quota/field.go",
			src: `package quota

import "github.com/relux-works/skill-agents-management/pkg/vendorplugin"

type limiter func() bool

type cache struct {
	byVendor map[vendorplugin.VendorID]limiter
}
`,
		},
		{
			name: "a value of a vendor binding-table type the vendor registry legitimately declares",
			file: "pkg/quota/holdbindings.go",
			src: `package vendorplugin

var shadow vendorBindings

func boot() vendorBindings { return make(vendorBindings) }
`,
		},
		{
			name: "runtime ids as literal keys, avoiding the RuntimeID type entirely",
			file: "pkg/quota/stringkeys.go",
			src: `package quota

type limiter func() bool

var shadow = map[string]limiter{
	"agy":  nil,
	"muse": nil,
}
`,
		},
		{
			name: "vendor ids as literal keys, assembled key by key from an init()",
			file: "pkg/quota/initassign.go",
			src: `package quota

type limiter func() bool

var shadow map[string]limiter

func init() {
	shadow = make(map[string]limiter, 2)
	shadow["anthropic"] = nil
	shadow["alibaba"] = nil
}
`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			requireClass(t, mutant.name, scanMutant(t, mutant.file, mutant.src), bindingClassTable)
		})
	}
}

// TestSingleSourceGuardCatchesCrossLayerBindings is the narrowing that the
// per-key-type list exists for, and the ONLY test that separates it from a
// flat allowlist of permitted files.
//
// Each mutant plants a binding for one layer's key type inside the OTHER
// layer's home — a file that legitimately holds a binding map, so every "is
// this file allowed to bind" rule answers yes. Under a flat allowlist both are
// admitted. Under bindingHomes both are violations, because the home is
// declared per fact and "which plugin implements vendor Y" is not the fact
// pkg/agentic/registry.go is the home of.
func TestSingleSourceGuardCatchesCrossLayerBindings(t *testing.T) {
	// The planted binding is APPENDED to the home file's real content, exactly
	// as a colleague adding a second table to an existing registry would write
	// it — which is why each snippet is a fragment rather than a whole file,
	// and why the qualified spelling carries no import line: the scan is
	// syntactic and the placement of an import is not what is under test.
	mutants := []struct {
		name    string
		home    string
		snippet string
	}{
		{
			name: "a vendor binding inside the agentic registry",
			home: "pkg/agentic/registry.go",
			snippet: `
type quota func() bool

var vendors = map[vendorplugin.VendorID]quota{}
`,
		},
		{
			name: "a runtime binding inside the agentic registry",
			home: "pkg/agentic/registry.go",
			snippet: `
type pair struct{}

var runtimes = map[vendorplugin.RuntimeID]pair{}
`,
		},
		{
			name: "a system binding inside the vendor registry",
			home: "pkg/vendorplugin/registry.go",
			snippet: `
type builder func() []string

var systems = map[agentic.SystemID]builder{}
`,
		},
	}

	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			corpus := map[string]string{
				"pkg/agentic/registry.go":      mutantRegistrySource,
				"pkg/vendorplugin/registry.go": mutantVendorRegistrySource,
			}
			corpus[mutant.home] += mutant.snippet
			violations, err := scanSingleSource(corpus, bindingHomes)
			if err != nil {
				t.Fatalf("scanSingleSource: %v", err)
			}
			found := false
			for _, v := range violations {
				if v.File == mutant.home && v.Class == bindingClassTable {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s: a binding for the other layer's key type inside %s was admitted; the home list is behaving like a flat allowlist. violations=%v",
					mutant.name, mutant.home, violations)
			}
		})
	}
}
