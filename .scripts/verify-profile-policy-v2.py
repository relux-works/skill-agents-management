#!/usr/bin/env python3
"""Run named, compile-clean policy narrowings using the refusal-matrix helpers."""
from __future__ import annotations
import argparse
import importlib.util
import pathlib
import shutil
import subprocess
import sys

sys.dont_write_bytecode = True

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("refusal_matrix", ROOT / ".scripts/verify-refusal-matrix.py")
shared = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = shared
spec.loader.exec_module(shared)
OUT = ROOT / ".temp/TASK-261002-3tyn1e/mutants"
FILE = "pkg/inferenceengine/profile_policy_v2.go"
MUTATIONS = []

def add(name, old, new, test, bound, file=FILE):
    MUTATIONS.append((shared.Mutation(name, file, old, new, "./pkg/inferenceengine", test), bound))

for field, low, high, key in [
    ("PromptTokens",1024,1000000,"prompt_tokens"),
    ("MaxOutputTokens",1,4096,"max_output_tokens"),
    ("StartupTimeoutSeconds",1,3600,"startup_timeout_seconds"),
    ("RequestTimeoutSeconds",1,86400,"request_timeout_seconds"),
    ("SampleIntervalMilliseconds",50,10000,"sample_interval_milliseconds"),
    ("MaxRestarts",1,100,"max_restarts"),
    ("RestartWindowSeconds",1,86400,"restart_window_seconds"),
    ("RestartDelayMilliseconds",0,60000,"restart_delay_milliseconds"),
]:
    for side, old, new, admitted in [
        ("low",f"value.{field} < {low}",f"value.{field} < {low-1}",low-1),
        ("high",f"value.{field} > {high}",f"value.{field} > {high+1}",high+1),
    ]:
        add(f"{key}-{side}",old,new,"TestPolicyV2NumericBoundaries",f"admits only adjacent {key}={admitted}")

for name,old,new,bound in [
    ("fatal-count-low","len(value.FatalOutputSubstrings) < 1","len(value.FatalOutputSubstrings) < 1 && value.FatalOutputSubstrings == nil","admits non-null empty list only"),
    ("fatal-count-high","len(value.FatalOutputSubstrings) > 16","len(value.FatalOutputSubstrings) > 17","admits 17 entries"),
    ("fatal-empty",'len(substring) == 0','len(substring) == 0 && len(value.FatalOutputSubstrings) == 1',"admits empty later entry in a multi-entry list"),
    ("fatal-bytes","len(substring) > 512","len(substring) > 513","admits 513-byte substring"),
    ("fatal-NUL","strings.ContainsRune(substring, '\\x00')","strings.ContainsRune(substring, '\\x00') && substring != \"a\\x00b\"","admits exactly a-NUL-b"),
    ("fatal-later",'for _, substring := range value.FatalOutputSubstrings {','for index, substring := range value.FatalOutputSubstrings {\n\t\t\tif index == 1 && substring == "" { continue }',"admits empty second substring only"),
]: add(name,old,new,"TestPolicyV2FatalSubstringBoundaries",bound)

