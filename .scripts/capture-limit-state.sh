#!/usr/bin/env bash
# Capture limit-state fixtures from the EXTRACTION SOURCE, so this repository's
# port can be checked against bytes the source's own code produced rather than
# against bytes this repository wrote and then agreed with itself about.
#
# Three artifacts land under pkg/providerlimits/testdata/:
#
#   real-state/       the operator's live machine state, written by the source
#                     binary over months of real runs. Copied, never generated.
#   source-written/   a suppression and a probe lease written HERE AND NOW by a
#                     program compiled against the source module's own
#                     providerlimits package, through its production entry
#                     points (ClassifyClaudePrompt/ClassifyCodexPrompt ->
#                     QuotaObservation -> Store.Observe, Store.ClaimProbe).
#   source-read/      the SOURCE's own Report over a state file this repository
#                     wrote, so the round trip is proven in both directions.
#
# The source checkout is treated as READ-ONLY. The scratch module that imports
# it is built under .temp/, never inside its tree.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# The source checkout is a SIBLING of this one, but "sibling" is not a fixed
# number of ".." away: this repository is routinely worked in a task-scoped
# worktree under .temp/<ID>/worktree, which is three levels deeper than the
# main checkout. So walk up until a sibling named skill-project-management
# turns up, rather than hardcoding a depth that is right in one layout only.
if [[ -z "${SOURCE_REPO:-}" ]]; then
  probe="$REPO_ROOT"
  while [[ "$probe" != "/" ]]; do
    candidate="$(dirname "$probe")/skill-project-management"
    if [[ -d "$candidate/pkg/providerlimits" ]]; then
      SOURCE_REPO="$candidate"
      break
    fi
    probe="$(dirname "$probe")"
  done
fi
SOURCE_REPO="${SOURCE_REPO:-}"
if [[ -z "$SOURCE_REPO" || ! -d "$SOURCE_REPO/pkg/providerlimits" ]]; then
  echo "capture-limit-state: the extraction source checkout was not found." >&2
  echo "  set SOURCE_REPO=/path/to/skill-project-management and re-run." >&2
  exit 2
fi

TESTDATA="$REPO_ROOT/pkg/providerlimits/testdata"
SCRATCH="$REPO_ROOT/.temp/TASK-260822-2jouz3/xrt"
LIVE_STATE="${LIVE_STATE:-$HOME/Library/Application Support/task-board/provider-limits}"

rm -rf "$SCRATCH"
mkdir -p "$SCRATCH/src" "$SCRATCH/state-source" "$SCRATCH/state-ours"
mkdir -p "$TESTDATA/real-state" "$TESTDATA/source-written" "$TESTDATA/source-read"

# --- 1. the operator's real, pre-extraction state ---------------------------
if [[ -d "$LIVE_STATE" ]]; then
  for f in "$LIVE_STATE"/*.json; do
    [[ -e "$f" ]] || continue
    install -m 0644 "$f" "$TESTDATA/real-state/$(basename "$f")"
  done
else
  echo "capture-limit-state: no live state at $LIVE_STATE; keeping the committed real-state fixtures" >&2
fi

# --- 2. a scratch module that imports the SOURCE package --------------------
cat > "$SCRATCH/src/go.mod" <<EOF
module sourcextr

go 1.25.5

require (
	github.com/relux-works/skill-project-management/pkg/providerlimits v0.0.0
	github.com/relux-works/skill-project-management/pkg/remoteconfig v0.0.0
)

replace github.com/relux-works/skill-project-management/pkg/providerlimits => $SOURCE_REPO/pkg/providerlimits

replace github.com/relux-works/skill-project-management/pkg/remoteconfig => $SOURCE_REPO/pkg/remoteconfig
EOF
cp "$REPO_ROOT/.scripts/limitstate_xrt.go" "$SCRATCH/src/main.go"

SOURCE_COMMIT="$(git -C "$SOURCE_REPO" rev-parse HEAD)"

# --- 3. source writes, we will read -----------------------------------------
(cd "$SCRATCH/src" && GOFLAGS=-mod=mod go run . write \
  --state-root "$SCRATCH/state-source" \
  --fixtures "$SOURCE_REPO/pkg/providerlimits/testdata")
# identities.json is deliberately NOT captured. It is a discovery aid the
# source rebuilds from the authoritative per-identity files, and its first_seen
# comes off a filesystem mtime rather than the injected clock — so capturing it
# would put a wall-clock timestamp in a fixture and teach every future reader
# to ignore that diff.
for f in "$SCRATCH/state-source/task-board/provider-limits"/*.json; do
  [[ -e "$f" ]] || continue
  base="$(basename "$f")"
  [[ "$base" == "identities.json" ]] && continue
  install -m 0644 "$f" "$TESTDATA/source-written/$base"
done

# --- 4. we write, source reads ----------------------------------------------
(cd "$REPO_ROOT" && env -u TASK_BOARD_DIR GOFLAGS=-mod=mod \
  go run ./.scripts/writestate --state-root "$SCRATCH/state-ours")
(cd "$SCRATCH/src" && GOFLAGS=-mod=mod go run . read \
  --state-root "$SCRATCH/state-ours") > "$TESTDATA/source-read/report.json"
install -m 0644 "$SCRATCH/state-ours/task-board/provider-limits"/*.state.json \
  "$TESTDATA/source-read/"
chmod 0644 "$TESTDATA/source-read"/*.json

# --- 5. the source's own tables, captured rather than re-typed --------------
(cd "$SCRATCH/src" && GOFLAGS=-mod=mod go run . tables) > "$TESTDATA/source-tables.json"
chmod 0644 "$TESTDATA/source-tables.json"

# --- 6. manifest -------------------------------------------------------------
{
  echo "{"
  echo "  \"source_repo_commit\": \"$SOURCE_COMMIT\","
  echo "  \"captured_by\": \".scripts/capture-limit-state.sh\","
  echo "  \"note\": \"real-state is the operator's live machine state; source-written was produced by the source module's own entry points; source-read is the source's Report over bytes this repository wrote\""
  echo "}"
} > "$TESTDATA/source-capture-manifest.json"
chmod 0644 "$TESTDATA/source-capture-manifest.json"

echo "capture-limit-state: captured at source commit $SOURCE_COMMIT"
