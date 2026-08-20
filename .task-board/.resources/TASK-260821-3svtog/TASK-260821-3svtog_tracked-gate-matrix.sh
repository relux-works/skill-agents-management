#!/bin/bash
# Mutation matrix for TASK-260821-3svtog rework: does
# TestBuildOutputIsIgnoredAndSourcesAreNot still bite once the CLI sources are
# TRACKED? Each row builds a throwaway git repo from the candidate tree, so the
# story worktree is never touched.
set -u

SRC="/Users/alexis/src/relux-works/skill-agents-management/.temp/STORY-260821-224xfu/worktree"
WORK="/Users/alexis/src/relux-works/skill-agents-management/.temp/TASK-260821-3svtog/scratch"
TESTFILE="tools/agents-management/cmd/build_integration_test.go"
rm -rf "$WORK"; mkdir -p "$WORK"

# Candidate file set: tracked + untracked-not-ignored. Excludes the built
# binary and .temp by construction.
( cd "$SRC" && git ls-files -co --exclude-standard ) > "$WORK/filelist"

make_tree() { # $1 = name, $2 = tracked|untracked
  local d="$WORK/$1"
  mkdir -p "$d"
  ( cd "$SRC" && rsync -a --files-from="$WORK/filelist" . "$d/" ) || return 1
  ( cd "$d" && git init -q . && git config user.email t@t && git config user.name t )
  if [ "$2" = tracked ]; then
    ( cd "$d" && git add -A && git commit -q -m candidate )
  fi
  echo "$d"
}

mut_bare() { # drop the / anchors: the founding-commit bug
  perl -0pi -e 's{^/agents-management$}{agents-management}m;
                 s{^/tools/agents-management/agents-management$}{tools/agents-management/agents-management}m' "$1/.gitignore"
}
mut_pkgdir() { # a plausible future edit: ignore the tool's package directory
  printf 'pkg/agents-management/\n' >> "$1/.gitignore"
}
mut_dropnoindex() { # revert the fix under review
  perl -0pi -e 's{"check-ignore", "-q", "--no-index", "--"}{"check-ignore", "-q", "--"}' "$1/$TESTFILE"
}
mut_oldpathlist() { # revert the new non-existent paths, keep --no-index
  perl -0pi -e 's{\t\t"tools/agents-management/cmd/future_command.go"\n\t\t"pkg/agents-management/foo.go",\n}{}' "$1/$TESTFILE"
  perl -0pi -e 's{^\t\t"tools/agents-management/cmd/future_command\.go",\n}{}m;
                 s{^\t\t"pkg/agents-management/foo\.go",\n}{}m' "$1/$TESTFILE"
}

row() { # $1 name, $2 tracked|untracked, $3 expect RED|GREEN, rest: mutators
  local name="$1" state="$2" expect="$3"; shift 3
  local d; d=$(make_tree "$name" "$state") || { echo "$name: SETUP FAILED"; return; }
  for m in "$@"; do "$m" "$d"; done
  ( cd "$d" && go test -mod=mod ./tools/agents-management/cmd \
      -run TestBuildOutputIsIgnoredAndSourcesAreNot -count=1 ) > "$d/out.txt" 2>&1
  local code=$?
  local got=GREEN; [ $code -ne 0 ] && got=RED
  local verdict=OK; [ "$got" != "$expect" ] && verdict="*** MISMATCH ***"
  echo "== $name | sources=$state | expect=$expect | got=$got (exit=$code) | $verdict"
  sed -n '1,12p' "$d/out.txt" | sed 's/^/   /'
  echo
}

row A-tracked-bare-fixed       tracked   RED   mut_bare
# B: first prediction was GREEN (vacuous). Wrong: dropping --no-index alone
# still leaves the two NON-EXISTENT paths untracked, so check-ignore reports the
# pattern truth for them and the gate bites. The two halves of the fix are
# independently sufficient here. G isolates --no-index on the old six-path list.
row B-tracked-bare-noflag      tracked   RED   mut_bare mut_dropnoindex
row C-tracked-clean-fixed      tracked   GREEN
row D-untracked-bare-fixed     untracked RED   mut_bare
row E-tracked-pkgdir-fixed     tracked   RED   mut_pkgdir
row F-tracked-pkgdir-oldpaths  tracked   GREEN mut_pkgdir mut_oldpathlist
row G-tracked-bare-noflag-oldpaths tracked GREEN mut_bare mut_dropnoindex mut_oldpathlist
row H-tracked-bare-fixed-oldpaths  tracked RED   mut_bare mut_oldpathlist
