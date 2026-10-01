#!/usr/bin/env bash
#
# Aggregate changelog.d/ fragments into CHANGELOG.md at release time.
#
# Landings add one fragment per change and never edit CHANGELOG.md. The tagger
# runs this script on a clean tree right before the tag:
#
#   .scripts/changelog-release.sh --check
#   .scripts/changelog-release.sh [--allow-empty] <version> <YYYY-MM-DD>
#
# --check validates every on-disk fragment (name grammar, non-empty, bullets
# only, no headings, UTF-8, trailing newline) and changes nothing. Release
# mode writes a "## <version> — <date>" section directly under the "# Changelog"
# title: the current "## Unreleased" bullets (kept for the transition) followed
# by the fragments in git add order (the commit that introduced each
# fragment's current path in its current lifetime; ties by name). It then
# removes the consumed fragments and leaves an empty "## Unreleased" heading.
# It never commits or tags; the tagger does.
#
# Refusals exit 1: an invalid version or date, a version already present in
# CHANGELOG.md, a dirty tree (including working-tree bytes hidden from git
# status by assume-unchanged, skip-worktree or ignorestat), an untracked or
# ignored fragment, a symlink fragment (working tree or git mode 120000 in
# either mode), an empty changelog.d/ without --allow-empty, invalid
# fragments, or a CHANGELOG.md without an "## Unreleased" section. Usage
# errors exit 2.
#
# All git reads run isolated from user, global and system config (see
# git_iso): the same repository yields the same bytes regardless of who runs
# the release. Fragment discovery and every git path read are NUL-delimited
# end to end, so a name containing a newline can neither hide nor split into
# two entries. Release aggregates committed bytes only: each fragment body
# comes from its HEAD blob, never from the working tree.
#
set -euo pipefail

PROG="changelog-release"
CHANGELOG="CHANGELOG.md"
FRAGDIR="changelog.d"
FRAGDOC="README.md"
NL=$'\n'

die() {
  echo "$PROG: $*" >&2
  exit 1
}

usage() {
  local code="${1:-2}"
  if [ "$code" -eq 0 ]; then
    cat <<'EOF'
usage: changelog-release.sh --check
       changelog-release.sh [--allow-empty] <version> <YYYY-MM-DD>
       changelog-release.sh -h|--help
EOF
  else
    cat >&2 <<'EOF'
usage: changelog-release.sh --check
       changelog-release.sh [--allow-empty] <version> <YYYY-MM-DD>
       changelog-release.sh -h|--help
EOF
  fi
  exit "$code"
}

# --- config-isolated git -----------------------------------------------------
# Every git read in this script runs isolated from user, global and system
# configuration that could change its output, and from GIT_CONFIG_*
# environment overrides. Local repository config is still read, so each call
# also pins its output-affecting options explicitly (--porcelain=v1,
# --untracked-files=all, --no-renames, -z, -c ...). The same repository
# always yields the same bytes regardless of who runs the release.
git_iso() {
  GIT_CONFIG_COUNT=0 \
  GIT_CONFIG_PARAMETERS='' \
  GIT_CONFIG_GLOBAL=/dev/null \
  GIT_CONFIG_SYSTEM=/dev/null \
  GIT_CONFIG_NOSYSTEM=1 \
  git -c core.quotePath=false -c color.ui=never -c diff.algorithm=myers \
    -c diff.renames=false -c log.showSignature=false -c log.showRoot=true \
    -c i18n.logOutputEncoding=UTF-8 --no-pager --no-optional-locks "$@"
}

# Capture each read in a directly called function, separately from predicates
# on its output. A nonzero exit (even with partial stdout) terminates the main
# shell, rather than becoming an empty/clean observation in a subshell.
git_read() {
  local target="$1" git_output git_status
  shift
  if git_output="$(git_iso "$@")"; then
    printf -v "$target" '%s' "$git_output"
  else
    git_status="$?"
    die "git $* failed (exit $git_status); nothing released"
  fi
}

