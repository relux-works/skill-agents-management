#!/usr/bin/env python3
from __future__ import annotations

import dataclasses
import pathlib
import shutil
import subprocess
import sys


ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT / ".temp" / "TASK-260830-1jpse1" / "mutants"


@dataclasses.dataclass(frozen=True)
class Mutation:
    name: str
    file: str
    old: str
    new: str
    package: str
    test: str


MUTATIONS = [
    Mutation(
        "plugin-nil-registry",
        "pkg/plugin/registry.go",
        'if r == nil {\n\t\treturn errors.New("plugin: cannot register into a nil registry")\n\t}',
        'if r == nil && len(plugins) == 0 {\n\t\treturn errors.New("plugin: cannot register into a nil registry")\n\t}',
        "./pkg/plugin",
        "TestNilRegistryRefusesRegistration",
    ),
    Mutation(
        "plugin-nil-resolution-registry",
        "pkg/plugin/registry.go",
        'if r == nil {\n\t\treturn Resolution{}, errors.New("plugin: cannot resolve from a nil registry")\n\t}',
        'if r == nil && id == "" {\n\t\treturn Resolution{}, errors.New("plugin: cannot resolve from a nil registry")\n\t}',
        "./pkg/plugin",
        "TestNilRegistryRefusesResolution",
    ),
    Mutation(
        "plugin-nil-plugin",
        "pkg/plugin/registry.go",
        "if nilPlugin(p) {",
        "if p == nil {",
        "./pkg/plugin",
        "TestRegisterRefusesTypedNilPlugin",
    ),
    Mutation(
        "plugin-invalid-declaration",
        "pkg/plugin/registry.go",
        "if string(dependency.ID) != depID || string(dependency.Kind) != depKind {",
        "if string(dependency.Kind) != depKind {",
        "./pkg/plugin",
        "TestRegisterAllRefusesUnnormalizedDependencyDeclaration",
    ),
    Mutation(
        "plugin-invalid-resolution-id",
        "pkg/plugin/registry.go",
        'normalized, err := normalize("plugin id", string(id))\n\tif err != nil {\n\t\treturn Resolution{}, err\n\t}\n\tr.mu.RLock()',
        'normalized, err := normalize("plugin id", string(id))\n\tif err != nil && id == "" {\n\t\treturn Resolution{}, err\n\t}\n\tr.mu.RLock()',
        "./pkg/plugin",
        "TestResolveRefusesInvalidPluginID",
    ),
    Mutation(
        "plugin-unstable-declaration",
        "pkg/plugin/registry.go",
        "if !reflect.DeepEqual(first, second) {",
        "if first.ID != second.ID || first.Kind != second.Kind {",
        "./pkg/plugin",
        "TestRegisterAllRefusesDependencyChangesAcrossDeclarationReads",
    ),
    Mutation(
        "plugin-duplicate-plugin",
        "pkg/plugin/registry.go",
        "if existing, found := candidate[registered.declaration.ID]; found {",
        "if existing, found := candidate[registered.declaration.ID]; found && existing.declaration.Kind != registered.declaration.Kind {",
        "./pkg/plugin",
        "TestRegisterRefusesSameKindDuplicate",
    ),
    Mutation(
        "plugin-duplicate-dependency",
        "pkg/plugin/registry.go",
        "if _, duplicate := seen[dependency.ID]; duplicate {",
        "if _, duplicate := seen[dependency.ID]; duplicate && dependency.ID == declaration.ID {",
        "./pkg/plugin",
        "TestRegisterAllRefusesRepeatedNonSelfDependency",
    ),
    Mutation(
        "plugin-missing-dependency",
        "pkg/plugin/registry.go",
        "for _, dependency := range declaration.Dependencies {\n\t\t\ttarget, found := graph[dependency.ID]",
        "for dependencyIndex, dependency := range declaration.Dependencies {\n\t\t\tif dependencyIndex > 0 {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\ttarget, found := graph[dependency.ID]",
        "./pkg/plugin",
        "TestRegisterRefusesALaterMissingDependencyInANonEmptyGraph",
    ),
    Mutation(
        "plugin-unsatisfiable-declaration",
        "pkg/plugin/registry.go",
        "if target.declaration.Kind != dependency.Kind {",
        'if target.declaration.Kind != dependency.Kind && target.declaration.Kind == "model-vendor" {',
        "./pkg/plugin",
        "TestRegisterAllRefusesALaterKindMismatchAgainstABatchNode",
    ),
    Mutation(
        "plugin-dependency-cycle",
        "pkg/plugin/registry.go",
        "case visiting:\n\t\t\tstart := 0",
        "case visiting:\n\t\t\tif len(stack) == 1 {\n\t\t\t\treturn nil\n\t\t\t}\n\t\t\tstart := 0",
        "./pkg/plugin",
        "TestRegisterAllRefusesSelfAndMultiHopCycles",
    ),
    Mutation(
        "plugin-not-registered",
        "pkg/plugin/registry.go",
        "if !found {\n\t\treturn Resolution{}, fmt.Errorf(\"%w: %q\", ErrPluginNotRegistered, normalized)\n\t}",
        "if !found && len(r.plugins) == 0 {\n\t\treturn Resolution{}, fmt.Errorf(\"%w: %q\", ErrPluginNotRegistered, normalized)\n\t}",
        "./pkg/plugin",
        "TestResolveRefusesMissingPluginInNonEmptyRegistry",
    ),
    Mutation(
        "plan-invalid-explicit-node",
        "pkg/agentic/multinode.go",
        'if strings.TrimSpace(raw.Process.Binary) == "" {',
        'if strings.TrimSpace(raw.Process.Binary) == "" && raw.ID == PrimaryPlanNodeID {',
        "./pkg/agentic",
        "TestBuildMultiNodePlanRefusesExplicitNodeWithEmptyBinary",
    ),
    Mutation(
        "plan-duplicate-node",
        "pkg/agentic/multinode.go",
        "if _, duplicate := byID[node.ID]; duplicate {",
        "if _, duplicate := byID[node.ID]; duplicate && node.ID == PrimaryPlanNodeID {",
        "./pkg/agentic",
        "TestBuildMultiNodePlanRefusesDuplicateExplicitAndPrimaryNodeIDs",
    ),
    Mutation(
        "plan-missing-dependency",
        "pkg/agentic/multinode.go",
        "for _, dependency := range byID[id].DependsOn {\n\t\t\tif _, found := byID[dependency]; !found {",
        "for dependencyIndex, dependency := range byID[id].DependsOn {\n\t\t\tif dependencyIndex > 0 {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tif _, found := byID[dependency]; !found {",
        "./pkg/agentic",
        "TestBuildMultiNodePlanRefusesMissingDependenciesFromPrimaryAndLaterEdges",
    ),
    Mutation(
        "plan-dependency-cycle",
        "pkg/agentic/multinode.go",
        "case visiting:\n\t\t\tstart := 0",
        "case visiting:\n\t\t\tif len(stack) > 0 && stack[len(stack)-1] == id {\n\t\t\t\treturn nil\n\t\t\t}\n\t\t\tstart := 0",
        "./pkg/agentic",
        "TestBuildMultiNodePlanRefusesASelfCycle",
    ),
    Mutation(
        "vendor-no-agentic-registry",
        "pkg/vendorplugin/registry.go",
        "if r.systems == nil {\n\t\treturn fmt.Errorf(\"%w: refusing every vendor rather than admitting one whose declared systems nobody checked\", ErrNoAgenticRegistry)\n\t}",
        "if false {\n\t\treturn fmt.Errorf(\"%w: refusing every vendor rather than admitting one whose declared systems nobody checked\", ErrNoAgenticRegistry)\n\t}",
        "./pkg/vendorplugin",
        "TestARegistryWithNoAgenticRegistryAdmitsNoVendor",
    ),
    Mutation(
        "vendor-unknown-system-late-model",
        "pkg/vendorplugin/registry.go",
        "for _, model := range models {\n\t\tif err := model.Validate(); err != nil {",
        "for modelIndex, model := range models {\n\t\tif err := model.Validate(); err != nil {",
        "./pkg/vendorplugin",
        "TestTheDependencyDirectionIsCheckedForEveryModel",
    ),
    Mutation(
        "vendor-shadow-sync",
        "pkg/vendorplugin/registry.go",
        "if !ok || !reflect.DeepEqual(existing, sourceDeclaration) {",
        "if !ok || (reflect.DeepEqual(existing.Kind, sourceDeclaration.Kind) && len(existing.Dependencies) != len(sourceDeclaration.Dependencies)) {",
        "./pkg/vendorplugin",
        "TestVendorRegistrationRefusesShadowDeclarationsWithEqualWidthButDifferentData",
    ),
    Mutation(
        "vendor-plugin-id-collision",
        "pkg/plugin/registry.go",
        "if existing, found := candidate[registered.declaration.ID]; found {",
        "if existing, found := candidate[registered.declaration.ID]; found && existing.declaration.Kind == registered.declaration.Kind {",
        "./pkg/vendorplugin",
        "TestVendorRegistrationRefusesVendorIDCollidingWithSyncedPluginKind",
    ),
    Mutation(
        "vendor-nil-registry",
        "pkg/vendorplugin/registry.go",
        'if r == nil {\n\t\treturn errors.New("vendorplugin: cannot register into a nil registry")\n\t}',
        'if false {\n\t\treturn errors.New("vendorplugin: cannot register into a nil registry")\n\t}',
        "./pkg/vendorplugin",
        "TestNilRegistryRefusesVendorRegistration",
    ),
    Mutation(
        "vendor-nil-vendor",
        "pkg/vendorplugin/registry.go",
        "if nilVendor(vendor) {",
        "if vendor == nil {",
        "./pkg/vendorplugin",
        "TestRegisterRefusesTypedNilVendor",
    ),
    Mutation(
        "vendor-unstable-id",
        "pkg/vendorplugin/registry.go",
        "if again := vendor.ID(); again != raw {",
        'if again := vendor.ID(); again != raw && raw == "" {',
        "./pkg/vendorplugin",
        "TestRegisterRefusesAnUnstableVendorID",
    ),
    Mutation(
        "vendor-invalid-id-type",
        "pkg/vendorplugin/registry.go",
        'return fmt.Errorf("vendorplugin: registering vendor: %w", err)',
        'return fmt.Errorf("vendorplugin: registering vendor: %v", err)',
        "./pkg/vendorplugin",
        "TestRegisterRefusesAnUnusableVendorID",
    ),
    Mutation(
        "vendor-unnormalized-id",
        "pkg/vendorplugin/registry.go",
        "if raw != id {",
        'if raw != id && raw == " narwhal " {',
        "./pkg/vendorplugin",
        "TestRegisterRefusesAnUnnormalizedVendorID",
    ),
    Mutation(
        "vendor-no-models",
        "pkg/vendorplugin/registry.go",
        "if len(models) == 0 {",
        "if models == nil {",
        "./pkg/vendorplugin",
        "TestRegisterRefusesUnusableModelRows",
    ),
    Mutation(
        "vendor-duplicate-model",
        "pkg/vendorplugin/registry.go",
        "if seenModels[model.ID] {",
        "if seenModels[model.ID] && modelIndex == 1 {",
        "./pkg/vendorplugin",
        "TestRegisterRefusesUnusableModelRows",
    ),
    Mutation(
        "vendor-invalid-model-id",
        "pkg/vendorplugin/vendor.go",
        'if strings.TrimSpace(value) != value || strings.ContainsAny(value, " \\t\\r\\n") {',
        "if strings.TrimSpace(value) != value {",
        "./pkg/vendorplugin",
        "TestRegisterRefusesUnusableModelRows",
    ),
    Mutation(
        "vendor-description-empty",
        "pkg/vendorplugin/vendor.go",
        'if strings.TrimSpace(string(d)) == "" {',
        'if string(d) == "" {',
        "./pkg/vendorplugin",
        "TestRegisterRefusesAModelWithNoUsageDescription",
    ),
    Mutation(
        "vendor-rank-invalid",
        "pkg/vendorplugin/vendor.go",
        'if strings.TrimSpace(evidence.Source) == "" {',
        'if evidence.Source == "" {',
        "./pkg/vendorplugin",
        "TestRegisterRefusesARankWithNoEvidence",
    ),
    Mutation(
        "vendor-lifecycle-invalid",
        "pkg/vendorplugin/vendor.go",
        'default:\n\t\treturn fmt.Errorf("%w: %q is not one of %q, %q or %q", ErrLifecycleInvalid, string(l), LifecycleCurrent, LifecyclePreview, LifecycleLegacy)',
        'case "deprecated":\n\t\treturn fmt.Errorf("%w: %q is not one of %q, %q or %q", ErrLifecycleInvalid, string(l), LifecycleCurrent, LifecyclePreview, LifecycleLegacy)\n\tdefault:\n\t\treturn nil',
        "./pkg/vendorplugin",
        "TestRegisterRefusesAModelWithNoLineupState",
    ),
    Mutation(
        "vendor-effort-declaration",
        "pkg/vendorplugin/vendor.go",
        "if seen[word] {",
        'if seen[word] && word == "" {',
        "./pkg/vendorplugin",
        "TestRegisterRefusesAContradictoryEffortDeclaration",
    ),
    Mutation(
        "vendor-supersession-invalid",
        "pkg/vendorplugin/vendor.go",
        "if !declared {",
        'if !declared && model.SupersededBy == "narrow-only" {',
        "./pkg/vendorplugin",
        "TestRegisterRefusesAnUnusableSupersession",
    ),
    Mutation(
        "vendor-recommendation-ambiguous",
        "pkg/vendorplugin/vendor.go",
        "if already.system == system {",
        "if already.system == system && already.model == model.ID {",
        "./pkg/vendorplugin",
        "TestRegisterRefusesTwoRecommendationsForOneSystem",
    ),
    Mutation(
        "vendor-pricing-invalid",
        "pkg/vendorplugin/vendor.go",
        "if math.IsNaN(plan.MonthlyUSD) || math.IsInf(plan.MonthlyUSD, 0) {",
        "if math.IsNaN(plan.MonthlyUSD) {",
        "./pkg/vendorplugin",
        "TestRegisterRefusesAnUnusablePricingContract",
    ),
    Mutation(
        "vendor-model-invalid",
        "pkg/vendorplugin/vendor.go",
        "if m.ContextWindowTokens < 0 {",
        "if m.ContextWindowTokens < -1 {",
        "./pkg/vendorplugin",
        "TestRegisterRefusesANegativeContextWindow",
    ),
    Mutation(
        "vendor-duplicate-vendor",
        "pkg/vendorplugin/registry.go",
        "if _, exists := r.vendors[id]; exists {",
        'if _, exists := r.vendors[id]; exists && id == "never-duplicate" {',
        "./pkg/vendorplugin",
        "TestRegisterRefusesNilAndDuplicates",
    ),
]


