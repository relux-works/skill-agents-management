package main

import (
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/refusalscan"
)

func TestHostedResumeCensusAndNamedNarrowingMutants(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := refusalscan.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	mapped := map[string]bool{}
	for _, row := range refusalscan.HostedResumeCoverage() {
		matched := 0
		for _, site := range sites {
			if site.Key() == row.Site.Key() {
				matched++
			}
		}
		if matched != 1 {
			t.Fatalf("hosted census stale: %s", row.Site.Key())
		}
		mapped[row.Site.Key()] = true
	}
	count := 0
	for _, site := range sites {
		switch site.File {
		case "resume.go", "systems/claude/resume.go", "systems/claude/restart.go", "systems/codex/resume.go", "systems/muse/resume.go":
			count++
			if !mapped[site.Key()] {
				t.Fatalf("hosted census missing site: %s", site.Key())
			}
		}
	}
	if count != len(mapped) {
		t.Fatal("hosted census contains duplicate rows")
	}
	for _, candidate := range hostedResumeMutants() {
		if candidate.testName == "" || candidate.runPattern == "" || candidate.narrows == "" || len(candidate.replacements) != 1 {
			t.Fatal("mutant lacks behavioral binding")
		}
	}
	t.Logf("hosted refusal census: %d of %d mapped; %d named narrowing mutants", count, count, len(hostedResumeMutants()))
}
