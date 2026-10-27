# Codex 0.164.0 permission qualification

TASK-261027-eukriv. Real installed binary; no model turn, credential copy, native-home edit, daemon restart or consumer launch. All Codex commands below set CODEX_HOME to the existing scratch directory `.tmp/codex-0164-home`; other native config overrides and XDG variables are cleared. Help validates CLI parsing, not execution.

## Result

- Admit exactly Codex `0.164.0` under `permission-grammar-v3`, `YoloSupported=true`.
- Map yolo to the existing canonical `--dangerously-bypass-approvals-and-sandbox`.
- Retain V2's closed CLI enums/config refusals. V3 refuses hidden policy aliases
  and compact attached approval/sandbox forms rather than guessing their interaction.
- Native contributes no permission flag. Stored-policy inspection is bounded
  observation, not effective-policy validation or permission admission.
- Unknown neighbours remain unverified. No 0.153.2 or other-provider row changes.

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --version
```

```text
codex-cli 0.164.0

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex exec --help
```

```text
Run Codex non-interactively

Usage: codex exec [OPTIONS] [PROMPT]
       codex exec [OPTIONS] <COMMAND> [ARGS]

Commands:
  resume  Resume a previous session by id or pick the most recent with --last
  fork    Fork a previous session by id into a new session
  review  Run a code review against the current repository
  help    Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Initial instructions for the agent. If not provided as an argument (or if `-` is used),
          instructions are read from stdin. If stdin is piped and a prompt is also provided, stdin
          is appended as a `<stdin>` block

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

      --thread-source <SOURCE>
          Source classification for newly created or forked threads

      --cyber-access-program <PROGRAM>
          Request an experimental Cyber access program for this turn (OpenAI provider only). Omit to
          use server defaults. Not supported with review; fork requires a prompt
          
          [possible values: standard, daybreak_blue, daybreak_red]

      --skip-git-repo-check
          Allow running Codex outside a Git repository

      --ephemeral
          Run without persisting session files to disk

      --ignore-user-config
          Do not load `$CODEX_HOME/config.toml`; auth still uses `CODEX_HOME`

      --ignore-rules
          Do not load user or project execpolicy `.rules` files

      --output-schema <FILE>
          Path to a JSON Schema file describing the model's final response shape

      --color <COLOR>
          Specifies color settings for use in the output
          
          [default: auto]
          [possible values: always, never, auto]

      --json
          Print events to stdout as JSONL

  -o, --output-last-message <FILE>
          Specifies file where the last message from the agent should be written

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex e --help
```

```text
Run Codex non-interactively

Usage: codex exec [OPTIONS] [PROMPT]
       codex exec [OPTIONS] <COMMAND> [ARGS]

Commands:
  resume  Resume a previous session by id or pick the most recent with --last
  fork    Fork a previous session by id into a new session
  review  Run a code review against the current repository
  help    Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Initial instructions for the agent. If not provided as an argument (or if `-` is used),
          instructions are read from stdin. If stdin is piped and a prompt is also provided, stdin
          is appended as a `<stdin>` block

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

      --thread-source <SOURCE>
          Source classification for newly created or forked threads

      --cyber-access-program <PROGRAM>
          Request an experimental Cyber access program for this turn (OpenAI provider only). Omit to
          use server defaults. Not supported with review; fork requires a prompt
          
          [possible values: standard, daybreak_blue, daybreak_red]

      --skip-git-repo-check
          Allow running Codex outside a Git repository

      --ephemeral
          Run without persisting session files to disk

      --ignore-user-config
          Do not load `$CODEX_HOME/config.toml`; auth still uses `CODEX_HOME`

      --ignore-rules
          Do not load user or project execpolicy `.rules` files

      --output-schema <FILE>
          Path to a JSON Schema file describing the model's final response shape

      --color <COLOR>
          Specifies color settings for use in the output
          
          [default: auto]
          [possible values: always, never, auto]

      --json
          Print events to stdout as JSONL

  -o, --output-last-message <FILE>
          Specifies file where the last message from the agent should be written

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex resume --help
```

```text
Resume a previous interactive session (picker by default; use --last to continue the most recent)

Usage: codex resume [OPTIONS] [SESSION_ID] [PROMPT]

Arguments:
  [SESSION_ID]
          Session id (UUID) or session name. UUIDs take precedence if it parses. If omitted, use
          --last to pick the most recent recorded session

  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --last
          Continue the most recent session without showing the picker

      --all
          Show all sessions (disables cwd filtering and shows CWD column)

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --include-non-interactive
          Include non-interactive sessions in the resume picker and --last selection

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex app-server generate-json-schema --help
```

```text
[experimental] Generate JSON Schema for the app server protocol

Usage: codex app-server generate-json-schema [OPTIONS] --out <DIR>

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

  -o, --out <DIR>
          Output directory where the schema bundle will be written

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --experimental
          Include experimental methods and fields in the generated output

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

  -h, --help
          Print help (see a summary with '-h')

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex app-server generate-json-schema --out .tmp/codex-0164-schema
```

```text

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -s read-only --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --sandbox=workspace-write --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -s danger-full-access --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --sandbox=unrestricted --help
```

```text
error: invalid value 'unrestricted' for '--sandbox <SANDBOX_MODE>'
  [possible values: read-only, workspace-write, danger-full-access]

For more information, try '--help'.

exit=2
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -a on-request --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --ask-for-approval=never --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -a untrusted --help
```

```text
error: invalid value 'untrusted' for '--ask-for-approval <APPROVAL_POLICY>'
  [possible values: on-request, never]

For more information, try '--help'.

exit=2
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -a on-failure --help
```

```text
error: invalid value 'on-failure' for '--ask-for-approval <APPROVAL_POLICY>'
  [possible values: on-request, never]

