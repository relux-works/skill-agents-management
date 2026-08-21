#!/usr/bin/env python3
"""Negative-evidence harness for TASK-260822-hp5fb4.

Each mutant narrows or removes ONE production gate, runs the suite, and must
turn it RED. A gate whose removal leaves the suite green is a gate nothing is
holding, and the test that "covers" it is decoration.

Sources are restored from an in-memory pristine copy after every mutant, and a
final verification re-runs the clean suite so a crashed run cannot leave the
checkout mutated.
"""
import os
import shutil
import subprocess
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
CODEX = "pkg/agentic/systems/codex"
CORE = "pkg/agentic"

# (name, file, old, new, what removing it would let through)
MUTANTS = [
    (
        "env: strip by PREFIX instead of by exact key",
        f"{CODEX}/env.go",
        "\t\tif _, drop := blocked[key]; !drop {\n\t\t\tresult = append(result, entry)\n\t\t}",
        "\t\tdrop := false\n\t\tfor blockedKey := range blocked {\n\t\t\tif strings.HasPrefix(key, blockedKey) || strings.HasPrefix(key, \"CODEX_\") {\n\t\t\t\tdrop = true\n\t\t\t}\n\t\t}\n\t\tif !drop {\n\t\t\tresult = append(result, entry)\n\t\t}",
        "every CODEX_-shaped variable an operator set for their own reasons",
    ),
    (
        "env: stop resolving the credential POINTERS",
        f"{CODEX}/env.go",
        "\tif tokenEnvName := envValue(environ, appServerTokenNameEnv); tokenEnvName != \"\" {\n\t\tkeys = append(keys, tokenEnvName)\n\t}",
        "\tif false {\n\t\tkeys = append(keys, \"\")\n\t}",
        "the app-server credential itself, whose variable name is chosen at runtime",
    ),
    (
        "env: stop sanitizing PATH",
        f"{CODEX}/env.go",
        "\treturn sanitizePath(filterEnvKeys(environ, keys...))",
        "\treturn filterEnvKeys(environ, keys...)",
        "the parent's arg0 shim directory, so the child resolves through a wrapper whose parent is gone",
    ),
    (
        "env: sanitize PATH by dropping every entry",
        f"{CODEX}/env.go",
        "\t\t\tif isRuntimePathEntry(part) {\n\t\t\t\tcontinue\n\t\t\t}",
        "\t\t\tif true {\n\t\t\t\tcontinue\n\t\t\t}",
        "an over-strip that breaks every tool the child needs, which no golden can see",
    ),
    (
        "env: discard the parent environment entirely",
        f"{CODEX}/env.go",
        "\tenv := agentic.WithRunContext(filterRuntimeEnv(parent), req)",
        "\tenv := agentic.WithRunContext(nil, req)",
        "every unrelated variable the operator's environment carried",
    ),
    (
        "env: let a blocked key collide with an injected one",
        f"{CODEX}/env.go",
        "\tsessionIDEnv = \"TASK_BOARD_SESSION_ID\"",
        "\tsessionIDEnv = \"TASK_BOARD_RUN_ID\"",
        "a collision that makes childEnv's filter-then-inject order load-bearing, "
        "unnoticed until the other order drops the variable from the child",
    ),
    (
        "binary: skip the managed-npm path",
        f"{CODEX}/binary.go",
        "\tif managed := managedBinaryPath(envValue(env, managedPackageRootEnv)); managed != \"\" {",
        "\tif managed := \"\"; managed != \"\" {",
        "a launch on the bare PATH shim while a managed install pinned a native binary",
    ),
    (
        "binary: skip the native-shim unwrap",
        f"{CODEX}/binary.go",
        "\tif native := nativeBinaryFromShim(binary); native != \"\" {",
        "\tif native := \"\"; native != \"\" {",
        "a node wrapper exec'd in place of the native binary behind it",
    ),
    (
        "binary: trust the managed layout without a stat",
        f"{CODEX}/binary.go",
        "\t\tif isRegularFile(managed) {\n\t\t\treturn managed, nil\n\t\t}\n\t}",
        "\t\treturn managed, nil\n\t}",
        "a path to a file that does not exist, reported as the launch target",
    ),
    (
        "binary: accept a directory as an executable",
        f"{CODEX}/binary.go",
        "\treturn info.Mode().IsRegular()\n}",
        "\treturn info.Mode().IsDir() || info.Mode().IsRegular()\n}",
        "a directory named codex resolved as the binary to exec",
    ),
    (
        "binary: drop the codex.js shape check",
        f"{CODEX}/binary.go",
        "\tif filepath.Base(resolved) != shimFileName {\n\t\treturn \"\"\n\t}",
        "\tif false {\n\t\treturn \"\"\n\t}",
        "an ordinary PATH binary rewritten into a package layout that does not exist",
    ),
    (
        "binary: drop the bin/ shape check",
        f"{CODEX}/binary.go",
        "\tif filepath.Base(binDir) != \"bin\" {\n\t\treturn \"\"\n\t}",
        "\tif false {\n\t\treturn \"\"\n\t}",
        "a codex.js anywhere on disk treated as an npm shim with a package root above it",
    ),
    (
        "binary: fall back to the ambient PATH when the launch env has none",
        f"{CODEX}/binary.go",
        "\tpathValue, ok := lookupEnv(env, \"PATH\")\n\tif !ok {\n\t\treturn \"\", ErrNoPathInLaunchEnvironment\n\t}",
        "\tpathValue, ok := lookupEnv(env, \"PATH\")\n\tif !ok {\n\t\tpathValue = os.Getenv(\"PATH\")\n\t}",
        "a resolution against a PATH the child will never see, reported as the launch binary",
    ),
    (
        "binary: accept a non-executable PATH candidate",
        f"{CODEX}/binary.go",
        "\treturn mode.IsRegular() && mode.Perm()&0o111 != 0",
        "\treturn mode.IsRegular()",
        "a source file named codex shadowing the real binary",
    ),
    (
        "args: spell the profile flag the managed-session way in exec mode",
        f"{CODEX}/args.go",
        "\t\t\targs = append(args, \"-p\", profile)",
        "\t\t\targs = append(args, \"--profile\", profile)",
        "the exact three-way drift the source paid a task to unwind",
    ),
    (
        "args: spell the model flag the exec way in managed-session mode",
        f"{CODEX}/args.go",
        "\t\t\t\"--model\", model,",
        "\t\t\t\"-m\", model,",
        "the other half of that drift, on the surface no golden covers",
    ),
    (
        "args: emit the effort override unconditionally",
        f"{CODEX}/args.go",
        "\tif effort := strings.TrimSpace(req.Effort); effort != \"\" {\n\t\targs = append(args, \"-c\", fmt.Sprintf(\"model_reasoning_effort=%q\", effort))\n\t}",
        "\targs = append(args, \"-c\", fmt.Sprintf(\"model_reasoning_effort=%q\", strings.TrimSpace(req.Effort)))",
        "an effort level spelled as the empty string",
    ),
    (
        "args: emit --add-dir with no directory",
        f"{CODEX}/args.go",
        "\t\tif boardDir := strings.TrimSpace(req.Run.BoardDir); boardDir != \"\" {\n\t\t\targs = append(args, \"--add-dir\", boardDir)\n\t\t}",
        "\t\targs = append(args, \"--add-dir\", strings.TrimSpace(req.Run.BoardDir))",
        "a sandbox grant of nothing, which fails the launch",
    ),
    (
        "args: pass an unrecognized service tier through verbatim",
        f"{CODEX}/args.go",
        "\tdefault:\n\t\treturn \"\"\n\t}\n}",
        "\tdefault:\n\t\treturn strings.ToLower(strings.TrimSpace(tier))\n\t}\n}",
        "a tier id codex does not accept, reaching the config override",
    ),
    (
        "args: alias the caller's composition prefix",
        f"{CODEX}/args.go",
        "\treturn append([]string{}, req.Composition.Prefix...)",
        "\treturn req.Composition.Prefix",
        "one launch's flags appearing in another launch's composition",
    ),
    (
        "composition: accept any MCP field",
        f"{CODEX}/composition.go",
        "\t\tif !compositionFields[leaf] {\n\t\t\treturn fmt.Errorf(\"codex composition contains a disallowed MCP field\")\n\t\t}",
        "\t\tif false {\n\t\t\treturn fmt.Errorf(\"codex composition contains a disallowed MCP field\")\n\t\t}",
        "an unreviewed per-server key reaching the harness",
    ),
    (
        "composition: accept a top-level flag between the pairs",
        f"{CODEX}/composition.go",
        "\t\tif prefix[i] != \"-c\" {\n\t\t\treturn fmt.Errorf(\"codex composition contains a disallowed top-level argument\")\n\t\t}",
        "\t\tif false {\n\t\t\treturn fmt.Errorf(\"codex composition contains a disallowed top-level argument\")\n\t\t}",
        "an arbitrary codex flag into a launch reviewed as an MCP composition",
    ),
    (
        "composition: stop checking the bearer reference against the metadata",
        f"{CODEX}/composition.go",
        "\t\t\tif leaf == \"bearer_token_env_var\" && decoded != server.BearerTokenEnvVar {",
        "\t\t\tif false && leaf == \"bearer_token_env_var\" && decoded != server.BearerTokenEnvVar {",
        "codex pointed at a different credential variable than the server authorized",
    ),
    (
        "composition: accept args that are not a TOML string array",
        f"{CODEX}/composition.go",
        "\t\t\tif err := validateTOMLStringArray(value); err != nil {",
        "\t\t\tif err := error(nil); err != nil {",
        "a value codex's own parser rejects, or reads as something other than arguments",
    ),
    (
        "plugin: declare no composition grammar",
        f"{CODEX}/codex.go",
        "\t\tCompositionGrammar:  GrammarTOMLConfigPairs,",
        "\t\tCompositionGrammar:  agentic.GrammarNone,",
        "every composed codex launch refused before the plugin sees it",
    ),
    (
        "plugin: declare budget support codex does not have",
        f"{CODEX}/codex.go",
        "\t\tSupportsBudget:      false,",
        "\t\tSupportsBudget:      true,",
        "a budget accepted and then silently dropped",
    ),
    (
        "plugin: read a failed prompt read as an empty stream",
        f"{CODEX}/codex.go",
        "\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn agentic.StdinPayload{}, fmt.Errorf(\"codex: reading the assignment prompt: %w\", err)\n\t\t}",
        "\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn agentic.StdinPayload{}, nil\n\t\t}",
        "a codex child launched on an empty stdin, reported as a model that produced no work",
    ),
    (
        "plugin: alias the caller's prompt bytes",
        f"{CODEX}/codex.go",
        "\t\treturn agentic.StdinPayload{Attached: true, Bytes: append([]byte(nil), req.Prompt...)}, nil",
        "\t\treturn agentic.StdinPayload{Attached: true, Bytes: req.Prompt}, nil",
        "a launcher rewriting the caller's prompt buffer through the plan",
    ),
    (
        "core: replace run-context values by PREFIX",
        f"{CORE}/runcontext.go",
        "\t\tif entry == key || strings.HasPrefix(entry, prefix) {",
        "\t\tif strings.HasPrefix(entry, key) {",
        "TASK_BOARD_LIKE_BUT_NOT rewritten while TASK_BOARD_RUN_ID is replaced",
    ),
    (
        "core: export a blank instead of removing the key",
        f"{CORE}/runcontext.go",
        "\tif value != \"\" {\n\t\tout = append(out, prefix+value)\n\t}",
        "\tout = append(out, prefix+value)",
        "a goal id that is present and meaningless, where the launch carried none",
    ),
    (
        "core: export the board selector as given",
        f"{CORE}/runcontext.go",
        "\tenv = SetEnvValue(env, EnvBoardDir, AbsoluteBoardDir(req.Run.BoardDir))",
        "\tenv = SetEnvValue(env, EnvBoardDir, strings.TrimSpace(req.Run.BoardDir))",
        "a child resolving .task-board against its own worktree instead of the authoritative board",
    ),
]


