package muse

import (
	"errors"
	"fmt"
	"strings"
)

// THIS FILE IS THE ONE MUSE BUILD-IDENTITY GRAMMAR.
//
// Every consumer of a Muse build identity — the launcher selector, the
// pinned filename, the version answer, the MSP gate and the module seal —
// parses and orders through these exports. No other regex, split or
// comparison in either repository may re-derive what a build is: a second
// spelling is how the mandatory-dot gate rejected launcher-valid builds
// while the selector accepted them.
//
// The grammar, exactly:
//
//	release := digit+ "." digit+ "." digit+
//	build   := release "-R" digit+ ("." digit+)?
//	name    := "muse-bin-" build
//	answer  := "Muse Code " release " (" build ")"
//
// Digits are ASCII 0-9 only. The release text in an answer must equal the
// build's release prefix byte for byte. There is no whitespace inside an
// identity, no suffix, no extra component, no prerelease label and no
// substring match: anything else is malformed, never a guessed identity.
// Leading zeros are syntactically valid and semantically ambiguous: two
// raw identities that compare numerically equal but differ as text refuse
// comparison rather than letting one win arbitrarily.
const (
	// pinnedBinaryNamePrefix is the versioned-filename namespace every
	// frozen Muse copy lives under. The exact legacy bare "muse-bin"
	// without a build suffix is not a member.
	pinnedBinaryNamePrefix = "muse-bin-"
	// versionAnswerPrefix opens every well-formed Muse version answer.
	versionAnswerPrefix = "Muse Code "
	// buildRevisionSeparator joins a release triple to its revision.
	buildRevisionSeparator = "-R"
)

var (
	// ErrInvalidBuildIdentity refuses a malformed Muse build identity:
	// a shape outside the grammar above, in any of the four parsed
	// positions (bare build, version answer, pinned filename, sealed
	// release selector).
	ErrInvalidBuildIdentity = errors.New("muse: invalid build identity")
	// ErrAmbiguousBuildIdentity refuses a build comparison whose two
	// raw identities are numerically equal but textually different, such
	// as leading-zero aliases. Comparison chooses nothing arbitrarily.
	ErrAmbiguousBuildIdentity = errors.New("muse: ambiguous build identity")
)

// BuildIdentity is one parsed Muse build: the exact release triple text
// and the exact full build text. Both spellings are the raw observed
// bytes — no normalization — because normalization is what would hide a
// leading-zero alias from the ambiguity refusal.
type BuildIdentity struct {
	// Release is the exact release triple, "1.4.2".
	Release string
	// Build is the exact full build, "1.4.2-R4684.1" or "1.4.5-R5500".
	Build string
}

// ParseBuildID parses exactly one full build identity: a release triple,
// "-R", a revision number and an optional dot-subrevision. A bare triple,
// a pinned filename and a version answer are all refused here; each has
// its own parser below.
func ParseBuildID(text string) (BuildIdentity, error) {
	if release, _, _, _, err := splitBuildID(text); err != nil {
		return BuildIdentity{}, err
	} else {
		return BuildIdentity{Release: release, Build: text}, nil
	}
}

// ParsePinnedBinaryName parses exactly one versioned binary filename:
// "muse-bin-" plus a full build identity. The argument is a base name,
// never a path: anything carrying a separator is refused rather than
// trimmed to one.
func ParsePinnedBinaryName(base string) (BuildIdentity, error) {
	rest, ok := strings.CutPrefix(base, pinnedBinaryNamePrefix)
	if !ok || rest == "" || strings.ContainsRune(base, '/') {
		return BuildIdentity{}, fmt.Errorf("%w: %q is not `muse-bin-<release>-R<revision>`", ErrInvalidBuildIdentity, base)
	}
	return ParseBuildID(rest)
}