# git_read_file is the same fail-closed contract for NUL-delimited or
# byte-exact output (path lists, blob bytes), which a shell variable cannot
# hold: the bytes land in a file, and only the checked exit decides.
git_read_file() {
  local file="$1" code
  shift
  if git_iso "$@" >"$file"; then
    :
  else
    code="$?"
    die "git $* failed (exit $code); nothing released"
  fi
}

# --- hidden-state and git-mode gates -----------------------------------------
# P1: git status trusts index flags (assume-unchanged, skip-worktree) and
# core.ignorestat. The byte gate below compares working-tree bytes with the
# HEAD blob directly, and the flag gate refuses hidden index states even
# when bytes currently match. P2: fragment type comes from git modes, not
# the filesystem, so a mode 120000 entry refuses in both modes whatever
# the checkout's symlink support.
HEAD_EXISTS=""
check_head_exists() {
  local code
  if git_iso rev-parse --verify --quiet HEAD -- >/dev/null 2>&1; then
    HEAD_EXISTS="yes"
  else
    code="$?"
    if [ "$code" -eq 1 ]; then
      HEAD_EXISTS="no"
    else
      die "git rev-parse --verify HEAD failed (exit $code); nothing released"
    fi
  fi
}

# check_index_symlink prints a file:line diagnostic and returns 1 when the
# index mode for $1 is 120000. Empty (untracked) passes. Dies on read failure.
check_index_symlink() {
  local entry="$1" tmp="$2" rec=""
  git_read_file "$tmp" ls-files -s -z -- "$entry"
  if [ ! -s "$tmp" ]; then
    return 0
  fi
  IFS= read -r -d '' rec <"$tmp" || [ -n "$rec" ]
  case "$rec" in
    120000\ *)
      echo "$entry:1: symlink in git (mode 120000); symlink fragments are not allowed" >&2
      return 1
      ;;
  esac
  return 0
}

# check_head_symlink is the same for the HEAD mode. The caller ensures HEAD
# exists; an empty read (path not in HEAD) passes. Dies on read failure.
check_head_symlink() {
  local entry="$1" tmp="$2" rec=""
  git_read_file "$tmp" ls-tree -z HEAD -- "$entry"
  if [ ! -s "$tmp" ]; then
    return 0
  fi
  IFS= read -r -d '' rec <"$tmp" || [ -n "$rec" ]
  case "$rec" in
    120000\ *)
      echo "$entry:1: symlink in git (mode 120000); symlink fragments are not allowed" >&2
      return 1
      ;;
  esac
  return 0
}

# refuse_hidden_state dies when $1 (a changelog input) hides uncommitted
# bytes from git status: working-tree bytes differing from the HEAD blob,
# or an index entry carrying assume-unchanged (lowercase tag) or
# skip-worktree (S). $2 is a scratch file for the flag read. Dies on any
# git read failure; empty flag output (untracked) passes here because the
# byte comparison or the tracked gate refuses it next.
refuse_hidden_state() {
  local path="$1" flag_tmp="$2" rec="" tag work_hash head_hash
  git_read_file "$flag_tmp" ls-files -v -z -- "$path"
  if [ -s "$flag_tmp" ]; then
    IFS= read -r -d '' rec <"$flag_tmp" || [ -n "$rec" ]
    tag="${rec:0:1}"
    case "$tag" in
      [a-z]|S) die "$path carries hidden index state ($tag: assume-unchanged/skip-worktree); clear the flag and commit before release" ;;
    esac
  fi
  git_read work_hash hash-object --no-filters -- "$path"
  git_read head_hash rev-parse "HEAD:$path"
  if [ "$work_hash" != "$head_hash" ]; then
    die "$path differs from HEAD; commit or stash before release"
  fi
}

# Scratch lives in one private directory outside the repository, created
# lazily so --help and usage errors leave no trace. work_tmp is global so
# the EXIT trap can see it under nounset; successful runs remove the trap
# and the directory explicitly, refusing runs let the trap clean up.
work_tmp=""
need_workdir() {
  if [ -n "$work_tmp" ]; then
    return 0
  fi
  if work_tmp="$(mktemp -d 2>/dev/null)"; then
    trap 'rm -rf "$work_tmp"' EXIT
  else
    die "cannot create temporary directory"
  fi
}

