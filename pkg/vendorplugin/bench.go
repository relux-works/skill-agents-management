package vendorplugin

import (
	"fmt"
	"regexp"
	"strings"
)

// The ONE benchmark every capability score in this module is anchored on.
//
// Until the bench, a score was hand-ordered decades per vendor (openai
// 130..10, anthropic 85..10) with nothing behind it but the author's placing,
// so "gpt-5.6-sol scores 120" said only that somebody wrote 120 there. A score
// is now a bench-anchored capability estimate: a row the leaderboard measured
// carries the exact fixed/105 value at its best effort setting, a row it did
// not measure carries an explicitly INTERPOLATED value between two named
// anchors, and every row cites the bench as a RankEvidence source through the
// constructors in this file.
//
// # What the common scale does and does not license
//
// Because every vendor's rows sit on the same bench scale, a score IS
// comparable across vendors — but only through the cited benchmark, and only
// as far as the bench measured. A measured row compared with a measured row is
// the leaderboard's own finding; a comparison that touches an interpolated row
// inherits that row's interpolation and says so in its evidence. Scores are
// still NOT policy: ceilings and workload classes stay in the consuming
// repository, and nothing admission-related reads a score.
//
// # Why the observation has a grammar
//
// The vendor files type the score and the bench claim SEPARATELY — the score
// as an int, the claim through BughuntMeasured or BughuntInterpolated — and
// the consistency test (bughunt_test.go) parses the claim back out and holds
// the two against each other and against an independent transcription of the
// leaderboard. Two independent statements per row are what let a slipped
// digit fail: a constructor that derived the observation FROM the score would
// agree with any score at all.

// BugHuntBenchSource names the leaderboard every measured and interpolated
// score rests on. The URL and the leaderboard date are in the string so a
// reader can open the page and see whether it moved.
const BugHuntBenchSource = "Bug Hunt Bench leaderboard, https://bughunt.productcompass.pm/ (updated 2026-09-13): planted bugs fixed out of 105, verified blind, unplanted fixes never counted"

// BugHuntBenchTotal is the denominator of every measured observation.
const BugHuntBenchTotal = 105

// BugHuntBenchFloor is the anchor an interpolated row names below itself when
// nothing in its lineup sits under it. It is a NAMED anchor rather than an
// omitted one so the consistency test can resolve it to 0 and hold the row's
// score strictly above it.
const BugHuntBenchFloor = "the bench floor (0/105)"

// BughuntMeasured is the evidence for a row the leaderboard measured: the
// fixed count at the effort setting the count was taken at, which must be the
// row's BEST setting — other settings are cost or latency evidence, recorded
// through BughuntCost, never the capability claim.
func BughuntMeasured(effort string, fixed int) RankEvidence {
	return RankEvidence{
		Source:      BugHuntBenchSource,
		Observation: fmt.Sprintf("fixed %d/%d at %s", fixed, BugHuntBenchTotal, effort),
	}
}

// BughuntMeasuredAs is BughuntMeasured for a row the leaderboard lists under a
// different spelling — qwen3.8-max-preview is measured as qwen3.8-max, and an
// alias row is measured as its identity. The bench spelling is in the
// observation so the consistency test resolves the claim against the
// leaderboard rather than against the row's own id.
func BughuntMeasuredAs(benchID string, effort string, fixed int) RankEvidence {
	return RankEvidence{
		Source:      BugHuntBenchSource,
		Observation: fmt.Sprintf("fixed %d/%d at %s, measured as %s", fixed, BugHuntBenchTotal, effort, benchID),
	}
}

// BughuntInterpolated is the evidence for a row the leaderboard did not
// measure. Both anchors are required: a row "somewhere below X" has no
// interval to have been interpolated into. Each anchor is a model id the same
// registry declares, a model id the leaderboard measured (in or out of the
// lineup), or BugHuntBenchFloor; the consistency test resolves every anchor
// and refuses one it cannot. The note follows the anchors and says why the
// row sits where it does — the vendor's own tiering, a kept registry order, a
// tie with a neighbour.
func BughuntInterpolated(above, below string, note string) RankEvidence {
	observation := fmt.Sprintf("interpolated between %s and %s", above, below)
	if strings.TrimSpace(note) != "" {
		observation += "; " + note
	}
	return RankEvidence{Source: BugHuntBenchSource, Observation: observation}
}

