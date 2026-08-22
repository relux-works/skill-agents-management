package providerlimits

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultGroupTableIsClaudeAndCodexOnly(t *testing.T) {
	for _, row := range DefaultGroups() {
		switch row.Provider {
		case ProviderClaude, ProviderCodex:
		default:
			t.Fatalf("group %q belongs to provider %q; the table covers claude and codex only, because a row for a provider with no classifier could never be written to", row.Group, row.Provider)
		}
		switch row.Provenance {
		case ProvenanceVendorStringEvidence, ProvenanceOwnerAssertion:
		default:
			t.Fatalf("group %q carries provenance %q; only vendor_string_evidence and owner_assertion are admissible", row.Group, row.Provenance)
		}
		if strings.Contains(string(row.Provenance), "registry_pricing") {
			t.Fatalf("group %q carries registry_pricing provenance, which means \"inferred from a price list\"", row.Group)
		}
		if strings.Contains(strings.ToLower(row.Group), "qwen") {
			t.Fatalf("group %q is a qwen row; limit detection is scoped to the two providers with captured error shapes", row.Group)
		}
	}
}

func TestDefaultGroupTableMatchesDesign(t *testing.T) {
	want := map[string]struct {
		provider   string
		provenance Provenance
		members    []string
	}{
		"claude-plan": {ProviderClaude, ProvenanceVendorStringEvidence, []string{
			"claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5",
			"claude-opus-4-8", "claude-opus-4-6", "claude-sonnet-4-6",
			"claude-haiku-4-5-20251001",
		}},
		"claude-usage-credits": {ProviderClaude, ProvenanceVendorStringEvidence, []string{"claude-fable-5"}},
		"codex-plan": {ProviderCodex, ProvenanceOwnerAssertion, []string{
			"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4",
			"gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2-codex", "gpt-5.2",
			"gpt-5.1-codex-max", "gpt-5.1-codex-mini",
		}},
		"codex-spark": {ProviderCodex, ProvenanceOwnerAssertion, []string{"gpt-5.3-codex-spark"}},
	}
	rows := DefaultGroups()
	if len(rows) != len(want) {
		t.Fatalf("group table has %d rows, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		expected, ok := want[row.Group]
		if !ok {
			t.Fatalf("unexpected group %q in the table", row.Group)
		}
		if row.Provider != expected.provider {
			t.Errorf("group %q provider = %q, want %q", row.Group, row.Provider, expected.provider)
		}
		if row.Provenance != expected.provenance {
			t.Errorf("group %q provenance = %q, want %q", row.Group, row.Provenance, expected.provenance)
		}
		if strings.Join(row.Members, ",") != strings.Join(expected.members, ",") {
			t.Errorf("group %q members = %v, want %v", row.Group, row.Members, expected.members)
		}
	}
}

func TestEveryMappedModelResolvesToExactlyOneGroup(t *testing.T) {
	table := MustGroupTable()
	seen := map[string]string{}
	for _, row := range DefaultGroups() {
		for _, member := range row.Members {
			key := row.Provider + "/" + member
			if prior, dup := seen[key]; dup {
				t.Fatalf("model %s is in both %q and %q", key, prior, row.Group)
			}
			seen[key] = row.Group
			if got := table.Group(row.Provider, member); got != row.Group {
				t.Errorf("Lookup(%s) = %q, want %q", key, got, row.Group)
			}
		}
	}
}

// A Qwen model resolves to its own singleton group, and nothing can ever suppress
// it, because this module ships no Qwen classifier. That is the whole consequence
// of removing the qwen row from the table: Qwen stays a legal provider-policy
// member while limit detection stays scoped to the evidenced providers.
func TestQwenResolvesToSingletonAndCanNeverBeSuppressed(t *testing.T) {
	table := MustGroupTable()
	ref := table.Lookup("qwen", "qwen3-max")
	if !ref.Unmapped {
		t.Fatalf("qwen3-max resolved to mapped group %q; the table has no qwen row", ref.Group)
	}
	if want := "qwen-unmapped:qwen3-max"; ref.Group != want {
		t.Fatalf("qwen3-max group = %q, want %q", ref.Group, want)
	}
	if ref.Provenance != "" {
		t.Errorf("singleton group carries provenance %q; an unmapped model asserts no mapping", ref.Provenance)
	}
	if HasClassifierForRuntime("qwen") {
		t.Fatal("HasClassifierForRuntime(qwen) is true, so a qwen failure could suppress a group; no qwen classifier exists")
	}

	// The only two ways a Classification can reach Observe are the shipped
	// classifiers, and neither of them will ever be handed Qwen output. Prove the
	// managed arm cannot manufacture quota evidence for an unclassified provider
	// either: without a classifier there is no call site, and the two shipped
	// classifiers are provider-fixed.
	if got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("ERROR: qwen quota exhausted\n")}).Class; got != ClassNotLimit {
		t.Fatalf("qwen-shaped output classified as %q through the codex classifier", got)
	}
	if got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429}`)}).Provider; got != ProviderClaude {
		t.Fatalf("the claude classifier reported provider %q; classifiers are provider-fixed", got)
	}
}

func TestUnmappedModelOfAClassifiedProviderGetsItsOwnPool(t *testing.T) {
	table := MustGroupTable()
	ref := table.Lookup(ProviderCodex, "gpt-6.0-unreleased")
	if !ref.Unmapped {
		t.Fatal("a model registered after the table was written must not inherit another pool's suppression")
	}
	if want := "codex-unmapped:gpt-6.0-unreleased"; ref.Group != want {
		t.Fatalf("group = %q, want %q", ref.Group, want)
	}
}

// Overrides replace whole rows. A merge would let a partial override silently
// keep members the operator meant to remove.
func TestGroupOverridesReplaceWholeRows(t *testing.T) {
	table, err := NewGroupTable([]GroupRow{
		{Group: "codex-plan", Provider: ProviderCodex, Members: []string{"gpt-5.6-sol"}, Provenance: ProvenanceOwnerAssertion},
	})
	if err != nil {
		t.Fatalf("NewGroupTable: %v", err)
	}
	if got := table.Group(ProviderCodex, "gpt-5.6-sol"); got != "codex-plan" {
		t.Errorf("sol group = %q, want codex-plan", got)
	}
	// gpt-5.6-luna was a default member of codex-plan. The override replaced the
	// row, so luna is no longer mapped at all rather than still pooled.
	ref := table.Lookup(ProviderCodex, "gpt-5.6-luna")
	if !ref.Unmapped {
		t.Fatalf("luna resolved to %q; a replaced row must not keep its old members", ref.Group)
	}
	// The rows the override did not name are untouched.
	if got := table.Group(ProviderCodex, "gpt-5.3-codex-spark"); got != "codex-spark" {
		t.Errorf("spark group = %q, want codex-spark", got)
	}
}

func TestGroupOverrideCanAddARow(t *testing.T) {
	table, err := NewGroupTable([]GroupRow{
		{Group: "codex-experimental", Provider: ProviderCodex, Members: []string{"gpt-6.0-preview"}},
	})
	if err != nil {
		t.Fatalf("NewGroupTable: %v", err)
	}
	if got := table.Group(ProviderCodex, "gpt-6.0-preview"); got != "codex-experimental" {
		t.Fatalf("group = %q, want codex-experimental", got)
	}
}

func TestDuplicateMembershipInAnOverrideIsAConfigError(t *testing.T) {
	_, err := NewGroupTable([]GroupRow{
		{Group: "codex-spark", Provider: ProviderCodex, Members: []string{"gpt-5.6-sol"}},
	})
	if err == nil {
		t.Fatal("moving a model into a second group without replacing the first must be rejected")
	}
	if !strings.Contains(err.Error(), "exactly one group") {
		t.Errorf("error = %q, want it to name the one-group rule", err)
	}
}

func TestGroupOverrideValidation(t *testing.T) {
	cases := []struct {
		name string
		rows []GroupRow
	}{
		{"empty group name", []GroupRow{{Group: "  ", Provider: ProviderCodex}}},
		{"no provider", []GroupRow{{Group: "codex-plan", Provider: ""}}},
		{"empty member", []GroupRow{{Group: "codex-plan", Provider: ProviderCodex, Members: []string{""}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewGroupTable(tc.rows); err == nil {
				t.Fatal("want a validation error")
			}
		})
	}
}

// The generic managed classifier is the ONE classifier that takes its provider as
// an argument, so it is the one place where an unsupported provider could mint
// quota evidence. This drives the whole production chain a call site would use —
// ClassifyManaged, QuotaObservation, Store.Observe — for Qwen, and proves nothing
// is written at any link.
//
// The test fails on the reviewed implementation, where ClassifyManaged returned
// provider_quota for any provider whatsoever.
func TestQwenCannotBeSuppressedThroughTheManagedClassifier(t *testing.T) {
	f := newStoreFixture(t)
	identity, err := IdentityFor("qwen", filepath.Join(f.root, "qwen-home"))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	group := UnmappedGroup("qwen", "qwen3-max")

	class := ClassifyManaged("qwen", ManagedSignalUsageLimited)
	if class.Class != ClassNotLimit {
		t.Fatalf("ClassifyManaged(qwen, usage_limited) = %q; a provider with no classifier yields no limit", class.Class)
	}
	if class.IsProviderQuota() || class.IsRunBudget() {
		t.Fatal("an unclassifiable provider must carry neither limit class")
	}
	if obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260802-00qw01", Model: "qwen3-max"}); ok {
		t.Fatalf("quota evidence was minted for a provider with no classifier: %+v", obs)
	}
	// The minting gate is independent of how the classification was produced: even
	// a classification that already carries the quota class cannot mint evidence
	// for a provider this module cannot classify.
	forgedClass := Classification{Class: ClassProviderQuota, Provider: "qwen", Marker: "hand-built"}
	if obs, ok := forgedClass.QuotaObservation(Evidence{RunID: "RUN-260802-00qw04"}); ok {
		t.Fatalf("QuotaObservation minted evidence for an unclassifiable provider: %+v", obs)
	}
	// Narrowing: the same shape for an evidenced provider still mints.
	if _, ok := (Classification{Class: ClassProviderQuota, Provider: ProviderCodex}).QuotaObservation(Evidence{RunID: "RUN-260802-00qw05"}); !ok {
		t.Fatal("the minting gate rejected an evidenced provider")
	}

	// The store boundary refuses the same evidence independently, so a future call
	// site cannot route it in by constructing the observation another way.
	forged := Observation{quota: true, Provider: "qwen", Evidence: Evidence{RunID: "RUN-260802-00qw02"}}
	if err := f.store.Observe(identity, group, nil, forged); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("Observe(qwen) = %v, want ErrUnsupportedProvider", err)
	}
	// So does the identity arm: real codex evidence written against a qwen home.
	codexClass := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("ERROR: You've hit your usage limit.\n")})
	codexObs, ok := codexClass.QuotaObservation(Evidence{RunID: "RUN-260802-00qw03"})
	if !ok {
		t.Fatal("the codex fixture must yield quota evidence")
	}
	if err := f.store.Observe(identity, group, nil, codexObs); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("Observe against a qwen identity = %v, want ErrUnsupportedProvider", err)
	}

	if _, exists := f.store.GroupRecordFor(identity, group); exists {
		t.Fatal("a qwen group must hold no record; nothing can suppress a provider with no classifier")
	}
	if _, err := os.Stat(f.store.Layout().StateFile(identity.Key)); !os.IsNotExist(err) {
		t.Fatalf("a qwen identity state file was written: %v", err)
	}
	// And the group still reads available to every caller, unconditionally.
	leased, unleased, _ := f.claimConcurrentlyFor(t, identity, group, 20)
	if leased != 0 || unleased != 20 {
		t.Fatalf("qwen claims: %d leased / %d unleased, want 0/20 — an unsuppressible group is never claim-gated", leased, unleased)
	}
}

// The gate is proven by narrowing, not only by deletion: the two evidenced
// providers still classify through exactly the same call.
func TestManagedClassifierStillClassifiesTheEvidencedProviders(t *testing.T) {
	for _, provider := range []string{ProviderCodex, ProviderClaude} {
		t.Run(provider, func(t *testing.T) {
			quota := ClassifyManaged(provider, ManagedSignalUsageLimited)
			if quota.Class != ClassProviderQuota {
				t.Fatalf("usage_limited = %q, want provider_quota", quota.Class)
			}
			if _, ok := quota.QuotaObservation(Evidence{RunID: "RUN-260802-00mg01"}); !ok {
				t.Fatal("an evidenced provider's usage_limited must mint quota evidence")
			}
			budget := ClassifyManaged(provider, ManagedSignalBudgetLimited)
			if budget.Class != ClassRunBudget {
				t.Fatalf("budget_limited = %q, want run_budget", budget.Class)
			}
		})
	}
	for _, provider := range []string{"qwen", "gemini", "openai", ""} {
		if got := ClassifyManaged(provider, ManagedSignalUsageLimited).Class; got != ClassNotLimit {
			t.Errorf("ClassifyManaged(%q, usage_limited) = %q, want no limit", provider, got)
		}
		if got := ClassifyManaged(provider, ManagedSignalBudgetLimited).Class; got != ClassNotLimit {
			t.Errorf("ClassifyManaged(%q, budget_limited) = %q, want no limit", provider, got)
		}
	}
}