# --- repository root -------------------------------------------------------
# Release ordering reads the fragments' add commits, so both modes run inside
# a git checkout and operate on its top level.
git_read ROOT rev-parse --show-toplevel
[ -n "$ROOT" ] || die "not inside a git checkout"
cd "$ROOT"

# --- fragment listing ------------------------------------------------------
# Every directory entry except README.md is a fragment candidate, including
# dotfiles: find (not globbing) so a misnamed entry can never hide from --check
# by failing to match "*". The list is NUL-delimited end to end (find -print0,
# sort -z, read -d ''): a name containing a newline is one entry, never two
# lines, and no line-based loop ever touches a path. list_fragments writes the
# byte-sorted list to the file named by $1; each stage checks its own status.
list_fragments() {
  local out="$1" raw code
  raw="$work_tmp/list.raw"
  if [ ! -d "$FRAGDIR" ]; then
    : >"$out"
    return 0
  fi
  if find "$FRAGDIR" -mindepth 1 -maxdepth 1 ! -name "$FRAGDOC" -print0 >"$raw"; then
    :
  else
    code="$?"
    die "find $FRAGDIR failed (exit $code); nothing released"
  fi
  if LC_ALL=C sort -z -o "$out" "$raw"; then
    :
  else
    code="$?"
    die "sort $FRAGDIR listing failed (exit $code); nothing released"
  fi
}

# --- fragment validation ---------------------------------------------------
# check_fragment prints "path:line: message" diagnostics and returns 1 on any
# violation. File-level violations (name, shape, encoding) report line 1;
# content violations report the offending line. The first violation per file
# wins; every file is still visited. $1 is the bytes to read, $2 the fragment
# name for the grammar, $3 (optional) the path named in diagnostics: release
# validates committed blob bytes while still blaming the fragment path.
check_fragment() {
  local path="$1" base="$2" display="${3:-$1}" id last

  # Name grammar: <id>.md, letters/digits/dash, starting and ending alnum.
  # Uppercase is admitted because board ids are uppercase; lowercase slugs are
  # a convention the grammar does not enforce. Control characters (including
  # newlines, which NUL-delimited discovery delivers intact) match nothing
  # here and refuse.
  case "$base" in
    *.md) : ;;
    *) echo "$display:1: invalid fragment name $base: want <id>.md (letters, digits, -)" >&2; return 1 ;;
  esac
  id="${base%.md}"
  case "$id" in
    "" | -* | *- | *[!A-Za-z0-9-]*)
      echo "$display:1: invalid fragment name $base: want <id>.md (letters, digits, -)" >&2
      return 1
      ;;
  esac

  # A symlink is refused before -f can follow it: its target bytes are not
  # committed content, and an ignored target is invisible to the dirty gate.
  if [ -L "$path" ]; then
    echo "$display:1: symlink fragments are not allowed" >&2
    return 1
  fi
  [ -f "$path" ] || { echo "$display:1: not a regular file" >&2; return 1; }
  [ -s "$path" ] || { echo "$display:1: empty fragment" >&2; return 1; }

  if command -v iconv >/dev/null 2>&1; then
    iconv -f UTF-8 -t UTF-8 "$path" >/dev/null 2>&1 \
      || { echo "$display:1: invalid UTF-8" >&2; return 1; }
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c 'import sys; open(sys.argv[1], encoding="utf-8").read()' "$path" 2>/dev/null \
      || { echo "$display:1: invalid UTF-8" >&2; return 1; }
  else
    die "cannot validate UTF-8: neither iconv nor python3 is available"
  fi

  if [ "$(tail -c 1 "$path" | wc -l)" -eq 0 ]; then
    last="$(wc -l <"$path")"
    last="$((last + 1))"
    echo "$display:$last: missing trailing newline" >&2
    return 1
  fi

  # Body: one or more "- " bullets with indented continuations, separated by
  # blank lines. Headings, front matter and bare text are refused with the
  # offending line. LC_ALL=C keeps the ASCII anchoring exact on any locale.
  LC_ALL=C awk -v path="$display" '
    function fail(n, msg) { print path ":" n ": " msg; bad = 1 }
    /^[ \t]*$/ { next }
    /^#/ { fail(NR, "heading not allowed in a fragment"); next }
    NR == 1 && /^---$/ { fail(NR, "front matter not allowed in a fragment"); next }
    /^- / {
      if ($0 ~ /^- +$/) { fail(NR, "empty bullet"); next }
      bullets++
      next
    }
    /^[ \t]/ {
      if (bullets == 0) { fail(NR, "indented line before any bullet"); next }
      next
    }
    { fail(NR, "not a bullet or continuation line: top-level lines must start with \"- \""); next }
    END {
      if (bullets == 0 && bad == 0) fail(1, "no bullets: body must contain at least one \"- \" bullet")
      exit bad
    }
  ' "$path" >&2 || return 1

  return 0
}

