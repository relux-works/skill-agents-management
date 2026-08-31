package parity

import (
	"fmt"
	"sort"
	"strings"
)

// MaskRule is one machine-local thing a captured surface contains that the
// contract does not.
//
// The set is CLOSED and frozen by TestMaskRuleSetIsFrozen. Adding a rule is
// the edit that makes parity evidence rot — every widening makes one more real
// difference invisible while the suite stays green — so it costs a deliberate
// change to a pinned literal plus the Why sentence that has to survive review.
type MaskRule struct {
	// Name is the rule's stable identifier, recorded in every golden it was
	// applied to.
	Name string
	// Why states what makes the thing it masks noise rather than contract. A
	// rule that cannot answer this is a difference being hidden.
	Why string
}

// maskRules is the frozen set. Order is the order Mask applies them in, which
// matters only in that longer, more specific literals must be substituted
// before shorter prefixes of themselves; Mask enforces that by length rather
// than by trusting this order.
var maskRules = []MaskRule{
	{
		Name: "capture-temp-slot",
		Why: "the source harness allocates its bin, work and package-root directories with testing.T.TempDir, " +
			"whose path carries the subtest name and a per-run random number. Two runs of identical code produce " +
			"different paths, so the literal is noise; WHICH slot a path came from is contract, and the " +
			"<TMPDIR:N> placeholders keep it.",
	},
	{
		Name: "capture-stub-bin-dir",
		Why: "the capture seeds PATH with a directory of stub executables so binary resolution is hermetic — the " +
			"source's own setHermeticAgentPath technique. Where that directory sits is the capture operator's " +
			"choice; that resolution landed in it rather than on a real installed binary is the fact worth keeping.",
	},
	{
		Name: "path-env-excluded",
		Why: "PATH is excluded from the environment diff entirely, ported verbatim from the source harness's " +
			"diffEnv. It is the seeded value from the rule above, so it differs run to run for reasons unrelated " +
			"to any port. The residual this leaves open — a port that changes what it strips FROM PATH — is " +
			"named in testdata/goldens/README.md rather than left for a reader to discover.",
	},
}

// MaskRules returns the frozen rule set.
func MaskRules() []MaskRule { return append([]MaskRule(nil), maskRules...) }

// Substitutions are the concrete machine-local directories ONE capture run or
// ONE plan-building test used. They are the input to the first two mask rules;
// the third needs none, because excluding PATH needs no knowledge of its value.
//
// Both sides of a parity comparison fill this in with their own directories and
// get the same placeholders out, which is the only arrangement under which a
// golden captured on one machine can be compared to a plan built on another.
type Substitutions struct {
	// TempSlots are the temp directories in allocation order: index 0 becomes
	// <TMPDIR:1>. The source harness allocates them in a fixed order per
	// capture case — bin directory, then work directory, then any case-specific
	// root — so the index is stable and a port test reproduces it by allocating
	// the same number of directories in the same order.
	TempSlots []string
	// StubBinDir is the PATH-seeded stub directory, or empty when the case
	// resolved no binary through PATH.
	StubBinDir string
}

// The placeholder vocabulary. It is closed: TestMaskRuleSetIsFrozen pins the
// rules and TestGoldensCarryNoMachineLocalPaths pins that nothing else leaked
// through into a golden.
const (
	// PlaceholderStubBinDir stands for Substitutions.StubBinDir.
	PlaceholderStubBinDir = "<PARITY-BIN>"
	// tempSlotPlaceholderFormat stands for Substitutions.TempSlots[N-1].
	tempSlotPlaceholderFormat = "<TMPDIR:%d>"
)

// TempSlotPlaceholder is the placeholder for the nth temp slot, 1-based, so a
// caller reading a golden can name the slot it has to reproduce.
func TempSlotPlaceholder(n int) string { return fmt.Sprintf(tempSlotPlaceholderFormat, n) }

// replacement is one literal-to-placeholder substitution.
type replacement struct {
	literal     string
	placeholder string
}

func (s Substitutions) replacements() []replacement {
	out := make([]replacement, 0, len(s.TempSlots)+1)
	for i, dir := range s.TempSlots {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		out = append(out, replacement{literal: dir, placeholder: TempSlotPlaceholder(i + 1)})
	}
	if strings.TrimSpace(s.StubBinDir) != "" {
		out = append(out, replacement{literal: s.StubBinDir, placeholder: PlaceholderStubBinDir})
	}
	// Longest literal first. A temp root and a slot beneath it share a prefix,
	// and substituting the shorter one first would leave the longer one
	// half-rewritten — a masked path that matches nothing, which reads as a
	// real difference and sends the next reader hunting a port bug that is not
	// there.
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].literal) > len(out[j].literal) })
	return out
}

// maskedFields names every Snapshot field Mask is allowed to rewrite.
//
// It is frozen by TestMaskingCoversExactlyTheDeclaredFields, which plants a
// maskable literal in EVERY field and asserts exactly this set changed. That
// is the pin the task asks for: masking that silently widens to a new field —
// StdinKind, say — would start hiding differences in a field nobody agreed to
// stop comparing, and this list is where that widening has to be written down.
var maskedFields = []string{"Binary", "Args", "EnvAdded", "EnvRemoved", "StdinData", "Error"}

// MaskedFields returns the frozen list of Snapshot fields masking rewrites.
func MaskedFields() []string { return append([]string(nil), maskedFields...) }

// Mask rewrites the machine-local literals in snap into placeholders.
//
// It does NOT apply the PATH exclusion: that rule lives in DiffEnv, because it
// has to act while the diff is being computed rather than on its result. Both
// are named in MaskRules so the reader gets one list rather than two.
func Mask(snap Snapshot, subs Substitutions) Snapshot {
	reps := subs.replacements()
	apply := func(s string) string {
		for _, r := range reps {
			s = strings.ReplaceAll(s, r.literal, r.placeholder)
		}
		return s
	}
	applyAll := func(in []string) []string {
		if in == nil {
			return nil
		}
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = apply(s)
		}
		return out
	}
	// Field by field, and deliberately not by reflection over every string
	// field: a field added to Snapshot must be a decision about whether it is
	// maskable, and reflection would answer that decision "yes" by default.
	return Snapshot{
		Binary:     apply(snap.Binary),
		Args:       applyAll(snap.Args),
		EnvAdded:   applyAll(snap.EnvAdded),
		EnvRemoved: applyAll(snap.EnvRemoved),
		StdinKind:  snap.StdinKind,
		StdinData:  apply(snap.StdinData),
		Error:      apply(snap.Error),
	}
}
