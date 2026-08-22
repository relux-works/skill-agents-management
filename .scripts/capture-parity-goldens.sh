#!/usr/bin/env bash
#
# Regenerate pkg/agentic/parity/testdata/goldens from the EXTRACTION SOURCE's
# own capture harness.
#
# This script runs skill-project-management's TestCaptureLaunchSurface and hands
# its output to this repository's generator. It does not capture anything
# itself, and it must not grow code that does: a golden captured beside the port
# proves only that the port agrees with itself.
#
# The source checkout is READ-ONLY here. The only thing this script does inside
# it is run one `go test`.
#
# Usage:
#   .scripts/capture-parity-goldens.sh [--source /path/to/skill-project-management]
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The default sibling lookup resolves through the MAIN checkout, not this one.
# Story work happens in a worktree under .temp/, where "../skill-project-management"
# points at a scratch directory rather than at the extraction source.
MAIN_CHECKOUT="$(cd "$(git -C "$REPO_ROOT" rev-parse --git-common-dir)/.." 2>/dev/null && pwd || echo "$REPO_ROOT")"
if [ -z "${SOURCE_REPO:-}" ]; then
  for candidate in "$REPO_ROOT/../skill-project-management" "$MAIN_CHECKOUT/../skill-project-management"; do
    if [ -d "$candidate" ]; then SOURCE_REPO="$(cd "$candidate" && pwd)"; break; fi
  done
fi

while [ $# -gt 0 ]; do
  case "$1" in
    --source) SOURCE_REPO="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "${SOURCE_REPO:-}" ] || [ ! -d "$SOURCE_REPO" ]; then
  echo "source checkout not found; pass --source /path/to/skill-project-management" >&2
  exit 2
fi

HARNESS_REL="tools/board-cli/internal/spawn/parity_capture_test.go"
if [ ! -f "$SOURCE_REPO/$HARNESS_REL" ]; then
  echo "no capture harness at $SOURCE_REPO/$HARNESS_REL" >&2
  exit 2
fi

SOURCE_COMMIT="$(git -C "$SOURCE_REPO" rev-parse HEAD)"
SOURCE_DIRTY="$(git -C "$SOURCE_REPO" status --porcelain)"
if [ -n "$SOURCE_DIRTY" ]; then
  # A golden captured from a dirty tree records a commit that does not describe
  # the code that produced it, which is worse than recording nothing.
  echo "source checkout $SOURCE_REPO is dirty; a golden's commit would not describe what captured it" >&2
  echo "$SOURCE_DIRTY" >&2
  exit 1
fi

SCRATCH="$REPO_ROOT/.temp/parity-capture"
rm -rf "$SCRATCH"
mkdir -p "$SCRATCH/tmp" "$SCRATCH/bin" "$SCRATCH/out"

# Stub executables on a PINNED PATH, the source harness's own
# setHermeticAgentPath technique applied from outside it. Without this, the
# dry-run cases resolve whichever providers the operator happens to have
# installed — or fail to resolve at all — and the fixture records the machine
# rather than the contract.
for name in claude codex qwen muse gemini agy; do
  printf '#!/bin/sh\ncat >/dev/null\nexit 0\n' > "$SCRATCH/bin/$name"
  chmod +x "$SCRATCH/bin/$name"
done

GO_BIN_DIR="$(dirname "$(command -v go)")"
RAW="$SCRATCH/out/raw-capture.json"
META="$SCRATCH/out/capture-meta.json"