cmd_check() {
  [ -d "$FRAGDIR" ] || die "no $FRAGDIR directory"
  need_workdir
  local list="$work_tmp/list" ls_tmp="$work_tmp/ls" tree_tmp="$work_tmp/tree"
  local count=0 bad=0 entry base
  check_head_exists
  list_fragments "$list"
  while IFS= read -r -d '' entry || [ -n "$entry" ]; do
    [ -n "$entry" ] || continue
    base="${entry##*/}"
    count="$((count + 1))"
    if ! check_fragment "$entry" "$base"; then
      bad=1
      continue
    fi
    if ! check_index_symlink "$entry" "$ls_tmp"; then
      bad=1
      continue
    fi
    if [ "$HEAD_EXISTS" = "yes" ]; then
      if ! check_head_symlink "$entry" "$tree_tmp"; then
        bad=1
        continue
      fi
    fi
  done <"$list"
  [ "$bad" -eq 0 ] || exit 1
  echo "$PROG: $count fragment(s) valid"
  trap - EXIT
  rm -rf "$work_tmp"
  work_tmp=""
}

# --- release validation ----------------------------------------------------
validate_version() {
  case "$1" in
    v[0-9]*.[0-9]*.[0-9]*)
      case "$1" in
        v*.*.*.*) die "invalid version $1: want vN.N.N" ;;
      esac
      local rest="${1#v}"
      case "$rest" in
        *[!0-9.]* | "" | .* | *. | *..*) die "invalid version $1: want vN.N.N" ;;
      esac
      ;;
    *) die "invalid version $1: want vN.N.N" ;;
  esac
}

validate_date() {
  local date="$1" y m d max rest
  case "$date" in
    ????-??-??) : ;;
    *) die "invalid date $date: want YYYY-MM-DD" ;;
  esac
  y="${date%%-*}"
  rest="${date#*-}"
  m="${rest%%-*}"
  d="${rest#*-}"
  case "$y$m$d" in
    *[!0-9]*) die "invalid date $date: want YYYY-MM-DD" ;;
  esac
  y="$((10#$y))"; m="$((10#$m))"; d="$((10#$d))"
  { [ "$m" -ge 1 ] && [ "$m" -le 12 ]; } || die "invalid date $date: month out of range"
  case "$m" in
    1|3|5|7|8|10|12) max=31 ;;
    4|6|9|11) max=30 ;;
    2)
      if [ "$((y % 4))" -eq 0 ] && { [ "$((y % 100))" -ne 0 ] || [ "$((y % 400))" -eq 0 ]; }; then
        max=29
      else
        max=28
      fi
      ;;
  esac
  { [ "$d" -ge 1 ] && [ "$d" -le "$max" ]; } || die "invalid date $date: day out of range"
}

