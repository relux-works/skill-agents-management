# Unresolved questions

- TASK-261023-g917dj: should `localmodels.Config` carry the SHA-256 of the exact
  engines.toml bytes already read by `LoadAt`, allowing catalog observation to
  reject content drift? The current contract requires producer-reported catalog
  and cache identity, and validates digest shape only. Do not add a second
  catalog content reader. See [catalog observation](docs/catalog-engine-observation.md).
- TASK-261023-g917dj integration: confirm the coordinated status-provenance/v1
  envelope against released curator-engines v0.4.3 before claiming real-binary
  acceptance. Controlled SAM fixtures do not establish engine readiness or
  inference, and this SAM change does not remove the SPM G31 refusal.
