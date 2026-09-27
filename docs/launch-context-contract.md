# Launch Context Bridge Contract

- **Document version:** v1.0.0
- **Status:** planned, not shipped. The current LaunchRequest has no typed Curator context yet; a follow-on consumer slice implements it.
- **Curator input:** `launch-env-fragment-v1` from `curator env resolve … --format json`.
- **Consumers:** task-board for tracked child launches; the interactive Curator path carries equivalent provenance in its composed plan to a session host.

## Version notes

v1.0.0 closes the contract gaps by pinning the boundary between a validated Curator fragment and typed launch-context descriptors, the provenance that child and session records must preserve, and the refusal conditions for incomplete, stale, or unsupported context.

This version deliberately leaves implementation to the next consumer slices. It does not specify the session host's entry-point mapping: the Claude PTY and Codex app-server mapping belongs to the session-host stream. The Decision 0020 draft remains in curator-spec issue #104 and is not copied here.

## 1. Ownership

Curator resolves the selected environment profile and emits a validated launch fragment. The `agents-management` launch plane accepts typed context data and is the single place that spells harness arguments and environment changes. task-board remains responsible for tracked-child process lifecycle, task prompt, worktree, deadline, cancellation, run record, and review routing. It passes context descriptors through the module; it does not interpret them into harness-specific flags.

For a primary interactive session, `curator run` resolves and composes the launch. A session host receives the composed plan and hosts it. The host does not resolve a Curator fragment, reconstruct its identity, or spell context channels. The host-entry mapping and transport details belong to the session-host contract and are outside this document.

## 2. Typed request contract

For a Curator-backed launch, the consumer validates the complete fragment against its declared schema, then supplies a typed value to the module's launch request:

- `Home` is the exact absolute path from the fragment's single managed-home environment entry. It is also the `Home` used by the module's launch plan; an independently inferred native home is not equivalent.
- The Curator profile name is context provenance. It is distinct from the module's existing harness-side `LaunchRequest.Profile` value.
- `Context.MCP`, when present, carries the fragment's MCP file path, sorted `env_names`, and typed descriptor list.
- `Context.SystemPrompt`, when present, carries the fragment's system-prompt file path and typed descriptor list. These descriptors also preserve their `append` or `replace` semantics.
- Every descriptor is a closed tagged union matching the Curator schema: `flag`, `config-key`, `variable`, or `file`. A `flag` preserves its declared argument kind, optional name, and companion flags; the other variants preserve their schema-defined key, variable, or filename.
- The typed context contains only resolved values needed to build the launch. It does not carry a second copy of Curator's profile contents or ask the module to resolve the profile.

The resulting launch plan exposes a typed, immutable context-provenance snapshot derived from the validated request, alongside its `Home`. The consumer copies that snapshot to its local run record. In the primary-session path, the composed plan carries the same snapshot to the host for persistence.

The Curator fragment remains the source of descriptor meaning. The module's registered system plugin interprets a supported descriptor for its declared launch mode and emits the corresponding launch surface. Consumers pass the typed descriptor without rewriting its semantics or generating a parallel argv spelling. Channel arguments are constructed once by the module; consumer-owned native arguments retain their defined order after the plan's channel arguments.

`precedence` remains Curator-resolved profile data. Consumers validate it as part of the closed fragment schema; they do not recompose profile overlays. Any other schema field that can affect a launch must either have an explicit typed mapping and capability or cause refusal.

## 3. Provenance and identity

The private run or session record preserves these values for every Curator-backed launch, copied from the launch plan's context-provenance snapshot:

1. Curator profile name.
2. The profile's `lock_sha256` exactly as emitted.
3. Managed-home environment variable and its resolved absolute path.
4. Fragment identity: the revision token, environment, and complete validated fragment value, including its profile pin, managed home, paths, and channel descriptors.

The v1 fragment has no separate fragment-instance ID. Consumers must not invent one from the profile name alone. They persist the structured identity above and compare it with a fresh resolution before resuming or otherwise reusing a launch record. Any mismatch in profile, lock, managed home, fragment revision, or descriptor-bearing fragment value is stale identity and refuses reuse. Primary-session composition carries the same provenance through the composed-plan metadata; the session host persists it without resolving the fragment again.

The absolute managed-home and fragment paths are local operational evidence. They stay in the private run/session record and are redacted from board text, public status/API projections, and general-purpose logs. No credential value is part of this provenance.

## 4. Refusal contract

A Curator-backed launch fails before process creation or session hosting in each of these cases:

| Condition | Required behavior |
| --- | --- |
| **Missing profile** | Refuse when Curator context was selected but the fragment lacks a profile name or valid lock pin, or profile resolution did not produce one. |
| **Unknown descriptor** | Refuse an unknown fragment revision, field, descriptor kind, semantics, argument kind, or descriptor member. Do not ignore it or pass it through as an opaque flag. |
| **Malformed fragment** | Refuse invalid JSON, schema violations, invalid paths, inconsistent identity fields, or an incomplete read. A command/read failure is not absence and never activates a fallback. |
| **Stale identity** | Refuse resume/reuse when a fresh fragment does not match the stored profile, lock, managed home, revision, or descriptor-bearing fragment value. |
| **Incompatible capability** | Refuse when a valid known descriptor or fragment feature is not supported by the selected system and launch mode. Do not drop it, approximate it, or launch with a different context. |

A caller may bypass Curator only through an explicit `source=none` choice. In that case no fragment is expected and no Curator provenance is claimed. An absent setting, a missing executable, a failed read, invalid JSON, or unsupported capability is not equivalent to `source=none`.

The public Curator protocol also defines `launch-env-fragment-v2`, which adds a required permissions member. A consumer may accept that revision only after it has typed support for the full revision and the requested launch mode. Until then, it refuses v2 as an incompatible capability; it must not parse it as v1 or discard the permissions member. The same rule applies to later fragment revisions and fields such as a reserved path transform until their capability is explicitly supported.

## 5. Compatibility and scope

This contract covers the fragment-to-typed-context boundary, local child/session provenance, and fail-closed behavior. It does not define runtime catalog policy, provider/model selection, permission policy, engine lifecycle, a headless Curator entry point, host argv splitting, or how a session host maps an entry point to a PTY or app-server.

The existing task-board launch-composition path is separate legacy behavior. Its presence is not evidence that this contract is implemented. The next consumer slice must wire the real tracked-child production launch through the typed module request and persist the provenance above before claiming conformance. The primary-session slice follows the composed-plan ownership described in section 1.

## Curator-spec follow-up references

- curator-spec #102 — review adoption of Decision 0019.
- curator-spec #103 — review adoption of Decision 0021.
- curator-spec #104 — Decision 0020 draft handed over; the draft remains with curator-spec.
- curator-spec #105 — reconcile Decision 0014 by recording its selected option before adoption.

## References

- [Curator environment protocol, §10.2](https://github.com/relux-works/curator-spec/blob/main/protocol/environments.md#102-the-launch-environment-fragment)
- [Curator `launch-env-fragment-v1` schema](https://github.com/relux-works/curator-spec/blob/main/schemas/v1/launch-env-fragment-v1.schema.json)
- [Curator Decision 0019: fragment consumers and one construction site](https://github.com/relux-works/curator-spec/blob/main/decisions/0019-fragment-consumers-and-one-construction-site.md)
- [Curator Decision 0021: sessions enter through `curator run`](https://github.com/relux-works/curator-spec/blob/main/decisions/0021-sessions-enter-through-curator-run.md)
- [agents-management architecture](https://github.com/relux-works/skill-agents-management/blob/main/docs/architecture.md)
