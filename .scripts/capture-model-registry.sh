#!/usr/bin/env bash
#
# Regenerate the vendor-layer fixtures from the EXTRACTION SOURCE.
#
# Two fixtures come out of one run, and they are two different KINDS of
# evidence:
#
#   pkg/vendorplugin/testdata/source-model-registry.json
#       every knownModels row as skill-project-management declares it, plus the
#       frozen spawn-policy-v2 admission tiers. Read out of the source's own Go
#       sources, which are never modified.
#
#   pkg/vendorplugin/testdata/source-admitted-pairs.json
#       the admitted-pair digests the SOURCE BINARY prints for the SOURCE
#       repository's own task-board.config.json. A digest is a frozen
#       compatibility surface, so it is captured from the binary that owns it
#       rather than recomputed beside the port — a digest computed by the port
#       and pinned by the port proves only that the port agrees with itself.
#
# The source checkout is READ-ONLY. The only things this script does inside it
# are read files and build its CLI into a scratch path OUTSIDE the checkout.
#
# Usage:
#   .scripts/capture-model-registry.sh [--source /path/to/skill-project-management]
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

TESTDATA="$REPO_ROOT/pkg/vendorplugin/testdata"
SCRATCH="$REPO_ROOT/.temp/capture-model-registry"
mkdir -p "$TESTDATA" "$SCRATCH"

echo "source: $SOURCE_REPO"

python3 "$REPO_ROOT/.scripts/extract-model-registry.py" \
  --source "$SOURCE_REPO" \
  --out "$TESTDATA/source-model-registry.json"

# The digests come from the source's OWN binary, built into a scratch path so
# nothing is written inside the source checkout.
SOURCE_BIN="$SCRATCH/task-board-source"
( cd "$SOURCE_REPO/tools/board-cli" && go build -mod=mod -o "$SOURCE_BIN" . )

# TASK_BOARD_DIR is unset deliberately: with it set, the source binary resolves
# the ACTIVE board's project config instead of its own repository's, and the
# fixture would then describe whichever config the operator happened to have
# exported. The capture is defined as "the source repository's own config".
( cd "$SOURCE_REPO" && env -u TASK_BOARD_DIR "$SOURCE_BIN" q 'project_config()' ) \
  > "$SCRATCH/project-config.json"

SOURCE_COMMIT="$(git -C "$SOURCE_REPO" rev-parse HEAD 2>/dev/null || echo unknown)"
SOURCE_REPO="$SOURCE_REPO" SOURCE_COMMIT="$SOURCE_COMMIT" \
python3 - "$SCRATCH/project-config.json" "$SOURCE_REPO/task-board.config.json" \
         "$TESTDATA/source-admitted-pairs.json" <<'PY'
import hashlib, json, os, sys

projection_path, config_path, out_path = sys.argv[1:4]
with open(projection_path, encoding="utf-8") as handle:
    projection = json.load(handle)
with open(config_path, "rb") as handle:
    config_bytes = handle.read()

reported = projection.get("config_path")
if os.path.realpath(reported) != os.path.realpath(config_path):
    raise SystemExit(
        f"the source binary projected {reported!r}, not the source repository's own "
        f"{config_path!r}; capturing that would pin a digest over somebody else's config"
    )

ceilings = projection["spawn"]["ceilings"]
runtimes = sorted(
    key for key, value in ceilings.items()
    if isinstance(value, dict) and "admitted_pairs" in value
)
if not runtimes:
    raise SystemExit("the projection carries no admitted_pairs; nothing to pin")

fixture = {
    "provenance": {
        "source_repo": os.environ["SOURCE_REPO"],
        "source_commit": os.environ["SOURCE_COMMIT"],
        "captured_from": "the source binary's project_config() projection",
        "config_path": config_path,
        "config_sha256": "sha256:" + hashlib.sha256(config_bytes).hexdigest(),
        "generated_by": ".scripts/capture-model-registry.sh",
    },
    "authority": ceilings.get("authority"),
    "snapshot_version": ceilings.get("snapshot_version"),
    "contract_version": projection["spawn"]["ceilings"].get("contract_version"),
    "ceilings": {},
}
for runtime in runtimes:
    section = ceilings[runtime]
    pairs = section["admitted_pairs"]
    fixture["ceilings"][runtime] = {
        "model": section.get("model", ""),
        "model_criterion": section.get("model_criterion", ""),
        "reasoning_effort": section.get("reasoning_effort", ""),
        "admitted_pairs": {
            "provider": pairs["provider"],
            "form": pairs["form"],
            "digest": pairs["digest"],
            "models": pairs["models"],
        },
    }

with open(out_path, "w", encoding="utf-8") as handle:
    json.dump(fixture, handle, indent=2, sort_keys=True)
    handle.write("\n")
print(f"wrote {out_path}: {len(runtimes)} pinned ceiling(s)", file=sys.stderr)
PY

echo "captured into $TESTDATA"
