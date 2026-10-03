package refusalscan

import (
	"path/filepath"
	"testing"
)

func TestDiscoverTypeIncludesPropagationWithArbitraryHelperName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.test/local\n\ngo 1.25.5\n")
	writeFile(t, root, "pkg/agentic/guard.go", `package agentic
 type CatalogError struct{}
 func (*CatalogError) Error()string{return "refused"}
 func renamed(a,b bool)error{if a||b{return &CatalogError{}};return nil}
 func Entry()error{if err:=renamed(true,false);err!=nil{return err};return nil}
 `)
	sites, err := DiscoverType(root, "example.test/local/pkg/agentic", "CatalogError")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 || sites[0].Function != "renamed" || sites[1].Function != "Entry" {
		t.Fatalf("semantic propagation census: %v", sites)
	}
	members, err := Members(root, sites)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckMemberCompleteness(root, sites, members); err != nil {
		t.Fatal(err)
	}
	if err := CheckMemberCompleteness(root, sites, members[1:]); err == nil {
		t.Fatal("missing member silently accepted")
	}
	if err := CheckMemberCompleteness(root, sites, members[:2]); err == nil {
		t.Fatal("missing propagated site silently accepted")
	}
}

func TestLocalCodexSemanticCensusIncludesCurrentPropagation(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := LocalCodexSites(root)
	if err != nil {
		t.Fatal(err)
	}
	propagated := 0
	for _, site := range sites {
		if site.Return == "return resolvedCatalog{}, err" {
			propagated++
		}
	}
	if propagated != 2 {
		t.Fatalf("catalog propagation  %d, want 2", propagated)
	}
	members, err := Members(root, sites)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckMemberCompleteness(root, sites, members); err != nil {
		t.Fatal(err)
	}
}
