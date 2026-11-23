#!/usr/bin/env bash
# Delete local branches whose upstream is [gone], removing their worktrees first.
#
# Gone branches come from `%(upstream:track)`, not `git branch -v | grep`, so a
# commit subject containing "[gone]" can't mark a live branch for deletion.
# Worktrees are matched by exact `branch refs/heads/<name>` in porcelain output.
# The main worktree is never removed, and a worktree git refuses to remove
# (dirty or locked) keeps its branch.
set -u

top=$(git rev-parse --show-toplevel) || exit 1
gone=$(git for-each-ref --format='%(refname:short) %(upstream:track)' refs/heads | awk '$2 == "[gone]" { print $1 }')

if [ -z "$gone" ]; then
  echo "No [gone] branches; nothing to clean."
  exit 0
fi

while read -r branch; do
  echo "Processing branch: $branch"
  worktree=$(git worktree list --porcelain | awk -v ref="branch refs/heads/$branch" '/^worktree / { wt = substr($0, 10) } $0 == ref { print wt }')
  if [ -n "$worktree" ] && [ "$worktree" != "$top" ]; then
    echo "  Removing worktree: $worktree"
    # No --force: a dirty or locked worktree makes git refuse, so its work is kept.
    if ! git worktree remove "$worktree"; then
      echo "  Skipped: worktree has uncommitted or untracked changes, or is locked; branch kept"
      continue
    fi
  fi
  echo "  Deleting branch: $branch"
  git branch -D "$branch"
done <<< "$gone"
