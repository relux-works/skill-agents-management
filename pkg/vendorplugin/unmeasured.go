package vendorplugin

import (
	"fmt"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/benchdata"
)

// UnmeasuredClaim is an explicit availability claim, never a numeric rank.
// It is mutually exclusive with Model.Rank and is excluded from every Lineup.
type UnmeasuredClaim = benchdata.UnmeasuredClaim

// UnmeasuredClaimKind is the only admitted kind of availability-only claim.
const UnmeasuredClaimKind = "unmeasured"

func (m Model) validateRankingClaim() error {
	if m.Unmeasured == nil {
		if m.Rank == nil {
			return fmt.Errorf("%w: model carries neither a rank nor an unmeasured claim", ErrRankInvalid)
		}
		return m.Rank.Validate()
	}
	if m.Rank != nil {
		return fmt.Errorf("%w: unmeasured model must carry no numeric rank (including zero)", ErrRankInvalid)
	}
	claim := m.Unmeasured
	if claim.Kind != UnmeasuredClaimKind {
		return fmt.Errorf("%w: unknown unmeasured claim kind %q", ErrRankInvalid, claim.Kind)
	}
	if _, err := time.Parse("2006-01-02", claim.AsOf); err != nil {
		return fmt.Errorf("%w: unmeasured availability requires an ISO date", ErrRankInvalid)
	}
	if strings.TrimSpace(claim.Source) == "" || strings.TrimSpace(claim.Observation) == "" {
		return fmt.Errorf("%w: unmeasured availability requires an attributed observation", ErrRankInvalid)
	}
	if m.AliasOf != "" || m.Recommended {
		return fmt.Errorf("%w: unmeasured model requires its exact id and cannot be a display recommendation", ErrRankInvalid)
	}
	return nil
}
