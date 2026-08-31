package parity

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Difference is one field on which a plan and its golden disagree.
type Difference struct {
	// Field is the Snapshot field name, so a failure names the surface that
	// drifted rather than only printing two blobs.
	Field string
	Want  string
	Got   string
}

func (d Difference) String() string {
	return fmt.Sprintf("%s:\n    golden: %s\n    plan:   %s", d.Field, d.Want, d.Got)
}

// Compare reports every field on which got differs from want.
//
// There is no tolerance, no normalization and no field it declines to look at.
// A parity harness that forgives one field forgives it forever, and the field
// it forgives is where the next divergence lands — the source repository's own
// history is a display binary drifting from the launch target for exactly as
// long as nothing compared it.
//
// TestCompareReportsEveryField holds this to the Snapshot type by reflection:
// a field added later and not handled here fails the suite until it is.
func Compare(want, got Snapshot) []Difference {
	var diffs []Difference
	str := func(field, w, g string) {
		if w != g {
			diffs = append(diffs, Difference{Field: field, Want: quote(w), Got: quote(g)})
		}
	}
	slice := func(field string, w, g []string) {
		if !equalStrings(w, g) {
			diffs = append(diffs, Difference{Field: field, Want: quoteAll(w), Got: quoteAll(g)})
		}
	}
	str("Binary", want.Binary, got.Binary)
	slice("Args", want.Args, got.Args)
	slice("EnvAdded", want.EnvAdded, got.EnvAdded)
	slice("EnvRemoved", want.EnvRemoved, got.EnvRemoved)
	str("StdinKind", want.StdinKind, got.StdinKind)
	str("StdinData", want.StdinData, got.StdinData)
	str("Error", want.Error, got.Error)
	return diffs
}

// ComparePlan is the whole harness in one call, and the entry point a port test
// drives: snapshot the plan against the golden's own recorded parent
// environment, mask it with the caller's machine-local directories, and compare
// it to the golden's surface.
//
// Taking the parent environment from the golden rather than from the caller is
// deliberate. EnvAdded and EnvRemoved are a diff, and a port test that supplied
// its own baseline would be comparing two different measurements that happen to
// share a schema.
func ComparePlan(g Golden, plan agentic.Plan, subs Substitutions) []Difference {
	return Compare(g.Surface, Mask(FromPlan(plan, g.Capture.ParentEnv), subs))
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func quote(s string) string { return fmt.Sprintf("%q", s) }

func quoteAll(in []string) string {
	if len(in) == 0 {
		return "[]"
	}
	parts := make([]string, len(in))
	for i, s := range in {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(parts, " ") + "]"
}
