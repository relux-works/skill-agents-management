package providerlimits

import (
	"encoding/json"
	"testing"
	"time"
)

// The backoff ladder and the broker keying, pinned against the SOURCE's own
// tables rather than against constants re-typed on this side.
//
// testdata/source-tables.json is produced by .scripts/capture-limit-state.sh
// running a program compiled against the source module: it reads the source's
// exported API for the ladder, the maximum step, the group table and the
// classifier predicates, and it probes the ALIAS POLICY behaviourally, by
// feeding each candidate row name through the source's real
// SpawnLimitsConfig JSON decode and recording whether the row survived.
//
// Behavioural probing matters for the alias set specifically: the source's key
// resolution is unexported, so a fixture that listed the three legacy spellings
// as a literal would be one person's reading of a comment. This one is the
// source's answer.

// sourceTables is the captured shape of testdata/source-tables.json.
type sourceTables struct {
	DefaultLadder            []string `json:"providerlimits_default_ladder"`
	MaxLadderStep            string   `json:"providerlimits_max_ladder_step"`
	DefaultProbeLeaseMinutes int      `json:"providerlimits_default_probe_lease_minutes"`
	DefaultProbeClaimMinutes int      `json:"providerlimits_default_probe_claim_max_minutes"`
	RemoteDefaultLadder      []string `json:"remoteconfig_default_backoff_ladder"`
	RemoteMaxBackoffStep     string   `json:"remoteconfig_max_backoff_step"`
	DefaultGroups            []struct {
		Group      string   `json:"group"`
		Provider   string   `json:"provider"`
		Broker     string   `json:"broker"`
		Members    []string `json:"members"`
		Provenance string   `json:"provenance"`
	} `json:"default_groups"`
	HasClassifierByBroker  map[string]bool `json:"has_classifier_by_broker"`
	HasClassifierByRuntime map[string]bool `json:"has_classifier_by_runtime"`
	BackoffKeyAccepted     map[string]bool `json:"backoff_key_accepted"`
}

func loadSourceTables(t *testing.T) sourceTables {
	t.Helper()
	var tables sourceTables
	if err := json.Unmarshal(readRawFixture(t, "testdata", "source-tables.json"), &tables); err != nil {
		t.Fatalf("decoding the captured source tables: %v", err)
	}
	if len(tables.DefaultLadder) == 0 || len(tables.DefaultGroups) == 0 || len(tables.BackoffKeyAccepted) == 0 {
		t.Fatal("the captured source tables are empty; every pin below would pass vacuously")
	}
	return tables
}

// TestTheShippedLadderIsTheSourcesLadder pins every step by value.
func TestTheShippedLadderIsTheSourcesLadder(t *testing.T) {
	tables := loadSourceTables(t)

	got := DefaultLadder()
	if len(got) != len(tables.DefaultLadder) {
		t.Fatalf("the shipped ladder has %d steps, the source's has %d: %v vs %v", len(got), len(tables.DefaultLadder), got, tables.DefaultLadder)
	}
	for i, step := range got {
		if step.String() != tables.DefaultLadder[i] {
			t.Errorf("ladder step %d is %s, the source's is %s", i+1, step, tables.DefaultLadder[i])
		}
	}

	// The source keeps a SECOND copy of the ladder in its config module, with
	// a comment saying a cross-module test pins the two together. Both copies
	// were captured; this side has one, so it must match both.
	for i, step := range got {
		if i < len(tables.RemoteDefaultLadder) && step.String() != tables.RemoteDefaultLadder[i] {
			t.Errorf("ladder step %d is %s; the source's config-side copy is %s", i+1, step, tables.RemoteDefaultLadder[i])
		}
	}

	if MaxLadderStep.String() != tables.MaxLadderStep {
		t.Errorf("MaxLadderStep = %s, the source's is %s", MaxLadderStep, tables.MaxLadderStep)
	}
	if MaxLadderStep.String() != tables.RemoteMaxBackoffStep {
		t.Errorf("MaxLadderStep = %s, the source's config-side ceiling is %s", MaxLadderStep, tables.RemoteMaxBackoffStep)
	}
	if DefaultProbeLeaseMinutes != tables.DefaultProbeLeaseMinutes {
		t.Errorf("DefaultProbeLeaseMinutes = %d, the source's is %d", DefaultProbeLeaseMinutes, tables.DefaultProbeLeaseMinutes)
	}
	if DefaultProbeClaimMaxMinutes != tables.DefaultProbeClaimMinutes {
		t.Errorf("DefaultProbeClaimMaxMinutes = %d, the source's is %d", DefaultProbeClaimMaxMinutes, tables.DefaultProbeClaimMinutes)
	}
}

