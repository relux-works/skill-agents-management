# Frozen accepted local-provider plans

These argv and sorted environment bytes were captured from **v0.5.56**, commit
`baefc62db453d791942d564d612fcf4b08765b8d`, on darwin/arm64 with Go 1.25.5.
Exec and dry-run intentionally produce identical command surfaces. Both the
ID-only and snapshot requests were captured and required to agree at that tag.
The request uses `providerRequest` and `writeProviderConfig`: local-story,
Qwen3.8-27B-Q4_K_M, low effort, and all four represented provider keys.

Only the allocated home, work, stub-bin and materialized-catalog directories
are replaced with placeholders. Environment entries are sorted without
filtering or deduplication; argv order, values, and the catalog content hash
are preserved. This fixture attests argv and environment, not every Plan field
or every possible accepted input value. The normal test cannot regenerate it.

Reproduce from the repository root, with task-scoped scratch paths:

```sh
mkdir -p .temp/TASK-261006-3pjn78/baseline
git archive v0.5.56 -o .temp/TASK-261006-3pjn78/v0.5.56.tar
tar -xf .temp/TASK-261006-3pjn78/v0.5.56.tar -C .temp/TASK-261006-3pjn78/baseline
cp pkg/agentic/systems/codex/testdata/provider-plans-v0.5.56/capture_test.go.txt .temp/TASK-261006-3pjn78/baseline/pkg/agentic/systems/codex/provider_plan_capture_test.go
cd .temp/TASK-261006-3pjn78/baseline
go test ./pkg/agentic/systems/codex -run '^TestCaptureAcceptedProviderV0556$' -count=1 -v
```

The capture writes `testdata/provider-plans-v0.5.56/{exec,dry-run}.json`
inside that archived package. Compare those files before copying them back.
Never capture the candidate as the baseline.
