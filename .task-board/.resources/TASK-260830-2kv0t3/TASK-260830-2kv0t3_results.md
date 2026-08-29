# TASK-260830-2kv0t3 producer evidence

## Input and exact base

- Accepted input: `accepted-revision-3.patch`, 276464 bytes, SHA-256 `06d9d0cca09f33a246082f9461cb2fff3e6314314d9fb639fed2958bff5364df`.
- Fresh fetch: `git fetch origin main` exited 0.
- `selected_base_oid`, `HEAD`, freshly fetched `origin/main`, and `FETCH_HEAD`: `75b105291011ac8988b714a86a38cf9f56771e13`.
- `git rev-list --left-right --count HEAD...origin/main`: `0 0`.
- The old accepted patch was read-only input. It was not blindly applied; plain `git apply --check` exited 1 on expected v0.4.3 overlaps.

## All 40 accepted paths

### Applied (26)

- `docs/architecture.md`
- `pkg/agentic/plan.go`
- `pkg/agentic/provenance.go`
- `pkg/agentic/provenance_test.go`
- `pkg/agentic/systems/pi/preflight.go`
- `pkg/agentic/systems/pi/preflight_test.go`
- `pkg/agentic/testdata/local-qwen-engine-mismatch-v1.json`
- `pkg/inferenceengine/contract.go`
- `pkg/inferenceengine/contract_test.go`
- `pkg/inferenceengine/engine.go`
- `pkg/inferenceengine/engine_test.go`
- `pkg/inferenceengine/engines/mlx/mlx.go`
- `pkg/inferenceengine/engines/mlx/mlx_test.go`
- `pkg/vendorplugin/engine.go`
- `pkg/vendorplugin/engine_launch_test.go`
- `pkg/vendorplugin/identity_literal_boundary_test.go`
- `pkg/vendorplugin/provenance.go`
- `pkg/vendorplugin/runtime.go`
- `pkg/vendorplugin/spawn.go`
- `pkg/vendorplugin/vendor.go`
- `pkg/vendorplugin/vendors/local-models/buildlaunch_test.go`
- `pkg/vendorplugin/vendors/local-models/config.go`
- `pkg/vendorplugin/vendors/local-models/config_test.go`
- `pkg/vendorplugin/vendors/local-models/models.go`
- `pkg/vendorplugin/vendors/local-models/testdata/local-qwen.toml`
- `pkg/vendorplugin/vendors/local-models/vendor_test.go`

### Already upstream (8)

- `pkg/agentic/multinode.go`
- `pkg/agentic/multinode_test.go`
- `pkg/agentic/registry.go`
- `pkg/agentic/registry_test.go`
- `pkg/agentic/system.go`
- `pkg/plugin/registry.go`
- `pkg/plugin/registry_test.go`
- `pkg/vendorplugin/registry_test.go`

### Semantically reconciled (6)

- `LOGBOOK.md`
- `README.md`
- `SKILL.md`
- `docs/consuming-the-module.md`
- `docs/shipped-state.md`
- `pkg/vendorplugin/registry.go`

Reconciliation retains v0.4.3's complete raw graph refusal inventory, all newer negative tests, and typed-nil vendor refusal. It adds the accepted inference-engine graph, static observation schemas, Registry-owned engine fact source, and persisted provenance validation without replacing newer release semantics.

## Trusted consumer gate and negative proof

- Production call site: `(*vendorplugin.Registry).ValidateLaunchProvenance` in `pkg/vendorplugin/provenance.go`, driven from real local-Qwen `BuildLaunch` provenance after JSON persistence.
- Negative: equal normalized correct-kind `attacker-engine` configured/resolved refs are refused because Registry declarations and graph resolution independently supply authority.
- Compile-clean narrowed mutant replaced trusted `configured`/`resolved` values with attacker-controlled persisted refs. Named test `TestBuildLaunchLocalQwenPersistedProvenanceRefusesEqualForgedEngineRefs/normalized_correct-kind_unconfigured_identity` exited 1 with `<nil>, want ErrLaunchProvenanceMismatch`.
- After exact restoration, that named test exited 0.
- The first attempted mutant was excluded from evidence because it did not compile; it was restored before the valid mutant.

## Validation (real exit codes)

- Focused local-Qwen provenance/preflight/observation suite: exit 0.
- Focused agentic/inferenceengine/vendor graph suite: exit 0.
- `go test ./... -count=1`: exit 0.
- `go test -race ./... -count=1`: exit 0.
- `go vet ./...`: exit 0.
- `go build ./...`: exit 0.
- `make regress`: exit 0.
- `python3 .scripts/verify-refusal-matrix.py`: exit 0; 37/37 compile-clean narrowed mutants killed, each named test exited 1.
- Restored attacker-engine focused test: exit 0.
- Repository Go formatting check: exit 0.
- `git diff --check`: exit 0.
- Static no-live-runtime scan for process, network, signal, socket, and listener calls across the changed inference-engine/Pi/provenance surfaces: exit 0 with no matches.

Pi and MLX validation used fakes/static schemas only. No live runtime, service, socket, process, SSH endpoint, pressure controller, or model was inspected, contacted, started, stopped, signalled, or mutated.