// TestTheGroupTableIsTheSourcesGroupTable pins every row, every member and
// every broker binding — the broker half especially, because DefaultGroups
// resolves it through vendorplugin here and through runtimeid there, and the
// two tables agreeing is not something either side can assume.
func TestTheGroupTableIsTheSourcesGroupTable(t *testing.T) {
	tables := loadSourceTables(t)
	ours := DefaultGroups()
	if len(ours) != len(tables.DefaultGroups) {
		t.Fatalf("this table has %d rows, the source's has %d", len(ours), len(tables.DefaultGroups))
	}
	for i, want := range tables.DefaultGroups {
		got := ours[i]
		if got.Group != want.Group || got.Provider != want.Provider {
			t.Errorf("row %d is (%q, %q), the source's is (%q, %q)", i, got.Group, got.Provider, want.Group, want.Provider)
			continue
		}
		if got.Broker != want.Broker {
			t.Errorf("row %q binds broker %q; the source binds %q — the (runtime, broker) tables disagree", got.Group, got.Broker, want.Broker)
		}
		if string(got.Provenance) != want.Provenance {
			t.Errorf("row %q has provenance %q, the source's is %q", got.Group, got.Provenance, want.Provenance)
		}
		if len(got.Members) != len(want.Members) {
			t.Errorf("row %q has %d members, the source's has %d: %v vs %v", got.Group, len(got.Members), len(want.Members), got.Members, want.Members)
			continue
		}
		for j := range got.Members {
			if got.Members[j] != want.Members[j] {
				t.Errorf("row %q member %d is %q, the source's is %q", got.Group, j, got.Members[j], want.Members[j])
			}
		}
	}
}

// TestClassifierOwnershipMatchesTheSource pins HasClassifier and its
// runtime-facing wrapper against the source's answers for every identifier the
// capture asked about — including the ones that must be FALSE.
//
// The false rows are the ones worth having. alibaba and google are real
// brokers of real built-in runtimes, and the scope fence for this port says
// alibaba stays exactly as the source has it: no captured classifier, so
// nothing can suppress it. A port that "helpfully" added one would pass every
// positive assertion here and fail these.
func TestClassifierOwnershipMatchesTheSource(t *testing.T) {
	tables := loadSourceTables(t)
	sawFalse := false
	for broker, want := range tables.HasClassifierByBroker {
		if !want {
			sawFalse = true
		}
		if got := HasClassifier(broker); got != want {
			t.Errorf("HasClassifier(%q) = %v, the source says %v", broker, got, want)
		}
	}
	for runtime, want := range tables.HasClassifierByRuntime {
		if !want {
			sawFalse = true
		}
		if got := HasClassifierForRuntime(runtime); got != want {
			t.Errorf("HasClassifierForRuntime(%q) = %v, the source says %v", runtime, got, want)
		}
	}
	if !sawFalse {
		t.Fatal("the captured tables contain no negative classifier answer, so this test could not tell a widened classifier set from a correct one")
	}
}

