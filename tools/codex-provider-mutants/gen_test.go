package main

import (
	"github.com/relux-works/skill-agents-management/internal/refusalscan"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDiscoverMutationsCoversEveryTypedRefusalSource(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := refusalscan.LocalCodexSites(root)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := discoverMutations(root)
	if err != nil {
		t.Fatal(err)
	}
	var members []refusalscan.Member
	propagated := false
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Site.File != "" {
			members = append(members, row.Member)
		}
		if row.Site.File != "" {
			seen[row.Site.Key()] = true
		}
		if row.Site.Function == "resolveLocalCatalog" && row.Site.Return == "return resolvedCatalog{}, err" {
			propagated = true
		}
		if row.Replacement != "" && !strings.Contains(row.Replacement, row.Expression) {
			t.Fatal("mutant lost the inspected token")
		}
	}
	if !propagated || len(seen) != len(sites) {
		t.Fatalf("census lost propagation or site: %d/%d", len(seen), len(sites))
	}
	if err := refusalscan.CheckMemberCompleteness(root, sites, members); err != nil {
		t.Fatal(err)
	}
	if err := refusalscan.CheckMemberCompleteness(root, sites, members[1:]); err == nil {
		t.Fatal("missing mutation mapping accepted")
	}
	t.Logf("semantic local census: %d sites, %d AST members", len(sites), len(rows))
}

func TestCapturedOutputIsBounded(t *testing.T) {
	output := &cappedOutput{}
	data := make([]byte, maxCapturedOutput+1)
	if n, err := output.Write(data); n != len(data) || err != nil || !output.truncated || len(output.bytes) != maxCapturedOutput {
		t.Fatal("captured output cap failed")
	}
}

func TestLexicalMutantsIncludeRemovalAndNarrowing(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := discoverMutations(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if strings.HasPrefix(row.ID, "parse/") && strings.Contains(row.ID, "string-lexical") || row.ID == "parse/admit-high-surrogate-lower-member" {
			if row.Replacement == "" || !strings.Contains(row.Replacement, row.Expression) {
				t.Fatal("lexical mutant must preserve the inspected expression")
			}
			seen[row.ID] = true
		}
	}
	for _, id := range []string{"parse/drop-string-lexical-check", "parse/admit-high-surrogate-lower-member"} {
		if !seen[id] {
			t.Fatal("missing AST-derived lexical mutant", id)
		}
	}
}

func TestDeclaredVocabularySubsetHasNarrowingMember(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := discoverMutations(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Site.Function == "checkLocalEffortVocabulary" && row.Expression == "!slices.Contains(native, word)" {
			if row.Replacement != "("+row.Expression+") && word != \"max\"" || row.Bound != "" {
				t.Fatalf("subset member lacks narrowing: %+v", row)
			}
			return
		}
	}
	t.Fatal("subset predicate absent from semantic mutant census")
}

func TestBehaviorPatternIncludesDeclaredVocabularyNegative(t *testing.T) {
	matched, err := regexp.MatchString(behaviorPattern, "TestLocalDeclaredVocabularyMustFitNativeCatalog")
	if err != nil || !matched {
		t.Fatalf("behavioral mask excludes the subset gate negative: matched=%v err=%v", matched, err)
	}
}