// ParseBuildIdentity parses the first line of Muse's version answer,
// such as "Muse Code 1.4.2 (1.4.2-R4684.1)". The release triple must
// agree with the build's release prefix byte for byte. Bounded later
// diagnostic lines are captured by the caller but can neither supply
// nor override the identity: only the first line is read, with an
// optional terminal CR before the LF tolerated.
func ParseBuildIdentity(versionOutput []byte) (BuildIdentity, error) {
	line, _, _ := strings.Cut(string(versionOutput), "\n")
	line = strings.TrimSuffix(line, "\r")
	line = strings.TrimSpace(line)
	rest, ok := strings.CutPrefix(line, versionAnswerPrefix)
	if !ok {
		return BuildIdentity{}, fmt.Errorf("%w: the answer %q is not `Muse Code <release> (<release>-R<revision>)`", ErrInvalidBuildIdentity, line)
	}
	release, tail, ok := strings.Cut(rest, " (")
	if !ok || !strings.HasSuffix(tail, ")") {
		return BuildIdentity{}, fmt.Errorf("%w: the answer %q is not `Muse Code <release> (<release>-R<revision>)`", ErrInvalidBuildIdentity, line)
	}
	build := strings.TrimSuffix(tail, ")")
	identity, err := ParseBuildID(build)
	if err != nil {
		return BuildIdentity{}, fmt.Errorf("%w: the answer %q is not `Muse Code <release> (<release>-R<revision>)`", ErrInvalidBuildIdentity, line)
	}
	if identity.Release != release {
		return BuildIdentity{}, fmt.Errorf("%w: the answer %q names release %q for build %q", ErrInvalidBuildIdentity, line, release, build)
	}
	return identity, nil
}

// CompareBuildIDs orders two build identities numerically: release
// triple, then R number, then optional subrevision. Comparison is on
// decimal strings — leading zeros trimmed for the comparison only, then
// length, then digits — never on fixed-width integers or locale order.
// An absent subrevision sorts before any present one, including .0.
// Numerically equal identities with different raw text are ambiguous
// and refuse; identical raw text compares zero.
func CompareBuildIDs(a, b BuildIdentity) (int, error) {
	var partsA buildIDParts
	if computed, err := buildIDComponents(a); err != nil {
		return 0, err
	} else {
		partsA = computed
	}
	var partsB buildIDParts
	if computed, err := buildIDComponents(b); err != nil {
		return 0, err
	} else {
		partsB = computed
	}
	for i := 0; i < 3; i++ {
		if c := compareDecimalStrings(partsA.release[i], partsB.release[i]); c != 0 {
			return c, nil
		}
	}
	if c := compareDecimalStrings(partsA.revision, partsB.revision); c != 0 {
		return c, nil
	}
	switch {
	case !partsA.hasSub && !partsB.hasSub:
	case !partsA.hasSub:
		return -1, nil
	case !partsB.hasSub:
		return 1, nil
	default:
		if c := compareDecimalStrings(partsA.subrevision, partsB.subrevision); c != 0 {
			return c, nil
		}
	}
	if a.Build != b.Build || a.Release != b.Release {
		return 0, fmt.Errorf("%w: %q and %q are numerically equal but textually different", ErrAmbiguousBuildIdentity, a.Build, b.Build)
	}
	return 0, nil
}

// buildIDParts is one validated build split into its numeric components.
type buildIDParts struct {
	release     [3]string
	revision    string
	subrevision string
	hasSub      bool
}

// buildIDComponents validates a BuildIdentity as a whole — the build
// parses, and the release is exactly the build's release prefix — and
// splits it for comparison. Structs no parser produced are still held to
// the same grammar.
func buildIDComponents(identity BuildIdentity) (buildIDParts, error) {
	var release, revision, subrevision string
	var hasSub bool
	if splitRelease, splitRevision, splitSubrevision, splitHasSub, err := splitBuildID(identity.Build); err != nil {
		return buildIDParts{}, err
	} else {
		release, revision, subrevision, hasSub = splitRelease, splitRevision, splitSubrevision, splitHasSub
	}
	if identity.Release != release {
		return buildIDParts{}, fmt.Errorf("%w: release %q is not the prefix of build %q", ErrInvalidBuildIdentity, identity.Release, identity.Build)
	}
	var releaseParts [3]string
	if computed, err := splitReleaseTriple(release); err != nil {
		return buildIDParts{}, err
	} else {
		releaseParts = computed
	}
	return buildIDParts{release: releaseParts, revision: revision, subrevision: subrevision, hasSub: hasSub}, nil
}

