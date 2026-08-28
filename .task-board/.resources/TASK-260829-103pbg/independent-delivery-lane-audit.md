# Highest-value independent delivery lane audit

Date: 2026-08-29 (Europe/Moscow)

Scope: read-only re-audit of `skill-project-management`, `skill-agents-management`, and `relux-agents-infra` for work that can proceed without `skill-project-management` PR #46, without touching a live local model/runtime/socket/service, and without overlapping active infra tasks `TASK-260829-1qh0ud` or `TASK-260829-1q31e0`.

## Recommendation

The highest-value lane available now is a **fresh exact-base replay of the accepted agents-management restart/quarantine status consumer** represented by `TASK-260829-1kpj01`, using only static fixtures.

Do not integrate, reparent, close, abort, clean, or otherwise mutate the existing `TASK-260829-1kpj01` / `STORY-260829-17byw1` lifecycle. Its code is accepted, but its board history is not safely integrable until the chained cross-Story move defect is fixed.

Instead, create a new replacement Story and a new leaf directly beneath it in `skill-agents-management`, attach the immutable accepted revision-3 patch as precondition evidence, and replay it on the freshly fetched current `main`. The new leaf must be created directly under its final Story and must never be reparented. This gives the replay a single clean ownership chain and avoids both known board defects without depending on PR #46.

No exact replacement Task ID exists yet. A read-only audit cannot allocate one, and none of the existing IDs is a safe substitute. The first authorized mutation should create that replacement Story and leaf and capture the returned IDs. The exact existing IDs and why they must remain untouched are recorded below.

## Why this lane wins

| Candidate | Current authoritative state | Can proceed independently now? | Decision |
| --- | --- | --- | --- |
| Agents-management restart/quarantine consumer, source `TASK-260829-1kpj01` | Accepted implementation and review exist; dependency infra PR #10 is contained in infra `main`; exact patch applies cleanly to agents-management `main`. | **Yes, through a fresh Story/leaf replay.** Static fixtures only; no overlap with pressure/retention. | **Selected.** |
| Existing accepted `TASK-260829-1kpj01` / `STORY-260829-17byw1` integration | `to-review`, CR rev3 accepted, but canonical integration refuses on a two-hop move chain. | No. Fix is `BUG-260829-w9ndi8`, itself tied to the board repair lane around PR #46. | Preserve as evidence; do not integrate or bypass. |
| Task-board local Qwen consumer `TASK-260828-3hultd` | `to-dev`; board candidate/base workflow remains tied to `BUG-260819-3rxy9c` and PR #46. | No. | Defer. |
| Activity lifecycle `TASK-260828-3u33xe` or remote config docs `TASK-260829-1i2a1g` | Accepted/implemented candidates exist in `skill-project-management`, but their exact-base replay/publication is on the board repo's blocked lane. | No for this constraint. | Defer. |
| Agents-management pressure consumer `TASK-260829-1gqlea` | `backlog`; consumes the contract currently being changed by active infra `TASK-260829-1qh0ud`. | No without overlapping/waiting on the excluded run. | Defer. |
| Long-run soak `TASK-260829-lx3o55` | `backlog`; depends on restart/status plus pressure/retention consumers and final contracts. | No; it would overlap or speculate ahead of both excluded infra runs. | Defer. |

## Authoritative repository and dependency state

### `skill-agents-management`

- Local `HEAD`, local `main`, cached `origin/main`, verified GitHub `main`, and tag `v0.3.0` all resolve to:

  ```text
  3bec0baf9a0c897b0f76e1182e371a25132fa509
  ```

- GitHub has no open agents-management pull request at audit time.
- The root landing suite is configured and non-empty:

  ```text
  make vet
  make build
  env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1
  make regress
  ```

- `version_control.confirm=true`; the configured owner policy requires a previous-day timestamp after 20:00 MSK.

### `relux-agents-infra`

- Local and remote `main` resolve to:

  ```text
  6d051f54440d36e3ca3d132f8d9d1e78d46289de
  ```

- Restart deadline/status dependency PR #10 merge `675f77ed63376320ed1213f46f9462a299c0abaf` is an ancestor of current infra `main`.
- Excluded active work remains separate:
  - `TASK-260829-1qh0ud` — `development`, assigned `[implementer] developer (codex)`, resource-pressure status.
  - `TASK-260829-1q31e0` — `analysis`, assigned `[analyst] solution-architect (codex)`, aggregate lifecycle-log retention.
