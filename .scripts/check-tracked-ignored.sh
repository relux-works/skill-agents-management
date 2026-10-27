#!/usr/bin/env bash
set -euo pipefail

# Inspect the whole calling repository, including paths outside the current
# subdirectory. NUL output keeps even whitespace-only names observable.
root=$(git rev-parse --show-toplevel)
ignored=$(mktemp)
trap 'rm -f -- "$ignored"' EXIT

if ! git -C "$root" ls-files -ci --exclude-standard -z > "$ignored"; then
  printf 'check-tracked-ignored: could not inspect the Git index\n' >&2
  exit 1
fi

if [[ -s "$ignored" ]]; then
  printf 'check-tracked-ignored: tracked ignored paths are forbidden:\n' >&2
  while IFS= read -r -d '' path; do
    printf '  %q\n' "$path" >&2
  done < "$ignored"
  exit 1
fi

printf 'check-tracked-ignored: clean\n'