# The PINNED PARENT ENVIRONMENT. The source harness diffs the child environment
# against os.Environ(), so an unpinned capture records whichever session
# variables the operator's shell carried: a key the plugin strips would be
# provably stripped only on the machine that happened to set it, and the
# fixture would carry that machine's paths and run ids.
#
# Every value below is synthetic. The keys are the ones the source's filters
# act on (filterCodexRuntimeEnv, filterQwenRuntimeEnv, withSpawnEnv), seeded so
# each strip and each replacement is OBSERVABLE in the capture rather than
# invisible for want of a parent value.
#
# The *_BYSTANDER keys at the end are the other half of that, and they are load-
# bearing: seeding only the keys a filter strips proves the filter's LOWER bound
# (it removes at least these) and says nothing about its upper bound (it removes
# no more). qwen is the case that makes the difference concrete — filterQwenRuntimeEnv
# strips every other key in this list, so without a surviving key its golden's
# env_removed covers 100% of parent_env, and a port whose ChildEnv discards the
# whole parent environment and returns only its injections produces the identical
# diff. The bystanders give every system a positive preservation bound.
#
# CODEX_LIKE_BUT_NOT and CLAUDECODE_LIKE_BUT_NOT are near-misses on purpose: the
# source strips by EXACT key (filterEnvKeys builds a blocked-key map, spawn.go:998),
# not by prefix, so a port that reaches for strings.HasPrefix("CODEX_") is a
# plausible defect that only a near-miss can catch. TASK_BOARD_LIKE_BUT_NOT does
# the same for withSpawnEnv's appendOrReplaceEnv, which also matches exact keys.
#
# See pkg/agentic/parity/testdata/goldens/README.md and parityBystanderKeys in
# golden_test.go, which fails if a later capture drops them.
PINNED_ENV=(
  CLAUDECODE=1
  CODEX_THREAD_ID=parity-thread
  CODEX_SESSION=parity-session
  CODEX_CI=parity-ci
  CODEX_MANAGED_BY_NPM=1
  CODEX_MANAGED_BY_BUN=1
  CODEX_MANAGED_PACKAGE_ROOT=/parity/pinned/codex-managed-package-root
  TASK_BOARD_CODEX_APP_SERVER_URL=http://parity.invalid/app-server
  TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV=PARITY_CODEX_APP_SERVER_TOKEN
  PARITY_CODEX_APP_SERVER_TOKEN=parity-codex-app-server-token
  TASK_BOARD_SESSION_MANAGER_URL=http://parity.invalid/session-manager
  TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV=PARITY_SESSION_MANAGER_TOKEN
  PARITY_SESSION_MANAGER_TOKEN=parity-session-manager-token
  TASK_BOARD_SESSION_ID=SESSION-parity-parent
  TASK_BOARD_RUN_ID=RUN-parity-parent
  TASK_BOARD_TASK_ID=TASK-parity-parent
  TASK_BOARD_BOARD_DIR=/parity/pinned/parent/.task-board
  TASK_BOARD_DELIVERY_GOAL_ID=GOAL-parity-parent
  PARITY_BYSTANDER=keep-me
  CODEX_LIKE_BUT_NOT=keep-me
  CLAUDECODE_LIKE_BUT_NOT=keep-me
  TASK_BOARD_LIKE_BUT_NOT=keep-me
)

# TASK_BOARD_DIR is deliberately ABSENT: the source sets it on the child from
# the run's board directory, and a parent value would mask that injection.
echo "capturing from $SOURCE_REPO @ $SOURCE_COMMIT"
env -i \
  HOME="$HOME" \
  PATH="$SCRATCH/bin:$GO_BIN_DIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  TMPDIR="$SCRATCH/tmp" \
  SPAWN_PARITY_CAPTURE_OUT="$RAW" \
  "${PINNED_ENV[@]}" \
  sh -c "cd '$SOURCE_REPO/tools/board-cli' && go test -mod=mod ./internal/spawn -run TestCaptureLaunchSurface -count=1"

# The meta the generator records into every fixture. parent_env is the pinned
# set above verbatim; parent_env_omitted names what the capture process also
# needed and why it is not recorded.
{
  printf '{\n'
  printf '  "source_repo": "skill-project-management",\n'
  printf '  "source_commit": "%s",\n' "$SOURCE_COMMIT"
  printf '  "source_harness": "%s::TestCaptureLaunchSurface",\n' "$HARNESS_REL"
  printf '  "capture_env_var": "SPAWN_PARITY_CAPTURE_OUT",\n'
  printf '  "captured_by": "%s",\n' "${PARITY_CAPTURED_BY:-TASK-260822-slgewd}"
  printf '  "parent_env": ['
  sep=""
  for entry in "${PINNED_ENV[@]}"; do
    printf '%s\n    "%s"' "$sep" "$entry"
    sep=","
  done
  printf '\n  ],\n'
  printf '  "parent_env_omitted": [\n'
  printf '    "HOME — the operator machine home the Go toolchain needs for its build cache; no filter under capture touches it, so it is in neither env_added nor env_removed",\n'
  printf '    "PATH — seeded with the stub bin directory and excluded from the env diff by the path-env-excluded mask rule",\n'
  printf '    "TMPDIR — the capture temp root, machine-local, masked out of the surface by capture-temp-slot",\n'
  printf '    "SPAWN_PARITY_CAPTURE_OUT — the source harness'"'"'s own output path; inherited unchanged by every child, so it diffs to nothing"\n'
  printf '  ],\n'
  printf '  "temp_root": "%s",\n' "$SCRATCH/tmp"
  printf '  "stub_bin_dir": "%s",\n' "$SCRATCH/bin"
  printf '  "forbidden_literals": ["%s", "%s", "%s"]\n' "$HOME" "$SOURCE_REPO" "$REPO_ROOT"
  printf '}\n'
} > "$META"

echo "generating goldens"
cd "$REPO_ROOT"
env -u TASK_BOARD_DIR \
  PARITY_GOLDEN_CAPTURE_IN="$RAW" \
  PARITY_GOLDEN_META_IN="$META" \
  PARITY_GOLDEN_OUT="$REPO_ROOT/pkg/agentic/parity/testdata/goldens" \
  go test -mod=mod ./pkg/agentic/parity -run TestWriteGoldensFromCapture -count=1 -v

echo "done: pkg/agentic/parity/testdata/goldens"
