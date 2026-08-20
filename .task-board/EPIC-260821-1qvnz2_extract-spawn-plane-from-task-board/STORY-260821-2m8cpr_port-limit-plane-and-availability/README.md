# STORY-260821-2m8cpr: port-limit-plane-and-availability

## Description
Move provider limit detection, classification (broker-owned), suppression, backoff and retry outcomes, plus the health/availability checks, behind the vendor plugin interface. The limit plane fails OPEN and SILENT - an absent state file reads as provider healthy - so the on-disk identity (IdentityKey(provider, home) feeding the state filename) must be demonstrated unchanged by round-tripping REAL pre-extraction state files, not argued from types.

## Scope
(define story scope)

## Acceptance Criteria
(define acceptance criteria)
