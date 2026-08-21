#!/usr/bin/env python3
"""Narrow every gate this task wrote, one at a time, and confirm the suite goes red.

A green suite means nothing unless something in it would have failed. This
harness weakens ONE piece of production code per run, records the real exit code
and the tests that failed, and restores the file byte-for-byte before the next
mutant. It is not a coverage tool: each mutant is a defect a port could
plausibly ship, and the test that catches it is named in the log.

It covers every refusal branch the four plugins and the two shared internals
introduced, not only the ones a reviewer named — which is the lesson the claude
rework's own harness recorded.

Usage:
    python3 .temp/TASK-260822-xz8rj5/mutants.py [--out DIR]

Exit status is 0 when EVERY mutant was caught. A mutant the suite admits is
reported as UNCAUGHT and fails the run — that is the finding, not an error.
"""

import argparse
import filecmp
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[2]
QWEN = "pkg/agentic/systems/qwen"
GEMINI = "pkg/agentic/systems/gemini"
MUSE = "pkg/agentic/systems/muse"
AGY = "pkg/agentic/systems/agy"
RUNTIMEENV = "internal/runtimeenv/runtimeenv.go"
MCPJSON = "internal/mcpjson/mcpjson.go"

# (name, file, old, new, why). `old` must appear exactly once; None creates the file.
MUTANTS = [
    # --- qwen: the environment fix this plugin exists to preserve -------------
    (
        "qwen-claudecode-not-stripped",
        f"{QWEN}/env.go",
        "\treturn runtimeenv.Filter(runtimeenv.FilterKeys(environ, sessionMarkerEnv))",
        "\treturn runtimeenv.Filter(environ)",
        "the source's fixed leak reopens: a qwen child inherits CLAUDECODE and refuses to start inside a Claude Code session",
    ),
    (
        "qwen-composition-prefix-not-copied",
        f"{QWEN}/args.go",
        "\treturn append([]string{}, req.Composition.Prefix...)",
        "\treturn req.Composition.Prefix",
        "the plugin appends through into its caller's backing array",
    ),
    (
        "gemini-composition-prefix-not-copied",
        f"{GEMINI}/args.go",
        "\targs := append([]string{}, req.Composition.Prefix...)",
        "\targs := req.Composition.Prefix",
        "the plugin appends through into its caller's backing array",
    ),
    (
        "muse-composition-prefix-not-copied",
        f"{MUSE}/args.go",
        "\targs := append([]string{}, req.Composition.Prefix...)",
        "\targs := req.Composition.Prefix",
        "the plugin appends through into its caller's backing array",
    ),
    (
        "agy-composition-prefix-not-copied",
        f"{AGY}/args.go",
        "\targs := append([]string{}, req.Composition.Prefix...)",
        "\targs := req.Composition.Prefix",
        "the plugin appends through into its caller's backing array",
    ),
    # --- qwen: the stdin protocol, which no argv comparison can see -----------
    (
        "qwen-effort-dropped-from-stdin",
        f"{QWEN}/stdin.go",
        '\t\t\t\t"effort":  strings.TrimSpace(req.Effort),',
        '\t\t\t\t"effort":  "",',
        "the configured reasoning effort never reaches the harness; qwen has no effort FLAG, so argv is byte-identical",
    ),
    (
        "qwen-prompt-html-escaped",
        f"{QWEN}/stdin.go",
        "\tencoder.SetEscapeHTML(false)\n",
        "",
        "an assignment carrying <, > or & reaches the child escaped and the model reads the escape sequence",
    ),
    (
        "qwen-session-fallback-reordered",
        f"{QWEN}/stdin.go",
        '\tif id := strings.TrimSpace(req.Run.RunID); id != "" {\n\t\treturn id\n\t}\n\tif id := strings.TrimSpace(req.Run.TaskID); id != "" {\n\t\treturn id\n\t}',
        '\tif id := strings.TrimSpace(req.Run.TaskID); id != "" {\n\t\treturn id\n\t}\n\tif id := strings.TrimSpace(req.Run.RunID); id != "" {\n\t\treturn id\n\t}',
        "the child reports its frames under the task id rather than the run id",
    ),
    (
        "qwen-unreadable-prompt-read-as-absent",
        f"{QWEN}/stdin.go",
        '\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn nil, false, fmt.Errorf("qwen: reading the assignment prompt: %w", err)\n\t\t}\n\t\treturn data, true, nil',
        "\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn nil, false, nil\n\t\t}\n\t\treturn data, true, nil",
        "a failed read is treated as a legitimate absence: the child launches with no assignment and the run reads as the model producing no work",
    ),
    # --- gemini ---------------------------------------------------------------
    (
        "gemini-dry-run-placeholder-in-a-real-launch",
        f"{GEMINI}/args.go",
        "\tcase agentic.LaunchModeExec:\n\tcase agentic.LaunchModeDryRun:\n\t\tprompt = promptPlaceholder",
        "\tcase agentic.LaunchModeExec:\n\t\tprompt = promptPlaceholder\n\tcase agentic.LaunchModeDryRun:\n\t\tprompt = promptPlaceholder",
        "a real launch passes the literal <prompt> in -p; gemini appends stdin to that value, so the child reads the placeholder as part of its assignment",
    ),
    (
        "gemini-workspace-dropped",
        f"{GEMINI}/args.go",
        '\t\targs = append(args, "--include-directories", workDir)',
        "\t\t_ = workDir",
        "the child cannot read the workspace it was launched for",
    ),
    (
        "gemini-composition-admitted",
        f"{GEMINI}/gemini.go",
        '\treturn fmt.Errorf("gemini: unsupported launch composition; this system declares no composition grammar")',
        "\treturn nil",
        "a system with no composition grammar admits every prefix a caller hands it",
    ),
    (
        "gemini-unreadable-prompt-read-as-absent",
        f"{GEMINI}/gemini.go",
        '\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn agentic.StdinPayload{}, fmt.Errorf("gemini: reading the assignment prompt: %w", err)\n\t\t}',
        "\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn agentic.StdinPayload{}, nil\n\t\t}",
        "-p is empty by design, so a swallowed read failure launches a child with NO assignment in either place",
    ),
    # --- muse -----------------------------------------------------------------
    (
        "muse-also-streams-the-assignment",
        f"{MUSE}/muse.go",
        "func (*System) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {\n\treturn agentic.StdinPayload{}, nil\n}",
        'func (*System) Stdin(req agentic.LaunchRequest) (agentic.StdinPayload, error) {\n\tif req.PromptPath == "" {\n\t\treturn agentic.StdinPayload{}, nil\n\t}\n\treturn agentic.StdinPayload{Attached: true, Bytes: []byte("muse exec-mode prompt")}, nil\n}',
        "the child receives the assignment through two transports: the --prompt-file path AND stdin",
    ),
    (
        "muse-dry-run-placeholder-in-a-real-launch",
        f"{MUSE}/args.go",
        '\tcase agentic.LaunchModeExec:\n\tcase agentic.LaunchModeDryRun:\n\t\tif promptFile == "" {\n\t\t\tpromptFile = promptFilePlaceholder\n\t\t}',
        '\tcase agentic.LaunchModeExec, agentic.LaunchModeDryRun:\n\t\tif promptFile == "" {\n\t\t\tpromptFile = promptFilePlaceholder\n\t\t}',
        "a real launch names <prompt-file> as its assignment path and the child cannot open it",
    ),
    (
        "muse-composition-admitted",
        f"{MUSE}/muse.go",
        '\treturn fmt.Errorf("muse: unsupported launch composition; this system declares no composition grammar")',
        "\treturn nil",
        "a system with no composition grammar admits every prefix a caller hands it",
    ),
    # --- agy: the preflight gate and the ARG_MAX budget ------------------------
    (
        "agy-exec-without-a-preflight-admitted",
        f"{AGY}/agy.go",
        "\tif mode == agentic.LaunchModeExec && s.runtime.IsZero() {\n\t\treturn nil, notPreflighted()\n\t}\n",
        "",
        "an exec plan carries the bare `agy` placeholder; the launcher execs whatever PATH holds, which is a binary no probe validated",
    ),
    (
        "agy-placeholder-binary-always",
        f"{AGY}/agy.go",
        "\tif s.runtime.IsZero() {\n\t\treturn displayPlaceholder\n\t}\n\treturn trimmedExecutable(s.runtime)",
        "\treturn displayPlaceholder",
        "the source's own AC3 bug: BuildArgs hardcoded `agy` and never consulted the preflight evidence even when it was known",
    ),
    (
        "agy-whitespace-accepted-as-evidence",
        f"{AGY}/runtime.go",
        'func (r Runtime) IsZero() bool { return strings.TrimSpace(r.Executable) == "" }',
        'func (r Runtime) IsZero() bool { return strings.TrimRight(r.Executable, "") == "" }',
        "a Runtime carrying whitespace is treated as evidence and the launcher execs a path that does not exist",
    ),
    (
        "agy-argv-budget-removed",
        f"{AGY}/args.go",
        "\t\tif bytes := commandArgvBytes(binary, args); bytes > argvBudgetBytes {",
        "\t\tif bytes := commandArgvBytes(binary, args); false && bytes > argvBudgetBytes {",
        "an oversize prompt reaches exec and fails with E2BIG after the run exists, naming neither the prompt nor its length",
    ),
    (
        "agy-argv-budget-ignores-the-binary",
        f"{AGY}/args.go",
        "\ttotal := len(binary) + 1",
        "\ttotal := 0\n\t_ = binary",
        "NARROWING, not deleting: the budget still refuses an obviously-oversize prompt, but stops counting the executable path — so a command the kernel rejects is admitted whenever that path is long",
    ),
    (
        "agy-empty-assignment-admitted",
        f"{AGY}/args.go",
        '\treturn "", fmt.Errorf("agy: an exec launch carries its assignment in argv and this one has none; supply the prompt file or its bytes")',
        '\treturn "", nil',
        "the child is launched with `--print \"\"` and told to do nothing",
    ),
    (
        "agy-unreadable-assignment-read-as-empty",
        f"{AGY}/args.go",
        '\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn "", fmt.Errorf("agy: reading the assignment prompt: %w", err)\n\t\t}',
        '\t\tdata, err := os.ReadFile(path)\n\t\tif err != nil {\n\t\t\treturn "", nil\n\t\t}',
        "a failed read becomes an empty prompt: an absence and a failure to read are different facts",
    ),
    # --- the shared internals -------------------------------------------------
    (
        "runtimeenv-prefix-strip",
        RUNTIMEENV,
        "\t\tif _, drop := set[key]; !drop {\n\t\t\tresult = append(result, entry)\n\t\t}",
        "\t\tdropped := false\n\t\tfor blockedKey := range set {\n\t\t\tif strings.HasPrefix(key, blockedKey) {\n\t\t\t\tdropped = true\n\t\t\t}\n\t\t}\n\t\tif !dropped {\n\t\t\tresult = append(result, entry)\n\t\t}",
        "the codex family is stripped by PREFIX rather than by whole key, stealing CODEX_LIKE_BUT_NOT from every codex and qwen child",
    ),
    (
        "runtimeenv-credential-pointers-not-resolved",
        RUNTIMEENV,
        '\tif tokenEnvName := launchenv.Value(environ, AppServerTokenNameEnv); tokenEnvName != "" {\n\t\tblocked = append(blocked, tokenEnvName)\n\t}\n\tif tokenEnvName := launchenv.Value(environ, SessionManagerTokenNameEnv); tokenEnvName != "" {\n\t\tblocked = append(blocked, tokenEnvName)\n\t}\n',
        '\tif tokenEnvName := launchenv.Value(environ, AppServerTokenNameEnv); tokenEnvName != "" {\n\t\t_ = tokenEnvName\n\t}\n\tif tokenEnvName := launchenv.Value(environ, SessionManagerTokenNameEnv); tokenEnvName != "" {\n\t\t_ = tokenEnvName\n\t}\n',
        "the pointer variables are stripped but the credentials they NAME reach the child",
    ),
    (
        "runtimeenv-path-not-sanitized",
        RUNTIMEENV,
        "\treturn SanitizePath(FilterKeys(environ, blocked...))",
        "\treturn FilterKeys(environ, blocked...)",
        "the child keeps the parent runtime's arg0-shim PATH entries and resolves a wrapper whose parent is gone; no golden can see this",
    ),
    # --- the shared composition grammar ---------------------------------------
    (
        "mcpjson-second-top-level-argument-admitted",
        MCPJSON,
        "\tif len(prefix) != 2 || prefix[0] != ConfigFlag {",
        "\tif len(prefix) < 2 || prefix[0] != ConfigFlag {",
        "a reviewed MCP composition can smuggle a top-level flag — a model, a permission mode — past the validator",
    ),
    (
        "mcpjson-trailing-json-admitted",
        MCPJSON,
        '\tvar trailing any\n\tif err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {\n\t\treturn fmt.Errorf("trailing JSON content")\n\t}\n\treturn nil',
        "\tvar trailing any\n\t_ = decoder.Decode(&trailing)\n\t_, _ = errors.Is(nil, io.EOF), io.EOF\n\treturn nil",
        "a second JSON document after the validated one reaches the child unchecked",
    ),
    (
        "mcpjson-bearer-mismatch-admitted",
        MCPJSON,
        '\t\t\texpected := "Bearer ${" + server.BearerTokenEnvVar + "}"\n\t\t\tif len(declared.Headers) != 1 || declared.Headers["Authorization"] != expected {\n\t\t\t\treturn fmt.Errorf("invalid %s bearer environment reference", Grammar)\n\t\t\t}\n\t\t\tcontinue',
        "\t\t\tcontinue",
        "the child reads a credential the composition was not reviewed against",
    ),
    (
        "mcpjson-absent-bearer-headers-admitted",
        MCPJSON,
        '\t\t\tif server.BearerTokenEnvVar == "" {\n\t\t\t\tif len(declared.Headers) != 0 {\n\t\t\t\t\treturn fmt.Errorf("unexpected %s HTTP headers", Grammar)\n\t\t\t\t}\n\t\t\t\tcontinue\n\t\t\t}',
        '\t\t\tif server.BearerTokenEnvVar == "" {\n\t\t\t\tcontinue\n\t\t\t}',
        "a server whose metadata declares NO bearer carries an unreviewed Authorization header anyway",
    ),
    # --- structural: the guards themselves ------------------------------------
    (
        "second-argv-construction-site",
        "pkg/agentic/mutant_second_site.go",
        None,
        "package agentic\n\n// TEMPORARY MUTANT: a second place each of this task's four harnesses'\n// flags are spelled, one package over.\nfunc mutantSecondQwenSite() []string   { return []string{\"--approval-mode\", \"yolo\"} }\nfunc mutantSecondGeminiSite() []string { return []string{\"--skip-trust\"} }\nfunc mutantSecondMuseSite() []string   { return []string{\"--yolo\"} }\nfunc mutantSecondAgySite() []string    { return []string{\"--print-timeout\", \"30m\"} }\n",
        "a second construction site for every one of the four ported grammars",
    ),
    (
        "shadow-binding-table",
        f"{AGY}/mutant_shadow_table.go",
        None,
        'package agy\n\nimport "github.com/relux-works/skill-agents-management/pkg/agentic"\n\n// TEMPORARY MUTANT.\nvar mutantShadow = map[agentic.SystemID]string{}\n',
        "a second SystemID binding outside the registry",
    ),
    (
        "plugin-not-registered",
        f"{MUSE}/muse.go",
        '\tif err := agentic.Register(New()); err != nil {\n\t\tpanic(fmt.Sprintf("muse: registering the plugin: %v", err))\n\t}',
        "\t_ = agentic.Register",
        "a plugin that compiles, is registrABLE, and is reachable from no binary",
    ),
]

