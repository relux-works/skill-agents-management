#!/usr/bin/env bash
#
# Capture the BOARD-OWNED model facts this module is porting, from the board's
# OWN BINARY.
#
#   pkg/vendorplugin/testdata/board-model-facts.json
#
# # Why the binary and not the sources
#
# The sibling capture script (.scripts/capture-model-registry.sh) text-parses
# skill-project-management's Go sources, because at the commit it froze, the
# facts it needed were spelled as literals in one table. They no longer are:
# since that repository's TASK-260823-1tis7o the model id set, the agentic
# systems and the whole reasoning-effort axis are READ from this module, and its
# own rows carry only what this module does not own. A text parser would now be
# reading half a table and silently resolving the other half.
#
# `q 'models()'` is the board's own answer about its own registry, so it needs
# no parser at all and cannot disagree with the binary an operator runs.
#
# # What is circular in that answer and therefore NOT pinned
#
# The projection JOINS the board's rows to this module's vendor plugins, so
# `agenticSystems`, `reasoning`, `supportedEfforts` and `recommendedEffort` come
# BACK from here for every row whose broker is established. Pinning those
# against this module would be the port agreeing with itself. The fixture
# carries them anyway, marked, so a reader can see the join happened — and
# pkg/vendorplugin/boardfacts_test.go pins only the board-owned columns plus the
# muse rows' effort, which the board still declares locally because no vendor
# owns those two rows.
#
# `description` is deliberately NOT captured. The board's display texts die with
# its half of this swap and this module authors its own; a copy of them sitting
# in testdata is an invitation to import one as a ported fact.
#
# # The fixture is TRANSITIONAL
#
# It exists to make ONE transcription safe — the port of the board's model
# facts into the vendor rows — and it dies with the board's half. When the board
# reads these facts from this module instead of declaring them, this fixture and
# the test that reads it have nothing left to compare and must be deleted rather
# than regenerated.
#
# The board checkout is READ-ONLY. The only things this script does inside it
# are read files and build its CLI into a scratch path OUTSIDE the checkout.
#
# Usage:
#   .scripts/capture-board-model-facts.sh [--source /path/to/skill-project-management]
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The default sibling lookup resolves through the MAIN checkout, not this one.
# Story work happens in a worktree under .temp/, where "../skill-project-management"
# points at a scratch directory rather than at the board.
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
  echo "board checkout not found; pass --source /path/to/skill-project-management" >&2
  exit 2
fi

TESTDATA="$REPO_ROOT/pkg/vendorplugin/testdata"
SCRATCH="$REPO_ROOT/.temp/capture-board-model-facts"
mkdir -p "$TESTDATA" "$SCRATCH"

echo "board: $SOURCE_REPO"

SOURCE_BIN="$SCRATCH/task-board-source"
( cd "$SOURCE_REPO/tools/board-cli" && go build -mod=mod -o "$SOURCE_BIN" . )

# TASK_BOARD_DIR is unset deliberately: the registry is compiled into the
# binary and does not depend on a board directory, and an inherited one only
# gives the process a board it has no business reading.
( cd "$SOURCE_REPO" && env -u TASK_BOARD_DIR "$SOURCE_BIN" q 'models()' ) > "$SCRATCH/models.json"

SOURCE_COMMIT="$(git -C "$SOURCE_REPO" rev-parse HEAD 2>/dev/null || echo unknown)"
SOURCE_REPO="$SOURCE_REPO" SOURCE_COMMIT="$SOURCE_COMMIT" \
python3 - "$SCRATCH/models.json" "$SOURCE_REPO/tools/board-cli/internal/spawn/models.go" \
         "$TESTDATA/board-model-facts.json" <<'PY'
import hashlib, json, os, sys

projection_path, registry_source, out_path = sys.argv[1:4]
with open(projection_path, encoding="utf-8") as handle:
    projection = json.load(handle)
with open(registry_source, "rb") as handle:
    registry_bytes = handle.read()

rows = projection["models"]
if projection.get("totalModels") != len(rows):
    raise SystemExit(
        f"the projection says it holds {projection.get('totalModels')} models and carries "
        f"{len(rows)}; the capture would be inconsistent with itself"
    )

# Columns the BOARD owns. These are the ones the port is pinned against.
BOARD_OWNED = [
    ("agent", "runtime", ""),
    ("policyRank", "policy_rank", 0),
    ("lifecycle", "lifecycle", ""),
    ("supersededBy", "superseded_by", ""),
    ("recommended", "recommended", False),
    ("contextWindowTokens", "context_window_tokens", 0),
]

# Columns the projection JOINS BACK from the vendor module. Carried so the join
# is visible, never pinned against the module for a row that has a broker.
JOINED = [
    ("broker", "broker", ""),
    ("agenticSystems", "agentic_systems", []),
    ("reasoning", "reasoning", ""),
    ("supportedEfforts", "supported_efforts", []),
    ("recommendedEffort", "recommended_effort", ""),
]

models = []
for row in rows:
    out = {"id": row["id"]}
    for source_key, fixture_key, default in BOARD_OWNED:
        out[fixture_key] = row.get(source_key, default)
    out["pricing"] = row.get("pricing")
    joined = {}
    for source_key, fixture_key, default in JOINED:
        joined[fixture_key] = row.get(source_key, default)
    out["joined_from_vendor_module"] = joined
    models.append(out)

fixture = {
    "provenance": {
        "source_repo": os.environ["SOURCE_REPO"],
        "source_commit": os.environ["SOURCE_COMMIT"],
        "captured_from": "the board binary's q 'models()' projection",
        "registry_source": "tools/board-cli/internal/spawn/models.go",
        "registry_sha256": "sha256:" + hashlib.sha256(registry_bytes).hexdigest(),
        "generated_by": ".scripts/capture-board-model-facts.sh",
        "lifetime": (
            "TRANSITIONAL. This fixture exists to make ONE transcription safe - the port of the "
            "board's model facts into this module's vendor rows - and it dies with the board's "
            "half of the swap. When the board reads these facts from this module instead of "
            "declaring them, delete this fixture and pkg/vendorplugin/boardfacts_test.go rather "
            "than regenerating them."
        ),
        "circularity": (
            "joined_from_vendor_module carries the columns the projection reads BACK from this "
            "module for every row whose broker is established. They are recorded so the join is "
            "visible and are NOT pinned against this module, because a port pinned against its "
            "own output proves only that it agrees with itself. The two muse rows are the "
            "exception the board itself names: no vendor owns them, so their effort axis is the "
            "board's own declaration and is pinned."
        ),
    },
    "model_count": len(models),
    "models": models,
}
with open(out_path, "w", encoding="utf-8") as handle:
    json.dump(fixture, handle, indent=2, sort_keys=True)
    handle.write("\n")
print(f"wrote {out_path}: {len(models)} model rows", file=sys.stderr)
PY

echo "captured into $TESTDATA"