# duplicate_version returns 0 when CHANGELOG.md already carries the version in
# a "## " header. Only headers are searched, so an Unreleased bullet citing a
# future version never collides; the version is matched as a delimited token
# so v0.5.3 never collides with a released v0.5.39.
duplicate_version() {
  local found
  # One read, consumed to EOF: an early match cannot SIGPIPE an upstream
  # reader, and a file read failure is distinguished from no match.
  if found="$(LC_ALL=C awk -v version="$1" '
    BEGIN { gsub(/[.]/, "[.]", version); pattern = "(^|[^0-9.])" version "([^0-9.]|$)" }
    /^## / && $0 ~ pattern { found = 1 }
    END { print found ? "yes" : "no" }
  ' "$CHANGELOG")"; then
    [ "$found" = "yes" ]
  else
    die "cannot read $CHANGELOG for duplicate versions"
  fi
}

# note_history records seq as the latest add commit for one history name.
# The last call wins, so a re-added fragment orders by its re-add. The
# order_* arrays are globals reset by each order_fragments call (indexed
# arrays stay portable to bash 3.2; values may contain newlines, never NUL).
note_history() {
  local name="$1" seq="$2" i
  i=0
  while [ "$i" -lt "$order_count" ]; do
    if [ "$name" = "${order_frags[$i]}" ]; then order_lasts[$i]="$seq"; fi
    i=$((i + 1))
  done
}

# order_fragments writes the NUL fragment list ($1) in git add order
# (NUL-delimited) to $3, using the NUL history read ($2): the commit that
# introduced the fragment's CURRENT path in its current lifetime. A deleted
# then re-added fragment orders by its re-add (the LAST
# "git log --diff-filter=A" appearance); a renamed fragment orders by the
# commit that created the new name (--no-renames renders the rename as a
# delete plus an add). Ties (one commit adding several fragments) keep
# discovery order, which is byte-sorted, so ties keep name order without any
# locale-dependent comparison. Unknown add history refuses rather than
# inventing an order. History is read and checked by cmd_release before this
# formatter runs; -z framing, --root and fixed encoding/format/diff flags
# make local presentation config irrelevant.
#
# History records are COMMIT headers and path names. The first name after
# each header carries one literal leading newline (the --name-only blank
# separator), stripped exactly once; a name may itself contain newlines, and
# only NUL separates records. A name that matches nothing is skipped.
order_fragments() {
  local list="$1" history="$2" out="$3"
  local entry rec rest name seq i best best_seq count
  order_frags=()
  order_lasts=()
  order_count=0
  while IFS= read -r -d '' entry || [ -n "$entry" ]; do
    [ -n "$entry" ] || continue
    order_frags+=("$entry")
    order_lasts+=("")
    order_count=$((order_count + 1))
  done <"$list"
  if [ "$order_count" -eq 0 ]; then
    : >"$out"
    return 0
  fi
  seq=0
  while IFS= read -r -d '' rec || [ -n "$rec" ]; do
    case "$rec" in
      COMMIT\ *)
        seq=$((seq + 1))
        rest="${rec#COMMIT }"
        case "$rest" in
          *"$NL"*)
            name="${rest#*"$NL"}"
            if [ -n "$name" ]; then
              note_history "$name" "$seq"
            fi
            ;;
        esac
        ;;
      *)
        case "$rec" in
          "$NL"*) rec="${rec#"$NL"}" ;;
        esac
        if [ -n "$rec" ]; then
          note_history "$rec" "$seq"
        fi
        ;;
    esac
  done <"$history"
  i=0
  while [ "$i" -lt "$order_count" ]; do
    if [ -z "${order_lasts[$i]}" ]; then
      echo "$PROG: git log has no add history for ${order_frags[$i]}" >&2
      return 1
    fi
    i=$((i + 1))
  done
  : >"$out"
  count=0
  while [ "$count" -lt "$order_count" ]; do
    best=""
    best_seq=""
    i=0
    while [ "$i" -lt "$order_count" ]; do
      if [ -n "${order_lasts[$i]}" ]; then
        if [ -z "$best_seq" ] || [ "${order_lasts[$i]}" -lt "$best_seq" ]; then
          best="$i"
          best_seq="${order_lasts[$i]}"
        fi
      fi
      i=$((i + 1))
    done
    printf '%s\0' "${order_frags[$best]}" >>"$out"
    order_lasts[$best]=""
    count=$((count + 1))
  done
}

