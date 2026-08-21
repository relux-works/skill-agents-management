package parity

import (
	"reflect"
	"strings"
	"testing"
)

// TestMaskRuleSetIsFrozen is the pin the parity mechanism rests on.
//
// Masking is the one place where a real difference can be made invisible while
// every test stays green. One widening at a time — "this path varies too", "so
// does this id" — and the goldens end up proving that two things agree about
// the fields nobody masked yet. The rule set is therefore a frozen literal:
// adding a rule costs an edit here plus the Why sentence that has to survive
// review, which is the smallest price at which the erosion stops being silent.
//
// If this test fails, do not update the literal to match. Read the new rule's
// Why first, and decide whether the thing it hides is genuinely machine-local
// noise or a difference between the source and the port.
func TestMaskRuleSetIsFrozen(t *testing.T) {
	frozen := []string{"capture-temp-slot", "capture-stub-bin-dir", "path-env-excluded"}
	got := MaskRules()
	if len(got) != len(frozen) {
		t.Fatalf("the mask rule set has %d rules, frozen at %d: %v", len(got), len(frozen), maskRuleNames())
	}
	for i, want := range frozen {
		if got[i].Name != want {
			t.Errorf("mask rule %d is %q, frozen as %q", i, got[i].Name, want)
		}
		if strings.TrimSpace(got[i].Why) == "" {
			t.Errorf("mask rule %q states no reason; a rule that cannot say what makes the thing it hides noise is a difference being suppressed", got[i].Name)
		}
	}
}

// TestMaskingCoversExactlyTheDeclaredFields is the other half of the pin: not
// which rules exist, but which FIELDS they are allowed to touch.
//
// A maskable literal is planted in every string-bearing field of a Snapshot at
// once, and exactly the declared fields must come back changed. A masker that
// silently widened to StdinKind — or that quietly stopped masking Error —
// fails here, because this drives the real Mask and compares the whole struct
// field by field rather than checking the fields it already expects.
func TestMaskingCoversExactlyTheDeclaredFields(t *testing.T) {
	const literal = "/machine/local/slot"
	planted := Snapshot{
		Binary:     literal + "/bin",
		Args:       []string{"--dir", literal},
		EnvAdded:   []string{"ADDED=" + literal},
		EnvRemoved: []string{"REMOVED=" + literal},
		StdinKind:  literal,
		StdinData:  "prompt at " + literal,
		Error:      "cannot read " + literal,
	}
	masked := Mask(planted, Substitutions{TempSlots: []string{literal}})

	declared := map[string]bool{}
	for _, name := range MaskedFields() {
		declared[name] = true
	}
	before := reflect.ValueOf(planted)
	after := reflect.ValueOf(masked)
	typ := before.Type()
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		changed := !reflect.DeepEqual(before.Field(i).Interface(), after.Field(i).Interface())
		switch {
		case declared[name] && !changed:
			t.Errorf("MaskedFields declares %s maskable, but masking left it alone; a golden generated with this masker would carry a machine-local literal in %s", name, name)
		case !declared[name] && changed:
			t.Errorf("masking rewrote %s, which MaskedFields does not declare. Masking that widens to a new field starts hiding differences in a field nobody agreed to stop comparing — add it to maskedFields deliberately, with a reason, or stop masking it", name)
		}
	}

	// And the declared list must name real fields, so a typo cannot quietly
	// exempt a field from the check above.
	for name := range declared {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("MaskedFields names %q, which is not a Snapshot field", name)
		}
	}
}

