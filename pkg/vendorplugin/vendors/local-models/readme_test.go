package localmodels_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

// TestReadmeLocalModelsRequiredRecommendationIsMandatory is the D1
// (recommendation-optional-doc) regression test: it reads the documented
// local-models.toml example straight out of the repository README — no second
// fixture — and pins both sides of the mandatory rule through the public API.
// The example as documented must register; the same example with its
// recommended_effort line removed must refuse with ErrEffortDeclaration.
func TestReadmeLocalModelsRequiredRecommendationIsMandatory(t *testing.T) {
	example := readmeLocalModelsExample(t)

	cfg, err := localmodels.ParseConfig([]byte(example))
	if err != nil {
		t.Fatalf("ParseConfig(README example): %v", err)
	}
	if _, err := effortRegistry(t, cfg); err != nil {
		t.Fatalf("Register(README example): %v", err)
	}

	withoutRecommendation := stripRecommendedEffortLine(t, example)
	omittedCfg, err := localmodels.ParseConfig([]byte(withoutRecommendation))
	if err != nil {
		t.Fatalf("ParseConfig(README example without recommendation): %v", err)
	}
	if _, err := effortRegistry(t, omittedCfg); !errors.Is(err, vendorplugin.ErrEffortDeclaration) {
		t.Fatalf("Register(README example without recommendation) = %v, want ErrEffortDeclaration", err)
	}
}

// readmeLocalModelsExample extracts the TOML example under the
// "Local-model effort configuration" README section. It reads the working
// module's README only.
func readmeLocalModelsExample(t *testing.T) string {
	t.Helper()
	_, caller, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: no caller")
	}
	readmePath := filepath.Join(filepath.Dir(caller), "..", "..", "..", "..", "README.md")
	body, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	const header = "### Local-model effort configuration"
	at := strings.Index(string(body), header)
	if at < 0 {
		t.Fatalf("README lacks %q", header)
	}
	rest := string(body)[at:]
	const fence = "```toml"
	open := strings.Index(rest, fence)
	if open < 0 {
		t.Fatal("README local-models section lacks a toml fence")
	}
	code := rest[open+len(fence):]
	close := strings.Index(code, "```")
	if close < 0 {
		t.Fatal("README local-models toml fence never closes")
	}
	example := strings.TrimSpace(code[:close]) + "\n"
	for _, want := range []string{"effort_vocabulary", "recommended_effort"} {
		if !strings.Contains(example, want) {
			t.Fatalf("README example lacks %q", want)
		}
	}
	return example
}

// stripRecommendedEffortLine removes the recommended_effort assignment from a
// TOML document while keeping every other line, including comments.
func stripRecommendedEffortLine(t *testing.T, document string) string {
	t.Helper()
	var kept []string
	removed := 0
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "recommended_effort") {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed != 1 {
		t.Fatalf("removed %d recommended_effort lines, want 1", removed)
	}
	return strings.Join(kept, "\n")
}