SUITE = ["go", "test", "-mod=mod", "./internal/...", "./pkg/...", "-count=1"]


def run_suite():
    env = {**os.environ}
    env.pop("TASK_BOARD_DIR", None)
    completed = subprocess.run(SUITE, cwd=ROOT, capture_output=True, text=True, env=env)
    return completed.returncode, completed.stdout + completed.stderr


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", default=str(pathlib.Path(__file__).parent))
    args = parser.parse_args()
    out = pathlib.Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    code, baseline = run_suite()
    (out / "mutants-baseline.log").write_text(baseline)
    if code != 0:
        print(f"BASELINE IS RED (exit {code}); every mutant below would be meaningless")
        print(baseline[-4000:])
        return 1

    failures = []
    for name, relpath, old, new, why in MUTANTS:
        path = ROOT / relpath
        created = old is None
        backup = None
        if created:
            path.write_text(new)
        else:
            with tempfile.NamedTemporaryFile(delete=False, suffix=".go") as handle:
                backup = pathlib.Path(handle.name)
            shutil.copy2(path, backup)
            body = path.read_text()
            if body.count(old) != 1:
                print(f"SKIP {name}: the anchor appears {body.count(old)} times, not once")
                failures.append(name)
                backup.unlink()
                continue
            path.write_text(body.replace(old, new))

        code, log = run_suite()
        (out / f"mutants-{name}.log").write_text(
            f"# mutant: {name}\n# defect: {why}\n# file: {relpath}\n# exit: {code}\n\n{log}"
        )

        if created:
            path.unlink()
        else:
            shutil.copy2(backup, path)
            if not filecmp.cmp(backup, path, shallow=False):
                print(f"RESTORE FAILED for {relpath}")
                return 1
            backup.unlink()

        caught = [line for line in log.splitlines() if line.startswith("--- FAIL") or line.startswith("    --- FAIL")]
        if code == 0:
            print(f"UNCAUGHT  {name:44s} exit=0  — {why}")
            failures.append(name)
        else:
            first = caught[0].strip() if caught else "(build failure)"
            print(f"caught    {name:44s} exit={code}  {first}")

    code, restored = run_suite()
    (out / "mutants-restored.log").write_text(restored)
    if code != 0:
        print(f"THE TREE DID NOT COME BACK GREEN (exit {code})")
        return 1

    if failures:
        print(f"\n{len(failures)} mutant(s) the suite admits: {', '.join(failures)}")
        return 1
    print(f"\nall {len(MUTANTS)} mutants caught; tree restored green")
    return 0


if __name__ == "__main__":
    sys.exit(main())
