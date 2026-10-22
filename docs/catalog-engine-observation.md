# Catalog-bound engine observation

`pkg/catalogobservation` composes one selected local-model binding from a parsed
`localmodels.Config`. It does not load another catalog, own runtime lifecycle,
or infer a cache root. The caller loads configuration with `localmodels.LoadAt`
or `Peek`, selects a runtime/model, and supplies its explicit cache-root policy.

```go
factory, err := catalogobservation.NewFactory(mlx.New(), llamacpp.New())
if err != nil {
    return err
}
binding, err := factory.ForBinding(cfg, runtimeID, modelID, cacheRoot)
if err != nil {
    return err
}
registry, err := vendorplugin.NewRegistryWithEngineObservationAdapters(
    systems, binding.Adapters...,
)
if err != nil {
    return err
}
if binding.Engine != nil {
    if err := registry.RegisterPlugin(binding.Engine); err != nil {
        return err
    }
}
for _, configured := range cfg.EnginePlugins() {
    if binding.Engine != nil && configured.PluginDeclaration().Ref() == binding.Engine.PluginDeclaration().Ref() {
        continue // The concrete node replaces this configured identity.
    }
    if err := registry.RegisterPlugin(configured); err != nil {
        return err
    }
}
// Register localmodels.New(cfg, ...), declare the selected runtime, and call
// vendorplugin.BuildLaunch using this registry, as in the local-model recipe.
```

Import `pkg/catalogobservation`, `pkg/inferenceengine/engines/mlx`,
`pkg/inferenceengine/engines/llamacpp`, and `pkg/vendorplugin` from this module;
`localmodels` is `pkg/vendorplugin/vendors/local-models`. A consumer should scope
its vendor configuration and registry to the selected launch as appropriate.
The existing [consumer guide](consuming-the-module.md) describes system/vendor
registration and the plan execution boundary.

`inferenceengine.ObservationPlugin` adds an immutable `ObservationKind()` to the
generic plugin declaration. MLX declares `native-transformer`; llama-cpp declares
`gguf-server`. The factory freezes those declarations in the existing generic
graph. Configured identity-only nodes and unsupported engine IDs do not gain
observation authority. Model-level engine overrides take precedence over runtime
inheritance. Unrelated unsupported runtime rows do not affect binding selection.
A pointer-only selected row returns an empty binding without requiring a catalog
or cache root. Missing selected rows, unsupported engine refs and invalid scopes
return errors without executing status.

For direct composition, `engineobservation.NewCatalogAdapter(kind, CatalogScope,
opts...)` freezes Engine/Runtime/Model/Profile and the selected pointer's Project,
`Config.EnginesConfig`, and CacheRoot. Query identity drift refuses before any
subprocess. Project, catalog and cache root must be absolute and NUL-free. At
construction, catalog parents and the existing cache directory are canonicalized;
the catalog leaf must be a regular non-symlink file. Catalog contents are never
read by this constructor or factory. Construction does not execute or mutate
runtime state. Changing the caller's Config or scope afterward cannot retarget
an existing adapter.

Each observation performs one bounded read with the selected project as cwd:

```text
curator-engines status --engine <profile> --json --config <canonical-engines.toml> --cache-root <canonical-cache-root>
```

It reuses the project-only adapter's timeout, cancellation, output cap, strict
observed-process/v3 transcription and validity interval. `BuildLaunch` then
validates those readings before system preflight or plan materialization. The
vendor's earlier `Spawn` callback is pure profile resolution, not harness
execution; observation refusal does not promise that callback was never called.
No engine ensure/start/lease operation is introduced.

## Status provenance

The producer contract coordinated for curator-engines v0.4.3 adds:

```json
{
  "provenance": {
    "contract_version": "status-provenance/v1",
    "engines_config": "/canonical/catalog/engines.toml",
    "engines_config_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "cache_root": "/canonical/cache"
  },
  "readings": {"contract_version": "observed-process/v3", "facts": []}
}
```

This illustrates the envelope only; empty facts do not admit a launch. The
producer reports the catalog and cache root actually used, and the SHA-256 of
the exact catalog bytes decoded by that status call. Legacy/compat responses and
older binaries omit provenance. The catalog-bound adapter requires the member;
it never falls back to project-only observation, HOME, or cwd discovery.

| Condition | Result |
| --- | --- |
| No provenance member | `ErrCatalogProvenanceUnavailable` |
| Unknown version; missing/null/duplicate/case-aliased member; malformed path or digest | `ErrReadingsMalformed` |
| Catalog path or cache root differs from the frozen canonical selection | `ErrCatalogIdentityMismatch` |
| Exact provenance and valid readings | Observation proceeds to normal readings validation |

Every refusal returns an empty observation. Paths must be absolute, cleaned and
NUL-free; the digest must be exactly 64 lowercase hexadecimal characters. The
adapter compares the reported canonical path strings directly with the frozen
selection and does not resolve a different response path into that selection.
The provenance object has a closed member set; unrelated outer status members
remain additive. The original project-only `NewAdapter` retains its existing
compatibility behavior.

The digest is shape-checked but not compared against `LoadAt` bytes: Config does
not yet carry that byte-snapshot digest. This API proves producer-reported file
and cache selection, not unchanged catalog bytes between loading and status.
Carrying the digest from the existing LoadAt read is a follow-up; adding a
second content reader would not establish the same snapshot. Real-binary
acceptance is separate from the controlled wire fixtures and requires the
producer release. Neither fixture admission nor a plan proves model inference.

## Focused checks

```sh
GOFLAGS= go test ./pkg/engineobservation ./pkg/catalogobservation ./pkg/inferenceengine/engines/... -count=1
GOFLAGS= go test ./tools/agents-management/cmd -run '^TestModelCheckCatalogProductionBinding$' -count=1
```

Tests cover exact argv/cwd, scope mutation, same-name catalog A admission and B
refusal, strict malformed provenance, real LoadAt composition, concrete kinds,
pointer-only controls, and refusal before Pi preflight. Task validation logs and
bounded isolated mutation evidence belong under `.temp/<TASK-ID>/`.
