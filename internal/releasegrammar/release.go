// Package releasegrammar validates release strings without process control.
package releasegrammar

import "strings"

// IsReleaseTriple reports whether s is a dotted release triple:
// three dot-separated runs of ASCII digits, "2.1.261". There is no
// leading-zero rule and no prerelease suffix: the pinned releases are
// exact triples, and anything else fails closed at the probe (never
// established) rather than matching a row it merely resembles. A future
// verified release with a suffix re-verifies this rule with its grammar
// version.
func IsReleaseTriple(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	return true
}
