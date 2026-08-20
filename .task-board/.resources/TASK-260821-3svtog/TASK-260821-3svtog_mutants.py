#!/usr/bin/env python3
"""Negative-evidence harness: apply one mutant at a time, run the tests that
must catch it, restore the file, and report red/green per mutant.

Backups are byte copies of the mutated file only. No git command is run."""
import pathlib, shutil, subprocess, sys, textwrap

ROOT = pathlib.Path(__file__).resolve().parents[2]
LOG = []

MUTANTS = [
    dict(
        name="M1 ldflags -X path drift (Makefile names a package that no longer exists)",
        file="Makefile",
        old="CLI_PKG := github.com/relux-works/skill-agents-management/tools/agents-management/cmd",
        new="CLI_PKG := github.com/relux-works/skill-agents-management/tools/agents-management/cmdX",
        expect_red=["TestMakeBuildInjectsVersionMetadata$",
                    "TestMakeBuildInjectsVersionMetadataIntoVersionFlag$"],
    ),
    dict(
        name="M2 ldflags dropped from `make build` entirely",
        file="Makefile",
        old="\t@go build $(GOFLAGS_MOD) -ldflags '$(LDFLAGS)' -o $(BIN) ./tools/agents-management",
        new="\t@go build $(GOFLAGS_MOD) -o $(BIN) ./tools/agents-management",
        expect_red=["TestMakeBuildInjectsVersionMetadata$",
                    "TestMakeBuildInjectsVersionMetadataIntoVersionFlag$"],
    ),
    dict(
        name="M3 formatVersion narrowed: injected commit/date silently dropped",
        file="tools/agents-management/cmd/root.go",
        old='\tv := fmt.Sprintf("agents-management version %s", Version)\n\tif Commit != "" {',
        new='\tv := fmt.Sprintf("agents-management version %s", Version)\n\tif false {',
        expect_red=["TestMakeBuildInjectsVersionMetadata$",
                    "TestVersionCommandReportsInjectedMetadata$",
                    "TestFormatVersionOmitsAbsentMetadata$"],
    ),
    dict(
        name="M4 version output moved off stdout onto stderr",
        file="tools/agents-management/cmd/version.go",
        old="fmt.Fprintln(cmd.OutOrStdout(), formatVersion())",
        new="fmt.Fprintln(cmd.ErrOrStderr(), formatVersion())",
        expect_red=["TestMakeBuildInjectsVersionMetadata$",
                    "TestVersionCommandReportsInjectedMetadata$",
                    "TestBuildWithoutLdflagsReportsDefaults$"],
    ),
    dict(
        name="M5 empty plugin registry treated as a failure",
        file="tools/agents-management/cmd/plugins.go",
        old="\t\tnames := pluginNames()\n\t\tout := cmd.OutOrStdout()",
        new='\t\tnames := pluginNames()\n\t\tif len(names) == 0 {\n\t\t\treturn fmt.Errorf("no plugins registered")\n\t\t}\n\t\tout := cmd.OutOrStdout()',
        expect_red=["TestPluginsCommandEmptyListSucceeds$",
                    "TestPluginsCommandEmptyListEncodesAsEmptyArray$",
                    "TestBuiltBinaryListsEmptyPluginsWithoutError$",
                    "TestBuiltBinaryEncodesEmptyPluginsAsEmptyArray$"],
    ),
    dict(
        name="M7 gitignore anchor dropped: bare `agents-management` swallows the CLI source dir",
        file=".gitignore",
        old="/agents-management\n",
        new="agents-management\n",
        expect_red=["TestBuildOutputIsIgnoredAndSourcesAreNot$"],
    ),
    dict(
        name="M8 build-output ignore rules deleted (narrowing: the binary becomes committable)",
        file=".gitignore",
        old="/agents-management\n/tools/agents-management/agents-management\n",
        new="",
        expect_red=["TestBuildOutputIsIgnoredAndSourcesAreNot$"],
    ),
    dict(
        name="M6 pluginNames returns the registry slice directly (nil -> JSON null, and aliases)",
        file="tools/agents-management/cmd/plugins.go",
        old="\tnames := make([]string, 0, len(registeredPlugins))\n\tnames = append(names, registeredPlugins...)\n\tsort.Strings(names)\n\treturn names",
        new="\tnames := registeredPlugins\n\tsort.Strings(names)\n\treturn names",
        expect_red=["TestPluginsCommandEmptyListEncodesAsEmptyArray$",
                    "TestBuiltBinaryEncodesEmptyPluginsAsEmptyArray$",
                    "TestPluginNamesDoesNotAliasRegistry$"],
    ),
]


def run_tests(pattern):
    cmd = ["go", "test", "-mod=mod", "./tools/agents-management/...",
           "-count=1", "-run", pattern]
    proc = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True,
                          env={**__import__("os").environ, "TASK_BOARD_DIR": ""})
    return proc.returncode, proc.stdout + proc.stderr


def main():
    failures = []
    for m in MUTANTS:
        target = ROOT / m["file"]
        backup = target.with_suffix(target.suffix + ".mutbak")
        shutil.copy2(target, backup)
        try:
            src = target.read_text()
            if m["old"] not in src:
                raise SystemExit(f"mutant {m['name']}: anchor not found in {m['file']}")
            target.write_text(src.replace(m["old"], m["new"], 1))
            pattern = "|".join(m["expect_red"])
            code, out = run_tests(pattern)
            verdict = "RED (gate caught the mutant)" if code != 0 else "GREEN (GATE IS VACUOUS)"
            LOG.append(f"### {m['name']}\n"
                       f"- mutated: `{m['file']}`\n"
                       f"- tests run: `-run '{pattern}'`\n"
                       f"- exit code: {code} -> {verdict}\n\n"
                       f"```\n{textwrap.shorten(out, 1800, placeholder=' ...[truncated]')}\n```\n")
            print(f"{m['name']}: exit={code} {verdict}")
            if code == 0:
                failures.append(m["name"])
        finally:
            shutil.copy2(backup, target)
            backup.unlink()

    # Clean baseline after every mutant is reverted.
    code, out = run_tests(".")
    LOG.append(f"### Baseline after all mutants reverted\n"
               f"- `go test -mod=mod ./tools/agents-management/... -count=1`\n"
               f"- exit code: {code}\n\n```\n{out.strip()[-1200:]}\n```\n")
    print(f"baseline restored: exit={code}")
    if code != 0:
        failures.append("baseline")

    (ROOT / ".temp/TASK-260821-3svtog/negative-evidence.md").write_text(
        "# TASK-260821-3svtog negative evidence\n\n"
        "Each mutant below was applied to the working tree, the tests that must\n"
        "catch it were run, and the file was restored byte-for-byte. A mutant\n"
        "that leaves its tests GREEN means the gate proves nothing.\n\n"
        + "\n".join(LOG))
    sys.exit(1 if failures else 0)


main()