For more information, try '--help'.

exit=2
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --ask-for-approval=sometimes --help
```

```text
error: invalid value 'sometimes' for '--ask-for-approval <APPROVAL_POLICY>'
  [possible values: on-request, never]

For more information, try '--help'.

exit=2
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --dangerously-bypass-approvals-and-sandbox --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --yolo --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --approve-for-me --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --not-so-yolo --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --remote-auth-token exec --help
```

```text
error: unexpected argument '--remote-auth-token' found

  tip: a similar argument exists: '--remote-auth-token-env'

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

For more information, try '--help'.

exit=2
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --remote-auth-token-env EXAMPLE_AUTH_ENV --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --no-daemon --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --worktree --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy="never"' --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex '--config=sandbox_mode="workspace-write"' --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex '-csandbox_permissions=["disk-full-read-access"]' --help
```

```text
Codex CLI

If no subcommand is specified, options will be forwarded to the interactive CLI.

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]
          Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal.
          
          Examples: - `-c model="o3"` - `-c 'sandbox_permissions=["disk-full-read-access"]'` - `-c
          shell_environment_policy.inherit=all`

      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`

      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`

      --remote <ADDR>
          Connect the TUI to a remote app server endpoint.
          
          Accepted forms: `ws://host:port`, `wss://host:port`, `unix://`, or `unix://PATH`.

      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket

      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex

  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt

  -m, --model <MODEL>
          Model the agent should use

      --oss
          Use open-source provider

      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection

  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config

  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands
          
          [possible values: read-only, workspace-write, danger-full-access]

      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox

      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed

      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources

  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root

      --worktree
          Run the session in a new managed Git worktree

      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace

  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command

          Possible values:
          - on-request: The model decides when to ask the user for approval
          - never:      Never ask for user approval Execution failures are immediately returned to
            the model

      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)

      --no-alt-screen
          Disable alternate screen mode
          
          Runs the TUI in inline mode, preserving terminal scrollback history.

      --no-daemon
          Run without the shared background server, even if it is already running

  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy="on-request"' app-server --print-startup-config
