# Changelog fragments

Landings never edit `CHANGELOG.md` directly. Each landing adds one fragment
here, and the tagger aggregates the fragments into `CHANGELOG.md` in a single
release commit right before the tag.

## Grammar

- One file per change, named `<id>.md`, where `<id>` is a lowercase slug or a
  board id. The name matches `[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?\.md`:
  letters, digits, `-`, starting and ending with a letter or digit.
  Uppercase survives only for board ids such as `TASK-261002-2w6op9.md`;
  slugs stay lowercase by convention.
- The body is one or more Markdown bullets (`- ...`) in the existing
  CHANGELOG voice, with continuation lines indented. Blank lines may separate
  bullets. At least one top-level `- ` bullet is required.
- No headings, no front matter, no top-level text outside bullets.
- The file must be non-empty, valid UTF-8, and end with a trailing newline.
- Symlinks are refused outright, in `--check` and release alike: a link's
  target bytes are not committed content. Commit a regular file instead.

This file (`README.md`) documents the grammar; it is not a fragment and is
ignored by the release script.

## Validation and release

```sh
.scripts/changelog-release.sh --check
.scripts/changelog-release.sh [--allow-empty] <version> <YYYY-MM-DD>
```

`--check` validates every on-disk fragment and changes nothing, so landings
can validate before committing. Release mode writes a
`## <version> — <date>` section directly under the `# Changelog` title (the
current `## Unreleased` bullets, kept for the transition, followed by the
fragments in add order, ties by name), removes the consumed fragments, and
leaves an empty `## Unreleased` heading. It refuses an invalid version or
date, a version already present in `CHANGELOG.md`, a dirty tree (including
bytes hidden from `git status` by assume-unchanged, skip-worktree or
ignorestat), an untracked, ignored or symlink fragment, or an empty
`changelog.d/` (unless `--allow-empty`). It never commits or tags; the tagger
does.

Discovery is NUL-delimited end to end (`find -print0`, `sort -z`, `read
-d ''`): every directory entry is visited exactly once under its exact
bytes, so a name containing a newline can neither hide nor split into two
entries. Any name outside the grammar above — including control characters
— refuses in both modes.

## Add order

Fragments are emitted in the order of the commit that introduced each
fragment's current path in its current lifetime
(`git log --root --no-renames --diff-filter=A`, last appearance wins):

- A name consumed by an earlier release and later re-added orders by its
  re-add, not by its first-ever add. Slug names are reusable by design.
- A renamed fragment orders by the commit that created the new name (the
  rename renders as a delete plus an add).
- Fragments added in the same commit tie-break by name.

All Git reads suppress environment, global and system config overrides. The
`--no-optional-locks` flag also avoids status refreshing the index on disk. The
repository-local output options are pinned too: `core.quotePath=false`,
`color.ui=never`, `diff.algorithm=myers`, `diff.renames=false`,
`log.showSignature=false`, `log.showRoot=true` and
`i18n.logOutputEncoding=UTF-8`. History uses `--root --no-color --no-renames
--no-ext-diff --no-textconv --reverse --format="COMMIT %H" --name-only -z`;
the tracked/mode read uses `ls-files -s -z`; the flag read uses
`ls-files -v -z`; the HEAD mode read uses `ls-tree -z HEAD`; the byte gate
uses `hash-object --no-filters` against `rev-parse HEAD:<path>`; status uses
`--porcelain=v1 --untracked-files=all`. External diff and textconv drivers
cannot affect these reads. Thus valid local presentation settings cannot
change fragment discovery, the dirty gate or add order.

Each Git read checks its own exit status before any release output is written
or fragment removed. A failed read refuses with the Git command and exit code,
even if it printed partial output. Invalid local config may cause Git itself
to refuse while loading the config; this is a read failure, never evidence of
a clean tree. A successful history read with missing add history also refuses
rather than inventing an order. The duplicate-version check reads the changelog
once to EOF, so matches do not depend on pipeline buffer size.

## Committed fragments

Release consumes only committed fragments, and only committed bytes: each
fragment body comes from its `HEAD` blob (`git cat-file blob HEAD:<path>`),
never from the working tree. An untracked fragment is refused by the dirty
gate (`--untracked-files=all`, immune to `status.showUntrackedFiles=no`);
an ignored fragment — invisible to `git status` — is refused explicitly
with the fragment path. A fragment or `CHANGELOG.md` whose working-tree
bytes differ from `HEAD` is refused by the byte gate (`hash-object
--no-filters` against `rev-parse HEAD:<path>`) before any blob is fetched,
even when `git status` is clean: assume-unchanged, skip-worktree and
`core.ignorestat` cannot hide edits from the release. An index entry
carrying assume-unchanged or skip-worktree for those paths refuses even
when bytes currently match. A symlink fragment is refused outright, in the
working tree and at git mode `120000` (index or `HEAD`) alike, in `--check`
and release: the mode check also covers `core.symlinks=false` checkouts,
where the link reads as a regular file. Commit every fragment and clear
hidden index flags before running the release; `--check` stays on-disk-only
(except for the git-mode symlink read) so it can run before the commit.