// splitBuildID splits exactly one full build into its release text,
// revision digits and optional subrevision digits.
func splitBuildID(text string) (release, revision, subrevision string, hasSub bool, err error) {
	before, after, ok := strings.Cut(text, buildRevisionSeparator)
	if !ok || before == "" || after == "" {
		return "", "", "", false, fmt.Errorf("%w: %q is not `<release>-R<revision>`", ErrInvalidBuildIdentity, text)
	}
	releaseParts, err := splitReleaseTriple(before)
	if err != nil {
		return "", "", "", false, fmt.Errorf("%w: %q is not `<release>-R<revision>`", ErrInvalidBuildIdentity, text)
	}
	_ = releaseParts
	revision, subrevision, hasSub = strings.Cut(after, ".")
	if revision == "" || !isASCIIDigits(revision) {
		return "", "", "", false, fmt.Errorf("%w: %q is not `<release>-R<revision>`", ErrInvalidBuildIdentity, text)
	}
	if hasSub {
		if subrevision == "" || !isASCIIDigits(subrevision) || strings.ContainsRune(subrevision, '.') {
			return "", "", "", false, fmt.Errorf("%w: %q is not `<release>-R<revision>`", ErrInvalidBuildIdentity, text)
		}
	}
	return before, revision, subrevision, hasSub, nil
}

// splitReleaseTriple splits exactly one dotted release triple into its
// three ASCII-digit components.
func splitReleaseTriple(text string) ([3]string, error) {
	var parts [3]string
	first, rest, ok := strings.Cut(text, ".")
	if !ok {
		return parts, fmt.Errorf("%w: %q is not `<digit>.<digit>.<digit>`", ErrInvalidBuildIdentity, text)
	}
	second, third, ok := strings.Cut(rest, ".")
	if !ok || strings.ContainsRune(third, '.') {
		return parts, fmt.Errorf("%w: %q is not `<digit>.<digit>.<digit>`", ErrInvalidBuildIdentity, text)
	}
	if first == "" || second == "" || third == "" || !isASCIIDigits(first) || !isASCIIDigits(second) || !isASCIIDigits(third) {
		return parts, fmt.Errorf("%w: %q is not `<digit>.<digit>.<digit>`", ErrInvalidBuildIdentity, text)
	}
	return [3]string{first, second, third}, nil
}

// isLowerHex64 reports whether s is exactly 64 lowercase hex digits: a
// SHA-256 in the frozen tuple and seal-selector spelling. Uppercase hex
// is a different spelling, never an alias.
func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		digit := (s[i] >= '0' && s[i] <= '9') || (s[i] >= 'a' && s[i] <= 'f')
		if !digit {
			return false
		}
	}
	return true
}

// isASCIIDigits reports whether s is one or more ASCII digits and nothing
// else. Unicode digits, signs and whitespace are not digits here.
func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// compareDecimalStrings orders two ASCII-digit runs numerically: leading
// zeros trimmed for the comparison only, then digit count, then digits.
// An all-zero run compares as zero, whatever its width.
func compareDecimalStrings(a, b string) int {
	trimmedA := strings.TrimLeft(a, "0")
	trimmedB := strings.TrimLeft(b, "0")
	if trimmedA == "" {
		trimmedA = "0"
	}
	if trimmedB == "" {
		trimmedB = "0"
	}
	if len(trimmedA) != len(trimmedB) {
		if len(trimmedA) < len(trimmedB) {
			return -1
		}
		return 1
	}
	return strings.Compare(trimmedA, trimmedB)
}
