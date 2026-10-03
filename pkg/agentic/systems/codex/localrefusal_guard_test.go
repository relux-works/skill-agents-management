package codex

import (
	"fmt"
	"github.com/relux-works/skill-agents-management/internal/refusalscan"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/gosources"
)

// localRefusalCoverageRow maps one typed LocalProviderRefusal return site to
// the negative test that drives it. The guard below fails closed when a site
// is added, removed or moved without updating its mapping.
type localRefusalCoverageRow struct {
	file          string
	function      string
	guard         string
	returned      string
	occurrence    int
	testName      string
	outOfContract string
}

// localRefusalCoverageTable covers every typed refusal return in this
// package's non-test sources. Guard and returned strings are matched after
// collapsing whitespace, so reformatting alone never breaks the mapping;
// occurrence disambiguates identical guards in one function in source order.
var localRefusalCoverageTable = []localRefusalCoverageRow{
	{file: "pkg/agentic/systems/codex/args.go", function: "Args", guard: "if err != nil", returned: "return nil, err", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/args.go", function: "Args", guard: "if err != nil", returned: "return nil, err", occurrence: 1, testName: "TestManagedInventoryReadsTheChildEnv/conflicting CODEX_HOME keeps provider precedence"},
	{file: "pkg/agentic/systems/codex/network.go", function: "managedFileServerEnvPairs", guard: "if err != nil", returned: "return nil, err", occurrence: 0, testName: "TestManagedInventoryReadsTheChildEnv/conflicting CODEX_HOME keeps provider precedence"},
	{file: "pkg/agentic/systems/codex/network.go", function: "managedChildEnv", guard: "if err != nil", returned: "return nil, err", occurrence: 0, testName: "TestManagedInventoryReadsTheChildEnv/conflicting CODEX_HOME keeps provider precedence"},
	{file: "pkg/agentic/systems/codex/network.go", function: "resolveManagedConfigRoot", guard: "if err != nil", returned: "return \"\", err", occurrence: 0, testName: "TestManagedInventoryReadsTheChildEnv/conflicting CODEX_HOME keeps provider precedence"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "resolveLocalCatalog", guard: "if !found", returned: "return resolvedCatalog{}, localProviderRefusal(agentic.LocalProviderAbsent, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesWhenNativeMetadataAbsent"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "resolveLocalCatalog", guard: "if !ok || pathValue != strings.TrimSpace(pathValue) || strings.TrimSpace(pathValue) == \"\"", returned: "return resolvedCatalog{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "resolveLocalCatalog", guard: "if !ok", returned: "return resolvedCatalog{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "resolveLocalCatalog", guard: "if err != nil", returned: "return resolvedCatalog{}, localCatalogRefusal(kind, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesWhenNativeMetadataAbsent"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "resolveLocalCatalog", guard: "if !ok", returned: "return resolvedCatalog{}, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)", occurrence: 0, testName: "TestLocalCatalogNativeInvalidWholeCatalogRefuses"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "resolveLocalCatalog", guard: "if err != nil", returned: "return resolvedCatalog{}, err", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "resolveLocalCatalog", guard: "if err != nil", returned: "return resolvedCatalog{}, err", occurrence: 1, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "selectCatalogEntry", guard: "if matches == 0", returned: "return codexCatalogModel{}, localCatalogRefusal(agentic.LocalProviderAbsent, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesWhenNativeMetadataAbsent"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "selectCatalogEntry", guard: "if matches > 1", returned: "return codexCatalogModel{}, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "validateCatalogEntry", guard: "if entry.UseResponsesLite == nil || entry.SupportsSearchTool == nil || entry.SupportsReasoningSummaryParameter == nil", returned: "return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "validateCatalogEntry", guard: "if strings.TrimSpace(entry.ToolMode) == \"\" || strings.TrimSpace(entry.MultiAgentVersion) == \"\"", returned: "return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "validateCatalogEntry", guard: "if *entry.UseResponsesLite || *entry.SupportsSearchTool || *entry.SupportsReasoningSummaryParameter", returned: "return nil, localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesHostedShapedMetadata"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "validateCatalogEntry", guard: "if entry.ToolMode != catalogToolMode || entry.MultiAgentVersion != catalogMultiAgentVersion", returned: "return nil, localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesHostedShapedMetadata"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "validateCatalogEntry", guard: "if len(entry.SupportedReasoningLevels) == 0", returned: "return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "validateCatalogEntry", guard: "if strings.TrimSpace(level.Effort) == \"\"", returned: "return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "validateCatalogEntry", guard: "if seen[level.Effort]", returned: "return nil, localCatalogRefusal(agentic.LocalProviderMalformed, catalogPath, providerID)", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "checkLocalEffort", guard: "if trimmed == \"\"", returned: "return localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)", occurrence: 0, testName: "TestIDPlanBindsBytesAndRefusesArtifactTamperBeforeExec"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "checkLocalEffort", guard: "unconditional", returned: "return localCatalogRefusal(agentic.LocalProviderUnsupported, catalogPath, providerID)", occurrence: 0, testName: "TestLocalEffortOutOfVocabularyRefuses"},
	{file: "pkg/agentic/systems/codex/catalog.go", function: "localCatalogRefusal", guard: "unconditional", returned: "return &agentic.LocalProviderRefusal{Kind: kind, File: filepath.Base(catalogPath), Subject: subject}", occurrence: 0, testName: "TestLocalLaunchRefusesMalformedCatalog"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "materializeCatalog", guard: "if err != nil", returned: "return \"\", localCatalogRefusal(agentic.LocalProviderReadFailed, \"catalog.json\", providerID)", occurrence: 0, testName: "TestLocalCatalogArtifactIORefusals"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "materializeCatalog", guard: "if writeErr != nil || closeErr != nil", returned: "return \"\", localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)", occurrence: 0, testName: "TestLocalCatalogArtifactIORefusals"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "materializeCatalog", guard: "if !os.IsExist(err)", returned: "return \"\", localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)", occurrence: 0, testName: "TestLocalCatalogArtifactIORefusals"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "materializeCatalog", guard: "if err != nil", returned: "return \"\", err", occurrence: 0, testName: "TestLocalCatalogArtifactIORefusals"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "verifyLaunchCatalog", guard: "if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400", returned: "return localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)", occurrence: 0, testName: "TestIDPlanBindsBytesAndRefusesArtifactTamperBeforeExec"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "verifyLaunchCatalog", guard: "if err != nil || !dir.IsDir() || dir.Mode().Perm() != 0o700", returned: "return localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)", occurrence: 0, testName: "TestIDPlanBindsBytesAndRefusesArtifactTamperBeforeExec"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "verifyLaunchCatalog", guard: "if err != nil", returned: "return localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)", occurrence: 0, testName: "TestIDPlanBindsBytesAndRefusesArtifactTamperBeforeExec"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "verifyLaunchCatalog", guard: "if fmt.Sprintf(\"%x\", sha256.Sum256(data)) != digest", returned: "return localCatalogRefusal(agentic.LocalProviderConflicting, path, providerID)", occurrence: 0, testName: "TestIDPlanBindsBytesAndRefusesArtifactTamperBeforeExec"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "catalogExecSeal.VerifyBeforeExec", guard: "if plan.Binary != seal.binary || !slices.Equal(plan.Argv, seal.argv)", returned: "return localCatalogRefusal(agentic.LocalProviderConflicting, seal.path, seal.providerID)", occurrence: 0, testName: "TestCatalogExecSealRefusesChangedArgvAndPermissions"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "catalogExecSeal.VerifyBeforeExec", guard: "unconditional", returned: "return verifyLaunchCatalog(seal.path, seal.digest, seal.providerID)", occurrence: 0, testName: "TestIDPlanBindsBytesAndRefusesArtifactTamperBeforeExec"},
	{file: "pkg/agentic/systems/codex/catalog_launch.go", function: "System.SealExecPlan", guard: "if err != nil", returned: "return nil, localCatalogRefusal(agentic.LocalProviderMalformed, \"catalog.json\", \"\")", occurrence: 0, testName: "TestCatalogExecSealRefusesMalformedPin"},
	{file: "pkg/agentic/systems/codex/codex.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/codex.go", function: "System.ChildEnv", guard: "if err != nil", returned: "return nil, err", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if strings.TrimSpace(providerID) == \"\"", returned: "return nil, &agentic.LocalProviderRefusal{Kind: agentic.LocalProviderUnbound}", occurrence: 0, testName: "TestPluginArgvRefusesAnEmptyLocalProviderBindingDirectly"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if providerID != strings.TrimSpace(providerID)", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, \"provider id\")", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if mode != agentic.LaunchModeExec && mode != agentic.LaunchModeDryRun", returned: "return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderForInteractiveMode"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if !strings.HasPrefix(providerID, \"local-\")", returned: "return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if !providerIDPattern.MatchString(providerID)", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, \"provider id\")", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if strings.TrimSpace(req.Model.ID) == \"\"", returned: "return nil, localProviderRefusal(agentic.LocalProviderUnbound, providerID)", occurrence: 0, testName: "TestLocalProviderEmptyModelRefusesDirectly"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, err", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if !ok || !snapshot.valid()", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestSnapshotBindingRefusesForgedSnapshots"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if snapshot.providerID != providerID", returned: "return nil, localProviderRefusal(agentic.LocalProviderConflicting, providerID)", occurrence: 0, testName: "TestSnapshotBindingRefusesForgedSnapshots"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if snapshot.modelSlug != strings.TrimSpace(req.Model.ID)", returned: "return nil, localCatalogRefusal(agentic.LocalProviderConflicting, snapshot.catalogPath, providerID)", occurrence: 0, testName: "TestSnapshotModelMismatchRefuses"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, err", occurrence: 1, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, err", occurrence: 2, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, localProviderRefusal(kind, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil || config == nil", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, err", occurrence: 3, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, err", occurrence: 4, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, err", occurrence: 5, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, err", occurrence: 6, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderHome", guard: "if !ok", returned: "return \"\", localProviderRefusal(agentic.LocalProviderUnbound, \"provider home\")", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderHome", guard: "if !resolvedOK || resolved != root", returned: "return \"\", localProviderRefusal(agentic.LocalProviderConflicting, \"provider home\")", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnbound, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !found", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnbound, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok || strings.TrimSpace(name) == \"\" || strings.TrimSpace(name) != name || strings.IndexFunc(name, unicode.IsControl) >= 0", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok || strings.TrimSpace(baseURL) != baseURL", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if errors.Is(err, errMalformedProviderURL)", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if err != nil", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 1, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if wireAPI != \"responses\"", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !found", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 2, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if requires", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if found", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/provider.go", function: "localProviderRefusal", guard: "unconditional", returned: "return &agentic.LocalProviderRefusal{Kind: kind, File: \"config.toml\", Subject: subject}", occurrence: 0, testName: "TestBuildPlanRefusesLocalProviderBindingAndTransportFailures"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if req.LocalProvider == nil || strings.TrimSpace(req.LocalProvider.ID) == \"\"", returned: "return nil, &agentic.LocalProviderRefusal{Kind: agentic.LocalProviderUnbound}", occurrence: 0, testName: "TestReadProviderSnapshotRefusesAnEmptyBinding"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if providerID != strings.TrimSpace(providerID)", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, \"provider id\")", occurrence: 0, testName: "TestReadProviderSnapshotRefusalParity"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if !strings.HasPrefix(providerID, \"local-\")", returned: "return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, testName: "TestReadProviderSnapshotRefusalParity"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if !providerIDPattern.MatchString(providerID)", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, \"provider id\")", occurrence: 0, testName: "TestReadProviderSnapshotRefusalParity"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if strings.TrimSpace(req.Model.ID) == \"\"", returned: "return nil, localProviderRefusal(agentic.LocalProviderUnbound, providerID)", occurrence: 0, testName: "TestLocalProviderEmptyModelRefusesDirectly"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if err != nil", returned: "return nil, err", occurrence: 0, testName: "TestReadProviderSnapshotRefusalParity"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if err != nil", returned: "return nil, localProviderRefusal(kind, providerID)", occurrence: 0, testName: "TestReadProviderSnapshotRefusalParity"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if err != nil || config == nil", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, testName: "TestReadProviderSnapshotRefusalParity"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if err != nil", returned: "return nil, err", occurrence: 1, testName: "TestReadProviderSnapshotRefusalParity"},
	{file: "pkg/agentic/systems/codex/snapshot.go", function: "ReadProviderSnapshot", guard: "if err != nil", returned: "return nil, err", occurrence: 2, testName: "TestReadProviderSnapshotRefusalParity"},
}

type localRefusalSite struct {
	file     string
	function string
	guard    string
	returned string
	line     int
}

func normalizeRefusalText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// discoverLocalRefusalSites walks every non-test Go file under
// pkg/agentic/systems/codex (discovered via the module walker, never
// hand-listed) and returns every return statement that yields a typed
// local-provider refusal.
func discoverLocalRefusalSites(t *testing.T) []localRefusalSite {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := gosources.Root(dir)
	if err != nil {
		t.Fatal(err)
	}
	found, err := refusalscan.DiscoverType(root, "github.com/relux-works/skill-agents-management/pkg/agentic", "LocalProviderRefusal")
	if err != nil {
		t.Fatal(err)
	}
	var sites []localRefusalSite
	for _, site := range found {
		if strings.HasPrefix(site.File, "systems/codex/") {
			sites = append(sites, localRefusalSite{file: "pkg/agentic/" + site.File, function: site.Function, guard: site.Guard, returned: site.Return, line: site.Line})
		}
	}
	return sites
}

// TestLocalProviderRefusalSitesHaveNamedNegativeCoverage fails when any typed
// refusal return under pkg/agentic/systems/codex has no mapped negative test,
// or when a mapping is stale.
func TestLocalProviderRefusalSitesHaveNamedNegativeCoverage(t *testing.T) {
	sites := discoverLocalRefusalSites(t)
	for _, site := range unmappedLocalSites(sites, localRefusalCoverageTable) {
		t.Errorf("unmapped typed refusal: %v", site)
	}
	if len(sites) == 0 {
		t.Fatal("discovered zero typed refusal sites; the scanner is blind, not clean")
	}
	// Index sites by (file, function, guard, returned) with occurrence order.
	type key struct {
		file, function, guard, returned string
		occurrence                      int
	}
	counts := map[string]int{}
	siteKeys := make([]key, len(sites))
	for i, site := range sites {
		flat := site.file + "\x00" + site.function + "\x00" + site.guard + "\x00" + site.returned
		occurrence := counts[flat]
		counts[flat]++
		siteKeys[i] = key{file: site.file, function: site.function, guard: site.guard, returned: site.returned, occurrence: occurrence}
	}
	covered := map[key]bool{}
	for _, row := range localRefusalCoverageTable {
		k := key{file: row.file, function: row.function, guard: normalizeRefusalText(row.guard), returned: normalizeRefusalText(row.returned), occurrence: row.occurrence}
		if covered[k] {
			t.Errorf("duplicate coverage mapping for %s :: %s :: %s (occurrence %d)", row.file, row.function, row.guard, row.occurrence)
		}
		covered[k] = true
		if row.outOfContract == "" && strings.TrimSpace(row.testName) == "" {
			t.Errorf("coverage row for %s :: %s :: %s has no test and no out-of-contract clause", row.file, row.function, row.guard)
		}
	}
	// Every mapping must resolve to a discovered site (no stale rows).
	discovered := map[key]bool{}
	for _, k := range siteKeys {
		discovered[k] = true
	}
	for _, row := range localRefusalCoverageTable {
		k := key{file: row.file, function: row.function, guard: normalizeRefusalText(row.guard), returned: normalizeRefusalText(row.returned), occurrence: row.occurrence}
		if !discovered[k] {
			t.Errorf("stale coverage mapping (no such site): %s :: %s :: %s :: %s (occurrence %d)", row.file, row.function, row.guard, row.returned, row.occurrence)
		}
	}
	// Every named test must exist in this package's test sources.
	testFuncs := discoverCodexTestFunctions(t)
	for _, row := range localRefusalCoverageTable {
		if row.outOfContract != "" || row.testName == "" {
			continue
		}
		base := strings.SplitN(row.testName, "/", 2)[0]
		if !testFuncs[base] {
			t.Errorf("mapped test %q for %s :: %s does not exist in package codex", row.testName, row.file, row.function)
		}
	}
	mapped := 0
	for _, row := range localRefusalCoverageTable {
		if row.testName != "" {
			mapped++
		}
	}
	t.Logf("local-provider refusal coverage: %d of %d sites have named negative tests", mapped, len(sites))
}

func discoverCodexTestFunctions(t *testing.T) map[string]bool {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the working directory: %v", err)
	}
	root, err := gosources.Root(dir)
	if err != nil {
		t.Fatalf("locating the module root: %v", err)
	}
	pkgDir := root + "/pkg/agentic/systems/codex"
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("reading codex package: %v", err)
	}
	funcs := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(pkgDir + "/" + entry.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, entry.Name(), body, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			funcs[fn.Name.Name] = true
		}
	}
	return funcs
}