// TestTheBackoffAliasPolicyIsTheSources is the alias half of the broker
// re-key, pinned against the source's own decode.
//
// Three runtime spellings are legal for one release — the ones that were
// already legal before the re-key. Three other built-in runtimes are NOT, and
// two of them (gemini, agy) have an established broker, so their rejection is
// specifically about the alias policy rather than about an unknown broker.
func TestTheBackoffAliasPolicyIsTheSources(t *testing.T) {
	tables := loadSourceTables(t)
	acceptedHere := 0
	rejectedHere := 0
	for key, want := range tables.BackoffKeyAccepted {
		got := ResolveLadderKey(key).Known
		if got {
			acceptedHere++
		} else {
			rejectedHere++
		}
		if got != want {
			t.Errorf("ResolveLadderKey(%q).Known = %v, the source's decode says %v", key, got, want)
		}
	}
	if acceptedHere == 0 || rejectedHere == 0 {
		t.Fatalf("the alias probe found %d accepted and %d rejected keys; it must find both or it distinguishes nothing", acceptedHere, rejectedHere)
	}

	// The legacy set is CLOSED, and it is the narrowing that matters: a key
	// accepted as a legacy runtime spelling must be one of exactly three.
	legacy := map[string]bool{}
	for _, key := range LegacyLadderKeys() {
		legacy[key] = true
	}
	if len(legacy) != 3 || !legacy["claude"] || !legacy["codex"] || !legacy["qwen"] {
		t.Fatalf("LegacyLadderKeys() = %v; the source's one-release migration set is exactly claude, codex and qwen", LegacyLadderKeys())
	}
	for key, want := range tables.BackoffKeyAccepted {
		resolution := ResolveLadderKey(key)
		if resolution.Legacy && !legacy[key] {
			t.Errorf("ResolveLadderKey(%q) reports a legacy runtime spelling, but %q is not in the closed set %v", key, key, LegacyLadderKeys())
		}
		if want && !resolution.Legacy && resolution.Broker != key {
			t.Errorf("ResolveLadderKey(%q) accepted the key as broker %q; a non-legacy key must resolve to itself", key, resolution.Broker)
		}
	}
}

// TestLegacyLadderKeysCannotBeWidenedByACaller is the aliasing guard on the
// accessor itself: it hands out a copy, so a caller appending "gemini" to the
// answer does not widen the policy for the next reader.
func TestLegacyLadderKeysCannotBeWidenedByACaller(t *testing.T) {
	keys := LegacyLadderKeys()
	keys = append(keys, "gemini")
	_ = keys
	if ResolveLadderKey("gemini").Known {
		t.Fatal("gemini became a legal ladder key after a caller appended it to LegacyLadderKeys(); the policy is not a copy")
	}
	if len(LegacyLadderKeys()) != 3 {
		t.Fatalf("LegacyLadderKeys() = %v after a caller appended to a previous answer", LegacyLadderKeys())
	}
}

// TestALadderRowIsFoundByRuntimeFirstThenBroker pins the source's lookup order.
func TestALadderRowIsFoundByRuntimeFirstThenBroker(t *testing.T) {
	runtimeSteps := []time.Duration{time.Minute, 2 * time.Minute}
	brokerSteps := []time.Duration{time.Hour, 2 * time.Hour}

	both := ResolveLadderFor(ProviderClaude, map[string][]time.Duration{
		ProviderClaude:  runtimeSteps,
		BrokerAnthropic: brokerSteps,
	})
	if both.Key != ProviderClaude || !both.Legacy {
		t.Errorf("with both rows present the runtime spelling must win: key=%q legacy=%v", both.Key, both.Legacy)
	}
	if both.Steps[0] != time.Minute {
		t.Errorf("the broker row was applied over the runtime row: %v", both.Steps)
	}

	brokerOnly := ResolveLadderFor(ProviderClaude, map[string][]time.Duration{BrokerAnthropic: brokerSteps})
	if brokerOnly.Key != BrokerAnthropic || brokerOnly.Legacy {
		t.Errorf("with only the broker row present it must supply the ladder: key=%q legacy=%v", brokerOnly.Key, brokerOnly.Legacy)
	}
	if brokerOnly.Steps[0] != time.Hour {
		t.Errorf("the broker row did not supply the steps: %v", brokerOnly.Steps)
	}

	// Two runtimes on one broker share the broker row. That is the whole
	// reason the table is keyed by broker: one pool, one ladder.
	shared := ResolveLadderFor(ProviderCodex, map[string][]time.Duration{BrokerOpenAI: brokerSteps})
	if !shared.Configured || shared.Key != BrokerOpenAI {
		t.Errorf("codex did not pick up its broker's row: %+v", shared)
	}

	absent := ResolveLadderFor(ProviderCodex, nil)
	if absent.Configured {
		t.Error("an unconfigured runtime reports a configured ladder")
	}
	if len(absent.Steps) != len(DefaultLadder()) || absent.Steps[0] != DefaultLadder()[0] {
		t.Errorf("an unconfigured runtime got %v, want the shipped ladder %v", absent.Steps, DefaultLadder())
	}
}