```

```text
{"appServerVersion":"0.164.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy="never"' app-server --print-startup-config
```

```text
{"appServerVersion":"0.164.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy="untrusted"' app-server --print-startup-config
```

```text
{"error":{"code":-32600,"data":{"reason":"configLoad","filePath":null,"line":null,"column":null,"detail":"approval_policy = \"untrusted\" is no longer supported; remove this setting"},"message":"failed to load configuration: approval_policy = \"untrusted\" is no longer supported; remove this setting"}}
Error: startup configuration unavailable

exit=1
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy="on-failure"' app-server --print-startup-config
```

```text
{"appServerVersion":"0.164.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy="sometimes"' app-server --print-startup-config
```

```text
{"error":{"code":-32600,"data":{"reason":"configLoad","filePath":null,"line":null,"column":null,"detail":"unknown variant `sometimes`, expected one of `untrusted`, `on-failure`, `on-request`, `granular`, `never`\nin `approval_policy`\n"},"message":"failed to load configuration: unknown variant `sometimes`, expected one of `untrusted`, `on-failure`, `on-request`, `granular`, `never`\nin `approval_policy`\n"}}
Error: startup configuration unavailable

exit=1
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'sandbox_mode="read-only"' app-server --print-startup-config
```

```text
{"appServerVersion":"0.164.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'sandbox_mode="workspace-write"' app-server --print-startup-config
```

```text
{"appServerVersion":"0.164.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'sandbox_mode="danger-full-access"' app-server --print-startup-config
```

```text
{"appServerVersion":"0.164.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'sandbox_mode="unrestricted"' app-server --print-startup-config
```

```text
{"error":{"code":-32600,"data":{"reason":"configLoad","filePath":null,"line":null,"column":null,"detail":"unknown variant `unrestricted`, expected one of `read-only`, `workspace-write`, `danger-full-access`\nin `sandbox_mode`\n"},"message":"failed to load configuration: unknown variant `unrestricted`, expected one of `read-only`, `workspace-write`, `danger-full-access`\nin `sandbox_mode`\n"}}
Error: startup configuration unavailable

exit=1
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy={granular={sandbox_approval=true,rules=true,mcp_elicitations=true}}' app-server --print-startup-config
```

```text
{"appServerVersion":"0.164.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}

exit=0
```

## Command

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -c 'approval_policy={granular={}}' app-server --print-startup-config
```

```text
{"error":{"code":-32600,"data":{"reason":"configLoad","filePath":null,"line":null,"column":null,"detail":"missing field `sandbox_approval`\nin `approval_policy`\n"},"message":"failed to load configuration: missing field `sandbox_approval`\nin `approval_policy`\n"}}
Error: startup configuration unavailable

exit=1
```

## Binary-generated config schema extract

Exact extraction command (exit 0):

```sh
python3 - <<'PY'
import json
from pathlib import Path
s = json.loads(Path('.tmp/codex-0164-schema/v2/ConfigReadResponse.json').read_text())
print(json.dumps({k: s['definitions'][k] for k in ['AskForApproval', 'SandboxMode', 'ConfigLayerSource', 'SandboxWorkspaceWrite']}, indent=2, sort_keys=True))
PY
```

SHA256 of complete generated ConfigReadResponse.json: `69a32e3d84e16045802dff8abf01770d759ebc758965c3258a30ea4a93159fce`. This is the binary-generated config/read protocol schema, not an inferred complete config.toml schema. Config allows additional properties.

```json
{
  "AskForApproval": {
    "oneOf": [
      {
        "enum": [
          "untrusted",
          "on-request",
          "never"
        ],
        "type": "string"
      },
      {
        "additionalProperties": false,
        "properties": {
          "granular": {
            "properties": {
              "mcp_elicitations": {
                "type": "boolean"
              },
              "request_permissions": {
                "default": false,
                "type": "boolean"
              },
              "rules": {
                "type": "boolean"
              },
              "sandbox_approval": {
                "type": "boolean"
              },
              "skill_approval": {
                "default": false,
                "type": "boolean"
              }
            },
            "required": [
              "mcp_elicitations",
              "rules",
              "sandbox_approval"
            ],
            "type": "object"
          }
        },
        "required": [
          "granular"
        ],
        "title": "GranularAskForApproval",
        "type": "object"
      }
    ]
  },
  "ConfigLayerSource": {
    "oneOf": [
      {
        "description": "Default configuration supplied with the installed Codex package.",
        "properties": {
          "file": {
            "allOf": [
              {
                "$ref": "#/definitions/AbsolutePathBuf"
              }
            ],
            "description": "Path to the packaged default configuration file."
          },
          "type": {
            "enum": [
              "packagedDefaults"
            ],
            "title": "PackagedDefaultsConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "file",
          "type"
        ],
        "title": "PackagedDefaultsConfigLayerSource",
        "type": "object"
      },
      {
        "description": "Managed preferences layer delivered by MDM (macOS only).",
        "properties": {
          "domain": {
            "type": "string"
          },
          "key": {
            "type": "string"
          },
          "type": {
            "enum": [
              "mdm"
            ],
            "title": "MdmConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "domain",
          "key",
          "type"
        ],
        "title": "MdmConfigLayerSource",
        "type": "object"
      },
      {
        "description": "Managed config layer from a file (usually `managed_config.toml`).",
        "properties": {
          "file": {
            "allOf": [
              {
                "$ref": "#/definitions/AbsolutePathBuf"
              }
            ],
            "description": "This is the path to the system config.toml file, though it is not guaranteed to exist."
          },
          "type": {
            "enum": [
              "system"
            ],
            "title": "SystemConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "file",
          "type"
        ],
        "title": "SystemConfigLayerSource",
        "type": "object"
      },
      {
        "description": "Enterprise-managed config layer delivered by the cloud config bundle.",
        "properties": {
          "id": {
            "description": "Stable identifier for the delivered layer.",
            "type": "string"
          },
          "name": {
            "description": "Admin-facing name for the delivered layer. This is surfaced in diagnostics so users know which cloud layer needs administrator attention.",
            "type": "string"
          },
          "type": {
            "enum": [
              "enterpriseManaged"
            ],
            "title": "EnterpriseManagedConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "id",
          "name",
          "type"
        ],
        "title": "EnterpriseManagedConfigLayerSource",
        "type": "object"
      },
      {
        "description": "User config layer from $CODEX_HOME/config.toml. This layer is special in that it is expected to be: - writable by the user - generally outside the workspace directory",
        "properties": {
          "file": {
            "allOf": [
              {
                "$ref": "#/definitions/AbsolutePathBuf"
              }
            ],
            "description": "This is the path to the user's config.toml file, though it is not guaranteed to exist."
          },
          "profile": {
            "description": "Name of the selected profile-v2 config layered on top of the base user config, when this layer represents one.",
            "type": [
              "string",
              "null"
            ]
          },
          "type": {
            "enum": [
              "user"
            ],
            "title": "UserConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "file",
          "type"
        ],
        "title": "UserConfigLayerSource",
        "type": "object"
      },
      {
        "description": "Path to a .codex/ folder within a project. There could be multiple of these between `cwd` and the project/repo root.",
        "properties": {
          "dotCodexFolder": {
            "$ref": "#/definitions/AbsolutePathBuf"
          },
          "type": {
            "enum": [
              "project"
            ],
            "title": "ProjectConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "dotCodexFolder",
          "type"
        ],
        "title": "ProjectConfigLayerSource",
        "type": "object"
      },
      {
        "description": "Session-layer overrides supplied via `-c`/`--config`.",
        "properties": {
          "type": {
            "enum": [
              "sessionFlags"
            ],
            "title": "SessionFlagsConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "type"
        ],
        "title": "SessionFlagsConfigLayerSource",
        "type": "object"
      },
      {
        "description": "`managed_config.toml` was designed to be a config that was loaded as the last layer on top of everything else. This scheme did not quite work out as intended, but we keep this variant as a \"best effort\" while we phase out `managed_config.toml` in favor of `requirements.toml`.",
        "properties": {
          "file": {
            "$ref": "#/definitions/AbsolutePathBuf"
          },
          "type": {
            "enum": [
              "legacyManagedConfigTomlFromFile"
            ],
            "title": "LegacyManagedConfigTomlFromFileConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "file",
          "type"
        ],
        "title": "LegacyManagedConfigTomlFromFileConfigLayerSource",
        "type": "object"
      },
      {
        "properties": {
          "type": {
            "enum": [
              "legacyManagedConfigTomlFromMdm"
            ],
            "title": "LegacyManagedConfigTomlFromMdmConfigLayerSourceType",
            "type": "string"
          }
        },
        "required": [
          "type"
        ],
        "title": "LegacyManagedConfigTomlFromMdmConfigLayerSource",
        "type": "object"
      }
    ]
  },
  "SandboxMode": {
    "enum": [
      "read-only",
      "workspace-write",
      "danger-full-access"
    ],
    "type": "string"
  },
  "SandboxWorkspaceWrite": {
    "properties": {
      "exclude_slash_tmp": {
        "default": false,
        "type": "boolean"
      },
      "exclude_tmpdir_env_var": {
        "default": false,
        "type": "boolean"
      },
      "network_access": {
        "default": false,
        "type": "boolean"
      },
      "writable_roots": {
        "default": [],
        "items": {
          "type": "string"
        },
        "type": "array"
      }
    },
    "type": "object"
  }
}
```

## Qualification verdict and limits

| Surface | Observed 0.164.0 behavior | Module contract |
|---|---|---|
| CLI sandbox | `-s` / `--sandbox`: `read-only`, `workspace-write`, `danger-full-access`; invalid value exits 2 | V3 retains the closed V2 values; every explicit separate/equals sandbox selector conflicts with module yolo |
| CLI approval | `-a` / `--ask-for-approval`: `on-request`, `never`; `untrusted`, `on-failure`, `sometimes` exit 2 | V3 retains the closed V2 values; known values conflict, unknown values refuse as unknown |
| Bypass | Canonical `--dangerously-bypass-approvals-and-sandbox`: skip confirmation and execute without sandboxing; `--yolo` is accepted as a hidden alias | `YoloSupported=true`; emit only the existing canonical flag exactly once, before caller suffix |
| Automatic review | `--approve-for-me`: automatic review using workspace-write; hidden `--not-so-yolo` also parses | No new yolo mapping; existing V2 conflict rule for `--approve-for-me` retained; no claim that automatic review equals bypass |
| Config overrides | `-c`, `--config`, equals and short-attached syntax parse; values are TOML with raw-string fallback | V3 retains the closed V2 keys only; all `approval_policy`, `sandbox_mode`, `sandbox_permissions` overrides conflict with yolo, regardless of value; unlisted/dotted policy keys refuse as unknown |
| Classification | Root help declares `exec` and alias `e`; root scalar model/approval/sandbox options; `--` introduces prompt data | V3 retains the classifier subset: first command, including after known scalar/boolean root options; never classify option values or prompt text as commands |
| Root drift | `--remote-auth-token` exits 2; replacement `--remote-auth-token-env` and new `--no-daemon` / `--worktree` parse | Removed option is indeterminate at 0.164.0; new/unmodeled options remain indeterminate instead of guessing arity; 0.153.2 behavior untouched |
| Stored approval | Startup validation accepts `on-request`, `never`, legacy `on-failure` and a fully specified granular policy; rejects `untrusted` and incomplete granular despite schema representations | Inspector reports explicit `never` as relaxing; it is not a native config validator. Its historical recognition of `untrusted` and partial granular objects cannot establish successful native startup |
| Stored sandbox | Startup validation accepts three sandbox modes and rejects `unrestricted`; generated schema has the same enum | Inspector reports workspace-write/full-access as relaxing and read-only as restrictive |
| Sources | Root/exec help describes config at `CODEX_HOME/config.toml` and named `<profile>.config.toml`; generated schema separately names packaged, system, user, project, session, and managed layers | Existing inspector reads supplied home, working-directory config and selected profile only. Missing/unparseable sources and unavailable managed requirements stay not inspected; it neither resolves native precedence nor grants access |

The native permission flag spelling and two closed CLI enums match V2, but
V2 forwards hidden policy aliases and attached short approval/sandbox forms.
Parsing alone does not prove their interaction with the canonical bypass.
Therefore 0.164.0 selects new `permission-grammar-v3`: it retains the V2
conflict/config subset and refuses `--yolo`, `--not-so-yolo`, compact attached forms such as `-anever`
and `-sdanger-full-access` as `ErrNativePolicyUnknown`. No speculative alias semantics
or native argument rewriting is introduced. Existing 0.153.2 stays on V2.
New root forms whose arity is not modeled are refused by classification.
No other provider row or existing 0.153.2 control changes.

“Operator-selected normal access” is not a third permission mode. For the stated
keeper use case, an explicit operator selection of unrestricted/no-approval
access maps to `PermissionModeYolo` and the canonical bypass above. A selection
of `native` (or an absent selection) maps to no module-owned permission flag;
stored native policy decides access. Native does not promise network/process/
a2a/tmux access. Never infer yolo from a failure, role, home, profile or model.
The launcher still owns recording the explicit operator selection, and release
and consumer adoption are separate gates. This PR provides no keeper runtime
or network proof and changes no hook-trust policy.

The native config schema generated here is the binary's config/read protocol
schema, not a claim of a complete file schema. Its `Config` has additional
properties. In particular, schema-only acceptance of `untrusted` does not
supersede the real startup rejection above. Existing `sandbox_permissions`
inspection is a conservative raw-value signal; it makes no claim about every
native permission-profile form or effective rights. Inspecting files is not a
full native-policy validator or proof that a session can launch.

## Real binary config/read (no model turn)

Exact driver command: `python3 .temp/config-read-0164.py`. Driver source follows; stdin JSON and output are reproduced below. The temporary process is terminated by its own recorded PID after both replies.

```python
import json, os, subprocess, selectors, time
from pathlib import Path
root=Path.cwd()
env={k:v for k,v in os.environ.items() if not k.startswith('XDG_') and k not in ('CODEX_HOME','CLAUDE_CONFIG_DIR','GIT_DIR','GIT_WORK_TREE','SSH_AUTH_SOCK','GOFLAGS')}
env['CODEX_HOME']=str(root/'.tmp/codex-0164-home')
cmd=['codex','-c','approval_policy="never"','-c','sandbox_mode="danger-full-access"','app-server','--listen','stdio://']
requests=[{'id':1,'method':'initialize','params':{'clientInfo':{'name':'permission-qualification','version':'1'},'capabilities':{'experimentalApi':True}}},{'method':'initialized'},{'id':2,'method':'config/read','params':{'includeLayers':True,'cwd':str(root/'.tmp')}}]
log=[]
p=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,env=env)
log.append('Started own standalone app-server PID '+str(p.pid)+'. No daemon proxy or model request.')
sel=selectors.DefaultSelector();sel.register(p.stdout,selectors.EVENT_READ)
try:
 for req in requests:
  log.append('stdin: '+json.dumps(req))
  p.stdin.write(json.dumps(req)+'\n');p.stdin.flush()
  if 'id' not in req: continue
  deadline=time.monotonic()+20
  while time.monotonic()<deadline:
   if not sel.select(timeout=max(0,deadline-time.monotonic())): raise TimeoutError('no response')
   line=p.stdout.readline()
   if not line: raise RuntimeError('unexpected EOF')
   log.append('stdout: '+line.strip())
   if json.loads(line).get('id')==req['id']: break
  else: raise TimeoutError('response deadline')
finally:
 p.terminate()
 try: out,err=p.communicate(timeout=5)
 except subprocess.TimeoutExpired: p.kill();out,err=p.communicate()
 log+=['stdout tail: '+out,'stderr: '+err,'Stopped only own PID '+str(p.pid)+', exit='+str(p.returncode)]

```

```text
Started own standalone app-server PID 38878. No daemon proxy or model request.
stdin: {"id": 1, "method": "initialize", "params": {"clientInfo": {"name": "permission-qualification", "version": "1"}, "capabilities": {"experimentalApi": true}}}
stdout: {"id":1,"result":{"userAgent":"permission-qualification/0.164.0 (Mac OS 27.0.1; arm64) dumb (permission-qualification; 1)","codexHome":"/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/codex-0164-home","platformFamily":"unix","platformOs":"macos"}}
stdin: {"method": "initialized"}
stdin: {"id": 2, "method": "config/read", "params": {"includeLayers": true, "cwd": "/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp"}}
stdout: {"method":"remoteControl/status/changed","params":{"status":"disabled","serverName":"<redacted host>","installationId":"<redacted installation identifier>","environmentId":null},"emittedAtMs":1793092835865}
stdout: {"id":2,"result":{"config":{"model":null,"review_model":null,"model_context_window":null,"model_auto_compact_token_limit":null,"model_auto_compact_token_limit_scope":null,"model_provider":null,"approval_policy":"never","approvals_reviewer":null,"sandbox_mode":"danger-full-access","sandbox_workspace_write":null,"forced_chatgpt_workspace_id":null,"forced_login_method":null,"web_search":null,"tools":null,"instructions":null,"developer_instructions":null,"compact_prompt":null,"model_reasoning_effort":null,"model_reasoning_summary":null,"model_verbosity":null,"service_tier":null,"analytics":null,"apps":null,"browser_use":null,"computer_use":null,"desktop":null,"goals":null,"profile":null,"model_catalog_json":null,"check_for_update_on_startup":null,"file_opener":"vscode","openai_base_url":null,"plan_mode_reasoning_effort":null,"hide_agent_reasoning":false,"suppress_unstable_features_warning":null,"orchestrator":null,"daybreak":null,"mcp_enterprise_managed_auth":null,"default_permissions":null,"log_dir":null,"personality":null,"apps_mcp_product_sku":null,"cloud":null,"experimental_realtime_ws_backend_prompt":null,"js_repl_node_path":null,"project_doc_fallback_filenames":[],"allow_symlinked_codex_home":null,"experimental_realtime_start_instructions":null,"experimental_realtime_ws_model":null,"include_environment_context_time":true,"memories":null,"background_terminal_max_timeout":300000,"include_permissions_instructions":true,"history":{"persistence":"save-all","max_bytes":null},"projects":null,"audio":null,"features":{"api_key_cyber_access_programs":true,"api_key_model_discovery":true,"auth_elicitation":true,"background_paginated_rollout_migration":false,"codex_apps_mcp_2026_07_28":false,"mcp_2026_07_28":false,"memories":false,"mentions_v2":true,"remote_control":false,"remote_plugin":true,"tool_suggest":true,"windows_sandbox_service":false},"notify":null,"mcp_oauth_callback_port":null,"ghost_snapshot":null,"responses_api_metadata":null,"otel":null,"plugins":{},"skills":null,"tui":null,"experimental_thread_store":null,"marketplaces":{},"notice":null,"windows":null,"auto_review":null,"profiles":{},"sqlite_home":null,"mcp_oauth_callback_url":null,"project_root_markers":[".git"],"project_doc_max_bytes":32768,"include_apps_instructions":true,"hooks":null,"model_post_turn_compact_threshold_percent":null,"experimental_compact_prompt_file":null,"realtime":null,"tool_suggest":null,"model_providers":{},"include_collaboration_mode_instructions":true,"agents":null,"allow_login_shell":true,"include_environment_context":true,"model_instructions_file":null,"experimental_realtime_webrtc_call_base_url":null,"oss_provider":null,"js_repl_node_module_dirs":null,"shell_environment_policy":{"inherit":null,"ignore_default_excludes":null,"exclude":null,"set":null,"include_only":null,"filters":null,"experimental_use_profile":null},"thread_unload_delay_secs":null,"mcp_servers":{},"disable_paste_burst":null,"experimental_use_unified_exec_tool":null,"experimental_realtime_ws_startup_context":null,"permissions":null,"experimental_realtime_ws_base_url":null,"cli_auth_credentials_store":"file","tool_output_token_limit":null,"chatgpt_base_url":"https://chatgpt.com/backend-api/","feedback":null,"mcp_oauth_credentials_store":"auto","show_raw_agent_reasoning":null,"mcp_optional_startup_grace_ms":null,"experimental_thread_store_endpoint":null},"origins":{"approval_policy":{"name":{"type":"sessionFlags"},"version":"sha256:6991964181dfa19472f967488746e3fd193513ac4ca3a4bbcbcd47fccfc184ef"},"sandbox_mode":{"name":{"type":"sessionFlags"},"version":"sha256:6991964181dfa19472f967488746e3fd193513ac4ca3a4bbcbcd47fccfc184ef"}},"layers":[{"name":{"type":"sessionFlags"},"version":"sha256:6991964181dfa19472f967488746e3fd193513ac4ca3a4bbcbcd47fccfc184ef","config":{"approval_policy":"never","sandbox_mode":"danger-full-access"}},{"name":{"type":"user","file":"/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/codex-0164-home/config.toml","profile":null},"version":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","config":{}},{"name":{"type":"system","file":"/etc/codex/config.toml"},"version":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","config":{}}]}}
stdout tail: 
stderr: 
Stopped only own PID 38878, exit=0
```

Redactions: the unsolicited remoteControl/status notification above has its host and installation identifier redacted; no permission/config result is redacted.

## Additional parsing control

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -anever -h
```

```text
Codex CLI

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]  Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal
      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`
      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`
      --remote <ADDR>
          Connect the TUI to a remote app server endpoint
      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket
      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex
  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt
  -m, --model <MODEL>
          Model the agent should use
      --oss
          Use open-source provider
      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection
  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config
  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands [possible
          values: read-only, workspace-write, danger-full-access]
      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox
      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed
      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources
  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root
      --worktree
          Run the session in a new managed Git worktree
      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace
  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command [possible
          values: on-request, never]
      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)
      --no-alt-screen
          Disable alternate screen mode
      --no-daemon
          Run without the shared background server, even if it is already running
  -h, --help
          Print help (see more with '--help')
  -V, --version
          Print version

exit=0
```

## Additional parsing control

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex -sdanger-full-access -h
```

```text
Codex CLI

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]  Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal
      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`
      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`
      --remote <ADDR>
          Connect the TUI to a remote app server endpoint
      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket
      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex
  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt
  -m, --model <MODEL>
          Model the agent should use
      --oss
          Use open-source provider
      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection
  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config
  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands [possible
          values: read-only, workspace-write, danger-full-access]
      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox
      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed
      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources
  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root
      --worktree
          Run the session in a new managed Git worktree
      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace
  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command [possible
          values: on-request, never]
      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)
      --no-alt-screen
          Disable alternate screen mode
      --no-daemon
          Run without the shared background server, even if it is already running
  -h, --help
          Print help (see more with '--help')
  -V, --version
          Print version

exit=0
```

## Additional parsing control

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --yolo -h
```

```text
Codex CLI

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]  Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal
      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`
      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`
      --remote <ADDR>
          Connect the TUI to a remote app server endpoint
      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket
      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex
  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt
  -m, --model <MODEL>
          Model the agent should use
      --oss
          Use open-source provider
      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection
  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config
  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands [possible
          values: read-only, workspace-write, danger-full-access]
      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox
      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed
      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources
  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root
      --worktree
          Run the session in a new managed Git worktree
      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace
  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command [possible
          values: on-request, never]
      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)
      --no-alt-screen
          Disable alternate screen mode
      --no-daemon
          Run without the shared background server, even if it is already running
  -h, --help
          Print help (see more with '--help')
  -V, --version
          Print version

exit=0
```

## Additional parsing control

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex --not-so-yolo -h
```

```text
Codex CLI

Usage: codex [OPTIONS] [PROMPT]
       codex [OPTIONS] <COMMAND> [ARGS]

Commands:
  agents            Browse all agent sessions on the shared local app-server daemon
  exec              Run Codex non-interactively [aliases: e]
  review            Run a code review non-interactively
  login             Manage login
  logout            Remove stored authentication credentials
  mcp               Manage external MCP servers for Codex
  plugin            Manage Codex plugins
  app-server        [experimental] Run the app server or related tooling
  remote-control    [experimental] Manage the app-server daemon with remote control enabled
  app               Launch the Desktop app (opens the app installer if missing)
  completion        Generate shell completion scripts
  update            Update Codex to the latest version
  doctor            Diagnose local Codex installation, config, auth, and runtime health
  sandbox           Run commands within a Codex-provided sandbox
  debug             Debugging tools
  apply             Apply the latest diff produced by Codex agent as a `git apply` to your local
                    working tree [aliases: a]
  resume            Resume a previous interactive session (picker by default; use --last to continue
                    the most recent)
  queue             Queue a message for an existing session
  archive           Archive a saved session by id or session name
  delete            Permanently delete a saved session by id or session name
  migrate-rollouts  Inspect or migrate legacy local sessions to paginated thread history
  migrate-nfs-db    [experimental] Migrate configured SQLite databases to DELETE mode in place
  unarchive         Unarchive a saved session by id or session name
  fork              Fork a previous interactive session (picker by default; use --last to fork the
                    most recent)
  cloud             [EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally
  exec-server       [EXPERIMENTAL] Run the standalone exec-server service
  features          Inspect feature flags
  help              Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]  Optional user prompt to start the session

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal
      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`
      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`
      --remote <ADDR>
          Connect the TUI to a remote app server endpoint
      --remote-auth-token-env <ENV_VAR>
          Name of the environment variable containing the bearer token to send to a remote app
          server websocket
      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex
  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt
  -m, --model <MODEL>
          Model the agent should use
      --oss
          Use open-source provider
      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection
  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config
  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands [possible
          values: read-only, workspace-write, danger-full-access]
      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox
      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed
      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources
  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root
      --worktree
          Run the session in a new managed Git worktree
      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace
  -a, --ask-for-approval <APPROVAL_POLICY>
          Configure when the model requires human approval before executing a command [possible
          values: on-request, never]
      --search
          Enable live web search. When enabled, the native Responses `web_search` tool is available
          to the model (no per‑call approval)
      --no-alt-screen
          Disable alternate screen mode
      --no-daemon
          Run without the shared background server, even if it is already running
  -h, --help
          Print help (see more with '--help')
  -V, --version
          Print version

exit=0
```

## Additional parsing control

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex exec --sandbox=read-only -h
```

```text
Run Codex non-interactively

Usage: codex exec [OPTIONS] [PROMPT]
       codex exec [OPTIONS] <COMMAND> [ARGS]

Commands:
  resume  Resume a previous session by id or pick the most recent with --last
  fork    Fork a previous session by id into a new session
  review  Run a code review against the current repository
  help    Print this message or the help of the given subcommand(s)

Arguments:
  [PROMPT]  Initial instructions for the agent. If not provided as an argument (or if `-` is used),
            instructions are read from stdin. If stdin is piped and a prompt is also provided, stdin
            is appended as a `<stdin>` block

Options:
  -c, --config <key=value>
          Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`.
          Use a dotted path (`foo.bar.baz`) to override nested values. The `value` portion is parsed
          as TOML. If it fails to parse as TOML, the raw string is used as a literal
      --enable <FEATURE>
          Enable a feature (repeatable). Equivalent to `-c features.<name>=true`
      --disable <FEATURE>
          Disable a feature (repeatable). Equivalent to `-c features.<name>=false`
      --strict-config
          Error out when config.toml contains fields that are not recognized by this version of
          Codex
  -i, --image <FILE>...
          Optional image(s) to attach to the initial prompt
  -m, --model <MODEL>
          Model the agent should use
      --oss
          Use open-source provider
      --local-provider <OSS_PROVIDER>
          Specify which local provider to use (lmstudio or ollama). If not specified with --oss,
          will use config default or show selection
  -p, --profile <CONFIG_PROFILE_V2>
          Layer $CODEX_HOME/<name>.config.toml on top of the base user config
  -s, --sandbox <SANDBOX_MODE>
          Select the sandbox policy to use when executing model-generated shell commands [possible
          values: read-only, workspace-write, danger-full-access]
      --approve-for-me
          Route approval requests through automatic review using the workspace-write sandbox
      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing. EXTREMELY
          DANGEROUS. Intended solely for running in environments that are externally sandboxed
      --dangerously-bypass-hook-trust
          Run enabled hooks without requiring persisted hook trust for this invocation. DANGEROUS.
          Intended only for automation that already vets hook sources
  -C, --cd <DIR>
          Tell the agent to use the specified directory as its working root
      --worktree
          Run the session in a new managed Git worktree
      --add-dir <DIR>
          Additional directories that should be writable alongside the primary workspace
      --thread-source <SOURCE>
          Source classification for newly created or forked threads
      --cyber-access-program <PROGRAM>
          Request an experimental Cyber access program for this turn (OpenAI provider only). Omit to
          use server defaults. Not supported with review; fork requires a prompt [possible values:
          standard, daybreak_blue, daybreak_red]
      --skip-git-repo-check
          Allow running Codex outside a Git repository
      --ephemeral
          Run without persisting session files to disk
      --ignore-user-config
          Do not load `$CODEX_HOME/config.toml`; auth still uses `CODEX_HOME`
      --ignore-rules
          Do not load user or project execpolicy `.rules` files
      --output-schema <FILE>
          Path to a JSON Schema file describing the model's final response shape
      --color <COLOR>
          Specifies color settings for use in the output [default: auto] [possible values: always,
          never, auto]
      --json
          Print events to stdout as JSONL
  -o, --output-last-message <FILE>
          Specifies file where the last message from the agent should be written
  -h, --help
          Print help (see more with '--help')
  -V, --version
          Print version

exit=0
```

## Additional parsing control

```sh
CODEX_HOME="$PWD/.tmp/codex-0164-home" codex exec -a never -h
```

```text
error: unexpected argument '-a' found

  tip: to pass '-a' as a value, use '-- -a'

Usage: codex exec [OPTIONS] [PROMPT]
       codex exec [OPTIONS] <COMMAND> [ARGS]

For more information, try '--help'.

exit=2
```

## Binary identity

Exact checksum command: `python3 -c 'import hashlib; from pathlib import Path; print(hashlib.sha256(Path("/Users/iv/.local/bin/codex").resolve().read_bytes()).hexdigest())'`

```text
d8990e3ee7acfbc26f91265b12a1a83f0fee40d0e0f8f1f40a37d4ec21f499cf
exit=0
```

## Planner verification controls

The tests use the repository's existing plan-only executable stub helpers;
those stubs never supply qualification evidence. Real-binary observations above
ran the installed executable, with the actual reported version and a scratch
CODEX_HOME. No native version, native auth or caller home was fabricated to
pass the consumer gate.

Two bounded controls used Go source overlays, leaving the working source bytes
unchanged: removing only the 0.164.0 row caused the mapping tests to fail with
the unverified-release refusal; disabling only the V3 additional policy guard
caused the five alias/compact-short refusal assertions to fail with nil errors.
Both controls exited 1 for the intended test failures, not setup or compilation.

### codex-focused-04.log

```text
$ env -u GIT_DIR -u GIT_WORK_TREE -u SSH_AUTH_SOCK -u GOFLAGS -u CODEX_HOME -u CLAUDE_CONFIG_DIR -u TASK_BOARD_DIR HOME=/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/isolated-home-j0c7oomf go test -mod=mod ./pkg/agentic ./pkg/agentic/systems/codex -run 'TestCodex0164|TestLookupReleaseCapability|TestCuratorRefusal' -count=1
ok  	github.com/relux-works/skill-agents-management/pkg/agentic	0.865s
ok  	github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex	6.595s

exit=0
```

### codex-control-row-01.log

```text
$ env -u GIT_DIR -u GIT_WORK_TREE -u SSH_AUTH_SOCK -u GOFLAGS -u CODEX_HOME -u CLAUDE_CONFIG_DIR -u TASK_BOARD_DIR HOME=/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/isolated-home-9i4t6nt7 go test -mod=mod -overlay=/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/codex-control-0.json ./pkg/agentic/systems/codex -run '^TestCodex0164PermissionMappingAndArgv$' -count=1
--- FAIL: TestCodex0164PermissionMappingAndArgv (0.00s)
    --- FAIL: TestCodex0164PermissionMappingAndArgv/#00 (0.00s)
        policy_0164_test.go:21: codex: agentic: tool release is not a verified permission-grammar row: tool release "0.164.0" is not verified under permission-grammar-v1, permission-grammar-v2 or permission-grammar-v3 (verified releases: 0.153.2); yolo fails closed on drift, while native forwards verbatim
    --- FAIL: TestCodex0164PermissionMappingAndArgv/native (0.00s)
        policy_0164_test.go:21: codex: agentic: tool release is not a verified permission-grammar row: tool release "0.164.0" is not verified under permission-grammar-v1, permission-grammar-v2 or permission-grammar-v3 (verified releases: 0.153.2); yolo fails closed on drift, while native forwards verbatim
    --- FAIL: TestCodex0164PermissionMappingAndArgv/yolo (0.00s)
        policy_0164_test.go:21: codex: agentic: tool release is not a verified permission-grammar row: tool release "0.164.0" is not verified under permission-grammar-v1, permission-grammar-v2 or permission-grammar-v3 (verified releases: 0.153.2); yolo fails closed on drift, while native forwards verbatim
FAIL
FAIL	github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex	0.293s
FAIL

exit=1
```

### codex-control-alias-01.log

```text
$ env -u GIT_DIR -u GIT_WORK_TREE -u SSH_AUTH_SOCK -u GOFLAGS -u CODEX_HOME -u CLAUDE_CONFIG_DIR -u TASK_BOARD_DIR HOME=/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/isolated-home-2v3ada4z go test -mod=mod -overlay=/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/codex-control-1.json ./pkg/agentic/systems/codex -run '^TestCodex0164ClosedOverrideGrammar$' -count=1
--- FAIL: TestCodex0164ClosedOverrideGrammar (2.86s)
    --- FAIL: TestCodex0164ClosedOverrideGrammar/--yolo (0.04s)
        policy_0164_test.go:135: err = <nil>, want agentic: unknown native policy form
    --- FAIL: TestCodex0164ClosedOverrideGrammar/--not-so-yolo (0.13s)
        policy_0164_test.go:135: err = <nil>, want agentic: unknown native policy form
    --- FAIL: TestCodex0164ClosedOverrideGrammar/-anever (0.06s)
        policy_0164_test.go:135: err = <nil>, want agentic: unknown native policy form
    --- FAIL: TestCodex0164ClosedOverrideGrammar/-awhatever (0.11s)
        policy_0164_test.go:135: err = <nil>, want agentic: unknown native policy form
    --- FAIL: TestCodex0164ClosedOverrideGrammar/-sdanger-full-access (0.13s)
        policy_0164_test.go:135: err = <nil>, want agentic: unknown native policy form
FAIL
FAIL	github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex	3.177s
FAIL

exit=1
```


## Final scoped source-guard verification

The context-owned permission route is covered by a named BuildPlan test,
and the shared Muse argv guard explicitly classifies the Codex alias scanner
as comparison-only. No Muse release or permission row changed.

```text
$ env -u GIT_DIR -u GIT_WORK_TREE -u SSH_AUTH_SOCK -u GOFLAGS -u CODEX_HOME -u CLAUDE_CONFIG_DIR -u TASK_BOARD_DIR HOME=/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/isolated-home-31xf9nk9 go test -mod=mod ./pkg/agentic ./pkg/agentic/systems/codex -run 'TestCodex0164|TestLookupReleaseCapability|TestRegistryPermissionMapping|TestNonInteractive|TestCuratorPluginRefusalSitesHaveNamedBuildPlanCoverage' -count=1
ok  	github.com/relux-works/skill-agents-management/pkg/agentic	0.876s
ok  	github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex	3.715s

exit=0
```

```text
$ env -u GIT_DIR -u GIT_WORK_TREE -u SSH_AUTH_SOCK -u GOFLAGS -u CODEX_HOME -u CLAUDE_CONFIG_DIR -u TASK_BOARD_DIR HOME=/Users/iv/Werkstatt/curator-dev5/skill-agents-management/.tmp/isolated-home-3yx2yb7f go test -mod=mod ./pkg/agentic/systems/muse -run 'TestMuseArgvHasExactlyOneConstructionSite|TestTheMuseGuardDoesNotFireOnTheOtherPlugins' -count=1
ok  	github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse	0.439s

exit=0
```

The first full test attempt is not passing evidence: ambient Git metadata was
removed as required, but this Werkstatt working tree has no local `.git`;
Git-aware tests refused. It also caught an intermediate typed-refusal scope
issue and the shared alias-scanner inventory, both corrected and checked above.
The changelog package exceeded the default 10-minute timeout. Final full
validation uses a real task-scoped Git worktree with the same isolated env,
and a larger test timeout rather than changing tests or native settings.
