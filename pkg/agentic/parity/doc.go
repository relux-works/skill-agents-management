// Package parity holds the launch-surface goldens the agentic-system plugin
// ports are proved against, and the harness that compares an agentic.Plan to
// one.
//
// # What a golden is, and what it is not
//
// A golden is the OBSERVABLE launch surface of one (system, mode) combination
// as the extraction source produced it: the binary that was resolved, the full
// argv, the environment keys added and removed relative to the parent process,
// and the stdin bytes. Invariant 1 of docs/architecture.md makes that surface
// the parity bar, and the reason it is these four fields rather than an argv
// string is that argv-string equality passes while the environment or the
// stdin protocol silently diverges.
//
// Every golden in testdata/goldens was captured by the SOURCE repository's own
// harness — skill-project-management,
// tools/board-cli/internal/spawn/parity_capture_test.go, TestCaptureLaunchSurface,
// which is skipped unless SPAWN_PARITY_CAPTURE_OUT is set. Nothing in this
// package captures anything. That division is the whole point: a golden
// captured by code living next to the port proves only that the new code
// agrees with itself, which is precisely the assurance a port must not be
// given. This package captures nothing and compares everything.
//
// The source commit each golden was captured at is recorded inside the golden
// file. A golden whose provenance is unknown cannot settle a dispute about
// what the source actually did.
//
// # What the harness does
//
// FromPlan maps an agentic.Plan onto the source's snapshot schema. Compare
// reports EVERY field that differs — it has no notion of a difference small
// enough to ignore, and TestCompareReportsEveryField holds it to that by
// reflection over the Snapshot type, so a field added later fails the suite
// until Compare learns it.
//
// # Masking
//
// Two things in a captured surface are machine-local rather than contractual:
// the temporary directories testing.T.TempDir allocates, and the PATH the
// capture seeded to make the harness hermetic. The source excluded PATH from
// its own env diff for exactly this reason and masked temp-dir noise before
// diffing its two snapshots. Both rules are ported here, in mask.go, as a
// CLOSED set — MaskRules is frozen by TestMaskRuleSetIsFrozen, and the set of
// snapshot fields masking is allowed to rewrite is frozen by
// TestMaskingCoversExactlyTheDeclaredFields.
//
// The pin matters more than the rules. Masking that grows is how parity
// evidence rots: each widening makes a real difference invisible, and the
// suite stays green while the port drifts. A widening must therefore cost a
// deliberate edit to a frozen list plus the sentence explaining why, which is
// the smallest price at which the erosion stops being silent.
package parity