// TestAnUnknownLadderRowIsReportedAndDroppedNotRefused is the source's
// tolerance rule: an unresolvable row must not fail the table.
func TestAnAnUnknownLadderRowIsReportedAndDroppedNotRefused(t *testing.T) {
	unknown, err := ValidateConfiguredLadders(map[string][]time.Duration{
		"gemini":          {time.Minute},
		BrokerAnthropic:   {time.Minute, 2 * time.Minute},
		"never-heard-of":  {time.Minute},
		"definitely-not":  {time.Minute},
		BrokerOpenAI:      {time.Minute},
		"another-unknown": {time.Minute},
	})
	if err != nil {
		t.Fatalf("an unresolvable row must be dropped, not refused: %v", err)
	}
	want := map[string]bool{"gemini": true, "never-heard-of": true, "definitely-not": true, "another-unknown": true}
	if len(unknown) != len(want) {
		t.Fatalf("unknown rows = %v, want exactly %d rows: %v", unknown, len(want), want)
	}
	for _, key := range unknown {
		if !want[key] {
			t.Errorf("row %q was reported unknown and should have resolved", key)
		}
	}
}

// TestTwoSpellingsOfOneBrokerAreRefused is the NEGATIVE that makes the broker
// keying mean something.
//
// Writing the legacy runtime row and the broker row for one pool side by side
// is the shape the one-release migration creates. Silently honouring one of
// them would apply a ladder the operator can see they did not choose, so both
// origins are named and the table is refused.
func TestTwoSpellingsOfOneBrokerAreRefused(t *testing.T) {
	_, err := ValidateConfiguredLadders(map[string][]time.Duration{
		ProviderClaude:  {time.Minute},
		BrokerAnthropic: {time.Hour},
	})
	if err == nil {
		t.Fatal("a table spelling one broker twice was admitted; the second row is silently unused")
	}
	for _, want := range []string{"claude", "anthropic"} {
		if !contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, so an operator has to work the collision out themselves: %v", want, err)
		}
	}

	// Narrowing: the same two rows for DIFFERENT brokers are fine. Without
	// this, a rule that refused any two rows at all would pass the test above.
	if _, err := ValidateConfiguredLadders(map[string][]time.Duration{
		BrokerAnthropic: {time.Minute},
		BrokerOpenAI:    {time.Hour},
	}); err != nil {
		t.Fatalf("two rows for two different brokers were refused: %v", err)
	}
}

// TestALadderRowThatBreaksTheLadderRulesIsRefused covers the step contract on
// configured rows: a step above the six-hour ceiling, a shrinking ladder and an
// empty row are each refused, with the row named.
func TestALadderRowThatBreaksTheLadderRulesIsRefused(t *testing.T) {
	for name, steps := range map[string][]time.Duration{
		"above the ceiling": {MaxLadderStep + time.Minute},
		"decreasing":        {time.Hour, time.Minute},
		"non-positive":      {0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateConfiguredLadders(map[string][]time.Duration{BrokerAnthropic: steps}); err == nil {
				t.Fatalf("a %s ladder was admitted: %v", name, steps)
			} else if !contains(err.Error(), BrokerAnthropic) {
				t.Errorf("the refusal does not name the offending row: %v", err)
			}
		})
	}
	if _, err := ValidateConfiguredLadders(map[string][]time.Duration{BrokerAnthropic: {2 * time.Minute, 2 * time.Minute, MaxLadderStep}}); err != nil {
		t.Fatalf("a legal non-decreasing ladder at the ceiling was refused: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
