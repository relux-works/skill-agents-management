package providerlimits

import (
	"strings"
	"testing"
)

// This file is the structural gate on the whole F8/F10/F12/F14/F15 family.
//
// Every one of those findings was the SAME defect found in a different private
// copy of the JSON walk: a copy that dropped the escape bit, one that compared
// totals instead of walking in order, one that tracked braces and not brackets,
// one that read brackets raw and let string content cancel a real opener.
// Repairing copies one at a time is what made it a series.
//
// The module now has exactly one scanner (tokenScanner), one state type
// (tokenState) and one definition of top-level-ness (topLevel). These tests fail
// if a second one comes back, whether or not it is currently exploitable.

// scanSpan drives the shared scanner over a span and returns the state after it.
// The tests below deliberately go through this rather than through any production
// helper, so "the production path agrees with the scanner" stays a claim under
// test rather than an identity.
func scanSpan(span string, start tokenState, q quoting) tokenState {
	sc := newTokenScanner(start, q)
	for i := 0; i < len(span); i++ {
		sc.step(span[i])
	}
	return sc.state()
}

// structureCorpus is the set of spans every property in this file is driven over.
// It is weighted toward the shapes that produced real findings: cuts inside
// escapes, closers inside string content, unmatched closers, mixed nesting, and
// containers left open.
var structureCorpus = []string{
	"",
	`{`,
	`[`,
	`]`,
	`}`,
	`"`,
	`\`,
	`{"a":1}`,
	`[{"a":1}]`,
	`["]",`,
	`["\"]",`,
	`["some ] text",`,
	`he said "hello [`,
	`he said "]" then [x]`,
	`] [`,
	`] ["]",`,
	`[ "]" ]`,
	`[ "\\" ]`,
	`"[" [`,
	`{"a":"\\"}`,
	`{"a":"b\"c"}`,
	`{"a":"[","b":"]"}`,
	`[[[`,
	`}}}`,
	`{"a":[1,2,`,
	`[{"type":"result"}`,
	`{"type":"result","result":"}"}`,
	`crash: " ] [ ]`,
	`[1,{"a":[2,{"b":3}]}]`,
	`{"a":"unterminated`,
	`[{"a":"\`,
}

// The property that makes "one scanner threaded whole" mean something: scanning a
// span in one pass must equal scanning it in two, with the intermediate state
// carried across the cut.
//
// This is F10 and F14 stated as an invariant rather than as two bug reports. F10
// cut between a backslash and its payload and re-initialised the escape bit; F14
// carried the state whole but the state itself described objects and not arrays.
// Both are cuts at which the two-pass answer diverges from the one-pass answer, so
// a state that is incomplete in ANY field fails here — including a field nobody
// has found an exploit for yet.
func TestOneScannerThreadsItsWholeStateAcrossAnyCut(t *testing.T) {
	for _, q := range []quoting{quotingTrusted, quotingIgnored} {
		for _, span := range structureCorpus {
			whole := scanSpan(span, tokenState{}, q)
			for cut := 0; cut <= len(span); cut++ {
				mid := scanSpan(span[:cut], tokenState{}, q)
				if got := scanSpan(span[cut:], mid, q); got != whole {
					t.Fatalf("quoting=%v span=%q cut at %d: two-pass state %+v != one-pass state %+v",
						q, span, cut, got, whole)
				}
			}
		}
	}
}

// The measured path must BE the shared scanner, not merely agree with it today.
// A private walk reintroduced inside claudeScanHeadFrom fails here.
func TestMeasuredHeadScanIsTheSharedScanner(t *testing.T) {
	starts := []tokenState{
		{},
		{containers: "["},
		{containers: "{"},
		{containers: "[{"},
		{inString: true},
		{containers: "[", inString: true},
		{containers: "{", inString: true, escaped: true},
	}
	for _, span := range structureCorpus {
		for _, start := range starts {
			want := scanSpan(span, start, quotingTrusted)
			if got := claudeScanHeadFrom([]byte(span), start); got != want {
				t.Fatalf("claudeScanHeadFrom(%q, %+v) = %+v, want the shared scanner's %+v",
					span, start, got, want)
			}
		}
	}
}

// claudeReachTopLevel must report the FIRST position the shared scanner calls top
// level, and must report none when the scanner never gets there. It used to carry
// its own copy of the walk, so this pins it to the scanner from both directions.
func TestReachTopLevelAgreesWithTheSharedScanner(t *testing.T) {
	starts := []tokenState{
		{containers: "["},
		{containers: "{"},
		{containers: "[{"},
		{inString: true},
		{containers: "[", inString: true},
		{containers: "{", inString: true, escaped: true},
	}
	for _, span := range structureCorpus {
		for _, start := range starts {
			idx, ok := claudeReachTopLevel(span, start)
			first, found := -1, false
			for j := 1; j <= len(span); j++ {
				if scanSpan(span[:j], start, quotingTrusted).atTopLevel() {
					first, found = j, true
					break
				}
			}
			if ok != found {
				t.Fatalf("claudeReachTopLevel(%q, %+v) ok = %v, but the shared scanner says reachable = %v",
					span, start, ok, found)
			}
			if ok && idx != first {
				t.Fatalf("claudeReachTopLevel(%q, %+v) = %d, but the shared scanner first reaches top level at %d",
					span, start, idx, first)
			}
		}
	}
}

// The prose region gate must be a pure function of the shared scanner over its
// declared input handling — nothing else. Reproducing the gate from the scanner
// alone, in the test, is what proves the gate holds no structure rule of its own.
func TestRegionGateDelegatesStructureToTheSharedScanner(t *testing.T) {
	for _, span := range structureCorpus {
		// The object test is a separate question (did an emitter write a value
		// here), so spans it claims are not evidence about the container walk.
		objectStart := false
		for i := 0; i < len(span); i++ {
			if claudeObjectBegins(span, i) {
				objectStart = true
				break
			}
		}
		if objectStart {
			if got := claudeRegionHidesStructure(span, 0, len(span)); got != regionOpensObject {
				t.Fatalf("claudeRegionHidesStructure(%q) = %v, want regionOpensObject", span, got)
			}
			continue
		}
		var neutral strings.Builder
		for i := 0; i < len(span); i++ {
			neutral.WriteByte(regionByte(span[i]))
		}
		want := regionInert
		for _, q := range []quoting{quotingIgnored, quotingTrusted} {
			if !scanSpan(neutral.String(), tokenState{}, q).containers.empty() {
				want = regionOpensContainer
				break
			}
		}
		if got := claudeRegionHidesStructure(span, 0, len(span)); got != want {
			t.Fatalf("claudeRegionHidesStructure(%q) = %v, but the shared scanner over the region's own input handling says %v",
				span, got, want)
		}
	}
}

// The two quoting modes must differ in string handling and in NOTHING else. A
// mutant that collapses them onto each other — the F11 regression, or the F15 one
// depending on direction — fails the first half; a mutant that makes them differ
// about containers fails the second.
func TestQuotingModesDifferOnlyInStringHandling(t *testing.T) {
	// A bracket inside string content: trusted reads it as content, ignored reads
	// it as structure. If these ever agree, one mode has stopped existing.
	if st := scanSpan(`"["`, tokenState{}, quotingTrusted); !st.containers.empty() {
		t.Fatalf("quotingTrusted read a bracket inside a string as structure: %+v", st)
	}
	if st := scanSpan(`"["`, tokenState{}, quotingIgnored); st.containers.empty() {
		t.Fatalf("quotingIgnored honoured a quote, so it is no longer a distinct reading: %+v", st)
	}
	if st := scanSpan(`"`, tokenState{}, quotingIgnored); st.inString {
		t.Fatal("quotingIgnored entered a string, so it is no longer quote-blind")
	}
	// With no quote and no backslash present there is nothing to disagree about,
	// so the two modes must produce identical state.
	for _, span := range structureCorpus {
		if strings.ContainsAny(span, `"\`) {
			continue
		}
		trusted := scanSpan(span, tokenState{}, quotingTrusted)
		ignored := scanSpan(span, tokenState{}, quotingIgnored)
		if trusted != ignored {
			t.Fatalf("span %q has no quoting in it, but the modes disagree: trusted %+v vs ignored %+v",
				span, trusted, ignored)
		}
	}
}

// regionByte is bounded in both directions. Neutralising braces is licensed only
// because the object test has already refused every brace that could open an
// object; neutralising anything more would disarm the gate, and neutralising less
// would cost a genuine envelope the narrowing this module has always had.
func TestProseBraceIsNeutralisedOnlyBecauseTheObjectTestDecidedIt(t *testing.T) {
	for _, tc := range []struct {
		name, region string
		want         regionRisk
	}{
		// A brace that JSON forbids from opening an object opens nothing, so the
		// envelope behind it still decides.
		{"a brace that cannot open an object", `panic: {not json at all`, regionInert},
		{"an unbalanced prose brace", `goroutine stack: {`, regionInert},
		{"prose braces and a balanced bracket pair", `{oops} [tag]`, regionInert},
		// A brace that CAN open an object is caught by the object test rather than
		// neutralised away.
		{"a narrated envelope", `saw {"type":"result"} once`, regionOpensObject},
		{"an unterminated narrated envelope", `saw {"type":`, regionOpensObject},
		{"an empty object", `it printed {} today`, regionOpensObject},
		// Brackets are never neutralised.
		{"a lone opener", `[`, regionOpensContainer},
		{"an opener after prose", `starting run [`, regionOpensContainer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeRegionHidesStructure(tc.region, 0, len(tc.region)); got != tc.want {
				t.Fatalf("claudeRegionHidesStructure(%q) = %v, want %v", tc.region, got, tc.want)
			}
		})
	}
}

// topLevel is the only definition of top-level-ness, and both readers must give
// the same answer for the same position. A reader that drops the string half —
// the shape that would let a scan resume inside a string — fails here.
func TestTopLevelHasOneDefinition(t *testing.T) {
	for _, span := range structureCorpus {
		for _, q := range []quoting{quotingTrusted, quotingIgnored} {
			sc := newTokenScanner(tokenState{}, q)
			for i := 0; i < len(span); i++ {
				sc.step(span[i])
				if got, want := sc.atTopLevel(), sc.state().atTopLevel(); got != want {
					t.Fatalf("span %q byte %d (quoting=%v): scanner says top level = %v, its own frozen state says %v",
						span, i, q, got, want)
				}
			}
		}
	}
	if topLevel(0, true) {
		t.Fatal("a position inside a string is not top level")
	}
	if topLevel(1, false) {
		t.Fatal("a position inside a container is not top level")
	}
	if !topLevel(0, false) {
		t.Fatal("a position outside every container and string is top level")
	}
}
