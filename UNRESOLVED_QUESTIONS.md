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


## PR #114 gateway consumer boundary, round 2

The producer scope deliberately leaves these reviewer questions open: N2
(generic gateway rendering without a basis), N4 (native effect of proxy/TLS
selectors), N5 (home config/profile precedence in qualified Codex), N7 (Build
can issue a basis for a shape or release that Admit will refuse), N9 (repeated
verification is not a single-execution lease; the process verifier takes no
workdir), N10 (the shipped pool omits gpt-6.1-sol), and N12 (the permission
policy's transport comment). No permission rows or release qualification change
is authorized here. Deferred consumer duties B4, N1, N3, N8 and B6's proxy-pair
and multiline restrictions are documented once in README's Consumer duties.
Native qualification, external consumer integration and atomic pathname-to-exec
protection remain outside this producer's validation. Evidence and command
exits: `.temp/pr114-round2/`.