// TestLocalRefusalGuardSeesEveryCodexSource proves the scan reaches what it
// claims: a guard that silently scans zero files reports clean forever.
func TestLocalRefusalGuardSeesEveryCodexSource(t *testing.T) {
	sites := discoverLocalRefusalSites(t)
	files := map[string]bool{}
	for _, site := range sites {
		files[site.file] = true
	}
	for _, required := range []string{
		"pkg/agentic/systems/codex/catalog.go",
		"pkg/agentic/systems/codex/provider.go",
		"pkg/agentic/systems/codex/snapshot.go",
	} {
		if !files[required] {
			t.Errorf("the scan found no refusal sites in %s; a file the guard never reads is a file it never guards", required)
		}
	}
}

// TestLocalRefusalGuardFiresOnAnUnmappedSite narrows the gate: the same
// discovery with one mapping removed must report that site as unmapped.
func TestLocalRefusalGuardFiresOnAnUnmappedSite(t *testing.T) {
	sites := discoverLocalRefusalSites(t)
	if missing := unmappedLocalSites(sites, localRefusalCoverageTable); len(missing) != 0 {
		t.Fatalf("baseline uncovered: %v", missing)
	}
	missing := unmappedLocalSites(sites, localRefusalCoverageTable[1:])
	if len(missing) != 1 || missing[0].function != sites[0].function || missing[0].line != sites[0].line {
		t.Fatalf("dropped mapping was not refused: %v", missing)
	}
}

func unmappedLocalSites(sites []localRefusalSite, rows []localRefusalCoverageRow) []localRefusalSite {
	keys := map[string]bool{}
	for _, r := range rows {
		keys[fmt.Sprintf("%s:%s:%s:%s:%d", r.file, r.function, r.guard, r.returned, r.occurrence)] = true
	}
	counts := map[string]int{}
	var missing []localRefusalSite
	for _, s := range sites {
		key := fmt.Sprintf("%s:%s:%s:%s", s.file, s.function, s.guard, s.returned)
		i := counts[key]
		counts[key]++
		if !keys[fmt.Sprintf("%s:%d", key, i)] {
			missing = append(missing, s)
		}
	}
	return missing
}
