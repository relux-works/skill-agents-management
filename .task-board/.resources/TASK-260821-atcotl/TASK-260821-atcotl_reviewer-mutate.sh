#!/bin/zsh
set -e
S=/tmp/atcotl-scratch
NAME=$1; FILE=$2; PY=$3
cd $S
git checkout -q -- .
python3 - "$S/$FILE" <<PYEOF
import sys,io
p=sys.argv[1]
src=open(p).read()
$PY
open(p,'w').write(src)
PYEOF
if ! git diff --quiet; then
  echo "### MUTANT $NAME applied to $FILE"
else
  echo "### MUTANT $NAME DID NOT APPLY (no diff) — harness bug"; git checkout -q -- .; exit 2
fi
set +e
OUT=$(env -u TASK_BOARD_DIR go test -mod=mod ./... -count=1 2>&1)
CODE=$?
if [ $CODE -eq 0 ]; then
  echo "SURVIVOR: exit 0 — no test noticed"
else
  echo "KILLED: exit $CODE. Tests that went red:"
  echo "$OUT" | grep -E '^\s*--- FAIL' | sed 's/^/    /' | head -25
  echo "$OUT" | grep -E '^(FAIL|ok|---)' | grep -c FAIL >/dev/null
fi
git checkout -q -- .
