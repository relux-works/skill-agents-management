# Agents Management Repository Instructions

`AGENTS.md` is the canonical instruction file for this repository. `CLAUDE.md`
must remain a relative symlink to this file so every supported agent reads the
same rules.

## Scope

This repository owns the vendor model registry and the agent-management
packages that other skills consume. Changes here propagate to dependents
through a tagged release, so a landed commit is a published contract, not a
local edit.

## Change Workflow

- An authorized repository-changing task includes automatic delivery: stage
  only its reviewed scope, create author-signed commits, publish the feature
  branch, open or update the pull request, complete review and checks, and land
  the accepted exact head in `main`. Do not stop merely to ask whether the
  completed task should be committed, pushed, or landed.
- Every change must be reviewed through a pull request. After approval and
  passing checks, land the exact signed feature head with a plain fast-forward
  push to `main`; **never land through GitHub rebase, squash, or merge-commit
  actions.** Those buttons create different, unsigned commit objects and
  discard the reviewed head.
- Never push unreviewed work, a different commit from the reviewed PR head, or
  a forced update to `main`. GitHub records the reviewed PR as indirectly
  merged when its exact head becomes reachable from `main`, and the PR's
  `mergeCommit` then reports that same single-parent head rather than a merge.
- Before creating a branch or worktree, fetch `origin`, switch to local `main`,
  and fast-forward it to `origin/main`. Never branch from stale `main`.

  ```bash
  git fetch origin main
  git switch main
  git merge --ff-only origin/main
  test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
  git switch -c <branch-name>
  ```

- Use this signed linear landing flow after the PR is approved and its checks
  pass:

  ```bash
  BRANCH=$(git branch --show-current)
  test "$BRANCH" != main
  git fetch origin

  # If main advanced, rewrite locally and sign every rewritten commit.
  BEFORE_REBASE=$(git rev-parse HEAD)
  git rebase -S origin/main

  # Update only the feature branch when the signed head changed.
  if [ "$(git rev-parse HEAD)" != "$BEFORE_REBASE" ]; then
    git push --force-with-lease origin HEAD:refs/heads/"$BRANCH"
  fi
  ```

  If the feature head changed, stop here until that exact head passes the
  required PR review and checks. Then land it without rewriting:

  ```bash
  BRANCH=$(git branch --show-current)
  test "$BRANCH" != main
  git fetch origin

  # Verify every commit that will be introduced and require a fast-forward.
  for sha in $(git rev-list --reverse origin/main..HEAD); do
    git verify-commit "$sha" || exit 1
  done
  git merge-base --is-ancestor origin/main HEAD

  SIGNED_HEAD=$(git rev-parse HEAD)
  test "$(git rev-parse "origin/$BRANCH")" = "$SIGNED_HEAD"
  test "$(gh pr view --json headRefOid --jq .headRefOid)" = "$SIGNED_HEAD"

  # Plain push fails safely if main advanced. Never force main.
  git push origin HEAD:refs/heads/main
  git fetch origin
  test "$(git rev-parse origin/main)" = "$SIGNED_HEAD"
  git verify-commit "$SIGNED_HEAD"
  ```

  If the plain push to `main` is rejected, refresh and repeat the signed
  rebase/review cycle; do not fall back to a GitHub merge action or force the
  protected branch.

- Keep changes focused, review the diff, and run the relevant tests and
  validation before opening or updating a pull request.

## Releases

A dependent repository can only adopt a change through a tag, so tag after the
head is landed and verified on `main`, never from a feature branch. Tags are
signed and annotated. Verify the tag and the commit it points at before any
dependent bumps its dependency.

## Git Identity and Signing

Use the repository-local identity and SSH signing configuration below:

```bash
git config --local user.name "Ivan Oparin"
git config --local user.email "oparin@me.com"
git config --local gpg.format ssh
git config --local user.signingkey /Users/iv/.ssh/ivanopcode.pub
git config --local commit.gpgsign true
git config --local tag.gpgsign true
```

Every commit and tag must be signed. Verify commits with `git verify-commit`
before publishing them. Rebase locally with `git rebase -S` whenever history
must move; GitHub rebase and squash create different unsigned commit objects
and are not valid landing mechanisms. Agent co-authorship is allowed through an
explicit `Co-authored-by` trailer when the task or user requests it and a real,
intentionally configured agent name and email are available. Keep Ivan Oparin
as the commit author and never invent an identity or verification address.

## Documentation

Write repository documentation, code comments, and commit messages in English.
