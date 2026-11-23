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

3. **Remove worktrees and delete [gone] branches** (handles both regular and worktree branches). Run the script with Bash, exactly as written:

   ```bash
   ${CLAUDE_SKILL_DIR}/scripts/clean-gone.sh
   ```

   It selects branches whose upstream tracking status is `[gone]`, removes each one's worktree (never the main worktree), then deletes the branch with `git branch -D`. It never forces a worktree removal: when a worktree has uncommitted or untracked changes, or is locked, git refuses, and the script prints `Skipped` and keeps that branch. It prints one `Processing` / `Removing worktree` / `Deleting branch` / `Skipped` line per action, or `nothing to clean` when no branch is gone. Report every skipped branch and its worktree path so the user can commit, stash, or remove it themselves. Never retry a skipped worktree with `--force`, even though git's error message suggests it.

## Expected Behavior

After executing these commands, you will:

- See a list of all local branches with their status
- Identify and remove any worktrees associated with [gone] branches
- Delete all branches marked as [gone]
- Provide feedback on which worktrees and branches were removed

If no branches are marked as [gone], report that no cleanup was needed.