def copy_source(destination: pathlib.Path) -> None:
    shutil.copytree(
        ROOT,
        destination,
        ignore=shutil.ignore_patterns(".git", ".temp"),
    )


def replace_once(path: pathlib.Path, old: str, new: str) -> None:
    source = path.read_text()
    count = source.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: replacement count {count}, want 1")
    path.write_text(source.replace(old, new, 1))


def apply_extra_mutation(mutation: Mutation, case: pathlib.Path) -> None:
    if mutation.name == "vendor-no-agentic-registry":
        replace_once(
            case / "pkg/vendorplugin/registry.go",
            "if source == nil {\n\t\treturn ErrNoAgenticRegistry\n\t}",
            "if source == nil {\n\t\treturn nil\n\t}",
        )
    elif mutation.name == "vendor-unknown-system-late-model":
        replace_once(
            case / "pkg/vendorplugin/registry.go",
            "if _, ok := r.systems.Lookup(system); !ok {",
            "if _, ok := r.systems.Lookup(system); !ok && modelIndex == 0 {",
        )
    elif mutation.name == "vendor-duplicate-model":
        replace_once(
            case / "pkg/vendorplugin/registry.go",
            "for _, model := range models {\n\t\tif err := model.Validate(); err != nil {",
            "for modelIndex, model := range models {\n\t\tif err := model.Validate(); err != nil {",
        )


