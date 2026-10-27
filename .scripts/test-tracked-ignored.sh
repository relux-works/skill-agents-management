#!/usr/bin/env bash
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"
guard="$root/.scripts/check-tracked-ignored.sh"
source_head=$(git rev-parse HEAD)
source_git=$(cd "$(git rev-parse --git-common-dir)" && pwd -P)
mkdir -p "$root/.temp"
scratch=$(mktemp -d "$root/.temp/tracked-ignored-controls.XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT
fixture="$scratch/clone"

# Every mutation targets this disposable clone, even with ambient Git overrides.
fixture_git() {
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
    git -C "$fixture" "$@"
}
run_guard() {
  (cd "$fixture" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
    bash "$guard")
}

env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
  git clone --quiet --no-hardlinks --no-checkout "$source_git" "$fixture"
fixture_git checkout --quiet --detach "$source_head"

# A clean checkout must pass; an indexed .out/x must fail with the intended
# diagnostic. The guard must leave the planted bytes and index unchanged.
test -z "$(fixture_git status --porcelain)"
run_guard > "$scratch/clean.log" 2>&1
grep -Fxq 'check-tracked-ignored: clean' "$scratch/clean.log"
test -z "$(fixture_git status --porcelain)"
printf 'PASS: clean clone accepted\n'

mkdir "$fixture/.out"
printf 'control plant\n' > "$fixture/.out/x"
fixture_git check-ignore -q .out/x
fixture_git add -f -- .out/x
fixture_git ls-files -s -z > "$scratch/index-before"
status=0
run_guard > "$scratch/plant.log" 2>&1 || status=$?
cat "$scratch/plant.log"
test "$status" -eq 1
grep -Fxq 'check-tracked-ignored: tracked ignored paths are forbidden:' "$scratch/plant.log"
grep -Fxq '  .out/x' "$scratch/plant.log"
fixture_git ls-files -s -z > "$scratch/index-after"
cmp "$scratch/index-before" "$scratch/index-after"
test "$(cat "$fixture/.out/x")" = 'control plant'
printf 'PASS: force-added .out/x rejected (exit 1); index and bytes unchanged\n'