// TestEveryMaskRuleIsNecessary proves the bound by NARROWING the mask rather
// than by deleting it.
//
// Dropping all substitutions would show only that masking does something. This
// drops ONE rule's substitution at a time and requires that at least one real
// golden stops matching — which pins WHICH rule carries which golden, so a
// later edit that collapses two rules into one loose one is visible here rather
// than in a port failure six weeks later.
func TestEveryMaskRuleIsNecessary(t *testing.T) {
	const (
		fakeSlot1 = "/capture/slot-one"
		fakeSlot2 = "/capture/slot-two"
		fakeSlot3 = "/capture/slot-three"
		fakeBin   = "/capture/stub-bin"
	)
	full := Substitutions{TempSlots: []string{fakeSlot1, fakeSlot2, fakeSlot3}, StubBinDir: fakeBin}

	// unmask turns a golden surface back into what a capture on this fictional
	// machine would have produced, so that re-masking it must reproduce the
	// golden exactly and masking it with a NARROWED rule set must not.
	unmask := func(snap Snapshot) Snapshot {
		expand := func(s string) string {
			s = strings.ReplaceAll(s, PlaceholderStubBinDir, fakeBin)
			s = strings.ReplaceAll(s, TempSlotPlaceholder(1), fakeSlot1)
			s = strings.ReplaceAll(s, TempSlotPlaceholder(2), fakeSlot2)
			s = strings.ReplaceAll(s, TempSlotPlaceholder(3), fakeSlot3)
			return s
		}
		expandAll := func(in []string) []string {
			if in == nil {
				return nil
			}
			out := make([]string, len(in))
			for i, s := range in {
				out[i] = expand(s)
			}
			return out
		}
		return Snapshot{
			Binary: expand(snap.Binary), Args: expandAll(snap.Args),
			EnvAdded: expandAll(snap.EnvAdded), EnvRemoved: expandAll(snap.EnvRemoved),
			StdinKind: snap.StdinKind, StdinData: expand(snap.StdinData), Error: expand(snap.Error),
		}
	}

	narrowings := []struct {
		rule string
		subs Substitutions
	}{
		{rule: "capture-temp-slot", subs: Substitutions{StubBinDir: fakeBin}},
		{rule: "capture-stub-bin-dir", subs: Substitutions{TempSlots: full.TempSlots}},
	}

	all := loadGoldens(t)
	for _, narrowed := range narrowings {
		t.Run(narrowed.rule, func(t *testing.T) {
			matchedAnyway := []string{}
			bitAtLeastOnce := false
			for id, g := range all {
				raw := unmask(g.Surface)
				if diffs := Compare(g.Surface, Mask(raw, full)); len(diffs) != 0 {
					t.Fatalf("%s: the full rule set does not reproduce its own golden, so this narrowing proves nothing: %v", id, diffs)
				}
				if len(Compare(g.Surface, Mask(raw, narrowed.subs))) == 0 {
					matchedAnyway = append(matchedAnyway, id)
					continue
				}
				bitAtLeastOnce = true
			}
			if !bitAtLeastOnce {
				t.Errorf("removing the %s substitution changed no golden's verdict; either the rule masks nothing any fixture contains, or two rules are covering for each other. still matched: %v", narrowed.rule, matchedAnyway)
			}
		})
	}
}

// TestMaskSubstitutesLongerLiteralsFirst pins the one ordering decision inside
// Mask. A temp root and a slot beneath it share a prefix; substituting the
// shorter first leaves the longer one half-rewritten, and a half-masked path
// matches nothing — it reads as a real difference and sends the next reader
// hunting a port bug that is not there.
func TestMaskSubstitutesLongerLiteralsFirst(t *testing.T) {
	subs := Substitutions{TempSlots: []string{"/capture", "/capture/001"}}
	masked := Mask(Snapshot{Binary: "/capture/001/codex"}, subs)
	want := TempSlotPlaceholder(2) + "/codex"
	if masked.Binary != want {
		t.Errorf("Mask produced %q, want %q; the shorter literal was substituted first and left the longer one half-rewritten", masked.Binary, want)
	}
}

// TestMaskIgnoresEmptySubstitutions holds the difference between "this case
// used no stub bin directory" and "substitute the empty string everywhere".
// The latter would splice a placeholder between every character of every field.
func TestMaskIgnoresEmptySubstitutions(t *testing.T) {
	snap := Snapshot{Binary: "codex", Args: []string{"exec"}}
	masked := Mask(snap, Substitutions{TempSlots: []string{"", ""}, StubBinDir: "   "})
	if diffs := Compare(snap, masked); len(diffs) != 0 {
		t.Errorf("empty substitutions changed the snapshot: %v", diffs)
	}
}
