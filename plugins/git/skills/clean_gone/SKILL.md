---
name: clean_gone
description: Delete local branches marked [gone] (deleted on the remote but still present locally), removing their worktrees first. Use when the user asks to "clean up branches", "prune gone branches", "delete stale local branches", or after remote branches were deleted (e.g. post-merge cleanup).
model: sonnet
effort: low
---

## Your Task

Clean up stale local branches that have been deleted from the remote repository, including any worktrees attached to them.

## Commands to Execute

0. **Optional — refresh remote-tracking refs first.** Branches only show `[gone]` after the deleted remote refs have been pruned. If you suspect tracking is stale, run:

   ```bash
   git fetch --prune
   ```

1. **List branches to identify any with [gone] status**

   ```bash
   git branch -v
   ```

   Note: branches with a `+` prefix have associated worktrees and must have their worktrees removed before deletion.

2. **Identify worktrees that may need to be removed for [gone] branches**

   ```bash
   git worktree list
   ```

3. **Remove worktrees and delete [gone] branches** (handles both regular and worktree branches)

   ```bash
   # Process all [gone] branches, removing '+' prefix if present
   git branch -v | grep '\[gone\]' | sed 's/^[+* ]//' | awk '{print $1}' | while read branch; do
     echo "Processing branch: $branch"
     # Find and remove worktree if it exists
     worktree=$(git worktree list | grep "\\[$branch\\]" | awk '{print $1}')
     if [ ! -z "$worktree" ] && [ "$worktree" != "$(git rev-parse --show-toplevel)" ]; then
       echo "  Removing worktree: $worktree"
       git worktree remove --force "$worktree"
     fi
     # Delete the branch
     echo "  Deleting branch: $branch"
     git branch -D "$branch"
   done
   ```

## Expected Behavior

After executing these commands, you will:

- See a list of all local branches with their status
- Identify and remove any worktrees associated with [gone] branches
- Delete all branches marked as [gone]
- Provide feedback on which worktrees and branches were removed

If no branches are marked as [gone], report that no cleanup was needed.
