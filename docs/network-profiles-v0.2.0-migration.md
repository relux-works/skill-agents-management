# Public network-profiles dependency

This module consumes `github.com/relux-works/curator-network-profiles v0.2.0`
through the public Go proxy and checksum database, without `replace` or
credentials. The release tag object is
`7e42e4a63e527b2ad09d9c7669e3a2ad704217da`, pointing to
`dac91d7365d67e16477710e5856e0e67a217f205`. The retired `v0.1.0` history is in
the private `curator-network-profiles-dev` repository.

## Used API comparison

Compared the old tag's peeled commit
`80ac8710c430af501e88055985611416fb83070e` with the public release. The module
imports `pkg/binding`, `pkg/envpatch` and `pkg/refusal`; their public signatures
and existing behavior remain compatible. `binding.Record` adds documentation
for the `inherited` origin, without changing its shape or constructor.
`envpatch.Generic.Patch` adds the direct-profile branch: the same unset family,
with no set entries. `refusal` changes only its license header. No production
API adaptation is needed in the launch plane.

The library also adds profile `kind = "direct"`, inherited selection precedence
and an additive JSON `kind` field. Catalog readers and selection are external
to this module. Its existing typed carrier passes inherited origins unchanged
and already handles unset-only managed patches; it does not infer either
selection. The v1 Record schema and exact adapter admission remain unchanged.
`TestBuildPlanPublicNetworkProfilesPreserveCarrierAndAdmission` drives public
normalization, patch creation and Record construction through both
`agentic.BuildPlan` and `agentic.BuildPlanWithEnvironment`, for proxy/direct
and explicit/inherited, including a neighbouring-build refusal in each case.

## Integration contract section 2.1 comparison

Read `docs/integration-contract.md` at the private development revision
`3960238fd9b5be61d876175a78480720139fcca9` through a read-only fetch, then
compared its complete section 2.1 with the public release. They are **not
byte-identical**. The task outcome contains both extracted sections and the
unified diff. Every changed group and its impact on
**TASK-261002-2b6xcp — codex-env-v1-network-adapter** is below.

| Changed group | Difference in v0.2.0 | Impact on the accepted Codex adapter |
| --- | --- | --- |
| Decision and task references | Private board IDs and tb-R148/tb-R151 references become operator decisions, descriptive task names and owner roles, including the placement and R7b gap references. | Editorial; no identity, admission or ownership change. |
| Claude verification | Specifies R0 startup only, one API-target CONNECT, six exact inherited MCP values, login exit 1, R2 zero sink requests and no transport refusal, R5 22 samples, and `claude -p` only. | Does not certify authenticated turns, interactive mode, hooks or tools; no Codex implementation change. |
| Codex transport evidence | Names API/startup CONNECT destinations, R0 startup only, R2 zero sink requests and supervisor SIGTERM/143 after 30 s, R5 zero non-loopback sockets over 315 samples; later retries remain uncertified. | No new routing requirement. Do not describe this as a natural terminal refusal or proof of indefinite absence of fallback. The accepted adapter's loader checks do not establish model traffic. |
| Codex child mitigation | Explicitly says R4 inherits none of the six values (including hostile values), while R4b explicit MCP env delivers all six. Generic patch plus per-entry set-half injection in launch-private config stays unchanged; shared config must not be rewritten. | The accepted per-entry injection mechanism remains required and compatible. Remove any inference of six-value mitigation from Muse evidence; Codex has its own R4b evidence. |
| Codex scope limits | Adds hooks to the unverified code-mode companion and tool-child scopes. | Retain all three as stated bounds. Acceptance of the MCP adapter does not certify them. |
| Muse status and source authority | Retains D8 supported-with-adapter, explicitly bounds unverified children, and replaces stale round-2 sources with R7 and later calibrated-detector evidence. | Muse support does not supply Codex evidence. No Codex allowlist expansion. |
| Muse MCP/hooks/launcher evidence | R4 fails both builds; R4b proves two values on 1.4.1 only. Hook evidence is 1.4.1 only; wrapping is required but unverified. R3 detailed variants are 1.4.1 only, with a later both-build summary; R5 is aggregate sampling. | No change to Codex's own six-value R4b or injection requirement. Do not generalize Muse mitigation or sampled absence to Codex. |
| Muse R6 model evidence | Identifies model-catalog fetch, one CONNECT and exit 1 on each build, dead-proxy exit 1, calibrated controls 137/7; not a completed model turn and not extra measurements for other rows. | Unrelated to Codex adapter correctness; cannot be used to claim Codex authenticated traffic or tool-child coverage. |

The normative Codex tuple remains `codex-cli 0.159.0`, `exec`,
`codex-env-v1`. Typed `Network{Patch, Record}`, application after `ChildEnv`,
Env/OwnedEnv parity, process-owner final overlays and exact verified tuples
are unchanged. The accepted adapter CR's recorded bounds already distinguish
loader resolution from model traffic and exclude companion/tool children;
the release text additionally makes hooks explicit. No semantic difference
requires reworking its injection implementation. This comparison does not
land that separate candidate or add its allowlist to this dependency bump.
