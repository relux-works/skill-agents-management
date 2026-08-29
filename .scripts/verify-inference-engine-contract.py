#!/usr/bin/env python3
from __future__ import annotations

import dataclasses
import pathlib
import shutil
import subprocess
import sys


ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT / ".temp" / "TASK-260830-ter72z" / "contract-mutants"


@dataclasses.dataclass(frozen=True)
class Mutation:
    name: str
    old: str
    new: str
    test: str


MUTATIONS = [
    Mutation(
        "trusted-composition-interface",
        "type Engine interface {\n\tplugin.Plugin\n\tEngineContract() Contract\n}",
        "type Engine interface {\n\tplugin.Plugin\n\tEngineContract() Contract\n\tDeriveObservation(Fact) string\n}",
        "TestEngineTrustBoundaryIsDeclarationOnly",
    ),
    Mutation(
        "readiness-requires-residency",
        "if !value.EndpointAnswering || !value.WeightsResident {",
        "if !value.EndpointAnswering && !value.WeightsResident {",
        "TestReadinessRefusesEndpointAnsweringWithoutResidentWeights",
    ),
    Mutation(
        "unsupported-refusal",
        "\t\tif got != want {\n\t\t\treturn Contract{}, fmt.Errorf(\"%w: %s must use source %q, value contract %q, and refuse read-failed, malformed, and unsupported derivations\", ErrContractInvalid, want.Fact, want.Source, want.ValueContract)\n\t\t}",
        "\t\tgotWithoutUnsupported := got\n\t\twantWithoutUnsupported := want\n\t\tgotWithoutUnsupported.OnFailure.Unsupported = FailureActionRefuse\n\t\twantWithoutUnsupported.OnFailure.Unsupported = FailureActionRefuse\n\t\tif gotWithoutUnsupported != wantWithoutUnsupported {\n\t\t\treturn Contract{}, fmt.Errorf(\"%w: %s must use source %q, value contract %q, and refuse read-failed, malformed, and unsupported derivations\", ErrContractInvalid, want.Fact, want.Source, want.ValueContract)\n\t\t}",
        "TestResolveContractRefusesFallbacksAndUntrustedSources/unsupported_silently_dropped",
    ),
    Mutation(
        "required-speculative-active-presence",
        "\t\trawValue, found := supplied[name]\n\t\tif !found {",
        "\t\trawValue, found := supplied[name]\n\t\tif !found && name != \"active\" {",
        "TestZeroValuedRequiredFieldsDistinguishExplicitFromOmitted/speculative_active_false",
    ),
    Mutation(
        "required-restart-max-presence",
        "\t\trawValue, found := supplied[name]\n\t\tif !found {",
        "\t\trawValue, found := supplied[name]\n\t\tif !found && name != \"max_restarts\" {",
        "TestZeroValuedRequiredFieldsDistinguishExplicitFromOmitted/restart_max_zero",
    ),
]


def copy_source(destination: pathlib.Path) -> None:
    shutil.copytree(ROOT, destination, ignore=shutil.ignore_patterns(".git", ".temp"))


def replace_once(path: pathlib.Path, old: str, new: str) -> None:
    source = path.read_text()
    count = source.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: replacement count {count}, want 1")
    path.write_text(source.replace(old, new, 1))


def main() -> int:
    if OUT.exists():
        shutil.rmtree(OUT)
    OUT.mkdir(parents=True)
    rows: list[tuple[str, str, str]] = []
    failed = False
    for mutation in MUTATIONS:
        case = OUT / mutation.name
        copy_source(case)
        replace_once(case / "pkg/inferenceengine/contract.go", mutation.old, mutation.new)
        command = [
            "go",
            "test",
            "-mod=mod",
            "./pkg/inferenceengine",
            "-run",
            f"^{mutation.test}$",
            "-count=1",
        ]
        result = subprocess.run(command, cwd=case, capture_output=True, text=True)
        log = result.stdout + result.stderr
        (OUT / f"{mutation.name}.log").write_text(log)
        failure_name = mutation.test.split("/", 1)[0]
        named_failure = f"--- FAIL: {failure_name}" in log
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
