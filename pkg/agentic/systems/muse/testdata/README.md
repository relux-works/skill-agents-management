`quota-synthetic.json` is synthetic, built from L1 §1.3 and the offline stable
MSP schema. No account or live session was consulted to construct it.

`quota-fresh-host.jsonl` transcribes the logged Muse 1.4.2 echo-provider probe
supplied in the W3 Muse update brief. The home is redacted; the build hash is
public build metadata. It demonstrates JSONL framing and absent usage.

`quota-schema.json` pins the parser's definitions from the stable Muse 1.4.2
export; `quota-schema-manifest.json` pins its fingerprint. SubscriptionUsage
and UsageReadResult are unchanged from 1.4.1. Supply an offline export via
`PROVIDERQUOTA_MUSE_SCHEMA_DIR` to run the schema-diff test; tests never run Muse.

Codex and Claude fixtures in adjacent plugins transcribe the redacted native
probes; Antigravity transcribes the review §3 document description.
