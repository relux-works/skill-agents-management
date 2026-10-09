package muse

import (
	"errors"
	"testing"
)

// TestParseBuildIdentityGrammar pins the one Muse build-identity grammar
// every consumer parses through: the bare build, the version answer and
// the pinned filename accept exactly the §2 shapes — including a
// revision WITHOUT a trailing subrevision — and refuse everything else.
// The comparison half pins numeric ordering on every component: release
// triple, R number and optional subrevision, with absent sorting before
// any present subrevision including .0.
func TestParseBuildIdentityGrammar(t *testing.T) {
	t.Parallel()
	t.Run("build", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			text    string
			release string
		}{
			{"1.4.1-R4503.1", "1.4.1"},
			{"1.4.2-R4684.1", "1.4.2"},
			{"1.4.5-R5500", "1.4.5"},
			{"10.20.30-R1.0", "10.20.30"},
			{"01.04.02-R04684.01", "01.04.02"},
		} {
			identity, err := ParseBuildID(tc.text)
			if err != nil || identity.Release != tc.release || identity.Build != tc.text {
				t.Fatalf("ParseBuildID(%q) = (%#v, %v), want release %q", tc.text, identity, err, tc.release)
			}
		}
		for _, text := range []string{
			"", "1.4.2", "1.4", "142-R1", "1.4.2-R", "1.4.2-R1.", "1.4.2-R.1",
			"1.4.2-R1.2.3", "1.4.2-r1", "1.4.2R1", "1.4.2-R1-2",
			"1.4.2-R1 ", " 1.4.2-R1", "1.4.2 -R1", "1.4.2-R1\n",
			"1.4.2-R1-beta", "v1.4.2-R1", "1.4.2-R1+meta",
			"muse-bin-1.4.2-R1", "Muse Code 1.4.2 (1.4.2-R1)",
			"1.4.2-R١", "1.4.2-R1.٢",
		} {
			if identity, err := ParseBuildID(text); !errors.Is(err, ErrInvalidBuildIdentity) {
				t.Fatalf("ParseBuildID(%q) = (%#v, %v), want ErrInvalidBuildIdentity", text, identity, err)
			}
		}
	})
	t.Run("answer", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			output  string
			release string
			build   string
		}{
			{"Muse Code 1.4.2 (1.4.2-R4684.1)", "1.4.2", "1.4.2-R4684.1"},
			{"Muse Code 1.4.5 (1.4.5-R5500)\n", "1.4.5", "1.4.5-R5500"},
			{"Muse Code 1.4.2 (1.4.2-R4684.1)\r\n", "1.4.2", "1.4.2-R4684.1"},
			{"Muse Code 1.4.2 (1.4.2-R4684.1)\nsecond line cannot supply identity\n", "1.4.2", "1.4.2-R4684.1"},
		} {
			identity, err := ParseBuildIdentity([]byte(tc.output))
			if err != nil || identity.Release != tc.release || identity.Build != tc.build {
				t.Fatalf("ParseBuildIdentity(%q) = (%#v, %v), want %q/%q", tc.output, identity, err, tc.release, tc.build)
			}
		}
		for _, output := range []string{
			"", "Muse Code 1.4.2", "Muse Code 1.4.2 (1.4.2-R4684.1) extra",
			"Muse Code 1.4.1 (1.4.2-R4684.1)", "Muse Code 01.4.2 (1.4.2-R4684.1)",
			"Muse Code (1.4.2-R4684.1)", "Muse Code 1.4.2 ()",
			"Other Code 1.4.2 (1.4.2-R4684.1)", "muse code 1.4.2 (1.4.2-R4684.1)",
			"Muse Code 1.4 (1.4-R4684.1)", "Muse Code 1.4.2 (1.4.2-Rinvalid)",
			"Muse Code 1.4.2  (1.4.2-R4684.1)", "Muse Code 1.4.2(1.4.2-R4684.1)",
			"1.4.2-R4684.1", "muse-bin-1.4.2-R4684.1",
		} {
			if identity, err := ParseBuildIdentity([]byte(output)); !errors.Is(err, ErrInvalidBuildIdentity) {
				t.Fatalf("ParseBuildIdentity(%q) = (%#v, %v), want ErrInvalidBuildIdentity", output, identity, err)
			}
		}
	})
	t.Run("filename", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			base    string
			release string
			build   string
		}{
			{"muse-bin-1.4.2-R4684.1", "1.4.2", "1.4.2-R4684.1"},
			{"muse-bin-1.4.5-R5500", "1.4.5", "1.4.5-R5500"},
		} {
			identity, err := ParsePinnedBinaryName(tc.base)
			if err != nil || identity.Release != tc.release || identity.Build != tc.build {
				t.Fatalf("ParsePinnedBinaryName(%q) = (%#v, %v), want %q/%q", tc.base, identity, err, tc.release, tc.build)
			}
		}
		for _, base := range []string{
			"", "muse-bin", "muse-bin-", "muse-bin-1.4.2", "muse",
			"muse-bin-1.4.2-R1/extra", "/bin/muse-bin-1.4.2-R1",
			"MUSE-BIN-1.4.2-R1", "muse-bin-v1.4.2-R1",
		} {
			if identity, err := ParsePinnedBinaryName(base); !errors.Is(err, ErrInvalidBuildIdentity) {
				t.Fatalf("ParsePinnedBinaryName(%q) = (%#v, %v), want ErrInvalidBuildIdentity", base, identity, err)
			}
		}
	})
	t.Run("order", func(t *testing.T) {
		t.Parallel()
		mustParse := func(text string) BuildIdentity {
			t.Helper()
			identity, err := ParseBuildID(text)
			if err != nil {
				t.Fatalf("ParseBuildID(%q): %v", text, err)
			}
			return identity
		}
		for _, tc := range []struct {
			less  string
			more  string
			cause string
		}{
			{"1.4.2-R1", "1.4.10-R1", "patch orders numerically, not lexically"},
			{"1.4.2-R1", "1.10.2-R1", "minor orders numerically"},
			{"1.4.2-R1", "2.4.2-R1", "major orders numerically"},
			{"1.4.2-R9", "1.4.2-R10", "revision orders numerically"},
			{"1.4.2-R1.2", "1.4.2-R1.10", "subrevision orders numerically"},
			{"1.4.2-R1", "1.4.2-R1.0", "absent subrevision sorts before .0"},
			{"1.4.2-R1", "1.4.2-R1.1", "absent subrevision sorts before present"},
			{"1.4.2-R1.9", "1.4.3-R1", "release beats revision"},
			{"1.4.2-R9999.9", "1.4.3-R1", "release beats a larger revision"},
		} {
			less, more := mustParse(tc.less), mustParse(tc.more)
			if got, err := CompareBuildIDs(less, more); err != nil || got != -1 {
				t.Fatalf("CompareBuildIDs(%q, %q) = (%d, %v), want -1 (%s)", tc.less, tc.more, got, err, tc.cause)
			}
			if got, err := CompareBuildIDs(more, less); err != nil || got != 1 {
				t.Fatalf("CompareBuildIDs(%q, %q) = (%d, %v), want 1 (%s)", tc.more, tc.less, got, err, tc.cause)
			}
		}
		same := mustParse("1.4.2-R4684.1")
		if got, err := CompareBuildIDs(same, same); err != nil || got != 0 {
			t.Fatalf("CompareBuildIDs identical = (%d, %v), want (0, nil)", got, err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		t.Parallel()
		valid, err := ParseBuildID("1.4.2-R4684.1")
		if err != nil {
			t.Fatalf("ParseBuildID(valid): %v", err)
		}
		badBuild := BuildIdentity{Release: "1.4.2", Build: "not-a-build"}
		if _, err := CompareBuildIDs(badBuild, valid); !errors.Is(err, ErrInvalidBuildIdentity) {
			t.Fatalf("CompareBuildIDs(bad, valid) = %v, want ErrInvalidBuildIdentity", err)
		}
		if _, err := CompareBuildIDs(valid, badBuild); !errors.Is(err, ErrInvalidBuildIdentity) {
			t.Fatalf("CompareBuildIDs(valid, bad) = %v, want ErrInvalidBuildIdentity", err)
		}
		mismatched := BuildIdentity{Release: "9.9.9", Build: "1.4.2-R4684.1"}
		if _, err := CompareBuildIDs(mismatched, valid); !errors.Is(err, ErrInvalidBuildIdentity) {
			t.Fatalf("CompareBuildIDs(mismatched, valid) = %v, want ErrInvalidBuildIdentity", err)
		}
	})
}

// TestAmbiguousNumericIdentityRefuses pins the ambiguity refusal: two raw
// identities that compare numerically equal but differ as text —
// leading-zero aliases — refuse comparison rather than letting one win.
// Identical raw text still compares zero.
func TestAmbiguousNumericIdentityRefuses(t *testing.T) {
	t.Parallel()
	mustParse := func(text string) BuildIdentity {
		t.Helper()
		identity, err := ParseBuildID(text)
		if err != nil {
			t.Fatalf("ParseBuildID(%q): %v", text, err)
		}
		return identity
	}
	for _, pair := range [][2]string{
		{"1.4.2-R4684.1", "01.4.2-R4684.1"},
		{"1.4.2-R4684.1", "1.4.2-R04684.1"},
		{"1.4.2-R1.1", "1.4.2-R1.01"},
		{"1.4.2-R4684.1", "01.04.02-R04684.01"},
	} {
		first, second := mustParse(pair[0]), mustParse(pair[1])
		for _, ordered := range [][2]BuildIdentity{{first, second}, {second, first}} {
			if got, err := CompareBuildIDs(ordered[0], ordered[1]); !errors.Is(err, ErrAmbiguousBuildIdentity) {
				t.Fatalf("CompareBuildIDs(%q, %q) = (%d, %v), want ErrAmbiguousBuildIdentity",
					ordered[0].Build, ordered[1].Build, got, err)
			}
		}
	}
}