def read(path):
    with open(os.path.join(ROOT, path)) as handle:
        return handle.read()


def write(path, text):
    with open(os.path.join(ROOT, path), "w") as handle:
        handle.write(text)


def run_suite():
    env = dict(os.environ)
    env.pop("TASK_BOARD_DIR", None)
    proc = subprocess.run(
        ["go", "test", "-mod=mod", "./...", "-count=1"],
        cwd=ROOT, capture_output=True, text=True, env=env,
    )
    return proc.returncode, proc.stdout + proc.stderr


def main():
    if not shutil.which("go"):
        print("go toolchain not found", file=sys.stderr)
        return 2

    pristine = {}
    for _, path, _, _, _ in MUTANTS:
        if path not in pristine:
            pristine[path] = read(path)

    code, output = run_suite()
    if code != 0:
        print("BASELINE IS ALREADY RED; no mutant below would prove anything\n" + output)
        return 1
    print("baseline: GREEN (exit 0)\n")

    survivors = []
    try:
        for name, path, old, new, lets in MUTANTS:
            source = pristine[path]
            if source.count(old) != 1:
                print(f"SKIPPED  {name}\n         the anchor appears {source.count(old)} times in {path}; the mutant did not apply and proves nothing")
                survivors.append((name, "anchor not unique"))
                continue
            write(path, source.replace(old, new, 1))
            code, output = run_suite()
            write(path, source)
            if code == 0:
                print(f"SURVIVED {name}\n         nothing failed. This gate admits: {lets}")
                survivors.append((name, "suite stayed green"))
            else:
                failures = sorted({
                    line.split(":")[0].strip()
                    for line in output.splitlines()
                    if line.strip().startswith("--- FAIL")
                } | {
                    line.strip().split(" ")[2]
                    for line in output.splitlines()
                    if line.strip().startswith("--- FAIL:")
                })
                named = ", ".join(
                    sorted({
                        line.strip().split(" ")[2]
                        for line in output.splitlines()
                        if line.strip().startswith("--- FAIL:")
                    })
                )[:400]
                compile_error = "build failed" in output or "cannot use" in output
                tag = "RED (compile)" if compile_error and not named else "RED"
                print(f"{tag:14} {name}\n         caught by: {named or 'the package failed to build under the mutation'}")
                _ = failures
    finally:
        for path, source in pristine.items():
            write(path, source)

    code, _ = run_suite()
    print(f"\nrestored checkout: {'GREEN (exit 0)' if code == 0 else 'RED — RESTORE FAILED'}")

    print(f"\n{len(MUTANTS) - len(survivors)}/{len(MUTANTS)} mutants caught")
    for name, why in survivors:
        print(f"  SURVIVOR: {name} ({why})")
    return 1 if survivors or code != 0 else 0


if __name__ == "__main__":
    sys.exit(main())
