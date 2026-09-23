package nativeargs

import (
	"reflect"
	"testing"
)

// The parsing rule's atoms, pinned as a truth table: the separator ends
// flag parsing, flag-shaped means longer than one byte and dash-leading,
// and a lone "-" is positional.
func TestThePredicatesNameTheRule(t *testing.T) {
	t.Parallel()
	for el, wantSeparator := range map[string]bool{
		"--":   true,
		"---":  false,
		"--x":  false,
		"-":    false,
		"":     false,
		"x":    false,
		" -- ": false,
		"--=x": false,
	} {
		if got := IsSeparator(el); got != wantSeparator {
			t.Errorf("IsSeparator(%q) = %v, want %v", el, got, wantSeparator)
		}
	}
	for el, wantFlag := range map[string]bool{
		"--":     true, // flag-shaped AND the separator: callers end the scan, never classify it
		"--x":    true,
		"-c":     true,
		"-":      false, // the stdin-marker shape: positional
		"":       false,
		"x":      false,
		"-c=x":   true,
		" -c":    false,
		"prompt": false,
	} {
		if got := IsFlagElement(el); got != wantFlag {
			t.Errorf("IsFlagElement(%q) = %v, want %v", el, got, wantFlag)
		}
	}
}

// FlagIndexes is the composed rule: flag-shaped elements before the
// first "--", and nothing else — however flag-like the prompt text
// reads. Each row is one scan shape the plugins rely on.
func TestFlagIndexesReturnsFlagPositionsOnly(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		args []string
		want []int
	}{
		{"empty", nil, nil},
		{"flags only", []string{"-c", "k=v", "--long"}, []int{0, 2}},
		{"positionals are prompt", []string{"deploy", "prod"}, nil},
		{"a lone dash is positional", []string{"-"}, nil},
		{"the separator ends the scan", []string{"-c", "k=v", "--", "-c", "evil=x"}, []int{0}},
		{"a leading separator parses nothing", []string{"--", "--dangerously-skip-permissions"}, nil},
		{"a trailing separator changes nothing", []string{"-c", "--"}, []int{0}},
		{"an empty element is positional", []string{"", "-c"}, []int{1}},
		{"prompt that looks like a flag is never returned", []string{"--", "--permission-mode", "ultrastrict"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := FlagIndexes(c.args); !reflect.DeepEqual(got, c.want) {
				t.Errorf("FlagIndexes(%q) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

// The narrowing the choke point buys: dropping the separator break
// parses prompt text as flags. This row plants flag-shaped prompt and
// requires silence; a FlagIndexes narrowed to "every flag-shaped
// element" returns [1 2] here and fails it.
// The `=`-form rule: the cut is at the first `=`, so a value carrying
// its own `=` stays whole after the flag's separator.
func TestSplitFlagValueCutsAtTheFirstEquals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		el          string
		name, value string
		hasValue    bool
	}{
		{"--permission-mode=auto", "--permission-mode", "auto", true},
		{"--config=k=\"a=b\"", "--config", "k=\"a=b\"", true},
		{"-c=", "-c", "", true},
		{"--flag", "--flag", "", false},
		{"prompt", "prompt", "", false},
		{"", "", "", false},
	} {
		name, value, hasValue := SplitFlagValue(c.el)
		if name != c.name || value != c.value || hasValue != c.hasValue {
			t.Errorf("SplitFlagValue(%q) = (%q, %q, %v), want (%q, %q, %v)", c.el, name, value, hasValue, c.name, c.value, c.hasValue)
		}
	}
}

func TestFlagIndexesStopsAtTheSeparator(t *testing.T) {
	t.Parallel()
	got := FlagIndexes([]string{"prompt", "--sandbox", "--", "--sandbox"})
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("FlagIndexes = %v, want [1]: the pre-separator flag is parsed and the post-separator twin is prompt", got)
	}
}