add("missing-boolean","if !present {",'if !present && key != "restart_on_failure" {',"TestPolicyV2ClosedDecoding","admits missing restart_on_failure as false only")
add("null-delay",'if string(value) == "null" {','if string(value) == "null" && key != "restart_delay_milliseconds" {',"TestPolicyV2ClosedDecoding","admits null restart delay as zero only")
add("duplicate-delay","if _, duplicate := fields[key]; duplicate {",'if _, duplicate := fields[key]; duplicate && key != "restart_delay_milliseconds" {',"TestPolicyV2ClosedDecoding","admits duplicate restart delay key only")
add("duplicate-configured","if _, duplicate := fields[key]; duplicate {",'if _, duplicate := fields[key]; duplicate && key != "configured" {',"TestPolicyV2NotConfiguredAndFailures","admits duplicate configured key only")
add("unknown-extra",'if _, known := required[key]; !known {','if key == "extra" { delete(fields, key); raw, _ = encode(fields); continue }; if _, known := required[key]; !known {',"TestPolicyV2ClosedDecoding","admits extra member only")
add("case-boolean",'for key := range fields {','if value, ok := fields["RESTART_ON_FAILURE"]; ok { fields["restart_on_failure"] = value; delete(fields, "RESTART_ON_FAILURE"); raw, _ = encode(fields) }; for key := range fields {',"TestPolicyV2ClosedDecoding","admits uppercase RESTART_ON_FAILURE only")
add("not-configured-extra",'len(fields) != 1 || string(configured) != "false"','(len(fields) != 1 && !(len(fields) == 2 && fields["extra"] != nil)) || string(configured) != "false"',"TestPolicyV2NotConfiguredAndFailures","admits extra member on configured=false only")
add("not-configured-true",'string(configured) != "false"','(string(configured) != "false" && string(configured) != "true")',"TestPolicyV2NotConfiguredAndFailures","admits configured=true only")
add("not-configured-zero",'string(configured) != "false"','(string(configured) != "false" && string(configured) != "0")',"TestPolicyV2NotConfiguredAndFailures","admits configured=0 only")
add("shape-null",'token, err := decoder.Token()\n\tif err != nil || token','if strings.TrimSpace(raw) == "null" { return true, nil }; token, err := decoder.Token()\n\tif err != nil || token',"TestPolicyV2ClosedDecoding","admits whole null as not-configured only")
add("trailing-not-configured",'!errors.Is(err, io.EOF) {','!errors.Is(err, io.EOF) && fields["configured"] == nil {',"TestPolicyV2NotConfiguredAndFailures","admits trailing JSON only on not-configured form")
add("typed-boolean-zero",'if err := decodeClosed(raw, destination); err != nil {','if string(fields["restart_on_failure"]) == "0" { fields["restart_on_failure"] = json.RawMessage("false"); raw, _ = encode(fields) }; if err := decodeClosed(raw, destination); err != nil {',"TestPolicyV2ClosedDecoding","admits numeric zero boolean only")
add("typed-fraction",'if err := decodeClosed(raw, destination); err != nil {','if string(fields["max_restarts"]) == "1.5" { fields["max_restarts"] = json.RawMessage("1"); raw, _ = encode(fields) }; if err := decodeClosed(raw, destination); err != nil {',"TestPolicyV2NumericBoundaries","admits max_restarts=1.5 only")
add("typed-array-false",'if err := decodeClosed(raw, destination); err != nil {','if string(fields["fatal_output_substrings"]) == "false" { fields["fatal_output_substrings"] = json.RawMessage(`["fatal"]`); raw, _ = encode(fields) }; if err := decodeClosed(raw, destination); err != nil {',"TestPolicyV2ClosedDecoding","admits boolean false substring array only")
add("version-stress-v1",'case FactStressPolicy:\n\t\t\treturn ValueContractStressPolicyV2','case FactStressPolicy:\n\t\t\treturn ValueContractStressPolicy',"TestPolicyV2VersionCompatibility","retains v1 stress under new version only")
add("version-restart-v1",'case FactRestartSupervisionPolicy:\n\t\t\treturn ValueContractRestartPolicyV2','case FactRestartSupervisionPolicy:\n\t\t\treturn ValueContractRestartPolicy',"TestPolicyV2VersionCompatibility","retains v1 restart under new version only")

add("multiple-versions", 'if len(versions) > 1 {', 'if len(versions) > 2 {', "TestPolicyV2VersionCompatibility", "admits exactly two version arguments", "pkg/inferenceengine/contract.go")
add("unknown-version", 'if version != ContractVersion && version != ContractVersionV3 {', 'if version == "observed-process/v4" { version = ContractVersion }; if version != ContractVersion && version != ContractVersionV3 {', "TestPolicyV2VersionCompatibility", "admits exactly observed-process/v4 as old version", "pkg/inferenceengine/contract.go")

add("round-zero-delay", 'if value.RestartDelayMilliseconds < 0 || value.RestartDelayMilliseconds > 60000 {', 'if value.RestartDelayMilliseconds == 0 { value.RestartDelayMilliseconds = 1 }; if value.RestartDelayMilliseconds < 0 || value.RestartDelayMilliseconds > 60000 {', "TestPolicyV2CanonicalValues", "rewrites only zero restart delay to one instead of preserving the catalog policy")

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--start",type=int,default=0)
    parser.add_argument("--limit",type=int,default=len(MUTATIONS))
    args = parser.parse_args()
    OUT.mkdir(parents=True,exist_ok=True)
    case = OUT / f"candidate-{args.start}"
    if case.exists(): shutil.rmtree(case)
    shared.copy_source(case)
    baselines = {file: (ROOT / file).read_bytes() for file in {mutation.file for mutation, _ in MUTATIONS}}
    rows = ["mutant\tnarrowing\ttest\texit\tstatus\tcommand"]
    failed = False
    try:
        for mutation, bound in MUTATIONS[args.start:args.start+args.limit]:
            for file, baseline in baselines.items(): (case / file).write_bytes(baseline)
            shared.replace_once(case / mutation.file,mutation.old,mutation.new)
            command = ["go","test","-mod=mod",mutation.package,"-run",f"^{mutation.test}$","-count=1","-v"]
            result = subprocess.run(command,cwd=case,capture_output=True,text=True)
            log = result.stdout + result.stderr
            (OUT / f"{mutation.name}.log").write_text(log)
            named = f"--- FAIL: {mutation.test}" in log
            status = "killed" if result.returncode == 1 and named and "[build failed]" not in log else "survivor" if result.returncode == 0 else "invalid"
            failures = [line.strip().split(" (",1)[0].removeprefix("--- FAIL: ") for line in log.splitlines() if "--- FAIL:" in line]
            rows.append("\t".join([mutation.name,bound,"; ".join(failures) or mutation.test,str(result.returncode),status," ".join(command)]))
            print(f"{mutation.name}: exit={result.returncode} {status}",flush=True)
            failed |= status != "killed"
    finally:
        for file, baseline in baselines.items(): (case / file).write_bytes(baseline)
        (OUT / f"summary-{args.start}.tsv").write_text("\n".join(rows)+"\n")
    return int(failed)

if __name__ == "__main__": raise SystemExit(main())