- The selected replay changes only `skill-agents-management`; it consumes the already-landed restart contract and does not read or alter the in-progress pressure/retention implementation.

### `skill-project-management`

- Verified GitHub `main`: `2a1042e563ea728fe8a67b82f03b9966d7cd83f8`.
- Local `main`: `8043c108f3ff933ccfa33f753b885b15e05809b1`, two commits ahead through open PR [#46](https://github.com/relux-works/skill-project-management/pull/46).
- The selected lane requires no code, commit, binary, or remote result from PR #46. It avoids the affected integration shape instead of claiming that shape is fixed.
- PR #46's remote checks are still red because the jobs did not get usable execution under the recorded billing/spending condition. This may also prevent a later hosted PR from satisfying repository merge policy, but it does not prevent the fresh local implementation/review/integration lane from proceeding and is not a code/base dependency.

## Existing source task and preserved accepted candidate

### Board state

`TASK-260829-1kpj01` — `widen-local-runtime-status-consumer-for-restart-quarantine`:

- status: `to-review`
- assignee: `[reviewer] reviewer (claude)`
- parent: `STORY-260829-17byw1`
- review policy: `required`
- hard and derived blockers: none
- checklist: 14/14 complete
- Change Request: `CR-TASK-260829-1kpj01-3`, revision 3, `accepted`, `story_final`

`STORY-260829-17byw1` — `republish-status-consumer-from-exact-release-base`:

- status: `to-review`
- managed branch: `task-board/story/STORY-260829-17byw1`
- base/tip: `3bec0baf9a0c897b0f76e1182e371a25132fa509`
- worktree: present and dirty with exactly the accepted eight-path candidate
- integration transaction: absent according to the preserved delivery audit

All five recorded producer/reviewer runs for the source lane are terminal `completed`; there is no implementation or reviewer process to duplicate:

| Run | Role/phase | Story | State |
| --- | --- | --- | --- |
| `RUN-260829-c197cb` | original producer | `STORY-260829-2qq4oo` | completed |
| `RUN-260829-94147f` | correct-base producer rev2 | `STORY-260829-17byw1` | completed |
| `RUN-260829-f85bd2` | reviewer rev2 | `STORY-260829-17byw1` | completed; changes requested |
| `RUN-260829-1b28f7` | producer rev3 | `STORY-260829-17byw1` | completed |
| `RUN-260829-e2eabf` | reviewer rev3 | `STORY-260829-17byw1` | completed; accepted |

### Immutable revision-3 evidence

- Patch:

  ```text
  /Users/alexis/src/relux-works/skill-agents-management/.task-board/.resources/TASK-260829-1kpj01/TASK-260829-1kpj01_change-request_rev3.patch
  ```

- Patch SHA-256:

  ```text
  87739e445c46446e2698462f1f0dc6eeea106145a82e1436c40629f6981e6109
  ```

- CR base OID:

  ```text
  3bec0baf9a0c897b0f76e1182e371a25132fa509
  ```

- Candidate tree OID:

  ```text
  f88509f0d604424a41972dee42e9e502c92eb357
  ```

- Accepted verdict:

  ```text
  /Users/alexis/src/relux-works/skill-agents-management/.task-board/.resources/TASK-260829-1kpj01/TASK-260829-1kpj01_review-verdict-rev3.md
  ```

- The patch passes `git apply --check` against current agents-management `main`.
- It changes exactly eight paths:
  - `LOGBOOK.md`
  - `docs/architecture.md`
  - `docs/shipped-state.md`
  - `pkg/localruntime/decode.go`
  - `pkg/localruntime/decode_test.go`
  - `pkg/localruntime/status.go`
  - `pkg/vendorplugin/vendors/local-models/availability.go`
  - `pkg/vendorplugin/vendors/local-models/availability_test.go`

The old accepted verdict is evidence for the replay, not authorization for a new CR. The new CR still requires its own independent reviewer and acceptance binding.

## Exact blockers on the old lane

The existing accepted CR cannot safely land now. Its integration admission failed before any ref move or transaction with:

```text
board_path_content_unrecognized: completed move for TASK-260829-1kpj01 does not match its authoritative parent
```

The recorded task move chain is:

```text
STORY-260829-37w91q -> STORY-260829-2qq4oo -> STORY-260829-17byw1
```

`BUG-260829-w9ndi8` tracks the production defect: the integrator validates the first hop against the final parent instead of folding the ordered chain. Merging a manually reconstructed patch branch would bypass managed integration, invalidate the accepted CR when trunk advances across the same eight paths, and leave board/workspace state outside the transaction model.

`STORY-260829-2qq4oo` must not be reused casually either:

- it currently has no children, but its managed worktree is dirty;
- its dirty diff SHA-256 is `ab69661bdb998b6fe5adb69ebd9ccb81bb4e3b53df13a1e2da4067043d2a577a`, the superseded pre-review patch rather than accepted rev3;
- its workspace record preserves contradictory historical provenance: `local_base_oid` / `current_base_oid` `1e9f203201d8317cd55c155104205ce97db38fa7`, while fetched upstream and selected base are `3bec0baf...`;
- its old rev1 CR is `ready`, carries base `1e9f203...`, and incorrectly expands to 45 paths;
- its recorded active run `RUN-260829-c197cb` is terminal completed, leaving stale workspace residue rather than a clean new lane.

Do not reset, clean, abort-discard, or adopt that residue as a new candidate. A brand-new Story is cheaper and preserves evidence.

## Safe next action

From the authoritative `skill-agents-management` control root:

1. Re-read GitHub/current upstream `main`. Proceed only if the freshly fetched selected base, local `main`, and GitHub `main` still agree. At audit time they are all `3bec0baf...`.
2. Create a **new** Story under `EPIC-260821-1qvnz2` for the static restart/quarantine status-consumer replay.
3. Create a **new leaf directly under that Story**. Copy the source task's description, acceptance criteria, completed dependency facts, and task-specific checklist. Do not move or reuse `TASK-260829-1kpj01`.
4. Attach the immutable rev3 patch and accepted-verdict evidence as preconditions on the new leaf. Record SHA-256 `87739e44...` and require exactly the eight named paths.
5. Spawn the producer in the new managed Story workspace. The producer should apply the immutable patch, run the configured four-command suite, and use static pre-extension/current/malformed fixtures only. It must not contact a local model, Pi daemon, HTTP endpoint, Unix socket, runtime service, or model harness.
6. Publish a new exact-base CR and verify its base equals the freshly selected upstream OID, its changed paths are exactly the eight listed paths, and its patch bytes/digest match the accepted revision-3 candidate.
7. Route a new independent reviewer. The reviewer must re-run the production `CheckAvailability` path and adversarial partial-cohort/timestamp/provenance tests; the old verdict cannot call `accept_cr` for the new revision.
8. Integrate the new Story through the normal managed transaction. Because the replacement leaf is born under its final parent, there is no move tombstone or chained-reparent dependency on `BUG-260829-w9ndi8` / PR #46.
9. Publish the exact integration-produced commits from a non-default branch and open a PR. Hosted merge still waits for green required checks and real hosted review; do not weaken that gate if the organization billing condition remains.
10. Preserve `TASK-260829-1kpj01`, both old Story worktrees, all old CR revisions, and their resources unchanged until the replacement is remotely merged. Then close/supersede the old lane through normal board operations with an explicit link to the delivered replacement; never delete its evidence.

## Stop conditions

Stop and re-audit rather than forcing the replay if any of these changes:

- fetched upstream `main`, local `main`, and GitHub `main` no longer resolve to one OID;
- the immutable rev3 patch no longer applies cleanly;
- the candidate changes any path outside the named eight;
- a new leaf is reparented after creation;
- the new workspace has pre-existing dirty bytes before the patch is applied;
- current infra removes or changes the already-landed PR #10 restart-status fields;
- validation attempts to contact a live local runtime/model/service rather than static fixtures;
- the new CR base differs from the freshly selected Story base;
- another owner/run appears on the replacement Story.

## Audit constraints and artifacts

This audit did not mutate board state, code, refs, remotes, configuration, user state, or live model/runtime/socket/service state. It created only this `.temp` report and task-scoped readiness scratch. Tool readiness is recorded at:

```text
/Users/alexis/src/relux-works/skill-project-management/.temp/lane-audit-2026-08-29/tool-readiness-01.log
```
