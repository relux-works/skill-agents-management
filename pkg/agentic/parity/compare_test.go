package parity

import (
	"reflect"
	"testing"
)

// TestCompareReportsEveryField is the reason Compare can be trusted to "fail on
// any field difference" rather than on the fields its author remembered.
//
// It walks the Snapshot type by reflection, mutates ONE field at a time, and
// requires Compare to report that field by name. A field added to Snapshot
// later and not handled in Compare fails here on the day it is added, which is
// the only moment anyone is looking.
func TestCompareReportsEveryField(t *testing.T) {
	base := Snapshot{
		Binary:     "/opt/probe/bin/probe",
		Args:       []string{"exec", "--model", "probe-1"},
		EnvAdded:   []string{"ADD=1"},
		EnvRemoved: []string{"GONE=1"},
		StdinKind:  StdinBytes,
		StdinData:  "prompt",
		Error:      "",
	}
	if diffs := Compare(base, base); len(diffs) != 0 {
		t.Fatalf("Compare reported differences between a snapshot and itself: %v", diffs)
	}

	typ := reflect.TypeOf(base)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		t.Run(field.Name, func(t *testing.T) {
			mutant := base
			value := reflect.ValueOf(&mutant).Elem().Field(i)
			switch field.Type.Kind() {
			case reflect.String:
				value.SetString(value.String() + "-mutated")
			case reflect.Slice:
				value.Set(reflect.ValueOf(append(append([]string(nil), value.Interface().([]string)...), "mutated")))
			default:
				t.Fatalf("Snapshot.%s is a %s, which this test does not know how to mutate; a field Compare might be ignoring is a field no port failure would name", field.Name, field.Type.Kind())
			}

			diffs := Compare(base, mutant)
			if len(diffs) == 0 {
				t.Fatalf("Compare found no difference after mutating %s. A parity harness that forgives one field forgives it forever, and the field it forgives is where the next divergence lands", field.Name)
			}
			named := false
			for _, d := range diffs {
				if d.Field == field.Name {
					named = true
				}
			}
			if !named {
				t.Errorf("Compare reported %v after mutating %s, but named no difference in that field; a failure that does not name the surface that drifted sends the reader to the wrong place", diffs, field.Name)
			}
			if len(diffs) != 1 {
				t.Errorf("mutating one field produced %d differences: %v", len(diffs), diffs)
			}
		})
	}
}

// TestCompareTreatsNilAndEmptySlicesAsEqual pins the one place Compare is
// deliberately not byte-literal. A plugin that returns no argv may return nil
// or an empty slice, and the JSON round trip through a golden turns one into
// the other. Reporting that as a difference would make every fixture depend on
// which one the encoder happened to write.
func TestCompareTreatsNilAndEmptySlicesAsEqual(t *testing.T) {
	if diffs := Compare(Snapshot{Args: nil}, Snapshot{Args: []string{}}); len(diffs) != 0 {
		t.Errorf("Compare distinguished a nil slice from an empty one: %v", diffs)
	}
	// But not an empty one from a populated one, which is the difference that
	// matters and the thing the leniency above must not be extended into.
	if diffs := Compare(Snapshot{Args: []string{}}, Snapshot{Args: []string{"--flag"}}); len(diffs) == 0 {
		t.Error("Compare treated an empty argv and a one-flag argv as equal")
	}
}

// TestCompareIsOrderSensitiveOnArgv holds argv to a sequence rather than a set.
// `-C dir --add-dir other` and `--add-dir other -C dir` are different command
// lines to a harness that reads positionally, and a comparison that sorted
// before comparing would call them the same.
func TestCompareIsOrderSensitiveOnArgv(t *testing.T) {
	a := Snapshot{Args: []string{"-C", "/work", "--add-dir", "/board"}}
	b := Snapshot{Args: []string{"--add-dir", "/board", "-C", "/work"}}
	if diffs := Compare(a, b); len(diffs) == 0 {
		t.Error("Compare treated two different argv orderings as equal; argv is a sequence, not a set")
	}
}
