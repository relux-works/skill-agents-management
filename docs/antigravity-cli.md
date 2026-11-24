# Antigravity CLI launch grammar

The native interactive terminal session starts with `agy`. It waits for the first prompt inside the session. The CLI also accepts optional `--model` before opening the session. This plugin places its model row's ID after `--model` when one is selected, then forwards the caller's native arguments unchanged:

```text
agy [--model <model-id>] [native arguments...]
```

Interactive argv carries no assignment prompt or positional tail after `--`. The headless form is `agy -p "<prompt>"`; the plugin adds no `-p`, print, output-format, timeout, or other exec-only flags to the interactive argv. Native arguments that replace the selected model or select a headless or exec-only form are refused; the remaining native tail keeps its original order and values. The codelab does not document an interactive resume flag.

The source is Google's [Hands-on with Antigravity CLI](https://codelabs.developers.google.com/antigravity-cli-hands-on), which reports CLI version `1.1.27` at its last update. It was checked on 2026-11-24. Sections 3 and 5 describe the bare interactive launch and model selection; section 5 distinguishes `-p` as non-interactive mode. The model selection example uses the codelab's displayed model name; this plugin emits the ID from the selected model row.
