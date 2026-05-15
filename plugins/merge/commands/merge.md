---
description: Merge a source branch into the current branch with conflict resolution
argument-hint: <source-branch>
allowed-tools: Bash(git *), Read, Edit, Grep
model: sonnet
effort: high
---

Merge a source branch into the current branch, automatically resolving any merge conflicts. Defaults to git's normal behavior: fast-forward when possible, otherwise create a merge commit.

## Step 0: Validate preconditions

1. Run `git status --porcelain` to check for uncommitted changes. If the working tree is dirty, **stop** and tell the user to commit or stash changes first.
2. Run `git branch --show-current` to confirm the current branch. Display it and state that `$1` will be merged into it.
3. Verify the source branch `$1` exists by running `git rev-parse --verify $1`. If it does not exist, try `origin/$1` and use that ref for the rest of the flow if found. If neither exists, stop and report the error.

## Step 1: Determine what will be merged

1. Resolve the source ref (the local branch name if it exists, otherwise `origin/$1`) and store it as the merge source.
2. Run `git log --oneline --reverse <current-branch>..<source-ref>` to list the commits the merge would bring in.
3. Run `git merge-base <current-branch> <source-ref>` and compare to both tips to classify the merge:
   - **Already up to date:** source is an ancestor of current branch. Report this and stop — nothing to do.
   - **Fast-forward possible:** current branch is an ancestor of source. Note that the merge will fast-forward (no merge commit).
   - **Divergent:** both branches have unique commits. Note that the merge will create a merge commit.

## Step 2: Show summary before proceeding

Display:

- Current branch (target)
- Source branch (and whether it resolved to local or `origin/$1`)
- Merge type (fast-forward or merge commit)
- Number of commits to be brought in
- List of commits (short SHA + message) from Step 1

Ask the user to confirm before proceeding.

## Step 3: Run the merge

Run `git merge <source-ref>` (no flags — let git choose fast-forward vs merge commit based on history).

Check the exit code:

- **If successful (exit 0):** Report success — distinguish fast-forward (no new commit) from merge commit (new commit created). Skip to Step 5.
- **If conflicts (exit non-zero):** Proceed to Step 4.

## Step 4: Resolve conflicts

When the merge produces conflicts:

Resolve every conflicting file directly in this context using the `Read` and `Edit` tools. Do not spawn a subagent (the `Agent` tool is intentionally not in this command's `allowed-tools`) — delegating conflict resolution breaks the per-context Read-before-Edit guard and causes cascading `File has not been read yet` errors when the main thread later edits files the subagent touched.

### 4a: Identify all conflicting files

Run `git status` and identify files marked as "both modified", "both added", "deleted by us", "deleted by them", or any other conflict state.

### 4b: Understand the context

Run `git log --format="%B" <current-branch>..<source-ref>` to read the commit messages of the commits being merged in — this explains the intent of the changes coming from the source branch.

### 4c: Resolve each conflicting file

For each conflicting file:

1. **Read the file** using the Read tool to see full contents including conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`).

2. **Understand both sides:**
   - Between `<<<<<<< HEAD` and `=======` is the current branch version (where the merge is happening).
   - Between `=======` and `>>>>>>> <source-ref>` is what the source branch introduces.
   - Run `git show <source-ref>:<filepath>` to see the full source-branch version of the file for additional context.
   - Run `git log --oneline <current-branch>..<source-ref> -- <filepath>` to see which source-branch commits touched this file.

3. **Resolve the conflict intelligently:**
   - If changes are in different logical sections (different functions, different import groups), keep both.
   - If changes modify the same code, understand the intent of each side:
     - The current branch version reflects work done on the target branch.
     - The source branch version reflects work done in parallel on the branch being merged in.
     - Combine both intents — neither side is "more correct" by default in a merge (unlike cherry-pick).
   - If one side adds new code and the other modifies existing code, integrate both.
   - If both sides modify the same line differently, combine if logically compatible. If genuinely incompatible, prefer the change that preserves the most recent semantic intent and note the decision for the final summary.
   - For import statements or dependency lists: include all from both sides, removing exact duplicates.
   - **Never leave conflict markers in the file.**

4. **Edit the file** using the Edit tool to write the resolved version, removing all conflict markers.

5. **Stage the resolved file:** Run `git add <filepath>`.

### 4d: Handle special conflict types

- **"deleted by us" or "deleted by them":** Ask the user whether to keep the deletion or restore the file with the source-branch changes.
- **Binary file conflicts:** Do not attempt to merge. Ask the user which version to keep (`git checkout --ours <file>` or `git checkout --theirs <file>`), then `git add <file>`.

### 4e: Continue the merge

After all conflicts are resolved and staged:

1. Use the Grep tool to search for `<<<<<<<` across the repository to verify no conflict markers remain.
2. Run `git merge --continue` (which will open the merge-commit message editor — git uses the prepared default message non-interactively when run from this command). If the editor environment causes a hang, use `git commit --no-edit` instead to finalize with the default merge message.
3. If this fails, check `git status` for remaining issues.

## Step 5: Verify the result

After the merge has been applied:

1. Run `git log --oneline --graph -10` to show recent history, including the merge commit if one was created.
2. Run `git diff ORIG_HEAD..HEAD --stat` to show a summary of all changes the merge introduced into the current branch.
3. If any conflicts were resolved, list the files where conflicts occurred and briefly summarize how each was resolved.
4. Report final status: source and target branches, merge type (fast-forward or merge commit), how many commits were brought in, how many files had conflicts, whether all resolved successfully.

## Error handling

- If `git merge --continue` fails after resolution, run `git status` to diagnose remaining conflicts or unstaged changes.
- If `git commit --no-edit` (or `git merge --continue`) is rejected by a pre-commit hook (husky, lint-staged, etc.), read the hook output, fix the reported issues (type errors, lint failures, failed tests, residual duplicate declarations from conflict resolution), re-stage the corrected files, and retry the commit. **Never pass `--no-verify`** — the hook is catching latent bugs the merge introduced.
- If the user wants to abort, run `git merge --abort` to restore the original state.
- If the merge fails for reasons other than conflicts (e.g., refusing because of unrelated histories), report the error and ask the user how to proceed — do not silently pass `--allow-unrelated-histories` or other overrides without confirmation.
- If the source branch is identical to the current branch (already up to date), report this from Step 1 and exit cleanly — do not invoke `git merge`.