def main() -> int:
    if OUT.exists():
        shutil.rmtree(OUT)
    OUT.mkdir(parents=True)
    rows: list[tuple[str, str, str]] = []
    failed = False
    for mutation in MUTATIONS:
        case = OUT / mutation.name
        copy_source(case)
        replace_once(case / mutation.file, mutation.old, mutation.new)
        apply_extra_mutation(mutation, case)
        command = [
            "go",
            "test",
            "-mod=mod",
            mutation.package,
            "-run",
            f"^{mutation.test}$",
            "-count=1",
        ]
        result = subprocess.run(command, cwd=case, capture_output=True, text=True)
        log = result.stdout + result.stderr
        (OUT / f"{mutation.name}.log").write_text(log)
        named_failure = f"--- FAIL: {mutation.test}" in log
        compile_clean = "[build failed]" not in log
        status = "killed" if result.returncode == 1 and named_failure and compile_clean else "invalid"
        rows.append((mutation.name, str(result.returncode), status))
        print(f"{mutation.name}: go test exit={result.returncode} status={status}")
        if status != "killed":
            failed = True
            print(log, file=sys.stderr)

    summary = ["mutation\tgo_test_exit\tstatus"]
    summary.extend("\t".join(row) for row in rows)
    (OUT / "summary.tsv").write_text("\n".join(summary) + "\n")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