// BughuntCost records a leaderboard observation that is NOT the capability
// claim: a cost, latency or second-setting reading that a reader choosing a
// model should see next to the score without mistaking it for one. The prefix
// is what keeps the two apart when the observation is parsed back.
func BughuntCost(observation string) RankEvidence {
	return RankEvidence{Source: BugHuntBenchSource, Observation: "cost: " + observation}
}

// BughuntClaimKind is which of the three observations a bench entry makes.
type BughuntClaimKind string

const (
	// BughuntMeasuredClaim is "fixed N/105 at <effort>".
	BughuntMeasuredClaim BughuntClaimKind = "measured"
	// BughuntInterpolatedClaim is "interpolated between <above> and <below>".
	BughuntInterpolatedClaim BughuntClaimKind = "interpolated"
	// BughuntCostClaim is a "cost: ..." annotation and carries no score.
	BughuntCostClaim BughuntClaimKind = "cost"
)

// BughuntClaim is one bench observation parsed back into its parts.
type BughuntClaim struct {
	Kind BughuntClaimKind
	// Fixed, Effort and BenchID are set for a measured claim. BenchID is the
	// row's own id unless the observation said "measured as".
	Fixed   int
	Effort  string
	BenchID string
	// Above and Below are set for an interpolated claim.
	Above, Below string
}

var (
	bughuntMeasuredPattern     = regexp.MustCompile(`^fixed (\d+)/(\d+) at ([a-z]+)(?:, measured as (\S+))?$`)
	bughuntInterpolatedPattern = regexp.MustCompile(`^interpolated between (.+?) and (.+?)(?:; .*)?$`)
)

// ErrBughuntObservationInvalid marks a bench-sourced observation that fits
// none of the three grammars.
var ErrBughuntObservationInvalid = fmt.Errorf("vendorplugin: bench observation fits no grammar")

// ParseBughuntEvidence reads a bench-sourced observation back into a claim.
//
// It is the inverse of the constructors above and refuses anything the
// constructors could not have produced, so a hand-typed observation that
// LOOKS like a claim — "fixed about 40/105" — is a refusal rather than a row
// the consistency test quietly skips. An entry from any other source is
// reported as not a bench entry.
func ParseBughuntEvidence(evidence RankEvidence) (BughuntClaim, error) {
	if evidence.Source != BugHuntBenchSource {
		return BughuntClaim{}, fmt.Errorf("%w: source %q is not the bench", ErrBughuntObservationInvalid, evidence.Source)
	}
	obs := evidence.Observation
	if strings.HasPrefix(obs, "cost: ") {
		return BughuntClaim{Kind: BughuntCostClaim}, nil
	}
	if m := bughuntMeasuredPattern.FindStringSubmatch(obs); m != nil {
		// fmt rather than strconv: the regexp already guarantees digits, and
		// strconv is outside the observer boundary's import allowlist.
		var fixed, total int
		fmt.Sscanf(m[1], "%d", &fixed)
		fmt.Sscanf(m[2], "%d", &total)
		if total != BugHuntBenchTotal {
			return BughuntClaim{}, fmt.Errorf("%w: %q counts out of %d and the bench plants %d", ErrBughuntObservationInvalid, obs, total, BugHuntBenchTotal)
		}
		return BughuntClaim{Kind: BughuntMeasuredClaim, Fixed: fixed, Effort: m[3], BenchID: m[4]}, nil
	}
	if m := bughuntInterpolatedPattern.FindStringSubmatch(obs); m != nil {
		return BughuntClaim{Kind: BughuntInterpolatedClaim, Above: m[1], Below: m[2]}, nil
	}
	return BughuntClaim{}, fmt.Errorf("%w: %q", ErrBughuntObservationInvalid, obs)
}
