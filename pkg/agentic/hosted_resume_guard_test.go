package agentic_test

import (
	"go/ast"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/refusalscan"
)

func hostedResumeCoverageRows() []curatorRefusalCoverageRow {
	var rows []curatorRefusalCoverageRow
	for _, row := range refusalscan.HostedResumeCoverage() {
		site := row.Site
		rows = append(rows, curatorRefusalCoverageRow{file: site.File, function: site.Function, guard: site.Guard, returned: site.Return, occurrence: site.Occurrence, testName: row.TestName, entryPoint: row.EntryPoint})
	}
	return rows
}

func testReachesHostedEntryPoint(testName, entry string, functions map[string]*ast.FuncDecl) bool {
	function := functions[testName]
	if function == nil {
		return false
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == entry {
			found = true
		}
		return true
	})
	return found
}

func TestHostedResumeRefusalMapRejectsMissingAndStaleRows(t *testing.T) {
	rows := hostedResumeCoverageRows()
	for _, row := range rows {
		if row.testName == "" || row.entryPoint == "" {
			t.Fatal("unbound hosted refusal")
		}
	}
	sites := []refusalscan.Site{rowsToSite(rows[0])}
	if len(unmappedCuratorRefusalSites(sites, nil)) != 1 {
		t.Fatal("missing hosted mapping admitted")
	}
	stale := rows[0]
	stale.returned += " altered"
	if _, ok := resolveCuratorRefusalMapping(stale, sites); ok {
		t.Fatal("stale hosted mapping admitted")
	}
}
func rowsToSite(row curatorRefusalCoverageRow) refusalscan.Site {
	return refusalscan.Site{File: row.file, Function: row.function, Guard: row.guard, Return: row.returned, Occurrence: row.occurrence}
}
