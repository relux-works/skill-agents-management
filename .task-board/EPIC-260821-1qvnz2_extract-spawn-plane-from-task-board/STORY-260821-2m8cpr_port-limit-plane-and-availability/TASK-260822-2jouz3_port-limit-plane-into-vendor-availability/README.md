# TASK-260822-2jouz3: port-limit-plane-into-vendor-availability

## Description
Port pkg/providerlimits - detection, broker-owned classification, suppression state, backoff ladders - behind the vendor Availability verdict. THE INVARIANT: IdentityKey(provider, home) feeds the on-disk state FILENAME and the plane fails open and silent; the identity must be demonstrated unchanged by round-tripping REAL pre-extraction state files from the source repo, and a limit event recorded by the source binary must be readable by this code and vice versa. The structured verdict maps: healthy=no suppression, limited-until=active suppression with its window and evidence, unknown=state unreadable (never healthy). This task lands in STORY-260821-2m8cpr's scope conceptually but the two stories share the vendor interface; it lives here because the Availability verdict is the seam.

## Scope
(define task scope)

## Acceptance Criteria
1. IdentityKey byte-compatibility: real source state files round-trip; the hash pinned by value as the source pins it. 2. A suppression written by the source binary reads as limited-until here with the same window; absent state reads unknown-or-healthy exactly as the source decides it (fail-open preserved and DOCUMENTED, not re-litigated). 3. Backoff ladders match the source per broker, pinned. 4. Corrupt state file behaviour matches the source (its corrupt-state bug fix carries over). 5. Guard and mutants per the story standard.