# strip_blanks drops leading and trailing blank lines, keeping the interior.
# It reads the named file, or stdin when given none.
strip_blanks() {
  if [ "$#" -eq 0 ]; then
    LC_ALL=C awk '/^[ \t]*$/ { if (started) pending++; next } { started = 1; while (pending > 0) { print ""; pending-- } print }'
  else
    LC_ALL=C awk '/^[ \t]*$/ { if (started) pending++; next } { started = 1; while (pending > 0) { print ""; pending-- } print }' "$1"
  fi
}

cmd_release() {
  local version="$1" date="$2" allow_empty="$3"
  local entry base blob bad frag carry first_part n
  local status ignore_status ls_rec tree_rec
  local list_tmp ls_tmp flag_tmp tree_tmp history_tmp ordered_tmp blobdir inner_tmp new_tmp

  validate_version "$version"
  validate_date "$date"

  need_workdir
  list_tmp="$work_tmp/list"
  ls_tmp="$work_tmp/ls"
  flag_tmp="$work_tmp/flag"
  tree_tmp="$work_tmp/tree"
  history_tmp="$work_tmp/history"
  ordered_tmp="$work_tmp/ordered"
  blobdir="$work_tmp/blobs"
  inner_tmp="$work_tmp/inner"
  new_tmp="$work_tmp/new"
  mkdir "$blobdir" || die "cannot create temporary directory"

  git_read status status --porcelain=v1 --untracked-files=all
  if [ -n "$status" ]; then
    echo "$PROG: refusing to release on a dirty tree; commit or stash first:" >&2
    printf '%s\n' "$status" >&2
    exit 1
  fi

  [ -f "$CHANGELOG" ] || die "no $CHANGELOG"
  # The carry-over below reads the working tree, so hidden CHANGELOG edits
  # (assume-unchanged, skip-worktree, ignorestat) must refuse before any
  # other CHANGELOG read trusts those bytes.
  refuse_hidden_state "$CHANGELOG" "$flag_tmp"
  grep -E -q "^## Unreleased$" "$CHANGELOG" || die "no ## Unreleased section in $CHANGELOG"

  if duplicate_version "$version"; then
    die "version $version is already present in $CHANGELOG"
  fi

  list_fragments "$list_tmp"
  if [ ! -s "$list_tmp" ] && [ "$allow_empty" != "yes" ]; then
    die "no fragments in $FRAGDIR (pass --allow-empty to release the Unreleased carry-over only)"
  fi

  # Fragments must be committed regular files, and the release aggregates
  # committed bytes only. An untracked or ignored fragment has no add commit
  # and would be deleted by the release without any git copy. The dirty gate
  # already refuses untracked files, but ignored paths are invisible to
  # status, so every fragment is checked explicitly; a successful empty
  # ls-files read means untracked, while an error is never classified as
  # absence, including errors accompanied by partial output. A symlink is
  # refused outright, in the working tree and at git mode 120000 (index or
  # HEAD) alike: its target bytes are not committed content, and an ignored
  # target is invisible to the dirty gate. Hidden working-tree edits
  # (assume-unchanged, skip-worktree, ignorestat) refuse via byte comparison
  # before any blob is fetched, so edited inputs are never deleted. Each
  # surviving fragment body is then fetched from its HEAD blob, never from
  # the working tree, and validated; every fragment is still visited so one
  # run reports every violation. --check deliberately validates on-disk
  # fragments regardless, so landings can validate before committing;
  # release is the gate that demands committed fragments.
  bad=0
  n=0
  while IFS= read -r -d '' entry || [ -n "$entry" ]; do
    [ -n "$entry" ] || continue
    git_read_file "$ls_tmp" ls-files -s -z -- "$entry"
    if [ ! -s "$ls_tmp" ]; then
      if git_iso check-ignore -q -- "$entry"; then
        die "fragment $entry is ignored by .gitignore; remove the ignore and commit it before release"
      else
        ignore_status="$?"
        case "$ignore_status" in
          1) die "fragment $entry is untracked; commit it before release" ;;
          *) die "git check-ignore -q -- $entry failed (exit $ignore_status); nothing released" ;;
        esac
      fi
    fi
    if [ -L "$entry" ]; then
      die "fragment $entry is a symlink; symlink fragments are not allowed"
    fi
    ls_rec=""
    IFS= read -r -d '' ls_rec <"$ls_tmp" || [ -n "$ls_rec" ]
    case "$ls_rec" in
      120000\ *) die "fragment $entry is a symlink in git (mode 120000); symlink fragments are not allowed" ;;
      100644\ *|100755\ *) : ;;
      *) die "fragment $entry is not a committed regular file; nothing released" ;;
    esac
    git_read_file "$tree_tmp" ls-tree -z HEAD -- "$entry"
    if [ -s "$tree_tmp" ]; then
      tree_rec=""
      IFS= read -r -d '' tree_rec <"$tree_tmp" || [ -n "$tree_rec" ]
      case "$tree_rec" in
        120000\ *) die "fragment $entry is a symlink in git (mode 120000); symlink fragments are not allowed" ;;
      esac
    fi
    refuse_hidden_state "$entry" "$flag_tmp"
    base="${entry##*/}"
    blob="$blobdir/$base"
    git_read_file "$blob" cat-file blob "HEAD:$entry"
    check_fragment "$blob" "$base" "$entry" || bad=1
    n=$((n + 1))
  done <"$list_tmp"
  [ "$bad" -eq 0 ] || exit 1

  # All Git observations and ordering checks finish before CHANGELOG.md is
  # touched. The root diff, name-only format, encoding, NUL framing and diff
  # behavior are explicit; external diff and textconv commands are never
  # invoked.
  git_read_file "$history_tmp" log --root --no-color --no-renames --no-ext-diff --no-textconv \
    --diff-filter=A --reverse --format="COMMIT %H" --name-only -z -- "$FRAGDIR"
  if order_fragments "$list_tmp" "$history_tmp" "$ordered_tmp"; then
    :
  else
    die "git log could not establish fragment add order; nothing released"
  fi

  # Assemble the section body: the Unreleased carry-over (kept for the
  # transition) followed by the fragments in add order. Parts join with
  # exactly one blank line. Fragment bodies come from the fetched HEAD blobs,
  # never from the working tree.
  {
    first_part="yes"
    carry="$(LC_ALL=C awk '/^## Unreleased$/ { in_unrel = 1; next } /^## / { if (in_unrel) exit } in_unrel { print }' "$CHANGELOG" | strip_blanks)"
    if [ -n "$carry" ]; then
      printf '%s\n' "$carry"
      first_part="no"
    fi
    if [ -s "$ordered_tmp" ]; then
      while IFS= read -r -d '' frag || [ -n "$frag" ]; do
        [ -n "$frag" ] || continue
        if [ "$first_part" = "no" ]; then
          printf '\n'
        fi
        strip_blanks "$blobdir/${frag##*/}"
        first_part="no"
      done <"$ordered_tmp"
    fi
  } >"$inner_tmp"

  # Splice: the new dated section goes directly under the title (and its
  # preamble, when one exists), newest-first. The old Unreleased body moved
  # into the new section, so Unreleased is re-emitted empty; prior version
  # sections and the tail follow byte-identical apart from blank separators.
  LC_ALL=C awk -v version="$version" -v date="$date" -v inner="$inner_tmp" '
    function is_blank(s) { return s ~ /^[ \t]*$/ }
    BEGIN {
      header = "## " version " — " date
      inner_count = 0
      while ((getline line < inner) > 0) inner_lines[++inner_count] = line
      close(inner)
      phase = "title"
    }
    phase == "title" {
      if (is_blank($0)) next
      title_line = $0
      phase = "preamble"
      next
    }
    phase == "preamble" {
      if ($0 ~ /^## Unreleased$/) { phase = "unreleased"; next }
      if ($0 ~ /^## /) { phase = "versions"; version_lines[++version_count] = $0; next }
      preamble_lines[++preamble_count] = $0
      next
    }
    phase == "versions" {
      if ($0 ~ /^## Unreleased$/) { phase = "unreleased"; next }
      version_lines[++version_count] = $0
      next
    }
    phase == "unreleased" {
      if ($0 ~ /^## /) { phase = "tail"; tail_lines[++tail_count] = $0; next }
      next
    }
    phase == "tail" { tail_lines[++tail_count] = $0; next }
    END {
      print title_line
      while (preamble_count > 0 && is_blank(preamble_lines[1])) {
        for (i = 1; i < preamble_count; i++) preamble_lines[i] = preamble_lines[i + 1]
        preamble_count--
      }
      while (preamble_count > 0 && is_blank(preamble_lines[preamble_count])) preamble_count--
      if (preamble_count > 0) {
        print ""
        for (i = 1; i <= preamble_count; i++) print preamble_lines[i]
      }
      print ""
      print header
      if (inner_count > 0) {
        print ""
        for (i = 1; i <= inner_count; i++) print inner_lines[i]
      }
      print ""
      print "## Unreleased"
      while (version_count > 0 && is_blank(version_lines[1])) {
        for (i = 1; i < version_count; i++) version_lines[i] = version_lines[i + 1]
        version_count--
      }
      while (version_count > 0 && is_blank(version_lines[version_count])) version_count--
      if (version_count > 0) {
        print ""
        for (i = 1; i <= version_count; i++) print version_lines[i]
      }
      if (tail_count > 0) {
        print ""
        for (i = 1; i <= tail_count; i++) print tail_lines[i]
      }
    }
  ' "$CHANGELOG" >"$new_tmp"

  cat "$new_tmp" >"$CHANGELOG"

  if [ -s "$list_tmp" ]; then
    while IFS= read -r -d '' frag || [ -n "$frag" ]; do
      [ -n "$frag" ] || continue
      rm -f "$frag"
    done <"$list_tmp"
  fi
  trap - EXIT
  rm -rf "$work_tmp"
  work_tmp=""

  echo "$PROG: wrote ## $version — $date ($n fragment(s) plus the Unreleased carry-over)"
  echo "$PROG: review the diff, then commit and tag; this script commits nothing"
}

# --- argument parsing ------------------------------------------------------
# Positionals accumulate in an array so empty and whitespace-padded arguments
# survive verbatim into validation instead of being mangled by word-splitting.
MODE="" ALLOW_EMPTY="no"
POSITIONAL=()
for arg in "$@"; do
  case "$arg" in
    -h | --help) usage 0 ;;
    --check) MODE="check" ;;
    --allow-empty) ALLOW_EMPTY="yes" ;;
    --*) echo "$PROG: unknown flag $arg" >&2; usage ;;
    *) POSITIONAL+=("$arg") ;;
  esac
done
if [ "${#POSITIONAL[@]}" -eq 0 ]; then
  set --
else
  set -- "${POSITIONAL[@]}"
fi

if [ "$MODE" = "check" ]; then
  [ "$#" -eq 0 ] || { echo "$PROG: --check takes no arguments" >&2; usage; }
  [ "$ALLOW_EMPTY" = "no" ] || { echo "$PROG: --allow-empty applies to release mode only" >&2; usage; }
  cmd_check
  exit 0
fi

if [ "$MODE" = "" ] && [ "$#" -eq 0 ] && [ "$ALLOW_EMPTY" = "no" ]; then
  usage
fi
[ "$#" -eq 2 ] || { echo "$PROG: release mode wants <version> <YYYY-MM-DD>" >&2; usage; }
cmd_release "$1" "$2" "$ALLOW_EMPTY"
