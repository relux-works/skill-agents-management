# Local Codex fixtures

`native-local-catalog.json` contains one complete public `gpt-6-luna` row from
`codex debug models --bundled` with Codex 0.159.0. Only these protocol flags
and the effort list were changed: Responses Lite off, direct tools,
multi-agent v1, search off, reasoning-summary parameter off, and low effort.
The test helpers change only the slug and declared effort list for their
synthetic local registrations. The native loading test uses a temporary
HOME/CODEX_HOME and skips when Codex 0.159.0 is unavailable.

Schema source:
https://github.com/openai/codex/blob/rust-v0.159.0/codex-rs/protocol/src/openai_models.rs
and its `openai_models/guardian_v2.rs`. `native-schema.json` freezes the
native required/defaulted fields, scalar types, enums and nested messages
used by the accepted local catalog subset. Unknown fields are ignored as
native serde does. Non-null guardian policies and access programs are outside
this subset and refuse rather than bypass native validation.

Key semantics follow native serde exactly: field names match case-sensitively,
a repeated recognized key is invalid at every scope (catalog, row, nested
message), and repeated unknown keys are ignored. Byte-surgery fixtures in
`exact_catalog_test.go` preserve these shapes; a map round-trip would erase
them, so parser-attack fixtures must never be built through one.

`additional-tools-request.json` retains the input-item structure from the
rejected Lite request. Long tool schemas and captured operator content were
removed; it contains no operator paths, credentials or personal identifiers.
